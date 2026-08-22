package ws

import (
	"strings"
	"testing"
)

func TestParseFindings_scaffoldRowIsPlaceholder(t *testing.T) {
	scaffold := `# Round 1 — t

Opened at now.

| # | level | finding | closed by |
|---|-------|---------|-----------|
| 1 | BLOCKER |  |  |
`
	rows := ParseFindings([]byte(scaffold))
	if len(rows) != 1 {
		t.Fatalf("want the one data row, got %d: %+v", len(rows), rows)
	}
	if !rows[0].Placeholder() || !rows[0].Blocker() {
		t.Fatalf("scaffold row must parse as a blocker placeholder: %+v", rows[0])
	}
}

func TestParseFindings_cellsAndLines(t *testing.T) {
	doc := `intro prose, no pipes
| # | level | finding | closed by |
|---|-------|---------|-----------|
| 1 | BLOCKER | rule copy stale | 64443fea |
| 2 | major | wording drift | deferred, see decision D-7 |
broken | row without table shape
`
	rows := ParseFindings([]byte(doc))
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d: %+v", len(rows), rows)
	}
	r := rows[0]
	if r.Num != "1" || !r.Blocker() || r.Text != "rule copy stale" || r.ClosedBy != "64443fea" || r.Line != 4 {
		t.Fatalf("row 1 parsed wrong: %+v", r)
	}
	if rows[1].Blocker() {
		t.Fatalf("level %q must not read as a blocker", rows[1].Level)
	}
}

// The digit rule: hex-looking English words in a prose cell are not commit
// claims, real abbreviated shas are.
func TestFindingRow_shas(t *testing.T) {
	cases := []struct {
		cell string
		want string // comma-joined
	}{
		{"64443fea", "64443fea"},
		{"closed by 64443fea and a9431023", "64443fea,a9431023"},
		{"the fix acceded to a deadbeef facade", ""},
		{"", ""},
		{"deferred, see decision D-7", ""},
		{"2344D3D8", "2344d3d8"},
		{"0123456789abcdef0123456789abcdef01234567", "0123456789abcdef0123456789abcdef01234567"},
	}
	for _, c := range cases {
		got := strings.Join(FindingRow{ClosedBy: c.cell}.Shas(), ",")
		if got != c.want {
			t.Errorf("Shas(%q) = %q, want %q", c.cell, got, c.want)
		}
	}
}

func TestFindingRow_fixInduced(t *testing.T) {
	if !(FindingRow{Text: "seam broken by round 3's fix [fix-induced]"}).FixInduced() {
		t.Fatal("marked row must report fix-induced")
	}
	if (FindingRow{Text: "a fix-induced-looking phrase without the marker"}).FixInduced() {
		t.Fatal("prose without the bracketed marker must not count")
	}
}

func TestParseFindings_noTable(t *testing.T) {
	if rows := ParseFindings([]byte("free-form findings, nothing tabular\n")); len(rows) != 0 {
		t.Fatalf("want no rows, got %+v", rows)
	}
}

// A markdown-escaped pipe belongs to the finding TEXT. Splitting on every pipe
// read the fourth fragment as the closure, which scored an open BLOCKER as
// "closed by prose" and let the round freeze.
func TestParseFindings_escapedPipeStaysInTheText(t *testing.T) {
	doc := "| # | level | finding | closed by |\n" +
		"|---|-------|---------|-----------|\n" +
		"| 1 | BLOCKER | the regex accepts `a \\| b` and must not |  |\n"
	rows := ParseFindings([]byte(doc))
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d: %+v", len(rows), rows)
	}
	r := rows[0]
	if r.Malformed {
		t.Fatalf("an escaped pipe is not malformed: %+v", r)
	}
	if r.ClosedBy != "" {
		t.Fatalf("closed by must be the LAST cell and empty here, got %q", r.ClosedBy)
	}
	if !strings.Contains(r.Text, "a \\| b") || !strings.Contains(r.Text, "must not") {
		t.Fatalf("the whole text must survive the split, got %q", r.Text)
	}
	if !r.Blocker() || r.Placeholder() {
		t.Fatalf("an open blocker must read as one: %+v", r)
	}
}

// A table-shaped line that is not four cells is REPORTED, not skipped. Skipping
// is how a three-column row carrying an open blocker became invisible.
func TestParseFindings_shortRowIsMalformedNotSkipped(t *testing.T) {
	doc := "| # | level | finding | closed by |\n" +
		"|---|-------|---------|-----------|\n" +
		"| 1 | BLOCKER | still open |\n"
	rows := ParseFindings([]byte(doc))
	if len(rows) != 1 {
		t.Fatalf("want the malformed row reported, got %d rows", len(rows))
	}
	if !rows[0].Malformed || rows[0].Placeholder() {
		t.Fatalf("want a malformed, non-placeholder row, got %+v", rows[0])
	}
	if !strings.Contains(rows[0].Raw, "still open") {
		t.Fatalf("the refusal must be able to quote the line, got %q", rows[0].Raw)
	}
}

// An UNESCAPED pipe in prose leaves the parser unable to tell text from
// closure, and guessing is what it must not do.
func TestParseFindings_unescapedPipeIsMalformed(t *testing.T) {
	doc := "| # | level | finding | closed by |\n" +
		"|---|-------|---------|-----------|\n" +
		"| 1 | BLOCKER | A | B is the rule | 2344d3d8 |\n"
	rows := ParseFindings([]byte(doc))
	if len(rows) != 1 || !rows[0].Malformed {
		t.Fatalf("want one malformed row, got %+v", rows)
	}
}

// The untouched scaffold still parses: an empty LAST cell keeps its place.
func TestParseFindings_scaffoldKeepsItsEmptyClosure(t *testing.T) {
	doc := "| # | level | finding | closed by |\n" +
		"|---|-------|---------|-----------|\n" +
		"| 1 | BLOCKER |  |  |\n"
	rows := ParseFindings([]byte(doc))
	if len(rows) != 1 || rows[0].Malformed || !rows[0].Placeholder() {
		t.Fatalf("the scaffold row must parse as a placeholder, got %+v", rows)
	}
}
