---
name: self-improve
description: >-
  Retrospective on the current session's friction (broken tool calls,
  repeated prompts, wasted effort).
---

# Self-Improve

Review this session for friction with Claude Code or its config, and file
each systemic problem with the `track-issue` skill. Systemic problems recur
in later sessions until someone changes the config. Skip
one-off slips of your own. Two real tasks beat ten speculative ones, and
each task points at something that happened this session.

Look for these kinds of friction:

- Tool calls that failed, or that a hook, a permission rule, or the fetch
  allowlist denied.
- A safe command that prompted for permission more than once.
- A CLAUDE.md line, skill, or agent that misled you or contradicted the
  user.
- A fact that you found by experiment because no doc stated it.
- Manual multi-step work that a skill, script, or Taskfile target can
  capture.
- Context bloat that guidance can prevent, such as a whole file read for
  one section or an unfiltered log. Bloat triggers earlier compaction, which
  loses detail.
- A user correction of how you worked.

Work from what is in context, and read the raw transcript only when the
user points you at one.

End by telling the user which tasks you filed or added notes to, with each
ref and title. If the session had no systemic friction, say that you filed
nothing.
