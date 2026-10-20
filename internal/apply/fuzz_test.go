package apply

import (
	"fmt"
	"strings"
	"testing"
)

func isMarker(line string) bool { return line == "<<<<" || line == "====" || line == ">>>>" }

func hasMarkerLine(s string) bool {
	for _, l := range strings.Split(s, "\n") {
		if isMarker(l) {
			return true
		}
	}
	return false
}

// A block written in the format comes back as the edit that wrote it, byte for
// byte in both halves — unless a half carries a bare marker line, which the
// format cannot mean and must refuse rather than mis-split (the P3d swallow).
func FuzzParseTextRoundTrip(f *testing.F) {
	f.Add("a.md", "", "old\n", "new", uint8(1))
	f.Add("docs/x.md", "why", "gone entirely", "", uint8(0))
	f.Add("b.md", "", "has\n====\ninside", "n", uint8(2))
	f.Fuzz(func(t *testing.T, file, note, old, nw string, count uint8) {
		// The header is split by strings.Fields, so a file name is one field:
		// no space of any kind, which includes \v and \f (the fuzzer's first
		// find was a file named by a vertical tab, not a parser defect).
		if fs := strings.Fields(file); len(fs) != 1 || fs[0] != file || strings.ContainsAny(file, "[]") || strings.Contains(file, "note=") || strings.Contains(file, "count=") {
			return
		}
		if strings.ContainsAny(note, "\n\r[]") {
			return
		}
		if strings.Contains(old, "\r") || strings.Contains(nw, "\r") {
			return
		}
		hdr := "=== " + file
		if count > 0 {
			hdr += fmt.Sprintf(" count=%d", count)
		}
		if note != "" {
			hdr += " note=" + note
		}
		src := hdr + "\n<<<<\n" + old + "\n====\n" + nw + "\n>>>>\n"
		edits, err := ParseText([]byte(src))
		if hasMarkerLine(old) || hasMarkerLine(nw) {
			if err == nil {
				t.Fatalf("a marker line inside a half must be refused: %q", src)
			}
			return
		}
		if err != nil {
			t.Fatalf("%v on %q", err, src)
		}
		if len(edits) != 1 {
			t.Fatalf("%d edits from one block: %q", len(edits), src)
		}
		e := edits[0]
		if e.File != file || e.Old != old || e.New != nw || e.Count != int(count) || e.Note != strings.TrimSpace(note) {
			t.Fatalf("round trip: %+v from %q", e, src)
		}
	})
}

// Any bytes at all: no panic, and every edit parsed names a file the text
// holds and halves whose lines the text holds.
func FuzzParseTextRaw(f *testing.F) {
	f.Add([]byte("=== a.md count=1 note=x\n<<<<\nold\n====\nnew\n>>>>\n"))
	f.Add([]byte("=== [a.md]\n"))
	f.Add([]byte("<<<<\n"))
	f.Fuzz(func(t *testing.T, b []byte) {
		edits, err := ParseText(b)
		if err != nil {
			return
		}
		text := string(b)
		for _, e := range edits {
			if !strings.Contains(text, e.File) || !strings.Contains(text, e.Old) || !strings.Contains(text, e.New) {
				t.Fatalf("edit %+v not in %q", e, text)
			}
			if hasMarkerLine(e.Old) || hasMarkerLine(e.New) {
				t.Fatalf("a half carrying a marker line parsed: %+v", e)
			}
		}
	})
}
