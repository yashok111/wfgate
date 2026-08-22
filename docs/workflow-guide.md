# Workflow orchestration guide

Full operating manual for the `Workflow` multi-agent orchestration tool: model routing, the opus
pre-flight script-review gate, the per-section dual-review gate, and cost/context discipline. **Read
this file in FULL before authoring or running any Workflow.**

Four companions, all used by the pre-flight gate below and none auto-loaded:
`~/.claude/workflow-gate-checks.md` (the standing list of failure modes from previous runs — reviewers
read it, and every novel blocker appends a line), `~/.local/bin/wf` (the gate harness: the workspace
that holds the artifacts, the atomic apply, the round with its validated findings ledger, the freeze,
the verify, the delta with its closure checklist, and four artifact checks — cite, dup, drift, rules — it
owns the BOOKKEEPING of the gate; the script's own static and dynamic checks stay
in the two binaries below),
`~/.local/bin/wfcheck` (a static checker for these
scripts; run it before the gate and after every fix) and `~/.local/bin/wfdry` (a dynamic runner that
executes the script with stubbed hooks across the modes a real run can enter; run it before each freeze).
All of it — `wf`, both scripts, and these two documents — is versioned in the wfgate repository
(`~/projects/wfgate`); the `~/.local/bin` and `~/.claude` paths are symlinks `make install` maintains,
so a fix to a tool or to a spec is a COMMIT there, never a hand edit of an installed copy. Extracted from CLAUDE.md 2026-06-10 to
keep the always-loaded context lean — these rules are only needed when you actually orchestrate, and
the Workflow tool is not invoked on most turns.

**Everything an agent is INSTRUCTED to do is written in English, in Latin script.** Every prompt, every
shared constant interpolated into one, every schema field description, and every companion document the
prompts tell an agent to read. This holds regardless of what language the human-facing documents of the
project are in — those stay in whatever language their readers use; the agent-facing layer does not.
Three reasons it is a rule and not a preference: prompt text becomes code — labels, log lines, report
fields and often comments in the tree the agents write — so a mixed-script prompt leaks a second script
into artifacts that are supposed to be uniform; the text is passed through shell quoting, template
literals, `grep` patterns and diff tooling, all of which have more failure modes with non-ASCII input and
none of which announce them; and an instruction and its restatement in a report have to be comparable by
eye and by search, which they are not across scripts.

**The gate's own artifacts ARE that layer.** The decision document, the context document, the findings
and the script are read and audited by agents, so they are written in English regardless of the
project's human language — a gate run in the project's language is the ambiguity this sentence closes
(the first live run wrote them in Russian, and paid for it in the citation checker's blind spot below).

The one carve-out is QUOTATION. Text quoted from a source document — a specification, a requirement, an
error message, an identifier — is DATA, not instruction, and it is reproduced verbatim in whatever script
it is written in; altering it would stop it being a quote and would break every search that looks for it.
Mark it as a quote and keep the surrounding instruction in English.

**Name the spawn wrapper `spawn`.** A script must wrap the spawn call — an unhandled throw at a serial
step aborts the run (see the gate-checks' failure-handling section) — and `wfcheck` reads every later
check off its inventory of call sites. A wrapper under an unrecognised name collapses that inventory to
nothing, so every check silently passes over an empty set and the tool exits clean. Measured: a 100 KB
script with nineteen spawn sites was reported as **one call site with a largest prompt of 0.0 KB, zero
errors**. A rule enforced by a tool has to name the thing the tool looks for; `wfcheck` now also ERRORs
when its own inventory is implausible against a second count of the same population, but do not rely on
the backstop — use the name.

---

# Workflow model routing

No `CLAUDE_CODE_SUBAGENT_MODEL` env override — removed 2026-05-29 (it silently overrode per-call
`opts.model`, contrary to documented order, so a `model:'opus'` agent quietly ran on sonnet). Do NOT
re-add it; never rely on any env/session default. Set the model EXPLICITLY, per agent.

**HARD RULE — every `agent()` call MUST carry an explicit `model`**, chosen by the workflow-owner
agent per task. A model-less `agent()` is a bug; fix before running.

**Tier by what the subagent does:**
- **haiku** — trivial/mechanical: grep-and-report, rename, format-preserving tweak, log extraction,
  yes/no checks. No reasoning.
- **sonnet** — DEFAULT for ordinary work: localized edits, single-file TDD slices, test writing,
  structured extraction, routine review.
- **opus** — ONLY if ALL THREE hold (else → sonnet): (1) hard enough that sonnet plausibly errs — many
  *interacting* constraints, or intricate logic (subtle concurrency, invariant-heavy refactor, tricky
  algorithm); (2) the error is NOT cheaply caught downstream — if the section-review gate (below) or a
  cheap later step catches it → sonnet; (3) the error is costly to undo (ships a subtle defect, or
  poisons a stage nothing re-checks). Real cases: top-level architecture/design; a fan-in agent
  reconciling *genuinely conflicting* inputs, run ONCE (a mechanical merge/dedupe fails prong 1 →
  sonnet); adversarial verification of money-/data-loss/auth/crypto claims. "Multi-step", "cross-file",
  "synthesis", "ambiguous" are NOT triggers on their own — apply the three.

**HARD RULE — inside a fan-out, producers are cheap.** For agents *within* a `parallel()`/`pipeline()`
over N items (a standalone agent not in a fan-out — e.g. top-level design — is governed by the tier
list above): per-item producers are sonnet (or haiku), never opus. The ONLY opus-eligible role is a
SINGLE fan-in convergence agent that reads all N outputs, runs ONCE, and clears the three-prong gate (a
mechanical merge won't). A per-item *terminal* stage runs N times → NOT that agent → sonnet. N
same-role opus producers in parallel is banned — if you think you need it, the decomposition is wrong;
split out one fan-in opus agent instead. (Gate reviewers are exempt — the section-review gate's two
reviewers AND the pre-flight gate's reviewers, each governed by its own section below.)

**Tie-breaks & round-ups:**
- Unsure between two tiers → pick cheaper. Default sonnet; escalate ONE fan-in agent before upgrading a
  whole fan-out.
- Auto round-up to opus ONLY for genuine money-/data-loss/auth/crypto reasoning. For an irreversible
  external action with no cheap rollback (prod deploy, fund transfer, destructive data op), round up the
  agent that *decides/verifies* it, NOT the one that merely executes — and NOT for ordinary git/PR/
  message actions (recoverable) or "this code ought to be correct" (all code does).
- Soft target: **≤1 opus producer per section** (section unit as in the review-gate section); over that,
  `log()` which opus case applies. Section-gate reviewers and the pre-flight gate are budgeted
  separately (their sections) — a genuinely security-critical section may legitimately carry an opus
  producer *and* an opus reviewer; that is intended, not a leak.

State the model on every `agent()` and mirror it on the matching `meta.phases[]` entry. Resolution
order (no env): per-call `opts.model` > `meta.phases[].model`.

The mirror rule holds only for a **single-tier** phase. A phase that deliberately mixes tiers — a review
phase with one top-tier and one default-tier reviewer is the common case — has one metadata slot and two
models, and mirroring is not expressible: leave `meta.phases[].model` off that entry and name the tiers
in its `detail` string instead. Do not flatten a phase to one tier just to satisfy the metadata.

# Workflow pre-flight script review (opus gate)

**HARD RULE — before you ever call the `Workflow` tool, the authored script MUST pass a pre-flight review.** This is a one-time gate on the *script source itself*, run BEFORE the workflow launches — distinct from the section-review gate below, which runs INSIDE the script after each section produces artifacts. Run it out-of-band with the `Agent` tool: spawn the reviewers (concurrently — multiple `Agent` calls emitted in one message run in parallel; sequential is fine too, order doesn't matter for a read-only gate), hand each the full script + the user's task/goal but point each at its own vector's focus, collect findings, then ACT before launching. Both this gate and the section-review gate are fixed harness procedures, NOT authored workflow logic — they are themselves exempt from review (no infinite regress; the fixes they demand are re-reviewed, the gates are not).

Reviewers are **`model: 'opus'` — all three.** This is a deliberate policy choice (a script this consequential is worth the opus spend), NOT a tier-list derivation: the gate is the last check before launch — a defect here is caught by NO downstream step and a bad script burns the entire fan-out (wasted budget, polluted artifacts) — so it is reviewed at the top tier on purpose. Two routing-rule clarifications: (a) these are standalone out-of-band `Agent` calls, so the fan-out "producers are cheap" rule does not apply — the tier list governs, and opus here is the owner's explicit call under it; (b) they are gate reviewers, explicitly exempted from the no-N-same-role-opus-in-parallel ban in the routing section above, so running all three on opus is intended. They run out-of-band BEFORE the workflow launches, so they do NOT draw on the script's `budget.total`; the Proportionality rule below scales the **count** (never the tier) so the gate never costs more than the work warrants.

**Before you spawn a single reviewer, in this order.** The order matters and is not the order these
paragraphs were written in — it is: open the WORKSPACE (-1) → open the ROUND (-1b) → gate the
DECISIONS (1) → author the script → static check (0) → **dry-run the artifact set (2b)** → freeze the
whole set (2) → hand reviewers the frozen paths (3). The dry-run comes BEFORE the freeze, reversed
2026-08-22: a dry-run defect found after freezing invalidates a freeze that correctly refuses to
re-freeze, which forced a whole round for a defect no reviewer ever saw (measured, P2e r12) — the
dry-run reads the working copy, and the freeze exists to pin what REVIEWERS read, so it comes after
the set executes clean. Each delta round repeats: collect the
round's findings and record them with `wf round record` → fix with `wf apply` → fill the closed-by
column and re-record (`wf round record <file> -round N`) → static check → re-dry-run → open
the next round → re-freeze → `wf verify` → hand the next reviewer `wf delta`.

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
   substance). A marker collision (text containing an exact `====` line) mis-splits and then FAILS
   the plan loudly, because the mis-split old matches nothing.

   Each replacement also declares how many occurrences it expects, and **that `count` is scoped to ONE
   file**: it catches a rule you believed appeared once in an artifact and that appears there twice. It
   does NOT reach a copy living in a second artifact — declaring `"count": 2` for a rule split across
   two files rejects the batch outright, and the obvious repair (drop it to 1) leaves the other copy
   standing, which is the very defect. The correct shape is **one edit per artifact in the same batch**,
   each with its own count; all-or-nothing is then what guarantees that both copies move or neither
   does. A hand edit plus `git commit` gives up both properties, which are the only reasons the command
   exists.

-1b. **Open the round: `wf round new -title "<what this round must find or close>" [-kind full|delta|tool]`.**
   It scaffolds `rounds/rN/findings.md`. This is not optional bookkeeping: `wf freeze` refuses while no
   round is open, so a gate that skips it cannot reach step 2. Each later round opens its own before
   re-freezing — a frozen round is never re-frozen. The KIND is recorded in the scaffold because the
   audit needs what a round did NOT look at: on the first live gate seven of ten rounds were delta
   rounds whose scope lived only in prose titles, and twice a round reviewed less than a reader of its
   findings would assume, with no record of it anywhere. A delta round's findings state what was not
   re-read. `tool` marks a round with NO reviewer — findings from `wfcheck`/`wfdry` or from the launch
   itself: P2e recorded two of those as ordinary rounds, and the stop rule then read a blocker descent
   of 15→13→4→1→1 where the reviewer descent, the one the rule is about, was 15→13→4→1. `wf stats`
   counts tool rounds apart, and a tooling round's blocker does not restart the stop rule's clock.

   **The scaffold is a form, and an empty form briefs nobody.** When round N's reviewers report, write
   what they found up and put it in with `wf round record <file>` BEFORE applying the fixes — the
   command exists so that filling the form is not the one place an operator falls back to a hand
   commit. That file is what
   `wf delta` puts in front of the next reviewer, whose first job is to check the claim that those
   findings were closed. Round N+1 is opened after the fixes, so at review time the open round's file
   is still blank; `wf delta` reads the file of the round it diffs FROM, which is the one being closed.

   **The findings table is a validated ledger, not free text.** `wf round record` parses the scaffold's
   table before writing a byte: a `closed by` cell naming a commit that does not exist in the workspace
   rejects the whole record (a landed lie briefs the next round with fiction; a rejected one announces
   itself), and the report prints the closure split — by commit / by prose / open. An OPEN blocker is
   legal at record time — findings are recorded when FILED, closures land later — but not at freeze
   time: `wf freeze` reads the findings of the round being CLOSED (the previous round — the frozen
   round's own file is always a blank scaffold, so checking only it would pass vacuously forever) as
   well as the open one, and refuses while a BLOCKER row stands without a `closed by`. So after the
   fixes land, fill the column with the apply commit's sha and re-record with `-round N`. A prose
   closure ("deferred, see decision D-7") passes the freeze with a counted "verify by hand" note — a
   deliberate escape, not a verified commit. A hex-looking English word is not read as a sha: a commit
   claim needs at least one digit. A comma-separated list of shas is fine, and case does not matter.

0. **`wfcheck <script>`** (`~/.local/bin/wfcheck`) — a static checker for these scripts. It must report
   **zero ERRORs** before the gate runs. It catches, for free, what four phases of opus gating kept
   spending rounds on: a report field named in a prompt that the agent's schema does not declare; a
   SIG/contract block interpolated into the writer but not its callers; one file claimed by two agents;
   `git add -N X && git diff` (which hides the diff entirely); a lister used as a gate that exits 0
   whether or not it printed (`gofmt -l X &&` and its equivalents in other formatters); a
   `<placeholder>` inside a fenced command; `phase()` titles that do not match `meta.phases`; a missing
   `model:`/`schema:`; an unused const; per-agent prompt sizes and per-blob multiplicity; a required
   schema field no consumer's prompt explains; a test name the tree does not declare. Reviewer rounds are
   worth ~10 minutes and six figures of tokens each — do not spend one on a regex. Re-run it after EVERY
   round's fixes: it has caught an unescaped apostrophe inside a quoted string that would have failed a
   run at launch.

   **Run a JS linter beside it, for the coordinate.** `wfcheck` extracts the script body into a temp
   wrapper before parsing it, so a syntax error is reported at that wrapper's line number, offset from
   the file you are editing — measured on a 1700-line script: `body.mjs:939` for what was really line
   959. A plain `oxlint --deny-warnings <script>` (or any parser: `node --check` on the body) takes
   about a tenth of a second and prints a true `file:line:col`. It catches exactly one class, and it is
   the class that actually recurs: a backtick or a `${` written into prompt PROSE, where the
   surrounding template literal treats it as syntax. It cannot see the other two failure modes of
   editing these scripts — a replacement pattern that no longer matches because the same rule also
   lives in a markdown companion with different escaping, and a patch script that throws before it
   writes — so run it as a coordinate finder, not as a second opinion.
1. **Gate the DECISIONS before the prompts.** The run-level `log()` block — slice boundaries, what is
   deferred and why, the architectural calls — is a few KB and is where the expensive mistakes live.
   Measured on one phase gated this way: 8 KB of decision list, two independent reviewers, **six
   blockers and a scope cut** — before a single prompt existed. The scope cut was the more valuable half: both reviewers
   independently showed that a third of the slice was P2 work the phase criterion did not need. Use TWO
   lenses here, not one, and make one external — they overlapped on two blockers and each found one the
   other missed, for a few minutes of wall clock. Run the external one in this exact form, because the
   two things that make it work are otherwise a file away and get paid for again:

   ```bash
   timeout 900 vcodex exec -s read-only -m <model> -c model_reasoning_effort="medium" \
     "<prompt naming absolute paths>" < /dev/null > /abs/path/round-N.txt 2>&1
   ```

   Redirect stdin or it blocks forever on "reading additional input"; write to a file, never pipe to
   `tail`, or progress is hidden and a hang looks like work; keep it read-only — a second opinion must
   not mutate the tree.

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
   stopped anchoring is writing citations the gate cannot really verify. Hand reviewers every path in the manifest, and diff
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
   exactly like a manifest over four), one that vanished, anything uncommitted, a missing tag, or a
   target repository that has moved — every line number the artifacts cite is a claim about one
   revision of the repository under review. A HEAD past the round's tag is printed as a NOTE, not a
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
   gone on purpose: the citation names the old path). This is the only mechanical signal the
   unanchored majority get — their range check stays green forever, because both lines keep existing —
   while anchored citations also self-report through `wf check cite` once they shift. Drift is
   measured from the newest frozen round (name another with `-round N`) and is not part of
   `wf check` without arguments, which must keep working before anything is frozen.

   A companion document must **declare its anchors explicitly and uniquely**, so a static check can
   confirm that every anchor a prompt names still exists. That is the whole of what holds statically: a
   reference that still resolves but no longer means what the prompt assumes is a reviewer's finding, not
   a checker's.
2b. **Dry-run the artifact set with stubbed hooks, BEFORE the freeze: `wfdry <script>`**
   (`~/.local/bin/wfdry`). It executes
   the script body with the `agent`, `parallel`, `pipeline`, `phase`, `log`, `args` and `budget` hooks
   replaced by stubs that derive each report from the `schema` the script hands that call — the schema
   IS the fixture contract, so nothing is hand-authored per run. Your own `spawn` wrapper is NOT
   stubbed: it is left in place and exercised, which is what makes the note below about a swallowed
   throw true. It must complete every mode before the gate runs. The six modes are `clean`, `blockers`
   (every gate returns one), `acceptfail`, `unfixed` (a fix round that leaves blockers open),
   `throw-serial` and `dead-reviews`. Killing a named agent is not a mode: `--dead <label>[,<label>…]`
   kills those labels in every one of the six. Print the phase
   order, the label/model list and the whole return value each time. Seconds of cost; it evaluates every
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

3. **Every reviewer gets `~/.claude/workflow-gate-checks.md`** as required reading — the standing list of
   failure modes from previous runs. Without it each new run's reviewers start from zero and re-discover
   `git add -N`. **Every round that finds a novel blocker appends one line to it.**

Default = **3 reviewers, each a different vector** (round 1 only — see round discipline below):
1. **Orchestration correctness & safety** — control flow is right: `pipeline()` vs `parallel()`-barrier choice justified; loop termination sound and every `while`-loop guards on `budget.total` (can't run to the 1000-agent cap); dedup-vs-`seen` correct; `schema` present on every structured return; stage args threaded; return shape sane; `.filter(Boolean)` after `parallel()`; no race on global `phase()` inside a fan-out (use `opts.phase`); `isolation:'worktree'` only when agents mutate files in parallel. ALSO enforce the **Workflow cost & context discipline** section: flag read-amplification (N producers each re-reading the whole corpus instead of working from a map + named slices), any large shared blob (>~10-20 KB) inlined into every fan-out agent, and run the total-agents × per-agent-read sanity estimate — a read-amplifying design is a BLOCKER (redesign before launch), not an advisory.
2. **Model-routing conformance & tier fit** — the script obeys the model-routing section above: every `agent()` carries an explicit `model`, mirrored on its `meta.phases[]` entry for a single-tier phase and named in that entry's `detail` for a phase that deliberately mixes tiers (a mixed phase has one metadata slot and two models — do not report the absent mirror as a violation, and do not accept a phase flattened to one tier just to produce one). Beyond that mechanical check, this reviewer ALWAYS judges tier fit and never rubber-stamps the author's choice — but reasons per *role*, not per call (a fan-out's N identical producers are ONE role, judged once; then spot-check that no individual call deviates from its role's tier). For each role, honestly weigh the assigned model's real capability against the task as written, then apply the routing tier-list:
   - **Under-powered → propose an UPGRADE, treated as a BLOCKER** — but ONLY when the assignment fails the routing section's *full* three-prong opus test: the error must also be uncatchable by the section-review gate or any cheap downstream step (prong 2) and costly to undo (prong 3). A producer whose error the gate catches stays cheap; never upgrade on prong-1 ("might err") alone.
   - **Over-powered → propose a DOWNGRADE, ADVISORY only** (recorded/surfaced, never loops the gate) — flag a tier above what the tier-list assigns (opus inside a fan-out is a hard ban; sonnet doing pure grep-and-report → haiku). Over-spend is a soft target, not a defect.
   The downgrade lens targets the *workflow's* runtime agents (which run at scale, up to the 1000-agent cap), NOT this gate's own opus reviewers (their top-tier spend is justified separately above). Task scope/shape is vector 3's call; runtime cost/cap safety (budget-guards, agent-count, fan-out size) is vector 1's — this reviewer judges only which *tier* each role should be. Direct this reviewer at the `agent()` calls and their `meta.phases[]` pairs.
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
  reviewers of the same round means the second reports what you already fixed.
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
  differed only by a count, a class no similarity threshold separates from agreement.

  **What no similarity metric reaches, `[RN]` rule markers make mechanical.** Copies that CONTRADICT
  in different words share no wording — eight of those on the first live gate, every one found by a
  reviewer and none by dup. Mark the sites of one rule with the same `[R12]` and `wf check rules`
  (part of `wf check` with no argument) demands they normalise identically, quoting and case erased
  as in dup. The population line prints the marker count, because zero markers is a corpus that opted
  out, not one that passed; the unmarked contradictions stay a reviewer's find.
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
- **Drop FACTS first when the count descends.** One gate's round 1 returned: SEAMS 3 blockers,
  CRITERION 3 blockers, FACTS 0 blockers (every citation and count in a 150 KB script was correct). Run FACTS once —
  its one major was an invariant asserted in another reviewer's own prompt that the code did not hold —
  then spend later rounds on SEAMS and CRITERION.
- **The fix protocol lives in `workflow-gate-checks.md` section I.** Short version: record what the
  round found with `wf round record <file>` BEFORE touching a fix — that file is what briefs the next
  reviewer; apply the round's fixes as ONE `wf apply` batch, one edit per artifact per rule, each
  declaring its own occurrence count; fill the `closed by` column with the apply commit and re-record
  (`-round N`) — the next freeze refuses while a closed round's blocker stands open, and a record
  naming a commit that does not exist is rejected before it writes; read the block around each edit
  after making it; sweep with
  `wf check dup` for copies that DRIFTED, then hand-check for copies that CONTRADICT, which no
  similarity metric reaches; re-run `wfcheck`, the JS linter and `wfdry`; open the next round,
  `wf freeze`, and `wf verify` before launching.
- **Vacuity is a BLOCKER, at classification time.** A finding that makes an observation, a guard or a
  tripwire *vacuous* — it passes whether or not the thing it checks exists — is a blocker, not a major.
  Classified there, it loops the gate under the rule below and its fix is reviewed like any other; no
  exception is needed and none should be made, because exempting a fix from review contradicts the one
  measured rule this gate has (a fix is unreviewed code, and the largest single source of blockers).
  Do NOT reason from the defect's label: downstream gates are perfectly capable of catching vacuity when
  a section reviewer was given a non-vacuity vector ("which line of this diff, if reverted, makes this
  check fail? if none, that is a blocker"). **Classify by actual downstream coverage** — check whether
  some later gate was actually assigned that vector before letting anything ride.
- **Stop rule, decided in advance:** a round that produces no BLOCKER ends the gate. Majors ride into the
  run — the per-section review gates exist to catch them, and another round costing 170k to fix one stale
  sentence is not a bargain. Read the trend off **`wf stats`**, not off your own awk: it prints each
  round's findings, blockers, closure split and apply batches from the ledger and git, and it knows the
  open round's scaffold placeholder is not a finding — the hand count on the first live gate closed the
  gate on 99/27 against a true 98/26 for exactly that reason. The share of a round's findings that are
  self-inflicted (fix-induced) is the companion number: it climbs toward 100% precisely when the gate is
  exhausted (measured: the last two rounds of the first live gate were 100%). Mark such findings with
  a literal `[fix-induced]` in the finding text — `wf stats` counts the marker per round and in total,
  and prints "not measured" when nobody marked, because an unmarked corpus is a different claim from
  "no self-inflicted findings".
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
- Collect findings, then ACT: a blocker from ANY reviewer means fix the script source and re-run the gate as a delta round (whatever vectors you ran — a fix in one can break another) before the `Workflow` call; advisory findings are recorded and surfaced.
- **Proportionality.** The full 3-reviewer gate is required only when the script can actually blow up: a data-driven fan-out over a dynamic N, a `while` / `budget` loop, multiple opus agents (or an opus agent inside a loop/fan-out), or parallel file mutation. For a small script with none of those (e.g. a fixed 2-3 agent `pipeline()`/`parallel()`, all sonnet/haiku, no loops, no file writes), a single opus reviewer — or the authoring agent's own careful re-read — suffices.
- Skip the gate entirely only for a trivial throwaway script (a single `agent()` call, or a pure read-only locate/extract that mutates nothing) — `log()` or state that it was skipped and why.

# Workflow section-review gate (dual review)

Every Workflow **section of work** ends with a review gate **before that section is considered done** and the next one starts. This is per-SECTION, **NOT per-agent** — never gate individual `agent()` calls; gate the section boundary.

A section is a **declared gate boundary**, not necessarily one `phase()`. Several producing phases may share one gate — that is often right, because the phases that build one seam are reviewable only together — but the grouping must be a STATED choice: the script says which phases each gate covers, in the gate agent's own prompt and in `meta`. Left unstated, a run with three producing phases and one gate is indistinguishable from a run that forgot two gates, and neither the author nor a reviewer can tell which it is.

The gate = **2 reviewer subagents in parallel** (a `parallel()` barrier), each auditing that section's changes along a **different vector that is actually valid for the task**. Pick the two most relevant from:
- code change → correctness/bugs · security · simplification/reuse/efficiency  (pick 2)
- research/report → factual accuracy (verify claims) · internal consistency · completeness/gaps  (pick 2)
- design/plan → feasibility vs real code · risk/blockers · alignment with the goal  (pick 2)

Rules:
- **Reviewers are `model: 'sonnet'` by DEFAULT.** Opus on a reviewer is reserved for a genuine extreme case — a security-/correctness-critical section where a subtle, high-cost-if-missed defect is plausible (e.g. crypto, auth, a kill-switch, money/data-loss paths). Opus review is the rare exception, not the norm; if you can't name the specific catastrophic miss it guards against, use sonnet.
- Feed each reviewer the section's diff or output + the section's goal.
- Collect findings, then ACT: a blocker from either vector loops that section (fix → re-review) before proceeding; advisory findings are recorded and surfaced.
- **The loop is BOUNDED, and what survives it is carried in the RETURN value.** An unbounded "loop until no blockers" does not terminate against a fix agent that keeps failing, and it has no budget guard — which is why every real script instead runs one bounded fix round and moves on. That divergence between the rule and the practice is the defect; the rule is what changes. Write the form that works: a fixed small number of fix→re-review rounds, guarded on `budget.total`, and every blocker still open when the rounds run out pushed into an explicit open-blockers list in the return value — never only into a `log()` line. A run that ends green while carrying open blockers must say so in its verdict, not in its scrollback.
- **A run ends on a read-only step, or on a gate over its last mutation.** The standard hole: an acceptance agent that may fix what it found, or a closing fix agent, makes the final edit of the entire run — and no gate follows it, so the return value reports green over code nothing reviewed. Either forbid those steps from writing (they report, a fix agent acts, and that fix is gated), or add a gate after every step permitted to mutate. Green over unreviewed final code looks exactly like green.
- Skip the gate only for trivial/throwaway sections (pure read-only locate/extract that mutate nothing) — `log()` that it was skipped and why.

# Workflow cost & context discipline

Lesson banked 2026-06-10: a full-repo audit workflow burned ~half a 5-hour usage window. The
post-mortem showed the cost was NOT the sonnet fan-out everyone assumes — it was **read-amplification +
orchestrator context**. Measured split: 58 sonnet agents ≈ the spend of the single frontier-model main
loop, because both re-billed large contexts every turn (≈230 M cache-read tokens total). These are HARD
rules, enforced by the pre-flight gate (vector 1) and the cost-estimate rule below. "It's just sonnets"
is a fallacy — the orchestrator and re-reads dominate.

**1. Read-amplification — the #1 trap.** Do NOT have N fan-out agents each independently read the whole
corpus (repo/dataset). The map/Explore phase reads the source ONCE; downstream agents work from the
maps + a NAMED slice of files (e.g. a map's riskAreas), never "scope: the entire repo" in every
producer's prompt. If every fan-out prompt re-reads the full tree, that is the bug — N readers × a
28k-LOC repo is N× the same tokens. Prefer the `Explore` agentType for breadth (reads excerpts, not
whole files).

**2. No large shared blob per fan-out agent.** A digest interpolated into all N agents is paid N times.
If a shared context blob exceeds ~10-20 KB, write it to a file and have each agent read only the slice
it needs — do not inline it into every prompt. (96 KB maps digest × 6 finders = 6× waste, measured.)

**3. Per-agent turn budget.** An agent doing dozens of small sequential reads re-bills its GROWING
context as cache-read every turn — this compounds (≈48 turns/agent produced ~200 M cache-read tokens
across 58 agents). Prefer fewer, larger reads (batch a directory in one pass). If an agent is expected
to exceed ~15-20 tool calls, its scope is too wide — split or narrow it.

**4. Stop/resume is lossy.** On resume, only FULLY-COMPLETED `agent()` calls cache; in-flight agents are
discarded and re-run from scratch. Do NOT repeatedly stop/resume a large workflow (each stop wastes up
to the concurrency-cap's worth of partial agents). If a pause is likely (usage limit, time pressure),
split the work into smaller sequential `Workflow` calls at phase boundaries so each completes and
caches, rather than one long run you keep interrupting.

Losing the SESSION (a dead terminal multiplexer, a killed process) kills the run the same way, and the
recovery has three parts, in order:
- **Read `journal.jsonl` in the transcript dir before assuming anything.** An agent with a `started` and
  no `result` is re-run on resume even when its work is finished, correct and green ON DISK — the cache
  keys off the recorded result, not the tree. That is not waste to route around: later phases are written
  against those agents' reported contracts, so they have to run again anyway.
- **Check the tree builds first.** A killed agent can leave a half-written file, and the resumed agents
  will read it as their own prior work.
- **Do NOT commit the partial work "to be safe".** Every gate prompt in a well-built script asserts "HEAD
  is the previous phase, so this diff is exactly what this run produced". Moving HEAD mid-run makes the
  completed phases invisible to every reviewer that follows.

The concurrency cap is `min(16, cpus - 2)`, so on a 4-core box a "parallel" phase of three runs two and
queues one — worth knowing both for wall-clock estimates and because it bounds what a kill can lose.

**5. Fan-out does NOT save a shared budget.** Under a constrained window (the 5-hour usage limit),
parallel fan-out spends the SAME quota FASTER, not cheaper — it buys wall-clock, not tokens. For
read-heavy audits/reviews, a single high-context pass (one agent, or the main loop on a big-context
model) is often CHEAPER than a wide fan-out that re-reads the same files N times. Reserve wide fan-out
for genuinely independent WRITE work, or when wall-clock matters more than quota AND the user has
opted into the spend.

**6. Orchestrator-context hygiene — the hidden half.** The main loop is the most expensive context to
grow: it is a frontier model re-billed in full EVERY turn. NEVER read large raw dumps (>~30 KB: tool
outputs, JSON harvests, whole-file reads) into the main-loop context — filter with `jq`/`rg`/`head` to
the few KB you actually need first. Persist big intermediate data to files; pull slices on demand. (In
the post-mortem the orchestrator's own cost ≈ the entire sonnet fan-out, because big JSON dumps were
read into its context and re-billed every turn.)

**7. Main-loop model tier for long mechanical sessions.** Heavy iterative main-loop work (harvesting,
bulk edits, search, status updates) is billed at the main-loop model's tier on EVERY turn. A top-tier
main loop (Opus/Fable) on a long edit-heavy session is a silent ×5 multiplier vs sonnet — if the
remaining work is mechanical, surface this and suggest dropping the main-loop model.

**8. Pre-launch cost estimate.** Before calling `Workflow`, estimate total-agents × per-agent-read-size.
If each agent re-reads a large shared corpus, the design is read-amplifying — redesign (map-once,
scope-slices, `Explore`) BEFORE launching, not after. This is a pre-flight gate (vector 1) blocker.
