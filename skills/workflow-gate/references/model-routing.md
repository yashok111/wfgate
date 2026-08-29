# Workflow model and effort routing

No `CLAUDE_CODE_SUBAGENT_MODEL` env override — removed 2026-05-29 (it silently overrode per-call
`opts.model`, contrary to documented order, so a `model:'opus'` agent quietly ran on sonnet). Do NOT
re-add it; never rely on any env/session default. Set the model EXPLICITLY, per agent.

**HARD RULE — every `agent()` call MUST carry an explicit `model`**, chosen by the workflow-owner
agent per task. A model-less `agent()` is a bug; fix before running.

**Tier by what the subagent does:**
- **haiku** — trivial/mechanical: grep-and-report, rename, format-preserving tweak, log extraction,
  yes/no checks. No reasoning.
- **sonnet** — DEFAULT for ordinary work: localized edits, single-file TDD slices, test writing,
  structured extraction, routine review.
- **opus** — ONLY if ALL THREE hold (else → sonnet): (1) hard enough that sonnet plausibly errs — many
  *interacting* constraints, or intricate logic (subtle concurrency, invariant-heavy refactor, tricky
  algorithm); (2) the error is NOT cheaply caught downstream — if the section-review gate (below) or a
  cheap later step catches it → sonnet; (3) the error is costly to undo (ships a subtle defect, or
  poisons a stage nothing re-checks). Real cases: top-level architecture/design; a fan-in agent
  reconciling *genuinely conflicting* inputs, run ONCE (a mechanical merge/dedupe fails prong 1 →
  sonnet); adversarial verification of money-/data-loss/auth/crypto claims. "Multi-step", "cross-file",
  "synthesis", "ambiguous" are NOT triggers on their own — apply the three.

**HARD RULE — inside a fan-out, producers are cheap.** For agents *within* a `parallel()`/`pipeline()`
over N items (a standalone agent not in a fan-out — e.g. top-level design — is governed by the tier
list above): per-item producers are sonnet (or haiku), never opus. The ONLY opus-eligible role is a
SINGLE fan-in convergence agent that reads all N outputs, runs ONCE, and clears the three-prong gate (a
mechanical merge won't). A per-item *terminal* stage runs N times → NOT that agent → sonnet. N
same-role opus producers in parallel is banned — if you think you need it, the decomposition is wrong;
split out one fan-in opus agent instead. (Gate reviewers are exempt from the BAN — the section-review
gate's two reviewers AND the pre-flight gate's reviewers, each governed by its own section below. The
exemption is not a licence: both of those sections set **sonnet** as the reviewer default and make opus an
exception that must name the miss it guards against, because a gate reviewer's tier is paid once per ROUND
and a gate is 10-25 rounds — 148 of them over the nine gates on record.)

**Tie-breaks & round-ups:**
- Unsure between two tiers → pick cheaper. Default sonnet; escalate ONE fan-in agent before upgrading a
  whole fan-out.
- Auto round-up to opus ONLY for genuine money-/data-loss/auth/crypto reasoning. For an irreversible
  external action with no cheap rollback (prod deploy, fund transfer, destructive data op), round up the
  agent that *decides/verifies* it, NOT the one that merely executes — and NOT for ordinary git/PR/
  message actions (recoverable) or "this code ought to be correct" (all code does).
- Soft target: **≤1 opus producer per section** (section unit as in the review-gate section); over that,
  `log()` which opus case applies. Section-gate reviewers and the pre-flight gate are budgeted
  separately (their sections) — a genuinely security-critical section may legitimately carry an opus
  producer *and* an opus reviewer; that is intended, not a leak.

State the model on every `agent()` and mirror it on the matching `meta.phases[]` entry. Resolution
order (no env): per-call `opts.model` > `meta.phases[].model`.

The mirror rule holds only for a **single-tier** phase. A phase that deliberately mixes tiers — a review
phase with one top-tier and one default-tier reviewer is the common case — has one metadata slot and two
models, and mirroring is not expressible: leave `meta.phases[].model` off that entry and name the tiers
in its `detail` string instead. Do not flatten a phase to one tier just to satisfy the metadata.


---

## Effort routing

`opts.effort` (`'low' | 'medium' | 'high' | 'xhigh' | 'max'`) sets how much reasoning a subagent spends
per call. It is a SECOND axis, orthogonal to `model`: the model is the capability class the task needs,
the effort is how much deliberation is spent reaching the answer. A cheap model at high effort and a top
model at low effort are both legitimate assignments — coupling the two axes, so that every opus call is
also `max` and every haiku call is `low`, is the common authoring error and it costs on the wide side of
a fan-out.

**HARD RULE — every `agent()` call MUST carry an explicit `effort`**, for the same reason it must carry a
`model`. An omitted `effort` inherits the LAUNCHING SESSION's setting: the same script then costs a
multiple of itself depending on who ran it and what they last typed into `/effort`, and neither the
author nor the pre-flight gate can read the run's cost off the script it is reviewing. An effort-less
`agent()` is a bug; fix before running. Resolution order: per-call `opts.effort` > session effort, and
nothing else — there is no `meta.phases[].effort` to mirror it on, so a phase that deliberately mixes
efforts names them in that entry's `detail` string, exactly as a mixed-tier phase names its models.

**Tier by how the answer is REACHED, not by how important the step is:**
- **low** — mechanical, single pass, nothing to weigh: grep-and-report, extraction, rename, a
  format-preserving tweak, a yes/no check, harvesting a log. The same population the tier list gives to
  haiku.
- **medium** — DEFAULT for ordinary producers: a localized edit, a single-file TDD slice, test writing,
  structured extraction, routine review.
- **high** — a step whose error is NOT cheaply caught downstream: a section-gate reviewer, adversarial
  verification, the single fan-in agent reconciling genuinely conflicting inputs, a top-level design
  call.
- **xhigh / max** — a step that runs ONCE, that nothing re-checks, and whose one wrong answer burns the
  fan-out. `max` is an escalation the script argues for in a comment or a `log()` line; a per-item
  fan-out agent never gets it.

**Inside a fan-out, effort is cheap for the same reason models are.** Per-item PRODUCERS under
`parallel()`/`pipeline()` are `medium`, or `low` where the item work is mechanical — never above. N
same-role high-effort producers in parallel is the same defect as N opus producers, and it bites
harder: effort multiplies the output tokens of every one of the N. The high-effort roles are the
single fan-in agent that runs once and the GATE REVIEWERS — the section gate's two reviewers run in a
`parallel()` barrier by design (see `section-gate.md`), and they carry the same exemption the model
half of this document grants them. The ban is on producers, not on everything that happens to sit
inside a `parallel()`.

**Unmeasured, and stated as such.** No gate has yet run one script at two efforts and compared the
findings, so the tiers above are derived from the cost model — reasoning tokens are output tokens,
billed per call and multiplied by the fan-out width — and from the reproducibility hole, not from a
measurement of quality lost. Until a run measures it, an over-powered effort is treated the way an
over-powered model is: a downgrade filed as a NOTE, never a blocker ("advisory" here means it does not
loop the gate; the LEVEL you file is on the closed BLOCKER/MAJOR/MINOR/NOTE scale, and over-spend is by
this section's own words not a defect — file MINOR where the row names a concrete cost). A MISSING effort is a blocker, because
that is not a tier judgement — it is a script whose cost is written in someone else's session.

**The pre-flight gate's own reviewers carry no effort knob.** They are out-of-band `Agent` calls, whose
options are the model and the agent type; their depth comes from the reviewing session and from the
agent definition. Do not report a missing `effort` on them — and do not drop the session's effort while
a gate round is running, because that is the one place the setting silently reaches.
