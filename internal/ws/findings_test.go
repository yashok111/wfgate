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
	// WHERE, not just THAT. Two gates have paid for this class, and on the
	// second the pipe was injected into the row by a shell hook rewriting a
	// command quoted inside the heredoc that wrote the table — so the operator
	// was hunting a character they had never typed, by hand, with `awk` and
	// `tr`. The fifth cell here opens at the pipe after `A`.
	row := doc[strings.Index(doc, "| 1 |"):]
	col := strings.Index(row, "| B is the rule") + 1 // the 4th unescaped pipe
	if rows[0].TailCol != col {
		t.Fatalf("the 4th pipe is at column %d, got %d", col, rows[0].TailCol)
	}
	// The tail is what the parser could not place, and the injected pipe is
	// inside it: WHICH pipe is the extra one is not knowable from the row.
	if rows[0].Tail != "B is the rule | 2344d3d8" {
		t.Fatalf("the tail past the 4th pipe must be quoted whole, got %q", rows[0].Tail)
	}
}

// The column is in CHARACTERS and counted from the start of the line AS
// WRITTEN. An indented row and the same row unindented used to report ONE
// column, and the indented one pointed at the wrong character; a row quoting a
// Russian product string as DATA is where characters and bytes part company,
// and the unit is stated rather than left to be discovered.
func TestParseFindings_columnCountsCharactersOfTheWrittenLine(t *testing.T) {
	rowOf := func(prefix string) FindingRow {
		doc := "| # | level | finding | closed by |\n" +
			"|---|-------|---------|-----------|\n" +
			prefix + "| 1 | BLOCKER | ру|сский | 2344d3d8 |\n"
		rows := ParseFindings([]byte(doc))
		if len(rows) != 1 || !rows[0].Malformed {
			t.Fatalf("want one malformed row, got %+v", rows)
		}
		return rows[0]
	}
	bare := rowOf("")
	// `| 1 | BLOCKER | ру|` — the 4th pipe is the 19th CHARACTER; it is the
	// 23rd byte, because each Cyrillic letter is two.
	if bare.TailCol != 19 {
		t.Fatalf("the 4th pipe is character 19 of that row, got %d", bare.TailCol)
	}
	indented := rowOf("    ")
	if indented.TailCol != bare.TailCol+4 {
		t.Fatalf("indentation moves the column: want %d, got %d", bare.TailCol+4, indented.TailCol)
	}
}

// A row with too FEW cells has no offending pipe, and inventing a column for
// one would point the operator at a character that is not the problem.
func TestParseFindings_shortRowNamesNoColumn(t *testing.T) {
	doc := "| # | level | finding | closed by |\n" +
		"|---|-------|---------|-----------|\n" +
		"| 1 | BLOCKER | A |\n"
	rows := ParseFindings([]byte(doc))
	if len(rows) != 1 || !rows[0].Malformed {
		t.Fatalf("want one malformed row, got %+v", rows)
	}
	if rows[0].TailCol != 0 || rows[0].Tail != "" {
		t.Fatalf("a short row names no column, got col=%d tail=%q", rows[0].TailCol, rows[0].Tail)
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

// The four states of a ledger the callers must keep apart. A missing file
// already had its own name (`noLedger`); an unfilled FORM fell through the same
// gap one case over and printed `0 finding(s), 0 blocker(s)` — which is what
// the round that ENDS a gate prints.
func TestScaffoldOnlySeparatesAnEmptyFormFromEverythingElse(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		// No table at all is "nothing was validated", which is a different
		// refusal with a different repair — never an unfilled form.
		{"no table", "# Findings\n\nprose only\n", false},
		{"the untouched scaffold", head + "| 1 | BLOCKER |  |  |\n", true},
		{"two untouched rows", head + "| 1 | BLOCKER |  |  |\n| 2 | MAJOR |  |  |\n", true},
		{"one filled row", head + "| 1 | MAJOR | the seam is undecided |  |\n", false},
		// An unreadable claim and an unfilled form are opposite things:
		// collapsing them hands a malformed row the scaffold's exemption.
		{"a malformed row", head + "| 1 | BLOCKER | still open |\n", false},
		{"a malformed row beside an empty one", head + "| 1 | BLOCKER |  |  |\n| 2 | BLOCKER | open |\n", false},
	}
	for _, c := range cases {
		if got := ScaffoldOnly(ParseFindings([]byte(c.body))); got != c.want {
			t.Errorf("%s: ScaffoldOnly = %v, want %v", c.name, got, c.want)
		}
	}
}

const head = "# Findings\n\n| # | level | finding | closed by |\n|---|-------|---------|-----------|\n"

// SetClosedBy exists because a hand `awk -F'|'` corrupted a live gate's ledger
// twice: the table escapes a literal pipe as `\|` and every field-splitting
// tool splits on the escape too, writing the commit id into a finding's text.
func TestSetClosedBy(t *testing.T) {
	for _, tc := range []struct {
		name, row, val, want string
		ok                   bool
	}{
		{
			name: "an escaped pipe in the text is not a cell boundary",
			row:  `| 1 | MAJOR | the doc says ` + "`rg --max-columns=200 --max-columns-preview x \\| head`" + ` |  |`,
			val:  "abc1234",
			want: `| 1 | MAJOR | the doc says ` + "`rg --max-columns=200 --max-columns-preview x \\| head`" + ` | abc1234 |`,
			ok:   true,
		},
		{
			name: "an existing value is replaced, not appended",
			row:  "| 2 | MAJOR | a finding | old9999 |",
			val:  "new1111",
			want: "| 2 | MAJOR | a finding | new1111 |",
			ok:   true,
		},
		{
			// The trailing pipe is optional in markdown and both spellings are
			// in the corpus. Whatever the row had is what it keeps.
			name: "a row with no trailing pipe keeps having none",
			row:  "| 3 | MAJOR | a finding | ",
			val:  "abc1234",
			want: "| 3 | MAJOR | a finding | abc1234 ",
			ok:   true,
		},
		{
			name: "an emptied cell keeps the table's shape",
			row:  "| 4 | MAJOR | a finding | abc1234 |",
			val:  "",
			want: "| 4 | MAJOR | a finding |  |",
			ok:   true,
		},
		{
			name: "a row without four unescaped pipes is refused, not repaired",
			row:  "| 5 | MAJOR | a finding with no closure cell",
			val:  "abc1234",
			want: "| 5 | MAJOR | a finding with no closure cell",
			ok:   false,
		},
		{
			// The whole point: a row whose text carries THREE escaped pipes
			// still has its fourth real pipe where the parser says it is.
			name: "escapes never advance the pipe count",
			row:  `| 6 | MAJOR | a \| b \| c \| d |  |`,
			val:  "abc1234",
			want: `| 6 | MAJOR | a \| b \| c \| d | abc1234 |`,
			ok:   true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := SetClosedBy(tc.row, tc.val)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if got != tc.want {
				t.Fatalf("got  %q\nwant %q", got, tc.want)
			}
			if !tc.ok {
				return
			}
			// The row must still parse into the four cells it claims, with the
			// closure where it was put — the read-back the command does.
			rows := ParseFindings([]byte("| # | level | finding | closed by |\n|---|---|---|---|\n" + got + "\n"))
			if len(rows) != 1 || rows[0].Malformed {
				t.Fatalf("the rewritten row no longer parses: %+v", rows)
			}
			if rows[0].ClosedBy != tc.val {
				t.Fatalf("closure reads back as %q, want %q", rows[0].ClosedBy, tc.val)
			}
		})
	}
}

// A sha written `target:<sha>` names a commit in the repository under review,
// which this workspace cannot check; it is prose to every reader. Two gates in
// a row fought `wf round close -prose` over exactly this spelling's absence.
func TestShas_targetPrefixIsProse(t *testing.T) {
	cases := map[string]int{
		"built in the target at target:39f4fc9a":  0,
		"built in the target at 39f4fc9a":         1,
		"TARGET:39F4FC9A then also 784f636d here": 1,
		"target: 39f4fc9a":                        1, // the space breaks the prefix; the sha is bare
	}
	for in, want := range cases {
		if got := (FindingRow{ClosedBy: in}).Shas(); len(got) != want {
			t.Errorf("%q: Shas = %v, want %d", in, got, want)
		}
	}
}

func TestSetNum_rewritesTheFirstCellInPlace(t *testing.T) {
	row := "|  | MINOR | quotes `a \\| b` | x |"
	got, ok := SetNum(row, "7")
	if !ok || got != "| 7 | MINOR | quotes `a \\| b` | x |" {
		t.Fatalf("SetNum = %q, %v", got, ok)
	}
	if _, ok := SetNum("no pipes here", "1"); ok {
		t.Fatal("a line with no row shape must report false")
	}
	if _, ok := SetNum("| only one pipe", "1"); ok {
		t.Fatal("a line with one pipe must report false")
	}
}
