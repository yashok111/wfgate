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
	"unicode/utf8"

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
// The PATH class accepts non-ASCII letters, the anchor class does not, and the
// asymmetry is the point. An anchor is a token an English-only artifact quotes
// out of the target, so a non-ASCII one is a policy violation this check
// reports. A path is DATA about the repository under review, which may be
// written in any language: an ASCII-only path class made `src/файл.go:1` match
// nothing at all, so the citation was neither verified nor counted — invisible
// rather than unverified, which is the one outcome this check exists to
// prevent.
var citeRe = regexp.MustCompile(`([\p{L}\p{N}_./\-]+\.([\p{L}\p{N}]{1,8}))` +
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
	Anchor    string // optional `#symbol`: the identifier, or a part of one, that must stand in range (AnchorIn)
	BadAnchor string // a trailing #token the anchor grammar did not accept
	BadRange  string // a range written with a dash and no end line, as found
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
				// The anchor class takes `.` and `-`, so a citation ending a
				// sentence carried its full stop into the anchor and failed on
				// a line holding the identifier.
				if a := strings.TrimRight(c.Anchor, ".-"); a != c.Anchor {
					c.Raw = strings.TrimSuffix(c.Raw, c.Anchor[len(a):])
					c.Anchor = a
				}
				// A dash right after the line or range is a range never
				// finished: `store.go:3-#Save` matched as line 3 with no anchor
				// and was reported as "citation without #anchor" when the
				// author had written one, and `3-5-#c` the same way. It is the
				// RANGE that is broken.
				if e := ix[1]; get(5) == "" && e < len(line) && line[e] == '-' {
					tok := line[e:]
					if i := strings.IndexAny(tok, " \t"); i >= 0 {
						tok = tok[:i]
					}
					tok = strings.TrimRight(tok, "`).,;|\"'")
					c.BadRange = c.Raw + tok
					c.Raw += tok
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
// file, and an `#anchor`, when present, stands within the cited range as an
// identifier or a part of one (AnchorIn).
//
// It cannot check that the cited lines say what the surrounding sentence claims.
// That is the reviewer's job, and `wf check cite --show` exists to make it cheap
// rather than to pretend it has been done.
func VerifyCitations(target string, ix *Index, cits []Citation) ([]Finding, error) {
	return VerifyCitationsWith(WorktreeReader(target), ix, cits)
}

// WorktreeReader reads a target path from the working tree.
func WorktreeReader(target string) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) { return os.ReadFile(filepath.Join(target, path)) }
}

// RevReader reads a target path AT a revision, so a citation can be checked
// against the bytes a freeze published rather than against a tree the run under
// review has since moved.
func RevReader(target, ref string) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) { return gitx.Repo{Dir: target}.ShowFile(ref, path) }
}

// VerifyCitationsWith is VerifyCitations over an arbitrary source of the
// target's bytes. ONE verifier: the rules about ranges, anchors and nearest
// occurrences are the same question whichever revision is being asked about,
// and a second copy of them would be this project's most-repeated defect.
func VerifyCitationsWith(read func(string) ([]byte, error), ix *Index, cits []Citation) ([]Finding, error) {
	lineCache := map[string][]string{}
	var out []Finding
	add := func(c Citation, level, msg string) {
		out = append(out, Finding{Citation: c, Level: level, Msg: msg})
	}
	for _, c := range cits {
		if c.BadAnchor != "" {
			add(c, "error", fmt.Sprintf("anchor %q is not an ASCII token; the artifacts are English-only — anchor with a token from the cited line, or cite the range without one", c.BadAnchor))
		}
		if c.BadRange != "" {
			add(c, "error", fmt.Sprintf("malformed range %q — a dash with no end line; write `%s:%d-N` with the last line, or drop the dash", c.BadRange, c.Path, c.Start))
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
			b, err := read(path)
			if err != nil {
				add(c, "error", fmt.Sprintf("read %s: %v", path, err))
				continue
			}
			// A zero-byte file has NO lines. strings.Split("", "\n") returns one
			// empty element, so `empty.go:1` passed the range check and -show
			// printed a blank line as its evidence.
			if len(b) == 0 {
				lines = nil
			} else {
				lines = strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
			}
			lineCache[path] = lines
		}
		if c.Start < 1 || c.End > len(lines) {
			add(c, "error", fmt.Sprintf("%s has %d lines, citation covers %d-%d", path, len(lines), c.Start, c.End))
			continue
		}
		if text := strings.Join(lines[c.Start-1:c.End], "\n"); c.Anchor != "" && !AnchorIn(text, c.Anchor) {
			at := nearest(lines, c.Anchor, c.Start)
			msg := fmt.Sprintf("anchor %q not in %s:%d-%d", c.Anchor, path, c.Start, c.End)
			// Present only INSIDE a longer word: say which, because "not in"
			// over a line that visibly holds the letters reads as a bug in the
			// check, and the word is the patch when the claim cites it.
			if w := enclosing(text, c.Anchor); w != "" {
				msg = fmt.Sprintf("anchor %q stands only inside `%s` in %s:%d-%d — neither a whole identifier nor a part after `_`, a camelCase hump, an acronym or a digit (and a part that begins an identifier and stops at a hump names a different one); anchor with `%s` if that is what the claim cites", c.Anchor, w, path, c.Start, c.End, w)
			}
			if at > 0 {
				msg += fmt.Sprintf("; nearest occurrence is line %d", at)
			}
			add(c, "error", msg)
		}
	}
	return out, nil
}

// extlessRe matches a citation-shaped reference whose path carries NO
// extension: the class citeRe deliberately requires an extension to exclude
// (`Makefile:90`). The two are disjoint by construction — this token class has
// no dot.
var extlessRe = regexp.MustCompile(`\b([A-Za-z][A-Za-z0-9_/\-]*):(\d+)\b`)

// ExtensionlessRefs finds citation-like references to extensionless TRACKED
// files. They are not citations — the extension rule is what separates a path
// from prose ratios and dotted hosts — so they are absent from every count the
// check prints, which is the failure this scan makes visible: unverified
// without being reported as unverified. Only a token that resolves in the
// target's index is reported; "see 3:4" stays prose.
func ExtensionlessRefs(root string, files []string, ix *Index) ([]Citation, error) {
	var out []Citation
	for _, rel := range files {
		f, err := os.Open(filepath.Join(root, rel))
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for n := 1; sc.Scan(); n++ {
			for _, m := range extlessRe.FindAllStringSubmatch(sc.Text(), -1) {
				if path, _ := ix.resolve(m[1]); path == "" {
					continue
				}
				line, _ := strconv.Atoi(m[2])
				out = append(out, Citation{Artifact: rel, AtLine: n, Raw: m[0], Path: m[1], Start: line, End: line})
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

// AnchorIn reports whether tok occurs in text as a whole identifier or as one
// or more of its PARTS — split at `_`, at a camelCase hump, at an acronym's
// end (a plural `s` after one included: `IDs`, `URLs`) and between letters and
// digits — except a part that BEGINS an identifier and stops at a camelCase
// hump or an acronym's end: `#Update` on `UpdateAll` names a different symbol.
//
// A plain substring match passed `#e` on `return nil` and `#Update` on
// `current.UpdatedAt` (the audit wave's adversarial vector). A
// whole-identifier match was measured first and refused: over every unpruned
// frozen round on disk (235 rounds, 1397 anchors distinct per target revision
// that the substring match passed) it failed 25, 22 of them correct citations
// of a part (`#RETENTION` on `MOCKER_CHECKPOINT_RETENTION`). The part rule
// alone failed 2, both word fragments, and passed one citation the gate had
// itself filed as wrong — `#useGetOperation` on a line holding only
// `useGetOperationOverride` — which is what the prefix exception refuses, with
// three loose anchors beside it (`#fence` on `fenceTx`) [M-48]. Suffixes stay
// parts: every camelCase suffix in that corpus was deliberate.
//
// The neighbours are read as RUNES: a byte test took a UTF-8 continuation byte
// for a boundary, so `#na` passed inside "naïve". A residue the rule accepts by
// definition: a one-letter anchor split off by a digit (`#e` in `1e9`).
func AnchorIn(text, tok string) bool {
	if tok == "" {
		return false
	}
	for i := 0; i <= len(text)-len(tok); {
		j := strings.Index(text[i:], tok)
		if j < 0 {
			return false
		}
		s := i + j
		if partAt(text, tok, s) {
			return true
		}
		i = s + 1
	}
	return false
}

// partAt reports whether the occurrence of tok at byte s of text is a whole
// identifier or a part of one, by AnchorIn's rule. tok is ASCII by the anchor
// grammar; its neighbours in text may be anything.
func partAt(text, tok string, s int) bool {
	e := s + len(tok)
	p, _ := utf8.DecodeLastRuneInString(text[:s])
	n, nsz := utf8.DecodeRuneInString(text[e:])
	nn, _ := utf8.DecodeRuneInString(text[e+nsz:])
	a, l := rune(tok[0]), rune(tok[len(tok)-1])
	word := func(r rune) bool { return r != utf8.RuneError && (unicode.IsLetter(r) || unicode.IsDigit(r)) }
	up, lo, dig := unicode.IsUpper, unicode.IsLower, unicode.IsDigit
	// An acronym's plural: `IDs`, `URLs` — the `s` closes the acronym.
	pluralAfter := func(last, next, after rune) bool { return up(last) && next == 's' && (!word(after) || up(after)) }

	wholeStart := !word(p) || !word(a)
	// `Ds` in `IDs` is the acronym's last letter and its plural, not a part.
	pluralTail := len(tok) == 2 && tok[1] == 's' && (!word(n) || up(n))
	start := wholeStart ||
		(up(a) && (lo(p) || dig(p))) || // fooBar: Bar
		(up(a) && up(p) && len(tok) > 1 && lo(rune(tok[1])) && !pluralTail) || // HTTPServer: Server, but not IDs: Ds
		dig(a) != dig(p) // p1b: 1b
	if !start {
		return false
	}
	if !word(n) || !word(l) {
		return true
	}
	camelEnd := (up(n) && (lo(l) || dig(l))) || // fooBar: foo
		(up(l) && up(n) && word(nn) && lo(nn)) || // HTTPServer: HTTP
		pluralAfter(l, n, nn) // userIDs: ID
	if camelEnd {
		// A part that begins an identifier and stops at a hump is a prefix
		// of a longer name, and in this corpus that longer name was a
		// different symbol.
		return !wholeStart
	}
	return dig(l) != dig(n) // answers409: answers
}

// enclosing returns the word around the first occurrence of tok in text, when
// that word is longer than tok — what a fragment anchor actually landed in.
func enclosing(text, tok string) string {
	i := strings.Index(text, tok)
	if i < 0 {
		return ""
	}
	in := func(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }
	s, e := i, i+len(tok)
	for s > 0 {
		r, sz := utf8.DecodeLastRuneInString(text[:s])
		if !in(r) {
			break
		}
		s -= sz
	}
	for e < len(text) {
		r, sz := utf8.DecodeRuneInString(text[e:])
		if !in(r) {
			break
		}
		e += sz
	}
	if w := text[s:e]; w != tok {
		return w
	}
	return ""
}

// nearest returns the line number of the occurrence of tok closest to want, or
// 0. An off-by-a-few citation is the common case, and naming the correct line
// turns the finding into a patch instead of a search.
func nearest(lines []string, tok string, want int) int {
	best, bestD := 0, 1<<30
	for i, l := range lines {
		if !AnchorIn(l, tok) {
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
	return ShowWith(WorktreeReader(target), ix, c)
}

// ShowWith prints the cited lines from wherever the caller reads the target.
func ShowWith(read func(string) ([]byte, error), ix *Index, c Citation) string {
	path, _ := ix.resolve(c.Path)
	if path == "" {
		return "  <unresolved>"
	}
	b, err := read(path)
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
