---
type: regex
pattern: '\bmodel:\s*\S'
target: last_message
match: contains
weight: 2
---

WHAT THIS CHECKS, exactly: that the script names a model at all. It does NOT
check that every agent call carries one — read the next paragraph before
treating a pass as that claim.

An LLM judge was tried for the stronger claim and removed after three runs. It
never once scored a correct script as passing: the manual prescribes routing
every call through a `spawn` wrapper, so the only literal `agent(` line in a
correct script has no model on it, and the four call sites that do are one
resolution step away. Votes were 3-0 FAIL, then 2-1 FAIL after the criteria
were rewritten to spell the wrapper out. Resolving a wrapper to its call sites
is static analysis, and this repository already owns the parser that does it —
`wfcheck`, over `internal/wfscript`, which exists for exactly "options resolved
through consts, spreads and factories". A judge reading a transcript is the
wrong instrument, and a grader that fails correct output is worse than a
weaker one that is honest about its reach.

What makes the weaker check worth keeping is that it separates the arms on the
measured evidence: 8 and 9 occurrences in the two WITH runs, 0 and 0 in the two
WITHOUT runs, whose scripts called agent() directly with no model anywhere.
