export const meta = { name: 'fixture-dead-reviews', description: 'wfdry fixture: a verdict that does not depend on its reviewers having run', phases: [{ title: 'P' }] }
// ONE edit a live operator plausibly makes, and it survived seven green modes:
// a comment justifies filtering dead REVIEWERS out of the green computation on
// the argument that only a dead PRODUCER stops a run. The result is a script
// whose green is true on a run where nothing reviewed anything.
//
// The label stays `review:correctness` on purpose. Relabelling it off the
// segment the mode matches would DISARM this guard rather than exercise it —
// the mode then kills nobody, the arming condition is empty and the existing
// "proved nothing" note is what fires. That the two edits compose into a script
// no check reaches is a hole in `reviewish`, not in this fixture, and a fixture
// must not describe the blind spot as if it were the covered case.
//
// `green` is a CONJUNCTION, which is why this passed: under the failing fixture
// the surviving reviewer returned blockers, so the second conjunct carried the
// verdict to false and the mode reported not-green whatever the script did with
// the deaths. The runner's own fixture, not the script, decided what was
// exercised — so the mode must run the PASSING fixture, leaving the deaths as
// the only thing wrong with the run.
const S = { type: 'object', properties: { blockers: { type: 'array' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
phase('P')
const build = await spawn('build it', { model: 'sonnet', effort: 'low', label: 'build', phase: 'P', schema: S })
const lens = await spawn('review it', { model: 'sonnet', effort: 'high', label: 'review:correctness', phase: 'P', schema: S })
const alive = { build, lens }
const dead = Object.keys(alive).filter(k => !alive[k])
const open = Object.values(alive).flatMap(r => (r ? r.blockers : []))
// only a dead PRODUCER stops the run; a dead lens merely reported nothing
return { green: dead.filter(k => k !== 'lens').length === 0 && open.length === 0, dead, open }
