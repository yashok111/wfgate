package check

import (
	"regexp"
	"sort"
	"strings"
)

// The stale check answers the question `wf apply` has been printing as advice
// and no command could act on: "if this batch changed a RULE, its consumers are
// NOT in this diff". It is possible only inside this tool, because the apply
// batch is the one artifact in the whole pipeline that records the EXACT text a
// fix replaced.
//
// Mechanism: take the OLD side of every replacement in a commit range, keep the
// tokens the fix RETIRED (present before, absent after), drop the ones the
// corpus uses everywhere, and report the sentences that were NOT touched by the
// range and still carry them.
//
// It is the class no existing check reaches. `dup` finds copies that DRIFTED
// apart; `rules` finds sites of one MARKED id disagreeing; neither sees a
// paragraph that argues from a superseded rule in different words, and that
// paragraph is what a live decision gate paid for in every round of five —
// round 1's fix replaced "+1 in place" with an allocator and left four
// consumers standing, three caught by hand-grep and one filed as a finding a
// round later; round 2's fix left five more, three of them blockers.
//
// The rare-token mechanism is crude ON PURPOSE. It runs in a tenth of a second,
// it is explainable at the point it fires — the tokens are printed — and `dup`'s
// own history says a check that paints most of its output is a check nobody
// runs twice. If the overlap proves noisy on a second gate, narrow the corpus;
// do not add a model.

// stemLen is where a token stops being itself and starts being its family.
// Five characters keeps "expect"/"expectation" and "serialise"/"serialised"
// together while leaving "version"/"versus" apart.
const stemLen = 5

// StaleHit is one untouched sentence still carrying vocabulary a fix retired.
type StaleHit struct {
	File   string
	Line   int
	Text   string
	Tokens []string // the retired tokens THIS sentence carries, rarest first
}

// StaleReport is what the check measured, not only what it found. Every
// population it dropped is carried out so the report cannot read as coverage.
type StaleReport struct {
	Hits []StaleHit
	// Retired is the vocabulary the range removed and did not put back, after
	// the rarity filter.
	Retired []string
	// Common is the count of retired tokens dropped for being everywhere in the
	// corpus. Printed: the OUTPUT is narrowed, never the check, and a hidden
	// population has to say its own size — `dup -since` established that.
	Common int
	// Touched is the number of sentences skipped because the range itself wrote
	// them. They are the fix, not its consumers.
	Touched int
	// Scanned is how many sentences the check actually compared.
	Scanned int
}

// FindStale mines retired vocabulary out of a unified diff (produced with
// --unified=0) and reports the untouched statements that still use it.
//
// maxDF is the rarity bar: a retired token appearing in more than maxDF
// untouched sentences is not evidence of a consumer, it is a word the corpus
// uses for everything.
func FindStale(sts []Statement, diff string, maxDF int) StaleReport {
	files := parseUnified(diff)

	// Retired = removed and NOT re-added. A token the fix kept is vocabulary the
	// rule still owns; only the words that left with the old text point at a
	// paragraph nobody re-read.
	removed, added := map[string]bool{}, map[string]bool{}
	touched := map[string][][2]int{}
	for _, fd := range files {
		for _, l := range fd.removed {
			_, toks := normalise(l)
			for _, t := range toks {
				removed[t] = true
			}
		}
		for _, l := range fd.added {
			_, toks := normalise(l)
			for _, t := range toks {
				added[t] = true
			}
		}
		// New-side ranges, under the NEW path: a statement inside one of them was
		// written by this very range.
		p := fd.newPath
		if p == "" || fd.deleted {
			continue
		}
		for _, h := range fd.hunks {
			if h.newCount == 0 {
				continue
			}
			touched[p] = append(touched[p], [2]int{h.newStart, h.newStart + h.newCount - 1})
		}
	}
	// A fix that replaces "expect" with "expectation" retired NOTHING: the rule
	// still owns the word, the author only inflected it. Measured on a live
	// gate's real fix range: of 11 "retired" tokens, five were the same word in
	// another form or another tense, and each one dragged a handful of innocent
	// sentences into the report. Stems are compared by a fixed prefix rather
	// than by a stemmer — one dependency was argued for once in this project and
	// an English morphology table is not that argument.
	addedStem := map[string]bool{}
	for t := range added {
		if len(t) >= stemLen {
			addedStem[t[:stemLen]] = true
		}
	}
	var cand []string
	for t := range removed {
		// A token with no letter is a line number, a count or a version — the
		// numeric class `dup` already reports as NUMERIC, and here it is pure
		// noise. Short tokens are articles and operators the normaliser kept.
		if added[t] || len(t) < 4 || !hasLetter(t) {
			continue
		}
		if len(t) >= stemLen && addedStem[t[:stemLen]] {
			continue
		}
		cand = append(cand, t)
	}
	sort.Strings(cand)

	rep := StaleReport{}
	// Document frequency over the UNTOUCHED corpus only: a token the fix wrote
	// into twenty new sentences is not common, it is the new rule.
	df := map[string]int{}
	var open []Statement
	for _, st := range sts {
		if inRanges(touched[st.File], st.Line) {
			rep.Touched++
			continue
		}
		open = append(open, st)
		for _, t := range st.Toks {
			df[t]++
		}
	}
	rep.Scanned = len(open)

	keep := map[string]bool{}
	for _, t := range cand {
		switch n := df[t]; {
		case n == 0:
			// Retired everywhere it appeared. Nothing left to re-read: this is
			// the fix that DID reach its consumers, and it is the good case.
		case n > maxDF:
			rep.Common++
		default:
			keep[t] = true
			rep.Retired = append(rep.Retired, t)
		}
	}
	sort.Slice(rep.Retired, func(i, j int) bool {
		if df[rep.Retired[i]] != df[rep.Retired[j]] {
			return df[rep.Retired[i]] < df[rep.Retired[j]]
		}
		return rep.Retired[i] < rep.Retired[j]
	})

	for _, st := range open {
		var hit []string
		for _, t := range st.Toks {
			if keep[t] {
				hit = append(hit, t)
			}
		}
		if len(hit) == 0 {
			continue
		}
		sort.Slice(hit, func(i, j int) bool {
			if df[hit[i]] != df[hit[j]] {
				return df[hit[i]] < df[hit[j]]
			}
			return hit[i] < hit[j]
		})
		rep.Hits = append(rep.Hits, StaleHit{File: st.File, Line: st.Line, Text: st.Text, Tokens: hit})
	}
	// Most retired vocabulary first: a sentence carrying three retired words is
	// a paragraph arguing from the old rule, one carrying a single word is
	// usually a coincidence of vocabulary.
	sort.SliceStable(rep.Hits, func(i, j int) bool {
		if len(rep.Hits[i].Tokens) != len(rep.Hits[j].Tokens) {
			return len(rep.Hits[i].Tokens) > len(rep.Hits[j].Tokens)
		}
		if rep.Hits[i].File != rep.Hits[j].File {
			return rep.Hits[i].File < rep.Hits[j].File
		}
		return rep.Hits[i].Line < rep.Hits[j].Line
	})
	return rep
}

// TouchedRanges reads a unified diff into the OLD-side line ranges each path's
// change covers — "which lines of the file as it stood did this commit touch".
// `check closure` asks exactly that of a claimed closing commit, and the
// citation it compares against was written before the commit landed.
func TouchedRanges(diff string) map[string][][2]int {
	out := map[string][][2]int{}
	for path, fd := range parseUnified(diff) {
		if fd.deleted {
			// A deleted file was touched everywhere. The finding's line, whatever
			// it is, is inside the change.
			out[path] = append(out[path], [2]int{1, 1 << 30})
			continue
		}
		for _, h := range fd.hunks {
			if h.oldCount == 0 {
				// A pure insertion after old line N touches no old line. It is
				// recorded as the seam N..N+1 so a finding citing either side of
				// the insertion point counts as addressed.
				out[path] = append(out[path], [2]int{h.oldStart, h.oldStart + 1})
				continue
			}
			out[path] = append(out[path], [2]int{h.oldStart, h.oldStart + h.oldCount - 1})
		}
	}
	return out
}

func inRanges(rs [][2]int, line int) bool {
	for _, r := range rs {
		if line >= r[0] && line <= r[1] {
			return true
		}
	}
	return false
}

// PathsNamed returns the artifacts a finding's text names, resolved against the
// workspace's own artifact list, with the line each mention cites (0 for none).
//
// It resolves rather than pattern-matches for the same reason the citation
// checker does: a finding writes `decisions.md:120`, `artifacts/decisions.md`
// and `./artifacts/decisions.md` for one file, and a check that treated those
// as three paths would report a closure as unverified whenever the operator
// abbreviated.
func PathsNamed(text string, arts []string) map[string][]int {
	out := map[string][]int{}
	for _, tok := range strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == '\t' || r == ',' || r == ';' || r == '(' || r == ')' ||
			r == '[' || r == ']' || r == '`' || r == '"' || r == '\'' || r == '<' || r == '>'
	}) {
		tok = strings.TrimRight(tok, ".:")
		// The anchor comes off FIRST. A citation is written `file.md:12#rule`,
		// and cutting the line number before the anchor left `:12#rule` as the
		// number — which parses as no number, so the whole citation was read as
		// a path and matched nothing. The finding then counted as naming no
		// artifact at all, which is the shape this check exists to refuse.
		if i := strings.IndexByte(tok, '#'); i >= 0 {
			tok = tok[:i]
		}
		path, line := tok, 0
		if i := strings.LastIndexByte(tok, ':'); i > 0 {
			if n := atoiSafe(tok[i+1:]); n > 0 {
				path, line = tok[:i], n
			}
		}
		path = strings.TrimPrefix(path, "./")
		for _, a := range arts {
			if a == path || strings.HasSuffix(a, "/"+path) {
				out[a] = append(out[a], line)
				break
			}
		}
	}
	return out
}

func atoiSafe(s string) int {
	if s == "" {
		return 0
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}

// RangesCover is inRanges for callers outside this package — `wf check closure`
// asks whether a claimed closing commit's change covers the line a finding
// cites.
func RangesCover(rs [][2]int, line int) bool { return inRanges(rs, line) }

// sectionIDRe is the shape a gate artifact gives its own sections: a short
// uppercase tag and a number — `D9`, `R12`, `A3`. Findings cite those, not line
// numbers: measured on a live decision gate, 126 of 127 closures named no path
// at all while nearly every one named a section, so a closure check that could
// only resolve paths reached ONE finding out of 127 and said so honestly, which
// is not the same as being useful.
var sectionIDRe = regexp.MustCompile(`\b[A-Z]{1,3}[0-9]{1,3}\b`)

// SectionRanges maps each id that heads a markdown section to that section's
// line range, ending where the next heading of the same or higher level begins.
func SectionRanges(body []byte) map[string][2]int {
	lines := strings.Split(string(body), "\n")
	type open struct {
		ids   []string
		level int
		start int
	}
	out := map[string][2]int{}
	var stack []open
	closeTo := func(level, endLine int) {
		for len(stack) > 0 && stack[len(stack)-1].level >= level {
			o := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, id := range o.ids {
				out[id] = [2]int{o.start, endLine}
			}
		}
	}
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if !strings.HasPrefix(t, "#") {
			continue
		}
		level := 0
		for level < len(t) && t[level] == '#' {
			level++
		}
		closeTo(level, i) // the previous section ends on the line before this heading
		ids := sectionIDRe.FindAllString(t, -1)
		if len(ids) == 0 {
			continue
		}
		stack = append(stack, open{ids: ids, level: level, start: i + 1})
	}
	closeTo(1, len(lines))
	return out
}

// SectionsNamed returns the section ids a finding's text cites.
func SectionsNamed(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range sectionIDRe.FindAllString(text, -1) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
