# wfgate — agent context

**Where things are.** This file is the WHY: the architecture, the hard rules,
the bars, and the four newest waves of the maintenance record. `README.md` is the
command reference — every flag, every report line, worked examples; go there
first for "how do I run a round". `skills/workflow-gate/references/` is the
OPERATING MANUAL, which is judgement and is what gate agents are briefed from;
this repository is the mechanics. `WAVES.md` is the maintenance record for
waves 2 to 18, and `IDEAS.md` holds the queue and the do-not-build list with the
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
`~/.claude/workflow-*.md` symlinks.
**An installed plugin is a RAW COPY of the source tree whose only exclusion is
`.git`** — `.gitignore` is not consulted, measured 2026-08-30 by comparing the
cache against the clone: 33 top-level entries against 34. Because this repo's
ROOT is the plugin, the three freshly built binaries rode into every cached
version: 36 MB of each 39 MB directory, and `claude plugin update` keeps every
superseded version forever, so 37 releases had accumulated **1.2 GB** for a
skill and two hooks that weigh 300 KB. `install.sh` now runs `make clean`
before the plugin step and prunes every cache version but the one it just
installed. Neither is a Makefile change: `make install` is also run alone by
somebody who wants `./wf` to stay. Before consolidation, 866 lines of
gate-guarding tooling and both specs had no version control at all, and the
P2e false green lived in exactly such a file. The mechanics/judgement split
stands: this repo is the mechanics, the gate-checks reference is the
judgement.

## Architecture

Fifteen units, each one decision — nine Go packages under `internal/`, plus
the TWO emitted templates, the dispatcher, the hooks, the skill and the agent.
Re-count them
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
- `templates/lens.md` — the reviewer's brief, embedded and emitted by
  `wf new-lens`. It carries what is true of EVERY round — the reading list by
  path, the three asks, the four level words from `ws.Levels`, the output
  contract — and POINTS at the workspace's pinned `gate-checks.md` §0 and §0c
  rather than restating them, because a rule restated in a prompt goes stale
  where `wf check dup` cannot reach it: a prompt is not an artifact [M-37]. The
  aim is left as a marker line and `wf round lens` refuses to bank a prompt
  still carrying it.
- `agents/gate-sweep.md` — the plugin's mutation-sweep reviewer, and the ONLY
  agent definition this repo ships. It pins the tool list and nothing else: the
  case for pinning a MODEL was refused on measurement (the sonnet rule shows
  88 opus / 0 sonnet before it and zero opus in the five gates after the one it
  landed in, so a pin buys nothing), and `isolation` is a parameter of the Agent
  CALL rather than frontmatter, so the definition SAYS to pass it.
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
  its rounds split. The entries are cited from the rules by `[M-nn]`, and
  `make check-docs` prints how many there are — it fails on a citation with no
  entry, on an entry nothing cites, and on an entry citing itself.
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

  *And the harvest the wave named as NOT DONE was then done* (2026-08-30): the
  sweep across `gate-checks.md`'s 147 checkbox entries, which the wave had
  deferred as "a much larger edit at a much higher risk of dropping a rule". It
  found the risk was real but not where it was expected. Two of the heaviest
  entries were not provenance at all but DUPLICATION missed by the first pass —
  a 27-line restatement of the round loop that `gate-harness.md` already carries
  as a seven-step runbook, and a 20-line restatement of the citation and anchor
  mechanics — and both collapsed to the reviewer's half plus a pointer. The rest
  of the file resisted: its entries are rule-dense because the checkbox form
  forced brevity years ago, and in `gate-checks` the anecdote is frequently the
  RECOGNITION SHAPE rather than the justification ("porcelain prints a status
  code and a path, never bytes"), which is a rule and stays. Eight more
  provenance entries came out, M-15 to M-22. `gate-checks.md` went 98 246 to
  **87 165** bytes across the whole restructure, **-11.3%**, and the loaded
  manual 248 166 to 233 684, **-5.8%** — the 25-35% sold at the start is still
  not met and this is the second time it has been named as not met. The number
  that did move is per-ACTIVITY and it moved because of the SPLIT rather than the
  compression: a reviewer's set went 208 230 bytes to **137 466**, **-34.0%**,
  because ~520 lines of operator `wf` mechanics stopped being loaded to review a
  script.
  One edit SWALLOWED a whole checkbox — a replacement whose new text omitted the
  `- [ ]` header, merging one entry into its predecessor — caught by counting the
  entries before and after. So `make check-docs` grew a fourth arm, a floor on that count (150 today, and it
  moves with every checkbox that lands — this same sentence has now been stale
  three waves running, at 147, at 148 and at 150), and it is proven by mutation like the other three: **an edit
  that swallows an entry reads exactly like an edit that shortened one**, and the
  reviewer's list IS the population, which is §F's own pinned-population rule
  applied to the file that states it. A token census over the whole reference set
  then confirmed the pass: of 2 992 distinct tokens in the old `gate-checks.md`,
  33 are absent from the new manual and every one is a tense, a case or a
  trailing-period variant of a word that survives, or an identifier re-banked in
  `measured.md`.

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

**Waves 2 to 18 are in `WAVES.md`**, not here. They are the maintenance record —
what each change answered, and the command that measured it — and they were 80%
of this file, paid in context on every session in this repository. The split
(2026-08-29) moved the text byte-identical; nothing was rewritten, and every
rule from them an agent still has to ACT on was lifted into "Hard rules" below.
Read `WAVES.md` when a decision looks arbitrary, when you are about to
contradict a banked number, or before proposing something that may already be
on the do-not-build list. Its index:

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

**The nineteenth wave — P3h, and the miss whose DIAGNOSIS was wrong in the
direction that made the fix cheaper** (`close/feedback.md` in
`~/projects/gates/mocker-p3h-basepath` — the first gate whose write-up is the
`wf close` scaffold rather than a hand-authored `docs/feedbacks/FEEDBACK-*.md` in this
repository; 10 rounds, 73 findings, 17 blockers, 129 edits over 822 changed
lines, median round 30.5 min, two artifacts — a decision document gated for
three rounds before the script was added at round 4, then both together for seven
more, closing on two consecutive rounds with no blocker). Every claim below was
reproduced before a line was written, and three of the five did not survive
that:

- *`wf check closure` reported four line misses and the two-batch explanation is
  not why* — the feedback says a second apply batch in the same round shifts the
  numbers the finding text names, which is a real mechanism and was not this
  one: the first miss's closing commit has the round's own FREEZE as its parent,
  so nothing had shifted. The cause is one line in `PathsNamed` — `lineAfter`
  read the `:454` of `p3h.mjs:454-459` and dropped the rest — so the check asked
  whether the fix touched the FIRST line of a region the finding drew. All four
  of that gate's line misses are that shape, and the sharpest is the one that
  named it: hunks covering 450-453 and 455-457, with 454 the single unchanged
  line inside the span, because `--unified=0` splits a region exactly there.
  Every one was closed. The citation is a RANGE now and the question is overlap;
  the row names the SPAN the finding wrote rather than the line the check reduced
  it to, and an INVERTED span keeps its start alone rather than being widened to
  cover both readings — `check cite` reports that shape as an error and repairing
  it here would answer a question about a region nobody wrote. Measured over the
  eleven unpruned workspaces — `grep -ohE '[A-Za-z0-9_./-]+\.(md|mjs):[0-9]+-[0-9]+'`
  over every `rounds/*/findings.md`, which is the reading, and it is written down
  because the first one was not and counted `.go:` citations into the reviewed
  TREE that `PathsNamed` never sees: **96 spans against an artifact-shaped path**,
  62 of them written with the `artifacts/` prefix, every one an ASCII hyphen and
  the widest 52 lines. The report goes from 78 line-miss rows to 62.
- *And the feedback's own explanation, wrong for P3h, is right about the corpus —
  so the citation is MAPPED into each commit's coordinates* — "a later batch in
  the same round shifts the numbers" explains none of P3h's four misses and
  describes the NORM everywhere else: measured over the eleven workspaces, **643
  of the 661 closures that cite a line name a commit whose parent is not the
  round's freeze**, because a round applies several batches with ledger and lens
  commits between them. The finding's numbers are freeze coordinates and the
  commit's old-side ranges are its parent's, so the check was comparing two
  documents — the arithmetic the SECTION half had avoided since it started
  resolving section ids at `sha^`. `check.ShiftSpan` sums the insertions and
  deletions between the two revisions and moves the citation by that much, which
  is `check drift`'s own algorithm one layer up. Two preconditions, and both cost
  a false report in the measurement before they were added: the freeze must
  PRECEDE the commit — `wf freeze -refreeze` moves a tag, and a round republished
  after its own fix makes `tag..sha^` a REVERSE diff that subtracts what the fix
  added, which is five spurious rows — and the cited lines must not themselves
  have been rewritten in between, where the numbers mean nothing and the closure
  is COUNTED and named rather than judged (2 corpus-wide, so it paints nothing).
  Line-miss rows corpus-wide: **78 before the wave, 62 with the span, 28 with the
  map**, and the three that APPEARED are true misses the old check passed by
  numeric coincidence.
- *`wf check stale` retires a word the fix only INFLECTED, and the case-fold half
  of that finding is refuted* — the feedback reports two misses, one blamed on a
  `TWENTY-TWO`/`twenty-two` case mismatch. The normaliser has lower-cased since
  `dup` shipped, so case is folded before anything is compared; that citation was
  invisible because the fix wrote `twenty-four`, which returns the token `twenty`
  to the added set, and `two` is three characters and below the candidate floor.
  The other half is real and is the flagship: the stem is a fixed FIVE-rune
  prefix, which is no stem at all below five characters, so `work` and `works`
  never met — a fix rewriting "consumers work" as "consumer works" retired the
  bare verb and reported four untouched sentences that merely use it. Over that
  gate ALL 8 survivor reports across four rounds were this class, and its round 5
  now reports nothing at all. What ships is a closed WHITELIST of English endings
  (`-s`, `-es`, `-ed`, `-d`, `-er`, `-r`, `-en`, `-n`, `-est`, `-st`, `-ly`,
  `-ing`) plus the doubled final consonant English writes before some of them,
  and the whitelist rather than a length bound is the measured part, by a probe
  that now ships beside the code (`WF_PROBE=1 go test ./internal/check/ -run
  TestCorpusInflectionPairs`, which reads the gate workspaces on this box and
  skips without them — the first reading of these numbers was banked without its
  command and did not reproduce): over the 160 ranges of the eleven workspaces
  the shipped predicate suppresses **171** removed tokens and a bare length bound
  **203**, and the 40 the bound reaches alone are a different word every time
  (`within` under `with`, `someone` under `some`, `formula` under `form`,
  `userid` under `user`, and two Cyrillic fragments quoted as data). Eight run
  the other way — `running`, `putting`, `mapping` and their kin, which the bound
  misses because a doubled consonant plus `ing` is four runes of growth. Corpus-wide the retired vocabulary falls
  556 to 484 and the reported sentences 768 to 663 over 160 ranges, and no range
  got noisier. **Both narrowings are counted in the report now** — the
  common-token one always was, this one and the pre-existing five-rune stem were
  not, and the counter covers BOTH — because a candidate population dropped in silence is one nobody can
  argue with, and that count is the `dup -since` rule applied to the other end of
  the same command.
- *Two of the five proposals were the tool and the manual already saying it,
  which is this project's own most-repeated finding class* — `-asks` prints its
  closed set in `wf round lens -h` AND names it in the refusal, so the asked-for
  hint has shipped since the flag did; and the schema-width probe is not merely
  in the manual but is `authoring.md`'s longest rule, carrying the recipe (one
  throwaway agent, the schema, placeholder prompt), both prices (four agents on
  one run, the fifteenth and last on another) and the instruction not to reason
  from a remembered number. What was true is REACHABILITY and not absence:
  `pre-flight-gate.md` step 2b, which is what an operator actually reads while
  running `wfdry`, never named the band or the probe at all, so the rule sat one
  file away from every reader of it. That is one pointer, and it is the same
  defect the wave before this one committed with nine `[M-nn]` citations pointing
  at a file no workspace holds.
- *Four findings changed the MANUAL, not the tool, and all four are about reading
  a round rather than writing one* — **a fix that says it closed a CLASS is
  re-read for the class, and the SENTENCE claiming the closure is the thing to
  distrust** (`gate-checks.md` §0b: one round closed an ownership hole and wrote
  that the task "now names every test file the arity change reaches"; the next
  found the same hole one level up, and it is the statement rather than the gap
  that stops the next round looking); **a zero from a lens set MISSING the vector
  that has been finding the blockers is not the same zero** (`round-discipline.md`,
  the stop rule: a zero read as the document closing, then six blockers from the
  first round carrying the external lens); **a guard is closed by ENUMERATING its
  inputs, not by fixing the direction the last reviewer looked from** (three
  consecutive rounds at one blocker each on one ledger guard, ended by a fourth
  round asking a lens to CONSTRUCT the shapes its input can take — roughly 1.5
  hours of a 5.1-hour gate, and the question was available in the first of them);
  and **keep one lens whose instruction is deliberately UNDER-specified beside
  the ones the round's findings aim** (two lenses over the same lines, the aimed
  one right and thorough, the blocker from the one that asked what the prompt had
  not). SIX manual edits in all: the compacted-read rule went to
  `gate-harness.md` — **a `clean` from a lens that COMPACTED its context is
  weaker evidence than one from a whole read**, banked in the roster because
  nothing else records which kind of read a report came from — and the stop rule
  in `round-discipline.md` now POINTS at `gate-harness.md` section 4's "an edit
  after the gate closes is still an edit" — the closing round's own fix is the one edit nothing reviews,
  and this gate ran a tenth round only because that section was read for
  `wf close`'s flags and it found a false arithmetic comparison stated identically
  in both artifacts. Six provenance entries, M-23 to M-28.
- *And its own numbers move under the command, in the class the corpus already
  banks* — the gate is 5.3 h re-run today against the 5.1 h its feedback records,
  because the span is last commit minus first and the closing commits land after
  the write-up, which is P3d's 9.1-against-9.2 one gate over. The workspace is 95
  commits; `mocker-p3g-deep-nesting` re-counts at 123 against the 117 banked one
  wave ago. **Re-pooled over the ELEVEN unpruned workspaces: 148 round spans of
  165 under two hours, median 24.0 min, middle half 17.6-31.5** — the reading is
  every `  round` line carrying a `; N min` span, run rounds included, spans of
  two hours and over dropped as breaks, quartiles by the rank `int(n*0.25)+1`,
  and it is `bash` over `wf stats -time`, `grep -oE '; [0-9]+\.[0-9] min'` and
  `sort -n`. The PER-GATE medians are not pooled at all — `wf stats -time` prints
  one per workspace and they are 13.8, 24.3, 19.1, 45.3, 21.5, 29.4, 39.0, 32.3,
  21.4, 19.6, 30.5, unchanged by this wave except for the eleventh, which is new.
  A first reading of this bullet re-derived them by hand instead of reading the
  line the command prints, moved five of them, and contradicted P3h's own entry
  twenty rows above: the command's number is the number.

- *And the wave earned its review round, for the fourteenth time running, and
  the review is again why it is trustworthy* — one external model over the diff,
  then two Opus vectors (Go correctness with a mutation sweep in an ISOLATED
  WORKTREE, and claims/specification), against five green bars. Between them:
  **no blocker, six majors in the record and three in the code, and one guard
  the diff had introduced with no case at all.**
  The claims vector's sharpest finding is a number this file INVENTED: the
  per-gate median list was re-derived by hand under an unstated reading, moved
  five of the eleven values, and contradicted P3h's own entry twenty rows above
  — while `wf stats -time` prints one median per workspace and none of them had
  moved. The command's number is the number, and a hand pooling of the same
  spans is a different measurement wearing its clothes. It also found P3g's
  headline numbers stale IN the commit that noticed P3g's workspace had grown
  (13 rounds and 106 findings against the 14 and 112 the run round left behind —
  the diff read the new state for the pooling and not for the bullet), the
  maintenance record called "waves 2 to 16" at a SECOND site after the seventeenth
  moved into it (the eighteenth wave's own named `count: 2` defect, one wave
  later), a `measured.md` entry count stated as fourteen against 28, P3g still
  called "the newest", and "295 span citations" reproducing under no reading —
  the first count globbed eight workspaces of eleven and matched `.go:` citations
  into the reviewed TREE that `PathsNamed` never sees. The artifact-shaped count
  is 96, and the reading is now written beside it.
  The correctness vector ran 28 mutations, 16 killed and 12 survived, and three
  of the survivors were real: the reverse inflection bound was one rune too
  narrow, so `stop`/`stopped` was suppressed and `stop`/`stopping` reported —
  the same word pair answered two ways; TEN of the twelve whitelist endings were
  exercised by nothing, so the wave's central design decision was defended by a
  test that only required `-in` to be ABSENT; and `RangesCover` became dead
  production code carrying a comment naming the caller this commit removed, with
  a second comment in `main.go` contrasting the new helper against it. Deleted.
  Four more guards got the case they lacked — the end-of-string bounds test in
  `rangeAfter`, which PANICS without it on a finding whose text ends with its
  citation; `uniqueRanges`'s dedup and its two-key order; and the report line
  that makes the new narrowing visible, which is the wave's own argument for
  counting it. Two of the new tests were hardened rather than trusted: the span
  test asserted only the ABSENCE of a miss row, which an unreachable closure also
  produces, and the reverse-inflection test asserted no hit without asserting the
  count.
  The external vector found the two the others did not. A branch in the citation
  parser that has never worked — `path#anchor:12` returned NO line, because `#`
  is itself a boundary character and the loop written to skip the anchor exited
  on its first test, in a function whose comment says it skips it (this project's
  "suspect the COMMENT first" class, shipped years before this wave and found by
  reading the code against its own prose). And the doc claim that each closing
  commit is read in its own coordinates, which was true of the SECTION half and
  false of the LINE half — the finding that became the coordinate map above.
  Two of its reds were REFUSED with the code: an inverted span is not a false
  pass (keeping the start asks a NARROWER question, and `check cite` already
  reports the shape as an error), and `past` under `pasting` is the one wrong
  pair in 171, named where the rule is stated.

- *And two more of its queue items were built from the LIVE run, while the run
  was still in flight, because both measurements are per-agent and neither needs
  the verdict.* **A subagent inherits the launching session's INSTRUCTIONS, not
  only its `effort`** — the half `model-routing.md` already had. Measured on the
  largest agent of P3h's run (a 1.9 MB transcript): **256 `Bash`, 64 `Edit`, 6
  `Read`**, of which 186 are navigation alone — `sed -n`, `rg`, `cat`, `grep` —
  doing what `Read` and `Grep` are for, each a round-trip
  carrying the agent's whole context — 186 of the 256 are navigation alone and 56
  carry a build, test or lint command, which corrects a first reading that called
  251 of them navigation and counted every `go build` as one. Run-wide over the
  finished run: 1799 `Bash` calls across 30 agents, 1206 navigation. Two earlier
  readings of that line (2120 over sixteen agents, 2319 over nineteen) were wrong
  in KIND — they counted lines of multi-line commands rather than calls, which is
  how a snapshot came out larger than the finished run's own total. The
  operator's own box tells its sessions to navigate through the shell and the
  script cannot see that instruction, so the override has to be IN the script:
  `templates/script.mjs` now carries one shared `NAV` const interpolated into
  all five prompts that read or write the tree, and the emitted skeleton stays
  `wfcheck`-clean and `wfdry` 7/7. The ratio AFTER the change is not measured
  and the rule says so. And **reviewer width is bounded by tokens, never by
  wall-clock**: section boundaries force serialism and cannot be argued away,
  but the reviewers inside a gate are read-only, conflict with nothing and run
  concurrently, so a section with more than one failure surface carries a vector
  per surface. That one turned out to be an EDIT to a stated rule rather than
  the note the queue proposed — `section-gate.md` said `The gate = 2 reviewer
  subagents in parallel` and `(pick 2)` three times — which is why the same
  commit reconciled its `(dual review)` title, the two `model-routing.md` sites
  that narrowed the reviewer exemption to "TWO reviewers", `wf docs`'s own help
  line and the `runGate` comment in the skeleton. The queue's THIRD item, an
  acceptance fan-out over `isolation: 'worktree'`, was deliberately NOT built:
  P3h's accept had not run, so its serial tail is the schema's shape rather than
  an observed number, and nothing has measured whether the target's suite is
  worktree-safe.

**The twentieth wave — P3f and nova, the first gate outside `mocker`, and the
two proposals that were REFUSED on measurement** (`docs/feedbacks/FEEDBACK-p3f.md`
and `docs/feedbacks/FEEDBACK-nova-budget-panel-batch.md`). P3f is 17 rounds,
128 findings, 58 blockers, 212 edits
over 1962 changed lines, 9.1 h, median round 28.8 min, authoring 4.6 min, over
TWO artifacts, a decision document for eight rounds at 11 -> 4 -> 2 -> 5 -> 2 ->
1 -> 1 -> 0 and the orchestration script for nine more at 5 -> 5 -> 7 -> 3 -> 4
-> 2 -> 5 -> 1 -> 0, both halves closed on the stop rule — and it is the gate
whose SECOND artifact cost more than its first: 67 findings and 32 blockers
against 61 and 26, more rounds and a bumpier descent, on an artifact the first
gate's own closure implicitly said was ready to be written. Every one of its
numbers reproduces under `wf stats` today, including the 26 of 58 fix-induced
blockers and the 5.3-per-100 prediction basis against a gate actual of 6.5; its
workspace also carries `post-gate — 1 apply batch(es) after gate-r17, 8 edit(s); in
the gate total, reviewed by no round`, which is the closing-edit class this wave
writes into the manual, printed by the tool one line under the totals. nova is
FOUR rounds, 55 findings, 17 blockers, 106 edits over 790 changed lines, 2.2 h,
and it is the first gate against a target other than `mocker` and the first
whose launched run was then exercised by a human in a browser — **its numbers
are quoted from its write-up and are NOT re-derivable here**, because that gate
ran on another machine and no workspace for it exists on this box.

- *Two of the EIGHT proposals were REFUSED, and one of the refusals is a claim
  that did not reproduce at all.* The write-up reports `wf freeze` refusing while
  an untracked scratch file sat in the workspace ROOT, with a message reading as
  though an artifact were dirty. Reproduced step by step, it does not: `wf freeze`
  reads `Dirty("artifacts")` and froze that workspace cleanly. What DOES speak is
  `wf verify`, which reads the whole tree, and it says the right thing — the file
  is listed as `uncommitted`, and the note under it reads "the artifacts match
  the freeze — every mismatch above is uncommitted round bookkeeping. Commit it;
  do not re-freeze." The wave banks the reproduction rather than the report. The
  second refusal is `wfcheck` measuring a schema's serialized WIDTH statically.
  It can read the literals, and that is not the objection: `wfdry` measures the
  width with `JSON.stringify` on the SAME engine the script runs on, which is
  exact, while a static walk would answer with Go's marshaller — a second
  measurement of one quantity, differing by the escapes, on a band whose whole
  window is 259 bytes. This project has never shipped two answers to one number.
  What was real in that finding was REACHABILITY, so `authoring.md` now says a
  schema edit is a `wfdry`-worthy event and carries nova's own measurement of
  what the shape costs: one bar report turned into a required-keyed object took
  three composed schemas to 4663, 4658 and **5312** bytes, all above the corpus's
  measured refusal, because an inline leaf serializes once per key; referencing
  it brought the same three to 1662, 1657 and 2311.
- *`wf init` refuses a workspace that resolves under `-target`, and the mistake
  it closes is silent from the only directory an operator is standing in.*
  Reproduced verbatim: `cd <target> && wf init <name> -target <target>` created
  `<target>/<name>`, printed the normal five-line summary and exited 0. The
  refusal resolves BOTH sides through symlinks and through the nearest EXISTING
  ancestor of a root that does not exist yet — without that, a target reached
  through a symlinked parent compares unequal to a workspace under the same real
  directory and the check misses exactly the operator whose box has one — tests
  the relation with `filepath.Rel` and a separator-aware `..` test rather than a
  string prefix (a directory named `..bak` under the target gives rel `..bak`,
  and a prefix test reads the one shape this exists for as outside), and fires
  BEFORE `MkdirAll`, because a refusal that leaves the tree dirty has committed
  the thing it refused.
- *The batch header's brackets, and the half of that defect nobody had noticed.*
  `gate-harness.md` renders the header `=== <file> [count=N] [note=...]`, where
  the brackets mean OPTIONAL inside a format whose other delimiters are literal;
  P3f's operator wrote them and paid a round-trip. Reproducing it found the
  count half exactly as reported — `unknown header field "[count=1]"`, which
  names the right spelling and not the mistake — and found that the NOTE half
  was accepted SILENTLY, with the closing bracket inside the note and a wrong
  value written into the commit. Both are refused now by one message that says
  to drop them, scanned only BEFORE the `note=` index so a note whose free text
  quotes the bracketed form still parses, and the manual and README gained the
  literal spelling.
- *`wf check closure` names the two populations it is not about.* Reproduced:
  `wf round record` printed `open 1` and the check printed "1 closure(s)
  claiming a commit, 1 checked, 0 not closed by that commit" over the same
  round — both true, over different populations, and read together the command
  called `closure` says everything checks out while the freeze is about to
  refuse. The rows carrying no commit are counted and split the way `record`
  splits them (empty cell / closed by prose), so the two commands cannot
  disagree in the operator's hands again. And the closures whose commits were an
  ADOPTION are named BY ROW, which is P3f's real finding: one of its closures
  was banked in the ledger and never written into the code, this check passed it
  correctly by its own rules, and the fact separating it from its neighbours —
  the commit carried no verified batch — was in git all along. Every sha of a
  closure must carry the trailer, never any, or a row resting half on a verified
  batch would be called unverified. Both lines are silent on an empty
  population: the `-whole` trailer is one wave old, so a printed `0 rest on an
  adoption` over an older workspace would be this project's own most-repeated
  defect. The list is grouped BY ROUND — the review round found the first
  version printing all 47 of P3f's adopted closures as one unwrapped line under
  an instruction to byte-verify each, which is the `dup` 161-of-176 lesson
  arriving in a report that had already been narrowed once.
- *`wf round close <finding#> <commit>` — the one bookkeeping step of the round
  loop with no command behind it, and the gap corrupted a live ledger twice.*
  Both times it was the operator's own `awk -F'|'`: the table escapes a literal
  pipe as `\|` — the escape exists because an unescaped one once scored an open
  BLOCKER as "closed by prose" and froze the round — and every field-splitting
  tool splits on the escape too, writing the commit id into the middle of a
  finding's text. `ws.SetClosedBy` rewrites the cell between the fourth and
  fifth UNESCAPED pipes and returns everything else byte-identical; the command
  validates the sha as a workspace commit before touching the file, re-parses
  the bytes it is about to write and refuses if the number, the text or the
  closure reads back differently, refuses the scaffold's empty row as a form
  rather than a claim, refuses a duplicate finding number as ambiguous, and
  refuses an already-closed cell without `-replace`. Its own first test found
  the last refusal missing: `-replace` naming the SAME commit writes identical
  bytes and died on git's `nothing staged`, a git-shaped error for an
  operator-shaped mistake. One branch was DELETED rather than shipped — the
  empty-value special case in `SetClosedBy`, which was both unreachable from the
  command and wrong, writing `| |` where the general path already writes the
  scaffold's own `|  |`.
- *`wfcheck`'s placeholder check fired ZERO times over the corpus, including on
  the gate that was filing it as a BLOCKER.* Measured over 73 frozen script
  revisions across the twelve unpruned workspaces: the predecessor required a FENCED line — four
  leading spaces and a command from a seven-word list — and both halves of that
  enumeration defeated it. P3f's command was `sed`, which was not on the list,
  and its other instance read `  3. Run ONLY the named test: go test … -run
  '<the test name>'`, which is two spaces of a numbered list and not four of a
  fence. This is the wave-18 rule measured again: a hand-written enumeration
  cannot be complete. The shape is now the command plus a placeholder on the
  same line, anywhere in the prompt, and it was NARROWED BEFORE it shipped
  rather than after: over those same 73 revisions the built binary reports 40
  lines on 23 of them, 12 of the quoted lines distinct, three of which are the
  defect and the rest ordinary templates an agent is meant to substitute
  (`git diff HEAD -- <path>`). Which of the two a line is depends on whether the receiving agent
  substitutes, and that is not in the text — so the wide shape is an INFO that
  does not move the exit code, and the fenced shape stays a WARN because a
  command on its own indented line has no reading under which the agent fills it
  in. One report per site, never two. It catches ONE of P3f's three instances on
  the revision that carried them, and that is the honest number: the other two
  live in prompts composed at run time, which `wfcheck` already reports as
  outside its reach.
- *The scaffold now asks for the `[fix-induced]` marker, where the operator is
  when the row is written.* nova marked not a single finding and `wf stats` said
  so plainly — after the gate — while at least two of its findings were the
  previous round's own repairs. The marker is opt-in and is the only way that
  share is ever measured; the paragraph above the table went from two rules to
  three, and a test asserts the count in the sentence matches the list under it,
  because "a count that became a claim" is this file's own most-banked class.
- *TEN edits changed the MANUAL, and the two largest are about a script's SHAPE
  rather than its contents.* **No phase that can WRITE may run after a phase
  that MEASURES** (`authoring.md`): P3f's last fix-bearing gate ran after its
  acceptance phase, so all eight measured terms were stale-able, and rounds 9,
  10, 11, 12 and 13 each found exactly one of them — five rounds, one defect,
  with the sibling guard rule silent throughout because nothing was being
  weakened; each fix was locally correct, one accumulator at a time, forever.
  Reordering deleted 149 lines and all five classes at once for the price of one
  new phase. **ENUMERATE by COMMAND when a round's findings keep having the same
  SHAPE** (`round-discipline.md`): the rounds that ended that gate were the two
  that told a lens to build COMPLETE tables by command instead of reading and
  judging — two questions closed permanently, the sixth instance of a five-round
  shape found as one round's only blocker, and they were the cheapest rounds of
  the gate per defect closed. Beside them: the auditor regress and what ends it
  (split the verdict's terms into MECHANICAL and JUDGEMENT, and gate the
  judgements on STRUCTURE, never on whether an answer was true); the POSITIVE
  form of the fixture limit, rediscovered on three separate rounds of one gate —
  the strongest gating shape a fixture cannot defeat is a reviewer's findings
  ARRAY, because the runner empties it; **measure the bars on the untouched tree
  before writing the criterion** (`gate-checks.md` §D), where nova found its
  target's own `CLAUDE.md` claiming a green type-checker over five errors and a
  root test command that never reaches the package the slice changes; the
  external lens's failure mode is NO finding arriving as exit 0, so its brief
  carries an output CONTRACT (nova's round 1: 15 700 lines of trace, no FINDINGS
  section, exit 0); the severity scale restated WHERE the verdict is asked, after
  five of about a dozen reports said `VERDICT: blockers` over findings that were
  all MAJOR or below; a closing edit costs the round its MEASUREMENT, because
  `wf freeze -refreeze` drops it from the cost meter's prediction basis; and
  `-whole` is the NORMAL shape for a script artifact rather than a lapse, with
  the hand-verification pass budgeted into the round and `wf check closure`'s new
  adoption list as its work-list.
- *And the manual gained the one thing nova measured that this corpus had no
  place for: **the gate is a bound on what an unsupervised RUN can get wrong,
  and not a bound on what the FEATURE can get wrong**.* Four rounds and 55
  findings hardened that script; the run came back green on every bar with zero
  ownership violations and one violated acceptance clause that was genuinely
  right; then a human opened the page and found two defects reachable from
  neither artifact — a tab indicator drawn 7px beside an 8px sibling, correct in
  isolation, and a one-frame radio flicker from a third-party table clearing its
  own selection state on every data push, a mechanism inside `node_modules` and
  named nowhere. Both are visual, and §D's own rule that a clause asserts what a
  COMMAND can observe correctly forbids writing one for either, so the class is
  uncovered BY CONSTRUCTION. For a backend slice the two bounds nearly coincide,
  which is why a corpus of backend slices never had to say it. `round-discipline.md`
  now has a short section naming the gap, what the gate still buys there (two of
  nova's round-2 blockers were ownership holes that would have wasted the whole
  2.4-hour run), and the window after the gate closes — where that same slice's
  own fix for the flicker introduced a fourth defect, in exactly the shape the
  stop rule already warns about.

- *And the wave earned its review round, for the fifteenth time running, and the
  flagship command was again the worst of it.* One external model over the diff,
  then two Opus vectors (Go correctness with a mutation sweep in an ISOLATED
  WORKTREE, and claims/specification), against five green bars. Between them:
  **one defect of BLOCKER shape, fifteen majors, a mutation score of 36/20/16
  and eight numbers wrong** — and every code finding but one landed on
  `wf round close`, the thing this wave built rather than corrected.
  The blocker-shaped one is the command's own subject: `wf round close 1 HEAD`
  was ACCEPTED and wrote the literal `HEAD` into the cell, so every reader
  downstream saw a closure "by prose" — `ws.FindingRow.Shas()` wants hex with a
  digit — which means `wf check closure` skipped the row and `wf freeze` passed
  the BLOCKER as prose-closed with a verify-by-hand note. The command whose
  entire purpose is a checkable commit reference was quietly producing an
  uncheckable one, and a symbolic ref is the natural thing to type. It resolves
  through `rev-parse` now, stores the short form (the full one when the short
  has no digit, which the ledger's own detector would refuse for the same reason
  it refuses `deadbeef`), and the `-replace` comparison is between COMMITS, so
  an abbreviation or a different case of one sha no longer reads as two.
  Second by consequence: a commit that FAILS — a hook, a read-only object store
  — left the ledger rewritten AND staged under a bare `exit status 1`, and both
  ways back in then refused (`already closed by …` without `-replace`, `names
  the same commit` with it), so the only exit was the hand `git commit` this
  command exists to remove. It rolls back now, and the rollback's own failures
  are reported rather than swallowed.
  The sweep ran 36 mutations, 16 surviving, and **eleven of the survivors were
  real**: the `EvalSymlinks` ancestor walk — the load-bearing half of the init
  refusal — was defended by nothing and demonstrably non-equivalent (with it
  replaced by a plain `Abs`, a `-dir` reaching the target through a symlinked
  parent created the workspace inside it); `isAdoption`'s two stated decisions
  (EVERY not ANY, and an unreadable commit counted as neither) were both
  unproven and both reachable, though multi-sha closures are the corpus norm;
  the open/prose split was tested with one row of each, so the line reads
  identically with the counters swapped, which is a floor armed on the value that
  disables it; `verifyClosureLanded` was exercised by nothing at all and is now
  pinned directly, being unfalsifiable through the command; and the duplicate-
  number refusal, `clip`'s rune slicing and `lineAround`'s arithmetic had no case
  each. Three guards were DELETED rather than shipped unproven — the `r.Line`
  bounds check (`ParseFindings` numbers its rows over the same split that
  indexes them), `resolveDir`'s error return (`filepath.Abs` fails only when
  `os.Getwd` does) and `isAdoption`'s `len(shas) > 0` (the caller skips such a
  row two lines earlier).
  Three findings were about a report painting rather than a defect: the adoption
  line printed **47 of P3f's 121 closures as one unwrapped line** of ids under an
  instruction to byte-verify each — the `dup` 161-of-176 lesson arriving in a
  report this file had already claimed was narrowed before shipping — and it is
  grouped by round now, with the share stated where the feature is described; a
  MALFORMED row was in NO count of the one report whose subject is the
  populations it is not about; and an empty population printed
  `0 closure(s) claiming a commit, 0 checked, 0 not closed` and exited zero,
  which is this project's single most-repeated defect, in the check this wave
  edited to state its populations. The `wfcheck` fenced branch tested the LINE
  rather than the match's position in it, so an indented English sentence
  mentioning a command was escalated to WARN under a message asserting there was
  no reading under which the agent fills the placeholder in.
  Of the wrong numbers the sharpest is one this wave INVENTED and one it
  inherited. **"14 distinct occurrences, about 0.6 per revision" reproduces under
  no reading** — the two halves are arithmetically inconsistent with each other
  (14/73 is 0.19), and both came from the ripgrep probe used to decide the
  design rather than from the shipped binary, which reports **40 lines on 23 of
  the 73 revisions, 12 of the quoted lines distinct, three of those the defect**.
  And **"the longest gate in this corpus by rounds" is false**: a-mcp ran 25
  rounds, P3a 24, P3b 22, and p2e ties P3f at 17 — while this same file says
  so 500 lines below, in the bullet that names P3b and P3a as the largest gates
  here. Also: "across ten gates" for a corpus of twelve workspaces; P3d's span
  banked as 9.1/9.2 h when both `wf stats -time` and its own `close/stats.txt`
  say **13.6** (the banked file was rewritten by the last `wf close`, so the
  "banked against re-run" contrast this file drew no longer exists); P3h's
  fix-induced share stated twice in one bullet in two different sets of numbers,
  the stale half surviving inside the very edit that corrected the headline; nova
  credited with three defects a person found that neither artifact could reach
  when the write-up says two of the three (the third is the clause the gate
  caught); the queue counted as five built / seven proposals against six and
  eight; the authoring median left at 10.7 when a twelfth value made it 10.3;
  P3e's fix-induced share over a denominator its own bullet corrected four lines
  above; p2d called a 10-round gate where `wf stats` says 13; and the checkbox
  floor quoted as 147 in the sentence describing the guard this same commit
  raised to 148.
  One finding was REFUSED with the code: `isAdoption` is not defeated by a
  commit whose SUBJECT quotes the trailer — the regex is anchored at a line
  start for exactly the reason the neighbouring comment gives, and it is the
  same standard the cost meter has always applied to the same trailer.

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

**`parseMixed` instead of `fs.Parse`.** `flag.FlagSet` stops at the first
positional argument, so `wf init p2c -target X` silently dropped `-target` and
printed a usage error naming the very flag that was passed.

**`safeJoin` in apply.** The edit list is usually written by an agent, and an
agent that has confused the workspace with the repository under review must be
stopped here, not discovered by a commit in the wrong tree.

**The twenty-first wave — P4a, and the three RUN rounds that each found what the
review rounds had not** (`docs/feedbacks/FEEDBACK-p4a.md`). 16 rounds, 97
findings, 30 blockers, 127 edits over 1195 changed lines, 9.4 h of review across
13 review rounds at a median of 21.5 min, three `-kind run` rounds counted apart
at 124.7, 38.3 and 8.5 min, two artifacts — a decision document gated for six
rounds, an orchestration script added at round 7 and gated for four more, then a
THIRD artifact (an evidence-only script derived from the second by deletion)
added at round 12. Prediction basis 5.5 per 100 changed lines against a gate
actual of 8.1 — and that basis reads **5.2 over 53 findings and 1029 changed
lines** under the binary the NEXT wave shipped, because the run round left the
population; the 5.5 is what the gate itself was predicted from and is left
standing as that, named rather than silently corrected. Every claim below is
what the gate REPORTED, written down before any of it was checked; the
twenty-second wave reproduced them one at a time and one did not survive.

- *`wfdry` prescribed the keyword the live launch refuses.* Round 8's dry run
  WARNed that a boolean outside its pass vocabulary reads false in every mode and
  advised `'x-pass': true` on the two booleans of an evidence-row schema. The
  launch refused the whole schema — `strict mode: unknown keyword: "x-pass"` —
  killing the first evidence agent at spawn and, through the script's own abort,
  the other two: 26 of 27 acceptance properties came back with no evidence after
  1.9 M subagent tokens and 116 minutes. The remedy that works is to NAME the
  fields inside the runner's vocabulary (`proven`, `clean`) rather than annotate
  them. Note the sixteenth wave already carries `x-pass` on ARRAYS; this is the
  same keyword failing at the other end, in the LAUNCH validator rather than in
  `wfdry`.
- *`gate-checks.md` section E names the class and not this instance.* Section E
  is written about what the dry runner cannot SEE. This defect is the opposite:
  the runner saw correctly, and its REMEDY was invalid in the harness it stubs. A
  `wfdry` remedy that changes a schema is a claim about the launch validator,
  which `wfdry` does not run.
- *A decision document's arithmetic is a falsifiable claim about the code and
  nothing made it be run.* The acceptance section pinned a grep count at six.
  Six was unreachable by any correct implementation — the function whose call the
  rule required deleted emits one row per suggestion and needs the list its
  replacement does not return. It survived five review rounds because it was
  internally consistent, and the LAUNCH found it, at the one step where changing
  production code is usually forbidden. Proposal: every number an acceptance
  property pins is produced by RUNNING its command at the moment the property is
  written, and the run recorded.
- *`wf round close` takes only a commit while `wf round record` reports
  `closed by prose` as a first-class category.* A NOTE row asserts no failure and
  has nothing to close; its cell can only be filled by editing the ledger by hand
  and re-recording — the one path the harness exists to remove. Three
  round-trips in this gate.
- *`wf delta` saves the brief only under `-save`, and nothing says so where it
  matters.* `wf round new` writes a findings scaffold and no brief; an operator
  who runs `wf delta` reads a correct diff on stdout and has no reason to think a
  file was expected. A reviewer handed a lens naming the absent
  `rounds/rN/brief.md` reconstructed it from the previous ledger and the tag diff
  and reported the gap. Either `wf round new` writes the brief or `wf delta`
  saves by default.
- *The cost meter prices a `-kind run` round against its fix's line count.*
  Round 14 prints `80.0 finding(s) per 100 changed line(s)`: four findings from a
  five-agent launch over the 5 lines its fix happened to touch. Run rounds are
  already excluded from the round-time median and from the prediction basis; the
  ratio should exclude them for the same reason.

And one number this gate contributes that no previous one could: **resume makes
a run round an instrument rather than a destination.** Round 16 replayed three
mutation agents and a fingerprint from cache and ran only the one changed prompt
live — 110 k subagent tokens and 8.5 min against round 14's 564 k and 38.3 — and
that made it a controlled experiment with a single live variable, in which the
two blockers the previous launch had produced did not return. Three run rounds,
and the first clean one is the sixteenth; the argument this gate's numbers make
is for reaching for a launch EARLIER, not for treating it as the thing a gate
builds toward.

**The twenty-second wave — P4a reproduced, and the annotation the manual had
been prescribing for two waves is refused by the launch everywhere**
(`docs/feedbacks/FEEDBACK-p4a.md`). Six claims, taken one at a time against the
code and against the workspace, which is on this box: five reproduced, one had a
FALSE PREMISE and the defect under it is larger than the report, and the
flagship one turned out to reach a shipped feature nobody had ever launched.

- *`x-pass` is refused by the launch ANYWHERE in a schema, so the sixteenth
  wave's array feature was poison too, and it had never been through a live
  launch to find out.* The report is that `wfdry` advised `'x-pass': true` on two
  booleans and the launch refused the whole schema. Reproduced from the
  workspace's own artifacts, which now carry the diagnosis as a comment. What the
  report does NOT say is that `authoring.md` prescribed the same keyword for
  ARRAYS at three sites and `pre-flight-gate.md` at a fourth — banked as a
  shipped feature by the sixteenth wave from a `wfdry` measurement, never once
  sent to a launch. **Probed rather than argued** (P3d's precedent, and the whole
  probe cost about 40 seconds and 390 k subagent tokens): `x-pass` on an array
  REFUSED, `x-pass` on a boolean REFUSED, `x-foo` REFUSED, a plain
  `passPolarity` REFUSED, the byte-identical schema without them ACCEPTED — so
  the refused class is UNKNOWN KEYWORD and the `x-` prefix is not the
  discriminator, which the fourth probe is the only thing that says. `$comment`,
  `description` and `title` are all ACCEPTED, on the root and on a leaf. The
  declaration moved to `$comment` — a standard annotation keyword the validator
  knows, and the one of the three the MODEL does not read as guidance about the
  field — and `wfdry` now REFUSES an unknown keyword as a mode failure rather
  than a WARN, because the launch refuses it at spawn and the stub should die
  where the agent would. The check reads the SERIALIZED schema and not the
  composed object: `JSON.stringify` drops a key whose value is `undefined` or a
  function, and reading the object would refuse a schema over a key nothing
  sends — the same boundary the width has always been measured at, and it is one
  reading, not two. It walks STRUCTURALLY rather than descending into every map,
  because under `properties` the keys are field names and under `enum`, `const`
  and `default` the value is data the agent may return; a generic walk reports
  both as keywords. **Narrowed BEFORE shipping, over 191 frozen script revisions
  across the 13 gate workspaces on disk: it fires 4 times, and all four are the
  P4a revisions that carried the annotation** — no other revision in the corpus's
  history is painted, and the four are the window between the round that followed
  the WARN and the round that undid it. The allowlist is a hand-written
  enumeration, which this file's own rule says cannot be complete, so the
  direction of its incompleteness is chosen and said: a missing keyword produces
  a red dry run over a schema the launch would have taken, against a false green
  that cost 116 minutes and 26 of 27 properties.
- *The cost meter's claim in the feedback is FALSE, and the real defect is one
  layer deeper than the one reported.* The report says run rounds are "already
  excluded from the round-time median and from the prediction basis" and asks for
  the per-round ratio to follow. They were excluded from the median and from
  NOTHING else: `batchHistory` never looked at a round's kind, so P4a's own
  basis line — `rounds 2-6, 8-10, 13-15` — contained run round 14, whose four
  findings from a five-agent launch over the 5 lines of its fix are the 80.0 per
  100 the report complains about, sitting inside the 5.5 the gate was predicted
  from. Excluded now in all three places and NAMED in each, beside the two
  exclusions already there: the per-round line, the basis line, and the apply
  note the operator actually acts on — a basis line that names an exclusion while
  the note does not is two commands and one number, which this project has
  already paid for once with the re-freeze exclusion. P4a re-prices to **5.2 over
  53 findings and 1029 changed lines**.
- *`wf round close` gained the door the harness already reported through.* A NOTE
  row asserts no failure and has nothing a commit could close, while
  `wf round record` has printed `closed by prose` as a first-class category since
  it shipped — so the only path was the hand ledger edit plus a re-record, three
  round-trips on that gate, and the hand edit is the one this command exists to
  remove. `-prose "why"` writes the same cell through the same rewrite and
  read-back, and refuses: a newline (a row is one line, and the rest would land
  in the document as text nobody wrote), prose carrying a sha-shaped token
  (`ws.FindingRow.Shas()` is asked, not a second detector — such a cell is a
  COMMIT claim to every reader downstream, and `wf freeze` would demand the
  commit exist), a commit argument beside it, the scaffold's empty row, and a
  `-replace` naming the same sentence. A literal pipe is ESCAPED rather than
  refused — a shell pipeline quoted in a sentence is the ordinary reason a cell
  holds one — and an already-escaped one is left alone, because the fix for
  following the ledger's documented rule must not be to stop following it. It
  says in the same breath that a prose closure is checked by no command, and on a
  BLOCKER row it says that too, where the row is written rather than leaving it
  to the freeze's verify-by-hand note.
- *`wf apply` was the last command still parsing its own arguments the wrong
  way.* `wf apply <file>` printed a usage line naming `-f`, and two gates
  reported it. The cause is that `cmdApply` used `flag.Parse` while every other
  command has used `parseMixed` since `wf init p2c -target X` silently dropped
  the flag it was given: a positional stops `flag.Parse`, so the file was
  discarded AND every flag after it. Both halves are closed — `parseMixed`, and a
  lone positional accepted as the batch — and a second positional is refused
  naming what the batch is.
- *The brief that exists nowhere, said by both commands that could have said it.*
  `wf round new` writes a findings scaffold and no brief, `wf delta` prints to
  stdout unless told `-save`, and a lens whose prompt names `rounds/rN/brief.md`
  as required reading finds nothing there — P4a round 15, where the reviewer
  reconstructed it from the previous ledger and the tag diff and REPORTED the
  gap, which is the only reason it is known. `wf round new` names the command
  when it opens a DELTA round, which is the kind briefed from a diff, and
  `wf delta` says on STDERR — stdout is the brief — that the round holds no
  `brief.md`, and stops saying it once one exists. Not made to save by default:
  `wf delta -from X -to Y` outside the loop is a legitimate read, and a read
  command that commits a file is a worse surprise than the one being fixed.
- *Two manual edits, and one of them is the class §E is written about, arriving
  from the opposite direction.* `gate-checks.md` §E is about what the dry runner
  cannot SEE; this defect is the runner seeing correctly and its REMEDY being
  invalid in the harness it stubs, so §E gained the entry that a `wfdry` remedy
  changing a SCHEMA is a claim about the LAUNCH validator, which `wfdry` does not
  run — follow a naming remedy freely, treat a remedy that adds a KEY as
  unverified. And §D gained the rule the launch found by running: every NUMBER an
  acceptance property pins is produced by RUNNING its command at the moment the
  property is written, and the run recorded — P4a pinned a grep count at six,
  six was unreachable by any correct implementation, and it survived five review
  rounds because it was internally consistent with the prose around it. That is
  §D's own bars rule extended from the bars to every other number in the section.
  Two provenance entries, M-35 and M-36, and the checkbox floor moves to 150.

- *And the wave earned its review round, for the sixteenth time running, and this
  time the flagship was wrong in the one direction it had claimed to have
  excluded.* One external model over the diff, then two Opus vectors (Go
  correctness with a mutation sweep in an ISOLATED WORKTREE, and
  claims/specification), against five green bars. Between them: **one defect of
  BLOCKER shape, thirteen majors, 44 mutations with 16 survivors, and a number
  this wave INVENTED.** (The wave's own sweep before the review was 28 mutations
  with none surviving, which is what a sweep over the guards you thought to write
  measures; the sixteen are the guards nobody thought to write, and a second
  sweep of 15 over everything the review added kills all fifteen.)
  The blocker-shaped one is `README.md`, which went on prescribing
  `"x-pass": true` AS A SCHEMA KEY at two sites while the manual, the runner and
  the code had all moved to `$comment` — the command reference an agent is told
  to read FIRST, telling authors to write the exact line that kills a run at
  spawn. The same file documented the new refusal nowhere at all, which is this
  file's own "a shipped capability the report never mentions is a capability
  nobody has".
  Sharpest by consequence: **`launchKeywords` was the UNION of draft-07 and
  2020-12**, so its incompleteness was TWO-WAY while the comment above it sold
  the check on being one-way — every keyword wrongly IN the list is a false
  GREEN, the direction the check exists to remove. Settled by a second probe,
  seven agents: `definitions`, `dependencies` and `$anchor` ACCEPTED,
  `prefixItems`, `dependentRequired` and `unevaluatedProperties` REFUSED as
  unknown keywords, and `additionalItems` refused for a DIFFERENT reason
  (`"additionalItems" is ignored when "items" is not an array of schemas`, which
  is strict mode complaining about a keyword it KNOWS). It is draft-07 plus
  `$defs` and `$anchor`; every later spelling came out, and the corpus still
  paints 4 of 186.
  **The number this wave invented is its own narrowing denominator: 191 does not
  reproduce, and the reading is 186** — the first scan counted every commit in
  which each script's blob merely EXISTED rather than every commit that TOUCHED
  it, which multiplies a workspace by the number of scripts it carries. Both
  reviewers reached 186 independently. And the sentence around it was wrong in a
  second way: the four painted revisions are not "the window between the round
  that followed the WARN and the round that undid it" — the annotation is in the
  artifact's FIRST commit, which PRECEDES the dry run the write-up credits with
  prescribing it.
  Four more numbers and pointers: the corpus enumerations still read TWELVE
  workspaces in four places inside the commit that raised the census to thirteen
  (re-pooled: 179 spans of 199, median 24.2, middle half 17.6-32.2, review share
  89.9%, authoring median 9.9 over thirteen); "two gates reported the positional
  form" is one; "the third probe" is the fourth; and the checkbox-floor sentence
  went stale for the third wave running, at 147, 148 and now 150.
  In the code, five more: the delta-brief note fired on an ad-hoc `-from/-to`
  read and — with the content comparison added the same hour — told the operator
  that a correct, current brief was a DIFFERENT one; `-prose ""` read as "no
  prose", so `wf round close 1 <sha> -prose ""` closed by COMMIT in silence (it
  is flag PRESENCE now, not emptiness); `polarityRe` had no right boundary, so
  `x-pass: falsely reported` parsed as `false`; the mis-spelling test matched the
  bare WORD, so any `$comment` merely mentioning the annotation was reported as a
  broken one; and `serializedSchema`'s own comment claimed to be "the ONE
  reading" while the code stringified twice.
  Of the 16 survivors, ten named a real gap and are pinned now — the two
  enumeration tables are the interesting pair, because a table read from the map
  it tests loses its case together with the entry, so both are written out
  LITERALLY beside the map. Two lines were DELETED rather than shipped unproven
  (the two `sort.Strings` inside the walk, whose ordering is unobservable because
  the result is sorted once at the end), and one condition was KEPT with a
  comment saying no mutation can kill it and why (`!byProse` in front of
  `sameCommit`, where `sha` is empty and the call would answer false anyway).
  `roundKinds` was hardened past what the review asked: it took the NEWEST
  subject matching `round N: open (kind)`, so any later commit whose message
  imitated one re-labelled the round for the time median, the lens classification
  and — as of this wave — the cost meter. The oldest match is the actual opening
  commit.
  Two tests were rewritten because they passed with their own claim reverted: the
  positional-apply test asserted a clean tree, which holds with `-m` discarded,
  and the stderr test ran through a helper that points ONE pipe at both streams,
  so it would have passed with the note written to stdout — the exact failure its
  comment says it guards.

**The twenty-third wave — the reviewer's brief is a scaffold, and the corpus
refused the other three things the wave was about to build.** No gate feedback
behind it: the question was whether gate reviewers should be able to ASK the
operator mid-round, and the answer measured out to no — a reviewer's question
IS a finding, the row is recorded and checkable where a mid-round question is
an input nothing holds, and the two vectors that actually needed something got
it in a different shape.

- *An OPEN question filed by a reviewer is a BLOCKER, and the scale now says so
  where the level is chosen.* `gate-checks.md` §0 listed "a question" under NOTE
  while §0c, 115 lines down, said "it is a QUESTION, not a NOTE" and named no
  replacement — and the scale is CLOSED, so a reviewer told which level NOT to
  use was left to derive one, which is how 1065 findings grew four vocabularies.
  §0's discriminator was also failing on its own population: "with no failure
  attached" is unanswerable exactly when whether something fails is the open
  question, so it is now "a question whose answer would change nothing". §0c
  DERIVES the level rather than choosing it — §0 holds a MAJOR to naming the
  later gate that catches the defect, and no gate catches a decision nobody
  took — and §I's deferral KEEPS its level, with the exit stated, because a
  deferred BLOCKER loops the round and `wf freeze` refuses while it stands.
- *`wf new-lens`, and the measurement that chose it over three other shapes.*
  Roughly a third of every banked reviewer brief is the manual RE-AUTHORED by
  hand, 200 of the prompts name `gate-checks` and exactly ONE names `wf docs`,
  and 26 carry a severity-scale line the manual has replaced — invisible,
  because `wf check dup` runs over artifacts and a prompt is not one. The
  numbers, their reading and the three readings that did NOT reproduce are
  banked in `measured.md` [M-37] rather than here or in the manual. What ships
  emits the fixed half and POINTS at the pinned `gate-checks.md` §0 and §0c
  instead of restating them; the aim is a marker line `wf round lens` refuses to
  bank. It is NOT the measurement that justified `wf new-script`, and the
  comment says so: that one counts block NAMES at five of six and records the
  blocks as re-authored at 0.05 similarity, this one counts identical line text
  at five of 313.
- *One agent definition, and the corpus refused the other.* `agents/gate-sweep.md`
  pins a tool list and nothing else. The case for pinning a MODEL was killed by
  the roster: across the banked lens records the sonnet rule shows 88 opus and
  ZERO sonnet before the day it was set, and zero opus in the five gates after
  the one it landed in — it is kept by hand, and a pin buys nothing. `isolation`
  is a parameter of the Agent CALL rather than frontmatter, so the definition
  says to pass it instead of claiming to carry it.
- *And the wave earned its review round, for the seventeenth time running, and
  the flagship was again the thing built rather than corrected.* The refusal the
  whole feature is described by — never write into `rounds/rN/`, where a file
  clears the freeze and close refusals demanding a round was read — **did not
  fire for a relative `-o`**, which is the spelling an operator types. It
  compared `filepath.Dir(dst)` against an ABSOLUTE round directory, and the test
  that pinned it built an absolute path: a guard armed on the one input the
  defect does not take, which is this file's own floor-on-the-wrong-field rule
  wearing a path. Both refusals compare RESOLVED paths now, across every round
  rather than the one being briefed. Beside it: `-o` was bounded by nothing and
  would write into the read-only TARGET; the template POINTED at §0 and
  restated its decision rule four lines below the pointer, in a file
  `make check-docs` does not stage, so the command built to stop unchecked
  copies emitted one; `-asks` was validated and then changed nothing while its
  help said it shaped the reading list; a `-round -3` was reported as "not
  given"; and a `-round 99` was accepted over a gate with one round while
  `wf round lens` refuses exactly that. Of the numbers, the wave's own headline
  share was wrong: 32.8% came from building the frequency table over
  `set(lines)` while keying on the STRIPPED text, so two lines differing only in
  leading whitespace incremented one key twice. It is 29.9%. And "16 120
  non-blank lines, median 55" carried one reading of each — the non-blank median
  is 44.
  The sweep ran 34 mutations with 22 surviving, and every survivor was a guard
  nothing proved — the positional check, the `-kind` and `-asks` refusals, both
  halves of "never over a file that exists", `MkdirAll`, the no-artifact reading
  line — including the wave's own central decision, because a mutation making
  EVERY round default to `full` passed all five bars. Ten cases were added and
  each was verified to kill its mutation; one wiring is documented as
  UNKILLABLE with the reason, because freezing `strings.Join(ws.Levels, ", ")`
  to the literal it currently produces passes any test that compares against
  that literal. Two of the wave's own assertions were self-referential and
  passed with their claim reverted — one compared the brief against the same
  expression the production line uses, the other asserted the template contains
  `lensAimMarker`, so narrowing the constant to `"<<<"` moved both sides
  together. Both are literals now, beside the source.
  The external vector found the three the others did not. A failed write left a
  PARTIAL brief on disk, which is still a `lens-*.md` and would be banked
  verbatim — the `wf round close` rollback of the wave before, one command over.
  The marker refusal was exact-byte, so an operator who REWORDS the line instead
  of replacing it walks past it; any prompt holding both `<<<` and `>>>` is
  refused now. And the brief asked for a `where` COLUMN, which keeps the path
  out of the finding text — and `wf check closure` reaches a closure only
  through the path the finding names, so the transcription would have dropped
  the one thing that check needs. The brief asks for the ledger's own four
  columns now, with the pipe-escaping rule beside them.
  One more copy of the scale was found by the same vector: `templates/script.mjs`
  defines the four levels in a schema `description`, where nothing can point,
  and its `note` clause carried no open-question rule — the fourth copy
  disagreeing with the three this wave had just reconciled.
  And the wave made the mistake its own manual names: the tree was EDITED while
  the external vector was reading it. The sweep vector was isolated and the
  external one was not, so a third of its findings describe code that had
  already changed. Nothing was mis-triaged, and the barrier rule is about
  reviewers of one round, which this was.

**The twenty-fourth wave — A4, and the report the gate had already printed and
nobody read** (`docs/feedbacks/FEEDBACK-a4.md`). 8 rounds, 70 findings, 20
blockers, 85 edits over 916 changed lines, 2.7 h, median round 19.5 min, TWO
artifacts in one workspace with independent descents — a decision document at
9-6-0-0 and the orchestration script, brought in by `wf add` at round 5, at
2-2-1-0 — and its launch came back `green: true` over 13 agents in 1.5 h. Each
of its five claims was reproduced against the workspace before a line was
written, and the flagship one is FALSE in its stated mechanism while the defect
under it is real and larger.

- *The three closure false positives are not `-whole` shifting the lines, and
  one of the three is the check being RIGHT.* The write-up blames a whole-file
  adoption for moving a cited `:545` out from under the check. It does not
  reproduce: between `gate-r5` and that adoption's parent the artifact was not
  touched at all — only lens and record commits stand between them — so the
  shift is zero and the map has nothing to do. r5 #5 cites `:545`, which is
  `reviewers: [` in the parent, and the commit rewrites 546; r5 #6 cites `:656`
  and the commit rewrites 654. Both are the OFF-BY-ONE citation habit the
  write-up's own section 0 banks three instances of, and the check reported them
  correctly. The third, r4 #5, is a real defect and a different one: the finding
  cites `decisions.md:607-611` — which the commit covered — and mentions D11 in
  passing, and the SECTION half PREEMPTED the line half, `continue`-ing before
  the lines were read. The comment three lines above it already said a section
  mentioned in passing is not a finding about it; the verdict had not been told.
  The two halves are ORed now, and where both miss the row carries both reasons
  in one verdict per row. **Narrowed against the corpus before shipping: 92 miss
  rows over the fourteen unpruned workspaces, 83 after, nothing added** — five of
  the removals in P4a alone, each verified by hand to be a commit whose hunks
  cover the cited span.
- *`wf round close` accepted EIGHT closures naming a commit that could not
  possibly have closed them, and the feedback never mentions it.* Round 6's
  findings 2 to 9 each name the ledger commit of the PREVIOUS `wf round close` —
  the operator read `HEAD` after each call and fed it to the next — and every one
  passed `wf round record`, which proves only that the sha exists. `wf check
  closure` printed all eight as FILE misses, in a report run after the round had
  shipped. The refusal is at the point of writing now: a commit touching nothing
  under `artifacts/` is refused, naming what it DID touch and naming `-prose` for
  a row no commit closes. It reads the DIRECTORY rather than today's artifact
  list, because an artifact renamed since would otherwise reject a closure that
  was right when it was written.
- *The ownership check in the emitted skeleton compares an absolute path against
  a repo-relative declaration.* A4's `record` section was reported as writing
  `CLAUDE.md` outside its ownership when `CLAUDE.md` is the FIRST LINE of its
  `YOU OWN` block: the agent self-reported `/home/yakov/projects/mocker/CLAUDE.md`
  and `ownership()` compared with `===`. A false major on a correct write is the
  direction that teaches an operator to stop reading the check. The self-report
  is normalised against `REPO` now, and the test EXECUTES the function on the
  same engine `wfdry` uses rather than asserting on the template's text — a text
  assertion passes with the comparison reverted.
- *And the pipe refusals say what an operator's instinct gets wrong.* A findings
  row carrying a literal `||` inside a backtick code span was refused correctly
  and by a message that named the escape and not the misconception: the row is
  split before any markdown exists, so a code span is not an escape. All three
  refusals say so now.
- *Three manual edits, and two of them are about a number nobody re-ran.*
  `gate-checks.md` §D: a pinned number is re-derived by the round that READS a
  clause resting on it, not only by the round that wrote it — A4's baseline
  recorded 29 passing packages against a real 28, so a clause comparing against
  it would have gone RED against a correct implementation, and it survived four
  rounds including two criterion lenses instructed to test every clause against a
  correct implementation as well as a wrong one; and, from the same incident, a
  clause is written so that no judgement is available, because that gate's
  acceptance agent reasoned past the discrepancy to the right answer by a route
  nobody can check. §I: after a batch applying SEVERAL fixes, re-read the region
  as a reader of the ARTIFACT rather than of the patch — two fixes from one batch
  cancelled each other (a `halted()` guard made a `died` flag unreachable) and
  the next round's blocker was the same shape one level up; both are invisible in
  the diff and immediate in the composed result, and two of that gate's eight
  rounds exist for the mechanism. And SKILL.md's routing table gained a row for a
  TRANSITION rather than an activity: every row read "you are about to X", so an
  operator who had correctly read three of four references reached the end of the
  decision rounds having never been sent to `pre-flight-gate.md`, the only file
  stating the whole sequence, and told their principal that `wf close` — step 4,
  the end of everything — came next while the script was unwritten. Three
  provenance entries, M-38 to M-40, and the checkbox floor moves to 152.
  `make check-docs` also scans SKILL.md for `[M-nn]` citations now: it was
  computing the used set over `references/` alone, so a citation in the front
  door resolved against nothing.

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
- **A change under `skills/`, `hooks/` or `agents/` bumps `version` in
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
  the four newest waves in this file, everything older in `WAVES.md` — where they
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

## State as of 2026-09-01 — read before trusting

- **SIXTEEN gates have run entirely through `wf` and closed, and the newest is
  A4** (`docs/feedbacks/FEEDBACK-a4.md`, `~/projects/gates/mocker-a4-mcp-reach`,
  143 commits): 8 rounds by its own `wf stats`, 70 findings, 20 blockers, 85
  edits over 916 changed lines, 2.8 h, median round 19.5 min, authoring 2.1 min.
  It is the first gate to carry a decision document AND its orchestration script
  in ONE workspace with independent descents — 9 -> 6 -> 0 -> 0 over rounds 1-4,
  then 2 -> 2 -> 1 -> 0 over rounds 5-8 after `wf add` brought the script in —
  and its first script review filed 7 findings and 2 blockers against a corpus
  precedent of 38 and 12, on a script authored after four rounds had pinned the
  method signature, the SQL predicate, the error taxonomy and every Fails-if
  clause. **It LAUNCHED and came back `green: true`** — 13 agents, 1.5 h, 1.83 M
  subagent tokens, no dead agent, no bar failure, the four bars re-run by hand
  afterwards. Its cost meter prices rounds 2, 6 and 7 only: round 1 adds an
  artifact and rounds 3 and 4 were re-frozen over their own repairs, knowingly,
  for cheap textual fixes. Its numbers were taken while it was still growing
  once already — a census over "the eleven workspaces that carry a lens record"
  was taken while this gate went from 8 prompts to 13 inside one hour — so read
  the command, not this bullet. This wave's own reproduction found three of its
  five claims wrong or incomplete, including the flagship.
- **P4a ran before it** (`docs/feedbacks/FEEDBACK-p4a.md`,
  `~/projects/gates/mocker-p4a-triage`, 210 commits): 16 rounds by its own `wf stats`, 97 findings, 30 blockers, 127
  edits over 1195 changed lines, 9.4 h over the 13 review rounds at a median of
  21.5 min, authoring 4.3 min, over THREE artifacts — a decision document gated
  for six rounds, an orchestration script added at round 7 and gated for four
  more, and an evidence-only script derived from the second by DELETION added at
  round 12. It is the first gate with three `-kind run` rounds, at 124.7, 38.3
  and 8.5 min, and each of the first two found what the review rounds before it
  had not; the third is its first clean run round and it cost 110 k subagent
  tokens because `resume` replayed four of five agents from cache. 19 of its
  findings are NOTE rows and 8 of its rounds with findings carry no
  `[fix-induced]` marker, so its 30-of-97 share is a lower bound. Its prediction
  basis was 5.5 per 100 when the gate ran and is **5.2** under the current
  binary, because the twenty-second wave took its run round out of the
  population. Its workspace is on disk and unpruned, and it is what the schema
  keyword check was narrowed against.
- **P3f and nova ran before it** (`docs/feedbacks/FEEDBACK-p3f.md`,
  `~/projects/gates/mocker-p3f-rederive`; and
  `docs/feedbacks/FEEDBACK-nova-budget-panel-batch.md`, whose workspace is NOT
  on this box). P3f is 17 rounds by its own `wf stats`, 128 findings, 58 blockers, 212 edits over 1962 changed
  lines, 9.1 h, median round 28.8 min, authoring 4.6 min, over TWO artifacts —
  a decision document for eight rounds at 11 -> 4 -> 2 -> 5 -> 2 -> 1 -> 1 -> 0
  and the orchestration script for nine more at 5 -> 5 -> 7 -> 3 -> 4 -> 2 ->
  5 -> 1 -> 0, both halves closed on the stop rule. Its SECOND artifact cost
  more than its first — 67 findings and 32 blockers against 61 and 26, more
  rounds and a bumpier descent — on an artifact the first gate's own closure
  implicitly said was ready to be written; that is the magnitude the FIRST
  REVIEW marker does not carry. Its fix-induced share is 37 of 128 findings and
  26 of 58 blockers, with eight rounds MAJORITY self-inflicted and the gate
  still converging. It launched from `gate-r17` while its write-up was being
  authored and the write-up says so rather than guessing; **that section is
  still one sentence long and the run's own numbers are not in this file.** Its
  workspace is on disk, unpruned, and carries `post-gate — 1 apply batch(es) after
  gate-r17, 8 edit(s); in the gate total, reviewed by no round`, which is the
  closing-edit class printed by the tool. nova is the first gate against a
  target other than `mocker` and the first whose launched run was then exercised
  BY A HUMAN in a browser: 4 rounds, 55 findings, 17 blockers, 106 edits over
  790 changed lines, 2.2 h, a run of 22 agents over 2.43 h returning
  `green: false` on exactly one violated acceptance clause — and then three
  defects a person found, TWO of which neither artifact could reach (the third,
  a hex literal cased against its own cited precedent, is the clause the gate
  caught). **Every nova number
  here is quoted from its write-up and is not re-derivable on this box**; its
  workspace lives on another machine, which is itself a first for this corpus.
- **P3h is the gate before those, and its own numbers MOVED again since the
  nineteenth wave banked them** (`close/feedback.md` in
  `~/projects/gates/mocker-p3h-basepath` — the first write-up that is the
  `wf close` scaffold rather than a hand-authored `docs/feedbacks/FEEDBACK-*.md`
  here): **11 rounds, 86 findings, 20 blockers** by its own `wf stats` today,
  against the 10 / 73 / 17 banked one wave ago, because its `-kind run` round 11
  has been filled since (13 findings, 3 blockers) — and 106 commits against 95,
  14.2 h against 5.3, median round 31.5 min against 30.5. 129 edits over 822
  changed lines is unchanged. Over TWO artifacts — a decision document
  gated for three rounds alone and an orchestration script added at round 4,
  both closing together on two consecutive rounds with no blocker. **It
  LAUNCHED, and came back `green: false` on exactly one term** — 30 agents,
  407 min, 2239 tool calls, 5.99 M tokens, 0 dead, 0 bar failures, 0
  inconsistent, `finalBarsGreen` and `goalMet` true, the conditional
  re-measurement fired — and TWO open blockers of one section that the bounded
  fix round did not close, the second of them a recheck refuting the fix's own
  claim that a missing test was "covered incidentally", proved by mutation. The
  run is also the corpus's baseline for the navigation rule: it predates `NAV`,
  so its 30 agents are the last measurement of the old behaviour. `wf stats` says why its fix-induced share (below) is a lower bound rather than
  a measurement: several rounds with findings carry no `[fix-induced]` marker at
  all. Its cost meter
  prices rounds 2-3 and 5-9 — 1 and 4 excluded as artifact-adding windows, 10
  because its tag MOVED over that round's own repairs. It is the gate that
  produced the citation-span and inflection fixes, and its own round 1 filed 2
  blockers. Its workspace is on disk and unpruned. Its fix-induced share reads
  10 of 86 findings and 2 of 20 blockers now, and FIVE of its rounds with
  findings carry no marker at all.
- **P3g ran before those** (`docs/feedbacks/FEEDBACK-p3g.md`,
  `~/projects/gates/mocker-p3g-deep-nesting`) — and its own numbers moved after
  its wave was written, because its run round was filled since:
  14 rounds by its own `wf stats` today, 112 findings, 34 blockers, 86 edits over
  1131 changed lines, 14.9 h, median round 19.6 min, over TWO artifacts — a decision
  document gated for eight rounds and an orchestration script for five, both
  halves closed on the stop rule, and the run launched from `gate-r13` with
  `wfcheck` 0/0, `wfdry` 7/7 and `wf verify` clean, and the run came back GREEN
  on all ten verdict terms — 26 agents, 0 errors, 4 h 27 min, 4.69 M tokens,
  22 files at +2991/-493, every bar re-run by hand afterwards. It is the first
  gate whose script was scaffolded by `wf new-script`, and the skeleton was `wfcheck`-clean
  and `wfdry` 7/7 as emitted and stayed that way — while all THREE harness edits
  its operator made were wrong on the first attempt, each caught by `wfdry` or a
  lens and none by reasoning, which is the manual's frozen-harness rule measured
  rather than argued. Its fix-induced share is 25 of 112 findings and 12 of 34
  blockers (35.3%), triangulating with P3c's 34%, P3d's 35% and P3e's 43.5%. Its
  cost meter prices only rounds 2-8, for TWO reasons and not one: rounds 10-13
  were applied by hand, which `wf apply -whole` now closes, and rounds 1 and 9
  are excluded by the rule that a window ADDING an artifact prices a corpus its
  batches did not write, which no command changes. Its workspace is on disk and unpruned,
  and it is what the `dead-reviews` phase note was validated against.
- **P3e ran before those** (`docs/feedbacks/FEEDBACK-p3e.md`,
  `~/projects/gates/mocker-p3e-nested`): 14
  rounds by its own `wf stats`, 193 findings, 46 blockers, 183 edits over 2535
  changed lines, over TWO artifacts — a decision document, 1323 -> 2091 lines
  over nine rounds, and an orchestration script gated for four more. Its
  decision half descended NON-monotonically (10, 8, 7, 2, 2, 1, **3**, 1, 0) and
  both halves closed on the stop rule. Its fix-induced share is 76 of 193
  findings (39.4%) and 20 of 46 blockers (43.5%), which triangulates with P3c's
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
- **P3d ran before that** (`docs/feedbacks/FEEDBACK-p3d.md`,
  `~/projects/gates/mocker-p3d-datasnap`): **15
  rounds by its own `wf stats` today, 319 findings, 85 blockers**, 446 edits over
  3933 changed lines, median round 32.3 min, over TWO artifacts — a decision document
  that went 752 -> 1481 lines in eight rounds, one undispatched round nobody
  filled, and an orchestration script gated for five more. It is NOT the largest
  gate here: P3b is 588/177 over 22 rounds and P3a 455/143 over 24. The 14
  rounds / 314 findings this file banked for it are the numbers BEFORE its run
  round was filled; the command is what to re-run. Its script
  half closed at 14 -> 7 -> 6 -> 1 -> 0. Its span is **13.6 h**, from `wf stats -time` and from the banked
  `close/stats.txt` alike — the 9.1/9.2 h this file carried for four waves was
  read before the last `wf close` rewrote that file, and the 7.9 h in the
  feedback's own header is an earlier close again. The figure is last commit
  minus first, so it grows whenever the workspace does; the reproducible number
  is the one the command prints today. **It LAUNCHED, after the
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
- **P3c ran before that** (`docs/feedbacks/FEEDBACK-p3c.md`,
  `~/projects/gates/mocker-p3c-ref-recipe`) and is the first
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
- **P3b ran before those** (`docs/feedbacks/FEEDBACK-p3b.md`,
  `~/projects/gates/mocker-p3b-resources`) and is the first that
  **stopped instead of closing**: 22 rounds by its own `wf stats`, 588 findings,
  177 blockers, 451 edits over 8388 changed lines, 25.7 h, median round 29.4 min.
  Its document half closed on the rule at 19 -> 0 over seventeen rounds; its
  script half ran four and was halted by the operator. It did NOT launch. The
  workspace is on disk and unpruned, and it is now the calibration corpus for the
  blocker-origin split and for the closure check that was REFUSED on it.
- **P3a ran before those** (`docs/feedbacks/FEEDBACK-p3a.md`,
  `~/projects/gates/mocker-p3a-resources`) and is the first
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
- **mocker A3 sits between those and the earliest four** (`docs/feedbacks/FEEDBACK-a3.md`,
  `~/projects/gates/mocker-a3-cas`): 13 rounds, 268 findings, 114 blockers, 151
  fix-induced, 19.1 h, median round 45.3 min — the highest per-round median in
  the corpus — and its round 13 CONTAINS the launch, which is why `-kind run`
  exists. It has been in the workspace census and in the wave index (waves 8-10)
  since those waves and had no bullet of its own here, which is why the gate
  count and the list of named gates disagreed by one.
- **The four earliest gates ran before all of those.** P2d (13 rounds by `wf stats`, of which rounds 11-13 filed nothing — the
  write-up's "10 rounds" is the reviewed prefix; 98
  findings — `docs/feedbacks/FEEDBACK-p2d.md`), P2e, the first over a workflow SCRIPT
  (17 rounds, 234 findings — `docs/feedbacks/FEEDBACK-p2e.md` says 235, and its own `wf stats` says 234; the
  hand-count is the one that is wrong — 293 KB of artifacts), and P2f,
  the first carrying both gates in one workspace (13 rounds, 111 findings, 180
  KB over three artifacts — `docs/feedbacks/FEEDBACK-p2f.md`; its implementing run then
  executed 20 agents over 149 minutes and came back green). P2e
  confirmed live: the anchor check turning a wrong citation into a patch, the
  ratchet firing on a real regression, record rejecting a placeholder sha,
  the fix-induced share measured at all, delta briefs at ~1/10 of a re-read. P2f
  added the first live yield from `check rules` — marked before the gate closed,
  it found a divergence across the derived pair on its FIRST run, which is the
  case the feature was built for — plus eight mis-aimed anchors and three
  citations that resolved, sat inside the file and pointed at the wrong lines.
  And mocker slice A (`docs/feedbacks/FEEDBACK-a-mcp.md`, `~/projects/gates/mocker-a-mcp`): 25
  rounds, 196 findings, 58 blockers, THREE gates in one workspace — a decision
  gate and one script gate per shipped slice — 262 KB over four artifacts, both
  slices shipped with every bar green. It is the first run whose report is
  mostly about the OPERATOR: four ways of drifting out of the tool that the tool
  watched in silence, which is what the seventh wave (`WAVES.md`) answers.
  `check drift` is STILL unexercised live.
- **The validation corpus DOES survive, and it is the best evidence this
  repository has.** The claim that it did not was stale and cost an
  investigation an hour of archaeology before it was checked: **FOURTEEN** gate
  workspaces are on disk with full git history — `~/gates/p2d`, `~/gates/p2e`,
  `~/gates/p2f` (82 / 102 / 87 commits), and
  `~/projects/gates/mocker-a3-cas`, `mocker-p3a-resources`,
  `mocker-p3b-resources`, `mocker-p3c-ref-recipe`, `mocker-p3d-datasnap`,
  `mocker-p3e-nested`, `mocker-p3f-rederive`, `mocker-p3g-deep-nesting`,
  `mocker-p3h-basepath`, `mocker-p4a-triage`, `mocker-a4-mcp-reach`
  (153 / 203 / 247 / 82 / 173 / 131 / 183 / 123 / 106 / 210 / 143 commits, all counts
  re-derived 2026-09-01 with `git rev-list --count HEAD` — the last stood at 101
  one wave ago, while that gate was still running, so a count taken then is a
  moment and not a total — 128 stood here for one
  wave and was the count BEFORE P3e's run round was filled, inherited by the next
  wave from the bullet whose own next sentence says not to; 117 for P3g lasted
  exactly one wave the same way; and P3h's 95 lasted exactly one wave for the
  third time, its run round filled since). **nova is the first gate whose
  workspace is NOT here at all** — it ran on another machine, so its numbers are
  readable only in its write-up and the count of gates has now come apart from
  the count of workspaces in a second way. This
  bullet listed three of them for three waves, which is why a census that called
  itself complete missed A3 entirely — and it then listed SIX for one more wave,
  which is why the fifteenth wave's own closure census said "six workspaces"
  while running over seven. Re-count them; do not inherit the number. `~/projects/gates/mocker-a-mcp` has been CLOSED AND PRUNED: no `.git`, no
  `artifacts/`, no `rounds/` — its numbers survive only in `close/stats.txt`,
  `close/history.tsv`, `close/freezes.tsv` and `close/rounds/`, and `wf stats`
  cannot be run there. Read the banked file; do not try to re-derive them. `~/projects/gates/mocker-a-mcp` is the only PRUNED gate, so
  the count of workspaces and the count of gates are two different numbers — in
  two directions now: one gate here was pruned, and one gate never had a
  workspace on this box. The six tracked orchestration scripts are in
  `~/projects/mocker/.claude/workflows/` (769-2658 lines each, 10 823 total; a
  live gate's own script sits there untracked while it runs). Run `wf stats
  -time` inside a workspace before reasoning about cost; the commit timestamps
  are what price a round, and nothing else records them. To rebuild a lab from
  scratch: `git clone --no-hardlinks ~/projects/mocker <lab>/head`, then a
  workspace over any set of documents.
- **The cost of a gate is now measured by the tool, and the answer moved the
  work.** `wf stats -time`, re-pooled 2026-09-01 over the thirteen unpruned
  workspaces with the current binary, over the 179 round spans of 199 that are
  under two hours: **median 24.2 min, middle half 17.6-32.2** (quartiles by the
  rank `int(n*0.25)+1`). The review segment is a median 89.9% of a round and the
  bookkeeping tail 3.3%, over the 164 of those spans that carry a
  `(review, fix, tail)` breakdown at all — a different population from the 179,
  and named because this file's own rule is to name it; 179 spans carry a
  breakdown in total, fifteen of them over two hours, so the two 179s are two
  different sets and not one. The other
  twenty spans are breaks (the longest 637.9 min) and none is a round
  length. State the READING with the number, because two of them disagree by a
  round: this one is every `  round` line carrying a `; N min` span, run rounds
  included, piped through `grep -oE '; [0-9]+\.[0-9] min'` and `sort -n`, where
  the eleven-workspace pooling one day earlier gave 148 of 165, a
  ten-workspace one 138 of 154 and a nine-workspace one 119 of 129; the
  twelve-workspace pooling one day before this one gave 165 of 183 at a median of
  24.3 and a middle half of 17.8-32.3. Re-derive it with the command and say which
  population the number came from.
  The old figures — "10-40 minutes, pooled median 15.5, review 70-87%" — were
  three gates and are not wrong so much as SMALL: per-gate medians run 13.8,
  24.3, 19.1, 45.3, 21.5, 29.4, 39.0, 32.3, 21.4, 28.8, 19.6, 31.5, 21.5, so the round
  got longer as the gates got bigger, and a number banked from the first three
  under-prices every gate since. Read them off `wf stats -time`, one line per
  workspace, rather than re-deriving them: a hand pooling of the same spans moved
  five of these by up to 0.8 and was wrong, while values DO move as a workspace
  grows: three moved against a reading two waves ago (13.8 against 14.2, 19.1
  against 19.5, 45.3 against 37.6) and P3h's moved again this wave, 30.5 to 31.5,
  when its run round was filled.
  So the ROUND COUNT is the cost function, the ~9-command sequence is not worth
  compressing (1.4 minutes across eleven of p2d's thirteen rounds), and the largest measured
  saving in the whole investigation is a DOCUMENT change already in the manual:
  running the decision gate to exhaustion before the script exists took one gate
  from 10+6 rounds to 8+3, about an hour. The authoring window — init to the
  first artifact — is **0.3 to 76.5 min (9.9, 31.7, 42.2, 4.3, 5.0, 39.2, 2.2,
  20.9, 76.5, 4.6, 0.3, 10.7, 4.3; median 9.9 over thirteen — a single middle
  value now, where twelve gave the 10.3 mean of two)**, and this file quoted only its two largest for
  three waves before quoting a ceiling P3e then doubled and a floor P3g then cut
  by seven times. It is the largest block no
  round covers, and the short end is worse than it looks: those are the gates
  whose document was drafted BEFORE `wf init`, so the authoring itself falls
  outside every measurement banked here. `interview.md`
  (step -2) is what now occupies that window, against a round-1 blocker baseline
  of 7, 15, 7, 13, 15, 19, 15, 13, 10, 11, 4, 2, 5 (one per unpruned workspace, in
  the order they are listed above; nova's round 1 filed 3, and its own write-up
  banks that as one data point AGAINST a large saving in blocker count from the
  interview and a clear one FOR what the step is actually for — not one of that
  round's fifteen findings was "this decision was never taken").
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
