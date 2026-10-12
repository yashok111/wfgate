# Workflow orchestration guide

Full operating manual for the `Workflow` multi-agent orchestration tool: model routing, the
pre-flight script-review gate, the per-section review gate, and cost/context discipline. It is
delivered as the `workflow-gate` skill; **before authoring or running any Workflow, read every
reference the SKILL.md maps to your activity — in full.** `wf docs guide` prints the manual's
references in reading order; the reviewer checklist is `wf docs checks` and the provenance behind
the rules is `wf docs measured`, both deliberately outside the guide.

Four companions, all used by the pre-flight gate below and none auto-loaded:
the gate-checks reference (`wf docs checks`; `wf init` writes a pinned copy into every workspace — the standing list of failure modes from previous runs — reviewers
read it, and every novel blocker appends a line), `~/.local/bin/wf` (the gate harness: the workspace
that holds the artifacts, the atomic apply, the round with its validated findings ledger, the freeze,
the verify, the delta with its closure checklist, and the deterministic artifact checks — `wf -h` names
them on its `wf check` line (`wf check -h` lists only the flags), and `wf check` with no argument runs the
set it can run — it
owns the BOOKKEEPING of the gate; the script's own static and dynamic checks stay
in the two binaries below),
`~/.local/bin/wfcheck` (a static checker for these
scripts, reading the parsed program; run it before the gate and after every fix) and
`~/.local/bin/wfdry` (a dynamic runner that executes the script with stubbed hooks across the modes a
real run can enter; run it before each freeze).
**This manual is not the script API.** The signatures and semantics of `agent()`, `parallel()`,
`pipeline()`, `phase()` and resume are the built-in `workflow-authoring` skill. Until Claude Code 2.1.248
they rode in the Workflow tool's own description, so any agent that could see the tool had them; since
then the description is a stub and the reference is a skill that loads only when asked for. Load it
beside this one before the first line: an API recalled from memory costs the same gate round a rule
recalled from memory does.
All of it — the three binaries and these references — is versioned in the wfgate repository
(`~/projects/wfgate`). Two delivery channels: `make install` copies the three version-stamped binaries
into `~/.local/bin`, and the wfgate PLUGIN carries this skill and the PreToolUse hook. Either way a fix
to a tool or to a spec is a COMMIT there, never a hand edit of an installed copy. Extracted from CLAUDE.md 2026-06-10 to
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

**Start from `wf new-script <name>`.** In a review workspace it emits the REVIEW skeleton instead —
finders, verifiers, a merge and a report, no producer, bar, fix round or acceptance step — and
everything this paragraph describes is the build skeleton's (`run-types.md`). It emits the harness half — the wrapper, the findings /
producer / fix schemas, the bar vocabulary and its positive test, the per-section gate with its
bounded budget-guarded fix round, the liveness ledger that counts only UNCONDITIONAL spawns, a
read-only final phase and a tally whose `green` is a conjunction of separately reported terms —
dead agents (the REVIEWERS included: filtering a dead one out let an entirely unreviewed gate report
clean), open blockers, failed or unreported bars from every agent rather than the acceptance step's
alone, and a review that contradicted its own verdict. The
skeleton is `wfcheck`-clean and `wfdry` 7/7 as it stands, and `make check-tools` in the wfgate repo
re-verifies that on every build, so it cannot drift from the rules below.

What it does not do is make the script good: two real gates' first drafts were already
`wfcheck`-clean and each still cost a round. What it carries is the forms no check reaches — and of
its TODO markers only the ones inside `YOU OWN` are visible to a tool. A TODO left in a prompt ships.

**And the harness it emits is FROZEN from round 1.** The skeleton is `wfcheck`-clean and `wfdry` 7/7
as it stands; what a gate is for is the PROMPTS, which decide what a fleet builds. An edit to the
harness is admitted only by a **demonstrated wrong RESULT under stubbed execution** — a mode that
returns a verdict the script cannot justify, a branch no mode reaches, an agent counted in the wrong
ledger — and never by a tool's note count. Measured on the gate that stopped instead of closing:
three of its four script rounds spent their weight in the harness and two edits made a full round
trip. The producer schema went literal, then a call, then three hand-expanded 14 KB literals, then
the call again (settled at `wfcheck` 18 notes against 23), and `abortIfDead` came back
byte-identical to the form it had two rounds earlier — with the argument for the version being
reverted still standing in the file as a comment indicting the line below it. The prompts were
judged converging throughout by the same reviewers.

**The two checkers pull opposite ways, and that is the mechanism.** `wfcheck` reads the parsed
program, so it rewards a schema it can see STATICALLY — a literal, not a call that builds one.
`wfdry` executes, so it rewards a run that goes DEEP. Tune for one and you can lose the other
without noticing: a red-bar abort added to satisfy a rigour argument about producers took five of
seven `wfdry` modes from 15/25/25/25/1/15/25 agents to 18/1/1/1/1/1/1, and the schema literals added the
next round to satisfy `wfcheck` shipped three unverified duplicates of one builder that had already
diverged in every `command.description`. Neither round re-measured the other tool, and nothing in
the workspace recorded which trade had been settled on what measurement, so the round after
re-argued it from zero. So: **a round that edits the harness runs `wfcheck`, `wfdry` and a stub
BEFORE and AFTER its own fix and records both deltas** — in the script, as a comment beside the
settled trade, with the numbers that settled it. A later round re-opening one then has to refute a
measurement rather than a preference.

**And the AFTER is re-taken at the END of the round, after the last edit of it.** A measurement
recorded at the moment the harness edit is made goes stale inside the very round that writes it —
measured twice on one gate, both times found by the NEXT round's lens reading a comment that no
longer described the file it sat in. A banked number is a claim like any other, and a round's last
batch is what it is a claim about.

**Measure every schema's WIDTH before the launch, and probe the wide one.** An output schema the
launch's safety classifier refuses kills that agent alone —
`blocked by safety classifier: output schema too large to classify safely` — and it lands wherever the
wide schema is, which on one run was four agents and on another the fifteenth and LAST agent, six
hours in. `wfdry` prints the serialized width of every schema the script composes, widest first: the
HAPPY PATH's width, which is the quantity its corpus points are measured in, and beside it the wider
one a failure branch composes, which is a separate statement — the launch sends whatever the branch
that runs composes, and a wide branch nobody rehearses is exactly where the expensive refusal landed.
**`wfdry` carries the band and WARNS across it — do not restate the two numbers here or reason from a
number you remember.** It holds the highest clean width the corpus has seen ACCEPTED and the lowest it
has seen REFUSED, and the gap between them is UNMEASURED rather than safe: a schema inside that gap is
warned with BOTH ends named, and one at or above the refusal is warned with the floor it crossed. The ceiling moves UP whenever an acceptance is measured and the floor moves DOWN whenever a
refusal is, so any figure written into a document is stale by construction; the runner's own message is
the current one. The answer to that warning
is not an argument about the number: it is a throwaway workflow with ONE cheap agent carrying the
schema and a prompt saying to fill it with placeholders, which answers in SECONDS at nearly no cost —
one recorded probe returned its refusal in under a tenth of a second for zero tokens, and a gate that
probed twice inside one round moved a corpus constant for well under a minute of work. A gate that had
measured its own width to the byte, written the risk down, and accepted it on the ground that
"acceptance cannot be measured from here" was wrong on exactly that clause. Narrow a wide schema by
declaring a repeated leaf once and referencing it: the launch RESOLVES `$defs`/`$ref` and ENFORCES
the `minimum`/`minLength` that reach a leaf through one — the same probe proved it, by having a
deliberately illegal first attempt rejected — and `wfdry` resolves them too, so the narrowing no
longer costs the dry run the leaf it references. (It did: a `$ref`'d bar leaf came back as the raw
placeholder STRING, `obs.bars[k].result` was undefined and the clean mode returned green:false with
every bar RED. A reference the runner cannot expand — a typo in the pointer, a remote URL — is now
NAMED in a note.) A schema that does not SERIALIZE fails `wfdry` outright: there is nothing for the
launch to send.

**A schema edit is a `wfdry`-worthy event, because width is discoverable ONLY by running it, and the
cost of a shape is not visible in the shape.** Measured on the first gate outside mocker: turning one
bar report into a required-keyed object — a correctness fix a reviewer had asked for, six keys — took
three composed schemas to 4663, 4658 and 5312 bytes, all above the corpus's measured refusal, because
an inline leaf is serialized once per key. Referencing that leaf brought the same three to 1662, 1657
and 2311. Nothing about writing the six-key object suggests that price, and no static check reports
it — `wfcheck` reads the parsed program and does not serialize, so a second width measurement there
would be a second answer to a question the runner already answers exactly. Run `wfdry` between schema
edits, not once at the end of the round.

**A `$ref` is anchored at the SCHEMA ROOT, so write the `$defs` there — or write the whole pointer.**
`#/$defs/prop` is a pointer into the document the schema IS, so a `$defs` on a SUB-schema — beside the
leaf it declares, in the same constant, which is the natural way to write it — is not reachable by
that pointer. A nested `$defs` is legal; what is wrong is the ROOT-ANCHORED spelling reaching for it,
and either fix works (move the `$defs`, or write `#/properties/report/$defs/prop`).
Every property then comes back a placeholder STRING and every term reading through one is false in
every mode. One gate read that as the reference support being broken, reverted to twenty-two inlined
copies of the leaf and paid about 1,900 bytes of width for it (2,894 inlined against 982 referenced,
re-derived — the artifact's own "about 1,800" prices its DESCRIPTION trim, not the reference), against a classifier with a measured
refusal at 4,233. The root-anchored spelling resolves, including from keys built by
`Object.fromEntries(...map(...))` — nothing about computed keys defeats it. `wfdry` now names the
pointer that WOULD resolve rather than only reporting that yours did not.

**A red `wfdry` mode is a fact about the SCRIPT or about the FIXTURE, and the two read alike.** Three
of one gate's dry-run reds were fixture-synthesis limits reported as artifact defects: a `$ref` the
runner could not expand, twenty-two booleans defaulted false because their names carry no polarity,
and `finalCount === 22`, an integer against a constant that no report-shaped fixture can satisfy.
Each cost a reading of twenty-two "unmet" rows to diagnose as ONE gap. `wfdry --fixture <label>`
prints the object the stub actually returned to that agent, which separates the three in one look;
the clean mode's own verdict violation now points at it. Read the fixture BEFORE editing the script:
the run this corpus most regrets is the one that changed a correct script to satisfy a stub.

**A completion boolean is recognised by its NAME.** `wfdry` fills a report field as pass-shaped only
when the field name is one of `passed/ok/green/clean/verified/proven/done/complete/completed/succeeded/
finished/met/satisfied/achieved`, optionally prefixed by `goal/scope/slice/section/acceptance/criteria/
criterion`. Anything else is FALSE in every mode, so the branch behind it is exercised by none and the
`clean` mode fails correctly while nothing says why — three field names were burned discovering this by
reading the runner's source. The WARN now names the vocabulary. Rename the field, or declare the
polarity in the field's `$comment` — `"$comment": "x-pass: true"`, `false` for a failure flag —
remembering that the annotation rides in the schema the safety classifier reads and widens it.

**The polarity declaration goes in `$comment` and NEVER as a schema keyword, because the launch
refuses the whole schema over an unknown one.** `agent({schema})` is validated in JSON Schema STRICT
mode: `x-pass` as a KEY comes back as `strict mode: unknown keyword: "x-pass"`, the schema is rejected
at spawn and the agent never runs. It cost one gate its most expensive round — a dry run advised the
annotation, the author wrote it, the first evidence agent died at spawn and the script's own abort
took the other two with it, 26 of 27 acceptance properties unproven after 1.9 M subagent tokens and
116 minutes [M-35]. Probed since, one cheap agent per schema: `x-pass` on an array, `x-pass` on a
boolean, `x-foo` and a plain `passPolarity` are all REFUSED, so the refused class is UNKNOWN KEYWORD
and not the `x-` prefix; `$comment`, `description` and `title` are all ACCEPTED, on the root and on a
leaf. `$comment` is the home rather than the other two because those two are read BY THE MODEL as
guidance about the field. `wfdry` refuses the keyword itself now, so the dry run fails where the
launch would. A second probe asked which DRAFT the validator implements, because a schema is refused
for a spelling from the wrong one just as surely: it is draft-07 plus `$defs` and `$anchor`, so
`prefixItems`, `dependentRequired`, `dependentSchemas`, `unevaluatedItems`/`unevaluatedProperties`
and the other 2019/2020-only spellings are REFUSED — write `items`, `dependencies` and
`definitions`/`$defs` [M-35].

**A report ARRAY is filled or emptied by its NAME too, and the vocabulary is a list of DEFECT
words.** `wfdry` returns an array EMPTY in the passing modes and holding a mixed PAIR elsewhere — two
items, so both sides of every `filter(f => f.severity === 'blocker')` are exercised — only when its
name carries one of `blocker/major/failure/unfixed/open/concern/dead/unverified/violation/
gap/risk/defect/error/issue/problem/finding`, and two of those sixteen are narrower still
(`failures` and `unfixed` are populated only in the mode named for them). Every other array comes
back holding one placeholder in every mode — so a `green` term reading `x.length === 0` over an array named for its subject rather
than for the vocabulary is FALSE in the clean mode, and the mode fails while nothing is wrong.
Measured on one gate that spent two rounds reaching for that term over `unheldProperties`, including
a reviewer's proposal to make the items OBJECTS rather than strings: it was tried, and it changed
nothing — the discriminator was never the item type nor `required`, which the runner does not read.
Declare the polarity instead, in the array's `$comment`: `"$comment": "x-pass: false"` says a
populated array is the failure shape (empty in the passing modes), `"x-pass: true"` says the opposite
(empty in `acceptfail`), and an explicit polarity beats every matcher. It costs bytes in the schema
the classifier reads. In `$comment` and not as a keyword, for the reason two rules above: the launch
refuses the whole schema over an unknown keyword, and this annotation was a keyword until a gate paid
116 minutes to find out.

**Name the spawn wrapper `spawn`.** A script must wrap the spawn call — an unhandled throw at a serial
step aborts the run (see the gate-checks' failure-handling section) — and `wfcheck` reads every later
check off its inventory of call sites. The name is a CONVENTION the checker reports, not the mechanism
it depends on: a wrapper is identified structurally, by its body calling `agent()`, under any name,
declared anywhere, and through an alias. That is a repair, not a relaxation — the checker used to keep
a list of acceptable names, and a 100 KB script with nineteen spawn sites was reported as **one call
site with a largest prompt of 0.0 KB, zero errors**. It also ERRORs when its inventory is implausible
against a second, textual count of the same population, and WARNs on a call that goes straight to
`agent()` with no wrapper at all. Use the name anyway: a reader greps for it.

---


**Declare file ownership in the form the checker parses.** A producer's prompt says what it owns, and
`wfcheck` reads those declarations to find two sections writing into one file — a collision that is a
LOST WRITE, not a merge conflict, when the two run in parallel. The form is a `YOU OWN` heading
followed by one path per line, each line STARTING with the path:

```
YOU OWN
  internal/api/preview.go — the handler and its request struct
  web/src/components/PreviewPanel.tsx
```

**The gate's `owns` array is the other end of the same check**, and `wfcheck` compares them: a path a
producer declares that no gate lists is a file a reviewer can be briefed on and no fix agent may
touch. A gate config is recognised as `owns` AND `reviewers` together, so `owns` on an agent's own
options object is not one. An entry may be a sentence about a path
(`internal/mcp/tools.go (the table only)`) and may end in `/` to own a directory; an entry naming no
path at all leaves the rest of the comparison NOT JUDGED and prints the gate's name, because a list the
tool could not read cannot convict a path of being unowned. It reads what the prompts DECLARE — a file
a phase is ordered to touch and nobody assigned to anyone is invisible to it.

A path in the middle of a sentence ("read `internal/router/router.go` for context") is prose and is not
counted; a `file:line` citation owns nothing; a line under `DO NOT`/`must not` is read as an exclusion.
A repository-root file with no slash is recognised by name (`CLAUDE.md`, `Makefile`, `go.mod`), and a
path declared twice is NEW if either of its lines says so. The block ends at the next SHOUTY heading.
`wfcheck` prints the population it parsed — `0 owned path(s) parsed from 4 prompt(s) declaring YOU OWN`
is a check going unrun over a script that thought it had declared ownership, and the DENOMINATOR counts
prompts that carry a `YOU OWN` heading whether or not their entries parsed, so the two numbers say
different things on purpose, and one live run shipped exactly that: adopting the form turned the note into `12
owned path(s) parsed from 8 prompt(s)` and found two pairs of sections writing into the same Go package.

**A block a factory composes is read from the SOURCE TEXT instead**, and the report says which half
ran. A prompt assembled by one factory out of a per-section config resolves statically to nothing, so
the parsed half honestly reports zero — measured on a live script that declares sixteen paths, on
which the arithmetic was then done by hand four times in one gate, by three different agents. `wfcheck` now reads every `YOU OWN`
block out of the script's string literals (comments excluded — a comment describing an ownership
scheme is not one), reports the paths no resolved prompt carries, ERRORs when two BLOCKS name one
file, and folds them into the one reach report against the gates' `owns` arrays. It is the WEAKER
half and says so: it knows which BLOCK declares a path, not which agent is handed which block, so a
block spawned twice looks like one. Keeping the factory is fine — inlining four 60-line prompts to buy
the parsed check back is a round trip one script gate made and reverted.

**Write `phase()` titles as LITERALS at the call site.** A gate helper that takes the title as a
parameter (`gate(name, phaseTitle, …)` calling `phase(phaseTitle)`) is unmatchable against `meta.phases`
by any static reader, and `wfcheck` reports the declared entries as uncalled. Hoisting the four calls
back out to literal `phase('Gate 1')` at each call site is also better for the progress display, which
is the same argument from the other end.

**A coordinate into a document the same gate edits must come from the document's STRUCTURE.** Handing
each agent `sed -n 'A,Bp' context.md` for its own section is correct exactly once: the next round's fix
grew one brief by 65 lines and left six of nine sections pointing at the wrong text — one truncated, one
starting inside its predecessor, one missing the block it had just been given, and the acceptance step
receiving the smoke section in place of the number table. Every agent would have read someone else's
task with no way to know. Anchor on the headings instead, which move with the text they name:

```js
D: "sed -n '/^## §D\\./,/^## §E\\./p' " + CTX,
```

**A command written in a markdown table is read RAW by its consumer.** The agent reads its section
through `sed`, not through a renderer, so the cell escaping survives into the shell. Escape only the
cell separator, and never put a `|` inside a regex in a table cell: GNU `grep -E` reads `\|` as a
LITERAL pipe, so the alternation matches nothing and the command prints `0` and exits 1. That is the
dangerous half — six other rows carrying `\|` in the same table failed LOUDLY (`sed: invalid option`),
and only the two inside a regex returned a plausible number, on an acceptance step whose whole job is
re-deriving numbers.

**A `<placeholder>` inside a command a prompt hands over runs, exits 0, and prints NOTHING.** The
receiving agent cannot tell that from a section that genuinely says nothing, so it reports the
absence. Measured on one gate: two BLOCKERs in one round for `sed -n '/^\*\*<its id> /,…'` ranges
handed to reviewers, and a third instance in the NEXT round's own lens prompts, caught by grepping
before dispatch rather than by any check. Either interpolate the value (`${id}`) or state in prose
that the agent substitutes it — and grep the prompts for `<` before a round goes out. `wfcheck` now
reports the shape as an INFO with the line, deliberately not louder: over 73 frozen script revisions
it reports 40 lines carrying 12 distinct commands, and only three of those were the defect — the
rest are templates an agent is meant to fill, and which one a line is depends on something that is
not in the text.

**An acceptance finding of the form "X does not exist" must name the COMMAND whose empty output it
read.** A property reported met carries its evidence and a later gate judges that evidence; a claim
that something is ABSENT carries none, and the acceptance step is read-only, so its report is the one
artifact in the pipeline nothing downstream re-executes. Measured on a green run: the step reported
that a discriminating test "does not exist anywhere in" a package, and the test existed — written by
that same run, named after exactly what it discriminates, stating its own mutation in its own doc
comment. Nothing caught it, because there was nothing to catch: an absence with no command behind it
cannot be reproduced or refuted. With the command in the finding, both are one paste away. Put such commands in a fenced block, or write the alternation without a pipe.

**No phase that can WRITE may run after a phase that MEASURES.** When the verdict rests on quantities
collected during the run, enumerate those quantities once and check the PHASE ORDER, rather than
repairing each accumulator at the site where it is read. Measured on a seventeen-round gate:
the last fix-bearing gate ran AFTER the acceptance phase, so every measured term was stale-able, and
there were eight of them. Rounds 9, 10, 11, 12 and 13 each found exactly ONE — `barFailures`, then
`unverified` and `disputed`, then `acceptBlockers`, then `barFailures` and `goalMet`, then
`acceptBlockers` and `goalMet` again. Five rounds, one defect, and the sibling rule about a guard
patched in the direction each reviewer faced did NOT fire, because nothing was being weakened: each
fix was locally correct, one accumulator at a time, forever. Reordering — the fix gate before the
acceptance phase, acceptance the LAST measuring phase, a read-only verification last — deleted 149
lines and all five defect classes at once, at the price of one new phase. When the order cannot be
changed, the measured term is re-taken after the write, not patched where it is read (which is what
the conditional re-measurement below is).

**Every agent that grades itself and gates the verdict draws another auditor, and the auditor grades
itself.** The same gate walked it: round 9 asked for a sampler because the acceptance claims were
self-graded, round 10 asked for a re-measurement because the sampler's own output could not be
cleared, round 11 asked who audits the re-measurement. What ends it is not a fourth auditor but a
change of KIND — split the verdict's terms into MECHANICAL ones (re-derivable by anyone, no
judgement) and JUDGEMENTS, and gate the judgements on STRUCTURE: was every property ANSWERED by
someone, do two independent readings DISAGREE. Never on whether an answer was true, which is the
question that regresses.

**The acceptance step is the run's most parallelisable work, and its re-measurement is the other half
of the same step.** Two rules, one place, because doing either without the other moves the cost rather
than removing it. FIRST: where an acceptance property is a MUTATION — apply what the property names,
run the test it names, watch the red, revert — the properties are independent in everything but the
tree, which is what `isolation: 'worktree'` exists for, and one agent walking them in series is the
largest single RECOVERABLE wall-clock item this corpus has measured — one run spent a fifth of itself
in that step and its re-measurement, both walking their properties in series [M-33].
Scope each agent's command by the paragraph below before fanning anything out. SECOND: a fix landed by
a gate AFTER the acceptance step invalidates what that step measured, so the run re-measures
conditionally over the edited tree — without it a launch reports an acceptance verdict frozen at its
pre-fix value, which is not hypothetical: one run returned red over a tree whose hole its own gate had
just closed. That re-measurement costs about what the step it repeats costs, so it fans out the same way or it
becomes the new serial tail. `wf new-script` emits both
as a TODO beside the acceptance step rather than as code: the shape is the author's, the constraint is
not.

**A producer iterates on the PACKAGE-scoped bar and runs the whole-tree one once, before it reports
done.** A whole-tree test costs roughly twenty times a package-scoped one, and most agents already
work this way, so the rule's own saving is small and it is banked at that size rather than sold larger
[M-32]. What makes it worth stating is that it is the SAME rule a fan-out needs one paragraph down,
where the failure is not slow but fatal. Write it into the prompt beside the bar list: iterate on your
own package, run the full set once when you believe you are finished, and report THAT run.

**A fan-out that verifies by MUTATION scopes its test command to the package the property names.**
The shape is right — N properties, each applying its own mutation and watching a named test go red,
are independent in everything but the tree they share, which is what `isolation: 'worktree'` exists
for. What kills it is the BAR each agent then runs: an unbounded whole-tree test run holds gigabytes,
and concurrent ones are what an OOM killer resolves by wiping the session [M-31]. So the discriminator
is the SCOPE of the command, not the number of worktrees — read the target's OWN build file for what
its bar actually invokes, because a bar that is already narrowed or memory-capped is a different
argument from `go test ./...`, and one wave of package-scoped runs is a different number from one wave
of whole-tree ones. A whole-tree bar, and anything through docker or a shared daemon, stays in the
single-tree half of the step. Size the fan-out against the concurrency cap rather than the property
count — the cap decides how many of the N actually run at once [M-30] — and if a property cannot be
observed by a package-scoped command, that property belongs in the single-tree half too.

**A worktree-isolated agent calls git by its ABSOLUTE PATH, and its prompt says so.** Claude Code
refuses a git command in an isolated agent unless it can see that the command targets the agent's own
worktree, and a PreToolUse hook that rewrites `git …` into a wrapper on the operator's box makes that
unreadable: the agent's `git log` and `git status` are refused while `/usr/bin/git log` runs [M-45].
The script cannot see the operator's hooks, so it cannot know whether its agents will hit this; the
prompt of every agent given `isolation: 'worktree'` that touches git carries one line telling it to
call git by the path `command -v git` prints. `make`, `go test` and `wf` run git as a child process,
which the check does not read, so the bars themselves are unaffected.

**A subagent inherits the LAUNCHING SESSION's instructions, not only its `effort`.** `model-routing.md`
states the effort half of this; the other half is that every standing instruction the launching session
carries — how to read a file, how to search, which tool to prefer — reaches every agent the script
spawns, and neither the script nor its pre-flight gate can read it off the source it is reviewing.
Measured on one live producer, the largest agent of its run: 256 `Bash`, 64 `Edit`, 6 `Read` — and
186 of the 256 are navigation alone, `sed -n`, `rg`, `cat` and `grep` doing what `Read` and `Grep` are
for, each one a separate round-trip carrying the agent's whole context [M-29]. Nothing in
the script asked for that — the operator's box tells its own sessions to navigate through the shell,
and the agents inherited it. So every prompt that reads or writes the tree carries a NAVIGATION line:
read with `Read`, search with `Grep` and `Glob`, and reserve `Bash` for the bars, for git and for what
genuinely needs a shell. `wf new-script` emits it as one shared `NAV` const interpolated into the
producer, reviewer, fix, recheck and acceptance prompts, because an instruction that arrives from
outside the script can only be overridden INSIDE it, and a habit stated once per prompt is a habit
stated in none. What is NOT measured is the ratio after the change; the claim is the 256:64.
