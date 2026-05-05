---
name: review-implementation
description: Review code changes against the plan they implemented.
---

## Your task

Launch the `implementation-reviewer` agent to review the changes.

In the prompt, include:
- The plan file path
- The base SHA
- Any specific concerns the user mentioned

If `implementation-reviewer` returns issues, address them. Then, run the agent again.
Continue until you get LGTM.
