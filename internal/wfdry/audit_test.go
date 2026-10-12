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
