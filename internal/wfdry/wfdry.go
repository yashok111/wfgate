// Package wfdry executes a Workflow orchestration script with every
// orchestration hook stubbed, across the modes a real run can enter.
//
// The static check reads the script. This one RUNS it, so it walks paths a
// parse cannot reach: a template literal that only composes on the failure
// branch, a field dereferenced on a result that is null in one mode, a gate
// accounted for on a branch that was never taken. A parse is not an execution.
//
// The script is JavaScript and is executed by an embedded engine (goja), so
// this tool has no external runtime to drift against — the previous version
// shelled out to whatever `node` happened to be on PATH. That engine is NOT the
// one the real Workflow tool uses, and that boundary is the honest limit of a
// dry run: it proves the script's control flow, not that the production
// runtime accepts it. The banked meta-guard fixture exists because a dry runner
// that diverges from the real harness is exactly how a false green ships.
package wfdry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dop251/goja"

	"git.sumka.site/yakov/wfgate/internal/check"

	"git.sumka.site/yakov/wfgate/internal/jstext"
	"git.sumka.site/yakov/wfgate/internal/wfscript"
	"git.sumka.site/yakov/wfgate/internal/ws"
)

// Modes are the states a real run can enter. `clean` matters most: reviewers
// read the failure branches, because that is where the risk looks like it
// lives, so the defects that survive a gate live on the happy path.
var Modes = []string{
	"clean",        // nothing goes wrong
	"blockers",     // every gate returns blockers; walks the fix and delta rounds
	"acceptfail",   // the acceptance step observes a failure; walks the closeout path
	"unfixed",      // a fix agent reports it could not close something
	"throw-serial", // the first spawn throws; walks the early-abort exit
	"dead-reviews", // every review/fix/closeout agent returns null
	"no-args",      // the script runs with no `args` — see Options.Args
}

// emptyMode is the read-only types' extra mode: every finder returns a report
// with NOTHING in it. A review's findings are its deliverable, so the passing
// fixture fills them and `clean` walks find → verify → merge; the path where
// the review found nothing is then a different path — no verifier spawns, the
// merge sees an empty list, the report has to say what was covered rather than
// "clean" — and it was walked by no mode at all until it had one.
const emptyMode = "empty"

// deadFindersMode is `dead-reviews` split in two for the read-only types. One
// mode that killed finders AND verifiers killed the finders first, so no
// verifier was ever spawned to die: a review whose `complete` ignored its
// unverified findings passed every mode (the wave's adversarial review,
// probe p1). `dead-finders` kills the finders and leaves the verifiers alive;
// `dead-reviews` kills the verifiers and critics and leaves the finders alive.
const deadFindersMode = "dead-finders"

// ModesFor is the mode list a run of this type enters. A read-only run has no
// acceptance step and no fix agent, so `acceptfail` and `unfixed` would walk
// vocabulary its script does not carry, and it gains `empty`. A type the
// workspace package does not know is refused by Run, not here.
func ModesFor(typ string) []string {
	if !ws.ReadOnly(normType(typ)) {
		return Modes
	}
	return []string{"clean", "blockers", emptyMode, "throw-serial", deadFindersMode, "dead-reviews", "no-args"}
}

func normType(typ string) string {
	if typ == "" {
		return ws.TypeBuild
	}
	return typ
}

// passFixture says whether a mode's stubbed agents report SUCCESS. It is not a
// synonym for `clean`: `dead-reviews` must isolate ONE variable — the reviewers
// returned nothing — and the failing fixture destroyed it, because every
// surviving agent then reported failure too and the run came back not-green
// whatever the script did with the deaths. Measured 2026-08-27 over the five
// correct scripts on disk (the emitted template, p2f, p3a, p3b, a3): the
// shipped mode returned `green: false` on all five AND on a script that filters
// its dead reviewers out of the verdict, so it separated nothing. Under the
// passing fixture the same five still return `green: false` — the deaths alone
// carry it — and the defective one returns `green: true`.
//
// `empty` is passing too: nothing failed in it, the finders just found nothing,
// and a verdict that reads as failure there is a script treating "no findings"
// as "did not run".
func passFixture(mode string) bool {
	return mode == "clean" || mode == "dead-reviews" || mode == emptyMode || mode == deadFindersMode
}

// Options configure one run.
type Options struct {
	Dead    []string      // labels whose spawn returns null
	Budget  *int          // budget.total; nil means the script sees null
	Args    string        // JSON handed to the script as `args` (every mode but no-args)
	Timeout time.Duration // per-mode deadline; 0 means the default

	// Type is the gate's run type (internal/ws), "" meaning build. It decides
	// the mode list (ModesFor), which way a finding-shaped array points, and
	// which labels `dead-reviews` kills — in a review the finders ARE the
	// reviewers.
	Type string
}

// DefaultArgs is what a parameterised workflow sees. It is NOT undefined, and
// that is a fix: the previous runner passed undefined in every mode, so a
// script opening with `if (!args) return {skipped:true}` returned that in all
// six — six green modes over a body that never executed. Both states are real,
// so both are modes now.
const DefaultArgs = `{"topic":"<wfdry topic>","items":["<a>","<b>"],"paths":["<p>"]}`

// Result is one mode's outcome.
type Result struct {
	Mode       string
	Calls      []string
	Phases     []string
	Logs       []string
	Notes      []string // the RUNNER's own notes, never the script's log() output
	Spawned    int
	Violations []string
	Return     any
	ReturnOK   bool
	Err        error

	// The labels this mode actually stubbed dead, in spawn order and
	// deduplicated. A count was not enough: `dead-reviews` reported OK over a
	// producer it had killed by accident, and nothing in the output named the
	// label, so the mode's claim about WHAT it exercised could not be read at
	// all. `--dead` labels are recorded here too — a label the operator names
	// that never spawns kills nobody, in silence.
	Killed []string

	// Every prompt this mode actually handed an agent, with its label. The
	// runner is the ONLY place these exist as text: measured on the P3a script
	// at the revision that motivated this, 14 of 15 prompts are composed at run
	// time (15 of 16 as it stands, after the chunking fix added an agent), so a
	// static reader sees one of them. The prompt sweep is built on this.
	Prompts   []check.NamedText
	defaulted map[string]int // report fields the fixture contract did not recognise
	family    map[string]int // spawns per label family, read-only runs only
	badRefs   map[string]int // `$ref`s the fixture could not expand, with the reason

	// What this mode actually handed each agent back, by label and in spawn
	// order. The runner synthesises it and then reports only the CONSEQUENCE —
	// a verdict that went false, a field it did not recognise — which is the
	// right register for a check and the wrong one for a diagnosis. Measured on
	// the P3e gate: THREE of its dry-run reds were fixture-synthesis limits
	// reported as artifact defects (a `$ref` the runner lost, twenty-two
	// booleans defaulted false by NAME, an integer term no fixture can satisfy),
	// and each cost a reading of twenty-two "unmet" rows to diagnose as one gap.
	// The object itself answers all three in one look, so `--fixture` prints it.
	Fixtures []FixtureUse

	// The serialized width of every schema this mode handed an agent, by label.
	// It is measured HERE and nowhere else: the runner holds the object the
	// script actually composed, so `JSON.stringify` of it is exact, while a
	// static reader would have to re-serialize an AST literal and would produce
	// a second number for one quantity — this project's most-repeated defect.
	// A schema composed at run time is invisible to the static half entirely.
	Schemas []SchemaUse
}

// SchemaUse is one agent call's schema, as the wire would carry it.
type SchemaUse struct {
	Label string
	Bytes int // len(JSON.stringify(schema)); -1 when it does not serialize

	// How many keys the schema names as `required`, over every object node in
	// it, and how many object nodes declare `properties` and require NONE of
	// them. Both are printed and neither is a verdict: an object with no
	// `required` is legal and common, and a bar over it would paint every
	// correct script.
	//
	// It is here because this is the only place the number exists. P3e lost
	// `required: ['met','evidence']` from a leaf inside a width-reduction edit
	// whose own comment was entirely about width, and for two rounds a property
	// could come back `{met: true}` with no evidence at all — the one thing the
	// evidence reviewer had to read. Nothing static reaches it: the leaf is
	// built inside an `Object.fromEntries(...map(...))` callback, so `wfcheck`
	// sees no literal. The runner holds the composed object. A DROP between two
	// rounds is a changed number in this line.
	Required   int
	NoRequired int
}

// FixtureUse is one agent call's synthesised report, exactly as the stub
// returned it.
type FixtureUse struct {
	Label string
	Value any

	// The JSON of Value, kept so that two spawns of one label are compared
	// once rather than at every later spawn.
	rendered []byte
}

// killedLabel reports whether this mode stubbed the named label dead.
func (r *Result) killedLabel(label string) bool {
	for _, k := range r.Killed {
		if k == label {
			return true
		}
	}
	return false
}

// addFixture records a label's synthesised report unless an identical one is
// already there. Equality is over the RENDERED form, because that is what the
// flag prints and what the reader compares.
//
// The value is DEEP-COPIED first, and that is the whole correctness of this
// flag. `vm.ToValue` hands goja a write-through view of the same Go map, so
// every assignment the script makes to an agent's result — `r.verdict = ...`,
// a field the harness fills in — lands in the map this list holds. Without the
// copy the flag printed the SCRIPT's own writes under the heading "the object
// the stub returned", which is precisely the confusion it exists to remove:
// reproduced, a label spawned twice with one schema printed the script's
// overwrite and the real fixture appeared nowhere, because the second, clean
// spawn deduped against the first's pre-mutation bytes.
func (r *Result) addFixture(label string, v any) {
	v = deepCopy(v)
	rendered, err := json.Marshal(v)
	if err != nil {
		rendered = nil
	}
	for _, f := range r.Fixtures {
		if f.Label == label && bytes.Equal(f.rendered, rendered) {
			return
		}
	}
	r.Fixtures = append(r.Fixtures, FixtureUse{Label: label, Value: v, rendered: rendered})
}

// deepCopy detaches a synthesised fixture from the object the script can write
// to. Only the shapes `synth` produces are handled — maps, slices and scalars.
func deepCopy(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = deepCopy(e)
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for _, e := range t {
			out = append(out, deepCopy(e))
		}
		return out
	default:
		return v
	}
}

// kill records a stubbed death once per label.
func (r *Result) kill(label string) {
	if !r.killedLabel(label) {
		r.Killed = append(r.Killed, label)
	}
}

// OK reports a mode that completed with nothing to answer for.
func (r Result) OK() bool { return r.Err == nil && len(r.Violations) == 0 && r.ReturnOK }

// effortTiers mirrors the closed set the Workflow tool accepts.
var effortTiers = map[string]bool{"low": true, "medium": true, "high": true, "xhigh": true, "max": true}

// Run executes the script once per mode.
func Run(path string, modes []string, o Options) ([]Result, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// The meta binding is neutralised so that a body touching `meta` dies here
	// the way it dies at launch: the real harness parses `export const meta` out
	// before the body runs. A binding left in scope produced a measured false
	// green — six modes, six rounds, over a guard reading meta.phases — while
	// the launch died in 29 ms.
	//
	// It is done through the PARSER and in place, byte for byte. A line-anchored
	// regex over the source rewrote any line that merely BEGAN with `export ` —
	// including one inside a prompt — and it disagreed with wfcheck, which
	// accepts a second `export const`, about what the same file even is.
	src, err := neutraliseMeta(string(raw), path)
	if err != nil {
		return nil, err
	}

	if err := ws.ValidType(normType(o.Type)); err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, m := range ModesFor(o.Type) {
		known[m] = true
	}
	var out []Result
	for _, m := range modes {
		if !known[m] {
			// An invented mode used to EXECUTE and report "1/1 modes completed".
			return nil, fmt.Errorf("unknown mode %q for a %s run; known modes: %s", m, normType(o.Type), strings.Join(ModesFor(o.Type), ", "))
		}
		out = append(out, runMode(src, m, o))
	}
	forgiveArgsGate(out)
	noteCollapse(out, ws.ReadOnly(normType(o.Type)))
	return out, nil
}

// noteCollapse names a mode that reached a small fraction of the agents the
// happy path reached.
//
// `gate-checks.md` section E already says a mode that spawns NOTHING fails, and
// one is not zero, so nothing fired on the case that cost a gate a round: a
// red-bar abort added to a producer guard took five of seven modes from
// 15/25/25/25/1/15/25 agents to 18/1/1/1/1/1/1 — the fix round, the recheck, the
// acceptance step and the final verdict exercised by nothing — and every mode
// still exited `OK`. A reviewer banking "wfdry 7 of 7" WOULD have credited a
// layer that
// covered a single agent. The ratio is the signal, not the absolute.
//
// A VIOLATION, not a note, and that was measured rather than argued. Over the
// gated orchestration scripts on disk the failure modes run WIDER than the happy
// path, because a fix round spawns more: p3a 36 clean against 44, p3b 18 against
// 30, and p3b one round earlier 15 against 25 — zero collapses across three
// correct scripts. The one revision carrying the red-bar abort reports 18 and
// then 1 in every other mode. A WARN would have left that run printing
// `7/7 modes completed` and exiting 0, which is the banked claim
// ("wfdry 7 of 7") a reviewer would make from it.
func noteCollapse(rs []Result, ro bool) {
	// A baseline of ZERO needs no guard of its own and was given one that no
	// mutation could make fail: every mode is then either a zero-spawn mode,
	// skipped below, or wider than the baseline, which the ratio passes. The
	// zero-spawn violation already refuses that run by name.
	//
	// The happy path is the baseline BY NAME, because that is the run every
	// other mode is a deviation from. Where it was not run — `--mode` selects a
	// subset — the widest mode stands in for it and the note says so, rather
	// than the check disappearing with the mode that anchors it.
	base, baseMode, basePhases := 0, "", []string(nil)
	for _, r := range rs {
		if r.Mode == "clean" {
			base, baseMode, basePhases = r.Spawned, r.Mode, r.Phases
			break
		}
	}
	if baseMode == "" {
		for _, r := range rs {
			if r.Spawned > base {
				base, baseMode, basePhases = r.Spawned, r.Mode, r.Phases
			}
		}
		// A run with ONE mode is every `--mode X` invocation, and there the
		// stand-in baseline IS that mode, which is then skipped — so the check
		// vanished in silence over exactly the reports that motivated it
		// (`--mode blockers` on the banked collapse fixture printed
		// `OK — 1 agents, 1 phases` and `1/1 modes completed`). An absent
		// comparison says so; it does not read as a clean one.
		for i := range rs {
			if len(rs) == 1 {
				rs[i].Notes = append(rs[i].Notes, "no `clean` mode in this run, so this mode's agent count and phase set were compared against NOTHING — the happy path is the baseline, and `wfdry <script>` runs every mode")
			} else {
				rs[i].Notes = append(rs[i].Notes, "no `clean` mode in this run — the widest mode (`"+baseMode+"`) stands in as the baseline for both the agent count and the phase set, which is not the happy path")
			}
		}
	}
	// A baseline that spawned agents and reached NO phase compares every other
	// mode against an empty set, so the phase check passes over a population of
	// zero — the class this runner exists against, in the check that was added
	// to close another instance of it. `wfcheck` is where a missing `phase()`
	// is an error; here it is said as what it is.
	if base > 0 && len(basePhases) == 0 {
		for i := range rs {
			rs[i].Notes = append(rs[i].Notes, "`"+baseMode+"` reached NO phase, so the phase-coverage check compared every mode against an empty set — NOT A PASS, and `wfcheck` is where a script with no `phase()` call is an error")
		}
	}
	for i := range rs {
		r := &rs[i]
		// `throw-serial` aborts AT the first spawn by construction, so its
		// count is one by design and a ratio over it measures the runner, not
		// the script. `no-args` is the other mode the RUNNER narrows on
		// purpose: a script that gates its body on `args` is correct there, and
		// forgiveArgsGate already says so at zero — warning at one is that same
		// reasoning applied backwards, and it fired on every correct script that
		// does any work before the gate.
		// `empty` is narrowed by the runner too: a review that found nothing
		// spawns no verifier and no merge, and that is the path it walks.
		if r.Mode == baseMode || r.Mode == "throw-serial" || r.Mode == "no-args" || r.Mode == emptyMode {
			continue
		}
		// A mode that spawned NOTHING has already been judged — by the
		// zero-spawn violation, or by the args forgiveness that lifts it for a
		// script legitimately gating its body. A ratio note on top would be a
		// second report of one fact, in the one place the runner has decided
		// the fact is not a defect.
		if r.Spawned == 0 {
			continue
		}
		// The second half of the same question, and the two are ADDITIVE because
		// they miss opposite shapes. The banked `collapse.mjs` narrows to one
		// agent inside ONE phase, so phases alone would not see it; the shape
		// that motivated this narrows by a whole SECTION while keeping its agent
		// count within the ratio — measured, 13 agents against 15 and 4 phases
		// against 5, which passes `Spawned*2 >= base` and reaches neither the
		// fifth section nor its gate.
		//
		// The comparison is over DISTINCT TITLES, never `len(r.Phases)`: a mode
		// that runs one phase twice reaches the same count having covered less,
		// and it is the titles a reader is being told were exercised. The
		// threshold is EXACT rather than a ratio, and that is a measurement, not
		// a preference — across the five correct scripts on disk every mode but
		// `throw-serial` reaches its baseline's phase set exactly, so a
		// tolerance would be calibrated on a population with no variance in it.
		// The missing titles are NAMED, because "fewer phases" sends an operator
		// to diff two runs by hand.
		// `dead-reviews` is exempt from the VIOLATION, and the reason is the
		// guard one screen up: that mode's shortfall is INJECTED by the runner,
		// and the sibling check demands the verdict depend on the reviewers
		// having run — so a script that reacts by HALTING is doing what it is
		// told and was then refused for it. The other failure modes keep the
		// check: there the narrowing is the script's own, and a red-bar abort
		// that empties them is the defect the thirteenth wave banked, not a
		// style.
		//
		// Exempt from the violation is not exempt from being SAID. Measured on
		// P3g: an abort guard that ended the run on any dead PRODUCER took
		// `dead-reviews` to 11 of 14 phases — Gate 6, the acceptance step and
		// Gate 7 exercised by nothing — while the mode printed `OK` and the run
		// printed `7/7 modes completed`, because 16 agents clears the
		// half-of-clean floor. A lens found it by reading the phase list by
		// hand. The WARN is safe to ship because it paints nothing: over the ten
		// gate scripts on disk and all fourteen testdata/ fixtures — twenty-four
		// files, re-counted after a first pass said seven and missed the three
		// P2 workspaces — `dead-reviews` reaches its baseline's phase set
		// EXACTLY everywhere but the fixture written to produce a shortfall, and
		// the one revision carrying that abort guard is the only other reading
		// that produces one.
		if missing := missingPhases(basePhases, r.Phases); len(missing) > 0 {
			// In a read-only run `blockers` is the mode in which every verifier
			// REFUTES, and a step that consumes confirmed findings — a merge, a
			// grouping — has nothing to consume: measured on the emitted review
			// skeleton, `Merge` is the one phase `blockers` cannot reach, by
			// construction. The shortfall is said, not failed, and it is said
			// as what it is: that step ran in the happy path alone.
			if ro && r.Mode == "blockers" {
				r.Notes = append(r.Notes, fmt.Sprintf("%s: never reached %s — the phase(s) `%s` ran in `%s` and not here. In a read-only run this is the mode in which every verifier refuted, so a step that consumes CONFIRMED findings has nothing to consume; it does mean those phases ran in the happy path alone and no failure mode covers them",
					r.Mode, plural(len(missing), "phase"), strings.Join(missing, "`, `"), baseMode))
			} else if r.Mode == "dead-reviews" || r.Mode == deadFindersMode {
				r.Notes = append(r.Notes, fmt.Sprintf("%s: never reached %s — the phase(s) `%s` ran in `%s` and not here. This mode's shortfall can be the runner's own injection (a script that HALTS when a reviewer dies is doing what it is told), which is why it is not a failure; it can equally be an abort guard that ends the run wider than it meant to, and then every section behind those phases, and every gate over them, is covered by no failure mode",
					r.Mode, plural(len(missing), "phase"), strings.Join(missing, "`, `"), baseMode))
			} else {
				r.Violations = append(r.Violations, fmt.Sprintf("%s: never reached %s — the phase(s) `%s` ran in `%s` and not here, so every section behind them, and every gate over those sections, ran ONLY in `%s` and no failure mode covers it",
					r.Mode, plural(len(missing), "phase"), strings.Join(missing, "`, `"), baseMode, baseMode))
			}
		}
		if r.Spawned*2 >= base {
			continue
		}
		// In a read-only run every failure mode narrows by the runner's own
		// injection: `dead-finders` leaves nothing to verify, `dead-reviews`
		// and `blockers` leave nothing CONFIRMED, and a review that stops
		// after rounds with no confirmed finding stops early in all three.
		// Measured on the hunt script on disk, run as a review: 199, 261 and
		// 271 agents against 596 in `clean`, every one of them a correct dry
		// stop. So the ratio is a note there, never a violation — which also
		// means no read-only mode can catch an abort guard by its width, and
		// the note says so.
		if ro {
			r.Notes = append(r.Notes, fmt.Sprintf("%s: reached %d agent(s) against %d in `%s` — in a review this mode leaves nothing to verify or nothing confirmed, so a narrower run is expected; it is also why the width check cannot tell that from an abort guard here, and the phases named above are what to read", r.Mode, r.Spawned, base, baseMode))
			continue
		}
		// The MODE's name is in the text, because the tail of the run
		// deduplicates notes by their string: five collapsed modes printed as
		// one anonymous "this mode reached 1 agent(s)", on the line the output
		// says is the one an operator acts on.
		r.Violations = append(r.Violations, fmt.Sprintf("%s: reached %d agent(s) against %d in `%s` — it exercised a fraction of the script, and every check in this runner passed over that fraction; a failure branch that spawns nothing is a branch no mode covers",
			r.Mode, r.Spawned, base, baseMode))
	}
}

const zeroSpawn = "the mode completed without spawning a single agent — every check in this runner passed over an empty set"

// forgiveArgsGate downgrades the zero-spawn violation for `no-args` alone, and
// only when another mode DID spawn. A script that legitimately returns early
// without `args` is behaving correctly there; a script that spawns nothing in
// every mode is the false green this rule exists for, and the difference is
// only visible across modes.
func forgiveArgsGate(rs []Result) {
	spawnedElsewhere := false
	for _, r := range rs {
		if r.Mode != "no-args" && r.Spawned > 0 {
			spawnedElsewhere = true
		}
	}
	if !spawnedElsewhere {
		return
	}
	for i := range rs {
		if rs[i].Mode != "no-args" {
			continue
		}
		kept := rs[i].Violations[:0]
		for _, v := range rs[i].Violations {
			if strings.HasSuffix(v, zeroSpawn) {
				rs[i].Logs = append(rs[i].Logs, "note: no agent spawned — the script gates its body on `args`, and the other modes do spawn")
				rs[i].ReturnOK = rs[i].ReturnOK || rs[i].Return != nil
				continue
			}
			kept = append(kept, v)
		}
		rs[i].Violations = kept
	}
}

// neutraliseMeta blanks the `export` keyword of every top-level declaration and
// renames the `meta` binding to an equally long dead name, so every other byte —
// and therefore every reported line and column — stays where it was.
func neutraliseMeta(src, path string) (string, error) {
	sc, err := wfscript.LoadSource(src, path)
	if err != nil {
		return "", err
	}
	out := []byte(src)
	blankKeyword := func(at int) {
		// Walk back to the start of the DECLARATION's own line and blank an
		// `export` found there. The index handed in is the declared NAME's, never
		// the initialiser's: `export const meta =` with the `{` on the next line
		// put the initialiser on line 2, this walked line 2 only, `export`
		// survived, and goja died with a bare `SyntaxError: Line 1:19 Unexpected
		// reserved word` — 0/1 modes and no diagnosis, over a file `wfcheck` read
		// clean.
		i := at
		for i > 0 && out[i-1] != '\n' {
			i--
		}
		line := string(out[i:min(len(out), at)])
		if k := strings.Index(line, "export "); k >= 0 {
			copy(out[i+k:i+k+7], "       ")
		}
	}
	for name, c := range sc.Consts() {
		at := sc.NameOffset(c)
		if at < 0 {
			continue
		}
		blankKeyword(at)
		if name == "meta" {
			if at+4 <= len(out) && string(out[at:at+4]) == "meta" {
				copy(out[at:at+4], "_mtA")
			}
		}
	}
	// A surviving `export` is a SyntaxError the engine reports against a line and
	// a column with no word about the keyword — the shape that reaches here is an
	// export this loop does not iterate, `export function` or `export default`,
	// which the parse pre-pass blanks for the PARSER and this one leaves standing
	// for the ENGINE. The runner knows what it failed to neutralise, so it says
	// so rather than handing the operator goja's message.
	if line, ok := survivingExport(string(out)); ok {
		return "", fmt.Errorf("line %d still begins with `export`, which this runner could not neutralise — it neutralises `export const`, and goja parses scripts rather than modules, so the engine would die with a bare SyntaxError. Drop the keyword: `wfdry` never imports the file, and a workflow script is not imported by anything else either", line)
	}
	return string(out), nil
}

// survivingExport finds a line-initial `export ` left in the CODE — a string or
// a comment containing one is not module syntax, and jstext blanks those in
// place, so an offset that still spells the keyword there is code.
func survivingExport(src string) (int, bool) {
	code := jstext.StripNonCode(src)
	for i := 0; i+7 <= len(code); i++ {
		if (i == 0 || code[i-1] == '\n') && code[i:i+7] == "export " {
			return jstext.LineAt(src, i), true
		}
	}
	return 0, false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func runMode(src, mode string, o Options) Result {
	r := Result{Mode: mode, defaulted: map[string]int{}, badRefs: map[string]int{}, family: map[string]int{}}
	vm := goja.New()
	ro := ws.ReadOnly(normType(o.Type))

	dead := map[string]bool{}
	for _, d := range o.Dead {
		dead[d] = true
	}
	bad := func(label, msg string) {
		// Recorded OUT OF BAND and only then thrown. The guide requires a script
		// to wrap the spawn call and turn a throw into null, so a stub that
		// signals only by throwing is signalling into the one mechanism
		// guaranteed to swallow it: the defect would be recorded as a dead agent,
		// the mode would complete, and the runner would report OK. Measured on a
		// real script — three injected defects, three green runs. The violation
		// list is what makes the mode fail.
		r.Violations = append(r.Violations, label+": "+msg)
		panic(vm.ToValue(msg))
	}

	agent := func(call goja.FunctionCall) goja.Value {
		r.Spawned++
		var opts *goja.Object
		if v := call.Argument(1); !goja.IsUndefined(v) && !goja.IsNull(v) {
			opts = v.ToObject(vm)
		}
		get := func(k string) string {
			if opts == nil {
				return ""
			}
			v := opts.Get(k)
			if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
				return ""
			}
			return v.String()
		}
		label := get("label")
		if label == "" {
			label = fmt.Sprintf("agent@%d", r.Spawned)
		}
		model, effort := get("model"), get("effort")
		r.Calls = append(r.Calls, fmt.Sprintf("%s [%s/%s]", label, or(model, "NO MODEL"), or(effort, "NO EFFORT")))
		if p := call.Argument(0); !goja.IsUndefined(p) && !goja.IsNull(p) {
			if t := p.ExportType(); t != nil && t.Kind() == reflect.String {
				r.Prompts = append(r.Prompts, check.NamedText{Name: label, Text: p.String()})
			}
		}

		// A runtime restatement of the static checks — free once the harness
		// runs, and it catches a prompt composed to `undefined` on one branch.
		p := call.Argument(0)
		if goja.IsUndefined(p) || goja.IsNull(p) {
			bad(label, "prompt is undefined, not a string")
		}
		// ExportType, not String(): every value coerces to a string, so a
		// prompt composed to a number or an object passed silently.
		if t := p.ExportType(); t == nil || t.Kind() != reflect.String {
			bad(label, fmt.Sprintf("prompt is %v, not a string", p.ExportType()))
		}
		if strings.TrimSpace(p.String()) == "" {
			bad(label, "empty prompt")
		}
		if opts == nil {
			bad(label, "no options object")
		}
		if v := opts.Get("schema"); v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
			bad(label, "no schema")
		} else {
			// The width of what this call would put on the wire. Measured at
			// the spawn because that is where the composed object exists: on
			// the P3a script as it stands, 15 of 16 prompts — and their schemas
			// with them — are built at run time.
			js, serializes := serializedSchema(vm, v)
			n := -1
			if serializes {
				n = len(js)
			}
			// The census is taken only over a schema that SERIALIZES. Not a
			// convenience: the honest unserialisable case is a cycle, and a
			// census walking one does not return — the census is a statement
			// about what the launch would oblige an agent to fill, and a
			// schema the launch cannot send obliges nothing. `-1` says
			// unmeasured, which is a different claim from zero.
			req, noreq := -1, -1
			if n >= 0 {
				req, noreq = requiredCensus(v.Export())
				// The launch validates in STRICT mode and refuses the whole
				// schema over one unknown keyword, at spawn, before the agent
				// exists — so the stub dies here exactly as the agent would
				// die there. It is `bad` and not a WARN because this is not a
				// question the corpus brackets: it was probed, and the runner
				// that ADVISED the keyword is the reason it must now refuse it.
				if bogus := refusedKeywordsIn(js); len(bogus) > 0 {
					bad(label, fmt.Sprintf("its schema carries %s, which the launch's validator refuses as an unknown keyword (`strict mode: unknown keyword: %q`) — the whole schema is rejected at spawn and the agent never runs. A polarity declaration belongs in `$comment` (`\"$comment\": \"x-pass: true\"`), which is a standard annotation keyword and is accepted; renaming the field into the vocabulary this runner matches costs nothing at all",
						strings.Join(quoteAll(bogus), ", "), bogus[0]))
				}
			}
			r.Schemas = append(r.Schemas, SchemaUse{Label: label, Bytes: n, Required: req, NoRequired: noreq})
			// A width is a QUESTION and never fails a mode — the corpus
			// brackets the boundary and does not locate it. A schema that does
			// not SERIALIZE is not a width question at all: it is a launch that
			// cannot happen, and a WARN there would leave `7/7 modes completed`
			// and exit 0 standing over it, which is the shape the collapse check
			// was promoted out of one wave earlier.
			if n < 0 {
				bad(label, "its schema does not serialize — `JSON.stringify` returned nothing, so there is nothing for the launch to send")
			}
		}
		if model == "" {
			bad(label, "no model")
		}
		if effort == "" {
			bad(label, "no effort (the call would inherit the launching session)")
		}
		if !effortTiers[effort] {
			bad(label, fmt.Sprintf("effort %q is not one of low, medium, high, xhigh, max", effort))
		}
		if dead[label] {
			r.kill(label)
			panic(vm.ToValue("stubbed death of " + label))
		}
		if mode == "throw-serial" && r.Spawned == 1 {
			panic(vm.ToValue("stubbed throw at the first spawn"))
		}
		if (mode == "dead-reviews" && (reviewish(label) || (ro && criticish(label)))) || (mode == deadFindersMode && findish(label)) {
			r.kill(label)
			return promiseOf(vm, goja.Null())
		}
		// In a read-only run the Nth agent of one label family takes the
		// opposite vocabulary on its top-level severity, the way item 1 of an
		// array already does: every verifier came back `minor` in `clean`, so
		// the skeleton confirmed only minors and no mode confirmed a blocker.
		top := 0
		if ro {
			fam := firstSegment(label)
			top = r.family[fam] % 2
			r.family[fam]++
		}
		fixture := synth(opts.Get("schema"), mode, label, ro, top, 0, r.defaulted, r.badRefs)
		// One entry per DISTINCT fixture, not per label. A label spawned in a
		// loop usually synthesises the same shape and a list with a hundred
		// copies of it in is a list nobody reads — but a label whose schema is
		// built per item composes DIFFERENT ones, and keeping only the first
		// would hide exactly the case the flag exists for behind the ordinal
		// that happened to run first.
		r.addFixture(label, fixture)
		return promiseOf(vm, vm.ToValue(fixture))
	}

	must := func(name string, v any) {
		if err := vm.Set(name, v); err != nil {
			r.Err = err
		}
	}
	must("agent", agent)
	must("phase", func(t string) { r.Phases = append(r.Phases, t) })
	must("log", func(m string) { r.Logs = append(r.Logs, m) })
	must("workflow", func(goja.FunctionCall) goja.Value { return promiseOf(vm, vm.NewObject()) })

	spent := func() int { return r.Spawned * 20000 }
	budget := vm.NewObject()
	if o.Budget != nil {
		_ = budget.Set("total", *o.Budget)
	} else {
		_ = budget.Set("total", goja.Null())
	}
	_ = budget.Set("spent", func() int { return spent() })
	// With no target set, remaining() is Infinity — the value a `while
	// (budget.total && budget.remaining() > N)` loop is written against.
	_ = budget.Set("remaining", func() float64 {
		if o.Budget == nil {
			return math.Inf(1)
		}
		if rem := float64(*o.Budget - spent()); rem > 0 {
			return rem
		}
		return 0
	})
	must("budget", budget)

	if mode == "no-args" {
		must("args", goja.Undefined())
	} else {
		a := o.Args
		if a == "" {
			a = DefaultArgs
		}
		v, err := vm.RunString("(" + a + ")")
		if err != nil {
			r.Err = fmt.Errorf("--args is not valid JSON: %w", err)
			return r
		}
		must("args", v)
	}

	if _, err := vm.RunString(harness); err != nil {
		r.Err = fmt.Errorf("harness: %w", err)
		return r
	}
	if r.Err != nil {
		return r
	}

	// A per-mode deadline. wfcheck refuses an unguarded `while`, but a `for`
	// with a decrementing counter is a plain typo it does not judge — and a
	// runner that hangs forever, silently, is worse than any finding it could
	// have printed.
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	stop := time.AfterFunc(timeout, func() { vm.Interrupt("wfdry: deadline") })
	v, err := vm.RunString("(async function(){" + src + "\n})()")
	stop.Stop()
	if err != nil {
		var ie *goja.InterruptedError
		if errors.As(err, &ie) {
			r.Err = fmt.Errorf("the script did not finish within %s — an unbounded loop, or work a dry run cannot satisfy", timeout)
		} else {
			r.Err = jsErr(vm, err)
		}
		return r
	}
	p, ok := v.Export().(*goja.Promise)
	if !ok {
		r.Err = fmt.Errorf("the script body did not return a promise")
		return r
	}
	switch p.State() {
	case goja.PromiseStateRejected:
		r.Err = rejection(vm, p.Result())
		return r
	case goja.PromiseStatePending:
		r.Err = fmt.Errorf("the script never settled — an await with nothing to resolve it")
		return r
	}
	res := p.Result().Export()
	r.Return = res
	if _, isMap := res.(map[string]any); !isMap {
		r.Violations = append(r.Violations, fmt.Sprintf("the run returned %T, not an object — a verdict nothing can read", res))
	} else {
		r.ReturnOK = true
	}
	// The `clean` mode IS the run everyone wants: every producer alive, every
	// gate clean, acceptance green. A verdict field that comes back false there
	// is the script contradicting the only fixture it cannot argue with, and
	// P2f shipped exactly that — a repair agent spawned only when acceptance
	// FAILS was counted in the liveness ledger, so the happy path logged
	// `dead: repair` and returned `green: false`. Seven modes were green because
	// acceptance failed in all seven and repair was therefore always spawned;
	// the defect was found by an opus reviewer hand-executing the script's tail
	// with green values. That is the technique this check makes unnecessary.
	//
	// A kill is a failure the fixture DID produce, so the guard's premise —
	// nothing failed — is false the moment `--dead` names a label that spawned.
	// Without this, `--mode clean --dead <a reviewer>` reported the correct
	// verdict as a violation on every one of the five correct scripts on disk.
	if mode == "clean" && r.ReturnOK && len(r.Killed) > 0 {
		// Standing down in SILENCE is an absent measurement in the register of
		// a pass: `--dead build` over the banked false-green fixture took it
		// from 6/7 exit 1 to 7/7 exit 0, and nothing in the report said the
		// verdict had gone unchecked.
		r.Notes = append(r.Notes, "`--dead` killed "+strings.Join(r.Killed, ", ")+", so the clean-mode verdict guard stood down — a killed agent IS something failing, and this mode's OK does not cover the verdict")
	}
	// `empty` is held to the same verdict: the finders found nothing, which is
	// a result, and a run that reports it as a failure — or as incomplete —
	// cannot tell "the code is fine" from "nobody looked".
	//
	// In a read-only `clean` only a COVERAGE word is held true: the fixture there
	// IS "found defects", so a verdict about the code under review — `clean`,
	// `passed`, `ok` — honestly reads false (probe p8b was a false red). In
	// `empty` nothing was found and nothing failed, and every pass word holds.
	if (mode == "clean" || mode == emptyMode) && r.ReturnOK && len(r.Killed) == 0 {
		for _, k := range verdictKeys(r.Return) {
			if ro && mode == "clean" && !coverageWord.MatchString(k) {
				continue
			}
			b, _ := r.Return.(map[string]any)[k].(bool)
			switch {
			case passBools.MatchString(k) && !b:
				r.Violations = append(r.Violations, mode+": the run returned `"+k+": false` on the path where nothing failed — no fixture in this mode can produce that verdict, so the script derives it from something it should not."+fixtureAdvice+roAdvice(ro))
			case failVerdicts.MatchString(k) && b:
				r.Violations = append(r.Violations, mode+": the run returned `"+k+": true` on the path where nothing failed — no fixture in this mode can produce that verdict, so the script derives it from something it should not."+fixtureAdvice+roAdvice(ro))
			}
		}
	}
	// The same statement from the other side, and the whole point of the mode.
	// Every agent that survived reported SUCCESS — that is what the passing
	// fixture buys — so the ONLY thing wrong with this run is that the reviewers
	// returned nothing. A verdict that still reads as success is a script whose
	// green does not depend on its gates having run, which is the one defect a
	// fan-out cannot survive: the run reports OK and nobody reviewed anything.
	// Measured before it was written: the shipped mode returned `green: false`
	// on five correct scripts AND on a script filtering its dead reviewers out
	// of the verdict, so it discriminated nothing; under this fixture the five
	// still return false and the defective one returns true.
	//
	// `len(r.Killed) > 0` is the arming condition, and it is a DIFFERENT field
	// from the verdict this constrains: a producer-only script has no reviewer
	// label, kills nobody, and is correct to come back green — the note below
	// already says that run proved nothing.
	if (mode == "dead-reviews" || mode == deadFindersMode) && r.ReturnOK && len(r.Killed) > 0 {
		// A verdict this guard cannot READ is not a verdict it checked. Three
		// one-line variants of the banked fixture — the verdict nested in an
		// object, under an unrecognised key, or as the string 'yes' — all came
		// back 7/7 at exit 0 with the loop passing over an empty set. The
		// runner has recorded unreadable SCHEMA booleans since the sixth wave;
		// the return value got nothing.
		if len(verdictKeys(r.Return)) == 0 {
			r.Notes = append(r.Notes, "this mode killed "+plural(len(r.Killed), "agent")+" and the run's verdict carries no boolean field this check can read — whether the deaths reached the verdict is UNMEASURED, not clean")
		}
		// The verdict is read AS A WHOLE, and that is a correction: firing on
		// each success-looking field alone rejected `{done: true, green: false}`
		// — a correct script whose `done` is a step flag and whose `green` is
		// the verdict, reporting failure exactly as the other half of this
		// guard demands. A run that says failure ANYWHERE has noticed the
		// deaths; only a report that reads as success in every field it states
		// is the defect.
		m, _ := r.Return.(map[string]any)
		keys := verdictKeys(r.Return)
		var pass []string
		saidFail := false
		for _, k := range keys {
			b, _ := m[k].(bool)
			switch {
			case passBools.MatchString(k) && b, failVerdicts.MatchString(k) && !b:
				pass = append(pass, "`"+k+": "+strconv.FormatBool(b)+"`")
			case passBools.MatchString(k) && !b, failVerdicts.MatchString(k) && b:
				saidFail = true
			}
		}
		if len(pass) > 0 && !saidFail {
			r.Violations = append(r.Violations, mode+": the run returned "+strings.Join(pass, ", ")+" and nothing to the contrary, while "+plural(len(r.Killed), "agent")+" it spawned returned NOTHING ("+strings.Join(r.Killed, ", ")+") — every other agent reported success, so this verdict does not depend on those agents having run")
		}
	}
	// A mode that completes having spawned NOTHING is indistinguishable from a
	// clean run, and every stub check above it is then a guard over the empty
	// set. Measured: a script whose only spawn sat behind a false flag passed
	// 6/6, and one that returned immediately passed 6/6 with zero phases too.
	if r.Spawned == 0 {
		r.Violations = append(r.Violations, "run: "+zeroSpawn)
	}
	if mode == "dead-reviews" && r.Spawned > 0 && len(r.Killed) == 0 {
		// The mode is supposed to walk the unreviewed-gate accounting. If no
		// label looked like a review, it walked nothing and said OK — a guard
		// over an empty set wearing a green verdict.
		segs := "review/fix/closeout/verify/accept"
		if ro {
			segs += "/critic/skeptic"
		}
		r.Notes = append(r.Notes, "no label matched a "+segs+" segment — this mode killed nobody and proved nothing")
	}
	// In a review the finders are not optional, so a `dead-finders` that killed
	// nobody is a VIOLATION, not the note above: the coverage accounting — a
	// dead finder reported as a hole — then ran in no mode, and a script that
	// drops dead finders from its verdict passed every one (probe p6).
	if mode == deadFindersMode && r.Spawned > 0 && len(r.Killed) == 0 {
		r.Violations = append(r.Violations, deadFindersMode+": no label matched a "+strings.Join(sortedKeys(findSegments), "/")+" segment — this mode killed nobody, so a dead finder's accounting ran in no mode. Label the finders `find:<lens>:<unit>`, or name them with --dead")
	}
	// `empty` claims to walk "the review found nothing". If the run's schemas
	// carry no array it can read as a deliverable, it emptied nothing and walked
	// the clean path under another name — measured, a finder array renamed
	// `results` gave `empty` the same agents and phases as `clean`, in silence.
	if ro && mode == emptyMode && r.Spawned > 0 && len(prefixed(r.defaulted, deliverableKey)) == 0 {
		r.Violations = append(r.Violations, emptyMode+": no finding-shaped array was synthesised, so this mode emptied nothing and walked the clean path — name the finders' array one of findings/issues/defects/problems, or declare `\"$comment\": \"x-pass: true\"` on it")
	}
	// A label the operator NAMED that never spawned. `--dead reviwer` is a
	// clean run of every guard over the empty set, reported as OK: the same
	// shape one line up, with the operator rather than the matcher choosing
	// wrong.
	var never []string
	for _, d := range o.Dead {
		if !r.killedLabel(d) {
			never = append(never, d)
		}
	}
	if len(never) > 0 && r.Spawned > 0 {
		r.Notes = append(r.Notes, "--dead named "+strings.Join(never, ", ")+", which no spawn in this mode used as a label — nothing was stubbed dead for them")
	}
	// The consequence, not the observation. P2f: `done` is the completion flag of
	// every BUILD_SCHEMA in that project, it matched neither boolean list, and
	// so it was false in all seven modes — including `clean`. The clean path was
	// therefore never executed by any mode, and the defect living there (a
	// repair agent, spawned only when acceptance fails, counted in the liveness
	// ledger, so the one run everyone wants returned not-green) survived seven
	// green modes. The note existed and said "defaulted them"; it was the
	// sixteenth line of a fifteen-hundred-line dump, filed under `log:` beside
	// the script's own output, and the operator read past it three times.
	// A `$ref` this runner could not expand is named, with the reason. The
	// resolvable ones are silent because they are now the normal case; an
	// unresolvable one leaves the referenced leaf a placeholder STRING, which
	// is exactly the shape that took one gate's clean mode to green:false with
	// nothing saying why.
	if len(r.badRefs) > 0 {
		refs := make([]string, 0, len(r.badRefs))
		for n := range r.badRefs {
			refs = append(refs, n)
		}
		sort.Strings(refs)
		r.Notes = append(r.Notes, "unresolved $ref — "+strings.Join(refs, "; ")+" — the referenced node was filled with a placeholder STRING rather than the object it declares, so every term reading through it is false in this mode. The launch DOES resolve `$defs`/`$ref` and enforces the `minimum`/`minLength` that reach a leaf through one (probed), so this is the runner losing the schema, not the launch")
	}
	// An array's unreadable polarity is a different claim from a boolean's
	// unrecognised NAME, and it travels in the same map under a prefix so that
	// one note cannot describe the other. Every clause of the boolean note is
	// wrong about an array: it did not come back false, it is not matched
	// against the completion vocabulary, and the remedy it offers is the
	// annotation the author just wrote.
	if bad := prefixed(r.defaulted, arrayPolarityKey); len(bad) > 0 {
		r.Notes = append(r.Notes, "unreadable `x-pass` in the `$comment` of array field(s) — "+strings.Join(bad, ", ")+" — so the array's polarity was decided by its NAME after all: an array is returned EMPTY in the passing modes only when its name carries one of the defect words ("+arrayVocabulary+"), and otherwise holds one placeholder in every mode. Write it as `\"$comment\": \"x-pass: false\"` — a populated array is then the failure shape — or `true` for the opposite; the value must read as true or false. It lives in `$comment` and NOT as a schema keyword because the launch refuses the whole schema over an unknown keyword (probed)")
	}
	// The field the clean-mode verdict guard could not name. That guard says "no
	// fixture in this mode can produce that verdict" and stops; when the verdict
	// is derived from a schema boolean this synthesiser set from its NAME, the
	// operator has no way from the report to the field. This note is that way.
	if named := prefixed(r.defaulted, boolPolarityKey); len(named) > 0 {
		r.Notes = append(r.Notes, "boolean(s) this mode valued from their NAME — "+strings.Join(named, "; ")+" — a verdict derived from one of them was derived from this runner's vocabulary, not from the script. If the polarity is wrong, declare it: `\"$comment\": \"x-pass: true\"` on the field, or false for a failure flag — NOT as a schema keyword, which the launch refuses")
	}
	if enums := prefixed(r.defaulted, enumFallbackKey); len(enums) > 0 {
		r.Notes = append(r.Notes, "enum field(s) none of whose members reads as pass or fail — "+strings.Join(enums, ", ")+" — got their FIRST member in this mode: harmless for a category label, but a branch on one of them ran on one side only")
	}
	if names := prefixed(r.defaulted, ""); len(names) > 0 {
		// The NAME is the contract, and the note has to say so. Without this
		// clause the message named the fields and not the rule they broke, so
		// the vocabulary could only be found by reading this file — three field
		// names were spent doing that on one gate. Renaming is offered FIRST
		// because it is free: the declaration rides in the schema the launch's
		// safety classifier reads and widens it, which is the other failure this
		// runner now measures. And it is offered in `$comment` and NOT as a
		// schema keyword — as a keyword the launch refuses the whole schema,
		// which is how this very note once cost a run 26 of 27 properties.
		r.Notes = append(r.Notes, "unrecognised report field(s) — "+strings.Join(names, ", ")+" — were FALSE in this mode: no branch guarded by one of them was exercised, and the mode's OK covers none of it. A boolean is read as pass-shaped only by its NAME ("+passBoolVocabulary()+"); rename the field, or declare its polarity in its `$comment` — `\"$comment\": \"x-pass: true\"`, or false for a failure flag — which costs bytes in the schema the classifier reads. NOT as a schema keyword: the launch refuses the whole schema over `x-pass` as a key, which is how this note once cost a run 26 of 27 properties")
	}
	return r
}

// serializedWidth is len(JSON.stringify(v)) computed by the SAME engine that
// composed the value, so an operator can compare it against the number a
// refusal quotes. -1 says the value does not serialize at all, which is its own
// answer: a schema carrying a function or a cycle is not a schema the launch
// can send.
func serializedWidth(vm *goja.Runtime, v goja.Value) int {
	s, ok := serializedSchema(vm, v)
	if !ok {
		return -1
	}
	return len(s)
}

// serializedSchema is the JSON the launch would receive, from the same engine
// that composed it. It is the ONE reading behind both the width and the keyword
// check, because two derivations of one quantity is this project's most
// repeated defect class — and because the two answer differently: the composed
// OBJECT can carry keys that never reach the wire, since `JSON.stringify` drops
// a key whose value is `undefined` or a function. Walking the object would
// refuse a schema over a key the validator never sees.
func serializedSchema(vm *goja.Runtime, v goja.Value) (string, bool) {
	j, ok := vm.Get("JSON").(*goja.Object)
	if !ok || j == nil {
		return "", false
	}
	fn, ok := goja.AssertFunction(j.Get("stringify"))
	if !ok {
		return "", false
	}
	s, err := fn(goja.Undefined(), v)
	// A THROW comes back as an error with a nil value, so `s == nil` was the same
	// condition spelled twice and neither spelling could be killed on its own.
	// UNDEFINED is a different case and a real one: `JSON.stringify` of a
	// function returns undefined, and a schema built by a factory that was passed
	// rather than called is exactly that. A null return is not a case at all —
	// stringify of null is the STRING "null" — so the branch for it was deleted
	// rather than shipped unprovable.
	if err != nil || goja.IsUndefined(s) {
		return "", false
	}
	return s.String(), true
}

// jsErr and rejection keep the LOCATION. Printing only the message left a
// TypeError on line 7 of a 900-line script reporting no line at all.
func jsErr(vm *goja.Runtime, err error) error {
	var ex *goja.Exception
	if errors.As(err, &ex) {
		return fmt.Errorf("%s", ex.String())
	}
	return err
}

func rejection(vm *goja.Runtime, v goja.Value) error {
	if o, ok := v.(*goja.Object); ok && o != nil {
		if st := o.Get("stack"); st != nil && !goja.IsUndefined(st) {
			return fmt.Errorf("%s", st.String())
		}
	}
	return fmt.Errorf("%v", v)
}

func or(a, b string) string {
	if a == "" {
		return b
	}
	return a
}

// missingPhases is the baseline's phase titles that this mode never reached, in
// the baseline's own order. Titles, not counts — see the caller.
func missingPhases(base, got []string) []string {
	reached := make(map[string]bool, len(got))
	for _, t := range got {
		reached[t] = true
	}
	var missing []string
	seen := map[string]bool{}
	for _, t := range base {
		if !reached[t] && !seen[t] {
			seen[t] = true
			missing = append(missing, t)
		}
	}
	return missing
}

// verdictKeys is the run's boolean report fields in a stable order, so two
// guards reading the same verdict cannot disagree about which fields they saw.
func verdictKeys(ret any) []string {
	m, _ := ret.(map[string]any)
	keys := make([]string, 0, len(m))
	for k := range m {
		if _, isBool := m[k].(bool); isBool {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// reviewish decides whether `dead-reviews` kills a label. It reads the label's
// SEGMENTS, not the label as one string, and that is a measured correction: the
// first version asked `strings.Contains(label, "review")`, so a producer
// labelled `preview` was killed as a reviewer. On P3a that ended the run at
// section five over a seventh of the agents the mode exists to exercise, and
// the mode reported OK — without ever killing a gate reviewer, which is its
// entire purpose. A dry mode silently measuring something else is the same
// shape as the vacuous guard this runner exists to catch.
//
// A segment MATCHES when it IS one of a closed set of spellings. Prefix
// matching was the first repair and it traded one over-kill class for a smaller
// one: `fix` is a prefix of `fixture`, so a fixture-building agent would be
// killed as a fixer — the same defect the prefix was fixing, one word along. A
// closed list is auditable, which the severity scale and `-asks` already are,
// and a spelling nobody thought of is an agent NOT killed rather than the wrong
// agent killed. The zero-kill note is what says so.
func reviewish(label string) bool {
	for _, seg := range labelSegments(label) {
		if reviewSegments[seg] {
			return true
		}
	}
	return false
}

var reviewSegments = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range []string{
		"review", "reviews", "reviewer", "reviewers", "reviewing", "rereview", "reread",
		"fix", "fixes", "fixer", "fixers", "fixing", "repair", "repairs",
		"closeout", "closeouts", "closing", "close",
		"verify", "verifies", "verifier", "verifiers", "verifying", "verification", "verified",
		"accept", "accepts", "acceptance", "accepting", "acceptor",
	} {
		m[w] = true
	}
	return m
}()

// coverageWord is the slice of the pass vocabulary a read-only `clean` holds
// true: completion, not the outcome of the review.
var coverageWord = regexp.MustCompile(`(?i)(complete|completed|done|finished)$`)

func roAdvice(ro bool) string {
	if !ro {
		return ""
	}
	return " In a review a finding-shaped array is the deliverable and is POPULATED here; if one of this run's arrays is a defect list instead — a critic grading the report — declare `\"$comment\": \"x-pass: false\"` on it."
}

func firstSegment(label string) string {
	if segs := labelSegments(label); len(segs) > 0 {
		return segs[0]
	}
	return label
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// criticish adds the read-only types' own reviewers to `dead-reviews`: a critic
// grading a review's coverage, a skeptic re-judging a finding.
func criticish(label string) bool {
	for _, seg := range labelSegments(label) {
		switch seg {
		case "critic", "critics", "skeptic", "skeptics":
			return true
		}
	}
	return false
}

// findish is reviewish for the read-only types, whose finders are the
// reviewers: a review that filters a dead finder out of its coverage and calls
// itself complete reviewed less than it says. Read-only only, because in a
// build a label carrying `find` may well be a producer, and killing a producer
// here is the over-kill the closed list above exists against.
func findish(label string) bool {
	for _, seg := range labelSegments(label) {
		if findSegments[seg] {
			return true
		}
	}
	return false
}

var findSegments = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range []string{
		"find", "finds", "finder", "finders",
		"hunt", "hunts", "hunter", "hunters",
		"scan", "scans", "scanner", "scanners",
		"audit", "audits", "auditor", "auditors",
		"lens", "lenses",
		"inspect", "inspector", "inspectors",
		"analyze", "analyzer", "analyzers", "analyst", "analysts",
	} {
		m[w] = true
	}
	return m
}()

// labelSegments splits a label the way its author wrote it: on every
// non-alphanumeric character and at every camelCase boundary. `review:store`,
// `review-store` and `reviewStore` are one reviewer under three spellings.
func labelSegments(label string) []string {
	var out []string
	cur := make([]rune, 0, len(label))
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	prevLower := false
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z':
			cur = append(cur, r)
			prevLower = true
		case r >= '0' && r <= '9':
			cur = append(cur, r)
			// A digit is a lower for the camelCase boundary: with it resetting
			// the state, `gate1Review` stayed one segment and the reviewer in
			// it was never killed.
			prevLower = true
		case r >= 'A' && r <= 'Z':
			if prevLower {
				flush()
			}
			cur = append(cur, r)
			prevLower = false
		default:
			flush()
			prevLower = false
		}
	}
	flush()
	return out
}

func promiseOf(vm *goja.Runtime, v goja.Value) goja.Value {
	p, resolve, _ := vm.NewPromise()
	_ = resolve(v)
	return vm.ToValue(p)
}

// harness defines the orchestration hooks that are pure JavaScript. They are
// the real semantics, restated: parallel() is a barrier that turns a throwing
// thunk into null, pipeline() runs each item through every stage independently.
const harness = `
globalThis.parallel = async (thunks) =>
  Promise.all(thunks.map((t) => Promise.resolve().then(t).catch(() => null)))
globalThis.pipeline = async (items, ...stages) =>
  Promise.all(items.map(async (it, i) => {
    let acc = it
    try { for (const st of stages) acc = await st(acc, it, i) } catch { return null }
    return acc
  }))
`

// ---------------------------------------------------------- the fixture contract --
//
// A stub cannot invent a report shape, so it derives one from the `schema` the
// script hands each call — that schema IS the contract — plus the well-known
// field names below, which carry a mode-dependent value. A script whose report
// fields fall outside the set still runs; it just gets placeholder strings,
// which is enough for control flow.

var failArrays = map[string]bool{
	"blockers": true, "majors": true, "failures": true, "unfixed": true,
	"openblockers": true, "concerns": true, "dead": true, "unverifiedgates": true,
	"violations": true, "gaps": true, "risks": true,
}

var failBools = regexp.MustCompile(`(?i)fail|broken|vacuous|missing|stale|dead|unreviewed|block|defect|error|violation|unfixed|open`)

// failArrayRe widens the closed list to the shape of the NAME. The list alone
// made `defects` a "good" array — one element even in `clean` — so the mode the
// package calls the one that matters most walked the failure branch, and the
// happy path was executed in no mode at all.
// The two vocabularies that read an enum MEMBER, kept apart from failArrayRe
// below, which reads an array's NAME — reusing that one picked `not-run` out of
// `['pass','fail','not-run']` for the CLEAN mode, because "not-run" names no
// failure word while being exactly a failure.
//
// They are matched as WHOLE TOKENS, never as substrings, and a member carrying
// a negator belongs to neither list. The first version matched substrings and
// fell through to a POSITION, which its own comment claimed it never did, and
// the result was measured wrong on every vocabulary tried — including two real
// ones out of `~/gates/p2e/artifacts/p2e.workflow.mjs`:
//
//	['complete','partial']            clean=partial    failing=complete   INVERTED
//	['clean','advisory','blocked']    clean=clean      failing=clean      no failure path
//	['ready','degraded','down']       clean=down       failing=ready      INVERTED
//	['broken','fixed']                clean=broken     failing=broken     "ok" inside br-OK-en
//	['not-ok','ok']                   clean=not-ok     failing=not-ok
//	['error-free','error']            clean=error      failing=error-free INVERTED
//
// Before enums were honoured at all those branches were merely unreachable;
// picking the wrong member makes them reachable and BACKWARDS, so a failure mode
// walks the happy path while the runner prints OK. Unreachable is a gap;-
// inverted is a lie, and this file exists to refuse the second.
var enumPassWords = []string{
	"pass", "ok", "green", "clean", "success", "succeed", "met", "none", "note",
	"minor", "advisory", "info", "yes", "true", "fixed", "done", "complete",
	"ready", "up", "healthy", "closed", "resolved",
}
var enumFailWords = []string{
	"blocker", "block", "blocked", "fail", "failure", "error", "reject", "refuse",
	"refused", "violation", "unfixed", "notrun", "red", "major", "partial",
	"broken", "missing", "stale", "dead", "defect", "skip", "vacuous",
	"degraded", "down", "no", "false", "open",
}

// enumNegators flip a member out of BOTH vocabularies: "not-ok" is not a pass
// word because it contains one, and "error-free" is not a failure word for the
// same reason. Without this, substring matching read each of them as its own
// opposite.
var enumNegators = []string{"not", "non", "free", "less", "without", "never"}

// boolNegators is that list plus `no`, and the boolean side carries `no` where
// the enum side must not: in an enum `no` is a MEMBER meaning failure
// (`['yes','no']`, and enumFailWords lists it), while in a field NAME it is the
// prefix that inverts the word after it — `noFailures`.
var boolNegators = append([]string{"no"}, enumNegators...)

// boolNegated reports a field NAME carrying a negator as a WHOLE token.
//
// Whole, not a prefix: `hasWord` matches prefixes so a plural finds its singular
// (`blockers` finds `blocker`), and a negator has no plural — under the prefix
// rule `noticedErrors` matched `not` and a failure flag became a pass flag,
// which is the inversion this file exists to refuse.
func boolNegated(name string) bool {
	for _, t := range camelTokens(name) {
		for _, w := range boolNegators {
			if t == w {
				return true
			}
		}
	}
	return false
}

// camelTokens splits a field NAME the way enumTokens splits an enum member, plus
// the lower→upper transition. The two differ deliberately: a field is written
// `noFailures`, an enum MEMBER is written `not-run`, and enumTokens leaves
// camelCase alone so an enum spelled that way falls to the path that SAYS it was
// unread rather than being guessed at.
func camelTokens(name string) []string {
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		c := name[i]
		if i > 0 && c >= 'A' && c <= 'Z' && name[i-1] >= 'a' && name[i-1] <= 'z' {
			b.WriteByte(' ')
		}
		b.WriteByte(c)
	}
	return enumTokens(b.String())
}

// enumTokens splits a member into comparable words: lower-case, and cut on
// anything that is not a letter or a digit, so `not-run`, `not_run`, `notRun`
// and `NOT RUN` are one shape. camelCase is not split — a member written that
// way reads as one token and simply falls to the unrecognised path, which SAYS
// so rather than guessing.
func enumTokens(member string) []string {
	var out []string
	cur := strings.Builder{}
	for _, r := range strings.ToLower(member) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
			continue
		}
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func hasWord(tokens, words []string) bool {
	for _, t := range tokens {
		for _, w := range words {
			// Prefix, so a plural reads as its singular: `['pass','blockers']`
			// must find "blockers" for a failing mode.
			if strings.HasPrefix(t, w) {
				return true
			}
		}
	}
	return false
}

func enumReads(member string, words []string) bool {
	tokens := enumTokens(member)
	if hasWord(tokens, enumNegators) {
		return false
	}
	return hasWord(tokens, words)
}

// pickEnum chooses one member of a declared enum for the mode being walked, and
// reports through rec when it could not tell — the boolean branch below has
// recorded its unrecognised names since the sixth wave, and an enum this runner
// cannot read is the same claim: the mode that was supposed to walk a branch may
// not have walked it.
func pickEnum(name string, members []string, mode string, idx int, rec map[string]int) string {
	words := enumFailWords
	if passFixture(mode) {
		words = enumPassWords
	}
	// Item 1 of a report array takes the opposite vocabulary, so one array
	// carries a blocker AND a non-blocker and both sides of the script's own
	// filter run. Item 0 keeps the mode's meaning, so every existing verdict
	// still reads the way its mode intends.
	if idx > 0 {
		if passFixture(mode) {
			words = enumFailWords
		} else {
			words = enumPassWords
		}
	}
	for _, m := range members {
		if enumReads(m, words) {
			return m
		}
	}
	// No guess dressed as a choice. The first member is a deterministic
	// fallback, named as one.
	if rec != nil {
		rec[enumFallbackKey+name+" (enum "+strings.Join(members, "|")+")"]++
	}
	return members[0]
}

var severityName = regexp.MustCompile(`(?i)severity|level|priority`)

// deliverableRe is the read-only types' finding vocabulary: the subset of
// failArrayRe that names what a review RETURNS rather than what went wrong.
//
// ANCHORED, the whole name: a substring rule matched `unverifiedFindings` and
// `openIssues` — failure-qualified lists — and gave them deliverable polarity.
// A deliverable under another name declares `"$comment": "x-pass: true"`.
var deliverableRe = regexp.MustCompile(`^(findings?|issues?|defects?|problems?)$`)

var failArrayRe = regexp.MustCompile(`(?i)blocker|major|failure|unfixed|open|concern|dead|unverified|violation|gap|risk|defect|error|issue|problem|finding`)

// `done` and its synonyms belong here for the same reason `ok` and `green` do:
// they are the completion flag of a report, and a completion flag that reads
// false in the mode named `clean` inverts every branch downstream of it. P2f
// measured the cost — seven green modes over a script whose happy path returned
// not-green, found in the end by an opus reviewer copying the script's tail into
// a file and executing it with green values by hand.
// The fail side of the clean-mode verdict guard is an ANCHORED list, not the
// substring regex the fixture synthesiser uses. That regex matches `fail` inside
// `noFailures` and `error` inside `errorFree` — names a script legitimately
// returns TRUE on a clean run — and a guard that FAILS the bar on one of those
// is a false red, which is worse than the silence it replaces. Same bargain as
// the numeric mask in `check dup`: an exact boundary with no noise population,
// rather than a loose one that has to be argued about.
var failVerdicts = regexp.MustCompile(`(?i)^(failed|failing|blocked|broken|vacuous|dead|unfixed|stale|unreviewed)$`)

// The optional qualifier and the `met` family were added when a skeleton the
// tool itself emits returned `goalMet` — completion vocabulary the list did not
// carry, so the CLEAN mode read false and correctly failed a script that was
// correct. Still anchored, and still a list: the alternative is the substring
// regex the comment above rejects, which matches `fail` inside `noFailures`.
var (
	passBoolPrefixes = []string{"goal", "scope", "slice", "section", "acceptance", "criteria", "criterion"}
	passBoolWords    = []string{"passed", "ok", "green", "clean", "verified", "proven", "done", "complete", "completed", "succeeded", "finished", "met", "satisfied", "achieved"}
	passBools        = regexp.MustCompile(`(?i)^(` + strings.Join(passBoolPrefixes, "|") + `)?(` + strings.Join(passBoolWords, "|") + `)$`)
)

// passBoolVocabulary is what the note prints, and it is built from the SAME
// slices the matcher compiles, because a message quoting a vocabulary it does
// not share is this project's most-repeated defect. Three field names were
// burned discovering this list by reading the Go source — `held`, `mutationMet`
// and `smokeAchieved`, each of which made `green` unreachable and failed the
// clean mode correctly, with nothing in the output saying that the NAME was
// what went unrecognised.
// arrayPolarityKey prefixes an array's unreadable `x-pass` inside the same
// record map, so the two notes cannot describe each other's population.
const arrayPolarityKey = "array x-pass: "

// boolPolarityKey prefixes a boolean whose value this mode decided from its NAME
// rather than from a declared polarity. It travels in the same map under its own
// prefix for the same reason arrayPolarityKey does: the unrecognised-field note
// says the field was NOT recognised, which is the opposite claim.
const boolPolarityKey = "bool by name: "

// enumFallbackKey prefixes an enum none of whose members carries pass or fail
// vocabulary. It was recorded under the unrecognised-BOOLEAN note, which then
// told a review author that a finding's `category (enum money|data-loss|…)`
// "was FALSE in this mode" — on every mode of both read-only scripts on disk,
// about a categorical label no branch depended on.
const enumFallbackKey = "enum by first member: "

// deliverableKey records a pass-shaped array — a review's deliverable — that a
// fixture synthesised. It is not a note: `empty` reads it to know whether it
// emptied anything at all.
const deliverableKey = "deliverable: "

// prefixed splits that map: with a prefix it returns the matching names with
// the prefix stripped, and with the empty one the names carrying NO prefix.
func prefixed(rec map[string]int, prefix string) []string {
	var out []string
	for n := range rec {
		switch {
		case prefix != "" && strings.HasPrefix(n, prefix):
			out = append(out, strings.TrimPrefix(n, prefix))
		case prefix == "" && !strings.HasPrefix(n, arrayPolarityKey) && !strings.HasPrefix(n, boolPolarityKey) && !strings.HasPrefix(n, enumFallbackKey) && !strings.HasPrefix(n, deliverableKey):
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// arrayVocabulary is the defect-word list an ARRAY's name is matched against,
// spelled for a reader. It is derived from the regex rather than retyped beside
// it: a vocabulary quoted in a message and edited in one place is this
// project's most repeated defect.
var arrayVocabulary = strings.ReplaceAll(strings.TrimPrefix(failArrayRe.String(), "(?i)"), "|", "/")

func passBoolVocabulary() string {
	return "one of " + strings.Join(passBoolWords, "/") +
		", optionally prefixed by " + strings.Join(passBoolPrefixes, "/")
}

func synth(schema goja.Value, mode, label string, ro bool, top, depth int, rec, refs map[string]int) any {
	if schema == nil || goja.IsUndefined(schema) || goja.IsNull(schema) || depth > 6 {
		return map[string]any{}
	}
	obj, ok := schema.Export().(map[string]any)
	if !ok {
		return map[string]any{}
	}
	// `$ref` is expanded ONCE, over the whole schema, before anything is
	// synthesised — the launch resolves it, and a runner that does not fills
	// the referenced leaf with a placeholder STRING where an object was
	// declared. See resolveRefs for the measurement.
	if resolved, ok := resolveRefs(obj, obj, refs).(map[string]any); ok {
		obj = resolved
	}
	if t, _ := obj["type"].(string); t == "array" {
		return arrayOf("", obj, mode, label, ro, depth, rec)
	}
	props, _ := obj["properties"].(map[string]any)
	out := map[string]any{}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		pv, _ := props[k].(map[string]any)
		// The top-level schema is one object, never an array element.
		out[k] = value(k, pv, mode, label, ro, depth, top, rec)
	}
	return out
}

func arrayOf(name string, v map[string]any, mode, label string, ro bool, depth int, rec map[string]int) []any {
	low := strings.ToLower(name)
	fail := failArrays[low] || failArrayRe.MatchString(low)
	// An explicit polarity beats every matcher here too, and an array needed it
	// more than a boolean did: the vocabulary is a list of DEFECT words, so an
	// array whose emptiness is the pass shape under any other name is filled in
	// every mode and no `length === 0` term over it can ever be true. Measured
	// on the P3d gate, which spent two rounds reaching for exactly that term:
	// `unheldProperties` matches no defect word, came back holding one
	// placeholder in the clean mode, and a reviewer's proposal to change the
	// item TYPE was tried and changed nothing — the discriminator was never the
	// type, it is the NAME, and the annotation is the way out that does not
	// require renaming a field away from its subject.
	//
	// The polarity is the array's POPULATED state, exactly as for a boolean:
	// `false` says a populated array is the failure shape (empty in the passing
	// modes), `true` says it is the pass shape (empty in `acceptfail`).
	if xp, state := declaredPolarity(v); state == polarityOK {
		if xp && rec != nil {
			rec[deliverableKey+name]++
		}
		return itemsOf(name, v, mode, label, ro, depth, rec, xpassCount(xp, mode))
	} else if state == polarityBad {
		// Present and unreadable, named rather than ignored: the author who
		// reached for the escape hatch would otherwise see the name-based
		// behaviour they were escaping, with nothing saying their spelling was
		// seen and rejected. It goes to its OWN note: the boolean one says the
		// field was FALSE in this mode and quotes the completion vocabulary,
		// and every one of those clauses is wrong about an array — it came back
		// holding a placeholder, it is matched against the DEFECT vocabulary,
		// and the remedy that note offers is the annotation the author just
		// wrote.
		if rec != nil {
			rec[arrayPolarityKey+name]++
		}
	}
	// In a read-only run a finding-shaped array is the DELIVERABLE, and the
	// polarity flips: populated in every mode but `empty`, and populated with
	// the mixed PAIR, because a review's happy path is "found some, of mixed
	// severity" — one item never reaches a merge step guarded by `length > 1`,
	// and never runs both sides of a severity filter. Measured on the two
	// read-only scripts on disk before the type existed: the passing fixture
	// emptied these arrays, so every verify, critic and merge label of one of
	// them was "required on a FAILURE branch (no clean mode composed this
	// label)". An array that is a finding list in name and a defect list in
	// meaning — a critic grading the report — declares `x-pass: false`, which
	// the branch above reads first.
	if ro && deliverableRe.MatchString(low) {
		if rec != nil {
			rec[deliverableKey+name]++
		}
		n := 2
		if mode == emptyMode {
			n = 0
		}
		return itemsOf(name, v, mode, label, ro, depth, rec, n)
	}
	n := 1
	if fail {
		if passFixture(mode) {
			n = 0
		}
	}
	switch low {
	case "failures":
		n = 0
		if mode == "acceptfail" {
			n = 1
		}
	case "unfixed":
		n = 0
		if mode == "unfixed" {
			n = 1
		}
	}
	// A failing mode's report array holds TWO items, not one: a real reviewer
	// returns a mix, and an array whose every element is identical exercises
	// only one side of every `filter(f => f.severity === 'blocker')` in the
	// script under test. Measured on the emitted skeleton — with one item, the
	// non-blocking bucket was empty in every failing mode, so the line that
	// collects it was executed by NO mode at all and its deletion passed every
	// bar. The clean mode keeps one benign item: a clean run must stay green.
	if n == 1 && !passFixture(mode) && fail {
		n = 2
	}
	return itemsOf(name, v, mode, label, ro, depth, rec, n)
}

// xpassCount is the declared polarity as a cardinality, and it is the boolean
// rule one type over: a pass-shaped array is populated everywhere but in the
// mode that fails acceptance, and a failure-shaped one is empty in the passing
// modes and holds the MIXED pair everywhere else.
func xpassCount(xp bool, mode string) int {
	if xp {
		// `empty` is the mode in which nothing was found, so a pass-shaped
		// array — a confirmed list — is empty there too.
		if mode == "acceptfail" || mode == emptyMode {
			return 0
		}
		return 1
	}
	if passFixture(mode) {
		return 0
	}
	return 2
}

func itemsOf(name string, v map[string]any, mode, label string, ro bool, depth int, rec map[string]int, n int) []any {
	items, _ := v["items"].(map[string]any)
	out := make([]any, 0, n)
	for i := 0; i < n; i++ {
		if t, _ := items["type"].(string); t == "object" {
			out = append(out, synthMap(items, mode, label, ro, depth+1, i, rec))
		} else {
			out = append(out, fmt.Sprintf("<%s from %s>", or(name, "item"), label))
		}
	}
	return out
}

func synthMap(schema map[string]any, mode, label string, ro bool, depth, idx int, rec map[string]int) any {
	props, _ := schema["properties"].(map[string]any)
	out := map[string]any{}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		pv, _ := props[k].(map[string]any)
		out[k] = value(k, pv, mode, label, ro, depth, idx, rec)
	}
	return out
}

func value(name string, v map[string]any, mode, label string, ro bool, depth, idx int, rec map[string]int) any {
	t, _ := v["type"].(string)
	// A declared `enum` is the only place a script says which STRINGS its own
	// branches compare against, and ignoring it meant no `x === 'literal'`
	// branch of any orchestration script was reachable in a dry run. Measured
	// 2026-08-25 on the emitted skeleton: every string synthesised to
	// "<severity from review:…>", which equals no enum member, so
	// `f.severity === 'blocker'` was FALSE in all seven modes — the gate logged
	// "gate clean" and `openBlockers` was empty in every one of them, and the
	// entire fix-round and re-review path was executed by no mode at all. That
	// is P2e's finding 8 one layer down: the runner's fixture, not the script,
	// decided what was exercised.
	//
	// The member is chosen by what the WORD means, never by its position: this
	// corpus writes severity worst-first (`['blocker','major','minor','note']`)
	// and verdicts worst-last (`['pass','blockers']`), so a positional rule
	// walks one of them backwards — measured, picking the last member for
	// `clean` handed the clean mode `verdict: 'blockers'`.
	if es, ok := v["enum"].([]any); ok && len(es) > 0 && t == "string" {
		var members []string
		for _, e := range es {
			if str, ok := e.(string); ok {
				members = append(members, str)
			}
		}
		if len(members) > 0 {
			// The top-level ordinal reaches a SEVERITY only: alternating every
			// top-level enum would hand every second gate-style critic of a
			// review `verdict: 'blockers'` in the mode where nothing failed.
			if depth == 0 && !severityName.MatchString(name) {
				idx = 0
			}
			return pickEnum(name, members, mode, idx, rec)
		}
	}
	switch t {
	case "array":
		return arrayOf(name, v, mode, label, ro, depth, rec)
	case "object":
		return synthMap(v, mode, label, ro, depth+1, idx, rec)
	case "integer", "number":
		// Item 1 of an array differs from item 0 in every scalar, not only in
		// its enums: a review deduplicates findings by `file:line`, and two
		// items carrying the same placeholder collapsed into one — the mixed
		// pair the array was given two items FOR never reached the script.
		return 1 + idx
	case "boolean":
		// A pass-shaped boolean is TRUE everywhere but `acceptfail` in a build,
		// where the gates' blockers ride in arrays and the booleans are step
		// flags. A read-only run has no `acceptfail`, so that rule made its
		// verifier's `verified` true in every mode and the refute path ran in
		// none — measured on the emitted review skeleton, `blockers` confirmed
		// both findings. There the boolean follows the fixture: false wherever
		// the agents are reporting failure.
		passTrue := mode != "acceptfail"
		if ro {
			passTrue = passFixture(mode)
		}
		// An explicit polarity beats every matcher. A domain boolean cannot
		// always be named after the matcher without being renamed away from its
		// subject — `smokeAchieved` is refused and `criterionMet` accepted, so
		// the field ends up named for this file rather than for its reader, in
		// an artifact whose whole discipline is that names carry meaning. The
		// annotation is NOT free: it rides in the schema the launch's safety
		// classifier reads, and that width is the other failure this runner
		// measures, so the note offers renaming first.
		if xp, state := declaredPolarity(v); state == polarityOK {
			if xp {
				return passTrue
			}
			return !passFixture(mode)
		} else if state == polarityBad {
			// Present and unreadable. The author who reaches for the escape
			// hatch after reading the WARN would otherwise get the same WARN
			// again, with nothing saying their spelling was seen and rejected.
			if rec != nil {
				rec["x-pass on "+name+" is not readable as true or false"]++
			}
		}
		if passBools.MatchString(name) {
			return passTrue
		}
		if failBools.MatchString(name) {
			// The negator rule the ENUM vocabulary has had since it was written,
			// applied to the other vocabulary that reads a NAME. `noFailures` and
			// `errorFree` carry a failure word and are PASS flags — the comment on
			// failVerdicts names both, and that guard is anchored precisely so it
			// does not convict them — but the fixture synthesiser matched the
			// substring, returned FALSE in `clean`, and the script derived a
			// verdict the clean-mode guard then reported as "no fixture in this
			// mode can produce that verdict": a red run over a correct script,
			// with no note naming the field that produced it.
			if boolNegated(name) {
				if rec != nil {
					rec[boolPolarityKey+name+" (a NEGATED failure word — read as a PASS flag)"]++
				}
				return passTrue
			}
			if rec != nil && passFixture(mode) {
				// Recorded only where the value is the SURPRISING one. A failure
				// word is false in a passing mode by design, and that is exactly
				// the value a verdict derived from it inverts — so this is the
				// population the guard's message could not name, and reporting the
				// other modes too would paint every run with what they expect.
				rec[boolPolarityKey+name+" (a failure word — FALSE in this passing mode)"]++
			}
			return !passFixture(mode)
		}
		// An unrecognised boolean defaults to FALSE in every mode, not to "true
		// in clean": a script branching on `if (r.blocking)` was pushed onto its
		// failure path by the mode meant to walk the happy one. The name is
		// recorded and printed rather than guessed at.
		if rec != nil {
			rec[name]++
		}
		return false
	default:
		if idx > 0 {
			return fmt.Sprintf("<%s from %s #%d>", name, label, idx+1)
		}
		return fmt.Sprintf("<%s from %s>", name, label)
	}
}

// Composed returns the prompts of the mode that composed the most of them, and
// that mode's name.
//
// The prompt diff lives here and not in wfcheck because this is where the
// prompts EXIST: measured on the P3a script, 14 of 15 are composed at run time,
// so a static reader sees one, and a sweep built there would report nothing on
// the very corpus that produced the class. The runner has the composed string
// in hand at every spawn.
//
// The mode is named by every caller: a mode that died early composed fewer
// prompts, and a diff over a partial set is a partial diff.
func Composed(rs []Result) ([]check.NamedText, string) {
	var best Result
	for _, r := range rs {
		if len(r.Prompts) > len(best.Prompts) || (len(r.Prompts) == len(best.Prompts) && r.Mode == "clean") {
			best = r
		}
	}
	return best.Prompts, best.Mode
}

// FindPrompt resolves one label the operator typed against the composed
// prompts: an exact label first, then a unique substring. An ambiguous spelling
// is an error naming what it could have meant — resolving it to the first match
// is the class `wf check cite` reports on a citation.
func FindPrompt(ps []check.NamedText, want string) (check.NamedText, error) {
	if strings.TrimSpace(want) == "" {
		return check.NamedText{}, fmt.Errorf("an empty label matches nothing; this run composed %s", labelList(ps))
	}
	var exact []check.NamedText
	for _, p := range ps {
		if p.Name == want {
			exact = append(exact, p)
		}
	}
	if len(exact) > 0 {
		return oneOf(want, exact, "spawn")
	}
	var hits []check.NamedText
	for _, p := range ps {
		if strings.Contains(p.Name, want) {
			hits = append(hits, p)
		}
	}
	switch len(hits) {
	case 0:
		return check.NamedText{}, fmt.Errorf("no agent labelled %q; this run composed %s", want, labelList(ps))
	default:
		return oneOf(want, hits, "label")
	}
}

// oneOf resolves several matches to one prompt, and refuses when they are not
// the same TEXT. One label spawned in a loop is many prompts — a per-item
// reviewer gets a per-item briefing — and returning the first would diff a
// prompt the operator did not choose. Resolving an ambiguous spelling to the
// first match is the class `wf check cite` reports on a citation path.
func oneOf(want string, hits []check.NamedText, kind string) (check.NamedText, error) {
	distinct := map[string]bool{}
	names := map[string]bool{}
	for _, h := range hits {
		distinct[h.Text] = true
		names[h.Name] = true
	}
	// The LABEL question first. Two agents whose prompts happen to be identical
	// are still two agents, and picking one of them because the texts matched
	// would answer a question the operator did not ask — its own test caught
	// this ordering the first time it was written the other way round.
	if len(names) > 1 {
		return check.NamedText{}, fmt.Errorf("%q matches %d labels — say which: %s", want, len(names), labelList(hits))
	}
	if len(distinct) == 1 {
		return hits[0], nil
	}
	return check.NamedText{}, fmt.Errorf("%q was spawned %d times with %d DIFFERENT prompts (one %s per item), so there is no single prompt to diff; give the pair a label that spawns once",
		want, len(hits), len(distinct), kind)
}

// labelList names the labels a run composed, deduplicated and in spawn order.
func labelList(ps []check.NamedText) string {
	seen := map[string]bool{}
	var out []string
	for _, p := range ps {
		if !seen[p.Name] {
			seen[p.Name] = true
			out = append(out, p.Name)
		}
	}
	return strings.Join(out, ", ")
}

// fixtureAdvice is appended to the clean mode's verdict violation, and it is
// the half that was missing. The violation states a fact about the SCRIPT — it
// derives the verdict from something no fixture can satisfy — and on the P3e
// gate that fact was three times a fact about the RUNNER instead: a `$ref` it
// could not expand, booleans it defaulted false because their names carry no
// polarity, and an integer term (`finalCount === 22`) that no report-shaped
// fixture can make true. All three read identically from here, and the object
// the stub returned separates them in one look.
const fixtureAdvice = " The fixture this mode returned is what the term read: `--fixture <label>` prints it"

// Fixtures returns one mode's synthesised reports for the named labels, in
// spawn order, and only for the labels this mode actually composed. A label
// that spawned in one mode and not another is the normal case — `dead-reviews`
// hands a reviewer null before a fixture exists — so a missing one is silence
// here and the caller's job across the whole run: a label NO mode composed is
// the `--dead` typo class, one flag over.
func Fixtures(r Result, labels []string) []FixtureUse {
	var out []FixtureUse
	for _, want := range labels {
		want = strings.TrimSpace(want)
		// EVERY distinct shape this label composed, not the first: a per-item
		// briefing built in a loop has more than one, and the one that differs
		// is the one worth reading.
		for _, f := range r.Fixtures {
			if f.Label == want {
				out = append(out, f)
			}
		}
	}
	return out
}
