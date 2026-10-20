export const meta = {
  name: 'SCRIPT_NAME',
  description: 'TODO one line: which subsystem this hunt searches and what it returns. Shown in the permission dialog.',
  whenToUse: 'TODO: run once, from the repo root, on a clean tree at COMMIT; the run changes nothing in the tree outside its reproduction worktrees.',
  phases: [
    { title: 'Find', detail: 'one finder per failure class and unit, read-only, in rounds — sonnet, effort medium', model: 'sonnet' },
    { title: 'Verify', detail: 'blockers and majors REPRODUCED in a throwaway worktree, the rest re-judged by reading — sonnet, effort medium', model: 'sonnet' },
    { title: 'Critic', detail: 'one completeness critic per round, naming the areas the next round searches — sonnet, effort high', model: 'sonnet' },
    { title: 'Merge', detail: 'groups confirmed findings that share one root cause — sonnet, effort medium', model: 'sonnet' },
  ],
}

// A HUNT run: it searches a subsystem for defects, in rounds until it stops
// finding them, and verifies the serious ones by REPRODUCTION — a failing test or command written inside a throwaway
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
// the cap: `wfdry --args '{"maxRepro":1}'` — with the default the fixture's
// serious findings, four over the default rounds, never reach it.
const argInt = (name, dflt, least) => (typeof args === 'object' && args && Number.isInteger(args[name]) && args[name] >= least) ? args[name] : dflt
const MAX_REPRO = argInt('maxRepro', 8, 0)

// The hunt runs in ROUNDS until it stops finding. On the one multi-round hunt
// on record (photowatch native/, four rounds, 391 confirmed findings), rounds
// 2 to 4 added 246 of them and 6 of the 17 majors — and none of its 2
// blockers — on that one hunt a later round bought breadth, not the worst
// defects. Round N+1 re-runs only the cells that added a confirmed finding in
// round N or died in it (that hunt re-ran every unit), plus the areas a
// completeness critic names; MAX_ROUNDS bounds it. `args.maxRounds`
// overrides it (1 is a single-pass hunt), `args.maxAgents` the ceiling and
// `args.maxExtras` the critic's share, so a dry run can walk each of them.
const MAX_ROUNDS = argInt('maxRounds', 3, 1)
const MAX_EXTRAS = argInt('maxExtras', 4, 0)
// Every spawn counts against this, under the 1000 agents a Workflow run is
// capped at — an override cannot lift it past the cap: a round that would not
// fit is not started, and a verifier past it is not spawned and is reported
// unverified.
const MAX_AGENTS = Math.min(argInt('maxAgents', 950, 1), 1000)

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

// The reading cells are narrowed AT SPAWN: the harness removes the tools named
// here from the agent's tool list before it starts — measured 2026-10-20, no
// denied call, the agent carries on and returns through its schema. READ_ONLY
// asks; this removes. Bash stays: NAV allows read-only git. `wfcheck --owns-diff`
// after the run is still the bar — an option the harness renames is a key
// ignored in silence, and only the tree says whether the property held.
// The reproducers are NOT narrowed: they write a test in their own worktree by
// design, which is what `isolation: 'worktree'` is for.
const NO_WRITE = ['Write', 'Edit', 'NotebookEdit']

const REPRO_RULES = 'REPRODUCTION RULES\n' +
  'You run in an isolated git worktree, never in ' + REPO + '. First: `' + GIT + ' checkout --detach ' + COMMIT + '` and confirm `' + GIT + ' rev-parse HEAD` prints ' + COMMIT + ' — a worktree can arrive at another commit, and a reproduction against the wrong one proves nothing. Call git ONLY as ' + GIT + '. You may write a test or a script inside the worktree; never commit, never push, never write outside the worktree.\n' +
  'Run it with: ' + REPRO_HOW + '\n'

// spawn is the one wrapper every agent() call goes through. An unhandled throw
// at a serial step aborts the whole run, so a failed agent becomes null here
// and the caller decides what a null means. It is also where MAX_AGENTS holds:
// a call past it is not made, and its label is kept so the report can say why
// that agent's answer is missing.
let spawnCount = 0
const overCeiling = []
async function spawn(prompt, opts) {
  if (spawnCount >= MAX_AGENTS) {
    overCeiling.push(opts && opts.label ? opts.label : '?')
    return null
  }
  spawnCount++
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
          file: { type: 'string', description: 'repo-relative path of the primary location', examples: ['internal/store/save.go', 'cmd/app/main.go'] },
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

// The critic's areas are the PASS shape — a critic naming nothing is a hunt
// that has run out of places to look — which the `$comment` says to a dry run.
const CRITIC_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['areas'],
  properties: {
    areas: {
      type: 'array',
      $comment: 'x-pass: true',
      description: 'at most ' + MAX_EXTRAS + ' under-examined areas, most promising first; an empty list if every area was searched closely',
      items: {
        type: 'object',
        additionalProperties: false,
        required: ['name', 'scope', 'ask'],
        properties: {
          name: { type: 'string', description: 'two or three words' },
          scope: { type: 'string', description: 'repo-relative files or directories a finder reads in full' },
          ask: { type: 'string', description: 'the failure class to look for there, two sentences' },
        },
      },
    },
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

// A cell's earlier findings ride in its later prompts, so a re-run looks past
// them instead of re-reporting them a line away — which the dedup key would
// not catch and a verifier would pay for.
function finderPrompt(c, round) {
  const prior = c.mine.length
    ? '\nROUND ' + round + '. This scope and class were searched before, and these were reported — do NOT report them again; look for what they missed:\n' + c.mine.map((f) => '- ' + f.file + ':' + f.line + ' ' + f.summary).join('\n') + '\n'
    : ''
  return 'You are one finder of a bug hunt over ' + REPO + ' at commit ' + COMMIT + '. Find defects that a user or operator would hit.\n\n' +
    'SCOPE: ' + c.unit.scope + '\n' +
    'FAILURE CLASS: ' + c.lens.ask + '\n' +
    'Report only what this class finds. A defect of another class is another finder\'s; a defect outside the scope is out of scope unless code in the scope makes it reachable.\n' +
    prior + '\n' +
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

function criticPrompt(round) {
  const cov = cells.filter((c) => c.runs > 0).map((c) => '- ' + c.key + ': ' + (c.reported ? c.count + ' finding(s); read: ' + c.reads.join('; ') + (c.gaps.length ? '; NOT read: ' + c.gaps.join(', ') : '') : 'DEAD, never reported')).join('\n')
  const conf = results.filter((r) => r.status === 'reproduced' || r.status === 'confirmed').map((r) => '- ' + r.file + ':' + r.line + ' ' + r.summary).join('\n')
  return 'You are the completeness critic of a bug hunt over ' + REPO + ' at commit ' + COMMIT + '. Round ' + round + ' just finished. Name the areas of the subsystem that NOBODY examined closely enough — files no finder read, seams between two units, a failure class no lens covers where this code is exposed to it.\n\n' +
    'WHAT EACH FINDER CELL COVERED:\n' + cov + '\n\n' +
    'CONFIRMED SO FAR:\n' + (conf || '(none)') + '\n\n' +
    'Each area you name becomes one finder in the next round, so name a scope one finder can read in full and an ask it can act on. Do not name an area already covered closely: an extra finder over searched ground costs an agent and finds duplicates.\n\n' +
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
// Rounds. Round 1 runs every cell. A cell runs again in the next round only if
// it added a confirmed finding in this one or died in it, and the completeness
// critic adds up to MAX_EXTRAS cells over the areas it judges under-examined.
// The hunt stops when no cell is left to run, at MAX_ROUNDS, or when the next
// round would not fit under MAX_AGENTS or the token budget — and the report
// says which. A finder that returns nothing is retried once; a cell that never
// reported in any round is a COVERAGE hole, and the report names it.
// ---------------------------------------------------------------------------

const cells = []
function newCell(lens, unit, round) {
  const c = { key: lens.id + ' x ' + unit.id, lens, unit, from: round, runs: 0, reported: false, diedLast: false, diedIn: [], count: 0, reads: [], gaps: [], mine: [] }
  cells.push(c)
  return c
}
LENSES.forEach((lens) => UNITS.forEach((unit) => newCell(lens, unit, 1)))

const found = []
const results = []
const capped = []
const seen = new Set()
const seenLoc = new Set()
const truncations = []
const deadCritics = []
const roundStats = []
let nextId = 1
let malformed = 0
let reproUsed = 0
let rounds = 0
let stoppedBy = 'dry'
let criticDied = false
let lastRoundTokens = 0
let active = cells.slice()

// Verify one round's fresh findings. Blockers and majors are REPRODUCED, worst
// first, while the run has reproductions left under MAX_REPRO; everything else
// is re-judged by reading. The verifier's severity is the one reported.
// Reproducers and readers both carry a `verify` segment in their label, so a
// dry run's `dead-reviews` kills them both.
async function verifyRound(fresh) {
  const ordered = fresh.slice().sort((a, b) => rank(a.severity) - rank(b.severity))
  const plan = ordered.map((f) => {
    const serious = rank(f.severity) <= 1
    if (serious && reproUsed >= MAX_REPRO) {
      capped.push(f.id)
      return { f, repro: false }
    }
    if (serious) reproUsed++
    return { f, repro: serious }
  })
  const verdicts = new Map((await parallel(plan.map((p) => async () => ({
    id: p.f.id,
    repro: p.repro,
    v: p.repro
      ? await spawn(reproPrompt(p.f), { label: 'verify:repro:' + p.f.id, phase: 'Verify', model: 'sonnet', effort: 'medium', isolation: 'worktree', schema: REPRO_SCHEMA })
      : await spawn(readerPrompt(p.f), { label: 'verify:read:' + p.f.id, phase: 'Verify', model: 'sonnet', effort: 'medium', disallowedTools: NO_WRITE, schema: VERDICT_SCHEMA }),
  })))).filter(Boolean).map((x) => [x.id, x]))
  const out = []
  fresh.forEach((f) => {
    const x = verdicts.get(f.id)
    const v = x && x.v
    if (!v) {
      const why = overCeiling.includes((x && x.repro ? 'verify:repro:' : 'verify:read:') + f.id) ? 'not spawned: the run reached MAX_AGENTS' : 'the verifier returned nothing'
      out.push(Object.assign({}, f, { status: 'unverified', finalSeverity: f.severity, reason: why }))
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
    out.push(Object.assign({}, f, { status, finalSeverity: sev, reason, command, head: x.repro ? String(v.head || '') : '', output: x.repro ? String(v.output || '') : '', capped: capped.includes(f.id) }))
  })
  return out
}

for (let round = 1; round <= MAX_ROUNDS; round++) {
  // Nothing left to run: dry — unless the critic that would have named new
  // areas died, and then whether anything was left is unknown.
  if (!active.length) {
    stoppedBy = criticDied ? 'critic' : 'dry'
    break
  }
  // A round is started only if all of its finders, their retries and its
  // critic (none in the last round, or with MAX_EXTRAS 0) fit under the
  // ceiling; its verifiers are refused one by one at the ceiling and reported
  // unverified, never silently dropped.
  const need = active.length * 2 + (round < MAX_ROUNDS && MAX_EXTRAS > 0 ? 1 : 0)
  if (MAX_AGENTS - spawnCount < need) {
    stoppedBy = 'agents'
    truncations.push('round ' + round + ' not run: ' + (MAX_AGENTS - spawnCount) + ' agent(s) left under MAX_AGENTS ' + MAX_AGENTS + ', the round needs up to ' + need)
    break
  }
  // The token budget, when the run has one: the next round is assumed to cost
  // what the last one did.
  if (budget.total && lastRoundTokens > 0 && budget.remaining() < lastRoundTokens) {
    stoppedBy = 'tokens'
    // remaining() stops at zero, so an overspent round reads as "at least".
    truncations.push('round ' + round + ' not run: ' + Math.round(budget.remaining()) + ' token(s) left, the last round spent ' + (budget.remaining() > 0 ? '' : 'at least ') + Math.round(lastRoundTokens))
    break
  }
  const before = budget.total ? budget.remaining() : 0
  rounds = round

  phase('Find')
  // Results are matched back to their cell by KEY, not by position, so the
  // `.filter(Boolean)` a thunk that throws needs cannot shift one finder's
  // report onto another's cell — a cell with no report is a dead cell either way.
  const reports = new Map((await parallel(active.map((c) => async () => {
    const label = 'find:r' + round + ':' + c.lens.id + ':' + c.unit.id
    const first = await spawn(finderPrompt(c, round), { label, phase: 'Find', model: 'sonnet', effort: 'medium', disallowedTools: NO_WRITE, schema: FINDINGS_SCHEMA })
    if (first) return { key: c.key, r: live(label, first) }
    const retry = await spawn(finderPrompt(c, round), { label: label + ':retry', phase: 'Find', model: 'sonnet', effort: 'medium', disallowedTools: NO_WRITE, schema: FINDINGS_SCHEMA })
    return { key: c.key, r: live(label + ':retry', retry) }
  }))).filter(Boolean).map((x) => [x.key, x.r]))

  const fresh = []
  active.forEach((c) => {
    c.runs++
    const r = reports.get(c.key)
    c.diedLast = !r
    if (!r) {
      c.diedIn.push(round)
      return
    }
    // A gap stands only while NO run of the cell read the file: a re-run told
    // to look past its earlier findings reads less than the whole scope, and
    // replacing the gaps with its list made every multi-round hunt incomplete.
    const gaps = Array.isArray(r.gaps) ? r.gaps.filter((g) => typeof g === 'string' && g) : []
    c.gaps = c.reported ? c.gaps.filter((g) => gaps.includes(g)) : gaps
    c.reads.push((c.runs > 1 ? 'round ' + round + ': ' : '') + String(r.coverage || ''))
    c.reported = true
    if (!Array.isArray(r.findings)) malformed++
    ;(Array.isArray(r.findings) ? r.findings : []).forEach((f) => {
      if (!f || typeof f.file !== 'string') {
        malformed++
        return
      }
      // One lens may report a location once, in any round; two lenses at one
      // line are two claims until the merge says they share a cause — a
      // `file:line` key alone dropped the second lens's defect before anything
      // verified it.
      // A critic's cell has no failure class of its own, so a location anyone
      // already reported is not a new claim from it: re-verifying it spent a
      // reproduction on a finding already filed.
      const key = f.file + ':' + f.line + ':' + c.lens.id
      const loc = f.file + ':' + f.line
      if (seen.has(key) || (c.unit.id === 'extra' && seenLoc.has(loc))) return
      seen.add(key)
      seenLoc.add(loc)
      const kept = Object.assign({}, f, { id: 'F' + String(nextId++).padStart(3, '0'), lens: c.lens.id, cell: c.key, round })
      found.push(kept)
      fresh.push(kept)
      c.mine.push(kept)
      c.count++
    })
  })

  let verified = []
  if (fresh.length) {
    phase('Verify')
    verified = await verifyRound(fresh)
    verified.forEach((x) => results.push(x))
  }
  const productive = new Set(verified.filter((x) => x.status === 'reproduced' || x.status === 'confirmed').map((x) => x.cell))
  roundStats.push({ round, finders: active.length, fresh: fresh.length, confirmed: verified.filter((x) => x.status === 'reproduced' || x.status === 'confirmed').length })
  log('round ' + round + ': ' + active.length + ' finder cell(s), ' + fresh.length + ' new finding(s), ' + productive.size + ' cell(s) with a confirmed one')
  const next = active.filter((c) => c.diedLast || productive.has(c.key))

  // At the ceiling nothing more can be spawned: the verifiers it refused are
  // already unverified, and a critic spawned now would be refused and read
  // as a dead one.
  if (spawnCount >= MAX_AGENTS) {
    stoppedBy = 'agents'
    truncations.push('MAX_AGENTS ' + MAX_AGENTS + ' reached during round ' + round + (overCeiling.length ? ': ' + overCeiling.length + ' agent(s) not spawned' : ''))
    break
  }
  // At the limit the critic is not asked, so whether anything was left to
  // search is unknown — the stop is the limit's, never "ran dry".
  if (round === MAX_ROUNDS) {
    stoppedBy = 'maxRounds'
    break
  }
  // With MAX_EXTRAS 0 the critic's areas would all be cut: it is not asked.
  if (MAX_EXTRAS === 0) {
    active = next
    lastRoundTokens = budget.total ? before - budget.remaining() : 0
    continue
  }

  // The completeness critic reads what every cell covered and what was
  // confirmed, and names the areas nobody examined closely. A dead critic
  // costs the next round its extra cells, not a declared cell, so it is
  // reported apart and does not make the hunt incomplete.
  phase('Critic')
  const crit = await spawn(criticPrompt(round), { label: 'critic:r' + round, phase: 'Critic', model: 'sonnet', effort: 'high', disallowedTools: NO_WRITE, schema: CRITIC_SCHEMA })
  criticDied = !crit
  if (!crit) deadCritics.push(round)
  // An area with no scope or no ask would spawn a finder told to search
  // nothing, and a critic naming more than MAX_EXTRAS would widen the next
  // round past its share. A dry run's stub fills both fields and names one
  // area, so this filter and the cut below are for a live critic, and no
  // fixture walks either.
  const areas = (crit && Array.isArray(crit.areas) ? crit.areas : []).filter((a) => a && typeof a.scope === 'string' && a.scope.trim() && typeof a.ask === 'string' && a.ask.trim())
  if (areas.length > MAX_EXTRAS) truncations.push('the round ' + round + ' critic named ' + areas.length + ' area(s); only the first ' + MAX_EXTRAS + ' were searched')
  areas.slice(0, MAX_EXTRAS).forEach((a, i) => {
    const c = newCell({ id: 'x' + (round + 1) + '-' + (i + 1), ask: a.ask }, { id: 'extra', scope: a.scope }, round + 1)
    c.name = String(a.name || '')
    next.push(c)
  })
  active = next
  lastRoundTokens = budget.total ? before - budget.remaining() : 0
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
  const m = await spawn(mergePrompt(confirmed), { label: 'merge', phase: 'Merge', model: 'sonnet', effort: 'medium', disallowedTools: NO_WRITE, schema: MERGE_SCHEMA })
  if (!m) mergeDied = overCeiling.includes('merge') ? 'the merge was not spawned (MAX_AGENTS reached)' : 'the merge agent died'
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

const STOPS = {
  dry: 'running dry — no cell was left to run: none added a confirmed finding or died, and the critic added no area',
  critic: 'a dead critic — no cell added a confirmed finding or died, and the critic that would name new areas returned nothing, so whether any area was left is unknown',
  maxRounds: 'the round limit MAX_ROUNDS ' + MAX_ROUNDS,
  agents: 'the agent ceiling MAX_AGENTS ' + MAX_AGENTS,
  tokens: 'the token budget',
}
// A cell the critic proposed for a round that never started was never run: it
// is listed as unsearched, not dead — the stop is already said, and a budget
// stop is not a coverage hole in what was declared.
// A DECLARED cell that never ran — a ceiling too low for round 1 — is a hole
// like a dead one: "0 of 0 cells" must not read as a clean hunt.
const deadCells = cells.filter((c) => (c.runs > 0 || c.from === 1) && !c.reported).map((c) => c.key + (c.runs ? '' : ' (never run)'))
const unrun = cells.filter((c) => c.runs === 0 && c.from > 1).map((c) => c.key + (c.name ? ' "' + c.name + '"' : ''))
const gapCells = cells.filter((c) => c.reported && c.gaps.length).map((c) => c.key + ' (' + c.gaps.join(', ') + ')')
const complete = deadCells.length === 0 && gapCells.length === 0 && unverified.length === 0
const out = []
out.push('Bug hunt over ' + REPO + ' at ' + COMMIT + ': ' + (cells.length - unrun.length - deadCells.length) + ' of ' + (cells.length - unrun.length) + ' finder cell(s) reported' + (deadCells.length ? ' — NOT COVERED: ' + deadCells.join(', ') : '') + '.')
out.push(rounds + ' round(s); stopped by ' + STOPS[stoppedBy] + '.' + roundStats.map((s) => ' Round ' + s.round + ': ' + s.finders + ' finder(s), ' + s.fresh + ' new, ' + s.confirmed + ' confirmed.').join(''))
if (gapCells.length) out.push('Files a finder did not read: ' + gapCells.join('; ') + '.')
out.push(found.length + ' finding(s) after dedup: ' + results.filter((r) => r.status === 'reproduced').length + ' reproduced, ' + results.filter((r) => r.status === 'confirmed').length + ' confirmed by reading, ' + notReproduced.length + ' not reproduced, ' + refuted.length + ' refuted, ' + unverified.length + ' unverified' + (capped.length ? ', ' + capped.length + ' serious finding(s) read rather than reproduced (MAX_REPRO: ' + capped.join(', ') + ')' : '') + (malformed ? ', ' + malformed + ' malformed report(s) dropped' : '') + '.')
if (!complete) out.push('INCOMPLETE: ' + [deadCells.length ? deadCells.length + ' dead cell(s)' : '', gapCells.length ? gapCells.length + ' cell(s) with unread files' : '', unverified.length ? unverified.length + ' unverified finding(s)' : ''].filter(Boolean).join(', ') + ' — what follows is not a clean hunt of the whole subsystem.')
if (deadCritics.length) out.push('The completeness critic died after round(s) ' + deadCritics.join(', ') + ': no extra cells came from it.')
const diedReruns = cells.filter((c) => c.reported && c.diedIn.length).map((c) => c.key + ' (round ' + c.diedIn.join(', ') + ')')
if (diedReruns.length) out.push('Runs that died, of cells that reported in another round: ' + diedReruns.join(', ') + '.')
truncations.forEach((t) => out.push('Truncated: ' + t + '.'))
if (unrun.length) out.push('Proposed by the critic and never searched, because the hunt stopped first: ' + unrun.join(', ') + '.')
if (!confirmed.length) out.push(found.length ? 'No finding was reproduced or confirmed.' : rounds ? 'Nothing was found over the cells listed below.' : 'No round ran, so nothing was searched.')
out.push('')
confirmed.forEach((f) => out.push('- [' + f.finalSeverity + '] ' + f.id + ' ' + f.file + ':' + f.line + ' — ' + f.summary + ' (round ' + f.round + ', ' + f.cell + ')\n  trigger: ' + f.failure + '\n  fix: ' + f.fix + (f.status === 'reproduced' ? '\n  reproduced at ' + f.head + ': `' + f.command + '`\n    ' + f.output.split('\n').join('\n    ') : '\n  confirmed by reading' + (f.capped ? ' (over MAX_REPRO, not reproduced)' : '') + ': ' + f.reason)))
groups.forEach((g) => out.push('- shared root cause (' + g.ids.join(', ') + '): ' + g.title))
if (mergeDied) out.push('- ' + mergeDied + ': findings sharing a root cause are listed separately')
refuted.filter((f) => f.capped).forEach((f) => out.push('- REFUTED BY READING, over MAX_REPRO and never reproduced [' + f.severity + '] ' + f.id + ' ' + f.file + ':' + f.line + ' — ' + f.summary + '\n  why: ' + f.reason))
notReproduced.forEach((f) => out.push('- NOT REPRODUCED [' + f.finalSeverity + '] ' + f.id + ' ' + f.file + ':' + f.line + ' — ' + f.summary + '\n  why: ' + f.reason))
unverified.forEach((f) => out.push('- UNVERIFIED [' + f.severity + '] ' + f.id + ' ' + f.file + ':' + f.line + ' — ' + f.summary + ' (' + f.reason + ')'))
out.push('')
cells.filter((c) => c.runs > 0).forEach((c) => out.push('coverage ' + c.key + (c.name ? ' "' + c.name + '"' : '') + ' (from round ' + c.from + ', ' + c.runs + ' run(s)): ' + (c.reported ? c.count + ' kept — ' + c.reads.join('; ') : 'DEAD')))

log(spawned.length - dead.length + '/' + spawned.length + ' finder(s) came back; ' + spawnCount + ' agent(s) spawned')
if (dead.length) log('DEAD: ' + dead.join(', '))

return {
  // complete is the hunt's coverage, not its outcome: a hunt that found
  // nothing over every cell IS complete, and one that found ten while a cell
  // never reported is not. A finding nobody could reproduce does not make it
  // incomplete: it is reported apart, as unproven. Where the rounds stopped is
  // `stoppedBy`, and a budget stop is not a coverage hole — every cell that
  // was declared had its round.
  complete,
  report: out.join('\n'),
  rounds,
  stoppedBy,
  confirmed: confirmed.map((f) => ({ id: f.id, severity: f.finalSeverity, file: f.file, line: f.line, summary: f.summary, round: f.round, reproduced: f.status === 'reproduced', command: f.command })),
  notReproduced: notReproduced.map((f) => f.id),
  refuted: refuted.length,
  unverified: unverified.map((f) => f.id),
  capped,
  deadCells,
  deadCritics,
}
