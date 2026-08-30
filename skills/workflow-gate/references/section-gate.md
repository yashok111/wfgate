# Workflow section-review gate (two vectors minimum)

Every Workflow **section of work** ends with a review gate **before that section is considered done** and the next one starts. This is per-SECTION, **NOT per-agent** — never gate individual `agent()` calls; gate the section boundary.

A section is a **declared gate boundary**, not necessarily one `phase()`. It is also a **COMPILATION boundary**, and that constraint is not negotiable by subject: measured on one gate, a Go package was split across two sections two phases apart because the two subsystems read as separate in the decision document — one section changed `populateScoped`'s signature and the other held its second call site — so the package did not compile in the window between them, and every gate over the first section reviewed a tree that could not build. Group by what must compile together first, then by subject. Several producing phases may share one gate — that is often right, because the phases that build one seam are reviewable only together — but the grouping must be a STATED choice: the script says which phases each gate covers, in the gate agent's own prompt and in `meta`. Left unstated, a run with three producing phases and one gate is indistinguishable from a run that forgot two gates, and neither the author nor a reviewer can tell which it is.

The gate = **at least 2 reviewer subagents in parallel** (a `parallel()` barrier), each auditing that section's changes along a **different vector that is actually valid for the task**. Pick from:
- code change → correctness/bugs · security · simplification/reuse/efficiency  (2 minimum)
- research/report → factual accuracy (verify claims) · internal consistency · completeness/gaps  (2 minimum)
- design/plan → feasibility vs real code · risk/blockers · alignment with the goal  (2 minimum)

**Two is the FLOOR, not the width, and the gate is the cheap place to widen.** Section boundaries force
serialism and cannot be argued away — they are compile boundaries, above — but the reviewers inside one
are read-only: they mutate nothing, share no tree state, conflict with nothing and run concurrently, so
a third or a fourth vector costs one more sonnet read of one section diff and NO wall-clock at all.
**Reviewer width is bounded by tokens, never by the clock.** So a section with more than one plausible
failure surface carries a vector per surface, and only a section with one surface stops at two. What the
extra vector buys is not a second opinion but a second QUESTION: a prompt asks what it asks and nothing
else, and two lenses reading the same lines, both correct and both thorough, routinely find different
defects — the one that found the blocker was the one asked the other question [M-27]. Widening is not a
licence to skip the vector choice: four vectors that all read "correctness" is one vector billed four
times.

Rules:
- **Reviewers are `model: 'sonnet'` and `effort: 'high'` by DEFAULT.** Both axes are explicit on every
  `agent()` call (see `model-routing.md`); a reviewer is a step whose error nothing downstream
  re-checks, which is what `high` is for, and the fan-out effort ban does not reach it. `wfcheck`
  refuses a call with no `effort`, so a reviewer written from this section without one does not run. Opus on a reviewer is reserved for a genuine extreme case — a security-/correctness-critical section where a subtle, high-cost-if-missed defect is plausible (e.g. crypto, auth, a kill-switch, money/data-loss paths). Opus review is the rare exception, not the norm; if you can't name the specific catastrophic miss it guards against, use sonnet.
- Feed each reviewer the section's diff or output + the section's goal.
- Collect findings, then ACT: a blocker from either vector loops that section (fix → re-review) before proceeding; advisory findings — MAJOR, MINOR or NOTE on the closed scale — are recorded and surfaced.
- **The loop is BOUNDED, and what survives it is carried in the RETURN value.** An unbounded "loop until no blockers" does not terminate against a fix agent that keeps failing, and it has no budget guard — which is why every real script instead runs one bounded fix round and moves on. That divergence between the rule and the practice is the defect; the rule is what changes. Write the form that works: a fixed small number of fix→re-review rounds, guarded on `budget.total`, and every blocker still open when the rounds run out pushed into an explicit open-blockers list in the return value — never only into a `log()` line. A run that ends green while carrying open blockers must say so in its verdict, not in its scrollback.
- **A run ends on a read-only step, or on a gate over its last mutation.** The standard hole: an acceptance agent that may fix what it found, or a closing fix agent, makes the final edit of the entire run — and no gate follows it, so the return value reports green over code nothing reviewed. Either forbid those steps from writing (they report, a fix agent acts, and that fix is gated), or add a gate after every step permitted to mutate. Green over unreviewed final code looks exactly like green.
- Skip the gate only for trivial/throwaway sections (pure read-only locate/extract that mutate nothing) — `log()` that it was skipped and why.

