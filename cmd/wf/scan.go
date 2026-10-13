package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"git.sumka.site/yakov/wfgate/internal/ws"
)

// `wf scan` folds the SARIF logs a security run's scanners wrote into the JSON
// the run reads as `args.scan`. It is a command and not an agent because the
// one property the skills that drive scanners agree on is that the
// MEASUREMENT is done by a script and the model only judges it: an agent asked
// to transcribe two hundred hits transcribes the first few, and a scanner that
// failed reads, in a model's summary, like a scanner that found nothing.
//
// What it keeps of a result is its location, its rule and a one-line message —
// never `region.snippet`, which for a secrets scanner IS the secret, and the
// args file is handed to every finder of the run.

// scanTool is one scanner's line in args.scan.
type scanTool struct {
	Tool    string `json:"tool"`
	File    string `json:"file"`
	Driver  string `json:"driver,omitempty"`
	OK      bool   `json:"ok"`
	Results int    `json:"results"`
	Errors  int    `json:"errors"`
	Warns   int    `json:"warnings"`
	Note    string `json:"note,omitempty"`
}

// scanHit is one result, as a candidate a finder adjudicates.
type scanHit struct {
	ID      string `json:"id"`
	Tool    string `json:"tool"`
	Rule    string `json:"rule"`
	Level   string `json:"level,omitempty"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Message string `json:"message"`
}

type scanArgs struct {
	Tools      []scanTool `json:"tools"`
	Candidates []scanHit  `json:"candidates"`
}

// The SARIF 2.1.0 subset read here. Everything else in a log is ignored.
type sarifLog struct {
	Runs []struct {
		Tool struct {
			Driver struct {
				Name string `json:"name"`
			} `json:"driver"`
		} `json:"tool"`
		Invocations []struct {
			ExecutionSuccessful *bool `json:"executionSuccessful"`
			Notifications       []struct {
				Level   string `json:"level"`
				Message struct {
					Text string `json:"text"`
				} `json:"message"`
			} `json:"toolExecutionNotifications"`
		} `json:"invocations"`
		Results []struct {
			RuleID  string `json:"ruleId"`
			Level   string `json:"level"`
			Message struct {
				Text string `json:"text"`
			} `json:"message"`
			Locations []struct {
				Physical struct {
					Artifact struct {
						URI string `json:"uri"`
					} `json:"artifactLocation"`
					Region struct {
						StartLine int `json:"startLine"`
					} `json:"region"`
				} `json:"physicalLocation"`
			} `json:"locations"`
		} `json:"results"`
	} `json:"runs"`
}

// scanMessageMax bounds one hit's message: it rides in a finder's prompt.
const scanMessageMax = 200

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

// repoRel turns a SARIF uri into the repo-relative path a unit's `paths`
// prefix is matched against: scanners write `file:///abs/…`, absolute paths,
// or paths relative to where they ran, which is the target's root.
func repoRel(uri, target string) string {
	p := strings.TrimPrefix(uri, "file://")
	if filepath.IsAbs(p) && target != "" {
		if rel, err := filepath.Rel(target, p); err == nil && !strings.HasPrefix(rel, "..") {
			p = rel
		}
	}
	return strings.TrimPrefix(filepath.ToSlash(p), "./")
}

func cmdScan(args []string) error {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	out := fs.String("o", "", "write here (default: artifacts/scan/args.json in the workspace; - for stdout)")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	w, err := open()
	if err != nil {
		return err
	}
	dir := filepath.Join(w.Artifacts(), "scan")
	files := pos
	if len(files) == 0 {
		files, _ = filepath.Glob(filepath.Join(dir, "*.sarif"))
	}
	if len(files) == 0 {
		return fmt.Errorf("no SARIF log under %s — run the scanners the decision document names into it first, one file per scanner named after it:\n"+
			"  semgrep scan --metrics=off --config auto --sarif -o %s %s\n"+
			"  betterleaks dir %s --redact --report-format sarif --report-path %s\n"+
			"  trivy fs --format sarif -o %s %s",
			dir, filepath.Join(dir, "semgrep.sarif"), w.M.Target, w.M.Target, filepath.Join(dir, "betterleaks.sarif"), filepath.Join(dir, "trivy.sarif"), w.M.Target)
	}
	sort.Strings(files)

	var res scanArgs
	seen := map[string]string{}
	for _, f := range files {
		tool := strings.SplitN(filepath.Base(f), ".", 2)[0]
		if prev, ok := seen[tool]; ok {
			return fmt.Errorf("%s and %s both name the scanner %q — a scanner's id is its file name up to the first dot, and two logs under one id would be read as one run", prev, f, tool)
		}
		seen[tool] = f
		b, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		var log sarifLog
		if err := json.Unmarshal(b, &log); err != nil {
			return fmt.Errorf("%s: not a SARIF log: %w", f, err)
		}
		if len(log.Runs) == 0 {
			return fmt.Errorf("%s: a SARIF log with no run — the scanner wrote no result set at all, which is not a scan that found nothing", f)
		}
		t := scanTool{Tool: tool, File: filepath.Base(f), OK: true}
		invoked := false
		var notes []string
		dropped := 0
		for _, run := range log.Runs {
			if t.Driver == "" {
				t.Driver = run.Tool.Driver.Name
			}
			for _, inv := range run.Invocations {
				invoked = true
				if inv.ExecutionSuccessful != nil && !*inv.ExecutionSuccessful {
					t.OK = false
				}
				// An error-level notification FAILS the scanner, whatever
				// executionSuccessful says. Measured 2026-10-13 on semgrep
				// 1.180.0 pointed at a config that does not exist: exit 7, a
				// SARIF log with executionSuccessful TRUE, two error
				// notifications and no result — read by that flag alone it is
				// "semgrep, 0 hits", a failed scan printed as a clean one.
				// Warnings (a file the parser could not read) are counted and
				// said, not failed.
				for _, n := range inv.Notifications {
					switch n.Level {
					case "error":
						t.Errors++
						t.OK = false
						if len(notes) < 3 {
							notes = append(notes, oneLine(n.Message.Text, scanMessageMax))
						}
					case "warning":
						t.Warns++
					}
				}
			}
			for _, r := range run.Results {
				if len(r.Locations) == 0 || r.Locations[0].Physical.Artifact.URI == "" {
					dropped++
					continue
				}
				loc := r.Locations[0].Physical
				t.Results++
				res.Candidates = append(res.Candidates, scanHit{
					ID:      fmt.Sprintf("%s-%d", tool, t.Results),
					Tool:    tool,
					Rule:    r.RuleID,
					Level:   r.Level,
					File:    repoRel(loc.Artifact.URI, w.M.Target),
					Line:    max(loc.Region.StartLine, 1),
					Message: oneLine(r.Message.Text, scanMessageMax),
				})
			}
		}
		if dropped > 0 {
			notes = append(notes, fmt.Sprintf("%d result(s) with no file location, not candidates", dropped))
		}
		if !invoked {
			notes = append(notes, "the log records no invocation, so success is read from the log parsing at all")
		}
		t.Note = strings.Join(notes, "; ")
		res.Tools = append(res.Tools, t)
		state := "ok"
		if !t.OK {
			state = "FAILED — the run names it and is incomplete until it is re-run or dropped from SCANNERS"
		}
		fmt.Printf("%-12s %s, %d candidate(s)", tool, state, t.Results)
		if t.Errors > 0 {
			fmt.Printf(", %d execution error(s)", t.Errors)
		}
		if t.Warns > 0 {
			fmt.Printf(", %d warning(s) — files or rules it skipped; read them in the log", t.Warns)
		}
		if t.Note != "" {
			fmt.Printf(" — %s", t.Note)
		}
		fmt.Println()
	}
	if res.Candidates == nil {
		res.Candidates = []scanHit{}
	}
	b, err := json.MarshalIndent(map[string]any{"scan": res}, "", " ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if *out == "-" {
		_, err := os.Stdout.Write(b)
		return err
	}
	dst := *out
	if dst == "" {
		dst = filepath.Join(dir, "args.json")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s — %d scanner(s), %d candidate(s); hand it to the run as `args` (merge any other args into it), and to the dry run: wfdry --args \"$(cat %s)\" <script>\n", dst, len(res.Tools), len(res.Candidates), dst)
	if len(res.Candidates) > 0 {
		fmt.Printf("  every candidate is adjudicated by a finder of its unit, a chunk of them per finder (SCAN_CHUNK in the skeleton) — a scanner over-reporting by the hundred is a ruleset to narrow before the launch, not after\n")
	}
	if strings.HasPrefix(dst, w.Artifacts()+string(filepath.Separator)) {
		fmt.Printf("  `wf add` the logs and this file, so the freeze pins the scan the run was given\n")
	}
	if w.M.RunType() != ws.TypeSecurity {
		fmt.Printf("  note: this workspace is a %s gate; only a security run's skeleton reads args.scan\n", w.M.RunType())
	}
	return nil
}
