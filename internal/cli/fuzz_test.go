package cli

import (
	"flag"
	"io"
	"slices"
	"strings"
	"testing"
)

// Flags and positionals in any order: every positional returned is one of the
// arguments, in the order given, and a parse that fails names its error
// instead of panicking.
func FuzzParseMixed(f *testing.F) {
	f.Add("init p2c -target X")
	f.Add("a -n 3 b -v c")
	f.Add("-- -n x")
	f.Add("-n")
	f.Add("a b c d") // flag-free: every argument is a positional, in order
	f.Fuzz(func(t *testing.T, line string) {
		args := strings.Fields(line)
		fs := flag.NewFlagSet("x", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		fs.String("target", "", "")
		fs.Int("n", 0, "")
		fs.Bool("v", false, "")
		pos, err := ParseMixed(fs, args)
		if err != nil {
			return
		}
		flagShaped := false
		for _, a := range args {
			if strings.HasPrefix(a, "-") {
				flagShaped = true
			}
		}
		if !flagShaped && !slices.Equal(pos, args) {
			t.Fatalf("no flags, yet %v != %v", pos, args)
		}
		k := 0
		for _, p := range pos {
			for k < len(args) && args[k] != p {
				k++
			}
			if k == len(args) {
				t.Fatalf("positional %q not in %q in order: %v", p, args, pos)
			}
			k++
		}
	})
}
