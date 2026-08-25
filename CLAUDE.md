# wfgate — agent context

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

Eleven units, each one decision.

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
  (authoring, model-routing, pre-flight-gate, section-gate, cost-discipline,
  gate-checks). The split preserved the gated text verbatim — cutting at
  headings is packaging, not rewriting, so no re-gate was owed. `embed.go` at
  the module root embeds the references into the binary; `wf docs guide`
  CONCATENATES them at runtime rather than storing a monolith — a second copy
  of a spec is this project's most-repeated defect class.

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

**The second wave, all from P2d live-run measurements** (`FEEDBACK-p2d.md`;
10 rounds, 98 findings, the whole gate run through `wf`). Each item is a
measured failure, not taste:

- *The extensionless-reference counter in `cite`* — a correct
  `Makefile:90#corepack` sat invisible in a green report of 237 citations; now
  it prints as "NOT verified".
- *`check rules` and `[RN]` markers* — eight contradictions between copies of
  one rule in different words over the slice, all found by reviewers, zero by
  `dup`: no similarity metric reaches that class, only explicit markup does.
  Zero markers prints as a population: a corpus nobody marked did not "pass".
- *`dup -since <tag>`* — the derived `context.md` yields 21–25 groups per one
  real contradiction; the OUTPUT is narrowed, never the threshold, and the
  hidden count is printed. (`-threshold` and `-min-words` exist as calibration
  flags, documented in the README; the shipped behaviour narrows the output, and
  a gate that moves the threshold is doing something it must say out loud.)
- *The anchor ratchet in `freeze.json`* — a direction, not a threshold: a share
  that fell against the previous freeze prints a note, because an author who
  stopped anchoring writes citations the gate cannot really verify.
- *`round new -kind full|delta`* — 7 of 10 rounds were deltas, the scope lived
  in prose titles, and twice a round reviewed less than a reader would assume.
- *`wf stats`* — a hand awk closed the gate on 99/27 against a true 98/26 by
  counting the scaffold placeholder as a finding; the ledger parser knows
  better.
- *`wf add` adopts an untracked draft already in place* — the refusal forced a
  hand `git commit` three times over the slice; it now guards only a tracked
  (reviewed) artifact and two drafts with differing bytes.
- *`verify` separates bookkeeping dirt* — "Do not launch" sounded identical for
  diverged artifacts and for a forgotten `rounds/r7/delta.txt`; when every
  mismatch is uncommitted bookkeeping, a note says "commit it, do not
  re-freeze".
- *Lowercasing in `Shas`* — `2344D3D8` silently rode off into "verify by hand".

**The third wave — the residue P2d named but the queue missed:**

- *The plain-text batch format for `apply`* (`apply.ParseText`) — assembling
  the JSON was the run's actual bottleneck: a 35-edit batch with multi-line
  `old` strings under three layers of quoting failed on escaping more often
  than on substance. Between `<<<<`/`====`/`>>>>` nothing is ever escaped; a
  marker collision mis-splits and then fails the plan LOUDLY, because the
  mis-split old matches nothing — all-or-nothing converts the format's one
  weakness into a rejection instead of a silent wrong edit.
- *`delta -save`* — the stray `rounds/r7/delta.txt` that taught verify to name
  bookkeeping dirt was the operator hand-saving the brief. If the brief is
  wanted as a file, it IS round bookkeeping: the tool writes
  `rounds/r<open>/brief.md` and commits it, killing the class at its source.
  An unchanged re-save is reported, not re-committed.
- *`[fix-induced]` in `wf stats`* — the share of findings introduced by the
  previous round's own fix is a CONTINUE signal, not a stop signal — three
  gates produced a round that was wholly self-inflicted and still carried, or
  was followed by, a blocker, and the ledger can carry it only where the author marks it. Stats
  prints "not measured" when nobody marked: an unmarked corpus and "no
  self-inflicted findings" are different claims, and only the second belongs
  to the finding's author.

**The fourth wave, from P2e — the first script gate** (`FEEDBACK-p2e.md`;
17 rounds, 234 findings by its own `wf stats`, 293 KB of artifacts):

- *`dup` unwraps hard-wrapped paragraphs before sentence-splitting* — the
  line-bounded splitter turned two derived artifacts wrapping the same
  sentences at different columns into 92 divergent groups with ZERO real ones
  (36 provable prefix-pairs). A sentence is compared as a sentence. The price:
  a sentence is attributed to the line it starts on, so blame reads that line.
- *`NUMERIC` conflicts in `dup`* — four of P2e's six real contradictions
  differed only by a count ("ONE comment" vs "two comments"); masking numeric
  tokens and demanding the masked forms match exactly catches that class with
  a population bounded by how often two artifacts legitimately state different
  counts of one thing (measured: never). The exact-mask boundary is
  deliberate: a plural drifting with the count breaks it, and loosening the
  mask would trade a zero-noise check for a noisy one.
- *`-kind tool`* — P2e recorded two reviewer-less rounds (tooling findings, a
  launch death) as ordinary rounds, and the stop rule read 15→13→4→1→1 where
  the reviewer descent was 15→13→4→1. Stats derives each round's kind from its
  opening commit subject (record overwrites the scaffold that carries the kind
  line; the subject is the copy nothing rewrites) and counts tool rounds
  apart.
- *Dry-run before freeze* — a guide-order inversion, not code: a dry-run
  defect found after freezing invalidates a freeze that correctly refuses to
  re-freeze, costing a round no reviewer ever saw (P2e r12). The freeze pins
  what reviewers read; it comes after the set executes clean.
- *The parser names the one-header-many-blocks trap* — the first shape a live
  operator reached for; the error now states that `count=` is scoped to one
  replacement.
- *The batch-size cost meter* — P2e measured ~0.25 findings per applied edit,
  stable across a 12x range of batch sizes; a 60-edit batch bought four extra
  rounds. `wf stats` derives each round's edits from a `wf-apply:` trailer in
  every apply commit (the subject carried it until P2f, and `-m` replaces the
  subject) and prints findings/edit per round and across the gate —
  the workspace's OWN number, the only form in which it is believed. `wf apply`
  prints a note (never a refusal) when a batch's size, against that history,
  predicts more than a handful of next-round findings; no history, no claim.
  An automatic split is deliberately absent: which edits belong together is
  judgement — one rule and all its copies must land or fail as a unit.

**The fifth wave — the five-reviewer audit of this repository** (2026-08-22;
three Opus agents on three vectors plus two vcodex passes, 73 raw findings, 52
after dedup). It was the first review of the gate harness BY the method the
harness exists to run, and it found the tool committing the class it was built
to catch:

- *`freeze` wrote and committed the manifest BEFORE refusing to re-freeze* — so
  a refused re-freeze rewrote `rounds/rN/freeze.json` with hashes of the current
  tree, and `wf verify`, which reads that file from the worktree, turned
  MISMATCH into "clean". The command that said no had already blessed what it
  refused. The refusal comes first now.
- *`freeze`'s open-blocker check passed vacuously and silently* — a findings.md
  that could not be read was skipped by `continue`, and a table that did not
  parse yielded zero rows and zero objections. `record` printed "no table
  recognized" for the same input; `freeze`, which IS the enforcement, printed
  its success block.
- *One pipe character carried an open BLOCKER past a freeze* — the row splitter
  cut on every `|`, so a finding quoting an escaped `\|` shifted its own text
  into the closed-by cell and scored as "closed by prose". Cells split on
  unescaped pipes; a row that still is not four cells is `Malformed` and
  refused by name, never skipped.
- *A deletion was verified by `strings.Count(body, "")`* — which is `len+1`, so
  every pure-deletion edit was verified vacuously; and buffers keyed by the path
  STRING made `artifacts/a.md` and `./artifacts/a.md` two files, both planned
  from the pristine bytes. `Verify` now compares each written file byte-for-byte
  with the planned buffer, which also stops it rejecting a chained batch that
  landed perfectly.
- *`dup` lost recall as a rule SPREAD* — a statement in an exact cross-file
  group was excluded from the near pass in both roles, so two verbatim copies
  plus a contradicting third reported nothing, while deleting one verbatim copy
  made the same corpus report it. The three-artifact shape is the one the check
  exists for.
- *`dup` and `rules` reported a contradiction as agreement* — the normaliser
  keeps only letters and digits, so `retry_count == 3` and `retry_count != 3`
  normalised identically: `rules` said 0 conflicts and `dup` said `sim 1.00`, an
  exact match. Statements now carry the operator skeleton, and the pair is an
  `OPERATOR` conflict.
- *A deleted line beginning `-- ` was read as a diff file header* — one SQL
  comment made `drift` file every later hunk under a phantom path and call an
  invalidated citation clean.
- *`wf add -as` wrote into the target* — the one tree no command may touch.
- *HEAD is not the whole tree* — a cited line rewritten without a commit left
  `verify` printing "clean" and `drift` printing "nothing to re-read". Both now
  separate a modified tracked file (refusal) from an untracked one (note).
- *The text scanner was the defect, not its checks* — `wfcheck` was defeated by
  an unrecognised wrapper plus an options factory ("0 call sites, 0 errors, exit
  0" on a script with five), by `effort:` inside a schema description, by a
  spread options object (four phantom ERRORs on a correct call), by an inline
  schema, by a multiline `phase()`, and by a comment quoting a footgun. All of
  those stopped existing when it started reading the parsed program.
- *`wfdry` passed 6/6 over runs that executed nothing* — a script whose only
  spawn sat behind a false flag, a body gated on an `args` it was never given,
  and an invented `--mode` that ran and reported "1/1 modes completed". A mode
  that spawns nothing now FAILS, `args` is supplied and withheld as two modes,
  and an unknown mode is refused.
- *8 of 9 `wfdry` guards had no test* — including the single `if
  (violations.length)` that turns a recorded defect into a failing mode. Every
  guard in both tools now has a case that fails without it.

**The sixth wave — P2f, the first run carrying a decision gate AND a script
gate to exhaustion in ONE workspace** (`FEEDBACK-p2f.md`; 13 rounds, 111
findings, 28 blockers, three artifacts at 180 KB, 27 apply batches over 150
edits). Every item below was measured by a command, and two of the four tool
defects were the tool reporting a population of ZERO and saying nothing about
it:

- *`wfcheck`'s inventory backstop fired on EVERY script written to the manual's
  own rule* — the manual REQUIRES a `spawn` wrapper, and `async function
  spawn(prompt, opts)` matches `spawn(`, so the textual population was always
  exactly one larger than the structural one. Six scripts, six unclearable
  ERRORs; five workflows launched over a standing one. The forwarding
  `agent(...)` was already subtracted — it was the DECLARATION that counted. The
  same population also counted a prose comment and a log string, so it now reads
  CODE, with comments and string literals blanked (`internal/wfcheck/text.go`).
  An ERROR nobody can clear is an ERROR nobody reads, and the day it means
  something it reads as the usual noise.
- *`wf stats` read ZERO apply batches over 27* — the meter parsed the DEFAULT
  commit subject, and `-m` replaces the subject; P2f wrote one for every batch.
  The headline planning number the tool exists to produce had no population at
  all, and `wf apply`'s split note never fired once for want of history. The
  count now lives in a `wf-apply:` trailer, which nothing overwrites; the
  subject form is still read so older workspaces still measure.
- *`wfdry`'s fixture decided which branch ran* — `done` is the completion flag of
  every schema in the project, matched neither boolean list, and was therefore
  FALSE in all seven modes including `clean`. The happy path was executed by no
  mode, and the defect living there survived seven green runs: a repair agent
  spawned only when acceptance FAILS was counted in the liveness ledger, so the
  run everyone wants logged `dead: repair` and returned `green: false`. Three
  fixes, and the third is the general one: the completion synonyms joined the
  pass list; the runner's own notes left the script's `log()` stream for a WARN
  that prints under `--quiet` and again after the tally; and a `clean` mode that
  returns a not-green verdict — or a failure flag set — now FAILS, because no
  fixture in that mode can produce either. `testdata/false-green.mjs` is that
  script, banked. It is P2e's finding 8 in a new costume, and the same sentence
  covers both: the runner's fixture, not the script, decided what was exercised.
- *`wf check rules` reported five identical-looking lines* — two sites of one
  rule were written `**[R1] … alone.**` and three plainly. The normaliser erases
  `*`; what broke was the SENTENCE splitter, because the closing `**` sat
  between the full stop and the space, so the statement never ended and swallowed
  the paragraph after it. It cost two cycles, and the first fix — breaking the
  line — changed nothing, because unwrapping rejoins a wrap. Terminators now
  tolerate closing markup. The actionable half was the REPORT: every conflict
  (`rules`, and dup's `NUMERIC`/`OPERATOR`/near-match) prints at the point the
  copies stop agreeing, with the shared opening trimmed to a few anchor words —
  the difference had to be found with `cat -A`.
- *`wfcheck`'s phase matcher and its ownership check* — "meta.phases entries with
  no phase() call" over four gates the script does call, because the title came
  through a helper's parameter: the message now says "no LITERAL phase() call"
  and names the computed count. The `YOU OWN` format the ownership check parses
  was documented nowhere but a test; it is in `authoring.md` now, and adopting it
  took a live script from `0 owned path(s) parsed from 4 prompt(s)` to 12 from 8,
  finding two pairs of sections writing into one Go package.
- *Three findings changed the MANUAL, not the tool* — the batch-size cost meter
  is measuring the wrong variable (P2e's ~0.25 "stable across a 12x range" came
  out 0.15–1.75 on P2f, with the four-edit batch the most expensive of the run
  and the thirteen-edit batch the cheapest): what predicts the cost is whether a
  fix changed a RULE, whose consumers are not in the diff. Also written down:
  run the decision gate to exhaustion BEFORE the script exists, and the sharper
  form of why both gates exist — the decision gate asks whether the design is
  right, the script gate asks whether the repository will let you build it. The
  second found a hand-pinned total in a test file that eight decision rounds
  missed.

**The seventh wave — mocker slice A, the first gate carrying THREE gates in one
workspace and shipping two slices of the repository under review**
(`FEEDBACK-a-mcp.md`; 25 rounds, 196 findings, 58 blockers, four artifacts at
262 KB, 139 workspace commits, ~6.5 h of real gate work). Its report is in two
halves and the first is the more useful one: four ways the OPERATOR stopped
using the tool correctly partway through, none of which the tool objected to.
That is the shape of every item below — the discipline held and only the
MEASUREMENT broke, silently:

- *The operator stopped using `wf apply` at round 18 and found out at close* —
  seven rounds and 39 findings absent from the cost meter, printed per round as
  a bare "0 apply batch(es)". The whole-gate note fires only when the meter has
  NO population at all, so a gate that used `wf apply` for seventeen rounds and
  stopped has a population, a headline ratio and seven rounds the ratio never
  saw. `wf freeze` now names a round whose artifacts changed without `wf apply`
  — one round old rather than seven — and `wf stats` names those rounds BY
  NUMBER. Same for the `[fix-induced]` marker: the rounds carrying none are
  named, because a share computed over the marked rounds is a lower bound, and
  its error has a direction — the true share was nearer 38% than the 34%
  printed, with every blocker of five consecutive rounds induced by the round
  before.
- *`wf freeze` counted anchors and never asked whether a citation still
  RESOLVED* — the gate shipped its own first slice mid-run, the target moved
  under a document frozen ten rounds earlier, and 26 citations drifted by a few
  lines each and were read as correct by every producer of the next artifact.
  Freeze resolves them now and names every one that does not, as a note: a round
  whose purpose is re-aiming them must still freeze.
- *`wfcheck`'s prompt resolver charged its depth budget PER NODE* — and `a + b +
  c + ...` is left-associative, so a prompt assembled from more than a dozen
  concatenated pieces lost its leftmost ones. A schema field named in the first
  chunk was reported as never mentioned after an unrelated insert made the chain
  longer, and every size the tool printed over such a prompt was a size budget
  read off fiction. Depth is charged per INDIRECTION now, the accounting `objectOf`
  already used. The check that asks whether a field is ABSENT is skipped
  entirely for a prompt composed at run time — the polarity rule the gate briefs
  its reviewers with, applied to the checker itself — and every size over such a
  prompt prints as a lower bound. Writing the test for it found a second defect:
  `const M = { a: M.b, b: M.a }` is legal JavaScript and crashed `wfcheck` with a
  stack overflow, because `memberOf` re-entered `ObjectOf` with a fresh budget
  (`wfdry` survives it and reports 0/7 modes). A script that kills the checker is
  a script nobody checked.
- *`wfcheck -root` defaulted to the working directory* — twenty-two "owns X,
  which does not exist" WARNs on a correct script run from the workspace, whose
  `gate.json` already records the target. It reads that now and says which tree
  it read.
- *The pre-flight sequence reads as a gate and is a smoke test* — both round-19
  blockers passed `wfcheck` 0/0 and `wfdry` 7/7, because both are properties of
  PROMPT TEXT and OWNERSHIP LISTS rather than of control flow. `wfcheck` now
  cross-references every `YOU OWN` path against the `owns` array of every gate
  config: a path no gate owns is a file a reviewer can be briefed on and no fix
  agent may touch. It reaches ONE of that gate's three blockers of this shape —
  round 20's, where a producer declared the path and no gate listed it. The other
  two were files named in NO `YOU OWN` block and no `owns` array at all, and a
  check that enumerates declared paths cannot see a file nobody declared: which
  files a phase was ORDERED to touch is judgement, and the manual says so rather
  than letting the sequence read as covering the class. The reach is script-WIDE — phase was the first join tried and it
  reported every correct script, because in the prescribed shape a producer runs
  in `Build` and its gate in `Gate 1`.
- *Two report changes for things the tool already said and nobody read* — `check
  rules` prints `NOT A PASS` in the HEADER when nothing was compared (eighteen
  markers, eighteen ids, zero comparisons: the note said so under a line whose
  other three numbers read like a pass — and the condition is `compared == 0`,
  not `markers > 0 && compared == 0`, because an UNMARKED corpus compares nothing
  either and is the far larger population), and `wf stats` names EVERY run of
  three or more rounds at exactly ONE blocker, with the file they all cite when
  they cite one. Keeping only the longest kept the FIRST longest, and that gate
  has three such runs: it would have named rounds 5-7 of the decision gate and
  attached the script gate's story to them. The measured run is rounds 22-24 —
  three rounds, one enumeration floor.
- *Four findings changed the MANUAL, not the tool* — the polarity rule's
  corollary (**a floor must be armed on a DIFFERENT field than the one it
  constrains**, or it disables itself at exactly the degenerate value the defect
  produces; four instances in ONE script over three rounds — r19, r21, r22 twice), the kind-of-question rule (**a guard
  over a report asks "is this what the document NAMES", never "does this look
  like enough"**), the barrier's sharper form (the pull to patch is strongest
  when one vector returns early and its findings look obviously correct), and
  **starting a script from a gated one copies its UNFOUND defects** — re-read
  that gate's ROUNDS FILE, not its code: a reviewer guard testing
  `findings.length` instead of "any finding of severity blocker" survived its own
  seven-round gate and was copied forward along with the first blocker it had
  already cost.

**The eighth wave — mocker A3, the first DECISION gate to file a queue while
still open** (`IDEAS.md`, "Queue after mocker A3"; 5 rounds, 127 findings, 67
blockers, 60 of 127 `[fix-induced]`, the artifact 332 -> ~1050 lines). Every
item was measured by a command in that workspace, and three of the four dearest
classes were things `wf` already held the data for and did not ask:

- *`wf check stale -since <ref>`* — the sweep `wf apply` had been ADVISING since
  the cost meter existed ("if this batch changed a RULE, its consumers are NOT
  in this diff") and no command could run. It is possible only here: the apply
  range is the one artifact in the pipeline that records the exact text each fix
  replaced. It keeps the tokens the fix RETIRED — removed and not put back, and
  not merely inflected (`expect` -> `expectation` retires nothing; five of
  eleven "retired" tokens on the live range were that) — drops the ones the
  corpus uses everywhere, and reports the untouched sentences still carrying
  them. `dup` finds copies that DRIFTED, `rules` finds marked sites that
  disagree; neither reaches a paragraph arguing from a superseded rule in other
  words, which is what round 1's four standing consumers and round 2's five
  were. `-max-df` was calibrated ON that corpus, not chosen: at 8 the report was
  26 hits carrying "were", "right", "stays" — English, not vocabulary — and at 3
  it is 8 hits over 5 retired tokens. A range that retired nothing prints NOT A
  PASS, because an addition-only fix is invisible to this check.
- *`wf check closure`* — `record` proves a `closed by` sha EXISTS; this asks
  whether its diff reaches what the finding NAMES. Two reviewer lenses asked it
  by hand-written prompt for three rounds and reported 6 and 4, 5 and 4, 6 and 4
  of 25, 23 and 21 findings — the most valuable half of those rounds, produced
  entirely by prose over data git already held. The first implementation
  resolved PATHS and reached ONE finding of 127, because a ledger cites
  SECTIONS (`D9`, `R12`) and 126 of those closures named no path at all. It
  resolves the section now, against the UNION of the closing commits and on ANY
  cited section: per-commit-and-every-section flagged 89 of 127, and `dup`'s
  lesson (161 groups of 176) applied before shipping rather than after leaves
  12, of which the first three were hand-verified as fixes that landed in the
  section PRODUCING the defect. So a FILE miss is a verdict and a SECTION miss
  is a question, and the report says which half ran.
- *The cost meter was calibrated on the wrong regime, and said so in the
  register of a measurement* — round 1's window IS the authoring commit (34
  findings against 9 attributable lines), so it read 377.8 per 100 and `wf
  apply` predicted ~355 findings for a round that produced 24. Such a round now
  prints NO ratio and is excluded from the prediction basis, by name. The
  sharper half: the ratio ROSE across the gate (5.4, 7.1, 8.7, 10.3) while the
  fixes SHRANK (447, 354, 264, 204 lines), so the pooled figure predicted 5-6
  against actuals of 21-25 — while a script gate measured it FLAT across a 12x
  range. Both are true of different regimes, and a pooled number cannot say
  which one it came from. `wf apply` prints the SERIES and names a monotone one;
  `wf stats` prints the prediction basis beside the gate total, because the two
  populations now legitimately differ and the previous wave's lesson was that
  they must never differ SILENTLY.
- *`wf check rules` prints WHICH question its four numbers answer* — `10
  marker(s), 5 id(s), 5 compared, 0 conflict(s)` was true about copy agreement
  and read as a statement about correctness, while both copies of `[R5]` agreed
  perfectly and both had gone FALSE against the four bullets arguing for them:
  the fix rewrote the argument, not the rule. The scope line names the class and
  points at `stale`.
- *`freeze` records the corpus SIZE, with its delta* — 332 to ~1050 lines while
  blockers ran 13, 17, 10, 16, 11. The blocker count alone reads as progress; a
  corpus that doubles under it is a design being ELABORATED, and nothing else in
  the workspace showed it.
- *And the wave earned its review round, for the third time running* — one
  external model over the diff, four bars green, 14 findings, 8 of them real
  bugs. The two worst were the tool's own named classes turned on it: `wf check
  closure -round 999` printed `0 closure(s), 0 checked, 0 not closed` and exited
  ZERO over a round that does not exist, and `stale` printed `NOT A PASS,
  nothing was swept` over a COMPLETE sweep, because a range that mined nothing
  and one whose every retired word is gone from the corpus both leave the
  reported vocabulary empty. The rest came from one root: `parseUnified` keyed
  only on the OLD path, so every file a diff ADDED was dropped — its words never
  entered the "added" set (a fix that MOVED a rule retired its whole
  vocabulary), and `check closure` reported the commit that CREATED an artifact
  as not having touched it. Also: ranges from several closing commits were
  unioned across DIFFERENT parents' coordinate systems (fixed, and it changed a
  verdict on the live gate: 12 became 11); `len(bad)` counted failed assertions
  rather than closures, so the headline could exceed the claims it was drawn
  from; the FILE-only population was counted per path and even where a line HAD
  been checked; an ambiguous basename silently resolved to the first artifact
  (the class `check cite` reports); headings inside fenced code headed phantom
  sections and a repeated id overwrote a real range; C-quoted paths matched
  nothing. One of the review's own suggestions was refused after measurement:
  `roundWindow.Authoring` reads EVERY artifact-addition commit deliberately — a
  round adding a new document prices a corpus its batches did not write exactly
  as round 1 does — so the COMMENT was wrong, not the code, which is this
  project's most frequent defect wearing the reviewer's clothes. And one guard
  written for the fix survived every mutation and was DELETED rather than
  shipped unproven.
- *Two items were NOT built, deliberately* — a review rung below the round
  (`wf apply -brief`) and a canned reviewer vector (`wf brief`), both with the
  deciding measurement written down in `IDEAS.md`. The second has a sharpened
  form from that gate worth more than the command: a criterion vector must
  REQUIRE, per clause, either a named defeating implementation or an explicit "I
  could not construct one". **The 1-vs-8 number this once carried is withdrawn**
  (2026-08-24): it compared round 1 with round 4 across a 6.4x growth in the
  section, a different model and a different round position. Held fixed, the
  strict brief LOWERED the yield (12, 14 loose -> 8 strict) while raising anchor
  density. The canned vector is now on the do-not-build list for a further
  reason measured on the A3 gate: a vector returning 12-18% of whatever surface
  it is handed can never return zero against a nonempty acceptance criterion,
  and each defeat is closed by adding a clause — it is a loop generator. The
  rule that survives is the AUTHOR-side one already shipped at
  `gate-checks.md:233`, which was never applied on that gate: 352 lines of
  acceptance section carried 2 lines naming a defeat.

**The ninth wave — the A3 loop, and the four-part answer that was measured and
NOT built** (`IDEAS.md`, "A3 after eight reviewed rounds"). Eight reviewed
rounds, 194 findings, 92 blockers, no descent; the artifact grew at every freeze
(333 -> 1319 lines) and ONE section — `D13. Acceptance` — went 23 -> 352 lines,
absorbed a third of all growth and was rewritten in every round. (What SHARE of
the 92 blockers were its is deliberately not quoted: five derivations of that
one quantity exist, 25 to 66, and the operator acted on the highest.) A human
then cut that section to 92 lines of properties — and round 9, which reviewed
the cut, returned 4 blockers and 20 findings, 17 of them defects IN the cut,
two of them design decisions that survived nowhere afterwards. So the cut moved
the loop rather than ending it, and this entry must not be read as saying
otherwise. The
obvious answer was a per-section loop meter with a verdict, a fourth closure
kind, a second stop clause and a canned criterion vector; all four were rejected
on measurements over five gate corpora, and the reasons are banked in `IDEAS.md`
because the do-not-build list is the most reusable thing that file holds. The
decisive datum was a counterfactual already on disk: the decision gate BEFORE
it, same operator and same target, has an acceptance section too — its blockers
ran 4, 2, 2 and then zero for four rounds, it was never cut, and its gate closed
on a genuine zero. The difference is FORM, not diligence: D12's clauses point at
things that execute or count, D13's specified tests in prose, and a clause whose
only reader is a reviewer imagining a cheat has no floor. What shipped:

- *One manual rule, zero lines of Go* — **an acceptance section states
  OBSERVATIONS, never test designs**, in `gate-checks.md` beside the two
  criterion rules it completes, with both gates' numbers. The rule that was
  supposed to prevent this ALREADY SHIPPED (`gate-checks.md`: each criterion
  names the wrong implementation it exists to fail) and had never been applied:
  352 lines of acceptance section carried 2 lines naming a defeat. So the same
  bullet now says the rule is AUTHOR-side and why moving it onto a reviewer was
  rejected — across three rounds of one gate that vector named a defeat on
  12-18% of the clauses it was handed (8/64, 9/51, 12/69) and never returned
  zero, each defeat was closed by ADDING a clause, and its other 82-88% was
  "none constructed", which nothing separates from "did not try". The mechanism
  generalises; the percentage does not, and the manual says which is which.
- *A round nobody read must not print what a round that found nothing prints* —
  the live hole, and it stopped being hypothetical the same day: that gate's
  round 9 was opened, tagged `gate-r9` and left undispatched, and `wf stats`
  printed `round 9 — 0 finding(s), 0 blocker(s)`, which under the stop rule is a
  legal close. It was then dispatched and returned FOUR blockers. `ws.ScaffoldOnly` names the shape
  the existing `noLedger` guard sat one case away from; `wf stats` marks the
  round line and names the rounds, `wf freeze` refuses to publish the round it
  CLOSES in that state, and `wf close` refuses to bank it as the round that
  ended the gate. A `-kind tool` round is exempt — its opening subject already
  records that no reviewer was dispatched — and the open round is exempt in
  freeze, because in the prescribed flow its scaffold is blank by construction.
  Run over the three closed gates on disk it names four rounds — p2d 11-13 and
  p2e 17 — every one a post-gate or relaunch bookkeeping round whose ledger
  nobody filled, and the last of them is p2e's FINAL round. `-kind tool` is what
  each of them should have been opened as, and none of them existed as a claim
  about findings until now.
- *`wf round lens <prompt> -name N -model M`* — the escape from that refusal is
  a RECORD, not a flag, and it is the item the queue said to build if only one
  was built. The roster was the largest visible driver in that gate and lived
  as DATA: over the delta rounds 2-8 the criterion vector was EXTERNAL at 2/4/6
  and internal at 3/5/7/8, blockers 17, 16, 12 against 10, 11, 6, 7 — 1.76x —
  and the two lowest-blocker rounds are the two after the external lens was
  dropped for quota. (Round 1 is outside that comparison because it is the full
  round against a first draft; its criterion vector was external too, 13
  blockers, and including it gives 1.71x. The narrowing is named at every site
  that quotes the number.) Any descent a stop rule reads there is confounded
  with a billing decision, and the roster survived only as prose in eight
  findings headers — hand-readable, readable by no command. It writes `rounds/rN/lens-<name>.md`, the prompt verbatim with what
  ran it — a FILE in the round directory, because `wf close` copies a round's
  files and skips directories, so a roster under `lenses/` would be dropped from
  the record and then deleted by `prune`. A banked prompt is never rewritten
  (the findings lesson, one file over); `-name` and `-model` are both required,
  because "unknown" in a roster is the same claim as no roster.
- *The stop rule lived at five sites in four wordings and no check compared
  them* — one of them lower-cased, which the case-erasing normaliser cannot see
  as a difference. All five now read "The gate ends on a REVIEWER round that
  produces no BLOCKER, and on nothing else [R1]" — the reviewer qualifier was
  missing from the canonical statement — and the manual marks its own rule so a
  gate over these documents compares the sites. **The claim was checked by
  running the check, and the first run refuted it twice.** Two of the marked
  sites were POINTERS the same wave had just written — a quotation and the
  sentence announcing the marking — inside the bullet that says a marked pointer
  disagrees with every statement of its rule; and the two `pre-flight-gate.md`
  sites led with a COLON, so the statement unit began before the canonical
  sentence and normalised differently, which is the sixth wave's terminator
  defect in a new costume. Both repaired, plus the manual's own pointer EXAMPLES
  ("because of [R2]"), which had been failing the check since before this wave.
  The corpus now reports `4 marker(s), 1 id(s), 1 compared, 0 conflict(s)`. The
  fifth site is a Go comment `wf check rules` can never reach, because it
  compares artifacts; it is kept in the same wording by hand, and the manual
  says so rather than counting it.
- *And one rule from round 9, which reviewed the cut* — **a scope cut is an edit
  like any other and it is reviewed like any other.** It is the largest single
  edit a gate makes and its deletions are invisible to every check this tool
  has: `rules` compares the SITES of a marked rule, `dup` and `stale` read what
  is still written, and nothing at all watches a decision that stops existing.
  The round that reviewed one diffed the pre-cut span against the whole post-cut
  artifact token by token and asked, of every token whose count went to zero,
  whether the decision it carried survives anywhere — 4 blockers, 17 of 20
  findings defects in the cut, two deleted decisions recovered.
- *And a gap in a shipped check, found by accident and written down* — `wf check
  rules` cannot distinguish a STATEMENT from a POINTER to one, and a marked site
  can DECAY into a pointer without the marker moving: the cut left one of the
  two sites of three rules reading "every one of the five routes of [R1]" with
  the enumeration deleted, and the check printed `5 compared, 0 conflict(s)`
  over the broken document both before and after.

- *And the wave earned its review round, for the fourth time running* — two
  Opus vectors (Go correctness with a mutation sweep, and claims/specification)
  plus one external model over the diff, against four green bars. They found
  two BLOCKERS in the new code and refuted the wave's own flagship claim:
  **freeze examined only the PREVIOUS round, and `wf round new` does not require
  the current round to be frozen** — so a round opened past without a freeze was
  examined by no refusal at all, reproduced end to end as a gate closing with an
  undispatched round 2 that only `wf stats` named; and **the escape from both
  refusals was a FILENAME** — `touch rounds/r1/lens-anything.md` cleared a
  refusal that had just fired, the roster then printed a vector with no model
  (the one thing the write path refuses to record), and an UNCOMMITTED lens
  counted the same way, so a file that would not exist in the tag could
  authorise it. Both refusals now sweep every closed round, and a lens counts
  only where it says which vector, what ran it and what it was asked, and is
  committed. Also found: a lens banked against a FUTURE round pre-authorised the
  round the guard exists to refuse; the unchanged-detection stripped every
  `Recorded:` line, so two DIFFERENT prompts compared equal and the second was
  reported as already on file; and `wf stats` went on calling a round "not a
  measured zero" after the refusals had accepted its banked roster — the report
  contradicting the guard that let it through. **The stop-rule unification
  failed its own check on the first run**, twice over, and the claim was only
  believed after `wf check rules` was actually run over the two references.
  Nine line citations in the wave's own prose had drifted — the `check drift`
  class, committed by the wave that ships a false-zero guard. Two guards
  survived every mutation: one was dead code and was deleted, one got the test
  it lacked, and five more that were killed only in pairs got assertions on the
  MESSAGE, because falling through to a true-but-useless refusal is not the same
  guard.

**The tenth wave — mocker A3 closed, and the gate whose script actually RAN**
(`FEEDBACK-a3.md`; 13 rounds, 268 findings, 114 blockers, 151 fix-induced, then
a launch of 38 agents over 4h36m that came back `green: true` with 0 dead and 0
open blockers, six bars re-verified by hand). It is the first entry in this
corpus written AFTER an execution rather than before one, and the first with a
number for what the gate was worth: four blockers of rounds 10-12 would have
burned the fan-out outright, at ~5.7 M subagent tokens. What shipped here is
four measured items and two the feedback asked for; the manual's new JUDGEMENT
rules from that file (an acceptance section states observations; wfdry's clean
mode as a semantic constraint on `green`) were deliberately NOT applied — they
change what every future gate is told, which is a separate decision.

- *The queue's flagship ask was ALREADY SHIPPED, and the tool never said so* —
  "let a `[RN]` marker be recognised inside a `.mjs` string literal" was checked
  by putting one there before any code was written, and `wf check rules` printed
  the exact conflict the gate had paid six blockers for. `ArtifactList` takes
  every file and the collector read the script as text. What was missing was
  that nothing in the manual said a script could carry markers — and a real
  defect next door: the collector read CODE and COMMENTS as statements, so a
  marker on a code line made a rule site whose text was joined JavaScript
  (`const A = { owns: ['internal/api'], vector: 'reviewer' } const P = "The two
  routes…`), and a comment quoting an id joined the population. A `.js`/`.mjs`
  artifact is now read as PROSE — comments and the literal chunks of strings,
  via the new `internal/jstext` — and each prose SPAN is its own paragraph:
  blanking the code alone was NOT enough, because two literals on neighbouring
  lines still joined into a sentence nobody wrote, which is the same defect one
  layer down. Comments stay in (a rule restated in a comment is a copy that can
  drift); `${...}` interpolations leave a hole rather than splicing an
  identifier, and the string inside one is an argument, not prose. Every report
  says which artifacts it read as programs, and a script whose prose is EMPTY is
  named rather than counted clean.
- *`wf stats` said "an artifact was ADDED in this window" AFTER the counts* —
  nine rounds of descent (13, 17, 10, 16, 11, 12, 6, 7, 4) read as convergence,
  one more empty round would have closed the gate under the stop rule, and then
  the script was added and round 10 produced 38 findings and 12 blockers over an
  artifact nobody had reviewed. The tool KNEW and put it in a parenthesis. It
  leads the line now — `FIRST REVIEW of an artifact ADDED in this window` — and
  it prints whether or not the round has edits to price, because the previous
  form only existed inside the ratio branch. The prescribed-sequence fixture
  turned out to contain the shape already: `notes.md` was added after `gate-r2`,
  so round 3 is the first to review it.
- *`wf apply -rule RN`* — the note beside the prediction has advised the sweep
  since the meter existed ("if this batch changed a RULE, its consumers are NOT
  in this diff") and named nothing, so the operator had to go and look. It now
  lists every site of the declared id, marking the ones in files the batch
  wrote; a declared rule that NOTHING marks prints as exactly that, because an
  unmarked rule is the case `wf check rules` cannot reach either. What predicted
  the next round across A3's three script rounds was not batch size but whether
  the batch changed a rule with consumers elsewhere.
- *`-kind run`, and the median that called a launch a break* — round 13 spans
  288.2 min against a median of 45.3 because it CONTAINED the run, and
  `wf stats -time`'s own note reads "a span far above the median is a break, not
  work". A run round is a REVIEWER round everywhere else (what a run reports is
  evidence produced by executing, so its zero is a measured zero); only the
  clock treats it apart, and its span prints on its own line.
- *And the wave earned its review round, for the fifth time running* — one
  external model over the diff, four bars green, three findings and two of them
  real. **A `-kind tool` round was printing FIRST REVIEW**: it dispatched no
  reviewer by declaration, so the new line claimed a reading nobody did — the
  wave's own defect class committed inside the fix for it, and the
  prescribed-sequence fixture had been asserting it. It now says an artifact was
  added and NO reviewer was dispatched. **A run round's split still read
  `review`**, and that first segment is open-to-first-fix, which on such a round
  IS the run. The third was taken by HALF: the median line had been renamed
  "review round(s)" while a `-kind tool` round is in that median and always has
  been — the reviewer proposed narrowing the population, and narrowing it
  silently is the defect one line up, so the LABEL was fixed instead and the
  time line over the three closed gates is byte-identical to before.
- *Two small ones from the feedback's own list* — the `closed by` refusal now
  names the repository it looked in and says when the sha IS a commit in the
  TARGET, which is what a round recording a LAUNCH reaches for (the rule itself
  stands: a closure cites the workspace commit that landed the fix). And
  `wf round record` prints a note when the round banks no lens: eleven of that
  gate's thirteen rounds banked none, the prompts for rounds 1-10 are gone, and
  the claim the operator most wanted to make from those numbers cannot be
  checked against anything. Recording findings is the step that feels like the
  work.

**The eleventh wave — the two things every agent was inventing for itself**
(2026-08-25). Both were found by asking the same question twice — "is there a
standard, or does each agent make one up?" — and both answers were measured
before a line was written, with `ws.ParseFindings` over the six gate ledgers on
disk rather than by hand (the first hand `awk` reported 101/29 on p2d against
the true 98/26, which is the 99/27 scar committed by the person who knows about
it):

- *The severity scale was open, and it had drifted into FOUR vocabularies* —
  1065 findings, every total reconciled against that workspace's own `wf stats`:
  BLOCKER universal; under it MAJOR on five gates and CONCERN on one; under that
  MINOR on four, ADVISORY on one, NIT on one. **172 of the 1065 — 16% — carry a
  level no other gate can be read against**, and they live in TWO workspaces, one
  closed and pruned and one still OPEN. The review round REFUTED this wave's own
  first argument: it said the script side does not drift because a JSON Schema
  `enum` validates it, measuring six OTHER phases' scripts that all descend from
  one ancestor. An enum validates the VALUE and never the LIST, and the scripts
  these six gates PRODUCED carry four lists — none at all (p2d),
  `['MAJOR','MINOR']` (p2e), `['BLOCKER','MAJOR','MINOR']` (p2f) and
  `['blocker','advisory']` (a1, a2, a3, straight out of this repository's own
  template). So the scale is closed not because one layer validates and the
  other does not, but because this is the one place both meet: **BLOCKER / MAJOR / MINOR / NOTE**,
  refused by `wf round record` before a byte is written and read permissively
  everywhere, because three closed workspaces carry the old words and a report
  that refused to read them would destroy the only evidence this project has.
  The DEFINITION is the operative half — *a finding's level is decided by what
  catches it downstream, never by how bad it reads* `[R2]` — and its checkable
  clause is that a MAJOR **names the later gate that catches it**; a row that
  cannot name one is a BLOCKER. That turns "classify by actual downstream
  coverage", which the manual already said, from a feeling into a claim the next
  round can check. NOTE is counted, never subtracted: every banked total includes
  whatever non-defect rows its gate filed, so `wf stats` names them apart and
  leaves the total where the corpus can still be compared against it. ONE gate
  filed a non-defect row (a3's single NOTE); p3a's NIT is that gate's MINOR
  tier. The template shipped a vocabulary of its own, `['blocker','advisory']` —
  the tool built to stop this committing it — and it reached three gated
  scripts.
- *The delta round's vectors were invented every time* — the manual prescribes
  three reviewers in full for round 1 and says `(round 1 only)`; the round
  discipline below it says what to HAND a delta reviewer (`wf delta`) and never
  what to ASK. Measured 2026-08-25: the two gates that banked any roster produced 13 lens
  RECORDS under TEN distinct names (17 FILES, four of them the copies `wf close`
  makes), of which exactly one — `criterion` — appears as a VECTOR NAME in the
  manual; the other four gates banked none, so their
  prompts are gone. `wf round lens` now takes `-asks`, required, from a closed
  set, with `-name` left free as the round's own label: a hard refusal on NAMES
  was considered and REJECTED, because nine of the ten banked names are
  slice-specific and it would have fired on almost all of them, and `other` is always available so the refusal only ever means "you
  typed something no report can compare". The standard is *a delta round asks
  CLOSURE, FIX-INDUCED and RIPPLE, and may add vectors but never drop those
  three* `[R3]`, each with a measured origin rather than a taxonomy's symmetry:
  closure (two hand-written closure lenses returned 6 and 4, 5 and 4, 6 and 4
  closures they judged NOT closed, out of rounds of 25, 23 and 21 findings — the
  most valuable half of those rounds), fix-induced (a
  fix is the largest single source of blockers: half the BLOCKERS of rounds 3+, then 5 of 11,
  then 9 of 9), ripple (a fix that changes a RULE has consumers not in the diff;
  THREE of four such on one run contained none of the rule's keywords). Coverage is a
  NOTE and never a refusal — the rule is new and every corpus that would have to
  satisfy it is closed.
- *And the wave earned its review round, for the sixth time running, and this
  one refuted its own flagship argument* — two Opus vectors (Go correctness with
  a 40-mutation sweep, and claims/specification) plus one external model over the
  diff, against four green bars: **6 blockers and 12 majors**. The claims vector
  killed the reasoning above; the mutation sweep found **23 of 40 mutations
  surviving**, split cleanly — every guard belonging to the SCALE was killed,
  almost every guard belonging to the COVERAGE REPORT survived, which is a map of
  where the tests were written and where they were assumed. The external vector
  found the one thing neither Opus vector did: `Placeholder()` means "the text
  cell is empty", it was checked BEFORE the level, and `| 1 | CONCERN |  |  |`
  rode straight through — the closed scale had a hole exactly the width of the
  scaffold's own row. Three findings were the project's own named class committed
  inside the fix for it: the roster and `wf round record` printed "no banked lens
  asks closure, fix-induced, ripple" over a roster whose lenses simply predate
  `-asks`, so an ABSENT measurement read as a measured zero, and `wf stats` said
  the opposite about the same round — two commands contradicting each other is
  worse than either answer. All three now go through ONE `classifyAsks`, which
  is kind-aware (the note fired on round 1 of every gate, a FULL round), counts a
  single unstated lens as making the whole round UNRECORDED, and prints the
  ACTUALLY missing question rather than the required list (a roster asking two of
  three was told it asked "none of" all three — and that line is what `wf close`
  banks into the one artifact `prune` leaves behind). Also: widening the template
  enum from two words to four widened the bucket `bank()` throws away, so a
  reviewer returning `severity: 'major'` — "a real defect" by this wave's own
  definition — produced `green: true` and survived as a number in a log line;
  and `wfdry` ignored a declared `enum`, so `f.severity === 'blocker'` was FALSE
  in all seven modes and the whole fix-round path was executed by no mode at all,
  which means the template change shipped untested through a green
  `make check-tools`. Choosing the enum member by POSITION then handed the clean
  mode `verdict: 'blockers'` and `result: 'not-run'`, so it is chosen by what the
  word MEANS, with a vocabulary kept apart from the one that reads array names.
  Ten numbers were wrong: "three closed workspaces" (two, one of them open),
  "two gates filed a non-defect row" (one — NIT is p3a's MINOR tier, which this
  wave's own test asserts), "four ripples" (three of four, contradicting a
  sentence 205 lines up in the file being edited), "17 lens files" (13 records;
  17 counts the copies `wf close` makes), and "measured and REJECTED ... half of
  correct practice" for a measurement that does not exist.
- *And the claim was checked by running the check* — the ninth wave's lesson,
  applied: `[R2]` and `[R3]` were marked at two sites each, a scratch workspace
  was built over the two references, and `wf check rules` was actually run
  (`8 marker(s), 3 id(s), 3 compared, 0 conflict(s)`). Every new guard was
  mutation-proven; one mutation first read as SURVIVED and was not — `sd` exits
  ZERO when it replaces nothing, so a mutation harness built on it can report a
  guard as unproven that was never mutated. Verify the replacement landed, not
  the exit code.

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
- **A change under `skills/` or `hooks/` bumps `version` in
  `.claude-plugin/plugin.json`.** `claude plugin update` is VERSION-keyed —
  measured: it copies nothing when the declared version is the one already
  installed, so a spec fix committed without a bump never reaches a session.
  `scripts/install.sh` forces a reinstall when `wf docs` reports drift, but that
  is the backstop; the bump is the mechanism. The plugin sat four releases
  behind the clone once, and the SKILL.md sessions loaded described a rule the
  manual had already replaced.
- **Artifacts are changed through `wf apply`, never by hand.** A hand edit plus
  `git commit` gives up both all-or-nothing and the declared `count` — the two
  reasons the command exists.
- **Command semantics are specification.** The `skills/workflow-gate/references`
  passed a gate and describe what each command does and why. Changing
  semantics or output means reconciling the references against the source, in
  the same commit (8 of that gate's 22 blockers were text-code divergences,
  and the 2026-08-21 reconciliation by the same method found the vacuous
  enforcement in freeze).

## Bars

```bash
make test        # go test ./cmd/... ./internal/... -race -count=1
make lint        # go vet + gofmt (+ golangci-lint when installed)
make build       # ./wf ./wfcheck ./wfdry with the version stamp (-ldflags)
make check-tools # the BUILT binaries against the banked fixtures (meta-guard must FAIL)
```

`make test` counting `ok` lines is not a green bar — one package can fail while
the count looks right. Read the FAIL lines.

After any code change — `make install` (`~/.local/bin/wf`): the documents
prescribe `wf ...` commands, and a run against the stale binary looks exactly
like a run against the new one. The stamp in `freeze.json` and the note in
`verify` catch it after the fact; they do not replace the reinstall.

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

## State as of 2026-08-24 — read before trusting

- **Four real gates have now run entirely through `wf`.** P2d (10 rounds, 98
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
  watched in silence, which is what the seventh wave above answers.
  `check drift` is STILL unexercised live.
- **The validation corpus DOES survive, and it is the best evidence this
  repository has.** The claim that it did not was stale and cost an
  investigation an hour of archaeology before it was checked: four gate
  workspaces are on disk with full git history — `~/gates/p2d`, `~/gates/p2e`,
  `~/gates/p2f` (82 / 102 / 87 commits, verified 2026-08-24). The fourth,
  `~/projects/gates/mocker-a-mcp`, has been CLOSED AND PRUNED: no `.git`, no
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
  work.** `wf stats -time` over the three closed gates: a round is 10-40
  minutes, the pooled median 15.5, the review segment 70-87% of it and the
  bookkeeping tail about 3%. So the ROUND COUNT is the cost function, the
  ~9-command sequence is not worth compressing (1.4 minutes across p2d's eleven
  rounds), and the largest measured saving in the whole investigation is a
  DOCUMENT change already in the manual: running the decision gate to exhaustion
  before the script exists took one gate from 10+6 rounds to 8+3, about an hour.
  The authoring window — init to the first artifact, 31.7 and 42.2 minutes — is
  the largest block no round covers.
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
- **No `round close`, no reopening a round.** Freezing is one-way: if tag
  `gate-rN` exists, you open the next round. `wf close` is a different thing
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
