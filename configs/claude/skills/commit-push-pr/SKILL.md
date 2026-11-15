---
name: commit-push-pr
description: Commit, push, and open a PR
disable-model-invocation: true
allowed-tools:
  - Bash(git checkout --branch:*)
  - Bash(git add:*)
  - Bash(git status:*)
  - mcp__git__git_push
  - Bash(git commit:*)
  - mcp__github__create_pull_request
---

## Context

- Current git status: !`git status`
- Current git diff (staged and unstaged changes): !`git diff HEAD`
- Current branch: !`git branch --show-current`
- Recent commits: !`git log --oneline -10`
- Remotes: !`git remote -v`
- Default branch of origin: !`git branch --remotes --list origin/HEAD origin/main origin/master`

## Conventional Commit Reference

If the codebase follows conventional commits (visible in recent commits above), the commit type must follow these rules:

- build: changes that affect the build system or external dependencies
- ci: changes to our CI configuration files and scripts
- docs: changes that touch only documentation
- feat: a new feature
- fix: a bug fix
- perf: a code change that improves performance
- refactor: a code change that neither fixes a bug nor adds a feature
- style: changes that do not affect the meaning of the code (such as white-space or formatting)
- test: adding missing tests or correcting existing tests
- chore: changes that affect auxiliary tools (such as linter version/configs)

## Your task

Based on the above changes:

1. If on `main`, `master`, or the default branch of origin, create a new branch named `<type>/<name>`, such as `fix/retry-timeout`. Choose `<type>` from the Conventional Commit Reference, whether or not the codebase follows conventional commits. The push and the draft pull request run without a prompt only from a branch with a name of that form.
2. Create a single git commit with an appropriate message. When the commit message, or the title or body of the pull request, breaks a prose rule, a hook denies the call with its findings. Rewrite the text and make the call again.
3. Push the branch to origin with `mcp__git__git_push`, passing the repository root's absolute path as `repo` and setting `set_upstream`. If the git MCP server rejects the repository path, ask the user to push the branch instead.
4. Open a draft pull request with `mcp__github__create_pull_request`. Set `draft` to true and `head` to the name of the branch. Take `owner` and `repo` from the URL of the origin remote, and set `base` to the default branch of origin.
5. You have the capability to call multiple tools in a single response. You MUST do all of the above in a single message. Do not use any other tools or do anything else. Do not send any other text or messages besides these tool calls.
