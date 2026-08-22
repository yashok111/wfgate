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
//
// Exit 1 if any mode fails.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"git.sumka.site/yakov/wfgate/internal/cli"
	"git.sumka.site/yakov/wfgate/internal/wfdry"
)

var version = "devel"

func main() {
	mode := flag.String("mode", "", "run one mode instead of all of them")
	deadCSV := flag.String("dead", "", "comma-separated labels whose spawn returns null")
	budget := flag.Int("budget", -1, "budget.total the script sees (default: none)")
	args := flag.String("args", "", "JSON handed to the script as `args`")
	quiet := flag.Bool("quiet", false, "verdicts only")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `wfdry — dynamic dry run over a Workflow orchestration script

usage: wfdry <script.mjs> [--mode M] [--dead a,b] [--budget N] [--args JSON] [--quiet]
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
	for _, r := range results {
		switch {
		case r.Err != nil:
			failed++
			fmt.Printf("\n=== %s: THREW ===\n%s\n", r.Mode, trunc(r.Err.Error()))
			// A recorded violation is why the mode threw as often as not, and
			// dropping it here loses the actual defect behind whatever error
			// surfaced afterwards.
			for _, v := range r.Violations {
				fmt.Println("  " + v)
			}
		case len(r.Violations) > 0:
			failed++
			fmt.Printf("\n=== %s: %d VIOLATION(S) — the script's spawn wrapper swallowed these ===\n", r.Mode, len(r.Violations))
			for _, v := range r.Violations {
				fmt.Println("  " + v)
			}
		default:
			fmt.Printf("\n=== %s: OK — %d agents, %d phases ===\n", r.Mode, r.Spawned, len(r.Phases))
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
	}
	fmt.Printf("\n%s: %d/%d modes completed\n", path, len(results)-failed, len(results))
	if failed > 0 {
		os.Exit(1)
	}
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
