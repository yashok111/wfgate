# Workflow model routing

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
split out one fan-in opus agent instead. (Gate reviewers are exempt — the section-review gate's two
reviewers AND the pre-flight gate's reviewers, each governed by its own section below.)

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

