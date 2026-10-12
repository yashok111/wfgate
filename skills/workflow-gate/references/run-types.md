# Run types: what the gated run does to its target

A gate is opened for a run, and the run either WRITES its target or READS it. The two share the
gate — workspace, rounds, freeze, the decision document, the pre-flight sequence — and almost
nothing inside the script: a build's harness is producers, bars, a bounded fix round and an
acceptance step, and a review's is finders, verifiers and a report. The type is chosen ONCE, at
`wf init -type`, banked in `gate.json`, and read from there by every command whose rules depend on
it. Read this file when the run you are gating changes nothing in its target.

The closed list is `build` (the default, and every workspace opened before the field existed) and
`review`. `wf init` refuses a type outside it, naming the list.

## What the type changes, and where

| | build | review |
|---|---|---|
| `wf new-script` emits | producers → per-section gate → acceptance (`templates/script.mjs`) | find → verify → merge → report (`templates/review.mjs`) |
| `wfdry` modes | clean, blockers, acceptfail, unfixed, throw-serial, dead-reviews, no-args | clean, blockers, **empty**, throw-serial, **dead-finders**, dead-reviews, no-args |
| a finding array in `clean` | EMPTY — it is a defect list | POPULATED with a mixed pair — it is the deliverable |
| a pass-shaped boolean (`verified`, `complete`) | true everywhere but `acceptfail` | true in the passing modes, false elsewhere |
| `dead-reviews` kills | review / fix / closeout / verify / accept labels | those, plus critic / skeptic — the finders LIVE |
| `dead-finders` kills | — | find / hunt / scan / audit / lens / inspect / analyze labels — the verifiers live, and a mode that kills nobody FAILS |
| `wfcheck --owns-diff` | an audit of the ownership map; exits 0 | a BAR: the tree must be unchanged and HEAD unmoved; exits 1 otherwise; the base must be a SHA |
| the gate's third vector | CRITERION | COVERAGE & PRECISION (below) |

`wfdry`, `wfcheck --owns-diff` and `wf new-script` read the type from the workspace that holds the
script — for `wf new-script`, the workspace the skeleton LANDS in — and SAY so on their first line
(`type review — from gate.json at …`); `-type` overrides it. Outside a workspace they fall back to
build in silence, so a review script dry-run from a scratch copy needs `-type review`.

**Why the dry run had to learn the type, and not only the skeleton.** Under the build reading a
review's happy path runs in NO passing mode: the passing fixture empties every finding-shaped array,
so `clean` finds nothing, spawns no verifier and reaches no merge, and the failure modes — the only
ones that do — are never compared against a baseline that walked the same path. Every mode still
reports OK [M-46]. A review's fixture flips that polarity: `clean` is "found some, of mixed severity,
all confirmed" — each finder's pair is mixed, and every second agent of a label family takes the
opposite top-level severity, so verifiers confirm a blocker and a minor alike; `blockers` is "found
some, every verifier refuted them"; `empty` is "found nothing"; `dead-finders` is "the finders returned
nothing"; `dead-reviews` is "the finders reported, the verifiers returned nothing". The two dead modes
are separate because one mode killing both killed the finders first, and then no verifier existed to
die.

Every failure mode of a review narrows by the runner's own injection — nothing to verify, or nothing
confirmed, and a review that stops after rounds with no confirmed finding stops early — so a review's
agent-count check is a NOTE, never a failure. The phases a mode never reached are named; read them,
because the width can no longer tell an abort guard from a correct early stop.

The verdict guards read the run's return. `dead-finders` and `dead-reviews` refuse a verdict that still
reads as success over dead agents; `empty` holds every pass word true, because nothing failed; `clean`,
in a review, holds only a COVERAGE word (`complete`, `done`, `finished`) — the fixture there IS
"found defects", so a verdict about the code (`clean`, `passed`) honestly reads false.

## Authoring a review script

Start from `wf new-script` inside the workspace; the skeleton passes both tools as it stands. What
it carries, and what each piece answers:

- **Read-only is held by the prompts, and checked by the tree.** Every prompt carries the
  `READ_ONLY` block; nothing else holds the property while the run is live. Start the run from a
  CLEAN tree and record its SHA; after the run, `wfcheck --owns-diff <that SHA> <script>` from the
  workspace is the observation that it held — a changed file, an untracked file the run created, or a
  moved HEAD fails it. A ref is refused as the base: it resolves at check time, and a run that
  committed onto it moved it too. What the bar does NOT see, and says on every run: gitignored paths,
  other refs (a new branch, a tag, a stash), and the difference between the run's writes and dirt that
  was already there.
- **Lenses are disjoint failure surfaces, units are slices of the diff.** One finder per lens and
  unit. Overlapping lenses find the same defect twice and the verifier pays for both; one unit over
  the whole diff is right until the diff is too large for one finder to read, and then units split
  it BY PATH. The prompt never carries the diff: each finder resolves `git diff BASE...HEAD` itself,
  because a shared blob is paid once per finder (`cost-discipline.md`).
- **Every finding is re-judged by an agent that did not write it, and the VERIFIER's severity is
  reported.** A finder rates its own find. The verifier is handed the claim and told to refute it
  by reading the code it cites — a verifier that cannot read the code is a second opinion on the
  prose, not a verification. Its boolean is named from the pass vocabulary (`verified`); `holds` or
  `refuted` are outside it, default to false in every mode, and leave one side of the confirm/refute
  split unwalked.
- **A finder reports what it did NOT read** (`gaps`), and a gap makes the review incomplete, named
  in the report. No dry-run mode walks a live finder with gaps — the array is failure-shaped, empty
  where the verdict guards look — so this term is held by the prompt and the gate, not the runner.
- **Duplicates are dropped per lens, not per location.** Two lenses at one line are two claims until
  the merge says they share a cause; the merge may only group ids it was handed.
- **The report states its population first.** `complete` is the review's COVERAGE, not its
  outcome: a review that found nothing over every cell is complete, and one that found ten while a
  finder died is not. "No finding was confirmed" over a dead cell or an unverified finding is
  printed as incomplete, never as clean — the absent-measurement rule, applied to the deliverable.
- **Results are matched to their cell by KEY.** A `parallel()` result filtered with
  `.filter(Boolean)` shifts positions when a thunk throws; matching by position would credit one
  finder's report to another's cell.
- **Name the deliverable array with the finding vocabulary** — the WHOLE name `findings`, `issues`,
  `defects` or `problems` — that is how `wfdry` knows which arrays a review returns; under any other
  name declare `"$comment": "x-pass: true"`, and `empty` fails a run in which it emptied nothing. An
  array that is a finding list in name and a defect list in meaning — a critic grading the report, or
  a gate-style reviewer schema carried over from a build — declares `"$comment": "x-pass: false"`
  (`authoring.md`, the fixture vocabulary) — otherwise the array comes back holding a blocker in the
  mode where its verdict says `pass`, and every branch on that contradiction runs in `clean`.
- **The verdict boolean is named from the pass vocabulary** (`complete`), so the `clean`, `empty`
  and `dead-reviews` verdict guards can read it: a review whose verdict survives its finders dying,
  or reads as failure when nothing was found, is refused by name.

## Gating a review script

The pre-flight sequence is the SAME (`pre-flight-gate.md`): static check, dry run, freeze, three
reviewers. A read-only run cannot damage its target, but it can spend a fleet and return a report
nobody can trust, and the rounds the read-only gates on record took were spent on exactly that —
not on mechanics. The skip rule's "pure read-only locate/extract" (`pre-flight-gate.md`, Rules) is
a single agent that finds a thing, not a fan-out that reviews one; the proportionality rule still
sets how many reviewers a small review script needs.

Vectors 1 (orchestration and cost) and 2 (routing) are unchanged; in a review, read amplification is
vector 1's main finding, because N finders each reading the whole diff is the default shape. Vector
3 becomes **COVERAGE & PRECISION**:

- **Coverage** — does every part of the diff reach at least one finder, is every lens's ask
  something a finder can act on, and does a dead finder surface as a named hole in the report and
  in `complete`?
- **Precision** — can the verifier actually refute (it reads the code, not only the claim)? Does
  the dedup key merge distinct defects, or keep one defect twice? Is the severity scale written for
  the report's CONSUMER — the author deciding to merge, a build gate the findings feed — rather
  than copied from this manual's gate scale?
- **The report** — ranked by the verifier's severity, every confirmed finding cites `file:line` at
  the reviewed revision, and the population line comes first.

## The decision document of a review

The interview (`interview.md`) maps the same tree; its first frontier for a review is usually the
diff (base and head refs), the lenses, the units, who consumes the report and what they do with a
finding, and how deep verification goes (one skeptic each, or a vote on the top severities). The
acceptance section states OBSERVATIONS (`gate-checks.md` section D), and a review's are few: the
tree is unchanged (`wfcheck --owns-diff` exits 0), the report's population line names every cell,
every confirmed finding cites a location that exists at the reviewed revision.
