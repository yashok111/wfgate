package ws

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The two setters rewrite ONE cell in place and promise every other byte of
// the row back unchanged: parse, set, parse again, and the three cells the
// setter did not name read exactly as before, while the one it did reads the
// value. A value is what the command validates before it gets here — a sha or
// a number — so it carries no pipe and no backslash.
func FuzzFindingsSettersKeepTheRow(f *testing.F) {
	f.Add("| 1 | BLOCKER | a `x \\| y` finding | abc1234 |", "deadbe1")
	f.Add("  | #3 | MINOR | text | |", "7")
	f.Add("| 1 | MAJOR | tail | sha | extra |", "a1b2c3d")
	f.Add("|||", "1")
	f.Add("| 1 | NOTE | naïve | ✓ |", "1")
	f.Fuzz(func(t *testing.T, row, val string) {
		// The setters take ONE line — the caller indexes the split the parser
		// numbered — so a row with a newline in it is two rows to the parser
		// and one to the setter, and not a shape either is given.
		if strings.ContainsAny(val, "|\\\n") || strings.ContainsRune(row, '\n') {
			return
		}
		val = strings.TrimSpace(val)
		// A `#` or a `---` written as the number turns the row into the
		// header or the separator the parser skips; the command never writes
		// either, and the fuzzer wrote both inside a minute.
		if val == "#" || isSeparator(val) || !utf8.ValidString(val) {
			return // and the splitter reads runes, so an invalid byte comes back as U+FFFD
		}
		before := ParseFindings([]byte(row))
		if len(before) != 1 || before[0].Malformed {
			return
		}
		b := before[0]
		if r2, ok := SetClosedBy(row, val); ok {
			after := ParseFindings([]byte(r2))
			if len(after) != 1 || after[0].Malformed {
				t.Fatalf("SetClosedBy made the row malformed: %q -> %q", row, r2)
			}
			a := after[0]
			if a.Num != b.Num || a.Level != b.Level || a.Text != b.Text || a.ClosedBy != val {
				t.Fatalf("SetClosedBy(%q, %q) = %q: %+v -> %+v", row, val, r2, b, a)
			}
		}
		if r2, ok := SetNum(row, val); ok {
			after := ParseFindings([]byte(r2))
			if len(after) != 1 || after[0].Malformed {
				t.Fatalf("SetNum made the row malformed: %q -> %q", row, r2)
			}
			a := after[0]
			if a.Num != val || a.Level != b.Level || a.Text != b.Text || a.ClosedBy != b.ClosedBy {
				t.Fatalf("SetNum(%q, %q) = %q: %+v -> %+v", row, val, r2, b, a)
			}
		}
	})
}

// A commit claim is hex, 7 to 40 long, with a digit, lowercased from the cell,
// and never the one written `target:`.
func FuzzShas(f *testing.F) {
	f.Add("fixed in ABC1234 and deadbeef, target:9f8e7d6 not")
	f.Add("")
	f.Fuzz(func(t *testing.T, cell string) {
		lower := strings.ToLower(cell)
		for _, s := range (FindingRow{ClosedBy: cell}).Shas() {
			if len(s) < 7 || len(s) > 40 || !strings.ContainsAny(s, "0123456789") || strings.Trim(s, "0123456789abcdef") != "" {
				t.Fatalf("sha %q from %q", s, cell)
			}
			if !strings.Contains(lower, s) {
				t.Fatalf("sha %q not in %q", s, lower)
			}
		}
	})
}
