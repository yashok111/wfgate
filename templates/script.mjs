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
          severity: { type: 'string', enum: ['blocker', 'advisory'], description: 'blocker = ships a defect or breaks a bar; advisory = worth knowing, does not stop the slice' },
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

// runGate is the per-section dual review: two independent vectors, then one
// bounded fix round, then a recheck of the FIX rather than of the section. A
// fix is unreviewed code and the largest single source of new defects.
async function runGate(cfg) {
  const reviews = await parallel(cfg.reviewers.map((rev) => async () =>
    live('review:' + cfg.slug + ':' + rev.slug, await spawn(
      'You are reviewing one section of a slice in the repository at ' + REPO + '.\n\n' +
      'SECTION GOAL\n' + cfg.goal + '\n\n' +
      'YOUR REVIEW VECTOR: ' + rev.vector + '\n\n' +
      'WHAT TO READ\n' +
      'HEAD is the commit BEFORE this run started, so the diff against HEAD is exactly what this run has produced. Read that diff first, then open only the files it touches. Do not read the whole repository.\n\n' +
      'THE SLICE GOAL\n' + GOAL + '\n\n' +
      'REPORT\n' +
      'Report only defects: no praise, no summary of what the code does, no scope beyond the section goal.\n' +
      'Set severity to blocker only when the finding ships a defect or breaks a bar; everything else is advisory.\n' +
      'For each finding give the file, a one-sentence summary, the concrete failure it produces, and a fix.\n' +
      'Set verdict to blockers when any finding is a blocker, otherwise pass. An empty findings array is a legitimate result.',
      { label: 'review:' + cfg.slug + ':' + rev.slug, phase: cfg.phase, schema: FINDINGS_SCHEMA, model: rev.model, effort: 'high' }
    ))
  ))

  // A reviewer that returned verdict `blockers` with no blocker in its findings
  // contradicts itself, and the cheap reading of that is "pass". Fail closed:
  // the disagreement is banked as an open blocker of its own.
  for (const r of reviews.filter(Boolean)) {
    const has = (r.findings || []).some((f) => f.severity === 'blocker')
    if ((r.verdict === 'blockers') !== has) {
      log(cfg.slug + ': a review contradicted itself — verdict ' + r.verdict + ' with ' + (has ? 'a' : 'no') + ' blocker finding')
      inconsistent.push(cfg.slug)
    }
  }

  const all = reviews.filter(Boolean).flatMap((r) => r.findings || [])
  const blockers = all.filter((f) => f.severity === 'blocker')
  const advisories = all.filter((f) => f.severity !== 'blocker')
  if (advisories.length) log(cfg.slug + ': ' + advisories.length + ' advisory finding(s) recorded')
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
      'Read the diff against HEAD and judge ONLY whether each blocker is actually closed, and whether the fix introduced a new one. Review the DELTA, not the original section.\n' +
      'Return one finding per blocker still open and one per NEW defect the fix introduced, each with its file, summary, failure and fix, and set verdict accordingly.\n' +
      'If everything is closed and nothing new broke, return an empty findings array and verdict pass.',
      { label: 'recheck:' + cfg.slug, phase: cfg.phase, schema: FINDINGS_SCHEMA, model: 'sonnet', effort: 'high' }
    )
    if (!recheck) break
    open = (recheck.findings || []).filter((f) => f.severity === 'blocker')
    if (!open.length) {
      log(cfg.slug + ': blockers closed')
      return { open: [], advisories }
    }
  }
  log(cfg.slug + ': ' + open.length + ' blocker(s) STILL OPEN after ' + MAX_FIX_ROUNDS + ' fix round(s)')
  return { open, advisories }
}

const openBlockers = []
const barFailures = []
const inconsistent = []
function bank(section, result) {
  for (const f of result.open) openBlockers.push({ section, ...f })
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
          severity: { type: 'string', enum: ['blocker', 'advisory'] },
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
  'Then set goalMet to true only if every part of the goal is demonstrably satisfied, and report a finding per part that is not, with its severity and a one-sentence summary.',
  { label: 'accept', phase: 'Accept', schema: ACCEPT_SCHEMA, model: 'sonnet', effort: 'medium' }
))

if (accept) bars('accept', accept.bars)
const acceptBlockers = accept ? (accept.findings || []).filter((f) => f.severity === 'blocker') : []

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
if (inconsistent.length) log('SELF-CONTRADICTING REVIEWS: ' + inconsistent.join(', '))

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
  barFailures,
  inconsistent,
  acceptBlockers,
  goalMet: Boolean(accept && accept.goalMet),
}
