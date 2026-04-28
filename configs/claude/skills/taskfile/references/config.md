<!--
Generated from https://taskfile.dev/llms-full.txt
Fetched: 2026-04-19
Content SHA256: 2a8a8f994bc74f3949010436972f8ae951013a6920c76f01fe1eeb73838c78f0
Sections included:
  - /docs/reference/config.md
  - /docs/reference/environment.md
-->

## Configuration Reference

Task has multiple ways of being configured. These methods are parsed, in
sequence, in the following order with the highest priority last:

* *Configuration files*
* [Environment variables](./config.md)
* [Command-line flags](./cli.md)

In this document, we will look at the first of the three options, configuration
files.

### File Precedence

Task will automatically look for directories containing configuration files in
the following order with the highest priority first:

* Current directory (or the one specified by the `--taskfile`/`--entrypoint`
  flags).
* Each directory walking up the file tree from the current directory (or the one
  specified by the `--taskfile`/`--entrypoint` flags) until we reach the user's
  home directory or the root directory of that drive.
* The users `$HOME` directory.
* The `$XDG_CONFIG_HOME/task` directory.

Config files in the current directory, its parent folders or home directory
should be called `.taskrc.yml` or `.taskrc.yaml`. Config files in the
`$XDG_CONFIG_HOME/task` directory are named the same way, but should not contain
the `.` prefix.

All config files will be merged together into a unified config, starting with
the lowest priority file in `$XDG_CONFIG_HOME/task` with each subsequent file
overwriting the previous one if values are set.

For example, given the following files:

```yaml [$XDG_CONFIG_HOME/task/taskrc.yml]
# lowest priority global config
option_1: foo
option_2: foo
option_3: foo
```

```yaml [$HOME/.taskrc.yml]
option_1: bar
option_2: bar
```

```yaml [$HOME/path/to/project/.taskrc.yml]
# highest priority project config
option_1: baz
```

You would end up with the following configuration:

```yaml
option_1: baz # Taken from $HOME/path/to/project/.taskrc.yml
option_2: bar # Taken from $HOME/.taskrc.yml
option_3: foo # Taken from $XDG_CONFIG_HOME/task/.taskrc.yml
```

### Configuration Options

#### `experiments`

The experiments section allows you to enable Task's experimental features. These
options are not enumerated here. Instead, please refer to our
[experiments documentation](../experiments/index.md) for more information.

```yaml
experiments:
  feature_name: 1
  another_feature: 2
```

#### `verbose`

* **Type**: `boolean`
* **Default**: `false`
* **Description**: Enable verbose output for all tasks
* **CLI equivalent**: [`-v, --verbose`](./cli.md#-v---verbose)
* **Environment variable**: [`TASK_VERBOSE`](#task-verbose)

```yaml
verbose: true
```

#### `silent`

* **Type**: `boolean`
* **Default**: `false`
* **Description**: Disables echoing of commands
* **CLI equivalent**: [`-s, --silent`](./cli.md#-s---silent)
* **Environment variable**: [`TASK_SILENT`](#task-silent)

```yaml
silent: true
```

#### `color`

* **Type**: `boolean`
* **Default**: `true`
* **Description**: Enable colored output. Colors are automatically enabled in CI environments (`CI=true`).
* **CLI equivalent**: [`-c, --color`](./cli.md#-c---color)
* **Environment variable**: [`TASK_COLOR`](#task-color)

```yaml
color: false
```

#### `disable-fuzzy`

* **Type**: `boolean`
* **Default**: `false`
* **Description**: Disable fuzzy matching for task names. When enabled, Task will not suggest similar task names when you mistype a task name.
* **CLI equivalent**: [`--disable-fuzzy`](./cli.md#--disable-fuzzy)
* **Environment variable**: [`TASK_DISABLE_FUZZY`](#task-disable-fuzzy)

```yaml
disable-fuzzy: true
```

#### `concurrency`

* **Type**: `integer`
* **Minimum**: `1`
* **Description**: Number of concurrent tasks to run
* **CLI equivalent**: [`-C, --concurrency`](./cli.md#-c---concurrency-number)
* **Environment variable**: [`TASK_CONCURRENCY`](#task-concurrency)

```yaml
concurrency: 4
```

#### `failfast`

* **Type**: `boolean`
* **Default**: `false`
* **Description**: Stop executing dependencies as soon as one of them fail
* **CLI equivalent**: [`-F, --failfast`](./cli.md#-f---failfast)
* **Environment variable**: [`TASK_FAILFAST`](#task-failfast)

```yaml
failfast: true
```

#### `interactive`

* **Type**: `boolean`
* **Default**: `false`
* **Description**: Prompt for missing required variables instead of failing.
  When enabled, Task will display an interactive prompt for any missing required
  variable. Requires a TTY. Task automatically detects non-TTY environments
  (CI pipelines, etc.) and skips prompts.
* **CLI equivalent**: [`--interactive`](./cli.md#--interactive)

```yaml
interactive: true
```

### Example Configuration

Here's a complete example of a `.taskrc.yml` file with all available options:

```yaml
# Global settings
verbose: true
silent: false
color: true
disable-fuzzy: false
concurrency: 2

# Enable experimental features
experiments:
  REMOTE_TASKFILES: 1
```

## Environment Reference

Task has multiple ways of being configured. These methods are parsed, in
sequence, in the following order with the highest priority last:

* [Configuration files](./config.md)
* *Environment variables*
* [Command-line flags](./cli.md)

In this document, we will look at the second of the three options, environment
variables. All Task-specific variables are prefixed with `TASK_` and override
their configuration file equivalents.

### Variables

All [configuration file options](./config.md) can also be set via environment
variables. The priority order is: CLI flags > environment variables > config files > defaults.

#### `TASK_VERBOSE`

* **Type**: `boolean` (`true`, `false`, `1`, `0`)
* **Default**: `false`
* **Description**: Enable verbose output for all tasks
* **Config equivalent**: [`verbose`](#verbose)

#### `TASK_SILENT`

* **Type**: `boolean` (`true`, `false`, `1`, `0`)
* **Default**: `false`
* **Description**: Disables echoing of commands
* **Config equivalent**: [`silent`](#silent)

#### `TASK_COLOR`

* **Type**: `boolean` (`true`, `false`, `1`, `0`)
* **Default**: `true`
* **Description**: Enable colored output
* **Config equivalent**: [`color`](#color)

#### `TASK_DISABLE_FUZZY`

* **Type**: `boolean` (`true`, `false`, `1`, `0`)
* **Default**: `false`
* **Description**: Disable fuzzy matching for task names
* **Config equivalent**: [`disable-fuzzy`](#disable-fuzzy)

#### `TASK_CONCURRENCY`

* **Type**: `integer`
* **Description**: Limit number of tasks to run concurrently
* **Config equivalent**: [`concurrency`](#concurrency)

#### `TASK_FAILFAST`

* **Type**: `boolean` (`true`, `false`, `1`, `0`)
* **Default**: `false`
* **Description**: When running tasks in parallel, stop all tasks if one fails
* **Config equivalent**: [`failfast`](#failfast)

#### `TASK_DRY`

* **Type**: `boolean` (`true`, `false`, `1`, `0`)
* **Default**: `false`
* **Description**: Compiles and prints tasks in the order that they would be run, without executing them

#### `TASK_ASSUME_YES`

* **Type**: `boolean` (`true`, `false`, `1`, `0`)
* **Default**: `false`
* **Description**: Assume "yes" as answer to all prompts

#### `TASK_INTERACTIVE`

* **Type**: `boolean` (`true`, `false`, `1`, `0`)
* **Default**: `false`
* **Description**: Prompt for missing required variables

#### `TASK_TEMP_DIR`

Defines the location of Task's temporary directory which is used for storing
checksums and temporary metadata. Can be relative like `tmp/task` or absolute
like `/tmp/.task` or `~/.task`. Relative paths are relative to the root
Taskfile, not the working directory. Defaults to: `./.task`.

#### `TASK_CORE_UTILS`

This env controls whether the Bash interpreter will use its own
core utilities implemented in Go, or the ones available in the system.
Valid values are `true` (`1`) or `false` (`0`). By default, this is `true` on
Windows and `false` on other operating systems. We might consider making this
enabled by default on all platforms in the future.

#### `FORCE_COLOR`

Force color output usage.

#### Custom Colors

All color variables are [ANSI color codes][ansi]. You can specify multiple codes
separated by a semicolon. For example: `31;1` will make the text bold and red.
Task also supports 8-bit color (256 colors). You can specify these colors by
using the sequence `38;2;R:G:B` for foreground colors and `48;2;R:G:B` for
background colors where `R`, `G` and `B` should be replaced with values between
0 and 255.

For convenience, we allow foreground colors to be specified using shorthand,
comma-separated syntax: `R,G,B`. For example, `255,0,0` is equivalent to
`38;2;255:0:0`.

A table of variables and their defaults can be found below:

| ENV | Default |
| --- | --- |
| `TASK_COLOR_RESET` | `0` |
| `TASK_COLOR_RED` | `31` |
| `TASK_COLOR_GREEN` | `32` |
| `TASK_COLOR_YELLOW` | `33` |
| `TASK_COLOR_BLUE` | `34` |
| `TASK_COLOR_MAGENTA` | `35` |
| `TASK_COLOR_CYAN` | `36` |
| `TASK_COLOR_BRIGHT_RED` | `91` |
| `TASK_COLOR_BRIGHT_GREEN` | `92` |
| `TASK_COLOR_BRIGHT_YELLOW` | `93` |
| `TASK_COLOR_BRIGHT_BLUE` | `94` |
| `TASK_COLOR_BRIGHT_MAGENTA` | `95` |
| `TASK_COLOR_BRIGHT_CYAN` | `96` |

[ansi]: https://en.wikipedia.org/wiki/ANSI_escape_code
