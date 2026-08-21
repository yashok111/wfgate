// Package check holds the deterministic checks a gate can run on its artifacts.
//
// The split of labour is the point. Of the blockers a real gate produces, the
// large majority are judgement about a specific codebase and no tool will find
// them. A measurable minority are mechanical — a citation pointing at the wrong
// line, a rule corrected in one artifact and left standing in another — and
// those cost review rounds, which are the expensive resource. These checks take
// only the mechanical share, and they must never be presented as coverage of
// the rest.
package check

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"git.sumka.site/yakov/wfgate/internal/gitx"
)

// citeRe matches `path.ext:LINE`, `path.ext:A-B`, and either form followed by
// `#anchor`.
//
// An extension is required, but NOT from a closed list. A whitelist of the
// extensions one project happens to use makes the check silently find nothing
// on a repository written in another language — the worst possible failure for
// a tool whose whole value is telling you what it did not verify. The extension
// must instead be short and contain at least one letter, which is what
// separates a real suffix from the last octet of `127.0.0.1:3000` and from a
// `3:4` in prose; both were the reason a whitelist looked necessary.
var citeRe = regexp.MustCompile(`([A-Za-z0-9_./\-]+\.([A-Za-z0-9]{1,8}))` +
	`:(\d+)(?:-(\d+))?(?:#([A-Za-z0-9_.\-]+))?`)

// hasLetter rejects an "extension" made only of digits, which is how a dotted
// numeric host or a version string reaches the pattern above.
func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// Citation is one `file:line` reference found in an artifact.
type Citation struct {
	Artifact  string // workspace-relative file the citation was written in
	AtLine    int    // line inside the artifact, so the operator can go fix it
	Raw       string
	Path      string
	Start     int
	End       int    // == Start for a single-line citation
	Anchor    string // optional `#symbol`: the token that must appear in range
	BadAnchor string // a trailing #token the anchor grammar did not accept
}

// Finding is a citation the check rejects.
type Finding struct {
	Citation
	Level string // "error" | "warn"
	Msg   string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s: %s  [%s]", f.Artifact, f.AtLine, f.Level, f.Msg, f.Raw)
}

// ScanCitations reads every file in files (workspace-relative) and extracts the
// citations it contains.
func ScanCitations(root string, files []string) ([]Citation, error) {
	var out []Citation
	for _, rel := range files {
		f, err := os.Open(filepath.Join(root, rel))
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for n := 1; sc.Scan(); n++ {
			line := sc.Text()
			for _, ix := range citeRe.FindAllStringSubmatchIndex(line, -1) {
				get := func(i int) string {
					if ix[2*i] < 0 {
						return ""
					}
					return line[ix[2*i]:ix[2*i+1]]
				}
				if !hasLetter(get(2)) {
					continue
				}
				start, _ := strconv.Atoi(get(3))
				end := start
				if get(4) != "" {
					end, _ = strconv.Atoi(get(4))
				}
				c := Citation{
					Artifact: rel, AtLine: n, Raw: get(0),
					Path: get(1), Start: start, End: end, Anchor: get(5),
				}
				// A `#` right after the match is an anchor the grammar did not
				// accept — Cyrillic, spaces. The artifacts are English-only by
				// rule, so this is a VIOLATION to report, never a citation to
				// silently count as unanchored: on the first live run 15 such
				// anchors read as "17 unverified" and hid that only two real
				// unanchored citations existed — and one of the invisible
				// anchors was factually wrong.
				if c.Anchor == "" {
					if e := ix[1]; e < len(line) && line[e] == '#' {
						tok := line[e+1:]
						if i := strings.IndexAny(tok, " \t"); i >= 0 {
							tok = tok[:i]
						}
						tok = strings.TrimRight(tok, "`).,;|\"'")
						c.BadAnchor = tok
						c.Raw += "#" + tok
					}
				}
				out = append(out, c)
			}
		}
		cerr := sc.Err()
		_ = f.Close() // read-only; the scan error above is the one that matters
		if cerr != nil {
			return nil, fmt.Errorf("%s: %w", rel, cerr)
		}
	}
	return out, nil
}

// Index is the target repository's tracked files, addressable by full path and
// by trailing path segment.
//
// Bare basenames are how citations are actually written — `bundle.go:340`, not
// `internal/bundle/bundle.go:340` — so resolution has to accept them. It also
// has to refuse them when they are ambiguous, because a repository with
// `repo.go` in five packages makes an unqualified citation unverifiable, and
// silently picking one would produce a green check over a wrong reference.
type Index struct {
	all    map[string]bool
	suffix map[string][]string
}

func NewIndex(target string) (*Index, error) {
	files, err := gitx.Repo{Dir: target}.Files()
	if err != nil {
		return nil, err
	}
	return indexFiles(files), nil
}

// NewIndexAt indexes the files tracked at ref rather than the working tree.
// Drift resolves citations against the revision they were frozen with, so a
// file the target has since deleted still resolves — and is then reported as
// gone, instead of silently failing to resolve at all.
func NewIndexAt(target, ref string) (*Index, error) {
	files, err := gitx.Repo{Dir: target}.FilesAt(ref)
	if err != nil {
		return nil, err
	}
	return indexFiles(files), nil
}

func indexFiles(files []string) *Index {
	ix := &Index{all: map[string]bool{}, suffix: map[string][]string{}}
	for _, f := range files {
		ix.all[f] = true
		parts := strings.Split(f, "/")
		for i := range parts {
			ix.suffix[strings.Join(parts[i:], "/")] = append(ix.suffix[strings.Join(parts[i:], "/")], f)
		}
	}
	return ix
}

func (ix *Index) resolve(p string) (string, []string) {
	if ix.all[p] {
		return p, nil
	}
	m := ix.suffix[p]
	if len(m) == 1 {
		return m[0], nil
	}
	return "", m
}

// VerifyCitations checks each citation against the target repository: the file
// resolves to exactly one tracked file, the range is well formed and inside the
// file, and an `#anchor`, when present, appears within the cited range.
//
// It cannot check that the cited lines say what the surrounding sentence claims.
// That is the reviewer's job, and `wf check cite --show` exists to make it cheap
// rather than to pretend it has been done.
func VerifyCitations(target string, ix *Index, cits []Citation) ([]Finding, error) {
	lineCache := map[string][]string{}
	var out []Finding
	add := func(c Citation, level, msg string) {
		out = append(out, Finding{Citation: c, Level: level, Msg: msg})
	}
	for _, c := range cits {
		if c.BadAnchor != "" {
			add(c, "error", fmt.Sprintf("anchor %q is not an ASCII token; the artifacts are English-only — anchor with a token from the cited line, or cite the range without one", c.BadAnchor))
		}
		path, amb := ix.resolve(c.Path)
		if path == "" {
			if len(amb) > 1 {
				sort.Strings(amb)
				show := amb
				if len(show) > 4 {
					show = append(append([]string{}, show[:4]...), fmt.Sprintf("… +%d", len(amb)-4))
				}
				add(c, "error", "ambiguous path, matches "+strings.Join(show, ", "))
			} else {
				add(c, "error", "no tracked file matches "+c.Path)
			}
			continue
		}
		if c.End < c.Start {
			add(c, "error", fmt.Sprintf("inverted range %d-%d", c.Start, c.End))
			continue
		}
		lines, ok := lineCache[path]
		if !ok {
			b, err := os.ReadFile(filepath.Join(target, path))
			if err != nil {
				add(c, "error", fmt.Sprintf("read %s: %v", path, err))
				continue
			}
			lines = strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
			lineCache[path] = lines
		}
		if c.Start < 1 || c.End > len(lines) {
			add(c, "error", fmt.Sprintf("%s has %d lines, citation covers %d-%d", path, len(lines), c.Start, c.End))
			continue
		}
		if c.Anchor != "" && !strings.Contains(strings.Join(lines[c.Start-1:c.End], "\n"), c.Anchor) {
			at := nearest(lines, c.Anchor, c.Start)
			msg := fmt.Sprintf("anchor %q not in %s:%d-%d", c.Anchor, path, c.Start, c.End)
			if at > 0 {
				msg += fmt.Sprintf("; nearest occurrence is line %d", at)
			}
			add(c, "error", msg)
		}
	}
	return out, nil
}

// nearest returns the line number of the occurrence of tok closest to want, or
// 0. An off-by-a-few citation is the common case, and naming the correct line
// turns the finding into a patch instead of a search.
func nearest(lines []string, tok string, want int) int {
	best, bestD := 0, 1<<30
	for i, l := range lines {
		if !strings.Contains(l, tok) {
			continue
		}
		d := i + 1 - want
		if d < 0 {
			d = -d
		}
		if d < bestD {
			best, bestD = i+1, d
		}
	}
	return best
}

// Show renders the cited lines so a reviewer can compare them with the claim.
func Show(target string, ix *Index, c Citation) string {
	path, _ := ix.resolve(c.Path)
	if path == "" {
		return "  <unresolved>"
	}
	b, err := os.ReadFile(filepath.Join(target, path))
	if err != nil {
		return "  <unreadable>"
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	var sb strings.Builder
	for n := c.Start; n <= c.End && n <= len(lines); n++ {
		if n < 1 {
			continue
		}
		fmt.Fprintf(&sb, "  %s:%d| %s\n", path, n, lines[n-1])
	}
	return sb.String()
}
