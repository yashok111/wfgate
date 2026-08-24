package check

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"git.sumka.site/yakov/wfgate/internal/gitx"
)

// The drift check answers the question verify's "target moved" mismatch opens:
// WHICH citations does the move invalidate. Anchored citations self-report
// through `check cite` once the target moves — the anchor falls out of range.
// The unanchored majority pass a range check forever (both lines keep
// existing), and this intersection of the target's diff with the cited ranges
// is the only mechanical signal they get.
//
// Two kinds are reported because they demand different work. A citation whose
// cited lines were EDITED is a claim that may no longer be true: re-read.
// A citation merely SHIFTED by insertions or deletions above it still points at
// the same content — the suggested new range makes re-aiming a patch instead of
// a search, the same trade the anchor check's "nearest occurrence" makes.

// Drift is one citation the target's movement affected.
type Drift struct {
	Citation
	Kind     string // "edited" | "gone" | "shifted" | "unresolved"
	NewStart int    // shifted only: where the same lines live now
	NewEnd   int
	Msg      string
}

func (d Drift) String() string {
	return fmt.Sprintf("%s:%d: %s: %s  [%s]", d.Artifact, d.AtLine, d.Kind, d.Msg, d.Raw)
}

// hunk is one old-side region the target's diff touched. Counts follow unified
// hunk headers: OldCount 0 is a pure insertion after line OldStart.
type hunk struct {
	oldStart, oldCount int
	newStart, newCount int
}

// fileDiff is everything the three readers of a unified diff need from one
// file's entry. ONE parser, not three: the `-- ` trap below is a rule with a
// measured cost behind it, and a second copy of it is the defect class this
// project names as its most frequent.
type fileDiff struct {
	newPath string
	deleted bool
	isNew   bool // the file did not exist before this diff
	hunks   []hunk
	removed []string // the OLD text of every replacement, without its `-`
	added   []string
}

// FindDrift intersects the target's frozenRef..HEAD diff with the cited ranges.
// It returns the drifted citations and the count of clean ones — the clean
// count is printed too, because "0 drifted" over 0 checked citations must not
// read like a verified corpus.
func FindDrift(target, frozenRef string, cits []Citation) ([]Drift, int, error) {
	ix, err := NewIndexAt(target, frozenRef)
	if err != nil {
		return nil, 0, err
	}
	// --no-renames keeps a rename honest as delete+add: the citation names the
	// old path, and "gone, re-cite against the new tree" is the true state of a
	// claim whose file was moved. Prefixes are forced so a host diff config
	// cannot bend the parse.
	raw, err := gitx.Repo{Dir: target}.Run("diff", "--no-renames", "--no-ext-diff",
		"--unified=0", "--src-prefix=a/", "--dst-prefix=b/", frozenRef, "HEAD")
	if err != nil {
		return nil, 0, err
	}
	files := parseUnified(raw)

	clean := 0
	var out []Drift
	for _, c := range cits {
		path, amb := ix.resolve(c.Path)
		if path == "" {
			// cite's own run reports resolution problems in detail; drift only
			// refuses to count an unresolvable citation as clean.
			msg := "no file at the frozen revision matches " + c.Path
			if len(amb) > 1 {
				msg = "ambiguous at the frozen revision: " + c.Path
			}
			out = append(out, Drift{Citation: c, Kind: "unresolved", Msg: msg})
			continue
		}
		fd := files[path]
		if fd != nil && fd.deleted {
			out = append(out, Drift{Citation: c, Kind: "gone",
				Msg: path + " is gone at the current head"})
			continue
		}
		edited, delta := false, 0
		var hs []hunk
		if fd != nil {
			hs = fd.hunks
		}
		for _, h := range hs {
			switch {
			case h.oldCount == 0:
				// Insertion after old line oldStart. Inside the cited range it
				// splices the passage the sentence quotes — re-read; entirely
				// above, it only shifts the numbers.
				if h.oldStart >= c.Start && h.oldStart < c.End {
					edited = true
				} else if h.oldStart < c.Start {
					delta += h.newCount
				}
			case h.oldStart <= c.End && h.oldStart+h.oldCount-1 >= c.Start:
				edited = true
			case h.oldStart+h.oldCount-1 < c.Start:
				delta += h.newCount - h.oldCount
			}
			if edited {
				break
			}
		}
		switch {
		case edited:
			out = append(out, Drift{Citation: c, Kind: "edited",
				Msg: fmt.Sprintf("%s:%s changed since the freeze", path, rangeStr(c.Start, c.End))})
		case delta != 0:
			out = append(out, Drift{Citation: c, Kind: "shifted",
				NewStart: c.Start + delta, NewEnd: c.End + delta,
				Msg: fmt.Sprintf("%s:%s is now %s (%+d)", path,
					rangeStr(c.Start, c.End), rangeStr(c.Start+delta, c.End+delta), delta)})
		default:
			clean++
		}
	}
	return out, clean, nil
}

func rangeStr(start, end int) string {
	if start == end {
		return strconv.Itoa(start)
	}
	return fmt.Sprintf("%d-%d", start, end)
}

var hunkRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// parseUnified reads a --unified=0 diff into one entry per file: the old-side
// hunks (drift), the new-side hunk starts, and the text each side carried
// (stale). An entry is created for EVERY file the diff names, including an
// added one and a mode-only change that carries no hunk at all — a file with no
// textual change is still a file this commit touched, and `check closure`'s
// FILE-level test is exactly that question. The first version keyed only on the
// OLD path and dropped `/dev/null` outright, so a commit that CREATED an
// artifact was invisible: its words never entered the "added" set (making them
// falsely retired) and closure reported the commit that added the document as
// not having touched it.
//
// The `--- ` and `+++ ` headers are read ONLY inside a file's header block,
// which begins at `diff --git` and ends at the first `+++` or `@@`. Outside it
// those prefixes belong to CONTENT: in a unified diff every deleted line starts
// with `-`, so a deleted line reading `-- a sql comment` is indistinguishable
// from a file header by prefix alone. Measured: one such line in a .sql target
// re-pointed the parser at a phantom path, every later hunk of that file was
// filed under it, and `wf check drift` called an invalidated citation clean.
// The milder shape is worse than silence — a real `edited` degrades to
// `shifted`, which tells the reviewer to re-aim the line number instead of
// re-reading the claim.
func parseUnified(raw string) map[string]*fileDiff {
	files := map[string]*fileDiff{}
	cur := ""     // the key this file's lines are filed under
	oldPath := "" // "" for an added file
	inHeader := false
	get := func() *fileDiff {
		if cur == "" {
			return nil
		}
		fd := files[cur]
		if fd == nil {
			fd = &fileDiff{}
			files[cur] = fd
		}
		return fd
	}
	for _, l := range strings.Split(raw, "\n") {
		switch {
		case strings.HasPrefix(l, "diff --git "):
			// The header pair is authoritative, but a mode-only change has no
			// header pair at all — only this line. Its paths are read here so
			// such a file still gets an entry, and are overwritten below when
			// the pair arrives.
			cur, oldPath, inHeader = "", "", true
			if a, b, ok := gitHeaderPaths(l); ok {
				cur, oldPath = a, a
				_ = b
				get()
			}
		case inHeader && strings.HasPrefix(l, "--- "):
			oldPath = diffPath(strings.TrimPrefix(l, "--- "), "a/")
			cur = oldPath
		case inHeader && strings.HasPrefix(l, "+++ "):
			np := diffPath(strings.TrimPrefix(l, "+++ "), "b/")
			if oldPath == "" {
				// An added file: keyed by its NEW path, because that is the only
				// name it has and the only one anything can cite.
				cur = np
			}
			if fd := get(); fd != nil {
				if np == "" {
					fd.deleted = true
					fd.newPath = ""
				} else {
					fd.newPath = np
				}
				fd.isNew = oldPath == ""
			}
			inHeader = false
		case strings.HasPrefix(l, "@@ "):
			inHeader = false
			m := hunkRe.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			fd := get()
			if fd == nil {
				continue
			}
			h := hunk{oldStart: mustAtoi(m[1]), oldCount: 1, newStart: mustAtoi(m[3]), newCount: 1}
			if m[2] != "" {
				h.oldCount = mustAtoi(m[2])
			}
			if m[4] != "" {
				h.newCount = mustAtoi(m[4])
			}
			fd.hunks = append(fd.hunks, h)
		case !inHeader && strings.HasPrefix(l, "-"):
			if fd := get(); fd != nil {
				fd.removed = append(fd.removed, l[1:])
			}
		case !inHeader && strings.HasPrefix(l, "+"):
			if fd := get(); fd != nil {
				fd.added = append(fd.added, l[1:])
			}
		}
	}
	return files
}

var gitHeaderRe = regexp.MustCompile(`^diff --git a/(.*) b/(.*)$`)

// gitHeaderPaths reads the `diff --git` line, which is the only header a
// mode-only change has. It is deliberately not trusted for anything else: a
// path containing " b/" makes this line ambiguous, which is why git also emits
// the unambiguous `---`/`+++` pair whenever there is content to describe.
func gitHeaderPaths(l string) (string, string, bool) {
	m := gitHeaderRe.FindStringSubmatch(l)
	if m == nil || strings.Contains(m[1], " b/") {
		return "", "", false
	}
	return unquotePath(m[1]), unquotePath(m[2]), true
}

// diffPath strips the diff prefix and returns "" for /dev/null.
func diffPath(p, prefix string) string {
	p = unquotePath(p)
	if p == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(p, prefix)
}

// unquotePath decodes the C-quoted form git writes for a path containing a
// space, a quote or a non-ASCII byte. Same lesson as `gitx.Dirty`, which uses
// -z for it: a quoted path matches nothing, so an artifact named in Cyrillic —
// legal as DATA, and the citation checker has a test for exactly that — was
// invisible to every check reading a diff.
func unquotePath(p string) string {
	if len(p) < 2 || p[0] != '"' {
		return p
	}
	if u, err := strconv.Unquote(p); err == nil {
		return u
	}
	return p
}

func mustAtoi(s string) int {
	n, _ := strconv.Atoi(s) // the regexp above admits digits only
	return n
}
