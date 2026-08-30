# What each rule was measured on

Provenance. Every rule in this manual answers a failure that actually happened on a real gate, and
this file is where the gate, the counts and the round costs live — cited from the rules by `[M-nn]`
and never auto-loaded.

**Why it is a separate file.** The manual already applies this test to NUMBERS: a figure belongs
beside a rule only if an agent CHANGES ITS BEHAVIOUR from the value and cannot get it from a command
at the moment it needs it. The same test applies to the STORY. An agent acts on "a fix is the
largest single source of blockers, so later rounds review the delta"; it does not act on which gate
that was, how many rounds it ran, or what the per-round split was. That is provenance — and
provenance is what makes a rule falsifiable rather than an assertion, so it is kept, not deleted.

**How to use it.** Read an entry when you are about to CONTRADICT the rule it belongs to, when the
rule looks arbitrary, or when you are proposing to drop one. Do not read it to follow a rule; the
rule is stated where it is stated. And a banked measurement is not sacred: `gate-checks.md` section
A carries the rule that a claim contradicting one is settled by RE-RUNNING the command, not by
weighing the two wordings — and on the record so far the banked side has also been the loser.

---

## M-01 — the three questions a delta round may never drop

CLOSURE: two hand-written closure lenses returned 6 and 4 of 24, 5 of 25, and 6 and 4 of 23 CLOSURES
they judged not closed, in rounds that themselves filed 25, 23 and 21 findings — round 4's second
lens checked citations rather than closures (4 unsupported of 70). That was the most valuable half of
those rounds, and it was prose over data git already held; `wf check closure` mechanised the
reachable part and not this one.

FIX-INDUCED: a fix is the largest single source of blockers — half the BLOCKERS of rounds 3+ on one
phase, 5 of 11 on the next, and 9 of 9 on the one after.

RIPPLE: a fix that changes a RULE has consumers that are not in the diff, and THREE of four such
ripples on one run argued from the retired rule while containing none of its keywords, so `grep`
returned nothing. `wf check stale` narrows that read and says NOT A PASS where it swept nothing.

The roster census behind "bank the vector": across the two gates that banked any roster at all, 13
lens RECORDS under TEN distinct names (2026-08-25), of which exactly one — `criterion` — appears as
a vector name anywhere in this manual. Four other gates banked no roster, so their prompts are gone.

The "finding nothing is allowed" clause, read off one script gate's banked roster: no prompt of its
first three rounds carried it (14, 7, 6 blockers), all four prompts of its fourth did (1), and three
of four of its fifth did (0, with thirteen defects still filed, two MAJOR and both genuine). They
did not go quiet, they RECLASSIFIED, and the reclassifications survived a hand check against the
tree. Two rounds against three at the convergent end of a gate that was descending anyway is NOT a
measurement of what the clause is worth — the corollary is what makes the rule, and it is stated in
`gate-checks.md` section 0b.

## M-02 — why the severity scale is closed rather than suggested

The six gate ledgers on disk held, on 2026-08-25, 1065 findings under FOUR vocabularies for one
scale: BLOCKER is universal, the tier under it is MAJOR on five gates and CONCERN on one, and the
tier under that is MINOR on four, ADVISORY on one and NIT on one. 172 of the 1065 — 16% — carry a
level no other gate can be read against, so no report could put a round of one gate beside a round
of another. A JSON Schema `enum` does not save the script side: it validates the VALUE against a
declared list and never the LIST, and the scripts those six gates PRODUCED carry three lists of
their own plus a drifted field NAME (`level` on two, `severity` on one). The scale is closed at
`wf round record` because that is the one place both paths meet.

## M-03 — `-asks` takes the set however it is written

One gate banked one vector per round and then hand-edited `Asks:` in the file, nineteen times over
ten rounds, having been refused the natural spelling.

## M-04 — the agent-count ratio and the phase set

A red-bar abort added to the FIRST producer took five of seven modes from 15/25/25/25/1/15/25 agents
to 18/1/1/1/1/1/1 — six modes then at one agent, `throw-serial` included, which is one by
construction — with the fix round, the recheck, the acceptance step and the final verdict exercised
by nothing, and every mode still printing OK. A reviewer banking "wfdry 7 of 7" WOULD have credited a
layer covering a single agent.

Failing rather than warning was measured too: across the three correct gated scripts on disk the
failure modes run WIDER than the happy path (36 against 44, 18 against 30, 15 against 25), so none of
them trips the floor.

The PHASE set is additive because it misses the opposite shape: a branch that skips a whole section
while keeping its agent count within half the happy path's is invisible to the ratio — measured on
such a shape, 13 agents against 15 and 4 phases against 5, so the ratio passed while neither the
fifth section nor its gate ran in any mode. The comparison is over distinct phase TITLES rather than
their count, because a mode running one phase twice reaches the same count having covered less; the
threshold is exact rather than a tolerance, because across the five correct scripts on disk every
mode but `throw-serial` reaches its baseline's phase set exactly, and a tolerance calibrated on a
population with no variance in it is calibrated on nothing.

`dead-reviews` is exempt from the phase floor as a WARN, and the warning still has to be read: an
abort guard written for a dead PRODUCER ended the run on any death at all, the mode reached 11 of 14
phases with a gate, the acceptance step and the last gate exercised by nothing, and the agent count
cleared the ratio so every mode printed OK.

The `clean`-verdict rule caught this shape: a repair agent spawned only when acceptance FAILS,
counted in the liveness ledger, so the one run everyone wants logged `dead: repair` and returned
`green: false`. Seven modes were green over it, because acceptance failed in all seven and repair was
therefore always alive; an opus reviewer found it by copying the script's tail into a file and
executing it with green values by hand.

## M-05 — the schema-width refusal, and how cheap the answer is

`blocked by safety classifier: output schema too large to classify safely` killed four agents of one
run, and on another run the FIFTEENTH and last agent, six hours in, with nothing downstream to catch
it. A gate that measured its own width to the byte, wrote down the risk and accepted it on the ground
that "acceptance cannot be measured from here" was wrong on exactly that clause: the next gate
probed it and had the answer in 82 milliseconds at zero tokens — 4,233 bytes REFUSED, 3,114 ACCEPTED.
The same probe proved that the launch RESOLVES `$defs`/`$ref` and ENFORCES the `minimum`/`minLength`
reached through one, by having a deliberately illegal first attempt rejected.

Two narrowings, both used by the corpus: declaring a repeated leaf ONCE and referencing it (which
took two refused schemas under), and trimming the prose in the descriptions (which took the third
one under, on a schema already carrying four references).

## M-06 — what the drift sweep reaches, and what it cannot

`dup`, measured on one phase's artifacts: 2065 sentences, 176 cross-file groups, 34 flagged, in
under a tenth of a second, and they were real drifts. An earlier version keyed on FILE-level history
and flagged 161 of 176 — a check that paints 91% of its output is a check nobody runs twice.

On a DERIVED artifact both DIVERGENT conditions fire by construction: a context document restating
the decisions produced 21-25 flagged groups per one real contradiction.

The contradiction half: eight such contradictions on one live gate, every one found by a reviewer and
none by `dup`. On mocker A3, six of round 10's twelve blockers were one decision that moved in the
document during round 9 and did not move in the prompt restating it — and the dearest of them
carried, verbatim, the document's own sentence warning that this half read alone is wrong.

The stop rule carries a rule marker of its own in these documents (id R1, written without the
bracket form here — a sentence that merely refers to a rule states nothing) because it lived at five
sites in four wordings,
one of them lower-cased, which the case-erasing normaliser cannot see as a difference and no check
then compares — and the "reviewer rounds only" half was missing from the canonical statement. One of
the five sites is a Go comment (`internal/ws/findings.go`) that `wf check rules` can never reach,
because it compares ARTIFACTS; it is kept in the same wording by hand.

## M-07 — a `required` array dropped inside a width edit

Twice. One gate narrowed a schema for the classifier and left a stale band CONSTANT behind; the next
lost `required: ['met','evidence']` from the leaf of a twenty-two-key acceptance report, in a hunk
whose own comment was entirely about width, with no warrant recorded — so ONE frozen revision
published a report whose every property could come back `{met: true}` with no evidence at all, the
only thing the gate over that report had to read. Nothing static reaches it where the leaf is built
inside an `Object.fromEntries(...map(...))` callback: both static tools were clean over that script.
The next round's lens caught it by reading. `wf check schema -since <ref>` is the mechanical version,
one line long.

## M-08 — the sweep a note cannot buy

On one gate, four times in four consecutive rounds, the fix landed where the finding was READ rather
than everywhere the rule ACTS. None of the four was reachable by `dup`, `stale` or `rules`, because
the copies shared no wording — and none would have been caught by `-rule`'s refusal either: that
gate marked nothing, so the id form reached nothing there, and three of the four are copies inside
ONE file, which is behind the FILE half. It is an argument for MARKING, not a mechanism that caught
them.

The free-text form exists because the id form reaches nothing on a script gate, whose rules live in
prompts and are marked in no document: the flag was refused outright and unusable for a whole gate
whose most valuable sweep it is.

## M-09 — the pinned diff algorithm, and why an edit is not a unit

The cost meter counts changed lines under `diff.algorithm=histogram`, pinned in `gitx`. Measured over
the three closed gates: git's default is myers, one box carried histogram in `~/.gitconfig`, and the
two readings of one gate differ by 2.8% overall and 5.3% on a round — which moves a round's cost
ratio by 0.2 per 100.

An edit is not a unit and never was: one gate's two-edit window changed 933 lines, 931 of them in a
single commit, and the per-edit meter — which had seen only the other, one-line edit — called the
window "38.00 finding(s)/edit".

## M-10 — three dry-run reds that were the FIXTURE

On one gate: a `$ref` the runner could not expand (a `$defs` written on a SUB-schema, where the
pointer is anchored at the schema ROOT), twenty-two booleans defaulted false because names like `P8a`
carry no polarity, and `finalCount === 22` — an integer against a constant that no report-shaped
fixture can satisfy. Each cost a reading of twenty-two "unmet" rows to diagnose as ONE gap. One of
the three ended with the script paying about 1,900 bytes of width to inline twenty-two copies of a
leaf it had correctly declared once, against a classifier with a measured refusal at 4,233.

## M-11 — why a gate reviewer's tier is scaled by the ROUND count

The ten gates that had run through `wf` by 2026-08-29 spent **166 rounds** between them (13, 17, 13,
25, 13, 24, 22, 10, 15, 14, by each gate's own `wf stats`) at one to four lenses a round. The policy
this replaced put opus on all three pre-flight lenses, justified as "the last check before launch",
which prices the gate as ONE round of three reviewers. It is not one round: the tier multiplies by
the round count, and that product is what an operator runs out of. Re-derive the count with
`wf stats` per workspace rather than quoting this one — it grows with every gate, which is the point.

## M-12 — reading a descent

The hand count on the first live gate closed it on 99 findings and 27 blockers against a true 98 and
26, because the open round's scaffold placeholder was counted as a finding. `wf stats` knows it is
not.

Non-monotone descents on record: 7, 5, 1, 0, 2, 5, 3, 2, 1, 0 on one gate. Every gate workspace on
record bumps at least once, and gates have closed on the stop rule with a bump inside their last few
rounds.

The first-review hole, on mocker A3: nine rounds of blocker descent on the decision document — 13,
17, 10, 16, 11, 12, 6, 7, 4 — read exactly like convergence, and one more empty round would have
closed the gate under the stop rule. Then the orchestration script was added as a second artifact and
round 10 produced 38 findings and 12 blockers over something nobody had reviewed at all.

The unfilled-scaffold hole: one gate's round 9 was opened, frozen as `gate-r9` and reviewed by
nobody, and printed the same `0 finding(s), 0 blocker(s)` line the round that ENDS a gate prints.

The fix-induced share of BLOCKERS across the corpus: 100 of 177 on the gate that stopped instead of
closing (11 of its 22 rounds a majority), 22 of 79 and 12 of 28 on two others, and one gate with no
markers at all, which prints as unmeasured rather than as zero. On one gate both measurement holes —
rounds with findings and no marker, rounds with findings and no `wf apply` batch — opened in the same
place, from round 18 to the close, while the totals above them kept reading as measured; the true
fix-induced share was nearer 38% than the 34% printed, with every blocker of five consecutive rounds
induced by the round before.

## M-13 — where a gate's findings actually are

Across the last two phases' gates (nine rounds), ZERO findings concerned orchestration mechanics as
such. Everything of value fell into the classes of `gate-checks.md`.

Vector yield, three reviewers one vector each, round 1: SEAMS 3 blockers, CRITERION 3 blockers, FACTS
0 blockers / 1 major / 12 minors — every citation and count in a 150 KB script checked out. The one
major was an invariant asserted in another reviewer's own prompt that the code did not hold, which is
why FACTS is dropped first but run ONCE.

The decision gate's value: on the phase after this corpus's fifth, two lenses independently rejected
the same architectural decision in round 1, which inverted the design and split the phase in half —
26 blockers over six rounds, against a document of a few tens of KB costing minutes per round.

The tooling that guards the gate: one static checker was measured finding ONE call site out of
nineteen. Every check it owned had been passing over an empty set, silently, across several phases
whose reviewer rounds then paid for exactly what it was written to catch — and it reported zero
errors throughout, which is what a clean script also looks like.

## M-14 — the two acceptance sections

Two decision gates over one product, same operator, same target. The section written as OBSERVATIONS
produced 8 blockers, went quiet after round 4, was never cut, and its gate closed on a genuine zero.
The section written as prose test specifications was rewritten in EVERY round of its gate and grew
23 -> 352 lines while the document around it grew 333 -> 1319 — a third of all growth — until a human
cut it back to 92 lines of properties. In the round where the number of clauses carrying a named
defeat went 9 -> 12, the assertion count went 51 -> 69: the section grew faster than it hardened.

**What share of that gate's 92 blockers were the section's is deliberately NOT recorded.** Five
derivations of that one quantity exist, from 25 to 66; they disagree by up to 2.4x on individual
rounds, and the one the operator acted on was the highest. A verdict must not be the first consumer
of a quantity nobody has agreed how to count.

## M-15 — what the citation checks caught, and what they were blind to

On one phase's round-1 artifacts: **148 citations, 16 findings — 15 an ambiguous bare filename, one
a path resolving to no tracked file at all, and NOT ONE out of range.** That distribution is the
argument for anchors: of the five citations that gate shipped wrong, the four later rewritten with
anchors were all caught that way and none by the range check, because both the cited line and the
correct line existed.

The extensionless trap: a correct `Makefile:90#corepack` sat invisible in a green report of 237
citations until a hand grep found it — a path with no extension was not recognised as a citation at
all, so it was unchecked without being reported as unchecked.

The non-ASCII anchor: 15 Cyrillic anchors dissolved into "17 unverified" on the first live run,
hiding both that only two truly unanchored citations existed and a factually wrong anchor among the
invisible ones.

## M-16 — density, when a descent goes flat

One gate's flat rounds ran at nearly TRIPLE the findings per hundred changed lines of the round
before, on a fix that had SHRUNK — which is the opposite of a surface problem, and is why density is
read before a section is blamed for a series that stopped descending.

## M-17 — the round that reviewed a cut

It returned 4 blockers and 20 findings, **17 of them defects IN the cut**, and two of them were
design decisions that survived nowhere afterwards — one already caught by hand two minutes after the
cut landed, which is how the class was known to exist at all. Over two gates that cut, the per-round
structural removals `wf delta` counts run to 49 and 215, and most of them are rewrites, which is why
it prints a count rather than a list. One measured cut replaced 28 clauses with 10 properties and
the artifact GREW by four lines.

## M-18 — the named defeat, author-side and not reviewer-side

Three review rounds over one acceptance list found almost nothing while the entries were bare
assertions; the round after each entry carried its own claim found six.

The rejected reviewer version: across three rounds of one gate a canned vector required to name a
defeating implementation per clause named a defeat on **12-18% of the clauses it was handed (8/64,
9/51, 12/69)** and never returned zero, and each defeat it named was closed by ADDING a clause. Its
other 82-88% is "none constructed", which nothing separates from "did not try". On the gate that ran
it, 352 lines of acceptance section carried 2 lines naming a defeat, so the rule had never been
applied at all.

## M-19 — the cheap guard that was reverted

A round-13 observation, correct on its own terms: when the acceptance gate edits the tree AND leaves
a blocker open, re-measuring costs half an hour and a docker build at the end of a four-hour run for
a verdict that is already red. The skip was added. What it bought, measured the next round:
the branch was true in EVERY `wfdry` failure mode because the stub's recheck always returns
blockers, the two newest agents became reachable by NO dry-run mode while all seven printed OK, the
agent count fell silently **from 42 to 40**, the skip nulled the acceptance report so the run lost
its own findings and unheld list, and a bar was filed "not reported" for an agent that had never been
spawned. Three defects for half an hour.

## M-20 — ranking prompt pairs by shared sentences was built and not shipped

Over seven tracked scripts the ranking surfaces SIBLING prompts — two lenses of one gate, at 0.33 to
0.86 shared — while the pair that produced the rule (two agents given the same question from two
prompts, five blockers between them) ranks BELOW every one of them, at 0.07 to 0.12. A ranking whose
top is the pair you do not need and whose bottom is the pair you do is not a ranking.

## M-21 — the floor armed on its own field, four times in one script

Rounds 19, 21 and 22 twice, all in one script:

- `seam.allowlistCount !== 40` seeds a blocker — an integer has no "not answered yet" value, so it
  fired on every dry fixture and made the clean mode unsatisfiable.
- `clauseNumbers.size > 1 && size < 8` — eight rows all numbered `1` give size 1, not `> 1`, so the
  check switched itself off AT the defect.
- `enumSets.size !== 4` — four distinct STRINGS is not the four REQUIRED names, and a bogus fourth
  stood in for a missing real one.
- `expected.length !== 6` — counts slots, not members, and six copies of one name passed.

## M-22 — verifying a batch by searching, and the vacuous half of it

One whole gate round was spent re-finding a defect whose fix had THROWN before the write. That is
the failure `wf apply`'s all-or-nothing answers.

The verification itself then had the same shape one layer down: searching for each change ONCE
verified every pure DELETION vacuously — `strings.Count(body, "")` is never zero — and rejected
chained edits that had landed perfectly. It is a byte-for-byte read-back against the planned buffer
now.

## M-23 — the class a fix DECLARED closed

Round 8 of one gate closed an ownership hole in a package's test files and its own closing sentence
said the task "now names every test file the arity change reaches". Round 9 found the same hole one
level up: the section also widens a second function whose unowned test file carries 50 call sites.
The statement was true of the half the fix had touched and false of the section, and the round in
between had read it and stopped looking. Round 10 was asked the question directly — was the CLASS
closed or only the instance — and swept every remaining reshaped function; it found none, which is
what the sweep is for.

## M-24 — the zero that was a property of the lens set

Round 2 of one gate returned ZERO blockers and was read as the decision document closing. Round 3 —
the first round carrying the external lens — returned six. The document had not closed; the round
that said so had not run the vector that finds those defects. By then the gate had added its second
artifact, and the cost showed up six rounds later as one decision written in three places with two
of them left behind: 3 rounds on the document alone against 16, 17 and 5 on the gates that ran the
document to exhaustion first.

## M-25 — the closing round's own fix

The gate that measured this ran one round past its close only because `gate-harness.md` section 4
was read for `wf close`'s flags, and that round found a real defect in the closing round's fix: a
false arithmetic comparison ("more than the other three together", 50 against 62) stated identically
in both artifacts. It would have ridden into the run and been repeated by whoever read it. The
warning it reproduces was itself measured on four "text-only" major fixes applied after a clean
closing round, which broke the artifact outright.

## M-26 — three rounds of one guard, and the question that ended it

Rounds 6, 7 and 8 of one gate produced exactly one blocker each and every one was the same recheck
closure ledger: a recheck clearing blockers it never mentioned, then the summary join collapsing two
blockers into one, then the index join trusting the field over the position. Four harness edits in
four rounds, each finding individually correct and each fix individually closing a real escape.
Round 9's lens was asked instead to CONSTRUCT the ledgers a real agent produces — one entry short,
one extra at the front, one index off by one, a single-blocker ledger — and check each against the
code; all four fail safe, and the run ended. Cost of the three patching rounds: roughly 1.5 hours of
a gate the command prices at 5.3.

## M-27 — two lenses on the same lines, one blocker

Round 8's prompt asked, in as many words, whether two recheck guards could DISAGREE with each other.
The delta lens answered exactly that, correctly and thoroughly: they cannot, because the
contradiction guard breaks before the index join executes. The external lens asked something the
prompt had not — whether an entry's position and its `index` field, two carriers of one fact, are
checked against each other at all — and that was the round's blocker.

## M-28 — a clean report from a compacted read

The external lens of one gate found the hardest defect of rounds 3, 5, 6, 7 and 8, usually the
round's only blocker. In round 9 it returned clean while an internal sonnet lens, the same round,
filed a MAJOR — an unowned test file with 50 call sites. Its own output carries the reason:
`context compacted` printed mid-run. Nothing in the workspace records which kind of read a report
came from, so the two explanations for that clean — the artifact was clean, the reader was not whole
— cannot be told apart afterwards.

## M-29 — a producer that navigated through the shell

The largest agent of one live run (a 1.9 MB transcript) made **256 `Bash` calls, 64 `Edit`s and 6
`Read`s**, and exactly five of the 256 were bars (`make lint` x3, `make test` x2). The other 251 were
`sed -n`, `rg` and `cat`. The COST of those calls was measured later, over 1580 `Bash` calls and 413 non-`Bash` ones across four
completed runs: `Bash` median **4.124 s**, `Read` median **0.010 s** — the 413 non-`Bash` calls cost 98
seconds in total against 110.8 minutes for the `Bash` ones. The mechanism is NOT that a shell is slow,
and an agent told to make shell faster will fix the wrong thing: the latency is bimodal at ~0.11 s and
~4.4 s with structurally identical commands in both modes, and the discriminator is the PERMISSION
path. On this box `rtk rewrite` returns exit 3 ("ask") for `grep`, `cat`, `ls`, `wc`, `tail`, `find`,
`git`, `go` and `make`, and exit 1 (passthrough) for `sed`, `rg` and anything containing a pipe —
measured over the 1239 navigation calls, exit 1 is median 0.10 s and exit 3 is median 4.42 s with 95%
over a second. `ls -la` and `cat f` measured 4.6 s and 4.35 s; the hook itself runs in 0.05 s. So
navigation shell was 48.9 min, **15.5% of those runs' wall-clock**, and up to 25.6% of a small gate
round. Two independent fixes compose: navigate with `Read`/`Grep` (this rule), and auto-allow the
read-only commands so the ask round-trip stops being paid at all.

The per-agent numbers are final — that agent had returned. The run-wide reading is a SNAPSHOT of a
run still in flight and says so: 2120 `Bash` calls over sixteen agents at the first reading and 2319
over nineteen an hour later, head words led by `grep`, `sed`, `rg` and `cat` — the habit was every
agent's, not one agent's, and the total is whatever the run has reached when you run the command. The script asked for
none of it; the launching session's own instructions say to read with `cat`/`sed -n` and search with
`grep`, and subagents inherit them. Re-derive per agent with
`jq -r '..|.name? // empty' agent-*.jsonl | sort | uniq -c | sort -rn` over
`~/.claude/projects/<project-slug>/<session-id>/subagents/workflows/<wf-id>/`, and the Bash population
with `jq -r '..|select(type=="object" and .name=="Bash")|.input.command'` over the same files.

## M-30 — the concurrency cap, measured for the first time

`cost-discipline.md` has stated `min(16, cpus - 2)` for as long as it has existed; nobody had priced
it. Over SEVEN Workflow runs on a 4-core box (`nproc` = 4, so the cap is 2), the maximum number of
agents in flight at one moment was **2 in every run, never 3**. The proof is a ten-way `parallel()`
fan-out whose ten agents were queued within 2 ms of each other and then started strictly two at a
time, each new start landing 6.8-8.2 s after a predecessor's end; the last four waited 9.3, 10.9, 11.9
and 13.5 minutes for a slot. In a run whose every gate carries exactly two reviewers the cap never
binds — that run's parallelism is 1.13x and is its own serial shape — so the cap is invisible until
something asks for three. **What it costs when something does is now measured too.** A 30-agent run
that completed later the same day held max concurrency 2 again (parallelism 1.10x, 3.2 min idle over
a 407-minute span), and exactly one of its eight gates carried THREE reviewers: that third lens
waited **437 seconds** for a slot, against 6 to 8 seconds of spawn latency for every other agent in
the run. A third vector is not free and it is not a whole gate either — it is one queued wave, and
on that run it was 7.3 minutes. **The formula was read from the binary rather than inherited**, because this project's own manual had
carried it for months in a form missing its floor: it is
`Math.min(16, Math.max(2, cpus - 2))`, evaluated once at module load from `os.availableParallelism()`,
in the same chunk that carries the `Workflow harness` strings. The two forms agree from 4 cores up and
disagree below — the written one gives 1 on three cores and 0 on two, the real one never goes under 2.
No environment variable is read there; `CLAUDE_CODE_MAX_CONCURRENT_SUBAGENTS` (default 20) is the Agent
tool's limiter and a different code path. Re-derive from a completed run's state file:
`jq -r '.workflowProgress[]|select(.type=="workflow_agent")|[.startedAt,.lastProgressAt]|@tsv'
~/.claude/projects/<slug>/workflows/wf_<id>.json` and sweep for the maximum overlap; note that the
runner writes that file only when the run ENDS, so an in-flight run has to be reconstructed from its
per-agent transcripts.

## M-31 — two full test suites at once took the box down, twice in one day

The target repository's own `Makefile` documents it in a comment above its memory-cap helper: one
package under `-race` peaks at ~650 MB RSS, `go test ./...` runs up to GOMAXPROCS package binaries
at once (~2.6 GB on 4 cores), a `docker build` compiles the same module again inside BuildKit, and
"the kernel OOM killer resolved that twice in one day by wiping the whole user slice". The box has
7 GB. Every orchestration script written against that repository since carries the same rule in its
own prompts — "run bars ONE AT A TIME: two at once has twice taken this box down" — which is four
scripts re-deriving one constraint, and the scaffold carried none of it. Re-derive the headroom with
`free -g` and `nproc`, and read the target's own build file before fanning out anything that runs
its bars.

## M-32 — a whole-tree bar is twenty times a package one, and the corpus mostly already knew

Over the 1799 `Bash` calls of one completed 30-agent run: whole-tree test runs (`make test`,
`go test ./...`) were **21 calls, 31.6 min, median 124.9 s**; package-scoped runs
(`go test ./internal/<pkg>/...`) were **193 calls, 24.7 min, median 6.2 s**. Eleven of the thirty
agents ran a whole-tree bar at all, and only one ran it five times, spending 7.4 minutes there. So the
recoverable slice is the repeats — roughly ten runs, part of which is a fix agent legitimately
re-running after a correction — and it is single-digit percent of a 407-minute run, not the tens of
minutes a pooled four-run estimate suggested. Banked at its real size deliberately: the rule earns its
place as the precondition for a mutation fan-out, where the same command is not slow but fatal
[M-31], not as a wall-clock win on its own. Re-derive by pairing each `tool_use` with its
`tool_result` timestamp in the run's `agent-*.jsonl` and classifying the command.

## M-33 — the acceptance tail is 23% of a run, and it is paid twice

One completed 30-agent run, 407 minutes: the acceptance step ran **45.4 minutes over 263 tool calls**
and its conditional re-measurement **49.2 minutes over 252**, together **94.7 minutes — 23.2% of the
whole run**. Both are one agent walking 25 properties in series, 21 of them mutation properties. The
re-measurement fired because a gate after the acceptance step had landed a fix (`remeasured: true` in
that run's verdict), which is the mechanism working as designed — an earlier launch on this corpus
returned an acceptance verdict frozen at its PRE-fix value, red over a tree whose hole had just been
closed. Read both figures off a completed run's state file:
`jq -r '.workflowProgress[]|select(.label=="accept" or .label=="remeasure")|[.label,(.durationMs/60000),.toolCalls]|@tsv'`.
The fan-out this argues for is bounded by the concurrency cap [M-30] and constrained by test scope
[M-31], so expect roughly half of it back rather than all of it.

## M-34 — the ownership map, checked against the tree instead of against a self-report

One 30-agent run whose script already compared every producer's reported `files` list to its `owns`
entries: that check filed **six** majors, and every one was a hole in the MAP rather than an agent
exceeding it. Re-asked against the tree — the declared map against the 54 files the run actually
changed — the answer is **nine** unowned files. The four the self-report missed are all test files
(`internal/admin/admin_test.go`, `internal/admin/loopback_test.go`, `internal/mcp/mcp_resources_test.go`,
`web/src/components/ResourcesPage.test.tsx`); nobody named them because nobody was asked. The other
direction produced exactly one row and it explains one of the nine: the map declares `env.example`
while the tree carries `.env.example`, a typo ten rounds of review did not see. Nine plus one over 54
changed files is a sixth of the population. The sharpest single path is
`internal/resources/populate.go`, where the function the whole slice moves lives: it appears in
NEITHER artifact, so no reviewer, no static check and no self-report could have reached it. Re-derive
with `wfcheck -root <repo> --owns-diff <base> <script>`.

---

## Appending here

One entry per rule whose provenance left the rule's own text, with the `[M-nn]` citation written
into that text in the same commit. State what was OBSERVED and by which command where one exists —
an entry nobody can re-derive is held to the same bar this manual holds a reviewer's row to. When a
rule is retired, its entry stays: the record of why something was believed is what stops the next
gate re-arguing it.
