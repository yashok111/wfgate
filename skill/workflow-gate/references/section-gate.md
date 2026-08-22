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

