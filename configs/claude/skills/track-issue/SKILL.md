---
name: track-issue
description: >-
  Load when you notice a bug or issue outside the current task, when the user
  defers one, or when you hit a notable issue with Claude Code or its config,
  to file it as an aven task.
---

# Track Issue

File an issue that falls outside the current task as an aven task, then
return to the task. The task keeps the issue on record, so the session can
stay narrow.

Track defects, failing tests, wrong docs, and issues that the user defers.
Skip issues that the current task fixes, and skip style preferences. Record
what you already saw, and do not investigate further or design a fix.

## Project

File the issue in the current project, which the `Project:` line of the
session-start aven context names. File friction with Claude Code or its
config in the `dotfiles` project with `--label self-improve`. The config
covers skills, agents, hooks, permission rules, MCP servers, and CLAUDE.md.
Pass `--project` to every `aven` command, because aven otherwise infers the
project from the working directory.

## Duplicates

Search first with `aven search --project <key> "<terms>"`. If an open task
matches, add the new evidence with `aven note <ref>` instead of filing. If a
done task matches, file a new task and link the two with
`aven related add <new-ref> <old-ref>`. If a canceled task matches, do not
refile, and mention its ref.

## Filing

```sh
aven add --project <key> "<title>" --description-stdin <<'EOF'
## Observed
<what happened, how often, and where: a file and line, a command, or a page>

## Expected
<what should happen>

## Reproduction
<the minimal steps or context>

## Evidence
<verbatim errors, output, code, or the user's words, with secrets redacted>
EOF
```

Title the task in sentence case after the symptom, not the fix, as in
`Retry loop ignores the context deadline`. Leave the status and priority at
their defaults for the user to triage.

## Report

Give each ref and title on one line in your final message. In a fork, that
line is the whole final report.
