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
reviewers is pinned with the gate, and a PreToolUse hook on the Workflow tool
(`scripts/hooks/workflow-preflight.sh`) injects the reminder
deterministically. The skill and the hook ship as the **wfgate plugin**
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

Ten units, each one decision.

- `internal/gitx` — the only place that runs `git`. Git is not an integration
  here, it is the storage layer; the wrapper exists so that "the workspace is a
  repository" stays a single decision instead of a shell-out smeared across six
  commands.
- `internal/ws` — the workspace: `gate.json` (the marker; `Find` walks up
  looking for it the way git itself does), `artifacts/`, `rounds/`. The
  findings-table parser (`findings.go`) lives here too — the table is
  **parsed, never generated**.
- `internal/apply` — the all-or-nothing batch of textual edits.
- `internal/check` — the deterministic checks: `cite.go`, `dup.go`,
  `drift.go`, `rules.go`.
- `cmd/wf` — the subcommand dispatcher.
- `internal/wfscript` — a Workflow orchestration script as a PARSED PROGRAM:
  the wrapper discovery, the agent-call inventory with its options resolved
  through consts, spreads and factories, the phase titles, the loops. Every
  question the two script-gate tools ask about a script is answered once, here.
- `internal/wfcheck` + `cmd/wfcheck` — the static check over that inventory.
- `internal/wfdry` + `cmd/wfdry` — the dynamic runner: it executes the script
  on an embedded engine with every orchestration hook stubbed, across the seven
  modes a real run can enter. Fixtures live in `testdata/` — the meta-guard
  fixture is the P2e false green, banked.
- `internal/cli` — `ParseMixed`, shared by all three binaries.
- `scripts/` — no longer code: the PreToolUse hook and `install.sh`.
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
  previous round's own fix is the close signal (100% on P2d's last two
  rounds), and the ledger can carry it only where the author marks it. Stats
  prints "not measured" when nobody marked: an unmarked corpus and "no
  self-inflicted findings" are different claims, and only the second belongs
  to the finding's author.

**The fourth wave, from P2e — the first script gate** (`FEEDBACK-p2e.md`;
17 rounds, 235 findings, 293 KB of artifacts):

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
case per finding and per guard), and three end-to-end files under `cmd/wf`
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

## State as of 2026-08-23 — read before trusting

- **Three real gates have now run entirely through `wf`.** P2d (10 rounds, 98
  findings — `FEEDBACK-p2d.md`), P2e, the first over a workflow SCRIPT
  (17 rounds, 235 findings, 293 KB of artifacts — `FEEDBACK-p2e.md`), and P2f,
  the first carrying both gates in one workspace (13 rounds, 111 findings, 180
  KB over three artifacts — `FEEDBACK-p2f.md`; its implementing run then
  executed 20 agents over 149 minutes and came back green). P2e
  confirmed live: the anchor check turning a wrong citation into a patch, the
  ratchet firing on a real regression, record rejecting a placeholder sha,
  fix-induced share deciding the stop, delta briefs at ~1/10 of a re-read. P2f
  added the first live yield from `check rules` — marked before the gate closed,
  it found a divergence across the derived pair on its FIRST run, which is the
  case the feature was built for — plus eight mis-aimed anchors and three
  citations that resolved, sat inside the file and pointed at the wrong lines.
  `check drift` is STILL unexercised live.
- **The validation corpus does not survive a session** (it lived in a scratch
  directory). To rebuild: `git clone --no-hardlinks ~/projects/mocker
  <lab>/head`, then a workspace over any set of documents.
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
  `gate-rN` exists, you open the next round.
- **No check that "the cited line says what the sentence beside it claims" —
  ever.** That is judgement; `wf check cite -show` exists to make it cheap, not
  to pretend it has been done.
- **No autofixes.** `check` only reports. A tool that fixed citations itself
  would be fixing them in an artifact somebody already reviewed.
- **No git hooks or CI IN A GATE WORKSPACE.** A workspace lives locally and dies
  with its slice; there is nothing to publish. This repository itself is pushed
  to Gitea and does ship a Claude Code PreToolUse hook — a different thing with
  the same word in it.
