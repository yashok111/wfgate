package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The shapes measured on real logs 2026-10-13 (semgrep 1.180.0, betterleaks
// 1.9.0, trivy 0.75.0): semgrep records an invocation with executionSuccessful
// and warning-level notifications for files it could not parse — and, pointed
// at a config that does not exist, executionSuccessful TRUE beside error-level
// notifications and no result; betterleaks and trivy record NO invocation;
// betterleaks without --redact writes the secret itself into region.snippet.
const sarifSemgrep = `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Semgrep OSS"}},
 "invocations":[{"executionSuccessful":true,"toolExecutionNotifications":[{"level":"warning","message":{"text":"Syntax error at line x.go:5"}}]}],
 "results":[{"ruleId":"go.sqli","level":"error","message":{"text":"tainted\n  query"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"internal/db/q.go"},"region":{"startLine":12}}}]},
            {"ruleId":"go.nofile","message":{"text":"no location"}}]}]}`

const sarifLeaks = `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"betterleaks"}},
 "results":[{"ruleId":"aws-access-token","message":{"text":"aws-access-token has detected secret for file conf.env."},
   "locations":[{"physicalLocation":{"artifactLocation":{"uri":"file://TARGET/conf.env"},"region":{"startLine":3,"snippet":{"text":"AKIAEXAMPLESECRET0042"}}}}]}]}]}`

const sarifSemgrepNoConfig = `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Semgrep OSS"}},
 "invocations":[{"executionSuccessful":true,"toolExecutionNotifications":[{"level":"error","message":{"text":"WARNING: unable to find a config; path /nonexistent.yml does not exist"}}]}],"results":[]}]}`

// trivy's secret scanner masks a key but keeps its tail inside a `Match:`
// line (measured: six characters of a planted key reached args.json), and a
// uri is percent-encoded.
const sarifTrivySecret = `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Trivy"}},
 "results":[{"ruleId":"stripe-secret-token","message":{"text":"Artifact: a b/conf.py\nType: \nSecret Stripe Secret Key\nSeverity: CRITICAL\nMatch: STRIPE = \"****************6hG5fD\""},
   "locations":[{"physicalLocation":{"artifactLocation":{"uri":"a%20b/conf.py"},"region":{"startLine":2}}}]}]}]}`

const sarifFailed = `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Trivy"}},
 "invocations":[{"executionSuccessful":false,"toolExecutionNotifications":[{"level":"error","message":{"text":"db download failed"}}]}],"results":[]}]}`

func scanWorkspace(t *testing.T) (ws, target string) {
	t.Helper()
	setGitEnv(t)
	tmp := t.TempDir()
	target = mkTarget(t, tmp)
	ws = filepath.Join(tmp, "g")
	_, err := runWF(t, tmp, "init", "g", "-target", target, "-dir", ws, "-type", "security")
	noErr(t, err, "init -type security")
	return ws, target
}

func readScanArgs(t *testing.T, path string) scanArgs {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m struct{ Scan scanArgs }
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: %v", b, err)
	}
	return m.Scan
}

func TestScanFoldsSARIFIntoArgs(t *testing.T) {
	g, target := scanWorkspace(t)
	dir := filepath.Join(g, "artifacts", "scan")
	write(t, filepath.Join(dir, "semgrep.sarif"), sarifSemgrep)
	write(t, filepath.Join(dir, "betterleaks.sarif"), strings.ReplaceAll(sarifLeaks, "TARGET", target))
	write(t, filepath.Join(dir, "trivy.sarif"), sarifFailed)
	out, err := runWF(t, g, "scan")
	noErr(t, err, "scan")
	mustContain(t, out, "trivy        FAILED", "1 execution error(s)", "1 warning(s)", "1 result(s) with no file location", "records no invocation", "wf add")

	raw, _ := os.ReadFile(filepath.Join(dir, "args.json"))
	if strings.Contains(string(raw), "AKIAEXAMPLESECRET0042") {
		t.Fatalf("a snippet is the secret itself and must never reach the args: %s", raw)
	}
	a := readScanArgs(t, filepath.Join(dir, "args.json"))
	byTool := map[string]scanTool{}
	for _, tl := range a.Tools {
		byTool[tl.Tool] = tl
	}
	if !byTool["semgrep"].OK || byTool["trivy"].OK || byTool["semgrep"].Warns != 1 || byTool["semgrep"].Results != 1 {
		t.Fatalf("per-scanner state: %+v", a.Tools)
	}
	if !byTool["betterleaks"].OK {
		t.Fatalf("a log with no invocation is read as having run: %+v", byTool["betterleaks"])
	}
	var leak, sql scanHit
	for _, h := range a.Candidates {
		switch h.Tool {
		case "betterleaks":
			leak = h
		case "semgrep":
			sql = h
		}
	}
	if leak.File != "conf.env" || leak.Line != 3 || leak.ID != "betterleaks-1" {
		t.Fatalf("an absolute file:// uri under the target is made repo-relative: %+v", leak)
	}
	if sql.File != "internal/db/q.go" || sql.Message != "tainted query" {
		t.Fatalf("a message is one line: %+v", sql)
	}
	if !byTool["semgrep"].Invoked || byTool["betterleaks"].Invoked || byTool["semgrep"].Dropped != 1 {
		t.Fatalf("invocation and dropped results are carried to the run: %+v", a.Tools)
	}
	// Every file is pinned under its own path: a bare `wf add` flattens it.
	mustContain(t, out, "wf add -as scan/semgrep.sarif ", "wf add -as scan/args.json ")
	_, err = runWF(t, g, "add", "-as", "scan/args.json", filepath.Join(dir, "args.json"))
	noErr(t, err, "the printed wf add -as line")
}

func TestScanDropsMatchTextAndDecodesURIs(t *testing.T) {
	g, _ := scanWorkspace(t)
	dir := filepath.Join(g, "artifacts", "scan")
	write(t, filepath.Join(dir, "trivy.sarif"), sarifTrivySecret)
	_, err := runWF(t, g, "scan")
	noErr(t, err, "scan")
	raw, _ := os.ReadFile(filepath.Join(dir, "args.json"))
	if strings.Contains(string(raw), "6hG5fD") {
		t.Fatalf("a Match: line's unmasked tail must not reach the args: %s", raw)
	}
	if h := readScanArgs(t, filepath.Join(dir, "args.json")).Candidates[0]; h.File != "a b/conf.py" {
		t.Fatalf("a percent-encoded uri is decoded: %+v", h)
	}
}

// A scanner that could not load its rules reports executionSuccessful TRUE and
// no result: it must read as FAILED, never as a scan that found nothing.
func TestScanErrorNotificationFails(t *testing.T) {
	g, _ := scanWorkspace(t)
	dir := filepath.Join(g, "artifacts", "scan")
	write(t, filepath.Join(dir, "semgrep.sarif"), sarifSemgrepNoConfig)
	out, err := runWF(t, g, "scan")
	noErr(t, err, "scan")
	mustContain(t, out, "semgrep      FAILED", "unable to find a config")
	if a := readScanArgs(t, filepath.Join(dir, "args.json")); a.Tools[0].OK {
		t.Fatalf("an error notification fails the scanner: %+v", a.Tools[0])
	}
}

func TestScanRefusals(t *testing.T) {
	g, _ := scanWorkspace(t)
	_, err := runWF(t, g, "scan")
	if err == nil || !strings.Contains(err.Error(), "semgrep scan --metrics=off --config p/default") || strings.Contains(err.Error(), "config auto ") {
		t.Fatalf("no log: refused with the commands that write one: %v", err)
	}
	dir := filepath.Join(g, "artifacts", "scan")
	write(t, filepath.Join(dir, "semgrep.sarif"), sarifSemgrep)
	write(t, filepath.Join(dir, "x", "semgrep.json.sarif"), sarifSemgrep)
	_, err = runWF(t, g, "scan", filepath.Join(dir, "semgrep.sarif"), filepath.Join(dir, "x", "semgrep.json.sarif"))
	if err == nil || !strings.Contains(err.Error(), `both name the scanner "semgrep"`) {
		t.Fatalf("two logs under one scanner id are refused: %v", err)
	}
	write(t, filepath.Join(dir, "empty.sarif"), `{"version":"2.1.0","runs":[]}`)
	_, err = runWF(t, g, "scan", filepath.Join(dir, "empty.sarif"))
	if err == nil || !strings.Contains(err.Error(), "no run") {
		t.Fatalf("a log with no run is not a scan that found nothing: %v", err)
	}
}

// The security skeleton is what new-script emits in a security workspace, and
// the operator is told the scan step and the report's secrets check.
func TestNewScriptSecurity(t *testing.T) {
	g, _ := scanWorkspace(t)
	out, err := runWF(t, g, "new-script", "sec")
	noErr(t, err, "new-script")
	mustContain(t, out, "wf scan", "betterleaks dir artifacts/report.md", "--owns-diff", "maxRepro")
	b, err := os.ReadFile(filepath.Join(g, "artifacts", "sec.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "'GIT_PATH'") || !strings.Contains(string(b), "const SCANNERS = []") {
		t.Fatalf("the emitted skeleton is the security one, with git's path written in")
	}
}

// The review round's SARIF shapes: a uri relative to a named base (trivy's
// ROOTPATH), a path that is no file under the target, and an error in
// toolConfigurationNotifications beside a success flag.
func TestScanBasesUnresolvedAndConfigErrors(t *testing.T) {
	g, target := scanWorkspace(t)
	write(t, filepath.Join(target, "svc", "go.mod"), "module x\n")
	dir := filepath.Join(g, "artifacts", "scan")
	write(t, filepath.Join(dir, "trivy.sarif"), `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Trivy"}},
 "originalUriBaseIds":{"ROOTPATH":{"uri":"file://`+target+`/svc/"}},
 "results":[{"ruleId":"CVE-1","message":{"text":"m"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"go.mod","uriBaseId":"ROOTPATH"},"region":{"startLine":1}}}]},
            {"ruleId":"CVE-2","message":{"text":"m"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"../elsewhere/x.py"},"region":{"startLine":1}}}]}]}]}`)
	write(t, filepath.Join(dir, "codeql.sarif"), `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"CodeQL"}},
 "invocations":[{"executionSuccessful":true,"toolConfigurationNotifications":[{"level":"error","message":{"text":"query pack missing"}}]}],"results":[]}]}`)
	out, err := runWF(t, g, "scan")
	noErr(t, err, "scan")
	mustContain(t, out, "codeql       FAILED", "query pack missing", "1 hit(s) name no file under the target")
	a := readScanArgs(t, filepath.Join(dir, "args.json"))
	for _, h := range a.Candidates {
		if h.Rule == "CVE-1" && h.File != "svc/go.mod" {
			t.Fatalf("a uri relative to its base is resolved through originalUriBaseIds: %+v", h)
		}
	}
	for _, tl := range a.Tools {
		if tl.Tool == "trivy" && tl.Unresolved != 1 {
			t.Fatalf("one hit names no file under the target: %+v", tl)
		}
	}
}

// A log older than the target's HEAD commit is refused: a failed re-run
// leaves the old one in place.
func TestScanRefusesAStaleLog(t *testing.T) {
	g, _ := scanWorkspace(t)
	f := filepath.Join(g, "artifacts", "scan", "semgrep.sarif")
	write(t, f, sarifSemgrep)
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(f, old, old); err != nil {
		t.Fatal(err)
	}
	_, err := runWF(t, g, "scan")
	if err == nil || !strings.Contains(err.Error(), "before the target's HEAD commit") {
		t.Fatalf("a log older than HEAD is not a scan of it: %v", err)
	}
}

// The sweep's survivors: executionSuccessful false with no notification at
// all, and a result whose location carries an EMPTY uri.
func TestScanFlagAloneAndEmptyURI(t *testing.T) {
	g, _ := scanWorkspace(t)
	dir := filepath.Join(g, "artifacts", "scan")
	write(t, filepath.Join(dir, "x.sarif"), `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"X"}},"invocations":[{"executionSuccessful":false}],
 "results":[{"ruleId":"r","message":{"text":"m"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":""},"region":{"startLine":1}}}]}]}]}`)
	out, err := runWF(t, g, "scan")
	noErr(t, err, "scan")
	mustContain(t, out, "x            FAILED", "1 result(s) with no file location")
	a := readScanArgs(t, filepath.Join(dir, "args.json"))
	if a.Tools[0].OK || len(a.Candidates) != 0 || a.Tools[0].Dropped != 1 {
		t.Fatalf("the flag alone fails the scanner, and an empty uri is no location: %+v", a)
	}
}
