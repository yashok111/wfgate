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

// checkOwnershipText reports what the parsed half could not reach, and returns
// the paths it read so the reach check can fold them in.
//
// It runs ALWAYS and prints only the paths the parsed half did not see, because
// two reports of one collision on adjacent lines is the second-copy defect this
// project names as its most frequent — committed here, in this comment, by the
// wave that names it. A script whose prompts are literals gets nothing from
// here, which is correct: there the strong half ran.
func checkOwnershipText(r *Report, s *wfscript.Script, calls []wfscript.AgentCall) map[string]bool {
	// The AGENTS the parsed half read per path, deduplicated by label exactly as
	// checkOwnership does it: two calls under one label are one owner there, and
	// two derivations of "how many agents own this" disagreeing by one is the
	// second-copy defect on adjacent lines of one report.
	parsedOwners := map[string][]string{}
	for _, c := range calls {
		for _, o := range ownedPaths(c.Prompt) {
			if !contains(parsedOwners[o.Path], c.Label) {
				parsedOwners[o.Path] = append(parsedOwners[o.Path], c.Label)
			}
		}
	}
	_, found := ownsFromLiterals(s.Src)
	blocksByPath := map[string][]int{}
	for _, o := range found {
		blocksByPath[o.Path] = append(blocksByPath[o.Path], o.Block)
	}
	reportCollisions(r, parsedOwners, blocksByPath)

	byPath := map[string][]int{}
	blocksSeen := map[int]bool{}
	var order []string
	unseen := 0
	for _, o := range found {
		if len(parsedOwners[o.Path]) > 0 {
			continue
		}
		unseen++
		// The BLOCKS that contributed one of these paths, not every block in the
		// file: this line is the operator's hand arithmetic ("16 paths in 4
		// blocks, 16 distinct"), and counting a block the parsed half already
		// read makes its middle number false.
		blocksSeen[o.Block] = true
		if _, seen := byPath[o.Path]; !seen {
			order = append(order, o.Path)
		}
		// No dedupe by block: `ownedPaths` already merges a path declared twice
		// inside one block into one entry, so the (path, block) pair cannot
		// repeat. A guard for it was written, no mutation could make it fail,
		// and it was deleted rather than shipped unproven.
		byPath[o.Path] = append(byPath[o.Path], o.Block)
	}
	if unseen == 0 {
		return nil
	}
	sort.Strings(order)
	r.add(INFO, 1, "ownership, read from the SOURCE TEXT: %d path(s) in %d `YOU OWN` block(s) that no resolved prompt carries, %d distinct — the WEAKER half, and it says so: it knows which BLOCK declares a path, not which agent is handed which block",
		unseen, len(blocksSeen), len(order))
	out := map[string]bool{}
	for _, p := range order {
		out[p] = true
	}
	return out
}

// reportCollisions is the dual-ownership ERROR over BOTH halves at once.
//
// The suppression used to be per PATH — a path any resolved prompt owned was
// dropped from the textual side entirely — and that lost the MIXED case, which
// is the one neither half can see alone: agent a's prompt is a literal declaring
// `internal/x.go`, agent b's is composed by a run-time factory whose literal
// declares it too. The parsed half sees ONE owner and does not fire; the textual
// half skipped the path because the parsed half had read it; the lost write
// shipped. Reproduced by disabling that one condition.
//
// So the suppression is per BLOCK instead. Every parsed prompt is composed from
// exactly one `YOU OWN` block, so of the blocks naming p the parsed half has
// already read len(parsedOwners[p]) of them and the rest are owners it never
// saw. The parsed half reports its own collisions, so this fires only where it
// stays silent — fewer than two agents — and two blocks still name the path.
func reportCollisions(r *Report, parsedOwners map[string][]string, blocksByPath map[string][]int) {
	var paths []string
	for p := range blocksByPath {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		agents, blocks := len(parsedOwners[p]), len(blocksByPath[p])
		if agents > 1 {
			continue // checkOwnership names both agents; two reports of one collision is worse
		}
		// Blocks alone, not max(agents, blocks). Every parsed prompt is composed
		// from exactly one block, so the union of the two halves is
		// max(agents, blocks) — but the line above has already sent every path
		// with two or more agents to the parsed half, so agents is 0 or 1 here
		// and the max can never exceed blocks. It was written, no mutation could
		// make it fail, and it was deleted rather than shipped unproven.
		if blocks < 2 {
			continue
		}
		if agents == 0 {
			r.add(ERROR, 1, "%s is declared in %d different `YOU OWN` block(s) — exactly one agent may own a file, and this was read from the text because no prompt carrying it resolves statically", p, blocks)
			continue
		}
		// The MIXED origin, and the message says which half saw what: one agent
		// this tool could read, plus at least one block it could only read as
		// text. It cannot say which agent is handed that block — that is the
		// standing limit of this half — so it names the agent it does know.
		r.add(ERROR, 1, "%s is owned by agent %s AND declared in %d `YOU OWN` block(s) in the source — exactly one agent may own a file; the extra block(s) reach no prompt that resolves statically, so this half knows which BLOCK declares the path and not which agent is handed it",
			p, parsedOwners[p][0], blocks-1)
	}
}
