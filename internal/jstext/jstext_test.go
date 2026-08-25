package jstext

import "testing"

func spanTexts(src string) []string {
	var out []string
	for _, sp := range ProseSpans(src) {
		out = append(out, src[sp.From:sp.To])
	}
	return out
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d span(s) %q, want %d %q", len(got), got, len(want), want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("span %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// The mocker A3 shape, one layer down: two literals on neighbouring lines are
// two different texts. Reading the file as prose made them one statement.
func TestProseSpans_codeIsNotProse(t *testing.T) {
	src := "const A = { owns: ['internal/api'], vector: 'reviewer' }\n" +
		"const P = \"The two routes must DETACH their 409 [R6].\" + tail\n"
	eq(t, spanTexts(src), []string{"internal/api", "reviewer", "The two routes must DETACH their 409 [R6]."})
	for _, sp := range ProseSpans(src) {
		if sp.Join {
			t.Fatalf("separate literals must not join: %+v", sp)
		}
	}
}

// A sentence with an interpolation in it is one sentence, and the string INSIDE
// the interpolation is an argument rather than prose: `${owns.join(', ')}` must
// not splice ", " into it, and the chunk after the hole must continue the chunk
// before it.
func TestProseSpans_templateChunksJoinAcrossAHole(t *testing.T) {
	src := "const P = `You own ${owns.join(', ')} and nothing else.`\n"
	eq(t, spanTexts(src), []string{"You own ", " and nothing else."})
	sp := ProseSpans(src)
	if sp[0].Join {
		t.Fatal("the first chunk continues nothing")
	}
	if !sp[1].Join {
		t.Fatal("the chunk after the hole continues the same sentence")
	}
}

// A template that OPENS with a hole emits nothing first, and the chunk after it
// must not be marked as continuing whatever span the scanner reported last.
func TestProseSpans_leadingHoleDoesNotJoinTheSpanBefore(t *testing.T) {
	src := "const A = 'a rule stated here'\nconst P = `${x} and the rest of it`\n"
	sp := ProseSpans(src)
	eq(t, spanTexts(src), []string{"a rule stated here", " and the rest of it"})
	if sp[1].Join {
		t.Fatal("a chunk after a LEADING hole starts its own text")
	}
}

// Wrapped prose inside a `//` block is what unwrapping exists for, so
// consecutive comment lines continue one another — but a comment that follows
// code does not continue the literal on the line above.
func TestProseSpans_commentLinesJoinOnlyEachOther(t *testing.T) {
	src := "// the composition is a pure function\n// called from buildRuntime, not a branch\nconst A = 'x'\n// a fresh thought\n"
	sp := ProseSpans(src)
	eq(t, spanTexts(src), []string{" the composition is a pure function", " called from buildRuntime, not a branch", "x", " a fresh thought"})
	if sp[0].Join {
		t.Fatal("the first comment line continues nothing")
	}
	if !sp[1].Join {
		t.Fatal("a wrapped comment line continues the one above it")
	}
	if sp[3].Join {
		t.Fatal("a comment after CODE starts its own text")
	}
}

// Two comments with CODE between them are two thoughts, and the previous span
// being a comment is not enough to make the second continue the first.
func TestProseSpans_commentsSeparatedByCodeDoNotJoin(t *testing.T) {
	src := "// the override store keeps one variant\nconst A = 1 // and this counts them\n"
	sp := ProseSpans(src)
	eq(t, spanTexts(src), []string{" the override store keeps one variant", " and this counts them"})
	if sp[1].Join {
		t.Fatal("a comment after code on the same line starts its own text")
	}
}

// A regex body is words to nobody. It is blanked for the code reader and never
// reaches the prose one — `/['"]/` used to open a string that ate the line.
func TestProseSpans_regexIsNeverProse(t *testing.T) {
	src := "const q = /['\"]/\nconst P = 'the real prose'\n"
	eq(t, spanTexts(src), []string{"the real prose"})
	if got, want := StripNonCode(src), "const q = /    /\nconst P = '              '\n"; got != want {
		t.Fatalf("code view: got %q, want %q", got, want)
	}
}

// Offsets are what a caller turns into line numbers, so a span must point at
// the line its text was written on.
func TestProseSpans_offsetsMapToLines(t *testing.T) {
	src := "const A = 1\nconst P = `\nfirst line of the prompt\nsecond line\n`\n"
	sp := ProseSpans(src)
	if len(sp) != 1 {
		t.Fatalf("want one template chunk, got %d", len(sp))
	}
	if l := LineAt(src, sp[0].From); l != 2 {
		t.Fatalf("the chunk starts on line 2 (just past the backtick), got %d", l)
	}
}
