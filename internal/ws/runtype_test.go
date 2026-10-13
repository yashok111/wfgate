package ws

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunTypeDefaultsAndRefusals(t *testing.T) {
	if (Manifest{}).RunType() != TypeBuild {
		t.Fatalf("a manifest written before the field existed is a build gate")
	}
	if ReadOnly(TypeBuild) || !ReadOnly(TypeReview) {
		t.Fatalf("build writes, review does not")
	}
	if err := ValidType("Review"); err == nil || !strings.Contains(err.Error(), "build, review") {
		t.Fatalf("an unknown type is refused naming the list: %v", err)
	}
}

// TypeAt is what the two script-gate tools read, and its SOURCE is what they
// print: a type that came from a workspace must say so, a default must not
// pretend to have come from one.
func TestTypeAt(t *testing.T) {
	dir := t.TempDir()
	if typ, src := TypeAt(filepath.Join(dir, "s.mjs")); typ != TypeBuild || strings.HasPrefix(src, "gate.json") {
		t.Fatalf("outside a workspace: %s, %s", typ, src)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestName), []byte(`{"name":"g","target":"/x","type":"review"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "artifacts"), 0o755); err != nil {
		t.Fatal(err)
	}
	typ, src := TypeAt(filepath.Join(dir, "artifacts", "s.mjs"))
	if typ != TypeReview || !strings.HasPrefix(src, "gate.json at ") {
		t.Fatalf("inside a review workspace: %s, %s", typ, src)
	}
}

// Only a hunt may leave reproduction worktrees; a review reproduces nothing.
func TestAllowsScratch(t *testing.T) {
	if !AllowsScratch(TypeHunt) || AllowsScratch(TypeReview) || AllowsScratch(TypeBuild) {
		t.Fatalf("scratch is a hunt's alone")
	}
	if !ReadOnly(TypeHunt) {
		t.Fatalf("a hunt is read-only")
	}
}

func TestAuditIsReadOnlyWithoutScratch(t *testing.T) {
	if err := ValidType(TypeAudit); err != nil || !ReadOnly(TypeAudit) || AllowsScratch(TypeAudit) {
		t.Fatalf("an audit is a read-only type with no scratch carve-out")
	}
}

// A security run proves its serious findings in PoC worktrees, so it carries
// the hunt's scratch carve-out; it is read-only everywhere else.
func TestSecurityIsReadOnlyWithScratch(t *testing.T) {
	if err := ValidType(TypeSecurity); err != nil || !ReadOnly(TypeSecurity) || !AllowsScratch(TypeSecurity) {
		t.Fatalf("a security run is a read-only type with the worktree carve-out")
	}
}
