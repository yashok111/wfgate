# Workflow pre-flight script review (the launch gate)

**HARD RULE — before you ever call the `Workflow` tool, the authored script MUST pass a pre-flight review.** This is a one-time gate on the *script source itself*, run BEFORE the workflow launches — distinct from the section-review gate below, which runs INSIDE the script after each section produces artifacts. Run it out-of-band with the `Agent` tool: spawn the reviewers (concurrently — multiple `Agent` calls emitted in one message run in parallel; sequential is fine too, order doesn't matter for a read-only gate), hand each the full script + the user's task/goal but point each at its own vector's focus, collect findings, then ACT before launching. Both this gate and the section-review gate are fixed harness procedures, NOT authored workflow logic — they are themselves exempt from review (no infinite regress; the fixes they demand are re-reviewed, the gates are not).

Reviewers are **`model: 'sonnet'` — every lens of this gate AND of the decision gate in step 1.** Changed 2026-08-29, and the policy it replaces is worth naming, because its argument was right about the wrong quantity: opus on all three was justified as "the last check before launch", which prices the gate as ONE round of three reviewers. It is not one round. The ten gates that have run through `wf` spent **166 rounds** between them (13, 17, 13, 25, 13, 24, 22, 10, 15, 14, by each gate's own `wf stats`, re-derived 2026-08-29) at one to four lenses a round, so the reviewer TIER multiplies by the round count — the cost function this manual already names — and that product, not the launch, is what an operator runs out of. A gate that stops because the quota stopped is the worse outcome: the round that is never dispatched finds nothing at all, and `wf stats` cannot tell it from a round that found nothing. Depth is the axis to keep, and it is the cheaper one — a reviewer's value is in how hard it reads, and for these out-of-band `Agent` calls that comes from the reviewing session rather than from a per-call `effort` (see `model-routing.md`), so do not drop the session's effort while a round is running. **Opus on a gate reviewer is a per-round exception the operator argues for out loud**, in the round's own lens roster (`wf round lens <prompt-file> -name N -model opus -asks Q`): a security-/correctness-critical artifact where a subtle, high-cost-if-missed defect is plausible, or a single fan-in lens reconciling genuinely conflicting reports that nothing downstream re-checks. If you cannot name the specific miss it guards against, it is sonnet. Two routing-rule clarifications survive the change: (a) these are standalone out-of-band `Agent` calls, so the fan-out "producers are cheap" rule does not apply — the tier list in `model-routing.md` governs; (b) they are gate reviewers, exempt from the no-N-same-role-in-parallel ban, so running every lens of a round on one tier is intended. They run out-of-band BEFORE the workflow launches, so they do NOT draw on the script's `budget.total`; the Proportionality rule below scales the **count**, and the tier is now scaled by this paragraph. The external lens (step 1, `vcodex`) is untouched — it bills against a different quota, which is a second reason the standing instruction to alternate internal and external lenses between rounds is worth following.

**Before you spawn a single reviewer, in this order.** The order matters and is not the order these
paragraphs were written in — it is: GRILL the decisions into existence (-2) → open the WORKSPACE
(-1) → open the ROUND (-1b) → gate the
DECISIONS (1) → author the script → static check (0) → **dry-run the artifact set (2b)** → freeze the
whole set (2) → hand reviewers the frozen paths (3) → and when the gate is done, BANK it (4). The dry-run comes BEFORE the freeze, reversed
2026-08-22: a dry-run defect found after freezing invalidates a freeze, and republishing it
(`wf freeze -refreeze`) means reviewers handed the old tag read different bytes; before the flag
existed it forced a whole round for a defect no reviewer ever saw (measured, P2e r12) — the
dry-run reads the working copy, and the freeze exists to pin what REVIEWERS read, so it comes after
the set executes clean. Each delta round repeats: collect the
round's findings and record them with `wf round record` → bank the vectors that produced them with
`wf round lens` → fix with `wf apply` → fill the closed-by
column and re-record (`wf round record <file> -round N`) → static check → re-dry-run → open
the next round → re-freeze → `wf verify` → hand the next reviewer `wf delta`.

**What the operator actually thinks, at the moment the discipline breaks.** Three rows are a measured
drift OUT of the tool and two are an over-reading of what it covers; all five were committed by
somebody who knew the rule. The rules are elsewhere in this manual and are POINTED at — the middle
column carries the consequence and no figures, because a measurement restated is a measurement that
can drift apart from itself, and nothing here compares the copies.

| the thought | what is true | where the rule is |
|---|---|---|
| "This edit is one word — quicker by hand than through `wf apply`" | The hand edit gives up all-or-nothing AND the declared occurrence count, and it drops the round out of the cost meter. One gate did it for several rounds and found out at close, with those rounds printing a bare zero | step 2, and `gate-checks.md` I |
| "`wfcheck` is 0/0 and `wfdry` is 7/7, so the script is checked" | `wfdry` runs control flow; `wfcheck` reads the parsed program and only what a prompt or an `owns` array DECLARES. A file named in neither is invisible to both, and one gate's round-19 blockers included two of exactly that shape | steps 0 and 2b |
| "This vector came back early and its findings are obviously correct — I will start fixing now" | Fixes land at a barrier. One gate applied a returning vector's fixes while two others were still reading, and one of those then reported as open a blocker that was already closed | `gate-checks.md` I |
| "The check printed its numbers, so something was measured" | An empty population prints numbers too. `rules`, `stale`, `closure` and `schema` say NOT A PASS when they compared nothing; `cite` and `dup` say it in the other shape, by printing the unverified or hidden population — on those two, that line IS the verdict and there is no phrase to search for | `gate-checks.md` A |
| "The freeze is stale — I will delete the tag and freeze again" | A hand `git tag -d` moves every window `wf stats` derives from it. The tool then refuses that round a ratio and drops it from the prediction basis BY NUMBER, so what the shortcut costs is the measurement, not merely its accuracy. `wf freeze -refreeze` republishes, moves the tag and records the supersession | step 2 |

-2. **Grill the DECISIONS into existence, before the workspace exists.** The document round 1
   reviews is written in a window no command in this toolchain can see. `wf stats -time` prices it — run it across the
   workspaces you have; the window has run from a couple of minutes to well over an hour, and its
   short end is not fast authoring but the gates whose document was drafted BEFORE `wf init`, so the
   drafting itself falls outside every measurement banked anywhere. What that window produces is read
   for the first time by round 1, and **round 1 is the most expensive round of every gate on
   record** — by a wide margin, in blockers and in findings alike, every one of them filed against a
   document nobody had questioned before it was frozen.

   The procedure is an INTERVIEW of the operator, and it is breadth-first. Map the slice as a design
   tree: every decision branches into the decisions that hang off it. The **frontier** is every
   decision whose prerequisites are already settled — the questions that can honestly be asked
   without guessing at an answer nobody has given yet — and a **round** is one frontier, asked in
   full: number each question, put the recommended answer on its own line so the round is answerable
   by number, and then WAIT. Two questions never share a round if one depends on the other; a
   question hanging on an answer still open belongs to the next round. Answers push the frontier
   outward and the next round is recomputed, never pre-written. This is not a `Workflow` and it
   spawns no fan-out; it is the operator's own session asking and the operator answering.

   **Facts are the interviewer's job, decisions are the operator's.** A frontier question the
   filesystem, the target repository or a command can settle is dispatched to a sub-agent — cheap
   model, cheap effort, the producer routing in `model-routing.md` governs — and the rest of the
   frontier is asked while it runs, because a running exploration is an unsettled prerequisite for
   its own branch and for nothing else. Never ask the operator something you could have looked up.

   **And the same question is asked of an ARGUMENT, not only of a fact: price the observation before
   spending a round on it.** Rank an open question by what it costs to OBSERVE, not by how interesting
   it is to reason about — where one throwaway agent, one command or one request answers it, go and
   get the answer, because a round spent arguing buys a document that sounds settled and nothing
   else. Measured on the class in `gate-checks.md` section E: a gate measured its own figures to the
   byte, wrote down that the other half "cannot be measured from here", argued it over two of its own
   rounds and then simply asked — and had the refusal back in a fraction of a second for no tokens.
   The rule is not LIMITED to that class. An observation settles the point it was taken at and not
   the band around it, so record what was observed and re-run the argument against it rather than
   banking a point as a rule. Where the observation is genuinely out of reach, write down what it
   would take: that sentence is what stops the next gate repeating the argument.

   **A recommendation is an OPTION, never a decision.** A question the operator did not answer is
   UNSETTLED, and it enters the draft as an open question rather than as a decision. This is the one
   rule that cannot be relaxed: an author who adopts the interviewer's answers hands round 1 a
   document the interviewer designed, so the gate's whole value — an independent read — is spent
   before the workspace exists. The same failure is documented upstream, where the interview
   technique comes from, as its most reported defect: an agent that answers its own questions has
   broken the procedure, not interpreted it liberally.

   **The first frontier is the CRITERION, not the design.** Every decision under it is judged by it,
   so it cannot be a later round, and it is the section this corpus has paid the most for: read
   `gate-checks.md` section D before the first clause is written. An acceptance section states
   OBSERVATIONS, never test designs — the gate that broke that rule rewrote its acceptance section in
   four consecutive rounds. Grill each criterion for the wrong implementation it exists to fail; a
   clause with no named defeat is a clause with no floor.

   **The output is the DOCUMENT, and a decision that survives only in the transcript did not
   survive.** Nothing downstream reads a conversation — not `wf apply`, not a reviewer, not
   `wf check rules`, not `wf check stale`. Every settled decision lands in the draft as a numbered
   section, and a rule stated at more than one site carries a `[RN]` marker at every site while you
   are still writing it, because that markup is the only thing `wf check rules` can read and a corpus
   carrying none compares nothing. Do NOT `wf add` the transcript: a derived document beside its
   source is measured noise (a derived context document yields 21 to 25 `dup` groups per one real
   contradiction) and every reviewer of every round would pay for it.

   **It ends when the frontier is empty AND the operator says the understanding is shared** — an
   interviewer that runs out of questions and starts drafting has replaced the interview with its own
   opinion. Then, and only then: `wf init`, `wf add <draft>`, `wf round new`, and round 1 reads it.
   The interview is NOT a round: it dispatches no reviewer, files no finding, banks no lens, and
   nothing about having run one lowers the round count you budget for. The checks never impersonate
   coverage and neither does this — an interview is the author sharpening their own thinking, a round
   is an independent read of frozen bytes.

   **Two honest limits.** The frontier is the interviewer's judgement and not a computed graph, so
   two questions can share a round and only afterwards prove dependent; when that happens, say so and
   reopen the branch in the next round. And the saving is a HYPOTHESIS: no gate on record ran this
   step, the round-1 blocker counts of the gates on record are the baseline it has to beat (take them
   from `wf stats` per gate — a pooled figure quoted here goes stale at the next gate), and ONE gate
   under the band is not evidence. The interview lengthens a window nothing measures in order to
   shorten a round count that is measured, and the exchange rate is one median round.

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

   Each replacement also declares how many occurrences it expects, and **that `count` is scoped to ONE
   file**: it catches a rule you believed appeared once in an artifact and that appears there twice. It
   does NOT reach a copy living in a second artifact — declaring `"count": 2` for a rule split across
   two files rejects the batch outright, and the obvious repair (drop it to 1) leaves the other copy
   standing, which is the very defect. The correct shape is **one edit per artifact in the same batch**,
   each with its own count; all-or-nothing is then what guarantees that both copies move or neither
   does. A hand edit plus `git commit` gives up both properties, which are the only reasons the command
   exists.

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
   NOTHING says so: see the stop rule below, and the refusals in `wf freeze` and `wf close`.
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

0. **`wfcheck -root <the repository the script will run in> <script>`** (`~/.local/bin/wfcheck`) — a
   static checker for these scripts. `-root` is load-bearing: with the wrong one, the ownership check
   WARNs that every owned path is missing and the drifted-test-name check silently does not apply.
   **Run inside a gate workspace it defaults to that workspace's `target`** and says so in a note; run
   anywhere else it defaults to the working directory. It used to default to the working directory
   always, and a live gate paid for it — twenty-two "owns X, which does not exist" WARNs on a correct
   script, because a script's owned paths are relative to the repository under review and the
   workspace is not that tree. Pass `-root` explicitly to check a script against a tree the workspace
   does not name. It must report
   **zero ERRORs** before the gate runs. It catches, for free, what four phases of opus gating kept
   spending rounds on: a report field named in a prompt that the agent's schema does not declare; a
   SIG/contract block interpolated into the writer but not its callers; one file claimed by two agents;
   `git add -N X && git diff` (which hides the diff entirely); a lister used as a gate that exits 0
   whether or not it printed (`gofmt -l X &&` and its equivalents in other formatters); a
   `<placeholder>` inside a fenced command; `phase()` titles that do not match `meta.phases`; a call
   that goes straight to `agent()` with no wrapper; a missing
   `model:`/`effort:`/`schema:`, and an `effort` outside `low|medium|high|xhigh|max`; an unused const;
   per-agent prompt sizes and per-blob multiplicity; a required
   schema field no consumer's prompt explains; a test name the tree does not declare; **and a path a
   producer owns that no gate's `owns` array lists** — a reviewer briefed on that file can file a
   blocker the fix agent that gate spawns is forbidden to touch. It is script-wide, not per phase: in
   the shape this manual prescribes a producer runs in `Build` and the gate reviewing it in `Gate 1`,
   so joining the two on the phase title would report every correct script. **Read its boundary before
   you trust it**: it enumerates the paths a prompt DECLARES, so of the three blockers of this shape on
   the gate it came from it reaches the one where a producer declared a path no gate listed. The other
   two were files that appeared in NO `YOU OWN` block and in no `owns` array — a phase was ordered by
   its prompt to convert files nobody had assigned to anyone — and which files a phase was ORDERED to
   touch is judgement, not a list this tool can read. A `owns` entry is a sentence about a path
   (`internal/admin/server.go (the one comment, nothing else)`), so it is read for the paths it names;
   an entry naming no path this tool recognises, or an array it cannot resolve, makes the remainder
   NOT JUDGED and says so by gate name — a path missing from a list it could not read is not a path
   nobody owns. A gate is recognised as `owns` AND `reviewers` together: `owns` alone is a key an
   author writes on an agent's own options object, and counting one made a script with no gate at all
   report full coverage. It reads the
   PARSED program, so an options object built by spreading a shared const, an options factory, a
   wrapper under any name and an inline schema are all understood — and where it cannot resolve
   something statically it says so as a note rather than reporting what it could not see as absent.
   It also PRINTS its populations on every run — call sites, wrappers, owned paths — because a check
   over an empty set reports zero findings and exits clean, which is indistinguishable from a clean
   script and is trusted more, since a tool said so. **Every size it prints over a prompt composed at
   run time is a LOWER BOUND and says so**, and the checks that read a whole prompt — the schema's
   required fields above all — are skipped for such a call rather than answered over the half it could
   read: asking whether evidence is ABSENT over text the tool never saw is the same polarity error the
   gate briefs its reviewers about, and it cost a live round when a field named in a prompt's first
   chunk was reported as never mentioned. Reviewer rounds are
   worth ~10 minutes and six figures of tokens each — do not spend one on a regex. Re-run it after EVERY
   round's fixes: it has caught an unescaped apostrophe inside a quoted string that would have failed a
   run at launch.

   **The one class that actually recurs is a syntax error inside prompt PROSE** — a backtick or a
   `${` written into a sentence, where the surrounding template literal treats it as syntax. `wfcheck`
   reports it at the coordinate in the file you are editing: the temp wrapper it parses carries no
   newline, so every line number in the parsed program is the file's own
   (`internal/wfscript/wfscript.go:36-38`). A second parser was prescribed here beside it purely for
   that coordinate, against a measured twenty-line offset; the offset no longer exists, and neither
   does the second tool. `wfcheck` still cannot see the other two failure modes of editing these
   scripts — a replacement pattern that no longer matches because the same rule also lives in a
   markdown companion with different escaping, and a patch script that throws before it writes.
1. **Gate the DECISIONS before the prompts.** The run-level `log()` block — slice boundaries, what is
   deferred and why, the architectural calls — is a few KB and is where the expensive mistakes live.
   Measured on one phase gated this way: 8 KB of decision list, two independent reviewers, **six
   blockers and a scope cut** — before a single prompt existed. The scope cut was the more valuable half: both reviewers
   independently showed that a third of the slice was P2 work the phase criterion did not need. Use TWO
   lenses here, not one, and make one external — both on sonnet, per the tier rule at the head of this
   file — they overlapped on two blockers and each found one the
   other missed, for a few minutes of wall clock. Run the external one in this exact form, because the
   two things that make it work are otherwise a file away and get paid for again:

   ```bash
   timeout 900 vcodex exec -s read-only -m <model> -c model_reasoning_effort="medium" \
     "<prompt naming absolute paths>" < /dev/null > /abs/path/round-N.txt 2>&1
   ```

   Redirect stdin or it blocks forever on "reading additional input"; write to a file, never pipe to
   `tail`, or progress is hidden and a hang looks like work; keep it read-only — a second opinion must
   not mutate the tree.

   **Bound what it READS, not only how long it runs.** `timeout` is a backstop, not a budget. Measured
   on one live gate: the external lens spent its whole 25 minutes reading, produced 1.4 MB of trace and
   no verdict, and was killed by its own timeout — one of round 1's two vectors lost inside a
   46.7-minute round. The repair that worked was a NARROWER BRIEF, not a longer timeout. So name the
   frozen artifact paths absolutely in the prompt, say what the lens is not to read, and put the read
   budget in the prompt itself rather than only in the shell. Launch it BEFORE the in-process
   reviewers: it is the one step of a round whose wall clock does not have to be serialized against
   them, and a lens that returns nothing has still spent the round.

   **Ask an external lens to RUN what the artifact claims, not to read it — that is where its edge
   is.** Measured over one script gate's nineteen blockers: the two lenses agreed on three and neither
   dominated, and every one the external lens found alone came from executing rather than reading — it
   ran the extracted quoting line against the real filename and found a `$` the shell ate, it read the
   Makefile instead of the prompt's description of the Makefile, and it raced `git add -N .` across
   concurrent agents in its head because it had run the command. The one disagreement went its way
   too: a clause assigned to two sections read as deliberate from the inside and was a blocker, because
   one walker answers it once and whichever proof exists satisfies it, leaving the other unobservable.

   **This gate has ROUNDS, and they run the same discipline as the script gate below** — barrier per
   round, delta rounds (hand the reviewer the document's diff plus the findings it was meant to close,
   never the whole artifact again), a descending lens count, and the same stop rule. Alternate the
   internal and external lens between rounds: a reviewer that has already passed the document twice
   reviews it by inertia. Measured on the last phase gated this way: **six rounds, 26 blockers,
   7+7 → 5 → 4 → 1 → 2 → 0.** Budget for the round COUNT, not the round: the architectural inversion
   arrived in round 1 and the acceptance criterion was still producing blockers in round 5. Three of
   those rounds found defects introduced by the previous round's own fix, which is the same rate the
   script gate shows — the rule below is not a property of scripts, it is a property of fixes.

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
2b. **Dry-run the artifact set with stubbed hooks, BEFORE the freeze: `wfdry <script>`**
   (`~/.local/bin/wfdry`). It executes
   the script body with the `agent`, `parallel`, `pipeline`, `phase`, `log`, `args` and `budget` hooks
   replaced by stubs (`agent`, `phase`, `log`, `parallel`, `pipeline`, `workflow`, `budget`, `args`)
   that derive each report from the `schema` the script hands that call — the schema
   IS the fixture contract, so nothing is hand-authored per run. Your own `spawn` wrapper is NOT
   stubbed: it is left in place and exercised, which is what makes the note below about a swallowed
   throw true. It must complete every mode before the gate runs. The seven modes are `clean`, `blockers`
   (every gate returns one), `acceptfail`, `unfixed` (a fix round that leaves blockers open),
   `throw-serial`, `dead-reviews` and `no-args` (the script runs with no `args`; every other mode hands
   it a representative object). `dead-reviews` decides what a reviewer is from the label's SEGMENTS —
   `review`, `fix`, `closeout`, `verif`, `accept` at the head of one — never from a substring, and the
   mode PRINTS the labels it killed. Measured: it once matched `review` inside `preview`, killed a
   producer, ended the run at section five and still reported OK over a seventh of the agents it
   exists to exercise, without killing a single gate reviewer. Read the `killed:` line; a mode
   silently exercising something else is the vacuous guard this runner is against. The mode runs the
   PASSING fixture, so the ONLY thing wrong with its run is that those agents returned nothing: a
   verdict that still reads as success is refused by name. Measured over five correct scripts and one
   carrying the defect, the failing fixture returned `green: false` on all six and separated nothing,
   because every surviving agent reported failure too. Killing a named
   agent is not a mode: `--dead <label>[,<label>…]`
   kills those labels in every one of the seven, and a label that spawned in NO mode FAILS the run —
   every guard behind it ran over the empty set, and the operator typed that label; `--args <json>` replaces what the six args-carrying
   modes pass, `--budget N` sets what the script sees as `budget.total`, and `--quiet` prints
   verdicts only. **`--prompt-diff <a>,<b>` prints the sentences one agent's prompt carries and the
   other's does not** — see the sweep in the round discipline below. It refuses two spellings of ONE
   prompt (a text does not differ from itself), refuses a label spawned with several DIFFERENT prompts
   (a per-item reviewer has a per-item briefing, so there is no single prompt to diff), and prints
   `NOT A PASS` rather than "the same sentences" where a prompt contributed no sentence at all. A mode that completes having spawned NO agent FAILS: every stub check is
   then a guard over the empty set, and that is indistinguishable from a clean run — the one exception
   is `no-args` on a script that gates its body on `args`, which is correct behaviour and is noted
   instead. **ONE is not zero, so the counts are also read as a RATIO**: every mode's line carries the
   `clean` mode's agent count beside its own, and a mode reaching fewer than half of it FAILS,
   naming both. Measured: a red-bar abort added to the first producer took five of seven modes from
   15/25/25/25/1/15/25 agents to 18/1/1/1/1/1/1 — six modes then at one agent, `throw-serial` included,
   which is one by construction — with the fix round, the recheck, the acceptance step and the
   final verdict exercised by nothing, and every mode still printing OK, so a reviewer banking "wfdry 7
   of 7" WOULD have credited a layer covering a single agent. Failing rather than warning was measured
   too: across the three correct gated scripts on disk the failure modes run WIDER than the happy path
   (36 against 44, 18 against 30, 15 against 25), so none of them trips it. `throw-serial` is exempt
   because it aborts AT the first spawn by construction, and so is `no-args`, which the runner narrows
   on purpose. A run with no `clean` mode SAYS its baseline is a stand-in, and a single `--mode` run
   says nothing was compared at all — the case where the check used to disappear in silence. **A `clean` run that returns a not-green verdict FAILS the mode**, and so does one that
   returns a failure flag set: no fixture in that mode can produce either, so the script is deriving the
   verdict from something it should not. Measured — the shape it caught: a repair agent spawned only
   when acceptance FAILS, counted in the liveness ledger, so the one run everyone wants logged
   `dead: repair` and returned `green: false`. Seven modes were green over it, because acceptance failed
   in all seven and repair was therefore always alive; an opus reviewer found it in the end by copying
   the script's tail into a file and executing it with green values by hand.
   **A red mode is a fact about the SCRIPT or about the FIXTURE, and the two read alike — so read the
   fixture BEFORE you edit the script.** `wfdry --fixture <label>` prints the object the stub handed
   that agent, per mode, and the clean mode's own verdict violation points at it. Measured on one
   gate: THREE of its dry-run reds were fixture-synthesis limits reported as artifact defects — a
   `$ref` the runner could not expand (a `$defs` written on a sub-schema; the pointer is anchored at
   the schema ROOT, and the note now names the pointer that would resolve), twenty-two booleans
   defaulted false because names like `P8a` carry no polarity, and `finalCount === 22`, an integer
   against a constant that no report-shaped fixture can satisfy — and each cost a reading of
   twenty-two "unmet" rows to diagnose as ONE gap. One of the three ended with the script paying about
   1,900 bytes of width to inline twenty-two copies of a leaf it had correctly declared once.
   A report field whose name the fixture contract does not recognise as pass-shaped or fail-shaped is
   FALSE in every mode, and the runner prints a WARN naming it — including under `--quiet`, and again
   after the tally. That WARN means "no mode exercised the branch guarded by this field": act on it,
   because a green tally standing over it is the false green this whole runner exists against. Print the phase order, the label/model/effort list and the whole return value each time. Seconds of cost; it evaluates every
   template literal on every path and walks branches a parse cannot reach. **The clean path is the one
   that matters most** — reviewers read the failure branches, because that is where the risk looks like
   it lives, so the defects that survive a gate are the ones on the happy path. It is a DISTINCT dynamic
   runner rather than a flag on the static checker because the two answer different questions and fail
   differently: `wfcheck` reads text and reports lines, `wfdry` executes and reports a mode. Seven
   throwaway harnesses were hand-written across two runs before this was written down.

   Note what it does NOT do: its assertions cannot be signalled by throwing, because your script is
   required to wrap the spawn call and turn a throw into null — it would swallow them and report a dead
   agent instead (measured: three injected defects, three green runs). It reports violations out of band
   and fails the mode on its own record. If you write a second harness, do the same.

3. **Every reviewer gets the workspace copy of the gate-checks reference** (`<workspace>/gate-checks.md`, written by `wf init` — the version that briefed a gate is pinned with it) as required reading — the standing list of
   failure modes from previous runs. The pin AGES, and `wf round new` and `wf freeze` now say so,
   naming the sections it predates: measured on a gate whose pin was sixteen rounds old, the reference
   had grown two whole sections since — the closed severity scale and the three questions a delta round
   may never drop — and every reviewer of the next two rounds was handed it as required reading.
   Nothing was misclassified, because the operator had restated the scale by hand in the prompt, and
   that is the trap: **a reviewer briefed from a stale reference agrees with the rules by coincidence,
   and you cannot tell that from agreement.** Keep the pin — one gate reads ONE version — and
   regenerate it (`wf docs checks > gate-checks.md`) only where the remaining rounds are meant to be
   briefed differently, saying so in the round title. A workspace with NO pin is named too: no pin is
   not a current pin, and silence over it would be the same absent measurement wearing a pass. Without it each new run's reviewers start from zero and re-discover
   `git add -N`. **Every round that finds a novel blocker appends one line to it.**

   **Add no CONCLUSION of your own to a lens prompt.** `wf delta` is what a delta reviewer is handed
   and it is handed WHOLE — the previous round's findings, the closures it claims and the artifact
   diff, in one output with no flag that omits a section. That is deliberate and it is the brief's
   whole cost argument, and it is not what this rule is about: across the rosters on record most
   lenses asking fix-induced or ripple ask CLOSURE in the same lens, so a brief partitioned by vector
   would be unsatisfiable for the shape these gates actually run. What the rule forbids is the
   operator's OWN verdict added on top — "this is fixed", "the acceptance section now states
   observations", "this was verified last round". A prompt that carries the answer is asking the
   reviewer to agree, and it usually does. The same applies to a verification the previous round
   BANKED: it is a claim like any other, so the next round SAMPLES it rather than inheriting it —
   `gate-checks.md` section 0b carries the measurement and the ratio, and this is a pointer to it.

Default = **3 reviewers, each a different vector** (round 1 only — see round discipline below):
1. **Orchestration correctness & safety** — control flow is right: `pipeline()` vs `parallel()`-barrier choice justified; loop termination sound and every `while`-loop guards on `budget.total` (can't run to the 1000-agent cap); dedup-vs-`seen` correct; `schema` present on every structured return; stage args threaded; return shape sane; `.filter(Boolean)` after `parallel()`; no race on global `phase()` inside a fan-out (use `opts.phase`); `isolation:'worktree'` only when agents mutate files in parallel. ALSO enforce the **Workflow cost & context discipline** section: flag read-amplification (N producers each re-reading the whole corpus instead of working from a map + named slices), any large shared blob (>~10-20 KB) inlined into every fan-out agent, and run the total-agents × per-agent-read sanity estimate — a read-amplifying design is a BLOCKER (redesign before launch), not an advisory.
2. **Model- and effort-routing conformance & tier fit** — the script obeys the model-routing section above: every `agent()` carries an explicit `model` AND an explicit `effort`, the model mirrored on its `meta.phases[]` entry for a single-tier phase and named in that entry's `detail` for a phase that deliberately mixes tiers (a mixed phase has one metadata slot and two models — do not report the absent mirror as a violation, and do not accept a phase flattened to one tier just to produce one). `effort` has NO metadata slot at all: a mixed-effort phase names its efforts in the `detail` string, and an absent `meta.phases[].effort` is never a finding — a MISSING per-call `effort` always is, because the omitted value is inherited from whichever session launches the run. Beyond that mechanical check, this reviewer ALWAYS judges tier fit and never rubber-stamps the author's choice — but reasons per *role*, not per call (a fan-out's N identical producers are ONE role, judged once; then spot-check that no individual call deviates from its role's tier). For each role, honestly weigh the assigned model's real capability against the task as written, then apply the routing tier-list:
   - **Under-powered → propose an UPGRADE, treated as a BLOCKER** — but ONLY when the assignment fails the routing section's *full* three-prong opus test: the error must also be uncatchable by the section-review gate or any cheap downstream step (prong 2) and costly to undo (prong 3). A producer whose error the gate catches stays cheap; never upgrade on prong-1 ("might err") alone.
   - **Over-powered → propose a DOWNGRADE, filed as a NOTE** (recorded/surfaced, never loops the gate — "advisory" is the ROLE, not the level, and over-spend is by this section's own words not a defect, which is what NOTE means on the closed scale; file MINOR where the row names a concrete cost) — flag a tier above what the tier-list assigns (opus inside a fan-out is a hard ban; sonnet doing pure grep-and-report → haiku). Over-spend is a soft target, not a defect.
   The downgrade lens targets the *workflow's* runtime agents (which run at scale, up to the 1000-agent cap), NOT this gate's own reviewers (their tier is set separately above). Task scope/shape is vector 3's call; runtime cost/cap safety (budget-guards, agent-count, fan-out size) is vector 1's — this reviewer judges only which *tier* and which *effort* each role should be. Judge the two axes SEPARATELY: an opus role at `low` and a haiku role at `high` are both legitimate, and a script where every tier moves in lockstep with its effort has almost certainly chosen one of them by reflex. The effort tiers are the ones in the routing section (mechanical → `low`, ordinary producer → `medium`, a step nothing downstream re-checks → `high`, a once-only step whose error burns the fan-out → `xhigh`/`max`), and `high`+ on a per-item agent inside a `parallel()`/`pipeline()` is the same defect as opus there. Until a run measures what a lower effort costs in findings, an over-powered effort is advisory like an over-powered model — filed NOTE, or MINOR where the row names a concrete cost, never looping the gate; treat only the absence as a blocker. Direct this reviewer at the `agent()` calls and their `meta.phases[]` pairs.
3. **Goal alignment, prompt adequacy & completeness** — the script actually fulfills the user's request and covers the full scope (no silent top-N / sampling caps without a `log()`); each agent's *prompt* carries the context/inputs it needs and the `schema`'s intent is unambiguous (its mere presence is vector 1's check; a correctly-wired agent still produces garbage if briefed badly); every artifact-producing section ends with the dual section-review gate; the `meta` block is a pure literal whose phase titles match the `phase()` calls.

**Where to aim them, from five phases of evidence.** Across the nine rounds of the last two gates, ZERO
findings concerned orchestration mechanics as such — no `parallel()` that should have been a `pipeline()`,
no unbounded loop, no budget defect. Everything of value was one of three things, and the vectors above
are worth re-cutting along them when a script's risk is concentrated: **FACTS** (every claim the prompts
make about the repo or the corpus, each re-verified by command — a wrong number becomes a wrong
instruction nothing downstream questions), **SEAMS** (every contract spanning two agents: signatures, file
ownership, lookup keys, enumerations, report fields — the defect class that compiles, passes every test
and does nothing), and **CRITERION** (can the phase criterion be reached by the harness the acceptance
agent is given, and observed rather than asserted). Vector 1's cost/read-amplification duty stays, but its
mechanical half now belongs to `wfcheck`.

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

**Round discipline** — the gate itself has failure modes, and these were paid for:
- **Barrier each round.** Collect ALL vectors of a round before applying ANY fix. Patching between two
  reviewers of the same round means the second reports what you already fixed. **The temptation is
  strongest exactly when one vector is fast and its findings look obviously correct** — measured: an
  external lens returned first, its five fixes went in while two internal lenses were still reading,
  and one of them judged a moving target and reported two blockers as open against a file where they
  were already closed. Nothing was lost that round because the other lens happened to read after the
  write, which is luck and not design. Dispatch every vector against ONE frozen snapshot and batch the
  fixes after the last one returns.
- **A delta round asks CLOSURE, FIX-INDUCED and RIPPLE, and may add vectors but never drop those three [R3].**
  Bank each one with `wf round lens <prompt-file> -name N -model M -asks Q` — `-name` stays the label your
  findings use, `-asks` is the closed set (`orchestration, routing, alignment, facts, seams, criterion,
  closure, fix-induced, ripple, other`) that lets one gate's roster be read beside another's, and `other`
  is always available so a vector your slice needs is never refused. Each required question has a
  measured origin, not a taxonomy's symmetry: **CLOSURE** — two hand-written closure lenses returned 6 and 4
  of 24, 5 of 25, and 6 and 4 of 23 CLOSURES they judged not closed, in rounds that themselves filed 25,
  23 and 21 findings — round 4's second lens checked citations, not closures (4 unsupported of 70) — the
  most valuable half of those rounds,
  produced by prose over data git already held (`wf check closure` mechanised the reachable part, not
  this one); **FIX-INDUCED** — a fix is the largest single source of blockers, half the BLOCKERS of rounds 3+
  on one phase, 5 of 11 on the next, and 9 of 9 on the one after; **RIPPLE** — a fix that changes a RULE has
  consumers that are not in the diff, and THREE of four such ripples on one run argued from the retired rule
  while containing none of its keywords, so `grep` returned nothing (`wf check stale` narrows that read
  and says NOT A PASS where it swept nothing). Measured on the two gates that banked any roster at all:
  13 lens RECORDS under TEN distinct names (measured 2026-08-25; one gate is open and the figure moves),
  of which exactly one — `criterion` — appears as a vector name anywhere in this manual, and the other
  four gates banked no roster, so their prompts are gone.
  **Every one of those prompts says that finding nothing is allowed**, in the same words in every
  round of the gate: *an empty findings list is the expected and legitimate result; manufacturing a
  finding to look thorough is one failure mode and downgrading a real one to make the count look good
  is the other.* `wf stats` and `wf round lens -asks` enforce the QUESTIONS by roster name and can see
  nothing of the framing. Read off one script gate's banked roster: no prompt of its first three
  rounds carried the clause (14, 7, 6 blockers), all four prompts of its fourth did (1), and three of
  four of its fifth did (0, with thirteen defects still filed, two MAJOR). Two rounds against three
  at the convergent end of a descending gate is not a measurement of the clause — but a count that
  means one thing in round 3 and another in round 4 is not a descent either, so the clause belongs in
  round ONE, and this is checkable only because the prompts were banked.
- **Later rounds are DELTA rounds.** A fix is unreviewed code — in one phase, *half* the blockers of
  rounds 3+ were introduced by the previous round's own fix; in the next, 5 of 11; and in the one after
  that, round 2 was **9 out of 9**: every blocker that round existed only because round 1's fix created
  it. Plan on that
  rather than on a clean round 2; a round whose findings are all self-inflicted is the expected case,
  not a sign the gate is malfunctioning. But a full re-read is the wrong instrument:
  hand the reviewer the script's `git diff` plus the findings it was meant to close, not 150 KB again.
  A delta round is ~20k tokens against ~170k for a re-read. `wf delta` prints exactly that brief — the
  findings filed by the round BEING CLOSED, the closure checklist derived from their `closed by`
  column (each claimed commit resolved to its subject and touched files; a prose closure marked
  "verify by hand"; an open blocker printed as OPEN), then the artifact diff since that round's tag —
  because a
  reviewer whose first job is to check a closure claim needs the claim in front of the diff, not in
  another file. **`-save` writes the brief to `rounds/r<open>/brief.md` and commits it** — hand-saving
  it produced the untracked stray file verify then had to learn to name; if the brief is wanted as a
  file, it is round bookkeeping and the tool banks it. Its default base is the round before the open
  one, deliberately: by review time the
  current round has itself been frozen, so "the newest tag" would diff against the very state under
  review and print nothing, and an empty diff looks exactly like a round that changed nothing. Name a
  different base with `-from gate-rN` only when you mean to.
- **Run the DECISION gate to exhaustion BEFORE the script exists.** Measured across three phases: the
  decision gate is cheaper per finding than the script gate, and a script written from a document whose
  architecture has already been inverted four times gets a script gate that is about the machine rather
  than about the design. The run that ordered it that way closed its decision gate in eight reviewer
  rounds and its script gate in three (against ten and six on the run that overlapped them), and every
  script-gate finding was ownership, liveness, coordinates or the fixture — which is the division of
  labour the two gates are supposed to have.
- **The decision gate asks whether the design is RIGHT; the script gate asks whether the REPOSITORY
  WILL LET YOU BUILD IT.** They find different things and neither substitutes. On one run, eight
  reviewer rounds over the decision document missed a hand-pinned total in
  `internal/admin/autocheckpoint_test.go` (`if len(mutating) != 27`) that the slice's one new POST
  route would have turned red — the document had correctly recorded that the two labelled sets it
  changed stayed the same size, and the total was pinned separately. The script gate's FIRST round
  found it, because its reviewer read the tree with the implementing run's file-ownership list in hand.
  That is a question only a script can ask, and it is the argument for not treating the script gate as
  a formality after a decision gate that closed at zero.
- **The two gates have different SHAPES, and the script gate's late rounds collapse into one defect
  class.** Measured on one phase, both gated to exhaustion: the DECISION gate produced **94 blockers
  over seven rounds** (31, 18, 13, 14, 12, 6, 0) and inverted eight decisions — the architecture
  arrived in round 1 and the acceptance criterion was still producing blockers in round 6. The SCRIPT
  gate over the same phase produced **15, front-loaded**: 7, 4, then 1, 1, 1, 1, 0. Every blocker from
  round 3 onward was the SAME thing — a rule corrected in one copy and left standing in another: a
  producer's prompt, a reviewer's bullet, a phase's `meta` entry, a scope list, a bar. Once a round's
  findings are all of that class, the next move is a mechanical sweep for every copy of every rule you
  changed, NOT another reviewer round. A round costs ten minutes and six figures of tokens to find what
  a search finds in a second — and one of those rounds was spent re-filing the previous round's own
  blocker, because the fix had thrown before its file was written (gate-checks §I).

  **`wf check dup` is that sweep.** It cuts the artifacts into sentences, normalises away quoting and
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
- **A reviewer of an INSERTED block gets semantic anchors, not line numbers.** Any coordinate you hand
  it is one the producer's own insertion invalidated by the length of what it inserted, so a reviewer
  checking it literally reads the middle of the new block and reports the wrong thing. Name the
  landmark ("the last assertion of the preceding block", "the header that opens the next one") and say
  that the numbers are the pre-insertion ones.
- **Reviewer count descends** 3 → 2 → 1 as findings narrow. The ROUND count does not cap: the last phase
  needed four (11 blockers), and its rounds 2 and 3 found *nothing but* defects introduced by the previous
  round's own fix. Budget for that rather than hoping round 2 is clean. Deltas trend down but **not
  monotonically** — one gate ran 1050 / 374 / 169 lines, the next 583 / 236 / 342 / 167, where the third
  round was 45% LARGER than the second because the second's fix opened a seam the third had to close
  across several consumers. Budget for the round COUNT; do not plan on later rounds being cheap.
  **`wf stats -time` prints what a round actually cost** — the span, and its split into review, fix
  and bookkeeping tail, derived from the workspace's own commit times. **Run it; do not read a figure
  out of this paragraph.** What holds across every gate on record is the SHAPE, and only the shape is
  written here: a round is TENS of minutes, the review segment is the overwhelming majority of it, and
  the bookkeeping tail is a low single-digit percentage — so the round COUNT is the cost function and
  the command sequence is not worth compressing. The round also got LONGER as the gates got bigger,
  which is why a pooled number taken over an older corpus under-prices the gate in front of you. This
  paragraph used to carry the pooled median, the middle half and the per-gate list; every one of them
  went stale, twice, and a review round had to re-derive them — the numbers live in this repository's
  own `CLAUDE.md`, where they are a maintenance record rather than something an agent acts on.
  Read the median rather than the total: a span of several hours is the operator away from the desk,
  and most workspaces carry one. A `-kind run` round's span is the run: it is printed on its own line,
  split under the head `run` rather than `review`, and kept out of the median rather than counted as a
  long round or a break. Nothing else narrows that population — a tool round is in this median too.
  The same line prints the AUTHORING window — init to the first artifact — which no round covers
  because it happens before round 1 opens, and which has run from a couple of minutes to well over an
  hour; its short end is a document drafted before `wf init` and therefore outside every measurement
  banked anywhere. Step -2 is what occupies that window.
- **Drop FACTS first when the count descends.** One gate's round 1 returned: SEAMS 3 blockers,
  CRITERION 3 blockers, FACTS 0 blockers (every citation and count in a 150 KB script was correct). Run FACTS once —
  its one major was an invariant asserted in another reviewer's own prompt that the code did not hold —
  then spend later rounds on SEAMS and CRITERION.
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
- **Vacuity STARTS at BLOCKER, at classification time.** A finding that makes an observation, a guard or
  a tripwire *vacuous* — it passes whether or not the thing it checks exists — is classified BLOCKER,
  and rides as a MAJOR only where the row can NAME the later gate actually assigned a non-vacuity vector
  for it, which is the MAJOR test of the rule below and not a second rule. Classified there, its fix is reviewed
  like any other; exempting a fix from review contradicts the one measured rule this gate has (a fix is
  unreviewed code, and the largest single source of blockers).
  Do NOT reason from the defect's label: downstream gates are perfectly capable of catching vacuity when
  a section reviewer was given a non-vacuity vector ("which line of this diff, if reverted, makes this
  check fail? if none, that is a blocker"). **Classify by actual downstream coverage** — check whether
  some later gate was actually assigned that vector before letting anything ride.
- **A finding's level is decided by what catches it downstream, never by how bad it reads [R2].** The
  scale is BLOCKER / MAJOR / MINOR / NOTE and `wf round record` refuses anything else — measured, the six
  ledgers held 1065 findings under four vocabularies for one scale (p3a through round 5), 172 of them at
  a level no other gate can be read against, and a script `enum` does not save the other side: it checks
  the VALUE and never the LIST, and the scripts those six gates produced carry three lists plus a drifted
  field NAME. BLOCKER is what no later gate catches, and it is where vacuity STARTS —
  a finding that makes an observation, a guard or a tripwire vacuous is classified BLOCKER and rides only
  where the row can name the later gate actually assigned a non-vacuity vector for it, which is the MAJOR
  test and not a second rule; MAJOR is a real defect whose row NAMES the later gate that catches it,
  and a row that cannot name one is a BLOCKER; MINOR is cheaper standing than looped for; NOTE asserts no
  failure. The full statement of each is in `gate-checks.md`, which every gate reviewer already gets.
- **Stop rule, decided in advance.** The gate ends on a REVIEWER round that produces no BLOCKER, and on nothing else [R1]. Majors ride into the
  run — the per-section review gates exist to catch them, and another round costing 170k to fix one stale
  sentence is not a bargain. Read the trend off **`wf stats`**, not off your own awk: it prints each
  round's findings, blockers, closure split and apply batches from the ledger and git, and it knows the
  open round's scaffold placeholder is not a finding — the hand count on the first live gate closed the
  gate on 99/27 against a true 98/26 for exactly that reason. The share of a round's findings that are
  self-inflicted (fix-induced) is the companion number, and it is a CONTINUE signal, never a stop
  signal: it says what THIS round found, not whether the next one will be empty. Measured on two
  gates — a round that was 4 of 4 fix-induced and carried one blocker was followed by a round filing
  seven findings and another blocker, and a 3-of-3 fix-induced round carried a blocker itself. The
  blocker descent is not monotone either — 7, 5, 1, 0, 2, 5, 3, 2, 1, 0 on one gate — so a
  zero is not a trend. Nor is a BUMP one, and this is the corpus NORM rather than an
  exception: **every gate workspace on record bounces at least once**, and gates have closed on the
  rule with a bump inside their last few rounds. What a bump is NOT is reliably "the round after a
  large fix" — one was measured following the SMALLEST fix of its gate to that point, and on another
  the majority of the bumped round's blockers were not marked `[fix-induced]` at all. Read a bump as
  the normal shape of a descent, never as a signal about the artifact, and read the `[fix-induced]`
  split beside it, which is the only thing in the workspace that says where a round's blockers came
  from. The stop rule stays exactly what it says above, and nothing else does. The gate ends on a REVIEWER round that produces no BLOCKER, and on nothing else [R1].
  **And a descent that PRECEDES a first review says nothing about it.** A round whose window brings a
  new artifact into the gate reviews it for the first time, and `wf stats` says so BEFORE that round's
  counts — the suppressed ratio used to sit in a parenthesis after them. Measured on mocker A3: nine
  rounds of blocker descent on the decision document — 13, 17, 10, 16, 11, 12, 6, 7, 4 — read exactly
  like convergence, one more empty round would have closed the gate under this rule, and then the
  orchestration script was added and round 10 produced 38 findings and 12 blockers over an artifact
  nobody had reviewed at all. On a `-kind tool` round the same line says an artifact was added and NO
  reviewer was dispatched: that round reviewed nothing by declaration, and claiming a first review for
  it would be this bullet's own defect committed in the fix for it.
  **And an UNFILLED findings scaffold is not a measured zero.** A round that was opened, frozen and
  never dispatched leaves the ledger `wf round new` wrote, so `wf stats` printed `0 finding(s),
  0 blocker(s)` — the same line the round that ENDS a gate prints, and under this rule a legal close
  (measured: one gate's round 9, frozen as `gate-r9`, reviewed by nobody). `wf stats` names such rounds
  and marks the line `ledger: UNFILLED scaffold, not a measured zero`; `wf freeze` refuses while ANY
  closed round stands in that state and `wf close` refuses to bank one. EVERY closed round is checked,
  not just the previous one: `wf round new` does not require the current round to be frozen, so a round
  opened past without a freeze was examined by no refusal at all. A round whose vectors ran and found
  nothing says so by banking them with `wf round lens`, and `wf stats` then reads it as `ledger: empty,
  and its lenses are banked — a measured zero`. A `-kind tool` round is exempt, because its opening
  subject already records that no reviewer was dispatched; the OPEN round is exempt because its
  scaffold is blank by construction.
  **And the number that decides whether to keep going is the share of a round's BLOCKERS that are
  fix-induced, not the share of its findings.** `wf stats` prints it per round beside the marker
  count (`fix-induced 16 (4 of 5 blocker(s))`), gives the gate total on its own line, and names
  every round whose blockers are a MAJORITY self-inflicted. A descent of 9, 3, 5, 5 whose last round
  is 4-of-5 its own predecessor's fix is a different object from the same digits arrived at
  honestly, and the stop rule reads the digits: the count goes flat exactly when the fix rate starts
  matching the introduction rate, which from outside looks like a gate holding steady against the
  artifact. Measured across the corpus — 100 of 177 blockers on the gate that stopped instead of
  closing (11 of its 22 rounds a majority), 22 of 79 and 12 of 28 on two others, and one gate with no
  markers at all, which prints as unmeasured rather than as zero. Read it beside the SUBJECT question
  in `gate-checks.md` section 0: would a fleet build the wrong thing, or merely be told something
  stale about a file it is about to read anyway. Mark such findings with
  a literal `[fix-induced]` in the finding text — `wf stats` counts the marker per round and in total,
  and prints "not measured" when nobody marked, because an unmarked corpus is a different claim from
  "no self-inflicted findings". It also names, BY ROUND NUMBER, the rounds it could not measure: the
  ones with findings and no `[fix-induced]` marker (the share is then a lower bound over the marked
  rounds, not a measurement of the gate) and the ones with findings and no `wf apply` batch (their
  edits are absent from every ratio it prints). Both holes opened in the same place on one gate — from
  round 18 to the close — and the totals above them kept reading as measured while the true
  fix-induced share was nearer 38% than the 34% printed, with every blocker of five consecutive rounds
  induced by the round before.

- **A run of rounds at exactly ONE blocker is a guard asking the wrong KIND of question.** `wf stats`
  names EVERY such run (three or more) and, where every blocker in it cites one file, says so. The
  stop rule is right to keep the gate open — each of those rounds does close a real escape — but
  measured on one gate, rounds 22 to 24 were one enumeration floor rewritten: a free-text sentence,
  then a non-empty array, then a distinct-member count, then a fixed cardinality, then exact member
  names, then stable ids. Every version but the last two constrained a property of the REPORT, and for
  every "at least N" there is an N, so the next degenerate report satisfied the new shape. **A guard
  over a report asks "is this what the document NAMES", never "does this look like enough".** When the
  signal fires, stop patching the guard and re-ask what it is for.
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

Rules:
- Collect findings, then ACT: a blocker from ANY reviewer means fix the script source and re-run the gate as a delta round (whatever vectors you ran — a fix in one can break another) before the `Workflow` call; advisory findings — MAJOR, MINOR or NOTE on the closed scale — are recorded and surfaced.
- **Proportionality.** The full 3-reviewer gate is required only when the script can actually blow up: a data-driven fan-out over a dynamic N, a `while` / `budget` loop, multiple opus agents (or an opus agent inside a loop/fan-out), or parallel file mutation. For a small script with none of those (e.g. a fixed 2-3 agent `pipeline()`/`parallel()`, all sonnet/haiku, no loops, no file writes), a single reviewer — or the authoring agent's own careful re-read — suffices.
- Skip the gate entirely only for a trivial throwaway script (a single `agent()` call, or a pure read-only locate/extract that mutates nothing) — `log()` or state that it was skipped and why.

