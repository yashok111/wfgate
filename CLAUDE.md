# wfgate — agent context

**Where things are.** This file is the WHY: the architecture, the hard rules,
the bars, and the two newest waves of the maintenance record. `README.md` is the
command reference — every flag, every report line, worked examples; go there
first for "how do I run a round". `skills/workflow-gate/references/` is the
OPERATING MANUAL, which is judgement and is what gate agents are briefed from;
this repository is the mechanics. `WAVES.md` is the maintenance record for
waves 2 to 16, and `IDEAS.md` holds the queue and the do-not-build list with the
measurement that closed each entry.

The `wf` CLI: the bookkeeping of a review gate. A gate's workspace is a git
repository, applying edits is atomic and becomes a commit, freezing a round is
a tag, and a delta round's input is a diff. Go 1.27, module
`git.sumka.site/yakov/wfgate`, `git` is run as a process.

**One dependency: `goja`** (a JavaScript parser and engine, pure Go, no cgo).
The rule used to be stdlib-only, on the argument that a tool watching other
people's artifact supply chain should not grow one of its own; it was lifted
2026-08-22, deliberately, when the two script-gate tools were rewritten from
JavaScript into Go. They read and RUN Workflow orchestration scripts, which are
JavaScript: `wfcheck` needs a parser and `wfdry` needs an engine, and the
alternatives were to keep interpreting them under whatever `node` happened to
be on PATH, or to re-derive a JS parser here. One audited dependency, used by
two binaries, is the smaller risk — and it removed an entire defect class,
because every check that a text scanner could be defeated by simply stopped
existing. Adding a SECOND dependency needs the same kind of argument.

The tool grew out of the measured losses of one real gate (mocker, slice P2c:
7 decision rounds / 94 blockers, 7 script rounds / 14 blockers) and treats
exactly those — see "Why each command is the way it is" below.

**This repository is the single source of truth for the whole gate toolchain**
(consolidated 2026-08-22, made self-shipping the same day): the `wf` binary,
the two script-gate binaries (`wfcheck`, `wfdry` — Node scripts until
2026-08-22, Go since),
and the operating manual, which lives as the `workflow-gate` SKILL
(`skills/workflow-gate/SKILL.md` + `references/`). Nothing about workflows
stands in an agent's always-on context any more: the skill self-triggers from
its one-line description, the binary EMBEDS the references (`go:embed`,
stdlib) and serves them via `wf docs`, `wf init` writes the gate-checks
reference into every workspace so the rules version that briefed a gate's
reviewers is pinned with the gate, and TWO PreToolUse hooks inject the reminder
deterministically — one on the Workflow tool
(`scripts/hooks/workflow-preflight.sh`) for the LAUNCH, and one on `Write|Edit`
(`scripts/hooks/script-authoring.sh`, once per session) for the AUTHORING the
first is too late for. The second exists because the first was the only
deterministic trigger and an agent wrote a whole orchestration script without
ever opening the manual: writing a script is `Write`, not `Workflow`, so the
phase the manual actually governs had no trigger at all. The skill and the hooks
ship as the **wfgate plugin**
(`.claude-plugin/plugin.json`, `hooks/hooks.json`, `skills/` — the repo root
IS the plugin, and its own `.claude-plugin/marketplace.json` makes the local
clone the marketplace): plugin hooks activate on install, so settings.json is
never edited, by anyone. `scripts/install.sh` is the one-command bootstrap
for any machine — `make install` for what a plugin cannot carry (the three
built binaries), `claude plugin` for the rest; it also
removes the retired `~/.claude/skills/workflow-gate` and
`~/.claude/workflow-*.md` symlinks. Before consolidation, 866 lines of
gate-guarding tooling and both specs had no version control at all, and the
P2e false green lived in exactly such a file. The mechanics/judgement split
stands: this repo is the mechanics, the gate-checks reference is the
judgement.

## Architecture

Thirteen units, each one decision — nine Go packages under `internal/`, plus
the emitted template, the dispatcher, the hooks and the skill. Re-count them
against `ls internal/` before quoting the number; it said "eleven" for four
units, which is this file's own most-banked defect class committed in its
opening line.

- `internal/gitx` — the only place that runs `git`. Git is not an integration
  here, it is the storage layer; the wrapper exists so that "the workspace is a
  repository" stays a single decision instead of a shell-out smeared across six
  commands. Being the single seam is also what makes the **diff algorithm
  pinnable**: every invocation carries `-c diff.algorithm=histogram`, because it
  is an UNDECLARED INPUT otherwise (measured 2026-08-25 over the three closed
  gates — git's default is myers, this box carried histogram in `~/.gitconfig`,
  and `wf stats` priced p2e at 4586 changed lines against 4482, 2.8% apart and
  5.3% on its round 2, which moves a round's cost ratio by 0.2 per 100).
  `check drift` is immune — byte-identical across myers/minimal/patience/
  histogram over 235/343/230 citations, because `--unified=0` leaves only
  old-side ranges and their union is stable — and `check stale` moved one range's
  boundary between "the fix wrote this sentence" and "this is an untouched
  consumer" (22 vs 25 scanned of 1891; the verdict held, the denominator did
  not). histogram rather than myers is not a claim about diff quality: it is
  what every banked number was measured under, so pinning myers would silently
  invalidate the corpus. `diff.indentHeuristic` was measured on the same corpora
  and changes nothing, so it is left UNPINNED rather than pinned on principle —
  a guard nothing can make fail is a guard nothing proves. The fixture is the
  real divergent region (p2e r4, `d698b29`) banked under
  `internal/gitx/testdata/`.
- `internal/ws` — the workspace: `gate.json` (the marker; `Find` walks up
  looking for it the way git itself does), `artifacts/`, `rounds/`. The
  findings-table parser (`findings.go`) lives here too — the table is
  **parsed, never generated**.
- `internal/apply` — the all-or-nothing batch of textual edits.
- `internal/check` — the deterministic checks: `cite.go`, `dup.go`,
  `drift.go`, `rules.go`.
- `templates/script.mjs` — the orchestration-script harness, embedded and
  emitted by `wf new-script`. Measured over the six tracked scripts of one
  repository (10 823 lines): the blocks recurring in five or more of them hold
  about a quarter of the lines and are RE-AUTHORED, not copied — two gate
  functions implement one algorithm at 0.05 textual similarity — and on one
  script gate four of eight round-1 blockers were harness shape rather than
  task content. It is NOT a claim that the result is reviewed: two real first
  drafts were already `wfcheck`-clean and cost a round each. `make check-tools`
  runs both tools over the emitted skeleton on every build, so the template
  cannot drift from the rules it instantiates.
- `cmd/wf` — the subcommand dispatcher, and `close.go`: the gate's own record
  and the pruning that record makes safe. ONE dispatch — the test harness used
  to keep a hand-copied switch, which went stale the first time a command was
  added.
- `internal/wfscript` — a Workflow orchestration script as a PARSED PROGRAM:
  the wrapper discovery, the agent-call inventory with its options resolved
  through consts, spreads and factories, the phase titles, the loops. Every
  question the two script-gate tools ask about a script is answered once, here.
- `internal/wfcheck` + `cmd/wfcheck` — the static check over that inventory.
- `internal/wfdry` + `cmd/wfdry` — the dynamic runner: it executes the script
  on an embedded engine with every orchestration hook stubbed, across the seven
  modes a real run can enter. Fixtures live in `testdata/` — the meta-guard
  fixture is the P2e false green, banked.
- `internal/jstext` — the ONE character-level reader of JavaScript, because two
  units need the same split from opposite sides: `wfcheck`'s textual backstop
  needs the CODE (a `agent(` inside a comment or a string is not a call), and
  `check` needs the PROSE (a script's prompts restate the document's decisions
  and must be compared against them). The polarity is two callbacks rather than
  a flag — a comment's `//` is blanked by one reader and is not prose to the
  other, a regex body is words to neither — and each prose span says whether it
  CONTINUES the one before it, which is the half a caller cannot recover:
  blanking the code alone left two literals on neighbouring lines joined into a
  sentence nobody wrote.
- `internal/cli` — `ParseMixed`, shared by all three binaries.
- `scripts/` — no longer code: the two PreToolUse hooks and `install.sh`.
- `skills/workflow-gate/` — the operating manual as a Claude Code skill: a thin
  SKILL.md front door and the gated spec split byte-exact into `references/`
  (TEN of them: authoring, model-routing, interview, pre-flight-gate,
  gate-harness, round-discipline, section-gate, cost-discipline, gate-checks and
  measured — re-count them against `ls skills/workflow-gate/references/`, which
  is also what `make check-docs` stages). The split
  preserved the gated text verbatim — cutting at headings is packaging, not
  rewriting, so no re-gate was owed. `embed.go` at
  the module root embeds the references into the binary; `wf docs guide`
  CONCATENATES them at runtime rather than storing a monolith — a second copy
  of a spec is this project's most-repeated defect class.
  The nine were six until 2026-08-30, when `pre-flight-gate.md` — 1108 lines
  under ONE heading, 40% of the whole manual — was cut along the boundaries
  SKILL.md's routing table was already drawing in PROSE ("steps -2, -1, -1b, 0,
  2, 2b and 4 are the operator mechanics and step 1 is the reviewer's half; the
  file is written in step order, which is NOT the order you execute"). A table
  that has to explain a file's structure is the structure problem admitted in
  the front door. The four are `interview.md` (step -2, before a workspace
  exists), `pre-flight-gate.md` (the gate itself: steps 0, 1, 2b, 3 and the
  three vectors), `gate-harness.md` (the operator's `wf` mechanics: steps -1,
  -1b, 2, 4, the checks, the fix protocol and the cost meter) and
  `round-discipline.md` (barrier, delta rounds, the descent, the stop rule).
  Verified as PACKAGING and not a rewrite by a token census over the four
  against the original — **18 705** tokens in, every one of them out, the only losses the
  `**Round discipline**` lead line and the bare `Rules:` label, both of which became file headings
  (the census is `tr -s '[:space:]' '\n' | grep -c .`; an earlier reading of 18 706 came from a
  different tokenizer and was banked without its command, which is the defect the manual's own
  section A names) — plus eleven
  cross-references re-aimed because they now cross a file (a `below` that no
  longer has anything below it is the citation-drift class this repo built
  `check drift` for, one layer up). What it buys is per-activity load: a
  reviewer read 2237 lines and now reads 1572 (296 + 1060 + 216), and an
  operator running the workspace read 1108 and now reads 557 — figures re-derived after the
  de-duplication commit below, which shrank all four files and left this line reading 1667/558 for
  as long as it took a review round to run `wc -l`. Same class, one commit deep, for the third wave
  running.
  `wf docs` grew `interview`, `harness` and `rounds`, and `make check-docs`
  went from 8 staged documents to 11, and to 12 once `measured.md` landed.
  The tenth is `measured.md` (2026-08-30, the same day): the manual's PROVENANCE,
  never auto-loaded and deliberately out of `wf docs guide`. The rule the
  seventeenth wave banked for perishable NUMBERS — a figure belongs in the
  manual only if an agent CHANGES ITS BEHAVIOUR from the value — was applied to
  the STORY around them, which that pass left untouched: an agent acts on "a fix
  is the largest single source of blockers", not on which gate that was or how
  its rounds split. Fourteen entries so far, cited from the rules by `[M-nn]`.
  Six topics were also collapsed to ONE home apiece, each of which had been
  stated at full length in two files in different words — the class `dup`
  structurally cannot see, and this file's own most-repeated defect:
  `wfdry`'s modes and floors to `pre-flight-gate.md` step 2b, the schema width
  band and the fixture vocabularies to `authoring.md`, `dup`/`[RN]`/`stale` to
  `gate-harness.md`, the fix protocol to `gate-checks.md` §I (with the operator's
  half re-cut as a seven-step COMMAND runbook, which is what the prose blob kept
  losing), and the delta round's three asks and the severity scale to
  `gate-checks.md` §0b and §0. The marked sites were preserved deliberately:
  collapsing a `[R2]` or `[R3]` statement into a pointer would drop that id to
  one site, and `wf check rules` would then compare nothing while
  `make check-docs`'s marker floor read the narrowing as agreement.
  **Measured, and it is well under the estimate the plan was sold on.** The
  loaded manual — every reference but `measured.md` — went 250 475 to 236 953
  bytes, **-5.4%**; a reviewer's set (pre-flight + checks + rounds) 153 408 to
  140 841, **-8.2%**. The estimate given before the work was 25-35%, and it was
  wrong because most of a de-duplicated passage becomes a pointer plus the
  clauses the other copy did not carry, not nothing. What would actually reach
  25% is a harvest across all hundred-odd `gate-checks.md` checkboxes, which is a
  much larger edit at a much higher risk of dropping a rule; it is not done.
  `make check-docs` grew a bar for the citation scheme — it fails on an `[M-nn]`
  with no entry and on an entry nothing cites — and all THREE arms are proven by
  mutation, each failing with its own message: the third is an entry citing
  ITSELF, which used to mask its own absence because the `used` set was read from
  every reference INCLUDING `measured.md`. Nothing was masking anything yet; the
  guard was one edit away from being defeated by the file it guards.

  *And the restructure earned its review round, which is the whole argument for
  having one.* Three vectors against the frozen `3fd3dc0` — one external model
  over the code half, one Opus vector asking only "did an actionable clause get
  LOST", one over the claims — against five green bars. Between them:
  **no blocker, six majors and nineteen findings**, and the bars had caught none
  of them. The rule-loss vector reproduced the packaging claim independently by
  token census and found the split clean, then found exactly two clauses the
  REWRITE dropped, both about `wfdry`'s own output: that `--quiet` does not
  suppress the runner's WARNs (the surviving half, "`--quiet` prints verdicts
  only", read as its negation) and that a mode which THREW prints its own detail
  instead of the `(clean: N agents)` comparison, so a red line's missing
  comparison is the shape of a red line rather than a check that did not run.
  The sharpest finding belongs to the claims vector and is a REACHABILITY
  regression this wave introduced: `wf init` pins `gate-checks.md` into a
  workspace and nothing else, and the de-duplication left NINE `[M-nn]`
  citations in that pinned copy pointing at a `measured.md` no workspace holds,
  with no command named — the manual's own unreachable-pointer defect, committed
  inside the file that states it, and passing a new bar that proves every
  citation resolves INSIDE THE REPO and nothing about the copy a reviewer is
  briefed from. The remaining majors are one class: **a pointer or a number that
  the restructure invalidated and neither commit swept.** `wf docs`'s own help
  said the guide excludes `checks` while the code comment four lines above it
  said `checks` and `measured`; `authoring.md` still said the guide prints "the
  five authoring references" when `guideOrder` is eight; the `references/`
  enumeration here was nine of ten; `make check-docs stages ... the six
  references` was never touched by EITHER commit though both moved that number;
  three `pre-flight-gate.md` pointers in this file named the emptied file,
  because the eleven re-aims swept `references/` and not `CLAUDE.md`; a rule was
  attributed to a "twenty-first wave" that does not exist; and the reviewer line
  count banked one commit earlier went stale inside the next one and sat
  twenty-five lines above a byte figure contradicting it. The token census was
  banked as 18 706 without its command and does not reproduce — every
  whitespace-split reading gives 18 705 — which is section A's own rule about a
  count that becomes a claim, and the commit message's "`guide` now names its
  members rather than claiming five" is INVERTED: the old help enumerated five
  members, the new one enumerates none.

## Why each command is the way it is

This is the one part that cannot be reconstructed from the code: every decision
answers a specific failure, not taste.

**`apply` is all-or-nothing.** A batch of three replacements, the second did
not match, the helper died on it, and the two that HAD matched **were never
written**. The operator saw one error, fixed that one edit, and believed the
batch had landed. It had not, and a round later a reviewer re-filed a finding
marked closed. Hence: the whole batch is verified in memory first, and if a
single edit does not produce its declared occurrence count, **nothing** goes to
disk. A partially applied batch is worse than a rejected one: a rejected one
announces itself.

**`Count` on an edit.** Not a convenience. The author declares how many
occurrences they expect; a rule living in two artifacts and corrected in one is
this gate's most frequent defect, and a declared `count: 2` turns it into an
apply-time error instead of a finding three rounds later.

**Re-reading from disk after the write.** An apply is verified not by the
absence of an error but by reading back what landed: every written file,
compared BYTE FOR BYTE with the buffer the plan produced. It searched for each
edit once, which verified every pure deletion vacuously and rejected chained
edits that had landed perfectly. Same story, its other end.

**`Commit` stages only the named paths, never `git add -A`.** The workspace
often holds a half-written artifact the operator is still drafting by hand, and
an apply that swept it into the same commit would destroy the property "one
commit = one reviewed change".

**`freeze` instead of `<sha8>-artifact` copies.** Freezing used to be a `cp`
of each artifact under a hash-bearing name plus a hand `sha256sum -c` before
launch. One manifest covering the whole set at once, bound to a commit, turns
the check into "the tree is clean at tag `gate-rN`". The manifest also records
the TARGET's HEAD: a frozen gate that cites line numbers is meaningful only
against one revision of the repository under review.

**`cite` prints anchor coverage always.** On the real corpus (round-1 artifacts
of that same gate) — 148 citations, 16 findings, **all** of the
"ambiguous path" class: not one "out of range". The 19 citations the gate
actually caught by hand were off-by-N — both lines exist, the range check is
powerless. Only an `#anchor` catches those, and only where the author wrote
one. So the number of citations WITHOUT an anchor is printed always: a green
run over 148 unanchored citations proved nothing, and the report must not read
as if it had.

**An anchor is ASCII only, and that is policy, not a limitation.** Gate
artifacts are English by workflow rule (decided 2026-08-21 after the P2d live
run). An anchor the grammar does not accept — Cyrillic, spaces — is reported as
an error (`BadAnchor`), never silently dropped into the unanchored count: on
P2d, 15 Cyrillic anchors dissolved into "17 unverified", hiding both that only
two truly unanchored citations existed and a factually wrong anchor among the
invisible ones. Extending the grammar to Unicode was the other road; this one
was chosen — the boundary runs along the artifact language, and the tool
declares it rather than blurring it.

**`dup`: divergence is two conditions, not one.** The first version used
`git log -1 -- <file>`: one edit to `decisions.md` flagged **161 groups out of
176**. A check that paints 92% of its output is a check that gets switched off.
Blame is now per-line, and a group is flagged `DIVERGENT` only when the copies
**both read differently and were last edited in different commits**. That made
it 34 of 176, and they were real divergences ("A round added one" vs "Round 4
added one"). Either condition alone is not enough: identical copies edited
apart are just a rule correctly restated; diverged copies from one commit are a
deliberate rewording the author saw side by side.

**Normalisation in `dup` erases quoting and case.** One rule is written with
plain backticks in markdown and with escaped ones inside a JS template literal.
Exactly that quoting mismatch is what let a corrected rule stand next to its
stale twin: copy-paste between artifacts broke the replacement pattern.

**`check drift` splits drift into two classes because the work differs.**
`verify` says "the target moved" but not WHICH citations that invalidated;
anchored citations surface on their own in `check cite`, while the unanchored
majority passes the range check forever — both lines keep existing.
Intersecting the target's diff (`frozen..HEAD`, `--unified=0`) with the cited
ranges is the only mechanical signal they get. `edited` (the cited lines
changed) — re-read the claim; `shifted` (insertions/deletions above) — re-aim
the number, and the new range is printed immediately so it is a patch, not a
search — the same trade the anchor check's "nearest occurrence" makes. A rename
is deliberately surfaced as delete+add (`--no-renames`): the citation names the
old path, and "the file is gone, re-cite" is that claim's honest state.

**Findings validation: record tolerates an open blocker, freeze does not.**
Findings are recorded when filed; closures land later. A record that rejected
an honest "still open" would teach the operator to write fiction into the
ledger. But a sha that does not exist in the workspace is refused always, and
before writing — a landed lie briefs the next round with fiction, a rejected
one announces itself. Freeze is the moment where the scaffold itself promises
"every blocker closed before the freeze", so the check lives there. A
placeholder row (empty finding text) is a form, not a claim: the untouched
scaffold freezes fine. Freeze checks **two** rounds — the one being closed and
the open one: in the prescribed flow a round is frozen right after opening, its
own findings file is always a blank scaffold, and checking only it would pass
vacuously forever. Found not by reviewing the code but by reconciling the spec
documents against the source — the same method that caught 8 of the 22 blockers
in the gate over those documents.

**The digit rule in the sha detector.** A commit claim is a hex token of 7–40
chars **with at least one digit**: "acceded" and "deadbeef" are pure-letter
hex, and a prose cell mentioning one must not be rejected as a lying sha. An
all-letter short sha (odds ~4e-4) degrades to "verify by hand" — an error in
the safe direction.

**The binary's version in `freeze.json`.** A measured failure, written down the
day it happened: after an edit, a run against the stale installed binary is
indistinguishable from a run against the new one. The `git describe` stamp via
`-ldflags` goes into the freeze manifest for the same reason the target's HEAD
does: it is part of what the freeze was verified WITH. In `verify` a mismatch
is a note, not a MISMATCH: a rebuilt binary legitimately verifies an older
freeze.

**Waves 2 to 16 are in `WAVES.md`**, not here. They are the maintenance record —
what each change answered, and the command that measured it — and they were 80%
of this file, paid in context on every session in this repository. The split
(2026-08-29) moved the text byte-identical; nothing was rewritten, and every
rule from them an agent still has to ACT on was lifted into "Hard rules" below.
Read `WAVES.md` when a decision looks arbitrary, when you are about to
contradict a banked number, or before proposing something that may already be
on the do-not-build list. Its index:

| Wave | Gate / source | What it answered |
|---|---|---|
| 2 | P2d live run (`FEEDBACK-p2d.md`) | The first ten rounds run entirely through `wf`: extensionless citations, `check rules` and `[RN]`, `dup -since`, the anchor ratchet, `-kind full\|delta`, `wf stats`, `wf add` adopting a draft, bookkeeping dirt in `verify` |
| 3 | The residue P2d named | `apply.ParseText` (the plain-text batch format), `delta -save`, `[fix-induced]` in stats |
| 4 | P2e, the first SCRIPT gate (`FEEDBACK-p2e.md`) | Hard-wrap unwrapping in `dup`, `NUMERIC` conflicts, `-kind tool`, dry-run BEFORE freeze, the batch-size cost meter |
| 5 | The five-reviewer audit of this repo | The tool committing the class it exists to catch: freeze writing before refusing, vacuous open-blocker checks, one pipe carrying a BLOCKER past a freeze, `strings.Count(body, "")`, `dup` losing recall as a rule spread, the text scanner replaced by a parsed program |
| 6 | P2f, both gates in ONE workspace (`FEEDBACK-p2f.md`) | `wfcheck`'s unclearable backstop ERROR, `wf stats` reading ZERO apply batches over 27, `wfdry`'s fixture deciding which branch ran, the sentence-terminator defect in `check rules` |
| 7 | mocker slice A, THREE gates (`FEEDBACK-a-mcp.md`) | Four ways the OPERATOR drifted out of the tool in silence; freeze resolving citations; the per-node depth budget; `wfcheck -root`; the pre-flight sequence as a smoke test, not a gate |
| 8 | mocker A3, queue filed while open | `wf check stale -since`, `wf check closure`, the cost meter calibrated on the wrong regime, the corpus SIZE in `freeze.json` |
| 9 | The A3 loop — measured and NOT built | Four proposals rejected on measurement (the do-not-build list); acceptance sections state OBSERVATIONS; `wf round lens`; the stop rule unified at five sites; a scope cut is reviewed like any other edit |
| 10 | mocker A3 closed, the script RAN (`FEEDBACK-a3.md`) | `[RN]` markers inside a `.mjs` (already shipped, never said); FIRST REVIEW leading the stats line; `wf apply -rule RN`; `-kind run` |
| 11 | The two things every agent invented | The severity scale closed to BLOCKER/MAJOR/MINOR/NOTE across four drifted vocabularies; `-asks` and the three questions a delta round may never drop |
| 12 | P3a, first workspace gated TWICE (`FEEDBACK-p3a.md`) | `--mode dead-reviews` matching a SUBSTRING; `--prompt-diff`; the aged `gate-checks.md` pin; JS escapes reaching the comparison undecoded; `-rule` as free text |
| 13 | P3b, the gate that STOPPED (`FEEDBACK-p3b.md`) | Blockers split by ORIGIN, not only findings; the strongest queue entry REFUSED on measurement; the agent-count ratio; `wf freeze -refreeze`; the frozen harness |
| 14 | The five-vector brainstorm | Nothing here is slow anywhere that matters (a closed hypothesis); `make check-docs`; the `dead-reviews` mode that discriminated nothing; phase-set collapse; the moved freeze tag derived from GIT |
| 15 | P3c, the launch-killer (`FEEDBACK-p3c.md`) | The schema-width band measured in the RUNNER; factory-composed `YOU OWN` blocks; `-rule` as a refusal; two closure reports saying NOT A PASS; `check cite -at frozen` |
| 16 | P3d, a probe instead of an argument (`FEEDBACK-p3d.md`) | The mis-split batch that verified itself byte-for-byte; `$defs`/`$ref` resolved in `wfdry`; the refusal floor at 4,233 probed for 82 ms; the ledger refusal naming a CHARACTER; `x-pass` on arrays |

**The seventeenth wave — P3e, and the two findings that were the tool already
saying it** (`FEEDBACK-p3e.md`; 14 rounds, 186 findings, 46 blockers, 183 edits
over 2535 changed lines, two artifacts — a decision document gated for nine
rounds at 10 -> 8 -> 7 -> 2 -> 2 -> 1 -> **3** -> 1 -> 0, then the
orchestration script for four more at 8 -> 3 -> 1 -> 0, then a `run` round whose
scaffold is still unfilled, which `wf close` refuses and `wf stats` names). The
feedback was written with the run in flight and says so, so it carries no number
from the other side of a launch. Its own headline was REFUTED by a measurement
anybody could have run, and the refutation is the more useful result:

- *The flagship item was a REGRESSION REPORT against a shipped fix, and there is
  no regression* — "`wfdry` still does not fill a `$ref`'d leaf". Reproduced
  here in two spellings before a line was written: a `$defs` at the SCHEMA ROOT
  referenced from twenty-two keys built by `Object.fromEntries(...map(...))`
  fills in every mode and always did — the computed keys defeat nothing, which
  was one of the two hypotheses the file offered — while a `$defs` written on a
  SUB-schema does not, because `#/$defs/x` is a pointer into the document the
  schema IS. That is the shape the gate wrote, and it is a wrong pointer rather
  than a runner bug. **The runner already said so by name**, in the violation
  block and again at the tail. That WARN shipped in `effd928`; the
  boolean-polarity WARN the file's §1.2 asks for has shipped since `b143864`
  (2026-08-22), `effd928` only editing its text — both ancestors of the
  `54bd9b2` every freeze in that workspace stamps. So two of three headline tool findings
  were the tool saying the thing and nobody reading it. What was actually owed
  is the PATCH: the note now names where a `$defs` declaring that name IS and
  prints the pointer that WOULD resolve — the trade `check drift` makes when it
  prints the re-aimed range rather than saying a citation moved. The gate
  reverted a correct narrowing over this and paid about 1,900 bytes of width
  against a classifier with a measured refusal at 4,233.
  `testdata/nested-defs.mjs` is that shape, banked.
- *`wfdry --fixture <label>`, because what failed was the READING* — a fourth
  message about a fixture limit would have been the third one nobody read.
  THREE of that gate's dry-run reds were fixture-synthesis limits reported as
  artifact defects — the `$ref` above, twenty-two booleans defaulted false
  because names like `P8a` carry no polarity, and `finalCount === 22`, an
  integer against a constant no report-shaped fixture can satisfy — and each
  cost a reading of twenty-two "unmet" rows to diagnose as ONE gap. The flag
  prints the object the stub actually returned, which separates the three in one
  look, and the clean mode's verdict violation points at it. A label no mode
  composed is refused with the labels the run did compose: the `--dead` typo
  class, one flag over.
- *The `required` census, and `wf check schema -since <ref>`* — the one item
  where the tool held the data and never asked. P3e lost
  `required: ['met','evidence']` from an acceptance leaf inside a hunk whose own
  comment was entirely about width, with no warrant recorded, and ONE frozen
  revision published a report whose every property could come back `{met: true}`
  with no evidence at all — the only
  thing the gate's own evidence reviewer had to read. Nothing static reaches it:
  the leaf is built inside an `Object.fromEntries(...map(...))` callback, so
  `wfcheck` sees no literal, and both static tools were clean over that script.
  Every schema line now prints how many keys the schema requires over every
  object node and how many nodes declare `properties` and require none — the
  second population is 1 object on three of the six GATE scripts (p3c, p3d, p3e),
  0 on the other three, and 0 on all six of the tracked orchestration scripts
  this file also calls a corpus, so it does not paint — and `wf check schema` compares the two revisions
  and NAMES a drop. Validated on that workspace's own history: `accept` goes 74
  required keys and 1 such object at the round-10 freeze to 33 and 23 at round
  11, at a width that FELL from 3,869 to 3,314, so the drop would have been
  named the round it happened. The census is taken over the EXPANDED schema
  (the obligation is per USE, and pricing the two spellings differently would
  move a number on an edit that is only about narrowing) and is NOT taken at all
  over a schema that does not serialize, nor over one carrying a `$ref` that did
  not expand — the honest unserialisable case is a cycle, and a census walking
  one does not return; an unexpanded reference leaves the referenced leaf a
  placeholder, so its `required` is not in the walk and the count is short by
  exactly the amount the author cares about (the banked fixture reports 4
  against 10 declared); `-1` is unmeasured, which is a different claim from
  zero.
- *`wf new-script` writes into `artifacts/`* — it landed in the working
  directory, which is the workspace ROOT when the operator is standing in it,
  and a stray file there is exactly what `wf verify` refuses: the command
  created the state the next command objects to, and the `wf add`, copy and
  delete were paid by hand. It also says the file is UNTRACKED until `wf add`
  adopts it.
- *Three findings changed the MANUAL, not the tool* — **a presence check is not
  a content check**, in `gate-checks.md` §F with both of its rounds: sixteen
  reviewer prompts were told "the diff against HEAD is exactly what this run has
  produced" while `git diff HEAD` shows no untracked file, and the fix
  (`git status --porcelain -uall`) was demonstrated VACUOUS by the next round
  running it — porcelain prints a status code and a path, never bytes, and every
  file the mutations touch already reads `" M"`, so a mutation left in place
  produces the identical two readings. **a `$ref` is anchored at the schema root — write the `$defs` there or write the
  whole pointer** and
  **a red `wfdry` mode is a fact about the script or about the fixture**, both in
  `authoring.md`. And in `gate-harness.md`: **a lens that died for a reason
  outside the artifact is banked too, with what replaced it** — an external lens
  was quota-blocked mid-round and an opus lens carried the identical prompt, and
  banking both is the only form in which a later reading of the descent is not
  confounded with a billing decision.
- *And the wave earned its review round, for the twelfth time running, and this
  one ran THREE vectors* — one external model over the diff, then two Opus
  vectors (Go correctness with a 71-mutation sweep, and claims/specification),
  against five green bars. Between them: **one BLOCKER, ten MAJOR, eight
  external findings and sixteen unproven guards of fifty-six.** The correctness
  vector ran in an ISOLATED WORKTREE, which is this project's own banked trap
  applied on purpose — a concurrent reader of a mutation sweep reads a poisoned
  file, and one wave already filed a red bar that did not reproduce because of
  it.
  The BLOCKER was in the wave's own flagship: `vm.ToValue` hands goja a
  WRITE-THROUGH view of the synthesised map, so every assignment the script made
  to an agent's result landed in the object `--fixture` prints — the flag built
  to separate "a fixture-synthesis limit" from "a script defect" was showing the
  SCRIPT's own writes under the heading "the object the stub returned", and the
  second, clean spawn then deduped against the first's pre-mutation bytes so the
  real fixture appeared nowhere. It is deep-copied at record now.
  Three more in `wf check schema`, each a false green: the no-required DROP arm
  sat AFTER both required-total comparisons and was therefore unreachable
  whenever the total moved, so an edit stripping `required` from every leaf while
  the root's own list grew printed `rose` at exit 0 — the exact shape the check
  exists for, arranged so the total does not fall; `censusOf` consulted only
  `Err` and not `OK()`, so a script whose clean run VIOLATED had every label it
  never reached reported as a deliberate GONE at exit 0; and a label composed by
  NO clean mode had its failure-branch census printed under the clean heading,
  typographically identical, on a mode the corpus band was never measured on.
  The rest was the map the eleventh wave already drew: every guard in the
  COUNTING logic was killed by a test and almost every guard around the
  UNMEASURED values was unproven. Those got tests — including two the reviewer
  proved live on a banked fixture — three dead guards were deleted, and two
  Makefile bars that passed on ANY non-zero exit now grep the message they claim
  to pin. One test that SKIPPED was rewritten to assert its own premise: a
  skipping test proves nothing.
  The external vector had found eight, all real, four HIGH.
  **Asked afterwards whether every blocker and major was actually fixed, the
  audit found four MINORs still open and one of the wave's own doc fixes gone
  STALE from the wave's own later code change** — the README said `check schema`
  reports the no-required population "only where the required COUNT is
  unchanged", which stopped being true the moment that MAJOR was fixed to judge
  the two independently. Its own named class, one commit deep. Of the four:
  three unproven guards got the case they lacked (the clean-width fallback, a
  `$defs` entry that is not an OBJECT, and a runtime throw's own text), and one
  reviewer finding was REFUTED with a reproduction — the second `expanded ==
  nil` is LIVE, and `$defs: { prop: 'not an object' }` is the schema that
  reaches it. Two of the three needed the guard RESTRUCTURED before a mutation
  could reach them: `rs[0].Err != nil` was unreachable for a parse failure (which
  `wfdry.Run` reports as its own error) and produced identical text for
  everything else, so the two branches are one now and what the distinction buys
  is the MESSAGE — a runtime throw leaves an empty violation list, and its own
  text is the difference between a fix and a hunt. A flag refusal also no longer
  exits BEFORE the mode tally and the tail WARNs: an operator's typo in
  `--fixture` used to suppress the one line an operator reads. The worst was the wave's own named class
  committed inside the fix for it: `requiredCensus` expanded `$ref`s with the
  failure map DISCARDED, so a schema whose reference did not resolve produced a
  partial count printed as a measurement — the banked `nested-defs.mjs` reported
  4 required keys against 10 declared. It is UNMEASURED now, like the
  unserialisable case. Three more in `wf check schema`: a typo in `-since`
  failed `ShowFile` per artifact and printed "no comparable copy at <ref>",
  which is the message for a script ADDED since the reference — so a bad ref
  read exactly like a first review of everything, the `check closure -round 999`
  class, and the ref is resolved ONCE up front now and refused; only the CURRENT
  artifacts were enumerated, so a script DELETED since the reference was
  invisible; and the census kept the FIRST spawn of a label, so a label composing
  a per-item schema in a loop compared whichever ordinal ran first against
  whichever ran first at the reference — it is named UNMEASURED, the ambiguity
  class `check cite` reports and `--prompt-diff` refuses, in a third place.
  `--fixture` had the same root and hid every later shape of a reused label.
  Two smaller ones were taken as written: `findDefs` stopped at ARRAYS, so a
  `$defs` under `allOf`/`anyOf` got no hint, and the emitted pointer was not
  RFC 6901-escaped, which hands the author a pointer that does not resolve —
  worse than no hint, because the whole value of the hint is that it is a patch.
  One finding was taken by HALF: "every NOT A PASS path exits 0" is the SHIPPED
  convention here — `rules`, `stale` and `closure` all print NOT A PASS and
  return zero, because the claim belongs in the report and the exit code is for
  findings — so only the unresolvable REF became a refusal. And one was a
  genuine over-claim in the manual: "a `$defs` goes at the schema root" is too
  strong, since a nested one is legal and the tool's own hint prints the pointer
  that reaches it; the rule is now about the ROOT-ANCHORED spelling.
- *The MANUAL stopped carrying perishable numbers, and that is a rule about
  where a measurement lives* — asked by a human as "why are these constantly
  updating figures in the manual at all; what does an agent do with them?", and
  the answer is measured. The references are 2490 lines and **228 of them carried
  a multi-digit number**, `pre-flight-gate.md` worst at 127 of 1029 — and that
  file is loaded into every gate agent's context on every run. The class
  "a stale number in the manual" is the DOMINANT finding of every claims vector
  this project has run: nine in the seventh wave, ten in the thirteenth, six in
  the eleventh, ten in the sixteenth, and eleven of fourteen in this one. That
  same file was carrying **three separate copies of the round-cost median**, all
  stale, plus a fourth of the authoring window and a fifth of the closure census.
  The test that separates them: **a number belongs in the manual only if an agent
  CHANGES ITS BEHAVIOUR from the value and cannot get it from a command at the
  moment it needs it.** Cost medians, per-gate rosters and round counts fail both
  halves — they are operator-planning data and `wf stats -time` prints them; the
  schema-width band fails the second, because the two constants live in the
  BINARY and `wfdry` prints them in the warning, so a document restating them is
  a second copy of one thing. What survives is the rule, the failure it answers,
  the command that re-derives the number, and an order of magnitude where the
  magnitude IS the point ("a round is tens of minutes"), which does not go stale.
  The measurements themselves stay HERE, in this file, where they are a
  maintenance record rather than something an agent acts on. `cost-discipline.md`
  was left alone deliberately: its numbers are thresholds an agent applies by
  hand (`~10-20 KB`, `~15-20 tool calls`, `min(16, cpus-2)`), which pass the
  test.
- *`wf launch` was DEFERRED with its reason written down* — the freeze is
  currently enforced by the operator remembering to `sha256sum` a copy, because
  the Workflow tool would not take the frozen artifact's absolute path. But the
  thing refusing the path is that tool, not this repository, and a command that
  copies-and-hashes is a workaround pinned to a behaviour nobody here controls.
  Build it when a SECOND gate pays for the hand hash, or when a copy is shown to
  have gone wrong once.

**The eighteenth wave — P3g, and the two proposals the tool had already
answered** (`FEEDBACK-p3g.md`; 13 rounds, 106 findings, 34 blockers, 86 edits
over 1131 changed lines, 8.1 h by its own `wf stats -time`, median round
19.6 min, two artifacts — a decision document gated for eight rounds at
4 -> 1 -> 4 -> 6 -> 3 -> 2 -> 0 -> 0, then the orchestration script for five
more at 8 -> 5 -> 1 -> 0 -> 0, both halves closed on the stop rule, and the run
launched from `gate-r13` — **it came back green**: 26 agents, 0 errors,
4 h 27 min, 4.69 M subagent tokens over 1532 tool calls, `green: true` on all
ten verdict terms, HEAD never moved, 22 files changed at +2991/-493, and every
bar re-run BY HAND afterwards including `make smoke` at 155 PASS). It is the
first gate whose script was scaffolded by `wf new-script` rather than copied
from a previous one. Four of its numbers do
not survive its own workspace and are corrected here rather than repeated: the
feedback says 105 findings against the command's **106** (its script half is 41,
not 40), splits the fix-induced blockers 8/4 where `wf stats` says **10/2** (the
total of 12 is right), and prices the gate at 9.3 findings per 100 changed lines
against **9.4**. Its authoring window is **0.3 min**, the shortest in this
corpus by an order of magnitude and a number that measures nothing but the
document having been drafted before `wf init`. Every claim below was reproduced
before a line was written:

- *The schema probe measured a width the binary already knew was safe* — the
  feedback's headline says 3,705 bytes landed "inside the band this corpus had
  never measured (3378 widest known ACCEPTED)" and that the ceiling moves to
  3,705. `SchemaAccepted` has been **3,974** since the seventeenth wave, the
  installed binary was at that revision, and 3,705 is below it: nothing moved,
  and the 24 seconds and 64k tokens bought a number already on record. The
  residue is the tool's, and it is this project's own named class — the two
  constants live in the binary and printed only inside the WARN, so an operator
  under the ceiling read SILENCE and could not tell "measured safe" from "not
  checked". The band is in the `=== schemas ===` header now, on every run.
- *The phase floor already fails a mode that loses a section, and P3g hit the one
  mode exempted from it* — the runner has compared phase TITLES since the
  fourteenth wave and NAMES what a mode never reached, but `dead-reviews` is
  exempt with a written reason: that mode's shortfall is INJECTED by the runner,
  and a script that HALTS on a dead reviewer is doing what the sibling check
  demands. P3g's abort guard ended the run on any dead PRODUCER, so the mode
  reached 11 of 14 phases — a gate, the acceptance step and the last gate
  exercised by nothing — while printing `OK` and `7/7`, because 16 agents clears
  the half-of-clean floor. It is a WARN now, not a failure, naming the phases:
  the exemption is right and silence was not. Safe to ship because it paints
  nothing — re-measured 2026-08-30 over the TEN gate scripts on disk (the three
  P2 workspaces carry one each and were missed by a first count that said seven)
  and all fourteen `testdata/` fixtures: of those twenty-four files the only
  shortfall is the fixture written to produce one, and the twenty-fifth reading
  is the `gate-r11` revision carrying that guard, which the new WARN names as
  `Gate 6`, `Accept`, `Gate 7` — the three a lens found by reading the phase
  list by hand. `testdata/dead-halt.mjs` is that shape banked at 6 agents over
  2 phases clean against 3 over 1.
- *`wf check closure` already checks the LINE, and the finding named none* —
  the feedback reports a claimed fix that changed nothing riding through on a
  file-level match, and says "the finding text named the lines". It did not: the
  round-11 row names `artifacts/p3g.mjs` and no line and no section, so it fell
  into the 41 closures the check reports as FILE-level with the note that says
  cite the section or the line and the check reaches the claim. Over the whole
  gate it checked 89 of 102 and found the one real miss. The proposal's other
  half — check the token a finding QUOTES — is on the do-not-build list, and it
  was re-measured on the gate that asked for it rather than inherited: of 68
  rows quoting a token, **20 would be flagged** and one of them is the real
  defect. Two narrowings were tried and neither rescues it — excluding citations
  into the reviewed tree gives 12 of 55, and additionally requiring the token to
  exist in the artifact before the fix gives 10 of 51, because a decision
  document QUOTES the code it is about, so a target identifier lives in both
  trees. Nine false for one true is `dup`'s 161-groups-of-176 lesson. Those
  three pairs are ONE reading of "quotes a token" — every backticked span of 3
  to 60 characters, a row counted closed when ANY of its tokens appears in any
  closing commit's added or removed lines — and the rule is written down because
  a review vector re-derived four other readings, landed on none of these pairs,
  and got 9 to 27 flagged over populations of 52 to 75. The DECISION is stable
  across every reading (13% to 36% of rows flagged for the one real defect); the
  numbers are not, and a measurement nobody else can re-derive is held to a
  lower standard than this file applies to everyone else.
- *`wf apply -whole`, because the METER lost four rounds of thirteen* — the one
  proposal that was a real gap. A structural merge of two script sections is
  forty-odd non-adjacent edits in one file; it was applied with `git commit`,
  `wf freeze` correctly reported the hand-edited rounds, and the cost meter then
  priced the gate on rounds 2-8 alone (5.1 per 100 against the gate's own 9.4).
  The command adopts the file as it lies, prices the round from the diff's HUNKS
  at `--unified=0` — the maximal split git reports, so nothing is invented and a
  restructure is not priced as one edit — and refuses an unchanged file, an
  untracked one (that is `wf add`), a path escaping the workspace, and `-f`
  beside it. It does NOT claim to be a batch: all-or-nothing and the declared
  `count` are unavailable over a rewrite nothing watched being made, so the note
  says so on every run, a second trailer records the act, and `wf stats` prints
  `(N ADOPTED whole, verified by no batch)` beside the round's ratio. A number
  that is honest about COST and silent about verification must say which it is.
- *The run's own residue is one manual rule, and it is about an assertion
  nothing can check* — the launch's five findings were one class, the class the
  gate's D12.3 had deliberately WITHDRAWN from the acceptance list and handed to
  the section gates, and the gates did find them: three real, all of them a
  comment that survived the change it describes, filed MINOR and carried. What
  the design does not close is that a MINOR carried out of a run is a MINOR
  nobody fixes unless a human does. The one worth a rule is the FALSE finding:
  the acceptance step reported that a discriminating test "does not exist
  anywhere in" a package, and it existed — written by that same run, named after
  exactly what it discriminates. The step is read-only and its report is the one
  artifact nothing downstream re-executes; a property reported MET carries
  evidence a later gate judges, and a claim that something is ABSENT carries
  none. So `authoring.md` now requires such a finding to name the COMMAND whose
  empty output it read. The run also fired its conditional re-measurement
  (`remeasured: true`), which is the third consecutive launch where the
  mechanism P3d's gate argued into existence against no evidence but the corpus
  decided what the verdict was measured over.

- *And the wave earned its review round, for the thirteenth time running, and
  the flagship command was the whole of it* — one external model over the diff,
  then two Opus vectors (Go correctness with a mutation sweep in an ISOLATED
  WORKTREE, and claims/specification), against five green bars. Between them:
  **no blocker, nine majors and eight wrong numbers**, and every code finding
  landed on `wf apply -whole`, the one thing this wave built rather than
  corrected. Two of them are ONE root cause through two doors: the command asked
  git about the INDEX where it meant HEAD. `ls-files` reported a brand-new file
  TRACKED once the operator had run `git add`, so the "that is `wf add`" refusal
  was defeated by a command run for an unrelated reason and a first draft was
  priced as a round's edits; and `worktreeChangedLines` — correct for `-f`,
  which writes its own files unstaged — returns the worktree-against-index diff,
  so a rewrite the operator had already staged read `0 changed line(s)` beside a
  hunk count of 2, the commit body recorded that zero forever, and `batchNote`
  went silent on the one run the command exists for. Both are read from HEAD
  now, and `sumNumstat` is the single parse both callers share. Three more:
  a DIRECTORY passed every check and would have been committed whole under a
  report saying `1 file(s)`; the `Whole` marker was an unanchored
  `strings.Contains` over a message that carries the artifact's own text, which
  is the defect `applyCommitEdits` defends against twenty lines above and is now
  an anchored regex; and `diff.interHunkContext` was unpinned, so a host config
  of 3 merges two changes four lines apart into ONE hunk and halves a round's
  recorded edits — pinned for the reason `gitx` pins the diff ALGORITHM. The
  worst by consequence was a guard that DIVERGED between two spellings of one
  command: `-rule` refused a batch under `-f` and merely printed under `-whole`,
  with `-rule-ack` accepted and ignored — the reason the refusal exists is
  stronger for a restructure, not weaker. Two smaller ones were taken as
  written: a BINARY artifact rewritten end to end has no `@@` line, so the
  refusal asserted it was unchanged, and the corpus band printed over a run
  where every schema was unserialisable — a measured-looking figure beside an
  empty measurement, in the header added to remove a different instance of it
  (`testdata/cyclic-schema.mjs` banks it). One guard was DELETED rather than
  shipped unproven: `filepath.Rel` of two absolute paths cannot fail, and its
  error branch survived every mutation. **Eleven guards survived the sweep and
  eight got the case they lacked** — the `os.Stat` refusal (without it a
  tracked-but-DELETED artifact is adopted as its own removal), the
  absolute-path normalisation, the cost-meter note, `-m`, the `artifacts/`
  boundary, the binary refusal, the band's own population gate, and a by-value
  assertion on both commit trailers, which are a WIRE FORMAT that writer and
  reader both derive from the symbol — the sixteenth wave's banked
  `4233 -> 4589` class, one constant over. Two of the first tests written did
  NOT reach their own condition and were rewritten until they failed: the
  trailer text never entered the commit MESSAGE, and the host git config the
  interHunkContext pin defends against was blanked by the test harness itself.
  Of the eight wrong numbers, the sharpest is P3e's commit count — this file
  said 128 where the workspace says **131**, inherited from the wave before by
  the very bullet whose next sentence reads "Re-count them; do not inherit the
  number", and 128 was correct only before P3e's run round was filled. Also:
  the maintenance record was called "waves 2 to 15" at TWO sites after the
  sixteenth moved into it (`count: 2`, exactly); the paint denominator said
  seven gate scripts where the disk holds TEN (the three P2 workspaces were
  missed) and "all twenty" for what is twenty-four files; the token measurement
  banked three pairs no independent reading reproduces, so the reading is
  written down beside them now; P3e was still called "the newest"; and the
  cost meter's window was blamed entirely on hand-edited rounds when two of the
  seven are excluded by a rule no command changes. One external finding was
  REFUTED with the code: a `dead-reviews` mode that spawned NOTHING gets no
  phase note, and it does not need one — the zero-spawn VIOLATION already
  refuses that run, and a second report of one fact is what the neighbouring
  comment refuses by name.

- *Three findings changed the MANUAL, not the tool* — **a section boundary is a
  COMPILATION boundary**, not a subject boundary (`section-gate.md`): one Go
  package was split across two sections two phases apart because the two
  subsystems read as separate in the decision document, and the package did not
  build in the window between them. **The guard-patching signal is the SECTION,
  not the blocker count** (`round-discipline.md`): the banked form is a run of
  rounds at exactly ONE blocker, P3g's were at 1, 4, 6 and 3 while one property
  was rewritten in all four and produced a blocker in three, so nothing fired
  and the gate took four rounds to act — and WITHDRAWING a guard in favour of a
  reader who holds the diff is now named as a legitimate answer, with the
  reusable half of why its two intermediate forms failed: **a hand-written
  enumeration cannot be complete, and a fixed pattern is both incomplete and
  over-complete against the same tree** (the two `rg` commands missed the two
  claims the slice most obviously falsified and printed two hits about nothing,
  both halves found by different lenses in one round). And **a marker goes on a
  site that STATES a rule, never on one that points at it** — said where the
  author reads before marking, because `wf check rules` names the cause under
  its output, which is one round too late to save the markup pass.

**`wf close` and `wf prune`, and why the record is not the repository.** Three
gates were written up by hand into `FEEDBACK-p2*.md`, each inventing its own
structure, each landing in this repository by a manual commit — the p2d file
ends "*the commit is yours*". The section that turned out to be the most
valuable is called "One observation about the process, not the tool" in two of
them and "One number for the planning table" in the third, so the corpus cannot
be grepped for its own best section. And the numbers were retyped: the first of
those post-mortems closed its gate on 99 findings and 27 blockers against a true
98 and 26. So the scaffold has fixed headings and the derived half already in
it, and a filled scaffold is never overwritten — that is the `findings.md`
lesson applied to the form that comes after.

What the snapshot takes is what nothing else holds. On the three closed gates
`.git` is 61-84% of the workspace (3.8-4.4 MB) and the entire history anyone has
ever read out of it — every commit's time and subject, which is what prices a
round — is 8.7 to 11.9 KB of text. So `close` writes that, `wf stats -time`
verbatim, the freeze manifests minus the artifact hashes that stop meaning
anything, and every round's ledger. `prune` then deletes `artifacts/`, `rounds/` and — LAST, so every
intermediate failure is still a `git checkout` away — `.git`: measured 4.2 MB
down to 696 KB on one closed gate. It refuses without
a record, refuses one committed together with the work it describes (a commit is
its own ancestor, so `--is-ancestor X X` passed), and reports without `-yes`.
Deletion is the one operation in this tool that a later command cannot announce,
so the record holds the artifacts themselves by default — their CONTENT is what
nothing else keeps, and a record of the bookkeeping alone is not a record of the
gate. `wf close -artifacts=false` opts out, and prune then names every artifact
by name before removing it.

**`parseMixed` instead of `fs.Parse`.** `flag.FlagSet` stops at the first
positional argument, so `wf init p2c -target X` silently dropped `-target` and
printed a usage error naming the very flag that was passed.

**`safeJoin` in apply.** The edit list is usually written by an agent, and an
agent that has confused the workspace with the repository under review must be
stopped here, not discovered by a commit in the wrong tree.

## Hard rules

- **origin is Gitea** `ssh://git@git.sumka.site:2222/yakov/wfgate.git`, branch
  `main`, private. **Never push to GitHub.** The module path matches origin.
- **The workspace is never inside the repository under review.** The gate's
  agents diff that tree against its own HEAD, and the rule "the tree stays
  untouched while a run is live" does not survive orchestration commits into
  it. `target` is read-only: no command writes into it.
- **The checks never impersonate coverage.** Of a real gate's blockers, about a
  fifth mechanises (wrong citations, stale copies). The rest is a model's
  judgement over a specific tree. A tool claiming to replace that would be the
  worst outcome: it would look like coverage.
- **The rules index is not auto-generated.** The gate-checks reference
  is valuable because it is short enough for every reviewer to read. The tool
  may propose a line; a human must write it in — otherwise the file becomes a
  log and stops being a checklist.
- **`CLAUDE.md` and `WAVES.md` carry NO `[RN]` marker.** A rule is STATED in
  `skills/workflow-gate/references/` and in `README.md`; everything here talks
  ABOUT rules, which by the check's own note is a pointer and disagrees with
  every statement of the id it wears. Measured 2026-08-29: 15 markers across the
  two files, every one of them narrative, producing 3 conflicts against a manual
  that agrees with itself. Written `R2`, never in the bracket form the check scans for. Run
  `wf check rules` over the manual AND these two files together when in doubt —
  it must report the manual's own 9 marked sites and nothing else.
- **A change under `skills/` or `hooks/` bumps `version` in
  `.claude-plugin/plugin.json`.** `claude plugin update` is VERSION-keyed —
  measured: it copies nothing when the declared version is the one already
  installed, so a spec fix committed without a bump never reaches a session.
  `scripts/install.sh` forces a reinstall when `wf docs` reports drift, but that
  is the backstop; the bump is the mechanism. The plugin sat four releases
  behind the clone once, and the SKILL.md sessions loaded described a rule the
  manual had already replaced.
- **Gate reviewers run on SONNET.** Every lens of a `wf` gate round — decision
  gate and script gate alike — plus the section-gate reviewers inside a script.
  Set 2026-08-29 after the round count, not the launch, turned out to be what
  exhausts the operator's quota: a reviewer's tier is paid once per ROUND, and
  the ten gates on record spent 166 of them (re-derived 2026-08-29; the count
  grows every gate, which is the point). Opus on a reviewer is a per-round
  exception that NAMES the miss it guards against and is banked in the roster
  (`wf round lens -model`). The manual's own opus-for-all-three pre-flight
  policy is retired; do not re-instate it from memory of an older reference.
- **Artifacts are changed through `wf apply`, never by hand.** A hand edit plus
  `git commit` gives up both all-or-nothing and the declared `count` — the two
  reasons the command exists.
- **A perishable number does not go in the manual.** A figure belongs in
  `skills/workflow-gate/references/` only if an agent CHANGES ITS BEHAVIOUR from
  the value and cannot get it from a command at the moment it needs it. Corpus
  sizes, round counts, cost medians and per-gate rosters fail that test —
  `wf stats -time` prints them — and a constant a check acts on lives in the
  BINARY, which prints it, so a document restating it is a second copy of one
  thing. The manual carries the RULE, the failure it answers, the command that
  re-derives the number, and an order of magnitude only where the magnitude is
  the point. Measured 2026-08-29: 228 of the references' 2490 lines carried a
  multi-digit number, one file held THREE stale copies of one median, and
  "a stale number in the manual" is the dominant finding of every claims vector
  this project has ever run. The measurements live in the maintenance record —
  the two newest waves in this file, everything older in `WAVES.md` — where they
  are a record rather than something an agent acts on. The same test was applied
  to THIS file on 2026-08-29 and it failed it: 80% of it was that record, paid
  in context on every session, so it moved.
- **Command semantics are specification.** The `skills/workflow-gate/references`
  passed a gate and describe what each command does and why. Changing
  semantics or output means reconciling the references against the source, in
  the same commit (8 of that gate's 22 blockers were text-code divergences,
  and the 2026-08-21 reconciliation by the same method found the vacuous
  enforcement in freeze).
- **A message carries the PATCH, not the diagnosis, and a target this tool
  INFERRED is announced with the flag that overrides it.** Every place an
  operator must then do something says what to do in the same line — `check
  drift` prints the re-aimed range rather than reporting that a citation moved,
  the ledger refusal names the column of the offending pipe and quotes the tail
  it could not place, the dry run's unresolved-reference note prints the
  pointer that would resolve, `wf round lens`'s read error says the argument is
  a PATH. Each of those was a round paid for a message that stated the
  diagnosis and stopped. The inference half is the same failure one layer up: a
  note meant for round 10 was recorded into round 11 because `wf round new` had
  advanced the counter and `-round N` stood in a freeze refusal's advice and in
  no usage block, so `round record` and `round lens` now name the round they
  chose — and ONLY where they chose it, because restating a number the operator
  typed is a line that paints every run and is read on none. The reviewer-side
  half of this rule is `gate-checks.md` §0c, which holds a finding's row to the
  same bar.

The seven below were lifted out of `WAVES.md` on 2026-08-29 because they are
the entries an agent still has to ACT on rather than merely read. Each names
the wave it came from; the measurement is there.

- **A mutation that breaks the BUILD is not a surviving guard.** The sweep
  scores a compile error as "the bars stayed green", and this file records the
  trap being met four separate times (waves 14, 16). Two more read as SURVIVED
  and had not been because `sd` exits ZERO when it replaces nothing (wave 11):
  verify the replacement LANDED, never the exit code. And a mutation sweep
  poisons the tree it runs in — a concurrent reader of one reads a mutated file,
  which is how a red bar that reproduced in no later run was filed (wave 16).
  Run a sweep in an isolated worktree, or run it alone.
- **A guard that survives every mutation is deleted, not shipped.** Four waves
  running have deleted one rather than ship it unproven, and one of them was
  dead code. If it cannot be made to fail, either it guards nothing or the
  reasoning belongs in a comment — both of which are cheaper than a line that
  reads like protection.
- **An absent measurement never prints in the register of a measured one.**
  The single most repeated defect in this project's history: `NOT A PASS` over
  an empty population, `0 conflict(s)` over nothing compared, "no banked lens
  asks X" over a roster that predates the flag, `1/1 modes completed` over a run
  that executed nothing. Every report that can have an empty population says so
  in its HEADER, and the condition is `compared == 0` and never
  `markers > 0 && compared == 0` — an unmarked corpus compares nothing either
  and is the larger population.
- **When code and its comment disagree, suspect the COMMENT first.** Named in
  waves 8, 12 and 16 as this project's most frequent defect wearing the
  reviewer's clothes: a reviewer finding was refused after measurement because
  the code was right and the sentence above it had gone stale. Re-run the
  command before believing either.
- **A floor is armed on a DIFFERENT field than the one it constrains.** Armed
  on the same field, it disables itself at exactly the degenerate value the
  defect produces — four instances in ONE script over three rounds (wave 7), and
  applied deliberately in wave 14 so a producer-only script is not accused of
  ignoring deaths it never had.
- **A new report is narrowed BEFORE it ships, not after.** `dup`'s first
  version painted 161 groups of 176 and would have been switched off; the same
  lesson has since been applied pre-ship twice (the closure check at 89 of 127,
  the blocker-origin split at eleven of twenty-two rounds). A check that paints
  most of its output is a check nobody reads.
- **A shipped capability the report never mentions is a capability nobody
  has.** Measured three times: `[RN]` markers inside a `.mjs` worked and cost a
  gate six blockers, `-asks` took a comma list while nineteen hand edits were
  paid against it, and two of one gate's three headline tool findings were the
  tool already saying the thing (waves 10, 13, 17). Shipping the behaviour is
  half the change; the other half is the line that says it.

## Bars

```bash
make test        # go test ./cmd/... ./internal/... -race -count=1
make lint        # go vet + gofmt (+ golangci-lint when installed)
make build       # ./wf ./wfcheck ./wfdry with the version stamp (-ldflags)
make check-tools # the BUILT binaries against the banked fixtures (meta-guard must FAIL)
make check-docs  # `wf check rules` over the shipped manual — it is a gate corpus too
make fmt         # gofmt -w; `make lint` only REPORTS the diff
make install     # ~/.local/bin/{wf,wfcheck,wfdry} — owed after every code change
make clean       # remove the three built binaries
```

`make test` counting `ok` lines is not a green bar — one package can fail while
the count looks right. Read the FAIL lines.

`make check-docs` stages README, SKILL.md and the ten references into a scratch
workspace and runs the two checks that read prose over them. It exists because
the checks that gate other people's artifacts had never been run over this
repository's own, and on their first run they were RED: `wf check rules`
reported 2 conflicts — `R2` and `R3` marked on POINTERS inside one README
command cell, which is the class the ninth wave banked a rule against, standing
in the manual that ships the check. The rules half is a bar; the `dup` half is
printed as a population and never fails, because a reference quoting a rule
verbatim is not drift and a bar that failed on that would be `dup`'s own
161-groups-of-176 lesson repeated. Two assertions guard the rig: an artifact set
carrying no markers exits ZERO under a header reading "NOT A PASS, nothing was
compared", so a copy step that silently narrowed the corpus would read as green,
and the staged file count is compared against the source list because a
reference renamed out of the glob takes its rules with it.

After any code change — `make install` (`~/.local/bin/wf`): the documents
prescribe `wf ...` commands, and a run against the stale binary looks exactly
like a run against the new one. The stamp in `freeze.json` and the note in
`verify` catch it after the fact; they do not replace the reinstall.

**`make install` comes AFTER the commit, never before it.** The version stamp is
`git describe` through `-ldflags`, so a binary built over a dirty tree installs
as `<sha>-dirty` — the sha of the commit the change is NOT in — and every
`freeze.json` written with it records a stamp that names no revision anybody can
check out. Observed 2026-08-29 in this repository: `wf -version` read
`5f74a49-dirty` after an install run one step early, with the wave's own commit
still unwritten. Order: bars, commit, `make install`.

Tests live beside what they pin: `internal/apply` (the lost-batch regression,
the vacuous deletion, two spellings of one path, the chained batch),
`internal/check` (citations, anchors, divergence on a real git repo, rule
markers, the operator conflict, drift), `internal/ws` (the ledger parser),
`internal/gitx` (porcelain quoting), `internal/wfscript` (every script shape
that defeated the text scanner), `internal/wfcheck` and `internal/wfdry` (one
case per finding and per guard), and the end-to-end files under `cmd/wf`
driving the prescribed sequence and asserting on OUTPUT.

Coverage is NOT complete and the claim that it is has been wrong once: a
mutation sweep after the first fix round found 33 guards deletable with the
bars green. Before claiming a guard is proven, delete it and run the bars.

## Style

As in `mocker`: comments explain the WHY and carry the measured reason ("161
groups of 176", "148 citations, none out of range"), never a retelling of the
code. Everything is English — code, comments, and documents alike (the workflow
is Latin-script by policy; Cyrillic may appear only as quoted DATA, e.g. test
fixtures for the non-ASCII anchor detector).

## State as of 2026-08-29 — read before trusting

- **ELEVEN real gates have now run entirely through `wf`,** and P3g
  (`FEEDBACK-p3g.md`, `~/projects/gates/mocker-p3g-deep-nesting`) is the newest:
  13 rounds by its own `wf stats`, 106 findings, 34 blockers, 86 edits over 1131
  changed lines, 8.1 h, median round 19.6 min, over TWO artifacts — a decision
  document gated for eight rounds and an orchestration script for five, both
  halves closed on the stop rule, and the run launched from `gate-r13` with
  `wfcheck` 0/0, `wfdry` 7/7 and `wf verify` clean, and the run came back GREEN
  on all ten verdict terms — 26 agents, 0 errors, 4 h 27 min, 4.69 M tokens,
  22 files at +2991/-493, every bar re-run by hand afterwards. It is the first
  gate whose script was scaffolded by `wf new-script`, and the skeleton was `wfcheck`-clean
  and `wfdry` 7/7 as emitted and stayed that way — while all THREE harness edits
  its operator made were wrong on the first attempt, each caught by `wfdry` or a
  lens and none by reasoning, which is the manual's frozen-harness rule measured
  rather than argued. Its fix-induced share is 25 of 106 findings and 12 of 34
  blockers (35.3%), triangulating with P3c's 34%, P3d's 35% and P3e's 43.5%. Its
  cost meter prices only rounds 2-8, for TWO reasons and not one: rounds 10-13
  were applied by hand, which `wf apply -whole` now closes, and rounds 1 and 9
  are excluded by the rule that a window ADDING an artifact prices a corpus its
  batches did not write, which no command changes. Its workspace is on disk and unpruned,
  and it is what the `dead-reviews` phase note was validated against.
- **TEN real gates ran entirely through `wf` before it,** and P3e
  (`FEEDBACK-p3e.md`, `~/projects/gates/mocker-p3e-nested`) is the one before: 14
  rounds by its own `wf stats`, 193 findings, 46 blockers, 183 edits over 2535
  changed lines, over TWO artifacts — a decision document, 1323 -> 2091 lines
  over nine rounds, and an orchestration script gated for four more. Its
  decision half descended NON-monotonically (10, 8, 7, 2, 2, 1, **3**, 1, 0) and
  both halves closed on the stop rule. Its fix-induced share is 76 of 186
  findings (40.9%) and 20 of 46 blockers (43.5%), which triangulates with P3c's
  34% (21 of 61) and P3d's 35% (30 of 85) on the blocker half. Its round 14 is the `run` round, and it was an UNFILLED scaffold while the
  launch was in flight — `wf close` refused to bank the gate in that state, the
  first time that guard has fired in this corpus and it fired right. **The run
  has since returned and the round is filled**: by the workspace's own
  `wf stats` the gate is now 193 findings over 14 rounds, with round 14 filing
  7 and no blocker. The launch came back **`green: false` on exactly one of ten
  verdict terms** — `inconsistent: ["gate1"]`, a reviewer returning
  `verdict: "blockers"` while filing one MINOR — over 29 agents, 0 errors,
  4 h 40 min and ~5.0 M tokens, with every bar re-verified by hand afterwards.
  Its acceptance step found SIX of twenty-two properties observed by NO test
  while all five bars were green, three of them BLOCKER, and the conditional
  re-measurement then re-verified all twenty-two over the edited tree: that is
  the second launch running where the pre-fix measurement would otherwise have
  been the reported one. Its workspace is on disk and unpruned, and it
  is the corpus the `$ref`-pointer note, the `required` census and
  `wf check schema` were all validated against.
- **Nine real gates ran entirely through `wf` before it,** and P3d
  (`FEEDBACK-p3d.md`, `~/projects/gates/mocker-p3d-datasnap`) is the one before: **15
  rounds by its own `wf stats` today, 319 findings, 85 blockers**, 446 edits over
  3933 changed lines, median round 32.3 min, over TWO artifacts — a decision document
  that went 752 -> 1481 lines in eight rounds, one undispatched round nobody
  filled, and an orchestration script gated for five more. It is NOT the largest
  gate here: P3b is 588/177 over 22 rounds and P3a 455/143 over 24. The 14
  rounds / 314 findings this file banked for it are the numbers BEFORE its run
  round was filled; the command is what to re-run. Its script
  half closed at 14 -> 7 -> 6 -> 1 -> 0. Its span is 9.1 h in the banked
  `close/stats.txt` and 9.2 h re-run today, because the figure is last commit
  minus first and the close commits land after the work; the 7.9 h in the
  feedback's own header is an earlier close of the same gate, and the
  reproducible number is the one the command prints. **It LAUNCHED, after the
  gate closed, and came back green** — 32 agents, 0 errors, 4 h 31 min, 4.95 M
  tokens — and the launch is the only validation in this corpus of a mechanism
  the gate argued about: the acceptance step returned `criterionMet: FALSE` over
  five GREEN bars, having found BY MUTATION that one acceptance property's
  subject was observed by no test (it deleted live rows and watched two whole
  suites stay green), the accept gate raised it, a fix agent added the missing
  fixture, and the conditional re-verification over the edited tree returned
  TRUE with `make smoke` at 225 PASS / 0 FAIL. Before round 12's fix every
  acceptance term was computed BEFORE the accept gate, so that run would have
  returned `green: false` on a `criterionMet` frozen at its pre-fix value — red
  over a tree whose hole had just been closed. Round 12's blocker 6 was not
  hypothetical; it was the first thing the run did. Its most
  valuable measurement is a PROBE it ran instead of an argument: 4,233 bytes
  REFUSED by the launch's safety classifier in 82 milliseconds at zero tokens,
  3,114 ACCEPTED — the narrowest refusal on record, taken after two of its rounds
  had argued about the band from measurements of SIZE alone. The same probe
  proved the launch RESOLVES `$defs`/`$ref` and enforces the constraints reached
  through one. Its workspace is on disk and unpruned, and it is what the `$ref`
  fixture support was validated against.
- **Eight real gates ran entirely through `wf` before it,** and P3c
  (`FEEDBACK-p3c.md`, `~/projects/gates/mocker-p3c-ref-recipe`) is the first
  where BOTH halves closed on the stop rule: 10 rounds by its own `wf stats`
  (nine plus the run round), 153 findings, 61 blockers, 20 apply batches over 43
  edits and 2408 changed lines. The decision gate ran five rounds at 15 -> 7 ->
  10 -> 6 -> 0 blockers and the script gate four at 11 -> 9 -> 1 -> 0. **It
  launched, and the run died on its LAST agent** — `output schema too large to
  classify safely` at a width the gate had measured, written down and accepted,
  on the stated ground that "what cannot be measured from here is acceptance
  itself, only size", which was false: one throwaway agent answers it in 16
  seconds. The relaunch after narrowing ran 15 of 15. Its workspace is on disk
  and unpruned, and it is the calibration corpus for the schema-width meter.
- **Seven real gates ran entirely through `wf` before those,** and P3b
  (`FEEDBACK-p3b.md`, `~/projects/gates/mocker-p3b-resources`) is the first that
  **stopped instead of closing**: 22 rounds by its own `wf stats`, 588 findings,
  177 blockers, 451 edits over 8388 changed lines, 25.7 h, median round 29.4 min.
  Its document half closed on the rule at 19 -> 0 over seventeen rounds; its
  script half ran four and was halted by the operator. It did NOT launch. The
  workspace is on disk and unpruned, and it is now the calibration corpus for the
  blocker-origin split and for the closure check that was REFUSED on it.
- **Six real gates ran entirely through `wf` before those,** and P3a
  (`FEEDBACK-p3a.md`, `~/projects/gates/mocker-p3a-resources`) is the first
  workspace gated TWICE — sixteen rounds over a decision document, closed, then
  the orchestration script added as a SECOND artifact to the same workspace and
  gated for more rounds plus a tooling round. **Its numbers here were wrong for
  three waves and are re-derived from its own `wf stats -time`: 24 rounds, 455
  findings, 143 blockers, 22.4 h over the 22 review rounds, median 21.5 min —
  and it DID launch, twice**, as `-kind run` rounds 23 and 24 spanning 224.1 and
  447.8 minutes. The claim that it did not, and that A3 was the only gate with a
  number from the other side of a launch, stood in this file until a review round
  re-ran the command. Its workspace is on disk and unpruned, which is what let
  the prompt-diff feature be validated against the revision before its own
  closing commit.
- **Four real gates ran entirely through `wf` before those.** P2d (10 rounds, 98
  findings — `FEEDBACK-p2d.md`), P2e, the first over a workflow SCRIPT
  (17 rounds, 234 findings — `FEEDBACK-p2e.md` says 235, and its own `wf stats` says 234; the
  hand-count is the one that is wrong — 293 KB of artifacts), and P2f,
  the first carrying both gates in one workspace (13 rounds, 111 findings, 180
  KB over three artifacts — `FEEDBACK-p2f.md`; its implementing run then
  executed 20 agents over 149 minutes and came back green). P2e
  confirmed live: the anchor check turning a wrong citation into a patch, the
  ratchet firing on a real regression, record rejecting a placeholder sha,
  the fix-induced share measured at all, delta briefs at ~1/10 of a re-read. P2f
  added the first live yield from `check rules` — marked before the gate closed,
  it found a divergence across the derived pair on its FIRST run, which is the
  case the feature was built for — plus eight mis-aimed anchors and three
  citations that resolved, sat inside the file and pointed at the wrong lines.
  And mocker slice A (`FEEDBACK-a-mcp.md`, `~/projects/gates/mocker-a-mcp`): 25
  rounds, 196 findings, 58 blockers, THREE gates in one workspace — a decision
  gate and one script gate per shipped slice — 262 KB over four artifacts, both
  slices shipped with every bar green. It is the first run whose report is
  mostly about the OPERATOR: four ways of drifting out of the tool that the tool
  watched in silence, which is what the seventh wave (`WAVES.md`) answers.
  `check drift` is STILL unexercised live.
- **The validation corpus DOES survive, and it is the best evidence this
  repository has.** The claim that it did not was stale and cost an
  investigation an hour of archaeology before it was checked: **TEN** gate
  workspaces are on disk with full git history — `~/gates/p2d`, `~/gates/p2e`,
  `~/gates/p2f` (82 / 102 / 87 commits), and
  `~/projects/gates/mocker-a3-cas`, `mocker-p3a-resources`,
  `mocker-p3b-resources`, `mocker-p3c-ref-recipe`, `mocker-p3d-datasnap`,
  `mocker-p3e-nested`, `mocker-p3g-deep-nesting` (153 / 203 / 247 / 82 / 173 /
  131 / 117 commits, all counts re-derived 2026-08-30 — 128 stood here for one
  wave and was the count BEFORE P3e's run round was filled, inherited by the
  next wave from the bullet whose own next sentence says not to). This
  bullet listed three of them for three waves, which is why a census that called
  itself complete missed A3 entirely — and it then listed SIX for one more wave,
  which is why the fifteenth wave's own closure census said "six workspaces"
  while running over seven. Re-count them; do not inherit the number. `~/projects/gates/mocker-a-mcp`, the
  eleventh, has been CLOSED AND PRUNED: no `.git`, no
  `artifacts/`, no `rounds/` — its numbers survive only in `close/stats.txt`,
  `close/history.tsv`, `close/freezes.tsv` and `close/rounds/`, and `wf stats`
  cannot be run there. Read the banked file; do not try to re-derive them. The six tracked orchestration scripts are in
  `~/projects/mocker/.claude/workflows/` (769-2658 lines each, 10 823 total; a
  live gate's own script sits there untracked while it runs). Run `wf stats
  -time` inside a workspace before reasoning about cost; the commit timestamps
  are what price a round, and nothing else records them. To rebuild a lab from
  scratch: `git clone --no-hardlinks ~/projects/mocker <lab>/head`, then a
  workspace over any set of documents.
- **The cost of a gate is now measured by the tool, and the answer moved the
  work.** `wf stats -time`, re-pooled 2026-08-29 over the TEN unpruned
  workspaces with the current binary, over the 138 round spans of 154 that are
  under two hours: **median 23.4 min, middle half 17.2-30.8**, the review segment
  a median 90% of a round and the bookkeeping tail a median 2.9%. The other
  sixteen are breaks (the longest 637.9 min) and none of them is a round length.
  That denominator is NOT the same population as the nine-workspace pooling one
  day earlier (119 of 129): this one counts every round line carrying a span,
  run rounds included. Re-derive it with the command and say which population
  the number came from.
  The old figures — "10-40 minutes, pooled median 15.5, review 70-87%" — were
  three gates and are not wrong so much as SMALL: per-gate medians run 13.8,
  24.3, 19.1, 45.3, 21.5, 29.4, 39.0, 32.3, 21.4, 19.6, so the round got longer
  as the gates got bigger, and a number banked from the first three under-prices
  every gate since. Three of those per-gate medians differ from the eight-workspace
  pooling one day earlier (13.8 against 14.2, 19.1 against 19.5, and 45.3
  against 37.6) — re-derive them with the command rather than inheriting them.
  So the ROUND COUNT is the cost function, the ~9-command sequence is not worth
  compressing (1.4 minutes across p2d's eleven rounds), and the largest measured
  saving in the whole investigation is a DOCUMENT change already in the manual:
  running the decision gate to exhaustion before the script exists took one gate
  from 10+6 rounds to 8+3, about an hour. The authoring window — init to the
  first artifact — is **0.3 to 76.5 min (9.9, 31.7, 42.2, 4.3, 5.0, 39.2, 2.2,
  20.9, 76.5, 0.3; median 15.4)**, and this file quoted only its two largest for
  three waves before quoting a ceiling P3e then doubled and a floor P3g then cut
  by seven times. It is the largest block no
  round covers, and the short end is worse than it looks: those are the gates
  whose document was drafted BEFORE `wf init`, so the authoring itself falls
  outside every measurement banked here. `interview.md`
  (step -2) is what now occupies that window, against a round-1 blocker baseline
  of 7, 15, 7, 13, 15, 19, 15, 13, 10, 4.
- **The seventh wave was reviewed the same way, and the review is again why it
  is trustworthy** (2026-08-24; two Opus vectors — Go correctness with a
  mutation sweep, and the claims/specification — plus one external model on the
  whole diff). It found FIVE blockers in a wave whose four bars were green, and
  the flagship feature was the worst of them: `checkFixerReach` ran over ZERO
  paths on both scripts of the very gate it was derived from, because `owns:
  closingOwns` is an identifier and the code type-asserted straight to an array
  literal. It also accepted any object carrying an `owns` key as a gate config
  (an agent's own options object reported full coverage on a script with no gate
  at all; a JSON schema whose `properties` names `owns` disabled the check
  script-wide, as an INFO), compared whole `owns` entries by string equality
  against entries that are SENTENCES about paths, and had moved the ownership
  denominator behind the parse so `0 owned path(s) parsed from 4 prompt(s)` — the
  line `authoring.md` tells authors to read — could no longer be printed. The
  claims vector found nine numbers wrong against the workspace's own banked
  `wf stats`, including the corpus claim above. Every fix carries a test that
  fails without it; two of the first tests written did NOT fail under mutation
  and were rewritten until they did. Same lesson as the wave before, now
  measured twice: a tooling change earns its review round.
- **The 2026-08-23 tooling wave was reviewed by three disjoint vectors, and the
  review is why it is trustworthy.** Two opus agents (one on Go correctness, one
  on the claimed measurements and the specification) and one external model on
  the whole diff. Each found what the other two missed: the correctness vector
  found a defeated guard in front of `wf prune`; the claims vector found that
  `wf stats` was still printing a rule the same session had retired from the
  manual, and that nine of the session's own numbers were wrong; the external
  one found that the emitted skeleton could report green over a gate nobody
  reviewed. The session had introduced TWO false greens into the toolchain built
  to refuse them — including a strict regression that silenced a real
  dual-ownership ERROR — and no bar, test or self-review caught either. Budget a
  review round for a tooling change the way a gate budgets one for an artifact;
  it cost about one round and paid three times.
- The history of decisions and measurements is in the git log; this file keeps
  only what the commits cannot reconstruct.
- The idea queue and the do-not-build list — `IDEAS.md`; as of 2026-08-21 the
  "build" queue is fully closed.

## What is deliberately absent

Not forgotten — deferred, so the gap does not read as an oversight:

- **No MCP server.** CLI first — and the live run measured the question: zero
  agent↔CLI seam errors over 10 rounds. The bottleneck was assembling the JSON
  edit batch, which is a different layer. Reopen only with new data.
- **Still no findings registry as data — only a parser.** `findings.md` is
  filled by hand; `ws.ParseFindings` reads the table to validate closures
  (record/freeze) and to print the delta checklist, but generates nothing and
  introduces no second format. The format is the same scaffold that four gate
  rounds — and ten more on the live P2d gate — filled by hand unchanged.
- **No `round close`, no reopening a round.** Freezing is one-way BY DEFAULT:
  if tag `gate-rN` exists you open the next round, and `wf freeze -refreeze` is
  the recorded escape — it moves the tag, banks how many times the round was
  published and which commits those publications named, and refuses when the
  bytes have not changed. `wf close` is a different thing
  with the same word in it: it closes the GATE, and it only reads rounds.
- **No check that "the cited line says what the sentence beside it claims" —
  ever.** That is judgement; `wf check cite -show` exists to make it cheap, not
  to pretend it has been done.
- **No autofixes.** `check` only reports. A tool that fixed citations itself
  would be fixing them in an artifact somebody already reviewed.
- **No git hooks or CI IN A GATE WORKSPACE.** A workspace lives locally and dies
  with its slice; there is nothing to publish. This repository itself is pushed
  to Gitea and does ship two Claude Code PreToolUse hooks — a different thing with
  the same word in it.
