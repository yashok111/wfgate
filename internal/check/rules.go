package check

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
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
	sites := map[string][]RuleSite{}
	markers := 0
	for _, rel := range files {
		f, err := os.Open(filepath.Join(root, rel))
		if err != nil {
			return nil, 0, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for n := 1; sc.Scan(); n++ {
			line := sc.Text()
			ids := ruleMarkRe.FindAllStringSubmatch(line, -1)
			if len(ids) == 0 {
				continue
			}
			markers += len(ids)
			text := trimMarkers(ruleMarkRe.ReplaceAllString(line, ""))
			norm, _ := normalise(text)
			for _, m := range ids {
				sites["R"+m[1]] = append(sites["R"+m[1]], RuleSite{File: rel, Line: n, Text: text, Norm: norm})
			}
		}
		cerr := sc.Err()
		_ = f.Close() // read-only; the scan error above is the one that matters
		if cerr != nil {
			return nil, 0, fmt.Errorf("%s: %w", rel, cerr)
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
		norms := map[string]bool{}
		for _, s := range ss {
			norms[s.Norm] = true
		}
		if len(norms) > 1 {
			out = append(out, RuleConflict{ID: id, Sites: ss})
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}
