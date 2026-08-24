package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// corpus writes artifacts and returns the statements of them, the way every
// caller of FindStale gets them.
func corpus(t *testing.T, files map[string]string) (string, []Statement) {
	t.Helper()
	root := t.TempDir()
	var names []string
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		names = append(names, rel)
	}
	sts, err := CollectStatements(root, names, 8)
	if err != nil {
		t.Fatal(err)
	}
	return root, sts
}

// The class the check exists for, in its smallest honest form: a fix replaced
// the rule in one artifact and a paragraph in another goes on arguing from the
// old one, in words no dup group and no [RN] marker reaches. Measured on a live
// decision gate: round 1's fix replaced an in-place counter with an allocator
// and left four such consumers standing.
func TestFindStale_reportsAnUntouchedConsumer(t *testing.T) {
	_, sts := corpus(t, map[string]string{
		"artifacts/a.md": "# A\n\n" +
			"The allocator hands out a fresh version identifier on every single write.\n",
		"artifacts/b.md": "# B\n\n" +
			"Because the counter is bumped in place, a reader never observes any gap.\n",
	})
	diff := `diff --git a/artifacts/a.md b/artifacts/a.md
--- a/artifacts/a.md
+++ b/artifacts/a.md
@@ -3,1 +3,1 @@
-The counter is bumped in place by one on every single write.
+The allocator hands out a fresh version identifier on every single write.
`
	rep := FindStale(sts, diff, 8)
	if len(rep.Hits) != 1 {
		t.Fatalf("want the one consumer, got %d: %+v", len(rep.Hits), rep.Hits)
	}
	h := rep.Hits[0]
	if h.File != "artifacts/b.md" {
		t.Fatalf("the hit must be the sentence the fix did NOT touch: %+v", h)
	}
	if !strings.Contains(strings.Join(h.Tokens, " "), "counter") ||
		!strings.Contains(strings.Join(h.Tokens, " "), "bumped") {
		t.Fatalf("the retired vocabulary must be printed with the hit: %+v", h.Tokens)
	}
	// The sentence the fix WROTE is not a consumer of the old rule.
	if rep.Touched != 1 {
		t.Fatalf("the rewritten sentence must be excluded and counted: %+v", rep)
	}
}

// A token the fix put back is vocabulary the rule still owns. Without this the
// check reports every sentence sharing any word with the edited region, which
// is `dup`'s abandoned first version — a check that paints most of its output.
func TestFindStale_aTokenTheFixKeptIsNotRetired(t *testing.T) {
	_, sts := corpus(t, map[string]string{
		"artifacts/b.md": "# B\n\n" +
			"Because the counter is bumped in place, a reader never observes any gap.\n",
	})
	diff := `diff --git a/artifacts/a.md b/artifacts/a.md
--- a/artifacts/a.md
+++ b/artifacts/a.md
@@ -3,1 +3,1 @@
-The counter is bumped in place by one on every single write.
+The counter is bumped in place by two on every single write.
`
	rep := FindStale(sts, diff, 8)
	if len(rep.Hits) != 0 {
		t.Fatalf("a fix that kept its vocabulary retires nothing, got %+v (retired %v)", rep.Hits, rep.Retired)
	}
	if len(rep.Retired) != 0 {
		t.Fatalf("retired must be empty: %v", rep.Retired)
	}
}

// The rarity bar, and the population it hides. `dup -since` established the
// rule: the OUTPUT is narrowed, never the check, and a hidden count prints its
// own size.
func TestFindStale_commonVocabularyIsDroppedAndCounted(t *testing.T) {
	body := "# B\n\n"
	for i := 0; i < 12; i++ {
		body += "The reviewer reads the manifest before every single launch of this gate.\n\n"
	}
	_, sts := corpus(t, map[string]string{"artifacts/b.md": body})
	diff := `diff --git a/artifacts/a.md b/artifacts/a.md
--- a/artifacts/a.md
+++ b/artifacts/a.md
@@ -3,1 +3,1 @@
-The manifest is read once.
+Nothing is read.
`
	rep := FindStale(sts, diff, 8)
	if rep.Common == 0 {
		t.Fatalf("a token in 12 sentences is corpus vocabulary and must be dropped AND counted: %+v", rep)
	}
	if len(rep.Hits) != 0 {
		t.Fatalf("dropping it must remove the hits too: %+v", rep.Hits)
	}
	// The same token below the bar IS reported: the filter is a bar, not a ban.
	if rep2 := FindStale(sts, diff, 20); len(rep2.Hits) == 0 {
		t.Fatalf("raising -max-df must surface them: %+v", rep2)
	}
}

// A range that retired nothing checked nothing, and the report has to say so —
// the vacuous pass is this project's named worst failure, and a stale run over
// a pure addition is exactly that shape.
func TestFindStale_pureAdditionRetiresNothing(t *testing.T) {
	_, sts := corpus(t, map[string]string{
		"artifacts/b.md": "# B\n\nBecause the counter is bumped in place, a reader never observes any gap.\n",
	})
	diff := `diff --git a/artifacts/a.md b/artifacts/a.md
--- a/artifacts/a.md
+++ b/artifacts/a.md
@@ -3,0 +4,1 @@
+A brand new sentence that removes nothing at all from the document.
`
	rep := FindStale(sts, diff, 8)
	if len(rep.Retired) != 0 || len(rep.Hits) != 0 {
		t.Fatalf("an addition-only range retires nothing: %+v", rep)
	}
}

// TouchedRanges is what `check closure` intersects a finding's cited line with.
// A pure insertion touches no old line at all, and reporting it as touching
// nothing would call every insertion-shaped fix a non-closure.
func TestTouchedRanges(t *testing.T) {
	diff := `diff --git a/artifacts/a.md b/artifacts/a.md
--- a/artifacts/a.md
+++ b/artifacts/a.md
@@ -10,3 +10,1 @@
-old one
-old two
-old three
+new
@@ -40,0 +39,1 @@
+inserted
diff --git a/artifacts/gone.md b/artifacts/gone.md
--- a/artifacts/gone.md
+++ /dev/null
@@ -1,2 +0,0 @@
-a
-b
`
	got := TouchedRanges(diff)
	if !RangesCover(got["artifacts/a.md"], 12) {
		t.Fatalf("the replaced region must be covered: %+v", got)
	}
	if RangesCover(got["artifacts/a.md"], 20) {
		t.Fatalf("an untouched line must not be covered: %+v", got)
	}
	if !RangesCover(got["artifacts/a.md"], 40) {
		t.Fatalf("the insertion seam must be covered: %+v", got)
	}
	if !RangesCover(got["artifacts/gone.md"], 999) {
		t.Fatalf("a deleted file was touched everywhere: %+v", got)
	}
}

// A finding writes the path however it likes. Resolving rather than
// pattern-matching is what keeps the closure check from reporting an
// abbreviated path as a missing one.
func TestPathsNamed(t *testing.T) {
	arts := []string{"artifacts/decisions.md", "artifacts/context.md"}
	got := PathsNamed("The rule in `decisions.md:120` contradicts artifacts/context.md.", arts)
	if lines, ok := got["artifacts/decisions.md"]; !ok || len(lines) != 1 || lines[0] != 120 {
		t.Fatalf("want decisions.md at line 120: %+v", got)
	}
	if lines, ok := got["artifacts/context.md"]; !ok || lines[0] != 0 {
		t.Fatalf("a path named without a line is still named: %+v", got)
	}
	if len(PathsNamed("no path here at all", arts)) != 0 {
		t.Fatal("a finding naming no artifact must resolve to nothing")
	}
	// An anchor is part of a citation, not part of the path.
	if _, ok := PathsNamed("see artifacts/decisions.md:12#rule", arts)["artifacts/decisions.md"]; !ok {
		t.Fatal("an anchored citation names its file")
	}
}

// A findings ledger cites SECTIONS, not paths: measured on a live decision
// gate, 126 of 127 closures named no path at all and nearly every one named a
// section, so the section range is the reach the closure check actually has.
func TestSectionRanges(t *testing.T) {
	body := []byte(strings.Join([]string{
		"# Decisions",        // 1
		"",                   // 2
		"## D1 — the first",  // 3
		"body of one",        // 4
		"",                   // 5
		"### D14 — nested",   // 6
		"deeper",             // 7
		"",                   // 8
		"## D2 — the second", // 9
		"body of two",        // 10
		"",                   // 11
		"## Not numbered",    // 12
		"tail",               // 13
	}, "\n"))
	got := SectionRanges(body)
	if r, ok := got["D1"]; !ok || r[0] != 3 || r[1] != 8 {
		t.Fatalf("D1 must run to the next heading of its own level: %+v", got)
	}
	// A nested subsection is its own range and does not end its parent.
	if r, ok := got["D14"]; !ok || r[0] != 6 || r[1] != 8 {
		t.Fatalf("D14: %+v", got)
	}
	if r, ok := got["D2"]; !ok || r[0] != 9 || r[1] != 11 {
		t.Fatalf("D2 must end before the next heading: %+v", got)
	}
	if _, ok := got["Not"]; ok {
		t.Fatalf("a heading with no id token heads no section: %+v", got)
	}
}

func TestSectionsNamed(t *testing.T) {
	got := SectionsNamed("D9's criterion contradicts D13, and R5 says otherwise")
	want := map[string]bool{"D9": true, "D13": true, "R5": true}
	if len(got) != 3 {
		t.Fatalf("want the three ids, got %v", got)
	}
	for _, id := range got {
		if !want[id] {
			t.Fatalf("unexpected id %q in %v", id, got)
		}
	}
	// Prose must not become an id, and a repeat is one id. An id is UPPERCASE
	// and stands alone: `sqlite3` and `utf8` are words a decision document uses
	// constantly, and reading them as sections would make every finding cite one.
	for _, prose := range []string{"the http 404 answer is fine", "stored in sqlite3 by the writer",
		"encoded as utf8 throughout", "the D9x variant is different"} {
		if ids := SectionsNamed(prose); len(ids) != 0 {
			t.Fatalf("%q names no section, got %v", prose, ids)
		}
	}
	if ids := SectionsNamed("D9 again, D9"); len(ids) != 1 {
		t.Fatalf("one id, however often cited: %v", ids)
	}
}

// A fix that replaces "expect" with "expectation" retired nothing — the rule
// still owns the word, the author only inflected it. Measured on a live gate's
// real fix range: five of eleven "retired" tokens were the same word in another
// form, each dragging innocent sentences into the report.
func TestFindStale_anInflectionIsNotARetirement(t *testing.T) {
	_, sts := corpus(t, map[string]string{
		"artifacts/b.md": "# B\n\n" +
			"A caller that sends no expect value at all makes no claim about the row.\n",
	})
	diff := `diff --git a/artifacts/a.md b/artifacts/a.md
--- a/artifacts/a.md
+++ b/artifacts/a.md
@@ -3,1 +3,1 @@
-The expect field is required on all five of the routes described above.
+The expectation field is required on all five of the routes described above.
`
	rep := FindStale(sts, diff, 8)
	for _, tok := range rep.Retired {
		if strings.HasPrefix(tok, "expect") {
			t.Fatalf("an inflection of a kept word is not retired vocabulary: %v", rep.Retired)
		}
	}
	if len(rep.Hits) != 0 {
		t.Fatalf("and it must drag no sentence into the report: %+v", rep.Hits)
	}
}
