// Package wfgate embeds the operating manual into the binary, so the shipped
// tool is self-sufficient: `wf docs` serves the same references the
// workflow-gate skill carries, and `wf init` writes the gate-checks reference
// into every workspace — the version of the rules that briefed a gate's
// reviewers is pinned with the gate, the way the target's HEAD is. The skill's
// references/ directory is the single source; the binary embeds it rather than
// duplicating it.
package wfgate

import "embed"

// The whole skill directory, not only the references: SKILL.md is the front
// door a session reads first, and an installed copy of it that has drifted from
// this binary is the same defect as a drifted reference — measured, with the
// stale copy still describing a rule the manual had replaced.
//
//go:embed skills/workflow-gate
var Docs embed.FS
