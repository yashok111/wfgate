# wfgate — gate harness

The `wf` CLI: a review gate's workspace as a git repository, atomic application
of edits, and deterministic checks over the artifacts. The repository is the
single source of truth for the whole gate toolchain: the script gate's two
tools (`wfcheck`, the static check over a Workflow script's parsed program, and
`wfdry`, which executes it with every orchestration hook stubbed) and
the operating manual, shipped as the `workflow-gate` skill
(`skills/workflow-gate/`) and embedded into the binary — `wf docs guide` prints
it, `wf init` writes the gate-checks reference into every workspace, and
nothing needs to stand in an agent's always-on context. The skill and the
two PreToolUse hooks (`scripts/hooks/workflow-preflight.sh` on every Workflow
call, `scripts/hooks/script-authoring.sh` once per session on a `Write`/`Edit`
that looks like an orchestration script) ship as the **wfgate plugin** — the
repo root is the plugin, and `.claude-plugin/marketplace.json` makes the local
clone its marketplace.

A gate over a large change spends effort on two different things. One is the
reviewing itself — that is judgement, and it is what the rounds are for. The
other is bookkeeping: which revision of which artifact a reviewer read, what a
round was obliged to close, whether a batch of edits landed whole, and whether
the second copy of a rule was corrected along with the first. This tool does
only the second.

## Install

On any machine with Claude Code (needs git, go, claude):

```bash
git clone ssh://git@git.sumka.site:2222/yakov/wfgate.git ~/projects/wfgate
~/projects/wfgate/scripts/install.sh      # three binaries + plugin (skill + hooks), and it UPDATES an installed plugin
```

After a `git pull`: re-run `scripts/install.sh`. An installed plugin is a cached
COPY, and refreshing the marketplace alone does not touch it — the copy sat four
releases behind the clone once, so the manual a session loaded described a rule
the manual had already replaced. `wf docs` names the SKILL.md or any
reference where the installed copy differs from the one this binary carries, and
install.sh forces a reinstall when it does — `claude plugin update` is
version-keyed and copies nothing when the declared version has not changed, so a
change under `skills/` or `hooks/` must bump `version` in
`.claude-plugin/plugin.json`.
`scripts/install.sh --uninstall` reverses everything. Piecemeal:

```bash
make build          # ./wf ./wfcheck ./wfdry, version-stamped via -ldflags
make install        # all three into ~/.local/bin
make test           # go test -race
make check-tools    # the built binaries against the banked fixtures (meta-guard must FAIL)
make check-docs     # `wf check rules` over this manual itself — a bar, not a report
```

`git` is taken from `PATH`, and every invocation is pinned with
`-c diff.algorithm=histogram` so that what this tool reports does not depend on
the operator's `~/.gitconfig`. Measured over the three closed gates: git's
default is myers, the box that produced every banked figure carried histogram,
and `wf stats` priced one gate at 4586 changed lines under myers against 4482
under histogram — 2.8 % apart, and 5.3 % on a single round, enough to move that
round's cost ratio. `check drift` is immune (byte-identical across all four
algorithms over 235/343/230 citations, because `--unified=0` leaves only
old-side ranges); `check stale` moved one range's boundary between "the fix
wrote this sentence" and "this is an untouched consumer". histogram rather than
myers is not a claim about diff quality: it is what every banked number was
measured under.

One dependency, `goja`: it is the JavaScript
parser `wfcheck` reads a script with and the engine `wfdry` runs it on. The
dependencies.

## The quick cycle

```bash
wf init p2d -target ~/projects/mocker      # a workspace with its own git repo
cd p2d
wf add ~/draft/context.md ~/draft/script.md  # first commit of an artifact; wf apply cannot make it

wf round new -title "kernel + checkpoints"   # -kind delta for a delta round
wf check                                   # cite + dup + rules + closure
wf apply -f fixes.json -m "close r1 blockers"
wf round record findings.md                # the round's ledger, closures validated
wf round lens criterion.md -name CRITERION -model "external gpt-5.6-sol" -asks criterion   # what reviewed it
wf freeze                                  # hashes + target HEAD + tool version → commit, tag gate-r1
wf verify                                  # the pre-launch check: what runs is what was reviewed

wf delta                                   # the delta round's brief: findings + closure checklist + diff
```

## Commands

| command | what it does |
|---|---|
| `wf init <name> -target <repo> [-dir D]` | workspace: git init, `gate.json`, `artifacts/`, `rounds/`, first commit |
| `wf add <file>...` | bring an authored artifact under the workspace and commit it; adopts an untracked draft already in place |
| `wf status` | manifest, current round, who last touched each artifact, dirt, tool version |
| `wf apply -f <edits> [-m msg] [-rule RN\|"name"] [--dry-run]` | all-or-nothing batch of edits (JSON or text blocks), then a commit of only the touched files. It NOTES a replacement whose `old` covers half the file or more, with how many lines that one replacement rewrites: all-or-nothing and `count` are per REPLACEMENT, so a whole-artifact block has a count of one that always matches and every change inside it is verified by nothing — measured on P3b, eighteen of sixty-eight batches were a single replacement, the changes inside them came from a pipeline whose non-matching `str.replace()` is a silent no-op, and a later round found 15 of one round's 30 recorded closures byte-identical to their pre-fix text (a second lens confirmed 11). A note and not a refusal: generating one minimal block per change failed twice on uniqueness on that same gate; `-rule` declares the `[RN]` rule this batch changes and the note then NAMES the other sites carrying that marker, marking the ones in files the batch wrote — a rule nothing marks is said out loud, because that is exactly the corpus `wf check rules` cannot reach either. It also takes a free-text rule NAME, which sweeps every sentence carrying all of its words and says out loud that it is the weaker half: a script gate's rules live in prompts and are marked in no document, so the id form reaches nothing there — measured on P3a, `-rule "bar accounting"` was refused outright and the flag was unusable for the whole gate whose most valuable sweep it is |
| `wf round new [-title T] [-kind full\|delta\|tool\|run]` | open the next round, scaffold `rounds/rN/findings.md` with its scope. `run` marks a round that CONTAINS a live run of what the gate built: it is a reviewer round everywhere except the clock, where its span is printed on its own, kept out of the median, and split under the head `run` rather than `review` — one gate's launch round spanned 288.2 min against a median of 45.3, and `-time`'s note called it a break rather than work |
| `wf round record <file>\|- [-round N]` | fill a round's findings.md from a file or from stdin (`-`), validating closures before writing; the file is POSITIONAL (there is no `-file`) and an empty input is refused rather than recorded over a filled ledger; the `level` cell must be one of **BLOCKER, MAJOR, MINOR, NOTE** and anything else is refused before a byte is written — a finding's level is decided by what catches it downstream, never by how bad it reads, and measured 2026-08-25 over the six gate ledgers then on disk, the open scale had produced FOUR vocabularies for one scale, 172 of 1065 findings at a level no other gate can be read against (the corpus has grown since; the shape has not), and a script `enum` does not save the other side: it validates the VALUE and never the LIST, so the scripts those six gates produced carry three lists of their own plus a drifted field NAME (`level` on one, `severity` elsewhere), including the `['blocker','advisory']` this repository's own template shipped into three gated scripts. Every READ path stays permissive so the closed workspaces keep being readable; a `closed by` sha the workspace does not have is refused by an error that names the repository it looked in and says when the sha IS a commit in the TARGET (the shape a round recording a LAUNCH reaches for); prints a note when the round banks no lens — measured: eleven of one gate's thirteen rounds recorded their findings and lost the prompts that produced them — and, on a DELTA round, what its banked roster does not cover, through the same classifier the roster and `wf stats` read. It also counts the closures `wf check closure` will NOT be able to reach — those whose finding text names no artifact path — at the moment the operator is still writing the file: measured on P3a, 0 of 350 claims checkable on the first record and 26 of 26 after the paths went in |
| `wf round lens <prompt> -name N -model M -asks Q[,Q] [-round N]` | bank one reviewer vector against a round — the prompt verbatim and what ran it — into `rounds/rN/lens-<name>.md`; no argument prints the roster. `-round` must name a round the gate has opened. A banked prompt is never rewritten, and one left uncommitted by a failed commit is committed on the next run. It is also how a round whose vectors ran and found NOTHING says so, which is what separates it from a round nobody dispatched. A file counts as a record only if it says which vector (`# Lens`), what ran it (`Model:`) and what it was asked (a prompt after the `---`), and only once committed — the roster is the escape from two refusals, so a `touch`ed filename must not be one. `-asks` takes the set however it is written — `closure,fix-induced,ripple`, `closure+fix-induced+ripple`, a space-separated list, or the flag repeated — because one gate banked a single vector per round and then hand-edited `Asks:` in the file nineteen times over ten rounds, having been refused the natural spelling. It is the COMPARABLE half beside a free-text `-name`: the set is closed (`orchestration, routing, alignment, facts, seams, criterion, closure, fix-induced, ripple, other`) and an unknown value is refused, while `other` is always available so a vector a slice genuinely needs is never refused — measured 2026-08-25, when two gates banked any roster at all: 13 lens RECORDS under TEN distinct names, of which exactly one appears as a VECTOR NAME anywhere in the manual. The corpus has since grown — three gates bank 92 records today, 23 distinct names in one of them — so read the date, not the figure. A delta round asks CLOSURE, FIX-INDUCED and RIPPLE, and may add vectors but never drop those three [R3]; the roster and `wf stats` NOTE what a delta round's roster does not cover, and never refuse it |
| `wf freeze [-refreeze]` | sha256 of all artifacts + target HEAD + tool version + citation counts + the corpus size in lines (with the delta against the previous freeze — a document that doubles while the blocker count holds is a design being elaborated rather than settled) → `rounds/rN/freeze.json`, commit, tag `gate-rN`; refuses while a blocker of the round being closed stands open; notes (never refuses) every citation that no longer RESOLVES against the target's HEAD, a round whose artifacts changed without `wf apply`, and — when it could not resolve them, or the round's window has no lower bound — that the check did not run. Refuses when ANY closed round carries an untouched findings scaffold and no banked lens: a round nobody is recorded as having read must not freeze as a round that found nothing (a `-kind tool` round is exempt — its opening subject already says no reviewer was dispatched; the OPEN round is exempt because its scaffold is blank by construction). Every closed round, not just the previous one — `wf round new` does not require the current round to be frozen, so a round opened past without a freeze was seen by no refusal at all. `-refreeze` republishes a round already tagged: freezing stays one-way by default (the manifest is read from the WORKTREE by `wf verify`, so a silent rewrite turned a MISMATCH into "clean"), but `wf check cite` surfaces a mis-aimed anchor without a freeze — what only freeze does is REPUBLISH the corrected bytes under the tag reviewers were handed, and five of one gate's twenty-two rounds deleted the tag by hand to do it, twelve times in all (round 13 six times, round 15 five). The superseded manifest is read from the TAG, never from the worktree copy this command rewrites: a re-freeze whose tag move failed otherwise left a NEW manifest under an OLD tag, and the retry compared it against itself and refused. The tag MOVES, the manifest records how many freezes the round has had and which commits they published, and it refuses when every artifact hashes the same against the same target revision: a re-freeze republishes CHANGED bytes |
| `wf verify [-round N]` | the pre-launch check: the tree is byte-for-byte what was frozen |
| `wf delta [-from REF] [-to REF] [-save]` | the round's findings, the closure checklist, and the artifact diff; `-save` commits the brief into the open round's bookkeeping |
| `wf check [cite\|dup\|drift\|rules\|stale\|closure]` | deterministic checks (no argument = cite + dup + rules + closure); `dup` takes `-since REF`, `-threshold F` and `-min-words N`; `stale` takes `-since REF` (default the newest freeze) and `-max-df N` (default 3); `closure` takes `-round N` (refused when that round does not exist). A `.js`/`.mjs` artifact is compared as PROSE — its comments and string literals, never its code — and every report says so; a script whose prose is empty is named rather than counted as clean |
| `wf stats [-time]` | per-round findings, closures, apply batches, edits and changed lines (with findings per 100 changed lines — the batch cost meter; a round whose window contains the commit that CREATED an artifact says FIRST REVIEW before its counts and prints no ratio at all, because it prices a corpus its batches did not write and a descent before it says nothing about it — on a `-kind tool` round the same line says an artifact was added and NO reviewer was dispatched, because that round reviewed nothing by declaration), each round's frozen corpus size and its growth, the prediction basis `wf apply` reads (narrower than the gate total, and the difference printed rather than reconciled; a workspace with no apply commit is named as an absent population, and a round whose window cannot be attributed has its numbers omitted rather than filled from the whole history), the opt-in `[fix-induced]` share (with the rounds carrying no marker named, so the share reads as the lower bound it is) and the same marker read over BLOCKERS — per round beside the count, as a gate total, and as one line naming every round whose blockers are a MAJORITY self-inflicted, because a flat descent is what the fix rate matching the introduction rate looks like from outside (measured: 100 of 177 blockers and 11 of 22 rounds on the gate that stopped instead of closing, 22 of 79 and 12 of 28 on two others), the rounds that applied no batch at all, every run of three or more rounds at exactly ONE blocker, the rounds whose findings.md could not be read at all (an absent measurement, not a measured zero), the rounds carrying an UNFILLED findings scaffold (an empty form, not a measured zero — the shape under which a gate can end without anyone having looked), each round's banked lens roster and the rounds that record none (tool rounds excluded from both, for the same reason), and reviewer-vs-tool round totals, from the ledger and git; `-time` adds each round's wall clock split into review / fix / bookkeeping tail, plus the authoring window and the median round — over every round except the `-kind run` ones, whose spans print on their own lines (the population is narrowed by nothing else: a tool round has always been in this median, and the medians published from these gates were measured over it) |
| `wf round new [-title T] [-kind K]` and `wf freeze` | ...and both now name a workspace `gate-checks.md` that is no longer the reference the binary carries — or the absence of one at all, because no pin is not a current pin — listing the sections the pin predates — a note, never a refusal: one gate reads ONE version of the rules, and measured on P3a a sixteen-round-old pin briefed two rounds of reviewers without the closed severity scale, found by a human asking rather than by a check |
| `wf new-script <name> [-target REPO] [-o FILE]` | emit the orchestration-script harness — wrapper, schemas, per-section gate, liveness ledger, read-only accept, tally; `wfcheck`-clean and `wfdry` 7/7 as emitted, and re-verified by `make check-tools`. Never writes over an existing file |
| `wf close [-archive DIR] [-artifacts=false]` | bank the gate's own record — `history.tsv` (commit times and subjects), `stats.txt` (`wf stats -time`), `freezes.tsv`, every round's findings, and every file of every round, and a feedback scaffold with the derived numbers prefilled; a filled scaffold is never overwritten. Refuses while ANY round carries an untouched findings scaffold with no banked lens, and names a last round that dispatched no reviewer at all (a `-kind tool` round is exempt from the refusal and is instead NAMED, because its opening subject already records that no reviewer was dispatched). the artifacts themselves are banked too, because their CONTENT is the one thing `prune` destroys that nothing else holds; `-artifacts=false` opts out, and `prune` then names each artifact before removing it |
| `wf prune [-yes]` | delete `artifacts/`, `rounds/` and then `.git` once the record is banked; refuses without one, refuses a stale one (including a record committed together with the work it describes), and reports without `-yes`. Measured on one closed gate: 4.2 MB down to 696 KB, and the record holds the artifacts, every round's files, the commit log and the stats |
| `wf docs [name]` | print the embedded operating manual (`guide`, `checks`, `routing`, `preflight`, ...) |
| `wf version` | the binary's stamped build version |

Exit codes: `0` clean, `1` findings or failure, `2` usage.

### `wf apply`

```json
[
  {"file": "artifacts/context.md",
   "old": "the exact string as it is in the file",
   "new": "the replacement",
   "count": 2,
   "note": "closes B7"}
]
```

`count` is how many occurrences the author EXPECTS (default 1). If the real
number differs, the whole batch is rejected and no file is written. That is the
command's point: a rule living in two artifacts and corrected in one becomes a
hard error at apply time, not a finding three rounds later.

After the write every written file is re-read and compared byte-for-byte with the buffer the plan produced.
An apply is verified by finding each change, not by the absence of an error.

For multi-line or quote-bearing `old` strings, prefer the plain-text block
format — nothing between markers is ever escaped:

```
=== artifacts/context.md  count=2  note=closes B7
<<<<
old text, verbatim — backticks, ${}, quotes, JSON all pass through
====
the replacement
>>>>
```

Each replacement block carries its own `=== <file>` header — `count` is scoped
to that one replacement, not to the file; fifteen replacements in one file are
fifteen headers. When a batch changes enough LINES that the workspace's own
history (findings per 100 changed lines, from the ledger and the apply commits)
predicts a costly next round, `apply` prints a note suggesting a split by rule,
and says that a rule's consumers are not in the diff — a note, never a refusal.

`-rule RN` turns that sentence into the list it was pointing at: the note names
every site of `[RN]` across the artifacts, marking those in files the batch
wrote, so the copies that were NOT touched are on screen at the moment the fix
lands. Measured on mocker A3: what predicted the next round's cost across three
script rounds was not the batch's size but whether it changed a RULE with
consumers elsewhere, and six of one round's twelve blockers were one such copy
left behind. A declared rule that no artifact marks prints as exactly that —
an unmarked rule is the case `wf check rules` cannot reach at all. A script gate has
no marked rules to declare, so `-rule` also takes the rule's NAME in free text and
lists every sentence of the artifact set carrying all of its words — the weaker
half of the same sweep, and it says so: it names the sentences that use the words,
not the sites of a rule.
It counts what git sees changed in the files just written, so a dry run says the
estimate is absent rather than printing one on a different basis. The meter's denominator is CHANGED LINES, not edits: measured across
three closed gates and excluding each gate's round 1 — whose window is the
authoring commit rather than a fix — findings correlate with edits at
+0.26..+0.49 and with changed lines at +0.47..+0.82, because one edit can
rewrite one word or nine hundred lines. Lines win on two of the three gates and
tie on the third; including round 1, the edit count wins on one. Every apply commit records its own edit count in a `wf-apply:`
trailer, then in the default subject, then in the per-edit lines of its body —
three sources in falling order of how hard they are to erase, and the third is
all that survives in a workspace older than the trailer whose operator wrote
every subject by hand (two such gates read as an empty meter until it was
added, over 186 and 150 edits that were sitting in commit bodies the whole
time). It used to read the default subject alone, and `-m`
replaces the subject, so a gate that wrote a subject for all 27 of its batches
measured nothing at all. Assembling the JSON was the measured bottleneck of the first
live gate: a 35-edit batch under three layers of quoting failed on escaping
more often than on substance. The format is detected by the first byte (`[`
means JSON).

### `wf check cite`

Finds `path.ext:LINE`, `path.ext:A-B` and `path.ext:LINE#anchor` in the
artifacts and verifies them against the target repository: the path resolves to
exactly one tracked file, the range is well-formed and inside the file, and the
`#anchor` occurs within the cited lines. An anchor miss prints the nearest line
carrying the token — a ready patch, not a reason to search.

The anchor is an ASCII token by policy (gate artifacts are English); a
non-ASCII anchor is an error, never a silent "unanchored". The count of
citations without an anchor is printed on every run, and citation-like
references to extensionless tracked files (`Makefile:90`) are listed as NOT
verified. `-show` prints the cited lines; `-require-anchor` fails on a citation
written without an anchor.

### `wf check dup`

Cuts the artifacts into sentences, normalises them (quoting and case erased —
one rule is written differently in markdown and in a JS template literal),
groups exact and near matches by Jaccard, and flags `DIVERGENT` only the groups
whose copies **both read differently and were last edited in different
commits**, by per-line `git blame`.

Either condition alone is not enough: identical copies edited apart are a rule
correctly restated, and diverged copies from one commit are a deliberate
rewording the author saw side by side. `-since gate-rN` narrows the output to
groups touched after that tag and prints how many it hid.

Sentences are unwrapped from hard-wrapped paragraphs before splitting — two
derived artifacts wrapping the same sentence at different columns are compared
as sentences, not as whatever fit on a line. Copies that agree except for a
number or number-word ("four checks" vs "FIVE checks") are flagged `NUMERIC`
and fail the check: no similarity threshold separates that class from
agreement, masking the number does. Copies that agree except for a COMPARISON
(`== 3` beside `!= 3`) are flagged `OPERATOR` for the same reason — the
normaliser erases every symbol, so without this the pair reported as `sim 1.00`,
an exact match, which reads as agreement over a direct contradiction.

The near-match pass is candidate-filtered by rare tokens, so a sentence built
entirely of common words has no candidate at all; the count of such sentences is
printed, because a filtered corpus must not read as an empty one.

### `wf check drift`

When the target moved after a freeze, intersects its `frozen..HEAD` diff with
the cited ranges: `edited` — the cited lines changed, re-read the claim;
`shifted` — the citation moved, the re-aimed range is printed; `gone` — the
file left the tree; `unresolved` — the path names no single file at the frozen
revision. The only mechanical signal an unanchored citation gets. It refuses a
target whose tracked files differ from HEAD: "the target has not moved" is a
claim about HEAD, and a cited line rewritten without a commit leaves HEAD where
it was.

### `wf check rules`

Sites of one rule marked `[RN]` must normalise identically across the
artifacts. This is the reach into what `dup` structurally cannot see — copies
that contradict in different words — and it works only where an author marked
the rule, so the marker population is always printed. It pays off across a
DERIVED artifact pair, where `dup` reports tens of false groups per real
contradiction: on the first run that marked rules before the gate closed, the
check found a divergence between a source document and its derived brief on its
first run over the pair.

A marked rule may be written with any markdown around it — emphasis, a bullet,
a table cell — and may share its line with other prose: sites are compared as
SENTENCES, and the normaliser erases the markup.

**A `.mjs` artifact carries markers too, and this is the reach that pays for
itself fastest.** An orchestration script's prompts restate the decisions of the
document beside it, and `dup` sees the pair (`sim 1.00` for a whole gate) while
correctly declining to call it a defect — a script restating a decision IS the
design. What is missing is a comparison of what the two copies SAY, and marking
both sites with one id is it. Measured on mocker A3: six of round 10's twelve
blockers were one decision that moved in the document during round 9 and did not
move in the prompt restating it; the most expensive instance carried, verbatim,
the document's own sentence warning that this half read alone is wrong. The
script is read as PROSE — its comments and the literal chunks of its strings,
each literal its own text — so a marker on a code line cannot make a rule site
out of `const GATE = { owns: [...] }`, and `${...}` interpolations leave a hole
in the sentence rather than splicing an identifier into it. A literal's ESCAPES
are decoded before anything reads it: `isn\'t` reached the comparison as a
backslash and a trailing `\n` sat between a full stop and the end of the
literal, so the sentence never terminated and glued itself to the next word.
That cost a live gate three iterations against the normaliser to state ONE rule
in a form both artifacts could be compared on, and on that same corpus decoding
it takes `dup` from 13 cross-file groups to 17 and from 10 divergent to 13.
Every decoded line keeps the SOURCE line it was written on: a literal spelling
three newlines on one line of source is three sentences of one line.

Nothing compared makes the header say `NOT A PASS` — ids all sitting at ONE
site, and an unmarked corpus, whose `0 marker(s), 0 id(s), 0 compared, 0
conflict(s)` reads like four numbers that passed: the note under it was missed on a gate carrying eighteen
markers and eighteen ids, because the other three numbers on that line read
like a pass. The class the markers exist for is a description and an
enumeration of one set disagreeing — mark the description AND the list.

Every conflict — here, and `dup`'s `NUMERIC`, `OPERATOR` and near-match groups
— is printed at the point the copies STOP agreeing, with the shared opening
trimmed to a few anchor words. Copies that conflict agree almost everywhere, so
truncating them from the start prints the same characters for every site: one
gate reported five sites of one rule as five byte-identical lines, and the
difference was found with `cat -A`.

### `wf check stale -since <ref>`

The sweep `wf apply` has been advising since the cost meter existed — "if this
batch changed a RULE, its consumers are NOT in this diff" — turned into a
command. It takes the OLD side of every replacement in `<ref>..HEAD` (default:
the newest freeze), keeps the tokens the fix RETIRED — removed and not put back
— drops the ones the corpus uses everywhere, and reports the sentences the range
did NOT touch that still carry them.

This is the class the other three structurally cannot reach: `dup` finds copies
that DRIFTED apart, `rules` finds marked sites of one id disagreeing, and
neither sees a paragraph arguing from a superseded rule in different words.
Measured on a live decision gate, roughly a fifth to a quarter of every round's
claimed closures were incomplete and almost always for this reason — the fix
corrected one copy of a rule and left another standing.

The mechanism is crude on purpose: rare tokens, a tenth of a second, and the
tokens themselves printed beside every hit so the judgement takes seconds. Two
populations are always printed — the retired vocabulary that was dropped as too
common (`-max-df`, default 3 — calibrated on a live gate's fix range, where 8 gave 26 hits carrying
"were", "right" and "stays" and 3 gives 8 hits over 5 retired tokens: the OUTPUT is narrowed, never
the check, and the dropped count is printed) and the
sentences the range itself wrote. A range that retired no vocabulary at all says
`NOT A PASS`: an addition-only fix is invisible to this check, and a green line
over it would be the vacuous pass this tool exists to refuse. A range that
retired plenty and finds none of it left is the opposite — a COMPLETE sweep, and
it prints as one ("N of M retired token(s) survive NOWHERE in the untouched
corpus"). Both leave the reported vocabulary empty, and printing the second as
the first told an operator who had reached every consumer that nothing had been
checked.

Which hit ARGUES from the old rule and which merely shares its vocabulary is
judgement. The check reports; it never rewrites.

### `wf check closure`

`wf round record` proves a `closed by` sha EXISTS. This asks the next question:
does that commit's diff touch what the finding NAMES. A file the commit never
opened is a closure that did not land where the finding said it would; a line it
did not cover is a question to re-ask, because a later batch in the same round
shifts the numbers.

**It resolves the SECTION a finding cites, not only the path.** A real ledger writes "D9's criterion
contradicts D13", never a line number: measured on that gate, 126 of 127 closures named no path at
all and nearly every one named a section, so a path-only check reached ONE finding of 127 — honest,
and useless. Any cited section, against the union of the closing commits: demanding every section of
every commit flagged 89 of those 127, and the surviving 12 read as real (each was a fix that landed
in the section PRODUCING the defect rather than the one cited, which is a question to re-ask). In a
one-artifact workspace a finding that names no path is checked against the only document there is,
and that fallback is printed.

Each closing commit is read in ITS OWN coordinates — a commit's old-side line numbers mean something
only against its own parent, so a closure carried by two commits is checked against each one's
sections and the claims are ORed, never unioned across parents. One verdict per closure, however many
paths and sections it names.

A FILE miss is a closure that did not land where the finding said it would. A SECTION or LINE miss is
a question, not a verdict. Findings that cite neither a section nor a line are counted and named as
checked only at FILE level, where a commit touching the document anywhere passes; findings naming a
path that matches two artifacts resolve to NEITHER (the ambiguity `check cite` also reports) and are
counted as unchecked; a round with no readable findings.md is an ABSENT measurement, and `-round N`
naming a round the workspace does not have is REFUSED rather than reported as a clean zero. Findings
that name no artifact path in a multi-artifact workspace are counted and named as NOT checked — the
check reaches a finding only where the finding names a path, so the checked
share has to be visible rather than assumed. Measured: two reviewer lenses,
told by hand-written prompt to ask exactly this, reported 6 and 4 of 24 closures
not closed, three rounds running; it was the most valuable half of those rounds
and it was produced entirely by prose instruction over data git already held.

### `wf round lens`, and the round nobody read

Two rounds print the same line. One dispatched its reviewers and they found
nothing; the other was opened, frozen and never dispatched. Both leave the
ledger `wf round new` scaffolded, both make `wf stats` say `0 finding(s),
0 blocker(s)`, and under the stop rule — the gate ends on a REVIEWER round that
produces no BLOCKER, and on nothing else — the second is a legal close.
Measured, and not hypothetically: one decision gate's round 9 was tagged
`gate-r9` over a blank scaffold and read as a zero for as long as nobody
dispatched it. When it was finally reviewed it returned four blockers, two of
them design decisions that a scope cut had deleted and that survived nowhere
else — a gate that had trusted that line would have shipped a document missing
both.

So the difference is made a RECORD rather than a guess. `wf round lens <prompt>
-name NAME -model MODEL -asks Q[,Q]` writes `rounds/rN/lens-<name>.md` — the prompt
verbatim, with what ran it — and `wf freeze` refuses while ANY closed round's
ledger is the untouched scaffold with no lens banked, `wf close` refuses to bank
one, and `wf stats` marks the line `ledger: UNFILLED scaffold, not a measured
zero` (and, when the lenses ARE banked, `ledger: empty, and its lenses are
banked — a measured zero`, which is the distinction the whole feature is for).
Every closed round is checked rather than only the previous one, because
`wf round new` does not require the current round to be frozen: a round opened
past without a freeze was examined by no refusal at all, and a gate closed
carrying an undispatched round that only `wf stats` ever named. A `-kind tool`
round is exempt: its opening subject already records that no reviewer was
dispatched. And a lens counts only if it is a RECORD — a `# Lens` name, a
`Model:` line, a prompt after the `---`, and a commit; the first version counted
a FILENAME, and `touch rounds/r1/lens-anything.md` cleared a refusal that had
just fired.

The roster is worth banking for its own sake. On the gate above, over its delta
rounds 2-8, the criterion vector was an external model at rounds 2, 4 and 6 and
an internal one at 3, 5, 7 and 8; the gate blockers were 17, 16, 12 against 10,
11, 6, 7 — 1.76x — and the two lowest-blocker rounds of the whole gate are the
two after the external lens was dropped for quota. (Round 1 is outside the
comparison because it is the full round against a first draft; its criterion
vector was external too, at 13 blockers, and including it gives 1.71x.) None of
that was recorded as data — it survives only as prose in eight findings headers,
so no command can read it and no report can print it, and every claim about
brief wording or model diversity made from those numbers had to be checked by
hand-reading them. One such claim in this repository was withdrawn after exactly
that reading. `wf stats` prints each round's roster and names the rounds
carrying none, `wf close` banks the prompts with the round (they are FILES in
`rounds/rN/`, which is what `close` copies and `prune` deletes), and a banked
prompt is never rewritten: the prompt a round actually ran with is the whole
point of the file.

### `wfcheck` and the target

`wfcheck -root <repo>` names the repository whose files a script's prompts
claim to own. Run inside a gate workspace, `-root` defaults to that workspace's
`target` and the report says so; anywhere else it is the working directory. The
default used to be the working directory always, which produced twenty-two
"owns X, which does not exist" warnings on a correct script — a script's owned
paths are relative to the tree under review, and the workspace is not it.

It also cross-references every `YOU OWN` path against the `owns` array of every
gate config in the script: a path no gate owns is a file a reviewer can be
briefed on and no fix agent may touch. The reach is script-wide, because a
producer and the gate reviewing it live in different phases by design.

A gate config is `owns` AND `reviewers` together — `owns` alone is a key an
author writes on an agent's own options object. The array is followed through a
name (`owns: closingOwns`) and a spread, and each entry is read for the PATHS it
names, because an entry is a sentence about a path rather than a path
(`internal/admin/server.go (the one comment, nothing else)`); an entry ending in
`/` owns the directory beneath it. What it cannot read it does not judge: an
unresolvable array, or an entry naming no path, leaves the remainder `NOT
JUDGED` and names the gate. It covers only what a prompt DECLARES — a file a
phase was ordered to touch and nobody assigned to anyone is invisible to it, and
that is judgement rather than a list.

### `wfdry` and the prompts

`wfdry <script> --prompt-diff <a>,<b>` prints the sentences one agent's prompt
carries and the other's does not. It lives in the RUNNER and not in the static
checker because that is where the prompts exist: measured on the P3a script, 14
of 15 are composed at run time, so a static reader sees one of them and the same
check built there would report nothing on the very corpus that produced the
class. Two agents given the same job are two copies of one briefing, and a rule
stated to one and not the other is reachable by nothing else here — `dup` needs
a rare token the two prompts share, `stale` needs something retired. Measured:
one gate's whole last round was that class, and its closing sweep — a hand diff
— found a sixth copy of one rule no reviewer lens had filed. Run over the
revision before that closing commit, the flag prints the three sentences the
commit added; run over the revision after it, none.

WHICH two agents were given one question is the operator's to say, and that is a
measurement rather than a shortcut. Ranking every pair by shared sentences was
built first: over seven tracked orchestration scripts it surfaces SIBLING
prompts — two lenses of one gate — at a similarity of 0.33 to 0.86, and the pair
that motivated the whole thing ranks BELOW every one of them at 0.07 to 0.12,
under every filter setting measured. Before a document-frequency filter it ranks
under the producers' shared preamble as well. A ranking that misses the case it
was derived from would be read as coverage.

`--dead <label>` and the `dead-reviews` mode now PRINT the labels they killed,
and `dead-reviews` matches a label's SEGMENTS rather than a substring of it: it
once matched `review` inside `preview`, killed a producer, ended the run at
section five and reported OK over a seventh of the agents it exists to exercise
— 7 agents and 6 phases against 24 and 15 on the same script — without killing a
single gate reviewer, which is its whole purpose. A `--dead` label that spawned in NO mode
FAILS the run: every guard behind it ran over the empty set, and unlike the
mode's own matcher, the operator typed that label. The mode's zero-kill stays a
note — a producer-only script has no reviewer label and is correct.

**`dead-reviews` runs the PASSING fixture, and a verdict that still reads as
success over an agent that returned nothing is a VIOLATION.** The mode exists to
isolate one variable — the reviewers reported nothing — and the failing fixture
destroyed it: every SURVIVING agent then reported failure too, so the run came
back not-green whatever the script did with the deaths. Measured 2026-08-27 over
the five correct scripts on disk and one carrying the defect: the mode returned
`green: false` on all six and separated nothing. Under the passing fixture the
five still return false — the deaths alone carry it — and the defective one
returns true and is refused by name. The arming condition is the KILL LIST, a
different field from the verdict it constrains, so a producer-only script that
kills nobody is not accused of ignoring deaths it never had. The banked shape is
`testdata/dead-reviews-green.mjs`: the emitted harness with two edits a live
operator plausibly makes — the reviewer relabelled off the segment the mode
matches, and a comment-justified filter dropping dead REVIEWERS from the green
computation. It passed seven modes. Where a label was killed, the `clean` mode's
own verdict guard stands down: it reads "no fixture in this mode can produce
that verdict", and a killed agent is exactly such a fixture.

**Every mode's line carries the `clean` mode's agent count beside its own, and a
mode reaching fewer than half of it FAILS.** `gate-checks.md` §E already
said a mode that spawns NOTHING fails, and one is not zero: measured on the gate
that stopped instead of closing, a red-bar abort added to the FIRST producer took
five of seven modes from 15/25/25/25/1/15/25 agents to 18/1/1/1/1/1/1 — the fix round, the
recheck, the acceptance step and the final verdict exercised by nothing — and
every mode still exited OK, so a reviewer banking "wfdry 7 of 7" WOULD have credited a layer
that covered a single agent. The RATIO is the signal, not the absolute.

**A mode that never reached one of the happy path's PHASES fails too, and the
phase is NAMED.** The two checks are additive because they miss opposite shapes:
the banked `collapse.mjs` narrows to one agent inside ONE phase, which a phase
check cannot see, and a branch that skips a whole section while keeping its
agent count within half the happy path's is invisible to the ratio — measured on
such a shape, 13 agents against 15 and 4 phases against 5, so the ratio passed
while neither the fifth section nor its gate ran in any mode, and `wfdry`
reported 7/7 at exit 0. The banked shape is `testdata/section-collapse.mjs`. The
comparison is over distinct phase TITLES and never their count, because a mode
running one phase twice reaches the same count having covered less. The
threshold is EXACT rather than a tolerance, and that is a measurement: across
the six correct scripts on disk every mode but `throw-serial` reaches its
baseline's phase set exactly (58 phase calls, 58 distinct titles), so a
tolerance would be calibrated on a population with no variance in it.
`throw-serial` and `no-args` are exempt for the same reasons the ratio exempts
them, and naming the missing title is the actionable half — "fewer phases" sends
an operator to diff two runs by hand.
Failing rather than warning was measured: across the three correct gated scripts
on disk the failure modes run WIDER than the happy path (36 against 44, 18
against 30, 15 against 25), so none of them trips it, while the one revision
carrying the abort does. `throw-serial` is exempt (it aborts AT the first spawn
by construction), and so are `no-args`, which the runner narrows on purpose, and
a mode that spawned nothing, which the zero-spawn rule already judges. A run with
no `clean` mode says its baseline was a stand-in — or, for a single `--mode`,
that nothing was compared at all, which is where the check used to vanish in
silence. The shape is banked as `testdata/collapse.mjs`, and `make check-tools`
fails if the runner exits 0 over it or stops printing the baseline.

The diff refuses what it cannot answer rather than answering it wrongly: two
spellings of ONE prompt (a text does not differ from itself — the first version
printed "nothing is stated to one and not the other" over `accept,accept`), a
label spawned with several DIFFERENT prompts, and a pair where a prompt
contributed no sentence at all, which prints `NOT A PASS` rather than agreement.
Sentences are compared with the operator skeleton the normaliser erases, so
`retry_count == 3` and `retry_count != 3` are two sentences and not one.

## Boundaries

The workspace is **never inside the repository under review**. The gate's
agents diff that tree against its own HEAD, and the rule "the tree stays
untouched while a run is live" does not survive orchestration commits into it.
`target` is read-only.

The checks take the mechanical share of blockers — about a fifth — and never
impersonate coverage of the rest. That is why `check cite` always prints how
many citations carry no anchor: a green run over 148 unanchored citations
proved nothing.
