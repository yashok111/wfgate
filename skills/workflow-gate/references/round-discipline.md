# Round discipline (the gate as a process)

How the rounds of a gate run and when the gate ends. The gate's own failure modes, and these were
paid for. The mechanics each bullet names are in `gate-harness.md`; the launch gate that opens the
sequence is `pre-flight-gate.md`.

## Barrier each round

- **Barrier each round.** Collect ALL vectors of a round before applying ANY fix. Patching between two
  reviewers of the same round means the second reports what you already fixed. **The temptation is
  strongest exactly when one vector is fast and its findings look obviously correct** — measured: an
  external lens returned first, its five fixes went in while two internal lenses were still reading,
  and one of them judged a moving target and reported two blockers as open against a file where they
  were already closed. Nothing was lost that round because the other lens happened to read after the
  write, which is luck and not design. Dispatch every vector against ONE frozen snapshot and batch the
  fixes after the last one returns.

## What a delta round asks

- **A delta round asks CLOSURE, FIX-INDUCED and RIPPLE, and may add vectors but never drop those three [R3].**
  Bank each one with `wf round lens <prompt-file> -name N -model M -asks Q` — `-name` stays the label your
  findings use, `-asks` is the closed set (`orchestration, routing, alignment, facts, seams, criterion,
  closure, fix-induced, ripple, other`) that lets one gate's roster be read beside another's, and `other`
  is always available so a vector your slice needs is never refused. Each required question has a
  measured origin, not a taxonomy's symmetry: **CLOSURE** — two hand-written closure lenses returned 6 and 4
  of 24, 5 of 25, and 6 and 4 of 23 CLOSURES they judged not closed, in rounds that themselves filed 25,
  23 and 21 findings — round 4's second lens checked citations, not closures (4 unsupported of 70) — the
  most valuable half of those rounds,
  produced by prose over data git already held (`wf check closure` mechanised the reachable part, not
  this one); **FIX-INDUCED** — a fix is the largest single source of blockers, half the BLOCKERS of rounds 3+
  on one phase, 5 of 11 on the next, and 9 of 9 on the one after; **RIPPLE** — a fix that changes a RULE has
  consumers that are not in the diff, and THREE of four such ripples on one run argued from the retired rule
  while containing none of its keywords, so `grep` returned nothing (`wf check stale` narrows that read
  and says NOT A PASS where it swept nothing). Measured on the two gates that banked any roster at all:
  13 lens RECORDS under TEN distinct names (measured 2026-08-25; one gate is open and the figure moves),
  of which exactly one — `criterion` — appears as a vector name anywhere in this manual, and the other
  four gates banked no roster, so their prompts are gone.
  **Every one of those prompts says that finding nothing is allowed**, in the same words in every
  round of the gate: *an empty findings list is the expected and legitimate result; manufacturing a
  finding to look thorough is one failure mode and downgrading a real one to make the count look good
  is the other.* `wf stats` and `wf round lens -asks` enforce the QUESTIONS by roster name and can see
  nothing of the framing. Read off one script gate's banked roster: no prompt of its first three
  rounds carried the clause (14, 7, 6 blockers), all four prompts of its fourth did (1), and three of
  four of its fifth did (0, with thirteen defects still filed, two MAJOR). Two rounds against three
  at the convergent end of a descending gate is not a measurement of the clause — but a count that
  means one thing in round 3 and another in round 4 is not a descent either, so the clause belongs in
  round ONE, and this is checkable only because the prompts were banked.

## Later rounds are DELTA rounds

- **Later rounds are DELTA rounds.** A fix is unreviewed code — in one phase, *half* the blockers of
  rounds 3+ were introduced by the previous round's own fix; in the next, 5 of 11; and in the one after
  that, round 2 was **9 out of 9**: every blocker that round existed only because round 1's fix created
  it. Plan on that
  rather than on a clean round 2; a round whose findings are all self-inflicted is the expected case,
  not a sign the gate is malfunctioning. But a full re-read is the wrong instrument:
  hand the reviewer the script's `git diff` plus the findings it was meant to close, not 150 KB again.
  A delta round is ~20k tokens against ~170k for a re-read. `wf delta` prints exactly that brief — the
  findings filed by the round BEING CLOSED, the closure checklist derived from their `closed by`
  column (each claimed commit resolved to its subject and touched files; a prose closure marked
  "verify by hand"; an open blocker printed as OPEN), then the artifact diff since that round's tag —
  because a
  reviewer whose first job is to check a closure claim needs the claim in front of the diff, not in
  another file. **`-save` writes the brief to `rounds/r<open>/brief.md` and commits it** — hand-saving
  it produced the untracked stray file verify then had to learn to name; if the brief is wanted as a
  file, it is round bookkeeping and the tool banks it. Its default base is the round before the open
  one, deliberately: by review time the
  current round has itself been frozen, so "the newest tag" would diff against the very state under
  review and print nothing, and an empty diff looks exactly like a round that changed nothing. Name a
  different base with `-from gate-rN` only when you mean to.

## Run the DECISION gate to exhaustion BEFORE the script exists

- **Run the DECISION gate to exhaustion BEFORE the script exists.** Measured across three phases: the
  decision gate is cheaper per finding than the script gate, and a script written from a document whose
  architecture has already been inverted four times gets a script gate that is about the machine rather
  than about the design. The run that ordered it that way closed its decision gate in eight reviewer
  rounds and its script gate in three (against ten and six on the run that overlapped them), and every
  script-gate finding was ownership, liveness, coordinates or the fixture — which is the division of
  labour the two gates are supposed to have.
- **The decision gate asks whether the design is RIGHT; the script gate asks whether the REPOSITORY
  WILL LET YOU BUILD IT.** They find different things and neither substitutes. On one run, eight
  reviewer rounds over the decision document missed a hand-pinned total in
  `internal/admin/autocheckpoint_test.go` (`if len(mutating) != 27`) that the slice's one new POST
  route would have turned red — the document had correctly recorded that the two labelled sets it
  changed stayed the same size, and the total was pinned separately. The script gate's FIRST round
  found it, because its reviewer read the tree with the implementing run's file-ownership list in hand.
  That is a question only a script can ask, and it is the argument for not treating the script gate as
  a formality after a decision gate that closed at zero.
- **The two gates have different SHAPES, and the script gate's late rounds collapse into one defect
  class.** Measured on one phase, both gated to exhaustion: the DECISION gate produced **94 blockers
  over seven rounds** (31, 18, 13, 14, 12, 6, 0) and inverted eight decisions — the architecture
  arrived in round 1 and the acceptance criterion was still producing blockers in round 6. The SCRIPT
  gate over the same phase produced **15, front-loaded**: 7, 4, then 1, 1, 1, 1, 0. Every blocker from
  round 3 onward was the SAME thing — a rule corrected in one copy and left standing in another: a
  producer's prompt, a reviewer's bullet, a phase's `meta` entry, a scope list, a bar. Once a round's
  findings are all of that class, the next move is a mechanical sweep for every copy of every rule you
  changed (`wf check dup` and the `[RN]` markers, in `gate-harness.md`), NOT another reviewer round. A round costs ten minutes and six figures of tokens to find what
  a search finds in a second — and one of those rounds was spent re-filing the previous round's own
  blocker, because the fix had thrown before its file was written (gate-checks §I).

## A reviewer of an INSERTED block gets semantic anchors

- **A reviewer of an INSERTED block gets semantic anchors, not line numbers.** Any coordinate you hand
  it is one the producer's own insertion invalidated by the length of what it inserted, so a reviewer
  checking it literally reads the middle of the new block and reports the wrong thing. Name the
  landmark ("the last assertion of the preceding block", "the header that opens the next one") and say
  that the numbers are the pre-insertion ones.

## Reviewer count descends, and what a round costs

- **Reviewer count descends** 3 → 2 → 1 as findings narrow. The ROUND count does not cap: the last phase
  needed four (11 blockers), and its rounds 2 and 3 found *nothing but* defects introduced by the previous
  round's own fix. Budget for that rather than hoping round 2 is clean. Deltas trend down but **not
  monotonically** — one gate ran 1050 / 374 / 169 lines, the next 583 / 236 / 342 / 167, where the third
  round was 45% LARGER than the second because the second's fix opened a seam the third had to close
  across several consumers. Budget for the round COUNT; do not plan on later rounds being cheap.
  **`wf stats -time` prints what a round actually cost** — the span, and its split into review, fix
  and bookkeeping tail, derived from the workspace's own commit times. **Run it; do not read a figure
  out of this paragraph.** What holds across every gate on record is the SHAPE, and only the shape is
  written here: a round is TENS of minutes, the review segment is the overwhelming majority of it, and
  the bookkeeping tail is a low single-digit percentage — so the round COUNT is the cost function and
  the command sequence is not worth compressing. The round also got LONGER as the gates got bigger,
  which is why a pooled number taken over an older corpus under-prices the gate in front of you. This
  paragraph used to carry the pooled median, the middle half and the per-gate list; every one of them
  went stale, twice, and a review round had to re-derive them — the numbers live in this repository's
  own `CLAUDE.md`, where they are a maintenance record rather than something an agent acts on.
  Read the median rather than the total: a span of several hours is the operator away from the desk,
  and most workspaces carry one. A `-kind run` round's span is the run: it is printed on its own line,
  split under the head `run` rather than `review`, and kept out of the median rather than counted as a
  long round or a break. Nothing else narrows that population — a tool round is in this median too.
  The same line prints the AUTHORING window — init to the first artifact — which no round covers
  because it happens before round 1 opens, and which has run from a couple of minutes to well over an
  hour; its short end is a document drafted before `wf init` and therefore outside every measurement
  banked anywhere. Step -2 is what occupies that window.
- **Drop FACTS first when the count descends.** One gate's round 1 returned: SEAMS 3 blockers,
  CRITERION 3 blockers, FACTS 0 blockers (every citation and count in a 150 KB script was correct). Run FACTS once —
  its one major was an invariant asserted in another reviewer's own prompt that the code did not hold —
  then spend later rounds on SEAMS and CRITERION.

## Vacuity, and the level a finding is filed at

- **Vacuity STARTS at BLOCKER, at classification time.** A finding that makes an observation, a guard or
  a tripwire *vacuous* — it passes whether or not the thing it checks exists — is classified BLOCKER,
  and rides as a MAJOR only where the row can NAME the later gate actually assigned a non-vacuity vector
  for it, which is the MAJOR test of the rule below and not a second rule. Classified there, its fix is reviewed
  like any other; exempting a fix from review contradicts the one measured rule this gate has (a fix is
  unreviewed code, and the largest single source of blockers).
  Do NOT reason from the defect's label: downstream gates are perfectly capable of catching vacuity when
  a section reviewer was given a non-vacuity vector ("which line of this diff, if reverted, makes this
  check fail? if none, that is a blocker"). **Classify by actual downstream coverage** — check whether
  some later gate was actually assigned that vector before letting anything ride.
- **A finding's level is decided by what catches it downstream, never by how bad it reads [R2].** The
  scale is BLOCKER / MAJOR / MINOR / NOTE and `wf round record` refuses anything else — measured, the six
  ledgers held 1065 findings under four vocabularies for one scale (p3a through round 5), 172 of them at
  a level no other gate can be read against, and a script `enum` does not save the other side: it checks
  the VALUE and never the LIST, and the scripts those six gates produced carry three lists plus a drifted
  field NAME. BLOCKER is what no later gate catches, and it is where vacuity STARTS —
  a finding that makes an observation, a guard or a tripwire vacuous is classified BLOCKER and rides only
  where the row can name the later gate actually assigned a non-vacuity vector for it, which is the MAJOR
  test and not a second rule; MAJOR is a real defect whose row NAMES the later gate that catches it,
  and a row that cannot name one is a BLOCKER; MINOR is cheaper standing than looped for; NOTE asserts no
  failure. The full statement of each is in `gate-checks.md`, which every gate reviewer already gets.

## The stop rule

- **Stop rule, decided in advance.** The gate ends on a REVIEWER round that produces no BLOCKER, and on nothing else [R1]. Majors ride into the
  run — the per-section review gates exist to catch them, and another round costing 170k to fix one stale
  sentence is not a bargain. Read the trend off **`wf stats`**, not off your own awk: it prints each
  round's findings, blockers, closure split and apply batches from the ledger and git, and it knows the
  open round's scaffold placeholder is not a finding — the hand count on the first live gate closed the
  gate on 99/27 against a true 98/26 for exactly that reason. The share of a round's findings that are
  self-inflicted (fix-induced) is the companion number, and it is a CONTINUE signal, never a stop
  signal: it says what THIS round found, not whether the next one will be empty. Measured on two
  gates — a round that was 4 of 4 fix-induced and carried one blocker was followed by a round filing
  seven findings and another blocker, and a 3-of-3 fix-induced round carried a blocker itself. The
  blocker descent is not monotone either — 7, 5, 1, 0, 2, 5, 3, 2, 1, 0 on one gate — so a
  zero is not a trend. Nor is a BUMP one, and this is the corpus NORM rather than an
  exception: **every gate workspace on record bounces at least once**, and gates have closed on the
  rule with a bump inside their last few rounds. What a bump is NOT is reliably "the round after a
  large fix" — one was measured following the SMALLEST fix of its gate to that point, and on another
  the majority of the bumped round's blockers were not marked `[fix-induced]` at all. Read a bump as
  the normal shape of a descent, never as a signal about the artifact, and read the `[fix-induced]`
  split beside it, which is the only thing in the workspace that says where a round's blockers came
  from. The stop rule stays exactly what it says above, and nothing else does. The gate ends on a REVIEWER round that produces no BLOCKER, and on nothing else [R1].
  **And a descent that PRECEDES a first review says nothing about it.** A round whose window brings a
  new artifact into the gate reviews it for the first time, and `wf stats` says so BEFORE that round's
  counts — the suppressed ratio used to sit in a parenthesis after them. Measured on mocker A3: nine
  rounds of blocker descent on the decision document — 13, 17, 10, 16, 11, 12, 6, 7, 4 — read exactly
  like convergence, one more empty round would have closed the gate under this rule, and then the
  orchestration script was added and round 10 produced 38 findings and 12 blockers over an artifact
  nobody had reviewed at all. On a `-kind tool` round the same line says an artifact was added and NO
  reviewer was dispatched: that round reviewed nothing by declaration, and claiming a first review for
  it would be this bullet's own defect committed in the fix for it.
  **And an UNFILLED findings scaffold is not a measured zero.** A round that was opened, frozen and
  never dispatched leaves the ledger `wf round new` wrote, so `wf stats` printed `0 finding(s),
  0 blocker(s)` — the same line the round that ENDS a gate prints, and under this rule a legal close
  (measured: one gate's round 9, frozen as `gate-r9`, reviewed by nobody). `wf stats` names such rounds
  and marks the line `ledger: UNFILLED scaffold, not a measured zero`; `wf freeze` refuses while ANY
  closed round stands in that state and `wf close` refuses to bank one. EVERY closed round is checked,
  not just the previous one: `wf round new` does not require the current round to be frozen, so a round
  opened past without a freeze was examined by no refusal at all. A round whose vectors ran and found
  nothing says so by banking them with `wf round lens`, and `wf stats` then reads it as `ledger: empty,
  and its lenses are banked — a measured zero`. A `-kind tool` round is exempt, because its opening
  subject already records that no reviewer was dispatched; the OPEN round is exempt because its
  scaffold is blank by construction.
  **And the number that decides whether to keep going is the share of a round's BLOCKERS that are
  fix-induced, not the share of its findings.** `wf stats` prints it per round beside the marker
  count (`fix-induced 16 (4 of 5 blocker(s))`), gives the gate total on its own line, and names
  every round whose blockers are a MAJORITY self-inflicted. A descent of 9, 3, 5, 5 whose last round
  is 4-of-5 its own predecessor's fix is a different object from the same digits arrived at
  honestly, and the stop rule reads the digits: the count goes flat exactly when the fix rate starts
  matching the introduction rate, which from outside looks like a gate holding steady against the
  artifact. Measured across the corpus — 100 of 177 blockers on the gate that stopped instead of
  closing (11 of its 22 rounds a majority), 22 of 79 and 12 of 28 on two others, and one gate with no
  markers at all, which prints as unmeasured rather than as zero. Read it beside the SUBJECT question
  in `gate-checks.md` section 0: would a fleet build the wrong thing, or merely be told something
  stale about a file it is about to read anyway. Mark such findings with
  a literal `[fix-induced]` in the finding text — `wf stats` counts the marker per round and in total,
  and prints "not measured" when nobody marked, because an unmarked corpus is a different claim from
  "no self-inflicted findings". It also names, BY ROUND NUMBER, the rounds it could not measure: the
  ones with findings and no `[fix-induced]` marker (the share is then a lower bound over the marked
  rounds, not a measurement of the gate) and the ones with findings and no `wf apply` batch (their
  edits are absent from every ratio it prints). Both holes opened in the same place on one gate — from
  round 18 to the close — and the totals above them kept reading as measured while the true
  fix-induced share was nearer 38% than the 34% printed, with every blocker of five consecutive rounds
  induced by the round before.

## A run of rounds at exactly ONE blocker

- **A run of rounds at exactly ONE blocker is a guard asking the wrong KIND of question.** `wf stats`
  names EVERY such run (three or more) and, where every blocker in it cites one file, says so. The
  stop rule is right to keep the gate open — each of those rounds does close a real escape — but
  measured on one gate, rounds 22 to 24 were one enumeration floor rewritten: a free-text sentence,
  then a non-empty array, then a distinct-member count, then a fixed cardinality, then exact member
  names, then stable ids. Every version but the last two constrained a property of the REPORT, and for
  every "at least N" there is an N, so the next degenerate report satisfied the new shape. **A guard
  over a report asks "is this what the document NAMES", never "does this look like enough".** When the
  signal fires, stop patching the guard and re-ask what it is for.

  **The signal is the SECTION, not the blocker count.** A run of ones is one shape of it; the
  general one is *the same artifact section produces a blocker in three consecutive rounds*, whatever
  the round totals are. A later gate's rounds ran at 1, 4, 6 and 3 while one property was rewritten
  in every one of them and produced a blocker in three, so nothing matching "a run of ones" fired and
  the gate took four rounds to act on a signal that had been true since the second.

  **Withdrawing the guard is one of the legitimate answers, and often the cheapest.** A property
  whose subject is better read by an agent HOLDING THE DIFF belongs in the section gates, not in a
  guard over the final report. Measured: that same property was a comment-freshness check, and it
  took two defensible intermediate forms — a table of fragments, then a pair of fixed `rg` commands —
  before withdrawal was obvious. Both failed the same way, which is the reusable half:
  **a hand-written enumeration cannot be complete, and a fixed pattern is both incomplete and
  over-complete against the same tree.** Those two `rg` commands missed the two claims the slice most
  obviously falsified and printed two hits about nothing, and both halves were found by different
  lenses in one round.
