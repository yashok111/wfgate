export const meta = { name: 'fixture-one-error', description: 'wfcheck fixture: exactly one ERROR, so the exit code is exercised', phases: [{ title: 'P' }] }
// The effort is outside the closed enum. wfcheck must report it AND exit 1:
// replacing that exit with a no-op left every bar green, because check-tools
// only ever ran the tool on a clean file.
const S = { type: 'object', properties: { findings: { type: 'array' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
await spawn(`do the thing`, { model: 'sonnet', effort: 'turbo', label: 'worker', phase: 'P', schema: S })
return { done: true, dead: [] }
