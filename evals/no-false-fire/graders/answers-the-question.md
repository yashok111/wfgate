---
type: llm
criteria: |
  Does the reply actually answer the question asked — that the diff algorithm
  is an input to the output, so leaving it to the caller's config makes it an
  undeclared one, and results measured under one algorithm are not comparable
  with results measured under another?

  Wording is free. A reply that changes the subject, that refuses, or that
  answers about orchestration scripts or review gates instead, is a no.
focus: last_message
weight: 1
---

A precision case still has to check the answer was given: a refusal would
otherwise score full marks for not loading the skill.
