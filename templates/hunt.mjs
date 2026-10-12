export const meta = {
  name: 'SCRIPT_NAME',
  description: 'TODO one line: which subsystem this hunt searches and what it returns. Shown in the permission dialog.',
  whenToUse: 'TODO: run once, from the repo root, on a clean tree at COMMIT; the run changes nothing in the tree outside its reproduction worktrees.',
  phases: [
    { title: 'Find', detail: 'one finder per failure class and unit, read-only — sonnet, effort medium', model: 'sonnet' },
    { title: 'Verify', detail: 'blockers and majors REPRODUCED in a throwaway worktree, the rest re-judged by reading — sonnet, effort medium', model: 'sonnet' },
    { title: 'Merge', detail: 'groups confirmed findings that share one root cause — sonnet, effort medium', model: 'sonnet' },
  ],
}

// A HUNT run: it searches a subsystem for defects and verifies the serious ones
// by REPRODUCTION — a failing test or command written inside a throwaway
// worktree (`isolation: 'worktree'`) that the run never merges. Emitted by
// `wf new-script` in a workspace opened with `wf init -type hunt`. The tree at
// REPO comes out of the run unchanged except for those worktrees, under
// `.claude/worktrees/`: after the run, `wfcheck --owns-diff <COMMIT> <script>`
// from the workspace asserts that and lists the worktrees left behind.

// ---------------------------------------------------------------------------
// Shared facts. Keep this block SMALL: it is paid once per fan-out agent.
// ---------------------------------------------------------------------------

const REPO = 'TARGET_REPO'

// The revision under hunt, as a SHA: every line a finding cites is a line of
// this commit, and every reproduction worktree is checked out at it.
const COMMIT = 'TODO: the commit SHA the hunt runs against (git rev-parse HEAD before the run)'

// One finder per failure class and unit. A LENS is a failure CLASS — data
// loss, money, concurrency, error handling, security — and disjoint classes
// find different defects. A UNIT is a slice of the subsystem BY PATH, small
// enough for one finder to read in full; together the units cover it.
const LENSES = [
  { id: 'correctness', ask: 'TODO: wrong results, broken invariants, unhandled states — what input makes this code do the wrong thing?' },
  { id: 'TODO-second-class', ask: 'TODO: a second, DISJOINT failure class (data loss, concurrency, error handling, security).' },
]
const UNITS = [
  { id: 'TODO-unit', scope: 'TODO: repo-relative paths this finder reads in full' },
]

const SEVERITY = 'blocker = a defect a user or operator hits with real damage (data lost, money wrong, a crash in a common path); major = a real defect with a narrower trigger or a recoverable outcome; minor = a real defect cheaper to leave than to fix now; note = not a defect. An open question is not a note: a finding that cannot state its trigger says which observation would settle it.'

// How a reproduction is RUN here, handed to every reproducer. A memory-heavy
// command — a whole-repo test run under a race detector — run by several
// reproducers at once is what an OOM killer resolves, so say "one package".
const REPRO_HOW = 'TODO: how one focused test or command is run in this repo, e.g. `go test ./internal/x -run TestY -count=1`'

// Reproductions are builds, one per worktree: the costliest agents of the run.
// Only blockers and majors are reproduced, worst first, up to this cap; the
// rest — and anything over the cap — are re-judged by reading, and the report
// names which were capped. `args.maxRepro` overrides it, so a dry run can walk
// the cap: `wfdry --args '{"maxRepro":1}'` — with the default the fixture's two
// serious findings never reach it.
const MAX_REPRO = (typeof args === 'object' && args && Number.isInteger(args.maxRepro)) ? args.maxRepro : 8

// A worktree-isolated agent calls git by its ABSOLUTE PATH: a PreToolUse hook
// that rewrites `git …` into a wrapper is refused inside a worktree
// (`wf docs authoring`, the worktree rule). `wf new-script` writes the path
// git resolves to on the box it runs on.
const GIT = 'GIT_PATH'

// ---------------------------------------------------------------------------
// Harness. Everything below this line is the same in every hunt script; the
// constants above and the prompts' asks are yours.
// ---------------------------------------------------------------------------

const NAV = 'NAVIGATION\n' +
  'Read files with Read, search with Grep and Glob. Reserve Bash for read-only git and for running a reproduction: a `cat` or a `sed -n` where Read would do is a round-trip that carries your entire context to fetch one file.\n'

// Every finder and reading verifier carries this. A reproducer carries
// REPRO_RULES instead: it writes, but only inside its own worktree.
const READ_ONLY = 'READ-ONLY\n' +
  'You change nothing: no Edit, no Write, no command that writes a file, no git command that moves HEAD, the index or a ref. The repository is ' + REPO + ', at commit ' + COMMIT + '.\n'

const REPRO_RULES = 'REPRODUCTION RULES\n' +
  'You run in an isolated git worktree, never in ' + REPO + '. First: `' + GIT + ' checkout --detach ' + COMMIT + '` and confirm `' + GIT + ' rev-parse HEAD` prints ' + COMMIT + ' — a worktree can arrive at another commit, and a reproduction against the wrong one proves nothing. Call git ONLY as ' + GIT + '. You may write a test or a script inside the worktree; never commit, never push, never write outside the worktree.\n' +
  'Run it with: ' + REPRO_HOW + '\n'

// spawn is the one wrapper every agent() call goes through. An unhandled throw
// at a serial step aborts the whole run, so a failed agent becomes null here
// and the caller decides what a null means.
async function spawn(prompt, opts) {
  try {
    return await agent(prompt, opts)
  } catch (err) {
    log('agent ' + (opts && opts.label ? opts.label : '?') + ' failed: ' + String(err))
    return null
  }
}

// The liveness ledger counts the finders, which are spawned UNCONDITIONALLY. A
// verifier only exists when a finding does, so a dead verifier is recorded on
// its finding instead — it must never be able to make a run with nothing to
// verify report a dead agent.
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
  required: ['coverage', 'gaps', 'findings'],
  properties: {
    coverage: { type: 'string', description: 'two sentences: which files you actually read' },
    gaps: { type: 'array', items: { type: 'string' }, description: 'repo-relative paths in your scope that you did NOT read; an empty list only if you read all of it' },
    findings: {
      type: 'array',
      items: {
        type: 'object',
        additionalProperties: false,
        required: ['severity', 'file', 'line', 'summary', 'failure', 'fix'],
        properties: {
          severity: { type: 'string', enum: ['blocker', 'major', 'minor', 'note'] },
          file: { type: 'string', description: 'repo-relative path of the primary location' },
          line: { type: 'integer', minimum: 1, description: 'line of the primary location at ' + COMMIT },
          summary: { type: 'string', description: 'one sentence stating the defect' },
          failure: { type: 'string', description: 'concrete input or state, then the wrong outcome — not a restatement of the summary' },
          fix: { type: 'string', description: 'one line naming the smallest correct fix' },
        },
      },
    },
  },
}

const REPRO_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['verified', 'onCommit', 'head', 'severity', 'command', 'output', 'reason'],
  properties: {
    verified: { type: 'boolean', description: 'true only if you REPRODUCED it: a command that fails at the commit for the stated reason' },
    onCommit: { type: 'boolean', $comment: 'x-pass: true', description: 'true only if git rev-parse HEAD in your worktree printed the commit you were given, before you ran anything' },
    head: { type: 'string', description: 'what git rev-parse HEAD printed in your worktree' },
    severity: { type: 'string', enum: ['blocker', 'major', 'minor', 'note'], description: 'your own rating, whatever the finder said' },
    command: { type: 'string', description: 'the exact command you ran, with the test or script it runs' },
    output: { type: 'string', description: 'the last lines of its output that show the failure, or show it passing' },
    reason: { type: 'string', description: 'two to four sentences: what the reproduction shows, and if it did not reproduce, why — not reproduced is not refuted' },
  },
}

const VERDICT_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['verified', 'severity', 'reason'],
  properties: {
    verified: { type: 'boolean', description: 'true only if the defect holds as stated after your own attempt to refute it' },
    severity: { type: 'string', enum: ['blocker', 'major', 'minor', 'note'], description: 'your own rating, whatever the finder said' },
    reason: { type: 'string', description: 'two to four sentences: what you checked and what decided it, with file:line' },
  },
}

const MERGE_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['groups'],
  properties: {
    groups: {
      type: 'array',
      description: 'only groups of two or more findings that share one root cause; an empty list is legitimate',
      items: {
        type: 'object',
        additionalProperties: false,
        required: ['ids', 'title'],
        properties: {
          ids: { type: 'array', items: { type: 'string' }, description: 'finding ids, exactly as given' },
          title: { type: 'string', description: 'one sentence naming the shared root cause' },
        },
      },
    },
  },
}

const LEVELS = ['blocker', 'major', 'minor', 'note']
const rank = (s) => (LEVELS.indexOf(s) < 0 ? LEVELS.length : LEVELS.indexOf(s))

function finderPrompt(lens, unit) {
  return 'You are one finder of a bug hunt over ' + REPO + ' at commit ' + COMMIT + '. Find defects that a user or operator would hit.\n\n' +
    'SCOPE: ' + unit.scope + '\n' +
    'FAILURE CLASS: ' + lens.ask + '\n' +
    'Report only what this class finds. A defect of another class is another finder\'s; a defect outside the scope is out of scope unless code in the scope makes it reachable.\n\n' +
    'SEVERITY: ' + SEVERITY + '\n\n' +
    READ_ONLY + NAV +
    '\nReturn through the StructuredOutput schema. An empty findings list is a legitimate answer; say in coverage what you read to reach it, and list in gaps every file in your scope you did not read.'
}

function finding(f) {
  return 'FINDING ' + f.id + ' [' + f.severity + '] ' + f.file + ':' + f.line + '\n' + f.summary + '\nTrigger and wrong outcome: ' + f.failure + '\n\n'
}

function reproPrompt(f) {
  return 'You are a reproducer. A finder of a bug hunt over ' + REPO + ' reported the defect below. REPRODUCE it: write the smallest test or command that fails at commit ' + COMMIT + ' for exactly the stated reason, run it, and report what it printed. If it does not fail, say why — a reproduction you could not make is not a refutation.\n\n' +
    finding(f) +
    'SEVERITY: ' + SEVERITY + '\n\n' +
    REPRO_RULES + NAV +
    '\nReturn through the StructuredOutput schema.'
}

function readerPrompt(f) {
  return 'You are a skeptic. A finder of a bug hunt over ' + REPO + ' at commit ' + COMMIT + ' reported the defect below. Try to REFUTE it: read the code it cites and what calls it, and decide whether it holds as stated.\n\n' +
    finding(f) +
    'SEVERITY: ' + SEVERITY + '\n\n' +
    READ_ONLY + NAV +
    '\nReturn through the StructuredOutput schema.'
}

function mergePrompt(list) {
  return 'These confirmed findings come from independent finders of one bug hunt. Group the ones that share ONE root cause, so the report names it once.\n\n' +
    list.map((f) => f.id + ' ' + f.file + ':' + f.line + ' ' + f.summary).join('\n') + '\n\n' +
    READ_ONLY + NAV +
    '\nReturn through the StructuredOutput schema.'
}

// ---------------------------------------------------------------------------
// Find. A finder that returns nothing is retried once; one still dead after its
// retry is a COVERAGE hole, and the report names it — a hunt that drops a
// dead finder and calls itself complete searched less than it says.
// ---------------------------------------------------------------------------

phase('Find')
const cells = []
LENSES.forEach((lens) => UNITS.forEach((unit) => cells.push({ lens, unit })))
// Results are matched back to their cell by KEY, not by position, so the
// `.filter(Boolean)` a thunk that throws needs cannot shift one finder's report
// onto another's cell — a cell with no report is a dead cell either way.
const reports = new Map((await parallel(cells.map((c) => async () => {
  const label = 'find:' + c.lens.id + ':' + c.unit.id
  const first = await spawn(finderPrompt(c.lens, c.unit), { label, phase: 'Find', model: 'sonnet', effort: 'medium', schema: FINDINGS_SCHEMA })
  if (first) return { key: label, r: live(label, first) }
  const retry = await spawn(finderPrompt(c.lens, c.unit), { label: label + ':retry', phase: 'Find', model: 'sonnet', effort: 'medium', schema: FINDINGS_SCHEMA })
  return { key: label, r: live(label + ':retry', retry) }
}))).filter(Boolean).map((x) => [x.key, x.r]))

const coverage = []
const found = []
const seen = new Set()
let nextId = 1
let malformed = 0
cells.forEach((c) => {
  const r = reports.get('find:' + c.lens.id + ':' + c.unit.id)
  const cell = c.lens.id + ' x ' + c.unit.id
  if (!r) {
    coverage.push({ cell, dead: true, count: 0, read: '', gaps: [] })
    return
  }
  let kept = 0
  if (!Array.isArray(r.findings)) malformed++
  ;(Array.isArray(r.findings) ? r.findings : []).forEach((f) => {
    if (!f || typeof f.file !== 'string') {
      malformed++
      return
    }
    // One lens may report a location once; two lenses at one line are two
    // claims until the merge says they share a cause — a `file:line` key alone
    // dropped the second lens's defect before anything verified it.
    const key = f.file + ':' + f.line + ':' + c.lens.id
    if (seen.has(key)) return
    seen.add(key)
    found.push(Object.assign({}, f, { id: 'F' + String(nextId++).padStart(3, '0'), lens: c.lens.id }))
    kept++
  })
  const gaps = Array.isArray(r.gaps) ? r.gaps.filter((g) => typeof g === 'string' && g) : []
  coverage.push({ cell, dead: false, count: kept, read: String(r.coverage || ''), gaps })
})
log(found.length + ' finding(s) after dedup from ' + cells.length + ' finder cell(s)')

// ---------------------------------------------------------------------------
// Verify. Blockers and majors are REPRODUCED, worst first, up to MAX_REPRO;
// everything else is re-judged by reading. The verifier's severity is the one
// reported. Reproducers and readers both carry a `verify` segment in their
// label, so a dry run's `dead-reviews` kills them both.
// ---------------------------------------------------------------------------

const results = []
const capped = []
if (found.length) {
  phase('Verify')
  const ordered = found.slice().sort((a, b) => rank(a.severity) - rank(b.severity))
  const plan = ordered.map((f) => {
    const serious = rank(f.severity) <= 1
    if (serious && ordered.filter((g, i) => i < ordered.indexOf(f) && rank(g.severity) <= 1).length >= MAX_REPRO) {
      capped.push(f.id)
      return { f, repro: false }
    }
    return { f, repro: serious }
  })
  const verdicts = new Map((await parallel(plan.map((p) => async () => ({
    id: p.f.id,
    repro: p.repro,
    v: p.repro
      ? await spawn(reproPrompt(p.f), { label: 'verify:repro:' + p.f.id, phase: 'Verify', model: 'sonnet', effort: 'medium', isolation: 'worktree', schema: REPRO_SCHEMA })
      : await spawn(readerPrompt(p.f), { label: 'verify:read:' + p.f.id, phase: 'Verify', model: 'sonnet', effort: 'medium', schema: VERDICT_SCHEMA }),
  })))).filter(Boolean).map((x) => [x.id, x]))
  found.forEach((f) => {
    const x = verdicts.get(f.id)
    const v = x && x.v
    if (!v) {
      results.push(Object.assign({}, f, { status: 'unverified', finalSeverity: f.severity, reason: 'the verifier returned nothing' }))
      return
    }
    const sev = LEVELS.includes(v.severity) ? v.severity : f.severity
    // Not reproduced is NOT refuted: the reproducer may have lacked the state,
    // the data or the timing, and the finding stands as unproven, listed apart.
    // A reproduction counts only with a command that can be re-run AND a
    // worktree the reproducer confirmed was at COMMIT; a "yes" without either
    // is a claim, listed as not reproduced with what was missing.
    const command = x.repro ? String(v.command || '').trim() : ''
    let reason = String(v.reason || '')
    let status = x.repro ? (v.verified ? 'reproduced' : 'not-reproduced') : (v.verified ? 'confirmed' : 'refuted')
    if (status === 'reproduced' && (!command || v.onCommit !== true)) {
      status = 'not-reproduced'
      reason = (!command ? 'no command reported' : 'the worktree was not confirmed at ' + COMMIT + ' (head: ' + String(v.head || '?') + ')') + '. ' + reason
    }
    results.push(Object.assign({}, f, { status, finalSeverity: sev, reason, command, head: x.repro ? String(v.head || '') : '', output: x.repro ? String(v.output || '') : '', capped: capped.includes(f.id) }))
  })
}
const confirmed = results.filter((r) => r.status === 'reproduced' || r.status === 'confirmed').sort((a, b) => rank(a.finalSeverity) - rank(b.finalSeverity))
const notReproduced = results.filter((r) => r.status === 'not-reproduced')
const refuted = results.filter((r) => r.status === 'refuted')
const unverified = results.filter((r) => r.status === 'unverified')

// ---------------------------------------------------------------------------
// Merge. Only when there is something to merge; a dead merger loses grouping,
// not findings, so it is said and the findings stand ungrouped.
// ---------------------------------------------------------------------------

let groups = []
let mergeDied = false
if (confirmed.length > 1) {
  phase('Merge')
  const m = await spawn(mergePrompt(confirmed), { label: 'merge', phase: 'Merge', model: 'sonnet', effort: 'medium', schema: MERGE_SCHEMA })
  if (!m) mergeDied = true
  else {
    // A group may only name findings the merger was handed: an id it invented
    // would print as a root cause nothing confirmed.
    const known = new Set(confirmed.map((f) => f.id))
    groups = (Array.isArray(m.groups) ? m.groups : [])
      .map((g) => ({ title: g && g.title, ids: g && Array.isArray(g.ids) ? g.ids.filter((id) => known.has(id)) : [] }))
      .filter((g) => g.title && g.ids.length > 1)
  }
}

// ---------------------------------------------------------------------------
// The report. Its population comes FIRST: "no findings" over a hunt whose
// finders died is not a clean subsystem, and the two must never read alike.
// ---------------------------------------------------------------------------

const deadCells = coverage.filter((c) => c.dead).map((c) => c.cell)
const gapCells = coverage.filter((c) => c.gaps && c.gaps.length).map((c) => c.cell + ' (' + c.gaps.join(', ') + ')')
const complete = deadCells.length === 0 && gapCells.length === 0 && unverified.length === 0
const out = []
out.push('Bug hunt over ' + REPO + ' at ' + COMMIT + ': ' + (cells.length - deadCells.length) + ' of ' + cells.length + ' finder cell(s) reported' + (deadCells.length ? ' — NOT COVERED: ' + deadCells.join(', ') : '') + '.')
if (gapCells.length) out.push('Files a finder did not read: ' + gapCells.join('; ') + '.')
out.push(found.length + ' finding(s) after dedup: ' + results.filter((r) => r.status === 'reproduced').length + ' reproduced, ' + results.filter((r) => r.status === 'confirmed').length + ' confirmed by reading, ' + notReproduced.length + ' not reproduced, ' + refuted.length + ' refuted, ' + unverified.length + ' unverified' + (capped.length ? ', ' + capped.length + ' serious finding(s) read rather than reproduced (MAX_REPRO: ' + capped.join(', ') + ')' : '') + (malformed ? ', ' + malformed + ' malformed report(s) dropped' : '') + '.')
if (!complete) out.push('INCOMPLETE: ' + [deadCells.length ? deadCells.length + ' dead cell(s)' : '', gapCells.length ? gapCells.length + ' cell(s) with unread files' : '', unverified.length ? unverified.length + ' unverified finding(s)' : ''].filter(Boolean).join(', ') + ' — what follows is not a clean hunt of the whole subsystem.')
if (!confirmed.length) out.push(found.length ? 'No finding was reproduced or confirmed.' : 'Nothing was found over the cells listed below.')
out.push('')
confirmed.forEach((f) => out.push('- [' + f.finalSeverity + '] ' + f.id + ' ' + f.file + ':' + f.line + ' — ' + f.summary + '\n  trigger: ' + f.failure + '\n  fix: ' + f.fix + (f.status === 'reproduced' ? '\n  reproduced at ' + f.head + ': `' + f.command + '`\n    ' + f.output.split('\n').join('\n    ') : '\n  confirmed by reading' + (f.capped ? ' (over MAX_REPRO, not reproduced)' : '') + ': ' + f.reason)))
groups.forEach((g) => out.push('- shared root cause (' + g.ids.join(', ') + '): ' + g.title))
if (mergeDied) out.push('- the merge agent died: findings sharing a root cause are listed separately')
refuted.filter((f) => f.capped).forEach((f) => out.push('- REFUTED BY READING, over MAX_REPRO and never reproduced [' + f.severity + '] ' + f.id + ' ' + f.file + ':' + f.line + ' — ' + f.summary + '\n  why: ' + f.reason))
notReproduced.forEach((f) => out.push('- NOT REPRODUCED [' + f.finalSeverity + '] ' + f.id + ' ' + f.file + ':' + f.line + ' — ' + f.summary + '\n  why: ' + f.reason))
unverified.forEach((f) => out.push('- UNVERIFIED [' + f.severity + '] ' + f.id + ' ' + f.file + ':' + f.line + ' — ' + f.summary))
out.push('')
coverage.forEach((c) => out.push('coverage ' + c.cell + ': ' + (c.dead ? 'DEAD' : c.count + ' kept — ' + c.read)))

log(spawned.length - dead.length + '/' + spawned.length + ' finder(s) came back')
if (dead.length) log('DEAD: ' + dead.join(', '))

return {
  // complete is the hunt's coverage, not its outcome: a hunt that found
  // nothing over every cell IS complete, and one that found ten while a finder
  // died is not. A finding nobody could reproduce does not make it incomplete:
  // it is reported apart, as unproven.
  complete,
  report: out.join('\n'),
  confirmed: confirmed.map((f) => ({ id: f.id, severity: f.finalSeverity, file: f.file, line: f.line, summary: f.summary, reproduced: f.status === 'reproduced', command: f.command })),
  notReproduced: notReproduced.map((f) => f.id),
  refuted: refuted.length,
  unverified: unverified.map((f) => f.id),
  capped,
  deadCells,
}
