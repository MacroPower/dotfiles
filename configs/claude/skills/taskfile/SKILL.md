---
name: taskfile
description: >-
  Reference and guidance for writing Taskfiles (Taskfile.yml, Taskfile.yaml,
  Taskfile.dist.yml) and using the `task` / go-task CLI (https://taskfile.dev/).
  Trigger when the user edits or creates Taskfile.yml/.taskrc.yml, pastes a
  taskfile.dev/schema.json yaml-language-server directive, asks about Task
  schema keys (version, tasks, vars, env, includes, deps, cmds, preconditions,
  requires, sources, generates, method, run, dotenv, status, aliases, watch,
  platforms), runs or troubleshoots the `task` CLI, or asks about Task
  templating ({{.VAR}}, special variables .TASK, .CHECKSUM, .USER_WORKING_DIR,
  .ROOT_DIR). Also trigger on references to `go-task`, `go-task/task`, or
  `task --init`. SKIP when the user is discussing generic to-do lists,
  Celery/Airflow/Luigi tasks, Claude Code's own TaskCreate/TaskList tools, or
  macOS Reminders. Those are unrelated to the go-task runner.
---

# Taskfile Task Runner Reference

[Task](https://taskfile.dev/) (package name `go-task`) is a YAML-based task runner, usually invoked as `task`. A project describes its tasks in `Taskfile.yml` (or `Taskfile.yaml`, `Taskfile.dist.yml`) at schema `version: '3'`. This skill is a snapshot of the official docs at <https://taskfile.dev/llms-full.txt>.

## Quick Reference

| Topic | Reference |
|-------|-----------|
| Taskfile YAML schema (every key, field, type) | [schema.md](references/schema.md) |
| `task` CLI (commands, flags, exit codes) | [cli.md](references/cli.md) |
| Templating (`{{.VAR}}`, special vars, functions) | [templating.md](references/templating.md) |
| Config files (`.taskrc.yml`) and `TASK_*` env vars | [config.md](references/config.md) |
| Experimental features and how to enable them | [experiments.md](references/experiments.md) |
| Usage guide (includes, env, dotenv, variables, sources/generates, watch, run modes, platforms, preconditions, internal tasks, parallelism, etc.) | [guide.md](references/guide.md) |

Open the matching reference file for full detail. Summaries below are for quick lookup.

## Taskfile Basics

Scaffold a new Taskfile with `task --init` (optionally takes a directory or filename, e.g. `task --init ./subdir` or `task --init Custom.yml`). That produces something like:

```yaml
version: '3'

vars:
  GREETING: Hello, World!

tasks:
  default:
    desc: Print a greeting message
    cmds:
      - echo "{{.GREETING}}"
    silent: true

  build:
    desc: Build the binary
    cmds:
      - go build ./cmd/main.go
```

Run tasks with `task <name>` (e.g. `task build`). Bare `task` runs the `default` task. Task discovers `Taskfile.yml` in the current directory and walks up the tree until it finds one, so `cd` into a subdir and `task` still works. Use `--dir <path>` or `--taskfile <file>` to target a specific location.

Commands run through [mvdan/sh](https://github.com/mvdan/sh), a native Go sh interpreter, so `sh`-style syntax works on every platform including Windows. Any binary you invoke must still be on `PATH`.

Always start new Taskfiles with `version: '3'`. Versions 1 and 2 are obsolete; do not recommend them, do not carry patterns from them, and ignore "in v2 this worked differently" asides.

## Common Schema Keys

Keys you'll reach for most often. Full detail in [schema.md](references/schema.md).

- `version`: schema version. Always `'3'`, or a more specific `'3.x'` to declare a minimum Task version.
- `vars`: named variables, used via `{{.NAME}}`. Can be literals, dynamic (`sh:` output), or references to other vars.
- `env`: environment variables exported to commands. Applies at root (all tasks) or task level.
- `dotenv`: list of `.env` files to load. Task-level `env:` overrides dotenv values.
- `includes`: import other Taskfiles under a namespace (`task docs:serve`). Supports `flatten:`, `internal:`, `optional:`, per-include `vars:`, and `excludes:`.
- `tasks`: map of task name to task definition.
- `desc` / `summary`: short and long descriptions. `desc` shows in `task --list`.
- `cmds`: list of commands to run. Can be inline strings, `{ cmd: ... }` objects, or `{ task: other, vars: {...} }` cross-task calls.
- `deps`: tasks to run (in parallel) before `cmds`.
- `sources` / `generates`: file globs used with `method: checksum` or `method: timestamp` to skip up-to-date tasks.
- `method`: up-to-date check. `checksum` (default), `timestamp`, or `none`.
- `status`: list of shell commands. Non-zero exit forces the task to run.
- `preconditions`: list of `{ sh: ..., msg: ... }`. Failing preconditions abort with a readable message.
- `requires`: required variables the caller must set. Pairs with `--interactive` / `TASK_INTERACTIVE` to prompt.
- `vars` (task-level): scoped to this task, overrides root vars.
- `dir`: run the task in a specific directory. Use `{{.USER_WORKING_DIR}}` for caller-relative behavior.
- `run`: `always` (default), `once`, or `when_changed`. Controls re-execution within a single invocation.
- `platforms`: restrict a task to specific OS/arch (`linux`, `darwin/arm64`, etc.).
- `aliases`: alternate names. Works on included namespaces too.
- `internal: true`: hides the task from `--list` and blocks direct invocation. Only callable from other tasks.
- `silent: true`: suppress command echoing for this task.
- `interactive: true`: task needs a TTY (e.g. `less`, `vim`). Task adjusts stdio accordingly.
- `watch: true`: pair with `task --watch` to re-run on file changes in `sources:`.

## CLI Cheatsheet

Full detail in [cli.md](references/cli.md).

```bash
task                       # run the `default` task
task <name> [<name>...]    # run one or more tasks
task --list       / -l     # list tasks with desc
task --list-all   / -a     # list everything, including undocumented
task --init       / -i     # scaffold a new Taskfile.yml
task --dry        / -n     # compile + print, do not execute
task --force      / -f     # ignore up-to-date checks
task --parallel   / -p     # run the listed tasks in parallel
task --watch      / -w     # re-run on source changes
task --dir <path> / -d     # chdir before running
task --taskfile <file> / -t  # use a specific Taskfile; `-t -` reads from stdin
task --global     / -g     # run $HOME/Taskfile.yml
task --status              # exit non-zero if any named task is stale
task --summary             # show a task's full summary
task --list --json         # machine-readable task listing
task -- <args>             # pass arguments to the task as `.CLI_ARGS`
```

## Templating Basics

Task uses Go `text/template` with [sprig](https://masterminds.github.io/sprig/) functions and a handful of Task-specific additions. Full detail in [templating.md](references/templating.md).

```yaml
vars:
  NAME: world
  STAMP:
    sh: date +%s

tasks:
  greet:
    cmds:
      - echo "Hello, {{.NAME}} at {{.STAMP}}"
      - echo "Root is {{.ROOT_DIR}}, task dir is {{.TASK_DIR}}"
      - echo "Called from {{.USER_WORKING_DIR}} as {{.TASK}}"
```

Special variables you'll reach for most:

- `{{.TASK}}`: name of the current task.
- `{{.ROOT_DIR}}`: directory containing the root Taskfile.
- `{{.TASKFILE}}` / `{{.TASKFILE_DIR}}`: path to the Taskfile that defined the current task. Differs from `ROOT_DIR` when `includes` are used.
- `{{.USER_WORKING_DIR}}`: the directory the user invoked `task` from. Useful in global or monorepo Taskfiles.
- `{{.CHECKSUM}}` / `{{.TIMESTAMP}}`: available inside `status:` / `sources:` checks.
- `{{.CLI_ARGS}}` / `{{.CLI_ARGS_LIST}}`: args passed after `--` on the command line.
- `{{env "VAR"}}`: read an OS env var at template time, bypassing Task's internal env.

## Common Patterns

Every feature below is documented in [guide.md](references/guide.md) with full syntax.

### Discovery and invocation

- **Supported file names.** Task finds `Taskfile.yml`, `taskfile.yml`, `.yaml`, and `.dist.*` variants. `.dist` lets a project commit a shared file while users override with their own gitignored `Taskfile.yml`.
- **Walks up the tree.** Running `task` from a subdirectory finds the nearest parent Taskfile and behaves as if run from that directory. Pair with `{{.USER_WORKING_DIR}}` when you want the task to act on the caller's cwd (common in monorepos).
- **Global Taskfile.** `task -g` runs `$HOME/Taskfile.yml`. Default cwd is `$HOME`. Override per-task with `dir: '{{.USER_WORKING_DIR}}'` for automation you can fire from anywhere.
- **Read from stdin.** `task -t - < file.yml` or piped input lets you run generated Taskfiles without writing them to disk.

### Includes

- **Namespaces.** `includes: { docs: ./docs }` makes `task docs:serve` work. Path resolves relative to the including file.
- **Per-include `vars`.** Reuse the same Taskfile with different inputs (`DOCKER_IMAGE: frontend` vs `backend`). Note: vars set in the included file win unless the included file uses `default`.
- **`flatten: true`.** Hoists included tasks into the root namespace. Collisions error out.
- **`internal: true`.** Marks every task in the include as internal (hidden from `--list`, not directly callable).
- **`optional: true`.** A missing file is not an error.
- **`excludes: [foo, bar]`.** Drops specific tasks from the include. Works with `flatten` too.
- **`aliases:`.** Give a namespace a shorter alias, e.g. `generate` + `gen`.
- **OS-specific includes.** `build: ./Taskfile_{{OS}}.yml` selects a file per platform via templating.
- **Per-include `dir`.** Forces included tasks to run in a specific directory regardless of caller cwd.

### Tasks composing tasks

- **`deps:`.** Prereqs run in parallel before `cmds`. `--failfast` cancels siblings on first failure.
- **`cmds: [{ task: build, vars: { TARGET: linux } }]`.** Cross-task calls with parameters. Prefer this over shell indirection.
- **`internal: true` on a task.** Only callable from other tasks, hidden from `--list`. Useful for parameterized helpers.
- **`run:` policy.** `always` (default), `once` (once per invocation even if called multiple times), `when_changed` (once per unique variable set).
- **`defer:`.** Cleanup hook. Runs after `cmds` whether they succeed or fail. Accepts a shell command or a `task:` call.

### Variables and environment

- **Literal vars.** `vars: { NAME: value }` referenced as `{{.NAME}}`.
- **Dynamic vars.** `vars: { STAMP: { sh: date +%s } }` captures shell output once.
- **Referencing other vars.** `vars: { FULL: '{{.PREFIX}}-{{.NAME}}' }`. Later vars can read earlier ones.
- **Map vars from JSON/YAML.** Use `fromJson`/`fromYaml` templates to parse config into a map variable, then index with `{{.MAP.key}}`.
- **`env:` at task or root level.** Exports to child processes. `env` supports `sh:` dynamic values too.
- **`dotenv:` loading.** List of `.env`-style files. Earlier entries win over later ones. Task-level `env:` still overrides dotenv. Dotenv does not compose through `includes`.

### Up-to-date / gating

- **Fingerprinting.** Declare `sources:` (inputs) and `generates:` (outputs) and Task skips when nothing changed. `method: checksum` (default) hashes file contents, `method: timestamp` compares mtimes, `method: none` always runs. `--force` overrides.
- **`status:`.** Shell commands that return 0 when the task is up-to-date. Any non-zero forces execution. Independent of `sources`/`generates`.
- **`preconditions:`.** `{ sh: ..., msg: ... }` pairs. Aborts with a readable message if any check fails.
- **`if:`.** Conditional execution on tasks, individual commands, or inside `for:` loops. Supports template expressions (`if: '{{.ENV == "prod"}}'`). Lighter than `preconditions:`; it just skips silently instead of erroring.
- **`requires: { vars: [A, B] }`.** Declares required caller-supplied variables. Supports `enum:` for allowed values and works with `--interactive` / `TASK_INTERACTIVE` to prompt on TTYs.

### Loops (`for:`)

- **Static list.** `for: [a, b, c]` with `{{.ITEM}}` in cmd.
- **Matrix.** `for: { matrix: { os: [linux, darwin], arch: [amd64, arm64] } }` expands the cross-product.
- **Over `sources:`.** `for: sources` (or `for: generates`) iterates the task's own file lists.
- **Over variables.** `for: { var: LIST, split: ',' }` splits a variable by a delimiter (or newline by default).
- **Over tasks or deps.** `for:` inside `deps:` or a `task:` call generates parallel invocations.
- **Rename.** `as: FILE` renames the loop variable so you're not stuck with `ITEM`.

### CLI arguments and wildcards

- **`{{.CLI_ARGS}}` / `{{.CLI_ARGS_LIST}}`.** Everything after `--` on the command line. Let users pass extra flags through.
- **Wildcard task names.** `build-*` in the Taskfile matches any suffix. `{{index .MATCH 0}}` grabs the captured segment. Handy for a single `deploy-<env>` pattern.

### Platforms

- **`platforms: [linux, darwin/arm64]`.** Skips a whole task on other platforms.
- **Per-command `platforms:`.** Inside `cmds:`, restrict a single command. Useful for mixing shell and OS-specific binaries in one task.

### Output and CI

- **`silent: true`.** Suppresses command echoing for a task (or globally via `--silent` / `.taskrc.yml`).
- **`--dry` / `-n`.** Prints what would run without executing.
- **`ignore_error: true`.** Continue after non-zero exit on a single command.
- **Output modes.** `-o interleaved` (default), `-o group` (buffer per task), `-o prefixed` (prefix lines with task name).
- **Output groups.** `--output-group-begin '::group::{{.TASK}}' --output-group-end '::endgroup::'` for GitHub Actions log folding. `--output-group-error-only` suppresses output on success.
- **Colored output.** On by default. `NO_COLOR=1` or `--color=false` disables; `FORCE_COLOR=1` forces. CI auto-detection (`CI=true`) keeps colors on.
- **Error annotations.** Task emits structured `::error file=...` annotations for GitHub Actions.

### Interaction and safety

- **`interactive: true` on a task.** Task wires stdio through so TUIs (`vim`, `less`, `fzf`) work.
- **`prompt:` on a task.** Warning prompt shown before running, e.g. for destructive deploys. Skipped with `--yes` / `TASK_ASSUME_YES`, or in non-TTY CI contexts.
- **Watch mode.** `task --watch <name>` re-runs on changes to `sources:`. Set `watch: true` on a task to watch by default. `-I 1s` tunes the interval.

### Convenience

- **`aliases: [b, bld]`.** Alternate names for a task. Works on included namespaces too.
- **Overriding task name.** `task: deploy-prod` key lets the map name differ from what users type (useful when combined with `aliases`).
- **`help:` / `desc:` / `summary:`.** `desc:` is the one-liner in `task --list`. `summary:` is the long form shown by `task --summary <name>`.
- **Short task syntax.** `build: go build ./...` collapses a single-command task to one line.
- **`set:` and `shopt:`.** Per-task or per-command shell options (`set: [errexit, pipefail]`, `shopt: [globstar]`). Applies to the `mvdan/sh` interpreter.
