// wfdry — the dynamic dry run over a Claude Code Workflow orchestration script.
//
// Usage:
//
//	wfdry <script.mjs>                  every mode
//	wfdry <script.mjs> --mode clean     one mode
//	wfdry <script.mjs> --dead a,b       kill these labels in every mode
//	wfdry <script.mjs> --budget 500000  run with a token target set
//	wfdry <script.mjs> --args '{"x":1}' hand this to the script as `args`
//	wfdry <script.mjs> --quiet          verdicts only, no per-mode detail
//	wfdry <script.mjs> --prompt-diff a,b  what one prompt says and the other does not
//	wfdry <script.mjs> --fixture accept   print the report the stub handed that agent
//
// Exit 1 if any mode fails.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"git.sumka.site/yakov/wfgate/internal/check"
	"git.sumka.site/yakov/wfgate/internal/cli"
	"git.sumka.site/yakov/wfgate/internal/wfdry"
	"git.sumka.site/yakov/wfgate/internal/ws"
)

var version = "devel"

// distinctLabels counts the labels behind `r.Calls`, whose entries are
// "label [model/effort]" or "label [model/effort/isolation]" — the same label spawned twice is one label, because
// the question is how much of the script's briefing the mode reached.
func distinctLabels(calls []string) int {
	seen := map[string]bool{}
	for _, c := range calls {
		l, _, _ := strings.Cut(c, " [")
		seen[l] = true
	}
	return len(seen)
}

func main() {
	mode := flag.String("mode", "", "run one mode instead of all of them")
	deadCSV := flag.String("dead", "", "comma-separated labels whose spawn returns null")
	budget := flag.Int("budget", -1, "budget.total the script sees (default: none)")
	args := flag.String("args", "", "JSON handed to the script as `args`")
	quiet := flag.Bool("quiet", false, "verdicts only")
	promptDiff := flag.String("prompt-diff", "", "two agent labels, `a,b`: print the sentences one prompt carries and the other does not")
	fixture := flag.String("fixture", "", "comma-separated agent labels: print the report the stub returned to each, per mode")
	typ := flag.String("type", "", "the gate's run type ("+strings.Join(ws.Types, "|")+"); default: the type of the workspace holding the script, else build")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `wfdry — dynamic dry run over a Workflow orchestration script

usage: wfdry <script.mjs> [--type T] [--mode M] [--dead a,b] [--budget N] [--args JSON] [--quiet] [--prompt-diff a,b] [--fixture a,b]
modes (build): %s
modes (read-only types): %s
`, strings.Join(wfdry.Modes, ", "), strings.Join(wfdry.ModesFor(ws.TypeReview), ", "))
	}
	pos, perr := cli.ParseMixed(flag.CommandLine, os.Args[1:])
	if perr != nil {
		os.Exit(2)
	}
	if *showVersion {
		fmt.Printf("wfdry %s\n", version)
		return
	}
	if len(pos) != 1 {
		flag.Usage()
		os.Exit(2)
	}
	path := pos[0]

	// The type decides the mode list and which way a findings array points, so
	// a type this run INFERRED is announced with the flag that overrides it —
	// a review script dry-run under the build rules walks its happy path in no
	// passing mode, and nothing else in the output would say why. Silent only
	// when there was nothing to infer from: no workspace, the historical
	// default.
	runType := *typ
	if runType == "" {
		t, src := ws.TypeAt(path)
		runType = t
		if strings.HasPrefix(src, "gate.json") {
			fmt.Printf("type %s — from %s; --type overrides\n", t, src)
		}
	}
	if err := ws.ValidType(runType); err != nil {
		fmt.Fprintln(os.Stderr, "wfdry:", err)
		os.Exit(2)
	}

	modes := wfdry.ModesFor(runType)
	if *mode != "" {
		modes = []string{*mode}
	}
	o := wfdry.Options{Args: *args, Type: runType}
	if *deadCSV != "" {
		o.Dead = strings.Split(*deadCSV, ",")
	}
	if *budget >= 0 {
		o.Budget = budget
	}

	results, err := wfdry.Run(path, modes, o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wfdry:", err)
		os.Exit(2)
	}
	failed := 0
	// The happy path's agent count, or -1 when this run did not include it.
	clean := -1
	for _, r := range results {
		if r.Mode == "clean" {
			clean = r.Spawned
		}
	}
	for _, r := range results {
		// WHICH labels the mode stubbed dead, always and next to the header —
		// this is the mode's own claim about what it exercised, not detail.
		// `dead-reviews` killed a producer labelled `preview` for a whole gate
		// and reported OK; nothing in the output named it, so the claim could
		// not be read even by an operator who suspected it. It prints on a
		// THREW too: killing the wrong agent is how a run ends early.
		killed := func() {
			if len(r.Killed) == 0 {
				return
			}
			// The REACH beside the list, as a population and never a verdict.
			// `dead-reviews` reports OK over a script whose gate reviewers it
			// never matched, and one unrelated kill silences the zero-kill note:
			// measured, p2f kills 1 of 18 spawned labels while p3b kills 13 of
			// 18 and both print the same word. Naming the survivors on every
			// run would paint every correct script; the ratio does not.
			reach := ""
			if n := distinctLabels(r.Calls); n > 0 {
				reach = fmt.Sprintf(" (%d of %d spawned label(s))", len(r.Killed), n)
			}
			fmt.Println("  killed:", strings.Join(r.Killed, ", ")+reach)
		}
		switch {
		case r.Err != nil:
			failed++
			fmt.Printf("\n=== %s: THREW ===\n%s\n", r.Mode, trunc(r.Err.Error()))
			killed()
			// A recorded violation is why the mode threw as often as not, and
			// dropping it here loses the actual defect behind whatever error
			// surfaced afterwards.
			for _, v := range r.Violations {
				fmt.Println("  " + v)
			}
		case len(r.Violations) > 0:
			failed++
			fmt.Printf("\n=== %s: %d VIOLATION(S) ===\n", r.Mode, len(r.Violations))
			killed()
			for _, v := range r.Violations {
				fmt.Println("  " + v)
			}
		default:
			// The happy path's count beside every other mode's, in the line the
			// operator reads. A mode that reached one agent of twenty-five
			// printed `OK — 1 agents, 1 phases` and nothing to compare it
			// with, seven times over, for a whole gate round.
			against := ""
			if clean >= 0 && r.Mode != "clean" {
				against = fmt.Sprintf(" (clean: %d agents)", clean)
			}
			fmt.Printf("\n=== %s: OK — %d agents, %d phases%s ===\n", r.Mode, r.Spawned, len(r.Phases), against)
			killed()
			if !*quiet {
				fmt.Println("  phases:", orNone(strings.Join(r.Phases, " | ")))
				fmt.Println("  agents:", orNone(strings.Join(r.Calls, " ")))
				for _, l := range r.Logs {
					fmt.Println("  log:", l)
				}
				// The whole return value: the verdict fields are what a reader is
				// checking, and summarising them would hide the case this tool
				// exists for.
				b, _ := json.MarshalIndent(r.Return, "  ", " ")
				fmt.Println("  return:", string(b))
			}
		}
		// The runner's own notes are not the script's log output and are not
		// printed beside it: a note saying "this mode proved less than it looks
		// like it did" that arrives inside a 1500-line dump, under the same
		// `log:` prefix as the script's own chatter, is a note nobody reads.
		// P2f read past one three times. They print after the mode's detail and
		// they print under --quiet, because --quiet asks for the verdicts and
		// this IS one.
		for _, n := range r.Notes {
			fmt.Println("  WARN:", n)
		}
	}
	deadUnused := false
	// A label the operator NAMED that no mode ever stubbed dead. Per mode it is
	// only a note — a run that died early legitimately never reached the spawn —
	// but across EVERY mode it means `--dead` did nothing at all, and every
	// guard behind it ran over the empty set while the tool exited 0. That is
	// the vacuous guard this runner exists against, with the typo one layer up.
	//
	// The `dead-reviews` mode's own zero-kill stays a NOTE, deliberately: a
	// producer-only script HAS no reviewer label and is correct — testdata/
	// clean.mjs is one — so failing there would fail correct scripts. The
	// difference is who made the claim: the operator typed `--dead`, nobody
	// typed the mode's matcher.
	if len(o.Dead) > 0 {
		killedAnywhere := map[string]bool{}
		for _, r := range results {
			for _, k := range r.Killed {
				killedAnywhere[k] = true
			}
		}
		var never []string
		for _, d := range o.Dead {
			if !killedAnywhere[d] {
				never = append(never, d)
			}
		}
		if len(never) > 0 {
			// NOT `failed++`: that counter is the modes tally, and a `--dead`
			// typo is not a mode that failed. Reporting it there made the run
			// print "6/7 modes completed" over seven modes that all completed.
			deadUnused = true
			fmt.Printf("\n=== --dead: %s matched NO spawn in any mode ===\n  every guard behind them ran over the empty set. Labels this run composed: %s\n",
				strings.Join(never, ", "), orNone(strings.Join(labelsOf(results), ", ")))
		}
	}
	// What each agent would put ON THE WIRE, printed ALWAYS and not only when
	// it is wide. A green run over an unmeasured width proves nothing about the
	// launch, and this corpus has lost agents to that number twice — once on a
	// run's LAST agent, six hours in, with nothing downstream to catch it. The
	// section is small: it groups by width, not by call site.
	widths := wfdry.SchemaWidths(results)
	if len(widths) > 0 {
		// The BAND in the header, on every run and not only when a width is in
		// trouble. The two constants live in this binary and were printed only
		// inside the WARN, so an operator under the ceiling read silence and
		// could not tell "measured safe" from "not checked" — measured on P3g,
		// which probed a 3,705-byte schema against a remembered ceiling of
		// 3,378 and spent 24 seconds and 64k tokens re-measuring a width this
		// binary already knew was below the widest acceptance on record. A
		// shipped capability the report never mentions is a capability nobody
		// has.
		// The band prints only where a width was actually measured. A run whose
		// every schema is unserialisable renders each as `does not serialize`,
		// and a band printed over that population is a measured-looking number
		// beside nothing measured — this project's single most repeated defect.
		priced := 0
		for _, w := range widths {
			if w.Clean >= 0 && w.Widest >= 0 {
				priced++
			}
		}
		if priced > 0 {
			fmt.Printf("\n=== schemas — %d distinct width(s), widest first; corpus band: <=%d ACCEPTED, >=%d REFUSED, the gap UNMEASURED ===\n",
				len(widths), wfdry.SchemaAccepted, wfdry.SchemaRefused)
		} else {
			fmt.Printf("\n=== schemas — %d distinct width(s), NONE of which serialize, so no width was measured and the corpus band says nothing about this run ===\n", len(widths))
		}
		for _, w := range widths {
			if w.Clean < 0 || w.Widest < 0 {
				fmt.Printf("  does not serialize: %s\n", strings.Join(w.Labels, ", "))
				continue
			}
			// The happy path's width is the quantity the corpus is measured in;
			// a wider FAILURE branch is a different statement and prints as one,
			// because a schema assembled on a branch is only sent where that
			// branch runs.
			branch := ""
			if w.Widest > w.Clean {
				branch = fmt.Sprintf(" (%d on a failure branch)", w.Widest)
			}
			// The `required` census beside the width, because the two move
			// TOGETHER and only one of them was ever printed. P3e dropped a
			// leaf's `required` inside an edit whose stated purpose was width;
			// nothing static could see it — the leaf is built in a `.map()`
			// callback — and for two rounds a property could come back with no
			// evidence at all. A drop is a changed number here.
			fmt.Printf("  %d bytes%s: %s%s\n", w.Clean, branch, strings.Join(w.Labels, ", "), requiredNote(w))
		}
	}
	// A refusal from either flag is collected and acted on AFTER the tally and
	// the tail WARNs, never instead of them. Exiting here suppressed the run's
	// own verdict — `6/7 modes completed` and every deduplicated WARN — over an
	// operator's typo in a flag, which is the one line an operator reads.
	flagCode := 0
	if *fixture != "" {
		flagCode = printFixtures(results, *fixture)
	}
	if *promptDiff != "" {
		if code := printPromptDiff(results, *promptDiff); code != 0 {
			flagCode = code
		}
	}
	fmt.Printf("\n%s: %d/%d modes completed\n", path, len(results)-failed, len(results))
	// And once more at the very end, deduplicated: the last line of the output
	// is the one an operator acts on, and a green tally standing alone over
	// modes that admitted they exercised nothing is the false green this whole
	// tool was written against.
	seen := map[string]bool{}
	for _, r := range results {
		for _, n := range r.Notes {
			if !seen[n] {
				seen[n] = true
				fmt.Println("WARN:", n)
			}
		}
	}
	// LAST, because it is the one line that is about the launch rather than
	// about the script: a mode tally cannot fail on a schema width, and an
	// operator who reads only the final lines must still see it.
	for _, n := range wfdry.SchemaNotes(widths) {
		fmt.Println("WARN:", n)
	}
	if flagCode != 0 {
		os.Exit(flagCode)
	}
	if failed > 0 || deadUnused {
		os.Exit(1)
	}
}

// printPromptDiff prints the sentences one prompt carries and the other does
// not, for two agents the OPERATOR says were given the same question.
//
// It is never a failure and never changes the exit code: which of the two
// should carry a sentence is judgement. Measured on P3a, the last round of a
// gate was spent entirely on this class — a sentence in the acceptance prompt,
// absent from the verification prompt, although the same batch had given the
// two agents the same job — and the sweep that closed it, which found a sixth
// copy of one rule no reviewer lens had filed, was a hand diff.
//
// No SENTENCE is dropped — the operator named these two, and a diff that hides
// part of itself sends them back to the hand diff. A single sentence's display
// is bounded and says so, because a composed prompt splices data into itself.
func printPromptDiff(results []wfdry.Result, spec string) int {
	ps, mode := wfdry.Composed(results)
	names := strings.SplitN(spec, ",", 3)
	if len(names) != 2 {
		fmt.Fprintln(os.Stderr, "wfdry: --prompt-diff takes exactly two labels, comma-separated")
		return 2
	}
	a, err := wfdry.FindPrompt(ps, strings.TrimSpace(names[0]))
	if err != nil {
		fmt.Fprintln(os.Stderr, "wfdry:", err)
		return 2
	}
	b, err := wfdry.FindPrompt(ps, strings.TrimSpace(names[1]))
	if err != nil {
		fmt.Fprintln(os.Stderr, "wfdry:", err)
		return 2
	}
	// Two spellings of ONE prompt. `--prompt-diff accept,accept` — or two
	// abbreviations that resolve to the same spawn — diffed a text against
	// itself and printed "nothing is stated to one and not the other", which is
	// a false green with the operator's own typo behind it.
	if a.Name == b.Name && a.Text == b.Text {
		fmt.Fprintf(os.Stderr, "wfdry: both labels resolve to the same prompt (`%s`) — a text does not differ from itself\n", a.Name)
		return 2
	}
	onlyA, onlyB, shared := check.PromptDiff(a, b)
	nA, nB := check.PromptSentences(a), check.PromptSentences(b)
	fmt.Printf("\n=== prompt diff (%s) — `%s` and `%s` share %d sentence(s) of %d and %d ===\n",
		mode, a.Name, b.Name, shared, nA, nB)
	printOnly(a.Name, onlyA)
	printOnly(b.Name, onlyB)
	switch {
	case nA == 0 || nB == 0:
		// NOT agreement. A prompt whose every line is shorter than the sentence
		// floor contributes no population at all, and "they carry the same
		// sentences" over an empty one is this project's own worst defect
		// class: an absent measurement in the register of a measured pass.
		fmt.Printf("  NOT A PASS — %s of %d words or more, so nothing was compared\n",
			emptySide(a.Name, nA, b.Name, nB), check.PromptMinWords)
	case len(onlyA)+len(onlyB) == 0:
		fmt.Println("  the two prompts carry the same sentences — nothing is stated to one and not the other")
	}
	return 0
}

// emptySide names whichever prompt contributed nothing, or both.
func emptySide(a string, nA int, b string, nB int) string {
	switch {
	case nA == 0 && nB == 0:
		return "neither prompt carried a sentence"
	case nA == 0:
		return "`" + a + "` carried no sentence"
	default:
		return "`" + b + "` carried no sentence"
	}
}

// promptSentenceWidth bounds ONE sentence's display, never the list. A composed
// prompt splices data into itself — a `JSON.stringify` of fifty clause answers
// is one "sentence" of several kilobytes — and printing it whole buries the
// sentences beside it. No sentence is dropped; a cut one says so.
const promptSentenceWidth = 200

func printOnly(label string, only []check.Statement) {
	for _, s := range only {
		txt, cut := check.Cut(s.Text, promptSentenceWidth)
		if cut {
			txt += fmt.Sprintf(" [%d chars]", len(s.Text))
		}
		fmt.Printf("  only `%s`: %s\n", label, txt)
	}
}

// labelsOf names every label any mode spawned, deduplicated and in order.
func labelsOf(results []wfdry.Result) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range results {
		for _, p := range r.Prompts {
			if !seen[p.Name] {
				seen[p.Name] = true
				out = append(out, p.Name)
			}
		}
	}
	return out
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func trunc(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) > 8 {
		lines = lines[:8]
	}
	return strings.Join(lines, "\n")
}

// requiredNote renders the `required` census for one width group, or nothing
// when the group's labels do not agree on it — two labels share a width when
// their schemas serialize to the same number of bytes, which does not make them
// the same schema.
func requiredNote(w wfdry.SchemaWidth) string {
	if w.Required < 0 {
		// NOT silence. A schema that serialises but carries a `$ref` this
		// runner could not expand has no census, and printing nothing there is
		// indistinguishable from the tool not bothering — the banked
		// `nested-defs.mjs` is exactly that shape.
		return " — census NOT MEASURED (see the $ref note below)"
	}
	s := fmt.Sprintf(" — %s required", plural(w.Required, "key"))
	if w.CleanMissing {
		// The numbers are a FAILURE branch's: no clean mode composed this
		// label. The corpus band is measured on the happy path, and so is this
		// census.
		s += " on a FAILURE branch (no clean mode composed this label)"
	}
	if w.NoRequired > 0 {
		// Not a verdict. An object that requires none of its properties is
		// legal and common, and every agent may omit every field of it; the
		// count is here so that losing a `required` shows up as a number.
		s += fmt.Sprintf(", %s declaring properties and requiring none", plural(w.NoRequired, "object"))
	}
	return s
}

func plural(n int, what string) string {
	return fmt.Sprintf("%d %s(s)", n, what)
}

// printFixtures prints what the stub handed each named agent, per mode.
//
// It is never a failure and never changes the exit code unless NO mode composed
// a label the operator named — that is the typo, and it is the same claim
// `--dead` makes one flag over: every reading behind the name went over the
// empty set.
//
// It exists because three of one gate's dry-run reds were fixture-synthesis
// limits reported as artifact defects — a `$ref` the runner lost, twenty-two
// booleans defaulted false by NAME, and an integer term no report-shaped
// fixture can satisfy — and each cost a reading of twenty-two "unmet" rows to
// diagnose as one gap. The object separates them in one look.
func printFixtures(results []wfdry.Result, spec string) int {
	// Two spellings of ONE label print the same fixture twice, which is the
	// class `--prompt-diff` refuses one flag over. Here the honest answer is to
	// collapse them: the operator asked for a label, not for a count.
	var want []string
	asked := map[string]bool{}
	for _, w := range strings.Split(spec, ",") {
		if w = strings.TrimSpace(w); w != "" && !asked[w] {
			asked[w] = true
			want = append(want, w)
		}
	}
	seen := map[string]bool{}
	for _, r := range results {
		fs := wfdry.Fixtures(r, want)
		if len(fs) == 0 {
			continue
		}
		fmt.Printf("\n=== fixtures (%s) ===\n", r.Mode)
		for _, f := range fs {
			seen[f.Label] = true
			// HTML escaping OFF: every placeholder this runner synthesises is
			// `<something from label>`, and `\u003c` on both ends of each of
			// twenty-two of them is the reading this flag exists to make cheap.
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)
			enc.SetIndent("  ", " ")
			if err := enc.Encode(f.Value); err != nil {
				fmt.Printf("  %s: does not render (%v)\n", f.Label, err)
				continue
			}
			fmt.Printf("  %s: %s", f.Label, buf.String())
		}
	}
	var never []string
	for _, w := range want {
		if w = strings.TrimSpace(w); w != "" && !seen[w] {
			never = append(never, w)
		}
	}
	if len(never) > 0 {
		// A label that DID spawn and was then refused by the runner — no
		// `effort`, an unserialisable schema — records a prompt and no
		// fixture, and calling that a typo names a label the run did compose.
		spawned := map[string]bool{}
		for _, l := range labelsOf(results) {
			spawned[l] = true
		}
		var typo, refused []string
		for _, n := range never {
			if spawned[n] {
				refused = append(refused, n)
			} else {
				typo = append(typo, n)
			}
		}
		if len(refused) > 0 {
			fmt.Fprintf(os.Stderr, "wfdry: --fixture named %s, which every mode spawned and this runner then REFUSED, so no fixture was ever synthesised for it — read the violation above rather than the label\n",
				strings.Join(refused, ", "))
		}
		if len(typo) > 0 {
			fmt.Fprintf(os.Stderr, "wfdry: --fixture named %s, which no mode composed as a label. Labels this run composed: %s\n",
				strings.Join(typo, ", "), orNone(strings.Join(labelsOf(results), ", ")))
		}
		return 2
	}
	return 0
}
