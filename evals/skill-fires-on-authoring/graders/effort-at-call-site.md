---
type: regex
pattern: '\beffort:\s*\S'
target: last_message
match: contains
weight: 1
---

The authoring hook's reminder names model and effort in one breath — "every
agent() carries an explicit model AND an explicit effort" — so the pair is
checked as a pair. Measured on the third run: 5 occurrences with the plugin,
0 without.
