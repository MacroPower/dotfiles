---
name: commit-push-pr
description: Commit, push, and open a PR
disable-model-invocation: true
allowed-tools:
  - Bash(git checkout --branch:*)
  - Bash(git add:*)
  - Bash(git status:*)
  - Bash(git push:*)
  - Bash(git commit:*)
  - Bash(gh pr create:*)
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

Based on the above changes:

1. Create a new branch if on main
2. Create a single git commit with an appropriate message
3. Push the branch to origin
4. Create a pull request using `gh pr create`
5. You have the capability to call multiple tools in a single response. You MUST do all of the above in a single message. Do not use any other tools or do anything else. Do not send any other text or messages besides these tool calls.
