# configs/claude

Source for Claude Code configuration. `home/claude.nix` symlinks these files
into `~/.claude/`, so edit them here and run `task switch` to apply.

- `skills/<name>/`: skill directories, each with a `SKILL.md` plus assets.
- `agents/<name>.md`: agent definitions.

## Writing

Load the `prose` skill before writing or editing any skill or agent `.md` file.

## Skill Descriptions

Keep skill descriptions as short as possible. Focus on describing when/where/why the
skill should be loaded. You do not need to describe skill contents.

Example:

```yaml
---
name: taskfile
description: >-
  ALWAYS load BEFORE creating or editing Taskfiles (Taskfile.yaml, .taskfiles/*).
---

# ...
```

Note how the contents of `taskfile` are not enumerated. We focus ONLY on giving
info about when/where/why the model should load the skill.

## Vendored Skills

You may edit the files of `skills/playwright-cli/` in place. They are the skill
files of the playwright-cli release that `pkgs/playwright-cli.nix` names, with
local edits. `pkgs/playwright-cli-update.sh` replaces them with the skill files
of the latest release, and it makes each local edit again in those files. When
a local edit conflicts with that release, the script prints the edit and fails.
The comment at the top of the script describes its options.
