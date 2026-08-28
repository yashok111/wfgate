// A script whose ONE agent carries a schema wider than the narrowest width this
// corpus has seen the launch's safety classifier refuse. It is otherwise
// correct: the width is a QUESTION and never fails a mode, so this fixture must
// come back 7/7 at exit 0 while SAYING what it would put on the wire.
//
// Banked because the delivery layer of that measurement — the `=== schemas ===`
// section and the WARN that repeats at the tail — could both be deleted with
// every Go test and every other bar green.
export const meta = { name: 'wide-schema', description: 'one agent, one wide schema', phases: [{ title: 'Build' }] }

const PAD = 'x'.repeat(4700)
const S = {
  type: 'object',
  properties: {
    done: { type: 'boolean' },
    notes: { type: 'array', items: { type: 'string' }, description: PAD },
  },
  required: ['done'],
}

const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }

phase('Build')
const r = await spawn(`Do the thing and report it.`, { model: 'sonnet', effort: 'low', label: 'build', phase: 'Build', schema: S })
log('build: ' + (r ? 'ok' : 'dead'))
return { done: Boolean(r && r.done) }
