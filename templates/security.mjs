export const meta = {
  name: 'SCRIPT_NAME',
  description: 'TODO one line: which subsystem (or diff) this security review searches, under which threat model, and what it returns. Shown in the permission dialog.',
  whenToUse: 'TODO: run once, from the repo root, on a clean tree at COMMIT, with `args.scan` from `wf scan`; the run changes nothing in the tree outside its PoC worktrees.',
  phases: [
    { title: 'Find', detail: 'one finder per vulnerability class and unit, one per chunk of scanner hits, then variant finders — read-only, in rounds — sonnet, effort medium', model: 'sonnet' },
    { title: 'Verify', detail: 'one refuting verifier per finding: traces the source to the sink, checks every protection, re-rates severity — sonnet, effort high', model: 'sonnet' },
    { title: 'Prove', detail: 'confirmed blockers and majors get a PoC in a throwaway worktree — sonnet, effort medium', model: 'sonnet' },
    { title: 'Merge', detail: 'groups confirmed findings that share one root cause — sonnet, effort medium', model: 'sonnet' },
  ],
}

// A SECURITY run: it searches a subsystem — or, with BASE set, the changes
// BASE...COMMIT — for vulnerabilities an attacker named by the threat model can
// exploit. Emitted by `wf new-script` in a workspace opened with
// `wf init -type security`. The method is the one the public security skills
// converge on (Trail of Bits' fp-check and variant-analysis, Anthropic's
// threat-model → vuln-scan → triage, Sentry's security-review): finders built
// for RECALL, a verifier built to REFUTE, severity re-derived by the verifier,
// and later rounds that hunt VARIANTS of what was confirmed. The PoC for what
// is serious is this harness's choice, carried over from the hunt's
// reproduction — Anthropic's triage forbids executing target code and leaves
// the PoC to a human; Strix and fp-check run one. The tree at REPO comes out of the run unchanged except for the PoC
// worktrees under `.claude/worktrees/`: after the run, `wfcheck --owns-diff
// <COMMIT> <script>` from the workspace asserts that and lists them.

// ---------------------------------------------------------------------------
// Shared facts. Keep this block SMALL: it is paid once per fan-out agent.
// ---------------------------------------------------------------------------

const REPO = 'TARGET_REPO'

// The revision under review, as a SHA: every line a finding cites is a line of
// this commit, and every PoC worktree is checked out at it.
const COMMIT = 'TODO: the commit SHA the run reviews (git rev-parse HEAD before the run)'

// Empty: the whole subsystem the units name is in scope. A SHA: a
// DIFFERENTIAL review — a finding must be introduced or exposed by a change in
// `git diff BASE...COMMIT`; the rest of the code is read for context only.
const BASE = ''

// The threat model, from the decision document — where the gate reviewed it.
// Every finder and verifier judges against it, so state what the attacker can
// NOT do as plainly as what they can: "the operator's environment variables
// and CLI flags are trusted" is what keeps a verifier from confirming an
// injection through a value only the operator sets.
const THREAT_MODEL = 'TODO: assets (what is worth stealing or breaking); the attacker (who, from where, with which access); what the attacker CANNOT do (trusted inputs: env, CLI flags, config files, other services); the trust boundaries and entry points in the units below.'

// One finder per vulnerability CLASS and unit. Disjoint classes find different
// defects; name the CWE family so a verifier and the report share one word.
// A UNIT is a slice of the subsystem BY PATH — `paths` are repo-relative
// prefixes, which is also how a scanner hit is assigned to a unit — small
// enough for one finder to read in full; together the units cover the scope.
const LENSES = [
  { id: 'injection', ask: 'attacker-controlled input reaching an interpreter: SQL, shell, template, path, deserialization, SSRF (CWE-89, CWE-78, CWE-94, CWE-22, CWE-502, CWE-918).' },
  { id: 'TODO-second-class', ask: 'TODO: a second, DISJOINT class (authentication and authorization — CWE-287, CWE-285, CWE-639; secrets and cryptography — CWE-798, CWE-327; memory safety; insecure defaults that fail open).' },
]
const UNITS = [
  { id: 'TODO-unit', paths: ['TODO/'] },
]

// What a finder must not report and a verifier must refute on sight — the
// exclusions the public triage procedures share, Anthropic's triage among
// them; add the target's own.
const DO_NOT_REPORT = 'volumetric denial of service (flooding, many requests) and missing rate limits — a ReDoS, an algorithmic-complexity blow-up or unbounded recursion one request triggers IS reportable; missing hardening with no concrete exploit; an outdated dependency with no reachable vulnerable call (the dependency scanner covers those); code that only runs in tests, examples or docs; theoretical races with no reachable window; a value the threat model says is trusted; an impact the precondition already grants (if the attacker must already be an admin to do it, it is not an admin-escalation).'

const SEVERITY = 'blocker = exploitable by the attacker the threat model names, with the access they have, with a concrete impact on a named asset (code execution, authentication bypass, data read or written across a trust boundary, a secret exposed); major = exploitable, but needing a precondition the attacker plausibly obtains, or with a bounded impact; minor = a weakness whose exploit needs preconditions that already grant most of its impact, or a missing defence in depth; note = not a vulnerability. An open question is not a note: a finding that cannot state its source, sink and the path between them says which observation would settle it.'

// Scanner output is CANDIDATES, never findings. The scanners run BEFORE the
// launch, by the operator — `semgrep --metrics=off --sarif`, `betterleaks`
// with a SARIF report, `trivy fs --format sarif` — into the workspace's
// `artifacts/scan/`, and `wf scan` folds those files into the JSON handed to
// this run as `args.scan`. Name here every scanner the decision document
// says was run: one missing from `args.scan`, or one that reported a failed
// execution, is NAMED in the report and makes the run incomplete — an absent
// scan never reads as a clean one. An empty list runs no scanner and says so.
const SCANNERS = []

// What a PoC is RUN with here, handed to every prover. A memory-heavy command
// run by several provers at once is what an OOM killer resolves, so say "one
// package".
const REPRO_HOW = 'TODO: how one focused test or command is run in this repo, e.g. `go test ./internal/x -run TestY -count=1`'

const argInt = (name, dflt, least) => (typeof args === 'object' && args && Number.isInteger(args[name]) && args[name] >= least) ? args[name] : dflt
// PoCs are builds, one per worktree: the costliest agents of the run. Only
// CONFIRMED blockers and majors get one, worst first, up to this cap; the rest
// stand on the verifier's reading and the report says which. `args.maxRepro`
// overrides it, so a dry run can walk the cap.
const MAX_REPRO = argInt('maxRepro', 6, 0)
// Round 1 runs every declared cell and every chunk of scanner hits. Round N+1
// runs a VARIANT finder for each finding first confirmed in round N, worst
// first, up to MAX_VARIANTS, plus every cell that died — and stops when there
// is none. `args.maxRounds`, `args.maxVariants` and `args.maxAgents` override.
const MAX_ROUNDS = argInt('maxRounds', 3, 1)
const MAX_VARIANTS = argInt('maxVariants', 4, 0)
// Every spawn counts against this, under the 1000 agents a Workflow run is
// capped at — an override cannot lift it past the cap.
const MAX_AGENTS = Math.min(argInt('maxAgents', 950, 1), 1000)

// A worktree-isolated agent calls git by its ABSOLUTE PATH: a PreToolUse hook
// that rewrites `git …` into a wrapper is refused inside a worktree
// (`wf docs authoring`, the worktree rule). `wf new-script` writes the path
// git resolves to on the box it runs on.
const GIT = 'GIT_PATH'

// ---------------------------------------------------------------------------
// Harness. Everything below this line is the same in every security script;
// the constants above and the prompts' asks are yours.
// ---------------------------------------------------------------------------

const NAV = 'NAVIGATION\n' +
  'Read files with Read, search with Grep and Glob. Reserve Bash for read-only git and for running a PoC: a `cat` or a `sed -n` where Read would do is a round-trip that carries your entire context to fetch one file.\n'

const READ_ONLY = 'READ-ONLY\n' +
  'You change nothing: no Edit, no Write, no command that writes a file, no git command that moves HEAD, the index or a ref. The repository is ' + REPO + ', at commit ' + COMMIT + '.\n'

// A report that quotes the secret it found has leaked it a second time, into a
// document that is shared. After the run, `betterleaks` over the saved report
// is the check that this held.
const SECRETS = 'SECRETS\nNever copy a secret value — a key, token, password, private key or connection string — into any field. Name the file:line and the KIND of secret; that is the whole finding.\n'

const SCOPE_RULE = BASE
  ? 'DIFFERENTIAL REVIEW: report only what a change in `git diff ' + BASE + '...' + COMMIT + '` introduces or exposes; read the rest of the code for context, and report nothing that was already exploitable at ' + BASE + '.\n'
  : ''

const REPRO_RULES = 'PoC RULES\n' +
  'You run in an isolated git worktree, never in ' + REPO + '. First: `' + GIT + ' checkout --detach ' + COMMIT + '` and confirm `' + GIT + ' rev-parse HEAD` prints ' + COMMIT + ' — a worktree can arrive at another commit, and a PoC against the wrong one proves nothing. Call git ONLY as ' + GIT + '. You may write a test or a script inside the worktree; never commit, never push, never write outside the worktree. A PoC runs against this code on this machine only: no traffic beyond the loopback interface, no real credentials, no third-party service.\n' +
  'Run it with: ' + REPRO_HOW + '\n'

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

const spawned = []
const dead = []
function live(label, result) {
  spawned.push(label)
  if (!result) dead.push(label)
  return result
}

// The scanner intake. `args.scan` is what `wf scan` wrote: per scanner whether
// its execution succeeded and how many hits it had, and the hits themselves.
// Only DECLARED scanners are read; one supplied but not declared is named and
// ignored, so a scan nobody put in the decision document cannot widen the run.
const SCAN = typeof args === 'object' && args && args.scan && typeof args.scan === 'object' ? args.scan : null
const scanTools = SCAN && Array.isArray(SCAN.tools) ? SCAN.tools.filter((t) => t && typeof t.tool === 'string') : []
const scanGaps = []
// A scanner listed twice is read as failed if ANY entry says so.
const toolOf = (name) => {
  const all = scanTools.filter((x) => x.tool === name)
  return all.length ? Object.assign({}, all[0], { ok: all.every((x) => x.ok === true) }) : null
}
SCANNERS.forEach((name) => {
  const t = toolOf(name)
  if (!t) scanGaps.push(name + ': NOT SUPPLIED in args.scan')
  else if (t.ok !== true) scanGaps.push(name + ': its execution FAILED' + (t.note ? ' (' + String(t.note) + ')' : ''))
  // A hit with no file location, or naming no file under the target, is
  // assigned to no unit and adjudicated by nobody: a hole in the scan's
  // coverage, named like one.
  if (t && Number.isInteger(t.dropped) && t.dropped > 0) scanGaps.push(name + ': ' + t.dropped + ' hit(s) with no file location, never adjudicated')
  if (t && Number.isInteger(t.unresolved) && t.unresolved > 0) scanGaps.push(name + ': ' + t.unresolved + ' hit(s) naming no file under the target — re-run it from the target\'s root')
})
const undeclared = scanTools.filter((t) => !SCANNERS.includes(t.tool)).map((t) => t.tool)
const hits = (SCAN && Array.isArray(SCAN.candidates) ? SCAN.candidates : [])
  .filter((h) => h && typeof h.id === 'string' && typeof h.file === 'string' && SCANNERS.includes(h.tool))
// `./a/`, `a/` and `a` name one directory, and `.`, `./` or '' the whole
// repository: read literally, each left every hit outside every unit.
const normPath = (p) => String(p).replace(/^(\.\/)+/, '').replace(/\/+$/, '').replace(/^\.$/, '')
const inUnit = (file, unit) => unit.paths.some((p) => {
  const f = normPath(file)
  const u = normPath(p)
  return u === '' || f === u || f.startsWith(u + '/')
})
const outOfScope = hits.filter((h) => !UNITS.some((u) => inUnit(h.file, u)))

// A finder sees at most this many scanner hits: one finder handed hundreds
// adjudicates the first few and waves the rest through.
const SCAN_CHUNK = 20

const FINDING_ITEM = {
  type: 'object',
  additionalProperties: false,
  required: ['severity', 'cwe', 'file', 'line', 'summary', 'source', 'sink', 'exploit', 'fix'],
  properties: {
    severity: { type: 'string', enum: ['blocker', 'major', 'minor', 'note'] },
    cwe: { type: 'string', description: 'the CWE id, e.g. CWE-89' },
    file: { type: 'string', description: 'repo-relative path of the SINK', examples: ['internal/api/users.go', 'cmd/app/main.go'] },
    line: { type: 'integer', minimum: 1, description: 'line of the sink at ' + COMMIT },
    summary: { type: 'string', description: 'one sentence stating the vulnerability' },
    source: { type: 'string', description: 'where the attacker-controlled value enters, file:line, and why the threat model says the attacker controls it' },
    sink: { type: 'string', description: 'what it reaches and why that is dangerous' },
    exploit: { type: 'string', description: 'the attacker, the input they send, and the outcome — concrete, not a restatement of the summary' },
    fix: { type: 'string', description: 'one line naming the smallest correct fix' },
  },
}

const FINDINGS_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['coverage', 'gaps', 'findings'],
  properties: {
    coverage: { type: 'string', description: 'two sentences: which files you actually read' },
    gaps: { type: 'array', items: { type: 'string' }, description: 'repo-relative paths in your scope that you did NOT read; an empty list only if you read all of it' },
    findings: { type: 'array', items: FINDING_ITEM },
  },
}

// A scanner chunk's schema names its hits by ENUM, so a finder can only
// account for a hit it was handed: every id must come back once, as the
// `scan` of a finding or in `dismissed` with a reason.
// Every hit id is a REQUIRED property of `hits`, so the schema itself makes the
// finder answer each one — an array of dismissals let a finder answer the first
// and stop, and a dry run synthesised one entry for a chunk of many, which
// read as unadjudicated hits on the unmodified skeleton. The leaf is a `$ref`,
// so twenty hits cost twenty short pointers, not twenty copies.
function scanSchema(ids) {
  const item = JSON.parse(JSON.stringify(FINDING_ITEM))
  item.required.push('scan')
  item.properties.scan = { type: 'string', enum: ['none'].concat(ids), description: 'the scanner hit this finding confirms, or none' }
  const hits = {}
  ids.forEach((id) => { hits[id] = { $ref: '#/$defs/hit' } })
  return {
    type: 'object',
    additionalProperties: false,
    required: ['coverage', 'gaps', 'findings', 'hits'],
    $defs: {
      hit: {
        type: 'object',
        additionalProperties: false,
        required: ['kind', 'why'],
        properties: {
          kind: { type: 'string', enum: ['dismissed', 'vulnerable'], description: 'vulnerable only if a finding in `findings` carries this hit as its `scan`' },
          why: { type: 'string', description: 'one sentence: for a dismissal, the protection, the trusted source or the exclusion that rules it out, with file:line' },
        },
      },
    },
    properties: {
      coverage: FINDINGS_SCHEMA.properties.coverage,
      gaps: FINDINGS_SCHEMA.properties.gaps,
      findings: { type: 'array', items: item },
      hits: { type: 'object', additionalProperties: false, required: ids, properties: hits, description: 'one verdict per scanner hit, by its id' },
    },
  }
}

// The verdict has THREE states. `cannot-verify` is neither a confirmation nor a
// refutation — the code needed to decide is outside the repository, or the
// decision turns on deployment — and it is listed apart for a human, never
// dropped and never counted as confirmed.
const VERDICT_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['verdict', 'severity', 'cwe', 'trace', 'reason'],
  properties: {
    verdict: { type: 'string', enum: ['true-positive', 'false-positive', 'cannot-verify'] },
    severity: { type: 'string', enum: ['blocker', 'major', 'minor', 'note'], description: 'your own rating from the attacker\'s preconditions and access, whatever the finder said' },
    cwe: { type: 'string', description: 'the CWE id, corrected if the finder named the wrong one' },
    trace: { type: 'string', description: 'the path from the entry point to the sink as file:line hops, the FIRST hop being the call site where the attacker\'s input enters; empty if you could not complete it' },
    reason: { type: 'string', description: 'two to four sentences: what you checked — each protection on the path and whether it holds on every path — and what decided it, with file:line' },
  },
}

const REPRO_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['verified', 'onCommit', 'head', 'command', 'output', 'reason'],
  properties: {
    verified: { type: 'boolean', description: 'true only if your PoC demonstrated the exploit: a command whose output shows the attacker\'s input reaching the sink with the stated effect' },
    onCommit: { type: 'boolean', $comment: 'x-pass: true', description: 'true only if git rev-parse HEAD in your worktree printed the commit you were given, before you ran anything' },
    head: { type: 'string', description: 'what git rev-parse HEAD printed in your worktree' },
    command: { type: 'string', description: 'the exact command you ran, with the test or script it runs' },
    output: { type: 'string', description: 'the last lines of its output that show the exploit, or show it failing' },
    reason: { type: 'string', description: 'two to four sentences: what the PoC shows, and if it did not work, why — an unproven exploit is not a refuted one' },
  },
}

const MERGE_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['groups'],
  properties: {
    groups: {
      type: 'array',
      description: 'only groups of two or more findings that share one root cause — fixing one fixes the others; an empty list is legitimate',
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
const allPaths = UNITS.map((u) => u.paths.join(', ')).join('; ')

function finding(f) {
  return 'FINDING ' + f.id + ' [' + f.severity + ', ' + f.cwe + '] ' + f.file + ':' + f.line + '\n' + f.summary +
    '\nSource: ' + f.source + '\nSink: ' + f.sink + '\nExploit: ' + f.exploit + '\n\n'
}

const BRIEF = 'THREAT MODEL: ' + THREAT_MODEL + '\n' + SCOPE_RULE + 'DO NOT REPORT: ' + DO_NOT_REPORT + '\nSEVERITY: ' + SEVERITY + '\n\n'

function finderPrompt(c, round) {
  const prior = c.mine.length
    ? '\nROUND ' + round + '. This scope and class were searched before, and these were reported — do NOT report them again; look for what they missed:\n' + c.mine.map((f) => '- ' + f.file + ':' + f.line + ' ' + f.summary).join('\n') + '\n'
    : ''
  if (c.lens.id === 'scan') {
    return 'You are one finder of a security review over ' + REPO + ' at commit ' + COMMIT + '. A static scanner reported the hits below in your scope. Scanners over-report: ADJUDICATE each one by reading the code. A hit that is a real vulnerability under the threat model becomes a finding whose `scan` is its id, and its entry in `hits` says vulnerable; every other hit is dismissed there, with the reason. Every id has its entry in `hits`.\n\n' +
      'SCOPE: ' + c.unit.paths.join(', ') + '\nHITS:\n' + c.hits.map((h) => '- ' + h.id + ' ' + h.tool + ' ' + h.rule + ' ' + h.file + ':' + h.line + ' — ' + h.message).join('\n') + '\n\n' +
      BRIEF + READ_ONLY + SECRETS + NAV +
      '\nReturn through the StructuredOutput schema. List in gaps every file a hit cites that you did not read.'
  }
  if (c.lens.id === 'variant') {
    return 'You are a variant finder of a security review over ' + REPO + ' at commit ' + COMMIT + '. The vulnerability below was CONFIRMED. Find OTHER places where the same root cause occurs. Start from the exact pattern of the confirmed case, confirm it matches that case, then generalise one element at a time, reading every match; stop generalising when most matches are noise. Report only locations OTHER than the confirmed one, each as its own finding.\n\n' +
      c.seed + 'SCOPE: ' + allPaths + '\n\n' +
      BRIEF + READ_ONLY + SECRETS + NAV +
      '\nReturn through the StructuredOutput schema. An empty findings list is a legitimate answer; say in coverage which patterns you searched for.'
  }
  return 'You are one finder of a security review over ' + REPO + ' at commit ' + COMMIT + '. Find vulnerabilities an attacker named by the threat model can exploit. Report anything with a plausible exploit path — a verifier will try to refute each one, so recall matters more here than certainty — but every finding names its source, its sink and the path between them.\n\n' +
    'SCOPE: ' + c.unit.paths.join(', ') + '\n' +
    'VULNERABILITY CLASS: ' + c.lens.ask + '\n' +
    'Report only what this class finds; other classes are other finders\'. A sink outside the scope is in scope when the source is inside it.\n' +
    prior + '\n' + BRIEF + READ_ONLY + SECRETS + NAV +
    '\nReturn through the StructuredOutput schema. An empty findings list is a legitimate answer; say in coverage what you read to reach it, and list in gaps every file in your scope you did not read.'
}

function verifierPrompt(f) {
  return 'You are a verifier of a security review over ' + REPO + ' at commit ' + COMMIT + '. A finder reported the vulnerability below. Your default assumption is that it is WRONG: finders over-report, and models overrate severity. Restate the claim in your own words, then:\n' +
    '1. read the cited code yourself;\n' +
    '2. trace BACKWARDS from the sink to where the value enters, and name the first call site with file:line — a trace you cannot complete is not a confirmation;\n' +
    '3. decide whether the attacker the threat model names controls that value, or whether the model says it is trusted;\n' +
    '4. find every protection on the path — validation, escaping, a framework default, an authorization check — and decide whether it holds on EVERY path;\n' +
    '5. apply DO NOT REPORT; then rate severity from the preconditions the attacker needs and the access they have.\n' +
    'Return true-positive only if all of that holds; false-positive if any step refutes it; cannot-verify if the decision turns on code outside the repository or on how it is deployed — say which.\n\n' +
    finding(f) + BRIEF + READ_ONLY + SECRETS + NAV +
    '\nReturn through the StructuredOutput schema.'
}

function proverPrompt(f) {
  return 'You are a prover. A verifier of a security review over ' + REPO + ' CONFIRMED the vulnerability below by reading. PROVE it: write the smallest test or script that drives the attacker\'s input from the entry point to the sink at commit ' + COMMIT + ' and shows the stated effect, run it, and report what it printed. If it does not work, say why — an exploit you could not demonstrate is not a refuted one.\n\n' +
    finding(f) + 'Verifier\'s trace: ' + f.trace + '\n\n' +
    'THREAT MODEL: ' + THREAT_MODEL + '\n\n' + REPRO_RULES + SECRETS + NAV +
    '\nReturn through the StructuredOutput schema.'
}

function mergePrompt(list) {
  return 'These confirmed vulnerabilities come from independent finders of one security review. Group the ones that share ONE root cause — fixing one fixes the others — so the report names it once.\n\n' +
    list.map((f) => f.id + ' [' + f.cwe + '] ' + f.file + ':' + f.line + ' ' + f.summary).join('\n') + '\n\n' +
    READ_ONLY + SECRETS + NAV +
    '\nReturn through the StructuredOutput schema.'
}

// ---------------------------------------------------------------------------
// Rounds. Round 1 runs every class x unit cell and one cell per chunk of
// scanner hits. A cell that died runs again; each finding first confirmed in a
// round seeds one VARIANT cell in the next, worst first, up to MAX_VARIANTS.
// The run stops when no cell is left, at MAX_ROUNDS, or when the next round
// would not fit under MAX_AGENTS or the token budget — and the report says
// which. A finder that returns nothing is retried once; a cell that never
// reported is a COVERAGE hole, and the report names it.
// ---------------------------------------------------------------------------

const cells = []
function newCell(lens, unit, round, extra) {
  const c = Object.assign({ key: lens.id + ' x ' + unit.id, lens, unit, from: round, runs: 0, reported: false, diedLast: false, diedIn: [], count: 0, reads: [], gaps: [], mine: [] }, extra || {})
  cells.push(c)
  return c
}
// `scan` and `variant` are the harness's own cells: a lens of that name shared
// a key with them, and its finder's death read as a report. Refused loudly.
LENSES.forEach((l) => { if (l.id === 'scan' || l.id === 'variant') throw new Error('LENSES: the id ' + l.id + ' is the harness\'s own cell kind; name the class otherwise') })
LENSES.forEach((lens) => UNITS.forEach((unit) => newCell(lens, unit, 1)))
UNITS.forEach((unit) => {
  const mine = hits.filter((h) => inUnit(h.file, unit) && !UNITS.slice(0, UNITS.indexOf(unit)).some((u) => inUnit(h.file, u)))
  for (let i = 0; i < mine.length; i += SCAN_CHUNK) {
    const chunk = mine.slice(i, i + SCAN_CHUNK)
    const c = newCell({ id: 'scan', ask: 'adjudicate scanner hits' }, { id: unit.id + (mine.length > SCAN_CHUNK ? '#' + (i / SCAN_CHUNK + 1) : ''), paths: unit.paths }, 1, { hits: chunk })
    c.key = 'scan x ' + c.unit.id
  }
})
// Results are matched back to cells by key, so two cells with one key share
// one report: refused, naming the key.
cells.forEach((c, i) => { if (cells.findIndex((d) => d.key === c.key) !== i) throw new Error('two cells share the key ' + c.key + ' — make the unit ids distinct') })

// Two findings of one class in one file within this many lines are one
// finding: the deterministic half of the dedup every triage procedure runs
// before it pays a verifier. The semantic half — one root cause — is the merge.
const WINDOW = 10
const found = []
const results = []
const capped = []
const kept = []
const dupes = []
const dismissed = []
const unadjudicated = []
const truncations = []
const roundStats = []
let nextId = 1
let malformed = 0
let reproUsed = 0
let rounds = 0
let stoppedBy = 'dry'
// The last round confirmed something and MAX_VARIANTS 0 searched no variant of
// it: the run did not run dry, it was told not to look.
let variantsCut = false
let lastRoundTokens = 0
let active = cells.slice()

// A class cell's finding is a duplicate of one of its own class. A scanner or
// variant cell has no class of its own, so its finding is compared by CWE: one
// a class cell already filed is not a new claim, and a different weakness a
// few lines away is — Anthropic's triage keeps "different categories in the
// same file region" distinct.
const cweOf = (x) => String(x.cwe || '').trim().toUpperCase()
function duplicateOf(f, lensId, byCwe) {
  return kept.find((k) => k.file === f.file && Math.abs(k.line - f.line) <= WINDOW && (byCwe ? cweOf(k) === cweOf(f) : k.lens === lensId))
}

for (let round = 1; round <= MAX_ROUNDS; round++) {
  if (!active.length) {
    stoppedBy = variantsCut ? 'noVariants' : 'dry'
    break
  }
  // A round starts only if all of its finders and their retries fit; its
  // verifiers and provers are refused one by one at the ceiling and reported.
  const need = active.length * 2
  if (MAX_AGENTS - spawnCount < need) {
    stoppedBy = 'agents'
    truncations.push('round ' + round + ' not run: ' + (MAX_AGENTS - spawnCount) + ' agent(s) left under MAX_AGENTS ' + MAX_AGENTS + ', the round needs up to ' + need)
    break
  }
  if (budget.total && lastRoundTokens > 0 && budget.remaining() < lastRoundTokens) {
    stoppedBy = 'tokens'
    truncations.push('round ' + round + ' not run: ' + Math.round(budget.remaining()) + ' token(s) left, the last round spent ' + (budget.remaining() > 0 ? '' : 'at least ') + Math.round(lastRoundTokens))
    break
  }
  const before = budget.total ? budget.remaining() : 0
  rounds = round

  phase('Find')
  // Matched back to the cell by KEY: a thunk that throws must not shift one
  // finder's report onto another's cell.
  const reports = new Map((await parallel(active.map((c) => async () => {
    const label = 'find:r' + round + ':' + c.lens.id + ':' + c.unit.id
    const schema = c.hits ? scanSchema(c.hits.map((h) => h.id)) : FINDINGS_SCHEMA
    const first = await spawn(finderPrompt(c, round), { label, phase: 'Find', model: 'sonnet', effort: 'medium', schema })
    if (first) return { key: c.key, r: live(label, first) }
    const retry = await spawn(finderPrompt(c, round), { label: label + ':retry', phase: 'Find', model: 'sonnet', effort: 'medium', schema })
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
    const gaps = Array.isArray(r.gaps) ? r.gaps.filter((g) => typeof g === 'string' && g) : []
    c.gaps = c.reported ? c.gaps.filter((g) => gaps.includes(g)) : gaps
    c.reads.push((c.runs > 1 ? 'round ' + round + ': ' : '') + String(r.coverage || ''))
    c.reported = true
    if (!Array.isArray(r.findings)) malformed++
    const list = Array.isArray(r.findings) ? r.findings : []
    // Every scanner hit the cell was handed is accounted for once — as the
    // `scan` of a finding or as a dismissal — or it is listed unadjudicated,
    // and the run is incomplete: a hit nobody judged is a scan nobody read.
    // A finding carrying the hit answers it, whatever its entry says; a
    // dismissal answers it; "vulnerable" with no finding filed answers nothing.
    if (c.hits) {
      const verdicts = r.hits && typeof r.hits === 'object' ? r.hits : {}
      c.hits.forEach((h) => {
        const v = verdicts[h.id]
        if (list.some((f) => f && f.scan === h.id)) return
        if (v && v.kind === 'dismissed') dismissed.push({ hit: h, why: String(v.why || '') })
        else unadjudicated.push(Object.assign({}, h, { why: v ? 'judged vulnerable, no finding filed' : 'no verdict' }))
      })
    }
    list.forEach((f) => {
      if (!f || typeof f.file !== 'string' || !Number.isInteger(f.line)) {
        malformed++
        return
      }
      const dup = duplicateOf(f, c.lens.id, c.lens.id === 'scan' || c.lens.id === 'variant')
      if (dup) {
        dupes.push(f.file + ':' + f.line + (f.scan && f.scan !== 'none' ? ' [hit ' + f.scan + ']' : '') + ' (' + c.key + ') = ' + dup.id)
        return
      }
      const k = Object.assign({}, f, { id: 'F' + String(nextId++).padStart(3, '0'), lens: c.lens.id, cell: c.key, round })
      kept.push(k)
      found.push(k)
      fresh.push(k)
      c.mine.push(k)
      c.count++
    })
  })

  let verified = []
  if (fresh.length) {
    verified = await verifyRound(fresh)
    verified.forEach((x) => results.push(x))
  }
  const newlyConfirmed = verified.filter((x) => x.status === 'confirmed' || x.status === 'exploited').sort((a, b) => rank(a.finalSeverity) - rank(b.finalSeverity))
  roundStats.push({ round, finders: active.length, fresh: fresh.length, confirmed: newlyConfirmed.length })
  log('round ' + round + ': ' + active.length + ' finder cell(s), ' + fresh.length + ' new finding(s), ' + newlyConfirmed.length + ' confirmed')

  if (spawnCount >= MAX_AGENTS) {
    stoppedBy = 'agents'
    truncations.push('MAX_AGENTS ' + MAX_AGENTS + ' reached during round ' + round + (overCeiling.length ? ': ' + overCeiling.length + ' agent(s) not spawned' : ''))
    break
  }
  if (round === MAX_ROUNDS) {
    stoppedBy = 'maxRounds'
    if (newlyConfirmed.length && MAX_VARIANTS > 0) truncations.push(newlyConfirmed.length + ' finding(s) confirmed in the last round never had their variants searched')
    break
  }
  const next = active.filter((c) => c.diedLast)
  variantsCut = newlyConfirmed.length > 0 && MAX_VARIANTS === 0
  if (newlyConfirmed.length > MAX_VARIANTS && MAX_VARIANTS > 0) truncations.push('round ' + round + ' confirmed ' + newlyConfirmed.length + ' finding(s); only the first ' + MAX_VARIANTS + ' had their variants searched')
  newlyConfirmed.slice(0, MAX_VARIANTS).forEach((f) => {
    const c = newCell({ id: 'variant', ask: 'variants of ' + f.id }, { id: f.id, paths: [] }, round + 1, { seed: finding(f) + 'Verifier\'s trace: ' + f.trace + '\n\n' })
    c.key = 'variant x ' + f.id
    next.push(c)
  })
  active = next
  lastRoundTokens = budget.total ? before - budget.remaining() : 0
}

// Verify one round's fresh findings: every one gets a refuting verifier. A
// confirmed blocker or major then gets a PoC, worst first, while the run has
// PoCs left under MAX_REPRO. The verifier's severity is the one reported.
// Hoisted: the rounds above call it. It sits here so the phases read in
// their running order — Find, Verify, Prove, Merge.
async function verifyRound(fresh) {
  phase('Verify')
  const verdicts = new Map((await parallel(fresh.map((f) => async () => ({
    id: f.id,
    v: await spawn(verifierPrompt(f), { label: 'verify:read:' + f.id, phase: 'Verify', model: 'sonnet', effort: 'high', schema: VERDICT_SCHEMA }),
  })))).filter(Boolean).map((x) => [x.id, x.v]))
  const out = fresh.map((f) => {
    const v = verdicts.get(f.id)
    if (!v) {
      const why = overCeiling.includes('verify:read:' + f.id) ? 'not spawned: the run reached MAX_AGENTS' : 'the verifier returned nothing'
      return Object.assign({}, f, { status: 'unverified', finalSeverity: f.severity, reason: why })
    }
    const sev = LEVELS.includes(v.severity) ? v.severity : f.severity
    const trace = String(v.trace || '').trim()
    let status = v.verdict === 'true-positive' ? 'confirmed' : v.verdict === 'false-positive' ? 'refuted' : 'inconclusive'
    let reason = String(v.reason || '')
    // A confirmation without a trace from the entry point is a claim about the
    // sink alone, which is what a scanner already said.
    if (status === 'confirmed' && !trace) {
      status = 'inconclusive'
      reason = 'confirmed with no trace from the entry point. ' + reason
    }
    return Object.assign({}, f, { status, verdict: v.verdict, finalSeverity: sev, cwe: String(v.cwe || f.cwe), trace, reason })
  })
  const serious = out.filter((x) => x.status === 'confirmed' && rank(x.finalSeverity) <= 1).sort((a, b) => rank(a.finalSeverity) - rank(b.finalSeverity))
  const plan = serious.filter((x) => {
    if (reproUsed >= MAX_REPRO) {
      capped.push(x.id)
      return false
    }
    reproUsed++
    return true
  })
  if (plan.length) {
    phase('Prove')
    const pocs = new Map((await parallel(plan.map((x) => async () => ({
      id: x.id,
      v: await spawn(proverPrompt(x), { label: 'verify:repro:' + x.id, phase: 'Prove', model: 'sonnet', effort: 'medium', isolation: 'worktree', schema: REPRO_SCHEMA }),
    })))).filter(Boolean).map((p) => [p.id, p.v]))
    plan.forEach((x) => {
      const v = pocs.get(x.id)
      if (!v) {
        // A dead prover leaves the finding confirmed by reading, and the run
        // incomplete: the proof it was owed was not attempted.
        x.poc = overCeiling.includes('verify:repro:' + x.id) ? 'not spawned: the run reached MAX_AGENTS' : 'the prover returned nothing'
        x.pocDied = true
        return
      }
      const command = String(v.command || '').trim()
      const head = String(v.head || '').trim()
      // An exploit counts only with a command that can be re-run AND a
      // worktree the prover confirmed was at COMMIT — a confirmation its own
      // `head` contradicts is no confirmation.
      const headDiffers = /^[0-9a-f]{7,40}$/.test(head) && !(head.startsWith(COMMIT) || COMMIT.startsWith(head))
      if (v.verified && command && v.onCommit === true && !headDiffers) {
        x.status = 'exploited'
        Object.assign(x, { command, head: String(v.head || ''), output: String(v.output || '') })
      } else {
        x.poc = (v.verified ? (!command ? 'no command reported' : 'the worktree was not confirmed at ' + COMMIT + ' (head: ' + String(v.head || '?') + ')') + '. ' : 'not demonstrated. ') + String(v.reason || '')
      }
    })
  }
  return out
}

const confirmed = results.filter((r) => r.status === 'exploited' || r.status === 'confirmed').sort((a, b) => rank(a.finalSeverity) - rank(b.finalSeverity))
const refuted = results.filter((r) => r.status === 'refuted')
const inconclusive = results.filter((r) => r.status === 'inconclusive')
const unverified = results.filter((r) => r.status === 'unverified')
const pocDied = confirmed.filter((r) => r.pocDied)

// ---------------------------------------------------------------------------
// Merge. Only when there is something to merge; a dead merger loses grouping,
// not findings.
// ---------------------------------------------------------------------------

let groups = []
let mergeDied = false
if (confirmed.length > 1) {
  phase('Merge')
  const m = await spawn(mergePrompt(confirmed), { label: 'merge', phase: 'Merge', model: 'sonnet', effort: 'medium', schema: MERGE_SCHEMA })
  if (!m) mergeDied = overCeiling.includes('merge') ? 'the merge was not spawned (MAX_AGENTS reached)' : 'the merge agent died'
  else {
    const known = new Set(confirmed.map((f) => f.id))
    groups = (Array.isArray(m.groups) ? m.groups : [])
      .map((g) => ({ title: g && g.title, ids: g && Array.isArray(g.ids) ? g.ids.filter((id) => known.has(id)) : [] }))
      .filter((g) => g.title && g.ids.length > 1)
  }
}

// ---------------------------------------------------------------------------
// The report. Its population comes FIRST — cells, then scanners: "no
// vulnerability" over a run whose finders died or whose scanner never ran is
// not a secure subsystem, and the two must never read alike.
// ---------------------------------------------------------------------------

const STOPS = {
  dry: 'running dry — no finding was newly confirmed, so no variant was left to search, and no cell died',
  noVariants: 'MAX_VARIANTS 0 — findings were confirmed and no variant of them was searched',
  maxRounds: 'the round limit MAX_ROUNDS ' + MAX_ROUNDS,
  agents: 'the agent ceiling MAX_AGENTS ' + MAX_AGENTS,
  tokens: 'the token budget',
}
const deadCells = cells.filter((c) => (c.runs > 0 || c.from === 1) && !c.reported).map((c) => c.key + (c.runs ? '' : ' (never run)'))
const unrun = cells.filter((c) => c.runs === 0 && c.from > 1).map((c) => c.key)
const gapCells = cells.filter((c) => c.reported && c.gaps.length).map((c) => c.key + ' (' + c.gaps.join(', ') + ')')
const complete = deadCells.length === 0 && gapCells.length === 0 && unverified.length === 0 && pocDied.length === 0 && scanGaps.length === 0 && unadjudicated.length === 0
const declared = cells.filter((c) => c.from === 1).length
const out = []
out.push('Security review of ' + REPO + ' at ' + COMMIT + (BASE ? ', the changes since ' + BASE : '') + ': ' + cells.filter((c) => c.from === 1 && c.reported).length + ' of ' + declared + ' round-1 cell(s) reported' + (deadCells.length ? ' — NOT COVERED: ' + deadCells.join(', ') : '') + '.')
out.push('Scanners: ' + (SCANNERS.length ? SCANNERS.map((n) => {
  const t = toolOf(n)
  return n + ' ' + (!t ? 'NOT SUPPLIED' : t.ok !== true ? 'FAILED' : (Number.isInteger(t.results) ? t.results : '?') + ' hit(s)' + (t.invoked === false ? ' (its log records no invocation, so that it ran to the end is not recorded)' : ''))
}).join(', ') : 'none declared — no deterministic scan backs this run') + (outOfScope.length ? '; ' + outOfScope.length + ' hit(s) outside every unit, not read' : '') + (undeclared.length ? '; supplied but not declared, ignored: ' + undeclared.join(', ') : '') + '.')
out.push(rounds + ' round(s); stopped by ' + STOPS[stoppedBy] + '.' + roundStats.map((s) => ' Round ' + s.round + ': ' + s.finders + ' finder(s), ' + s.fresh + ' new, ' + s.confirmed + ' confirmed.').join(''))
if (gapCells.length) out.push('Files a finder did not read: ' + gapCells.join('; ') + '.')
out.push(found.length + ' finding(s) after dedup (' + dupes.length + ' within ' + WINDOW + ' lines of one already filed): ' + results.filter((r) => r.status === 'exploited').length + ' exploited by a PoC, ' + results.filter((r) => r.status === 'confirmed').length + ' confirmed by reading, ' + inconclusive.length + ' cannot be verified, ' + refuted.length + ' refuted, ' + unverified.length + ' unverified' + (capped.length ? ', ' + capped.length + ' serious finding(s) with no PoC attempted (MAX_REPRO: ' + capped.join(', ') + ')' : '') + (malformed ? ', ' + malformed + ' malformed report(s) dropped' : '') + '.')
if (!complete) {
  out.push('INCOMPLETE: ' + [
    deadCells.length ? deadCells.length + ' dead cell(s)' : '',
    gapCells.length ? gapCells.length + ' cell(s) with unread files' : '',
    unverified.length ? unverified.length + ' unverified finding(s)' : '',
    pocDied.length ? pocDied.length + ' PoC(s) owed and not attempted' : '',
    scanGaps.length ? 'scanner(s) — ' + scanGaps.join('; ') : '',
    unadjudicated.length ? unadjudicated.length + ' scanner hit(s) nobody adjudicated' : '',
  ].filter(Boolean).join(', ') + ' — what follows is not a clean review of the whole scope.')
}
const diedReruns = cells.filter((c) => c.reported && c.diedIn.length).map((c) => c.key + ' (round ' + c.diedIn.join(', ') + ')')
if (diedReruns.length) out.push('Runs that died, of cells that reported in another round: ' + diedReruns.join(', ') + '.')
truncations.forEach((t) => out.push('Truncated: ' + t + '.'))
if (unrun.length) out.push('Variant searches never run, because the review stopped first: ' + unrun.join(', ') + '.')
if (!confirmed.length) out.push(found.length ? 'No finding was confirmed.' : rounds ? 'Nothing was found over the cells listed below.' : 'No round ran, so nothing was searched.')
out.push('')
confirmed.forEach((f) => out.push('- [' + f.finalSeverity + '] ' + f.id + ' ' + f.cwe + ' ' + f.file + ':' + f.line + ' — ' + f.summary + ' (round ' + f.round + ', ' + f.cell + ')' +
  '\n  exploit: ' + f.exploit + '\n  trace: ' + f.trace + '\n  fix: ' + f.fix +
  (f.status === 'exploited' ? '\n  EXPLOITED at ' + f.head + ': `' + f.command + '`\n    ' + f.output.split('\n').join('\n    ') : '\n  confirmed by reading' + (f.poc ? '; PoC: ' + f.poc : capped.includes(f.id) ? ' (over MAX_REPRO, no PoC attempted)' : '') + ': ' + f.reason)))
groups.forEach((g) => out.push('- shared root cause (' + g.ids.join(', ') + '): ' + g.title))
if (mergeDied) out.push('- ' + mergeDied + ': findings sharing a root cause are listed separately')
inconclusive.forEach((f) => out.push('- CANNOT VERIFY — needs a human [' + f.finalSeverity + '] ' + f.id + ' ' + f.cwe + ' ' + f.file + ':' + f.line + ' — ' + f.summary + '\n  why: ' + f.reason))
unverified.forEach((f) => out.push('- UNVERIFIED [' + f.severity + '] ' + f.id + ' ' + f.file + ':' + f.line + ' — ' + f.summary + ' (' + f.reason + ')'))
unadjudicated.forEach((h) => out.push('- UNADJUDICATED scanner hit ' + h.id + ' ' + h.tool + ' ' + h.rule + ' ' + h.file + ':' + h.line + ' (' + h.why + ')'))
if (dismissed.length) out.push('- ' + dismissed.length + ' scanner hit(s) dismissed by a finder: ' + dismissed.map((d) => d.hit.id + ' ' + d.hit.file + ':' + d.hit.line + ' (' + d.why + ')').join('; '))
out.push('')
cells.filter((c) => c.runs > 0).forEach((c) => out.push('coverage ' + c.key + ' (from round ' + c.from + ', ' + c.runs + ' run(s)): ' + (c.reported ? c.count + ' kept — ' + c.reads.join('; ') : 'DEAD')))

log(spawned.length - dead.length + '/' + spawned.length + ' finder(s) came back; ' + spawnCount + ' agent(s) spawned')
if (dead.length) log('DEAD: ' + dead.join(', '))

return {
  // complete is the review's COVERAGE, not its outcome: every declared cell
  // reported, every finding was judged, every PoC owed was attempted, every
  // declared scanner ran and every hit was adjudicated. A review that found
  // nothing over all of that IS complete; one that found ten while a scanner
  // never ran is not. A finding nobody could decide is listed apart as
  // cannot-verify and does not make it incomplete.
  complete,
  report: out.join('\n'),
  rounds,
  stoppedBy,
  confirmed: confirmed.map((f) => ({ id: f.id, severity: f.finalSeverity, cwe: f.cwe, file: f.file, line: f.line, summary: f.summary, round: f.round, exploited: f.status === 'exploited', command: f.command || '' })),
  inconclusive: inconclusive.map((f) => ({ id: f.id, severity: f.finalSeverity, file: f.file, line: f.line, summary: f.summary, reason: f.reason })),
  refuted: refuted.length,
  unverified: unverified.map((f) => f.id),
  capped,
  scanGaps,
  unadjudicated: unadjudicated.map((h) => h.id),
  deadCells,
}
