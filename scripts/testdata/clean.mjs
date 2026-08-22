export const meta = { name: 'fixture-clean', description: 'wfdry fixture: a minimal script that completes every mode', phases: [{ title: 'P' }] }
// The smallest script that satisfies wfdry's contract: a `spawn` wrapper that
// turns a throw into null, one phase, one agent with a model and a schema, and
// a structured return that carries the dead list.
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const r = await spawn('do the thing', { model: 'sonnet', label: 'worker', schema: { type: 'object', properties: { blockers: { type: 'array' } } } })
return { done: true, dead: r ? [] : ['worker'] }
