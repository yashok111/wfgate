package check

import (
	"regexp"
	"sort"
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
