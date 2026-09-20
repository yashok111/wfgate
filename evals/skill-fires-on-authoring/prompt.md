---
name: skill-fires-on-authoring
description: The manual must be loaded BEFORE the first line of an orchestration script is written. This case asks for a script and checks the skill fired and its two hard rules survived into the output.
tags: [core, authoring]
max_turns: 20
timeout_seconds: 900
allowed_tools: [Read, Glob, Grep, Skill]
---

I want to review four files of a repository along three dimensions each, with
every finding verified by a second pass. Write me the Workflow orchestration
script for that.

Print the finished script in your reply. Do not write it to a file.
