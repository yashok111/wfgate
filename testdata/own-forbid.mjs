export const meta = { name: 'fixture-own-forbid', description: 'wfcheck fixture: a forbidding phrase must not lose the owners it stands beside', phases: [{ title: 'P' }] }
// Banked. A pathless "DO NOT" heading is an exclusion region; the same words on
// the YOU OWN header line are prose about the block. Ending the scan at either
// lost every owner of the prompt and silenced a real dual-ownership ERROR —
// a checker may neither invent an ERROR nor lose one.
const S = { type: 'object', properties: { ok: { type: 'boolean' } } }
async function spawn(p, o) { try { return await agent(p, o) } catch { return null } }
phase('P')
const a = await spawn('YOU OWN these files and must not write anywhere else.\n  internal/shared.go — the seam\n', { model: 'sonnet', effort: 'low', label: 'one', phase: 'P', schema: S })
const b = await spawn('YOU OWN\n  internal/shared.go — also the seam\n\nDO NOT\n  internal/other.go\n', { model: 'sonnet', effort: 'low', label: 'two', phase: 'P', schema: S })
return { done: true, dead: [a, b].filter((x) => !x).length }
