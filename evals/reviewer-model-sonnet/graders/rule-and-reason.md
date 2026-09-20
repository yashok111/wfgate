---
type: llm
criteria: |
  Does the reply state BOTH of these?

  1. Gate reviewers run on sonnet by default.
  2. Opus is a per-round exception that has to name the specific miss it
     guards against (and, where mentioned, is banked in the roster).

  A reply that recommends opus for reviewers, that presents the choice as a
  judgement call with no default, or that gives sonnet without the exception
  rule, is a no.
focus: last_message
weight: 3
---

The rule's value is the reason: a reviewer's tier is paid once per ROUND, so it
multiplies by the round count. That is what an operator's quota runs out on.
