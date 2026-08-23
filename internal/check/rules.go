package check

import (
	"regexp"
	"sort"
	"strings"
)

// Rule markers close the gap dup structurally cannot reach. dup finds copies
// that DRIFTED — same sentence, reworded; copies that CONTRADICT in different
// words share no wording, and on the first live gate that class appeared eight
// times, every one found by a reviewer and none by dup. A `[R12]` marker makes
// the author's intent explicit — "these sites state ONE rule" — and turns the
// contradiction into a mechanical comparison: every site of an id must
// normalise to the same text. The price is markup discipline, which is why the
// check reports its population: zero markers is a corpus that opted out, not a
// corpus that passed.

var ruleMarkRe = regexp.MustCompile(`\[R(\d+)\]`)

// RuleSite is one marked statement of a rule.
type RuleSite struct {
	File string
	Line int
	Text string // as written, markers stripped, trimmed
	Norm string
	Ops  string // the comparison operators Norm erases; see opRe in dup.go
}

// RuleConflict is one rule id whose sites do not agree.
type RuleConflict struct {
	ID    string
	Sites []RuleSite
}

// CollectRuleSites scans the artifacts for `[RN]` markers and groups the
// statements that carry them by id. The marker itself is stripped before
// normalising, so the same rule marked in two files compares by its words.
func CollectRuleSites(root string, files []string) (map[string][]RuleSite, int, error) {
	// Built on the SAME unwrapping collector dup uses, not on a line scan. The
	// line scan compared only the physical line the marker sat on, so a rule
	// hard-wrapped across two lines was compared by its first line alone: two
	// copies whose continuations said "three times" and "five times" agreed.
	// A rule is a sentence, and it is compared as one — the lesson dup paid for
	// on P2e, applied to the check that needs it just as much.
	sts, err := CollectStatements(root, files, 0)
	if err != nil {
		return nil, 0, err
	}
	sites := map[string][]RuleSite{}
	markers := 0
	for _, st := range sts {
		ids := ruleMarkRe.FindAllStringSubmatch(st.Text, -1)
		if len(ids) == 0 {
			continue
		}
		markers += len(ids)
		text := trimMarkers(ruleMarkRe.ReplaceAllString(st.Text, ""))
		norm, _ := normalise(text)
		ops := opsOf(text)
		for _, m := range ids {
			sites["R"+m[1]] = append(sites["R"+m[1]], RuleSite{File: st.File, Line: st.Line, Text: text, Norm: norm, Ops: ops})
		}
	}
	return sites, markers, nil
}

// RuleConflicts returns every id whose sites normalise to more than one text.
// A single-site id is fine — a rule stated once needs no reconciliation — and
// identical sites are the markup working as intended.
func RuleConflicts(sites map[string][]RuleSite) []RuleConflict {
	var out []RuleConflict
	for id, ss := range sites {
		// The operator skeleton is part of the identity, because the normaliser
		// erases it: `[R1] retry_count == 3` and `[R1] retry_count != 3`
		// normalised to the same string, so the check whose entire purpose is
		// catching contradictions between marked copies reported none over a
		// direct one. See opRe in dup.go for why the set is four tokens wide.
		norms := map[string]bool{}
		for _, s := range ss {
			norms[s.Norm+"\x00"+s.Ops] = true
		}
		if len(norms) > 1 {
			out = append(out, RuleConflict{ID: id, Sites: ss})
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}

// Divergence renders each text focused on the FIRST place the set stops
// agreeing, instead of on its opening characters.
//
// Measured on P2f: five sites of one rule were reported as disagreeing and
// printed as five byte-identical lines, because the truncation cut at 110
// characters and the difference started past 130. The operator found it with
// `cat -A`. A report that shows only what its own subjects agree about is
// telling the reader the opposite of what it found.
//
// The comparison is by WORD, so a wrap or a run of spaces does not read as a
// difference, and a few words of the shared prefix are kept as an anchor — the
// divergence has to be locatable in the artifact, not merely visible.
func Divergence(texts []string, width int) []string {
	if len(texts) == 0 {
		return nil
	}
	words := make([][]string, len(texts))
	shortest := -1
	for i, t := range texts {
		words[i] = strings.Fields(t)
		if shortest < 0 || len(words[i]) < shortest {
			shortest = len(words[i])
		}
	}
	pre := 0
	for pre < shortest {
		same := true
		for i := 1; i < len(words); i++ {
			if words[i][pre] != words[0][pre] {
				same = false
				break
			}
		}
		if !same {
			break
		}
		pre++
	}
	// The common SUFFIX is trimmed too: two sites differing by one inserted
	// clause otherwise print the whole tail after it, and the tail is the part
	// they agree about.
	suf := 0
	for pre+suf < shortest {
		same := true
		for i := 1; i < len(words); i++ {
			if words[i][len(words[i])-1-suf] != words[0][len(words[0])-1-suf] {
				same = false
				break
			}
		}
		if !same {
			break
		}
		suf++
	}
	// Copies that agree ENTIRELY are not a conflict — `dup` prints exact
	// cross-file groups too, and rendering those as a divergence deleted the
	// sentence from its own report.
	same := true
	for _, ws := range words {
		if len(ws) != pre {
			same = false
			break
		}
	}
	if same {
		out := make([]string, len(texts))
		for i, t := range texts {
			out[i] = cut(strings.Join(strings.Fields(t), " "), width)
		}
		return out
	}
	const anchor = 4 // words of agreed text kept before the divergence
	out := make([]string, len(texts))
	for i, ws := range words {
		from := pre - anchor
		if from < 0 {
			from = 0
		}
		to := len(ws) - suf
		if to < pre {
			to = pre // a site that IS the common prefix plus suffix
		}
		mid := strings.Join(ws[from:to], " ")
		if to <= pre {
			// This site has nothing where the others differ — the difference is
			// that its text STOPS. Saying so is the finding, and the anchor
			// words before it are what makes the place locatable.
			mid = strings.TrimSpace(mid + " ⟨nothing here⟩")
		}
		mid = cut(mid, width)
		if from > 0 {
			mid = "…" + mid
		}
		if to < len(ws) {
			mid += "…"
		}
		out[i] = mid
	}
	return out
}

// cut truncates on a RUNE boundary. The corpus is English by workflow rule, but
// a quoted identifier or an em dash is not ASCII, and a byte cut through one
// prints a replacement character in the middle of the very text the reader was
// sent here to compare.
func cut(s string, width int) string {
	if len(s) <= width {
		return s
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width]) + "…"
}
