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
