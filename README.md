# wfgate — gate harness

The `wf` CLI: a review gate's workspace as a git repository, atomic application
of edits, and deterministic checks over the artifacts.

A gate over a large change spends effort on two different things. One is the
reviewing itself — that is judgement, and it is what the rounds are for. The
other is bookkeeping: which revision of which artifact a reviewer read, what a
round was obliged to close, whether a batch of edits landed whole, and whether
the second copy of a rule was corrected along with the first. This tool does
only the second.

## Install

```bash
make build          # ./wf, version-stamped via -ldflags
make install        # ~/.local/bin/wf
make test           # go test -race
```

Stdlib only; `git` is taken from `PATH`.

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
| `wf init <name> -target <repo>` | workspace: git init, `gate.json`, `artifacts/`, `rounds/`, first commit |
| `wf add <file>...` | bring an authored artifact under the workspace and commit it; adopts an untracked draft already in place |
| `wf status` | manifest, current round, who last touched each artifact, dirt, tool version |
| `wf apply -f <edits> [-m msg] [--dry-run]` | all-or-nothing batch of edits (JSON or text blocks), then a commit of only the touched files |
| `wf round new [-title T] [-kind full\|delta]` | open the next round, scaffold `rounds/rN/findings.md` with its scope |
| `wf round record <file> [-round N]` | fill a round's findings.md, validating closures before writing |
| `wf freeze` | sha256 of all artifacts + target HEAD + tool version + citation counts → `rounds/rN/freeze.json`, commit, tag `gate-rN`; refuses while a blocker of the round being closed stands open |
| `wf verify [-round N]` | the pre-launch check: the tree is byte-for-byte what was frozen |
| `wf delta [-from REF] [-to REF] [-save]` | the round's findings, the closure checklist, and the artifact diff; `-save` commits the brief into the open round's bookkeeping |
| `wf check [cite\|dup\|drift\|rules]` | deterministic checks (no argument = cite + dup + rules) |
| `wf stats` | per-round findings, closures, apply batches and the opt-in `[fix-induced]` share, from the ledger and git |
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

After the write the files are re-read from disk and every edit is searched for.
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

Assembling the JSON was the measured bottleneck of the first live gate: a
35-edit batch under three layers of quoting failed on escaping more often than
on substance. The format is detected by the first byte (`[` means JSON).

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

### `wf check drift`

When the target moved after a freeze, intersects its `frozen..HEAD` diff with
the cited ranges: `edited` — the cited lines changed, re-read the claim;
`shifted` — the citation moved, the re-aimed range is printed; `gone` — the
file left the tree. The only mechanical signal an unanchored citation gets.

### `wf check rules`

Sites of one rule marked `[RN]` must normalise identically across the
artifacts. This is the reach into what `dup` structurally cannot see — copies
that contradict in different words — and it works only where an author marked
the rule, so the marker population is always printed.

## Boundaries

The workspace is **never inside the repository under review**. The gate's
agents diff that tree against its own HEAD, and the rule "the tree stays
untouched while a run is live" does not survive orchestration commits into it.
`target` is read-only.

The checks take the mechanical share of blockers — about a fifth — and never
impersonate coverage of the rest. That is why `check cite` always prints how
many citations carry no anchor: a green run over 148 unanchored citations
proved nothing.
