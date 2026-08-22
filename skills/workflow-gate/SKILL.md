---
name: workflow-gate
description: MANDATORY before authoring, reviewing, or running ANY Workflow (multi-agent orchestration) script, and before reviewing a workflow gate's artifacts. Maps the operating manual — model routing, the pre-flight review gate, the wf gate harness, cost discipline — and names which reference to read for the activity at hand. Trigger on "Workflow tool", "workflow script", "orchestration", "fan-out", "wf gate", "pre-flight review".
---

# Workflow gate

The Workflow tool spawns fleets of agents; an unreviewed script burns the whole
fan-out. This skill is the operating manual's front door. The manual is the
specification a gate reviewed — do not act from memory of it; read the
reference for what you are about to do, in full.

All references live in `references/` next to this file; `wf docs <name>` prints
the same text from the installed binary (`wf docs guide` = the whole manual).

## Read before you act — by activity

| you are about to | read first |
|---|---|
| author or edit a workflow script | `references/authoring.md`, then `references/model-routing.md` |
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
- No Workflow launches without the pre-flight gate: static check (`wfcheck`),
  dry-run (`wfdry`, BEFORE the freeze), freeze, `wf verify` green.
- The gate's bookkeeping goes through `wf` (workspace, rounds, apply, freeze,
  verify, delta, stats) — artifacts are never edited by hand.
- A fix is unreviewed code and the largest single source of blockers; later
  rounds review the delta (`wf delta`), not the original.

The tooling is versioned in `~/projects/wfgate`; `~/.local/bin/{wf,wfcheck,wfdry}`
are what `make install` maintains. A fix to a tool or a spec is a commit there.
