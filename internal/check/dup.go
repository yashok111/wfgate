package check

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"git.sumka.site/yakov/wfgate/internal/gitx"
)

// A gate's artifacts restate the same rules in different places: the decision
// document states one, the context document the agents read restates it, and the
// script's prompt strings restate it again in the wording an agent will see.
// Correcting a rule therefore means correcting every copy, and the copy that
// gets left behind is this project's single most repeated defect — it accounted
// for most of the findings in the cheap tail rounds of the last gate, where each
// round cost a full fan-out to produce one blocker.
//
// The check below finds those copies mechanically. It is text similarity, not
// understanding: it reports that two artifacts say nearly the same sentence and
// that git last touched them in different commits. Whether the difference is a
// correction or an intentional rewording is the operator's call, and the check
// prints both texts so that call takes seconds.

// Statement is one normalised sentence from an artifact.
type Statement struct {
	File string
	Line int
	Text string   // as written, trimmed
	Norm string   // normalised form used for comparison
	Toks []string // sorted unique tokens of Norm
}

// Site is one location of a duplicated statement, with the commit that last
// touched THAT LINE.
type Site struct {
	File   string
	Line   int
	Text   string
	Commit string
}

// DupGroup is a set of near-identical statements living in different files.
type DupGroup struct {
	Similarity float64 // 1.0 for an exact normalised match
	Sites      []Site
	Divergent  bool // see mkGroup: the copies differ AND were edited apart
}

// noiseRe-equivalent: characters that carry no meaning across artifacts. Quoting
// differs by artifact — a rule written with plain backticks in markdown is
// written with escaped backticks inside a JavaScript template literal — so the
// normaliser must erase quoting entirely or the two copies never compare equal.
// That exact mismatch is what let a corrected rule sit next to its stale twin.
func normalise(s string) (string, []string) {
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteByte(' ')
		}
	}
	toks := strings.Fields(b.String())
	uniq := map[string]bool{}
	var us []string
	for _, t := range toks {
		if !uniq[t] {
			uniq[t] = true
			us = append(us, t)
		}
	}
	sort.Strings(us)
	return strings.Join(toks, " "), us
}

// trimMarkers strips the leading decoration a statement carries because of the
// file it lives in — list bullets, comment openers, table pipes — so the same
// sentence in a markdown bullet and in a Go comment normalises alike.
func trimMarkers(s string) string {
	s = strings.TrimSpace(s)
	for {
		t := s
		for _, p := range []string{"//", "*", "#", "-", "|", ">", "+"} {
			t = strings.TrimSpace(strings.TrimPrefix(t, p))
		}
		if t == s {
			return s
		}
		s = t
	}
}

// CollectStatements splits artifacts into comparable sentences. minWords drops
// fragments: below roughly eight words, two unrelated sentences collide often
// enough that the report stops being worth reading.
//
// Lines are UNWRAPPED into paragraphs before sentence-splitting. The splitter
// used to be line-bounded, and two derived artifacts hard-wrapping the same
// sentences at different columns turned every shared thought into differing
// fragment pairs — measured on P2e: 92 divergent groups, ZERO real, 36 of them
// provable prefix-pairs of a wrap split. A sentence is compared as a sentence,
// not as whatever happened to fit on a line.
func CollectStatements(root string, files []string, minWords int) ([]Statement, error) {
	var out []Statement
	for _, rel := range files {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return nil, err
		}
		lines := strings.Split(string(b), "\n")
		type piece struct {
			text string
			line int
		}
		var para []piece
		flush := func() {
			if len(para) == 0 {
				return
			}
			texts := make([]string, len(para))
			starts := make([]int, len(para))
			off := 0
			for i, p := range para {
				texts[i] = p.text
				starts[i] = off
				off += len(p.text) + 1 // the joining space
			}
			joined := strings.Join(texts, " ")
			for _, s := range splitSentencesAt(joined) {
				norm, toks := normalise(s.text)
				if len(strings.Fields(norm)) < minWords {
					continue
				}
				// The sentence is attributed to the line it STARTS on; blame
				// reads that line. A sentence spanning a wrap whose edit landed
				// on its second line attributes to the first — the price of
				// comparing sentences instead of fragments, and the smaller
				// error of the two.
				line := para[0].line
				for i := len(para) - 1; i >= 0; i-- {
					if starts[i] <= s.off {
						line = para[i].line
						break
					}
				}
				out = append(out, Statement{File: rel, Line: line, Text: strings.TrimSpace(s.text), Norm: norm, Toks: toks})
			}
			para = para[:0]
		}
		for n := 1; n <= len(lines); n++ {
			raw := lines[n-1]
			t := trimMarkers(raw)
			if t == "" {
				flush()
				continue
			}
			if startsBlock(raw) {
				flush()
			}
			para = append(para, piece{t, n})
		}
		flush()
	}
	return out, nil
}

// startsBlock reports a line that opens a new statement even without a blank
// line above it: list items, table rows, headings and fences are units of
// their own, and joining two bullets would compare invented sentences nobody
// wrote. A comment continuation (`//`, `>`) is deliberately NOT here — wrapped
// prose inside comments and quotes is exactly what unwrapping exists for.
func startsBlock(raw string) bool {
	t := strings.TrimSpace(raw)
	for _, p := range []string{"- ", "* ", "+ ", "|", "#", "```", "~~~"} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	i := 0
	for i < len(t) && t[i] >= '0' && t[i] <= '9' {
		i++
	}
	return i > 0 && i+1 < len(t) && (t[i] == '.' || t[i] == ')') && t[i+1] == ' '
}

type sentenceAt struct {
	text string
	off  int
}

func splitSentencesAt(s string) []sentenceAt {
	var out []sentenceAt
	start := 0
	for i := 0; i < len(s)-1; i++ {
		if (s[i] == '.' || s[i] == ';') && s[i+1] == ' ' {
			out = append(out, sentenceAt{s[start:i], start})
			start = i + 1
		}
	}
	out = append(out, sentenceAt{s[start:], start})
	return out
}

// FindDuplicates groups statements that appear in more than one file, exactly or
// above the similarity threshold.
func FindDuplicates(root string, sts []Statement, threshold float64) ([]DupGroup, error) {
	repo := gitx.Repo{Dir: root}
	blame := map[string][]string{}
	site := func(s Statement) Site {
		b, ok := blame[s.File]
		if !ok {
			b, _ = repo.BlameLines(s.File)
			blame[s.File] = b
		}
		sha := ""
		if s.Line-1 < len(b) {
			sha = b[s.Line-1]
		}
		return Site{File: s.File, Line: s.Line, Text: s.Text, Commit: sha}
	}

	// Exact normalised matches first. They are the common case and grouping them
	// up front keeps the quadratic pass below from re-reporting them.
	byNorm := map[string][]Statement{}
	for _, s := range sts {
		byNorm[s.Norm] = append(byNorm[s.Norm], s)
	}
	var groups []DupGroup
	exact := map[string]bool{}
	for norm, g := range byNorm {
		if len(g) < 2 || !multiFile(g) {
			continue
		}
		exact[norm] = true
		groups = append(groups, mkGroup(1.0, g, site))
	}

	// Near matches. Candidates come from an inverted index over rare tokens: a
	// pair that shares no rare token cannot reach the threshold, and skipping it
	// turns an O(n^2) scan over every artifact sentence into something that
	// finishes instantly on a real workspace.
	df := map[string]int{}
	for _, s := range sts {
		for _, t := range s.Toks {
			df[t]++
		}
	}
	inv := map[string][]int{}
	for i, s := range sts {
		for _, t := range s.Toks {
			if df[t] <= rareDF {
				inv[t] = append(inv[t], i)
			}
		}
	}
	seen := map[[2]int]bool{}
	for i, s := range sts {
		if exact[s.Norm] {
			continue
		}
		cand := map[int]bool{}
		for _, t := range s.Toks {
			if df[t] > rareDF {
				continue
			}
			for _, j := range inv[t] {
				if j != i {
					cand[j] = true
				}
			}
		}
		for j := range cand {
			o := sts[j]
			if o.File == s.File || exact[o.Norm] {
				continue
			}
			key := [2]int{min(i, j), max(i, j)}
			if seen[key] {
				continue
			}
			seen[key] = true
			if sim := jaccard(s.Toks, o.Toks); sim >= threshold {
				groups = append(groups, mkGroup(sim, []Statement{s, o}, site))
			}
		}
	}

	sort.Slice(groups, func(a, b int) bool {
		if groups[a].Divergent != groups[b].Divergent {
			return groups[a].Divergent
		}
		if groups[a].Similarity != groups[b].Similarity {
			return groups[a].Similarity > groups[b].Similarity
		}
		return groups[a].Sites[0].File < groups[b].Sites[0].File
	})
	return groups, nil
}

// rareDF is the document-frequency cap above which a token is considered too
// common to narrow candidates. It is a performance knob only: raising it costs
// time, never recall of an exact match, which the pass above already has.
const rareDF = 12

func multiFile(g []Statement) bool {
	for _, s := range g[1:] {
		if s.File != g[0].File {
			return true
		}
	}
	return false
}

func mkGroup(sim float64, g []Statement, site func(Statement) Site) DupGroup {
	d := DupGroup{Similarity: sim}
	for _, s := range g {
		d.Sites = append(d.Sites, site(s))
	}
	// Divergence is two conditions, not one. The copies must actually READ
	// differently — an exact normalised match means they agree, whatever their
	// history — and their lines must have been last edited in different commits.
	// Either condition alone is noise: identical copies edited apart are simply
	// a rule restated correctly, and differing copies written in one commit are
	// a deliberate rewording the author already saw side by side.
	if sim < 1.0 {
		for _, s := range d.Sites[1:] {
			if s.Commit != d.Sites[0].Commit {
				d.Divergent = true
			}
		}
	}
	return d
}

// Number words up to twelve: the counts artifacts state in prose. Digits of
// any length are numeric on their own.
var numWords = map[string]bool{
	"zero": true, "one": true, "two": true, "three": true, "four": true,
	"five": true, "six": true, "seven": true, "eight": true, "nine": true,
	"ten": true, "eleven": true, "twelve": true,
}

func isNumericTok(t string) bool {
	if numWords[t] {
		return true
	}
	for i := 0; i < len(t); i++ {
		if t[i] < '0' || t[i] > '9' {
			return false
		}
	}
	return len(t) > 0
}

// NumericConflict is one masked sentence whose copies disagree on a number.
type NumericConflict struct {
	Sites []Statement
}

// FindNumericDrift groups statements whose normalised forms are IDENTICAL once
// numeric tokens are masked, but whose numbers differ, across files. Four of
// the six real contradictions of the P2e gate differed ONLY by a count ("ONE
// comment" vs "two comments"), and no similarity threshold separates that from
// agreement — masking the number does. The false-positive population is
// bounded by how often two artifacts legitimately state different counts of
// the same thing; in the measured corpus, never.
func FindNumericDrift(sts []Statement) []NumericConflict {
	type entry struct {
		nums string
		st   Statement
	}
	byMask := map[string][]entry{}
	for _, s := range sts {
		toks := strings.Fields(s.Norm)
		masked := make([]string, len(toks))
		var nums []string
		for i, t := range toks {
			if isNumericTok(t) {
				masked[i] = "#"
				nums = append(nums, t)
			} else {
				masked[i] = t
			}
		}
		if len(nums) == 0 {
			continue
		}
		key := strings.Join(masked, " ")
		byMask[key] = append(byMask[key], entry{strings.Join(nums, " "), s})
	}
	var keys []string
	for k, g := range byMask {
		distinct := map[string]bool{}
		files := map[string]bool{}
		for _, e := range g {
			distinct[e.nums] = true
			files[e.st.File] = true
		}
		if len(distinct) > 1 && len(files) > 1 {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var out []NumericConflict
	for _, k := range keys {
		nc := NumericConflict{}
		for _, e := range byMask[k] {
			nc.Sites = append(nc.Sites, e.st)
		}
		out = append(out, nc)
	}
	return out
}

func jaccard(a, b []string) float64 {
	set := make(map[string]bool, len(a))
	for _, t := range a {
		set[t] = true
	}
	inter := 0
	for _, t := range b {
		if set[t] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}
