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
	sts, _, err := CollectStatements(root, names, 8)
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
	got, amb := PathsNamed("The rule in `decisions.md:120` contradicts artifacts/context.md.", arts)
	if len(amb) != 0 {
		t.Fatalf("nothing here is ambiguous: %v", amb)
	}
	if lines, ok := got["artifacts/decisions.md"]; !ok || len(lines) != 1 || lines[0] != [2]int{120, 120} {
		t.Fatalf("want decisions.md at line 120: %+v", got)
	}
	if lines, ok := got["artifacts/context.md"]; !ok || lines[0] != [2]int{} {
		t.Fatalf("a path named without a line is still named: %+v", got)
	}
	if n, _ := PathsNamed("no path here at all", arts); len(n) != 0 {
		t.Fatal("a finding naming no artifact must resolve to nothing")
	}
	// An anchor is part of a citation, not part of the path.
	if n, _ := PathsNamed("see artifacts/decisions.md:12#rule", arts); len(n["artifacts/decisions.md"]) == 0 {
		t.Fatal("an anchored citation names its file")
	}
	// A path is a mention only when it stands on its own: an artifact named
	// notes.md must not be found inside release-notes.md.
	if n, _ := PathsNamed("see release-notes.md for the rest", []string{"artifacts/notes.md"}); len(n) != 0 {
		t.Fatalf("a suffix of another filename is not a mention: %+v", n)
	}
}

// A spelling matching two artifacts resolves to NEITHER — the class `check
// cite` already reports as ambiguous. Taking the first match would report a
// closure as landed against a file nobody named, which is worse than saying it
// could not tell.
func TestPathsNamed_ambiguousSpellingIsRefusedNotGuessed(t *testing.T) {
	arts := []string{"artifacts/a/rules.md", "artifacts/b/rules.md"}
	got, amb := PathsNamed("the rule in rules.md is wrong", arts)
	if len(got) != 0 {
		t.Fatalf("an ambiguous spelling names nothing: %+v", got)
	}
	if len(amb) != 1 || amb[0] != "rules.md" {
		t.Fatalf("and it must be reported by name: %v", amb)
	}
	// Written from the workspace root it is unambiguous again.
	got, amb = PathsNamed("the rule in artifacts/b/rules.md is wrong", arts)
	if len(amb) != 0 || len(got["artifacts/b/rules.md"]) != 1 {
		t.Fatalf("a full path resolves: %+v %v", got, amb)
	}
}

// An artifact path may contain a space — the citation checker has a test for
// exactly that class. Cutting the finding text into whitespace tokens made such
// a path impossible to name, so every closure citing it counted as naming no
// artifact at all.
func TestPathsNamed_pathWithASpace(t *testing.T) {
	arts := []string{"artifacts/design notes.md"}
	got, _ := PathsNamed("the claim in `design notes.md:12` is stale", arts)
	if lines, ok := got["artifacts/design notes.md"]; !ok || lines[0] != [2]int{12, 12} {
		t.Fatalf("want the spaced path at line 12: %+v", got)
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

// The parser dropped every file the diff ADDED, because an added file has no
// old side and the old path was the only key. Three checks read that parse: the
// added file's words never entered the "added" set, so a fix that MOVED a rule
// into a new artifact retired its whole vocabulary; the new file's own
// sentences were then scanned as if they predated the fix; and `TouchedRanges`
// could not see the commit that created a document at all.
func TestParseUnified_addedFileIsNotInvisible(t *testing.T) {
	diff := `diff --git a/artifacts/new.md b/artifacts/new.md
new file mode 100644
--- /dev/null
+++ b/artifacts/new.md
@@ -0,0 +1,2 @@
+The allocator hands out a fresh version identifier on every single write.
+A second line.
diff --git a/artifacts/old.md b/artifacts/old.md
--- a/artifacts/old.md
+++ b/artifacts/old.md
@@ -3,1 +3,0 @@
-The allocator hands out a fresh version identifier on every single write.
`
	// The rule moved: every word of it is on both sides of the range, so nothing
	// was retired.
	_, sts := corpus(t, map[string]string{
		"artifacts/b.md": "# B\n\nBecause the counter is bumped in place, a reader never observes any gap.\n",
	})
	rep := FindStale(sts, diff, 8)
	for _, tok := range rep.Retired {
		if tok == "allocator" || tok == "identifier" {
			t.Fatalf("a word re-added in a NEW file is not retired: %v", rep.Retired)
		}
	}
	// And closure can see the commit that created the file.
	got := TouchedRanges(diff)
	if _, ok := got["artifacts/new.md"]; !ok {
		t.Fatalf("the created file must be a touched path: %+v", got)
	}
	if !RangesCover(got["artifacts/new.md"], 42) {
		t.Fatalf("a file this commit created was touched everywhere: %+v", got)
	}
}

// A mode change touches a file without changing a line. The FILE-level question
// `check closure` asks of a finding citing no section is exactly "did this
// commit open that document", and a missing key answered no.
func TestTouchedRanges_modeOnlyChangeIsATouchedPath(t *testing.T) {
	diff := `diff --git a/artifacts/a.md b/artifacts/a.md
old mode 100644
new mode 100755
`
	got := TouchedRanges(diff)
	if _, ok := got["artifacts/a.md"]; !ok {
		t.Fatalf("a mode-only change still touched the file: %+v", got)
	}
	if RangesCover(got["artifacts/a.md"], 5) {
		t.Fatalf("but it covers no LINE: %+v", got)
	}
}

// git C-quotes a path carrying a space or a non-ASCII byte, and a quoted path
// matches nothing — the same lesson `gitx.Dirty` learned with -z. Cyrillic is
// legal here as DATA, and the citation checker has a test for exactly that.
func TestParseUnified_quotedPathIsDecoded(t *testing.T) {
	diff := "diff --git \"a/artifacts/\\320\\264\\320\\276\\320\\272.md\" \"b/artifacts/\\320\\264\\320\\276\\320\\272.md\"\n" +
		"--- \"a/artifacts/\\320\\264\\320\\276\\320\\272.md\"\n" +
		"+++ \"b/artifacts/\\320\\264\\320\\276\\320\\272.md\"\n" +
		"@@ -1,1 +1,1 @@\n-old\n+new\n"
	got := TouchedRanges(diff)
	if _, ok := got["artifacts/док.md"]; !ok {
		t.Fatalf("the quoted path must decode to the real one: %+v", got)
	}
}

// A decision document is full of fenced code, and `# D9 example` inside a block
// is a shell comment. A phantom section would give the closure check a range to
// measure a commit against, and a duplicate id would overwrite the real one.
func TestSectionRanges_codeAndMalformedHeadings(t *testing.T) {
	body := []byte(strings.Join([]string{
		"# Decisions",          // 1
		"",                     // 2
		"## D1 — the rule",     // 3
		"```sh",                // 4
		"# D2 not a heading",   // 5
		"```",                  // 6
		"#not-a-heading",       // 7
		"####### D3 too deep",  // 8
		"body",                 // 9
		"",                     // 10
		"## D2 — the real one", // 11
		"two",                  // 12
	}, "\n"))
	got := SectionRanges(body)
	if r, ok := got["D2"]; !ok || r[0] != 11 {
		t.Fatalf("D2 must be the real heading, not the one inside the fence: %+v", got)
	}
	if _, ok := got["D3"]; ok {
		t.Fatalf("seven hashes are not a heading: %+v", got)
	}
	if r, ok := got["D1"]; !ok || r[1] != 10 {
		t.Fatalf("D1 must run to the next real heading: %+v", got)
	}
}

// One id heading two real sections gets the UNION: a finding citing it is
// closed by a fix reaching either, which is the same "any" rule the closure
// check applies to the ids themselves. Overwriting would silently move the
// range a commit is measured against.
func TestSectionRanges_repeatedIDIsUnioned(t *testing.T) {
	body := []byte(strings.Join([]string{
		"## D9 — first half", // 1
		"a",                  // 2
		"",                   // 3
		"## Other",           // 4
		"b",                  // 5
		"",                   // 6
		"## D9 — continued",  // 7
		"c",                  // 8
	}, "\n"))
	got := SectionRanges(body)
	r, ok := got["D9"]
	if !ok || r[0] != 1 || r[1] != 8 {
		t.Fatalf("want the union of both sections: %+v", got)
	}
}

// A fix that MOVES a rule into a new artifact writes every sentence of that
// artifact. They are the fix, not its consumers, and the parser's blindness to
// added files made them both: the moved vocabulary counted as retired and the
// new document counted as the paragraph still using it.
func TestFindStale_sentencesOfANewFileAreNotConsumers(t *testing.T) {
	_, sts := corpus(t, map[string]string{
		"artifacts/new.md": "# New\n\n" +
			"Because the counter is bumped in place, a reader never observes any gap at all.\n",
	})
	diff := `diff --git a/artifacts/new.md b/artifacts/new.md
new file mode 100644
--- /dev/null
+++ b/artifacts/new.md
@@ -0,0 +1,3 @@
+# New
+
+Because the counter is bumped in place, a reader never observes any gap at all.
diff --git a/artifacts/old.md b/artifacts/old.md
--- a/artifacts/old.md
+++ b/artifacts/old.md
@@ -3,1 +3,0 @@
-Because the counter is bumped in place, a reader never observes any gap at all.
`
	rep := FindStale(sts, diff, 8)
	if len(rep.Hits) != 0 {
		t.Fatalf("the file the range CREATED is the fix, not a consumer: %+v", rep.Hits)
	}
	if rep.Touched == 0 {
		t.Fatalf("and its sentences must be counted as written by the range: %+v", rep)
	}
}

// A citation is a SPAN and the closure check asks about the span: over the
// eleven gate workspaces on disk, 295 closure citations carry a `:120-134` and
// every one of them writes an ASCII hyphen. An INVERTED span keeps the start
// alone rather than being widened to cover both readings — `check cite` reports
// that shape as an error, and repairing it silently here would answer a question
// about a region the author did not write.
func TestPathsNamed_readsTheSpanAndRefusesAnInvertedOne(t *testing.T) {
	arts := []string{"artifacts/p3h.mjs"}
	got, _ := PathsNamed("EXTERNAL. `artifacts/p3h.mjs:454-459` — the index join", arts)
	if len(got["artifacts/p3h.mjs"]) != 1 || got["artifacts/p3h.mjs"][0] != [2]int{454, 459} {
		t.Fatalf("want the whole span 454-459: %+v", got)
	}
	got, _ = PathsNamed("see artifacts/p3h.mjs:459-454 for the rest", arts)
	if got["artifacts/p3h.mjs"][0] != [2]int{459, 459} {
		t.Fatalf("an inverted span is the start alone: %+v", got)
	}
	// A trailing hyphen with no second number is a line, not an open-ended span.
	got, _ = PathsNamed("see artifacts/p3h.mjs:120- and nothing else", arts)
	if got["artifacts/p3h.mjs"][0] != [2]int{120, 120} {
		t.Fatalf("a dangling hyphen leaves a line: %+v", got)
	}
}

// A word the fix INFLECTED is not a word the fix retired, and the five-rune
// stem cannot see that for a token shorter than five. Measured on a live gate:
// a fix rewrote "consumers work" as "consumer works", the bare `work` left the
// text, and the check reported four untouched sentences that merely use a common
// English verb. Over that gate all 8 of its survivor reports were this class.
func TestFindStale_aShortWordTheFixOnlyInflectedIsNotRetired(t *testing.T) {
	_, sts := corpus(t, map[string]string{
		"artifacts/a.md": "# A\n\nThe literal matcher works under a parameterised base path.\n",
		"artifacts/b.md": "# B\n\nEvery downstream matcher must work the same way here.\n",
	})
	// ONE token changes, so the count below is about this token and no other.
	diff := `diff --git a/artifacts/a.md b/artifacts/a.md
--- a/artifacts/a.md
+++ b/artifacts/a.md
@@ -3,1 +3,1 @@
-The literal matcher work under a parameterised base path.
+The literal matcher works under a parameterised base path.
`
	rep := FindStale(sts, diff, 8)
	for _, h := range rep.Hits {
		t.Fatalf("an inflection is not a retirement: %+v (retired %v)", h, rep.Retired)
	}
	// And the narrowing is COUNTED, the way the common-token one is: a
	// population dropped in silence is a population nobody can argue with.
	if rep.Reinflected != 1 {
		t.Fatalf("the one token dropped as an inflection must be counted: %d", rep.Reinflected)
	}
	// And the same the other way round: the fix wrote the SHORTER form.
	_, sts2 := corpus(t, map[string]string{
		"artifacts/a.md": "# A\n\nThe literal matcher work under a parameterised base path.\n",
		"artifacts/b.md": "# B\n\nEvery downstream matcher still works the same way here.\n",
	})
	diff2 := `diff --git a/artifacts/a.md b/artifacts/a.md
--- a/artifacts/a.md
+++ b/artifacts/a.md
@@ -3,1 +3,1 @@
-The literal matcher works under a parameterised base path.
+The literal matcher work under a parameterised base path.
`
	if rep2 := FindStale(sts2, diff2, 8); len(rep2.Hits) != 0 {
		t.Fatalf("nor in the other direction: %+v (retired %v)", rep2.Hits, rep2.Retired)
	}
}

// The five-rune stem drops its own share of the same class, and it is counted
// too: the report names ONE narrowing, so both halves have to reach it.
func TestFindStale_theStemSuppressionIsCountedAsWell(t *testing.T) {
	_, sts := corpus(t, map[string]string{
		"artifacts/a.md": "# A\n\nEvery consumer of the rule reads the frozen revision.\n",
		"artifacts/b.md": "# B\n\nThe consumers of that rule were never re-read at all.\n",
	})
	diff := `diff --git a/artifacts/a.md b/artifacts/a.md
--- a/artifacts/a.md
+++ b/artifacts/a.md
@@ -3,1 +3,1 @@
-Every consumers of the rule reads the frozen revision.
+Every consumer of the rule reads the frozen revision.
`
	rep := FindStale(sts, diff, 8)
	if rep.Reinflected != 1 || len(rep.Hits) != 0 {
		t.Fatalf("consumers/consumer is one word, dropped and counted: %d %+v", rep.Reinflected, rep.Hits)
	}
}

// The suppression is a WHITELIST of endings and not a length bound, because a
// bound alone pairs words that are not related: over the eleven gate workspaces
// the bound alone matched `within` under `with`, `someone` under `some` and
// `formula` under `form`. A retirement it swallows is a consumer nobody reports.
func TestFindStale_aLongerWordThatIsNotAnInflectionStillRetires(t *testing.T) {
	_, sts := corpus(t, map[string]string{
		"artifacts/a.md": "# A\n\nThe reviewer stands within the frozen revision it reads.\n",
		"artifacts/b.md": "# B\n\nEvery reviewer runs with the frozen artifacts and nothing else.\n",
	})
	diff := `diff --git a/artifacts/a.md b/artifacts/a.md
--- a/artifacts/a.md
+++ b/artifacts/a.md
@@ -3,1 +3,1 @@
-The reviewer stands with the frozen revision it reads.
+The reviewer stands within the frozen revision it reads.
`
	rep := FindStale(sts, diff, 8)
	if len(rep.Hits) != 1 || !strings.Contains(strings.Join(rep.Hits[0].Tokens, " "), "with") {
		t.Fatalf("`within` is not an inflection of `with`: %+v (retired %v)", rep.Hits, rep.Retired)
	}
}

// English doubles the final consonant before some of those endings, and a plain
// suffix test misses every one of them — `stop`/`stopped`, `bump`/`bumping`,
// `gzip`/`gzipped` are one word each.
func TestFindStale_aDoubledConsonantIsStillOneWord(t *testing.T) {
	_, sts := corpus(t, map[string]string{
		"artifacts/a.md": "# A\n\nThe guard stopped the run before the second gate could ever begin.\n",
		"artifacts/b.md": "# B\n\nNothing here can stop a run that has already been dispatched.\n",
	})
	diff := `diff --git a/artifacts/a.md b/artifacts/a.md
--- a/artifacts/a.md
+++ b/artifacts/a.md
@@ -3,1 +3,1 @@
-The guard can stop the run before the second gate could ever begin.
+The guard stopped the run before the second gate could ever begin.
`
	if rep := FindStale(sts, diff, 8); len(rep.Hits) != 0 {
		t.Fatalf("stop/stopped is one word: %+v (retired %v)", rep.Hits, rep.Retired)
	}
}
