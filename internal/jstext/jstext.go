// Package jstext is the ONE character-level reader of JavaScript in this
// repository, and it exists because two units need the same split from
// opposite sides.
//
// `wfcheck` needs the CODE: its textual backstop counts `agent(` call sites,
// and a call written inside a comment or a string is neither a call nor
// evidence that the parser missed one (measured on P2f, all three shapes on
// real scripts — a comment, a log string, and the wrapper's own declaration).
//
// `check` needs the PROSE: a gate's orchestration script is an artifact like
// any other, its prompts restate the decisions of the document beside it, and
// `wf check rules` must compare those restatements. Measured on mocker A3: six
// of round 10's twelve blockers were one decision that moved in the document
// and did not move in the prompt restating it. Without this split the collector
// read the whole script as prose and made a rule site out of joined code —
// `const A = { owns: ['internal/api'] } const P = "The two routes…` was
// reported, and compared, as a statement of a rule.
//
// One scanner, two polarities, and the polarity is TWO CALLBACKS rather than a
// flag: every span says for itself what the code reader must blank and what the
// prose reader must keep, and the two bounds are not the same span. A comment's
// `//` is blanked by one and is not prose to the other; a regex body is blanked
// by one and is words to neither. A second copy of this scanner would be this
// project's most-repeated defect class committed inside the tool that reports
// it.
package jstext

// StripNonCode blanks the text of comments and string literals, preserving
// length and line structure so an offset still maps to its line. Template
// literals keep their `${...}` interpolations — those ARE code, and a spawn
// call written inside one must stay counted.
//
// Regex literals are read too, because leaving them out was wrong in the one
// direction a backstop cannot report: `/['"]/` — the ordinary way to match a
// quote — opened a string literal that ran to the end of its line and BLANKED
// the code after it, so a call there vanished from the population and the check
// went quiet. A `/` opens a regex only where a value cannot precede it, which is
// decided by the last significant character; anywhere else it is division.
func StripNonCode(src string) string {
	out := []byte(src)
	scan(src, func(i, j int) { blank(out, i, j) }, nil, 0, false)
	return string(out)
}

// Span is one run of text a human wrote as WORDS: a comment body, or the
// literal chunk of a string or template. From and To are byte offsets into the
// source; Join says this span CONTINUES the text of the one before it.
//
// Join is the half a caller cannot recover on its own, and it is why this
// returns spans rather than a blanked-out copy of the file. Two adjacent string
// literals are two different texts and must never be read as one sentence —
// blanking the code between them left `const A = { owns: ['internal/api'],
// vector: 'reviewer' }` and the prompt on the line below it joined into a
// single "statement", which is the mocker A3 defect one layer down. But a
// template chunk after a `${...}` hole, and the next line of a `//` comment
// block, ARE continuations, and splitting those would cut sentences in half.
type Span struct {
	From, To int
	Join     bool
}

// ProseSpans returns every prose span of a JavaScript source, in order.
//
// A `${...}` interpolation is CODE: it is not prose, and the chunks around it
// are joined across the hole rather than spliced with the identifier. A regex
// body is words to nobody and never appears here.
func ProseSpans(src string) []Span {
	var out []Span
	var kinds []int // spanKind of each emitted span, for the join decision
	scan(src, nil, func(i, j, kind int) {
		join := false
		switch {
		case kind == spanTemplateCont:
			join = true
		case kind == spanLineComment && len(out) > 0 && kinds[len(kinds)-1] == spanLineComment:
			// A `//` block wraps its prose across lines exactly as a markdown
			// paragraph does, and unwrapping it is what the caller's splitter
			// exists for. Only whitespace and the next opener may sit between.
			join = onlyCommentGap(src[out[len(out)-1].To:i])
		}
		out = append(out, Span{From: i, To: j, Join: join})
		kinds = append(kinds, kind)
	}, 0, false)
	return out
}

// onlyCommentGap reports a gap between two line comments that holds nothing but
// the newline, the indent and the next `//`.
func onlyCommentGap(gap string) bool {
	for i := 0; i < len(gap); i++ {
		switch {
		case isSpaceByte(gap[i]):
		case gap[i] == '/':
		default:
			return false
		}
	}
	return true
}

// The span kinds the join decision needs to tell apart. They are not exported:
// a caller reads Join, which is the question it actually has.
const (
	spanQuoted = iota
	spanTemplate
	spanTemplateCont
	spanLineComment
	spanBlockComment
)

// blank overwrites a span with spaces, leaving newlines so an offset still maps
// to the line it was on.
func blank(out []byte, i, j int) {
	for k := i; k < j && k < len(out); k++ {
		if out[k] != '\n' {
			out[k] = ' '
		}
	}
}

func call(fn func(i, j int), i, j int) {
	if fn != nil && j > i {
		fn(i, j)
	}
}

func callProse(fn func(i, j, kind int), i, j, kind int) {
	if fn != nil && j > i {
		fn(i, j, kind)
	}
}

// scan walks JavaScript from i onward, reporting each span twice over: to
// onBlank with the bounds the CODE reader must erase, and to onProse with the
// bounds the PROSE reader must keep. Either may be nil.
//
// With inInterp set it stops just past the `}` that closes the `${...}` it was
// called for — so a brace inside a string inside an interpolation
// (`${ f('}') }`) does not end the interpolation early, which would hand the
// rest of the template back to the scanner as code and count its prose.
func scan(src string, onBlank func(i, j int), onProse func(i, j, kind int), i int, inInterp bool) int {
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
			call(onBlank, i, j)
			// The `//` is not prose. Two characters normalise away on their
			// own, but they would open every comment line of a kept sentence.
			callProse(onProse, min(i+2, j), j, spanLineComment)
			i = j
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '*':
			j := i + 2
			for j+1 < len(src) && (src[j] != '*' || src[j+1] != '/') {
				j++
			}
			end := min(j+2, len(src))
			call(onBlank, i, end)
			callProse(onProse, min(i+2, len(src)), min(j, len(src)), spanBlockComment)
			i = end
		case src[i] == '/' && regexCanStart(prev):
			i = scanRegex(src, onBlank, i)
		case src[i] == '\'' || src[i] == '"':
			i = scanQuoted(src, onBlank, onProse, i)
		case src[i] == '`':
			i = scanTemplate(src, onBlank, onProse, i)
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
// scanned as code. That is deliberate: it can only over-count, and the check
// that reads the code already has to explain a population that is too large.
func regexCanStart(prev byte) bool {
	switch prev {
	case 0, '(', ',', '=', ':', '[', '!', '&', '|', '?', '{', '}', ';', '+', '-', '*', '%', '<', '>', '~', '^':
		return true
	}
	return false
}

// scanRegex walks one regex literal and returns the offset just past it. A `/`
// inside a character class does not close it, and a newline ends the scan: an
// unterminated regex is a parse error, not a licence to eat the file. Its body
// is reported to the code reader only — a character class is not a sentence.
func scanRegex(src string, onBlank func(i, j int), i int) int {
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
			call(onBlank, i+1, j)
			return j + 1
		}
		j++
	}
	call(onBlank, i+1, min(j, len(src)))
	return j
}

func isSpaceByte(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

// scanQuoted walks one single- or double-quoted literal and returns the offset
// just past it. The bound is on the LOOP, not inside it: an escape at the end of
// the input would otherwise index one past it. A newline ends the scan too — an
// unterminated literal is not a licence to eat the rest of the file.
func scanQuoted(src string, onBlank func(i, j int), onProse func(i, j, kind int), i int) int {
	q := src[i]
	j := i + 1
	for j < len(src) && src[j] != q && src[j] != '\n' {
		if src[j] == '\\' {
			j++
		}
		j++
	}
	j = min(j+1, len(src))
	call(onBlank, i+1, j-1)
	callProse(onProse, i+1, j-1, spanQuoted)
	return j
}

// scanTemplate walks one template literal, reporting its literal chunks and
// handing every `${...}` interpolation back to scan as the code it is.
func scanTemplate(src string, onBlank func(i, j int), onProse func(i, j, kind int), i int) int {
	j := i + 1
	chunk := j
	kind := spanTemplate
	emit := func(from, to int) {
		call(onBlank, from, to)
		callProse(onProse, from, to, kind)
		if to > from {
			// Every chunk after the first continues the SAME literal across a
			// `${...}` hole, and a sentence with an interpolation in it is one
			// sentence. The flip is on an EMITTED chunk only: a template
			// opening with `${...}` emits nothing first, and marking the chunk
			// after it as a continuation would join it to whatever span the
			// scanner happened to report before this literal began.
			kind = spanTemplateCont
		}
	}
	for j < len(src) {
		switch {
		case src[j] == '\\':
			j += 2
		case src[j] == '`':
			emit(chunk, j)
			return j + 1
		case src[j] == '$' && j+1 < len(src) && src[j+1] == '{':
			emit(chunk, j)
			// The interpolation is CODE, and a string literal inside it is an
			// argument rather than prose: `${cfg.owns.join(', ')}` would
			// otherwise splice ", " into the sentence and make the chunk after
			// the hole a continuation of THAT. Its blanking still happens —
			// the code reader must not find a call hiding in it.
			j = scan(src, onBlank, nil, j+2, true)
			chunk = j
		default:
			j++
		}
	}
	emit(chunk, min(j, len(src)))
	return j
}

// LineAt returns the 1-based line an offset falls on.
func LineAt(s string, off int) int {
	line := 1
	for i := 0; i < off && i < len(s); i++ {
		if s[i] == '\n' {
			line++
		}
	}
	return line
}
