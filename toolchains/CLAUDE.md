# Toolchains

This file provides guidance to Claude Code (claude.ai/code) when working with Dagger toolchains in this repository.

## Overview

- `dev`: Development shell and sandbox containers.
- `nix`: Nix utils, e.g. `format`, `lint`.

## Code Style

### Go Conventions

- Document all exported items with doc comments.
- Package documentation in `doc.go` files.
- Wrap errors with `fmt.Errorf("context: %w", err)`, or `fmt.Errorf("%w: %w", ErrSentinel, err)`.
- Avoid using "failed" or "error" in library error messages.
- Use global error variables for common errors.
- Use constructors with functional options.
- Accept interfaces, return concrete types.
- Prefer consistency over performance, avoid "fast paths" that could lead to unpredictable behavior.

### Documentation

- Use `[Name]` syntax for Go doc links. Use `[*Name]` for pointer types.
- Functional option types should have a list linking to all functions of that type.
- Functional options should always have a link to their type.
- Docs should explain concepts and usage patterns; **do not enumerate exports**.

## Testing

The tests of each toolchain are Dagger functions in the test module at `toolchains/<name>/tests`. No task runs `go test` in a toolchain module, so do not add a `_test.go` file. Outside a Dagger session, `go test` fails in these modules, because the generated `internal/dagger` package panics with `DAGGER_SESSION_PORT is not set`.

### Test Functions

Each test is a method of `Tests` that returns an error, such as `func (m *Tests) TestDevBase(ctx context.Context) error`. Follow these conventions in each test.

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
