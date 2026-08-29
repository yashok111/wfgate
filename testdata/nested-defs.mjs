export const meta = { name: 'fixture-nested-defs', description: 'wfdry fixture: a `$defs` written on a SUB-schema, which `#/$defs/x` cannot reach', phases: [{ title: 'P' }] }
// The shape a script reaches for when the width classifier pushes it to declare
// a repeated leaf once: the `$defs` and the leaf are written TOGETHER in one
// constant, and the constant is then nested under `properties`. A `$ref` is
// anchored at the schema ROOT, so `#/$defs/prop` does not reach it — and the
// note used to say only `"$defs" is not in the schema`, which names the failure
// and not the fix. The P3e gate read that as the whole feature being broken,
// reverted to twenty-two inlined copies of the leaf and paid about 1,800 bytes
// of width for it. The root-anchored spelling resolves, and always did.
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const NAMES = ['P1', 'P2', 'P3']
const REPORT = {
  type: 'object',
  $defs: { prop: { type: 'object', required: ['met', 'evidence'], properties: { met: { type: 'boolean' }, evidence: { type: 'string' } } } },
  required: NAMES,
  properties: Object.fromEntries(NAMES.map((n) => [n, { $ref: '#/$defs/prop' }])),
}
const r = await spawn('accept the slice', { model: 'sonnet', effort: 'low', label: 'accept', schema: { type: 'object', required: ['properties'], properties: { properties: REPORT } } })
const dead = r ? [] : ['accept']
// Every property comes back a placeholder STRING rather than the object it
// declares, so this term is false in every mode — which is the red the operator
// reads as twenty-two authoring defects.
const allMet = r ? NAMES.every((n) => r.properties[n].met === true) : false
return { done: dead.length === 0 && allMet, dead }
