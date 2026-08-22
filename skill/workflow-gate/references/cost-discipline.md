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
