// Package apply performs an all-or-nothing batch of textual edits.
//
// It exists because of one measured failure: a batch of three replacements
// where the second did not match, the helper aborted on it, and the two that
// HAD matched were never written. The operator saw one error, fixed that one
// edit, and believed the batch had landed. It had not, and a later review round
// re-filed a finding that was supposed to be closed.
//
// The rule this package enforces is therefore: every edit in a batch is
// verified against the file contents BEFORE any byte is written, and if a
// single one fails to match its expected occurrence count, nothing at all is
// written. A partially applied batch is worse than a rejected one, because a
// rejected batch announces itself.
package apply

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Edit is one replacement. Count is the number of occurrences the caller
// expects; 0 means exactly one.
//
// Count is not a convenience. A rule that lives in two artifacts and is
// corrected in one of them is this project's most frequent defect, and an edit
// that declares "I expect this string twice" turns that defect into a hard
// error at apply time instead of a finding three review rounds later.
type Edit struct {
	File  string `json:"file"`  // path relative to the workspace root
	Old   string `json:"old"`   // literal, not a pattern
	New   string `json:"new"`   // literal
	Count int    `json:"count"` // expected occurrences of Old; 0 => 1
	Note  string `json:"note"`  // free text, echoed into the commit message
}

func (e Edit) want() int {
	if e.Count == 0 {
		return 1
	}
	return e.Count
}

// Result is one edit's verdict. Reason is empty when OK.
type Result struct {
	Index  int
	Edit   Edit
	Got    int
	OK     bool
	Reason string

	// What the edit was measured against, so the caller can say how much of
	// the file ONE replacement covers. Both guarantees this command exists for
	// — all-or-nothing and the declared `count` — are per REPLACEMENT, so a
	// replacement carrying the whole artifact makes both of them vacuous: its
	// count is 1 and always matches, and the many changes inside it are
	// verified by nothing. Measured on a gate that rewrote its artifact this
	// way round after round: one lens found 15 of a round's 30 recorded
	// closures byte-identical to their pre-fix text, a second confirmed 11, and
	// two of them were the FOURTH claimed closure on one unedited line.
	// Span is what this edit replaces IN TOTAL — one occurrence times the
	// occurrences it matched, because ReplaceAll changes every one of them and
	// three edits of a fifth of a file each are three fifths of it.
	Span int
	// FileBytes is the file as it was READ, never the evolving buffer. Priced
	// against the buffer, a five-byte edit following a whole-file restructure in
	// the same batch reported as half the artifact — a false positive of the
	// note, manufactured by any batch that restructures a document and then
	// touches it again.
	FileBytes int
	// How many lines this replacement changes, across every occurrence it
	// matched. NOT a hunk count and it does not claim to be one: it is the
	// number of separate fixes one replacement can be hiding.
	Rewritten int
}

var ErrRejected = errors.New("batch rejected: no file was written")

// Plan reads every file the batch touches, applies the edits in order against
// the in-memory buffer, and returns per-edit results plus the resulting
// contents. Nothing is written.
//
// Edits are applied against the EVOLVING buffer, not against the file as it was
// read: two edits to the same file where the second's Old overlaps the first's
// New must see the first's effect, otherwise the count a caller declares is a
// count of a state that will never exist on disk.
func Plan(root string, edits []Edit) ([]Result, map[string][]byte, error) {
	bufs := map[string][]byte{}
	// The size of each file as it was READ. Every edit after the first is
	// applied against a buffer earlier edits have already changed, and pricing a
	// replacement against that reports a later small edit as a large share of a
	// file that no longer exists.
	pristine := map[string]int{}
	results := make([]Result, 0, len(edits))
	ok := true

	for i, e := range edits {
		r := Result{Index: i, Edit: e}
		if e.Old == "" {
			r.Reason = "empty old"
			results, ok = append(results, r), false
			continue
		}
		if e.Old == e.New {
			r.Reason = "old == new (no-op)"
			results, ok = append(results, r), false
			continue
		}
		abs, key, err := SafeJoin(root, e.File)
		if err != nil {
			r.Reason = err.Error()
			results, ok = append(results, r), false
			continue
		}
		if _, seen := bufs[key]; !seen {
			b, err := os.ReadFile(abs)
			if err != nil {
				r.Reason = fmt.Sprintf("read: %v", err)
				results, ok = append(results, r), false
				continue
			}
			bufs[key] = b
			pristine[key] = len(b)
		}
		body := string(bufs[key])
		got := strings.Count(body, e.Old)
		r.Got = got
		r.Span, r.FileBytes = len(e.Old)*got, pristine[key]
		r.Rewritten = rewrittenLines(e.Old, e.New) * got
		switch {
		case got == 0:
			r.Reason = "no match"
		case got != e.want():
			r.Reason = fmt.Sprintf("expected %d occurrence(s), found %d", e.want(), got)
		default:
			r.OK = true
			bufs[key] = []byte(strings.ReplaceAll(body, e.Old, e.New))
		}
		if !r.OK {
			ok = false
		}
		results = append(results, r)
	}

	if !ok {
		return results, nil, ErrRejected
	}
	return results, bufs, nil
}

// Write commits the planned buffers to disk atomically per file: a temp file in
// the same directory, fsync, rename. A crash mid-batch can therefore leave some
// files old and some new, but never a file half-written — and the caller's next
// `git status` shows exactly which ones landed, which is the property the whole
// tool is for.
func Write(root string, bufs map[string][]byte) ([]string, error) {
	written := make([]string, 0, len(bufs))
	for rel, b := range bufs {
		abs, _, err := SafeJoin(root, rel)
		if err != nil {
			return written, err
		}
		if err := atomicWrite(abs, b); err != nil {
			return written, fmt.Errorf("%s: %w", rel, err)
		}
		written = append(written, rel)
	}
	return written, nil
}

// FileVerdict is one file's read-back result.
type FileVerdict struct {
	File   string
	OK     bool
	Reason string
}

// Verify re-reads every file the batch wrote and compares it BYTE FOR BYTE with
// the buffer Plan produced. This is the second half of the lesson: an apply is
// not verified by the absence of an error, it is verified by reading back what
// landed.
//
// It compares buffers rather than searching for each edit's New, because
// searching was wrong in both directions. A deletion (`new` empty) was verified
// by strings.Count(body, ""), which is len(body)+1 and therefore never zero: the
// whole class of pure-deletion edits was checked vacuously. And a CHAINED batch
// — `alpha`→`beta` then `beta`→`gamma` in one file — landed perfectly and then
// failed verification, because the intermediate text the search looked for no
// longer exists on disk; the operator was left with an uncommitted artifact and
// a batch that could not be re-run, which pushes them to the hand edit this tool
// forbids. The planned buffer is what the batch MEANT to produce, so comparing
// against it is both stricter and free of either error.
func Verify(root string, bufs map[string][]byte) []FileVerdict {
	out := make([]FileVerdict, 0, len(bufs))
	for rel, want := range bufs {
		v := FileVerdict{File: rel}
		abs, _, err := SafeJoin(root, rel)
		if err != nil {
			v.Reason = err.Error()
			out = append(out, v)
			continue
		}
		got, err := os.ReadFile(abs)
		if err != nil {
			v.Reason = fmt.Sprintf("read back: %v", err)
			out = append(out, v)
			continue
		}
		if !bytes.Equal(got, want) {
			v.Reason = fmt.Sprintf("on disk it is %d byte(s), the planned batch is %d", len(got), len(want))
			out = append(out, v)
			continue
		}
		v.OK = true
		out = append(out, v)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].File < out[b].File })
	return out
}

func atomicWrite(path string, b []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".wf-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	// no-op once the rename succeeded; a failure to remove a temp file must not
	// mask the write error the caller is about to see.
	defer func() { _ = os.Remove(tmp) }()
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SafeJoin refuses a path that escapes the workspace and returns both the
// absolute path and the workspace-relative KEY that names it. The edit list is
// usually machine-written by an agent, and an agent that has confused the
// workspace with the repository under review must be stopped here rather than
// discovered by a stray commit in the wrong tree. It is exported because `wf
// add -as` takes a path from the operator too, and joined it raw: `-as
// ../../target/x.md` wrote into the repository under review, which no command
// may touch.
//
// The key matters as much as the refusal. Buffers used to be keyed by the path
// string as the author WROTE it, so `artifacts/a.md` and `./artifacts/a.md` in
// one batch were two files: both planned against the pristine bytes, both
// written, and whichever landed second silently discarded the other's edits — a
// lost edit reported as committed, the exact failure this package exists to
// prevent.
func SafeJoin(root, rel string) (abs, key string, err error) {
	if filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("%s: absolute paths are not allowed", rel)
	}
	abs = filepath.Join(root, rel)
	key, err = filepath.Rel(root, abs)
	if err != nil || key == ".." || strings.HasPrefix(key, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("%s: escapes the workspace", rel)
	}
	return abs, key, nil
}

// rewrittenLines counts how many lines a replacement changes, comparing old and
// new as MULTISETS rather than by position: a replacement that moves a paragraph
// and rewrites two sentences inside it changes two lines, not the whole
// paragraph. It is deliberately not a diff — the number says how many separate
// changes ONE replacement can be carrying, and for that a count of lines that
// stopped existing or started existing is cheap and close enough.
//
// BOTH directions, and a floor of one. Counting only the lines that stopped
// existing returned ZERO for a replacement that only INSERTS, so the note beside
// it read "rewriting 0 line(s)" over a real change — a number in the register of
// a measurement, saying the opposite of the truth, inside the note written
// against exactly that. The floor is safe because Plan rejects `old == new`
// before this is reached, so a replacement always changes something.
func rewrittenLines(old, new string) int {
	count := func(a, b string) int {
		have := map[string]int{}
		for _, l := range strings.Split(b, "\n") {
			have[l]++
		}
		n := 0
		for _, l := range strings.Split(a, "\n") {
			if have[l] > 0 {
				have[l]--
				continue
			}
			n++
		}
		return n
	}
	removed, added := count(old, new), count(new, old)
	if removed < added {
		removed = added
	}
	if removed < 1 {
		return 1
	}
	return removed
}
