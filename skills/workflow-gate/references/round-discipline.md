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
  What each one asks is `gate-checks.md` section 0b, which every gate reviewer already holds — it is
  STATED there and pointed at here. Bank each vector with
  `wf round lens <prompt-file> -name N -model M -asks Q` (`gate-harness.md`): `-name` stays the label
  your findings use, `-asks` keeps one gate's roster readable beside another's.
  Two of the three exist because no command reaches the whole class — `wf check closure` mechanises
  the reachable half of CLOSURE and `wf check stale` the reachable half of RIPPLE — so the vector
  covers the rest, and dropping it because the check came back green drops the larger half [M-01].
  **Every one of those prompts says that finding nothing is allowed, and says it from round ONE**, in
  the same words in every round of the gate. The sentence, and why the round it first appears in
  changes what the whole descent MEANS, are in `gate-checks.md` section 0b.

- **Keep one lens whose instruction is deliberately UNDER-specified, beside the ones the round's own
  findings aim [M-27].** A narrow prompt is a statement about where the author already believes the
  defect is, and a lens that answers it exactly will not volunteer the question next to it. Measured:
  two lenses read the same lines of one round, the aimed one answered the prompt's question correctly
  and thoroughly, and the round's blocker came from the other one asking something the prompt had not
  — a verified absence of one failure mode over a set of lines is not evidence about a second one over
  the same lines. Bank it as `-asks other`, which is what that value is for.

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
  scale is BLOCKER / MAJOR / MINOR / NOTE, closed, and `wf round record` refuses any other word before
  it writes a byte — the scale is why one gate's round can be read beside another's [M-02]. What each
  level MEANS, and the naming test that separates MAJOR from BLOCKER, is `gate-checks.md` section 0,
  which every gate reviewer already gets.

## The stop rule

- **Stop rule, decided in advance.** The gate ends on a REVIEWER round that produces no BLOCKER, and on nothing else [R1]. Majors ride into the
  run — the per-section review gates exist to catch them, and another round costing six figures of
  tokens to fix one stale sentence is not a bargain. Read the trend off **`wf stats`**, not off your
  own awk: it prints each round's findings, blockers, closure split and apply batches from the ledger
  and git, and it knows the open round's scaffold placeholder is not a finding — a hand count closed
  a gate on the wrong totals for exactly that reason [M-12].
- **A gate may end AGAINST the rule on the target OWNER's call, and the record must say so [M-42].**
  The question is `gate-checks.md` §0's — would a fleet BUILD the wrong thing, or is the round
  re-filing a guard? Measured: a round left three blockers, every one about the artifact's own
  guards (an enumeration by coordinates that was not the population, a class list closed by
  instance, a defeat clause asking for a judgement) and none a claim that the SUBJECT was wrong; the
  owner chose to build, and the build found two more members of the class, both
  "a promise in the document with no surface behind it", which no reading of the document produces.
  When every standing blocker is about a guard, the cheapest next reviewer is the compiler. What is
  NOT permitted is a bare close: `wf close` refuses a last reviewer round carrying a BLOCKER unless
  `-early "<why>"` names the reason, and banks it in `gate.json`, at the head of `wf stats` and in
  the record, so a reader of the corpus can tell the two endings apart without re-reading a ledger.
  And the build is a review round the ledger otherwise never sees: open it as
  `wf round new -kind run` BEFORE `wf close`, record what the compiler and the smoke found as its
  findings, and a class that grows during the build is in the descent rather than in a carve-out
  file by hand.
- **The blocker descent is not monotone, and a BUMP is the corpus norm.** Every gate workspace on
  record bounces at least once, and gates have closed on the rule with a bump inside their last few
  rounds. A bump is NOT reliably "the round after a large fix" — one followed the smallest fix of its
  gate to that point, and on another the majority of the bumped round's blockers were not marked
  `[fix-induced]` at all. Read a bump as the normal shape of a descent, never as a signal about the
  artifact; a zero is not a trend either. The stop rule stays exactly what it says above, and nothing
  else does. The gate ends on a REVIEWER round that produces no BLOCKER, and on nothing else [R1].
- **A descent that PRECEDES a first review says nothing about it.** A round whose window brings a new
  artifact into the gate reviews it for the FIRST time, and `wf stats` says so BEFORE that round's
  counts. Measured: nine rounds of clean-looking descent on a decision document, one more empty round
  from closing the gate, and then the orchestration script was added and the next round produced 38
  findings and 12 blockers over an artifact nobody had reviewed at all. On a `-kind tool` round the
  same line says an artifact was added and NO reviewer was dispatched — that round reviewed nothing by
  declaration, and claiming a first review for it would be this bullet's own defect committed in the
  fix for it.
- **A zero from a lens set that is MISSING the vector which has been finding the blockers is not the
  same zero [M-24].** The rule ends the gate on a reviewer round with no blocker; a round is evidence
  about the ARTIFACT only in as much as it ran the vectors that have been producing defects. Measured:
  a round returning zero blockers was read as the document closing, the next round — the first to
  carry the external lens — returned six, and by then the gate had already moved on and paid for it.
  Before reading a zero, read the round's own roster (`wf round lens` with no file prints it, and
  `wf stats` prints it per round): if a vector that has been filing blockers did not run, the zero is
  a statement about the set, and the cheap answer is one more round with it.
- **A closing edit costs the round its MEASUREMENT, and you find out afterwards.** Applying the last
  round's majors after that round has already ended the gate means re-publishing it with
  `wf freeze -refreeze`, and `wf stats` then drops the round from the cost meter's prediction basis
  because the tag moved over the round's own repairs. That is correct and it is not a cost anybody
  expects, so choose it knowingly: either the fixes ride into the next gate's first round, or the
  round stops being a priced one. Neither is wrong; being surprised by it is.
- **The closing round's own FIX is unreviewed, and what governs it is `gate-harness.md` section 4 —
  "an edit after the gate closes is still an edit" [M-25].** The stop rule is about the findings a
  round LEAVES; it says nothing about the code that closes the ones it does not leave, and those
  edits are the last thing anybody writes and the first thing nobody reads. That section states the
  rule and the measurement; read it before you close, not after.
- **An UNFILLED findings scaffold is not a measured zero.** A round opened, frozen and never
  dispatched leaves the ledger `wf round new` wrote, which prints as `0 finding(s), 0 blocker(s)` —
  the same line the round that ENDS a gate prints, and under this rule a legal close. `wf stats` marks
  it `ledger: UNFILLED scaffold, not a measured zero`, `wf freeze` refuses while ANY closed round
  stands in that state and `wf close` refuses to bank one; EVERY closed round is checked, because
  `wf round new` does not require the current round to be frozen and a round opened past without a
  freeze was examined by no refusal at all. A round whose vectors ran and found NOTHING says so by
  banking them with `wf round lens`, and the line then reads `ledger: empty, and its lenses are
  banked — a measured zero`. A `-kind tool` round is exempt by declaration; the OPEN round is exempt
  because its scaffold is blank by construction.
- **The number that decides whether to keep going is the share of a round's BLOCKERS that are
  fix-induced, not the share of its findings** — and it is a CONTINUE signal, never a stop signal: it
  says what THIS round found, not whether the next will be empty. `wf stats` prints it per round
  beside the marker count (`fix-induced 16 (4 of 5 blocker(s))`), gives the gate total, and names
  every round whose blockers are a MAJORITY self-inflicted. The count goes flat exactly when the fix
  rate starts matching the introduction rate, which from outside looks like a gate holding steady
  against the artifact. Read it beside the SUBJECT question in `gate-checks.md` section 0: would a
  fleet BUILD the wrong thing, or merely be told something stale about a file it is about to read
  anyway. Mark such findings with a literal `[fix-induced]` in the finding text — an unmarked corpus
  prints "not measured", which is a different claim from "no self-inflicted findings" — and read the
  rounds `wf stats` names BY NUMBER as unmeasurable: findings with no marker (the share is then a
  lower bound) and findings with no `wf apply` batch (their edits are absent from every ratio it
  prints). Both holes opened in the same place on one gate and its totals kept reading as measured
  [M-12].

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
  the gate took four rounds to act on a signal that had been true since the second. `wf stats`
  prints the section form as well — `<path>#<anchor> is cited by a BLOCKER in rounds N-M` — over
  the anchors the finding rows cite, whatever file each blocker names; a bare `#D5` is attributed
  to the first document the row cites with an anchor, wherever in the row it stands. It fired twice on the gate that paid for
  it [M-42], where the run of ones never did.

  **A guard is closed by ENUMERATING its inputs, not by fixing the direction the last reviewer looked
  from [M-26].** Each round of such a run files a finding that is individually correct and a fix that
  individually closes a real escape, which is exactly why the run continues: a reviewer aimed at the
  guard reports the hole it happens to face. The move that ends it is to hand one lens the guard and
  the SHAPES its input can take — one entry short, one extra at the front, an index off by one, the
  single-member case — and ask it to check each against the code. Measured: three consecutive rounds
  at one blocker each on one ledger guard, then a fourth round asking that question found the
  remaining forms all fail safe and closed it. The enumerating question was available in the first of
  those rounds.

  **ENUMERATE by COMMAND when a round's findings keep having the same SHAPE.** The rule above is
  about one guard; this is about the round's whole yield. When two or more consecutive rounds file a
  blocker of the form "X is missing from a set that already has N members", stop sampling: give a
  lens the set, the COMMAND that enumerates it, and the instruction to report a COMPLETE table with
  member counts. A guard covering its whole class is then a NOTE saying so with the count, and a
  missing member is a finding. Measured on one seventeen-round gate, where five reviewer
  rounds had each found one instance of that shape — two spawn sites without a guard ten siblings
  carried, one acceptance property with no route into `incomplete` that twenty-four siblings had, two
  commands with an unfilled hole. The round that was told to build three complete tables closed two
  of those questions PERMANENTLY and found the sixth instance as its only blocker; the round after it
  generalised the shape rather than the instance ("a guard written for a CLASS of agents, applied to
  a SUBSET"), swept the file, and returned three classes complete with member counts plus one gap.
  Those two rounds were the cheapest of the gate per defect closed, and they were cheap BECAUSE the
  lens was asked to count rather than to judge. A reviewer asked to read and judge finds one member
  per round; a reviewer asked to count finds all of them or proves there are none.

  **Withdrawing the guard is one of the legitimate answers, and often the cheapest.** A property
  whose subject is better read by an agent HOLDING THE DIFF belongs in the section gates, not in a
  guard over the final report. Measured: that same property was a comment-freshness check, and it
  took two defensible intermediate forms — a table of fragments, then a pair of fixed `rg` commands —
  before withdrawal was obvious. Both failed the same way, which is the reusable half:
  **a hand-written enumeration cannot be complete, and a fixed pattern is both incomplete and
  over-complete against the same tree.** Those two `rg` commands missed the two claims the slice most
  obviously falsified and printed two hits about nothing, and both halves were found by different
  lenses in one round.

## What the gate does NOT bound

- **The gate is a bound on what an unsupervised RUN can get wrong. It is not a bound on what the
  FEATURE can get wrong.** For a backend slice those two nearly coincide, which is why the
  distinction went unstated for a corpus of them. Measured on the first UI slice in this corpus: four
  rounds and 55 findings hardened the script, the run came back green on every bar with zero
  ownership violations and one violated acceptance clause that was genuinely right — and then a human
  opened the page and found two defects reachable from neither artifact. A tab indicator drawn 7px
  beside an 8px sibling, correct in isolation and wrong next to its neighbour; and a one-frame radio
  flicker caused by a third-party table clearing its own selection state on every data push, a
  mechanism living in `node_modules` and named in no artifact. Both are visual, and section D's own
  rule — an acceptance clause asserts what a COMMAND can observe — correctly forbids writing a clause
  for either. So the class is uncovered BY CONSTRUCTION, not by oversight.
- **Say which of the two you are buying, before the run.** A slice whose failures are visual or
  interactive needs a pass the gate does not contain, and the honest plan names it as a separate step
  with an owner rather than hoping the acceptance section reaches it. What the gate still buys there
  is real and was measured on the same slice: two round-2 blockers were ownership holes — a section
  required to edit a file it did not own — which would have wasted the whole run.
- **And the window AFTER the gate closes is where that pass's own fixes land, which is the window
  this file's stop rule already warns about.** On that same slice the fix for the flicker introduced a
  fourth defect — two rules for one question, in two places — which is the artifact-drifts-from-its-
  own-specification class the gate had spent a round on, committed by the operator once nobody was
  reviewing.
