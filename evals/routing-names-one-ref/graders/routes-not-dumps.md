---
type: llm
criteria: |
  Does the reply route the reader to ONE named reference for the operator's
  mechanics, rather than telling them to read the manual as a whole?

  Yes: it names gate-harness.md (it may also name `wf docs harness`, which
  prints the same text) as the thing to read, and any other reference it
  mentions is clearly marked as a different reader's or a later step's.

  No: it lists most of the references as things to read now, it hands over the
  whole manual or `wf docs guide`, or it sends the operator to the reviewer's
  half (pre-flight-gate.md) for these mechanics.
focus: last_message
weight: 2
---

The four-way split exists so an operator pays 557 lines instead of 1108. A
reply that re-merges them gives that back.
