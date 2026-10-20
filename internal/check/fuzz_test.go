package check

import (
	"strings"
	"testing"
	"unicode"
)

// An anchor that matches is in the text; a text matches itself whole; a token
// standing alone between spaces is always a whole identifier; and the word an
// anchor is reported as landing in holds it and is longer.
func FuzzAnchorIn(f *testing.F) {
	f.Add("MOCKER_CHECKPOINT_RETENTION", "RETENTION")
	f.Add("useGetOperationOverride", "useGetOperation")
	f.Add("naïve", "na")
	f.Add("userIDs", "ID")
	f.Add("1e9", "e")
	f.Fuzz(func(t *testing.T, text, tok string) {
		if AnchorIn(text, tok) && !strings.Contains(text, tok) {
			t.Fatalf("AnchorIn(%q, %q) without the substring", text, tok)
		}
		if tok != "" && !AnchorIn(tok, tok) {
			t.Fatalf("a text is not its own anchor: %q", tok)
		}
		if tok != "" && !strings.ContainsAny(tok, " \t\n") && !AnchorIn(" "+tok+" ", tok) {
			t.Fatalf("a token alone between spaces is not whole: %q", tok)
		}
		if w := enclosing(text, tok); w != "" && (!strings.Contains(w, tok) || len(w) <= len(tok)) {
			t.Fatalf("enclosing(%q, %q) = %q", text, tok, w)
		}
		// The rule's two fixed answers: a token between underscores is a PART
		// (`#RETENTION` on `MOCKER_CHECKPOINT_RETENTION`), and a run of lower-case
		// letters inside a longer lower-case word is a fragment.
		if tok != "" && strings.Trim(tok, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") == "" {
			if !AnchorIn("X_"+tok+"_Y", tok) {
				t.Fatalf("a token between underscores is a part: %q", tok)
			}
			if strings.ToLower(tok) == tok && strings.Trim(tok, "abcdefghijklmnopqrstuvwxyz") == "" && AnchorIn("x"+tok+"y", tok) {
				t.Fatalf("a lower-case run inside a word is a fragment: %q", tok)
			}
		}
	})
}

// The normaliser is a fixpoint — the joined form normalises to itself — and
// its unique list is sorted, lowercase, and exactly the token set.
func FuzzNormalise(f *testing.F) {
	f.Add("The `retry_count` == 3, NOT 3!")
	f.Add("Ünïcode ŚTRING")
	f.Fuzz(func(t *testing.T, s string) {
		joined, uniq := Normalise(s)
		// Every maximal run of letters and digits in the input is one token
		// out — a normaliser that dropped text would be a fixpoint too.
		runs, in := 0, false
		for _, r := range s {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				if !in {
					runs++
				}
				in = true
			} else {
				in = false
			}
		}
		if n := len(strings.Fields(joined)); n != runs {
			t.Fatalf("%d runs in %q, %d tokens in %q", runs, s, n, joined)
		}
		if j2, _ := Normalise(joined); j2 != joined {
			t.Fatalf("not a fixpoint: %q -> %q -> %q", s, joined, j2)
		}
		set := map[string]bool{}
		for _, tok := range strings.Fields(joined) {
			set[tok] = true
		}
		if len(set) != len(uniq) {
			t.Fatalf("%d distinct tokens, %d unique: %q", len(set), len(uniq), s)
		}
		for i, u := range uniq {
			if !set[u] || (i > 0 && uniq[i-1] >= u) {
				t.Fatalf("uniq %v for %q", uniq, s)
			}
			for _, r := range u {
				// Lowered, not "not upper": U+03D3 is upper-case and has no
				// lower-case mapping, which the fuzzer found in a minute.
				if unicode.ToLower(r) != r || (!unicode.IsLetter(r) && !unicode.IsDigit(r)) {
					t.Fatalf("token %q for %q", u, s)
				}
			}
		}
	})
}
