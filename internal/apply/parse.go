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
// unindented, nothing else on the line. Text that itself contains such a line
// (a setext underline of exactly four equals) will mis-split — and then fail
// the plan, because the mis-split old no longer matches the file:
// all-or-nothing turns the collision into a loud rejection instead of a silent
// wrong edit.
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
			return nil, fmt.Errorf("line %d: expected `=== <file>` header, got %q", i+1, line)
		}
		e := Edit{}
		rest := strings.TrimSpace(line[len("=== "):])
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
		old, next, err := collectUntil(lines, i+1, "====")
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.File, err)
		}
		e.Old = old
		nw, next, err := collectUntil(lines, next, ">>>>")
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.File, err)
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
