---
name: commit
description: Create a git commit
allowed-tools:
  - Bash(git add:*)
  - Bash(git status:*)
  - Bash(git commit:*)
---

## Context

- Current git status: !`git status`
- Current git diff (staged and unstaged changes): !`git diff HEAD`
- Current branch: !`git branch --show-current`
- Recent commits: !`git log --oneline -10`

## Conventional Commit Reference

If the codebase follows conventional commits (visible in recent commits above), the commit type must follow these rules:

- build: changes that affect the build system or external dependencies
- ci: changes to our CI configuration files and scripts
- docs: documentation only changes
- feat: a new feature
- fix: a bug fix
- perf: a code change that improves performance
- refactor: a code change that neither fixes a bug nor adds a feature
- style: changes that do not affect the meaning of the code (e.g. white-space, formatting)
- test: adding missing tests or correcting existing tests
- chore: changes that affect auxiliary tools (e.g. linter version/configs)

## Your task

Based on the above changes, create a single git commit.

You have the capability to call multiple tools in a single response. Stage and create the commit using a single message. Do not use any other tools or do anything else. Do not send any other text or messages besides these tool calls.
