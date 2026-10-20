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

	// TailCol is the 1-based column of the FOURTH unescaped pipe — in a
	// well-formed row, the boundary where the closed-by cell begins — and Tail
	// is everything after it. Both are zero/empty on a row with too FEW cells,
	// where no pipe is the offender: pointing at a character that is not the
	// problem is worse than pointing at none.
	//
	// The column is in CHARACTERS and counted from the start of the line AS
	// WRITTEN, indentation included, because that is what the operator's editor
	// shows them. The first version counted from the start of the TRIMMED row,
	// so an indented row and the same row unindented reported one column, and
	// the indented one pointed at the wrong character. Characters and not
	// bytes: gate artifacts are English by workflow rule, but a finding quoting
	// a Russian product string quotes it as DATA, and there the two differ —
	// the unit is stated rather than left to be discovered.
	//
	// Measured on the P3d gate, the second run to pay for this class: the pipe
	// was not even authored — a shell hook rewrote an `rg` command inside the
	// heredoc that was writing the table and injected an alternation pipe into
	// the TEXT of a row. Two rejected records, and finding the cell meant
	// `awk NR==34 | tr '|' '\n'` by hand. The refusal named the row and the
	// cell count and pointed at nothing. WHICH extra pipe is the author's is
	// not knowable — a row with five cells has one too many and any of them
	// could be it — so what is printed is the boundary the parser read the
	// text up to, and the tail it could not place.
	TailCol int
	Tail    string
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
// did — the scripts these six gates PRODUCED carry three of them:
// `['BLOCKER','MAJOR','MINOR']` (p2d, under the field name `level`, and p2f
// under `severity`), `['MAJOR','MINOR']` (p2e), and `['blocker','advisory']`
// (a1, a2, a3, straight out of this repository's own template). The field NAME
// drifted too, which no value-level check can see — and the first census missed
// p2d entirely by grepping `severity`.
//
// Six OTHER phases' scripts do agree on `['blocker','major','minor']`, and the
// agreement is one COPIED LINE, not a shared ancestor: this repository has
// measured those same six as otherwise RE-AUTHORED, two of their gate functions
// implementing one algorithm at 0.05 textual similarity. So this is not "the
// layer that validates"; it is the one place both paths meet, which is why the
// scale is closed here.
//
// BLOCKER / MAJOR / MINOR is not an invention — it is what four of the six
// ledgers already used, and what TWO of their own scripts declared. NOTE is the
// fourth because ONE gate reached for a non-defect row and had nowhere to file
// it (a3's single NOTE row, elided: "The script executed as written: 38 agents,
// 0 errors, 0 dead, `green: true` … No finding."). p3a's NIT is that gate's MINOR tier, not a non-defect
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
//
// A token written `target:<sha>` is NOT a claim: it names a commit in the
// repository under review, which this workspace cannot check and no reader here
// verifies, so it counts as prose. Two gates in a row (P4a, A18) fought `wf
// round close -prose` over exactly this — the closure was "built in the target
// at 39f4fc9", the detector read a commit claim, and the refusal's advice
// ("close it with that commit instead") named a commit the workspace does not
// have. The prefix is the spelling the refusal now prescribes.
func (r FindingRow) Shas() []string {
	var out []string
	lower := strings.ToLower(r.ClosedBy)
	for _, loc := range shaRe.FindAllStringIndex(lower, -1) {
		m := lower[loc[0]:loc[1]]
		if !strings.ContainsAny(m, "0123456789") {
			continue
		}
		if strings.HasSuffix(lower[:loc[0]], TargetShaPrefix) {
			continue
		}
		out = append(out, m)
	}
	return out
}

// TargetShaPrefix marks a sha in a closure cell as belonging to the TARGET
// repository: unverifiable here by construction, read as prose by every reader.
const TargetShaPrefix = "target:"

// SetNum rewrites the `#` cell of one findings row IN PLACE — the first cell,
// between the first and second unescaped pipes — and leaves every other byte of
// the line as written. Same discipline as SetClosedBy: the row is never split
// and re-joined. The second return is false when the line does not carry an
// opening pipe followed by a second unescaped one.
func SetNum(row, val string) (string, bool) {
	esc := false
	first, second := -1, -1
	for i, r := range row {
		switch {
		case esc:
			esc = false
		case r == '\\':
			esc = true
		case r == '|':
			if first < 0 {
				first = i
			} else {
				second = i
			}
		}
		if second >= 0 {
			break
		}
	}
	if first < 0 || second < 0 {
		return row, false
	}
	return row[:first+1] + " " + val + " " + row[second:], true
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
		// What the trim took off the front, so a column names the line as the
		// operator sees it. Bytes and characters are the same number here by
		// construction — the prefix is spaces and tabs — and counting runes
		// over it was a care no mutation could make fail, so it says what it
		// is instead.
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		cells, at := splitCells(t)
		if len(cells) == 0 {
			continue
		}
		if cells[0] == "#" || isSeparator(cells[0]) {
			continue
		}
		if len(cells) != 4 {
			r := FindingRow{Line: i + 1, Malformed: true, Raw: t, Cells: len(cells)}
			if len(cells) > 4 {
				r.TailCol = at[2] + indent
				r.Tail = strings.Join(cells[3:], " | ")
			}
			rows = append(rows, r)
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
// The second return is the 1-based COLUMN of the pipe that terminated each
// cell, in the row as passed (0 for the last cell, which ends at end of line).
// It travels with the cells because the caller's refusal has to point at a
// character and the split is the only place that knows where the characters
// were.
func splitCells(row string) ([]string, []int) {
	var cells []string
	var at []int
	var b strings.Builder
	esc := false
	col := 0
	for _, r := range row {
		col++
		switch {
		case esc:
			b.WriteRune(r) // the backslash is already written; keep the cell as written
			esc = false
		case r == '\\':
			b.WriteRune(r)
			esc = true
		case r == '|':
			cells = append(cells, strings.TrimSpace(b.String()))
			at = append(at, col)
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	cells = append(cells, strings.TrimSpace(b.String()))
	at = append(at, 0)
	// `| a | b |` splits into an empty cell at each end; a genuinely empty LAST
	// cell (the scaffold's unfilled "closed by") keeps its place because only
	// one trailing empty is dropped.
	if len(cells) > 0 && cells[0] == "" {
		cells, at = cells[1:], at[1:]
	}
	if len(cells) > 0 && cells[len(cells)-1] == "" {
		cells, at = cells[:len(cells)-1], at[:len(at)-1]
	}
	return cells, at
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

// SetClosedBy rewrites the closure cell of one findings row IN PLACE, without
// splitting the line into cells and joining it back.
//
// That is the whole point of the function. Measured on the first gate outside
// mocker: both times the ledger was corrupted it was the operator's own
// `awk -F'|'` filling this column — the table requires a literal pipe inside a
// cell to be written `\|`, and every field-splitting tool splits on the escape
// too, so the commit id landed in the middle of a finding's text. The demand
// for the escape is right; what was missing was a way to fill the column that
// never splits the row. Everything before the fourth unescaped pipe and
// everything from the fifth on are returned byte-identical.
//
// The second return is false when the line does not carry four unescaped pipes
// — a malformed row, which the caller refuses rather than repairs.
func SetClosedBy(row, val string) (string, bool) {
	var pipes []int
	esc := false
	for i, r := range row {
		switch {
		case esc:
			esc = false
		case r == '\\':
			esc = true
		case r == '|':
			pipes = append(pipes, i)
		}
	}
	if len(pipes) < 4 {
		return row, false
	}
	head := row[:pipes[3]+1]
	tail := ""
	if len(pipes) >= 5 {
		tail = row[pipes[4]:]
	} else if strings.TrimSpace(val) == "" {
		// A row with no closing pipe — markdown allows `| a | b | c | d`, and
		// the setter keeps the spelling it was given — holds its fourth cell
		// only while something is written in it: the parser drops ONE trailing
		// empty cell, so an EMPTY closure written there left three cells and
		// the next record refused the row as malformed. Found by the fuzzer
		// (`||||0`, value ""). The one byte the setter adds that the author
		// did not write is the pipe the row was missing, and only here.
		tail = "|"
	}
	// No special case for an empty value: the general form already writes the
	// scaffold's own `|  |`, and the branch that used to handle it wrote `| |`
	// — a shape nothing else in this repository produces. It was also
	// unreachable from the command, which validates the sha as a commit before
	// it gets here. Deleted rather than fixed, twice over.
	return head + " " + val + " " + tail, true
}
