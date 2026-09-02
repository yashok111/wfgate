package wfdry

import (
	"strings"
	"testing"
)

// `export const meta =` with the brace on the NEXT line. The pass that blanks
// the keyword walked back from the INITIALISER's own line, which is line 2, so
// `export` survived on line 1 and goja died with a bare `SyntaxError: Line 1:19
// Unexpected reserved word`: 0/1 modes, no diagnosis, over a file wfcheck reads
// clean. It walks back from the DECLARED NAME now, which is on the keyword's own
// line by construction.
func TestNeutraliseMeta_exportWithTheBraceOnTheNextLine(t *testing.T) {
	body := "export const meta =\n" +
		"{ name: 'x', description: 'd', phases: [{ title: 'P' }] }\n" +
		"const S = { type: 'object', properties: { blockers: { type: 'array' } } }\n" +
		"const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }\n" +
		"phase('P')\n" +
		"await spawn(`go`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })\n" +
		"return { done: true }"
	r := run1(t, body, "clean")
	if r.Err != nil {
		t.Fatalf("the script must run: %v", r.Err)
	}
	if r.Spawned != 1 {
		t.Fatalf("want the one spawn, got %d", r.Spawned)
	}
}

// And where the keyword genuinely cannot be reached, the runner names the LINE
// instead of handing over goja's message. An absent diagnosis in the register of
// a parse error is the same silence as a false green.
func TestNeutraliseMeta_namesASurvivingExport(t *testing.T) {
	body := "export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }\n" +
		"export function helper() { return 1 }\n" +
		"return { done: true }"
	_, err := neutraliseMeta(body, "s.mjs")
	if err == nil {
		t.Fatal("a surviving `export` was handed to the engine")
	}
	if !strings.Contains(err.Error(), "line 2") || !strings.Contains(err.Error(), "export") {
		t.Fatalf("the error must name the line and the keyword: %v", err)
	}
}

// A prompt line that merely READS like module syntax is not one: the scan runs
// over the code, which has its literals blanked in place.
func TestNeutraliseMeta_aTemplateLiteralIsNotASurvivingExport(t *testing.T) {
	body := "export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }\n" +
		"const P = `first:\nexport PATH=/usr/local/bin\nthen build.`\n" +
		"return { done: true, p: P }"
	if _, err := neutraliseMeta(body, "s.mjs"); err != nil {
		t.Fatalf("a prompt's own export line was read as module syntax: %v", err)
	}
}
