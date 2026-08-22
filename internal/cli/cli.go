// Package cli holds the argument handling every wfgate binary shares.
package cli

import "flag"

// ParseMixed lets flags and positional arguments appear in any order.
//
// flag.FlagSet stops at the first non-flag argument, so `wf init p2c -target X`
// silently drops -target and reports a usage error naming the very flag the
// caller passed — and `wfdry script.mjs --mode clean`, the form both documents
// prescribe, printed usage instead of running. Re-parsing the tail after each
// positional is the standard remedy, and it lives here rather than once per
// binary: a second copy of one decision is this project's most repeated defect.
func ParseMixed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return pos, nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}
