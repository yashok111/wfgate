// Package wfcheck is the static check over a Workflow orchestration script.
//
// The pre-flight gate is expensive — opus reviewers, minutes per round — and a
// large share of what it found across five multi-agent phases was mechanical: a
// report field named in a prompt that the agent's schema does not declare, a
// contract block handed to the writer but not to its caller, two agents owning
// one file, `git add -N X && git diff` (which hides the diff entirely), `gofmt
// -l X &&` (which never fires). Those are cheaper to catch here than with a
// frontier model, and catching them here means the reviewers spend their round
// on the things only a reader can see.
//
// Every check below reads the PARSED program (see internal/wfscript). Its
// predecessor read text, and an audit defeated nearly all of it with shapes a
// real author writes — so where a check cannot know something, it now says so
// out loud rather than passing silently over what it could not see.
package wfcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/dop251/goja/ast"

	"git.sumka.site/yakov/wfgate/internal/wfscript"
)

// Level orders the report and decides the exit code.
type Level int

const (
	INFO Level = iota
	WARN
	ERROR
)

func (l Level) String() string { return [...]string{"INFO", "WARN", "ERROR"}[l] }

// Finding is one reported observation.
type Finding struct {
	Level Level
	Line  int
	Msg   string
}

// Report is the outcome of one run.
type Report struct {
	Findings []Finding
}

func (r *Report) add(l Level, line int, format string, a ...any) {
	r.Findings = append(r.Findings, Finding{l, line, fmt.Sprintf(format, a...)})
}

// Counts returns the number of findings per level.
func (r *Report) Counts() (errs, warns, notes int) {
	for _, f := range r.Findings {
		switch f.Level {
		case ERROR:
			errs++
		case WARN:
			warns++
		default:
			notes++
		}
	}
	return
}

// effortTiers is the closed set the Workflow tool accepts. A value outside it
// is rejected at the call — mid-run, after the earlier phases have been paid
// for — so it is an error here.
var effortTiers = []string{"low", "medium", "high", "xhigh", "max"}

// Run performs every check against the script at path. root is the repository
// the script will run in; it is used only by the checks that ask whether a file
// a prompt names actually exists. An EMPTY root means "work it out": inside a
// gate workspace that is the workspace's target, and the report says so.
func Run(path, root string) (*Report, error) {
	s, err := wfscript.Load(path)
	if err != nil {
		return nil, err
	}
	root, rootNote := resolveRoot(root)
	r := &Report{}
	if rootNote != "" {
		r.add(INFO, 1, "%s", rootNote)
	}
	checkMeta(r, s)
	calls := s.AgentCalls()
	checkInventory(r, s, calls)
	checkOptions(r, s, calls)
	checkPromptsVsSchemas(r, s, calls)
	checkOwnership(r, s, calls, root)
	checkFixerReach(r, s, calls, checkOwnershipText(r, s, calls))
	checkPromptFootguns(r, calls)
	checkResultSafety(r, s)
	checkConsts(r, s, calls)
	checkCost(r, calls)
	checkTestNames(r, calls, root)

	sort.SliceStable(r.Findings, func(a, b int) bool {
		if r.Findings[a].Level != r.Findings[b].Level {
			return r.Findings[a].Level > r.Findings[b].Level
		}
		return r.Findings[a].Line < r.Findings[b].Line
	})
	return r, nil
}

// --------------------------------------------------------------- 1. meta --

func checkMeta(r *Report, s *wfscript.Script) {
	c, ok := s.Consts()["meta"]
	if !ok {
		r.add(ERROR, 1, "no `export const meta = {...}` — every workflow script must begin with one")
		return
	}
	if c.Obj == nil {
		r.add(ERROR, c.Line, "meta must be an object literal")
		return
	}
	if bad := impureNode(s, c.Obj); bad != "" {
		r.add(ERROR, c.Line, "meta is not a PURE literal: %s — it must be readable without running the script", bad)
	}
	props, complete := s.ObjectOf(c.Obj)
	if !complete {
		r.add(ERROR, c.Line, "meta is not a pure literal — it must be readable without running the script")
	}
	for _, k := range []string{"name", "description"} {
		if _, ok := props[k]; !ok {
			r.add(ERROR, c.Line, "meta has no `%s`", k)
		}
	}

	// meta.phases against the phase() calls. Both populations are printed,
	// because a comparison used to be SKIPPED when either side came out empty —
	// and one side came out empty whenever a phase() was written across lines or
	// with double quotes, so a declared phase nobody calls passed.
	var declared []string
	if pe, ok := props["phases"]; ok {
		if arr, ok := pe.(*ast.ArrayLiteral); ok {
			for _, el := range arr.Value {
				ep, _ := s.ObjectOf(el)
				if t, ok := ep["title"]; ok {
					if txt, ok := s.StaticString(t); ok {
						declared = append(declared, txt)
					}
				}
			}
		}
	}
	all := s.PhaseTitles()
	var called []string
	computed := 0
	for _, t := range all {
		if t == "<computed>" {
			computed++
			continue
		}
		called = append(called, t)
	}
	if computed > 0 {
		// Named, not guessed at: comparing a title the checker could not read
		// against the declared set manufactures a conflict on both sides.
		r.add(INFO, c.Line, "%d phase() title(s) are computed at run time and are left out of the comparison below", computed)
	}
	if len(declared) == 0 && len(called) == 0 {
		return
	}
	miss, extra := diff(called, declared), diff(declared, called)
	if len(miss) > 0 {
		r.add(ERROR, c.Line, "phase() titles with no meta.phases entry: %s", strings.Join(miss, ", "))
	}
	if len(extra) > 0 {
		// "no phase() call" is what the operator reads, and it is not what the
		// checker means: the title is matched as a LITERAL argument. P2f
		// factored five gates into one `gate(name, phaseTitle, …)` helper whose
		// body calls `phase(phaseTitle)`, and spent an investigation on four
		// entries the script does call. The computed count is on its own INFO
		// line above; the sentence that sends the reader there belongs here.
		hint := ""
		if computed > 0 {
			hint = fmt.Sprintf(" — %d phase() call(s) pass a computed title, which cannot be matched by text; move the title to a literal at the call site", computed)
		}
		r.add(WARN, c.Line, "meta.phases entries with no LITERAL phase() call: %s%s", strings.Join(extra, ", "), hint)
	}
	if len(miss) == 0 && len(extra) == 0 && strings.Join(declared, "|") != strings.Join(called, "|") {
		r.add(WARN, c.Line, "meta.phases and phase() calls have the same titles in a DIFFERENT order — meta: %s / calls: %s",
			strings.Join(declared, " > "), strings.Join(called, " > "))
	}
}

// impureNode names the first thing inside meta that is not a literal. The
// previous test searched the meta SOURCE for `${`, which both missed
// `name: getName()` (accepted as a pure literal) and rejected a description
// legitimately containing the two characters.
func impureNode(s *wfscript.Script, root ast.Node) string {
	found := ""
	wfscript.WalkNode(root, func(n ast.Node) {
		if found != "" {
			return
		}
		switch v := n.(type) {
		case *ast.CallExpression:
			found = "a function call"
		case *ast.Identifier:
			found = "the identifier " + string(v.Name)
		case *ast.TemplateLiteral:
			if len(v.Expressions) > 0 {
				found = "a template interpolation"
			}
		case *ast.BinaryExpression:
			found = "an expression"
		}
	})
	return found
}

func diff(a, b []string) []string {
	have := map[string]bool{}
	for _, x := range b {
		have[x] = true
	}
	var out []string
	for _, x := range a {
		if !have[x] {
			out = append(out, x)
		}
	}
	return out
}

// ---------------------------------------------------------- 2. inventory --

func checkInventory(r *Report, s *wfscript.Script, calls []wfscript.AgentCall) {
	ws := s.Wrappers()
	names := make([]string, 0, len(ws))
	for _, w := range ws {
		names = append(names, w.Name)
	}
	r.add(INFO, 1, "%d agent() call site(s) through %d wrapper(s)%s", len(calls), len(ws), listSuffix(names))

	// A script with no call sites is not a workflow, and reporting zero findings
	// over it is the founding false green: the tool exits clean, and is trusted
	// MORE for it, because a tool said so.
	if len(calls) == 0 {
		r.add(ERROR, 1, "no agent() call sites found — a workflow script that spawns nothing is either wrong or not a workflow; every check below would pass over an empty set")
	}
	// A SECOND, independent population. The inventory above is structural and
	// should not miss anything — but "should not" is what the previous checker
	// also believed while reporting one call site out of nineteen, so the count
	// is cross-checked against a textual one. Each wrapper legitimately
	// contributes its own forwarding call, which the inventory excludes.
	//
	// The population is counted over CODE only, and a wrapper's own declaration
	// is not a call. Both were measured on P2f: `async function spawn(prompt,
	// opts)` matches `spawn(`, so every conforming script — the guide REQUIRES
	// the wrapper — reported exactly one call site too few and this ERROR fired
	// on all six scripts of the run. A prose comment (`// one agent() call, no
	// loop`) and a log string (`'2 agent(s) came back'`) did the same. An
	// operator who launches five workflows over a standing ERROR is an operator
	// trained to ignore the word, which costs the run the day it means
	// something.
	code := stripNonCode(s.Src)
	declared := map[string]map[int]bool{}
	for _, w := range ws {
		if declared[w.Name] == nil {
			declared[w.Name] = map[int]bool{}
		}
		declared[w.Name][w.Line] = true
	}
	textual := 0
	for _, name := range append([]string{"agent"}, names...) {
		for _, loc := range regexp.MustCompile(`\b`+regexp.QuoteMeta(name)+`\s*\(`).FindAllStringIndex(code, -1) {
			if declared[name][lineAt(code, loc[0])] {
				continue
			}
			textual++
		}
	}
	forwards := 0
	for _, w := range ws {
		if w.Forwards > 0 {
			forwards += w.Forwards
		} else {
			forwards++ // an alias or a wrapper-on-wrapper forwards once
		}
	}
	if textual-forwards > len(calls) {
		r.add(ERROR, 1, "inventory implausible: %d call site(s) found, but the source mentions %d call(s) to agent() or a wrapper — every check below is passing over a smaller set than the script has",
			len(calls), textual-forwards)
	}
	// The guide requires the wrapper to be called `spawn`. This is now a rule
	// the checker REPORTS rather than the mechanism it depends on.
	direct := 0
	for _, c := range calls {
		if c.Wrapper == "" {
			direct++
		}
	}
	if direct > 0 {
		r.add(WARN, 1, "%d call(s) go straight to agent() with no wrapper — an unhandled throw at a serial step aborts the whole run, which is why the guide requires one", direct)
	}
	for _, w := range ws {
		if w.Name != "spawn" {
			r.add(WARN, w.Line, "the spawn wrapper is named %q — the guide's canonical name is `spawn`, and a reader greps for it", w.Name)
		}
	}
}

func listSuffix(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return " (" + strings.Join(names, ", ") + ")"
}

// ------------------------------------------------------------ 3. options --

func checkOptions(r *Report, s *wfscript.Script, calls []wfscript.AgentCall) {
	for _, c := range calls {
		// The prompt is checked FIRST, so a call missing both still reports
		// both: the options branch below returns early to keep a call with no
		// options object from also drawing one line per option it lacks.
		if !c.PromptSet {
			r.add(ERROR, c.Line, "agent %s: no prompt argument", c.Label)
		} else if !c.PromptOK {
			r.add(INFO, c.Line, "agent %s: its prompt is composed at run time — checks that read the whole prompt (the schema's required fields, the size meter) do not cover this call; the ones that fire on text FOUND still do", c.Label)
		}
		if !c.OptsFound {
			r.add(ERROR, c.Line, "agent %s: no options object — model, effort and schema have nowhere to be", c.Label)
			continue
		}
		if !c.OptsComplete {
			// Saying "I could not see all of it" is the whole difference between
			// this and the predecessor, which reported what it could not resolve
			// as missing: four phantom ERRORs on a correct `{...BASE}` call.
			r.add(WARN, c.Line, "agent %s: its options spread a value this check cannot resolve — the required-option check below is incomplete for this call", c.Label)
		}
		for _, f := range []string{"model", "effort", "schema"} {
			if _, ok := c.Opt(f); ok {
				continue
			}
			if !c.OptsComplete {
				continue
			}
			why := ""
			if f == "effort" {
				why = " — it then inherits the launching session, so the script does not say what the run costs"
			}
			r.add(ERROR, c.Line, "agent %s: no `%s:` in its options%s", c.Label, f, why)
		}
		for _, f := range []string{"label", "phase"} {
			if _, ok := c.Opt(f); !ok && c.OptsComplete {
				if f == "label" && c.Labelled {
					continue
				}
				r.add(WARN, c.Line, "agent %s: no `%s:` in its options", c.Label, f)
			}
		}
		if _, ok := c.Opt("effort"); ok && c.OptsComplete {
			// OptStr, not a second resolution: re-resolving the expression here
			// happens with the wrapper's parameter binding popped, so a factory
			// parameter answered from a same-named global.
			txt, known := c.OptStr("effort")
			switch {
			case !known:
				// Named, not shrugged at: a static report that stays silent here
				// reads as "verified".
				r.add(INFO, c.Line, "agent %s: effort is not a static value — the closed enum cannot be checked here, the dry run checks it at the call", c.Label)
			case !contains(effortTiers, txt):
				r.add(ERROR, c.Line, "agent %s: effort %q is not one of %s", c.Label, txt, strings.Join(effortTiers, ", "))
			}
		}
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// --------------------------------------------- 4. prompts against schemas --

var reportsIn = regexp.MustCompile(`\b(?:in|into|under|to)\s+"([a-z][A-Za-z]{2,})"`)
var negated = regexp.MustCompile(`(?i)\bno\b|\bnot\b|never|loses it|does not`)

func checkPromptsVsSchemas(r *Report, s *wfscript.Script, calls []wfscript.AgentCall) {
	for _, c := range calls {
		e, ok := c.Opt("schema")
		if !ok {
			continue
		}
		name := "the inline schema"
		if id, isID := e.(*ast.Identifier); isID {
			name = string(id.Name)
		}
		props, complete := schemaProps(s, e)
		if !complete {
			r.add(INFO, c.Line, "agent %s: its schema (%s) could not be read statically — the prompt-vs-schema checks below do not cover this call", c.Label, name)
			continue
		}
		if len(props) == 0 {
			continue
		}
		// The predecessor only reported a field that some OTHER schema declared
		// ("is this field real, and misrouted?"). A field invented in a prompt
		// and declared NOWHERE — the likelier defect — was structurally
		// unreachable. Both are reported now, and the message says which.
		for _, m := range reportsIn.FindAllStringSubmatchIndex(c.Prompt, -1) {
			f := c.Prompt[m[2]:m[3]]
			if props[f] {
				continue
			}
			// Only the text PRECEDING the field: "report it in \"x\" — do not
			// summarise" is an instruction with a trailing clause, and matching
			// the whole line let the commonest such clause disable the check.
			if negated.MatchString(lineBefore(c.Prompt, m[0])) {
				continue
			}
			r.add(ERROR, c.Line, "agent %s: its prompt tells it to report in %q, but its schema (%s) has no such field (it has: %s)",
				c.Label, f, name, strings.Join(sortedKeys(props), ", "))
		}
		// A domain-specific required field the prompt never mentions is a value
		// the agent must report without having been told how to obtain it.
		//
		// This one asks whether evidence is ABSENT, so it is the one check here
		// that a partially-read prompt can turn into a false report — and it
		// did, on a live gate: the missing text was the prompt's own first
		// chunk. The polarity rule this project briefs its reviewers with
		// applies to the checker itself. The ERROR above is unaffected: it fires
		// on text it FOUND.
		if !c.PromptOK {
			continue
		}
		for _, f := range schemaRequired(s, e) {
			if genericRequired[f] || strings.Contains(c.Prompt, f) {
				continue
			}
			r.add(WARN, c.Line, "agent %s: %s.required includes %q, but its prompt never mentions it", c.Label, name, f)
		}
	}
}

var genericRequired = map[string]bool{
	"files": true, "summary": true, "verified": true, "contracts": true,
	"findings": true, "output": true,
}

func lineBefore(s string, i int) string {
	return s[strings.LastIndexByte(s[:i], '\n')+1 : i]
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// schemaProps returns the top-level property names of a JSON Schema literal,
// whether it was written inline or bound to a const. An inline schema silently
// disabled this whole check in the predecessor, because schemas were harvested
// only from `const NAME = {...}` and looked up by identifier.
func schemaProps(s *wfscript.Script, e ast.Expression) (map[string]bool, bool) {
	obj, complete := s.ObjectOf(e)
	if !complete {
		return nil, false
	}
	pe, ok := obj["properties"]
	if !ok {
		return nil, true
	}
	props, ok2 := s.ObjectOf(pe)
	out := map[string]bool{}
	for k := range props {
		out[k] = true
	}
	return out, ok2
}

func schemaRequired(s *wfscript.Script, e ast.Expression) []string {
	obj, complete := s.ObjectOf(e)
	if !complete {
		return nil
	}
	re, ok := obj["required"]
	if !ok {
		return nil
	}
	arr, ok := re.(*ast.ArrayLiteral)
	if !ok {
		return nil
	}
	var out []string
	for _, el := range arr.Value {
		if txt, ok := s.StaticString(el); ok {
			out = append(out, txt)
		}
	}
	return out
}

// ---------------------------------------------------------- 5. ownership --

// pathRe accepts any `dir/file.ext`, plus the bare filenames a repository root
// carries. The predecessor hardcoded a Go directory vocabulary
// (internal|cmd|scripts|pkg|src|test|tests), so on a JS, Python or
// `packages/`-shaped tree the ownership map came out EMPTY and the
// dual-ownership error looped over zero entries without a word.
// The third alternative is any bare filename at a repository root, because the
// hardcoded list was a vocabulary too: two live gates own `CLAUDE.md`, which is
// in no list and has no slash, so the entry naming it read as unreadable. Its
// shape is deliberately narrow — a stem of at least two characters and an
// ALPHABETIC extension of at least two — because the loose form matches `e.g`
// and `i.e`, and a check that reports "every blocker in this run cites e.g" is
// worse than one that misses a file.
var pathRe = regexp.MustCompile(`\.?[\w.@{}-]+(?:/[\w.@{}-]+)+\.\w{1,8}|\b(?:Makefile|Dockerfile|README\.md|go\.mod|go\.sum|package\.json|compose\.yaml|docker-compose\.yml)\b|\.?\b[\w@{}-]{2,}\.[A-Za-z]{2,8}\b`)
var citationRe = regexp.MustCompile(`^[\w./-]+\.\w+:\d+`)
var shoutyRe = regexp.MustCompile(`^[A-Z][A-Z0-9 ,'()-]{8,}$`)
var proseRe = regexp.MustCompile(`(?i)[—:]|\s(and|for|the|from|via|see|read)\s`)
var afterPathRe = regexp.MustCompile(`^(\s*$|\s*[—–\-(,:]|\s{2,})`)
var forbidRe = regexp.MustCompile(`(?i)DO NOT|must not|may not|forbidden`)

// ownedPaths returns the paths a prompt's `YOU OWN` block declares, in the
// order it declares them. ONE parser: the dual-ownership error, the
// existence check and the reviewer/fixer reach check all read ownership from
// it, because two readings of "what does this agent own" is the second-copy
// defect this project names as its most frequent.
func ownedPaths(prompt string) []ownedPath {
	i := strings.Index(strings.ToUpper(prompt), "YOU OWN")
	if i == -1 {
		return nil
	}
	var out []ownedPath
	lines := strings.Split(prompt[i:], "\n")
	excluding := false
	for n, l := range lines {
		if n >= 40 {
			break
		}
		t := strings.TrimSpace(l)
		if n > 0 && shoutyRe.MatchString(t) {
			break
		}
		// A blank line ends an exclusion region. The region is a REGION and
		// not the rest of the prompt: ending the whole scan at "DO NOT" lost
		// every owner declared after it, and — because the header line
		// itself often reads "YOU OWN these files and must not write
		// anywhere else" — lost every owner of such a prompt outright. That
		// silenced a real dual-ownership ERROR the previous release
		// reported, which is a worse defect than the false one it was
		// fixing: this checker may neither invent an ERROR nor lose one.
		if t == "" {
			excluding = false
			continue
		}
		if forbidRe.MatchString(t) {
			// Pathless and below the header: the heading of an exclusion
			// block ("DO NOT"), which shoutyRe cannot end because it demands
			// nine characters and "DO NOT" is six. Carrying a path: an
			// inline exclusion, and only that line is skipped. On the header
			// line itself: prose about the block, never a heading.
			if n > 0 && pathRe.FindString(t) == "" {
				excluding = true
			}
			continue
		}
		if excluding {
			continue
		}
		first := pathRe.FindString(t)
		// An ownership entry STARTS with the path. A path mid-sentence
		// ("read internal/x.go for context") is prose, and counting it
		// manufactures a collision.
		if first == "" || !strings.HasPrefix(t, first) {
			continue
		}
		if citationRe.MatchString(t) {
			continue // a file:line citation owns nothing
		}
		if !afterPathRe.MatchString(t[len(first):]) {
			continue // a path followed by a verb is prose about that file
		}
		for _, m := range pathRe.FindAllStringIndex(t, -1) {
			if proseRe.MatchString(t[:m[0]]) {
				break
			}
			p := t[m[0]:m[1]]
			// NEW is read off the DECLARING line, never off the prompt: a file
			// the run is to create does not exist yet, and a mention of the word
			// three paragraphs away says nothing about this entry.
			//
			// A path declared twice keeps NEW if EITHER line carries it. First
			// occurrence wins made the flag order-dependent: `src/x.ts — NEW`
			// then `src/x.ts — and you also finish it` was silent, and the same
			// two lines swapped drew a "does not exist and is not marked NEW"
			// WARN on a file the script creates.
			isNew := strings.Contains(strings.ToUpper(t), "NEW")
			if i := indexOfPath(out, p); i >= 0 {
				out[i].New = out[i].New || isNew
				continue
			}
			out = append(out, ownedPath{Path: p, New: isNew})
		}
	}
	return out
}

func checkOwnership(r *Report, s *wfscript.Script, calls []wfscript.AgentCall, root string) {
	owners := map[string][]string{}
	declaring := 0
	for _, c := range calls {
		// The denominator counts prompts that CLAIM ownership, not prompts the
		// parser managed to read. Moving it behind the parse collapsed it onto
		// its numerator, so `0 owned path(s) parsed from 4 prompt(s)` — the line
		// authoring.md tells an author to read as "your ownership block is in a
		// form this tool cannot parse" — became `parsed from 0 prompt(s)`, which
		// reads as a script that declares nothing. Measured on two live scripts:
		// the population dropped 5 to 4 and 2 to 1.
		if strings.Contains(strings.ToUpper(c.Prompt), "YOU OWN") {
			declaring++
		}
		paths := ownedPaths(c.Prompt)
		if len(paths) == 0 {
			continue
		}
		for _, o := range paths {
			if !contains(owners[o.Path], c.Label) {
				owners[o.Path] = append(owners[o.Path], c.Label)
			}
			if !o.New && !strings.Contains(o.Path, "{") && root != "" {
				if _, err := os.Stat(filepath.Join(root, o.Path)); err != nil {
					r.add(WARN, c.Line, "agent %s owns %s, which does not exist and is not marked NEW", c.Label, o.Path)
				}
			}
		}
	}
	// The population, always. An empty ownership map and a script whose agents
	// share no file look identical in a green report, and only one of them is
	// news.
	r.add(INFO, 1, "%d owned path(s) parsed from %d prompt(s) declaring YOU OWN", len(owners), declaring)
	var paths []string
	for p := range owners {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if len(owners[p]) > 1 {
			r.add(ERROR, 1, "%s is owned by %d agents: %s — exactly one agent may own a file",
				p, len(owners[p]), strings.Join(owners[p], ", "))
		}
	}
}

// ownedPath is one entry of a `YOU OWN` block.
type ownedPath struct {
	Path string
	New  bool // the entry marks it as a file this run creates
}

func containsPath(ps []ownedPath, p string) bool { return indexOfPath(ps, p) >= 0 }

func indexOfPath(ps []ownedPath, p string) int {
	for i, x := range ps {
		if x.Path == p {
			return i
		}
	}
	return -1
}

// ------------------------------------------------------ 6. shell footguns --

type footgun struct {
	re    *regexp.Regexp
	level Level
	msg   string
}

var footguns = []footgun{
	{regexp.MustCompile(`git add -N\s+(?:[^.\n;][^\n;]*)&&`), ERROR,
		"git add -N <paths> && … : a pathspec matching nothing exits 128 having added nothing, and && then suppresses the diff entirely — reviewers approve an empty diff. Use `git add -N . ; git diff HEAD -- <paths>`"},
	{regexp.MustCompile(`gofmt -l (?:[^.\s][^\s]*)\s*&&`), ERROR,
		`gofmt -l X && … never fires: gofmt -l exits 0 whether or not it printed. Use test -z "$(gofmt -l .)"`},
	{regexp.MustCompile(`(?m)^\s{4,}(?:git|go|make|ls|docker|curl|test)\b[^\n]*<[^\n>]+>`), WARN,
		"a placeholder inside a fenced command line gets pasted into a shell verbatim — move it into the prose"},
}

// checkPromptFootguns scans PROMPT TEXT, not the source file. The predecessor
// scanned the raw source, so a comment WARNING against a footgun — the natural
// way an author records the lesson — was reported as committing it.
func checkPromptFootguns(r *Report, calls []wfscript.AgentCall) {
	for _, c := range calls {
		for _, f := range footguns {
			for _, m := range f.re.FindAllString(c.Prompt, -1) {
				line := strings.TrimSpace(strings.SplitN(m, "\n", 2)[0])
				if len(line) > 120 {
					line = line[:120]
				}
				r.add(f.level, c.Line, "agent %s: %s\n        → %s", c.Label, f.msg, line)
			}
		}
	}
}

// ------------------------------------------------------- 7. result safety --

func checkResultSafety(r *Report, s *wfscript.Script) {
	par := s.CallsTo("parallel")
	pipe := s.CallsTo("pipeline")
	filt := s.FilterBooleanCalls()
	if par > 0 && filt < par {
		r.add(WARN, 1, "%d parallel() call(s) but only %d .filter(Boolean) — a thunk that throws resolves to null in the result array", par, filt)
	}
	if par > 0 && pipe == 0 {
		r.add(INFO, 1, "%d parallel() barrier(s) and no pipeline() — check each barrier is needed for cross-item context", par)
	}
	for _, l := range s.Loops() {
		if !l.Guarded {
			r.add(ERROR, l.Line, "this loop has no budget.total guard of its own — it can run to the 1000-agent cap")
		}
	}
}

// ---------------------------------------------------------- 8. constants --

func checkConsts(r *Report, s *wfscript.Script, calls []wfscript.AgentCall) {
	for name, c := range s.Consts() {
		if name == "meta" {
			continue
		}
		uses := strings.Count(s.Src, name) - 1 // the declaration itself
		if uses <= 0 {
			r.add(WARN, c.Line, "const %s is declared but never used", name)
			continue
		}
		// Only a BLOCK — a contract, a signature, a shared instruction — is
		// expected to reach a prompt. A short scalar const is a value (an
		// effort tier, a model name, a path), and reporting it as "reaches no
		// prompt" is a false error on correct code.
		if !c.IsStr || (!strings.Contains(c.Text, "\n") && len(c.Text) < 200) {
			continue
		}
		in, unread := 0, 0
		for _, a := range calls {
			if !a.PromptOK {
				unread++
				continue
			}
			if c.Text != "" && strings.Contains(a.Prompt, c.Text) {
				in++
			}
		}
		switch {
		case unread > 0 && in == 0:
			// The report says two lines up that it could not read those
			// prompts; erroring on what it could not see would contradict its
			// own note.
			r.add(INFO, c.Line, "%s reaches no prompt this check could read — %d of %d prompts are composed at run time", name, unread, len(calls))
		case in == 0:
			r.add(ERROR, c.Line, "%s is defined but reaches NO prompt", name)
		case in == 1 && unread == 0 && len(calls) > 1:
			r.add(WARN, c.Line, "%s reaches exactly ONE prompt — a shared contract block must reach the writer AND every caller; one that reached neither of its callers shipped a contract two agents then guessed differently", name)
		}
	}
}

// --------------------------------------------------------------- 9. cost --

func checkCost(r *Report, calls []wfscript.AgentCall) {
	biggest, biggestLabel, biggestPartial, partial := 0, "", false, 0
	for _, c := range calls {
		n := len(c.Prompt)
		// A prompt whose text is only partly known measures a LOWER BOUND. The
		// WARN below is unaffected in its POLARITY — it fires on bytes counted,
		// never on bytes missing — but the number it prints is still a floor,
		// and it said so nowhere: a reader told "this prompt is 41 KB" acts on
		// 41 KB. The headline has the same duty for the same reason: a size
		// budget read off a partially resolved prompt is read off fiction.
		if c.PromptSet && !c.PromptOK {
			partial++
		}
		if n > biggest {
			biggest, biggestLabel, biggestPartial = n, c.Label, c.PromptSet && !c.PromptOK
		}
		if n > 40*1024 {
			at := "is " + kb(n)
			if c.PromptSet && !c.PromptOK {
				at = "is at LEAST " + kb(n) + " (composed at run time — the rest is not readable here)"
			}
			r.add(WARN, c.Line, "agent %s: composed prompt %s — over the ~40 KB mark, split its scope or move a blob to a file", c.Label, at)
		}
	}
	switch {
	case biggestLabel != "":
		at := "at " + kb(biggest)
		if biggestPartial {
			at = "at LEAST " + kb(biggest) + " (composed at run time — the rest is not readable here)"
		}
		r.add(INFO, 1, "largest composed prompt: %s %s", biggestLabel, at)
		if partial > 0 {
			r.add(INFO, 1, "%d of %d prompt(s) are composed at run time — every size above is a lower bound", partial, len(calls))
		}
	case len(calls) > 0:
		// 0.0 KB with no name attached is the founding symptom restated.
		r.add(INFO, 1, "no statically readable prompt in %d call site(s) — the size and content checks covered nothing", len(calls))
	}
}

func kb(n int) string { return fmt.Sprintf("%.1f KB", float64(n)/1024) }

// ---------------------------------------------------- 10. drifted symbols --

var goTestName = regexp.MustCompile(`\bTest[A-Z][A-Za-z0-9_]{4,}`)

// checkTestNames reports a prompt that names a Go test which no *_test.go in
// the tree declares. A prompt that names a test tells the agent that test
// exists, and gate rounds have twice been spent on a name that had drifted.
func checkTestNames(r *Report, calls []wfscript.AgentCall, root string) {
	if root == "" {
		return
	}
	var hay strings.Builder
	found := false
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", ".wf":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), "_test.go") {
			found = true
			if b, err := os.ReadFile(p); err == nil {
				hay.Write(b)
				hay.WriteByte('\n')
			}
		}
		return nil
	})
	if !found {
		return // not a Go tree: the check does not apply
	}
	seen := map[string]bool{}
	for _, c := range calls {
		for _, name := range goTestName.FindAllString(c.Prompt, -1) {
			key := c.Label + ":" + name
			if seen[key] {
				continue
			}
			seen[key] = true
			if !strings.Contains(hay.String(), name) {
				r.add(WARN, c.Line, "agent %s: names %s, which no *_test.go in this tree declares — a drifted test name is an instruction the agent cannot follow", c.Label, name)
			}
		}
	}
}
