# Workflow gate: known failure modes

Standing checklist for the pre-flight gate on a Workflow script, in ANY project and any language.
**Every gate reviewer gets this file** ("known failure modes from previous runs — check these first"), and
**every gate round that finds a novel blocker appends one line here.** This is to orchestration what a
project's HANDOFF is to its code: without it, each new run's reviewers start from zero and re-discover
`git add -N`.

Every entry below actually happened on a real run and was verified by command before it was fixed. The
rules are stack-neutral; where an example is needed it is given in the smallest concrete form that makes
the rule recognisable. The corpus so far is eight multi-agent phases of one HTTP service (Go, SQLite,
~213 blockers over 43 gate rounds) plus one gate over these two documents themselves (22 blockers over
three rounds), so the *examples* lean that way while the *rules* do not — when you add an entry, state
it so it survives a change of language.

Six facts worth knowing before reading the list:

- Across the last two phases' gates (nine rounds), **zero** findings concerned orchestration mechanics as
  such — no `parallel()` that should have been a `pipeline()`, no unbounded loop, no budget defect.
  Everything of value fell into classes A–I below. Weight the review accordingly.
- **Vector yield, measured** (three reviewers, one vector each, round 1): SEAMS 3 blockers, CRITERION 3
  blockers, FACTS 0 blockers / 1 major / 12 minors — every citation and count in a 150 KB script checked
  out. FACTS is the vector to drop first as rounds descend, but run it ONCE: its one major was an
  invariant asserted in another reviewer's own prompt that the code did not hold.
- **The most expensive mistake is settled before any prompt exists, and it is usually a SCOPE or an
  ARCHITECTURE call.** Gate the decisions first, with two lenses, one of them external. Measured on the
  phase after this file's fifth: the two lenses independently rejected the same architectural decision in
  round 1, which inverted the design and split the phase in half — 26 blockers over six rounds, against a
  document that was a few tens of KB and cost minutes per round. Nothing found later in that phase came
  close in value, and none of it would have been findable in a prompt.
- **A fix is unreviewed code, and it is the single largest source of blockers.** One phase: half the
  blockers of rounds 3+. The next: 5 of 11, with two consecutive rounds finding *nothing but* fix-induced
  defects. The one after that: **9 of 9** in a single round — every BLOCKER existed only because the
  previous round's fix created it. A round whose findings are all self-inflicted is the expected case,
  not a malfunctioning gate. Review the delta, not the original — and see section I.
- **A guard, a tripwire and a criterion are what to attack hardest.** They are the parts that look
  identical whether they work or not.
- **Including the tooling that guards the gate.** A static checker that comes up empty reports zero
  findings and exits clean, which is indistinguishable from a clean script and is trusted more, because
  a tool said it. One was measured finding one call site out of nineteen — every check it owned had been
  passing over an empty set, silently, across several phases whose reviewer rounds then paid for exactly
  what it was written to catch. Before crediting a tool for a layer of the gate, make it print the size
  of the population it examined. A checker that verifies a property only where the author opted in must
  print the count it did NOT verify, in the same line as its verdict: an empty population and a fully
  unverified one both read as green, and the second is the commoner of the two.

---

## 0. The severity scale, which is closed

Fill the `level` cell with one of **BLOCKER, MAJOR, MINOR, NOTE** and nothing else. `wf round record`
refuses any other word before it writes a byte, and every read path stays permissive so the closed
gates keep being readable.

**A finding's level is decided by what catches it downstream, never by how bad it reads [R2].**

- **BLOCKER** — no later gate catches it: if it rides, the run produces a wrong result that nothing
  refuses. Only BLOCKER loops the gate, and only BLOCKER is what the stop rule reads. **Vacuity starts
  here**: a finding that makes an observation, a guard or a tripwire *vacuous* — it passes whether or
  not the thing it checks exists — is classified BLOCKER, and rides only if this row can name the later
  gate that was ACTUALLY assigned a non-vacuity vector for it, which is the MAJOR test below and not a
  separate rule.
- **MAJOR** — a real defect, and the row **NAMES the later gate that catches it**. Cannot name one?
  Then it is a BLOCKER. That naming is what turns the classification from a feeling into a claim the
  next round can check, and it is the operative half of "classify by actual downstream coverage".
- **MINOR** — a real defect whose cost, standing forever, is below the round it would take to close.
- **NOTE** — not a defect. An observation or a question with no failure attached. Counted in a round's
  findings, never subtracted from them, and named apart by `wf stats`.

**Measured, which is why the scale is closed rather than suggested.** The six gate ledgers on disk held,
on 2026-08-25, 1065 findings under FOUR vocabularies for one scale: BLOCKER is universal, the tier under
it is MAJOR on five gates and CONCERN on one, and the tier under that is MINOR on four, ADVISORY on one
and NIT on one. 172 of the 1065 — 16% — carry a level no other gate can be read against, so no report
could put a round of one gate beside a round of another. (One of those gates is still open, so the
totals move; the shape does not.) A JSON Schema `enum` does not save the script side either: it validates the VALUE against a declared
list and never the LIST, and the scripts these six gates PRODUCED carry three lists of their own —
`['BLOCKER','MAJOR','MINOR']` (p2d, under the field name `level`, and p2f under `severity`),
`['MAJOR','MINOR']` (p2e), and `['blocker','advisory']` (a1, a2, a3, out of `wf new-script`'s own
template). The field NAME drifted too, which no value-level check can see. Six OTHER phases' scripts do
agree on `['blocker','major','minor']`, and the agreement is one COPIED LINE: this repository has
measured those same six as otherwise re-authored rather than copied, two of their gate functions
implementing one algorithm at 0.05 textual similarity. So the scale is closed here because this is the
one place both paths meet, not because either layer validates it.

**And what ENDS a gate is a question about a finding's SUBJECT, not its level.** The stop rule is
stated in section I and is unchanged by this; what follows is the question to ask beside it, from the
round where the count stops descending: **would a fleet BUILD the wrong thing, or would it merely be
told something slightly stale about a file it is about to read anyway?** Measured on the gate that
stopped instead of closing: not one finding after its round 19 was a defect in the SPECIFICATION —
they were defects in the harness and in the reporting, every one of them correctly classified
BLOCKER because no later gate catches it. The fleet would have built the right slice; the run would
have misreported it, aborted, or destroyed itself. The last blocker of the other kind was round 19's
— the one file where any tool is registered, owned by no section and named nowhere — and the gate
ran three more rounds after it. `wf stats` prints the mechanical half of the same reading: the share
of each round's blockers carrying `[fix-induced]`, and it names the rounds where that share is a
majority. A round that mostly re-fixes the previous round's fix is not evidence the artifact still
has defects.

---

## 0b. What a delta round asks

A delta round asks CLOSURE, FIX-INDUCED and RIPPLE, and may add vectors but never drop those three [R3].

- **CLOSURE** — for every `closed by` claim in the brief: did that commit reach what the finding names,
  and is the finding actually gone? `wf check closure` answers the mechanical half (a FILE miss is a
  verdict, a SECTION miss is a question); this vector answers the rest.
- **FIX-INDUCED** — what did the previous round's own fix break that was not broken before? Mark each
  one `[fix-induced]` in the ledger so the share can be read later.
- **RIPPLE** — which untouched paragraphs still argue from what the fix changed? `wf check stale -since
  <ref>` narrows the read to the sentences carrying retired vocabulary and prints NOT A PASS where the
  range retired none.

Anything else the slice needs is welcome beside them; `-asks other` records it so the roster stays
comparable without pretending the vector was one of the named ones. The three are a SET and every
spelling of a set is accepted: `-asks closure,fix-induced,ripple`, `-asks
closure+fix-induced+ripple`, a space-separated list, or the flag repeated.

**A banked verification is a claim like any other, and a delta round SAMPLES the previous round's
rather than inheriting them.** The habit is worth keeping — a NOTE row saying "verified, recorded so
a later round does not re-derive it" saves real work — and it failed checkably: one round banked
"ownership union exact at 33 files, and gate 6 lists all 33" while a thirty-fourth file joined in
the same commit that banked the claim. It was found because the next round's lens was instructed to
spot-check three banked verifications rather than trust them; three of four held, which is the right
ratio to keep the habit and the wrong one to trust it blind.

---

## A. Facts and measurements in the prompt

A number or a claim about the codebase that reaches an agent's prompt becomes an instruction; a wrong one
becomes a wrong instruction that no downstream step questions.

- [ ] **A number repeated across an artifact is a SEAM — derive it, or grep for every copy.** One count
      that grew by two lived in eight places; a careful fix updated four and left the other four stale,
      and the two it missed were the prompts of the reviewers whose job was to audit that very
      population, so both were told to check a smaller set than the run produced. And never write "this
      number is stated in N places" as a reconciliation note: that sentence goes stale before the number
      does, and while it stands it licenses every copy it failed to count.

- [ ] **Every number was produced by a command, not by memory.** Recorded misses: "zero references" when
      there were 781 (a bad search template over structured output); "17 of these responses are 204" (16
      were, one was a 200); "13 assertions in that file" (11); "this test asserts X twice" (zero — the
      pattern had matched an unrelated identifier).
- [ ] **A count is meaningless without its unit.** One document was legitimately "130 operations", "114
      schema-bearing primary variants" and "419 response variants". An agent handed the wrong unit
      silently narrows its work to match the number it was given.
- [ ] **Every cited file:line still says what the prompt claims.** Line numbers drift between rounds.
      `wf check cite` holds the checkable half — a path with an extension is verified, and a
      citation-like reference to an extensionless TRACKED file is now listed as "NOT verified" on
      every run rather than silently absent from the count (one correct `Makefile:90#corepack` sat
      invisible in a green report of 237 until a hand grep found it).
      What it holds: the path resolves to exactly one tracked file, the
      cited range exists, and where the citation was written with `#anchor`, the token is present in it.
      An anchor is an ASCII token — the artifacts are English-only — and a non-ASCII one is reported as
      an error, not silently counted as unanchored (15 Cyrillic anchors once hid inside "17 unverified",
      one of them factually wrong).
      When the TARGET repository itself moves after a freeze, `wf check drift` intersects its diff with
      the cited ranges and says which citations were EDITED (re-read the claim), which merely
      SHIFTED (re-aim; the new range is printed), which are GONE (the file left the tree) and which
      are UNRESOLVED (the path names no single file at the frozen revision) — the only mechanical
      signal an unanchored citation gets, since both its lines keep existing and the range check
      stays green forever. It refuses a target whose tracked files differ from its HEAD: "the target
      has not moved" is a claim about HEAD, and a cited line rewritten without a commit leaves HEAD
      where it was.
      Whether the cited lines SAY what the sentence beside them claims is the half no tool reaches, and
      it is the half this bullet is about; run the check to clear the mechanical noise first, then read.
- [ ] **Every symbol named in a prompt exists** with that exact name, arity and parameter order.
- [ ] **A status measured on a bare router is not the status the service returns.** Middleware in front
      of the router answers first: a request without the media type or origin the CSRF layer demands
      gets 415/403 where the prompt promised 405, so a mandated regression test fails against correct
      code. Measure through the whole chain the product actually assembles.
- [ ] **Mounting a catch-all in front of an existing router changes what that router answers for a
      method mismatch.** Registering "everything else" as a route steals the framework's own
      405-with-Allow and turns it into the catch-all's answer — check every method the existing routes
      do NOT accept before claiming the mount changes nothing.
- [ ] **Trap cases in the corpus are named explicitly.** If a heuristic must not match something, the
      prompt says which something — the one path that looks like an auth route and is not, the one field
      whose name ends in `_count` and is not a time.

- [ ] **A library's default limit, assigned inside its constructor, can be stricter than the
      application's own documented limit — so the documented one never governs.** Read the
      constructor, not just the option struct, and set the value explicitly.
- [ ] **A count that becomes a claim names the command that produced it.** A search tool's count flag
      counts LINES, not matches — the two differ by whatever the density happens to be, and the number
      reaches a prompt as an instruction either way. State the invocation next to the figure, so the
      next reader can re-run it instead of trusting it.
- [ ] **Reproduce, by command, the failure the phase exists to fix, BEFORE writing the prompt that asks
      an agent to fix it.** A prompt written from a hypothesis produces a fix nothing can falsify — the
      agent closes a defect that may not exist, and the acceptance check is written to match the fix.
      One written from a reproduction names the exact input that must stop working.
- [ ] **An option named for a response format may not cover every method the library serves.**
      One transport flag promised plain request/response; a method registered unconditionally by
      the same library forced streaming regardless, producing exactly the long-lived response the
      flag was chosen to avoid. Grep the library for the flag's own name before believing it.

- [ ] **Paste what the command actually printed, not what you expect it to print.** A pattern with
      a `*` quantifier can match an empty span and count a fragment you then add again by hand:
      one recipe read "→ 24, and the odd one is the twenty-fifth" where the command already
      returned 25, so every re-derivation landed one too high — in the single paragraph three
      gate rounds had been spent making command-verifiable.

## B. Seams between agents

The single richest class. A contract that spans two agents and lives in neither is the defect that
compiles, passes every test, and does nothing.

- [ ] **Every signature is pasted verbatim into the writer AND into every caller.** A contract left as an
      optional field of a report schema is a contract the caller will guess.
- [ ] **Every field an agent is INSTRUCTED to fill has a consumer, and the consumer runs after the
      producer.** Two halves of one defect, both hit in a single run: first a report field every producer
      was told to use for anything it could not do itself, which no line of the script read — so the one
      sanctioned escape from the ownership rule silently discarded every finding it collected; then,
      after a collector was added, the collector ran AFTER the only step that rendered it, so the field
      was populated for the return value and empty for the agent meant to act on it. An instruction to
      report something nobody reads is an instruction to waste a turn, and it reads exactly like a
      working channel from inside the prompt. Two adjacent same-typed
      parameters swap silently and still compile.
- [ ] **A contract block referenced only once is a bug**: it reached the writer or the caller, not both.
- [ ] **Any identifier two modules must agree on has ONE owner.** If module A may not depend on module B,
      A cannot compute B's encoding — drop the field and let the caller compute it. Otherwise the key is
      invented twice and rows land under keys nothing ever looks up.
- [ ] **A key is a key, not a description.** Check what actually distinguishes two rows. One key collapsed
      three distinct response selectors into a single status number and would have merged 88 records'
      hashes — including the one the phase criterion depended on.
- [ ] **Two spellings of "the same" identifier are two different keys.** A template (`/widgets/{id}`) and
      the concrete value that matched it (`/api/v1/widgets/7`) are both called "the path" in prose. A
      conversion between them writes a key the lookup can never generate: the row is orphaned, the merged
      view never shows it, and the test passes — vacuously, whenever the fixture happens to have no
      variable segment and no prefix. Ask what actually PRODUCES each side of the key at lookup time.
- [ ] **Both sides of a lookup key were read.** One side canonical, the other relative; both compile,
      every test passes, the lookup never fires in production.
- [ ] **The variant a later stage looks up is the one actually served**, after any override, not the one
      originally chosen.
- [ ] **A structured hand-off is not filtered by a regex that drops what matters.** In-module seams are
      often lower-case/private; a case-sensitive filter hands the next agent an empty list. Prefer passing
      the whole capped list.
- [ ] **A rule enforced by more than one agent gets ONE wording, pasted like a signature.** One field
      shipped with three definitions across seven consumers; the loosest turned the phase's own mandated
      output into a blocker.
- [ ] **A wire shape that must REJECT something needs a type that can SEE it.** Most decoders drop unknown
      keys silently, so a handler whose struct lacks the field answers "unknown value" instead of the
      documented refusal. A key you refuse is still part of your DTO.
- [ ] **A rule the acceptance asserts on is a contract, and belongs in the PRODUCER's prompt too.** An
      assertion written only in the test agent's brief ("the result element is absent before the button
      is pressed") is satisfiable by three reasonable implementations and only one of them passes.
- [ ] **A report field named in a prompt exists in that agent's schema.** A strict schema plus "report it
      in `measurements`" on a schema without `measurements` loses the number or kills the agent.

- [ ] **An accepted advisory applied to the metadata but not to the call is worse than not
      applying it.** A reviewer tier was changed in the phase table and not at the spawn, where
      per-call configuration wins: the change had no effect and the table now asserted something
      false.
- [ ] **Two producers must not publish the same field name with two different meanings.** One
      list tool's `truncated` meant "the list was capped"; another's meant "this row's body was
      cut". The consumer is a model reading both in one context.

- [ ] **Where a hook is INSTALLED decides which middleware has already run, and "before
      authentication" and "before authorization" are different positions.** A wrapper attached
      where the router builds its table sits INSIDE the chain, so anything the chain attaches to
      the request is already there; only the per-handler rejection is still ahead of it. Stating
      the wrong one of the two inverts what the hook can read — one draft concluded "no user is
      available here" and would have shipped the field permanently empty, behind a green suite.

- [ ] **A finding needs an IDENTITY before anything downstream can claim to have closed it.** Claims
      made against a finding's free text match nothing silently, match two findings at once, and are
      satisfiable by pasting every string back. Assign a stable id where the finding is first seen,
      claim by id, and fail the run on a claim that resolves to nothing — then normalise the claim
      (trim, strip a list marker) or an honest claim fails the run instead.

- [ ] **Two agents asked the SAME question from two prompts are two copies of one briefing.** Diff the
      instruction sets, not the code: five of one gate's script blockers were a sentence the first
      prompt carried and the second lacked — the field definitions, the carve-out for the one item the
      question does not fit, the discipline sentence, the severity that item earns. `dup` cannot reach
      them (the two prompts share no rare tokens there) and `stale` cannot (nothing was retired); it
      is found by printing the sentences one prompt has and the other does not, which is
      `wfdry <script> --prompt-diff <label>,<label>`. Naming the two is yours: ranking every pair by
      shared sentences was built and measured, and over seven tracked scripts it surfaces SIBLING
      prompts — two lenses of one gate, at 0.33 to 0.86 — while the pair that produced this rule
      ranks below every one of them, at 0.07 to 0.12.

## C. Ownership and definition of done

- [ ] **Every file an agent must change is owned by exactly one agent**, and every file whose behaviour
      the phase deletes has an owner. Tests asserting the behaviour being removed belonged to nobody once,
      so "green" was unreachable by construction.
- [ ] **When a phase REMOVES a behaviour, grep for the text that asserts it and give every hit an owner.**
      Not just the tests. Comments explaining why the behaviour exists, user-visible strings describing
      it, failure messages that quote it — all become lies the moment the change lands, and every one of
      them that is not a test is caught by NO bar. Measured: four such files in one phase; the ownership
      table listed none of them, and one was named in no producer's prompt at all, so a clean gate would
      have shipped a documented falsehood in a string every client of that surface reads.
- [ ] **Count the call sites before allowing a signature change** — in the tests as well as the sources.
      One constructor had 7 call sites in 6 files while the agent owned 1, with "the build is clean" as
      its definition of done. Prefer a setter or a new method over a new constructor parameter.
- [ ] **No prompt contradicts itself about ownership** ("change the signature if needed" + "the existing
      helpers must not have to change").
- [ ] **Two agents writing different files of the SAME module in parallel, referencing each other's
      symbols, is a deadlock**: neither reaches "it builds". Serialize, or give each a dedicated stub it
      deletes.
- [ ] **An instruction goes to the agent that can act on it.** "Compile it at build time" went to the
      agent forbidden to touch the build; "key it by the served status" went to the agent that builds the
      map rather than the one that reads it.
- [ ] **A report field is not a check.** An agent asked to "paste the grep output proving you wired it"
      reported the task complete with the file untouched, and the gate reviewer explicitly told to grep
      for the same thing passed it. Self-reports and reviewer instructions both failed on the same fact in
      one run. The only reliable enforcement is a step whose OWN success depends on the thing existing —
      an end-to-end check through the deployed artifact, a test that cannot pass without the wiring, a
      later agent that cannot build without it.
- [ ] **Wiring into the real entry point has an owner and is imperative, not conditional.** A `nil` left
      in the composition root ships as a dead feature with a fully green suite, because the TESTS wire it
      and the binary does not. Name the file, the call and its position.
- [ ] **A new test file in a shared package must not redeclare existing helpers.** List the package's
      existing identifiers and mandate a prefix for new ones — and check the package/module clause: a test
      that needs something importing the code under test may have to live in a separate test package.

## D. The criterion

- [ ] **The criterion is reachable by the harness the acceptance agent is told to use.** One end-to-end
      harness wired none of the phase's new sources and the agent could not edit it — the criterion was
      untestable while every test passed.
- [ ] **The criterion is observed, not asserted.** Decode the token, don't count its characters. Validate
      the body against its schema, don't check it is non-empty.
- [ ] **Each criterion NAMES the wrong implementation it exists to fail, in the artifact itself.** Not in
      your head — written beside the check, as "this fails an implementation that does X". A criterion
      that cannot name one is decoration. Measured: three review rounds over one acceptance list found
      almost nothing while the entries were bare assertions; the round after each entry carried its own
      claim found six, because a reviewer can audit a CLAIM and cannot audit an assertion. **This is an
      AUTHOR-side rule and it is the whole of what survives here.** Moving it onto the reviewer — a canned
      vector required to name a defeating implementation per clause — was measured and rejected: across
      three rounds of one gate that vector named a defeat on 12-18% of the clauses it was handed (8/64,
      9/51, 12/69) and never returned zero, and each defeat it named was closed by ADDING a clause, so it
      fed the loop it appeared to detect. The mechanism is what generalises, not the percentage: a
      reviewer asked for a per-clause verdict over a nonempty criterion returns SOMETHING, and its other
      82-88% is "none constructed", which nothing separates from "did not try". Measured on the gate that ran it: 352 lines of acceptance section carried 2
      lines naming a defeat, so the rule had never been applied at all.
- [ ] **An acceptance section states OBSERVATIONS, never test designs.** Every clause names a command whose
      exit status decides it, a number a query returns, or a property a reviewer can hold against a real
      diff — never a test to be written later and argued about now. A clause whose only reader is a
      reviewer IMAGINING an implementation has no floor: each round imagines a better cheat, the fix for
      each defeat is another clause, and the section grows faster than it hardens (measured on one gate:
      the assertion count went 51 → 69 in the round where the number carrying a named defeat went 9 → 12).
      Verification of a property that needs a test moves to the implementing run, and the section-review
      gate over the real diff judges whether the tests discriminate. Measured on two decision gates over
      one product, same operator, same target: the acceptance section written as observations — "`make
      test` and `make lint` are green", "the population literal equals the number of method+path
      operations in `api/openapi.json`, 48, not the number of `paths` keys, 31" — produced 8 blockers,
      went quiet after round 4, was never cut, and its gate closed on a genuine zero; the one written as
      prose test specifications was rewritten in EVERY round of its gate and grew 23 → 352 lines while
      the document around it grew 333 → 1319, absorbing a third of all growth, until a human cut it back
      to 92 lines of properties. **What share of that gate's 92 blockers were its is deliberately not
      quoted here**: five derivations of that one quantity exist, from 25 to 66, they disagree by up to
      2.4x on individual rounds, and the one the operator acted on was the highest — a verdict must not
      be the first consumer of a quantity nobody has agreed how to count.
- [ ] **A scope cut is an edit like any other and it is reviewed like any other.** Cutting a section back
      to its properties is the largest single edit a gate makes, and its DELETIONS are invisible to every
      check this tool has: `wf check rules` compares the SITES of a marked rule and cannot tell a
      statement from a pointer to one, `dup` and `stale` read what is still written, and nothing at all
      watches a decision that stops existing. Aim the round that reviews a cut at the deletion — diff the
      pre-cut span against the WHOLE post-cut artifact token by token, and for every token whose count
      went to zero ask whether the decision it carried survives anywhere. Measured: the round that did
      this returned 4 blockers and 20 findings, 17 of them defects IN the cut, and two of them were
      design decisions that survived nowhere afterwards — one of which had already been caught by hand
      two minutes after the cut landed, which is how the class was known to exist at all.
- [ ] **A criterion can be wrong in the direction of FALSE FAILURE, and that direction is worse.**
      Everyone checks whether a check is too weak. One that goes red against a CORRECT implementation
      stalls the run at the single step usually forbidden to change production code — so the agent that
      hits it can neither pass it nor fix it, and its options are a false failure report or quietly
      weakening the check. Test every criterion twice: against the wrong implementation it names, and
      against the right one.
- [ ] **An assertion about a BUILD ARTIFACT is verified against a real built artifact before it ships as
      a criterion.** Build tooling splits, minifies, renames and drops unreferenced code. The literal you
      expect in the output may live in a fragment the entry point never references, or may not survive
      the build at all. One command against a real build answers it; a plausible-sounding assertion
      about "the built output" does not.
- [ ] **No acceptance test can skip itself silently.** Report every SKIP the suite prints. One shipped
      exactly that: a relative path that did not resolve from the test's own working directory.
- [ ] **An assertion whose baseline must be captured FIRST cannot live in a subtest.** "Give each numbered
      assertion its own subtest" and "capture the baseline before anything below runs" contradict each
      other; obeyed literally, both captures happen after the mutating steps, and the guard passes while
      proving nothing. Structure instructions and ordering instructions need the exception written down.
- [ ] **A derivation assertion passes with the feature deleted when before and after look alike.** "Turn
      this observed 404 into an endpoint, then request it again" is satisfied by the original 404. Assert
      what CHANGED — the status, the kind, the stored key. Two corollaries for the inverse assertion, the
      one that claims something did NOT change: it must name WHICH subject it reads when the run creates
      more than one candidate (against the wrong one it passes vacuously), and it must be conditional on
      the action that would have changed it having actually been observed — a failed call leaves the
      counter unchanged too, so the unconditional form passes with the whole feature deleted.
- [ ] **When a client could hard-code a configurable value, run the acceptance with a NON-DEFAULT one.**
      Otherwise a client that hard-codes the default and one that reads it from the server emit
      byte-identical requests, and the assertion passes with the whole configuration seam deleted.
- [ ] **A proof that a setting took effect must run after the thing it probes exists.** One acceptance
      script checked a per-resource endpoint before the resource was created; the answer was the
      generic not-found, so the check failed for a reason unrelated to the setting it was proving.
- [ ] **The criterion is checked through the deployed artifact**, not only the test runner. "Green tests ≠
      phase done": the container path has found defects nothing else could, twice.

- [ ] **A step added to an acceptance sequence can invalidate a LATER assertion in that same
      sequence.** A call added purely for coverage bumped a counter a later check asserted was
      unchanged — red against correct code. When adding a step, re-read every assertion
      downstream of it and say which reads bracket which action.
- [ ] **Two assertions in one sequence must not pin the same sentinel value.** A lower layer
      pinned the same status a higher layer's check forced; both halves of the second check then
      passed with the second feature entirely deleted. Where two checks use a magic value, say in
      the brief that they must differ, and name the value the house style would reach for.

- [ ] **Check that each criterion's named counter-example actually FAILS it.** Naming the wrong
      implementation is not enough — write the counter-example out and run the assertion against
      it in your head. Measured: "copy this record, then change the source, then re-read the copy
      → unchanged" was green against the very implementation it named, because a snapshot taken
      at copy time is also unchanged by a later edit. Move the divergence BEFORE the operation.
- [ ] **The way the harness normally builds its fixtures may itself invoke the feature under
      test.** A criterion asserting "no side effect has happened yet" goes red against CORRECT
      code when the standard two-step setup calls a route the new hook wraps. Read the fixture
      recipe, not just the assertion, and pin the setup shape in the criterion — including a note
      that it deliberately departs from the house pattern, or the next producer restores it.
- [ ] **A configuration value whose effect is only exercised by unit tests is never proven to
      reach the binary.** Set it to a NON-DEFAULT value in the deployed run and make at least one
      observation whose OUTCOME depends on the value; otherwise a build that ignores the knob
      entirely passes every check. (An existing preamble in the same script did this and said
      why; the new criterion quoted that preamble and then skipped its rule.)
- [ ] **A window measured against whole-second timestamps is one tick wide when it is small.**
      With second-granular storage, a two-second window suppresses only while the truncated
      difference is below two — one slow round trip computes two and writes the row, red against
      correct code. Size the window several ticks above the noise, and pin the comparison
      (`>=` versus `>`) in the artifact, because a sleep of exactly the window depends on it.

## E. Harness mechanics that silently do nothing

- [ ] `git add -N <paths> && git diff` — a pathspec matching nothing exits 128 having added nothing, and
      `&&` then suppresses the diff entirely. Reviewers approve emptiness. Use
      `git add -N . ; git diff HEAD -- <paths>`.
- [ ] `git diff HEAD` does not show untracked files — and a new phase is mostly new files.
- [ ] `git diff --stat` likewise shows nothing for untracked files; a file-list tripwire needs the
      `git add -N .` in front of it.
- [ ] **A lister that exits 0 whether or not it printed cannot gate anything.** `gofmt -l X && …` never
      fires, and other formatters' list modes have the same shape. Use `test -z "$(...)"` or the tool's
      real check mode.
- [ ] A diff path list that omits the composition root, the dependency manifest or the lockfile hides the
      wiring and the dependency changes from the final review.
- [ ] A reviewer's tripwire must not flag the phase's own required output as a violation — list the
      expected new files, and see F for the same trap on MODIFIED ones.
- [ ] **A status-listing command that collapses untracked directories prints the same line in the good
      and the broken state** (`git status --porcelain` shows `?? dir/` whether the ignore rule works or
      is missing entirely; `-uall` is what makes it discriminate).
- [ ] **A file list taken from the version-control index cannot see what the run just created.** A
      format or lint bar built from `git ls-files` passes over every new untracked file; give the tool
      the directories instead. The same trap inverted: a tripwire built from the index AFTER an
      intent-add reports the run's own new files as forbidden modifications — derive that list from the
      committed tree (`git ls-tree HEAD`) and filter to modifications and deletions.
- [ ] **A second toolchain's dependency directory inside the module makes language-wide bars walk
      foreign sources.** `./...`, `gofmt -l .` and their equivalents then compile or rewrite files
      shipped by a package manager, unfixably by any agent — scope every bar to the project's own
      source roots, in the prompts AND in the build file, or the two disagree about what green means.
- [ ] **A generated-artifact placeholder that the build tool deletes must be restored by EVERY bar that
      builds**, not only by the one target that was written to do it. One build tool empties its output
      directory on every run; six later prompts ran the raw build, so the run would have ended with the
      placeholder gone and a fresh clone unable to compile.
- [ ] **The container's environment file may be pinned by the orchestration file, not selectable by
      flag.** A CLI `--env-file` that only feeds interpolation looks like isolation and is not: two
      scripts then share one env file and must be serialised instead.
- [ ] A placeholder like `<the section paths above>` inside a fenced command block gets pasted verbatim
      into a shell.
- [ ] Concurrent agents running a script that tears down shared state (bringing containers down with
      volumes, rewriting an env file) manufacture phantom failures. Exactly one agent per round may run it.
- [ ] **A run in flight and a person working the same tree are two writers.** Every reviewer's diff is
      taken against HEAD, so a commit landing mid-run makes the remaining reviewers diff an empty tree and
      report a clean section they never saw; a fix agent then "finds nothing to fix" because someone
      already fixed it by hand. Nothing breaks loudly — the run just reports success for work it did not
      audit. While a run is live, leave the tree alone: no commits, no manual fixes. If a phase must be
      finished by hand because the run died, stop the run first rather than racing it.
- [ ] **A prompt that quotes the TARGET language's variable syntax gets interpolated by the
      ORCHESTRATION language.** A shell `${VAR}` written into a JS template literal is a JS
      reference, not the text you meant: it parses clean, passes the linter, and throws
      ReferenceError at launch with zero agents spawned. Escape it — and note that no static
      checker can catch this, because the reference is syntactically valid.
- [ ] **Dry-run the script with stubbed hooks BEFORE the freeze (and again before launching):
      `wfdry <script>`.** A dry-run defect found after freezing invalidates a freeze, and the repair
      is `wf freeze -refreeze`, which republishes the round under a MOVED tag and records that it did
      — reviewers handed the old tag read different bytes. Running the dry-run first costs nothing and
      leaves nothing to republish; the round it once cost was a defect no reviewer ever saw. It must
      complete EVERY one of the seven modes — `clean`, `blockers`, `acceptfail`, `unfixed`,
      `throw-serial`, `dead-reviews`, `no-args` — not the three an earlier version of this entry
      prescribed by hand. The last one runs the script with no `args`; the other six hand it a
      representative object, which `--args <json>` replaces. Killing named agents is a flag,
      `--dead <label>[,<label>…]`, applied inside every mode, not a mode of its own. A mode that
      completes having spawned NO agent FAILS: every check in the runner is then a guard over the
      empty set — **and ONE is not zero.** A mode that reaches a small fraction of the agents the
      `clean` mode reaches proved that fraction and printed OK over the rest, so a mode under HALF
      the happy path's agent count now FAILS, naming both counts, and every mode that COMPLETES
      prints the happy path's count beside its own — a mode that threw or
      reported violations prints its own detail instead. Measured: a red-bar abort added to the FIRST
      producer took five of seven modes from 15/25/25/25/1/15/25 agents to 18/1/1/1/1/1/1 — six modes
      then at one agent, `throw-serial` included, which is one by construction — with the fix round,
      the recheck, the acceptance step and the final verdict exercised by nothing, and a reviewer
      banking "wfdry 7 of 7" WOULD have credited a layer that covered a single agent. Failing rather
      than warning was measured too: across the three correct gated scripts on disk the failure modes
      run WIDER than the happy path (36 against 44, 18 against 30, 15 against 25), so none of them
      trips it. `throw-serial` is exempt, and so is `no-args`, which the runner narrows on purpose.
      The RATIO is the signal, not the absolute. **And a mode that never reached one of the happy
      path's PHASES fails too, naming the phase.** The two are additive because they miss opposite
      shapes: an abort that narrows to one agent inside ONE phase is invisible to a phase check, and
      a branch that skips a whole section while keeping its agent count within half the happy path's
      is invisible to the ratio — measured on such a shape, 13 agents against 15 and 4 phases against
      5, so the ratio passed while neither the fifth section nor its gate ran in any mode. The
      comparison is over distinct phase TITLES, never their count, because a mode running one phase
      twice reaches the same count having covered less; the threshold is exact rather than a
      tolerance, because across the six correct scripts on disk every mode but `throw-serial` reaches
      its baseline's phase set exactly and a tolerance would be calibrated on a population with no
      variance in it. Seconds of cost, and it evaluates every
      template literal on every path, catching the class above plus any unreachable binding a linter
      cannot type-check. A parse is not an execution. The clean path is the one that matters most:
      reviewers read the failure branches, because that is where the risk looks like it lives.
      **The stub must not leave in scope anything only the real harness provides.** A dry runner
      that kept the script's `meta` binding alive ran six modes × six rounds green over a line the
      real harness makes unexecutable (it parses `meta` OUT before the body runs); the launch died
      in 29 ms with zero agents. Anything a script touches that only the harness provides must be
      verified against the real harness, not against the runner that stubs it.
- [ ] A repo-wide format/build/test bar in a PARALLEL phase fails for reasons an agent cannot fix, and
      pushes it to "repair" a sibling's half-written file. Scope the bar to what each agent owns; run the
      repo-wide one once, at the end.

- [ ] **A guard that compares the RAW request path is bypassed by encoding when the router
      decodes before matching.** Percent-encode one letter of a forbidden prefix and the string
      check passes while the router still dispatches to the forbidden handler — with any
      middleware that guard stood in for. Match against the router's own patterns instead of a
      string, so the guard and the dispatcher cannot disagree.
- [ ] **A patch script that writes each file as it finishes leaves a half-applied state.**
      Validate every replacement across every file in memory first, then write — otherwise a
      failure in file two leaves file one patched and the script no longer re-runnable.

- [ ] **Do not background a long-running command inside a harness that already backgrounds it.**
      `nohup … &` under a runner that tracks the foreground process makes the tracked process exit
      immediately; the group is then killed and the real work dies MID-RUN with a success exit
      code and a truncated output file. Short runs finish first and look healthy, which is what
      makes the pattern survive: four long reviews were written off as a broken external tool
      before the invocation was the cause.

- [ ] **Run every command an artifact prescribes, in the form an agent will run it — including as
      SEPARATE tool calls, because shell state does not persist between them.** Two rounds of one
      gate were spent on a snapshot command that could not run at all (`mktemp` CREATES a zero-byte
      file and git rejects it as an index) and then on the same command split across calls, where an
      unset variable made it print git's EMPTY tree — a plausible 40-hex sha that every guard
      accepted. Neither was visible by reading; both fell out in seconds of executing.
- [ ] **Scoping a diff by FILE does not isolate one phase's work when nothing is committed.** Against
      a pre-run base, a narrowed pathspec still shows every phase's hunks in those files. What
      isolates a phase is a BASE — a tree object recorded partway through the run captures the whole
      working tree, untracked files included, without moving HEAD or touching the index.

- [ ] **`git add -N .` mutates an index every concurrent agent shares.** Two reviewers in one barrier,
      or five walkers in one phase, race on the index lock; one fails and its following diff is blind
      to exactly the untracked files the command was added to reveal. Read-only alternatives cost
      nothing: the diff of tracked files, plus a listing of untracked ones, as two commands.
- [ ] **A pathspec interpolated into a shell command is quoted for the SHELL, not for the reader.**
      Double quotes let the shell expand anything a filename legitimately contains — a `$` in a
      route-file naming convention, a backtick, a `!` under some shells — so the command names a path
      that does not exist and the agent reads an empty diff it cannot tell from a clean section. Single
      quotes with the embedded-quote escape, or a literal the tool never re-parses.

## F. Guard integrity

The regression guard is the thing most worth attacking, because a vacuous guard looks exactly like a
passing one.

- [ ] **A blanket prohibition does not forbid the mechanism another rule depends on.** Second instance of
      this shape, so it is a pattern and not an accident: the first was a naming rule whose violation
      blinded the static checker that enforces every other rule; the second was a shared "never run any
      of these commands" block that forbade the exact invocation the review step's own diff command
      requires, so a reviewer obeying it literally would inspect an empty diff and approve emptiness.
      Both were written to protect the run and both disabled a guard instead. Before shipping any
      shared "never do X" text: grep the whole artifact for X, and check that nothing the artifact
      itself instructs an agent to do is an instance of it.

- [ ] **The guard actually compares against the previous state.** Two runs of the *current* build agreeing
      proves determinism within one build; it cannot see a change that shifted every output consistently,
      which is the failure the rule exists for. Capture the baseline from the unmodified tree BEFORE the
      phase edits anything; that moment does not come back.
- [ ] **The guard must sit DOWNSTREAM of what the phase edits.** One phase's stated invariant was guarded
      by a hash of a generator's own output — upstream of every seam the phase touched, in a module no
      agent owned. It matches no matter what the phase does. Ask: **which line of this phase's own diff
      can make this guard fail?** If the answer is none, the guard belongs to an earlier phase, and the
      real one is usually the existing test corpus of the module being edited.
- [ ] **The guard is write-once, and every agent that could rewrite it is told so** — including the
      end-of-run fix agent, which owns every file. Regenerating a baseline keeps the assertion and
      destroys its meaning, which is worse than deleting it because it reads as a pass.
- [ ] **A phase that MANDATES editing a guarded file must authorise that edit by name, in EVERY place the
      tripwire is stated.** Otherwise the phase's required output is reported as the guard being
      dismantled, on a fully correct run — and the end-of-run fix agent, told the guard outranks
      everything, reverts a change the tree cannot build without.
- [ ] **The check for tampering is possible.** If the run commits nothing, version control cannot tell
      you; timestamps can, but only against files the run actually edits — comparing against files the
      phase is forbidden to touch fails 100% of the time and burns a fix round on a phantom.
- [ ] **A guard read through the index cannot see a worktree deletion.** "Is the placeholder still
      there" is a filesystem question; asking version control answers "yes" long after the build
      deleted it.
- [ ] **A checker whose own inventory comes up empty prints zero findings and exits clean.** Measured:
      a static checker extracted its population by matching a list of known call names; the script used
      a fourth name, the checker found one call site out of nineteen, every check downstream of that
      inventory passed over an empty set, and it reported zero errors — which is what a clean script
      also looks like, and is trusted more because a tool said it. Any tool that extracts a population
      must cross-check it against a SECOND, independent count of the same thing and fail on divergence.
      The same applies to the tool's own inputs: a checker reading the FIRST match of a repeated key
      (a nested `required`, a nested option block) has been checking the wrong one silently.
- [ ] **A harness that signals by THROWING into code required to catch throws has no signal.** A dry
      runner asserted its preconditions by raising; the script under test wraps every spawn precisely
      to turn a raise into a null, so each assertion was caught, recorded as a dead agent, and the run
      reported OK — three injected defects, three green runs. Any checker embedded in the code it
      checks must report out of band, and fail on its own record rather than on the host's reaction.
- [ ] **A floor must be ARMED ON A DIFFERENT FIELD than the one it constrains**, or it disables itself
      at exactly the degenerate value the defect produces. This is the corollary of asking whether
      evidence of FAILURE is present rather than whether evidence of success is absent, and one gate
      hit it four times in ONE script over three rounds — r19, r21 and r22 twice:
      `seam.allowlistCount !== 40` seeds a blocker — an integer has no "not answered yet" value, so it
      fired on every dry fixture and made the clean mode unsatisfiable; `clauseNumbers.size > 1 &&
      size < 8` — eight rows all numbered `1` give size 1, not `> 1`, so the check switched itself off
      at the defect; `enumSets.size !== 4` — four distinct STRINGS is not the four REQUIRED names, and
      a bogus fourth stood in for a missing real one; `expected.length !== 6` — counts slots, not
      members, and six copies of one name passed. The fix each time had one shape: arm on a field the
      report cannot fake by repetition (a two-value enum, a decided-row count), then demand the
      identity in full. **An integer compared to a constant is the single easiest way to write a
      verdict no honest fixture can satisfy.**
- [ ] **A guard over a REPORT asks "is this what the document NAMES", never "does this look like
      enough".** For every "at least N" there is an N: a distinct-member count, a fixed cardinality
      and a non-empty array are all satisfied by the next degenerate report, which is why one gate
      spent three consecutive rounds at one blocker each rewriting a single enumeration floor. Name the
      members, then demand them by name; the property-of-the-report forms are what you write when the
      document has not decided what the set IS.
- [ ] **The guard's population is pinned by number**, so silently narrowing it is visible.
- [ ] **Everything the result depends on is pinned**, not just the obvious half: the request shape feeds a
      seeded generator as much as its options do.

- [ ] **A guard over a hand-maintained sidecar list must derive its population from the CANONICAL
      list, not assert the sidecar's own contents.** "Every key resolves", "the set equals these
      N", "the size is N" are all still green when a new entry is added to the canonical list and
      forgotten in the sidecar — which is the omission the guard exists to catch. Enumerate the
      canonical population and demand each member be either handled or in a named exclusion list.
      Then COUNT the exclusions before shipping the rule: one draft implied five and needed
      nineteen, which would have been red before a line of the feature was written.

- [ ] **Check a new guard against the SPECIFIC wrong value the step it guards can produce, and
      against the values nearest to hand.** Measured twice on one gate: two verification commands
      added to protect a recorded sha both passed on the empty tree, which is exactly what the broken
      form of that step emits; and the fixed set of three still passed on HEAD's own tree and HEAD's
      commit sha — the values an agent is likeliest to substitute, since everything else in the run
      diffs against them. A guard that admits the nearest wrong answer is decoration.
- [ ] **A briefing document that every agent reads can bypass a per-agent parameter at the point of
      use.** Three rounds built a per-reviewer diff base; the shared document still said, in bold,
      to diff against the default one, and the shared text is what an agent follows. When a parameter
      becomes per-agent, grep the shared material for the old constant.

- [ ] **A completeness check over a population an agent REPORTS belongs in the schema, not in a loop.**
      As an array the schema is satisfied by one entry naming anything, so the only place left to check
      the population is code that can log and cannot make the model answer again — and a hard check
      there fails every fixture, so the clean dry-run mode goes red and the check gets softened to a
      warning. Measured: a green run over twenty-six "not reported" warnings, two of them the one bar
      proving the feature was wired in at all. Make the population the REQUIRED KEYS of an object
      (the commands a step must run, the numbered items it must answer, the findings it must
      re-judge): the fixture fills every key so the clean path stays green, and a real omission is
      refused where a report can still be sent back.

## G. Failure handling in the script itself

- [ ] **A dead agent and a clean result are not the same value.** Findings `[]` from two dead reviewers
      reads as "reviewed, nothing found" in the return object. Carry a `dead`/`unreviewed` list into the
      RETURN value, not only into `log()` — and do the same for dead PRODUCERS, whose absence otherwise
      shows up only as a slightly shorter file list.
- [ ] **A report field that asks for pasted command output is a death trap; make it a LIST of short
      lines.** Measured three times across two phases: an agent finishes its work correctly, emits one
      multi-kilobyte string field holding a build log, the report fails to parse as JSON five times,
      and the agent is recorded dead — its files survive on disk, its contracts do not, and every later
      agent loses the seam it was supposed to publish. Prose caps ("keep it under 2000 characters")
      shrink the payload without fixing it. Ask for one short line per command ("format check → empty",
      "tests → ok, 21 packages"), and put a cheap check after any agent whose output the rest of the
      run is written against, so a lost report is survivable rather than silent.
- [ ] **The spawn call can THROW, not only return null.** A subagent that fails to emit a valid
      structured report N times makes the call raise, and an unhandled raise at any SERIAL step aborts the
      entire run — losing the orchestration of every finished phase while their work sits on disk. Wrap
      every serial spawn so a death becomes the null value the rest of the script already handles; calls
      inside a parallel barrier are usually already null-on-throw, the serial ones are not.
- [ ] **A schema's `required` set is a failure surface, not documentation.** Every required field is one
      more thing the agent must produce or die retrying. Require what the run genuinely cannot proceed
      without; let the rest be optional and test for absence explicitly. A field required of a schema
      shared by two agents must be answerable by BOTH.
- [ ] **A gate that never had to run is not an unverified gate.** The accounting that records "this
      gate had dead reviewers" must be guarded on the branch that would have NEEDED the reviewer;
      called unconditionally, it reports a dead reviewer that was never spawned — and it does so on
      the CLEAN path, which is the path nobody exercises, because every reviewer reads the failure
      branches where the risk looks like it lives.
- [ ] **Blockers survive a dead fix agent** and a dead re-review agent.
- [ ] **A dead kernel stops the run.** Otherwise every later agent works against a tree that cannot build
      and the return value still looks structurally normal.
- [ ] **Parsing a verifier's per-finding verdicts cannot lose findings**; drifted numbering carries the
      whole list forward.
- [ ] **A dead fix agent is logged**, or the verifier is told "a fix agent just addressed these" about an
      untouched tree.
- [ ] **Agents die independently: never read one's fields inside a check on another.** A tidy-up that
      moved a log loop closed one `if` early and left two guard checks dereferencing the other agent's
      result — a crash exactly when one survived and the other did not.
- [ ] **A summary field computed from a mid-run agent is a stale snapshot.** One run returned "criterion
      not proven" for a criterion that passed: the flag came from the acceptance agent, which ran BEFORE
      the fix round that closed the failures it had reported. Compute end-of-run verdicts from the LAST
      step that observed the tree, or name the field for the moment it describes.
- [ ] **The run journal and the gate's reviewer outputs are the only record that the gate happened.**
      Keep them past the run: without them the next process change is argued from memory, and a
      recollected incident cannot be checked against the artifacts — which is how a rule gets written
      for a defect the tree can prove never existed.
- [ ] **The no-findings path still ends on an observation.** If "nothing actionable" skips fix AND verify,
      the run's verdict is the self-report of the agent that wrote and graded its own acceptance test.

- [ ] **Classify EVERY term of the final verdict as a fact about the RUN or about the TREE, in the
      artifact, before fixing any one of them.** A run fact accumulates honestly — an agent that died
      cannot be un-died. A tree fact must be re-derived after the last mutation, or a defect a later
      step actually fixed keeps the verdict red, and a regression the last step introduced keeps it
      green. Measured: four consecutive rounds on one rule, each closing the variable that round
      happened to name, including a round whose own finding text said "one fix must cover both" where
      there were three.

## H. Cost

What one full implementation phase costs, measured end to end, as an anchor for proportionality: **18
agents, ~3.25 M subagent tokens, ~1220 tool calls, ~4 hours wall clock** for the run itself — plus the
gates before it, ~10 reviewer agents across 9 rounds (6 on the decisions, 3 on the script). The gates
are a small fraction of the run and they removed 47 blockers before a single production line was
written; that ratio is the argument for gating, and it is why the round COUNT is worth budgeting.

The phase after it gated harder and is the upper anchor: **fourteen rounds, ~21 reviewer runs, 109
blockers** — 94 from the decision gate (7 rounds) and 15 from the script gate (7 rounds), against six
revisions of the script, six of its context document, seven of the decision document and eight frozen
manifests. Those nineteen revisions and eight manifests were tracked by hand — copies under `.rN`
names, `diff -u` between them, a `sha256sum -c` per artifact before launch — which is the bookkeeping
`wf` now owns; the numbers stay here because they are what a fully-gated phase COSTS, not what its
bookkeeping cost. Read the two anchors together rather than averaging them: the decision half of the second
was the productive half and would have been cheap at twice the price, while its script half spent five
of its seven rounds on a defect class a search finds in a second (see the round-discipline note in the
guide). Gate the decisions to exhaustion; sweep the script mechanically before opening another round on
it.

- [ ] No agent's scope is "the whole repo"; every producer has a named file list.
- [ ] No blob over ~10–20 KB is inlined into a fan-out.
- [ ] A tripwire scope does not force N reviewers to re-read the largest diff in the run; downgrade it to
      a file-list check.
- [ ] Reading duplication is bounded: "read the whole skeleton and six references" × 9 agents was ~590 KB
      of re-reading on one phase.

## I. Fixing a finding

The fix is the largest single source of blockers. These are protocol, not taste.

- [ ] **Apply a round's fixes at a barrier**, never between two reviewers of the same round. The pull is
      strongest when one vector returns early and its findings look obviously correct: measured, five
      such fixes went in while two other vectors were still reading, and one then reported two blockers
      as open against a file where they were already closed.
- [ ] **Starting a script from a gated one copies its UNFOUND defects, so re-read that gate's ROUNDS
      FILE, not its code.** Measured: an A2 script built from a shipped A1 script inherited its harness
      verbatim, including a reviewer guard that tested `findings.length` instead of "does any finding
      carry severity blocker" — so a reviewer returning `verdict: "blockers"` with advisory-only
      findings read as CLEAN. That defect had survived A1's own seven-round gate and is still in the
      script that ran. Copying the code also reproduced A1's FIRST blocker as A2's first blocker, an
      ownership hole, which the rounds file would have handed over for free. (`wf check dup` flags the
      copied harness and is right to; the harness being deliberate is not the finding.)
- [ ] **Execute the procedure you wrote, literally, once**, in a scratch copy and never in the live
      one. Reading checks whether the steps are right; only running checks whether they are reachable
      in the order given. One round found three blockers this way that no reading had found, and the
      two below are two of them.
- [ ] **Never default a diff base to "the newest" state.** Point it at the last state a reviewer
      actually read. Where the procedure freezes the current round before briefing anyone, "the newest"
      IS the state under review, so the diff is against itself — and an empty diff is indistinguishable
      from a round that changed nothing.
- [ ] **Give every exception to a rule its own command, or drop the rule.** "Never edit by hand" had
      two gaps — the first write of a file, and filling the round's own findings form — and both were
      papered over with the very hand edit the rule forbids, by the author of the rule, who did not
      notice. A rule an operator steps outside of to do ordinary work is one they stop reaching for.
- [ ] **A fix that REPLACES a rule must replace every copy.** Grep the whole artifact set for the old
      wording before calling it fixed. Two consecutive rounds each found the superseded sentence sitting
      a few lines under its own replacement, in a different consumer — and an agent follows whichever it
      reads last. **Declare the occurrence count per edit** (`wf apply`'s `count`, which counts inside
      ONE file): it catches a rule you believed lived once in an artifact and that lives there twice. A
      copy in a SECOND artifact needs its own edit in the same batch — the count cannot see across
      files, and declaring a cross-file total only rejects the batch. All-or-nothing is what then makes
      both copies move together or neither move at all.
- [ ] **Half of that class IS mechanizable, and the half that is not is the dangerous half.** An earlier
      note here said it was not, on the evidence of a near-duplicate detector that found nothing. That
      was true of the detector, not of the class. Split it: copies that DRIFTED — the same sentence
      reworded in one artifact and not the other — are found by near-duplicate matching over normalised
      text, provided the tool also demands that the two copies were last edited in different commits
      (measured on one phase's artifacts: 2065 sentences, 176 cross-file groups, 34 flagged, and they
      were real drifts). Copies that CONTRADICT — two differently-worded sentences asserting opposite
      rules — share no wording and no similarity metric reaches them; they stay a reviewer's find
      UNLESS the author marked the rule: `[RN]` markers on every site of one rule let `wf check rules`
      demand the sites normalise identically, which is the only mechanical reach into that class
      (measured need: eight such contradictions on one live gate, all found by reviewers). Mark only
      a site that STATES the rule — a sentence referring to one ("because of R2", written with the
      marker) states nothing
      and disagrees with every statement of it; read the `compared` count, because ids that each sit
      at a single site are a check that reached nothing and still printed zero conflicts. **A `.mjs`
      artifact carries markers too**: it is compared as PROSE — its comments and the literal chunks of
      its strings, never its code — so an orchestration script's prompt restating a decision is a site
      like any other, and marking it together with the document's statement is the only mechanical
      comparison of what the two copies SAY (`dup` reports the pair as `sim 1.00` for a whole gate and
      correctly declines to call it a defect: a script restating a decision IS the design). Measured on
      mocker A3: six of round 10's twelve blockers were one decision that moved in the document during
      round 9 and did not move in the prompt restating it, and the dearest of them carried, verbatim,
      the document's own sentence warning that this half read alone is wrong. **And a marked
      site can DECAY into a pointer without the marker moving.** Measured: a fix that cut a section left
      one of the two sites of three rules reading "every one of the five routes of R1" — the marker
      still there, the enumeration it named deleted from the document — and `wf check rules` printed
      `5 compared, 0 conflict(s)` over the broken artifact both before and after, because it compares
      sites carrying an id and cannot tell a statement from a pointer to one. After any fix that DELETES
      text, re-read the marked sites: the check cannot. **These documents mark their own stop rule with
      the id R1**, at every site that STATES it here and in `pre-flight-gate.md`, because it lived at
      five sites in four wordings — one of them lower-cased, which the case-erasing normaliser cannot
      see as a difference and no check then compares — and the "reviewer rounds only" half was missing
      from the canonical statement. Neither this sentence nor the quotation above carries the marker in
      its bracket form: both POINT at the rule, and a marked pointer is the defect this bullet is about.
      One of the five sites is a Go comment (`internal/ws/findings.go`) that `wf check rules` can never
      reach, because it compares ARTIFACTS — it is kept in the same wording by hand. So sweep
      mechanically for drift, mark the rules that repeat, and keep a human vector for the unmarked
      contradictions. **And read what the marked check does NOT ask:** it compares the sites of one id
      against each other, never a rule against the section that argues for it — on one gate both copies
      of a rule agreed perfectly while both had gone FALSE against the four bullets beneath them,
      because the fix rewrote the argument and not the rule, and `5 compared, 0 conflict(s)` was read
      as a statement about correctness. That class is `wf check stale -since <ref>`, which mines the
      OLD text of the fixes themselves and reports every untouched sentence still using the vocabulary
      a fix retired. On a DERIVED artifact narrow the drift sweep with `-since gate-rN` — a context
      document restating the decisions satisfies both DIVERGENT conditions by construction (21-25
      flagged groups per one real contradiction, measured) — and do not let either green report stand
      in for the human pass.
- [ ] **A closure names a commit; check that the commit touched what the finding NAMES.** That a sha
      exists is what `wf round record` proves, and it is the smaller half: on one live gate two
      reviewer lenses asked the larger one by hand-written prompt and reported 6 and 4 of 24 closures
      not closed, three rounds running — the fix had corrected one copy of a rule and left the copy the
      finding cited standing. `wf check closure` intersects each claimed commit's changed ranges with
      the SECTION the finding cites — `D9`, `R12`, the form a ledger actually uses, since 126 of those
      127 named no path at all — and with the artifact and line where it names them, and counts the findings that name no path at all,
      because the checked share must be visible rather than assumed. A FILE the commit never opened is
      a closure that did not land; a LINE it did not cover is a question to re-ask, since a later batch
      in the same round shifts the numbers.
- [ ] **Verify a batch of edits by SEARCHING for each one, not by the absence of an error.** A patch
      script that validates as it goes reports only the edit that FAILED and silently discards the ones
      that succeeded before it, because the write happens at the end — so a fix is reported applied,
      never reached the file, and the next review round re-files the identical blocker. A partially
      applied batch is worse than a rejected one, because a rejected one announces itself. `wf apply`
      is this rule made mandatory: it validates every replacement against the evolving buffer first,
      writes nothing if one misses, then re-reads every written file and compares it BYTE FOR BYTE with
      the buffer it planned. (It searched for each change once, which verified every pure DELETION
      vacuously — `strings.Count(body, "")` is never zero — and rejected chained edits that had
      landed perfectly.)
      A hand edit followed by `git commit` gives up both halves.
      (Measured: one whole gate round spent re-finding a defect whose fix had thrown before the write.)
- [ ] **The same rule living in two artifacts with different quoting is a seam.** When one prose rule
      appears both in a companion document and inside the orchestration script's own string literals,
      the two copies need DIFFERENT escaping — so a sentence moved between them by copy-paste stops
      matching any search written for it, and the copy left unchanged is the one an agent reads. Search
      for the rule's distinctive words, never for its punctuation.
- [ ] **Read the block around every edit after making it**, not just the edited line. Three self-inflicted
      defects in one round were structural: a brace closed early, a field added to a schema's `required`
      without a matching instruction, a rule tightened in one consumer of six.
- [ ] **Record the round's findings, re-run `wfcheck` and the JS linter, open the next round, and
      re-freeze.** The
      findings go into the closing round's own `rounds/rN/findings.md`, via `wf round record <file>`,
      BEFORE the fixes are applied — that file is what `wf delta` puts in front of the next reviewer,
      and the scaffold `wf round new` writes is blank. An open blocker is legal at record time; a
      `closed by` naming a commit that does not exist is not — the record rejects before writing.
      After the fixes, fill the `closed by` column with the apply commit and re-record (`-round N`):
      `wf freeze` reads the findings of the round being CLOSED (the frozen round's own file is always
      a blank scaffold) and refuses while a BLOCKER stands without a closure; a prose closure passes
      with a "verify by hand" note. It also refuses when that round's ledger is the UNTOUCHED scaffold
      and no lens was banked for it — and it checks EVERY closed round, not only the previous one,
      because `wf round new` does not require the current round to be frozen and a round opened past
      without a freeze was examined by nothing. A round nobody is recorded as having read must not
      freeze as a round that found nothing: bank the vectors with `wf round lens <prompt> -name N -asks Q -model
      M -round N`, which is also the record that makes the round's reviewers part of the corpus, and
      which counts only where it says which vector, what ran it and what it was asked. A `-kind tool`
      round is exempt: its opening subject already records that no reviewer was dispatched. Then `wf round new`, then `wf freeze` — which
      refuses while any ARTIFACT is uncommitted and refuses, by default, to re-freeze a round that is
      already tagged (`-refreeze` republishes it, moves the tag and records the supersession; it is
      what the citation notes below are for), and records one manifest over the whole artifact set plus the reviewed repository's HEAD
      and the `wf` version that wrote it. `wfdry` runs BEFORE that freeze — a dry-run defect found
      after it forces a round no reviewer saw — then `wf verify` before launching, so that all three
      copies of this loop name the same steps.
      **A clean working tree is NOT the check** that what will run is what was reviewed: edits are
      committed as they are applied, so a post-freeze edit leaves the tree exactly as clean as none at
      all. `wf verify` is the check. The linter caught an unescaped apostrophe inside a quoted string
      that would have failed the run at launch, for free.
- [ ] **Mark fix-induced findings `[fix-induced]` in the ledger.** The share of a round's findings
      introduced by the previous round's own fix describes what THIS round found; it is a continue
      signal, never a stop signal. Measured on three gates: a 4-of-4 fix-induced round carrying one
      blocker was followed by a round filing seven findings and another blocker, and a 3-of-3 round
      carried a blocker itself. The gate ends on a REVIEWER round that produces no BLOCKER, and on nothing else [R1].
      `wf stats` can only print the share where the author marked it — an unmarked corpus reads as
      "not measured", never as "none".
- [ ] **Budget rounds, not perfection.** Reviewer COUNT descends (3 → 2 → 1); the round count does not cap.
      The gate ends on a REVIEWER round that produces no BLOCKER, and on nothing else [R1]. The last phase needed four.

- [ ] **When a finding names a CLASS, enumerate the class before fixing a member.** "Both of the
      accumulating terms", "every copy of this rule", "each of these prompts" — fixing the members the
      finding happened to list is the same defect one round later, and it reads as closed in the
      ledger. Write the enumeration into the artifact; the next round checks it instead of rediscovering
      it.

---

## Appending to this file

One line per novel blocker, in the section it belongs to, in the imperative, with the concrete failure in
parentheses. If a blocker does not fit a section, add a section — the taxonomy is descriptive, not fixed.

Two rules about what goes here, because this file ships with the tool into every project:

- **State the rule so it survives a change of language, framework and domain.** Strip package names, type
  names and product nouns; keep the shape of the failure. "A template and the concrete value that matched
  it are two keys" travels; the same sentence with two column names from one schema does not.
- **Project-specific facts belong in that project's own HANDOFF**, not here: which module owns what, which
  file is the baseline, what the phase criterion is. This file is the failure taxonomy; the project file
  is the state.

Do not record findings the tooling already catches — `wfcheck` (static defects in the script), `wfdry`
(defects that only appear on a mode a real run can enter), `wf check` (a citation that does not
resolve, a rule whose copies drifted apart, a MARKED rule whose sites contradict, a citation the
target's own movement since the freeze edited or shifted) or the findings-ledger validation in
`wf round record`/`wf freeze` (a closure
naming a commit that does not exist, a blocker still open when its round is closed). Record what none
of them can see. And when a rule here is
superseded because a tool grew to cover it, say which half it now covers rather than deleting the
entry: the half that stays manual is the half that was always going to be missed.
