// wfcheck — the static check over a Claude Code Workflow orchestration script.
//
// Usage:
//
//	wfcheck <script.mjs>            check
//	wfcheck --freeze <script.mjs>   copy to .wf/gate/<sha8>-<name> and print the
//	                                path (hand THAT to the gate reviewers so their
//	                                line numbers cannot drift while fixes land)
//
// Exit 1 on any ERROR, 0 otherwise.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"git.sumka.site/yakov/wfgate/internal/cli"
	"git.sumka.site/yakov/wfgate/internal/wfcheck"
)

// version is stamped by the build. A run against a stale installed binary looks
// exactly like a run against the new one — the same reason `wf` carries it.
var version = "devel"

func main() {
	freeze := flag.Bool("freeze", false, "copy the script under .wf/gate/ and print the frozen path")
	root := flag.String("root", ".", "repository the script will run in (for the file-existence checks)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, `wfcheck — static check over a Workflow orchestration script

usage:
  wfcheck [-root <dir>] <script.mjs>
  wfcheck --freeze <script.mjs>
`)
	}
	pos, perr := cli.ParseMixed(flag.CommandLine, os.Args[1:])
	if perr != nil {
		os.Exit(2)
	}
	if *showVersion {
		fmt.Printf("wfcheck %s\n", version)
		return
	}
	if len(pos) != 1 {
		flag.Usage()
		os.Exit(2)
	}
	path := pos[0]

	if *freeze {
		out, err := freezeScript(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "wfcheck:", err)
			os.Exit(1)
		}
		fmt.Println(out)
		return
	}

	rep, err := wfcheck.Run(path, *root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wfcheck:", err)
		os.Exit(1)
	}
	for _, f := range rep.Findings {
		fmt.Printf("%-9s %3d  %s\n", f.Level, f.Line, f.Msg)
	}
	errs, warns, notes := rep.Counts()
	fmt.Printf("\n%s: %d error(s), %d warning(s), %d note(s)\n", filepath.Base(path), errs, warns, notes)
	if errs > 0 {
		os.Exit(1)
	}
}

// freezeScript copies the script under .wf/gate/ named by its own hash, so the
// reviewers read one immutable revision while fixes land on the working copy.
func freezeScript(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	dir := filepath.Join(".wf", "gate")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	out := filepath.Join(dir, hex.EncodeToString(sum[:])[:8]+"-"+filepath.Base(path))
	if err := os.WriteFile(out, b, 0o644); err != nil {
		return "", err
	}
	return out, nil
}
