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
wf check                                   # cite + dup + rules + closure + stale (from the newest freeze), one summary line
wf check schema -since gate-r10            # what an agent's schema stopped requiring
wf round ingest lens-*.out.md              # each lens's LAST table into the ledger, tagged [lens: NAME]; dedup by hand
wf round record rounds/r1/findings.md      # the round's ledger, closures validated
wf apply -f fixes.json -m "close r1 blockers" -closes 1,2   # the batch, and its commit into those rows' closure cells
wf round lens criterion.md -name CRITERION -model "external gpt-5.6-luna" -asks criterion   # what reviewed it
wf freeze                                  # hashes + target HEAD + tool version → commit, tag gate-r1
wf verify                                  # the pre-launch check: what runs is what was reviewed

wf delta                                   # the delta round's brief: findings + closure checklist + diff
```

## Commands

| command | what it does |
|---|---|
| `wf init <name> -target <repo> [-dir D]` | workspace: git init, `gate.json`, `artifacts/`, `rounds/`, first commit. It REFUSES a workspace root that resolves under `-target` — including one that IS the target — and names a `-dir` outside it, because the operator's natural working directory is the target and the mistake is silent from there: measured on the first gate against a target other than mocker, `wf init <name> -target <that same path>` put the workspace at `<target>/<name>` and the first thing that said so was `?? <name>/` in the TARGET's own `git status`, which every agent of a later run would have read as an untracked directory the run did not create. Both paths are resolved through symlinks and the nearest EXISTING ancestor of the root, so a target reached through a symlinked parent does not slip past; a path that cannot be resolved at all is not refused, and the refusal happens BEFORE the directory is created |
| `wf add <file>...` | bring an authored artifact under the workspace and commit it; adopts an untracked draft already in place |
| `wf status` | manifest, current round, who last touched each artifact, dirt, tool version |
| `wf apply -f <edits> [-m msg] [-rule RN\|"name"] [-rule-ack] [--dry-run]` (the file may also be the single positional argument: `wf apply fixes.json`, which a gate typed and which used to print a usage line naming the flag they had passed — it was the one command still using `flag.Parse`, so a positional silently dropped every flag after it) | all-or-nothing batch of edits (JSON or text blocks), then a commit of only the touched files. It NOTES a replacement whose `old` covers half the file or more, with how many lines that one replacement rewrites: all-or-nothing and `count` are per REPLACEMENT, so a whole-artifact block has a count of one that always matches and every change inside it is verified by nothing — measured on P3b, eighteen of sixty-eight batches were a single replacement, the changes inside them came from a pipeline whose non-matching `str.replace()` is a silent no-op, and a later round found 15 of one round's 30 recorded closures byte-identical to their pre-fix text (a second lens confirmed 11). A note and not a refusal: generating one minimal block per change failed twice on uniqueness on that same gate; `-rule` declares the `[RN]` rule this batch changes and the note then NAMES the other sites carrying that marker, marking the ones in files the batch wrote — a rule nothing marks is said out loud, because that is exactly the corpus `wf check rules` cannot reach either. With the id form the batch is REFUSED before anything is written when a marked site sits in a file the batch does not touch, and `-rule-ack` is the waiver: a note arrives after the round has spent its edit, and the sweep it advises is then a separate act of discipline. Measured on P3c, four times in four consecutive rounds the fix landed where the finding was READ rather than everywhere the rule ACTS, and `dup`, `stale` and `rules` reach none of them because the copies share no wording — but this refusal would have caught NONE of those four: that gate marked nothing, so `wf check rules` reports `0 marker(s), 0 id(s), 0 compared` and the id form reaches nothing there, and three of the four are copies inside ONE file, which is behind the FILE half. It is an argument for marking, not a mechanism that caught them. A second site inside a file the batch DOES write is not refused, and the site list beside the refusal is what reaches those. It also takes a free-text rule NAME, which sweeps every sentence carrying all of its words and says out loud that it is the weaker half: a script gate's rules live in prompts and are marked in no document, so the id form reaches nothing there — measured on P3a, `-rule "bar accounting"` was refused outright and the flag was unusable for the whole gate whose most valuable sweep it is |
| `wf apply -whole <path> [-m msg] [-rule RN\|"name"] [--dry-run]` | adopt a file the operator rewrote in the worktree: the diff against HEAD IS the batch, its HUNKS at `--unified=0` are the edit count, and the commit carries both the meter's trailer and a second one marking the act. It is NOT a batch this command verified — `-f` rejects a batch whole when one replacement misses and checks each declared occurrence count, and neither reaches a rewrite it never watched being made, so the note says so on every run and `wf stats` prints `(N ADOPTED whole, verified by no batch)` beside the round's ratio. It exists because the METER loses the round otherwise: measured on P3g, a structural merge of two script sections is forty-odd non-adjacent edits in one file, it was applied with `git commit`, four of thirteen rounds contributed no edit count, and the cost meter priced the gate on the rounds that used the block format. Refused on a file byte-identical to HEAD (an edit count from an unchanged file prices a round that changed nothing), on anything that is not a REGULAR file (a directory passes every other check and would be committed whole under a report naming one file), on a path not in HEAD (that is `wf add`, and routing a first draft here would record it as a round's edits — tested with `ls-tree HEAD` and not `ls-files`, which reads the index and let a staged new file through), on a path escaping the workspace, and together with `-f`. `-rule` behaves exactly as it does under `-f` — the same refusal before anything is committed, the same `-rule-ack` waiver — because a guard that differs between two spellings of one command is a specification defect. Two quantities are pinned against the host's git config for the reason `gitx` pins the diff algorithm: `diff.interHunkContext=0`, because two changes four lines apart merge into one hunk at 3 and halve the recorded edits, and the changed-line count is taken against HEAD rather than the index, because a rewrite the operator already staged reads as zero changed lines |
| `wf apply … -closes 1,2` | either form above: fill those findings' `closed by` cells with the batch's OWN commit, in one ledger commit after the batch's. Validated before a byte is written — the round is open, each number names exactly one recorded row, none is already closed (that one is `wf round close -replace`'s) — so a typo in the list leaves the batch unwritten rather than committed without its closure. Measured on A4: 70 of the workspace's 143 commits were one `wf round close` each, and eight of one round's closures named the previous LEDGER commit because the operator read `HEAD` after each call and fed it to the next; a sha nobody reads back cannot be the wrong one |
| `wf round new [-title T] [-kind full\|delta\|tool\|run]` | open the next round, scaffold `rounds/rN/findings.md` with its scope. `run` marks a round that CONTAINS a live run of what the gate built: it is a reviewer round everywhere except the clock, where its span is printed on its own, kept out of the median, and split under the head `run` rather than `review` — one gate's launch round spanned 288.2 min against a median of 45.3, and `-time`'s note called it a break rather than work |
| `wf round ingest <report>... [-round N] [-lens NAME]` | append the rows of each reviewer report's LAST table to the round's ledger — numbered on from what it holds, each tagged `[lens: NAME]` (the file name with `lens-` and the extension stripped, or `-lens`), any closure cell the lens filled dropped and counted — and commit. Every report is refused before a byte is written on a malformed row, a level off the closed scale, a report with no table (a vector that returned nothing: bank it, do not read it as clean) or an empty one. The judgement half stays by hand in the ledger — two lenses filing one defect become one row, each level re-derived under section 0 — then `wf round record`, which is a no-op over bytes already committed rather than a git failure |
| `wf round record <file>\|- [-round N]` | fill a round's findings.md from a file or from stdin (`-`), validating closures before writing; the file is POSITIONAL (there is no `-file`); with `-round` absent the command NAMES the round it inferred and the flag that overrides it, because a note meant for the round just reviewed lands silently in the one `wf round new` has since opened, and it stays quiet when the round was typed; an empty input is refused rather than recorded over a filled ledger; a row that does not split into four cells is refused by NAME and, when it splits into too many, the refusal gives the COLUMN of the FOURTH unescaped pipe — where a well-formed row's `closed by` cell opens — in characters from the start of the line as written, and quotes the tail it could not place (which of the extra pipes is the author's is not knowable from the row, so the tail is printed whole and the injected one is inside it; a row with too FEW cells names no column at all) — two gates have paid for an unescaped pipe, and on the second it was not even authored: a shell hook rewrote a command quoted inside the heredoc writing the table and injected an alternation pipe into a row's text, so the operator hunted a character they had never typed with `awk` and `tr` (an edit file and a findings file are DATA — write them with a file tool, never through a heredoc something may rewrite); the `level` cell must be one of **BLOCKER, MAJOR, MINOR, NOTE** and anything else is refused before a byte is written — a finding's level is decided by what catches it downstream, never by how bad it reads, and measured 2026-08-25 over the six gate ledgers then on disk, the open scale had produced FOUR vocabularies for one scale, 172 of 1065 findings at a level no other gate can be read against (the corpus has grown since; the shape has not), and a script `enum` does not save the other side: it validates the VALUE and never the LIST, so the scripts those six gates produced carry three lists of their own plus a drifted field NAME (`level` on one, `severity` elsewhere), including the `['blocker','advisory']` this repository's own template shipped into three gated scripts. Every READ path stays permissive so the closed workspaces keep being readable; a `closed by` sha the workspace does not have is refused by an error that names the repository it looked in and says when the sha IS a commit in the TARGET (the shape a round recording a LAUNCH reaches for); prints a note when the round banks no lens — measured: eleven of one gate's thirteen rounds recorded their findings and lost the prompts that produced them — and, on a DELTA round, what its banked roster does not cover, through the same classifier the roster and `wf stats` read. It also counts the closures `wf check closure` will NOT be able to reach — those whose finding text names no artifact path — at the moment the operator is still writing the file: measured on P3a, 0 of 350 claims checkable on the first record and 26 of 26 after the paths went in |
| `wf round close <finding#> <commit>\|-prose "why" [-round N] [-replace]` | fill ONE finding's `closed by` cell with the commit that closed it, rewriting the cell in place and never splitting the row into fields. It is the only bookkeeping step of the round loop that had no command behind it, and the gap cost a live gate two corrupted ledgers: the table escapes a literal pipe as `\|` — the escape exists because an unescaped one once scored an open BLOCKER as "closed by prose" and froze the round — and every field-splitting tool splits on the escape too, writing the commit id into the middle of a finding's text. The sha is validated as a commit in the WORKSPACE before a byte is written (the same refusal `wf round record` gives, naming the target repository when the sha is a commit THERE), the row is re-parsed from the bytes about to be written and refused if the number, the text or the closure reads back differently, a `#3` is accepted as `3`, a commit that touched NO artifact is refused naming what it DID touch (a closure names the commit that fixed the artifact, and one changing only `rounds/` is bookkeeping — eight of one round's nine closures on a live gate named the ledger commit of the previous `wf round close`), the scaffold's empty row is refused as a form rather than a claim, a duplicate finding number is refused as ambiguous, and a cell that already names something is refused without `-replace` (and `-replace` naming the same commit is refused before anything is written, because it would write identical bytes and die on git's `nothing staged`; the comparison is between COMMITS, so an abbreviation or a different case of the same sha is caught too). With `-round` absent it NAMES the round it inferred, and stays quiet when the round was typed. It prints the check that reads the cell it just filled. `-prose "why"` is the other door, for the row no commit closes — a NOTE asserts no failure and has nothing to close, and `wf round record` has always reported `closed by prose` as a first-class category, so the harness knew about a closure this command could not write: one gate paid three round-trips to the hand ledger edit this command exists to remove. It takes the same read-back, refuses a newline (a row is one line), escapes an unescaped pipe rather than refusing it, and refuses prose carrying a sha-shaped token — which every reader downstream would read as a COMMIT claim — asking instead for that commit or a rewording. It says in the same breath that a prose closure is checked by no command, and names the row's level when that level is BLOCKER |
| `wf round lens <prompt-file> -name N -model M -asks Q[,Q] [-round N]` | bank one reviewer vector against a round — the prompt verbatim and what ran it — into `rounds/rN/lens-<name>.md`; no argument prints the roster. `-round` must name a round the gate has opened, and with the flag absent the inferred round is named on the banking path — not on the roster branch, which leads with that number already. A banked prompt is never rewritten, and one left uncommitted by a failed commit is committed on the next run. It is also how a round whose vectors ran and found NOTHING says so, which is what separates it from a round nobody dispatched. A prompt still carrying the `wf new-lens` marker line is refused for the same reason an empty one is: the scaffold's fixed half banked with its VECTOR unwritten records that a lens ran and not what it was aimed at, which `-name`, `-model` and `-asks` are each already refused for and the prompt was not. A file counts as a record only if it says which vector (`# Lens`), what ran it (`Model:`) and what it was asked (a prompt after the `---`), and only once committed — the roster is the escape from two refusals, so a `touch`ed filename must not be one. `-asks` takes the set however it is written — `closure,fix-induced,ripple`, `closure+fix-induced+ripple`, a space-separated list, or the flag repeated — because one gate banked a single vector per round and then hand-edited `Asks:` in the file nineteen times over ten rounds, having been refused the natural spelling. It is the COMPARABLE half beside a free-text `-name`: the set is closed (`orchestration, routing, alignment, facts, seams, criterion, closure, fix-induced, ripple, other`) and an unknown value is refused, while `other` is always available so a vector a slice genuinely needs is never refused — measured 2026-08-25, when two gates banked any roster at all: 13 lens RECORDS under TEN distinct names, of which exactly one appears as a VECTOR NAME anywhere in the manual. The corpus has since grown — SIX gates bank 170 records under 59 distinct names, counted 2026-08-29 — so read the date, not the figure. A delta round asks CLOSURE, FIX-INDUCED and RIPPLE, and may add vectors but never drop those three [R3]; the roster and `wf stats` NOTE what a delta round's roster does not cover, and never refuse it |
| `wf freeze [-refreeze]` | sha256 of all artifacts + target HEAD + tool version + citation counts + the corpus size in lines (with the delta against the previous freeze — a document that doubles while the blocker count holds is a design being elaborated rather than settled) → `rounds/rN/freeze.json`, commit, tag `gate-rN`; refuses while a blocker of the round being closed stands open; notes (never refuses) every citation that no longer RESOLVES against the target's HEAD, a round whose artifacts changed without `wf apply`, and — when it could not resolve them, or the round's window has no lower bound — that the check did not run. Refuses when ANY closed round carries an untouched findings scaffold and no banked lens: a round nobody is recorded as having read must not freeze as a round that found nothing (a `-kind tool` round is exempt — its opening subject already says no reviewer was dispatched; the OPEN round is exempt because its scaffold is blank by construction). Every closed round, not just the previous one — `wf round new` does not require the current round to be frozen, so a round opened past without a freeze was seen by no refusal at all. `-refreeze` republishes a round already tagged: freezing stays one-way by default (the manifest is read from the WORKTREE by `wf verify`, so a silent rewrite turned a MISMATCH into "clean"), but `wf check cite` surfaces a mis-aimed anchor without a freeze — what only freeze does is REPUBLISH the corrected bytes under the tag reviewers were handed, and five of one gate's twenty-two rounds deleted the tag by hand to do it, twelve times in all (round 13 six times, round 15 five). The superseded manifest is read from the TAG, never from the worktree copy this command rewrites: a re-freeze whose tag move failed otherwise left a NEW manifest under an OLD tag, and the retry compared it against itself and refused. The tag MOVES, the manifest records how many freezes the round has had and which commits they published, and it refuses when every artifact hashes the same against the same target revision: a re-freeze republishes CHANGED bytes. A re-freeze also prints how many changed line(s) the tag just moved over: every window in `wf stats` is bounded by `gate-rN`, so a batch applied after the round's FIRST publication is priced as the fix that produced that round's findings, and `wf stats` excludes such a round from the prediction basis by number. That detection reads GIT — the tag's commit against the round's first freeze commit — and not the manifest's `refreezes` counter, which only this flag writes: measured on the gate that stopped instead of closing, twelve extra freeze commits over five rounds left it ABSENT from all five manifests, the note fired not once, and 611 of 8388 changed lines (7.3%) were priced against the round that produced them rather than the round they repaired |
| `wf verify [-round N]` | the pre-launch check: the tree is byte-for-byte what was frozen |
| `wf delta [-from REF] [-to REF] [-save]` | the round's findings, the closure checklist, and the artifact diff; `-save` commits the brief into the open round's bookkeeping. WITHOUT `-save` the brief exists nowhere, and a lens told to read `rounds/rN/brief.md` finds nothing there — measured on P4a round 15, where the reviewer reconstructed it from the previous ledger and the tag diff and reported the gap. So a run that printed a brief into a round holding no `brief.md` says so on stderr (stderr, because stdout is the brief), and `wf round new` names the command when it opens a delta round. Ends with a `## deletions` section when the range REMOVES structural leads (headings, numbered items, list items, bold lead-ins): a count and deliberately no list — measured over the two gates that cut, the per-round removals reach 49 on one and 215 on the other, and most are rewrites, so a list would paint most of its output. A cut's deletions are watched by no check here, and the section points at `wf check stale`, whose `swept` line is the same fact from the other side. Do not look for a cut in a net-negative diff: one measured cut replaced 28 clauses with 10 properties and the artifact GREW by four lines |
| `wf check [cite\|dup\|drift\|rules\|stale\|closure\|schema]` | deterministic checks (no argument = cite + dup + rules + closure + stale from the newest freeze — SKIPPED and said so before the first freeze — ending in one `## check —` summary line naming which found something and which compared NOTHING — `nothing compared (NOT A PASS)`, never `clean`, over an empty population); `dup` takes `-since REF`, `-threshold F` and `-min-words N`; `stale` takes `-since REF` (default the newest freeze) and `-max-df N` (default 3); `closure` takes `-round N` (refused when that round does not exist); `schema` takes `-since REF` (default the newest freeze) and RUNS each `.js`/`.mjs` artifact, so it is not part of "all". A `.js`/`.mjs` artifact is compared as PROSE — its comments and string literals, never its code — and every report says so; a script whose prose is empty is named rather than counted as clean |
| `wf stats [-time]` | per-round findings, closures, apply batches, edits and changed lines (with findings per 100 changed lines — the batch cost meter; a round whose window contains the commit that CREATED an artifact says FIRST REVIEW before its counts and prints no ratio at all, because it prices a corpus its batches did not write and a descent before it says nothing about it — on a `-kind tool` round the same line says an artifact was added and NO reviewer was dispatched, because that round reviewed nothing by declaration), each round's frozen corpus size and its growth, the prediction basis `wf apply` reads (narrower than the gate total, and the difference printed rather than reconciled; a workspace with no apply commit is named as an absent population, a `-kind run` round is excluded and named — its findings come from EXECUTING what the gate built while its lines are only whatever the fix touched, so the two halves of the ratio are about different things, and P4a's run round 14 priced 4 findings against 5 lines, 80.0 per 100, inside a basis of 5.5 (the same round prints `no ratio` on its own line for the same reason) — and a round whose window cannot be attributed has its numbers omitted rather than filled from the whole history — where every candidate is excluded the line reads `NOT A PASS, no round is priceable` and still names the exclusions, because a line that VANISHES cannot say whether a gate has no basis or lost every round it had), the opt-in `[fix-induced]` share (with the rounds carrying no marker named, so the share reads as the lower bound it is) and the same marker read over BLOCKERS — per round beside the count, as a gate total, and as one line naming every round whose blockers are a MAJORITY self-inflicted, because a flat descent is what the fix rate matching the introduction rate looks like from outside (measured: 100 of 177 blockers and 11 of 22 rounds on the gate that stopped instead of closing, 22 of 79 and 12 of 28 on two others), the rounds whose `gate-rN` was PUBLISHED MORE THAN ONCE (derived from git — the tag's commit against the round's first freeze commit, never the manifest's `refreezes` counter, which only the sanctioned path writes — with the changed lines the move re-attributed; such a round prints no per-round ratio and leaves the prediction basis by number, and `wf apply` names it there too), the rounds that applied no batch at all, every run of three or more rounds at exactly ONE blocker, the rounds whose findings.md could not be read at all (an absent measurement, not a measured zero), the rounds carrying an UNFILLED findings scaffold (an empty form, not a measured zero — the shape under which a gate can end without anyone having looked), each round's banked lens roster and the rounds that record none (tool rounds excluded from both, for the same reason), and reviewer-vs-tool round totals, from the ledger and git; `-time` adds each round's wall clock split into review / fix / bookkeeping tail, plus the authoring window and the median round — over every round except the `-kind run` ones, whose spans print on their own lines (the population is narrowed by nothing else: a tool round has always been in this median, and the medians published from these gates were measured over it) |
| `wf round new [-title T] [-kind K]` and `wf freeze` | ...and both now name a workspace `gate-checks.md` that is no longer the reference the binary carries — or the absence of one at all, because no pin is not a current pin — listing the sections the pin predates — a note, never a refusal: one gate reads ONE version of the rules, and measured on P3a a sixteen-round-old pin briefed two rounds of reviewers without the closed severity scale, found by a human asking rather than by a check |
| `wf new-script <name> [-target REPO] [-o FILE]` | emit the orchestration-script harness — wrapper, schemas, per-section gate, liveness ledger, read-only accept, tally; `wfcheck`-clean and `wfdry` 7/7 as emitted, and re-verified by `make check-tools`. Never writes over an existing file. Inside a workspace it lands in `artifacts/` and says it is UNTRACKED until `wf add` adopts it: it used to land in the working directory, which is the workspace ROOT when the operator is standing in it, and a stray file there is exactly what `wf verify` refuses — the command created the state the next command objects to, and the copy-and-delete was paid by hand |
| `wf new-lens <name> [-round N] [-kind delta\|full] [-asks Q,Q] [-o FILE]` | emit the FIXED half of a reviewer's brief — the reading list by path (the workspace's pinned `gate-checks.md` with sections 0, 0b and 0c named, the artifacts, the round's `brief.md`, named EITHER WAY on a delta round and carrying `run wf delta -save` when it is missing — a reading list that omitted it silently would send the operator to the launch without one), the three asks a delta round may never drop, the closed scale taken from the binary, and the output contract. `-asks` is validated here and carried into the `wf round lens` line printed at the end; it does not change the brief. `-o` is refused into ANY `rounds/rN/` — a `lens-*.md` there is the shape the roster scans, so an unbanked scaffold reads as a vector that ran and `wf verify` reports it, and `-o rounds/r5/x.md` would CREATE a round directory nobody opened — and into the read-only target, both compared as RESOLVED paths: the first version compared a relative `-o` against an absolute round directory, so the one spelling an operator types went straight through. A brief landing INSIDE the workspace is accepted and says so, because `wf verify` will report it as uncommitted under advice that is wrong for a scaffold. It does NOT restate the scale or what a row must carry: it points at sections 0 and 0c of the pinned copy, because a restated rule goes stale in place — measured over the 309 banked lens prompts of eleven workspaces, the lines recurring in five or more of them are 29.9% of 16 380, 200 name `gate-checks` and exactly ONE names `wf docs` — see `wf docs measured` [M-37] for the command, because the corpus is live and these move. The VECTOR is left as a marker line and `wf round lens` refuses to bank a prompt still carrying it. Refuses to write into `rounds/rN/`, where a file is what clears the freeze and close refusals that demand a round was read |
| `wf close [-archive DIR] [-artifacts=false]` | bank the gate's own record — `history.tsv` (commit times and subjects), `stats.txt` (`wf stats -time`), `freezes.tsv`, every round's findings, and every file of every round, and a feedback scaffold with the derived numbers prefilled; a filled scaffold is never overwritten. Refuses while ANY round carries an untouched findings scaffold with no banked lens, and names a last round that dispatched no reviewer at all (a `-kind tool` round is exempt from the refusal and is instead NAMED, because its opening subject already records that no reviewer was dispatched). the artifacts themselves are banked too, because their CONTENT is the one thing `prune` destroys that nothing else holds; `-artifacts=false` opts out, and `prune` then names each artifact before removing it |
| `wf prune [-yes]` | delete `artifacts/`, `rounds/` and then `.git` once the record is banked; refuses without one, refuses a stale one (including a record committed together with the work it describes), and reports without `-yes`. Measured on one closed gate: 4.2 MB down to 696 KB, and the record holds the artifacts, every round's files, the commit log and the stats |
| `wf docs [name]` | print the embedded operating manual — `guide` (the manual in reading order — every name below but `checks` and `measured`), `authoring`, `routing`, `interview`, `preflight`, `harness`, `rounds`, `section-gate`, `cost`, `checks`, `measured` |
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
fifteen headers. The header's fields are written BARE, as in the example above:
where the manual renders them `[count=N] [note=...]` the brackets mean optional,
inside a format whose other delimiters are literal. An operator read that the
other way and the two halves failed differently — `[count=1]` was refused as an
unknown field, naming the right spelling but not the mistake, while `[note=x]`
was accepted SILENTLY with the bracket inside the note. Both are now refused by
a message that says to drop them, and a note whose free text QUOTES the
bracketed form still parses, because a note runs to the end of the line. A bare `<<<<`, `====` or `>>>>` line INSIDE either half is
REFUSED at parse, naming the line. The rule used to be that a marker collision
"mis-splits and then fails the plan loudly, because the mis-split old matches
nothing", and that holds only for the OLD half: in the NEW half it matches
everything and is WRITTEN. Measured on the P3d gate — a forgotten `>>>>`
swallowed the following block, every edit reported `ok (1 match)`, the batch
verified byte-for-byte (the verification is against the batch AS PARSED, so a
mis-split is confirmed in the same words a correct batch gets), the literal
markers landed in the artifact and took `wfcheck` from 0 errors to `Unexpected
token ===`, and the recovery was two `git revert`s and about half a round. Text
that must legitimately carry such a line — this README documents the format and
does — is the one case for the JSON form. When a batch changes enough LINES that the workspace's own
history (findings per 100 changed lines, from the ledger and the apply commits)
predicts a costly next round, `apply` prints a note suggesting a split by rule,
and says that a rule's consumers are not in the diff — a note, never a refusal.

`-rule RN` turns that sentence into a step. The batch is REFUSED before anything
is written when a marked site of `[RN]` sits in a file the batch does not touch,
and `-rule-ack` is the waiver for a batch that legitimately corrects one site
while the others stand; either way the report names every site of `[RN]` across
the artifacts, marking those in files the batch wrote, so the copies that were
NOT touched are on screen at the moment the fix lands. It is the FILE half and
says so: a second site inside a file the batch DOES write is not refused, and the
site list is what reaches those. Measured on mocker A3: what predicted the next round's cost across three
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

**`-at frozen` verifies against the bytes reviewers were handed** — the target
revision the newest freeze manifest names — and `-at <rev>` takes any revision;
the header always says which tree it read. A `-kind run` round moves the target
by everything the run wrote, so every citation in a document frozen before it
resolves a few lines late: measured on P3c, **16 findings against the working
tree and ZERO against the frozen revision**, which its operator had to establish
by hand and write into the round's ledger. When the target has moved and there
are findings, the check says so, names the round it was frozen against, and names
that round's kind when it is a `run`.

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

**A word the fix only INFLECTED is not a word the fix retired.** Tokens are compared by a fixed
five-rune prefix, which is no stem at all for anything shorter, so `work` and `works` never met: on a
live gate a fix rewrote "consumers work" as "consumer works", the bare token left the text, and the
check reported four untouched sentences reusing a common English verb — all 8 of that gate's survivor
reports were this class. A removed token is now also dropped when the range put the same word back
with one of a closed list of English endings (`-s`, `-es`, `-ed`, `-d`, `-er`, `-r`, `-en`, `-n`,
`-est`, `-st`, `-ly`, `-ing`), including the doubled final consonant English writes before some of
them (`stop`/`stopped`, `run`/`running`), in either direction and with the shorter side down to three
runes (`box`/`boxes`). The list is a WHITELIST and not a length bound because a bound alone pairs
`within` under `with`, `someone` under `some` and `formula` under `form`: over the 160 ranges of those
eleven workspaces the shipped predicate suppresses 171 removed tokens and a bare bound 203, the 40 it
reaches alone are a different word every time, and eight run the other way (`running`, `putting` and
their kin, four runes of growth). That count is a banked probe, not a hand reading —
`WF_PROBE=1 go test ./internal/check/ -run TestCorpusInflectionPairs`. Corpus-wide, retired tokens
fell 556 to 484 and reported sentences 768 to 663 over 160 ranges, and no range got noisier. **The
narrowing is counted in the report** — this suppression and the five-rune stem's own, which was
silent before — because a candidate population dropped in silence is one nobody can argue with.

The mechanism is crude on purpose: rare tokens, a tenth of a second, and the
tokens themselves printed beside every hit so the judgement takes seconds. Three
populations are printed — the retired vocabulary that was dropped as too
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

That COMPLETE sweep has a second reading this check cannot separate from the
first: a retired word surviving nowhere means the fix reached every consumer, or
the decision those words carried stopped existing and nothing replaced it. The
line says both. A round that CUT reads it the second way — measured on P3c,
where a clause split in two by round 3 was dropped by the cut in round 5 while
the survivor kept the mutation requirement the deleted half carried, and it cost
a second launch to find.

Which hit ARGUES from the old rule and which merely shares its vocabulary is
judgement. The check reports; it never rewrites.

### `wf check schema`

What every agent's output schema OBLIGES its agent to fill, compared between a
reference and the working tree, per label. A DROP is the finding; a rise is
printed and is not, because a schema requiring more of its agent is the
direction this check exists to protect.

It answers one measured shape, twice. P3d narrowed a schema for the width
classifier and left a stale band behind; P3e, narrowing one for the same reason,
lost `required: ['met','evidence']` from the leaf of a twenty-two-key acceptance
report — in a hunk whose own comment was entirely about width, with no warrant
recorded — and one frozen revision published a report whose every property could
come back `{met: true}` with no evidence at all, which was the only thing that gate's evidence reviewer had to
read. Nothing static reaches it: the leaf is built inside an
`Object.fromEntries(...map(...))` callback, so `wfcheck` sees no literal, and
both static tools were clean over that script. The RUNNER holds the composed
object, so this check runs it — which is why it is not part of `wf check` with
no argument.

The width prints beside the drop deliberately: that is the correlation the
corpus has, and both instances happened INSIDE an edit whose stated purpose was
size. On P3e's own history `accept` goes 74 required keys at the round-10
freeze to 33 at round 11, at a width that fell from 3,869 to 3,314, and
`wf check schema -since gate-r10` prints exactly that as one DROP line, with the
second population beside it — 1 object requiring none of its properties to 23 —
because the two move TOGETHER and each is half the statement. They are judged
INDEPENDENTLY: reporting the no-required rise only where the required total was
unchanged made it unreachable whenever the total moved, so an edit stripping
`required` from every leaf while the root's own list GREW printed `rose` at exit
0 — this check's own shape, arranged so the total does not fall. A label that appeared or disappeared is NAMED and is not a drop;
a side whose schema does not serialize has no census and prints NOT A PASS,
because comparing an unmeasured -1 against a real count would file the largest
finding in the report over a schema nothing measured.

### `wf check closure`

`wf round record` proves a `closed by` sha EXISTS. This asks the next question:
does that commit's diff touch what the finding NAMES. A file the commit never
opened is a closure that did not land where the finding said it would; a line it
did not cover is a question to re-ask, because a later batch in the same round
shifts the numbers.

**A section the finding MENTIONS does not preempt the line it CITES.** Both halves are asked and the
verdict is their OR: a finding citing `decisions.md:607-611` that also names D11 in passing used to
be reported as missing D11 while the commit had covered the very lines it cited. Measured over the
fourteen unpruned workspaces on the box that built this — 92 miss rows before, 85 after, nothing
added — and where both halves miss, the row now carries both reasons, one verdict per row.

**It names the two populations it is not about.** The rows carrying NO commit — an empty closure
cell, or prose — are counted and reported apart, split the way `wf round record` splits them
(`open` and `by prose`), because the two commands were read side by side on a live gate and only one
of them said what it was counting: `open 1` beside "1 closure claiming a commit, 1 checked, 0 not
closed by that commit", both true, and together they read as a gate that checks out while the freeze
was about to refuse. And the closures whose commits were an ADOPTION (`wf apply -whole`) are named
by row, because the tool watched nothing being made there: `wf stats` already draws that distinction
per ROUND and the closure an operator has to trust is per ROW — measured on the gate that asked for
it, exactly one closure was banked in the ledger and never written into the code, and this check
passed it correctly by its own rules. The list is grouped BY ROUND, which is the
narrowing it needed before shipping: measured across the twelve unpruned
workspaces it fires on two of them, and on P3f it reaches **47 of 121 closures
across seven rounds** — an unwrapped line of 47 ids under an instruction to
byte-verify each is a work-list nobody reads. Neither line is a failure and
neither is counted into the miss total. Both lines are silent when their population is empty: the `-whole` trailer is newer than most
of the corpus, so a printed `0 rest on an adoption` over a workspace that predates it would be an
absent measurement in the register of a measured one.

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
sections and its own mapping of the cited span, and the claims are ORed, never unioned across parents. One verdict per closure, however many
paths and sections it names.

**A citation is a RANGE and the LINE half asks about the range.** Measured over the eleven gate
workspaces on disk, 96 closure citations carry a `:120-134` span against an artifact-shaped path (62
of them written with the `artifacts/` prefix, the widest 52 lines) and every one writes an ASCII
hyphen; reading only the first number asks whether the fix touched the FIRST line of a region the
finding drew. On one gate ALL FOUR of its line-level misses were that — `p3h.mjs:454-459` reported
not closed by a commit whose hunks covered 450-453 and 455-457, because 454 is the one unchanged line
inside the span and `--unified=0` splits a region there — and every one was closed. An edit anywhere
inside the cited span closes it, the row names the SPAN the finding wrote rather than the line the
check reduced it to, and an INVERTED span (`:459-454`) keeps its start alone rather than being
widened to cover both readings: `check cite` reports that shape as an error, and repairing it
silently here would answer a question about a region nobody wrote.

**And the span is MAPPED into each commit's own coordinates before it is compared.** A finding's line
numbers are written against the round's FREEZE; a commit's old-side ranges are written against its
PARENT, and measured over those eleven workspaces **643 of the 661 closures that cite a line name a
commit whose parent is not the freeze** — a round applies several batches with ledger and lens commits
between them, so the mismatch is the norm rather than the exception. The insertions and deletions
between the two revisions are summed and the citation moves by that much, the same arithmetic
`check drift` does one layer up. Two preconditions, both of which cost a false report before they
were checked: the freeze must actually PRECEDE the commit (`wf freeze -refreeze` moves a tag, and a
round republished after its own fix landed makes that a REVERSE diff, which subtracts what the fix
added), and the cited lines must not themselves have been rewritten in between — where they were, the
numbers mean nothing in that commit's coordinates and the closure is COUNTED and named rather than
judged either way. Corpus-wide the two changes together took the line-miss rows from 78 to 28, with 2
closures named as unmappable.

A FILE miss is a closure that did not land where the finding said it would. A SECTION or LINE miss is
a question, not a verdict. Findings that cite neither a section nor a line are counted and named as
checked only at FILE level, where a commit touching the document anywhere passes; findings naming a
path that matches two artifacts resolve to NEITHER (the ambiguity `check cite` also reports) and are
counted as unchecked; a round with no readable findings.md is an ABSENT measurement, and `-round N`
naming a round the workspace does not have is REFUSED rather than reported as a clean zero. **The
header says NOT A PASS when the check could not reach MORE closures than it checked** — measured
2026-08-28 over the SEVEN unpruned workspaces on disk (p2d, p2e, p2f, mocker-a3-cas, and the p3a, p3b
and p3c resource gates): p2d and p2e reached nothing at all and already said so, p2f checked 3 of 110,
A3 5 of 266 and P3a 68 of 421 while printing a headline that reads like a pass, and only P3b (568 of
587) and P3c (132 of 153) are genuinely covered. Two closures recorded on a live gate that no commit
touched rode through exactly that shape. The predicate is a sentence, not a chosen share, and an
UNREADABLE sha counts as unreached like an unnamed one — leaving it out let three unreadable shas of
four closures print `1 checked` with no marker at all. Findings
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

So the difference is made a RECORD rather than a guess. `wf round lens
<prompt-file> -name NAME -model MODEL -asks Q[,Q]` writes
`rounds/rN/lens-<name>.md` — the prompt verbatim, with what ran it. The argument
is the PATH of a file holding the prompt, never the prompt itself: a live gate
pasted one on the command line and got `file name too long` from the syscall,
twice, with nothing saying what was wanted — and `wf freeze` refuses while ANY closed round's
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

### `wfcheck` and a `<placeholder>` inside a command a prompt hands over

Such a command runs, exits 0 having matched nothing, and prints NOTHING — which
the receiving agent cannot tell from a section that genuinely says nothing, so
it reports the absence. Measured on one gate: two BLOCKERs in one round for
`sed -n '/^**<its id> /,…'` ranges handed to reviewers, and a third instance in
the next round's own lens prompts, caught by grepping before dispatch rather
than by any check.

The predecessor of this check required a FENCED line — four leading spaces and a
command from a seven-word list — and over 73 frozen script revisions across the
twelve unpruned workspaces it fired ZERO times, including on the three revisions of the gate
that was filing this exact shape as a blocker at the time. Both halves of the enumeration
defeated it: the command was `sed`, which was not on the list, and the line read
`  3. Run ONLY the named test: go test … -run '<the test name>'`, which is two
spaces of a numbered list and not four of a fence.

So the shape is now the command plus a placeholder on the same line, anywhere in
the prompt, at **INFO** — measured before shipping over those same 73 revisions
with the built binary: **40 report lines on 23 of them, 12 distinct quoted
lines**, three of which were the defect and the rest ordinary templates an agent
is meant to substitute (`git diff HEAD -- <path>`). The reading is
`wfcheck` over `git show <gate-r*>:<artifacts/*.mjs>` for every tag in every
workspace, counting its own report lines. Which of the two a line is depends on whether the
receiving agent substitutes, and that is not in the text, so the level does not
move the exit code. The narrower fenced shape stays a **WARN**, because a
command on its own indented line is meant to be run as written and there is no
reading under which the placeholder is the agent's to fill. One report per site
and never two: the fenced shape is a narrowing of the wide one.

### `wfcheck --owns-diff <ref>` — the ownership map against the tree, AFTER a run

Everything else `wfcheck` does is static. This is the one question that needs
the run to have happened: `wfcheck -root <repo> --owns-diff <the commit the run
started from> <script>` reads the map from both halves (the gate configs' `owns`
arrays and the `YOU OWN` blocks in the literals) and compares it against
`<ref>..worktree`, reporting BOTH directions — a file the run wrote that no
section owns, and a path a section owns that the run never wrote.

It is an AUDIT, not a bar: it exits zero whatever it finds, because a file
nobody owns may be a hole in the map or a legitimate incidental edit, and an
owned file nobody wrote may be a section that skipped its task. The working
TREE and not `HEAD`, because the audit happens before the slice is committed —
on the run this was written for, 54 files were changed and HEAD had not moved,
so a commit-to-commit diff would have compared a range that changed nothing and
printed a clean report. Every git call carries `--no-optional-locks`: a plain
`git diff` refreshes the index of the tree it runs in, and the target of a gate
is read-only.

Two limits it states rather than hides. The range is `base..working tree`, and
nothing records what was already dirty when the run started, so an edit that was
sitting there is counted as the run's — the report prints that line on every run.
And a map read in PART is worse than one read not at all: a `owns` array built at
runtime, or a `YOU OWN` block split across concatenated literals, leaves paths the
tool cannot see, and those print as "owned by no section" while meaning "not
read". So the report prints `PARTIAL MAP` whenever a gate's `owns` did not
resolve statically or the script writes `YOU OWN` in blocks the textual reader
could not join — the P3h script is exactly that case, and its nine rows are true
anyway, verified against the run's own six.

Measured on that run, against a script whose own harness already compared each
producer's SELF-REPORTED file list to its `owns`: the self-report found six, the
tree found **eight** — four of them test files no self-report had named — and
nothing in the other direction. A first run of this command reported nine and
one, and the extra pair was this tool's own path grammar: `ownsTargets` trimmed
the leading dot off a declared `.env.example`, which produced a false row in each
direction from one strip and was banked for a day as a typo in the script being
audited. Eight over a 54-file diff is a seventh of the population, not a wash of
paint.

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

**A `YOU OWN` block a factory composes is read from the SOURCE TEXT**, because
the parsed half cannot reach it and reported so honestly: measured on P3c, `0
owned path(s) parsed from 1 prompt(s)` over a script declaring sixteen, on which
the arithmetic was then done by hand four times in one gate, by three different
agents. Every block in the script's string literals is read — comments
excluded, escapes decoded, a block split by a `${...}` hole still one block —
the paths no resolved prompt carries are reported, two BLOCKS naming one file is
an ERROR, and they are folded into the ONE reach report rather than printed
beside it (both halves speaking at once produced a report saying every path was
covered and sixteen were owned by nobody, of the same file). It is the weaker
half and says so: it knows which block declares a path, not which agent is
handed which block. On that script it reads **16 paths in 4 blocks, 16 distinct,
16 reachable by a fix agent** — the hand result, mechanised.

### `wfdry` and the completion boolean

A report field is filled as pass-shaped by its NAME: one of
`passed/ok/green/clean/verified/proven/done/complete/completed/succeeded/finished/met/satisfied/achieved`,
optionally prefixed by `goal/scope/slice/section/acceptance/criteria/criterion`.
Anything else is FALSE in every mode, so the branch behind it is exercised by
none and the `clean` mode fails correctly — which is right, and used to say
nothing about WHY. Three field names were burned on one gate discovering the
list by reading this tool's source (`held`, `mutationMet`, `smokeAchieved`), so
the WARN now names the vocabulary it is matching against, built from the same
list the matcher compiles.

A script may instead declare the polarity in the property's `$comment`:
`"$comment": "x-pass: true"` on a boolean makes it pass-shaped whatever it is
called, `false` makes it a failure flag. Renaming is offered first because it is
free — the annotation rides in the schema the launch's safety classifier reads,
and widens it. In `$comment` and NEVER as a schema KEY: the launch validates in
JSON Schema strict mode and refuses the whole schema over an unknown keyword
(`strict mode: unknown keyword: "x-pass"`), which killed a run's first agent at
spawn and, through that script's own abort, two more with it. `wfdry` refuses the
key itself now — see below.

An ARRAY is filled or emptied by its name in the same way, and its vocabulary is
a list of DEFECT words: `blocker/major/failure/unfixed/open/concern/dead/
unverified/violation/gap/risk/defect/error/issue/problem/finding`. An array
whose name carries one comes back EMPTY in the passing modes and holding a mixed
PAIR elsewhere — two items, so both sides of every
`filter(f => f.severity === 'blocker')` are exercised; every other array comes
back holding one placeholder in every mode. Two of the sixteen are narrower
still: `failures` and `unfixed` are populated only in the mode named for them
(`acceptfail`, `unfixed`), because those are the modes that exist to walk one
branch each. So a `green` term reading `x.length === 0` over an array named for its
subject is false in the clean mode, and the mode fails while nothing is wrong.
P3d spent two rounds reaching for exactly that term over `unheldProperties`,
including a reviewer's proposal to make the items objects rather than strings —
it was tried and changed nothing, because the discriminator is neither the item
type nor `required`, which this runner does not read. The array's `$comment`
declares it: `"$comment": "x-pass: false"` says a populated array is the failure
shape, `"x-pass: true"` says the opposite, and an explicit polarity beats every
matcher. It is `$comment` and not a schema key for the reason above, and that
form had never been through a live launch until it was probed.

### `wfdry` and the keywords the launch refuses

Every `agent({schema})` is validated by the launch in JSON Schema STRICT mode,
which refuses the WHOLE schema over a keyword it does not know — at spawn,
before the agent exists. `wfdry` FAILS the mode over one, rather than warning:
the launch refuses it, so the stub dies where the agent would. Probed, one cheap
agent per schema: `x-pass` on an array, `x-pass` on a boolean, `x-foo` and a
plain `passPolarity` are all REFUSED — so the refused class is UNKNOWN KEYWORD
and not the `x-` extension prefix — while the same schema carrying none of them,
and `$comment`, `description` and `title` on the root and on a leaf, are all
ACCEPTED. A second probe asked which DRAFT it implements, because a spelling from
the wrong one is refused just as surely: `definitions`, `dependencies` and
`$anchor` are ACCEPTED and `prefixItems`, `dependentRequired` and
`unevaluatedProperties` are REFUSED, so it is draft-07 plus `$defs` and
`$anchor`, and that is what the allowlist carries. It was the UNION of the two
drafts for one afternoon, which made its incompleteness two-way — every keyword
wrongly IN it is a false green — and the probe is what narrowed it.

The check reads the schema as SERIALIZED, because `JSON.stringify` drops a key
whose value is `undefined` or a function and the launch never sees it; and it
walks STRUCTURALLY rather than descending into every map, because under
`properties`, `patternProperties` and `$defs` the keys are FIELD NAMES and under
`enum`, `const`, `default` and `examples` the value is DATA the agent may
return — a generic walk reports both as keywords. Measured before shipping over
186 frozen script revisions across the thirteen gate workspaces on disk: it
fires on 4, and all four are the revisions of one script that carried the
annotation.

### `wfdry` and `$defs`/`$ref`

A `$ref` is expanded against the schema's own `$defs` before the fixture is
synthesised, because the LAUNCH resolves it: a probe against the real thing had
a deliberately illegal `subjects: 0` rejected, so the `minimum` and `minLength`
that reach a leaf through a reference are enforced there. This runner used to
resolve nothing, and the referenced node — carrying no `type` of its own —
synthesised to a placeholder STRING. Measured on P3d, per revision of its own
script: the acceptance schema was 6,038 bytes with the bar leaf inline;
declaring that leaf once and referencing it took it to 3,155 — and took `wfdry`
from 7/7 modes to 6/7, with `obs.bars[k].result` undefined and all five bars
reported RED, so the gate REVERTED it one commit later and went back to 4,233,
above the width its own probe then measured as REFUSED. What took the shipped
schema under in the end was trimming the descriptions, with the per-property
leaf's `$ref` kept: 3,114 bytes against 5,721 fully inlined, by the gate's own
record. So the narrowing was reverted for exactly the cost this change
removes. So the narrowing that the width band keeps asking
for used to cost the dry run its own happy path; it no longer does. Local pointers only
(`#/$defs/...`); a key written beside a `$ref` survives the expansion and wins;
a reference this runner cannot expand — a typo, a remote URL, a cycle — is
NAMED in a note rather than left to surface as a placeholder. The WIDTH is
unaffected: it is measured from the object the script composed, which is what
the launch receives, with its references in it.

The pointer is anchored at the SCHEMA ROOT. A `$defs` on a sub-schema — beside
the leaf it declares, in the same constant, which is how a script naturally
writes it — is legal, but it is NOT reachable by `#/$defs/x`, so every property
comes back a placeholder STRING. Either fix works: move the `$defs` to the root,
or write the pointer that reaches it. P3e read that as
the whole feature being broken, reverted to twenty-two inlined copies of its
acceptance leaf and paid about 1,900 bytes of width for it — 2,894 bytes
inlined against 982 referenced, re-derived here rather than taken from the
artifact's own "about 1,800", which prices its description trim. The root-anchored
form resolved then and resolves now, including through keys built by
`Object.fromEntries(...map(...))`; what was missing was the note saying WHERE
the `$defs` it could not reach actually is. It now prints the pointer that would
resolve, which is the same trade `check drift` makes when it prints the re-aimed
range rather than saying a citation moved.

### `wfdry --fixture <label>`

Prints the object the stub returned to that agent, per mode. Three of one gate's
dry-run reds were fixture-synthesis limits reported as artifact defects — a
`$ref` the runner lost, twenty-two booleans defaulted false because their names
carry no polarity, and `finalCount === 22`, an integer against a constant no
report-shaped fixture can satisfy — and each cost a reading of twenty-two
"unmet" rows to diagnose as one gap. The object separates them in one look, and
the clean mode's verdict violation points at it. A label no mode composed is
refused with the labels the run did compose, for the reason `--dead` refuses one:
every reading behind the name went over the empty set.

### The `required` census beside the width

Every schema line also prints how many keys the schema names as `required`, over
every object node, and how many nodes declare `properties` and require none of
them. Neither is a verdict: an object requiring none of its properties is legal
and common. What the pair buys is that losing a `required` is a CHANGED NUMBER.
P3e dropped `required: ['met','evidence']` from an acceptance leaf inside a hunk
whose own comment was entirely about width, and one frozen revision published a
report whose every property could come back `{met: true}` with no evidence at all — the only thing that gate's
evidence reviewer had to read. Nothing static reaches it: the leaf is built
inside an `Object.fromEntries(...map(...))` callback, so `wfcheck` sees no
literal, and both static tools were clean over that script. On the workspace's
own history the `accept` schema goes 74 required keys and 1 such object at the
round-10 freeze to 33 and 23 at round 11, at a width that FELL from 3,869 to
3,314. The census is taken over the EXPANDED schema, because the obligation is
per USE, and it is not taken at all over a schema that does not SERIALIZE or one
carrying a `$ref` that did not EXPAND — an unexpanded reference leaves the
referenced leaf a placeholder, so its `required` is not in the walk and the
count would be short by exactly the amount the author cares about. `-1` prints
as nothing, which is a different claim from zero.

### `wfdry` and the schema width

Every run prints what each agent would put ON THE WIRE: the serialized width of
the schema the HAPPY PATH composes, and beside it the widest any failure branch
composes when the two differ. Always, not only when it is wide — a green run
over an unmeasured width proves nothing about the launch.

The quantity matters as much as the number. The corpus points are clean-path
widths, and the first version of this meter compared them against the widest
branch: on one script those coincide, on another they differ by 55%, and the
tool warned that a schema the launch had accepted 44 times over had been
refused. One number computed, another calibrated.

Measured with this runner over the four gates that have probed or launched, against the
launch's safety classifier (`output schema too large to classify safely`):

| | refused | accepted |
|---|---|---|
| P3a, launch 2 | `verify` 27,212 · `accept:confirm` 8,596 · `accept:writes` 5,760 · `accept:reads` 4,814 | `accept:surface` 3,390 |
| P3a, launch 3 (44 of 44, zero errors) | — | widest 3,435 |
| P3c, launch 1 | `accept` 4,589 — the FIFTEENTH and last agent, six hours in | — |
| P3c, launch 2 (15 of 15) | — | `accept` 3,877 |
| P3d, probed before the launch | `accept` 4,233 — 82 ms, zero tokens | 3,114 — 41 s |
| P3e, probed TWICE before the launch | — | `accept` 3,869 — 18 s · then 3,974 — 19 s |

So on the clean quantity the corpus separates: every accepted width is at or
below 3,974 and every refused one at or above 4,233. The gap between them is
UNMEASURED, not safe, and this tool does not pretend to know where in it the
boundary falls. The floor is the LOWEST refusal recorded, not the last one
heard, and the ceiling is the HIGHEST acceptance: P3d's probe moved the floor
down 356 bytes for 82 milliseconds of work, after two rounds of that gate had
argued about the band from measurements of SIZE alone, and P3e moved the ceiling
UP 97 bytes by probing twice inside one gate — the second time because restoring
a `required` array that a width-reduction edit had dropped took the schema into
the band, and it was re-probed rather than argued about, for about 40 seconds
and 116k tokens across both. P3c's refusal quoted 4,563 against this runner's 4,589 — two
serializers 26 bytes, 0.6%, apart, with this one the LARGER, which is the safe
direction for a warning.

A wide FAILURE branch is a separate statement, never evidence about the
recorded widths: the launch sends whatever the branch that runs composes, and the
refusal this corpus paid most for landed on an agent at the very tail. P3a's
`verify` composes 3,435 on the happy path and 5,326 on its failure branches, and
that is what the second line says.

A width is a QUESTION and never a verdict: nothing fails a mode on one. The
answer is a probe — one cheap agent carrying the schema and a prompt saying to
fill it with placeholders, seconds against a multi-hour run. The gate that lost
the last agent had measured its own width to the byte and written down that
"acceptance cannot be measured from here", which was false.

A schema that does not SERIALIZE at all is the other thing entirely and FAILS
the mode: there is nothing for the launch to send, and a WARN would leave `7/7
modes completed` and exit 0 standing over it.

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
the five correct scripts on disk every mode but `throw-serial` reaches its
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
