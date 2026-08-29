export const meta = { name: 'fixture-dead-halt', description: 'wfdry fixture: an abort guard that halts on ANY death and loses a phase under dead-reviews', phases: [{ title: 'S1' }, { title: 'S2' }] }
// The shape `dead-reviews` is exempt from the phase VIOLATION for, and the
// reason the exemption is not silence. Measured on P3g: an abort guard written
// to end the run when a producer dies ends it when ANYTHING dies, so the mode
// that kills reviewers reached 11 of 14 phases — a gate, the acceptance step
// and the last gate exercised by nothing — while printing `OK` and `7/7 modes
// completed`, because its agent count cleared the half-of-clean floor. Here:
// 6 agents over 2 phases clean, 3 over 1 under `dead-reviews`, and 3*2 >= 6
// keeps the ratio quiet exactly as it did there.
//
// It is a NOTE and not a violation because the same output is what a correct
// script produces when it HALTS on a dead reviewer on purpose — the runner
// injected that death. Which of the two this is, is the author's to answer;
// the runner's job is to stop printing OK over the question.
const S = { type: 'object', properties: { blockers: { type: 'array' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
const opts = (l, ph) => ({ model: 'sonnet', effort: 'low', label: l, phase: ph, schema: S })
phase('S1')
const a = await spawn('build one', opts('build-one', 'S1'))
const b = await spawn('build two', opts('build-two', 'S1'))
const r1 = await spawn('review one', opts('review:one', 'S1'))
const early = { a, b, r1 }
const earlyDead = Object.keys(early).filter(k => !early[k])
// the guard as it was written: "a death means the tree may not even compile, so
// stop" — true of a producer, and applied to every agent in the section
if (earlyDead.length) return { green: false, dead: earlyDead, open: [], stoppedAt: 'S1' }
phase('S2')
const c = await spawn('build three', opts('build-three', 'S2'))
const r2 = await spawn('review two', opts('review:two', 'S2'))
const g = await spawn('gate two', opts('gate:two', 'S2'))
const alive = { a, b, r1, c, r2, g }
const dead = Object.keys(alive).filter(k => !alive[k])
const open = Object.values(alive).flatMap(x => (x ? x.blockers : []))
return { green: dead.length === 0 && open.length === 0, dead, open, stoppedAt: null }
