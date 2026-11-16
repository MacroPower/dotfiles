# Toolchains

This file provides guidance to Claude Code (claude.ai/code) when working with Dagger toolchains in this repository.

## Overview

- `dev`: Development shell and sandbox containers.
- `nix`: Nix utils, such as `format` and `lint`.

## Code Style

Each toolchain is a Dagger module in `package main`, and Dagger builds the API of the module from its Go code. Dagger publishes each exported method of the main object, such as `Dev` or `Nix`, as a function of the module. It publishes the comments of the module as the descriptions that `dagger functions` and `dagger call --help` print.

### Go Conventions

Follow these conventions in the Go code of each module.

- Take each setting of the module as an argument of the `New` constructor. Set the default of the argument with a Dagger pragma, as `+defaultPath="/"` sets the default of `source`.
- Store each setting in an exported field of the main object, as `New` stores `source` in `Source`. Dagger passes only the exported fields from the constructor to a function.
- Keep each helper unexported, so that Dagger does not publish it as a function.
- Wrap each error that another package returns, such as the error of a Dagger call, with `fmt.Errorf("context: %w", err)`. `CheckLint` of each toolchain runs `golangci-lint` with the root `.golangci.yaml` on each Go module of that toolchain, which includes its test module. The `wrapcheck` linter reports each such error that a function returns unwrapped.
- Name the step in the context of the wrap, such as `reading sbom`. Do not write that the step failed, as in `failed to read sbom` or `error reading sbom`.
- Return only `error` from each function that carries the `+check` pragma. To check a command, run its container with `Sync(ctx)` and wrap the error that `Sync` returns, as `FlakeCheck` of the `nix` toolchain does. When a check function returns a value, such as a `*dagger.Container`, Dagger fails to load a module whose `engineVersion` is a 1.0 beta or a later version. Follow the rule in a module with a lower `engineVersion` as well, so that the module still loads after its `engineVersion` rises to 1.0.
- Prefer consistency over performance. Do not add a fast path that makes the behavior of a function hard to predict.
- Do not run a `nix profile` command in a function of the `nix` toolchain. The default profile of the container is in `/nix/var/nix`, which `base` mounts from a cache volume, so the profile outlives the run. After nixpkgs-unstable changes a package, `nix profile install` fails on a profile that holds the earlier copy of the package. `TestProfileCommands` in the test module of the `nix` toolchain fails on a Go file of the root module that holds a `nix profile` command.
- Run a tool of nixpkgs in the `nix` toolchain with `nix run`, as `Editorconfig` does. When the step that runs the tool accepts each exit status, build the tool with `nix build --out-link` in an earlier step, as `grypeBase` does. Then a failure of nix still fails the function.
- In each Go module of a toolchain, which includes its test module, format the Go code with the formatters of the root `.golangci.yaml`. `CheckLint` of the toolchain runs gofmt, gofumpt with its extra rules, gci, and golines, and it fails when one of them changes a file.
- To apply the formatters to the `dev` toolchain, run `dagger call dev format -y`, which runs `golangci-lint run --fix` in each module that `goModules` of that toolchain lists. `task format` runs that command after `task nix:format`, which runs treefmt on the other files of the repository.
- To apply the formatters to the `nix` toolchain, run `golangci-lint fmt *.go` in the directory of each Go module of that toolchain. No Dagger function applies them there. Pass the files, because `golangci-lint fmt` without a path also formats the files of a nested module. In `toolchains/nix`, it then sorts the imports of the test module as the imports of the root module, and `CheckLint` rejects that order.
- `nix fmt -- toolchains` runs only gofmt on the Go files of the `nix` toolchain. In the commit hook, `Lint` of the `nix` toolchain runs `nix fmt -- --fail-on-change` on the project source, so the hook rejects a Go file of that toolchain that gofmt changes.
- Keep the Go files of the `dev` toolchain out of gofmt in the treefmt config of `flake.nix`, so that `nix fmt` does not format them and `Lint` of the `nix` toolchain does not check them. `Format` of the `nix` toolchain and `Format` of the `dev` toolchain are both generators, and `dagger generate` fails when two generators change the same file.
- List each Go module that you add to a toolchain in `goModules` of that toolchain, which `toolchains/dev/main.go` and `toolchains/nix/lint.go` declare. `CheckLint` of the toolchain runs `golangci-lint` only in the modules of that list, and so does `Format` of the `dev` toolchain.
- Keep the files that Dagger generates in a module out of the project source of the `nix` toolchain. `Lint`, `Format`, and `Editorconfig` of that toolchain read each file under `toolchains/`, except the paths that the `+ignore` pragma of `source` in `toolchains/nix/main.go` lists. The pragma lists the generated paths, which Dagger also writes to the `.gitignore` file of each module. When Dagger writes a path to a `.gitignore` file and the pragma does not match the path, add the path to the pragma.
- Select a function of the `nix` toolchain with `dagger check nix:<name>` only when the function carries the `+check` pragma. The pragma makes a function a check, and a `dagger check` pattern selects only checks. Run a function without the pragma with `dagger call nix <name>`. In both commands, spell `<name>` as `dagger functions -m toolchains/nix` lists the function, such as `build-home` for `BuildHome`. Write `dagger check`, not its alias `dagger checks`, which `TestTaskfileCommands` fails.
- In a task that selects a function of the `nix` toolchain by name, give each `dagger check` command one pattern and no flag. After the pattern, put nothing but a shell operator, such as `&&` or a pipe. In a `dagger call` command of such a task, put no flag before the function name. Dagger passes a `dagger check` command when one of its patterns matches a check, so Dagger does not report a second pattern that matches no check.
- A `dagger check` command of such a task ends at the end of its line only when YAML keeps the line break there. In a plain scalar, YAML joins two lines with a space. So a pattern on the second line is a second pattern of the command on the first line. In a folded scalar (`>`), YAML joins two lines the same way only when neither line has a deeper indent than the first line of the scalar. It keeps the line break before and after a deeper line, as it keeps each line break of a literal scalar (`|`). So the shell runs a pattern on a deeper line as a command of its own, unless the line before it ends in a backslash.
- Write each dagger command of such a task outside shell quotes and backticks, not as `sh -c 'dagger check nix:lint'`. The shell reads `dagger check nix:lint 'nix:build-home'` as a command with two patterns, and `TestTaskfileCommands` cannot tell that quote from a quote that ends a command. So the test fails each `dagger check` command that ends at a quote or a backtick. It also fails each `dagger call` command with a quote or a backtick right after the function name.
- To split a dagger command of such a task across lines, write the command in a literal block scalar (`|`). End each line except the last one with a backslash. `TestTaskfileCommands` joins those lines as the shell does, and it checks the command that the shell runs. In a plain scalar, YAML joins the lines with a space. A folded scalar (`>`) joins two lines with a space when neither line has a deeper indent than the first line of the scalar. The shell then reads the backslash and that space as a space at the start of the next word.
- Do not put a `#` on a line that ends in a backslash, or at the start of the line after it. `TestTaskfileCommands` cannot tell whether a comment ends a line that holds a `#`. So it does not join that line to the next one, and the backslash stays in the command. The test then fails a `dagger check` command with that backslash. It also fails a `dagger call` command when that backslash comes before the space or the shell operator that follows the function name. After a line that ends in a backslash, the test reads a line that starts with `#` as part of the command, comment text included. So it can fail a dagger command in that comment, which the shell does not run.
- When you add or remove the `+check` pragma of a function of the `nix` toolchain, update each task in `taskfiles/nix/Taskfile.yaml` that runs the function. `TestTaskfileCommands` in the test module of the `nix` toolchain checks each dagger command of that Taskfile as Task runs the command. It reads every string of the Taskfile, such as the `desc` of a task. It fails when a command names a function that the command cannot run or spells the name of the function another way. It also fails on a command of another form, such as one with a second pattern or a flag before the function name.

### Documentation

Follow these conventions in the comments of each module.

- Open `main.go` with a comment that describes the module. Dagger publishes that comment as the description of the module. It reads the comment only from the file that declares the main object, so do not move the comment to a `doc.go` file.
- Leave a blank line between a comment at the top of a file and the `package main` clause. Without the blank line, Go reads the comment as the doc comment of the package. In `CheckLint` of each toolchain, the `godoclint` linter reports each package that carries a doc comment in more than one file.
- Document each exported item with a doc comment. Dagger publishes the doc comment of a function as the description of the function. In `CheckLint` of each toolchain, the `godoclint` linter reports each exported function and each exported method that has no doc comment.
- In the module comment and in each doc comment, explain what the module or the item does and how to use it. Do not list the exported items that the comment covers. `dagger functions` lists the functions of the module, so do not list them in the module comment or in the doc comment of the main object.
- Put a comment above each argument of an exported function, except `ctx`. Dagger publishes the comment as the description of the argument. Put each pragma of the argument, such as `+optional` or `+default`, on a line of its own after the description.
- Put each pragma of a function, such as `+check`, `+generate`, or `+cache`, on a line of its own at the end of the doc comment.
- Link a Go name with the `[Name]` syntax, such as `[Dev]` for the main object, `[Dev.DevBase]` for a function of the module, and `[withClaudeState]` for a helper.

## Testing

The tests of each toolchain are Dagger functions in the test module at `toolchains/<name>/tests`. No task runs `go test` in a toolchain module, so do not add a `_test.go` file. Outside a Dagger session, `go test` fails in these modules, because the generated `internal/dagger` package panics with `DAGGER_SESSION_PORT is not set`.

### Test Functions

Each test is a method of `Tests` that returns an error, such as `func (m *Tests) TestDevBase(ctx context.Context) error`. Follow these conventions in each test.

- Apply the conventions of the Code Style section to the code of a test module.
- Start the name of the method with `Test`. `TestAggregates` counts only those methods as tests.
- Call the toolchain through its `dag` binding, such as `dag.Dev()`, and return an error when a result is wrong.
- Name an expected value `want`.
- Open the doc comment with what the test verifies.
- Set `InsecureRootCapabilities` or `ExperimentalPrivilegedNesting` only in a test that needs it, because each option lets the container bypass its sandbox. Justify the option in a `Security review:` paragraph of the doc comment. The header of `toolchains/dev/tests/main.go` sets this rule.
- End the doc comment with a line that holds only `+check`. `dagger check` runs each function that carries the pragma.
- Leave the pragma off a test that needs `InsecureRootCapabilities`, pushes an image to a registry, or builds the full shell image. Those tests are too privileged or too slow for every run. In the doc comment, say why the test does not carry the pragma, and give the `dagger call` command that runs the test.

### Aggregates

`All` and `AllFull` aggregate the tests of a module. Each aggregate reports success without running a test that it does not call, so add each test that you write to its aggregate.

- Call each test that carries `+check` from `All`.
- Call each test that does not carry `+check` from `AllFull`. `AllFull` also calls `All`, so it runs every test of the module.

`TestAggregates` in `aggregates.go` parses the Go files of the module. It fails when `All` or `AllFull` does not call a test that it must call. Each test module holds a copy of `aggregates.go`, so apply each change to every copy.

### Tasks

The tasks in `taskfiles/toolchains/Taskfile.yaml` run one command for each test module that the `INTEGRATION_MODULES` variable lists. In the table, `<module>` stands for the path of the test module.

| Task                               | Command                            | Tests                           |
| ---------------------------------- | ---------------------------------- | ------------------------------- |
| `task toolchains:integration`      | `dagger check -m <module>`         | Each test that carries `+check` |
| `task toolchains:integration:full` | `dagger call -m <module> all-full` | Every test of the module        |

To run one test, call it by the name that `dagger functions -m <module>` lists, such as `dagger call -m toolchains/dev/tests test-sandbox-network`. For a Go name that holds an acronym, the listed name can differ from the kebab-case form of the Go name. For example, `dagger functions` lists `TestShellSSHReady` as `test-shell-sshready`.

When you add a test module, copy `aggregates.go` into it, define `All` and `AllFull`, and add its path to `INTEGRATION_MODULES`. The tasks fail when a directory matches `toolchains/*/tests` and the variable does not list it.
