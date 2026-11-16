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
- Wrap each error that another package returns, such as the error of a Dagger call, with `fmt.Errorf("context: %w", err)`. `CheckLint` of the `dev` toolchain runs `golangci-lint` on each Go module of that toolchain, which includes its test module. The `wrapcheck` linter reports each such error that a function returns unwrapped.
- Name the step in the context of the wrap, such as `reading sbom`. Do not write that the step failed, as in `failed to read sbom` or `error reading sbom`.
- Prefer consistency over performance. Do not add a fast path that makes the behavior of a function hard to predict.
- Format the Go code with gofmt. `nix fmt -- toolchains` runs gofmt on each Go file of the toolchains, and `nix fmt -- --fail-on-change toolchains` also fails when gofmt changes a file.
- In each Go module of the `dev` toolchain, which includes its test module, also format the Go code with the formatters of the root `.golangci.yaml`. `CheckLint` of the `dev` toolchain runs gofumpt with its extra rules, gci, and golines, and it fails when one of them changes a file. These formatters are stricter than gofmt, so a file that passes `nix fmt` can fail `CheckLint`. To apply them, run `dagger call dev format -y`, which runs `golangci-lint run --fix` in each module that `goModules` lists.
- List each Go module that you add to the `dev` toolchain in `goModules` in `toolchains/dev/main.go`. `CheckLint` and `Format` run `golangci-lint` only in the modules of that list.

### Documentation

Follow these conventions in the comments of each module.

- Open `main.go` with a comment that describes the module. Dagger publishes that comment as the description of the module. It reads the comment only from the file that declares the main object, so do not move the comment to a `doc.go` file.
- Leave a blank line between a comment at the top of a file and the `package main` clause. Without the blank line, Go reads the comment as the doc comment of the package. In `CheckLint` of the `dev` toolchain, the `godoclint` linter reports each package that carries a doc comment in more than one file.
- Document each exported item with a doc comment. Dagger publishes the doc comment of a function as the description of the function.
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
