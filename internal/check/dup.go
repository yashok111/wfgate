package check

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	Ops  string   // the comparison operators Norm erases; see opsOf
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

// opRe is deliberately the UNAMBIGUOUS multi-character comparison set and
// nothing more. The normaliser erases every non-alphanumeric rune — which is
// what makes a rule written with plain backticks in markdown compare equal to
// the same rule inside a JavaScript template literal — and the price is that it
// also erases the difference between `retry_count == 3` and `retry_count != 3`.
// Measured on this repository's own audit: the two normalised identically, so
// `check rules` reported 0 conflicts over a direct contradiction AND `check dup`
// reported the pair as `sim 1.00`, an exact match — a reviewer reads that as
// "consistent in two places". Being wrong in that direction is worse than being
// blind.
//
// Single-character operators are NOT in the set: `-` opens a markdown bullet and
// hyphenates words, `*` marks emphasis, `<`/`>` appear in `<placeholder>` syntax
// and blockquotes. The four below cannot occur as markup, so this check has no
// noise population at all — the same bargain the numeric mask makes.
var opRe = regexp.MustCompile(`==|!=|<=|>=`)

// opsOf returns the comparison operators of a statement, in order, as one
// string — the skeleton the normaliser drops.
func opsOf(s string) string { return strings.Join(opRe.FindAllString(s, -1), " ") }

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
				ops := opsOf(s.text)
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
				out = append(out, Statement{File: rel, Line: line, Text: strings.TrimSpace(s.text), Norm: norm, Toks: toks, Ops: ops})
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
	// A rule marker at the head of a line opens a statement of its own. Without
	// this, two consecutive `// [R1] ...` / `// [R2] ...` comment lines — the
	// natural way a rule set is written in code — join into one paragraph and
	// each id inherits the other's words.
	if loc := ruleMarkRe.FindStringIndex(trimMarkers(t)); loc != nil && loc[0] == 0 {
		return true
	}
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

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f'
}

type sentenceAt struct {
	text string
	off  int
}

// A terminator may be followed by CLOSING MARKUP before the space. Measured on
// P2f: one rule was marked `**[R1] … never of \`route.Path\` alone.**` in the
// derived artifact and plainly in the source one. The normaliser erases `*`, so
// the two sites should have compared equal — but the closing `**` sat between
// the full stop and the space, so the sentence never ended and swallowed the
// whole paragraph after it. `wf check rules` then reported five sites of one
// rule as disagreeing, printed five byte-identical lines (the difference was
// past the truncation), and cost two cycles: the first fix — breaking the line
// — changed nothing, because unwrapping rejoins it.
// The set is exactly the markup that legitimately WRAPS a sentence and closes
// after its full stop, and nothing else. A backtick was in it and came out: a
// code span ending in a period (“ `router.Build.` “) is one token, not the end
// of a thought, and splitting there invents a sentence boundary inside code —
// the same false-split class this whole splitter was fixed for, from the other
// side. Brackets are out for the same reason: English puts the stop outside
// them ("(an aside)."), so a `.` before `)` is far more often a decimal or an
// abbreviation inside a parenthetical than a terminator.
var sentenceClosers = "*_\"'"

func splitSentencesAt(s string) []sentenceAt {
	var out []sentenceAt
	start := 0
	for i := 0; i < len(s)-1; i++ {
		if s[i] != '.' && s[i] != ';' {
			continue
		}
		j := i + 1
		for j < len(s) && strings.IndexByte(sentenceClosers, s[j]) >= 0 {
			j++
		}
		// Any whitespace, not only the space a paragraph join happens to
		// produce: a tab between sentences is a merge nobody would look for.
		if j >= len(s) || !isSpace(s[j]) {
			continue
		}
		// The closers ride with the NEXT sentence, where the normaliser drops
		// them: cutting at i keeps this sentence identical to the same sentence
		// written without the emphasis, which is the whole point.
		out = append(out, sentenceAt{s[start:i], start})
		start = i + 1
	}
	out = append(out, sentenceAt{s[start:], start})
	return out
}

// FindDuplicates groups statements that appear in more than one file, exactly or
// above the similarity threshold.
func FindDuplicates(root string, sts []Statement, threshold float64) ([]DupGroup, int, error) {
	repo := gitx.Repo{Dir: root}
	blame := map[string][]string{}
	// A blame that FAILS gave every line of that file the empty commit, and two
	// copies whose commits are both "" compare as edited together — so a real
	// divergence between two artifacts git cannot blame (an untracked draft, a
	// repository in an unexpected state) was silently downgraded to agreement.
	// The error is carried out instead.
	var blameErr error
	site := func(s Statement) Site {
		b, ok := blame[s.File]
		if !ok {
			var err error
			if b, err = repo.BlameLines(s.File); err != nil && blameErr == nil {
				blameErr = fmt.Errorf("%s: %w", s.File, err)
			}
			blame[s.File] = b
		}
		sha := ""
		if s.Line-1 >= 0 && s.Line-1 < len(b) {
			sha = b[s.Line-1]
		}
		return Site{File: s.File, Line: s.Line, Text: s.Text, Commit: sha}
	}

	// Exact normalised matches first. They are the common case and grouping them
	// up front keeps the quadratic pass below from re-reporting them.
	// Keyed by the normalised form AND the operator skeleton: two statements
	// whose words agree and whose operators do not are a CONTRADICTION, and
	// grouping them here would print it as `sim 1.00`.
	byNorm := map[string][]Statement{}
	for _, s := range sts {
		byNorm[s.Norm+"\x00"+s.Ops] = append(byNorm[s.Norm+"\x00"+s.Ops], s)
	}
	var groups []DupGroup
	for _, g := range byNorm {
		if len(g) < 2 || !multiFile(g) {
			continue
		}
		groups = append(groups, mkGroup(1.0, g, site))
	}

	// Near matches. Candidates come from an inverted index over rare tokens,
	// which turns an O(n^2) scan over every artifact sentence into something
	// that finishes instantly on a real workspace.
	//
	// This is a RECALL trade, not a free one, and the comment here used to claim
	// otherwise ("a pair that shares no rare token cannot reach the threshold" —
	// false: two eleven-token statements sharing ten COMMON tokens reach 0.83).
	// Measured: the same near-duplicate pair is reported, or not, depending on
	// how many unrelated statements elsewhere in the corpus happen to use its
	// words. So the statements this filter leaves with NO candidate are counted
	// and returned, and the report prints them: a corpus the near pass could not
	// see is not a corpus it found nothing in.
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
	// A statement that belongs to an exact cross-file group is NOT excluded from
	// this pass. Excluding it — in either role — is how the check's recall FELL
	// as a rule spread: two verbatim copies plus a third that contradicts them
	// reported nothing, while deleting one of the verbatim copies made the same
	// corpus report the contradiction at sim 0.94. The rule living in a decision
	// document, a context document and a prompt string is the shape this check
	// was built for, so the third copy is exactly the one it must reach. Only
	// the pair the exact pass already reported is skipped, below.
	seen := map[[2]int]bool{}
	blind := 0
	for i, s := range sts {
		cand := map[int]bool{}
		rare := 0
		for _, t := range s.Toks {
			if df[t] > rareDF {
				continue
			}
			rare++
			for _, j := range inv[t] {
				if j != i {
					cand[j] = true
				}
			}
		}
		// A statement built entirely from words the corpus uses often has no
		// candidate at all: the near-match pass cannot see it, whatever it says.
		if rare == 0 {
			blind++
		}
		for j := range cand {
			o := sts[j]
			// Identical normalised forms belong to the exact pass, or — when
			// only their operators differ — to FindOperatorConflicts; either
			// way a jaccard of 1.0 over them would print as agreement. A rule
			// present verbatim in three artifacts and reworded in a fourth
			// yields one near-match pair per verbatim copy: the multiplicity is
			// the price of naming every site of the contradiction.
			if o.File == s.File || o.Norm == s.Norm {
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
	if blameErr != nil {
		return nil, 0, fmt.Errorf("blame: %w; divergence cannot be decided without it", blameErr)
	}
	return groups, blind, nil
}

// rareDF is the document-frequency cap above which a token is considered too
// common to narrow candidates. Raising it costs time and buys recall; it never
// affects the exact pass, which is complete on its own. FindDuplicates returns
// how many statements the cap left with no candidate at all, because a
// threshold that silently hides part of the corpus is the one thing this
// project refuses to let a green report imply.
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

// OperatorConflict is one set of statements whose WORDS are identical and whose
// comparison operators are not — `retry_count == 3` beside `retry_count != 3`.
// It is reported separately from a duplicate because it is the opposite of one:
// a duplicate says two artifacts agree, this says they cannot both be followed.
type OperatorConflict struct {
	Sites []Site
}

// FindOperatorConflicts groups statements that normalise identically but carry
// different comparison operators, across files. See opRe for why the operator
// set is four tokens wide and has no noise population.
func FindOperatorConflicts(root string, sts []Statement) ([]OperatorConflict, error) {
	repo := gitx.Repo{Dir: root}
	blame := map[string][]string{}
	var blameErr error
	site := func(s Statement) Site {
		b, ok := blame[s.File]
		if !ok {
			var err error
			if b, err = repo.BlameLines(s.File); err != nil && blameErr == nil {
				blameErr = fmt.Errorf("%s: %w", s.File, err)
			}
			blame[s.File] = b
		}
		sha := ""
		if s.Line-1 >= 0 && s.Line-1 < len(b) {
			sha = b[s.Line-1]
		}
		return Site{File: s.File, Line: s.Line, Text: s.Text, Commit: sha}
	}
	byNorm := map[string][]Statement{}
	for _, s := range sts {
		byNorm[s.Norm] = append(byNorm[s.Norm], s)
	}
	var keys []string
	for k, g := range byNorm {
		ops, files := map[string]bool{}, map[string]bool{}
		for _, s := range g {
			ops[s.Ops] = true
			files[s.File] = true
		}
		if len(ops) > 1 && len(files) > 1 {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var out []OperatorConflict
	for _, k := range keys {
		oc := OperatorConflict{}
		for _, s := range byNorm[k] {
			oc.Sites = append(oc.Sites, site(s))
		}
		out = append(out, oc)
	}
	return out, blameErr
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
