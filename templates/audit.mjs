export const meta = {
  name: 'SCRIPT_NAME',
  description: 'TODO one line: the question this audit answers about which code. Shown in the permission dialog.',
  whenToUse: 'TODO: run once, from the repo root, on a clean tree at COMMIT; the run changes nothing in the tree.',
  phases: [
    { title: 'Find', detail: 'one finder per part of the question and unit, read-only — sonnet, effort medium', model: 'sonnet' },
    { title: 'Verify', detail: 'one citation checker per claim, who reads the cited lines and may reject the claim — sonnet, effort medium', model: 'sonnet' },
    { title: 'Write', detail: 'one writer composes the answer from the VERIFIED claims only — sonnet, effort high', model: 'sonnet' },
  ],
}

// An AUDIT run: it answers a QUESTION about its target — does the code do what
// the spec says, do the docs match the code, is this property held — with a
// document whose every claim cites `path:line#anchor`, the anchor an
// identifier on the cited line. Emitted by `wf new-script` in a workspace
// opened with `wf init -type audit`. The tree comes out of the run unchanged;
// the answer comes back in the return value. After the run: save `citable` into
// the workspace (`artifacts/answer.md`, then `wf add`) and run
// `wf check cite -at <COMMIT> -require-anchor` — every citation resolves and
// its anchor is on the cited lines, or it is named — and
// `wfcheck -type audit --owns-diff <COMMIT> <script>` for the tree.

// ---------------------------------------------------------------------------
// Shared facts. Keep this block SMALL: it is paid once per fan-out agent.
// ---------------------------------------------------------------------------

const REPO = 'TARGET_REPO'

// The revision the answer is true of, as a SHA: every citation is a line of it.
const COMMIT = 'TODO: the commit SHA the audit runs against (git rev-parse HEAD before the run)'

// The question, and its PARTS. A part is a sub-question one finder can answer
// from the code alone; the parts together are the question — a part nobody
// asked is a part the answer is silent on. A UNIT is a slice of the code by
// path that a finder reads in full.
const QUESTION = 'TODO: the question, in one or two sentences, as the reader of the answer would ask it.'
const PARTS = [
  { id: 'TODO-part-one', ask: 'TODO: one sub-question, answerable from the code.' },
  { id: 'TODO-part-two', ask: 'TODO: a second, DISJOINT sub-question.' },
]
const UNITS = [
  { id: 'TODO-unit', scope: 'TODO: repo-relative paths this finder reads in full' },
]

// ---------------------------------------------------------------------------
// Harness. Everything below this line is the same in every audit script; the
// constants above are yours.
// ---------------------------------------------------------------------------

const NAV = 'NAVIGATION\n' +
  'Read files with Read, search with Grep and Glob. Reserve Bash for read-only git: a `cat` or a `sed -n` where Read would do is a round-trip that carries your entire context to fetch one file.\n'

const READ_ONLY = 'READ-ONLY\n' +
  'You change nothing: no Edit, no Write, no command that writes a file, no git command that moves HEAD, the index or a ref. The repository is ' + REPO + ', at commit ' + COMMIT + '.\n'

// The reading cells are narrowed AT SPAWN: the harness removes the tools named
// here from the agent's tool list before it starts — measured 2026-10-20, no
// denied call, the agent carries on and returns through its schema. READ_ONLY
// asks; this removes. Bash stays: NAV allows read-only git. `wfcheck --owns-diff`
// after the run is still the bar — an option the harness renames is a key
// ignored in silence, and only the tree says whether the property held.
const NO_WRITE = ['Write', 'Edit', 'NotebookEdit']

// The citation form is the one `wf check cite` verifies: `path.ext:LINE` or
// `path.ext:A-B`, then `#anchor` — an ASCII identifier that occurs on the cited
// lines. The anchor is what catches a citation off by a few lines, which a
// range check cannot: both lines exist.
const CITE = 'CITATIONS\n' +
  'Cite every claim as `path.ext:LINE#anchor` or `path.ext:A-B#anchor`, the path repo-relative, the lines those of commit ' + COMMIT + ', and the anchor an ASCII identifier — a function, type or constant name — that occurs ON the cited lines.\n'

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

const CLAIMS_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['coverage', 'gaps', 'claims'],
  properties: {
    coverage: { type: 'string', description: 'two sentences: which files you actually read' },
    gaps: { type: 'array', items: { type: 'string' }, description: 'repo-relative paths in your scope that you did NOT read; an empty list only if you read all of it' },
    claims: {
      type: 'array',
      items: {
        type: 'object',
        additionalProperties: false,
        required: ['statement', 'kind', 'file', 'line', 'lastLine', 'anchor', 'evidence'],
        properties: {
          statement: { type: 'string', description: 'one sentence, true or false of the code, that answers part of your sub-question' },
          kind: { type: 'string', enum: ['holds', 'violated', 'partial'], description: 'holds: the code does what the question asks of it; violated: it does not; partial: only in some cases, named in the statement' },
          file: { type: 'string', examples: ['pkg/store.go', 'pkg/load.go'], description: 'repo-relative path of the cited lines, with its extension, no leading ./ or /' },
          line: { type: 'integer', minimum: 1, description: 'first cited line at the commit' },
          lastLine: { type: 'integer', minimum: 0, description: 'last cited line, or 0 for a single line' },
          anchor: { type: 'string', examples: ['Save', 'Load'], description: 'an identifier of two or more letters, digits or underscores that occurs on the cited lines' },
          evidence: { type: 'string', description: 'the cited code, quoted, and why it supports the statement' },
        },
      },
    },
  },
}

const VERDICT_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['verified', 'reason'],
  properties: {
    verified: { type: 'boolean', description: 'true only if the cited lines, read at the commit, carry the anchor and support the statement as written' },
    reason: { type: 'string', description: 'two to four sentences: what the cited lines say, and why they do or do not support the statement' },
  },
}

const WRITER_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['sentences', 'openQuestions'],
  properties: {
    sentences: {
      type: 'array',
      description: 'the answer, one sentence per item, in reading order',
      items: {
        type: 'object',
        additionalProperties: false,
        required: ['text', 'uses'],
        properties: {
          text: { type: 'string', description: 'one sentence of the answer, with NO citation in it — the harness appends them' },
          uses: { type: 'array', items: { type: 'integer', minimum: 1 }, description: 'the numbers of the claims this sentence rests on; at least one' },
        },
      },
    },
    openQuestions: { type: 'array', items: { type: 'string' }, description: 'the parts of the question the verified claims do NOT settle; an empty list only if they settle all of it' },
  },
}

// The shape every citation must have before anything trusts it — the grammar
// `wf check cite` reads. A free-text `line` such as "L7" made a citation
// INVISIBLE to that check, and an anchor with a space was cut at the space and
// then matched (the review round's probes); an anchor of one letter pins
// almost nothing — it matched almost any line before `check cite` read
// identifier parts.
const FILE_RE = /^[A-Za-z0-9_][^\s#:]*\.[A-Za-z0-9]+$/
const ANCHOR_RE = /^[A-Za-z_][A-Za-z0-9_]+$/
function wellFormed(c) {
  return typeof c.file === 'string' && FILE_RE.test(c.file) &&
    Number.isInteger(c.line) && c.line >= 1 &&
    Number.isInteger(c.lastLine) && (c.lastLine === 0 || c.lastLine >= c.line) &&
    typeof c.anchor === 'string' && ANCHOR_RE.test(c.anchor)
}

function cite(c) {
  return c.file + ':' + c.line + (c.lastLine > c.line ? '-' + c.lastLine : '') + '#' + c.anchor
}

function finderPrompt(part, unit) {
  return 'You are one finder of an audit of ' + REPO + ' at commit ' + COMMIT + '. The question: ' + QUESTION + '\n\n' +
    'YOUR PART: ' + part.ask + '\n' +
    'SCOPE: ' + unit.scope + '\n' +
    'Return CLAIMS that answer your part — what the code does AND what it fails to do; a claim that the code holds is as much an answer as one that it is violated. Only what the code at the commit shows; no claim without a citation.\n\n' +
    CITE + READ_ONLY + NAV +
    '\nReturn through the StructuredOutput schema. An empty claims list is a legitimate answer; say in coverage what you read, and list in gaps every file in your scope you did not read.'
}

function verifierPrompt(c) {
  return 'You are a citation checker for an audit of ' + REPO + ' at commit ' + COMMIT + '. Read the cited lines at that commit and decide whether they carry the anchor and support the claim AS WRITTEN — a claim the lines only half support is not verified.\n\n' +
    'CLAIM ' + c.id + ' [' + c.kind + '] ' + c.statement + '\nCited: ' + cite(c) + '\nThe finder\'s evidence: ' + c.evidence + '\n\n' +
    READ_ONLY + NAV +
    '\nReturn through the StructuredOutput schema.'
}

function writerPrompt(list) {
  return 'You write the answer of an audit of ' + REPO + ' at commit ' + COMMIT + '. The question: ' + QUESTION + '\n\n' +
    'Use ONLY the numbered verified claims below. Write the answer as sentences; give each sentence the numbers of the claims it rests on, at least one, and add no fact those claims do not carry. Put no citation in the text — the harness appends each sentence\'s citations from the numbers. Where the claims do not settle a part of the question, list it in openQuestions.\n\n' +
    'THE PARTS OF THE QUESTION:\n' + PARTS.map((p) => '- ' + p.id + ': ' + p.ask).join('\n') + '\n\n' +
    'VERIFIED CLAIMS:\n' + list.map((c, i) => '[' + (i + 1) + '] (' + c.lens + ', ' + c.kind + ') ' + c.statement).join('\n') + '\n\n' +
    READ_ONLY +
    '\nReturn through the StructuredOutput schema.'
}

// ---------------------------------------------------------------------------
// Find. A finder that returns nothing is retried once; one still dead after its
// retry is a COVERAGE hole, and the report names it — an audit that drops
// a dead finder and calls itself complete answered from less than it says.
// ---------------------------------------------------------------------------

phase('Find')
const cells = []
PARTS.forEach((lens) => UNITS.forEach((unit) => cells.push({ lens, unit })))
// Results are matched back to their cell by KEY, not by position, so the
// `.filter(Boolean)` a thunk that throws needs cannot shift one finder's report
// onto another's cell — a cell with no report is a dead cell either way.
const reports = new Map((await parallel(cells.map((c) => async () => {
  const label = 'find:' + c.lens.id + ':' + c.unit.id
  const first = await spawn(finderPrompt(c.lens, c.unit), { label, phase: 'Find', model: 'sonnet', effort: 'medium', disallowedTools: NO_WRITE, schema: CLAIMS_SCHEMA })
  if (first) return { key: label, r: live(label, first) }
  const retry = await spawn(finderPrompt(c.lens, c.unit), { label: label + ':retry', phase: 'Find', model: 'sonnet', effort: 'medium', disallowedTools: NO_WRITE, schema: CLAIMS_SCHEMA })
  return { key: label, r: live(label + ':retry', retry) }
}))).filter(Boolean).map((x) => [x.key, x.r]))

const coverage = []
const found = []
const seen = new Set()
let nextId = 1
let malformed = 0
const malformedList = []
cells.forEach((c) => {
  const r = reports.get('find:' + c.lens.id + ':' + c.unit.id)
  const cell = c.lens.id + ' x ' + c.unit.id
  if (!r) {
    coverage.push({ cell, dead: true, count: 0, read: '', gaps: [] })
    return
  }
  let kept = 0
  if (!Array.isArray(r.claims)) malformed++
  ;(Array.isArray(r.claims) ? r.claims : []).forEach((f) => {
    if (!f || !wellFormed(f)) {
      malformed++
      malformedList.push(c.lens.id + ': ' + String(f && f.statement || '?').slice(0, 120) + ' (cited ' + String(f && f.file) + ' ' + String(f && f.line) + ' #' + String(f && f.anchor) + ')')
      return
    }
    // Two claims at one location are one only when they are the same claim:
    // a `holds` and a `violated` on one line, or two parts asking of it, are
    // different answers.
    const key = f.file + ':' + f.line + ':' + f.anchor + ':' + f.kind + ':' + c.lens.id
    if (seen.has(key)) return
    seen.add(key)
    found.push(Object.assign({}, f, { id: 'C' + String(nextId++).padStart(3, '0'), lens: c.lens.id }))
    kept++
  })
  const gaps = Array.isArray(r.gaps) ? r.gaps.filter((g) => typeof g === 'string' && g) : []
  coverage.push({ cell, dead: false, count: kept, read: String(r.coverage || ''), gaps })
})
log(found.length + ' claim(s) after dedup from ' + cells.length + ' finder cell(s)')

// ---------------------------------------------------------------------------
// Verify. Every claim's citation is read by an agent that did not write it. A
// claim the cited lines do not support is dropped from the answer and listed
// as unsupported — it never reaches the writer.
// ---------------------------------------------------------------------------

const results = []
if (found.length) {
  phase('Verify')
  const verdicts = new Map((await parallel(found.map((c) => async () => ({ id: c.id, v: await spawn(verifierPrompt(c), { label: 'verify:' + c.id, phase: 'Verify', model: 'sonnet', effort: 'medium', disallowedTools: NO_WRITE, schema: VERDICT_SCHEMA }) })))).filter(Boolean).map((x) => [x.id, x.v]))
  found.forEach((c) => {
    const v = verdicts.get(c.id)
    if (!v) results.push(Object.assign({}, c, { status: 'unverified', reason: 'the checker returned nothing' }))
    else results.push(Object.assign({}, c, { status: v.verified ? 'verified' : 'unsupported', reason: String(v.reason || '') }))
  })
}
const verified = results.filter((r) => r.status === 'verified')
const unsupported = results.filter((r) => r.status === 'unsupported')
const unverified = results.filter((r) => r.status === 'unverified')

// ---------------------------------------------------------------------------
// Write. One writer composes the answer from the verified claims. With none,
// there is nothing to write from, and the report says the question went
// unanswered rather than inventing an answer.
// ---------------------------------------------------------------------------

// The writer names claims by NUMBER and the harness appends their citations,
// so no citation in the answer can be one a checker did not read: a writer
// that wrote citation strings itself could cite a real line nobody checked.
// A sentence resting on no valid number is marked UNCITED, and an answer
// with one is not `answered`.
let sentences = []
let uncited = 0
let openQuestions = []
let writerDied = false
let writerEmpty = false
if (verified.length) {
  phase('Write')
  const w = await spawn(writerPrompt(verified), { label: 'write:answer', phase: 'Write', model: 'sonnet', effort: 'high', disallowedTools: NO_WRITE, schema: WRITER_SCHEMA })
  if (!w) writerDied = true
  else {
    sentences = (Array.isArray(w.sentences) ? w.sentences : []).filter((s) => s && typeof s.text === 'string' && s.text.trim()).map((s) => {
      const uses = (Array.isArray(s.uses) ? s.uses : []).filter((n) => Number.isInteger(n) && n >= 1 && n <= verified.length)
      if (!uses.length) uncited++
      return { text: s.text.trim(), cites: uses.map((n) => cite(verified[n - 1])) }
    })
    writerEmpty = sentences.length === 0
    openQuestions = Array.isArray(w.openQuestions) ? w.openQuestions.filter((q) => typeof q === 'string' && q) : []
  }
}
// A part with no verified claim is unsettled whatever the writer says: the
// harness lists it, rather than trusting the writer to notice.
const unsettled = PARTS.filter((p) => !verified.some((c) => c.lens === p.id)).map((p) => p.id + ': ' + p.ask)
const answer = sentences.map((s) => s.text + (s.cites.length ? ' (' + s.cites.join(', ') + ')' : ' [UNCITED]')).join(' ')

// ---------------------------------------------------------------------------
// The report. Its population comes FIRST; then the answer; then every verified
// claim with its citation, in the form `wf check cite` reads, so the citations
// are checkable once the report is saved into the workspace.
// ---------------------------------------------------------------------------

const deadCells = coverage.filter((c) => c.dead).map((c) => c.cell)
const gapCells = coverage.filter((c) => c.gaps && c.gaps.length).map((c) => c.cell + ' (' + c.gaps.join(', ') + ')')
const complete = deadCells.length === 0 && gapCells.length === 0 && unverified.length === 0 && !writerDied && !writerEmpty
const answered = sentences.length > 0 && uncited === 0
const out = []
out.push('# Audit: ' + QUESTION)
out.push('')
out.push(REPO + ' at ' + COMMIT + ': ' + (cells.length - deadCells.length) + ' of ' + cells.length + ' finder cell(s) reported' + (deadCells.length ? ' — NOT COVERED: ' + deadCells.join(', ') : '') + '.')
if (gapCells.length) out.push('Files a finder did not read: ' + gapCells.join('; ') + '.')
out.push(found.length + ' claim(s) after dedup: ' + verified.length + ' verified, ' + unsupported.length + ' unsupported by their citation, ' + unverified.length + ' unverified' + (malformed ? ', ' + malformed + ' malformed claim(s) or report(s) dropped' : '') + '.')
if (!complete) out.push('INCOMPLETE: ' + [deadCells.length ? deadCells.length + ' dead cell(s)' : '', gapCells.length ? gapCells.length + ' cell(s) with unread files' : '', unverified.length ? unverified.length + ' unverified claim(s)' : '', writerDied ? 'the writer died, so there is no composed answer' : '', writerEmpty ? 'the writer returned no sentence' : ''].filter(Boolean).join(', ') + ' — what follows is not a complete answer.')
out.push('')
out.push('## Answer')
out.push('')
if (answer) out.push(answer)
else if (verified.length) out.push('No composed answer — the writer returned nothing; the verified claims below stand on their own.')
else out.push('The question went UNANSWERED: ' + (found.length ? 'no claim survived its citation check.' : 'no claim was found.'))
if (uncited) out.push('', uncited + ' sentence(s) above rest on no verified claim and are marked [UNCITED] — the answer is not established by them.')
const notSettled = unsettled.concat(openQuestions.filter((q) => !unsettled.includes(q)))
if (notSettled.length) {
  out.push('')
  out.push('Not settled by the verified claims:')
  notSettled.forEach((q) => out.push('- ' + q))
}
out.push('')
out.push('## Verified claims')
out.push('')
verified.forEach((c) => out.push('- ' + c.id + ' [' + c.kind + '] ' + c.statement + ' — ' + cite(c)))
// Everything below the verified claims is agent free text, and it is kept out
// of `citable`: `wf check cite` reads every citation-shaped token, and a
// checker's reason quoting "main.go:1-7" failed the check over text that is not
// a claim.
const citable = out.slice()
if (unsupported.length || unverified.length || malformedList.length) {
  out.push('')
  out.push('## Not in the answer')
  out.push('')
  unsupported.forEach((c) => out.push('- UNSUPPORTED ' + c.id + ' ' + c.statement + ' (cited ' + c.file + ' line ' + c.line + ') — ' + c.reason))
  unverified.forEach((c) => out.push('- UNVERIFIED ' + c.id + ' ' + c.statement + ' (cited ' + c.file + ' line ' + c.line + ')'))
  malformedList.forEach((m) => out.push('- MALFORMED CITATION ' + m))
}
out.push('')
coverage.forEach((c) => out.push('coverage ' + c.cell + ': ' + (c.dead ? 'DEAD' : c.count + ' kept — ' + c.read)))

log(spawned.length - dead.length + '/' + spawned.length + ' finder(s) came back')
if (dead.length) log('DEAD: ' + dead.join(', '))

return {
  // complete is the audit's coverage, and that its writer neither died nor
  // came back empty — not its outcome: "the code violates the spec" over every
  // cell IS complete.
  complete,
  // answered is apart from complete on purpose: an audit whose finders read
  // everything and found no claim covered its ground and still answered
  // nothing — for an audit that is no "clean" result, and the report says so.
  answered,
  report: out.join('\n'),
  // citable is the answer and its verified claims alone — what to save as
  // artifacts/answer.md for `wf check cite -require-anchor`.
  citable: citable.join('\n'),
  unsettled,
  verified: verified.map((c) => ({ id: c.id, kind: c.kind, statement: c.statement, cite: cite(c) })),
  unsupported: unsupported.length,
  unverified: unverified.map((c) => c.id),
  openQuestions,
  deadCells,
}
