package wfcheck

import (
	"sort"

	"git.sumka.site/yakov/wfgate/internal/jstext"
	"git.sumka.site/yakov/wfgate/internal/wfscript"
)

// The TEXTUAL backstop of the ownership check, and it is the sibling of the
// inventory one in text.go: there the parser might be wrong, here the parser is
// right and cannot reach.
//
// The collision check is the most valuable static check this tool has — it is
// what catches a LOST WRITE, two agents writing one file — and it reads a
// `YOU OWN` block only out of a prompt it can resolve STATICALLY. Measured on
// mocker P3c: every producer prompt is composed by one factory, so the parsed
// half reported `0 owned path(s) parsed from 1 prompt(s)` and the reach check
// "compared nothing", honestly, over a script declaring sixteen paths. Inlining
// four ~60-line prompts to buy the check back is the round trip the P3b script
// gate made and REVERTED, so the arithmetic was instead done by hand — four
// times over that gate, by three different agents, each writing its own python
// over the file's own literals.
//
// This reads the same literals. It is the WEAKER half and says so: it knows
// which BLOCKS declare a path and not which AGENT is handed which block, so it
// can see that two blocks name one file and cannot see that one prompt is
// spawned twice. Comments are excluded — a comment describing an ownership
// block is not one, and that script carries exactly such a comment.
type textOwn struct {
	Path  string
	New   bool
	Block int // which literal block declared it; two blocks naming one path is the collision
	Line  int
}

// ownsFromLiterals reads every `YOU OWN` block out of the STRING LITERALS of a
// script, decoded, and returns the paths they declare with the block each came
// from.
func ownsFromLiterals(src string) (blocks int, out []textOwn) {
	for _, chunk := range literalChunks(src) {
		paths := ownedPaths(chunk.text)
		if len(paths) == 0 {
			continue
		}
		blocks++
		for _, p := range paths {
			out = append(out, textOwn{Path: p.Path, New: p.New, Block: blocks, Line: chunk.line})
		}
	}
	return blocks, out
}

type litChunk struct {
	text string
	line int
}

// literalChunks joins the prose spans that CONTINUE one another and returns one
// text per string literal, decoded. Joining is not cosmetic: a template literal
// interrupted by a `${...}` hole is one block, and reading its halves as two
// would split an ownership list down the middle.
//
// Comments are dropped. A `//` block explaining an ownership scheme is prose
// about the check, not a declaration, and the script that motivated this file
// carries one directly above the data it describes.
func literalChunks(src string) []litChunk {
	var out []litChunk
	for _, sp := range jstext.ProseSpans(src) {
		if !sp.Escaped {
			continue // a comment
		}
		text := jstext.Unescape(src[sp.From:sp.To])
		if sp.Join && len(out) > 0 {
			out[len(out)-1].text += text
			continue
		}
		out = append(out, litChunk{text: text, line: jstext.LineAt(src, sp.From)})
	}
	return out
}

// checkOwnershipText reports what the parsed half could not reach.
//
// It runs ALWAYS and prints only the paths the parsed half did not see, because
// two reports of one collision on adjacent lines is the second-copy defect this
// project names as its most frequent. A script whose prompts are literals
// therefore gets nothing from here, which is correct: there the strong half ran.
// checkOwnershipText reports what the parsed half could not reach, and returns
// the paths it read so the reach check can fold them in.
//
// It prints only the paths the parsed half did not see, because two reports of
// one collision on adjacent lines is the second-copy defect this project names
// as its most frequent. A script whose prompts are literals therefore gets
// nothing from here, which is correct: there the strong half ran.
func checkOwnershipText(r *Report, s *wfscript.Script, calls []wfscript.AgentCall) map[string]bool {
	parsed := map[string]bool{}
	for _, c := range calls {
		for _, o := range ownedPaths(c.Prompt) {
			parsed[o.Path] = true
		}
	}
	blocks, found := ownsFromLiterals(s.Src)
	byPath := map[string][]int{}
	var order []string
	unseen := 0
	for _, o := range found {
		if parsed[o.Path] {
			continue
		}
		unseen++
		if _, seen := byPath[o.Path]; !seen {
			order = append(order, o.Path)
		}
		if !containsInt(byPath[o.Path], o.Block) {
			byPath[o.Path] = append(byPath[o.Path], o.Block)
		}
	}
	if unseen == 0 {
		return nil
	}
	sort.Strings(order)
	r.add(INFO, 1, "ownership, read from the SOURCE TEXT: %d path(s) in %d `YOU OWN` block(s) that no resolved prompt carries, %d distinct — the WEAKER half, and it says so: it knows which BLOCK declares a path, not which agent is handed which block",
		unseen, blocks, len(order))
	out := map[string]bool{}
	for _, p := range order {
		out[p] = true
		if len(byPath[p]) > 1 {
			r.add(ERROR, 1, "%s is declared in %d different `YOU OWN` block(s) — exactly one agent may own a file, and this was read from the text because no prompt carrying it resolves statically", p, len(byPath[p]))
		}
	}
	return out
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
