package wfcheck

import (
	"strings"
	"testing"
)

// The shape that motivated this file, reduced: every producer prompt is built by
// one factory out of a config whose `ownsBlock` is a template literal, so the
// parsed half reads nothing and the collision check — the one that catches a
// LOST WRITE — compares nothing. Measured on mocker P3c, where the arithmetic
// was instead done by hand four times, by three agents.
const factoryHead = `export const meta = { name: 'x', description: 'd', phases: [{ title: 'P' }] }
const S = { type: 'object', properties: { done: { type: 'boolean' } } }
const spawn = async (p, o) => { try { return await agent(p, o) } catch { return null } }
`

func factory(blocks ...string) string {
	body := factoryHead + "const SECTIONS = [\n"
	for i, b := range blocks {
		body += "  { slug: 's" + string(rune('a'+i)) + "', ownsBlock: `" + b + "` },\n"
	}
	body += "]\nphase('P')\nfor (const cfg of SECTIONS) { await spawn('do it\\n' + cfg.ownsBlock, { model: 'sonnet', effort: 'low', label: cfg.slug, phase: 'P', schema: S }) }\nreturn { done: true }"
	return body
}

func TestOwnershipText_readsABlockNoResolvedPromptCarries(t *testing.T) {
	r := run(t, factory("YOU OWN\n  internal/a.go — the one thing\n", "YOU OWN\n  internal/b.go — the other\n"))
	if !has(r, INFO, "read from the SOURCE TEXT: 2 path(s) in 2 `YOU OWN` block(s)") {
		t.Fatalf("the text half must reach a factory-composed block:\n%s", dump(r))
	}
	if !has(r, INFO, "2 distinct") {
		t.Fatalf("the three numbers the hand arithmetic produced:\n%s", dump(r))
	}
}

// The whole point: two blocks naming one file is a LOST WRITE, and it was
// invisible in exactly the scripts the manual's own round trip produces.
func TestOwnershipText_twoBlocksNamingOneFileIsAnError(t *testing.T) {
	r := run(t, factory("YOU OWN\n  internal/a.go — the one thing\n", "YOU OWN\n  internal/a.go — also me\n"))
	wantErr(t, r, "internal/a.go is declared in 2 different `YOU OWN` block(s)")
}

// A script whose prompts ARE literals gets nothing from here. Two reports of one
// collision on adjacent lines of one report is the second-copy defect this
// project names as its most frequent.
func TestOwnershipText_isSilentWhereTheParsedHalfAlreadyRead(t *testing.T) {
	body := factoryHead + "phase('P')\n" +
		"await spawn(`YOU OWN\n  internal/a.go — mine\n`, { model: 'sonnet', effort: 'low', label: 'one', phase: 'P', schema: S })\n" +
		"return { done: true }"
	r := run(t, body)
	if has(r, INFO, "read from the SOURCE TEXT") {
		t.Fatalf("the strong half ran; the text half must stay quiet:\n%s", dump(r))
	}
	if !has(r, INFO, "1 owned path(s) parsed from 1 prompt(s)") {
		t.Fatalf("the parsed half still reports:\n%s", dump(r))
	}
}

// A comment ABOUT an ownership block is not one. The script that motivated this
// file carries a comment describing its own ownership scheme directly above the
// data, and counting it would manufacture a collision with the real block.
func TestOwnershipText_aCommentIsNotADeclaration(t *testing.T) {
	// A BLOCK comment, because it keeps its line structure: a `//` pair would
	// be skipped for the wrong reason (the join runs two lines together and the
	// path stops leading its line), and a fixture that passes for the wrong
	// reason proves nothing about the rule it was written for.
	body := factoryHead + `/* YOU OWN
  internal/a.go — this is prose about the scheme, not a declaration
*/
phase('P')
await spawn('go', { model: 'sonnet', effort: 'low', label: 'one', phase: 'P', schema: S })
return { done: true }`
	r := run(t, body)
	if has(r, INFO, "read from the SOURCE TEXT") {
		t.Fatalf("a comment declares nothing:\n%s", dump(r))
	}
}

// ONE reach report. Before this, the text half said "every path is covered" and
// the parsed half said "sixteen paths are owned by nobody" — both true of one
// file, because they counted different populations, and a report contradicting
// itself is worse than either answer.
func TestOwnershipText_isFoldedIntoTheOneReachReport(t *testing.T) {
	body := factoryHead + `const G = { slug: 'g1', owns: ['internal/a.go', 'internal/b.go'], reviewers: [{ slug: 'r', vector: 'v', model: 'sonnet' }] }
const SECTIONS = [{ slug: 'sa', ownsBlock: ` + "`YOU OWN\n  internal/a.go — the one thing\n`" + ` }]
phase('P')
for (const cfg of SECTIONS) { await spawn('do it\n' + cfg.ownsBlock, { model: 'sonnet', effort: 'low', label: cfg.slug, phase: 'P', schema: S }) }
log(G.slug)
return { done: true }`
	r := run(t, body)
	if !has(r, INFO, "reviewer/fixer reach: 1 owned path(s) against 1 gate config(s), 1 reachable") {
		t.Fatalf("the text-read path must be counted by the reach check:\n%s", dump(r))
	}
	if !has(r, INFO, "read from the SOURCE TEXT rather than from a resolved prompt") {
		t.Fatalf("the reach line must say where its paths came from:\n%s", dump(r))
	}
	// And the stale-entry note must not accuse a path the text half declares.
	for _, f := range r.Findings {
		if strings.Contains(f.Msg, "that no agent this tool could read declares owning") &&
			strings.Contains(f.Msg, "internal/a.go") {
			t.Fatalf("a path the text half declares is not a stale gate entry:\n%s", dump(r))
		}
	}
	// The gate's OTHER path is genuinely declared by nobody, and that still says so.
	if !has(r, INFO, "internal/b.go") {
		t.Fatalf("a gate entry no block declares is still news:\n%s", dump(r))
	}
}

// A path in no gate's `owns` is a file a reviewer can be briefed on and no fix
// agent may touch — the mocker slice A class. It must fire from the text half
// too, and it must name the block rather than an agent, because that is what
// this half knows.
func TestOwnershipText_aTextReadPathInNoGateOwnsWarns(t *testing.T) {
	body := factoryHead + `const G = { slug: 'g1', owns: ['internal/other.go'], reviewers: [{ slug: 'r', vector: 'v', model: 'sonnet' }] }
const SECTIONS = [{ slug: 'sa', ownsBlock: ` + "`YOU OWN\n  internal/a.go — the one thing\n`" + ` }]
phase('P')
for (const cfg of SECTIONS) { await spawn('do it\n' + cfg.ownsBlock, { model: 'sonnet', effort: 'low', label: cfg.slug, phase: 'P', schema: S }) }
log(G.slug)
return { done: true }`
	r := run(t, body)
	if !has(r, WARN, "a `YOU OWN` block declares internal/a.go, and no gate lists it") {
		t.Fatalf("want the reach WARN from the text half:\n%s", dump(r))
	}
}

// A template literal broken by a `${...}` hole is ONE block. Read as two, the
// half after the hole carries no `YOU OWN` header and its paths are lost — which
// is the same defect one layer down as reading two adjacent literals as one
// sentence, the reason jstext returns spans rather than a blanked copy.
func TestOwnershipText_aBlockSplitByAnInterpolationIsStillOneBlock(t *testing.T) {
	body := factoryHead + "const WHO = 'the thing'\n" +
		"const SECTIONS = [{ slug: 'sa', ownsBlock: `YOU OWN\n  internal/a.go — ${WHO}\n  internal/b.go — the other\n` }]\n" +
		"phase('P')\n" +
		"for (const cfg of SECTIONS) { await spawn('do it\\n' + cfg.ownsBlock, { model: 'sonnet', effort: 'low', label: cfg.slug, phase: 'P', schema: S }) }\n" +
		"return { done: true }"
	r := run(t, body)
	if !has(r, INFO, "read from the SOURCE TEXT: 2 path(s) in 1 `YOU OWN` block(s)") {
		t.Fatalf("both halves of an interpolated block belong to it:\n%s", dump(r))
	}
}

// A block written in a single-quoted string carries its line breaks as `\n`
// escapes. Undecoded, the whole block is ONE line, the paths stop leading their
// lines, and the check reads nothing while reporting nothing — the twelfth
// wave's class, which is why jstext decodes.
func TestOwnershipText_readsABlockWrittenWithEscapedNewlines(t *testing.T) {
	body := factoryHead +
		"const SECTIONS = [{ slug: 'sa', ownsBlock: 'YOU OWN\\n  internal/a.go — the one thing\\n  internal/b.go — the other\\n' }]\n" +
		"phase('P')\n" +
		"for (const cfg of SECTIONS) { await spawn('do it\\n' + cfg.ownsBlock, { model: 'sonnet', effort: 'low', label: cfg.slug, phase: 'P', schema: S }) }\n" +
		"return { done: true }"
	r := run(t, body)
	if !has(r, INFO, "read from the SOURCE TEXT: 2 path(s) in 1 `YOU OWN` block(s)") {
		t.Fatalf("an escaped-newline block is still a block:\n%s", dump(r))
	}
}

// One path named TWICE inside ONE block is not a collision — it is a list that
// repeats itself, and reporting it would be the phantom-ERROR class this
// checker's own history is built against.
func TestOwnershipText_onePathTwiceInOneBlockIsNotACollision(t *testing.T) {
	r := run(t, factory("YOU OWN\n  internal/a.go — the one thing\n  internal/a.go — and again\n"))
	if has(r, ERROR, "is declared in") {
		t.Fatalf("one block naming a path twice owns it once:\n%s", dump(r))
	}
	if !has(r, INFO, "1 distinct") {
		t.Fatalf("the distinct count is what the operator checks:\n%s", dump(r))
	}
}

// A script with NO gate config gets no per-path reach WARN. The summary line
// already says the comparison covered nothing, and a WARN beside it would be the
// report contradicting its own summary — which is the defect the fold was
// written to prevent.
func TestOwnershipText_aScriptWithNoGateGetsNoReachWarn(t *testing.T) {
	r := run(t, factory("YOU OWN\n  internal/a.go — the one thing\n"))
	if has(r, WARN, "no gate lists it in `owns`") {
		t.Fatalf("with no gate there is nothing to be missing from:\n%s", dump(r))
	}
	if !has(r, INFO, "the reviewer/fixer reach check covered nothing") {
		t.Fatalf("the population must still be printed:\n%s", dump(r))
	}
}

// A gate whose `owns` could not be read in full cannot convict a path of being
// unowned — the polarity rule this file's own doc invokes. Without the
// abstention the text half would file a WARN the parsed half explicitly
// withholds on the same script.
func TestOwnershipText_anUnreadableOwnsArrayWithholdsTheReachWarn(t *testing.T) {
	body := factoryHead + `const G = { slug: 'g1', owns: [...someGlobalList], reviewers: [{ slug: 'r', vector: 'v', model: 'sonnet' }] }
const SECTIONS = [{ slug: 'sa', ownsBlock: ` + "`YOU OWN\n  internal/a.go — the one thing\n`" + ` }]
phase('P')
for (const cfg of SECTIONS) { await spawn('do it\n' + cfg.ownsBlock, { model: 'sonnet', effort: 'low', label: cfg.slug, phase: 'P', schema: S }) }
log(G.slug)
return { done: true }`
	r := run(t, body)
	if has(r, WARN, "no gate lists it in `owns`") {
		t.Fatalf("a list this tool could not read is not a list nobody owns:\n%s", dump(r))
	}
	if !has(r, INFO, "NOT JUDGED") {
		t.Fatalf("the abstention must be said out loud:\n%s", dump(r))
	}
}

// The text half's WARNs come out of a map, and a report whose line order moves
// between runs cannot be diffed. Two unreachable paths, asserted in order.
func TestOwnershipText_reachWarnsAreOrdered(t *testing.T) {
	body := factoryHead + `const G = { slug: 'g1', owns: ['internal/zzz.go'], reviewers: [{ slug: 'r', vector: 'v', model: 'sonnet' }] }
const SECTIONS = [{ slug: 'sa', ownsBlock: ` + "`YOU OWN\n  internal/b.go — second alphabetically\n  internal/a.go — first alphabetically\n`" + ` }]
phase('P')
for (const cfg of SECTIONS) { await spawn('do it\n' + cfg.ownsBlock, { model: 'sonnet', effort: 'low', label: cfg.slug, phase: 'P', schema: S }) }
log(G.slug)
return { done: true }`
	for i := 0; i < 5; i++ {
		r := run(t, body)
		var order []string
		for _, f := range r.Findings {
			if f.Level == WARN && strings.Contains(f.Msg, "no gate lists it") {
				order = append(order, f.Msg)
			}
		}
		if len(order) != 2 {
			t.Fatalf("two unreachable paths, got %d:\n%s", len(order), dump(r))
		}
		if !strings.Contains(order[0], "internal/a.go") || !strings.Contains(order[1], "internal/b.go") {
			t.Fatalf("the WARNs must be ordered, got:\n%s\n%s", order[0], order[1])
		}
	}
}

// The middle number of the line the operator checks by hand counts the blocks
// that CONTRIBUTED an unseen path, not every literal block in the file: a script
// with one resolved prompt and one factory block said "1 path(s) in 2 blocks".
func TestOwnershipText_theBlockCountCountsOnlyContributingBlocks(t *testing.T) {
	body := factoryHead + `const SECTIONS = [{ slug: 'sa', ownsBlock: ` + "`YOU OWN\n  internal/a.go — the factory one\n`" + ` }]
phase('P')
await spawn(` + "`YOU OWN\n  internal/literal.go — read statically\n`" + `, { model: 'sonnet', effort: 'low', label: 'lit', phase: 'P', schema: S })
for (const cfg of SECTIONS) { await spawn('do it\n' + cfg.ownsBlock, { model: 'sonnet', effort: 'low', label: cfg.slug, phase: 'P', schema: S }) }
return { done: true }`
	r := run(t, body)
	if !has(r, INFO, "1 path(s) in 1 `YOU OWN` block(s)") {
		t.Fatalf("the block the parsed half already read is not one of these:\n%s", dump(r))
	}
}
