# Workflow orchestration guide

Full operating manual for the `Workflow` multi-agent orchestration tool: model routing, the opus
pre-flight script-review gate, the per-section dual-review gate, and cost/context discipline. It is
delivered as the `workflow-gate` skill; **before authoring or running any Workflow, read every
reference the SKILL.md maps to your activity — in full.** `wf docs guide` prints the five authoring
references; the reviewer checklist is `wf docs checks`.

Four companions, all used by the pre-flight gate below and none auto-loaded:
the gate-checks reference (`wf docs checks`; `wf init` writes a pinned copy into every workspace — the standing list of failure modes from previous runs — reviewers
read it, and every novel blocker appends a line), `~/.local/bin/wf` (the gate harness: the workspace
that holds the artifacts, the atomic apply, the round with its validated findings ledger, the freeze,
the verify, the delta with its closure checklist, and four artifact checks — cite, dup, drift, rules — it
owns the BOOKKEEPING of the gate; the script's own static and dynamic checks stay
in the two binaries below),
`~/.local/bin/wfcheck` (a static checker for these
scripts, reading the parsed program; run it before the gate and after every fix) and
`~/.local/bin/wfdry` (a dynamic runner that executes the script with stubbed hooks across the modes a
real run can enter; run it before each freeze).
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

**Start from `wf new-script <name>`.** It emits the harness half — the wrapper, the findings /
producer / fix schemas, the bar vocabulary and its positive test, the per-section gate with its
bounded budget-guarded fix round, the liveness ledger that counts only UNCONDITIONAL spawns, a
read-only final phase and a tally whose `green` is a conjunction of separately reported terms. The
skeleton is `wfcheck`-clean and `wfdry` 7/7 as it stands, and `make check-tools` in the wfgate repo
re-verifies that on every build, so it cannot drift from the rules below.

What it does not do is make the script good: two real gates' first drafts were already
`wfcheck`-clean and each still cost a round. What it carries is the forms no check reaches — and of
its TODO markers only the ones inside `YOU OWN` are visible to a tool. A TODO left in a prompt ships.

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

A path in the middle of a sentence ("read `internal/router/router.go` for context") is prose and is not
counted; a `file:line` citation owns nothing; a line under `DO NOT`/`must not` is read as an exclusion.
The block ends at the next SHOUTY heading. `wfcheck` prints the population it parsed — `0 owned path(s)
parsed from 4 prompt(s) declaring YOU OWN` is a check going unrun over a script that thought it had
declared ownership, and one live run shipped exactly that: adopting the form turned the note into `12
owned path(s) parsed from 8 prompt(s)` and found two pairs of sections writing into the same Go package.

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
re-deriving numbers. Put such commands in a fenced block, or write the alternation without a pipe.
