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
}

// Blocker matches by word, not by list: levels beyond BLOCKER are free text,
// and only BLOCKER carries the scaffold's "must be closed before freezing"
// contract.
func (r FindingRow) Blocker() bool { return strings.EqualFold(r.Level, "BLOCKER") }

// Placeholder reports the scaffold's empty row, which is a form to fill, not a
// claim to validate.
func (r FindingRow) Placeholder() bool { return r.Text == "" }

// FixInduced reports the opt-in `[fix-induced]` marker in the finding text.
// The share of a round's findings introduced by the previous round's own fix
// is the number that says a gate is exhausted — it climbed to 100% on the
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
		parts := strings.Split(t, "|")
		if len(parts) < 6 { // leading empty, 4 cells, trailing empty
			continue
		}
		cells := parts[1 : len(parts)-1]
		for j := range cells {
			cells[j] = strings.TrimSpace(cells[j])
		}
		if cells[0] == "#" || isSeparator(cells[0]) {
			continue
		}
		rows = append(rows, FindingRow{
			Line: i + 1, Num: cells[0], Level: cells[1], Text: cells[2], ClosedBy: cells[3],
		})
	}
	return rows
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
