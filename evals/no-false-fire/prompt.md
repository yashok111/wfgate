---
name: no-false-fire
description: Precision guard. The skill's description is written to fire hard, which is how a MANDATORY skill starts firing on everything and stops being read. This case is in the plugin's own subject area and has nothing to do with orchestration; loading the manual here is a cost with no return.
tags: [core, precision]
max_turns: 8
allowed_tools: [Read, Glob, Grep, Skill]
---

A command-line tool of mine shells out to git and parses the diff it gets back.
Why would it pass an explicit diff algorithm on every invocation instead of
letting the user's own git config decide? Short answer, a paragraph.
