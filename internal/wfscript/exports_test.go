package wfscript

import (
	"strings"
	"testing"
)

// `export ` was blanked wherever it began a LINE, including inside a template
// literal. A prompt line reading `export PATH=/usr/local/bin` — an ordinary
// thing to hand an agent — became `       PATH=/usr/local/bin`, so every check
// reading the resolved prompt quoted a line the file does not contain, and
// wfcheck escalated a placeholder INFO to a WARN on the indent it had invented.
func TestBlankExports_leavesATemplateLiteralAlone(t *testing.T) {
	const src = "export const meta = { name: 'x', description: 'd' }\n" +
		"const P = `Set the path first:\n" +
		"export PATH=/usr/local/bin\n" +
		"then run the build.`\n" +
		"return { done: true }\n"
	s, err := LoadSource(src, "s.mjs")
	if err != nil {
		t.Fatal(err)
	}
	c, ok := s.Consts()["P"]
	if !ok || !c.IsStr {
		t.Fatalf("the prompt const did not resolve: %+v", c)
	}
	if !strings.Contains(c.Text, "export PATH=/usr/local/bin") {
		t.Fatalf("the prompt's own `export` line was blanked:\n%q", c.Text)
	}
	// And the same for a single-quoted literal, which the raw scan could reach
	// only at the start of the file — kept because a fixture that passes for the
	// wrong reason proves nothing.
	if strings.Contains(c.Text, "       PATH=") {
		t.Fatalf("the line was blanked in place:\n%q", c.Text)
	}
}

// The keyword itself must still go: goja parses scripts, not modules.
func TestBlankExports_stillBlanksARealExport(t *testing.T) {
	const src = "export const meta = { name: 'x', description: 'd' }\nreturn { done: true }\n"
	s, err := LoadSource(src, "s.mjs")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Consts()["meta"]; !ok {
		t.Fatal("the exported meta was not collected — the keyword survived the pre-pass")
	}
}
