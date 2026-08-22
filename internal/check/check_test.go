package check

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// mkrepo builds a git repository with the given files and one commit per call,
// so a test can control which commit last touched which file — the signal the
// duplicate check reads.
func mkrepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	run(t, dir, "config", "user.email", "t@example.invalid")
	run(t, dir, "config", "user.name", "t")
	commit(t, dir, "initial", files)
	return dir
}

func commit(t *testing.T, dir, msg string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", msg)
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func TestScanCitations(t *testing.T) {
	root := t.TempDir()
	body := "see `internal/a/repo.go:42` and DESIGN.md:100-120 and repo.go:7#Bump\n" +
		"not a citation: http://127.0.0.1:3000 or ratio 3:4\n"
	if err := os.WriteFile(filepath.Join(root, "ctx.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cits, err := ScanCitations(root, []string{"ctx.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cits) != 3 {
		t.Fatalf("want 3 citations, got %d: %+v", len(cits), cits)
	}
	if cits[1].Start != 100 || cits[1].End != 120 {
		t.Fatalf("range parsed wrong: %+v", cits[1])
	}
	if cits[2].Anchor != "Bump" || cits[2].Start != 7 || cits[2].End != 7 {
		t.Fatalf("anchor parsed wrong: %+v", cits[2])
	}
	if cits[0].AtLine != 1 {
		t.Fatalf("artifact line wrong: %+v", cits[0])
	}
}

// The extension is not a whitelist, because a whitelist of one project's
// languages makes the check find nothing at all on a repository written in
// another — and a check that finds nothing reports zero findings and exits
// clean, which is indistinguishable from a clean artifact set.
func TestScanCitations_extensionIsNotAWhitelist(t *testing.T) {
	root := t.TempDir()
	body := "py src/main.py:12 rs core/lib.rs:3-9 java A.java:5#Bump kt x.kt:1 hpp v/a.hpp:2\n" +
		"host http://127.0.0.1:3000 and 10.0.0.1:8080 and ratio 3:4 and v1.2.3:9\n"
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cits, err := ScanCitations(root, []string{"a.md"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range cits {
		got = append(got, c.Path)
	}
	want := []string{"src/main.py", "core/lib.rs", "A.java", "x.kt", "v/a.hpp"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// The artifacts are English-only by rule, so a Cyrillic (or multi-word) anchor
// is a violation to REPORT, never a citation to silently count as unanchored.
// On the first live run 15 such anchors hid inside "17 unverified" — and one of
// them was factually wrong, which the silence also hid.
func TestCitations_nonASCIIAnchorIsReportedNotSilent(t *testing.T) {
	root := t.TempDir()
	body := "see `a/f.go:3#якорь` then b/g.md:1-2#на самом деле and `a/f.go:1#OK`\n"
	if err := os.WriteFile(filepath.Join(root, "x.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cits, err := ScanCitations(root, []string{"x.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cits) != 3 {
		t.Fatalf("want 3 citations, got %d: %+v", len(cits), cits)
	}
	if cits[0].BadAnchor != "якорь" || cits[0].Anchor != "" {
		t.Fatalf("cyrillic anchor must land in BadAnchor: %+v", cits[0])
	}
	if cits[1].BadAnchor != "на" {
		t.Fatalf("multi-word anchor must be flagged by its first token: %+v", cits[1])
	}
	if cits[2].Anchor != "OK" || cits[2].BadAnchor != "" {
		t.Fatalf("an ASCII anchor must stay a normal anchor: %+v", cits[2])
	}

	target := mkrepo(t, map[string]string{
		"a/f.go": "// OK\n_\n_\n",
		"b/g.md": "one\ntwo\n",
	})
	ix, err := NewIndex(target)
	if err != nil {
		t.Fatal(err)
	}
	f, err := VerifyCitations(target, ix, cits)
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 2 {
		t.Fatalf("want exactly the two bad-anchor findings, got %d: %v", len(f), f)
	}
	for _, x := range f {
		if !strings.Contains(x.Msg, "English-only") {
			t.Errorf("finding must state the policy, got %q", x.Msg)
		}
	}
}

func TestVerifyCitations(t *testing.T) {
	target := mkrepo(t, map[string]string{
		"internal/a/repo.go": "package a\n\nfunc Bump() {}\n\nfunc Other() {}\n",
		"internal/b/repo.go": "package b\n",
		"DESIGN.md":          "one\ntwo\nthree\n",
	})
	ix, err := NewIndex(target)
	if err != nil {
		t.Fatal(err)
	}
	cits := []Citation{
		{Artifact: "x.md", AtLine: 1, Raw: "internal/a/repo.go:3", Path: "internal/a/repo.go", Start: 3, End: 3},
		{Artifact: "x.md", AtLine: 2, Raw: "repo.go:1", Path: "repo.go", Start: 1, End: 1},
		{Artifact: "x.md", AtLine: 3, Raw: "DESIGN.md:99", Path: "DESIGN.md", Start: 99, End: 99},
		{Artifact: "x.md", AtLine: 4, Raw: "nowhere.go:1", Path: "nowhere.go", Start: 1, End: 1},
		{Artifact: "x.md", AtLine: 5, Raw: "internal/a/repo.go:1#Bump", Path: "internal/a/repo.go", Start: 1, End: 1, Anchor: "Bump"},
		{Artifact: "x.md", AtLine: 6, Raw: "DESIGN.md:3-1", Path: "DESIGN.md", Start: 3, End: 1},
	}
	f, err := VerifyCitations(target, ix, cits)
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 5 {
		t.Fatalf("want 5 findings, got %d: %v", len(f), f)
	}
	want := []string{"ambiguous path", "has 3 lines", "no tracked file matches", "anchor \"Bump\"", "inverted range"}
	for i, w := range want {
		if !strings.Contains(f[i].Msg, w) {
			t.Errorf("finding %d = %q, want it to mention %q", i, f[i].Msg, w)
		}
	}
	// An off-by-a-few anchor names the right line, which turns the finding into
	// a patch rather than a search.
	if !strings.Contains(f[3].Msg, "nearest occurrence is line 3") {
		t.Errorf("anchor finding does not point at the real line: %q", f[3].Msg)
	}
}

// Rule markers close dup's structural blind spot — copies contradicting in
// DIFFERENT words — but only where an author marked the rule; quoting and case
// still normalise away, so markup does not reintroduce the escaping seam.
func TestRuleMarkers(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"a.md": "- [R1] The layer is composed at startup, never per request.\n" +
			"- [R2] Errors are `wrapped`, not swallowed.\n",
		"b.md": "// [R1] the layer is composed at STARTUP, never per request\n" +
			"// [R2] errors are wrapped, not swallowed\n" +
			"- [R3] Only one site states this rule.\n",
		"c.md": "- [R1] The layer is composed per request when the flag says so.\n",
	}
	for rel, body := range files {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sites, markers, err := CollectRuleSites(root, []string{"a.md", "b.md", "c.md"})
	if err != nil {
		t.Fatal(err)
	}
	if markers != 6 || len(sites) != 3 {
		t.Fatalf("want 6 markers over 3 ids, got %d over %d", markers, len(sites))
	}
	conflicts := RuleConflicts(sites)
	if len(conflicts) != 1 || conflicts[0].ID != "R1" {
		t.Fatalf("want exactly R1 in conflict (c.md contradicts), got %+v", conflicts)
	}
	if len(conflicts[0].Sites) != 3 {
		t.Fatalf("a conflict must list every site of the id, got %d", len(conflicts[0].Sites))
	}
}

// Only a reference that RESOLVES to a tracked file is reported: the note exists
// to reveal unverified references to real files, not to re-admit the prose
// ratios and hosts the extension rule exists to exclude.
func TestExtensionlessRefs(t *testing.T) {
	target := mkrepo(t, map[string]string{"Makefile": "all:\n\tgo build\n", "src/x.go": "package x\n"})
	ix, err := NewIndex(target)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	body := "see Makefile:2 and nowhere/else:7 and ratio 3:4 and http://127.0.0.1:3000\n"
	if err := os.WriteFile(filepath.Join(root, "x.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	refs, err := ExtensionlessRefs(root, []string{"x.md"}, ix)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Path != "Makefile" || refs[0].Start != 2 {
		t.Fatalf("want exactly the Makefile reference, got %+v", refs)
	}
}

func TestFindDuplicates_divergentCopy(t *testing.T) {
	root := mkrepo(t, map[string]string{
		"artifacts/context.md": "- The composition is a pure function called from buildRuntime, not a branch inside it.\n",
		"artifacts/script.js":  "// the composition is a PURE function called from buildRuntime, not a branch inside it\n",
	})
	sts, err := CollectStatements(root, []string{"artifacts/context.md", "artifacts/script.js"}, 8)
	if err != nil {
		t.Fatal(err)
	}
	// Both copies are still in the initial commit: a rule that legitimately
	// lives in two artifacts is not a finding.
	g, _, err := FindDuplicates(root, sts, 0.82)
	if err != nil {
		t.Fatal(err)
	}
	if len(g) != 1 {
		t.Fatalf("want 1 group, got %d", len(g))
	}
	if g[0].Similarity != 1.0 {
		t.Fatalf("quoting and case must normalise away, sim = %v", g[0].Similarity)
	}
	if g[0].Divergent {
		t.Fatal("copies committed together must not be flagged")
	}

	// Now correct one copy and leave the other. This is the defect the check is
	// for, and it is the divergent commit that reveals it.
	commit(t, root, "correct the rule in the context only", map[string]string{
		"artifacts/context.md": "- The composition is a pure function called from buildRuntime, never a branch inside it.\n",
	})
	sts, err = CollectStatements(root, []string{"artifacts/context.md", "artifacts/script.js"}, 8)
	if err != nil {
		t.Fatal(err)
	}
	g, _, err = FindDuplicates(root, sts, 0.82)
	if err != nil {
		t.Fatal(err)
	}
	if len(g) != 1 || !g[0].Divergent {
		t.Fatalf("want one divergent group, got %+v", g)
	}
	if g[0].Similarity >= 1.0 {
		t.Fatalf("the copies differ now, sim = %v", g[0].Similarity)
	}
}

// The P2e class: two derived artifacts hard-wrap the same sentence at
// different columns. Line-bounded splitting turned that into divergent
// fragment pairs (92 flagged, zero real); unwrapping makes the copies compare
// as the identical sentences they are — sim 1.0, never divergent.
func TestCollectStatements_unwrapsHardWrappedParagraphs(t *testing.T) {
	root := mkrepo(t, map[string]string{
		"artifacts/a.md": "The override store keeps exactly one variant per\noperation and the smoke check reads it back after every write.\n",
		"artifacts/b.md": "The override store keeps exactly one\nvariant per operation and the smoke check reads it back after\nevery write.\n",
	})
	sts, err := CollectStatements(root, []string{"artifacts/a.md", "artifacts/b.md"}, 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(sts) != 2 {
		t.Fatalf("want one unwrapped statement per file, got %d: %+v", len(sts), sts)
	}
	// Different commits would once have made these wrap-fragments DIVERGENT;
	// unwrapped they are identical, and identical is never flagged.
	commit(t, root, "rewrap b only", map[string]string{
		"artifacts/b.md": "The override store keeps exactly one variant per operation\nand the smoke check reads it back after every write.\n",
	})
	sts, err = CollectStatements(root, []string{"artifacts/a.md", "artifacts/b.md"}, 8)
	if err != nil {
		t.Fatal(err)
	}
	g, _, err := FindDuplicates(root, sts, 0.82)
	if err != nil {
		t.Fatal(err)
	}
	if len(g) != 1 || g[0].Similarity != 1.0 || g[0].Divergent {
		t.Fatalf("rewrapped copies must group as identical and unflagged: %+v", g)
	}
}

// Bullets, table rows and headings stay their own statements — joining two
// bullets would compare invented sentences nobody wrote.
func TestCollectStatements_blocksDoNotJoin(t *testing.T) {
	root := t.TempDir()
	body := "- the first bullet rule states one thing about the store here\n" +
		"- the second bullet rule states a different thing about the reader\n"
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	sts, err := CollectStatements(root, []string{"a.md"}, 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(sts) != 2 {
		t.Fatalf("want 2 bullet statements, got %d: %+v", len(sts), sts)
	}
	if sts[0].Line != 1 || sts[1].Line != 2 {
		t.Fatalf("bullet lines misattributed: %+v", sts)
	}
}

// Four of P2e's six real contradictions differed only by a number or a
// number-word; no similarity threshold separates that from agreement, masking
// the number does.
func TestFindNumericDrift(t *testing.T) {
	root := t.TempDir()
	// The mask is exact after number-removal: a plural drifting with the count
	// ("line" vs "lines") breaks it, which is the documented boundary of this
	// check — it catches the count-only subset, mechanically.
	files := map[string]string{
		"a.md": "The smoke section carries four checks over the store today.\nExactly 3 recipe kinds ship in this slice of the generator.\n",
		"b.md": "The smoke section carries FIVE checks over the store today.\nExactly 3 recipe kinds ship in this slice of the generator.\n",
	}
	for rel, body := range files {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sts, err := CollectStatements(root, []string{"a.md", "b.md"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	nds := FindNumericDrift(sts)
	if len(nds) != 1 {
		t.Fatalf("want exactly the ONE-vs-two conflict, got %d: %+v", len(nds), nds)
	}
	if len(nds[0].Sites) != 2 {
		t.Fatalf("conflict must list both sites: %+v", nds[0])
	}
}

func TestFindDuplicates_ignoresSameFileAndShortLines(t *testing.T) {
	root := mkrepo(t, map[string]string{
		"artifacts/a.md": "short line here\nsame sentence repeated twice in one file for emphasis today\nsame sentence repeated twice in one file for emphasis today\n",
		"artifacts/b.md": "short line here\n",
	})
	sts, err := CollectStatements(root, []string{"artifacts/a.md", "artifacts/b.md"}, 8)
	if err != nil {
		t.Fatal(err)
	}
	g, _, err := FindDuplicates(root, sts, 0.82)
	if err != nil {
		t.Fatal(err)
	}
	if len(g) != 0 {
		t.Fatalf("want no cross-file groups, got %+v", g)
	}
}

// A deleted line whose CONTENT begins with `-- ` reaches the parser as
// `--- ...`, indistinguishable from a file header by prefix alone. It used to
// re-point the parser at a phantom path, so every later hunk of that file was
// filed under it and an invalidated citation was reported clean.
func TestFindDrift_deletedDashDashLineIsNotAFileHeader(t *testing.T) {
	body := "CREATE TABLE t (id int);\n-- a sql comment\n"
	for i := 3; i <= 20; i++ {
		body += "INSERT INTO t VALUES (" + strconv.Itoa(i) + ");\n"
	}
	dir := mkrepo(t, map[string]string{"db.sql": body})
	frozen := head(t, dir)

	// The comment changes AND the cited line 20 is rewritten, in one commit.
	changed := strings.Replace(body, "-- a sql comment", "-- a different comment", 1)
	changed = strings.Replace(changed, "INSERT INTO t VALUES (20);", "DELETE FROM t WHERE id = 20;", 1)
	commit(t, dir, "edit the comment and line 20", map[string]string{"db.sql": changed})

	cits := []Citation{{Artifact: "artifacts/a.md", AtLine: 1, Raw: "db.sql:20", Path: "db.sql", Start: 20, End: 20}}
	ds, clean, err := FindDrift(dir, frozen, cits)
	if err != nil {
		t.Fatal(err)
	}
	if clean != 0 || len(ds) != 1 {
		t.Fatalf("the cited line was rewritten; want 1 drift and 0 clean, got %d drift(s) and %d clean: %+v", len(ds), clean, ds)
	}
	if ds[0].Kind != "edited" {
		t.Fatalf("a rewritten cited line is `edited`, not %q — `shifted` tells the reviewer to re-aim a number instead of re-reading the claim", ds[0].Kind)
	}
}

func head(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// The shape this check was built for: one rule living in a decision document, a
// context document and a prompt string. Two copies are verbatim, the third
// contradicts. Excluding members of an exact group from the near-match pass
// made recall FALL as the rule spread — the same corpus reported the
// contradiction once a verbatim copy was deleted, and nothing while all three
// were present.
func TestFindDuplicates_thirdCopyIsStillCompared(t *testing.T) {
	rule := "The composition stays a pure function called from buildRuntime at startup.\n"
	contra := "The composition never stays a pure function called from buildRuntime at startup.\n"
	root := mkrepo(t, map[string]string{
		"artifacts/decisions.md": rule,
		"artifacts/context.md":   rule,
	})
	commit(t, root, "the prompt restates it, differently", map[string]string{"artifacts/prompt.md": contra})

	files := []string{"artifacts/decisions.md", "artifacts/context.md", "artifacts/prompt.md"}
	sts, err := CollectStatements(root, files, 8)
	if err != nil {
		t.Fatal(err)
	}
	groups, _, err := FindDuplicates(root, sts, 0.82)
	if err != nil {
		t.Fatal(err)
	}
	near, divergent := 0, 0
	for _, g := range groups {
		if g.Similarity < 1.0 {
			near++
			if g.Divergent {
				divergent++
			}
		}
	}
	if near == 0 {
		t.Fatalf("the contradicting third copy must be compared against the verbatim ones, got groups: %+v", groups)
	}
	if divergent == 0 {
		t.Fatalf("the third copy was committed apart and reads differently — that is DIVERGENT: %+v", groups)
	}
}
