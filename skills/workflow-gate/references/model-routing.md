# Workflow model and effort routing

No `CLAUDE_CODE_SUBAGENT_MODEL` env override — removed 2026-05-29 (it silently overrode per-call
`opts.model`, contrary to documented order, so a `model:'opus'` agent quietly ran on sonnet). Since
Claude Code 2.1.251 that variable is only a DEFAULT that an explicit per-spawn model beats, and the old
override lives on as `CLAUDE_CODE_SUBAGENT_MODEL_FORCE` (2.1.257) — so the failure no longer happens by
accident, but an environment can still force it. Do NOT re-add either; never rely on any env/session
default. Set the model EXPLICITLY, per agent. Since 2.1.223 a requested model the account may not use
prints a warning and something else runs: for a family alias (`'opus'`, `'sonnet'`) the newest model of
that family the account allows (2.1.222), for a full model id the PARENT's model. An `opts.model` is a
request, and a reviewer tier the roster banks is only what ran if no such warning appeared.

**HARD RULE — every `agent()` call MUST carry an explicit `model`**, chosen by the workflow-owner
agent per task. A model-less `agent()` is a bug; fix before running.

**Tier by what the subagent does:**
- **haiku** — every role that passes the HAIKU TEST below. Mechanical work passes it trivially
  (grep-and-report, rename, format-preserving tweak, log extraction, yes/no checks); a current haiku
  passes it for a wider class, which is why this is a test and not a list.
- **sonnet** — DEFAULT for ordinary work that fails the haiku test: localized edits, single-file TDD
  slices, test writing, routine review, and every role whose output nothing re-checks.
- **opus** — ONLY if ALL THREE hold (else → sonnet): (1) hard enough that sonnet plausibly errs — many
  *interacting* constraints, or intricate logic (subtle concurrency, invariant-heavy refactor, tricky
  algorithm); (2) the error is NOT cheaply caught downstream — if the section-review gate (below) or a
  cheap later step catches it → sonnet; (3) the error is costly to undo (ships a subtle defect, or
  poisons a stage nothing re-checks). Real cases: top-level architecture/design; a fan-in agent
  reconciling *genuinely conflicting* inputs, run ONCE (a mechanical merge/dedupe fails prong 1 →
  sonnet); adversarial verification of money-/data-loss/auth/crypto claims. "Multi-step", "cross-file",
  "synthesis", "ambiguous" are NOT triggers on their own — apply the three.

**The haiku test — haiku when ALL THREE hold, else sonnet.** It is the opus test turned around: there,
an error something cheap downstream catches is a reason NOT to upgrade; here it is the reason to
downgrade.
1. **A later step RE-JUDGES the output against its source before anything trusts it.** A verifier
   that re-reads the slice the haiku read, a later stage that confirms each candidate on its own, a
   build, test or `wfcheck` that fails loudly on a wrong answer. A schema checks SHAPE only, and a
   fan-in that merges outputs without re-reading their sources trusts them — neither counts. An
   output taken as truth by nothing that re-judges it fails: the fan-in's own synthesis, a gate
   reviewer's verdict (the verdict IS the check), a final report. An edit that lands in the tree
   passes only on paper — what catches a bad edit is a section-gate round, which costs more than the
   tier saved — so producers that write code stay sonnet, the mechanical edits above excepted.
2. **Its input is a NAMED SLICE, not an exploration.** Its prompt names every input it reads — paths,
   line ranges, records, one diff hunk — and asks for no search of its own; that is decidable from the
   script, a tool-call count is not. Price and capability point the same way: a subagent's prompt
   grows with every turn (system prompt, tools, everything read so far), haiku's price advantage
   shrinks sharply once a prompt gets long, and its gap to sonnet is widest on multi-step agentic work
   and narrow on single-answer tasks. An exploring haiku pays most of sonnet's price for a weaker
   answer.
3. **The answer is LOCAL.** It can be judged from the slice alone: does this cited line say what the
   sentence claims, which of these categories is this, extract these fields, map this file. An answer
   that depends on constraints spread across files — a cross-file invariant, a design choice,
   reconciling two reports — fails. So does a slice of offensive-security content (exploit
   techniques, attack paths): the current haiku's cyber safeguards are stricter than its predecessors', and a
   refusal is an answer a fan-in can read as "nothing found".

Where it pays is WIDTH: the per-item stage of a fan-out (check each citation, classify each finding,
triage each log, extract from each record) whose every answer the sonnet stage acting on it
re-judges; and the first half of a find-then-verify pair, where haiku proposes candidates over narrow
slices and sonnet confirms only the candidates. Recall is bought with DIFFERENT slices, not with N
haiku over the same one — duplicates on one slice are read-amplification (`cost-discipline.md` §1).
The test is derived from where an error is caught, from the price model and from the vendor's
positioning — Anthropic calls its haiku "best suited to more narrowly scoped tasks" and says sonnet
and opus "remain better choices for complex agentic coding tasks" — not from a run in this harness
comparing the two tiers on one role. Whether the claude.ai usage quota weighs haiku the way the API
price does is unmeasured too: a role the test placed on haiku that measurably misses goes back to
sonnet.

**HARD RULE — inside a fan-out, producers are cheap.** For agents *within* a `parallel()`/`pipeline()`
over N items (a standalone agent not in a fan-out — e.g. top-level design — is governed by the tier
list above): per-item producers are haiku where the haiku test passes and sonnet where it does not,
never opus. The ONLY opus-eligible role is a
SINGLE fan-in convergence agent that reads all N outputs, runs ONCE, and clears the three-prong gate (a
mechanical merge won't). A per-item *terminal* stage runs N times → NOT that agent → sonnet. N
same-role opus producers in parallel is banned — if you think you need it, the decomposition is wrong;
split out one fan-in opus agent instead. (Gate reviewers are exempt from the BAN — the section-review
gate's reviewers AND the pre-flight gate's reviewers, each governed by its own section below. The
exemption is not a licence: both of those sections set **sonnet** as the reviewer default and make opus an
exception that must name the miss it guards against, because a gate reviewer's tier is paid once per ROUND,
not once per gate, so the tier MULTIPLIES by the round count — `wf stats -time` prints it per workspace,
and it is what an operator's quota actually runs out on.)

**Tie-breaks & round-ups:**
- Unsure between two tiers → pick cheaper. Default sonnet; escalate ONE fan-in agent before upgrading a
  whole fan-out. The one exception is haiku against sonnet when you are unsure whether a later step
  re-judges the output: that is the haiku test's first condition failing, so it is sonnet.
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
  format-preserving tweak, a yes/no check, harvesting a log. The mechanical END of what the haiku test
  places on haiku, not all of it: a haiku judging whether a cited line supports a claim has something to
  weigh, and is `medium`.
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
single fan-in agent that runs once and the GATE REVIEWERS — the section gate's reviewers run in a
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

**The pre-flight gate's own reviewers carry `effort: 'xhigh'`, written on the `Agent` call.** They are
out-of-band `Agent` calls, and the `Agent` tool takes an `effort` since Claude Code 2.1.292; before
that its options were the model and the agent type, and a reviewer ran at its agent definition's effort,
else the reviewing session's — which is still what an omitted one does, the same reproducibility hole
the hard rule above closes for `agent()`. The tool sets it only when instructions such as a skill ask
for a level, so this paragraph is that ask. `xhigh` is not the tier list's `high`: every banked gate
round inherited its effort from the session that ran it, those sessions are configured at `xhigh`, and
no roster recorded the level, so `xhigh` keeps what was measured. Dropping a reviewer to `high` is a
measurement to run first, not a default to assume. Nothing checks an `Agent` call the way `wfcheck`
checks an `agent()`, so an omitted one is the operator's slip, not a finding against the script under
review.
