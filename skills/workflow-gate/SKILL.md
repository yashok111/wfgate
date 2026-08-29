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
the same text from the installed binary (`wf docs guide` concatenates the five
authoring references; the gate-checks list is `wf docs checks`, because it is a
reviewer's checklist rather than part of the manual's argument).

## Read before you act — by activity

| you are about to | read first |
|---|---|
| author or edit a workflow script | `references/authoring.md`, then `references/model-routing.md` |
| author or edit the DECISION document — above all its ACCEPTANCE section | `references/gate-checks.md` section D, before the first clause is written. Its rules are AUTHOR-side and this table reached them from nowhere: the two other rows naming that file are for REVIEWING and for BRIEFING a reviewer, so an author who does neither never opens it. Measured on the gate that added this row — an acceptance section written as prose test specifications rather than observations was rewritten in four consecutive rounds, killing 8 then 2 then 5 then 9 clauses while the document around it was settling, and the rule it broke was written out in section D the whole time |
| open and RUN the gate workspace (`wf`, `wfcheck`, `wfdry`) | `references/pre-flight-gate.md` — steps -1, -1b, 0, 2, 2b and 4 are the operator mechanics, 19 KB of its 54; step 1 and step 3 are the reviewer's half. The file is written in step order, which is NOT the order you execute |
| choose models or reasoning effort for agents | `references/model-routing.md` |
| review a script or gate its artifacts (the pre-flight gate) | `references/pre-flight-gate.md` + `references/gate-checks.md` |
| add per-section review gates inside a script | `references/section-gate.md` |
| estimate or cut cost, plan fan-out width | `references/cost-discipline.md` |
| brief a gate REVIEWER | hand them the workspace's `gate-checks.md` (written by `wf init`) |

## Invariants that hold regardless of activity

- Everything agent-facing is English, Latin script; quotation is the one
  carve-out. Gate artifacts ARE agent-facing.
- Every `agent()` call carries an explicit `model` AND an explicit `effort` —
  an omitted one inherits the launching session, so the script's cost is not
  written in the script. Producers inside a fan-out are cheap on both axes;
  opus and `high`+ are the exceptions that must argue for themselves.
- Gate REVIEWERS are **sonnet** — every lens of the pre-flight gate and of the
  decision gate before it, and both section-gate reviewers. Opus on a reviewer
  is a per-round exception that NAMES the miss it guards against and is banked
  in the round's roster (`wf round lens -model`). A reviewer's tier is paid once
  per ROUND, not once per gate: the nine gates on record spent 148 rounds, and
  the round nobody dispatches because the quota ran out finds nothing at all.
- No Workflow launches without the pre-flight gate: static check (`wfcheck`),
  dry-run (`wfdry`, BEFORE the freeze), freeze, `wf verify` green.
- The gate's bookkeeping goes through `wf` (workspace, rounds, apply, freeze,
  verify, delta, stats) — artifacts are never edited by hand.
- A fix is unreviewed code and the largest single source of blockers; later
  rounds review the delta (`wf delta`), not the original.

The tooling is versioned in `~/projects/wfgate`; `~/.local/bin/{wf,wfcheck,wfdry}` are three
version-stamped binaries `make install` copies there. A fix to a tool or a spec is a commit there.
