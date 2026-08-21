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
		{"0123456789abcdef0123456789abcdef01234567", "0123456789abcdef0123456789abcdef01234567"},
	}
	for _, c := range cases {
		got := strings.Join(FindingRow{ClosedBy: c.cell}.Shas(), ",")
		if got != c.want {
			t.Errorf("Shas(%q) = %q, want %q", c.cell, got, c.want)
		}
	}
}

func TestParseFindings_noTable(t *testing.T) {
	if rows := ParseFindings([]byte("free-form findings, nothing tabular\n")); len(rows) != 0 {
		t.Fatalf("want no rows, got %+v", rows)
	}
}
