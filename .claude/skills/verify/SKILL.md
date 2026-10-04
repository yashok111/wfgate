---
name: verify
description: Run wfgate's bars before a commit — lint, race tests, the built binaries against the banked fixtures, the manual as a gate corpus, the plugin manifests — and check the plugin version bump the commit owes. Claude Code runs a project skill named `verify` right before committing (2.1.286 and later); it is also the checklist for "is this green".
---

# verify — wfgate's pre-commit bars

Claude Code tells the agent to run a project skill named `verify` right before
a commit, except docs-only and tests-only commits. This is that skill for this
repository. It lives in `.claude/skills/`, not in the plugin's `skills/`,
because only a project or user skill triggers this: the binary looks the name up
among skills whose source is `skills` (or legacy `commands`), and a plugin skill
loads as `wfgate:verify` from source `plugin`, so it would never fire. The bars
are this repository's alone in any case.

## 1. The bars, in this order

```bash
make lint && make test && make check-tools && make check-docs && make check-plugin
```

- `make test`: read the FAIL lines. Counting `ok` lines is not a green bar —
  one package can fail while the count looks right.
- `make check-plugin` says `NOT RUN` without `claude` on PATH. That is a red
  bar, not a skipped one.
- A red bar stops the commit. Do not commit "to fix in the next one".

## 2. The version bump the commit owes

If the change touches `skills/`, `hooks/`, `agents/` or `scripts/hooks/` (the
last because `hooks/hooks.json` executes those files):

```bash
{ git diff HEAD --name-only; git ls-files -o --exclude-standard; } \
  | grep -E '^(skills|hooks|agents|scripts/hooks)/'
```

then `.claude-plugin/plugin.json` `version` must be bumped IN THE SAME COMMIT.
The test reads the working tree against HEAD plus untracked files, not
`--cached`: run before `git add`, a staged-only test answers "no bump owed"
over an empty set. `claude plugin update` copies nothing when the declared
version is the one already installed, so a fix without a bump never reaches a
session that loads a CACHED copy. This machine's sessions read the folder
marketplace in place (wave 29), so here the edit is live before the commit —
the bump serves every other install.

## 3. After the commit, never before

```bash
make install                              # stamps the binaries with the NEW commit
claude plugin update wfgate@wfgate-tools  # only if step 2 applied; read the version pair
```

`make install` over a dirty tree stamps `<old-sha>-dirty` into every binary,
and every `freeze.json` written with it records a revision nobody can check
out. The plugin update says "Restart to apply changes", and it means it: the
running session keeps the old skill.

## 4. What this skill does not replace

A tooling wave still earns a review round (three disjoint vectors), and a new
guard is not proven until a mutation makes a bar fail. Green bars are the floor.
