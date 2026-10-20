package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yashok111/wfgate/internal/gitx"
	"github.com/yashok111/wfgate/internal/ws"
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
	// Invoked is false for a log that records no invocation — betterleaks'
	// and trivy's never do — so the report can say that the scanner running to
	// the end is not recorded, rather than print its count as a measured one.
	Invoked bool `json:"invoked"`
	// Dropped counts results with no file location: no unit owns them and no
	// finder adjudicates them, so the run names them as a coverage hole.
	Dropped int `json:"dropped"`
	// Unresolved counts hits whose path is no file under the target.
	Unresolved int    `json:"unresolved"`
	Note       string `json:"note,omitempty"`
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

type sarifNote struct {
	Level   string `json:"level"`
	Message struct {
		Text string `json:"text"`
	} `json:"message"`
}

// The SARIF 2.1.0 subset read here. Everything else in a log is ignored.
type sarifLog struct {
	Runs []struct {
		Tool struct {
			Driver struct {
				Name string `json:"name"`
			} `json:"driver"`
		} `json:"tool"`
		// A uri may be relative to a named base: trivy writes `go.mod` with
		// uriBaseId ROOTPATH = the directory it scanned, so a scan of `svc/`
		// named `svc/go.mod` as `go.mod`.
		OriginalURIBaseIDs map[string]struct {
			URI string `json:"uri"`
		} `json:"originalUriBaseIds"`
		Invocations []struct {
			ExecutionSuccessful *bool       `json:"executionSuccessful"`
			Notifications       []sarifNote `json:"toolExecutionNotifications"`
			// Rule and configuration errors live in the OTHER array: a log
			// with an error there and executionSuccessful true read as a
			// clean scan (the wave-37 review round, a codeql-shaped log).
			ConfigNotifications []sarifNote `json:"toolConfigurationNotifications"`
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
						URI       string `json:"uri"`
						URIBaseID string `json:"uriBaseId"`
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

// matchLine cuts a scanner message at a `Match:` marker. trivy's secret
// scanner writes one into its message, masked but for a tail: measured,
// `STRIPE = "****...6hG5fD"` carried the last six characters of a planted key
// into args.json, and from there into every finder's prompt.
func dropMatch(s string) string {
	if i := strings.Index(s, "Match:"); i >= 0 {
		return strings.TrimSpace(s[:i]) + " [match text dropped]"
	}
	return s
}

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
	p := uri
	// A uri is a URI: `file:///a%20b/c.go` is `/a b/c.go`, and `file://host/x`
	// names host's /x — read as a path, both missed their unit.
	if u, err := url.Parse(uri); err == nil && (u.Scheme == "file" || u.Scheme == "") {
		p = u.Path
		if u.Scheme == "" && u.Opaque != "" {
			p = u.Opaque
		}
	}
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
			"  cd %[2]s\n"+
			"  rm -f %[1]s/semgrep.sarif && semgrep scan --metrics=off --config p/default --sarif -o %[1]s/semgrep.sarif .   (`--config auto` refuses to run with metrics off; name the rulesets the document chose)\n"+
			"  rm -f %[1]s/betterleaks.sarif && betterleaks dir . --redact --report-format sarif --report-path %[1]s/betterleaks.sarif\n"+
			"  rm -f %[1]s/trivy.sarif && trivy fs --scanners vuln,misconfig --format sarif -o %[1]s/trivy.sarif .   (secrets are betterleaks'; trivy's own secret scanner leaves a key's tail in its message)\n"+
			"  from the target's root, so every path is repo-relative; `rm -f` first, because betterleaks and trivy leave the OLD log in place when they fail",
			dir, w.M.Target)
	}
	sort.Strings(files)
	// A log older than the target's HEAD commit cannot be a scan of it, and a
	// failed re-run leaves the old log byte-identical: measured, trivy with an
	// empty cache and betterleaks with a missing config each exited 1 over an
	// existing log and left it in place, which then read as a fresh scan.
	if ct, err := (gitx.Repo{Dir: w.M.Target}).Run("log", "-1", "--format=%ct"); err == nil {
		if sec, err := strconv.ParseInt(strings.TrimSpace(ct), 10, 64); err == nil {
			head := time.Unix(sec, 0)
			for _, f := range files {
				if st, err := os.Stat(f); err == nil && st.ModTime().Before(head) {
					return fmt.Errorf("%s was written %s, before the target's HEAD commit (%s): it is not a scan of the commit the run reviews — re-run the scanner, removing the old log first, because a failed run leaves it in place", f, st.ModTime().Format(time.RFC3339), head.Format(time.RFC3339))
				}
			}
		}
	}

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
		var unresolved []string
		for _, run := range log.Runs {
			if t.Driver == "" {
				t.Driver = run.Tool.Driver.Name
			}
			for _, inv := range run.Invocations {
				invoked = true
				t.Invoked = true
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
				for _, n := range append(append([]sarifNote{}, inv.Notifications...), inv.ConfigNotifications...) {
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
				uri := loc.Artifact.URI
				if base, ok := run.OriginalURIBaseIDs[loc.Artifact.URIBaseID]; ok && base.URI != "" {
					if b, err := url.Parse(strings.TrimSuffix(base.URI, "/") + "/"); err == nil {
						if u, err := url.Parse(uri); err == nil && !u.IsAbs() {
							uri = b.ResolveReference(u).String()
						}
					}
				}
				file := repoRel(uri, w.M.Target)
				// A path that is no file under the target is a hit no unit
				// can own: a scanner run from another directory writes
				// `../tgt/x.py`, and every such hit printed "outside every
				// unit" over a run that called itself complete.
				if _, err := os.Stat(filepath.Join(w.M.Target, filepath.FromSlash(file))); err != nil || filepath.IsAbs(file) || strings.HasPrefix(file, "../") {
					t.Unresolved++
					if len(unresolved) < 3 {
						unresolved = append(unresolved, file)
					}
				}
				res.Candidates = append(res.Candidates, scanHit{
					ID:      fmt.Sprintf("%s-%d", tool, t.Results),
					Tool:    tool,
					Rule:    r.RuleID,
					Level:   r.Level,
					File:    file,
					Line:    max(loc.Region.StartLine, 1),
					Message: oneLine(dropMatch(r.Message.Text), scanMessageMax),
				})
			}
		}
		if dropped > 0 {
			t.Dropped = dropped
			notes = append(notes, fmt.Sprintf("%d result(s) with no file location, not candidates — the run names them as never adjudicated", dropped))
		}
		if t.Unresolved > 0 {
			notes = append(notes, fmt.Sprintf("%d hit(s) name no file under the target (%s) — run the scanner from the target's root; the run names them as never adjudicated", t.Unresolved, strings.Join(unresolved, ", ")))
		}
		if !invoked {
			notes = append(notes, "the log records no invocation, so it cannot say the scanner failed — only a missing log can")
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
	// `wf add` stores a file under its BASENAME unless -as names it: a plain
	// `wf add artifacts/scan/args.json` pinned a copy at artifacts/args.json
	// while the run read the untracked original. One line per file, -as each.
	if rel, err := filepath.Rel(w.Artifacts(), dst); err == nil && !strings.HasPrefix(rel, "..") {
		fmt.Printf("  pin the scan the run is given — each file under its own path, which needs -as:\n")
		for _, f := range append(append([]string{}, files...), dst) {
			if r, err := filepath.Rel(w.Artifacts(), f); err == nil && !strings.HasPrefix(r, "..") {
				fmt.Printf("    wf add -as %s %s\n", filepath.ToSlash(r), f)
			}
		}
	}
	if w.M.RunType() != ws.TypeSecurity {
		fmt.Printf("  note: this workspace is a %s gate; only a security run's skeleton reads args.scan\n", w.M.RunType())
	}
	return nil
}
