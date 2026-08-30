package check

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The corpus probe behind the inflection numbers this repository banks. It is
// NOT a bar: it reads the gate workspaces on this box, which no CI has, so it
// skips unless WF_PROBE is set. It exists because a measurement nobody can
// re-derive is held to a lower standard than this project holds a reviewer's
// row to — the first reading of these pairs was written down without its
// command and did not reproduce.
//
//	WF_PROBE=1 go test ./internal/check/ -run TestCorpusInflectionPairs -v
//
// It prints, over every `gate-r*` range of every unpruned workspace: the pairs
// the SHIPPED predicate suppresses, the pairs a bare length bound would, and
// the ones only the bound matches — which is the population the whitelist
// exists to refuse, and where its wrong pairs are read off.
func TestCorpusInflectionPairs(t *testing.T) {
	if os.Getenv("WF_PROBE") == "" {
		t.Skip("probe")
	}
	home, _ := os.UserHomeDir()
	var ws []string
	for _, d := range []string{"gates/p2d", "gates/p2e", "gates/p2f"} {
		ws = append(ws, filepath.Join(home, d))
	}
	for _, d := range []string{"a3-cas", "p3a-resources", "p3b-resources", "p3c-ref-recipe", "p3d-datasnap", "p3e-nested", "p3g-deep-nesting", "p3h-basepath"} {
		ws = append(ws, filepath.Join(home, "projects/gates/mocker-"+d))
	}
	white := map[string]bool{} // suppressed by the shipped whitelist predicate
	bound := map[string]bool{} // suppressed by a bare length bound
	ranges := 0
	for _, w := range ws {
		tags, _ := exec.Command("git", "-C", w, "tag").Output()
		for _, tag := range strings.Fields(string(tags)) {
			if !strings.HasPrefix(tag, "gate-r") {
				continue
			}
			raw, err := exec.Command("git", "-C", w, "-c", "core.quotePath=false", "diff", "--no-renames",
				"--no-ext-diff", "--unified=0", "--src-prefix=a/", "--dst-prefix=b/", tag, "HEAD", "--", "artifacts").Output()
			if err != nil {
				continue
			}
			ranges++
			files := parseUnified(string(raw))
			removed, added := map[string]bool{}, map[string]bool{}
			for _, fd := range files {
				for _, l := range fd.removed {
					_, toks := normalise(l)
					for _, tk := range toks {
						removed[tk] = true
					}
				}
				for _, l := range fd.added {
					_, toks := normalise(l)
					for _, tk := range toks {
						added[tk] = true
					}
				}
			}
			addedStem := map[string]bool{}
			for a := range added {
				if st, ok := stemOf(a); ok {
					addedStem[st] = true
				}
			}
			for tk := range removed {
				if added[tk] || len(tk) < 4 || !hasLetter(tk) {
					continue
				}
				if st, ok := stemOf(tk); ok && addedStem[st] {
					continue
				}
				// The unit is the removed TOKEN, not the pair: both predicates
				// stop at the first partner they find and they need not pick the
				// same one, so a pair-wise difference reports disagreements that
				// are only a choice of partner.
				if inflected(tk, added) {
					white[tk] = true
				}
				if boundOnly(tk, added) {
					bound[tk] = true
				}
			}
		}
	}
	keys := func(m map[string]bool) []string {
		var o []string
		for k := range m {
			o = append(o, k)
		}
		sort.Strings(o)
		return o
	}
	fmt.Printf("ranges=%d  whitelist token(s)=%d  length-bound token(s)=%d\n", ranges, len(white), len(bound))
	var only []string
	for _, k := range keys(bound) {
		if !white[k] {
			only = append(only, k)
		}
	}
	var wonly []string
	for _, k := range keys(white) {
		if !bound[k] {
			wonly = append(wonly, k)
		}
	}
	fmt.Printf("bound-only (%d): %s\n", len(only), strings.Join(only, " | "))
	fmt.Printf("whitelist-only (%d): %s\n", len(wonly), strings.Join(wonly, " | "))
	fmt.Printf("whitelist: %s\n", strings.Join(keys(white), " | "))
}

// boundOnly is the rejected design: any added token within three runes that has
// this one as a prefix, in either direction, with no test of what the ending is.
func boundOnly(t string, added map[string]bool) bool {
	rt := []rune(t)
	if len(rt) < stemLen {
		for a := range added {
			ra := []rune(a)
			if len(ra) > len(rt) && len(ra)-len(rt) <= 3 && string(ra[:len(rt)]) == t {
				return true
			}
		}
		return false
	}
	for n := 4; n >= 3; n-- {
		if len(rt) <= n || len(rt)-n > 3 {
			continue
		}
		if added[string(rt[:n])] {
			return true
		}
	}
	return false
}
