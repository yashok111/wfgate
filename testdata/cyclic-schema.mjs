export const meta = { name: 'fixture-cyclic-schema', description: 'wfdry fixture: a schema that does not serialize, so no width is measured', phases: [{ title: 'P' }] }
// The population the corpus band says NOTHING about. A schema holding a cycle
// cannot be serialised, so its width is not a number — and the band header used
// to print `<=3974 ACCEPTED, >=4233 REFUSED` over a run where nothing had been
// priced against it, which is a measured-looking figure beside an empty
// measurement: this project's single most repeated defect, in the header added
// to remove a different instance of it.
const S = { type: 'object', properties: { blockers: { type: 'array' } } }
S.properties.self = S
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const build = await spawn('build it', { model: 'sonnet', effort: 'low', label: 'build', phase: 'P', schema: S })
return { green: build !== null, dead: build ? [] : ['build'] }
