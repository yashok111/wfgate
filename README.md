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
```

`git` is taken from `PATH`. One dependency, `goja`: it is the JavaScript
parser `wfcheck` reads a script with and the engine `wfdry` runs it on. The
dependencies.

## The quick cycle

```bash
wf init p2d -target ~/projects/mocker      # a workspace with its own git repo
cd p2d
wf add ~/draft/context.md ~/draft/script.md  # first commit of an artifact; wf apply cannot make it

wf round new -title "kernel + checkpoints"   # -kind delta for a delta round
wf check                                   # cite + dup + rules
wf apply -f fixes.json -m "close r1 blockers"
wf round record findings.md                # the round's ledger, closures validated
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
| `wf apply -f <edits> [-m msg] [--dry-run]` | all-or-nothing batch of edits (JSON or text blocks), then a commit of only the touched files |
| `wf round new [-title T] [-kind full\|delta\|tool]` | open the next round, scaffold `rounds/rN/findings.md` with its scope |
| `wf round record <file> [-round N]` | fill a round's findings.md, validating closures before writing |
| `wf freeze` | sha256 of all artifacts + target HEAD + tool version + citation counts → `rounds/rN/freeze.json`, commit, tag `gate-rN`; refuses while a blocker of the round being closed stands open |
| `wf verify [-round N]` | the pre-launch check: the tree is byte-for-byte what was frozen |
| `wf delta [-from REF] [-to REF] [-save]` | the round's findings, the closure checklist, and the artifact diff; `-save` commits the brief into the open round's bookkeeping |
| `wf check [cite\|dup\|drift\|rules]` | deterministic checks (no argument = cite + dup + rules); `dup` takes `-since REF`, `-threshold F` and `-min-words N` |
| `wf stats` | per-round findings, closures, apply batches, edits and changed lines (with findings per 100 changed lines — the batch cost meter; a workspace with no apply commit is named as an absent population, and a round whose window cannot be attributed has its numbers omitted rather than filled from the whole history), the opt-in `[fix-induced]` share, and reviewer-vs-tool round totals, from the ledger and git |
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
fifteen headers. When a batch is large enough that the workspace's own history
(findings per edit, from the ledger and the apply commits) predicts a costly
next round, `apply` prints a note suggesting a split by rule — a note, never a
refusal. The meter's denominator is CHANGED LINES, not edits: measured across
three closed gates, findings correlate with edits at +0.26..+0.49 and with
changed lines at +0.47..+0.82, because one edit can rewrite one word or nine
hundred lines. Every apply commit records its own edit count in a `wf-apply:`
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

Every conflict — here, and `dup`'s `NUMERIC`, `OPERATOR` and near-match groups
— is printed at the point the copies STOP agreeing, with the shared opening
trimmed to a few anchor words. Copies that conflict agree almost everywhere, so
truncating them from the start prints the same characters for every site: one
gate reported five sites of one rule as five byte-identical lines, and the
difference was found with `cat -A`.

## Boundaries

The workspace is **never inside the repository under review**. The gate's
agents diff that tree against its own HEAD, and the rule "the tree stays
untouched while a run is live" does not survive orchestration commits into it.
`target` is read-only.

The checks take the mechanical share of blockers — about a fifth — and never
impersonate coverage of the rest. That is why `check cite` always prints how
many citations carry no anchor: a green run over 148 unanchored citations
proved nothing.
