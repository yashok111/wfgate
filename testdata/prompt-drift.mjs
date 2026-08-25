export const meta = { name: 'fixture-prompt-drift', description: 'wfdry fixture: two agents given one job, one rule stated to only one of them', phases: [{ title: 'P' }] }
// The P3a class, banked. Two agents are given the SAME question by the same
// batch; a governing sentence reaches one prompt and not the other, and no
// other check in this toolchain sees it — `dup` needs a rare token the two
// prompts share and `stale` needs something retired. That gate spent its last
// round on this class and closed it with a hand diff.
//
// Both prompts are COMPOSED at run time, deliberately: on the real script 14
// of 15 are, which is why the diff lives in the runner and not in wfcheck.
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
const S = { type: 'object', properties: { blockers: { type: 'array' } } }
const COMMON = 'Answer every acceptance clause in the block you were handed, by its bare number.'
const RULE = 'A clause you believe is true but that no test holds is satisfied FALSE.'
phase('P')
const walk = await spawn([COMMON, RULE].join(' '), { model: 'sonnet', effort: 'low', label: 'accept:reads', schema: S, phase: 'P' })
const check = await spawn([COMMON, 'Report each bar under its own key with its verbatim output.'].join(' '), { model: 'sonnet', effort: 'low', label: 'verify', schema: S, phase: 'P' })
return { done: true, dead: [walk, check].filter((x) => !x).length }
