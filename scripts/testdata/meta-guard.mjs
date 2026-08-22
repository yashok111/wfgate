export const meta = { name: 'fixture-meta-guard', description: 'wfdry fixture: touches the harness-only meta binding; every mode must FAIL', phases: [{ title: 'P' }] }
// The P2e regression, banked as a fixture. The real harness parses the `meta`
// block OUT before the body runs, so the line below is unexecutable at launch
// (it died in 29 ms with zero agents). A dry runner whose stub leaves `meta`
// bound runs this green — the false green that shipped the only production
// defect of that gate. wfdry must FAIL every mode on this file, forever.
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
if (meta.phases.length !== 1) throw new Error('unreachable')
const r = await spawn('do the thing', { model: 'sonnet', label: 'worker', schema: { type: 'object' } })
return { done: true, dead: r ? [] : ['worker'] }
