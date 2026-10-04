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
	# Each step reports its own outcome. The first version ran both plugin
	# commands under `|| true` and printed "uninstalled: binaries, plugin and
	# marketplace registration" with no `claude` on PATH at all — exit 0, the
	# plugin and its hooks still installed and still firing.
	need claude "remove the plugin registration"
	rm -f "$HOME/.local/bin/wf" "$HOME/.local/bin/wfcheck" "$HOME/.local/bin/wfdry"
	echo "uninstalled: binaries from ~/.local/bin"
	rc=0
	if claude plugin uninstall wfgate >/dev/null 2>&1; then
		echo "uninstalled: plugin wfgate"
	else
		echo "install: plugin wfgate was NOT uninstalled (claude plugin uninstall failed or it was not installed) — run: claude plugin uninstall wfgate" >&2
		rc=1
	fi
	if claude plugin marketplace remove wfgate-tools >/dev/null 2>&1; then
		echo "uninstalled: marketplace registration wfgate-tools"
	else
		echo "install: marketplace wfgate-tools was NOT removed — run: claude plugin marketplace remove wfgate-tools" >&2
		rc=1
	fi
	exit $rc
fi

need git "clone/pull the repo"
need go "build the three binaries"
need claude "register the plugin"

mkdir -p "$HOME/.local/bin"
make -C "$ROOT" install
make -C "$ROOT" check-tools

# Drop the build output BEFORE the plugin copy. An installed plugin is a raw
# copy of the source tree with `.git` as its only exclusion — `.gitignore` is
# not consulted — and this repository's root IS the plugin, so the three
# freshly built binaries rode into every cached version. Measured 2026-08-30:
# 36 MB of the 39 MB each version directory held, across 37 kept versions,
# 1.2 GB for a skill and two shell hooks that weigh 300 KB. They have no
# business there either way: the plugin ships the manual and the hooks, and
# `make install` above has already put the binaries where they are run from.
# `make build` regenerates them in seconds, and every bar that needs them
# depends on that target.
make -C "$ROOT" clean
# Same class, one directory over: `claude plugin eval` writes a timestamped
# results directory per run — the scored JSON, the self-contained HTML report
# and the mock recordings — under `evals/results/`, and the plugin copy takes
# it along with everything else. It is run OUTPUT: `.gitignore` keeps it out of
# the repository, which is exactly why the plugin copy is where it would
# otherwise accumulate, one kept version per release. The size tripwire below
# cannot see it — a report is well under 2 MB — so it is named here.
# `--eval-dir` and the manifest's `experimental.evals` can point the suite
# somewhere else; this covers the default, which is what this repo uses.
if [ -d "$ROOT/evals/results" ]; then
	echo "install: dropping eval run output before the plugin copy: evals/results/"
	rm -rf "$ROOT/evals/results"
fi
# And refuse to stage the tree while ANY binary is still in it. `make clean`
# knows the paths it builds to; a sweep agent's worktree under
# `.claude/worktrees/` carries its own built `wf` (measured 2026-09-02: 20 MB
# of a 23 MB cached version), and the next place a binary appears is a place
# nobody has listed yet. Anything over 2 MB outside `.git` is not a skill or a
# hook, and the plugin copy takes everything.
big=$(find "$ROOT" -path "$ROOT/.git" -prune -o -type f -size +2M -print)
if [ -n "$big" ]; then
	echo "install: the tree still holds files no plugin should carry (the plugin copy is the RAW tree, .gitignore not consulted):" >&2
	echo "$big" | sed 's/^/  /' >&2
	echo "         remove them (a finished sweep worktree: git worktree remove <path>) and re-run" >&2
	exit 1
fi

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
	claude plugin update wfgate@wfgate-tools -y >/dev/null 2>&1 ||
		echo "install: plugin update needs a confirmation this shell could not give — run: claude plugin update wfgate@wfgate-tools -y" >&2
	echo "plugin: wfgate already installed, updated from this clone"
else
	claude plugin install wfgate@wfgate-tools --scope user ||
		echo "install: plugin install needs a confirmation this shell could not give — run: claude plugin install wfgate@wfgate-tools --scope user" >&2
fi

# `claude plugin update` is VERSION-keyed: measured, it copies nothing when
# plugin.json declares the version already installed, so a spec fix committed
# without a version bump never reaches a session that loads the cached copy.
# `wf docs` answers whether the manual a session loads is in step with the
# binary, and for a cached copy the only way to force one is a reinstall.
#
# A FOLDER marketplace is read in place (measured 2026-10-04, 2.1.286 to
# 2.1.289: skills, agent and hooks.json all loaded from the clone, the cache
# read by nothing), and `make install` above has just built the binary from
# this very tree — so drift read in place means the marketplace names ANOTHER
# folder than $ROOT, and a reinstall copies a cache that changes nothing.
# `wf docs` says so and names the folder; that note is printed instead. Without this, the drift note tells an operator to run this script
# and the script does nothing — an unclearable warning, which is the failure
# this repo keeps finding in its own tools.
drift() { "$HOME/.local/bin/wf" docs 2>&1 >/dev/null | grep -q 'differs from the one this binary carries'; }
# `drift` is SILENT with no plugin installed at all (`wf docs` has nothing to
# compare), so "no drift" after a reinstall proved only that the uninstall
# half ran. Measured with a stub `claude` whose install exits 1: the plugin was
# gone and the script printed "reinstalled, the manual is in step with the
# binary", exit 0. The reinstall's own exit status is read, and the success
# line is printed only over a registration `claude plugin list` can see.
registered() { claude plugin list 2>/dev/null | grep -q '^wfgate\|wfgate@'; }
note=$("$HOME/.local/bin/wf" docs 2>&1 >/dev/null || true)
if printf '%s\n' "$note" | grep -q 'read in place from'; then
	printf '%s\n' "$note" >&2
	echo "install: the plugin is read in place from another folder than $ROOT — a reinstall would not change it; follow the note above" >&2
elif drift; then
	echo "plugin: the installed manual is behind this clone — reinstalling to force a copy"
	claude plugin uninstall wfgate >/dev/null 2>&1 || true
	if ! claude plugin install wfgate@wfgate-tools --scope user >/dev/null 2>&1; then
		echo "install: the plugin was uninstalled and the reinstall FAILED — the plugin is not installed now. Run by hand:" >&2
		echo "         claude plugin install wfgate@wfgate-tools --scope user" >&2
		exit 1
	fi
	if drift; then
		echo "install: the installed manual is STILL behind this clone. Bump \"version\" in" >&2
		echo "         .claude-plugin/plugin.json and re-run: plugin update is version-keyed." >&2
	else
		echo "plugin: reinstalled, the manual is in step with the binary"
	fi
fi
if ! registered; then
	echo "install: \`claude plugin list\` does not show wfgate — the plugin is NOT installed, whatever the lines above said. Run by hand:" >&2
	echo "         claude plugin install wfgate@wfgate-tools --scope user" >&2
	exit 1
fi

# Every `claude plugin update` writes a NEW version directory and keeps the old
# one forever, so the cache grows by one copy of the tree per release. Nothing
# reads a version the registry does not point at: keep the one just installed
# and drop the rest. Scoped to this plugin's own cache, and it refuses to run
# at all unless the kept directory is there, because a prune that has lost its
# survivor is a delete-everything.
CACHE="$HOME/.claude/plugins/cache/wfgate-tools/wfgate"
KEEP=$(sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$ROOT/.claude-plugin/plugin.json" | head -1)
if [ -n "$KEEP" ] && [ -d "$CACHE/$KEEP" ]; then
	stale=$(find "$CACHE" -maxdepth 1 -mindepth 1 -type d ! -name "$KEEP" | wc -l)
	if [ "$stale" -gt 0 ]; then
		find "$CACHE" -maxdepth 1 -mindepth 1 -type d ! -name "$KEEP" -exec rm -rf {} +
		echo "plugin: pruned $stale superseded cache version(s), kept $KEEP"
	fi
	# The copy is the raw tree, so the repository's OWN `.claude/` rides along:
	# the `verify` skill this repo runs before its commits, and any sweep
	# worktree. Neither belongs to the plugin, and a project skill sitting in a
	# cache directory is one a session started above it could discover. Removed
	# from the installed copy only — the clone keeps it.
	if [ -d "$CACHE/$KEEP/.claude" ]; then
		rm -rf "$CACHE/$KEEP/.claude"
		echo "plugin: dropped the repository's own .claude/ from the installed copy"
	fi
fi

echo
echo "installed from $ROOT:"
echo "  wf, wfcheck, wfdry $("$HOME/.local/bin/wf" version | cut -d' ' -f2)  -> ~/.local/bin/"
echo "  wfgate plugin               -> workflow-gate skill, the gate-sweep agent, PreToolUse hooks (Workflow, Write|Edit)"
echo
echo "verify in a NEW session: the skill list shows wfgate:workflow-gate; wf docs | head"
echo "  (wf docs names any reference where the manual a session loads has drifted from this binary)"
