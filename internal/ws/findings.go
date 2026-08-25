package ws

import (
	"regexp"
	"strings"
)

// The findings table is parsed, never generated. The format is not invented
// here: it is the table `wf round new` scaffolds and four gate rounds filled by
// hand without changing. Parsing the human-written table — rather than adding a
// machine format for humans to mirror — is the deliberate first step of the
// deferred findings registry: closures become checkable while the file stays
// the thing a reviewer actually reads.

// FindingRow is one data row of a round's findings table. Cells are kept as
// written; classification is the caller's job.
type FindingRow struct {
	Line     int    // 1-based line in findings.md, for error messages
	Num      string // the "#" cell as written
	Level    string
	Text     string
	ClosedBy string

	// Malformed marks a table-shaped line that is NOT four cells. It is a row
	// rather than a skip because skipping is how a ledger validates nothing and
	// says nothing: the first version dropped any line with too few cells, so a
	// three-column row carrying an open BLOCKER was invisible and the round
	// froze green. The caller must refuse it out loud.
	Malformed bool
	Raw       string // the line as written, so the refusal can quote it
	Cells     int    // how many cells the splitter produced, for the refusal to state
}

// Levels is the CLOSED severity scale, in descending order.
//
// It is closed because the open one drifted, measured 2026-08-25 with this same
// parser over the six gate ledgers on disk — 1065 findings, every total
// reconciled against that workspace's own `wf stats`:
//
//	p2d    98  BLOCKER  26  MAJOR 42  MINOR 30
//	p2e   234  BLOCKER  79  MAJOR 95  MINOR 60
//	p2f   111  BLOCKER  28  MAJOR 40  MINOR 43
//	a-mcp 196  BLOCKER  58  MAJOR 57  ADVISORY 81
//	a3    268  BLOCKER 114  MAJOR 90  MINOR 63  NOTE 1
//	p3a   158  BLOCKER  67  CONCERN 70  NIT 21
//
// BLOCKER is universal. Under it, MAJOR on five gates and CONCERN on one; under
// THAT, MINOR on four, ADVISORY on one, NIT on one. Four vocabularies for one
// scale, and 172 of the 1065 findings — 16% — carry a level no other gate can
// be read against. Those 172 live in TWO workspaces, one closed and pruned
// (a-mcp, ADVISORY 81) and one still OPEN (p3a, CONCERN 70 + NIT 21), so the
// figure above is dated rather than current.
//
// A JSON Schema `enum` does NOT save the script side, and the first version of
// this comment claimed it did. An enum validates the VALUE against a declared
// list and never the LIST itself, and the list drifted exactly as the ledgers
// did — the scripts these six gates PRODUCED carry four of them: none at all
// (p2d), `['MAJOR','MINOR']` (p2e), `['BLOCKER','MAJOR','MINOR']` (p2f) and
// `['blocker','advisory']` (a1, a2, a3, straight out of this repository's own
// template). Six OTHER phases' scripts do agree on `['blocker','major','minor']`,
// and they agree because all six descend from one ancestor — a fact about
// copying, not about validation. So this is not "the layer that validates";
// it is the one place both paths meet, which is why the scale is closed here.
//
// BLOCKER / MAJOR / MINOR is not an invention — it is what four of the six
// ledgers already used, and what one gate's own script declared. NOTE is the
// fourth because ONE gate reached for a non-defect row and had nowhere to file
// it (a3's single NOTE row: "The script executed as written: 38 agents, 0
// errors. No finding."). p3a's NIT is that gate's MINOR tier, not a non-defect
// level.
var Levels = []string{"BLOCKER", "MAJOR", "MINOR", "NOTE"}

// KnownLevel reports whether the row's level is on the closed scale. Reading
// stays permissive on purpose: two workspaces — one closed and pruned, one
// still open — carry ADVISORY, CONCERN and NIT rows, and a report that refused
// to read them would destroy the only corpus this project reasons from. The
// refusal lives on the WRITE path, which is the one place a new spelling can
// still be prevented.
//
// No TrimSpace here or below: splitCells already trims every cell and
// ParseFindings is the only constructor of a FindingRow, so a trim added here
// survived every mutation. This project deletes a guard nothing can make fail
// rather than shipping it unproven.
func (r FindingRow) KnownLevel() bool {
	for _, l := range Levels {
		if strings.EqualFold(r.Level, l) {
			return true
		}
	}
	return false
}

// Blocker matches by word, not by list: only BLOCKER carries the scaffold's
// "must be closed before freezing" contract, and the stop rule reads it alone.
func (r FindingRow) Blocker() bool { return strings.EqualFold(r.Level, "BLOCKER") }

// Note reports the one level that is NOT a defect claim.
//
// It is counted APART rather than subtracted: every finding total this project
// has banked includes whatever non-defect rows its gate happened to file, so
// removing them from the count would silently move five gates' numbers. The
// report says how many of the findings asserted no failure and leaves the total
// where the corpus can still be compared against it.
func (r FindingRow) Note() bool { return strings.EqualFold(r.Level, "NOTE") }

// Placeholder reports the scaffold's empty row, which is a form to fill, not a
// claim to validate. A malformed row is never a placeholder: an unreadable
// claim and an unfilled form are opposite things, and collapsing them would
// hand a malformed row the exemption the scaffold gets.
func (r FindingRow) Placeholder() bool { return !r.Malformed && r.Text == "" }

// FixInduced reports the opt-in `[fix-induced]` marker in the finding text.
// The share of a round's findings introduced by the previous round's own fix
// is a continue signal, not a stop signal: the gate ends on a REVIEWER round
// that produces no BLOCKER, and on nothing else [R1] — it climbed to 100% on the
// first live gate's last two rounds — and the ledger can only carry it where
// the author marked it, which is why stats prints the marker's population
// rather than pretending an unmarked corpus measured anything.
func (r FindingRow) FixInduced() bool { return strings.Contains(r.Text, "[fix-induced]") }

// A commit claim is a hex token of 7-40 chars containing at least one digit.
// The digit rule is what keeps English out: "acceded"- and "deadbeef"-class
// words are pure-letter hex, and a prose cell mentioning one must not be
// rejected as a lying sha. An all-letter abbreviated sha (odds ~4e-4 at 8
// chars) degrades to "verify by hand" — safe in the direction that matters.
var shaRe = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)

// Shas extracts the commit claims from the closed-by cell. The cell is
// lowercased first: an UPPERCASE sha pasted from some UI is the same claim, and
// classifying it as prose silently demotes a verifiable closure to "verify by
// hand" (measured on the first live run).
func (r FindingRow) Shas() []string {
	var out []string
	for _, m := range shaRe.FindAllString(strings.ToLower(r.ClosedBy), -1) {
		if strings.ContainsAny(m, "0123456789") {
			out = append(out, m)
		}
	}
	return out
}

// ParseFindings extracts the table rows from a findings document. A document
// with no table yields no rows — the CALLER must say so out loud, because a
// validation that silently checked nothing is this project's named worst
// failure.
func ParseFindings(b []byte) []FindingRow {
	var rows []FindingRow
	for i, line := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "|") {
			continue
		}
		cells := splitCells(t)
		if len(cells) == 0 {
			continue
		}
		if cells[0] == "#" || isSeparator(cells[0]) {
			continue
		}
		if len(cells) != 4 {
			rows = append(rows, FindingRow{Line: i + 1, Malformed: true, Raw: t, Cells: len(cells)})
			continue
		}
		rows = append(rows, FindingRow{
			Line: i + 1, Num: cells[0], Level: cells[1], Text: cells[2], ClosedBy: cells[3], Cells: 4,
		})
	}
	return rows
}

// splitCells splits a markdown table row on its UNESCAPED pipes.
//
// Measured: the first version split on every `|` and read cells[3] as the
// closure. A finding whose text quotes a pipe — written `\|`, as markdown
// requires — shifted a fragment of that text into the closed-by cell, so an
// open BLOCKER was scored as "closed by prose" and `wf freeze` tagged the
// round. The tag is one-way, and the ledger is the one place the gate promises
// no blocker survives the freeze.
//
// A row whose pipes are NOT escaped comes out with the wrong cell count and is
// returned as such: the caller reports it as malformed rather than guessing
// which fragment was the author's closure.
func splitCells(row string) []string {
	var cells []string
	var b strings.Builder
	esc := false
	for _, r := range row {
		switch {
		case esc:
			b.WriteRune(r) // the backslash is already written; keep the cell as written
			esc = false
		case r == '\\':
			b.WriteRune(r)
			esc = true
		case r == '|':
			cells = append(cells, strings.TrimSpace(b.String()))
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	cells = append(cells, strings.TrimSpace(b.String()))
	// `| a | b |` splits into an empty cell at each end; a genuinely empty LAST
	// cell (the scaffold's unfilled "closed by") keeps its place because only
	// one trailing empty is dropped.
	if len(cells) > 0 && cells[0] == "" {
		cells = cells[1:]
	}
	if len(cells) > 0 && cells[len(cells)-1] == "" {
		cells = cells[:len(cells)-1]
	}
	return cells
}

func isSeparator(cell string) bool {
	if cell == "" {
		return false
	}
	for _, c := range cell {
		if c != '-' && c != ':' {
			return false
		}
	}
	return true
}

// ScaffoldOnly reports a ledger that PARSED and carries nothing but the
// scaffold's unfilled rows.
//
// It exists because `0 finding(s), 0 blocker(s)` was printed identically for a
// round whose reviewers found nothing and for a round that was opened, frozen
// and never dispatched — and under the stop rule the second reads as a legal
// close. Measured on a live gate: round 9 was opened, tagged `gate-r9`, no lens
// was ever run against it, and every derived number in the workspace called it
// a measured zero. A missing ledger is already named (`noLedger`); an untouched
// FORM fell through the same gap one case over.
//
// A malformed row makes this false, and it needs no clause of its own:
// Placeholder is already false for one, by construction. An unreadable claim is
// not an unfilled form, and the caller refuses it by its own name.
func ScaffoldOnly(rows []FindingRow) bool {
	if len(rows) == 0 {
		return false
	}
	for _, r := range rows {
		if !r.Placeholder() {
			return false
		}
	}
	return true
}
