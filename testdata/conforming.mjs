export const meta = { name: 'fixture-conforming', description: 'wfcheck fixture: a script written exactly as the manual requires must report no ERROR', phases: [{ title: 'P' }] }
// P2f, banked. The manual REQUIRES the spawn wrapper, and `async function
// spawn(prompt, opts)` matches `spawn(` — so the inventory's textual backstop
// counted the declaration as a call and fired on every conforming script of the
// run. The operator launched five workflows over a standing ERROR. A comment
// naming agent() and a log string naming agent(s) did the same thing, and are
// here for the same reason.
const S = { type: 'object', properties: { findings: { type: 'array' } } }
async function spawn(prompt, opts) {
  // one agent() call per site, no loop, no fan-out
  try { return await agent(prompt, opts) } catch { return null }
}
phase('P')
const a = await spawn('audit it', { model: 'sonnet', effort: 'high', label: 'auditor', phase: 'P', schema: S })
log('1/2 agent(s) came back')
return { done: true, dead: a ? [] : ['auditor'] }
