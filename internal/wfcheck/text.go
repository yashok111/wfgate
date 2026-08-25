package wfcheck

import "git.sumka.site/yakov/wfgate/internal/jstext"

// The textual backstop of the inventory check is the one place this package
// still reads characters instead of the parsed program, and it exists precisely
// because the parser might be wrong. That makes its input worth preparing: a
// call is CODE, and a `agent(` inside a comment or a string is neither a call
// nor evidence that the parser missed one.
//
// Measured on P2f, where all three shapes fired on real scripts:
//   - `// one agent() call, no loop, no fan-out`        — a comment
//   - `log('… ' + n + '/2 agent(s) came back')`         — a string
//   - `async function spawn(prompt, opts)`              — the declaration
//
// The first two are handled by the scanner, the third by the caller (a
// wrapper's declaration line is not one of its call sites). All three produced
// the same output: an ERROR nobody could clear, on a script the checker had
// read correctly.
//
// The scanner itself lives in `internal/jstext`, because `internal/check` needs
// the same split from the other side — the PROSE of a script, whose prompts
// restate the document's decisions and must be compared against them.
func stripNonCode(src string) string { return jstext.StripNonCode(src) }

// lineAt returns the 1-based line an offset falls on.
func lineAt(s string, off int) int { return jstext.LineAt(s, off) }
