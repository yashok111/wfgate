export const meta = { name: 'fixture-zero-spawn', description: 'wfdry fixture: a mode that spawns nothing must FAIL', phases: [{ title: 'P' }] }
// The only spawn sits behind a false flag. Every stub check in the runner is
// then a guard over the empty set, and a runner that calls that OK is the
// founding false green in a smaller costume.
const DEEP_SCAN = false
const S = { type: 'object', properties: { findings: { type: 'array' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
if (DEEP_SCAN) { await spawn(`never`, { model: 'sonnet', effort: 'low', label: 'w', phase: 'P', schema: S }) }
return { done: true, dead: [] }
