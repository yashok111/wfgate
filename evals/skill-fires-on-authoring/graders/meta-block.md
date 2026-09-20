---
type: regex
pattern: 'export\s+const\s+meta\s*='
target: last_message
match: contains
weight: 1
---

Every script must open with the meta literal; a script without it does not run.
