#!/bin/sh
# install.sh — put the whole gate toolchain onto this machine's Claude Code.
#
# One command on a fresh clone:
#   git clone ssh://git@git.sumka.site:2222/yakov/wfgate.git && wfgate/scripts/install.sh
#
# Two delivery channels, split by what each does well:
#   - `make install` covers what a plugin cannot: builds the three
#     version-stamped binaries — wf, wfcheck, wfdry — and copies them into
#     ~/.local/bin.
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
	rm -f "$HOME/.local/bin/wf" "$HOME/.local/bin/wfcheck" "$HOME/.local/bin/wfdry"
	claude plugin uninstall wfgate 2>/dev/null || true
	claude plugin marketplace remove wfgate-tools 2>/dev/null || true
	echo "uninstalled: binaries, plugin and marketplace registration"
	exit 0
fi

need git "clone/pull the repo"
need go "build the three binaries"
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
# An installed plugin is a CACHED COPY, not a view of the clone, and refreshing
# the marketplace does not touch it. Measured: the plugin sat at 1.0.0 from the
# day it was installed while the clone reached 1.3.0 — every reference file
# differed, and the SKILL.md that sessions actually loaded still said "every
# agent() carries an explicit model" with no word about effort. A stale manual
# is the class this repo already guards for binaries with a version stamp in
# `freeze.json`; the spec channel had no such stamp and no update either.
if claude plugin list 2>/dev/null | grep -q '^wfgate\|wfgate@'; then
	claude plugin update wfgate@wfgate-tools >/dev/null 2>&1 ||
		echo "install: plugin update needs a confirmation this shell could not give — run: claude plugin update wfgate@wfgate-tools" >&2
	echo "plugin: wfgate already installed, updated from this clone"
else
	claude plugin install wfgate@wfgate-tools --scope user ||
		echo "install: plugin install needs a confirmation this shell could not give — run: claude plugin install wfgate@wfgate-tools --scope user" >&2
fi

echo
echo "installed from $ROOT:"
echo "  wf, wfcheck, wfdry $("$HOME/.local/bin/wf" version | cut -d' ' -f2)  -> ~/.local/bin/"
echo "  wfgate plugin               -> workflow-gate skill + PreToolUse hooks (Workflow, Write|Edit)"
echo
echo "verify in a NEW session: the skill list shows wfgate:workflow-gate; wf docs | head"
echo "  (wf docs names any reference where the installed plugin has drifted from this binary)"
