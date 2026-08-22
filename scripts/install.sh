#!/bin/sh
# install.sh — put the whole gate toolchain onto this machine's Claude Code.
#
# One command on a fresh clone:
#   git clone ssh://git@git.sumka.site:2222/yakov/wfgate.git && wfgate/scripts/install.sh
#
# Two delivery channels, split by what each does well:
#   - `make install` covers what a plugin cannot: builds the version-stamped
#     `wf` binary (copy) and symlinks wfcheck/wfdry (single source of truth
#     stays this repo).
#   - the wfgate PLUGIN covers the rest: the workflow-gate skill and the
#     PreToolUse hook on the Workflow tool. Plugin hooks activate on install —
#     settings.json is never edited, by anyone.
#
# Re-running is safe; `install.sh --uninstall` reverses what it did.
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)

need() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "install: missing prerequisite: $1 ($2)" >&2
		exit 1
	}
}

if [ "${1:-}" = "--uninstall" ]; then
	rm -f "$HOME/.local/bin/wf"
	for l in "$HOME/.local/bin/wfcheck" "$HOME/.local/bin/wfdry"; do
		[ -L "$l" ] && rm "$l"
	done
	claude plugin uninstall wfgate 2>/dev/null || true
	claude plugin marketplace remove wfgate-tools 2>/dev/null || true
	echo "uninstalled: binary, symlinks, plugin and marketplace registration"
	exit 0
fi

need git "clone/pull the repo"
need go "build the wf binary"
need node "run wfcheck and wfdry"
need claude "register the plugin"

mkdir -p "$HOME/.local/bin"
make -C "$ROOT" install
make -C "$ROOT" check-tools

# The marketplace is the LOCAL CLONE, not a remote: the binary needs the clone
# anyway, so there is exactly one checkout to keep fresh. After a `git pull`,
# run `claude plugin marketplace update wfgate-tools` to refresh the plugin.
if claude plugin marketplace list 2>/dev/null | grep -q wfgate-tools; then
	claude plugin marketplace update wfgate-tools >/dev/null 2>&1 || true
	echo "plugin: marketplace wfgate-tools already registered, refreshed"
else
	claude plugin marketplace add "$ROOT"
fi
if claude plugin list 2>/dev/null | grep -q '^wfgate\|wfgate@'; then
	echo "plugin: wfgate already installed"
else
	claude plugin install wfgate@wfgate-tools --scope user ||
		echo "install: plugin install needs a confirmation this shell could not give — run: claude plugin install wfgate@wfgate-tools --scope user" >&2
fi

echo
echo "installed from $ROOT:"
echo "  wf $("$HOME/.local/bin/wf" version | cut -d' ' -f2)  -> ~/.local/bin/wf"
echo "  wfcheck, wfdry              -> symlinks into scripts/"
echo "  wfgate plugin               -> workflow-gate skill + PreToolUse(Workflow) hook"
echo
echo "verify in a NEW session: the skill list shows wfgate:workflow-gate; wf docs | head"
