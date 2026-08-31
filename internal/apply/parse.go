package apply

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseText parses the plain-text batch format:
//
//	=== <file> [count=N] [note=free text to the end of the line]
//	<<<<
//	old text, verbatim
//	====
//	new text, verbatim
//	>>>>
//
// The format exists because assembling the JSON batch was the measured
// bottleneck of the first live gate: a 35-edit batch with multi-line `old`
// strings had to survive three layers of quoting (JSON inside a heredoc inside
// a shell), and the escaping failures cost more attempts than the edits
// themselves. Here nothing is ever escaped: old and new are taken verbatim
// between marker lines.
//
// A marker line counts only when it is EXACTLY `<<<<`, `====` or `>>>>` —
// unindented, nothing else on the line. A marker line INSIDE either half is
// refused, and the refusal is the whole of the guard: the manual used to say a
// collision "mis-splits and then fails the plan loudly, because the mis-split
// old matches nothing", and that is true only of the OLD half. In the NEW half
// it matches EVERYTHING and is written. Measured on the P3d gate: a block
// whose `>>>>` was forgotten swallowed the next block whole, every edit
// reported `ok (1 match)`, `verified 1 file(s) byte-for-byte against the
// planned batch` printed — the verification is against the batch AS PARSED, so
// a mis-split is confirmed in the same words a correct batch gets — and the
// literal `<<<<` and `====` lines landed in the artifact, took `wfcheck` from
// 0 errors to `Unexpected token ===`, and cost two `git revert`s and about half
// a round. A text half that must carry such a line is the one case for the JSON
// batch format, where the halves are delimited by nothing.
func ParseText(b []byte) ([]Edit, error) {
	lines := strings.Split(string(b), "\n")
	var edits []Edit
	i := 0
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			i++
			continue
		}
		if !strings.HasPrefix(line, "=== ") {
			// The one shape operators actually reach for (measured on the P2e
			// gate: one header, fifteen blocks) gets the rule spelled out at
			// the point of failure, not a generic complaint.
			if line == "<<<<" {
				return nil, fmt.Errorf("line %d: each replacement block carries its own `=== <file>` header — count= is scoped to one replacement, not to the file", i+1)
			}
			return nil, fmt.Errorf("line %d: expected `=== <file>` header, got %q", i+1, line)
		}
		e := Edit{}
		rest := strings.TrimSpace(line[len("=== "):])
		// The manual renders this header as `=== <file> [count=N] [note=...]`,
		// where the brackets mean OPTIONAL — inside a format whose other
		// delimiters (`<<<<`, `====`, `>>>>`) are literal. Measured on the P3f
		// gate: the operator wrote the brackets, which is one of the two
		// defensible readings of that line, and paid a round-trip. Both halves
		// of the class are closed here, and they fail differently without it:
		// `[count=1]` was refused as an unknown field, naming the correct
		// spelling but not the mistake, while `[note=x]` was accepted SILENTLY
		// with the closing bracket inside the note. The refusal names the patch.
		if err := refuseBracketedFields(rest, i+1); err != nil {
			return nil, err
		}
		// note= swallows the rest of the line, so it may contain spaces and
		// even a stray count= — parse it off first.
		if k := strings.Index(rest, "note="); k >= 0 {
			e.Note = strings.TrimSpace(rest[k+len("note="):])
			rest = rest[:k]
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return nil, fmt.Errorf("line %d: header names no file", i+1)
		}
		e.File = fields[0]
		for _, f := range fields[1:] {
			v, ok := strings.CutPrefix(f, "count=")
			if !ok {
				return nil, fmt.Errorf("line %d: unknown header field %q (count=N and note=... only)", i+1, f)
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return nil, fmt.Errorf("line %d: count=%q is not a positive number", i+1, v)
			}
			e.Count = n
		}
		i++
		for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
			i++
		}
		if i >= len(lines) || lines[i] != "<<<<" {
			return nil, fmt.Errorf("%s: expected `<<<<` after the header", e.File)
		}
		oldStart := i + 1
		old, next, err := collectUntil(lines, oldStart, "====")
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.File, err)
		}
		if err := noStrayMarker(lines, oldStart, next-1, e.File, "old"); err != nil {
			return nil, err
		}
		e.Old = old
		newStart := next
		nw, next, err := collectUntil(lines, newStart, ">>>>")
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.File, err)
		}
		if err := noStrayMarker(lines, newStart, next-1, e.File, "new"); err != nil {
			return nil, err
		}
		e.New = nw
		i = next
		edits = append(edits, e)
	}
	if len(edits) == 0 {
		return nil, fmt.Errorf("no edit blocks found")
	}
	return edits, nil
}

func collectUntil(lines []string, i int, term string) (string, int, error) {
	var buf []string
	for ; i < len(lines); i++ {
		if lines[i] == term {
			return strings.Join(buf, "\n"), i + 1, nil
		}
		buf = append(buf, lines[i])
	}
	return "", 0, fmt.Errorf("unterminated block: no %q line", term)
}

// noStrayMarker refuses a block half carrying a bare marker line.
//
// The half is already delimited by the marker that ENDED it, so anything left
// inside is a marker the format cannot have meant: a forgotten `>>>>` makes the
// following block part of this one's new text, a forgotten `====` does the same
// to the old text, and both are written or matched verbatim. There is no
// legitimate plain-text edit carrying one — this repository's own README
// documents the format and does it inside a fenced block, which is still a bare
// marker line, so the refusal names the JSON batch as the way to write one.
func noStrayMarker(lines []string, from, to int, file, half string) error {
	// No `i < len(lines)` bound: `collectUntil` returns the index AFTER a line
	// it matched, so `to` is inside the slice by construction. It was there, no
	// mutation could make it fail, and this project deletes a guard nothing can
	// prove rather than shipping it.
	for i := from; i < to; i++ {
		switch lines[i] {
		case "<<<<", ">>>>":
			// An OPENER or a TERMINATOR inside a half is unambiguous: the half
			// it belongs to was never closed. `<<<<` in a new half is the next
			// block swallowed whole; `>>>>` in an old half is this block's own
			// end read as text.
			return fmt.Errorf("%s: line %d is a bare %q inside the %s half of a block — the %q above it was never closed, so the block that follows was swallowed into this one and would be written verbatim, not reported; add the missing %q",
				file, i+1, lines[i], half, openerOf(half), terminatorOf(half))
		case "====":
			// AMBIGUOUS, and the refusal says so rather than asserting a
			// forgotten terminator: a `====` in a new half is either the
			// separator of a block whose `>>>>` is missing, or content that
			// genuinely carries a markdown setext underline. Both are refused,
			// and the second is refused on its own ground — a bare marker line
			// written INTO an artifact makes every later text batch over that
			// file mis-split. This repository's own README carries all three
			// markers in its format section and is therefore JSON-only, which
			// is the price and is documented.
			return fmt.Errorf("%s: line %d is a bare %q inside the %s half of a block; either the %q above it is missing its %q, or the text genuinely carries that line — both are refused, because a marker written into an artifact mis-splits every later batch over that file. Use the JSON batch format for text that must carry one",
				file, i+1, lines[i], half, openerOf(half), terminatorOf(half))
		}
	}
	return nil
}

func terminatorOf(half string) string {
	if half == "old" {
		return "===="
	}
	return ">>>>"
}

func openerOf(half string) string {
	if half == "old" {
		return "<<<<"
	}
	return "===="
}

// refuseBracketedFields catches a header written with the manual's optionality
// brackets around a real field. Only the part of the line BEFORE `note=` is
// scanned: a note swallows the rest of the line and is free text, so a note
// that quotes `[count=2]` is prose about a batch, not a malformed header.
func refuseBracketedFields(rest string, line int) error {
	head := rest
	if k := strings.Index(rest, "note="); k >= 0 {
		if k > 0 && rest[k-1] == '[' {
			return bracketError(line, "note=...")
		}
		head = rest[:k]
	}
	if strings.Contains(head, "[count=") {
		return bracketError(line, "count=N")
	}
	return nil
}

func bracketError(line int, field string) error {
	return fmt.Errorf("line %d: header field %s is written inside square brackets. The manual writes `=== <file> [count=N] [note=...]` and there the brackets mark the field OPTIONAL — they are not part of the syntax, unlike `<<<<`, `====` and `>>>>`, which are literal. Drop them: `=== artifacts/doc.md count=2 note=why`", line, field)
}
