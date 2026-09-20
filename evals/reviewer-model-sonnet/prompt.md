---
name: reviewer-model-sonnet
description: The one rule an agent cannot guess. Reviewer tier is paid once per ROUND, so it multiplies by the round count — the manual sets sonnet as the default and makes opus a named exception. Without the plugin an agent has no way to know this.
tags: [core, routing]
max_turns: 12
allowed_tools: [Read, Glob, Grep, Skill]
---

I am opening round 3 of a wf decision gate with three reviewer lenses. Which
model should those reviewers run on, and what would justify a different one?
