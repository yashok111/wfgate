package jstext

import (
	"strings"
	"testing"
)

// A regex opening after a KEYWORD was read as division, so the quote inside its
// character class opened a string literal that ran to the end of the line and
// BLANKED the code after it. The `agent(` there vanished from the backstop's
// population — the one direction a backstop cannot report — and the blanked
// bytes were handed to the prose reader as a sentence nobody wrote.
func TestStripNonCode_regexAfterAKeywordIsARegex(t *testing.T) {
	for _, src := range []string{
		`const q = (s) => { return /['"]/.test(s) && agent(s, o) }`,
		`switch (k) { case /['"]/.source: agent(k, o) }`,
		`const x = typeof /['"]/ === 'object' && agent(k, o)`,
		`if (!(k in /['"]/)) { agent(k, o) }`,
		`throw /['"]/.test(k) ? agent(k, o) : null`,
	} {
		got := StripNonCode(src)
		if !strings.Contains(got, "agent(") {
			t.Errorf("the code after a keyword-position regex was blanked:\n  in:  %s\n  out: %s", src, got)
		}
		if len(got) != len(src) {
			t.Errorf("StripNonCode must preserve length: %d != %d", len(got), len(src))
		}
	}
}

// The other direction, and it is the reason the fix is a keyword LIST rather
// than "any word": after a value a `/` is division, and reading it as a regex
// would blank real code — `total / count` and a variable whose name merely ends
// in a keyword.
func TestStripNonCode_divisionAfterAValueIsStillDivision(t *testing.T) {
	for _, src := range []string{
		`const r = total / count; agent(r, o)`,
		`const r = noreturn / count; agent(r, o)`,
		`const r = x[0] / y; agent(r, o)`,
	} {
		if got := StripNonCode(src); got != src {
			t.Errorf("a division was read as a regex:\n  in:  %s\n  out: %s", src, got)
		}
	}
}

// And the prose reader must not be handed the blanked remains either: a regex
// body is words to nobody, and the code after one is not a sentence.
func TestProseSpans_aKeywordPositionRegexContributesNoProse(t *testing.T) {
	const src = `const q = (s) => { return /['"]/.test(s) && agent(s, 'the real prose') }`
	var got []string
	for _, sp := range ProseSpans(src) {
		got = append(got, src[sp.From:sp.To])
	}
	if len(got) != 1 || got[0] != "the real prose" {
		t.Fatalf("want exactly the one string literal as prose, got %q", got)
	}
}

// The walk-back reads the ORIGINAL source, where a `//` comment's text is still
// there, and a newline is a space byte. So without the "the last significant
// byte must be part of a word" gate the skip runs past the line break into the
// comment above: after `// the ratio we return`, the `/` opening the next line
// found the keyword `return` and opened a REGEX, blanking every byte to the
// closing slash — a division read as a regex, which is the under-count
// direction a backstop cannot report.
func TestStripNonCode_aKeywordInACommentDoesNotReachTheNextLine(t *testing.T) {
	const src = "const n = f() // the ratio we return\n/ 2 && agent(p, o) / 3\n"
	got := StripNonCode(src)
	if !strings.Contains(got, "agent(") {
		t.Fatalf("the division after a comment was read as a regex:\n%s", got)
	}
	if len(got) != len(src) {
		t.Fatalf("StripNonCode must preserve length: %d != %d", len(got), len(src))
	}
}
