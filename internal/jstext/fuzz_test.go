package jstext

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The seeds are the five emitted `.mjs` templates: every shape the scanner was built
// for — nested template holes, a regex matching a quote, comment blocks — is in
// them, and a fuzzer that starts from them mutates real scripts.
func seedTemplates(f *testing.F) {
	f.Helper()
	paths, _ := filepath.Glob("../../templates/*.mjs")
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(string(b))
	}
	f.Add("const s = 'a\\'b' + `x${ `y${1}` }z` // c\n/* d */ /['\"]/.test(s)")
	f.Add("a = b / c / d; r = /x/g; `${`")
	f.Add("'unterminated\n\"also\n`and\n/* never closed")
}

// StripNonCode blanks IN PLACE: the length is the length, every newline stays
// where it was, and a byte is either itself or a space. An offset into the
// blanked copy must still name the line it named in the source — that is the
// whole reason the copy is the same shape.
func FuzzStripNonCodeKeepsShape(f *testing.F) {
	seedTemplates(f)
	f.Fuzz(func(t *testing.T, src string) {
		out := StripNonCode(src)
		if len(out) != len(src) {
			t.Fatalf("length %d -> %d", len(src), len(out))
		}
		for i := 0; i < len(src); i++ {
			if out[i] == src[i] {
				continue
			}
			if out[i] != ' ' || src[i] == '\n' {
				t.Fatalf("byte %d: %q -> %q", i, src[i], out[i])
			}
		}
	})
}

// The two polarities read ONE scanner, so a prose span must lie inside what the
// code reader blanked: a byte that is prose to one and code to the other is a
// call counted in a string or a rule read out of code. Spans are in order and
// never overlap, and a Join never opens the list.
func FuzzProseSpansInsideBlanked(f *testing.F) {
	seedTemplates(f)
	f.Fuzz(func(t *testing.T, src string) {
		out := StripNonCode(src)
		prev := 0
		for k, sp := range ProseSpans(src) {
			if sp.From < prev || sp.To < sp.From || sp.To > len(src) {
				t.Fatalf("span %d [%d,%d) after %d of %d", k, sp.From, sp.To, prev, len(src))
			}
			if k == 0 && sp.Join {
				t.Fatalf("the first span joins nothing")
			}
			for i := sp.From; i < sp.To; i++ {
				if out[i] != ' ' && src[i] != '\n' {
					t.Fatalf("span %d byte %d %q is prose and code", k, i, src[i])
				}
			}
			prev = sp.To
		}
	})
}

// Unescape only ever shortens: every escape spells a rune in more bytes than
// the rune takes, a lone trailing backslash is dropped, and text with no
// backslash is returned as it came.
func FuzzUnescape(f *testing.F) {
	f.Add(`a\'b\né\u{1F600}\x41\`)
	f.Add(`\u{`)
	f.Add(`\u{}\x`)
	f.Add("plain")
	f.Fuzz(func(t *testing.T, s string) {
		out := Unescape(s)
		if !strings.ContainsRune(s, '\\') && out != s {
			t.Fatalf("no escape, changed: %q -> %q", s, out)
		}
		if len(out) > len(s) {
			t.Fatalf("grew: %q -> %q", s, out)
		}
		// A decoder that stopped decoding would pass the two above; a complete
		// escape in front decodes to its rune and leaves the rest to the same
		// decoder.
		if got := Unescape(`\n` + s); got != "\n"+out {
			t.Fatalf("Unescape(\\n+%q) = %q, want %q", s, got, "\n"+out)
		}
		if got := Unescape(`\u0041` + s); got != "A"+out {
			t.Fatalf("Unescape(\\u0041+%q) = %q, want %q", s, got, "A"+out)
		}
	})
}

// LineAt is one plus the newlines before the offset, clamped to the text.
func FuzzLineAt(f *testing.F) {
	f.Add("a\nb\nc", 3)
	f.Add("", 5)
	f.Add("x", -1)
	f.Fuzz(func(t *testing.T, s string, off int) {
		got := LineAt(s, off)
		want := 1
		if off > 0 {
			want += strings.Count(s[:min(off, len(s))], "\n")
		}
		if got != want {
			t.Fatalf("LineAt(%q, %d) = %d, want %d", s, off, got, want)
		}
	})
}
