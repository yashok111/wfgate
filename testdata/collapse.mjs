export const meta = { name: 'fixture-collapse', description: 'wfdry fixture: a red-bar abort that empties every failure mode', phases: [{ title: 'P' }] }
// The shape that cost a gate a round: a guard added to the FIRST producer
// aborts the run when its bars come back red, and the non-clean fixture returns
// a red bar for every agent — so the fix round, the recheck, the acceptance
// step and the final verdict are exercised by nothing while every mode still
// exits OK. One is not zero, so the zero-spawn rule never fired.
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
const S = { type: 'object', properties: { result: { type: 'string', enum: ['pass', 'fail'] }, blockers: { type: 'array' } } }
phase('P')
const built = []
for (const s of ['one', 'two', 'three', 'four', 'five', 'six']) {
  const r = await spawn('build ' + s, { model: 'sonnet', effort: 'low', label: 'producer-' + s, schema: S })
  if (!r || r.result === 'fail') return { done: false, dead: r ? [] : ['producer-' + s], aborted: s }
  built.push(s)
}
return { done: true, dead: [], built }
