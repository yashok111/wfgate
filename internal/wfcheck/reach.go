package wfcheck

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dop251/goja/ast"

	"git.sumka.site/yakov/wfgate/internal/wfscript"
)

// gateCfg is the object literal a per-section gate is configured with — the
// `{ slug, phase, goal, reviewers, owns }` the emitted harness passes to
// runGate. It is recognised by SHAPE rather than by the name of the function it
// is passed to: the harness is re-authored per script, and a check that keyed on
// `runGate(` would cover the template and nothing else.
//
// The shape is `owns` AND `reviewers` together, not `owns` alone. An `owns`
// array by itself is a key an author naturally writes on an agent's own options
// object — the emitted harness builds its fix agent's YOU OWN block from exactly
// such a list — and counting one made a script with no gate at all report
// `1 owned path(s) against 1 gate config(s), 1 reachable, 0 NOT`. A gate is the
// thing that spawns reviewers; nothing else in the prescribed shape carries the
// key, and a gate written without one prints as "no object with an `owns`
// array", which is news rather than a green line.
type gateCfg struct {
	Name  string
	Line  int
	Files map[string]bool // paths named by the entries
	Dirs  []string        // directory prefixes an entry names, e.g. `internal/mcp/`
	// OwnsComplete is false only for a GATE-shaped object whose `owns` could
	// not be read in full. An `owns` that does not resolve to an array at all
	// is not a gate config and must not disable anything: a JSON schema with a
	// property NAMED owns switched the whole check off, script-wide, as an
	// INFO — the `effort:`-inside-a-schema-description class, re-entering
	// through a property name.
	OwnsComplete bool
}

// gateConfigs collects every gate-shaped object literal in the script.
func gateConfigs(s *wfscript.Script) []gateCfg {
	var out []gateCfg
	s.Walk(func(n ast.Node) {
		lit, ok := n.(*ast.ObjectLiteral)
		if !ok {
			return
		}
		props, _ := s.ObjectOf(lit)
		ownsExpr, ok := props["owns"]
		if !ok {
			return
		}
		if _, ok := props["reviewers"]; !ok {
			return
		}
		elems, complete := s.ArrayOf(ownsExpr)
		if !complete && len(elems) == 0 {
			// Nothing of the array was readable. Distinguish "a name this tool
			// cannot follow" — a real gate, and the check must say so — from a
			// value that is not an array in the first place, which is not a
			// gate config at all.
			if !looksLikeArray(ownsExpr) {
				return
			}
		}
		g := gateCfg{Line: s.Line(lit.Idx0()), OwnsComplete: complete, Files: map[string]bool{}}
		for _, key := range []string{"slug", "label", "name"} {
			if e, ok := props[key]; ok {
				if txt, ok := s.StaticString(e); ok {
					g.Name = txt
					break
				}
			}
		}
		if g.Name == "" {
			g.Name = fmt.Sprintf("gate@%d", g.Line)
		}
		for _, el := range elems {
			txt, ok := s.StaticString(el)
			if !ok {
				g.OwnsComplete = false
				continue
			}
			files, dirs := ownsTargets(txt)
			// An entry naming nothing this tool recognises as a path is not a
			// readable entry. Counting it as read is how a check reports
			// coverage it does not have.
			if len(files) == 0 && len(dirs) == 0 {
				g.OwnsComplete = false
				continue
			}
			for _, f := range files {
				g.Files[f] = true
			}
			g.Dirs = append(g.Dirs, dirs...)
		}
		out = append(out, g)
	})
	return out
}

// looksLikeArray says whether an expression could be an array this tool merely
// failed to follow, as opposed to a value that is plainly something else.
func looksLikeArray(e ast.Expression) bool {
	switch e.(type) {
	case *ast.ArrayLiteral, *ast.Identifier, *ast.DotExpression, *ast.CallExpression:
		return true
	}
	return false
}

// ownsTargets reads the paths out of ONE `owns` entry. An entry is not a path,
// it is a sentence about one: measured on the gate that motivated this check,
// 19 entries of one script read `internal/admin/server.go (the one D7 comment
// above the preview route, nothing else)` and
// `internal/mcp/tools_ops.go and internal/mcp/tools_traffic.go (the path-building
// of the nine existing tools, nothing else)`. Comparing whole entries by string
// equality drew a WARN on every one of them — the phantom-ERROR failure this
// project has already paid for once.
//
// A token ending in `/` is a directory the gate owns wholesale (`internal/mcp/
// (the new table, helper and test files)`), and reaches every path beneath it.
func ownsTargets(entry string) (files, dirs []string) {
	fields := strings.FieldsFunc(entry, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == ',' || r == '(' || r == ')'
	})
	for _, tok := range fields {
		// The LEADING dot is not punctuation to trim: `.env.example`,
		// `.gitignore` and `.github/workflows/ci.yml` are paths, and trimming
		// it produced a map entry (`env.example`) that matches no file in the
		// tree — so the declared file printed as owned by nobody AND as never
		// written, two false rows from one strip, banked in this project's own
		// record for a day as a typo in somebody else's script. Trailing
		// punctuation is still trimmed, and a `./` prefix is normalised away.
		tok = strings.TrimRight(tok, "`'\"“”.;:*")
		tok = strings.TrimLeft(tok, "`'\"“”;:*")
		tok = strings.TrimPrefix(tok, "./")
		if tok == "" {
			continue
		}
		if strings.HasSuffix(tok, "/") && strings.Contains(strings.TrimSuffix(tok, "/"), "/") {
			dirs = append(dirs, tok)
			continue
		}
		// The whole token must be the path: a path mid-word is not one, and
		// the producer side (ownedPaths) reads paths with this same grammar so
		// the two ends of the comparison cannot disagree about what a path is.
		if pathRe.FindString(tok) == tok {
			files = append(files, tok)
		}
	}
	return files, dirs
}

// checkFixerReach reports a path a REVIEWER can reach that no fix agent can
// write.
//
// Measured on mocker slice A, round 19 and round 20 — blockers that passed
// `wfcheck` 0/0 and `wfdry` 7/7, because both are properties of prompt text and
// ownership lists rather than of control flow: a section owned files that
// appeared in no gate's `owns`, so a finding its own reviewer was briefed to
// look for could not have been fixed by the fix agent that gate spawns.
//
// The reach is script-WIDE, not per phase. Phase was the first join tried and it
// is wrong: in the shape the manual prescribes a producer runs in `Build` and
// the gate reviewing it in `Gate 1`, so joining on the title reported every
// correct script. What the invariant actually says is that a file somebody owns
// must be writable by SOME fix agent; which gate reaches it is the author's
// arrangement.
//
// A gate whose `owns` could not be read in full disables the check and says so:
// reporting what it could not see as missing is how the predecessor drew four
// phantom ERRORs on a correct call. The same rule governs the PROMPT side — a
// prompt composed at run time is text this tool never saw, and asking whether a
// path is absent from it is the polarity error the gate briefs its reviewers
// about.
// textOwned is what the SOURCE-TEXT backstop read, and it is folded into both
// directions here rather than reported beside them. Two reach lines about one
// script — one saying every path is covered and one saying sixteen are owned by
// nobody — is the contradiction this project refuses everywhere else; measured
// on mocker P3c, where both were true of the same file because they counted
// different populations.
func checkFixerReach(r *Report, s *wfscript.Script, calls []wfscript.AgentCall, textOwned map[string]bool) {
	gates := gateConfigs(s)
	var incomplete []string
	for _, g := range gates {
		if !g.OwnsComplete {
			incomplete = append(incomplete, g.Name)
		}
	}

	// A prompt composed at run time is text this tool never saw in full. That
	// bounds what it may conclude, and the bound has a DIRECTION: "this agent
	// owns X" is read off text that is present, so a WARN below stands on what
	// was read and the unread half can only hide more owners. "No agent owns X"
	// is the absence claim, and it is the one the unread text can falsify — so
	// the stale-entry note at the bottom is the part that goes silent.
	// Filtering these calls out of BOTH directions took the check from 6 owned
	// paths to 2 on one live script and from 9 to 0 on the other, which is the
	// feature switching itself off in the name of the rule it was obeying.
	readable := make([]wfscript.AgentCall, 0, len(calls))
	unread := 0
	for _, c := range calls {
		if c.PromptSet && !c.PromptOK {
			unread++
			continue
		}
		readable = append(readable, c)
	}

	// Distinct paths, not occurrences: checkOwnership prints distinct ones on the
	// line immediately above, and two derivations of "how many paths does this
	// script own" disagreeing by one — the second declaration of a
	// dually-owned file — is the second-copy defect on adjacent lines of one
	// report.
	seenPath := map[string]bool{}
	declared, covered, unreachable, fromText := 0, 0, 0, 0
	for _, c := range calls {
		for _, o := range ownedPaths(c.Prompt) {
			if seenPath[o.Path] {
				continue
			}
			seenPath[o.Path] = true
			declared++
			if len(gates) == 0 {
				continue
			}
			if reaches(gates, o.Path) {
				// Sound even when another gate's list is partly unreadable:
				// an entry this tool DID read names the path, and a list it
				// could not read can only add reach, never remove it. Only the
				// negative needs the complete picture.
				covered++
				continue
			}
			if len(incomplete) > 0 {
				continue
			}
			unreachable++
			where := ""
			if ph, ok := c.OptStr("phase"); ok {
				where = fmt.Sprintf(" in phase %q", ph)
			}
			r.add(WARN, c.Line, "agent %s owns %s%s, and no gate lists it in `owns` — a reviewer briefed on that file can file a blocker no fix agent is allowed to touch",
				c.Label, o.Path, where)
		}
	}

	// The text-read paths, in the same two directions and with no label to name:
	// this half knows which BLOCK declares a path and not which agent is handed
	// it, which is exactly why the WARN below says the block rather than the
	// agent.
	// No `seenPath` filter here, and that is a decision rather than an omission:
	// `checkOwnershipText` returns only the paths NO resolved prompt carries, so
	// the two sets are disjoint by construction and a second filter is one fact
	// checked twice — a condition no mutation could make fail.
	var textPaths []string
	for p := range textOwned {
		textPaths = append(textPaths, p)
	}
	sort.Strings(textPaths)
	for _, p := range textPaths {
		declared++
		fromText++
		if len(gates) == 0 {
			continue
		}
		if reaches(gates, p) {
			covered++
			continue
		}
		if len(incomplete) > 0 {
			continue
		}
		unreachable++
		r.add(WARN, 1, "a `YOU OWN` block declares %s, and no gate lists it in `owns` — a reviewer briefed on that file can file a blocker no fix agent is allowed to touch (read from the source text: no prompt carrying this block resolves statically)", p)
	}

	// The population, always: a script with no gate config and a script whose
	// ownership lines up produce the same silence, and only one of them is news.
	// A check that exits zero having covered nothing is this project's founding
	// defect.
	lower := ""
	if unread > 0 {
		lower = fmt.Sprintf(" (a LOWER BOUND: %d prompt(s) are composed at run time and were not read)", unread)
	}
	if fromText > 0 {
		lower += fmt.Sprintf(" — %d of the paths above were read from the SOURCE TEXT rather than from a resolved prompt", fromText)
	}
	switch {
	case len(gates) == 0:
		r.add(INFO, 1, "%d owned path(s) and no object with both an `owns` array and `reviewers` — the reviewer/fixer reach check covered nothing%s; it reads the list a gate builds its fix agent's YOU OWN block from", declared, lower)
	case len(incomplete) > 0:
		r.add(INFO, 1, "reviewer/fixer reach: %d owned path(s) against %d gate config(s), %d reachable by a fix agent, the remaining %d NOT JUDGED%s — the `owns` array of gate(s) %s could not be read in full, and a path missing from a list this tool could not read is not a path nobody owns",
			declared, len(gates), covered, declared-covered, lower, strings.Join(incomplete, ", "))
	case declared == 0:
		r.add(INFO, 1, "%d gate config(s) with an `owns` array and no prompt or literal declaring YOU OWN — the reviewer/fixer reach check compared nothing%s", len(gates), lower)
	default:
		r.add(INFO, 1, "reviewer/fixer reach: %d owned path(s) against %d gate config(s), %d reachable by a fix agent, %d NOT%s", declared, len(gates), covered, unreachable, lower)
	}

	// The other direction is not an error — a fix agent may own more than any
	// producer declares — but a gate owning a path NOBODY produces is usually a
	// stale entry, and it is free to say so. This IS the absence claim, so it is
	// read only over prompts that were read, and it states that bound instead of
	// going silent: every harness in the prescribed shape composes its prompts,
	// so a note that waits for a script with none is a note that never prints.
	var stale []string
	for _, g := range gates {
		for f := range g.Files {
			produced := textOwned[f]
			for _, c := range readable {
				if produced {
					break
				}
				if containsPath(ownedPaths(c.Prompt), f) {
					produced = true
					break
				}
			}
			if !produced {
				stale = append(stale, g.Name+": "+f)
			}
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		bound := ""
		if unread > 0 {
			bound = fmt.Sprintf(" — and %d prompt(s) composed at run time were not read, so this list is a CANDIDATE list, not a verdict", unread)
		}
		r.add(INFO, 1, "%d path(s) in a gate's `owns` that no agent this tool could read declares owning: %s — a fix agent may legitimately own more, but a stale entry looks the same%s", len(stale), strings.Join(stale, ", "), bound)
	}
}

// reaches says whether some gate's `owns` covers p, by name or by owning a
// directory above it.
func reaches(gates []gateCfg, p string) bool {
	for _, g := range gates {
		if g.Files[p] {
			return true
		}
		for _, d := range g.Dirs {
			if strings.HasPrefix(p, d) {
				return true
			}
		}
	}
	return false
}
