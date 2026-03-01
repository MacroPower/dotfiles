# Go SDK Patterns

## Table of Contents

1. [Module Structure](#module-structure)
2. [Constructors](#constructors)
3. [Function Signatures](#function-signatures)
4. [Arguments](#arguments)
5. [Return Types](#return-types)
6. [Builder Pattern (Chaining)](#builder-pattern-chaining)
7. [With() Composition](#with-composition)
8. [Custom Types](#custom-types)
9. [State and Getters](#state-and-getters)
10. [Enumerations](#enumerations)
11. [Interfaces](#interfaces)
12. [Error Handling](#error-handling)
13. [Lazy Evaluation and Sync](#lazy-evaluation-and-sync)
14. [Debugging](#debugging)
15. [Custom Telemetry](#custom-telemetry)
16. [Documentation](#documentation)

---

## Module Structure

### Minimal module

```go
package main

import (
    "context"
    "dagger/my-module/internal/dagger"
)

type MyModule struct{}

func (m *MyModule) Hello(ctx context.Context) (string, error) {
    return dag.Container().
        From("alpine").
        WithExec([]string{"echo", "hello"}).
        Stdout(ctx)
}
```

### Multi-file module

All files share the same package. Only the top-level package is public API:

```
main.go     — MyModule struct + core functions
build.go    — build-related functions
test.go     — test-related functions
```

### Sub-packages

Access Dagger types from sub-packages by importing the generated package. Since `dag` is only available in the main package, sub-packages must receive a `*dagger.Client` parameter:

```go
// utils/utils.go
package utils

import "dagger/my-module/internal/dagger"

func DoThing(client *dagger.Client) *dagger.Directory {
    return client.Container().From("golang:1.22").Directory("/src")
}
```

---

## Constructors

### Simple constructor with defaults

```go
func New(
    // +default="Hello"
    greeting string,
    // +default="World"
    name string,
) *MyModule {
    return &MyModule{
        Greeting: greeting,
        Name:     name,
    }
}

type MyModule struct {
    Greeting string
    Name     string
}

func (m *MyModule) Message() string {
    return fmt.Sprintf("%s, %s!", m.Greeting, m.Name)
}
```

### Constructor with complex type defaults

```go
func New(
    // +optional
    ctr *dagger.Container,
) *MyModule {
    if ctr == nil {
        ctr = dag.Container().From("alpine:3.14.0")
    }
    return &MyModule{Ctr: *ctr}
}

type MyModule struct {
    Ctr dagger.Container
}
```

### Constructor accepting source directory

This is the most common pattern for build/CI modules:

```go
func New(
    // Project source directory
    // +defaultPath="."
    // +ignore=["**/node_modules", "**/.git"]
    source *dagger.Directory,
) *MyModule {
    return &MyModule{Source: source}
}

type MyModule struct {
    Source *dagger.Directory
}
```

### Important

- Fields must be **exported** (capitalized) to serialize correctly across Dagger calls
- Use `// +private` on struct fields to hide them from the API while keeping serialization
- A module has exactly one constructor
- Constructor parameters become flags on the `dagger` command directly (e.g., `dagger call --name=Foo message`)

---

## Function Signatures

### Basic patterns

```go
// No args, no error
func (m *MyModule) Version() string

// With context and error (needed for any Dagger API call that resolves a value)
func (m *MyModule) Build(ctx context.Context) (string, error)

// Accepting and returning Dagger types (lazy — no ctx needed)
func (m *MyModule) Container(src *dagger.Directory) *dagger.Container

// Returning void with possible error
func (m *MyModule) Verify(ctx context.Context) error
```

### Check functions

Annotate functions with `// +check` to register them as checks that run with `dagger check`. Check functions must not require arguments (optional args with defaults are fine). They can return `error` (pass/fail) or `*dagger.Container` (exit code determines result):

```go
// Lint runs the linter
// +check
func (m *MyModule) Lint(ctx context.Context) error {
    _, err := dag.Container().
        From("golangci/golangci-lint:latest").
        WithDirectory("/src", m.Source).
        WithWorkdir("/src").
        WithExec([]string{"golangci-lint", "run"}).
        Sync(ctx)
    return err
}

// Build verifies the project compiles (container exit code = check result)
// +check
func (m *MyModule) Build() *dagger.Container {
    return dag.Container().
        From("golang:1.22").
        WithDirectory("/src", m.Source).
        WithWorkdir("/src").
        WithExec([]string{"go", "build", "./..."})
}
```

### When to include `context.Context`

Include `ctx` whenever the function makes Dagger API calls that resolve values (`.Stdout(ctx)`, `.Sync(ctx)`, `.Export(ctx, ...)`). If the function only constructs a pipeline without resolving, `ctx` is not needed.

### When to return `error`

Return `error` alongside your value type whenever the function calls operations that can fail — essentially any API call that takes `ctx`.

---

## Arguments

### Strings with defaults

```go
func (m *MyModule) Greet(
    // Who to greet
    // +default="World"
    name string,
) string {
    return "Hello, " + name
}
```

### Optional arguments

When `// +optional` is used, omitted Go scalar arguments receive their zero value (`""` for strings, `0` for ints, `false` for bools). Omitted pointer/object types receive `nil`. Check for zero values to detect whether the caller provided the argument:

```go
func (m *MyModule) Build(
    ctx context.Context,
    src *dagger.Directory,
    // +optional
    tags string,
) *dagger.Container {
```

### Directory and File arguments

```go
func (m *MyModule) Lint(
    // Source code
    // +defaultPath="."
    // +ignore=["**/.git", "**/vendor"]
    src *dagger.Directory,
) *dagger.Container {
```

### Container arguments with default address

```go
func (m *MyModule) Version(
    ctx context.Context,
    // +defaultAddress="alpine:latest"
    ctr *dagger.Container,
) (string, error) {
    return ctr.WithExec([]string{"cat", "/etc/alpine-release"}).Stdout(ctx)
}
```

The `+defaultAddress` annotation provides a default container image when the caller omits the argument. Valid formats: `alpine:latest`, `golang:1.22`, `ghcr.io/owner/image:tag`.

### Secret arguments

```go
func (m *MyModule) Publish(
    ctx context.Context,
    // Registry token
    token *dagger.Secret,
) (string, error) {
```

### Array arguments

```go
func (m *MyModule) Build(
    ctx context.Context,
    // Build targets
    targets []string,
) *dagger.Directory {
```

### Boolean arguments

```go
func (m *MyModule) Build(
    ctx context.Context,
    // Enable verbose output
    // +optional
    verbose bool,
) (string, error) {
```

### Socket arguments

```go
func (m *MyModule) ClonePrivate(
    ctx context.Context,
    // SSH socket for authentication
    sshSocket *dagger.Socket,
) *dagger.Directory {
    return dag.Git("git@github.com:user/repo.git",
        dagger.GitOpts{SSHAuthSocket: sshSocket}).
        Ref("main").Tree()
}
```

### Numeric arguments

```go
func (m *MyModule) Add(a int, b int) int { return a + b }
func (m *MyModule) Divide(a float64, b float64) float64 { return a / b }
```

---

## Return Types

### Returning containers (chainable)

```go
func (m *MyModule) Build(src *dagger.Directory) *dagger.Container {
    return dag.Container().
        From("golang:1.22").
        WithDirectory("/src", src).
        WithWorkdir("/src").
        WithExec([]string{"go", "build", "-o", "app"})
}
```

Callers can chain: `dagger call build --src=. publish --address=...`

### Returning directories/files

```go
func (m *MyModule) Build(src *dagger.Directory) *dagger.Directory {
    return dag.Container().
        From("golang:1.22").
        WithDirectory("/src", src).
        WithWorkdir("/src").
        WithExec([]string{"go", "build", "-o", "app"}).
        Directory("/src")
}
```

Callers can export: `dagger call build --src=. export --path=./out`

### Returning strings (final values)

```go
func (m *MyModule) Version(ctx context.Context) (string, error) {
    return dag.Container().
        From("alpine").
        WithExec([]string{"cat", "/etc/os-release"}).
        Stdout(ctx)
}
```

### Returning custom types

```go
type TestResult struct {
    Report   *dagger.File
    ExitCode int
}

func (m *MyModule) Test(ctx context.Context) (*TestResult, error) {
    // ...
}
```

---

## Builder Pattern (Chaining)

Return `*MyModule` from `With*` methods to enable fluent chaining:

```go
type MyModule struct {
    Greeting string
    Name     string
}

func (m *MyModule) WithGreeting(ctx context.Context, greeting string) (*MyModule, error) {
    m.Greeting = greeting
    return m, nil
}

func (m *MyModule) WithName(ctx context.Context, name string) (*MyModule, error) {
    m.Name = name
    return m, nil
}

func (m *MyModule) Message(ctx context.Context) (string, error) {
    greeting := m.Greeting
    if greeting == "" { greeting = "Hello" }
    name := m.Name
    if name == "" { name = "World" }
    return fmt.Sprintf("%s, %s!", greeting, name), nil
}
```

The `ctx`/`error` signature is the canonical pattern. If your `With*` methods do no async work, you can simplify to `func (m *MyModule) WithGreeting(greeting string) *MyModule` — both forms work.

Usage: `dagger call with-greeting --greeting=Hi with-name --name=Dagger message`

---

## With() Composition

The `With()` method on Dagger types accepts a function for reusable modifications. The type `dagger.WithContainerFunc` (defined as `func(*dagger.Container) *dagger.Container`) creates composable helpers:

```go
func EnvVariables(envs map[string]string) dagger.WithContainerFunc {
    return func(c *dagger.Container) *dagger.Container {
        for k, v := range envs {
            c = c.WithEnvVariable(k, v)
        }
        return c
    }
}

func (m *MyModule) Build(src *dagger.Directory) *dagger.Container {
    return dag.Container().
        From("golang:1.22").
        WithDirectory("/src", src).
        With(EnvVariables(map[string]string{
            "CGO_ENABLED": "0",
            "GOOS":        "linux",
            "GOARCH":      "amd64",
        })).
        WithExec([]string{"go", "build", "-o", "app"})
}
```

This pattern keeps container modification logic modular and testable.

---

## Custom Types

Define additional types alongside the main object. They are accessible only through chaining from the main object:

```go
import "dagger/my-module/internal/dagger"

type MyModule struct{}

func (m *MyModule) DaggerOrganization() *Organization {
    url := "https://github.com/dagger"
    return &Organization{
        URL:          url,
        Repositories: []*dagger.GitRepository{dag.Git(url + "/dagger")},
        Members: []*Account{
            {"jane", "jane@example.com"},
            {"john", "john@example.com"},
        },
    }
}

type Organization struct {
    URL          string
    Repositories []*dagger.GitRepository
    Members      []*Account
}

type Account struct {
    Username string
    Email    string
}

func (a *Account) URL() string {
    return "https://github.com/" + a.Username
}
```

Custom types are automatically prefixed with the main object name in the API schema (e.g., `MyModuleOrganization`, `MyModuleAccount`).

---

## State and Getters

Exported struct fields are automatically exposed as getter functions in the API:

```go
type MyModule struct {
    // The greeting to use (exposed as API function)
    Greeting string
    // Who to greet (hidden from API)
    // +private
    Name string
}
```

`Greeting` becomes queryable: `dagger call --name=Foo greeting` returns `"Hello"`.
`Name` is serialized but not exposed as a function.

---

## Enumerations

Restrict argument values to a predefined set:

```go
type Platform string

const (
    // Linux platform
    PlatformLinux   Platform = "linux"
    // macOS platform
    PlatformDarwin  Platform = "darwin"
    // Windows platform
    PlatformWindows Platform = "windows"
)

func (m *MyModule) Build(
    ctx context.Context,
    // Target platform
    platform Platform,
) *dagger.Container {
```

Comments above each const value become descriptions in the API. Enum values are case-sensitive — use UPPER_CASE by convention.

---

## Interfaces

Define abstract types for loose coupling between modules:

```go
type Buildable interface {
    DaggerObject
    Build(ctx context.Context) (*dagger.Directory, error)
}

func (m *MyModule) Deploy(ctx context.Context, app Buildable) error {
    dir, err := app.Build(ctx)
    // deploy dir...
}
```

Any module implementing the `Build` method can be passed as a `Buildable`. The SDK auto-generates conversion functions (e.g., `AsMyModuleBuildable()`).

Interface function signature rules:
- Must embed `DaggerObject`
- Functions returning scalars/arrays: must accept `context.Context` and return `error`
- Functions returning chainable objects: must NOT return `error`, do not need `context.Context`
- Argument names are required (they map to GraphQL field argument names)

---

## Error Handling

### Standard Go errors

```go
func (m *MyModule) Build(ctx context.Context) (string, error) {
    out, err := dag.Container().
        From("golang:1.22").
        WithExec([]string{"go", "build", "./..."}).
        Stdout(ctx)
    if err != nil {
        return "", fmt.Errorf("build failed: %w", err)
    }
    return out, nil
}
```

### Inspecting execution errors

Use `*dagger.ExecError` to access stderr from failed commands:

```go
func (m *MyModule) Test(ctx context.Context) (string, error) {
    out, err := dag.Container().
        From("golang:1.22").
        WithExec([]string{"go", "test", "./..."}).
        Stdout(ctx)
    if err != nil {
        var e *dagger.ExecError
        if errors.As(err, &e) {
            return "", fmt.Errorf("tests failed:\n%s", e.Stderr)
        }
        return "", err
    }
    return out, nil
}
```

### Continuing after errors

Use `Expect: dagger.ReturnTypeAny` to allow non-zero exit codes instead of treating them as errors:

```go
func (m *MyModule) Test(ctx context.Context) (*TestResult, error) {
    ctr, err := dag.Container().
        From("golang:1.22").
        WithDirectory("/src", m.Source).
        WithWorkdir("/src").
        WithExec([]string{"go", "test", "-json", "./..."}, dagger.ContainerWithExecOpts{
            Expect: dagger.ReturnTypeAny,
        }).
        Sync(ctx)
    if err != nil {
        return nil, err
    }

    exitCode, _ := ctr.ExitCode(ctx)
    return &TestResult{
        Report:   ctr.File("/src/test-report.json"),
        ExitCode: exitCode,
    }, nil
}
```

### Input validation

```go
func (m *MyModule) Divide(a, b int) (int, error) {
    if b == 0 {
        return 0, fmt.Errorf("division by zero")
    }
    return a / b, nil
}
```

---

## Lazy Evaluation and Sync

Dagger operations are lazy — nothing executes until a scalar value (string, int, bool) is resolved. Use `Sync(ctx)` to force evaluation when you need side effects without extracting a value:

```go
// Force execution: Sync returns the object after executing the pipeline
func (m *MyModule) Deploy(ctx context.Context) error {
    _, err := dag.Container().
        From("alpine").
        WithExec([]string{"deploy.sh"}).
        Sync(ctx)
    return err
}

// Lazy: just builds the pipeline, nothing executes yet
func (m *MyModule) BuildEnv(src *dagger.Directory) *dagger.Container {
    return dag.Container().
        From("golang:1.22").
        WithDirectory("/src", src).
        WithWorkdir("/src")
}
```

Avoid resolving intermediate values prematurely — let the DAG engine optimize the entire pipeline.

---

## Debugging

### Interactive terminal

Drop into a container's shell at any point in the pipeline using `Terminal()`:

```go
func (m *MyModule) Debug(src *dagger.Directory) *dagger.Container {
    return dag.Container().
        From("golang:1.22").
        WithDirectory("/src", src).
        WithWorkdir("/src").
        WithExec([]string{"go", "build", "./..."}).
        Terminal()  // drops into interactive shell here
}
```

You can chain multiple `Terminal()` calls to inspect the container at different stages. From the CLI, attach a terminal to any function's output:

```shell
dagger call build --src=. terminal
```

For directories, specify a container and shell:

```go
dir.Terminal(dagger.DirectoryTerminalOpts{
    Container: dag.Container().From("ubuntu"),
    Cmd:       []string{"/bin/bash"},
})
```

---

## Custom Telemetry

Add named spans to the Dagger UI for better observability:

```go
import "dagger/my-module/internal/telemetry"

func (m *MyModule) Deploy(ctx context.Context) (rerr error) {
    ctx, span := telemetry.Tracer().Start(ctx, "deploying to production")
    defer telemetry.End(span, func() error { return rerr })

    // ... deployment logic
    return nil
}
```

Named return values (`rerr`) are used with the deferred telemetry end to capture errors in the span.

---

## Documentation

Go comments on types and functions become API documentation:

```go
// MyModule provides CI/CD functions for Go projects.
type MyModule struct{}

// Build compiles the Go application and returns the build container.
// The container includes the compiled binary at /app.
func (m *MyModule) Build(
    ctx context.Context,
    // Source code directory containing go.mod
    src *dagger.Directory,
) *dagger.Container {
```

Module-level description (comment before `package main`). The first comment block becomes the short description; subsequent blocks are extended documentation:

```go
// A simple example module to say hello.

// Further documentation for the module here.

package main
```

These comments appear in `dagger functions`, `dagger call --help`, and on daggerverse.dev. Good documentation is essential for LLM integration — when modules are attached to an LLM environment, function comments become tool descriptions that the LLM uses to decide which tools to call and how.
