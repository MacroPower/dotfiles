---
name: humanize
description: Review prose for AI writing patterns and rewrite it to sound more natural.
---

## Your task

Launch the `humanizer` agent to review and rewrite the target prose.

Determining the target:
- If the user passed text or a file path as an argument, use that.
- If no argument was given, review all prose changes that were made in the session.

In the subagent prompt, include:
- The exact text to humanize, or the file path(s) to read
- Any tone or audience constraints the user has mentioned
- A request to either make edits or return rewritten text
