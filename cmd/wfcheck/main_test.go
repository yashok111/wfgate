package main

import (
	"os/exec"
	"testing"
)

// The cleanup line is printed to be pasted into a shell, and a worktree path
// or branch is attacker-shaped text: quoted, it must come back from `sh` as
// exactly itself, and nothing inside may run.
func TestShellQuoteSurvivesTheShell(t *testing.T) {
	for _, s := range []string{"plain", "a;touch PWNED", "it's", "$(id)", "`id`", "a b|c&d", "'"} {
		out, err := exec.Command("sh", "-c", "printf %s "+shellQuote(s)).Output()
		if err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		if string(out) != s {
			t.Errorf("shellQuote(%q) came back as %q", s, out)
		}
	}
}
