export const meta = {
  name: 'SCRIPT_NAME',
  description: 'TODO one line: what this review reads and what it returns. Shown in the permission dialog.',
  whenToUse: 'TODO: run once, from the repo root; the run changes nothing in the tree.',
  phases: [
    { title: 'Find', detail: 'one finder per lens and unit, read-only — sonnet, effort medium', model: 'sonnet' },
    { title: 'Verify', detail: 'one skeptic per finding, who re-judges it and may refute it — sonnet, effort medium', model: 'sonnet' },
    { title: 'Merge', detail: 'groups confirmed findings that share one root cause — sonnet, effort medium', model: 'sonnet' },
  ],
}

// A REVIEW run: it reads a diff and returns findings, and the tree comes out of
// it byte-identical. Emitted by `wf new-script` in a workspace opened with
// `wf init -type review`; `wfdry` reads that type from gate.json and walks this
// script's happy path as "found some, verified them, merged them" — the build
// skeleton's producers, bars, fix round and acceptance step are absent on
// purpose, because a run that writes nothing has nothing for them to check.
// After the run: `wfcheck --owns-diff <the SHA the run started from> <script>`
// from the workspace asserts that the tree did not change.

// ---------------------------------------------------------------------------
// Shared facts. Keep this block SMALL: it is paid once per fan-out agent.
// ---------------------------------------------------------------------------

const REPO = 'TARGET_REPO'

// The diff under review. Both ends are REFS the finders resolve themselves, so
// the prompt never carries the diff: a shared blob over ~10-20 KB is paid once
// per finder, and every finder reads the slice it was given.
const BASE = 'TODO: the ref the diff is taken from, e.g. origin/main'
const HEAD_REF = 'HEAD'

// One finder per lens and unit. A LENS is a failure surface the finder is told
// to look for — disjoint lenses find different defects, overlapping ones find
// the same defect twice and the verifier pays for both. A UNIT is a slice of
// the diff; one unit over the whole diff is right until the diff is too large
// for one finder to read, and then units split it by path.
const LENSES = [
  { id: 'correctness', ask: 'TODO: wrong results, broken invariants, unhandled states — what input makes this code do the wrong thing?' },
  { id: 'TODO-second-lens', ask: 'TODO: a second, DISJOINT failure surface (security, concurrency, API compatibility, tests that cannot fail).' },
]
const UNITS = [
  { id: 'all', scope: 'every file the diff touches' },
]

const SEVERITY = 'blocker = merging this ships a defect nothing later catches; major = a real defect, and you name what catches it or why it is not caught; minor = a real defect cheaper to leave than to fix now; note = not a defect. An open question is not a note: a finding that cannot state its fix says which observation would settle it.'

// ---------------------------------------------------------------------------
// Harness. Everything below this line is the same in every review script; the
// constants above and the prompts' asks are yours.
// ---------------------------------------------------------------------------

const NAV = 'NAVIGATION\n' +
  'Read files with Read, search with Grep and Glob. Reserve Bash for read-only git (`git diff`, `git log`, `git show`): a `cat` or a `sed -n` where Read would do is a round-trip that carries your entire context to fetch one file.\n'

// Every prompt carries this, because the property the run promises — the tree
// is unchanged — is held by nothing else while the run is live.
const READ_ONLY = 'READ-ONLY\n' +
  'You change nothing: no Edit, no Write, no command that writes a file, no git command that moves HEAD, the index or a ref. The repository is ' + REPO + '.\n'

// The reading cells are narrowed AT SPAWN: the harness removes the tools named
// here from the agent's tool list before it starts — measured 2026-10-20, no
// denied call, the agent carries on and returns through its schema. READ_ONLY
// asks; this removes. Bash stays: NAV allows read-only git. `wfcheck --owns-diff`
// after the run is still the bar — an option the harness renames is a key
// ignored in silence, and only the tree says whether the property held.
const NO_WRITE = ['Write', 'Edit', 'NotebookEdit']

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
          line: { type: 'integer', minimum: 1, description: 'line of the primary location at ' + HEAD_REF },
          summary: { type: 'string', description: 'one sentence stating the defect' },
          failure: { type: 'string', description: 'concrete input or state, then the wrong outcome — not a restatement of the summary' },
          fix: { type: 'string', description: 'one line naming the smallest correct fix' },
        },
      },
    },
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
  return 'You are one finder of a code review. Find defects in the diff ' + BASE + '...' + HEAD_REF + ' — resolve it yourself with `git diff ' + BASE + '...' + HEAD_REF + '`.\n\n' +
    'SCOPE: ' + unit.scope + '\n' +
    'LENS: ' + lens.ask + '\n' +
    'Report only what this lens finds. A defect outside the lens is another finder\'s; a defect outside the diff is out of scope unless the diff makes it reachable.\n\n' +
    'SEVERITY: ' + SEVERITY + '\n\n' +
    READ_ONLY + NAV +
    '\nReturn through the StructuredOutput schema. An empty findings list is a legitimate answer; say in coverage what you read to reach it, and list in gaps every file in your scope you did not read.'
}

function verifierPrompt(f) {
  return 'You are a skeptic. A finder of a code review over ' + BASE + '...' + HEAD_REF + ' reported the defect below. Try to REFUTE it: read the code it cites and what calls it, and decide whether it holds as stated.\n\n' +
    'FINDING ' + f.id + ' [' + f.severity + '] ' + f.file + ':' + f.line + '\n' +
    f.summary + '\nFailure: ' + f.failure + '\n\n' +
    'SEVERITY: ' + SEVERITY + '\n\n' +
    READ_ONLY + NAV +
    '\nReturn through the StructuredOutput schema.'
}

function mergePrompt(list) {
  return 'These confirmed findings come from independent finders of one review. Group the ones that share ONE root cause, so the report names it once.\n\n' +
    list.map((f) => f.id + ' ' + f.file + ':' + f.line + ' ' + f.summary).join('\n') + '\n\n' +
    READ_ONLY + NAV +
    '\nReturn through the StructuredOutput schema.'
}

// ---------------------------------------------------------------------------
// Find. A finder that returns nothing is retried once; one still dead after its
// retry is a COVERAGE hole, and the report names it — a review that drops a
// dead finder and calls itself complete reviewed less than it says.
// ---------------------------------------------------------------------------

phase('Find')
const cells = []
LENSES.forEach((lens) => UNITS.forEach((unit) => cells.push({ lens, unit })))
// Results are matched back to their cell by KEY, not by position, so the
// `.filter(Boolean)` a thunk that throws needs cannot shift one finder's report
// onto another's cell — a cell with no report is a dead cell either way.
const reports = new Map((await parallel(cells.map((c) => async () => {
  const label = 'find:' + c.lens.id + ':' + c.unit.id
  const first = await spawn(finderPrompt(c.lens, c.unit), { label, phase: 'Find', model: 'sonnet', effort: 'medium', disallowedTools: NO_WRITE, schema: FINDINGS_SCHEMA })
  if (first) return { key: label, r: live(label, first) }
  const retry = await spawn(finderPrompt(c.lens, c.unit), { label: label + ':retry', phase: 'Find', model: 'sonnet', effort: 'medium', disallowedTools: NO_WRITE, schema: FINDINGS_SCHEMA })
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
// Verify. Every finding is re-judged by an agent that did not write it, and the
// VERIFIER's severity is the one reported: a finder rates its own find.
// ---------------------------------------------------------------------------

const results = []
if (found.length) {
  phase('Verify')
  const verdicts = new Map((await parallel(found.map((f) => async () => ({ id: f.id, v: await spawn(verifierPrompt(f), { label: 'verify:' + f.id, phase: 'Verify', model: 'sonnet', effort: 'medium', disallowedTools: NO_WRITE, schema: VERDICT_SCHEMA }) })))).filter(Boolean).map((x) => [x.id, x.v]))
  found.forEach((f) => {
    const v = verdicts.get(f.id)
    if (!v) results.push(Object.assign({}, f, { status: 'unverified', finalSeverity: f.severity, reason: 'the verifier returned nothing' }))
    else results.push(Object.assign({}, f, { status: v.verified ? 'confirmed' : 'refuted', finalSeverity: LEVELS.includes(v.severity) ? v.severity : f.severity, reason: String(v.reason || '') }))
  })
}
const confirmed = results.filter((r) => r.status === 'confirmed').sort((a, b) => rank(a.finalSeverity) - rank(b.finalSeverity))
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
  const m = await spawn(mergePrompt(confirmed), { label: 'merge', phase: 'Merge', model: 'sonnet', effort: 'medium', disallowedTools: NO_WRITE, schema: MERGE_SCHEMA })
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
// The report. Its population comes FIRST: "no findings" over a review whose
// finders died is not a clean review, and the two must never read alike.
// ---------------------------------------------------------------------------

const deadCells = coverage.filter((c) => c.dead).map((c) => c.cell)
const gapCells = coverage.filter((c) => c.gaps && c.gaps.length).map((c) => c.cell + ' (' + c.gaps.join(', ') + ')')
const complete = deadCells.length === 0 && gapCells.length === 0 && unverified.length === 0
const out = []
out.push('Review of ' + BASE + '...' + HEAD_REF + ' in ' + REPO + ': ' + (cells.length - deadCells.length) + ' of ' + cells.length + ' finder cell(s) reported' + (deadCells.length ? ' — NOT COVERED: ' + deadCells.join(', ') : '') + '.')
if (gapCells.length) out.push('Files a finder did not read: ' + gapCells.join('; ') + '.')
out.push(found.length + ' finding(s) after dedup: ' + confirmed.length + ' confirmed, ' + refuted.length + ' refuted, ' + unverified.length + ' unverified' + (malformed ? ', ' + malformed + ' malformed report(s) dropped' : '') + '.')
if (!complete) out.push('INCOMPLETE: ' + [deadCells.length ? deadCells.length + ' dead cell(s)' : '', gapCells.length ? gapCells.length + ' cell(s) with unread files' : '', unverified.length ? unverified.length + ' unverified finding(s)' : ''].filter(Boolean).join(', ') + ' — what follows is not a clean review of the whole diff.')
if (!confirmed.length) out.push(found.length ? 'No finding survived verification.' : 'Nothing was found over the cells listed below.')
out.push('')
confirmed.forEach((f) => out.push('- [' + f.finalSeverity + '] ' + f.id + ' ' + f.file + ':' + f.line + ' — ' + f.summary + '\n  failure: ' + f.failure + '\n  fix: ' + f.fix + '\n  verified: ' + f.reason))
groups.forEach((g) => out.push('- shared root cause (' + g.ids.join(', ') + '): ' + g.title))
if (mergeDied) out.push('- the merge agent died: findings sharing a root cause are listed separately')
unverified.forEach((f) => out.push('- UNVERIFIED [' + f.severity + '] ' + f.id + ' ' + f.file + ':' + f.line + ' — ' + f.summary))
out.push('')
coverage.forEach((c) => out.push('coverage ' + c.cell + ': ' + (c.dead ? 'DEAD' : c.count + ' kept — ' + c.read)))

log(spawned.length - dead.length + '/' + spawned.length + ' finder(s) came back')
if (dead.length) log('DEAD: ' + dead.join(', '))

return {
  // complete is the review's coverage, not its outcome: a review that found
  // nothing over every cell IS complete, and one that found ten while a finder
  // died is not.
  complete,
  report: out.join('\n'),
  confirmed: confirmed.map((f) => ({ id: f.id, severity: f.finalSeverity, file: f.file, line: f.line, summary: f.summary })),
  refuted: refuted.length,
  unverified: unverified.map((f) => f.id),
  deadCells,
}
