# Run types: what the gated run does to its target

A gate is opened for a run, and the run either WRITES its target or READS it. The two share the
gate — workspace, rounds, freeze, the decision document, the pre-flight sequence — and almost
nothing inside the script: a build's harness is producers, bars, a bounded fix round and an
acceptance step, and a review's is finders, verifiers and a report. The type is chosen ONCE, at
`wf init -type`, banked in `gate.json`, and read from there by every command whose rules depend on
it. Read this file when the run you are gating changes nothing in its target.

The closed list is `build` (the default, and every workspace opened before the field existed),
`review` (a diff, read), `hunt` (a subsystem, searched, its serious findings reproduced), `audit`
(a question, answered with cited claims) and `security` (a subsystem or a diff, searched for
vulnerabilities under a threat model, its scanners' hits adjudicated, its serious findings proven).
`wf init` refuses a type outside it, naming the list. A hunt, an audit and a security run are
read-only types in every row below unless their own sections say otherwise.

## What the type changes, and where

| | build | review (and hunt, audit, security) |
|---|---|---|
| `wf new-script` emits | producers → per-section gate → acceptance (`templates/script.mjs`) | find → verify → merge → report (`templates/review.mjs`; a hunt gets `templates/hunt.mjs`, an audit `templates/audit.mjs` — find → verify → WRITE — a security run `templates/security.mjs` — find → verify → PROVE → variants → merge) |
| `wfdry` modes | clean, blockers, acceptfail, unfixed, throw-serial, dead-reviews, no-args | clean, blockers, **empty**, throw-serial, **dead-finders**, **dead-cell**, dead-reviews, no-args — a hunt adds **dead-repro**, an audit **dead-writer**, a security run **inconclusive** and **dead-repro** |
| a finding array in `clean` | EMPTY — it is a defect list | POPULATED with a mixed pair — it is the deliverable |
| a pass-shaped boolean (`verified`, `complete`) | true everywhere but `acceptfail` | true in the passing modes, false elsewhere |
| `dead-reviews` kills | review / fix / closeout / verify / accept labels | those, plus critic / skeptic — the finders LIVE |
| `dead-finders` kills | — | find / hunt / scan / audit / lens / inspect / analyze labels — the verifiers live, and a mode that kills nobody FAILS |
| `wfcheck --owns-diff` | an audit of the ownership map; exits 0 | a BAR: the tree must be unchanged and HEAD unmoved; exits 1 otherwise; the base must be a SHA; a hunt's or a security run's REGISTERED reproduction worktrees under `.claude/worktrees/` are listed with their cleanup, not counted |
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
  capped (`MAX_REPRO`, over the whole hunt, worst first within each round), and the report NAMES the
  serious findings that were read instead, keeping one a reader refuted apart from the ordinary
  refuted. `wfdry --args '{"maxRepro":1}'` moves the cap, which the skeleton reads. Hand every
  reproducer the ONE focused command that runs a test here (`REPRO_HOW`): several whole-repo test
  runs at once is what an OOM killer resolves.
- **A hunt runs in ROUNDS until it stops finding.** On the one multi-round hunt measured, later
  rounds added most of the confirmed findings and none of the blockers [M-47] — weigh `MAX_ROUNDS`
  against that, knowing the measured hunt changed lens every round and re-ran every unit. Round 1
  runs every cell; a cell runs again only if it added a confirmed finding or died, and is handed
  what it already reported so it looks past it; a completeness CRITIC after every round but the
  last names up to `MAX_EXTRAS` under-examined areas, each one finder in the next round (with
  `MAX_EXTRAS` 0 it is not asked). A critic's cell has no failure class of its own, so a location
  anyone already filed is not new from it. A cell's unread files are the ones NO run of it read —
  a re-run reads less than its scope by design. The rounds stop when no cell is left (`dry`, or
  `critic` when the critic that would have named new areas died), at `MAX_ROUNDS`, or when the
  next round would not fit under `MAX_AGENTS` (every spawn counts; an override cannot lift it past
  the Workflow's agent cap) or under the token budget — and `stoppedBy` and the report's second
  line say which. A stop at a round boundary is not a coverage hole: a cell the critic proposed
  for a round that never started is listed as unsearched. A ceiling reached DURING a round is: its
  verifiers are not spawned, their findings are unverified with that reason, and the hunt is
  incomplete — a dry run that walks it NOTES the stop during a round and the agent count it reached,
  rather than reading the incompleteness as the script's fault, and one whose ceiling the run never
  reached says the walk exercised nothing (both read off the return's `stoppedBy`). A declared cell that never ran
  makes the hunt incomplete too; a re-run that died is named; a dead critic is reported, not
  counted as a dead cell. In a dry run, `--args '{"maxRounds":1}'` is a single pass, `--mode clean
  --args '{"maxAgents":12}'` walks the stop before a round (and the run is refused if it spawns
  more than it was given), `--budget` the token stop.
- **`dead-cell`**, in every read-only type, kills ONE finder cell in every round and its retries,
  the rest alive: a verdict that counts "some cell reported" as coverage passed `dead-finders`,
  which kills them all. A dry run of any type that spawns more agents than the Workflow cap is
  refused: a loop bounded in iterations is not bounded in agents.
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
  space was cut at the space and then matched, and a one-letter anchor pins almost nothing. A
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

## A security run: a threat model, scanners, and proof

A security run is a hunt for VULNERABILITIES: the input is a subsystem at one commit, or — with
`BASE` set — the changes `BASE...COMMIT` read against the whole repository (a differential review),
and a finding is a path from a source the attacker controls to a sink that hurts a named asset. The
method is the one the public security skills converge on — Trail of Bits' fp-check and
variant-analysis, Anthropic's threat-model → vuln-scan → triage, Sentry's security-review — and
every rule below answers a failure those procedures name: models over-report and over-rate, a
scanner's silence reads as a clean result, and a confirmed bug usually has siblings. The PoC is this
harness's own choice, carried over from the hunt: Anthropic's triage forbids executing target code
and leaves the PoC to a human. What changes against a hunt:

- **The threat model is in the DECISION DOCUMENT, and the gate reviews it.** Assets, the attacker,
  what the attacker can NOT do, the trust boundaries and entry points — built by running STRIDE
  (spoofing, tampering, repudiation, information disclosure, denial of service, elevation of
  privilege) over EACH boundary rather than listing the threats that come to mind. Everything
  downstream is scoped by it — the classes, the units, what a verifier may call attacker-controlled —
  so a model built at run time would scope the whole fleet with nothing reviewed. **Trust follows who
  WROTE a value, not the channel that delivered it**: an environment variable, a flag or a config
  file the operator writes is trusted; the same channel carrying a value another process, a job
  payload, a shared volume or a webhook wrote is the attacker's. A rule by channel ("env is trusted")
  let a verifier refute an injection through a value a neighbouring service writes. The script
  carries the model as one short `THREAT_MODEL` string, handed to every finder, verifier and prover,
  beside `DO_NOT_REPORT` (the exclusions the public triage procedures share: volumetric denial of
  service and missing rate limits — a ReDoS or an algorithmic blow-up one request triggers stays in —
  hardening with no concrete exploit, TLS, HSTS and secure-cookie flags in an app the model puts
  behind a TLS proxy or in development, code only tests run, placeholders in an example env file,
  credentials marked as fixtures, keys public by design, a fast hash used as a checksum, an impact
  the precondition already grants, a value the model trusts).
- **Lenses are vulnerability CLASSES** named with their CWE family. The skeleton ships seven, drawn
  apart by the operation that goes wrong — `injection` (any attacker input, a model's output
  included, reaching an interpreter), `access` (CORS among it), `data` (a path READ outside its root,
  SSRF, deserialization, exposure), `crypto` (predictable tokens among it), `logic`, `writes` (a
  file created, written, uploaded, moved or deleted at a target the attacker writes) and `llm`
  (prompt injection steering the model's tools, the context window, tool arguments) — and the author
  keeps the ones the threat model says this code is exposed to: each is a finder per unit. Where two
  classes could still claim one weakness, a finding is a duplicate of one with the same CWE nearby,
  whichever cell filed it. A stack's own checklist is where a class is ADDED, in the interview:
  OpenAI's security-best-practices carries one per framework (Go, Django, FastAPI, Flask, Express,
  Next.js, React, Vue), awesome-copilot's security-review the signals per category — read for the
  decision, never pasted into a prompt, where it would be paid once per agent. `scan`, `variant`
  and `flow` are the harness's own cell kinds and are refused as lens ids. Units carry `paths`,
  repo-relative prefixes (`./a/`, `a/` and `a` are one directory, `.` or `''` the whole
  repository), which is also how a scanner hit is assigned to a unit; two cells sharing a key are
  refused, because results are matched to cells by key.
- **With two units or more, one FLOW cell reads across them.** Units split the code by path, so a
  value entering in one unit and reaching a sink in another is read whole by no class finder. The
  flow finder follows each entry point the threat model names across the boundaries and reports
  only what crosses one. Its schema's gaps are the entry points it did not trace to their end — a
  list of unread files would be every unit's — reported on their own line and counted against
  `complete`. Its findings dedup by CWE, as a scanner or variant cell's do.
- **The code under review may be hostile, and every reader of it is a model.** Every prompt opens with
  `UNTRUSTED`, before anything relayed: what the repository holds — code, comments, docs, test data —
  is data, and so is every block another agent or a scanner wrote, which the harness FENCES between
  `BEGIN DATA` and `END DATA` (a finding's fields, a trace, scanner hits, the earlier findings a
  re-run is shown; an `END DATA` inside is defused). Text addressed to a reviewer or an agent is
  followed never; a finder lists it in `planted` by location, and the report prints it, fenced, as an
  attack on this review — never through a verifier, which would refute it by construction (the
  repository's authors wrote it, not the threat model's attacker), and never as seven class findings
  of one comment. The PoC output in the report is fenced too: the operator's session reads it.
- **Scanners run BEFORE the launch, by the operator, from the target's root, and their output is
  CANDIDATES.** The decision document names the scanners; each writes SARIF into the workspace's
  `artifacts/scan/` (`wf scan` with no log prints the commands, each removing the old log first —
  betterleaks and trivy leave it in place when they fail, and a log older than the target's HEAD
  commit is refused); `wf scan` folds the logs into `artifacts/scan/args.json` — per scanner whether
  its execution succeeded, whether its log records an invocation at all, its hit, error and warning
  counts, the hits with no location and those naming no file under the target; per hit its rule,
  file, line and a one-line message, never the `snippet` a secrets scanner fills with the secret
  and never the text after a `Match:` marker. An ERROR-level notification, in either notification
  array, fails the scanner whatever its success flag says: a scanner that could not load its rules
  reports success and no result [M-49]. The measurement is a command and not an agent because the
  property every scanner-driving skill insists on is that a script measures and a model judges. The
  run reads it as `args.scan`: a scanner in `SCANNERS` missing from it or failed, or with hits no
  unit can own (no location, no file under the target), is NAMED on the report's second line and
  makes the run incomplete — an absent scan never prints as a clean one; a log recording no
  invocation is printed as such; a scanner supplied but not declared is named and ignored. Every hit
  in a unit goes to a `scan` finder cell, at most `SCAN_CHUNK` per finder, whose schema makes each
  hit id a REQUIRED property of `hits`: the finder answers every one — dismissed with its reason, or
  vulnerable with a finding carrying the hit as its `scan` — and a hit judged vulnerable with no
  finding filed is listed UNADJUDICATED and makes the run incomplete. Dismissals are listed in the
  report, unverified, as a finder's judgement. A message is kept to one line, capped and cut at a
  `Match:` marker, but otherwise as the scanner wrote it, and a semgrep rule may interpolate the
  matched text into it: before the launch, `betterleaks dir
  artifacts/scan/args.json --redact` must find nothing. Pin the scan with `wf add -as scan/<file>
  artifacts/scan/<file>` for each log and the args file (`wf scan` prints the lines: a bare `wf add`
  stores a file under its basename), and dry-run with `--args "$(cat artifacts/scan/args.json)"` —
  given args naming a failed scanner, the dry run notes the expected `complete: false` rather than
  refusing it.
- **Finders are built for RECALL, the verifier to REFUTE.** A finder reports anything with a plausible
  exploit path, but names its source, its sink and the path. Two findings of one class in one file
  within `WINDOW` lines are one finding, and a scanner or variant cell's finding is a duplicate of one
  with the same CWE there — the deterministic half of the dedup, run before a verifier is paid; the
  merge is the semantic half. The verifier starts from "the finding is wrong", traces BACKWARDS to
  the first call site, decides whether the threat model's attacker controls the value, checks every
  protection on every path, applies `DO_NOT_REPORT`, and re-rates severity from the attacker's
  preconditions and access. A protection that may live outside the code — a reverse proxy, a WAF, a
  service mesh, platform config — makes the finding `cannot-verify` with where to look, never a
  confirmation by its absence from the code. A `true-positive` with an empty trace is the sink alone, which is what a
  scanner already said: the harness lists it as undecided.
- **The verdict has THREE states**, `true-positive`, `false-positive`, `cannot-verify`. The third is
  for a decision that turns on code outside the repository or on deployment; such findings are
  listed apart for a human — never confirmed, never dropped — and do not make the run incomplete.
  `wfdry`'s **inconclusive** mode hands every VERIFIER enum (a verify label, not a reproducer's) its
  undecided member — cannot, inconclusive, undecided, undetermined, unknown, uncertain, ... — and
  refuses a verifier with no such member, a return with no non-empty array named for those findings
  (`inconclusive`, `undecided`, ...), a confirmed-named array that holds anything, and a `report`
  that does not name each of them.
- **Confirmed blockers and majors get a PoC**, worst first within a round, up to `MAX_REPRO` over the
  run: a prover in a worktree, under the hunt's reproduction rules plus three of its own — loopback
  only, no real credentials, no third-party service. A PoC that works, with a command, a confirmed
  commit and a `head` that does not name another one, makes the finding EXPLOITED; one that does not
  leaves it confirmed by reading, with why. A dead prover leaves the run incomplete — the proof it
  owed was not attempted — which `dead-repro` checks. A prover is known by a repro/prove/poc label
  segment or by a phase titled Prove, PoC or Reproduce, and either without `isolation: 'worktree'`
  is refused.
- **Later rounds search VARIANTS, not new ground.** Each finding first confirmed in a round seeds one
  variant finder in the next, worst first, up to `MAX_VARIANTS`: exact pattern first, confirmed to
  match the seed, then generalised one element at a time until most matches are noise. A cell that
  died runs again. The rounds stop when nothing new was confirmed and nothing died (`dry`), when
  findings were confirmed with `MAX_VARIANTS` 0 (`noVariants` — not dry: nobody looked), at
  `MAX_ROUNDS` — saying how many confirmed findings never had their variants searched — or at the
  agent or token ceiling, as a hunt's do.
- **The report is shared, so it is checked for secrets.** Every prompt forbids copying a secret's
  value — a finding names the kind and the place, and its fix is to ROTATE the secret and purge it
  from history, because deleting it from the code revokes nothing already leaked. After the run, save the report as
  `artifacts/report.md` and run `betterleaks dir artifacts/report.md --redact`: it must find nothing.

Vector 3 of a security gate is COVERAGE & PRECISION as for a hunt, plus the THREAT MODEL: does it
name the attacker's non-capabilities, does every entry point it lists fall in some unit, are the
classes the ones this code is exposed to, does `DO_NOT_REPORT` exclude only what the model makes
unexploitable, does it say who WRITES each input rather than which channel carries it — and three
things no runner sees: `SCANNERS` matches the scanners the document says were run, the verifier's
prompt still demands the backwards trace and the every-path check, and no prompt lost `UNTRUSTED`.

## The decision document of a review

The interview (`interview.md`) maps the same tree; its first frontier for a review is usually the
diff (base and head refs), the lenses, the units, who consumes the report and what they do with a
finding, and how deep verification goes (one skeptic each, or a vote on the top severities). The
acceptance section states OBSERVATIONS (`gate-checks.md` section D), and a review's are few: the
tree is unchanged (`wfcheck --owns-diff` exits 0), the report's population line names every cell,
every confirmed finding cites a location that exists at the reviewed revision. A hunt's first frontier
is the subsystem and its units, the failure classes, how a reproduction is run here and what it may
touch, and the cap; its acceptance adds that every REPRODUCED finding carries a command that fails at
`COMMIT` when re-run. A security run's first frontier is the threat model, STRIDE over each trust
boundary and who writes each input, then the classes (the stack's checklist read here), the
units and their entry points, the scanners and their rulesets, whether it is differential (`BASE`),
and what a PoC may touch; its acceptance adds that every scanner `SCANNERS` names ran (the report's
second line), every EXPLOITED finding carries a command that shows the exploit at `COMMIT` when
re-run, and `betterleaks` over the args file before the launch and over the saved report after it
finds nothing. An audit's first frontier is the question itself, its parts, the units that
can answer them, and who reads the answer; its acceptance is `wf check cite -at <COMMIT>
-require-anchor` clean over the saved `citable` with at least one citation counted, `answered: true`,
and every part either answered or listed as unsettled.
