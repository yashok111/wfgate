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

// inflectionSuffixes is the closed set of English endings a fix may add while
// keeping the rule it states. It is a WHITELIST rather than a length bound
// because a bound alone reaches words that are not related at all: over the
// eleven gate workspaces on disk, "any added token up to three runes longer"
// paired 169 removals, and about a dozen of them were `within` under `with`,
// `someone` under `some`, `userid` under `user`, `formula` under `form`.
// Filtered through this list the population is 148 pairs and exactly ONE is
// wrong (`past` under `pasting`), which is the direction to be wrong in: a
// suppression the check gets wrong is a consumer it never reports.
var inflectionSuffixes = map[string]bool{
	"s": true, "es": true, "d": true, "ed": true, "r": true, "er": true,
	"n": true, "en": true, "st": true, "est": true, "ly": true, "ing": true,
}

// inflectedInto says whether long is short plus one of those endings, with the
// doubled final consonant English writes before some of them — `stop`/`stopped`,
// `bump`/`bumping`, `gzip`/`gzipped` are one word each and a plain suffix test
// misses all three.
func inflectedInto(short, long string) bool {
	if !strings.HasPrefix(long, short) || len(short) == 0 {
		return false
	}
	rest := long[len(short):]
	if inflectionSuffixes[rest] {
		return true
	}
	return len(rest) > 1 && rest[0] == short[len(short)-1] && inflectionSuffixes[rest[1:]]
}

// inflected says whether the fix put this token back in another form, for the
// pairs the fixed-prefix stem above cannot see: one side shorter than stemLen,
// which gets no stem at all.
//
// Measured need: on a live gate a fix rewrote "consumers work" as "consumer
// works", the bare token `work` vanished from the new text, and the check
// retired it and reported four untouched sentences that merely reuse a common
// English verb. Over that gate it produced 8 survivor reports across four rounds
// and every one was a false positive of this class.
func inflected(t string, added map[string]bool) bool {
	rt := []rune(t)
	if len(rt) < stemLen {
		// The fix wrote the LONGER form. A scan, because the ending cannot be
		// enumerated from the token.
		for a := range added {
			if len(a) > len(t) && inflectedInto(t, a) {
				return true
			}
		}
		return false
	}
	// The fix wrote the SHORTER one. Only a form BELOW stemLen escapes the stem
	// comparison, and the candidate list of `cand` already drops anything under
	// four runes, so there is exactly one length to ask about.
	if len(rt) <= stemLen+2 {
		short := string(rt[:4])
		return added[short] && inflectedInto(short, t)
	}
	return false
}

// stemOf cuts a token at stemLen RUNES, not bytes: the artifacts are English by
// workflow rule, but Cyrillic is legal as quoted data and a byte cut would
// split a rune and compare halves of characters.
func stemOf(t string) (string, bool) {
	n := 0
	for i := range t {
		n++
		if n > stemLen {
			return t[:i], true
		}
	}
	if n == stemLen {
		return t, true
	}
	return "", false
}

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
	// Mined is every token the range retired, before the corpus is consulted.
	// It is what separates "this range removed no rare vocabulary, so nothing
	// was swept" from "every word it retired is gone from the corpus too" — the
	// second is a COMPLETE sweep and the best outcome the check has, and it was
	// printed as `NOT A PASS, nothing was swept` because both left Retired
	// empty. Reporting the good case as the vacuous one is the same defect
	// class as the reverse, in the tool built to refuse it.
	Mined int
	// Swept is the retired vocabulary that survives nowhere in the untouched
	// corpus: the consumers this fix DID reach.
	Swept int
	// Touched is the number of sentences skipped because the range itself wrote
	// them. They are the fix, not its consumers.
	Touched int
	// Scanned is how many sentences the check actually compared.
	Scanned int
	// Reinflected is how many removed tokens were dropped because the fix put
	// the SAME word back in another form. It is printed for the reason `Common`
	// is: this check narrows its candidate population in two places, and a
	// narrowing nobody can see is one nobody can argue with. On a live gate the
	// short-token half of it was the whole of that gate's false positives.
	Reinflected int
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
		// A file this range CREATED needs no special case here: its single hunk
		// covers every line of it, so the loop below marks the whole document as
		// written by the range. The branch that said so explicitly survived every
		// mutation — an unprovable guard is a guard this project deletes, and the
		// lesson it carried lives in the parser, which used to drop added files
		// entirely (their words never entered the "added" set above, so a fix
		// that MOVED a rule into a new artifact retired its whole vocabulary).
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
		if st, ok := stemOf(t); ok {
			addedStem[st] = true
		}
	}
	rep := StaleReport{}
	var cand []string
	for t := range removed {
		// A token with no letter is a line number, a count or a version — the
		// numeric class `dup` already reports as NUMERIC, and here it is pure
		// noise. Short tokens are articles and operators the normaliser kept.
		if added[t] || len(t) < 4 || !hasLetter(t) {
			continue
		}
		if st, ok := stemOf(t); ok && addedStem[st] {
			rep.Reinflected++
			continue
		}
		// The same question for the pairs a five-rune prefix cannot answer.
		if inflected(t, added) {
			rep.Reinflected++
			continue
		}
		cand = append(cand, t)
	}
	sort.Strings(cand)

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
	rep.Mined = len(cand)
	for _, t := range cand {
		switch n := df[t]; {
		case n == 0:
			// Retired everywhere it appeared. Nothing left to re-read: this is
			// the fix that DID reach its consumers, and it is the good case —
			// counted, so the report can tell it apart from a range that mined
			// nothing at all.
			rep.Swept++
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
		// EVERY file the diff names gets a key, even one with no hunks at all: a
		// mode change touches a file without changing a line, and the FILE-level
		// question "did this commit open that document" is the whole of what
		// `check closure` can ask about a finding citing no section. A missing
		// key answered NO to it.
		if _, ok := out[path]; !ok {
			out[path] = nil
		}
		if fd.deleted || fd.isNew {
			// Deleted or created: the finding's line, whatever it is, is inside
			// the change. A commit that CREATED the artifact used to be reported
			// as not having touched it.
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

// PathsNamed returns the artifacts a finding's text names, with the line RANGE
// each mention cites ({0,0} for none), and separately the spellings that matched
// MORE than one artifact.
//
// A range and not a line, because a finding cites one: measured over the eleven
// gate workspaces on disk, 295 closure citations carry a `:120-134` span and not
// one of them uses anything but an ASCII hyphen. Reading only the first number
// asks whether the fix touched the FIRST line of a region the finding drew, and
// on a live gate all four of its line-level closure misses were exactly that —
// `p3h.mjs:454-459` against a commit whose hunks covered 450-453 and 455-457, so
// the one line the check asked about is the one unchanged line inside the span.
//
// It searches for the KNOWN artifact paths inside the text rather than cutting
// the text into whitespace tokens. Two reasons, both measured elsewhere in this
// tool: an artifact path may contain a space (the citation checker has a test
// for exactly that class), and a finding writes `decisions.md:120`,
// `artifacts/decisions.md` and `./artifacts/decisions.md` for one file.
//
// An ambiguous spelling resolves to NOTHING, the way `check cite` treats a path
// matching two tracked files. Returning the first match would report a closure
// as landed against a file nobody named — and a check that guesses is worse
// than one that says it could not tell.
func PathsNamed(text string, arts []string) (named map[string][][2]int, ambiguous []string) {
	named = map[string][][2]int{}
	// Every spelling that could name an artifact: the full path and every
	// suffix of it that begins at a path segment.
	spellings := map[string][]string{} // spelling -> artifacts it could mean
	for _, a := range arts {
		spellings[a] = append(spellings[a], a)
		for i := 0; i < len(a); i++ {
			if a[i] == '/' {
				spellings[a[i+1:]] = append(spellings[a[i+1:]], a)
			}
		}
	}
	seenAmb := map[string]bool{}
	for sp, owners := range spellings {
		for _, at := range indexAll(text, sp) {
			// A spelling is only a mention when it stands on its own: the
			// artifact `notes.md` must not match inside `release-notes.md`.
			if at > 0 && !isBoundary(text[at-1]) {
				continue
			}
			if len(owners) > 1 {
				if !seenAmb[sp] {
					seenAmb[sp] = true
					ambiguous = append(ambiguous, sp)
				}
				continue
			}
			named[owners[0]] = append(named[owners[0]], rangeAfter(text, at+len(sp)))
		}
	}
	for a := range named {
		named[a] = uniqueRanges(named[a])
	}
	sort.Strings(ambiguous)
	return named, ambiguous
}

// rangeAfter reads the `:120` or `:120-134` a citation may carry, ignoring an
// `#anchor` between the path and the number. The anchor is cut FIRST: reading the
// line number before it left `:12#rule` as the number, which parses as none, so
// the whole citation was read as a path and matched nothing.
//
// An INVERTED span (`:459-454`) keeps the start alone rather than being widened
// to cover both readings: `check cite` reports that shape as an error, and a
// check that silently repaired it here would answer a question about a region
// the author did not write. A trailing hyphen with no second number is the same
// case — `:120-` is a line, not a span.
func rangeAfter(text string, i int) [2]int {
	if i < len(text) && text[i] == '#' {
		for i < len(text) && !isBoundary(text[i]) && text[i] != ':' {
			i++
		}
	}
	if i >= len(text) || text[i] != ':' {
		return [2]int{}
	}
	j := i + 1
	for j < len(text) && text[j] >= '0' && text[j] <= '9' {
		j++
	}
	start := atoiSafe(text[i+1 : j])
	if start == 0 {
		return [2]int{}
	}
	if j >= len(text) || text[j] != '-' {
		return [2]int{start, start}
	}
	k := j + 1
	for k < len(text) && text[k] >= '0' && text[k] <= '9' {
		k++
	}
	end := atoiSafe(text[j+1 : k])
	if end < start {
		return [2]int{start, start}
	}
	return [2]int{start, end}
}

func indexAll(hay, needle string) []int {
	var out []int
	for off := 0; ; {
		i := strings.Index(hay[off:], needle)
		if i < 0 {
			return out
		}
		out = append(out, off+i)
		off += i + len(needle)
	}
}

// isBoundary is what may precede a path mention: anything that is not a
// character a path segment can start with.
func isBoundary(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return false
	case c == '-' || c == '_' || c == '.' || c == '/':
		return false
	}
	return true
}

func uniqueRanges(xs [][2]int) [][2]int {
	seen := map[[2]int]bool{}
	var out [][2]int
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i][0] != out[j][0] {
			return out[i][0] < out[j][0]
		}
		return out[i][1] < out[j][1]
	})
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
//
// Fenced code is skipped, a heading needs a space after its hashes, and seven
// hashes are not a heading: a decision document is full of shell and Go blocks,
// and `# D9 example` inside one would head a phantom section that the closure
// check then measures a commit against. An id heading two real sections gets
// the UNION of them — a finding citing it is closed by a fix reaching either,
// which is the same "any" rule the closure check applies to the ids themselves.
func SectionRanges(body []byte) map[string][2]int {
	lines := strings.Split(string(body), "\n")
	type open struct {
		ids   []string
		level int
		start int
	}
	out := map[string][2]int{}
	put := func(id string, r [2]int) {
		if old, ok := out[id]; ok {
			if old[0] < r[0] {
				r[0] = old[0]
			}
			if old[1] > r[1] {
				r[1] = old[1]
			}
		}
		out[id] = r
	}
	var stack []open
	closeTo := func(level, endLine int) {
		for len(stack) > 0 && stack[len(stack)-1].level >= level {
			o := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, id := range o.ids {
				put(id, [2]int{o.start, endLine})
			}
		}
	}
	fence := ""
	for i, l := range lines {
		t := strings.TrimSpace(l)
		// A fence opens with three or more backticks or tildes and closes with
		// the same character. Everything between belongs to the code.
		if f := fenceOf(t); f != "" {
			switch {
			case fence == "":
				fence = f
			case strings.HasPrefix(f, fence[:1]):
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		// An indented code block is code too, and a heading is never indented
		// by four spaces.
		if strings.HasPrefix(l, "    ") || strings.HasPrefix(l, "\t") {
			continue
		}
		if !strings.HasPrefix(t, "#") {
			continue
		}
		level := 0
		for level < len(t) && t[level] == '#' {
			level++
		}
		// `#not-a-heading` is a fragment link or a shell comment, and `#######`
		// is past the six levels markdown has.
		if level > 6 || level >= len(t) || t[level] != ' ' {
			continue
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

// fenceOf returns the fence marker a line opens or closes with, or "".
func fenceOf(t string) string {
	for _, c := range []string{"```", "~~~"} {
		if strings.HasPrefix(t, c) {
			return c
		}
	}
	return ""
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
