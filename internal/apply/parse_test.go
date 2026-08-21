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
		{"\n\n", "no edit blocks"},
	}
	for _, c := range cases {
		if _, err := ParseText([]byte(c.src)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("ParseText(%q): want error containing %q, got %v", c.src, c.want, err)
		}
	}
}
