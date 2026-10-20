package wfcheck

import "testing"

// A `model:` on every call verifies nothing when the launching environment
// overrides it; the check must fire on the override and stay silent without
// one, because a line printed on every run is read on none.
func TestEnvironmentOverrideIsReported(t *testing.T) {
	body := "export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }\n" +
		"const S = { type: 'object', properties: { findings: { type: 'array' } } }\n" +
		"const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }\n" +
		"phase('P')\nawait spawn(`go`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S })\nreturn {}"

	t.Run("workflow override", func(t *testing.T) {
		t.Setenv("CLAUDE_CODE_WORKFLOW_SUBAGENT_MODEL", "haiku")
		if r := run(t, body); !has(r, WARN, `CLAUDE_CODE_WORKFLOW_SUBAGENT_MODEL="haiku" is set`) {
			t.Fatalf("want the override reported, got:\n%s", dump(r))
		}
	})
	t.Run("inherit switches it off", func(t *testing.T) {
		t.Setenv("CLAUDE_CODE_WORKFLOW_SUBAGENT_MODEL", "inherit")
		if r := run(t, body); has(r, WARN, "CLAUDE_CODE_WORKFLOW_SUBAGENT_MODEL") {
			t.Fatalf("`inherit` is the binary's off switch and must not be reported, got:\n%s", dump(r))
		}
	})
	t.Run("force", func(t *testing.T) {
		t.Setenv("CLAUDE_CODE_SUBAGENT_MODEL_FORCE", "1")
		t.Setenv("CLAUDE_CODE_SUBAGENT_MODEL", "sonnet")
		if r := run(t, body); !has(r, WARN, `CLAUDE_CODE_SUBAGENT_MODEL_FORCE is set and CLAUDE_CODE_SUBAGENT_MODEL="sonnet"`) {
			t.Fatalf("want the forced default reported, got:\n%s", dump(r))
		}
	})
	t.Run("a default without force is not an override", func(t *testing.T) {
		t.Setenv("CLAUDE_CODE_SUBAGENT_MODEL_FORCE", "")
		t.Setenv("CLAUDE_CODE_SUBAGENT_MODEL", "sonnet")
		if r := run(t, body); has(r, WARN, "CLAUDE_CODE_SUBAGENT_MODEL") {
			t.Fatalf("a default an explicit model beats is not an override, got:\n%s", dump(r))
		}
	})
	t.Run("silent when unset", func(t *testing.T) {
		t.Setenv("CLAUDE_CODE_WORKFLOW_SUBAGENT_MODEL", "")
		t.Setenv("CLAUDE_CODE_SUBAGENT_MODEL_FORCE", "")
		if r := run(t, body); has(r, WARN, "SUBAGENT_MODEL") {
			t.Fatalf("nothing set, nothing to report, got:\n%s", dump(r))
		}
	})
}
