package wfdry

import (
	"strings"
	"testing"
)

// A boolean whose NAME carries a failure word but which IS a pass flag —
// `noFailures`, `errorFree`; failVerdicts' own comment names both, and that
// guard is anchored precisely so it does not convict them. The fixture
// synthesiser matched the substring instead, returned FALSE in `clean`, the
// script derived `green: false` from it, and the clean-mode verdict guard
// reported that as "no fixture in this mode can produce that verdict": a red run
// over a correct script, with no note naming the field.
func TestCleanMode_aNegatedFailureNameIsAPassFlag(t *testing.T) {
	for _, field := range []string{"noFailures", "errorFree", "nonBlocking", "defectFree"} {
		body := "export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }\n" +
			"const S = { type: 'object', properties: { " + field + ": { type: 'boolean' } } }\n" +
			"const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }\n" +
			"phase('P')\n" +
			"const r = await spawn(`go`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })\n" +
			"return { green: !!(r && r." + field + ") }"
		r := run1(t, body, "clean")
		if r.Err != nil {
			t.Fatalf("%s: the fixture must run: %v", field, r.Err)
		}
		for _, v := range r.Violations {
			if strings.Contains(v, "green: false") {
				t.Errorf("%s: synthesised FALSE in clean, so the guard fired on a correct script: %s", field, v)
			}
		}
		// And the decision is NAMED. The guard's own message could not say which
		// field produced the verdict, which is the half of this defect that
		// survives every renaming.
		if !hasNote(r, field) {
			t.Errorf("%s: no note names the boolean this mode valued from its name:\n%v", field, r.Notes)
		}
	}
}

// The other direction, unchanged: a plain failure word is still false in a
// passing mode, and the note names it there too — that is the population the
// clean-mode guard could not point at.
func TestCleanMode_aPlainFailureNameIsStillFalse(t *testing.T) {
	body := "export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }\n" +
		"const S = { type: 'object', properties: { hasBlockers: { type: 'boolean' } } }\n" +
		"const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }\n" +
		"phase('P')\n" +
		"const r = await spawn(`go`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })\n" +
		"return { green: !(r && r.hasBlockers) }"
	r := run1(t, body, "clean")
	if r.Err != nil {
		t.Fatalf("the fixture must run: %v", r.Err)
	}
	if len(r.Violations) > 0 {
		t.Fatalf("a failure-named boolean must be false in clean: %v", r.Violations)
	}
	if !hasNote(r, "hasBlockers") {
		t.Fatalf("the note must name it, so the verdict can be traced to the field:\n%v", r.Notes)
	}
}

// The negator is matched as a WHOLE token. Under the prefix rule `hasWord` uses
// — which exists so a plural finds its singular — `noticedErrors` matched `not`
// and a failure flag became a pass flag, which is the inversion this file exists
// to refuse.
func TestBoolNegated_matchesWholeTokensOnly(t *testing.T) {
	for _, yes := range []string{"noFailures", "errorFree", "notBlocked", "nonBlocking", "neverStale", "failureLess"} {
		if !boolNegated(yes) {
			t.Errorf("%q carries a negator and was not read as one", yes)
		}
	}
	for _, no := range []string{"noticedErrors", "nonceFailure", "blockers", "freestyleErrors", "hasErrors"} {
		if boolNegated(no) {
			t.Errorf("%q carries no negator and was read as one", no)
		}
	}
}

func hasNote(r Result, substr string) bool {
	for _, n := range r.Notes {
		if strings.Contains(n, substr) {
			return true
		}
	}
	return false
}
