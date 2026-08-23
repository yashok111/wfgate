export const meta = { name: 'fixture-false-green', description: 'wfdry fixture: the happy path must not return a not-green verdict', phases: [{ title: 'P' }] }
// P2f, banked. `repair` is spawned ONLY when acceptance fails, and it is counted
// in the liveness ledger anyway — so on the one run everyone wants (every
// producer alive, acceptance green) the ledger reports a dead agent and the run
// returns not-green. Seven modes were green over this, because acceptance
// failed in every one of them and repair was therefore always spawned; the
// runner's own fixture, not the script, decided which branch was exercised.
const S = { type: 'object', properties: { done: { type: 'boolean' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const build = await spawn('build it', { model: 'sonnet', effort: 'low', label: 'build', phase: 'P', schema: S })
const accept = await spawn('accept it', { model: 'sonnet', effort: 'high', label: 'accept', phase: 'P', schema: S })
let repair = null
if (!accept || !accept.done) {
  repair = await spawn('repair it', { model: 'sonnet', effort: 'medium', label: 'repair', phase: 'P', schema: S })
}
const alive = { build, accept, repair }
const dead = Object.keys(alive).filter(k => !alive[k])
return { green: dead.length === 0, dead }
