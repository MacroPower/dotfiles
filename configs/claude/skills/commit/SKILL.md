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

If the codebase follows conventional commits (visible in recent commits above),
each subject takes the form `type(scope): summary`.

### Type

The type describes what the change does to the codebase:

- feat: adds behavior a user or caller can observe
- fix: corrects behavior that was wrong
- refactor: changes code without changing behavior
- perf: changes code to make it faster or use fewer resources
- style: changes formatting or whitespace only, with no change to meaning
- test: adds or corrects tests, and nothing else
- build: changes the build system or external dependencies (lockfiles, package
  manifests, Dockerfiles)
- ci: changes CI configuration or scripts
- docs: changes documentation only
- chore: changes auxiliary tooling that is neither code, tests, build, nor CI
  (linter config, editorconfig, gitignore, editor settings)

Pick the type from what the diff touches, in this order:

1. If every changed file is a test, use `test`. A repair to a broken test is
   still `test`.
2. If every changed file is documentation, use `docs`.
3. If the diff touches CI config, build files, or dependencies and nothing else,
   use `ci` or `build`.
4. Otherwise the diff touches code, so choose among `feat`, `fix`, `refactor`,
   `perf`, and `style` based on its effect on behavior.

`chore` is the last resort. It fits only when none of the rules above match,
so the diff touches no source, test, docs, build, or CI file.

### Scope

The scope is OPTIONAL. It names the area of the codebase the change belongs
to, so a reader scanning `git log` can tell which part moved without opening
the commit. It is usually the package, module, or component name.

- Derive it from the nearest meaningful directory or package.
  `foobar/tests/test_foo.py` gives `foobar`, `home/claude.nix` gives `claude`,
  and `src/api/handlers/user.go` gives `api`.
- Never include slashes or file extensions. You MAY use dashes or underscores
  when the scope calls for them.
- IMPORTANT: You MUST omit the scope when the change spans more than one area.

### Breaking Changes

A change is breaking when someone who upgrades must edit their own code, config,
or workflow to keep working. The type stays whatever the diff earns (`feat`,
`fix`, `refactor`, and so on). The breaking marker sits on top of that type.

Mark a breaking change in both places, because release tooling reads the subject
to bump the major version and reads the footer to write the release notes:

1. Put `!` directly before the colon in the subject: `type(scope)!: summary` or
   `type!: summary`.
2. End the message with a `BREAKING CHANGE: <what changed and what the reader
   must do>` footer, with a blank line between the body and the footer. Spell
   the token in uppercase, because tooling matches it literally.

Never mark a change as breaking while the project is on a v0 release (the latest
tag starts with `0.`) or while its README, changelog, or package manifest labels
it UNSTABLE, ALPHA, or pre-release. Semantic versioning makes no compatibility
promise before v1, and a `!` or `BREAKING CHANGE` footer would push release
tooling to cut a v1 the project has not chosen to ship. Describe what changed
and what the reader must do in the normal body instead.

### Example

```
feat(api)!: require an explicit region on every client

The client used to fall back to us-east-1 when the caller gave no
region, which silently routed requests to the wrong account for users
outside that region.

BREAKING CHANGE: NewClient now returns an error when the region is
empty. Pass Region explicitly or set AWS_REGION before constructing
the client.
```

## Your Task

Commit the above changes as one commit per logical change, never as one commit
for the whole diff.

A logical change is one thing a reviewer can accept or revert on its own. Two
edits belong in one commit only when neither makes sense without the other.
Files are not the unit; a diff across three files can be one change, and a diff
in one file can be three.

For each change, in order:

1. Stage only the paths that belong to it with `git add <path>...`.
2. Commit it with its own message.
3. Move to the next change. Do not stage the next change until the previous
   commit exists.

When a single file carries hunks from two different changes, stop and invoke
`/git-surgeon` through the Skill tool to stage only the hunks for the current
change. `git add <path>` stages the whole file and cannot do this. Do not fold
both changes into one commit to avoid the extra step.

Each commit subject must be 70 characters or less. The description must wrap at
70 characters. Use plain ASCII characters only. Keep the description short and
to the point.
