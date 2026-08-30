export const meta = {
  name: 'SCRIPT_NAME',
  description: 'TODO one line: what this run produces. Shown in the permission dialog.',
  whenToUse: 'TODO: run once, from the repo root, on a clean tree whose HEAD is the previous slice.',
  phases: [
    { title: 'Build', detail: 'TODO one producer per owned file', model: 'sonnet' },
    { title: 'Gate 1', detail: 'two reviewers: correctness, and reuse/simplification — both sonnet, effort high' },
    { title: 'Accept', detail: 'read-only: run every bar and report, never fix', model: 'sonnet' },
    { title: 'Gate 2', detail: 'two reviewers: the closing fix, and completeness against the goal — both sonnet, effort high' },
  ],
}

// ---------------------------------------------------------------------------
// Shared facts. Keep this block SMALL: a shared blob over ~10-20 KB is paid
// once per fan-out agent, so anything larger belongs in a file each agent reads
// a named slice of. Producers are given NAMED FILES, never "the repo".
// ---------------------------------------------------------------------------

const REPO = 'TARGET_REPO'

// A LIST, not a paragraph: a bar nobody ran and a bar nobody reported look the
// same in a prose blob, and the tally below checks that every one of these came
// back by name.
//
// TODO: if these bars are memory-heavy, say so IN the prompt — "run them one at
// a time". A whole-repo test run under a race detector can hold gigabytes, and
// two of them at once is what an OOM killer resolves by wiping the session. Four
// scripts on one target re-derived that sentence by hand; it belongs beside the
// list it constrains, and it is a precondition for fanning any of this out.
const BARS = [
  'TODO make test',
  'TODO make lint',
]
const BARS_TEXT = BARS.map((b) => '  ' + b).join('\n')

const GOAL = 'TODO: what this slice must be true of when it is done. One paragraph, no implementation.'

const MAX_FIX_ROUNDS = 1

// ---------------------------------------------------------------------------
// Harness. Everything below this line is the same in every script; the parts
// above and the sections below are yours.
// ---------------------------------------------------------------------------

// How every agent NAVIGATES the tree. A subagent inherits the launching
// session's own instructions, not only its `effort`: a box whose sessions are
// told to read with `cat`/`sed -n` and search with `grep` hands that habit to
// every agent this script spawns, and neither the script nor its pre-flight
// gate can see it. Measured on one live producer, the largest agent of its run:
// 256 Bash, 64 Edit, 6 Read — and exactly five of the 256 were bars. The other
// 251 were navigation, each a separate round-trip carrying the whole context.
const NAV = 'NAVIGATION\n' +
  'Read files with Read, search with Grep and Glob. Reserve Bash for the bars, for git, and for what genuinely needs a shell: a `cat` or a `sed -n` where Read would do is a round-trip that carries your entire context to fetch one file.\n'

// spawn is the one wrapper every agent() call goes through. An unhandled throw
// at a serial step aborts the whole run, so a failed agent becomes null here
// and the caller decides what a null means. Named `spawn` by convention:
// wfcheck identifies a wrapper structurally, but a reader greps for the name.
async function spawn(prompt, opts) {
  try {
    return await agent(prompt, opts)
  } catch (err) {
    log('agent ' + (opts && opts.label ? opts.label : '?') + ' failed: ' + String(err))
    return null
  }
}

// The liveness ledger. It counts agents that are spawned UNCONDITIONALLY: an
// agent that only runs when something fails must never be able to make a clean
// run report a dead agent, which is how a green run once returned green:false.
const spawned = []
const dead = []
function live(label, result) {
  spawned.push(label)
  if (!result) dead.push(label)
  return result
}

const FINDINGS_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['verdict', 'findings'],
  properties: {
    verdict: { type: 'string', enum: ['pass', 'blockers'], description: 'blockers if at least one finding has severity blocker, otherwise pass' },
    findings: {
      type: 'array',
      items: {
        type: 'object',
        additionalProperties: false,
        required: ['severity', 'file', 'summary', 'failure'],
        properties: {
          severity: { type: 'string', enum: ['blocker', 'major', 'minor', 'note'], description: 'the level is decided by what catches it downstream, never by how bad it reads: blocker = no later gate catches it, and vacuity STARTS here — a finding that makes a guard or a tripwire vacuous is classified blocker and rides only where you can name the gate that catches it; major = a real defect and you NAME the later gate that catches it; minor = a real defect cheaper to leave standing than to loop for; note = not a defect' },
          file: { type: 'string', description: 'repo-relative path, with :line when known' },
          summary: { type: 'string', description: 'one sentence stating the defect' },
          failure: { type: 'string', description: 'concrete inputs or state leading to the wrong outcome, not a restatement of the summary' },
          fix: { type: 'string', description: 'one line naming the smallest correct fix' },
        },
      },
    },
  },
}

const BAR_RESULTS = {
  type: 'array',
  description: 'One entry per bar actually run. A bar that was not run is reported as not-run, never as passing.',
  items: {
    type: 'object',
    additionalProperties: false,
    required: ['command', 'result'],
    properties: {
      command: { type: 'string', description: 'the bar as invoked' },
      result: { type: 'string', enum: ['pass', 'fail', 'not-run'] },
      detail: { type: 'string', description: 'the failing output verbatim when result is fail; the reason when not-run' },
    },
  },
}

// barFailed asks the POSITIVE question. A negative test treats every value it
// does not recognise as a failure, which makes the verdict unsatisfiable by a
// fixture that has not produced a real enum value yet — and a verdict no clean
// run can satisfy is one nobody trusts when it reads false for real. `not-run`
// counts as a failure because a bar nobody ran is not evidence.
function barFailed(bar) {
  return bar.result === 'fail' || bar.result === 'not-run'
}

const PRODUCER_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['summary', 'files', 'bars'],
  properties: {
    summary: { type: 'string', description: 'what you changed, in two sentences' },
    files: { type: 'array', items: { type: 'string' }, description: 'every repo-relative path you wrote' },
    bars: BAR_RESULTS,
  },
}

const FIX_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['perFinding', 'bars'],
  properties: {
    perFinding: {
      type: 'array',
      items: {
        type: 'object',
        additionalProperties: false,
        required: ['summary', 'action', 'why'],
        properties: {
          summary: { type: 'string', description: 'the blocker you were handed' },
          action: { type: 'string', enum: ['fixed', 'refused'] },
          why: { type: 'string', description: 'what you changed, or the file:line that refutes the finding' },
        },
      },
    },
    bars: BAR_RESULTS,
  },
}

// runGate is the per-section review gate: two independent vectors at minimum,
// then one bounded fix round, then a recheck of the FIX rather than of the
// section. A fix is unreviewed code and the largest single source of new
// defects. `reviewers` is a LIST because width is bounded by tokens and never
// by wall-clock: they are read-only, they run concurrently, and a section with
// more than one failure surface carries a vector per surface.
async function runGate(cfg) {
  const reviews = await parallel(cfg.reviewers.map((rev) => async () =>
    live('review:' + cfg.slug + ':' + rev.slug, await spawn(
      'You are reviewing one section of a slice in the repository at ' + REPO + '.\n\n' +
      'SECTION GOAL\n' + cfg.goal + '\n\n' +
      'YOUR REVIEW VECTOR: ' + rev.vector + '\n\n' +
      'WHAT TO READ\n' +
      'HEAD is the commit BEFORE this run started, so the diff against HEAD is exactly what this run has produced. Read that diff first, then open only the files it touches. Do not read the whole repository.\n\n' +
      NAV + '\n' +
      'THE SLICE GOAL\n' + GOAL + '\n\n' +
      'REPORT\n' +
      'Report only defects: no praise, no summary of what the code does, no scope beyond the section goal.\n' +
      'A finding\'s level is decided by what catches it downstream, never by how bad it reads.\n' +
      'blocker = no later gate catches it, so it ships; a finding that makes a guard or a tripwire VACUOUS starts here.\n' +
      'major = a real defect, and your summary NAMES the later gate that catches it; cannot name one means blocker.\n' +
      'minor = a real defect cheaper to leave standing. note = not a defect, asserts no failure.\n' +
      'For each finding give the file, a one-sentence summary, the concrete failure it produces, and a fix.\n' +
      'Set verdict to blockers when any finding is a blocker, otherwise pass. An empty findings array is a legitimate result.',
      { label: 'review:' + cfg.slug + ':' + rev.slug, phase: cfg.phase, schema: FINDINGS_SCHEMA, model: rev.model, effort: 'high' }
    ))
  ))

  // A reviewer that returned verdict `blockers` with no blocker in its findings
  // contradicts itself, and the cheap reading of that is "pass". Fail closed:
  // the disagreement is banked as an open blocker of its own.
  //
  // It records WHICH REVIEWER and WHAT IT SAID, never just the gate. Measured
  // on a real launch whose only red term was `inconsistent: ['gate1']`: finding
  // out which of two reviewers had contradicted itself, and over what, meant
  // parsing `journal.jsonl` by hand and indexing agents positionally — while
  // the guard had the label and the severities in hand at the moment it fired.
  // A run that is already red is exactly the run where a human is trying to
  // find out what happened. Indexed rather than filtered, because
  // `filter(Boolean)` destroys the correspondence with `cfg.reviewers`.
  reviews.forEach((r, i) => {
    if (!r) return
    const label = 'review:' + cfg.slug + ':' + (cfg.reviewers[i] ? cfg.reviewers[i].slug : i)
    const levels = (r.findings || []).map((f) => f.severity)
    const has = levels.includes('blocker')
    if ((r.verdict === 'blockers') !== has) {
      log(label + ': a review contradicted itself — verdict ' + r.verdict + ' with ' + (has ? 'a' : 'no') +
        ' blocker finding; it filed [' + (levels.join(', ') || 'nothing') + ']')
      inconsistent.push({ label, verdict: r.verdict, levels })
    }
  })

  const all = reviews.filter(Boolean).flatMap((r) => r.findings || [])
  const blockers = all.filter((f) => f.severity === 'blocker')
  let advisories = all.filter((f) => f.severity !== 'blocker')
  if (advisories.length) log(cfg.slug + ': ' + advisories.length + ' finding(s) recorded that do not loop this section (major/minor/note)')
  if (!blockers.length) {
    log(cfg.slug + ': gate clean')
    return { open: [], advisories }
  }

  // The fix round is SKIPPED, not attempted, when the turn's token target is
  // nearly spent: a fix that cannot then be re-reviewed is worse than an open
  // blocker reported honestly.
  if (budget.total && budget.remaining() < 60000) {
    log(cfg.slug + ': ' + blockers.length + ' blocker(s), fix round SKIPPED — budget exhausted')
    return { open: blockers, advisories }
  }

  let open = blockers
  for (let round = 0; round < MAX_FIX_ROUNDS; round++) {
    log(cfg.slug + ': fix round ' + (round + 1) + ' over ' + open.length + ' blocker(s)')
    const fixed = await spawn(
      'You are fixing blocker findings from a review of the repository at ' + REPO + '.\n\n' +
      'SECTION GOAL\n' + cfg.goal + '\n\n' +
      'BLOCKERS TO FIX\n' + JSON.stringify(open, null, 2) + '\n\n' +
      'RULES\n' +
      '- Fix exactly these findings. Do not refactor anything else, do not rename, do not reformat untouched code.\n' +
      '- If a finding is WRONG about the code, do not "fix" it: leave the code alone and say so, with the file:line that refutes it.\n' +
      '- Every bar below must be green when you are done. Run them.\n' + BARS_TEXT + '\n\n' +
      'YOU OWN\n' + cfg.owns.map((p) => '  ' + p).join('\n') + '\n\n' +
      NAV + '\n' +
      'RETURN\n' +
      '- perFinding: one entry per blocker you were handed, each with its summary, an action of fixed or refused, and a why.\n' +
      '- bars: every bar you ran, with its command and result, and the detail when it failed or was not run.\n',
      { label: 'fix:' + cfg.slug, phase: cfg.phase, schema: FIX_SCHEMA, model: 'sonnet', effort: 'high' }
    )
    if (!fixed) {
      log(cfg.slug + ': fix agent died — blockers stay open')
      break
    }
    bars('fix:' + cfg.slug, fixed.bars)
    const recheck = await spawn(
      'You are re-reviewing ONE fix in the repository at ' + REPO + '.\n\n' +
      'The fix claims to close these blockers:\n' + JSON.stringify(open, null, 2) + '\n\n' +
      'The fix agent reported:\n' + JSON.stringify(fixed, null, 2) + '\n\n' +
      'A finding the fix agent REFUSED is not automatically closed and not automatically wrong: judge the refusal against the code the same way you judge a fix.\n\n' +
      'Read the diff against HEAD and judge ONLY whether each blocker is actually closed, and whether the fix introduced a new one. Review the DELTA, not the original section.\n\n' +
      NAV + '\n' +
      'Return one finding per blocker still open and one per NEW defect the fix introduced, each with its file, summary, failure and fix, and set verdict accordingly.\n' +
      'If everything is closed and nothing new broke, return an empty findings array and verdict pass.',
      { label: 'recheck:' + cfg.slug, phase: cfg.phase, schema: FINDINGS_SCHEMA, model: 'sonnet', effort: 'high' }
    )
    if (!recheck) break
    open = (recheck.findings || []).filter((f) => f.severity === 'blocker')
    // The re-review is the ONE vector aimed at "did the fix introduce a new
    // one", which is this corpus's largest single source of blockers — so its
    // non-blocking half has to reach the bucket as well. The first version of
    // this fix widened the enum from two words to four and collected only the
    // FIRST review's leftovers, so a `major` filed by the recheck was deleted
    // in the same function the fix was written into.
    advisories = advisories.concat((recheck.findings || []).filter((f) => f.severity !== 'blocker'))
    if (!open.length) {
      log(cfg.slug + ': blockers closed')
      return { open: [], advisories }
    }
  }
  log(cfg.slug + ': ' + open.length + ' blocker(s) STILL OPEN after ' + MAX_FIX_ROUNDS + ' fix round(s)')
  return { open, advisories }
}

const openBlockers = []
const carried = []
const barFailures = []
const inconsistent = []
function bank(section, result) {
  for (const f of result.open) openBlockers.push({ section, ...f })
  // Everything that does NOT loop the section is carried into the result, not
  // thrown away. Under the old two-value enum the only non-blocking word was
  // `advisory`, described in-schema as "worth knowing, does not stop the
  // slice"; the scale's `major` is "a real defect", and dropping the bucket
  // would let a real defect survive a green run as a number in one log line.
  // It does not gate `green` — a major rides into the run by design — it
  // is REPORTED, the way barFailures and inconsistent are.
  for (const f of result.advisories || []) carried.push({ section, ...f })
}

// Every bar failure, from every agent, in ONE place. Logging a producer's red
// bar and then computing `green` from the acceptance step's bars alone is a
// report that knows something it does not say.
function bars(section, reported) {
  for (const bar of reported || []) {
    if (barFailed(bar)) barFailures.push({ section, command: bar.command, result: bar.result })
  }
  // A bar nobody reported is not a bar that passed, and the schema is satisfied
  // by an EMPTY array — so an agent that ran nothing and said nothing looks
  // exactly like one whose bars were green. That is the failure, and it fails.
  if (!(reported || []).length) {
    barFailures.push({ section, command: BARS.join(', '), result: 'not-reported' })
    return
  }
  // Names are only WARNED about. A green run must stay reachable, and no
  // report-shaped fixture can be made to echo these literal strings back —
  // a check nothing can satisfy is a check that gets switched off.
  for (const want of BARS) {
    if (!reported.some((b) => b.command && b.command.includes(want))) {
      log(section + ': no bar reported matching ' + want)
    }
  }
}

// ---------------------------------------------------------------------------
// Section 1 — Build.  TODO: replace with your producers, one per owned file.
//
// A section coordinate into a document this run also edits must come from the
// document's STRUCTURE, not from line numbers: anchor on headings, which move
// with the text they name.
// ---------------------------------------------------------------------------

phase('Build')

const build = live('build', await spawn(
  'You are implementing one section of a slice in the repository at ' + REPO + '.\n\n' +
  'THE SLICE GOAL\n' + GOAL + '\n\n' +
  'TODO: the task, the named files to read for context, and the contract this section must satisfy.\n\n' +
  'YOU OWN\n' +
  '  TODO/path/one.go — what you own in it\n' +
  '  TODO/path/two.go\n\n' +
  'DO NOT\n' +
  '  TODO/path/owned-by-another-section.go\n\n' +
  'BARS — every one must be green before you report done. Run them.\n' + BARS_TEXT + '\n\n' +
  NAV + '\n' +
  'RETURN a summary of what you changed, the files list naming every repo-relative path you wrote, and bars: every bar you ran, with its command and result.',
  { label: 'build', phase: 'Build', schema: PRODUCER_SCHEMA, model: 'sonnet', effort: 'medium' }
))

if (build) bars('build', build.bars)

phase('Gate 1')
bank('build', await runGate({
  slug: 'gate1',
  phase: 'Gate 1',
  goal: 'TODO: what section 1 had to achieve.',
  owns: ['TODO/path/one.go', 'TODO/path/two.go'],
  reviewers: [
    { slug: 'correctness', vector: 'Correctness against the real code: wrong behaviour, missed cases, broken contracts.', model: 'sonnet' },
    { slug: 'reuse', vector: 'Reuse and simplification: duplicated logic, a helper that already exists, needless indirection.', model: 'sonnet' },
  ],
}))

// ---------------------------------------------------------------------------
// Section 2 — Accept.  READ-ONLY on purpose: an agent that can fix what it is
// measuring cannot report that the measurement failed. A run must not end on a
// step that mutates the tree.
// ---------------------------------------------------------------------------

phase('Accept')

const ACCEPT_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['bars', 'goalMet', 'findings'],
  properties: {
    bars: BAR_RESULTS,
    goalMet: { type: 'boolean', description: 'true only when every part of the slice goal is demonstrably satisfied' },
    findings: {
      type: 'array',
      items: {
        type: 'object',
        additionalProperties: false,
        required: ['severity', 'summary'],
        properties: {
          severity: { type: 'string', enum: ['blocker', 'major', 'minor', 'note'] },
          summary: { type: 'string', description: 'one sentence stating what is not satisfied' },
        },
      },
    },
  },
}

const accept = live('accept', await spawn(
  'You are the acceptance step for a slice in the repository at ' + REPO + '. You are READ-ONLY: you do not edit, you do not fix, you do not commit. If something is wrong you report it.\n\n' +
  'THE SLICE GOAL\n' + GOAL + '\n\n' +
  'RUN EVERY BAR and report them in bars, each with its command and result, and the verbatim output when it fails. A bar you did not run is reported as not-run, never as passing.\n' + BARS_TEXT + '\n\n' +
  NAV + '\n' +
  'Then set goalMet to true only if every part of the goal is demonstrably satisfied, and report a finding per part that is not, with its severity and a one-sentence summary.',
  { label: 'accept', phase: 'Accept', schema: ACCEPT_SCHEMA, model: 'sonnet', effort: 'medium' }
))

// TODO: two things this skeleton does NOT do, both measured on real runs.
//
// (1) The step above is ONE agent walking N properties in series. Where those
// properties are independent MUTATIONS — apply the mutation, run the test it
// names, watch the red, revert — they are the most parallelisable work in the
// whole run: measured at 45.4 min of a 407-minute run, and its re-measurement
// another 49.2, so 23% of the run in one serial walk. `isolation: 'worktree'`
// is what makes them safe to run at once. Scope each agent's test command to
// the package its property names FIRST; a fan-out of whole-tree test runs is
// not slow, it is an OOM. See `wf docs authoring`.
//
// (2) A fix landed by a gate AFTER this step invalidates what the step
// measured. A conditional RE-MEASUREMENT re-runs it over the edited tree when
// anything changed; without one, a run reports an acceptance result frozen at
// its pre-fix value — one real launch did exactly that, red over a tree whose
// hole had just been closed. Re-measure the same shape you accept with,
// fan-out included, or the serial tail simply moves.
if (accept) bars('accept', accept.bars)
const acceptBlockers = accept ? (accept.findings || []).filter((f) => f.severity === 'blocker') : []
// The acceptance step files one finding per part of the goal that is NOT
// satisfied. Only its blockers were kept, so a `major` there — a named
// unsatisfied part of the slice goal — went nowhere at all: not into the
// result, not into a log line. `goalMet` still gates green; what was lost is
// what the finding SAID.
for (const f of accept ? (accept.findings || []) : []) {
  if (f.severity !== 'blocker') carried.push({ section: 'accept', ...f })
}

phase('Gate 2')
bank('accept', await runGate({
  slug: 'gate2',
  phase: 'Gate 2',
  goal: 'TODO: the closing question — is the slice complete and is the closing fix correct?',
  owns: ['TODO/path/one.go'],
  reviewers: [
    { slug: 'closing', vector: 'The closing fix: is every blocker actually closed, and did closing it break something?', model: 'sonnet' },
    { slug: 'completeness', vector: 'Completeness against the slice goal: what was silently left out?', model: 'sonnet' },
  ],
}))

// ---------------------------------------------------------------------------
// The tally. green is a CONJUNCTION of everything that can go wrong, and every
// term of it is reported separately so a false one names itself.
// ---------------------------------------------------------------------------

log(spawned.length - dead.length + '/' + spawned.length + ' agent(s) came back')
if (dead.length) log('DEAD: ' + dead.join(', '))
if (openBlockers.length) log('OPEN BLOCKERS: ' + openBlockers.length)
if (barFailures.length) log('BAR FAILURES: ' + barFailures.map((b) => b.section + ' ' + b.command + ' ' + b.result).join('; '))
if (inconsistent.length) log('SELF-CONTRADICTING REVIEWS: ' + inconsistent.map((c) => c.label + ' (verdict ' + c.verdict + ', filed [' + (c.levels.join(', ') || 'nothing') + '])').join('; '))
if (carried.length) log('CARRIED (major/minor/note, do not loop a section): ' + carried.length)

return {
  green:
    dead.length === 0 &&
    openBlockers.length === 0 &&
    barFailures.length === 0 &&
    inconsistent.length === 0 &&
    acceptBlockers.length === 0 &&
    Boolean(accept && accept.goalMet),
  dead,
  openBlockers,
  carried,
  barFailures,
  inconsistent,
  acceptBlockers,
  goalMet: Boolean(accept && accept.goalMet),
}
