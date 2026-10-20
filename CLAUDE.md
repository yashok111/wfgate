# wfgate — agent context

**Where things are.** This file is the WHY: the architecture, why each command
is the way it is, the hard rules and the bars — what an agent has to ACT on.
Four files hold what it only has to LOOK UP, and each says when to open it:
`README.md` is the command reference — every flag, every report line, worked
examples; go there first for "how do I run a round". `WAVES.md` is the
maintenance record, waves 2 to 41 — what each change answered and the command
that measured it; open it when a decision looks arbitrary or before
contradicting a banked number. `GATES.md` is the gate census — every gate,
its workspace and its numbers. `IDEAS.md` holds the queue and the do-not-build
list with the measurement that closed each entry.
`skills/workflow-gate/references/` is the OPERATING MANUAL, which is judgement
and is what gate agents are briefed from; this repository is the mechanics.

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
`~/.claude/workflow-*.md` symlinks.
**An installed plugin is a RAW COPY of the source tree whose only exclusion is
`.git`** — `.gitignore` is not consulted, measured 2026-08-30 by comparing the
cache against the clone: 33 top-level entries against 34. Because this repo's
ROOT is the plugin, the three freshly built binaries rode into every cached
version: 36 MB of each 39 MB directory, and `claude plugin update` keeps every
superseded version forever, so 37 releases had accumulated **1.2 GB** for a
skill and two hooks that weigh 300 KB. `install.sh` now runs `make clean`
before the plugin step, drops `evals/results/` — `claude plugin eval` writes a
timestamped report and scored JSON there per run, which `.gitignore` keeps out
of the repository and therefore out of everywhere BUT the plugin copy, and
which the 2 MB tripwire is too coarse to see — and prunes every cache version
but the one it just installed. Neither is a Makefile change: `make install` is also run alone by
somebody who wants `./wf` to stay. **And no session on this machine reads that
copy** (wave 29, 2026-10-04): a plugin from a FOLDER marketplace — which is what
`install.sh` registers — is loaded in place from the clone on 2.1.286 to 2.1.289,
the cache still written and read by no session. The pruning stays because the copy is
still written; the copy is no longer what the manual's freshness hangs on.
Before consolidation, 866 lines of
gate-guarding tooling and both specs had no version control at all, and the
P2e false green lived in exactly such a file. The mechanics/judgement split
stands: this repo is the mechanics, the gate-checks reference is the
judgement.

## Architecture

Nineteen units, each one decision — nine Go packages under `internal/`, plus
the SIX emitted templates, the dispatcher, the hooks, the skill and the agent.
Re-count them against `ls internal/` before quoting the number.

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
- `templates/lens.md` — the reviewer's brief, embedded and emitted by
  `wf new-lens`. It carries what is true of EVERY round — the reading list by
  path with the pinned file's section index BY LINE, the reading rule (`Read`
  and `Grep`, not a shell), the three asks, the four level words from
  `ws.Levels`, the output
  contract — and POINTS at the workspace's pinned `gate-checks.md` §0 and §0c
  rather than restating them, because a rule restated in a prompt goes stale
  where `wf check dup` cannot reach it: a prompt is not an artifact [M-37]. The
  aim is left as a marker line and `wf round lens` refuses to bank a prompt
  still carrying it.
- `agents/gate-sweep.md` — the plugin's mutation-sweep reviewer, and the ONLY
  agent definition this repo ships. It pins the tool list, `omitClaudeMd` (every
  rule a sweep acts on is in the file, so the ~50 KB of WHY here is not loaded
  into it), a one-hour prompt cache and `isolation: worktree` — honoured from
  a plugin agent's frontmatter, measured 2026-10-03 with a probe spawned
  without the parameter — and no model: the
  case for pinning a MODEL was refused on measurement (the sonnet rule shows
  88 opus / 0 sonnet before it and zero opus in the five gates after the one it
  landed in, so a pin buys nothing). Inside that worktree a plain `git` is
  refused once a PreToolUse hook rewrites it into a wrapper, so the agent calls
  git by absolute path.
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
- `templates/review.mjs` — the same, for a READ-ONLY run (`wf init -type
  review`): finders per lens and unit, a verifier per finding, a merge, a report
  whose population line comes first; every reading cell of it, and of the three
  read-only templates below, is narrowed at spawn with `disallowedTools` (wave
  40). A second template rather than a mode of
  the first because the two share almost no lines: the two read-only gates on
  record each began by deleting the build skeleton's producers, bars, fix round
  and acceptance step by hand. The run type lives in `gate.json` (`ws.Types`, a
  closed list) and is read by `wf new-script`, `wfdry` and `wfcheck
  --owns-diff`, each announcing where it read it — wave 32 in `WAVES.md`.
- `templates/hunt.mjs` — a review over a SUBSYSTEM at one commit, whose
  blockers and majors are REPRODUCED by a worktree-isolated agent writing a
  failing test, the rest re-judged by reading; "not reproduced" is listed
  apart from "refuted". `--owns-diff` lets a hunt leave its worktrees under
  `.claude/worktrees/` and lists them — wave 33. It runs in ROUNDS: productive
  cells and a completeness critic's areas go again, bounded by `MAX_ROUNDS`, an
  agent ceiling and the token budget, and the report names the stop — wave 35.
- `templates/audit.mjs` — a QUESTION split into parts; finders return
  claims of both polarities citing `path:line#anchor`; a checker reads each
  citation; one writer composes the answer from verified claims only. The
  citation form is the one `wf check cite -require-anchor` reads, which is the
  audit's mechanical acceptance once the report is saved — wave 34.
- `templates/security.mjs` — a subsystem, or with `BASE` a diff, searched for
  vulnerabilities under the threat model the decision document states: finders
  per CWE class and per chunk of scanner hits (`args.scan`, folded from SARIF
  by `wf scan`, `cmd/wf/scan.go`, every hit adjudicated by enum), a verifier
  that defaults to REFUTING with a three-state verdict, a PoC worktree for
  confirmed blockers and majors, VARIANT rounds seeded by what was confirmed.
  `wfdry --mode inconclusive` walks the third verdict — wave 37. Seven default
  classes, a cross-unit flow cell, trust by writer and `UNTRUSTED` in every
  prompt — wave 38.
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
  (ELEVEN of them: authoring, model-routing, run-types, interview,
  pre-flight-gate, gate-harness, round-discipline, section-gate,
  cost-discipline, gate-checks and measured — re-count them against
  `ls skills/workflow-gate/references/`, which is also what `make check-docs`
  stages). The split
  preserved the gated text verbatim — cutting at headings is packaging, not
  rewriting, so no re-gate was owed. `embed.go` at
  the module root embeds the references into the binary; `wf docs guide`
  CONCATENATES them at runtime rather than storing a monolith — a second copy
  of a spec is this project's most-repeated defect class.
  The references are cut by ACTIVITY, not by topic: `interview.md` is step -2,
  before a workspace exists; `pre-flight-gate.md` is the gate itself (steps 0,
  1, 2b, 3 and the three vectors); `gate-harness.md` is the operator's `wf`
  mechanics (steps -1, -1b, 2, 4, the checks, the fix protocol and the cost
  meter); `round-discipline.md` is the barrier, delta rounds, the descent and
  the stop rule. A reviewer loads pre-flight + checks + rounds, an operator
  the harness — re-derive what each costs with `wc -l`, never from a number
  written here. `wf docs` serves `interview`, `harness` and `rounds` apiece.
  `measured.md` is the manual's PROVENANCE: never auto-loaded and deliberately
  out of `wf docs guide`, because an agent acts on the rule ("a fix is the
  largest single source of blockers"), not on which gate measured it. Rules
  cite it by `[M-nn]`; `make check-docs` fails on a citation with no entry, an
  entry nothing cites, and an entry citing itself.
  Each topic has ONE home — `wfdry`'s modes and floors in `pre-flight-gate.md`
  step 2b, the schema width band and fixture vocabularies in `authoring.md`,
  `dup`/`[RN]`/`stale` in `gate-harness.md`, the fix protocol in
  `gate-checks.md` §I, the delta round's three asks and the severity scale in
  `gate-checks.md` §0b and §0 — EXCEPT the marked sites: collapsing an `R2` or
  `R3` statement into a pointer would drop that id to one site, and
  `wf check rules` would then compare nothing while `make check-docs`'s marker
  floor read the narrowing as agreement.
  How the split was made, verified and measured — the token census, the sizes
  before and after, the `gate-checks.md` harvest and the review round it
  earned — is `WAVES.md`, "The manual restructure of 2026-08-30".

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

**An anchor names an identifier or a PART of one, never a fragment of a
word.** A substring match passed `#e` on any line with a word in it. A
whole-identifier match was the obvious fix and was MEASURED before it shipped:
over 235 frozen rounds it failed 25 of 1397 passing anchors, 22 of them
correct citations of a part (`#RETENTION` on `MOCKER_CHECKPOINT_RETENTION`) —
23 false errors among 82, in 10 of 28 gates. Splitting at `_`, camelCase
humps, acronym ends and letter-digit boundaries failed 2 genuine fragments and
passed a citation the gate had filed as wrong — `#useGetOperation` on
`useGetOperationOverride` — so a part that BEGINS an identifier and stops at a
hump is refused as well: 6 of 1397, each with the whole identifier as its
patch.

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

**Waves 2 to 41 are in `WAVES.md`**, not here. They are the maintenance record —
what each change answered, and the command that measured it — and they were 80%
of this file, paid in context on every session in this repository. The first
split (2026-08-29) moved waves 2 to 18 byte-identical and kept the newest here;
the second (2026-09-02) moved the rest the same way, because this file had
grown back to 174 KB. Nothing was rewritten, and every rule from them an agent
still has to ACT on is in "Hard rules" below. Read `WAVES.md` when a decision
looks arbitrary, when you are about to contradict a banked number, or before
proposing something that may already be on the do-not-build list. Its index:

| Wave | Gate / source | What it answered |
|---|---|---|
| 2 | P2d live run (`docs/feedbacks/FEEDBACK-p2d.md`) | The first ten rounds run entirely through `wf`: extensionless citations, `check rules` and `[RN]`, `dup -since`, the anchor ratchet, `-kind full\|delta`, `wf stats`, `wf add` adopting a draft, bookkeeping dirt in `verify` |
| 3 | The residue P2d named | `apply.ParseText` (the plain-text batch format), `delta -save`, `[fix-induced]` in stats |
| 4 | P2e, the first SCRIPT gate (`docs/feedbacks/FEEDBACK-p2e.md`) | Hard-wrap unwrapping in `dup`, `NUMERIC` conflicts, `-kind tool`, dry-run BEFORE freeze, the batch-size cost meter |
| 5 | The five-reviewer audit of this repo | The tool committing the class it exists to catch: freeze writing before refusing, vacuous open-blocker checks, one pipe carrying a BLOCKER past a freeze, `strings.Count(body, "")`, `dup` losing recall as a rule spread, the text scanner replaced by a parsed program |
| 6 | P2f, both gates in ONE workspace (`docs/feedbacks/FEEDBACK-p2f.md`) | `wfcheck`'s unclearable backstop ERROR, `wf stats` reading ZERO apply batches over 27, `wfdry`'s fixture deciding which branch ran, the sentence-terminator defect in `check rules` |
| 7 | mocker slice A, THREE gates (`docs/feedbacks/FEEDBACK-a-mcp.md`) | Four ways the OPERATOR drifted out of the tool in silence; freeze resolving citations; the per-node depth budget; `wfcheck -root`; the pre-flight sequence as a smoke test, not a gate |
| 8 | mocker A3, queue filed while open | `wf check stale -since`, `wf check closure`, the cost meter calibrated on the wrong regime, the corpus SIZE in `freeze.json` |
| 9 | The A3 loop — measured and NOT built | Four proposals rejected on measurement (the do-not-build list); acceptance sections state OBSERVATIONS; `wf round lens`; the stop rule unified at five sites; a scope cut is reviewed like any other edit |
| 10 | mocker A3 closed, the script RAN (`docs/feedbacks/FEEDBACK-a3.md`) | `[RN]` markers inside a `.mjs` (already shipped, never said); FIRST REVIEW leading the stats line; `wf apply -rule RN`; `-kind run` |
| 11 | The two things every agent invented | The severity scale closed to BLOCKER/MAJOR/MINOR/NOTE across four drifted vocabularies; `-asks` and the three questions a delta round may never drop |
| 12 | P3a, first workspace gated TWICE (`docs/feedbacks/FEEDBACK-p3a.md`) | `--mode dead-reviews` matching a SUBSTRING; `--prompt-diff`; the aged `gate-checks.md` pin; JS escapes reaching the comparison undecoded; `-rule` as free text |
| 13 | P3b, the gate that STOPPED (`docs/feedbacks/FEEDBACK-p3b.md`) | Blockers split by ORIGIN, not only findings; the strongest queue entry REFUSED on measurement; the agent-count ratio; `wf freeze -refreeze`; the frozen harness |
| 14 | The five-vector brainstorm | Nothing here is slow anywhere that matters (a closed hypothesis); `make check-docs`; the `dead-reviews` mode that discriminated nothing; phase-set collapse; the moved freeze tag derived from GIT |
| 15 | P3c, the launch-killer (`docs/feedbacks/FEEDBACK-p3c.md`) | The schema-width band measured in the RUNNER; factory-composed `YOU OWN` blocks; `-rule` as a refusal; two closure reports saying NOT A PASS; `check cite -at frozen` |
| 16 | P3d, a probe instead of an argument (`docs/feedbacks/FEEDBACK-p3d.md`) | The mis-split batch that verified itself byte-for-byte; `$defs`/`$ref` resolved in `wfdry`; the refusal floor at 4,233 probed for 82 ms; the ledger refusal naming a CHARACTER; `x-pass` on arrays |
| 17 | P3e, and the two findings that were the tool already saying it (`docs/feedbacks/FEEDBACK-p3e.md`) | A regression report against a shipped fix with no regression; `wfdry --fixture`; the `required` census and `wf check schema -since`; `wf new-script` writing into `artifacts/`; the manual stopped carrying perishable NUMBERS |
| 18 | P3g, and the two proposals the tool had already answered (`docs/feedbacks/FEEDBACK-p3g.md`) | `wf apply -whole`, the adoption that prices a round nothing watched; the schema band printed in the header; the `dead-reviews` phase WARN; a section boundary is a COMPILATION boundary; reviewer width is bounded by tokens; `NAV` in the emitted skeleton |
| 19 | P3h, the first write-up that is the `wf close` scaffold (`close/feedback.md` in `~/projects/gates/mocker-p3h-basepath`) | A citation is a SPAN mapped into each commit's coordinates; the inflection whitelist in `stale`; the `path#anchor:12` branch that never worked; four manual rules about reading a round; `NAV` in the emitted script; reviewer width bounded by tokens |
| 20 | P3f and nova, the first gate outside `mocker` (`docs/feedbacks/FEEDBACK-p3f.md`, `FEEDBACK-nova-budget-panel-batch.md`) | Two proposals REFUSED on measurement; `wf init` refusing a workspace under `-target`; bracketed header fields; `wf check closure` naming its populations and the adoption list; `wf round close`; the placeholder check re-shaped; no WRITE phase after a MEASURE phase; the gate bounds a RUN, not the FEATURE |
| 21 | P4a, three RUN rounds (`docs/feedbacks/FEEDBACK-p4a.md`) | `wfdry` prescribing the keyword the launch refuses; a pinned number nothing ran; resume as an instrument |
| 22 | P4a reproduced | `$comment` for `x-pass`, unknown schema keywords refused at dry-run; run rounds out of the cost meter; `wf round close -prose`; `wf apply` on `parseMixed`; the brief that existed nowhere; `launchKeywords` narrowed by a second probe |
| 23 | The reviewer's brief is a scaffold | `wf new-lens`; an open question is a BLOCKER; `agents/gate-sweep.md` and the model pin refused; the `-o` refusal that compared an absolute path |
| 24 | A4, the report nobody read (`docs/feedbacks/FEEDBACK-a4.md`) | Closure halves ORed per SPAN; bookkeeping commits refused as closures via `CommitPaths`; ownership normalised against `REPO` in the skeleton; the routing TRANSITION row; a pinned number re-derived by the round that READS it |
| 25 | Where a round's MINUTES go | The brief's reading rule and section index by line; `wf apply -closes`; `wf check` with one summary line and `stale` from the newest freeze; `wf round ingest`; `wf init` FIRST; the external vector that died |
| 26 | A bug hunt by five vectors | 47 findings, 8 BLOCKER: hooks granting permission, `apply` writing outside `artifacts/`, a `#1` row closing the wrong one, factory wrappers invisible to `wfcheck`, a heading's passing mention closing a section; the 68-mutation sweep and the symlink, merge and fence findings of its review round |
| 27 | A18, the gate that closed with three blockers standing (`docs/feedbacks/FEEDBACK-a18.md`) | `wf close -early` banking the owner's call; the SECTION run in `wf stats` over the anchors a blocker cites; `round record` numbering empty `#` cells; `target:<sha>` as a prose closure; three manual rules (a guard counting its own statement, shapes never coordinates, the build as a `-kind run` round) |
| 28 | Two months of Claude Code release notes (2.1.221 to 2.1.288), read against the plugin | The manual's "there is no knob" for workflow concurrency, three weeks stale; usage-limit pauses that now resume; the script API moved out of the Workflow tool into a skill; `make check-plugin`; `omitClaudeMd` and a 1h cache on the sweep agent; the repo's `verify` skill |
| 29 | Claude Code 2.1.289, read against the plugin | A folder-marketplace plugin is read IN PLACE, not from the cache; `wf docs` compared a copy no session loaded; install.sh no longer reinstalls over in-place drift; `plugin validate` on a folder had skipped the plugin, `make check-plugin` never affected |
| 30 | Claude Code 2.1.290 to 2.1.294, read against the plugin | The `Agent` tool's `effort` made "gate reviewers carry no effort knob" stale; reviewers pinned at `xhigh`, the level they had inherited; the "Large workflow" warning knobs; Haiku 5.5 left to a measurement |
| 31 | The owner's question after wave 30: which roles go to haiku | The haiku test in `model-routing.md` (output checked downstream, a named slice, a local answer); code producers and gate reviewers stay sonnet; vector 2 files a haiku role that fails it |
| 32 | The owner's ask: the plugin assumed every run writes code | Run types — `wf init -type review`; the review skeleton; `wfdry` modes, fixture polarity and `dead-reviews` per type; `--owns-diff` as a bar for a read-only run; `run-types.md` |
| 33 | The second run type | `hunt`: a subsystem at one commit, serious findings reproduced in worktrees, not-reproduced kept apart from refuted, the worktree carve-out in `--owns-diff` |
| 34 | The third run type | `audit`: a question in parts, claims of both polarities citing `path:line#anchor`, a citation checker, one writer from verified claims, `answered` apart from `complete`, `dead-writer`, `check cite` as the mechanical acceptance |
| 35 | The multi-round hunt queued after wave 33 | Rounds until dry: productive cells re-run with their own findings, a completeness critic's extra cells, four stops each named (`stoppedBy`), an agent ceiling under the Workflow cap; `wfdry` refuses a mode over that cap, and its review round added `dead-cell` (one finder cell dead in every round) |
| 36 | `check cite` precision, queued by wave 34's adversarial vector | An anchor matches a whole identifier or a PART of one, never a word fragment (measured over 235 frozen rounds first); `-require-anchor` misses counted in the header; `path:3-#x` a malformed range |
| 37 | The owner's fifth type, distilled from 21 security skills on skills.sh | `security`: the threat model gated in the document, scanners run before the launch and folded by `wf scan` (an error notification FAILS a scanner — M-49), every hit adjudicated, a refuting verifier with a third verdict and `wfdry --mode inconclusive`, PoC worktrees, variant rounds, `betterleaks` over the saved report |
| 38 | Five more security skills, read on the owner's ask | Seven default classes (destructive paths and LLM features among them), a cross-unit flow cell, trust follows who WROTE a value, `UNTRUSTED` first in every prompt with relayed text fenced and planted instructions listed apart, infrastructure controls as `cannot-verify`, STRIDE per boundary and build threats as acceptance observations |
| 39 | Claude Code 2.1.295 to 2.1.304, read against the plugin | `CLAUDE_CODE_WORKFLOW_SUBAGENT_MODEL` overriding every `agent()` model, and `wfcheck` reporting it; the harness frames every workflow agent's task as no-authority and relays the LAUNCH turn above it; `onFailure: "block"` on the Workflow hook; `disallowedTools` queued |
| 40 | The option wave 39 queued, probed | Every reading cell of the four read-only skeletons narrowed at spawn with `disallowedTools`, the reproducers exempt; `wfdry` refuses a reader without it and a value the harness would refuse; the tree bar stays the bar |
| 41 | The owner's question: can our functions be fuzzed | Eleven `Fuzz*` targets over the five text readers with INVARIANTS, not "no panic"; `make fuzz`, not a bar; one real find (`SetClosedBy` losing a cell on a row with no closing pipe), five invariants the fuzzer corrected; the crasher banked as a replayed test |

**`wf close` and `wf prune`, and why the record is not the repository.** Three
gates were written up by hand into `docs/feedbacks/FEEDBACK-p2*.md`, each inventing its own
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

**`close -early` is a refusal first and a field second.** The stop rule ends a
gate on a reviewer round with no BLOCKER; A18 ended on one with three, all
about the artifact's own guards, and the owner's call to build was right — the
build found the two class members the rounds would not have. But the close
banked it with nothing saying so, and `wf stats` printed the summary of a gate
that met the rule: the tag, the ledger and the close commit are identical for
the two endings, so it is the one fact about a gate's end git cannot derive.
A bare close over a blocker round is refused with both roads named; the reason
given is banked in `gate.json` and printed in the header of `wf stats`, the
close and the record; `-early` over a round with NO blocker is refused too,
because an absent measurement must not print in the register of a measured
one; and a banked call is dropped when the gate is re-opened past it. The
last REVIEWER round is what is read — a `tool` round after it dispatched
nobody — which is the same reading every other consumer of the stop rule uses.

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
- **`CLAUDE.md`, `WAVES.md` and `GATES.md` carry NO `[RN]` marker.** A rule is STATED in
  `skills/workflow-gate/references/` and in `README.md`; everything here talks
  ABOUT rules, which by the check's own note is a pointer and disagrees with
  every statement of the id it wears. Measured 2026-08-29: 15 markers across the
  two files, every one of them narrative, producing 3 conflicts against a manual
  that agrees with itself. Written `R2`, never in the bracket form the check scans for. Run
  `wf check rules` over the manual AND these three files together when in doubt —
  it must report the manual's own 9 marked sites and nothing else.
- **A change under `skills/`, `hooks/`, `agents/` or `scripts/hooks/` bumps
  `version` in `.claude-plugin/plugin.json`.** `scripts/hooks/` is listed
  because `hooks/hooks.json` executes those files: they ship in the plugin as
  surely as the manual does. `claude plugin update` is VERSION-keyed —
  measured: it copies nothing when the declared version is the one already
  installed, so a spec fix committed without a bump never reaches a session
  that loads the CACHED copy. On this machine no session does: a folder
  marketplace is read IN PLACE (wave 29 — measured on 2.1.286 to 2.1.289, the
  skill, the agent and hooks.json all load from the clone), so the bump no
  longer gates delivery here and is kept for every install that is not a folder
  marketplace and for Claude Code builds older than the ones probed. The
  converse is the live risk: an UNCOMMITTED edit under these four directories
  is what the next session loads, and a script under `scripts/hooks/` runs from
  the clone on every call — in sessions already running too, a half-written
  hook included. Edit a hook in a worktree, or finish it before the next tool
  call anywhere fires it. `wf docs` compares the manual a session actually
  loads; read in place, a difference means the binary and the clone were built
  from different trees, and the patch is `make install` there, not a reinstall.
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
  `WAVES.md`, every wave since the second — where they
  are a record rather than something an agent acts on. The same test was applied
  to THIS file on 2026-08-29 and it failed it: 80% of it was that record, paid
  in context on every session, so it moved — and again on 2026-09-02, when the
  waves kept here "because they are the newest" had grown it back to 174 KB.
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
  trap being met six separate times (waves 14, 16, 26, 27). Two more read as SURVIVED
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
make check-plugin # validate both manifests, RUN each hooks.json command, read agent keys (needs claude, jq, yq)
make fuzz        # NOT a bar: the Fuzz* targets for FUZZTIME each; a find lands in testdata/fuzz/ and `make test` replays it
make fmt         # gofmt -w; `make lint` only REPORTS the diff
make install     # ~/.local/bin/{wf,wfcheck,wfdry} — owed after every code change
make clean       # remove the three built binaries
```

`make test` counting `ok` lines is not a green bar — one package can fail while
the count looks right. Read the FAIL lines.

**`evals/` is NOT a bar, and must not be run like one.** `claude plugin eval .`
measures whether the `workflow-gate` skill CHANGES an agent's answer — it runs
each case twice, with the plugin and without, and reports the delta — and every
run is a full `claude` child process on the operator's own credential. Four
cases at the default three runs is twenty-four of them, so it is an instrument
an operator reaches for deliberately, not a step in a commit. `evals/README.md`
is the reference: what each case measures, why the three `skill-loaded` graders
are `arm: with-only` (a plugin scored for loading itself is the "absent
measurement in the register of a measured one" class), and why a NEGATIVE delta
on `no-false-fire` is that case's finding rather than its failure.

`make check-docs` stages README, SKILL.md and the eleven references into a scratch
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

Five of those packages also carry `Fuzz*` targets (wave 41) — the text readers
nobody else writes for: `jstext` (the blanked copy keeps its shape, prose lies
inside what the code reader blanked), `ws` (the two setters give the row back
with one cell changed), `apply` (a block round-trips or is refused), `check`
(an anchor is in its text, the normaliser is a fixpoint) and `cli`. Each
asserts an INVARIANT, never only "no panic": the fuzzer's first find was a
real one and the next five were invariants written too strong, and only a
stated property tells the two apart. `make fuzz` runs them for a bounded time
and is not a bar; what it finds is written under `testdata/fuzz/` and replayed
by `make test` from then on.

Coverage is NOT complete and the claim that it is has been wrong once: a
mutation sweep after the first fix round found 33 guards deletable with the
bars green. Before claiming a guard is proven, delete it and run the bars.

## Style

As in `mocker`: comments explain the WHY and carry the measured reason ("161
groups of 176", "148 citations, none out of range"), never a retelling of the
code. Everything is English — code, comments, and documents alike (the workflow
is Latin-script by policy; Cyrillic may appear only as quoted DATA, e.g. test
fixtures for the non-ASCII anchor detector).

## State — read `GATES.md` before trusting a number

The gate census — every gate that has run through `wf`, its workspace, its
numbers and the command that re-derives them — is `GATES.md`, moved out of
this file on 2026-09-02 (27 KB, read only when a number is about to be quoted
or contradicted). What an agent needs without opening it:

- **Seventeen gates have run entirely through `wf`**, the newest A18
  (`~/projects/gates/mocker-a18-functions`, `docs/feedbacks/FEEDBACK-a18.md`),
  and **THIRTY-ONE gate workspaces are on disk** (re-counted 2026-10-12 —
  twenty-three under `~/projects/gates/`, the rest under `~/gates/`), of which
  `mocker-a-mcp` is pruned to its record and several, the photowatch read-only
  gates and `nova-epm-mocker-import` among them, have no bullet in `GATES.md`
  yet. Re-count with
  `ls -d ~/projects/gates/*/ ~/gates/*/`; the count has been inherited wrong
  four times, and was inherited wrong again between A4 and A18 — the line said
  nineteen while the directory held twenty-two.
- **A18 is the first gate that did NOT close on the stop rule**, and it closed
  correctly: its last round's three blockers were all about its own GUARDS, not
  about the subject, and the target's owner chose to build. `wf close` banked it
  with no field saying so; `close -early` (in "Why each command is the way it
  is") is the answer, banked into A18's workspace retroactively. Read that
  feedback before quoting the stop rule as a thing every closed gate met.
- **Run `wf stats -time` inside a workspace before reasoning about cost.** The
  commit timestamps are what price a round and nothing else records them; a
  hand pooling of the same spans has moved five per-gate medians and been
  wrong. The pooled figures in `GATES.md` name their population and reading.
- Every tooling wave earns a review round — three disjoint vectors — and the
  record of what each found is `WAVES.md`; the argument for the rule is the
  seventh and the 2026-08-23 waves there.
- The history of decisions and measurements is in the git log; this file keeps
  only what the commits cannot reconstruct. The idea queue and the
  do-not-build list are `IDEAS.md`.

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
