---
type: tool_used
tool: Skill
input_match: workflow-gate
max: 0
weight: 2
---

Scored in BOTH arms on purpose. The without arm cannot load the skill, so it
passes trivially — which makes a NEGATIVE delta on this case the signal: it
means the plugin fires where it has nothing to offer.
