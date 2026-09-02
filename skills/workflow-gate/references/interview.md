# Grilling the decisions into existence (step -2)

The window before the first artifact exists, and what has to come out of it. Step -2 of the
sequence in `pre-flight-gate.md`; the workspace itself opens in `gate-harness.md` — and it opens
FIRST.

-2. **Grill the DECISIONS into existence, inside a workspace opened before the first question.**
   Run `wf init` (step -1) before the interview starts, not after the document exists: the only
   clock this toolchain has is the workspace's own git, and `wf stats -time`'s authoring window
   (init → first artifact) prices the interview only when the interview runs inside it. Measured on
   one gate whose document was drafted before `wf init`: the session spent longer before the
   workspace existed than the gate's own first three rounds took together, and every minute of it
   is outside every number the tool prints [M-41]. `wf stats -time` prices the window — run it
   across the workspaces you have; it has run from a couple of minutes to well over an hour, and its
   short end is not fast authoring but exactly those gates whose document was drafted BEFORE
   `wf init`, so the drafting itself falls outside every measurement banked anywhere. What that
   window produces is read
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
   carrying none compares nothing. **A marker goes on a site that STATES the rule, never on one that
   points at it.** "That is D3.1's rule", "forced by", "so X is satisfied by" — the natural instinct
   is to mark every mention, and a sentence that merely refers to a rule states nothing and therefore
   disagrees with every statement of it. The check reports those as conflicts and names the cause
   under its output, which is one round too late to save the markup pass; the repair is to drop the
   marker from the reference, never to reword the artifact until the check is quiet. Do NOT `wf add` the transcript: a derived document beside its
   source is measured noise (a derived context document yields 21 to 25 `dup` groups per one real
   contradiction) and every reviewer of every round would pay for it.

   **It ends when the frontier is empty AND the operator says the understanding is shared** — an
   interviewer that runs out of questions and starts drafting has replaced the interview with its own
   opinion. Then, and only then — the workspace has been open since step -1: `wf add <draft>`,
   `wf round new`, and round 1 reads it.
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
