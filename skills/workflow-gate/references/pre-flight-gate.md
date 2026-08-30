# Workflow pre-flight script review (the launch gate)

**HARD RULE — before you ever call the `Workflow` tool, the authored script MUST pass a pre-flight review.** This is a one-time gate on the *script source itself*, run BEFORE the workflow launches — distinct from the section-review gate (`section-gate.md`), which runs INSIDE the script after each section produces artifacts. Run it out-of-band with the `Agent` tool: spawn the reviewers (concurrently — multiple `Agent` calls emitted in one message run in parallel; sequential is fine too, order doesn't matter for a read-only gate), hand each the full script + the user's task/goal but point each at its own vector's focus, collect findings, then ACT before launching. Both this gate and the section-review gate are fixed harness procedures, NOT authored workflow logic — they are themselves exempt from review (no infinite regress; the fixes they demand are re-reviewed, the gates are not).

Reviewers are **`model: 'sonnet'` — every lens of this gate AND of the decision gate in step 1.** A
reviewer's tier is paid once per ROUND, not once per gate, so the tier MULTIPLIES by the round count —
the cost function this manual already names — and that product, not the launch, is what an operator
runs out of. A gate that stops because the quota stopped is the worse outcome: the round that is never
dispatched finds nothing at all, and `wf stats` cannot tell it from a round that found nothing [M-11].
Depth is the axis to keep, and it is the cheaper one: for these out-of-band `Agent` calls a reviewer's
depth comes from the reviewing SESSION rather than from a per-call `effort` (`model-routing.md`), so
do not drop the session's effort while a round is running. **Opus on a gate reviewer is a per-round
exception the operator argues for out loud**, in the round's own lens roster
(`wf round lens <prompt-file> -name N -model opus -asks Q`): a security-/correctness-critical artifact
where a subtle, high-cost-if-missed defect is plausible, or a single fan-in lens reconciling genuinely
conflicting reports that nothing downstream re-checks. If you cannot name the specific miss it guards
against, it is sonnet. Two routing-rule clarifications: (a) these are standalone out-of-band `Agent`
calls, so the fan-out "producers are cheap" rule does not apply — the tier list in `model-routing.md`
governs; (b) they are gate reviewers, exempt from the no-N-same-role-in-parallel ban, so running every
lens of a round on one tier is intended. They run BEFORE the workflow launches, so they do NOT draw on
the script's `budget.total`; the Proportionality rule below scales the COUNT, and this paragraph
scales the tier. The external lens (step 1, `vcodex`) is untouched — it bills against a different
quota, which is a second reason to alternate internal and external lenses between rounds.

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

## 0. Static check: `wfcheck`

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

## 1. Gate the DECISIONS before the prompts

1. **Gate the DECISIONS before the prompts.** The run-level `log()` block — slice boundaries, what is
   deferred and why, the architectural calls — is a few KB and is where the expensive mistakes live.
   Measured on one phase gated this way: 8 KB of decision list, two independent reviewers, **six
   blockers and a scope cut** — before a single prompt existed. The scope cut was the more valuable half: both reviewers
   independently showed that a third of the slice was P2 work the phase criterion did not need. Use TWO
   lenses here, not one, and make one external — the INTERNAL one on sonnet, per the tier rule at the
   head of this file; the external one is pinned by the recipe below and bills against a different
   quota — they overlapped on two blockers and each found one the
   other missed, for a few minutes of wall clock. Run the external one in this exact form, because the
   three things that make it work are otherwise a file away and get paid for again:

   ```bash
   timeout 900 vcodex exec -s read-only -m gpt-5.6-luna -c model_reasoning_effort="high" \
     "<prompt naming absolute paths>" < /dev/null > /abs/path/round-N.txt 2>&1
   ```

   **The model and the effort are PINNED per run, on the command line, never left to
   `~/.codex/config.toml`.** `gpt-5.6-luna` at effort `high` is the external lens of a gate round, and
   both halves are spelled out in the invocation for the same reason `freeze.json` stamps the `wf`
   binary's version: a config edit moves the reviewer setup silently, and an unpinned run does not say
   in its own command line what it ran as — a banked roster that cannot be read back to a setup prices
   nothing. Effort is `high` rather than the config default because this is a gate reviewer, not a
   producer: it is the one read nothing downstream re-checks, which is the routing section's own
   trigger for raising effort, and the external lens bills outside the session's quota, so depth here
   does not multiply against the round count the way the internal tier does. Bank it verbatim —
   `wf round lens <prompt-file> -name N -model "external gpt-5.6-luna" -asks Q` — so a roster read a
   gate later says which lens ran and at what depth.

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

   **This gate has ROUNDS, and they run the same discipline as the script gate (`round-discipline.md`)** — barrier per
   round, delta rounds (hand the reviewer the document's diff plus the findings it was meant to close,
   never the whole artifact again), a descending lens count, and the same stop rule. Alternate the
   internal and external lens between rounds: a reviewer that has already passed the document twice
   reviews it by inertia. Measured on the last phase gated this way: **six rounds, 26 blockers,
   7+7 → 5 → 4 → 1 → 2 → 0.** Budget for the round COUNT, not the round: the architectural inversion
   arrived in round 1 and the acceptance criterion was still producing blockers in round 5. Three of
   those rounds found defects introduced by the previous round's own fix, which is the same rate the
   script gate shows — that rule (`round-discipline.md`) is not a property of scripts, it is a property of fixes.

## 2b. Dry-run the artifact set with stubbed hooks, BEFORE the freeze: `wfdry`

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
   verdicts only — the runner's own WARNs print anyway, under `--quiet` and again after the mode
   tally, because a WARN is a verdict about a branch no mode exercised. **`--prompt-diff <a>,<b>` prints the sentences one agent's prompt carries and the
   other's does not** — see the sweep in `gate-harness.md`. It refuses two spellings of ONE
   prompt (a text does not differ from itself), refuses a label spawned with several DIFFERENT prompts
   (a per-item reviewer has a per-item briefing, so there is no single prompt to diff), and prints
   `NOT A PASS` rather than "the same sentences" where a prompt contributed no sentence at all. A mode that completes having spawned NO agent FAILS: every stub check is
   then a guard over the empty set, and that is indistinguishable from a clean run — the one exception
   is `no-args` on a script that gates its body on `args`, which is correct behaviour and is noted
   instead. **ONE is not zero, so the counts are also read as a RATIO**: every mode's line carries the
   `clean` mode's agent count beside its own, and a mode reaching fewer than half of it FAILS,
   naming both — a mode that THREW or reported violations prints its own detail instead of that
   comparison, so the absence of a `(clean: N agents)` on a red line is the shape of a red line and
   not a comparison the runner failed to make. Measured: a red-bar abort added to the first producer took five of seven modes from
   15/25/25/25/1/15/25 agents to 18/1/1/1/1/1/1 — six modes then at one agent, `throw-serial` included,
   which is one by construction — with the fix round, the recheck, the acceptance step and the
   final verdict exercised by nothing, and every mode still printing OK, so a reviewer banking "wfdry 7
   of 7" WOULD have credited a layer covering a single agent. Failing rather than warning was measured
   too: across the three correct gated scripts on disk the failure modes run WIDER than the happy path
   (36 against 44, 18 against 30, 15 against 25), so none of them trips it. `throw-serial` is exempt
   because it aborts AT the first spawn by construction, and so is `no-args`, which the runner narrows
   on purpose. **The PHASE set is the additive half of the same question**: a mode that reaches every
   phase but a fraction of the agents, and a mode that keeps its agent count while losing a whole
   section, miss opposite shapes, so the runner compares the phase TITLES too and names the ones a
   mode never reached. `dead-reviews` is the one mode where that shortfall is a WARN and not a
   failure — the runner injected those deaths, so a script that HALTS on them is doing what the
   sibling check demands — and the warning still has to be read: measured, an abort guard written for
   a dead PRODUCER ended the run on any death at all, the mode reached 11 of 14 phases with a gate,
   the acceptance step and the last gate exercised by nothing, and the agent count cleared the ratio
   so every mode printed OK. Which of the two it is, is yours to answer; the runner's job is to stop
   printing OK over the question. A run with no `clean` mode SAYS its baseline is a stand-in, and a single `--mode` run
   says nothing was compared at all — the case where the check used to disappear in silence. **A `clean` run that returns a not-green verdict FAILS the mode**, and so does one that
   returns a failure flag set: no fixture in that mode can produce either, so the script is deriving the
   verdict from something it should not. Measured — the shape it caught: a repair agent spawned only
   when acceptance FAILS, counted in the liveness ledger, so the one run everyone wants logged
   `dead: repair` and returned `green: false`. Seven modes were green over it, because acceptance failed
   in all seven and repair was therefore always alive; an opus reviewer found it in the end by copying
   the script's tail into a file and executing it with green values by hand.
   **A red mode is a fact about the SCRIPT or about the FIXTURE, and the two read alike — so read the
   fixture BEFORE you edit the script.** `wfdry --fixture <label>` prints the object the stub handed
   that agent, per mode, and the clean mode's own verdict violation points at it. Three of one gate's
   dry-run reds were fixture-synthesis limits reported as artifact defects, and one of them ended
   with a correct narrowing reverted [M-10]. What the synthesis CAN and cannot derive — the field-name
   vocabularies that decide a boolean's and an array's polarity, `x-pass` when the name will not do,
   and the `$defs` placement a `$ref` needs — is `authoring.md`; a WARN naming a field the contract
   does not recognise means "no mode exercised the branch guarded by this field", and a green tally
   standing over one is the false green this whole runner exists against. Print the phase order, the label/model/effort list and the whole return value each time. Seconds of cost; it evaluates every
   template literal on every path and walks branches a parse cannot reach. **The clean path is the one
   that matters most** — reviewers read the failure branches, because that is where the risk looks like
   it lives, so the defects that survive a gate are the ones on the happy path. It is a DISTINCT dynamic
   runner rather than a flag on the static checker because the two answer different questions and fail
   differently: `wfcheck` reads text and reports lines, `wfdry` executes and reports a mode. Seven
   throwaway harnesses were hand-written across two runs before this was written down.

   **It also prints every schema's serialized WIDTH, with the corpus band in the header, and being
   inside that band is not the same as being safe.** The band's ceiling is the widest width the corpus
   has seen ACCEPTED by the launch's safety classifier and its floor the lowest seen REFUSED; the gap
   between them is UNMEASURED. What to do about a schema in or above it — probe it with one throwaway
   agent rather than reason about the number, and how to narrow one — is `authoring.md`, and that rule
   was paid for twice by gates that measured their own width, wrote the risk down and launched anyway.

   Note what it does NOT do: its assertions cannot be signalled by throwing, because your script is
   required to wrap the spawn call and turn a throw into null — it would swallow them and report a dead
   agent instead (measured: three injected defects, three green runs). It reports violations out of band
   and fails the mode on its own record. If you write a second harness, do the same.

## 3. Brief every reviewer with the workspace's gate-checks reference

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

## The three vectors, and where to aim them

Default = **3 reviewers, each a different vector** (round 1 only — see `round-discipline.md`):
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

## Rules

- Collect findings, then ACT: a blocker from ANY reviewer means fix the script source and re-run the gate as a delta round (whatever vectors you ran — a fix in one can break another) before the `Workflow` call; advisory findings — MAJOR, MINOR or NOTE on the closed scale — are recorded and surfaced.
- **Proportionality.** The full 3-reviewer gate is required only when the script can actually blow up: a data-driven fan-out over a dynamic N, a `while` / `budget` loop, multiple opus agents (or an opus agent inside a loop/fan-out), or parallel file mutation. For a small script with none of those (e.g. a fixed 2-3 agent `pipeline()`/`parallel()`, all sonnet/haiku, no loops, no file writes), a single reviewer — or the authoring agent's own careful re-read — suffices.
- Skip the gate entirely only for a trivial throwaway script (a single `agent()` call, or a pure read-only locate/extract that mutates nothing) — `log()` or state that it was skipped and why.
