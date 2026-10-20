# wfgate eval suite

What this measures: whether the `workflow-gate` skill CHANGES an agent's answer,
not whether it loads. `claude plugin eval` runs every case twice by default —
once with the plugin and once without — and reports the delta. A case whose two
arms score the same is a case the plugin did not earn.

## Running

```bash
claude plugin eval .                      # all four cases, 3 runs each, both arms
claude plugin eval . --runs 1             # the cheap pass while editing a case
claude plugin eval . --case 'reviewer*'   # one case
claude plugin eval . --json eval.json     # machine-readable, for a record
```

Each run is a full `claude` child process on your own credential; four cases at
three runs is twenty-four of them. `--max-cost-usd <n>` is the hard ceiling and
exits 2 on breach. `-j 2` halves the wall clock and shares one rate limit.

No case needs `--allow-tools`: every prompt asks for the answer in the reply
rather than on disk, so the suite runs under the default read-only grant. Adding
a case that writes a file means granting `Write` and accepting that the
`Write|Edit` authoring hook will fire inside it.

## The cases

| Case | What it is for |
|---|---|
| `skill-fires-on-authoring` | The manual is read before the FIRST LINE, not before the launch. Asks for a script; checks every `agent()` carries an explicit model. |
| `reviewer-model-sonnet` | The one rule an agent cannot guess: reviewers run on sonnet, opus is an exception that names its miss. The strongest with/without delta in the suite. |
| `routing-names-one-ref` | The skill's stated job is to name ONE reference, not to hand over the manual. Checks it routes an operator to `gate-harness.md`. |
| `no-false-fire` | Precision. In the plugin's subject area, nothing to do with orchestration. |

## Reading the result

`aggregates.delta.improvement` is the number the suite exists for.

Graders marked `arm: with-only` — the three `skill-loaded` ones — are plugin-fired
INDICATORS, not part of the score. They answer "did the skill trigger at all",
which is a different question from "did it help", and scoring them would credit
the plugin for loading itself.

`no-false-fire` is the one case where a NEGATIVE delta is the finding. Its
`skill-not-loaded` grader is scored in both arms, and the without arm passes it
trivially because it has no skill to load. So that case's delta is zero when the
skill stays out of the way and negative when it fires on work it has nothing to
say about — which is how a MANDATORY-worded description stops being read.

An absent measurement never prints in the register of a measured one: a delta
over one run is not a delta. Keep `--runs 3` for anything banked.

## First run, 2026-09-20 — and the case it broke

Four cases at `--runs 1`, both arms, 549 s, $2.46, `partial: false`, plugin
1.76.1. Mean delta +0.354; three of four cases passed against the default
threshold of 1.0.

| Case | with | without | delta |
|---|---|---|---|
| `routing-names-one-ref` | 1.00 | 0.00 | +1.00 |
| `reviewer-model-sonnet` | 1.00 | 0.25 | +0.75 |
| `no-false-fire` | 1.00 | 1.00 | 0.00 |
| `skill-fires-on-authoring` | 0.00 | 0.33 | -0.33 |

The two positive deltas are the suite doing its job, and both are specific: the
without arm never named `gate-harness.md` at all, and on the sonnet rule it said
the word (`says-sonnet` passed) without stating the rule or its exception
(`rule-and-reason` failed).

**`skill-fires-on-authoring` measured its own timeout, not the script.** The
WITH arm died at 302 s against the 300 s default, on turn 6 of 12 — time-bound,
not turn-bound — having loaded the skill and gone off to read the references.
It scored 0 against a WITHOUT arm that finished in 89 s and scored 0.33, so the
delta printed -0.33: a 0 for never answering, in the register of a 0 for
answering badly. The case now carries `timeout_seconds: 900` and `max_turns:
20`. Reading the manual and then writing a whole orchestration script is the
most expensive thing this plugin asks of an agent, and a case that cannot pay
for it measures the budget instead. Since Claude Code 2.1.299 a run that ends on
an API error scores 0 WITHOUT grading and the suite exits 1 — the register
problem above, now at least named by the exit code; a time-bound 0 is still a
graded one.

The WITHOUT arm is the reason the case is worth re-running rather than
re-designing: it produced a script (`meta-block` passed) whose `agent()` calls
did not all carry an explicit model (`explicit-model-per-agent` failed), which
is exactly the delta the case was written to catch.

**One run is not a measurement.** These four numbers come from a single run per
arm; `runs: 3` is the default for anything banked.

## Second run, 2026-09-20 — the budget held, the grader did not

`--case 'skill-fires*' --runs 1`, both arms, 514 s, $1.85. No timeout: the WITH
arm finished in 445 s over 12 turns under the new 900 s budget, so the case
measured the script this time. Both arms scored 0.333 — `meta-block` passed,
`explicit-model-per-agent` failed — and the delta printed 0.

Read against the scripts, that zero is wrong in one arm:

| Arm | Call sites | `model:` | Judge | True |
|---|---|---|---|---|
| with | 4 × `spawn(...)`, one `agent(prompt, opts)` inside the helper | 4, plus 4 `effort:` | FAIL 3/3 | every call explicit |
| without | 2 × direct `agent(` | none | FAIL | correct |

The WITH arm obeyed the rule and the grader could not see it. Its judge looked
for a model on the literal `agent(` line, found the wrapper, and voted FAIL
three times out of three — while the options objects at the four `spawn` call
sites each carried `model: 'sonnet'` and an explicit `effort`.

The wrapper is not the script freelancing: the manual prescribes it, and the
authoring hook's own reminder text names it ("the spawn wrapper turns a throw
into null"). So the plugin's prescribed shape defeated the grader written to
check the plugin. This repository has the class banked one layer down — factory
wrappers invisible to `wfcheck`, the 26th wave — and it was rebuilt here in a
prompt.

`explicit-model-per-agent` now tells the judge to resolve helpers to their call
sites before judging, and says the `phases` array is documentation rather than a
call site. The WITHOUT arm's failure was honest and is expected to stand, so the
case should discriminate once the judge can see through the wrapper. Re-running
it is what shows that; until then this case has measured nothing about the rule.

## Third run, 2026-09-20 — and the judge retired

Same case, same shape: 470 s, $2.21, both arms 0.333, delta 0 again. The
rewritten criteria moved the judge's votes from 3-0 FAIL to 2-1 FAIL and no
further. Read by hand, the WITH arm's script was correct for the third time —
four `spawn` call sites at lines 230, 269, 300 and 357, each carrying
`model: 'sonnet'` and an explicit `effort`, over a wrapper that forwards
`model: opts.model`.

So the grader was removed rather than rewritten again. The assumption under it
was wrong: an LLM judge reading a transcript cannot do the static resolution
this needs, and no wording fixes that. The manual prescribes the `spawn`
wrapper, which means the one literal `agent(` line in a CORRECT script never
carries a model — the instrument that resolves that is a parser, and this
repository already ships it as `wfcheck` over `internal/wfscript`.

`explicit-model-per-agent` is replaced by two regex graders — `model-at-call-site`
(weight 2) and `effort-at-call-site` (weight 1), the pair the authoring hook's
reminder names in one breath. They check something WEAKER than the rule: that a
model and an effort are named at all, not that every call carries one. Each
grader says so in its own body, because a check that reads as coverage it does
not have is the failure this whole repository is built against.

The weaker check earns its place by separating the arms on every script the
suite has produced: `model` present in both WITH runs (8 and 9 occurrences) and
absent in both WITHOUT runs, same for `effort` on the run where it was counted.
Both patterns were replayed against all four saved scripts — 4 of 4 correct,
with no eval run spent.

Expected on the next run: with 1.00 (2+1+1 of 4), without 0.25 (meta-block
alone), delta +0.75. The stronger claim stays unmeasured, and a case that wants
it has to hand the script to `wfcheck` rather than to a judge.

## Fourth run, 2026-09-20 — the case passes

567 s, $2.21. WITH 1.00 (every grader), WITHOUT 0.50, delta **+0.50**.

The prediction written above was +0.75 and it was wrong. The WITHOUT arm matched
`\beffort:\s*\S` this time while still matching no `\bmodel:` — the grader
explanations say exactly that — so it scored `effort` and `meta-block` rather
than `meta-block` alone. The 0.25 came from the two earlier WITHOUT scripts,
which named no effort at all; that arm varies from run to run, and a floor
derived from two samples is not a floor.

The suite as it stands, one run per arm each, cases 2-4 from the first run and
case 1 from this one:

| Case | with | without | delta |
|---|---|---|---|
| `routing-names-one-ref` | 1.00 | 0.00 | +1.00 |
| `reviewer-model-sonnet` | 1.00 | 0.25 | +0.75 |
| `skill-fires-on-authoring` | 1.00 | 0.50 | +0.50 |
| `no-false-fire` | 1.00 | 1.00 | 0.00 |

Four runs of the harness cost about $8.73 in total, three of them spent on one
case that was measuring its own budget and then its own judge rather than the
plugin. That is the cost of building the instrument, not of using it.

These numbers are still one run per arm. `--runs 3` is what makes them a
measurement.
