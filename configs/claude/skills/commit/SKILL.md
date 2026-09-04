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

- feat: a new feature
- fix: a bug fix
- refactor: a code change that neither fixes a bug nor adds a feature
- perf: a code change that improves performance
- style: changes that do not affect the meaning of the code (e.g. white-space, formatting)
- test: adding missing tests or correcting existing tests
- build: changes that affect the build system or external dependencies
- ci: changes to our CI configuration files and scripts
- docs: documentation only changes
- chore: changes that affect auxiliary tools (e.g. linter version/configs)

## Your task

Commit the above changes as one commit per logical change, never as one commit for the whole diff.

A logical change is one thing a reviewer can accept or revert on its own. Two edits belong in one commit only when neither makes sense without the other. Files are not the unit; a diff across three files can be one change, and a diff in one file can be three.

For each change, in order:

1. Stage only the paths that belong to it with `git add <path>...`.
2. Commit it with its own message.
3. Move to the next change. Do not stage the next change until the previous commit exists.

When a single file carries hunks from two different changes, stop and invoke `/git-surgeon` through the Skill tool to stage only the hunks for the current change. `git add <path>` stages the whole file and cannot do this. Do not fold both changes into one commit to avoid the extra step.

Each commit subject must be 70 characters or less. The description must wrap at 70 characters. Use plain ASCII characters only. Keep the description short and to the point.
