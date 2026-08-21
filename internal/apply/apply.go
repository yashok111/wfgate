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
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
		abs, err := safeJoin(root, e.File)
		if err != nil {
			r.Reason = err.Error()
			results, ok = append(results, r), false
			continue
		}
		if _, seen := bufs[e.File]; !seen {
			b, err := os.ReadFile(abs)
			if err != nil {
				r.Reason = fmt.Sprintf("read: %v", err)
				results, ok = append(results, r), false
				continue
			}
			bufs[e.File] = b
		}
		body := string(bufs[e.File])
		got := strings.Count(body, e.Old)
		r.Got = got
		switch {
		case got == 0:
			r.Reason = "no match"
		case got != e.want():
			r.Reason = fmt.Sprintf("expected %d occurrence(s), found %d", e.want(), got)
		default:
			r.OK = true
			bufs[e.File] = []byte(strings.ReplaceAll(body, e.Old, e.New))
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
		abs, err := safeJoin(root, rel)
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

// Verify re-reads the files from disk and confirms each edit's New is present.
// This is the second half of the lesson: an apply is not verified by the
// absence of an error, it is verified by searching for each change.
func Verify(root string, edits []Edit) []Result {
	cache := map[string]string{}
	out := make([]Result, 0, len(edits))
	for i, e := range edits {
		r := Result{Index: i, Edit: e}
		body, seen := cache[e.File]
		if !seen {
			abs, err := safeJoin(root, e.File)
			if err != nil {
				r.Reason = err.Error()
				out = append(out, r)
				continue
			}
			b, err := os.ReadFile(abs)
			if err != nil {
				r.Reason = fmt.Sprintf("read back: %v", err)
				out = append(out, r)
				continue
			}
			body = string(b)
			cache[e.File] = body
		}
		r.Got = strings.Count(body, e.New)
		if r.Got == 0 {
			r.Reason = "new text absent after write"
		} else {
			r.OK = true
		}
		out = append(out, r)
	}
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

// safeJoin refuses a path that escapes the workspace. The edit list is usually
// machine-written by an agent, and an agent that has confused the workspace with
// the repository under review must be stopped here rather than discovered by a
// stray commit in the wrong tree.
func safeJoin(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("%s: absolute paths are not allowed", rel)
	}
	abs := filepath.Join(root, rel)
	r, err := filepath.Rel(root, abs)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s: escapes the workspace", rel)
	}
	return abs, nil
}
