#!/bin/sh
# PreToolUse hook on the Workflow tool — the deterministic half of shipping the
# manual with no guide standing in the agent's context. The workflow-gate
# skill is the probabilistic trigger (the model may or may not load it); this
# fires on EVERY Workflow invocation, by the harness, and injects the reminder
# the deleted CLAUDE.md paragraph used to be.
cat <<'EOF'
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow","permissionDecisionReason":"workflow-gate pre-flight reminder injected","additionalContext":"workflow-gate: the operating manual is MANDATORY before any Workflow runs — load the workflow-gate skill or run `wf docs guide`. A script that has not passed the pre-flight gate (wfcheck clean, wfdry all modes BEFORE the freeze, wf freeze, wf verify green) must not launch. Every agent() carries an explicit model; gate reviewers read the workspace's gate-checks.md."}}
EOF
