// Package wfgate embeds the operating manual into the binary, so the shipped
// tool is self-sufficient: `wf docs` serves the same references the
// workflow-gate skill carries, and `wf init` writes the gate-checks reference
// into every workspace — the version of the rules that briefed a gate's
// reviewers is pinned with the gate, the way the target's HEAD is. The skill's
// references/ directory is the single source; the binary embeds it rather than
// duplicating it.
package wfgate

import "embed"

//go:embed skill/workflow-gate/references
var Docs embed.FS
