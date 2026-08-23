package wfcheck

// The textual backstop of the inventory check is the one place this package
// still reads characters instead of the parsed program, and it exists precisely
// because the parser might be wrong. That makes its input worth preparing: a
// call is CODE, and a `agent(` inside a comment or a string is neither a call
// nor evidence that the parser missed one.
//
// Measured on P2f, where all three shapes fired on real scripts:
//   - `// one agent() call, no loop, no fan-out`        — a comment
//   - `log('… ' + n + '/2 agent(s) came back')`         — a string
//   - `async function spawn(prompt, opts)`              — the declaration
//
// The first two are handled here, the third by the caller (a wrapper's
// declaration line is not one of its call sites). All three produced the same
// output: an ERROR nobody could clear, on a script the checker had read
// correctly.

// stripNonCode blanks the text of comments and string literals, preserving
// length and line structure so an offset still maps to its line. Template
// literals keep their `${...}` interpolations — those ARE code, and a spawn call
// written inside one must stay counted.
//
// Regex literals are read too, because leaving them out was wrong in the one
// direction a backstop cannot report: `/['"]/` — the ordinary way to match a
// quote — opened a string literal that ran to the end of its line and BLANKED
// the code after it, so a call there vanished from the population and the check
// went quiet. A `/` opens a regex only where a value cannot precede it, which is
// decided by the last significant character; anywhere else it is division.
func stripNonCode(src string) string {
	out := []byte(src)
	scanCode(src, out, 0, false)
	return string(out)
}

// blank overwrites a span with spaces, leaving newlines so an offset still maps
// to the line it was on.
func blank(out []byte, i, j int) {
	for k := i; k < j && k < len(out); k++ {
		if out[k] != '\n' {
			out[k] = ' '
		}
	}
}

// scanCode blanks comment and literal text from i onward and returns where it
// stopped. With inInterp set it stops just past the `}` that closes the
// `${...}` it was called for — so a brace inside a string inside an
// interpolation (`${ f('}') }`) does not end the interpolation early, which
// would hand the rest of the template back to the scanner as code and count its
// prose.
func scanCode(src string, out []byte, i int, inInterp bool) int {
	depth := 0
	prev := byte(0) // the last significant character, for the regex/division call
	for i < len(src) {
		c := src[i]
		switch {
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '/':
			j := i
			for j < len(src) && src[j] != '\n' {
				j++
			}
			blank(out, i, j)
			i = j
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '*':
			j := i + 2
			for j+1 < len(src) && (src[j] != '*' || src[j+1] != '/') {
				j++
			}
			j = min(j+2, len(src))
			blank(out, i, j)
			i = j
		case src[i] == '/' && regexCanStart(prev):
			i = blankRegex(src, out, i)
		case src[i] == '\'' || src[i] == '"':
			i = blankQuoted(src, out, i)
		case src[i] == '`':
			i = blankTemplate(src, out, i)
		case inInterp && src[i] == '{':
			depth++
			i++
		case inInterp && src[i] == '}':
			if depth == 0 {
				return i + 1
			}
			depth--
			i++
		default:
			i++
		}
		if !isSpaceByte(c) {
			prev = c
		}
	}
	return i
}

// regexCanStart reports a position where a `/` cannot be division, because no
// value can precede it. Everything else — an identifier, a literal, a closing
// bracket — makes it an operator, so a regex opening a STATEMENT (`return /x/`,
// or one on its own line after a call) is read as division and its body is
// scanned as code. That is deliberate: it can only over-count, and this check
// already has to explain a population that is too large.
func regexCanStart(prev byte) bool {
	switch prev {
	case 0, '(', ',', '=', ':', '[', '!', '&', '|', '?', '{', '}', ';', '+', '-', '*', '%', '<', '>', '~', '^':
		return true
	}
	return false
}

// blankRegex blanks one regex literal's body and returns the offset just past
// it. A `/` inside a character class does not close it, and a newline ends the
// scan: an unterminated regex is a parse error, not a licence to eat the file.
func blankRegex(src string, out []byte, i int) int {
	j := i + 1
	class := false
	for j < len(src) && src[j] != '\n' {
		switch {
		case src[j] == '\\':
			j += 2
			continue
		case src[j] == '[':
			class = true
		case src[j] == ']':
			class = false
		case src[j] == '/' && !class:
			blank(out, i+1, j)
			return j + 1
		}
		j++
	}
	blank(out, i+1, min(j, len(src)))
	return j
}

func isSpaceByte(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

// blankQuoted blanks one single- or double-quoted literal and returns the offset
// just past it. The bound is on the LOOP, not inside it: an escape at the end of
// the input would otherwise index one past it. A newline ends the scan too — an
// unterminated literal is not a licence to eat the rest of the file.
func blankQuoted(src string, out []byte, i int) int {
	q := src[i]
	j := i + 1
	for j < len(src) && src[j] != q && src[j] != '\n' {
		if src[j] == '\\' {
			j++
		}
		j++
	}
	j = min(j+1, len(src))
	blank(out, i+1, j-1)
	return j
}

// blankTemplate blanks the literal chunks of one template literal and returns
// the offset just past its closing backtick, having handed every `${...}`
// interpolation back to scanCode as the code it is.
func blankTemplate(src string, out []byte, i int) int {
	j := i + 1
	chunk := j
	for j < len(src) {
		switch {
		case src[j] == '\\':
			j += 2
		case src[j] == '`':
			blank(out, chunk, j)
			return j + 1
		case src[j] == '$' && j+1 < len(src) && src[j+1] == '{':
			blank(out, chunk, j)
			j = scanCode(src, out, j+2, true)
			chunk = j
		default:
			j++
		}
	}
	blank(out, chunk, min(j, len(src)))
	return j
}

// lineAt returns the 1-based line an offset falls on.
func lineAt(s string, off int) int {
	line := 1
	for i := 0; i < off && i < len(s); i++ {
		if s[i] == '\n' {
			line++
		}
	}
	return line
}
