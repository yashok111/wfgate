# Run types: what the gated run does to its target

A gate is opened for a run, and the run either WRITES its target or READS it. The two share the
gate — workspace, rounds, freeze, the decision document, the pre-flight sequence — and almost
nothing inside the script: a build's harness is producers, bars, a bounded fix round and an
acceptance step, and a review's is finders, verifiers and a report. The type is chosen ONCE, at
`wf init -type`, banked in `gate.json`, and read from there by every command whose rules depend on
it. Read this file when the run you are gating changes nothing in its target.

The closed list is `build` (the default, and every workspace opened before the field existed),
`review` (a diff, read), `hunt` (a subsystem, searched, its serious findings reproduced) and `audit`
(a question, answered with cited claims). `wf init` refuses a type outside it, naming the list. A
hunt and an audit are read-only types in every row below unless their own sections say otherwise.

## What the type changes, and where

| | build | review (and hunt, audit) |
|---|---|---|
| `wf new-script` emits | producers → per-section gate → acceptance (`templates/script.mjs`) | find → verify → merge → report (`templates/review.mjs`; a hunt gets `templates/hunt.mjs`, an audit `templates/audit.mjs` — find → verify → WRITE) |
| `wfdry` modes | clean, blockers, acceptfail, unfixed, throw-serial, dead-reviews, no-args | clean, blockers, **empty**, throw-serial, **dead-finders**, dead-reviews, no-args — a hunt adds **dead-repro**, an audit **dead-writer** |
| a finding array in `clean` | EMPTY — it is a defect list | POPULATED with a mixed pair — it is the deliverable |
| a pass-shaped boolean (`verified`, `complete`) | true everywhere but `acceptfail` | true in the passing modes, false elsewhere |
| `dead-reviews` kills | review / fix / closeout / verify / accept labels | those, plus critic / skeptic — the finders LIVE |
| `dead-finders` kills | — | find / hunt / scan / audit / lens / inspect / analyze labels — the verifiers live, and a mode that kills nobody FAILS |
| `wfcheck --owns-diff` | an audit of the ownership map; exits 0 | a BAR: the tree must be unchanged and HEAD unmoved; exits 1 otherwise; the base must be a SHA; a hunt's REGISTERED reproduction worktrees under `.claude/worktrees/` are listed with their cleanup, not counted |
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
  `defects`, `problems` or (an audit's) `claims` — that is how `wfdry` knows which arrays a review returns; under any other
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

## A hunt: a subsystem, and reproduction

A hunt is a review whose input is a SUBSYSTEM at one commit rather than a diff, and whose serious
findings are proven by running something. What changes against the review rules above:

- **Units cover the subsystem BY PATH, and together they are all of it.** A review's one unit over
  the whole diff covers by construction; a subsystem does not fit one finder, so the units are the
  coverage claim, and a file in no unit is a file nobody searched. Lenses are FAILURE CLASSES (data
  loss, money, concurrency, error handling, security), disjoint as a review's are.
- **Blockers and majors are REPRODUCED; the rest are read.** A reproducer runs in a throwaway worktree
  (`isolation: 'worktree'` — `wfdry` refuses a hunt agent labelled `repro…` without it, in every
  mode), checks out `COMMIT` and confirms it — a worktree can arrive at another commit — writes the
  smallest failing test or command, runs it and reports the command, its output and what `git
  rev-parse HEAD` printed. A "reproduced" with no command, or with the commit unconfirmed, is listed
  as NOT reproduced, saying which was missing; the confirmation is the reproducer's own report, not an
  observation the harness can make. It calls git by absolute path (`authoring.md`, the worktree rule
  — `wf new-script` writes the path git resolves to on the emitting box), never commits, never writes
  outside its worktree. Minor and note findings get a reading skeptic, as in a review.
- **Not reproduced is not refuted.** A reproducer may lack the state, the data or the timing; its
  finding is listed apart as NOT REPRODUCED, with the reason, and neither confirmed nor dropped.
  Where nothing can be reproduced at all — a defect that needs production data, an external service
  — a vote of reading skeptics on the serious findings is the substitute; the skeleton does not carry
  one, and adding it is a decision for the document.
- **Reproductions are the costliest agents of the run** — one build per worktree — so they are
  capped (`MAX_REPRO`), worst first, and the report NAMES the serious findings that were read instead,
  keeping one a reader refuted apart from the ordinary refuted. The dry run's two serious findings
  never reach the default cap, so run it once more with `wfdry --args '{"maxRepro":1}'`, which the
  skeleton reads. Hand every reproducer the ONE focused command that runs a test here (`REPRO_HOW`): several
  whole-repo test runs at once is what an OOM killer resolves.
- **The tree is unchanged except for the worktrees.** Measured for an Agent-tool subagent spawned by
  a headless session [M-45]: its worktree lands under `.claude/worktrees/` of the repository the
  LAUNCHING session runs in — so launch the hunt from the target's root — and Claude Code's own
  description says it keeps a worktree whose agent changed something, which is every reproduction
  that wrote a test. A Workflow `agent()` was not measured separately. `wfcheck -type hunt --owns-diff
  <COMMIT> <script>` exempts exactly the REGISTERED linked worktrees there (`git worktree list`, so a
  gitignored `.claude/` does not hide one), untracked and not symlinks; a tracked file under the
  prefix, a loose file, or a symlink out of it counts as a write. It prints each worktree's cleanup,
  `git worktree remove --force <path> && git branch -D <branch>`, because the branch outlives the
  worktree.
- **What the bar cannot see** — and a reproducer holding the absolute path of `REPO` is the agent
  most able to reach it: gitignored paths, other refs, `.git` itself (hooks, config, `info/exclude`,
  index flags), and a HEAD detached at the base, which it reports as a note. The rule that a
  reproducer writes only inside its worktree is held by `isolation` and the prompt; the bar catches
  what lands in tracked or untracked files.
- **`dead-repro`** is the hunt's extra mode: only the reproducers die, the readers live, so a script
  that drops a dead reproduction from its results — masked in `dead-reviews` by the readers' deaths —
  is refused. It killing nobody is a note, not a failure: a hunt that verifies by vote has no
  reproducer.

Vector 3 of a hunt's gate is COVERAGE & PRECISION as for a review, plus REPRODUCTION: can a
reproducer actually run something here (is `REPRO_HOW` a command that works in a fresh worktree, and
does the subsystem build there), is the cap set against the expected finding count, does the report
keep "not reproduced" apart from "refuted" — and three things no runner can judge, read line by line:
the reproducer's prompt carries `REPRO_RULES` (the `COMMIT` checkout and its confirmation), never
`READ_ONLY`; the readers carry no `isolation`; and the harness below the skeleton's marker line is the
emitted one — re-emit with `wf new-script` and diff, because a status mapping or a `complete` edited
there passes every dry-run mode.

## An audit: a question, and an answer that cites

An audit answers a QUESTION about its target at one commit — does the code do what the spec says, do
the docs match the code, does a property hold — and its deliverable is a DOCUMENT, not a list of
defects. What changes against a review:

- **The question is split into PARTS, and the parts are the question.** A part is a sub-question one
  finder can answer from the code alone; a part nobody asked is a part the answer is silent on.
  Finders run per part and unit, as a review's run per lens and unit.
- **A finder returns CLAIMS, true or false of the code alike** — `holds`, `violated`, `partial` —
  each citing `path.ext:LINE#anchor`, the anchor an ASCII identifier ON the cited lines. The
  citation arrives as FIELDS — a path with an extension, an integer line and last line, an
  identifier of two or more characters — and the harness checks their shape before anything trusts
  them: a free-text line such as `L7` made a citation invisible to `check cite`, an anchor with a
  space was cut at the space and then matched, and a one-letter anchor matches almost any line. A
  claim that fails is MALFORMED, listed, never verified; a file with no extension is malformed too,
  because `check cite` does not verify one. "The code
  does what the spec asks" is as much an answer as "it does not"; an audit that returns only
  violations is a review that forgot its question. The array is `claims`, the deliverable's name.
- **Every claim's citation is read by a checker that did not write it**, and a claim the cited lines
  do not support as written is UNSUPPORTED: dropped from the answer, listed apart, never handed to the
  writer.
- **One WRITER composes the answer from the verified claims only.** It returns sentences, each naming
  the NUMBERS of the claims it rests on, and the harness appends their citations — so no citation in
  the answer is one a checker did not read. A sentence resting on no valid number is printed
  [UNCITED] and the answer is then not `answered`. A part of the question with no verified claim is
  listed as unsettled by the harness, whatever the writer says. With no verified claim nothing is
  written, and the report says UNANSWERED — and whether no claim was found or none survived.
  `wfdry` refuses an audit whose writer runs before any agent returned `verified: true`. `dead-writer` is
  the audit's extra mode: the writer dies, the claims stand, and a run calling itself complete without
  its answer is refused; a writer the runner cannot identify by a write/writer/synth/compose label is
  a violation, because the missing-answer path then runs in no mode.
- **`complete` is coverage and a writer that neither died nor came back empty; `answered` is
  apart.** An audit whose finders read everything and verified
  nothing covered its ground and answered nothing — for a review "found nothing" is a result, for an
  audit it is not, and the return says it in its own field.
- **The citations are checked mechanically after the run.** Save the returned `citable` — the answer
  and its verified claims, without the checkers' and finders' free text, which can quote
  citation-shaped prose — as `artifacts/answer.md` in the workspace, `wf add` it, and run `wf check
  cite -at <COMMIT> -require-anchor`: every citation resolves and its anchor is on the cited lines, or
  the check names the nearest line carrying it. It checks FORM and ANCHOR, not meaning: whether the
  lines support the statement is the in-run checker's judgement, a model's verdict like any other.
  And an unanswered audit has no citations, so "`check cite` clean" says nothing there — the
  acceptance is `answered: true` AND a citation count above zero AND the check clean.

Vector 3 of an audit's gate is COVERAGE & PRECISION as for a review, with the question in place of the
diff: do the parts together make up the question, can each be answered from code in the units, does
the finder's prompt ask for claims of BOTH polarities (and the `kind` enum keep a member of each),
does the writer's prompt forbid facts the claims do not carry, does the "Verified claims" section come
from the verified list alone — no runner sees any of those four in a dry run.

## The decision document of a review

The interview (`interview.md`) maps the same tree; its first frontier for a review is usually the
diff (base and head refs), the lenses, the units, who consumes the report and what they do with a
finding, and how deep verification goes (one skeptic each, or a vote on the top severities). The
acceptance section states OBSERVATIONS (`gate-checks.md` section D), and a review's are few: the
tree is unchanged (`wfcheck --owns-diff` exits 0), the report's population line names every cell,
every confirmed finding cites a location that exists at the reviewed revision. A hunt's first frontier
is the subsystem and its units, the failure classes, how a reproduction is run here and what it may
touch, and the cap; its acceptance adds that every REPRODUCED finding carries a command that fails at
`COMMIT` when re-run. An audit's first frontier is the question itself, its parts, the units that
can answer them, and who reads the answer; its acceptance is `wf check cite -at <COMMIT>
-require-anchor` clean over the saved `citable` with at least one citation counted, `answered: true`,
and every part either answered or listed as unsettled.
