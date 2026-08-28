package apply

import (
	"strings"
	"testing"
)

// The whole point of the format: quoting-hostile content passes through with
// zero escaping — backticks, ${}, quotes, JSON — the classes that crashed the
// heredoc assembly of the first live gate's 35-edit batch.
func TestParseText(t *testing.T) {
	src := "=== a.md  count=2  note=closes B7, keeps `${x}` intact\n" +
		"<<<<\n" +
		"line with `backticks` and ${dollar} and \"quotes\"\n" +
		"second line\n" +
		"====\n" +
		"new ${text}\n" +
		">>>>\n" +
		"\n" +
		"=== b.md\n" +
		"<<<<\n" +
		"gone entirely\n" +
		"====\n" +
		"\n" +
		">>>>\n"
	edits, err := ParseText([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 2 {
		t.Fatalf("want 2 edits, got %d: %+v", len(edits), edits)
	}
	e := edits[0]
	if e.File != "a.md" || e.Count != 2 || e.Note != "closes B7, keeps `${x}` intact" {
		t.Fatalf("header parsed wrong: %+v", e)
	}
	if e.Old != "line with `backticks` and ${dollar} and \"quotes\"\nsecond line" || e.New != "new ${text}" {
		t.Fatalf("blocks parsed wrong: old=%q new=%q", e.Old, e.New)
	}
	if edits[1].Count != 0 || edits[1].Old != "gone entirely" || edits[1].New != "" {
		t.Fatalf("second edit parsed wrong: %+v", edits[1])
	}
}

func TestParseText_errors(t *testing.T) {
	cases := []struct{ src, want string }{
		{"junk\n", "expected `=== <file>` header"},
		{"=== a.md\n<<<<\nold\n", "unterminated block"},
		{"=== a.md flag=1\n<<<<\no\n====\nn\n>>>>\n", "unknown header field"},
		{"=== a.md count=zero\n<<<<\no\n====\nn\n>>>>\n", "not a positive number"},
		{"===\n<<<<\no\n====\nn\n>>>>\n", "header"},
		{"=== a.md count=2\n<<<<\no\n====\nn\n>>>>\n<<<<\no2\n====\nn2\n>>>>\n", "its own `=== <file>` header"},
		{"\n\n", "no edit blocks"},
	}
	for _, c := range cases {
		if _, err := ParseText([]byte(c.src)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("ParseText(%q): want error containing %q, got %v", c.src, c.want, err)
		}
	}
}

// A mis-split is refused, and the two shapes are the two forgotten
// terminators. Measured on the P3d gate: the NEW-half case was accepted,
// planned, reported `ok (1 match)` for every block and then verified
// byte-for-byte — the verification is against the batch AS PARSED — so the
// literal marker lines landed in the artifact and the recovery was two
// `git revert`s.
func TestParseText_strayMarkerInBlock(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{
			// A forgotten `>>>>`: the next block is swallowed into this new
			// half and every line of it is WRITTEN.
			"forgotten terminator swallows the next block",
			"=== a.md\n<<<<\nold one\n====\nnew one\n<<<<\nold two\n====\nnew two\n>>>>\n",
			`line 6 is a bare "<<<<" inside the new half`,
		},
		{
			// A bare `====` in the new half is AMBIGUOUS — a missing `>>>>`, or
			// content carrying a setext underline — and the message must not
			// assert the first. Both are refused: a marker written into an
			// artifact mis-splits every later batch over that file.
			"marker line in the new half",
			"=== a.md\n<<<<\nold\n====\nnew\n====\nmore\n>>>>\n",
			`either the "====" above it is missing its ">>>>", or the text genuinely carries that line`,
		},
		{
			// The LAST line of a half. Nothing proved the scan reached it.
			"marker on the last line of the new half",
			"=== a.md\n<<<<\nold\n====\nnew\n<<<<\n>>>>\n",
			`line 6 is a bare "<<<<" inside the new half`,
		},
		{
			"marker on the last line of the old half",
			"=== a.md\n<<<<\nold\n>>>>\n====\nnew\n>>>>\n",
			`line 4 is a bare ">>>>" inside the old half`,
		},
		{
			// A forgotten `====`: the old half runs on and matches nothing —
			// this one always failed the plan, and it fails at parse now, where
			// the message can say which line is the problem.
			"marker line in the old half",
			"=== a.md\n<<<<\nold\n>>>>\nmore\n====\nnew\n>>>>\n",
			`line 4 is a bare ">>>>" inside the old half`,
		},
	}
	for _, c := range cases {
		_, err := ParseText([]byte(c.src))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want error containing %q, got %v", c.name, c.want, err)
		}
	}
	// The message names the half's OWN opener and terminator, and each half has
	// a different pair. Asserting only "line N is a bare X inside the Y half"
	// let a swap of the two survive every mutation, which is the ninth wave's
	// lesson: falling through to a true-but-useless refusal is not the same
	// guard.
	for _, c := range []struct{ src, want string }{
		{"=== a.md\n<<<<\nold\n>>>>\n====\nnew\n>>>>\n", `the "<<<<" above it was never closed`},
		{"=== a.md\n<<<<\nold\n====\nnew\n<<<<\n>>>>\n", `the "====" above it was never closed`},
	} {
		_, err := ParseText([]byte(c.src))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("want the message to contain %q, got %v", c.want, err)
		}
	}
	// The terminator itself is not a stray marker: the half ends there.
	if _, err := ParseText([]byte("=== a.md\n<<<<\nold\n====\nnew\n>>>>\n")); err != nil {
		t.Errorf("correct batch refused: %v", err)
	}
}
