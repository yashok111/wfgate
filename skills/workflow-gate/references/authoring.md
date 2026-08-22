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

