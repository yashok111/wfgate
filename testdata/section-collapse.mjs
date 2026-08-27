export const meta = { name: 'fixture-section-collapse', description: 'wfdry fixture: a failure branch that skips a whole section, inside the agent ratio', phases: [{ title: 'S1' }, { title: 'S2' }] }
// The half the agent ratio cannot see. A guard added midway aborts the rest of
// the run when a gate comes back with blockers, so every failing mode stops
// before section two — its phase, its producers and the gate over them are
// exercised by NO mode — while the agent count stays within half of the happy
// path's and the ratio passes. `collapse.mjs` is the opposite shape: it narrows
// to one agent inside ONE phase, which phases alone would not see. The two
// checks are additive because they miss opposite things.
const S = { type: 'object', properties: { blockers: { type: 'array' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
const opts = (l, ph) => ({ model: 'sonnet', effort: 'low', label: l, phase: ph, schema: S })
phase('S1')
const a = await spawn('build one', opts('build-one', 'S1'))
const b = await spawn('build two', opts('build-two', 'S1'))
const g1 = await spawn('gate one', opts('gate:one', 'S1'))
if (!g1 || g1.blockers.length) return { done: false, dead: [], stoppedAt: 'S1' }
phase('S2')
const c = await spawn('build three', opts('build-three', 'S2'))
const g2 = await spawn('gate two', opts('gate:two', 'S2'))
const alive = { a, b, g1, c, g2 }
const dead = Object.keys(alive).filter(k => !alive[k])
return { done: dead.length === 0, dead }
