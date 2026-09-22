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
  - Bash(gh pr create:*)
  - Bash(prose-lint:*)
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

1. Compose the commit message and the PR body, then lint each one. The
   heredoc keeps `prose-lint` as the command prefix so the allow rule
   matches without a pipeline:

   ```sh
   prose-lint --commit <<'EOF'
   type(scope): summary

   Body of the message.
   EOF
   ```

   Fix every finding and lint again until both commands exit 0 with no
   output. Send the lint calls in their own message, since their findings
   change the text you commit.
2. Once the text is clean, do all of the following in a single message.
   You have the capability to call multiple tools in a single response.
   Do not use any other tools or do anything else, and do not send any
   other text or messages besides these tool calls.
   1. Create a new branch if on main.
   2. Create a single git commit with the clean message.
   3. Push the branch to origin with `mcp__git__git_push`, passing the
      repository root's absolute path as `repo` and setting `set_upstream`.
      If the git MCP server rejects the repository path, ask the user to
      push the branch instead.
   4. Create a pull request using `gh pr create` with the clean body.
