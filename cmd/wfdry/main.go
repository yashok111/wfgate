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
//
// Exit 1 if any mode fails.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"git.sumka.site/yakov/wfgate/internal/check"
	"git.sumka.site/yakov/wfgate/internal/cli"
	"git.sumka.site/yakov/wfgate/internal/wfdry"
)

var version = "devel"

// distinctLabels counts the labels behind `r.Calls`, whose entries are
// "label [model/effort]" — the same label spawned twice is one label, because
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
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `wfdry — dynamic dry run over a Workflow orchestration script

usage: wfdry <script.mjs> [--mode M] [--dead a,b] [--budget N] [--args JSON] [--quiet] [--prompt-diff a,b]
modes: %s
`, strings.Join(wfdry.Modes, ", "))
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

	modes := wfdry.Modes
	if *mode != "" {
		modes = []string{*mode}
	}
	o := wfdry.Options{Args: *args}
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
			fmt.Printf("\n=== %s: %d VIOLATION(S) — the script's spawn wrapper swallowed these ===\n", r.Mode, len(r.Violations))
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
	if *promptDiff != "" {
		if code := printPromptDiff(results, *promptDiff); code != 0 {
			os.Exit(code)
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
