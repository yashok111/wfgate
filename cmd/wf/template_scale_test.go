// The template is where the drift STARTED. `['blocker','advisory']` shipped
// from here into three gated scripts (a1, a2, a3), and the wave that closed the
// scale enforced it on the ledger side — `wf round record` refuses an off-scale
// word — while leaving this side pinned by nothing: reverting the enum passed
// every bar, which is how it got out the first time.
package main

import (
	"regexp"
	"strings"
	"testing"

	wfgate "github.com/yashok111/wfgate"
	"github.com/yashok111/wfgate/internal/ws"
)

var severityEnumRe = regexp.MustCompile(`severity: \{ type: 'string', enum: \[([^\]]*)\]`)

func TestTemplate_severityEnumsAreTheClosedScale(t *testing.T) {
	b, err := wfgate.Templates.ReadFile("templates/script.mjs")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.ToLower(strings.Join(ws.Levels, ","))

	found := severityEnumRe.FindAllStringSubmatch(string(b), -1)
	if len(found) == 0 {
		t.Fatal("no `severity:` enum in the template — the emitted skeleton stopped declaring one, so a reviewer's level is whatever it invents")
	}
	// BOTH of them: the previous wave widened one schema and left the
	// acceptance schema on the old two-word vocabulary for a whole round.
	if len(found) != 2 {
		t.Fatalf("%d severity enum(s) in the template, want 2 (the review schema and the acceptance schema)", len(found))
	}
	for _, m := range found {
		got := strings.NewReplacer("'", "", " ", "").Replace(m[1])
		if got != want {
			t.Errorf("template severity enum is [%s], want [%s] — the ledger side is enforced by `wf round record` and this side by this test; a scale that holds on one path and not the other is not closed",
				got, want)
		}
	}
}
