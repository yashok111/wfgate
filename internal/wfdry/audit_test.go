package wfdry

import (
	"strings"
	"testing"

	wfgate "git.sumka.site/yakov/wfgate"
	"git.sumka.site/yakov/wfgate/internal/ws"
)

func auditSkeleton(t *testing.T) string {
	t.Helper()
	b, err := wfgate.Templates.ReadFile("templates/audit.mjs")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAuditSkeletonAllModes(t *testing.T) {
	for _, r := range runTyped(t, auditSkeleton(t), ws.TypeAudit) {
		if r.Err != nil || len(r.Violations) > 0 {
			t.Errorf("%s: err=%v violations=%v", r.Mode, r.Err, r.Violations)
		}
	}
}

// `clean` finds claims, verifies them and WRITES the answer; `empty` covers
// its ground and answers nothing, which an audit reports apart from coverage.
func TestAuditCleanAndEmpty(t *testing.T) {
	rs := runTyped(t, auditSkeleton(t), ws.TypeAudit, "clean", "empty")
	c := byMode(rs, "clean")
	if strings.Join(c.Phases, " ") != "Find Verify Write" {
		t.Fatalf("clean must reach find → verify → write: %v", c.Phases)
	}
	if retOf(c)["answered"] != true || retOf(c)["complete"] != true {
		t.Fatalf("clean answers: %+v", retOf(c))
	}
	e := retOf(byMode(rs, "empty"))
	if e["answered"] != false || e["complete"] != true {
		t.Fatalf("empty covered everything and answered nothing: %+v", e)
	}
	if !strings.Contains(e["report"].(string), "UNANSWERED") {
		t.Fatalf("an unanswered audit must say so")
	}
}

// `dead-writer` kills only the writer, and an audit that calls itself complete
// without its answer is refused.
func TestAuditDeadWriter(t *testing.T) {
	body := mutate(t, auditSkeleton(t), "unverified.length === 0 && !writerDied", "unverified.length === 0")
	r := byMode(runTyped(t, body, ws.TypeAudit, deadWriterMode), deadWriterMode)
	wantViolation(t, r, "dead-writer: the run returned `complete: true`")
	if strings.Join(r.Killed, " ") != "write:answer" {
		t.Fatalf("dead-writer must kill the writer alone: %v", r.Killed)
	}
	if strings.Contains(strings.Join(ModesFor(ws.TypeReview), " "), deadWriterMode) || !strings.Contains(strings.Join(ModesFor(ws.TypeAudit), " "), deadWriterMode) {
		t.Fatalf("dead-writer is an audit's mode alone")
	}
}

// A writer the runner cannot identify leaves the missing-answer path to no
// mode, and that is a violation.
func TestAuditDeadWriterThatKilledNobody(t *testing.T) {
	body := mutate(t, auditSkeleton(t), "label: 'write:answer'", "label: 'summary:answer'")
	wantViolation(t, byMode(runTyped(t, body, ws.TypeAudit, deadWriterMode), deadWriterMode), "killed nobody")
}

func TestClaimsIsADeliverable(t *testing.T) {
	if !deliverableRe.MatchString("claims") || deliverableRe.MatchString("unsupportedclaims") {
		t.Fatalf("`claims` is an audit's deliverable, by its whole name")
	}
}

// Only VERIFIED claims reach the writer: in `blockers` every checker rejects,
// so no writer is spawned and the report says the question went unanswered.
func TestAuditWriterSeesVerifiedOnly(t *testing.T) {
	r := byMode(runTyped(t, auditSkeleton(t), ws.TypeAudit, "blockers"), "blockers")
	if strings.Contains(strings.Join(r.Calls, " "), "write:") {
		t.Fatalf("no claim was verified, so no writer may run: %v", r.Calls)
	}
	if !strings.Contains(retOf(r)["report"].(string), "UNANSWERED") {
		t.Fatalf("the report must say the question went unanswered")
	}
}

// The runner refuses an audit whose writer runs with no verified claim before
// it — the template's own guard is not the only one.
func TestAuditWriterWithNothingVerifiedIsRefused(t *testing.T) {
	body := mutate(t, auditSkeleton(t), "if (verified.length) {\n  phase('Write')", "if (found.length) {\n  phase('Write')")
	wantViolation(t, byMode(runTyped(t, body, ws.TypeAudit, "blockers"), "blockers"), "ran though no agent before it returned `verified: true`")
}

// `answered: true` over a dead writer is refused whatever else the verdict says.
func TestAuditAnsweredOverADeadWriter(t *testing.T) {
	body := mutate(t, auditSkeleton(t), "const answered = sentences.length > 0 && uncited === 0", "const answered = true")
	wantViolation(t, byMode(runTyped(t, body, ws.TypeAudit, deadWriterMode), deadWriterMode), "returned `answered: true` while its writer returned NOTHING")
}

// A verdict whose booleans the guard cannot READ is said to be unmeasured even
// when booleans exist.
func TestUnreadableVerdictIsUnmeasured(t *testing.T) {
	body := preamble + `phase('P')
const a = await spawn('one', { model: 'sonnet', effort: 'low', label: 'reviewer', schema: S, phase: 'P' })
return { covered: !!a, answered: true }`
	r := run1(t, body, "dead-reviews")
	if !strings.Contains(strings.Join(r.Notes, "\n"), "UNMEASURED") {
		t.Fatalf("two booleans, neither readable, must read as unmeasured: %v", r.Notes)
	}
}

// The writer is the label's ROLE: a unit named `compose` does not make its
// finders writers.
func TestWriterIsTheFirstSegment(t *testing.T) {
	if !writerish("write:answer") || writerish("find:part:compose") || writerish("find:write-path:u") {
		t.Fatalf("writerish must read the first segment only")
	}
}

// `examples` gives a string field a valid-looking value, item by item, and a
// scalar array's items follow their own type.
func TestExamplesAndScalarItems(t *testing.T) {
	ex := map[string]any{"type": "string", "examples": []any{"pkg/a.go", "pkg/b.go"}}
	if value("file", ex, "clean", "f", true, 0, 1, nil) != "pkg/b.go" {
		t.Fatalf("item 1 takes the second example")
	}
	arr := map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}
	got := itemsOf("uses", arr, "clean", "w", true, 0, nil, 2)
	if got[0] != 1 || got[1] != 2 {
		t.Fatalf("integer items must be integers: %v", got)
	}
}

// Claims of both polarities in `clean`, every part unsettled when nothing is
// verified, and agent free text kept out of `citable`.
func TestAuditPolarityUnsettledAndCitable(t *testing.T) {
	rs := runTyped(t, auditSkeleton(t), ws.TypeAudit, "clean", "blockers")
	kinds := map[string]bool{}
	for _, v := range retOf(byMode(rs, "clean"))["verified"].([]any) {
		kinds[v.(map[string]any)["kind"].(string)] = true
	}
	if !kinds["holds"] || !kinds["violated"] {
		t.Fatalf("clean must verify claims of BOTH polarities: %v", kinds)
	}
	// …and `kind` reads as a real pass/fail pair, not a first-member fallback
	// the runner apologises for in a note.
	if n := strings.Join(byMode(rs, "clean").Notes, "\n"); strings.Contains(n, "kind (enum") {
		t.Fatalf("`kind` must read as pass/fail vocabulary: %s", n)
	}
	b := retOf(byMode(rs, "blockers"))
	if u, _ := b["unsettled"].([]any); len(u) != 2 {
		t.Fatalf("with nothing verified, every part is unsettled: %v", b["unsettled"])
	}
	if !strings.Contains(b["report"].(string), "## Not in the answer") || strings.Contains(b["citable"].(string), "## Not in the answer") {
		t.Fatalf("the unsupported claims' free text belongs in report, never in citable")
	}
}

// A claim whose citation fields fail the shape check is MALFORMED: never
// verified, listed apart. Walked by giving the anchor one-letter examples — a
// one-letter anchor matches almost any line, so the shape check refuses it.
func TestAuditMalformedCitationsAreDropped(t *testing.T) {
	body := mutate(t, auditSkeleton(t), "examples: ['Save', 'Load']", "examples: ['S', 'L']")
	r := byMode(runTyped(t, body, ws.TypeAudit, "clean"), "clean")
	if strings.Join(r.Phases, " ") != "Find" {
		t.Fatalf("every claim is malformed, so nothing is verified: %v", r.Phases)
	}
	if !strings.Contains(retOf(r)["report"].(string), "MALFORMED CITATION") {
		t.Fatalf("malformed claims must be listed")
	}
}
