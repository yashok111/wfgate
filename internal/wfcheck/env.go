package wfcheck

import "os"

// checkEnvironment reports the two environment variables that make every
// `model:` in the script a dead letter at launch. Since Claude Code 2.1.296
// CLAUDE_CODE_WORKFLOW_SUBAGENT_MODEL replaces the model of every agent() of a
// workflow run — the call's own option and the agent type's frontmatter are
// both set aside before resolution — and CLAUDE_CODE_SUBAGENT_MODEL_FORCE
// turns CLAUDE_CODE_SUBAGENT_MODEL from a default into the same override for
// every subagent. The required-option check above then verifies text the run
// never reads. The check reads ITS OWN environment: the pre-flight gate runs it
// from the session that launches, which is what makes the two the same, and
// the report says nothing when neither is set because "no override seen here"
// is not a claim about the launch.
func checkEnvironment(r *Report) {
	if v := os.Getenv("CLAUDE_CODE_WORKFLOW_SUBAGENT_MODEL"); v != "" && v != "inherit" {
		r.add(WARN, 1, "CLAUDE_CODE_WORKFLOW_SUBAGENT_MODEL=%q is set in this environment — at launch it replaces the `model:` of every agent() above, and the run prints `Workflow agent model ... ignored` per spawn; `unset CLAUDE_CODE_WORKFLOW_SUBAGENT_MODEL` in the launching session before the run", v)
	}
	if os.Getenv("CLAUDE_CODE_SUBAGENT_MODEL_FORCE") != "" {
		if v := os.Getenv("CLAUDE_CODE_SUBAGENT_MODEL"); v != "" && v != "inherit" {
			r.add(WARN, 1, "CLAUDE_CODE_SUBAGENT_MODEL_FORCE is set and CLAUDE_CODE_SUBAGENT_MODEL=%q — together they replace the `model:` of every subagent, this script's included; `unset CLAUDE_CODE_SUBAGENT_MODEL_FORCE` in the launching session before the run", v)
		}
	}
}
