# The wf gate harness (the operator's half)

The bookkeeping of a gate, in the order an operator runs it: open the workspace, open the round,
apply, record, bank the lens, freeze, verify, sweep, close. Steps -1, -1b, 2 and 4 of the sequence
in `pre-flight-gate.md`, plus the checks and the cost meter a round's fix goes through.

The gate itself — who reviews, along which vectors, and when it ends — is `pre-flight-gate.md` and
`round-discipline.md`. What a reviewer is briefed with is `gate-checks.md`.

## What the operator actually thinks, at the moment the discipline breaks

**What the operator actually thinks, at the moment the discipline breaks.** Three rows are a measured
drift OUT of the tool and two are an over-reading of what it covers; all five were committed by
somebody who knew the rule. The rules are elsewhere in this manual and are POINTED at — the middle
column carries the consequence and no figures, because a measurement restated is a measurement that
can drift apart from itself, and nothing here compares the copies.

| the thought | what is true | where the rule is |
|---|---|---|
| "This edit is one word — quicker by hand than through `wf apply`" | The hand edit gives up all-or-nothing AND the declared occurrence count, and it drops the round out of the cost meter. One gate did it for several rounds and found out at close, with those rounds printing a bare zero | step 2, and `gate-checks.md` I |
| "`wfcheck` is 0/0 and `wfdry` is 7/7, so the script is checked" | `wfdry` runs control flow; `wfcheck` reads the parsed program and only what a prompt or an `owns` array DECLARES. A file named in neither is invisible to both, and one gate's round-19 blockers included two of exactly that shape | `pre-flight-gate.md`, steps 0 and 2b |
| "This vector came back early and its findings are obviously correct — I will start fixing now" | Fixes land at a barrier. One gate applied a returning vector's fixes while two others were still reading, and one of those then reported as open a blocker that was already closed | `gate-checks.md` I |
| "The check printed its numbers, so something was measured" | An empty population prints numbers too. `rules`, `stale`, `closure` and `schema` say NOT A PASS when they compared nothing; `cite` and `dup` say it in the other shape, by printing the unverified or hidden population — on those two, that line IS the verdict and there is no phrase to search for | `gate-checks.md` A |
| "The freeze is stale — I will delete the tag and freeze again" | A hand `git tag -d` moves every window `wf stats` derives from it. The tool then refuses that round a ratio and drops it from the prediction basis BY NUMBER, so what the shortcut costs is the measurement, not merely its accuracy. `wf freeze -refreeze` republishes, moves the tag and records the supersession | step 2 |

## -1. Open a gate workspace, and change artifacts through `wf apply`

-1. **Open a gate workspace: `wf init <name> -target <repo>`.** Every artifact of the gate — the
   decision document, the context document the prompts read, the script, each round's findings — lives
   under its `artifacts/` and `rounds/`, and the workspace is its own git repository. It is NOT inside
   the repository under review: the run's own agents diff that tree against its HEAD, and the rule that
   the tree stays untouched while a run is live does not survive the orchestration committing into it.

   **Seed it with `wf add <file>...`**, which copies an authored file in and commits it. That is the
   one commit `wf apply` cannot make — every edit must match text that already exists — and without it
   `wf freeze`, which refuses while any ARTIFACT is uncommitted, is unreachable on the first pass.
   (Only `wf verify` fails on anything uncommitted anywhere in the workspace.) `wf add`
   refuses a name that is already a REVIEWED (tracked) artifact, because a silent whole-file overwrite
   is exactly what the next paragraph is there to prevent — but a first draft written straight into
   `artifacts/` is adopted and committed as it lies: refusing the untracked file forced the hand
   `git commit` the command exists to remove, three times on the first live gate.

   From here on, **an artifact is changed by `wf apply -f <edits>`, never by hand.** The batch is
   validated in full before a byte is written and rejected whole if one replacement misses, so a fix is
   never half-applied — the failure that costs a whole round is the batch that reported one error and
   silently discarded the edits that had already matched.

   The batch is written either as a JSON array or as the plain-text block format — `=== <file>
   [count=N] [note=...]`, then `<<<<` old `====` new `>>>>`, everything between markers taken
   VERBATIM. **Each replacement block carries its own header**: `count=` is scoped to that one
   replacement, never to the file — fifteen replacements in one file are fifteen headers (the shape
   one header + fifteen blocks was the first thing a live operator reached for, and the parser now
   names the rule when it sees it). Prefer the text form for anything with multi-line or quote-bearing `old` strings:
   assembling the JSON was the measured bottleneck of the first live gate (a 35-edit batch under
   three layers of quoting — JSON in a heredoc in a shell — crashed on escaping more often than on
   substance). A bare `<<<<`, `====` or `>>>>` line INSIDE either
   half is refused at parse, and the wording that used to stand here — "a marker collision mis-splits
   and then FAILS the plan loudly, because the mis-split old matches nothing" — was true only of the
   OLD half. In the NEW half it matches EVERYTHING and is WRITTEN: a forgotten `>>>>` swallowed the
   next block whole, every edit reported `ok (1 match)`, the batch verified byte-for-byte (the
   verification is against the batch AS PARSED, so a mis-split is confirmed in the words a correct
   batch gets), the literal markers landed in the artifact, `wfcheck` went from 0 errors to
   `Unexpected token ===`, and the recovery was two `git revert`s and about half a round. Text that
   must carry such a line is the one case for the JSON form.

   **An edit file and a findings file are DATA: write them with a file tool, never through a shell
   heredoc.** The same gate had two records rejected by a pipe it had never typed — a shell hook
   rewrote a command quoted inside the heredoc that was writing the table and injected an alternation
   pipe into a row's TEXT. Anything between the shell and the file may rewrite it, and the artifact
   that arrives is not the one that was authored.

   **One replacement carrying the whole artifact is a legitimate shape for a RESTRUCTURE and it
   forfeits every guarantee this command has.** All-or-nothing and the declared `count` are per
   REPLACEMENT: a replacement whose `old` is the document has a count of one that always matches, so
   the batch is "verified" and the many changes inside it are verified by nothing. Measured on the
   gate that stopped instead of closing — eighteen of its sixty-eight batches were a single
   replacement, the changes inside them were produced by a pipeline whose non-matching
   `str.replace()` is a silent no-op, and a later round found 15 of one round's 30 recorded
   closures byte-identical to their pre-fix text (a second lens confirmed 11), two of them the FOURTH claimed closure on one
   unedited line. It cost three rounds. `wf apply` NOTES a replacement covering half a file or more
   and says how many lines it rewrites; the note is not a refusal, because generating one minimal
   block per change failed twice on uniqueness on that same gate. When you use the shape, byte-verify
   each change against `git show HEAD:<artifact>` before recording a closure for it — an assertion
   that a replacement MATCHED covers only the replacements somebody wrote, and a fix nobody wrote is
   silent in exactly the same way.

   **A restructure you made by hand is adopted with `wf apply -whole <path>`, never with
   `git commit`.** The command takes the file as it lies, prices the round from the diff's hunks, and
   marks the commit as an ADOPTION so nothing later reads it as a batch the tool verified — the
   guarantees are forfeited either way, and the difference is whether the round is priced at all.
   Measured: one gate applied a structural merge of two script sections by hand, four of its thirteen
   rounds then contributed no edit count, and the cost meter every later round was handed came from
   the rounds that used the block format.

   Each replacement also declares how many occurrences it expects, and **that `count` is scoped to ONE
   file**: it catches a rule you believed appeared once in an artifact and that appears there twice. It
   does NOT reach a copy living in a second artifact — declaring `"count": 2` for a rule split across
   two files rejects the batch outright, and the obvious repair (drop it to 1) leaves the other copy
   standing, which is the very defect. The correct shape is **one edit per artifact in the same batch**,
   each with its own count; all-or-nothing is then what guarantees that both copies move or neither
   does. A hand edit plus `git commit` gives up both properties, which are the only reasons the command
   exists.

## -1b. Open the round, record its findings, bank its lenses

-1b. **Open the round: `wf round new -title "<what this round must find or close>" [-kind full|delta|tool|run]`.**
   It scaffolds `rounds/rN/findings.md`. This is not optional bookkeeping: `wf freeze` refuses while no
   round is open, so a gate that skips it cannot reach step 2. Each later round opens its own before
   re-freezing — a frozen round is re-frozen only through `wf freeze -refreeze`,
   which moves the tag and records the supersession. The KIND is recorded in the scaffold because the
   audit needs what a round did NOT look at: on the first live gate seven of ten rounds were delta
   rounds whose scope lived only in prose titles, and twice a round reviewed less than a reader of its
   findings would assume, with no record of it anywhere. A delta round's findings state what was not
   re-read. `tool` marks a round with NO reviewer — findings from `wfcheck`/`wfdry` or from the launch
   itself: P2e recorded two of those as ordinary rounds, and the stop rule then read a blocker descent
   of 15→13→4→1→1 where the reviewer descent, the one the rule is about, was 15→13→4→1. `wf stats`
   counts tool rounds apart, and a tooling round's blocker does not restart the stop rule's clock.
   `run` marks a round that CONTAINS a live run of what the gate built. It is a reviewer round
   everywhere else — what a run reports is evidence produced by executing, so its zero is a measured
   zero and its findings belong in the descent the stop rule reads — and only the CLOCK treats it
   apart: `wf stats -time` keeps its span out of the median and prints it on its own line. Measured:
   one gate's round 13 spans 288.2 minutes against a median of 45.3 because it held the launch (38
   agents over 4h36m), and the note under the time line — "a span far above the median is a break,
   not work" — is precisely backwards there. The launch is the one event in a gate's life most worth
   timing.

   **The scaffold is a form, and an empty form briefs nobody.** When round N's reviewers report, write
   what they found up and put it in with `wf round record <file>` BEFORE applying the fixes — the
   command exists so that filling the form is not the one place an operator falls back to a hand
   commit. That file is what
   `wf delta` puts in front of the next reviewer, whose first job is to check the claim that those
   findings were closed. Round N+1 is opened after the fixes, so at review time the open round's file
   is still blank; `wf delta` reads the file of the round it diffs FROM, which is the one being closed.

   **Bank the round's LENS ROSTER with `wf round lens <prompt-file> -name NAME -model MODEL -asks Q[,Q]`,** one call
   per vector, and it writes `rounds/rN/lens-<name>.md` — the prompt verbatim, with what ran it. It is
   not bookkeeping: on one gate the criterion vector alternated between an external model and an
   internal one across its delta rounds, and the rounds it ran externally returned close to TWICE the
   blockers — the two lowest-blocker rounds of the whole gate being the two after the external lens was
   dropped for quota. (The exact ratio, and the round it excludes, are in this repository's own
   `CLAUDE.md`; what an operator acts on is that the roster is the only thing separating the two
   explanations.) Any descent a stop rule reads there is otherwise confounded with a billing decision,
   and nothing in the workspace recorded which model ran what AS DATA: `rounds/rN/brief.md` is the
   `wf delta` output, not the reviewer prompt, and the roster survived only as prose in each findings
   header — checkable by hand-reading eight of them, readable by no command and printable in no report.
   One claim in this toolchain was withdrawn after exactly that hand-reading. **A lens counts only if it
   is a RECORD** — it says which vector (`# Lens`), what ran it (`Model:`) and what it was asked (a
   prompt after the `---`), and it is committed. The roster is the escape from the two refusals below,
   so an empty file of the right name must not be one. `wf stats` prints each
   round's roster and names the rounds carrying none (a `-kind tool` round is excluded from that note:
   it dispatched no reviewer by declaration); a banked prompt is never rewritten, and
   `wf round lens` with no file prints the roster. It is also how a round whose vectors ran and found
   NOTHING says so: see the stop rule in `round-discipline.md`, and the refusals in `wf freeze` and `wf close`.
   `-asks` takes the SET however you write it — `closure,fix-induced,ripple`,
   `closure+fix-induced+ripple`, a space-separated list, or the flag repeated — because a delta round is required to ask all
   three and one gate banked one vector per round and then hand-edited `Asks:` in the file,
   nineteen times over ten rounds, having been refused the natural spelling.
   **A lens that DIED for a reason outside the artifact is banked too, with what replaced it.** On one
   round an external lens was quota-blocked mid-round and produced no report; it was replaced by an
   opus lens carrying the identical prompt, and BOTH were banked. That is the only form in which a
   later reading of the descent is not confounded with a billing decision — which is the same
   confounding the roster exists to remove, arriving through a different door. A round whose vector
   simply vanished from the roster reads as a round that never had it.
   `wf round record` prints a note when the round it just recorded banks no lens, because recording
   the findings is the step that feels like the work: measured on mocker A3, eleven of thirteen rounds
   banked none, and for rounds 1-10 the findings are in the ledger while the prompts that produced
   them are gone — so the claim that gate most wanted to make from its own numbers could not be
   checked against anything.

   **The findings file is POSITIONAL** — `wf round record <file> [-round N]`, with `-` for stdin; there
   is no `-file`, and an empty input is refused rather than recorded over a filled ledger.

   **The findings table is a validated ledger, not free text.** `wf round record` parses the scaffold's
   table before writing a byte: a `closed by` cell naming a commit that does not exist in the workspace
   rejects the whole record (a landed lie briefs the next round with fiction; a rejected one announces
   itself) — the refusal names the repository it looked in, and says so when the sha IS a commit in the
   TARGET, which is what a round recording a LAUNCH reaches for; cite the workspace commit that landed
   the fix and write the target's sha as prose, and the report prints the closure split — by commit / by prose / open. An OPEN blocker is
   legal at record time — findings are recorded when FILED, closures land later — but not at freeze
   time: `wf freeze` reads the findings of the round being CLOSED (the previous round — the frozen
   round's own file is always a blank scaffold, so checking only it would pass vacuously forever) as
   well as the open one, and refuses while a BLOCKER row stands without a `closed by`. So after the
   fixes land, fill the column with the apply commit's sha and re-record with `-round N`. A prose
   closure ("deferred, see decision D-7") passes the freeze with a counted "verify by hand" note — a
   deliberate escape, not a verified commit. A hex-looking English word is not read as a sha: a commit
   claim needs at least one digit. A comma-separated list of shas is fine, and case does not matter.
   **Write the artifact path into the finding text while you are writing it.** `wf check closure` asks
   whether a closing commit's diff reaches what the finding NAMES, and a finding naming no artifact
   path cannot be reached at all: `wf round record` counts those and says so at the moment they are
   still cheap to fix. Measured on one gate — 0 of 350 claims checkable on the first record, 26 of 26
   after the paths went in. Both reports say NOT A PASS where the check could not reach MORE closures
   than it checked — the same predicate in both, because two commands contradicting each other about
   one round is worse than either answer, and an unreadable or ambiguous row is unreached as surely as
   an unnamed one. Measured across the unpruned gate workspaces: two reached
   nothing at all, and three more checked a small fraction of their closures while printing a headline
   that read like a pass — only the most recent gates are genuinely covered. Two closures recorded on a live
   gate that no commit touched rode through exactly that way. The scaffold `wf round new` writes states
   both table rules — name the artifact path, escape a `|` inside a cell.

## 2. Freeze the manifest, and verify before the launch

2. **Freeze the MANIFEST for the round: `wf freeze`.** It refuses while any artifact is uncommitted
   (and while a blocker of the round being closed stands open — see -1b),
   then writes one record over the WHOLE set — sha256 of every artifact, the target repository's
   HEAD, the version of the `wf` binary that wrote it (`wf version` prints the running one; the
   stamp exists because a run against a stale installed binary is otherwise indistinguishable from a
   run against the new one), and the citation counts for the anchor RATCHET — commits it and tags the
   round `gate-rN`. The ratchet is a direction, not a threshold: a fixed bar would be arbitrary, but
   when the anchored share falls against the previous freeze the note says so, because an author who
   stopped anchoring is writing citations the gate cannot really verify.

   **Re-freezing is `wf freeze -refreeze`, and it is a RECORD rather than a hand `git tag -d`.**
   Freezing stays one-way by default, for a measured reason: the manifest is read from the WORKTREE by
   `wf verify`, so a freeze that silently rewrote it turned a MISMATCH into "clean". But the citation
   resolution below runs nowhere else, so the command that surfaces a mis-aimed anchor is the command
   an operator must run again after fixing one — five of one gate's twenty-two rounds deleted the tag
   by hand to do it, twelve times in all. With the flag the tag MOVES to the new commit and the manifest records how many freezes this
   round has had and which commits they published, because somebody may have been handed the old tag
   and read different bytes. It refuses when every artifact hashes the same against the same target
   revision: a re-freeze republishes CHANGED bytes, and there is nothing to record otherwise.

   **A moved tag re-attributes cost, and both commands now say so.** Every window in `wf stats` is
   bounded by `gate-rN`, so a batch applied after the round's FIRST publication is priced as the fix
   that produced that round's findings rather than the fix it actually is. `-refreeze` prints how many
   changed lines the tag just moved over, at the moment the operator can still act on it, and
   `wf stats` excludes such a round from the prediction basis BY NUMBER — there is no ratio there to
   correct, the same reasoning an authoring window already gets. The detection reads GIT, not the
   manifest's `refreezes` counter, because that counter is written only by this flag: measured on the
   gate that stopped instead of closing, twelve extra freeze commits over five rounds left it ABSENT
   from all five manifests, the note built to say so fired not once, and 611 of 8388 changed lines
   (7.3%) were priced against the round that produced them. The claim is that the TAG moved, never
   that two freeze commits exist — a refused re-freeze used to commit its manifest before refusing, so
   a workspace predating that fix carries a second freeze commit for a round whose reviewers all read
   the same bytes.

   **It also records the corpus SIZE, in lines, with the delta against the previous freeze.** One
   number per round, and it answers a question no other number in the workspace does: a gate whose
   document went 332 to ~1050 lines over five rounds while its blockers ran 13, 17, 10, 16, 11 is a
   design being ELABORATED, not one being settled. The blocker count alone reads as steady progress.

   **Freeze also RESOLVES the citations, and names every one that no longer does** — a note, never a
   refusal, because a round whose whole purpose is re-aiming them must still freeze. It counted the
   anchors and never asked whether the citations still pointed at anything: on a gate that shipped its
   own first slice mid-run, the target moved under a document frozen ten rounds earlier and 26
   citations drifted by a few lines each, read as correct by every producer of the next artifact and
   found at close. Running `wf check cite` while the cited document is the artifact under review is
   not enough — the tree it cites keeps moving after that, and freeze is the moment every round has.
   **And it names a round whose artifacts were changed without `wf apply`.** Every gate discipline
   survives a hand edit; only the measurement breaks, silently — one gate stopped using `wf apply` at
   round 18 for a real reason and found out at close, seven rounds and 39 findings later, all of them
   absent from the cost meter. Hand reviewers every path in the manifest, and diff
   every path in a delta round. Otherwise you patch the working copy while they read, their line numbers
   drift, and a round burns findings on defects you already fixed (measured: two of four findings in one
   round).

   One manifest over all artifacts replaces the per-artifact hash files this step used to prescribe
   (`wfcheck --freeze` writing `.wf/gate/<sha8>-<name>`, one call per document, one `sha256sum -c` per
   document before launch). The failure it removes is the one an operator cannot see: a freeze that
   covered three artifacts and forgot the fourth looks exactly like a freeze that covered four. The
   target's HEAD is in the record for the same reason — a frozen gate that cites line numbers is
   meaningful only against one revision of the repository it reviews.

   **Then bind the launch to the manifest.** Once the substance moves into a context document — which is
   what the fan-out blob rule (cost discipline #2) drives it to — the running script reads that document
   at its MUTABLE original path. Freezing a copy for reviewers does not stop the launched run from
   consuming a version edited after the gate closed: reviewed and executed silently differ. The check
   immediately before calling `Workflow` is **`wf verify`**, and it must be that rather than a look at
   the working tree, because a clean tree proves nothing here: artifacts are changed through `wf apply`,
   which commits as it writes, so an edit made AFTER the gate closed leaves the tree exactly as clean as
   one made before it. Demonstrated on this very document set — after a post-freeze apply,
   `git status --porcelain` printed nothing while verify reported HEAD one commit past the tag and a
   changed sha256. Verify re-hashes every artifact against the frozen manifest and FAILS on a changed
   hash, an artifact ADDED after the freeze (a manifest over three files and a set of four looks
   exactly like a manifest over four), one that vanished, anything uncommitted, a missing tag, a
   frozen manifest that differs from the copy stored under its own tag (a record the thing it records
   can edit is no record), or a target repository that has moved, whose tracked files differ from its
   HEAD, or whose HEAD does not resolve at all — every line number the artifacts cite is a claim about
   one revision of the repository under review, and untracked files there are a note rather than a
   refusal, since build output beside a repository cites nothing. A HEAD past the round's tag is printed as a NOTE, not a
   mismatch: recording findings and opening the next round move HEAD without touching an artifact, and
   a check that failed on those would make the bookkeeping break its own gate. A freeze written by a
   different `wf` version is a NOTE for the same reason — a rebuilt binary legitimately verifies an
   older freeze. Refuse to launch on any mismatch; read the notes. When EVERY mismatch is uncommitted
   round bookkeeping (a stray file under `rounds/`, never an artifact), the verdict says so — the
   reaction is "commit it", not "re-freeze", and the two used to be indistinguishable without reading
   each porcelain line.

   **When verify says the target moved, `wf check drift` turns that fact into a work list.** It
   intersects the target's frozen..HEAD diff with every cited range and classifies each affected
   citation: EDITED — the cited lines themselves changed, so the claim beside them must be re-read;
   SHIFTED — insertions or deletions above moved it, and the re-aimed range is printed, a patch
   instead of a search; GONE — the file no longer exists at the current head (a rename is reported as
   gone on purpose: the citation names the old path); UNRESOLVED — the path names no single tracked
   file at the FROZEN revision, so there is nothing to measure drift against. This is the only
   mechanical signal the
   unanchored majority get — their range check stays green forever, because both lines keep existing —
   while anchored citations also self-report through `wf check cite` once they shift. Drift is
   measured from the newest frozen round (name another with `-round N`) and is not part of
   `wf check` without arguments, which must keep working before anything is frozen.

   **`wf check cite -at frozen` verifies the document against the bytes its reviewers were handed** —
   the target revision the newest freeze manifest names — and `-at <rev>` takes any revision. A
   `-kind run` round moves the target by everything the run wrote, so every citation in the frozen
   document then resolves a few lines late: measured on one gate, 16 cite failures against the working
   tree and ZERO against the frozen revision, which is drift and not error. The check says which
   revision it read in its header, and when the target has moved and findings exist it names the move,
   the round it was frozen against, and the round's kind.

   A companion document must **declare its anchors explicitly and uniquely**, so a static check can
   confirm that every anchor a prompt names still exists. That is the whole of what holds statically: a
   reference that still resolves but no longer means what the prompt assumes is a reviewer's finding, not
   a checker's.

## Citations and anchors

**Write every citation with an anchor, and let `wf check cite` hold them.** A citation is
`path:LINE`, `path:A-B`, `path:LINE#anchor` or `path:A-B#anchor`, and **the path must carry an
extension** — a short suffix containing at least one letter, which is what separates a path from the
last octet of a dotted numeric address. A reference to an extensionless file (`Makefile:40`) is not
recognised as a citation at all: it is absent from the count the check prints, so it is unchecked
without being reported as unchecked. Give such references a line of prose instead of a
colon-and-number — and the check now prints the ones it can SEE: a citation-like reference to an
extensionless TRACKED file is listed as "NOT verified" on every run, because on the first live gate a
correct `Makefile:90#corepack` sat invisible in a green report of 237 citations and only a hand grep
found it. Every recognised form is checked for RESOLUTION
(the path must resolve to exactly one tracked file) and for RANGE (the cited lines must exist); the
`#anchor` form is additionally checked for CONTENT, and that third check is the only one that catches a
citation which resolves, sits inside the file, and still points at the wrong line — naming the nearest
line that does contain the token, which turns a finding into a patch instead of a search.

**An anchor is an ASCII token, by RULE, not by limitation** — the artifacts are English-only, and the
check enforces the boundary: a `#anchor` its grammar does not accept (Cyrillic, spaces) is reported as
an ERROR, never silently counted as unanchored. Measured on the first live run, before this rule was
enforced: 15 Cyrillic anchors dissolved into "17 unverified", hiding both that only two real unanchored
citations existed and that one of the invisible anchors pointed at a line that did not contain it. When
the cited line of a non-English target offers no ASCII token, cite the range without an anchor — the
unanchored count then reports the gap honestly instead of a wrong anchor hiding it.

Measured on one phase's round-1 artifacts: **148 citations, 16 findings — 15 an ambiguous bare
filename, one a path resolving to no tracked file at all, and NOT ONE out of range.** That distribution
is the argument for anchors. Of the five citations that gate shipped wrong, the four later rewritten
with anchors were all caught that way and none by the range check: both the cited line and the correct
line existed, so resolution and range are blind to the whole class. So the check prints how many
citations carry NO anchor on every run — at 148 unanchored, a green report has verified almost nothing,
and it must not be readable as if it had. Prefer a bare filename never: it is accepted silently
wherever it happens to be unique, so a green check over `repo.go:630` proves nothing about which
package you meant, and it starts erroring only once a second `repo.go` appears.

## The mechanical sweep: `wf check dup` and the `[RN]` rule markers

  **`wf check dup` is the mechanical sweep for every copy of a rule a fix changed.** It cuts the artifacts into sentences, normalises away quoting and
  case (the same rule is written with plain backticks in a document and escaped ones inside a template
  literal — that mismatch is what let a corrected rule stand beside its stale twin), groups the
  near-identical ones across files, and flags a group only when the copies BOTH read differently AND
  were last edited in different commits, by line-level `git blame`. Both conditions are needed:
  identical copies edited apart are a rule correctly restated, and differing copies written in one
  commit are a rewording the author already saw side by side. Measured on that phase's artifacts: 2065
  sentences, 176 cross-file groups, **34 flagged**, in under a tenth of a second. An earlier version
  keyed on file-level history and flagged 161 of 176 — a check that paints 91% of its output is a check
  nobody runs twice, which is the real bar for a sweep tool.

  On a DERIVED artifact the same two conditions fire by construction — a context document that
  deliberately restates the decisions produced 21-25 flagged groups per one real contradiction on the
  first live gate — so **`-since gate-rN` narrows the OUTPUT to groups a copy of which was touched
  after that tag**: a divergence that survived earlier freezes was seen and left standing. The hidden
  count is printed; a filter that hides silently would turn focus into blindness. Sentences are
  UNWRAPPED from hard-wrapped paragraphs before splitting (P2e: 92 divergent groups, zero real —
  every one a wrap artifact of two files filling lines at different columns), and copies that agree
  except for a NUMBER or number-word are flagged NUMERIC: four of P2e's six real contradictions
  differed only by a count, a class no similarity threshold separates from agreement. Copies that
  agree except for a COMPARISON (`== 3` beside `!= 3`) are flagged OPERATOR for the same reason —
  the normaliser erases every symbol, so without this the pair reported as `sim 1.00`, an exact
  match, which reads as agreement over a direct contradiction. Both classes FAIL the check.

  The near-match pass is candidate-filtered by rare tokens, so a sentence built entirely of common
  words has no candidate at all; the report prints how many sentences that leaves invisible, because
  a filtered corpus must not read as an empty one.

  **What no similarity metric reaches, `[RN]` rule markers make mechanical.** Copies that CONTRADICT
  in different words share no wording — eight of those on the first live gate, every one found by a
  reviewer and none by dup. Mark the sites of one rule with the same id — R12, say, written in
  square brackets — and `wf check rules`
  (part of `wf check` with no argument) demands they normalise identically, quoting and case erased
  as in dup. **A marker means THIS TEXT STATES THE RULE.** A sentence that POINTS at one — "because
  of R2", "forced by R4", "so R8 is satisfied by" (each written with the marker) — states nothing,
  so the check compares it
  against the statement beside it and reports a conflict that is not one: measured on a live gate,
  four ids in a single run, and the repair chosen was rewording the ARTIFACT until every id had one
  site left and the check reached nothing at all. Drop the marker from the reference instead. The
  population line prints the marker count, the id count and how many ids were actually COMPARED —
  zero markers is a corpus that opted out, and ids all sitting at one site each is a check that ran
  over nothing; neither is a pass — the header line says NOT A PASS whenever nothing was compared,
  the UNMARKED corpus included, because the note under it was missed on a gate carrying eighteen
  markers and eighteen ids: the other three numbers on that line read like a pass, and
  `0 marker(s), 0 id(s), 0 compared, 0 conflict(s)` reads like four of them. **Mark the PAIR, not the rule you happen to be proud of.** One
  id per rule at one site each is decoration; the check reaches a rule only where the same id marks two
  or more sites. The class it is for is a description and an enumeration of the same set disagreeing —
  the single most expensive failure of one decision gate, three separate times, each costing rounds —
  so the id goes on the sentence that describes the set AND on the list that enumerates it.
  The unmarked contradictions stay a reviewer's find.
  **Mark the SCRIPT's restatements too.** A `.js`/`.mjs` artifact is compared as PROSE — its comments
  and the literal chunks of its strings, each literal its own text, never the code — so a prompt that
  restates a decision is a rule SITE like any other, and marking both it and the document's statement
  with one id is the only mechanical comparison of what the two copies SAY. `dup` sees the pair — a
  script restating a decision reports `sim 1.00` for a whole gate — and correctly declines to call it
  a defect, because that restatement IS the design. Measured on mocker A3: six of round 10's twelve
  blockers were one decision that moved in the document during round 9 and did not move in the prompt
  restating it, and the most expensive instance carried, verbatim, the document's own sentence warning
  that this half read alone is wrong. `wf check stale` catches the vocabulary half and misses every
  restatement that paraphrased the old rule in new words, which is most of them.
  **Mark the rules while
  the gate is still running, not afterwards** — this is where the check earns its keep, and it earned
  it on the first run that did: two rules marked after they had bitten in three consecutive rounds
  produced a divergence across the DERIVED pair on the check's first run over it, which is exactly the
  place `dup` is structurally blind (23 flagged groups on that pair, all false). Markup is free: a site
  may carry emphasis, sit in a bullet or a table cell, and share its line with other prose — sites are
  compared as sentences. Every conflict prints at the point the copies stop agreeing, so read the
  printed line, not the file.

## The fix protocol, and the cost meter that sizes a batch

- **The fix protocol lives in `gate-checks.md` section I.** Short version: record what the
  round found with `wf round record <file>` BEFORE touching a fix — that file is what briefs the next
  reviewer; apply the round's fixes as ONE `wf apply` batch, one edit per artifact per rule, each
  declaring its own occurrence count; fill the `closed by` column with the apply commit and re-record
  (`-round N`) — the next freeze refuses while a closed round's blocker stands open, and a record
  naming a commit that does not exist is rejected before it writes; read the block around each edit
  after making it; sweep with
  `wf check dup` for copies that DRIFTED and `wf check stale` for the consumers the fix's OWN old text
  names, then hand-check for copies that CONTRADICT, which no similarity metric reaches; on a SCRIPT,
  add `wf check schema` where the batch touched a SCHEMA: it compares what every agent's output schema
  obliges its agent to fill, against the previous freeze, and a DROP is the finding. ONCE so far a
  `required` array has disappeared inside an edit whose stated purpose was WIDTH — leaving an
  acceptance report where every property could come back without the evidence field the gate over it
  had to read — and once a width-reduction edit left a stale band CONSTANT behind instead, which is
  the same edit doing two jobs and saying so about one. Nothing static reaches the first where the
  leaf is built in a `.map()` callback, and both static tools were clean over that script;
  add `wfdry <script> --prompt-diff <a>,<b>` for each pair of agents the batch gave the same job — the
  rule and its measurement are in `gate-checks.md` section B, "Two agents asked the SAME question";
  run `wf check closure` before recording the closures, because it asks of every `closed by` sha the
  question `wf round record` does not — whether that commit's diff reaches the SECTION the finding
  cites (`D9`, `R12`: the form a ledger uses, and the only one that reaches a finding citing no line);
  a FILE miss is a closure that did not land, a section miss is a question to re-ask, and a closure
  carried by two commits is checked against each one's own parent rather than against a union of
  coordinate systems; re-run `wfcheck`, the JS linter and `wfdry`; open the next round,
  `wf freeze`, and `wf verify` before launching. Size the batch by the workspace's own cost meter:
  `wf stats` prints each round's edits, its CHANGED LINES and the findings per 100 changed lines —
  counted under a PINNED diff algorithm (`histogram`), so the denominator is comparable across gates
  and across machines rather than reading whatever `diff.algorithm` the operator's gitconfig carries;
  measured, the two readings of one gate differ by 2.8% overall and 5.3% on a round — and
  `wf apply -rule RN` declares the rule a batch changes and NAMES the other sites carrying that
  marker, marking the ones in files the batch wrote — the sweep the note beside the number has been
  pointing at since the meter existed, done at the moment the fix lands. With the id form the batch is
  **REFUSED, before anything is written**, when a marked site sits in a file the batch does not touch:
  `-rule-ack` is the waiver for a batch that legitimately corrects one site while the others stand.
  A note arrives after the round has already spent its edit, and the sweep it advises is then a
  separate act of discipline — measured on one gate, four times in four consecutive rounds the fix
  landed where the finding was READ rather than everywhere the rule ACTS, and none of the four was
  reachable by `dup`, `stale` or `rules`, because the copies shared no wording. Those four would have
  been caught by NONE of this refusal: that gate marked nothing, so the id form reached nothing there,
  and three of the four are copies inside ONE file, which is behind the FILE half. It is an argument
  for marking rather than a mechanism that caught them. A second site inside a file the batch DOES
  write is not refused either, and the site list beside the refusal is what reaches those. **`-rule` also takes a
  free-text rule NAME** (`-rule "bar accounting"`), which sweeps every sentence of the artifact set
  carrying all of its words: a script gate's rules live in prompts and are marked in no document, so
  the id form reaches nothing there — measured, the flag was refused outright and unusable for a whole
  gate whose most valuable sweep it is. The phrase form says out loud that it is the weaker half: it
  names the sentences that use the words, not the sites of a rule;
  `wf apply` notes when a batch's CHANGED LINES predict a costly next round — measured, the case that
  matters is few edits over many lines: one gate's two-edit window changed 933 lines, 931 of them in
  a single commit, and the per-edit meter — which had seen only the other, one-line edit — called the
  window "38.00 finding(s)/edit". The note also says the half no tool can check: if the batch
  changed a rule, its consumers are not in the diff. Split by RULE — one rule and all its copies per batch — never
  mechanically: which edits belong together is judgement, and all-or-nothing is per batch.
- **What a fix costs is decided by whether it changes a RULE, not by how many edits it has.** One gate
  measured ~0.25 findings per edit and called it stable across a 12x range of batch sizes; the next
  ranged 0.15 to 1.75 over 150 edits, and batch size predicted nothing — its FOUR-edit batch produced
  the most findings of the run and its thirteen-edit batch the fewest, closing the gate. What separated
  them: the expensive batches each changed one rule (every declared path parameter becomes REQUIRED;
  the declared set is derived from `basePath + route.Path`, not `route.Path`), and the cheap one was
  thirteen edits of renumbering and prose. **The separation is visible in the denominator the tool now
  prints.** Across the three closed gates the MIDDLE of the distribution has a prose fix at roughly 2
  to 5 findings per 100 changed lines and a rule-changing fix at roughly 6 to 15 — the four-edit batch
  above reads 14.9 and the twenty-seven-edit prose batch of the same gate reads 3.4. The tails are
  real and are not the rule: a round whose window is the authoring commit prices a corpus its batches
  did not write — it read 377.8 per 100 on one gate and `wf apply` predicted ~355 findings for a round
  that produced 24 — so the tool now prints NO ratio for such a round and excludes it from the
  prediction basis, naming the exclusion; a round whose few changed lines are post-gate tidying still
  reads in the hundreds and still says nothing about a fix. **Read the SERIES, not the pooled ratio**,
  which `wf apply` now prints: one gate ran 5.4, 7.1, 8.7, 10.3 findings per 100 changed lines while
  its fixes SHRANK (447, 354, 264, 204 lines), so the pooled figure predicted 5-6 against actuals of
  21-25, while a script gate measured the same ratio flat across a 12x range of batch sizes. The two
  do not disagree: batch size predicts cost while what is left to fix is PROSE, and once the work
  reaches the tightly-coupled core every changed line is a rule-changing line, the denominator shrinks
  faster than the numerator, and the prediction is a floor. **A RISING findings-per-100 under FALLING
  blockers is a gate converging, not a gate degrading** — and the direction of the prediction's error
  flips mid-gate, so a series read for its level rather than its shape reads convergence as collapse.
  Measured on a script gate: 2.5, 4.1, 8.0, 9.3 per 100 while its blockers ran 6, 3, 0, 0 and its
  fixes shrank 785, 367, 125, 53 lines; at round 18 `wf apply` predicted from 10.6 against an actual
  2.5, at round 21 from 8.3 against an actual 9.3. The blocker count is what the stop rule reads; the
  ratio says how much re-reading the next fix buys. An edit is not a unit and never was: one
  two-edit window of another gate changed 933 lines, 931 of them in one commit, and the per-edit
  ratio printed the window as `38.00 finding(s)/edit`. Read the per-100-lines figure and read your own workspace's, not this paragraph's. **A fix that changes a rule re-opens every consumer of that
  rule, and the consumers are not in the diff.** Round 5 of that gate was the only round in which the
  blocker count ROSE, and four of round 7's seven findings were sentences arguing from the superseded
  form.

## After a rule change, sweep for restatements of the OLD rule

- **After a rule change, sweep for restatements of the OLD rule, not for the rule's name.** Three of
  four rule-change ripples on that run were sentences that argued from the old rule while containing
  none of its keywords — `grep -ni parallel context.md` returned nothing while a paragraph two lines
  below a corrected bar still described the parallel shape in other words. `wf check dup` finds the
  copies that drifted; **`wf check stale -since <ref>` finds this class mechanically**, because the
  apply batch is the one artifact in the pipeline that records the exact text each fix replaced: it
  keeps the tokens the fix RETIRED, drops the ones the corpus uses everywhere, and prints every
  untouched sentence still carrying them, with the tokens beside it. What it cannot reach is a
  consumer that shares NO vocabulary with the old text, and a fix that only added text retires nothing
  at all — the check says `NOT A PASS` there rather than printing a clean line. So it narrows the read
  of every paragraph the rule's CONCLUSION touches; it does not replace it, and it is cheaper than the
  round the class otherwise costs.

## 4. When the gate is done: `wf close` and `wf prune`

4. **When the gate is done: `wf close`.** It writes the gate's own record into the workspace and
   commits it — the commit log as text (the timings that price every round live there and nowhere
   else), `wf stats -time` verbatim, the freeze manifests' surviving half, every FILE of every round
   — the findings ledger and the delta brief that round's reviewer was actually handed — and a
   feedback scaffold with the derived numbers already filled in — and the artifacts themselves, whose
   CONTENT is the one thing prune destroys that nothing else holds. `-artifacts=false` opts out for a
   gate whose artifacts live somewhere that outlives the workspace, and prune then names each of them
   before it removes them. Fill the scaffold and
   run `wf close` again to bank it; a filled form is never overwritten. `-archive DIR` (or
   `$WF_ARCHIVE`) copies the record out to a corpus that outlives the workspace.

   **Write the feedback while the gate is still in front of you.** Three gates were written up by
   hand, each invented its own structure, and the section that turned out to be the most valuable is
   called two different things across them — a corpus that cannot be searched for its own best
   section. The numbers are prefilled because the first of those post-mortems closed its gate on 99
   findings and 27 blockers against a true 98 and 26.

   `wf prune -yes` then deletes `.git`, `artifacts/` and `rounds/`, leaving the record and the
   gate-checks copy that briefed the reviewers. It refuses without a record, refuses when the record
   is older than the artifacts, and reports without `-yes`: the deletion is not reversible. Measured
   on one closed gate: 4.2 MB down to 696 KB, and everything anyone has ever read out of the
   repository is in what stays — `.git` was 84% of the workspace's blocks and the whole of its
   history as text is 12 KB.

- **An edit after the gate closes is still an edit.** The stop rule permits majors to ride; it does not
  make applying them free. Measured: four "text-only" major fixes applied after a clean closing round
  broke the artifact outright — the static checker went from zero errors to one and the dry runner from
  every mode to none, because the new prose carried a character the surrounding quoting treats as
  syntax. So: anything you change after the gate closes goes through the static AND dynamic check again
  before launch, and anything that touches logic rather than prose re-opens a round. "Cosmetic" is not a
  category the tools recognise, and it is not a category you can reliably assign to your own edit.
- **Optional canary** when a prompt exceeds ~25 KB or the phase's shape is new: spawn ONE cheap agent with
  the kernel prompt and have it return a PLAN only, no code. The gate can check that a prompt is written
  correctly; only a canary shows whether it *reads* the way you think.
