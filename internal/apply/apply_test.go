package apply

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The measured failure this package exists for: a three-edit batch whose middle
// edit does not match must leave all three files exactly as they were. The two
// that WOULD have matched are the dangerous ones — under the old helper they
// were silently skipped and the operator, seeing one error, fixed one edit and
// believed the batch had landed.
func TestPlan_oneMissRejectsWholeBatch(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.md", "alpha\n")
	write(t, root, "b.md", "beta\n")
	write(t, root, "c.md", "gamma\n")

	edits := []Edit{
		{File: "a.md", Old: "alpha", New: "ALPHA"},
		{File: "b.md", Old: "NOT PRESENT", New: "x"},
		{File: "c.md", Old: "gamma", New: "GAMMA"},
	}
	results, bufs, err := Plan(root, edits)
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("want ErrRejected, got %v", err)
	}
	if bufs != nil {
		t.Fatal("a rejected batch must return no buffers")
	}
	if !results[0].OK || results[1].OK || !results[2].OK {
		t.Fatalf("per-edit verdicts wrong: %+v", results)
	}
	if results[1].Reason != "no match" {
		t.Fatalf("reason = %q", results[1].Reason)
	}
	// The point of the test: the two matching edits reached no file.
	if got := read(t, root, "a.md"); got != "alpha\n" {
		t.Fatalf("a.md was written despite rejection: %q", got)
	}
	if got := read(t, root, "c.md"); got != "gamma\n" {
		t.Fatalf("c.md was written despite rejection: %q", got)
	}
}

func TestPlan_countMismatchIsRejected(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.md", "rule\nrule\n")
	// The author believes the rule lives once. It lives twice — which is exactly
	// the "one copy left behind" shape — so the batch must stop, not fix one.
	_, _, err := Plan(root, []Edit{{File: "a.md", Old: "rule", New: "RULE"}})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("want rejection on count mismatch, got %v", err)
	}
	// Declaring the true count makes it succeed, and both copies move together.
	_, bufs, err := Plan(root, []Edit{{File: "a.md", Old: "rule", New: "RULE", Count: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(bufs["a.md"]); got != "RULE\nRULE\n" {
		t.Fatalf("got %q", got)
	}
}

func TestPlan_sameFileEditsSeeEachOther(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.md", "one two\n")
	_, bufs, err := Plan(root, []Edit{
		{File: "a.md", Old: "one", New: "three"},
		{File: "a.md", Old: "three two", New: "done"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(bufs["a.md"]); got != "done\n" {
		t.Fatalf("second edit did not see the first: %q", got)
	}
}

func TestPlan_noOpAndEmptyAreRejected(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.md", "x\n")
	for _, e := range []Edit{
		{File: "a.md", Old: "x", New: "x"},
		{File: "a.md", Old: "", New: "y"},
	} {
		if _, _, err := Plan(root, []Edit{e}); !errors.Is(err, ErrRejected) {
			t.Fatalf("edit %+v should be rejected, got %v", e, err)
		}
	}
}

func TestPlan_pathEscapeRefused(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "outside.md")
	if err := os.WriteFile(outside, []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(root, "wsroot")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	res, _, err := Plan(inner, []Edit{{File: "../outside.md", Old: "secret", New: "leak"}})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("want rejection, got %v", err)
	}
	if res[0].Reason == "" {
		t.Fatal("no reason recorded")
	}
	if b, _ := os.ReadFile(outside); string(b) != "secret\n" {
		t.Fatal("file outside the workspace was modified")
	}
}

func TestWriteAndVerify(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.md", "alpha\n")
	edits := []Edit{{File: "a.md", Old: "alpha", New: "ALPHA"}}
	_, bufs, err := Plan(root, edits)
	if err != nil {
		t.Fatal(err)
	}
	written, err := Write(root, bufs)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 1 || written[0] != "a.md" {
		t.Fatalf("written = %v", written)
	}
	for _, v := range Verify(root, bufs) {
		if !v.OK {
			t.Fatalf("verify failed: %s", v.Reason)
		}
	}
	// Verify reads from disk, so a file reverted behind the tool's back fails.
	write(t, root, "a.md", "alpha\n")
	if v := Verify(root, bufs); v[0].OK {
		t.Fatal("verify passed on a reverted file")
	}
}

// A pure DELETION was verified by strings.Count(body, ""), which is len+1 and
// therefore never zero: the whole class was checked vacuously, and a deletion
// that never reached disk reported as landed.
func TestVerify_deletionIsNotVacuous(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.md", "keep\nDELETEME \ntail\n")
	edits := []Edit{{File: "a.md", Old: "DELETEME ", New: ""}}
	_, bufs, err := Plan(root, edits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Write(root, bufs); err != nil {
		t.Fatal(err)
	}
	// Something else puts the deleted text back, the way a second edit planned
	// from a stale buffer did.
	write(t, root, "a.md", "keep\nDELETEME \ntail\n")
	v := Verify(root, bufs)
	if len(v) != 1 || v[0].OK {
		t.Fatalf("a deletion that did not land must fail verification, got %+v", v)
	}
}

// Two spellings of one path are one file. Keyed by the raw string they were
// two: both planned against the pristine bytes, both written, and the second
// write silently discarded the first edit while the batch reported success.
func TestPlan_pathSpellingsAreOneBuffer(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.md", "DELETEME PPP\n")
	edits := []Edit{
		{File: "a.md", Old: "DELETEME ", New: ""},
		{File: "./a.md", Old: "PPP", New: "QQQ"},
	}
	results, bufs, err := Plan(root, edits)
	if err != nil {
		t.Fatalf("both edits must plan: %v (%+v)", err, results)
	}
	if len(bufs) != 1 {
		t.Fatalf("two spellings of one path must share one buffer, got %d: %v", len(bufs), bufs)
	}
	if _, err := Write(root, bufs); err != nil {
		t.Fatal(err)
	}
	for _, v := range Verify(root, bufs) {
		if !v.OK {
			t.Fatalf("verify failed: %s", v.Reason)
		}
	}
	b, _ := os.ReadFile(root + "/a.md")
	if string(b) != "QQQ\n" {
		t.Fatalf("both edits must land, got %q", b)
	}
}

// A chained batch lands correctly and must VERIFY correctly: searching for each
// edit's New found the intermediate text missing and rejected a perfect apply,
// leaving an uncommitted artifact and a batch that could not be re-run.
func TestVerify_chainedEditsAreNotAFalseFailure(t *testing.T) {
	root := t.TempDir()
	write(t, root, "b.md", "alpha\n")
	edits := []Edit{
		{File: "b.md", Old: "alpha", New: "beta"},
		{File: "b.md", Old: "beta", New: "gamma"},
	}
	_, bufs, err := Plan(root, edits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Write(root, bufs); err != nil {
		t.Fatal(err)
	}
	for _, v := range Verify(root, bufs) {
		if !v.OK {
			t.Fatalf("a chained batch that landed must verify, got %s", v.Reason)
		}
	}
	b, _ := os.ReadFile(root + "/b.md")
	if string(b) != "gamma\n" {
		t.Fatalf("got %q", b)
	}
}

// The absolute-path half of SafeJoin's refusal. Without it an absolute path is
// silently re-rooted under the workspace instead of refused — safe in
// direction, but the refusal the comment promises was unproven.
func TestSafeJoin_absoluteRefused(t *testing.T) {
	if _, _, err := SafeJoin(t.TempDir(), "/etc/passwd"); err == nil {
		t.Fatal("an absolute path must be refused")
	}
	if _, _, err := SafeJoin(t.TempDir(), "../outside.md"); err == nil {
		t.Fatal("an escaping path must be refused")
	}
}
