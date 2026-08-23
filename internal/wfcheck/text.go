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
// literals keep their `${...}` interpolations — those ARE code, and a spawn
// call written inside one must stay counted.
//
// It is a scanner, not a lexer: a regex literal containing `agent(` would still
// be counted. That is the safe direction for a backstop — it can only ever make
// the textual population LARGER than the truth, which the caller's own
// subtractions then have to explain, and this check's whole job is to be
// suspicious of a population that is too small.
func stripNonCode(src string) string {
	out := []byte(src)
	blank := func(i, j int) {
		for k := i; k < j && k < len(out); k++ {
			if out[k] != '\n' {
				out[k] = ' '
			}
		}
	}
	// tmpl tracks the template literals whose interpolations we are inside, so
	// the closing brace of `${...}` returns to the template rather than to code.
	var tmpl []int
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			j := i
			for j < len(src) && src[j] != '\n' {
				j++
			}
			blank(i, j)
			i = j
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			j := i + 2
			for j+1 < len(src) && (src[j] != '*' || src[j+1] != '/') {
				j++
			}
			j = min(j+2, len(src))
			blank(i, j)
			i = j
		case c == '\'' || c == '"':
			j := i + 1
			for j < len(src) && src[j] != c {
				if src[j] == '\\' {
					j++
				}
				if src[j] == '\n' { // an unterminated literal is not a licence to eat the file
					break
				}
				j++
			}
			j = min(j+1, len(src))
			blank(i+1, j-1)
			i = j
		case c == '`':
			i = blankTemplate(src, out, i, blank)
		case c == '{' && len(tmpl) > 0:
			tmpl[len(tmpl)-1]++
			i++
		case c == '}' && len(tmpl) > 0:
			i++
		default:
			i++
		}
	}
	return string(out)
}

// blankTemplate blanks the literal chunks of one template literal and returns
// the offset just past its closing backtick, having left every `${...}`
// interpolation untouched. Nested templates inside an interpolation recurse.
func blankTemplate(src string, out []byte, i int, blank func(int, int)) int {
	j := i + 1
	chunk := j
	for j < len(src) {
		switch {
		case src[j] == '\\':
			j += 2
		case src[j] == '`':
			blank(chunk, j)
			return j + 1
		case src[j] == '$' && j+1 < len(src) && src[j+1] == '{':
			blank(chunk, j)
			depth := 1
			j += 2
			for j < len(src) && depth > 0 {
				switch src[j] {
				case '{':
					depth++
					j++
				case '}':
					depth--
					j++
				case '`':
					j = blankTemplate(src, out, j, blank)
				default:
					j++
				}
			}
			chunk = j
		default:
			j++
		}
	}
	blank(chunk, j)
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
