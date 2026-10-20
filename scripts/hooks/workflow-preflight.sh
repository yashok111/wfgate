#!/bin/sh
# PreToolUse hook on the Workflow tool — the deterministic half of shipping the
# manual with no guide standing in the agent's context. The workflow-gate
# skill is the probabilistic trigger (the model may or may not load it); this
# fires on EVERY Workflow invocation, by the harness, and injects the reminder
# the deleted CLAUDE.md paragraph used to be.
#
# It injects CONTEXT and decides NOTHING: `additionalContext` stands on its own
# in the PreToolUse contract. The first version also emitted
# `"permissionDecision":"allow"`, which the contract defines as "bypasses the
# permission system entirely so the user is not prompted" — so every Workflow
# launch, the one tool that spawns a whole fleet, was auto-approved by a hook
# whose stated purpose was a reminder. A reminder that grants permission is a
# different tool from the one plugin.json describes.
#
# hooks.json gives this hook `onFailure: "block"` (Claude Code 2.1.295): a hook
# that cannot start, times out or exits with a code the contract does not know
# BLOCKS the launch instead of letting it through. Before that key existed the
# failure ran in the silent direction — a plugin directory the harness could
# not find (a 2.1.297 fix names the case) meant a fleet launched with no
# reminder and nothing saying so. The Write|Edit hook deliberately carries no
# such key: blocking every write of a session over a reminder is a different
# trade, and that hook exits 0 on nearly every call.
cat <<'EOF'
{"hookSpecificOutput":{"hookEventName":"PreToolUse","additionalContext":"workflow-gate: the operating manual is MANDATORY before any Workflow runs — load the workflow-gate skill or run `wf docs guide`. A script that has not passed the pre-flight gate (wfcheck clean, wfdry all modes BEFORE the freeze, wf freeze, wf verify green) must not launch. Every agent() carries an explicit model AND an explicit effort (omitted, effort is inherited from this session); gate reviewers read the workspace's gate-checks.md."}}
EOF
