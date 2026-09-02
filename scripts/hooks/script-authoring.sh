#!/bin/sh
# PreToolUse hook on Write|Edit — the deterministic trigger for AUTHORING.
#
# Measured hole: the only other deterministic trigger is the PreToolUse hook on
# the Workflow tool, which fires at LAUNCH. Writing a script is Write/Edit, so
# the whole authoring phase — the one `authoring.md` governs — had no
# deterministic trigger at all, and the skill's own description is a
# probabilistic one the model may or may not act on. An agent wrote a whole
# orchestration script without ever reading the manual; by the time the launch
# hook fires, the script exists and the manual says it should already have been
# gated.
#
# It fires ONCE per session. A reminder repeated on every edit is a reminder
# nobody reads, which is the failure class this harness keeps finding in its own
# tools.
input=$(cat)

field() {
	printf '%s' "$input" | sed -n "s/.*\"$1\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p"
}

path=$(field file_path)

case "$path" in
# The harness's own tree. Its fixtures ARE orchestration scripts by
# construction, and the reminder is for someone about to run one, not for
# someone maintaining the checker that reads them.
*/wfgate/*) exit 0 ;;
esac

# Two independent signals, because either alone misses: a path nobody names
# `.workflow.mjs`, and a script written into a file with any name at all.
orchestration=0
case "$path" in
*.mjs | *workflow* | *orchestrat*) orchestration=1 ;;
esac
if printf '%s' "$input" | grep -q 'export const meta' &&
	printf '%s' "$input" | grep -qE 'agent\(|spawn\('; then
	orchestration=1
fi
[ "$orchestration" = 1 ] || exit 0

# Once per SESSION needs a session to key on. Without an id the first version
# fell back to a marker named `nosession`, shared by every session on the box:
# the reminder fired once ever and was silent for every later session — the
# degradation ran in the silent direction, which is the failure the hook
# exists to prevent. With no id it now fires and keeps no marker (loud, once
# per write), and a marker it cannot write is treated the same way: the
# redirection order `: >"$marker" 2>/dev/null` applied the silence AFTER the
# failure, so an unwritable TMPDIR printed the shell's own error — and then
# fired on every write, or not at all, depending on the shell.
sid=$(field session_id)
if [ -n "$sid" ]; then
	marker="${TMPDIR:-/tmp}/wfgate-authoring-$(printf '%s' "$sid" | tr -c 'A-Za-z0-9._-' '_')"
	[ -e "$marker" ] && exit 0
	# `touch`, not `: >"$marker"`: `:` is a SPECIAL builtin, and POSIX makes a
	# redirection error on one fatal to a non-interactive shell — so an
	# unwritable TMPDIR ended the script before the reminder was printed.
	touch "$marker" 2>/dev/null || true
fi

# Context only — no `permissionDecision`. `allow` bypasses the permission
# prompt entirely, and the first version emitted it on every orchestration
# write the hook matched. See workflow-preflight.sh.
cat <<'EOF'
{"hookSpecificOutput":{"hookEventName":"PreToolUse","additionalContext":"workflow-gate: this looks like a Workflow orchestration script. The operating manual is MANDATORY BEFORE authoring one, not only before launching it — load the workflow-gate skill, or run `wf docs authoring` and `wf docs routing` now. Written from memory, a script costs a gate round to correct what the manual would have said for free: every agent() carries an explicit model AND an explicit effort, the spawn wrapper turns a throw into null, a coordinate into a document under the same gate comes from its headings and never from its line numbers, and ownership is declared in the YOU OWN form wfcheck parses."}}
EOF
