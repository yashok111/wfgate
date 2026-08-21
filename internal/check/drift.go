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
	oldStart, oldCount, newCount int
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
	hunks, deleted := parseUnified(raw)

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
		if deleted[path] {
			out = append(out, Drift{Citation: c, Kind: "gone",
				Msg: path + " is gone at the current head"})
			continue
		}
		edited, delta := false, 0
		for _, h := range hunks[path] {
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

var hunkRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+\d+(?:,(\d+))? @@`)

// parseUnified reads a --unified=0 diff into old-side hunks per old path, plus
// the set of deleted files (new side /dev/null).
func parseUnified(raw string) (map[string][]hunk, map[string]bool) {
	hunks := map[string][]hunk{}
	deleted := map[string]bool{}
	cur := ""
	for _, l := range strings.Split(raw, "\n") {
		switch {
		case strings.HasPrefix(l, "--- "):
			cur = strings.TrimPrefix(l, "--- ")
			if cur == "/dev/null" {
				cur = "" // an added file has no old side to drift
			} else {
				cur = strings.TrimPrefix(cur, "a/")
			}
		case strings.HasPrefix(l, "+++ "):
			if cur != "" && strings.TrimPrefix(l, "+++ ") == "/dev/null" {
				deleted[cur] = true
			}
		case strings.HasPrefix(l, "@@ "):
			if cur == "" {
				continue
			}
			m := hunkRe.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			h := hunk{oldStart: mustAtoi(m[1]), oldCount: 1, newCount: 1}
			if m[2] != "" {
				h.oldCount = mustAtoi(m[2])
			}
			if m[3] != "" {
				h.newCount = mustAtoi(m[3])
			}
			hunks[cur] = append(hunks[cur], h)
		}
	}
	return hunks, deleted
}

func mustAtoi(s string) int {
	n, _ := strconv.Atoi(s) // the regexp above admits digits only
	return n
}
