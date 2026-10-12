---
name: workflow-gate
description: MANDATORY before AUTHORING, reviewing, or running ANY Workflow (multi-agent orchestration) script — read it before the FIRST LINE is written, not before the launch; a script written from memory costs a gate round to correct. Also mandatory before reviewing a workflow gate's artifacts. Maps the operating manual — model routing, the pre-flight review gate, the wf gate harness, cost discipline — and names which reference to read for the activity at hand. Trigger on "Workflow tool", "workflow script", "orchestration script", "orchestrate", "fan-out", "spawn agents", "run N agents", "agents in parallel", "multi-agent", "agent pipeline", "phases and gates", "wf gate", "pre-flight review", ".mjs script that calls agent()", or any request to build something that launches subagents.
---

# Workflow gate

The Workflow tool spawns fleets of agents; an unreviewed script burns the whole
fan-out. This skill is the operating manual's front door. The manual is the
specification a gate reviewed — do not act from memory of it; read the
reference for what you are about to do, in full.

**The manual is read BEFORE the script is written, not before it is launched.**
Measured: an agent authored a whole orchestration script without ever opening
it, because the only deterministic reminder fired on the Workflow tool — at
launch, when the script already exists and the manual says it should already
have been gated. There is now a second one on `Write`/`Edit`, but a hook that
fires once per session is a backstop, not the trigger: if you are about to
write or edit a script that calls `agent()`, the table below is the first thing
you do.

All references live in `references/` next to this file; `wf docs <name>` prints
the same text from the installed binary (`wf docs guide` concatenates the manual
in reading order; the gate-checks list is `wf docs checks`, because it is a
reviewer's checklist rather than part of the manual's argument).

**Read the reference your ROW names, not the whole manual.** The gate's own text
was one 1108-line file with no headings until 2026-08-30, and this table had to
say in prose which of its steps belonged to which reader — an operator opening a
round loaded the reviewer's vectors and the reviewer loaded the `wf apply` batch
format. It is four files now, cut at the boundaries the table already drew:
`interview.md` (the interview, inside a workspace opened first), `pre-flight-gate.md` (the launch
gate itself), `gate-harness.md` (the operator's `wf` mechanics) and
`round-discipline.md` (when the gate ends). No rule moved; only which reader
pays for it.

**A rule states the failure it answers; the gate, the counts and the round costs
live in `references/measured.md`, cited by `[M-nn]`.** The same test the manual
already applies to a NUMBER applies to the story behind it: an agent acts on "a
fix is the largest single source of blockers, so later rounds review the delta",
and not on which gate that was or how its rounds split. Provenance is kept, not
deleted — it is what makes a rule falsifiable instead of an assertion — but it is
read when a rule is about to be CONTRADICTED, not when it is being followed.
`make check-docs` fails on a citation with no entry and on an entry nothing
cites.

## Read before you act — by activity

| you are about to — or have just FINISHED one step and not chosen the next | read first |
|---|---|
| author or edit a workflow script | `references/authoring.md`, then `references/model-routing.md`. The script API itself — `agent()`, `parallel()`, `pipeline()`, `phase()`, resume — is the built-in `workflow-authoring` skill: load it too, because since Claude Code 2.1.248 the Workflow tool's own description no longer carries it, and this manual governs the gate around the API, not the API |
| author or edit the DECISION document — above all its ACCEPTANCE section | `references/interview.md`, which is how the document gets written at all, then `references/gate-checks.md` section D, before the first clause is written. Its rules are AUTHOR-side and this table reached them from nowhere: the rows naming the gate files are for REVIEWING and for BRIEFING a reviewer, so an author who does neither never opens them. Measured on the gate that added this row — an acceptance section written as prose test specifications rather than observations was rewritten in four consecutive rounds, killing 8 then 2 then 5 then 9 clauses while the document around it was settling, and the rule it broke was written out in section D the whole time |
| gate a run that READS its target rather than writing it — a code review, a findings report | `references/run-types.md`, before `wf init`: the type (`wf init -type review`) is chosen once and decides the emitted skeleton, the dry run's modes and fixture polarity, which labels `dead-reviews` kills, the post-run `--owns-diff` bar and the gate's third vector |
| open and RUN the gate workspace (`wf`) | `references/gate-harness.md` — the operator's half, in the order it is run: workspace, round, apply, record, lens, freeze, verify, the checks, the cost meter, close |
| decide whether the gate keeps going | `references/round-discipline.md` — barrier, delta rounds, the descent, the stop rule, fix-induced |
| finish one step and not know which is next — the decision gate has gone quiet, the script is not written, a round closed | `references/pre-flight-gate.md` — the only place the whole SEQUENCE is stated (-1 workspace first, -2 interview inside it, -1b round, 1 decisions, author the script, 0 static, 2b dry-run, 2 freeze, 3 reviewers, 4 close) and where in it you now are [M-38] |
| choose models or reasoning effort for agents | `references/model-routing.md` |
| review a script or gate its artifacts (the pre-flight gate) | `references/pre-flight-gate.md` + `references/gate-checks.md` |
| add per-section review gates inside a script | `references/section-gate.md` |
| estimate or cut cost, plan fan-out width | `references/cost-discipline.md` |
| brief a gate REVIEWER | `wf new-lens <name>` — it emits the FIXED half of the brief (the reading list, which points at the workspace's pinned `gate-checks.md` written by `wf init`, the three asks a delta round may never drop, and the output contract) and leaves the VECTOR as a marker line for you to write; `wf round lens` refuses to bank a prompt still carrying that marker |
| CONTRADICT a rule, or propose dropping one | `references/measured.md` — what that rule was measured on, cited from its text by `[M-nn]`. Never read to FOLLOW a rule; the rule is stated where it is stated |

## Invariants that hold regardless of activity

- Everything agent-facing is English, Latin script; quotation is the one
  carve-out. Gate artifacts ARE agent-facing.
- Every `agent()` call carries an explicit `model` AND an explicit `effort` —
  an omitted one inherits the launching session, so the script's cost is not
  written in the script. Producers inside a fan-out are cheap on both axes;
  opus and `high`+ are the exceptions that must argue for themselves, and
  haiku against sonnet is decided per role by the haiku test in
  `references/model-routing.md`.
- Gate REVIEWERS are **sonnet** — every lens of the pre-flight gate and of the
  decision gate before it, and every section-gate reviewer. Opus on a reviewer
  is a per-round exception that NAMES the miss it guards against and is banked
  in the round's roster (`wf round lens -model`). A reviewer's tier is paid once
  per ROUND, not once per gate, so the tier MULTIPLIES by the round count — read
  it off `wf stats -time`, per workspace, rather than from a figure quoted here —
  and the round nobody dispatches because the quota ran out finds nothing at all.
- An interview round with the operator goes through the `AskUserQuestion`
  tool, never as a numbered list in a message — `references/interview.md`
  step -2 says how a frontier maps onto it.
- No Workflow launches without the pre-flight gate: static check (`wfcheck`),
  dry-run (`wfdry`, BEFORE the freeze), freeze, `wf verify` green.
- The gate's bookkeeping goes through `wf` (workspace, rounds, apply, freeze,
  verify, delta, stats) — artifacts are never edited by hand.
- A fix is unreviewed code and the largest single source of blockers; later
  rounds review the delta (`wf delta`), not the original.

The tooling is versioned in `~/projects/wfgate`; `~/.local/bin/{wf,wfcheck,wfdry}` are three
version-stamped binaries `make install` copies there. A fix to a tool or a spec is a commit there.
