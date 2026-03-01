---
name: dagger-modules
description: >-
  Guide for creating Dagger modules, toolchains, and CI pipelines using the Go SDK.
  Covers module initialization, function design, type system, caching, services,
  secrets, LLM/agent integration, testing, and architecture patterns. Use this skill
  whenever the user wants to create or modify a Dagger module, build a toolchain,
  write Dagger Functions in Go, set up CI/CD with Dagger, or asks about Dagger
  patterns, best practices, or architecture. Also trigger when you see dagger.json,
  the dagger CLI, imports from "dagger/<module>/internal/dagger", or references to
  the `dag` client. Trigger on "dagger call", "dagger check", "dagger install",
  "dagger toolchain install", "daggerverse", or "dagger develop". Even if the user
  doesn't say "Dagger" explicitly, use this skill when the context involves
  containerized build pipelines, module-based CI, programmable CI/CD in Go, or
  building AI agents with Dagger's LLM type.
---

# Dagger Modules — Go SDK

This skill covers the full lifecycle of building Dagger modules in Go: initialization, function design, types, caching, services, secrets, LLM integration, testing, dependencies, toolchains, and publishing.

## Quick Reference

| Topic | Reference |
|-------|-----------|
| Go code patterns and examples | [go-patterns.md](references/go-patterns.md) |
| Architecture, types, caching, services, LLM | [architecture.md](references/architecture.md) |
| Common recipes (builds, containers, etc.) | [cookbook.md](references/cookbook.md) |

Read these reference files when you need detailed code examples or deeper guidance on a specific topic.

## Module Creation Workflow

### 1. Initialize

```shell
dagger init --sdk=go --name=my-module
```

This creates:
- `dagger.json` — module metadata and dependencies
- `main.go` — entry point with the main object
- `go.mod` / `go.sum` — standard Go module files

The main object name must match the module name in PascalCase. For `my-module`, the struct is `MyModule`.

### 2. Structure

```
my-module/
├── dagger.json
├── go.mod
├── go.sum
├── main.go          # Main object + functions
├── build.go         # Additional functions (same package)
└── internal/        # Generated code (do not edit)
    ├── dagger/
    └── telemetry/
```

Split across multiple files in the same package freely. Only the top-level package is part of the public API.

If a `go.work` file exists in the repository root, Dagger automatically adds new modules to it.

For sub-packages, import `dagger/<module>/internal/dagger` to access Dagger types. Since `dag` is only available in the main package, pass `*dagger.Client` as a parameter:

```go
package utils

import "dagger/my-module/internal/dagger"

func DoThing(client *dagger.Client) *dagger.Directory {
    return client.Container().From("golang:1.22").Directory("/src")
}
```

### 3. Write Functions

Every exported method on the main object becomes a Dagger Function. Read [go-patterns.md](references/go-patterns.md) for the full catalog of patterns.

The basic shape:

```go
type MyModule struct{}

func (m *MyModule) Build(ctx context.Context, src *dagger.Directory) *dagger.Container {
    return dag.Container().
        From("golang:1.22").
        WithDirectory("/src", src).
        WithWorkdir("/src").
        WithExec([]string{"go", "build", "-o", "app", "."})
}
```

### 4. Develop and Test

```shell
# Re-generate code after changing types/functions
dagger develop

# List available functions
dagger functions

# Call a function
dagger call build --src=.

# Call a function in a submodule (e.g., tests/)
dagger call -m tests all

# Interactive shell
dagger

# Debug: drop into container shell at any point
dagger call build --src=. terminal
```

Run `dagger develop` after adding new functions or changing signatures to regenerate the internal code.

### 5. Install Dependencies

```shell
# Add a module dependency
dagger install github.com/user/module@version

# Update a dependency
dagger update module-name

# Remove a dependency
dagger uninstall module-name

# Access via dag in code
dag.ModuleName().FunctionName(ctx)
```

## Key Design Principles

### Functions are the building blocks
Each function should do one thing well. Accept typed inputs, return typed outputs. Functions run in sandboxed containers — they have no implicit access to the host.

### Lazy evaluation
Dagger operations are lazy — nothing executes until a scalar value is resolved (via `ctx`). Build pipelines by chaining operations. Don't resolve intermediate values unless you need them. Use `Sync(ctx)` when you need to force execution for side effects without extracting a value.

### Immutability drives caching
Every Dagger operation takes immutable inputs and produces immutable outputs. This content-addressing enables automatic caching. Embrace the functional, pipeline-style approach.

### Chain, don't orchestrate
Instead of imperative step-by-step scripting, compose operations into pipelines using method chaining. The DAG engine handles execution order, parallelism, and caching.

### Constructor for module-wide config
Use a `New()` constructor to accept module-wide configuration (source directory, base image, flags). Store config in exported struct fields so it serializes correctly.

```go
func New(
    // +optional
    // +defaultPath="."
    // +ignore=["**/.git", "**/node_modules"]
    source *dagger.Directory,
) *MyModule {
    return &MyModule{Source: source}
}

type MyModule struct {
    Source *dagger.Directory
}
```

### Return the right type
- Return `*dagger.Container` when callers might chain further operations
- Return `*dagger.Directory` or `*dagger.File` for build artifacts
- Return `string` only for final output (stdout, computed values)
- Return `*MyModule` itself for builder-pattern chaining (`With*` methods)
- Return `error` alongside any type when operations can fail

## Function Arguments

Annotate arguments with Go comments to control their behavior:

```go
func (m *MyModule) Build(
    ctx context.Context,
    // Source code directory
    // +defaultPath="."
    // +ignore=["**/.git", "**/vendor"]
    source *dagger.Directory,
    // Go build tags
    // +optional
    tags string,
    // Target OS
    // +default="linux"
    goos string,
    // Base container image
    // +defaultAddress="golang:1.22"
    base *dagger.Container,
) *dagger.Container {
```

Key annotations:
- `// +optional` — argument is not required. When omitted, Go scalars receive their zero value (`""`, `0`, `false`); pointer/object types receive `nil`
- `// +default="value"` — default value for scalar arguments
- `// +defaultPath="."` — default path for Directory/File args. In git repos: absolute paths resolve from the repo root, relative paths from the `dagger.json` directory. Outside git repos: all paths resolve from the `dagger.json` directory
- `// +defaultAddress="image:tag"` — default image for Container args (e.g., `"alpine:latest"`, `"golang:1.22"`)
- `// +ignore=["pattern", ...]` — exclude files from Directory/File args using gitignore-style patterns. Supports negation (`!`) to re-include. Order matters: `["*", "!**/*.go"]` includes only Go files
- `// +private` — hide a struct field from the API (still serialized)

Supported argument types: `string`, `bool`, `int`, `float64`, `[]string`, `*dagger.Directory`, `*dagger.File`, `*dagger.Container`, `*dagger.Service`, `*dagger.Secret`, `*dagger.Socket`, enums, and custom types.

## Caching Strategy

### Function caching (result-level)
By default, function results are cached for 7 days. Control with a comment annotation above the function:

```go
// +cache="10m"
func (m *MyModule) FetchData(ctx context.Context) (string, error) {
```

Options: `"10s"`, `"10m"`, `"10h"` (TTL), `"session"` (current session only), `"never"` (always re-execute). Max 7 days, min 1 second.

Cache key = module source code + argument values + parent object values. Any change invalidates.

Function caching and layer caching are independent — even with `cache="never"`, unchanged internal steps (WithExec, etc.) still use layer caching.

Modules created before v0.19.4 default to `"session"` caching. After running `dagger develop`, a `"disableDefaultFunctionCaching": true` flag appears in `dagger.json`. Remove it after reviewing function caching needs to opt in to the new defaults.

### Cache volumes (filesystem-level)
Mount persistent caches for package managers and build caches:

```go
dag.Container().
    WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-mod")).
    WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
    WithMountedCache("/root/.cache/go-build", dag.CacheVolume("go-build")).
    WithEnvVariable("GOCACHE", "/root/.cache/go-build")
```

Cache volumes are scoped to the defining module by default. Pass them explicitly to share across modules.

### Cache invalidation
To bust a specific step's cache, inject a changing input:

```go
WithEnvVariable("CACHE_BUST", time.Now().String()).
WithExec([]string{"go", "build", "./..."})
```

See [architecture.md](references/architecture.md) for detailed caching guidance.

## Toolchains

Toolchains are modules designed for direct consumption without writing code. They provide ready-to-use functions and checks, allowing teams to add CI/CD capabilities with zero Dagger code.

```shell
# Users install your toolchain
dagger toolchain install github.com/you/my-toolchain

# And use it directly
dagger call my-toolchain build
dagger check
```

A good toolchain:
- Focuses on a single tool (eslint, pytest, golangci-lint) rather than being monolithic
- Uses `+defaultPath="."` on source arguments so it works without explicit args
- Provides check functions (annotated with `// +check`) that integrate with `dagger check`
- Includes sensible defaults with customization via optional arguments

### Writing check functions

Check functions must not require any arguments (but can accept optional arguments with defaults). Annotate with `// +check`. Checks can return `error` (pass/fail) or `*dagger.Container` (exit code determines result):

```go
// Lint runs golangci-lint on the source code
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

// Build verifies the project compiles (exit code determines pass/fail)
// +check
func (m *MyModule) Build() *dagger.Container {
    return dag.Container().
        From("golang:1.22").
        WithDirectory("/src", m.Source).
        WithWorkdir("/src").
        WithExec([]string{"go", "build", "./..."})
}
```

Checks from toolchains are namespaced: `dagger check my-toolchain:lint`. Run all checks from a toolchain with `dagger check my-toolchain:*`. List available checks with `dagger check -l`.

### Toolchain customization

Users customize toolchains in their `dagger.json`:

```json
{
  "toolchains": [
    {
      "source": "github.com/you/golangci-lint-toolchain@v1.0.0",
      "customizations": [
        {
          "function": ["lint"],
          "argument": "version",
          "default": "v1.58.0"
        }
      ],
      "ignoreChecks": ["experimental-*"]
    }
  ]
}
```

See [architecture.md](references/architecture.md) for full toolchain design patterns.

## Services

Turn any container with exposed ports into a service with `AsService()`, then bind it to other containers with `WithServiceBinding()`. Services start lazily, are health-checked, de-duplicated within a session, and stop automatically after a 10-second grace period.

```go
func (m *MyModule) Test(ctx context.Context) (string, error) {
    db := dag.Container().
        From("postgres:16").
        WithEnvVariable("POSTGRES_PASSWORD", "test").
        WithExposedPort(5432).
        AsService(dagger.ContainerAsServiceOpts{UseEntrypoint: true})

    return dag.Container().
        From("golang:1.22").
        WithServiceBinding("db", db).
        WithExec([]string{"go", "test", "./..."}).
        Stdout(ctx)
}
```

`AsService` options: `UseEntrypoint: true` runs the container's default entrypoint; `Args: []string{...}` specifies a custom command.

Key patterns:
- **Expose to host**: `dagger call my-service up --ports 8080:80`
- **Accept host services**: `dagger call test --db=tcp://localhost:5432`
- **Custom hostnames**: `svc.WithHostname("mydb")` for service-to-service communication
- **Explicit lifecycle**: `svc.Start(ctx)` / `svc.Stop(ctx)` for precise control
- **Endpoints**: `svc.Endpoint(ctx, dagger.ServiceEndpointOpts{Scheme: "http", Port: 8080})`
- **Persistent state**: mount cache volumes on service containers for data that should survive restarts

See [architecture.md](references/architecture.md) for advanced patterns (interdependent services, host services, persistent state, explicit lifecycle, endpoints).

## Secrets

Accept secrets as typed `*dagger.Secret` arguments — never as plain strings. Secrets are never written to logs or cached on disk. They are automatically scrubbed from stdout/stderr and scoped to the defining module.

Callers pass secrets via providers:

```shell
dagger call deploy --token=env://API_TOKEN
dagger call deploy --token=file://./token.txt
dagger call deploy --token=cmd://"vault read ..."
dagger call deploy --token=op://vault/item/field        # 1Password
dagger call deploy --token=vault://path/to/secret.item  # HashiCorp Vault
dagger call deploy --token=aws://prod/github/token      # AWS Secrets Manager
```

By default, secrets with identical plaintext share cache entries. For rotating secrets that should still share cache, use a `cacheKey`:

```shell
dagger call deploy --token=env://API_TOKEN?cacheKey=my-stable-key
```

Functions that return values referencing secrets created via `dag.SetSecret()` behave as session-scoped caching regardless of configured TTL.

See [architecture.md](references/architecture.md) and [cookbook.md](references/cookbook.md) for secret code patterns.

## LLM Integration

Dagger provides a native `LLM` type for building AI agents. Modules can use LLMs as tools, and LLMs can discover and call any Dagger Function. Function documentation is automatically provided to the LLM as tool descriptions.

```go
func (m *MyModule) GoProgram(assignment string) *dagger.Container {
    env := dag.Env().
        WithStringInput("assignment", assignment, "the assignment to complete").
        WithContainerInput("builder",
            dag.Container().From("golang").WithWorkdir("/app"),
            "a container to use for building Go code").
        WithContainerOutput("completed", "the completed assignment in the Golang container")

    work := dag.LLM().
        WithEnv(env).
        WithPrompt("You are an expert Go programmer. Your assignment is: $assignment")

    return work.Env().Output("completed").AsContainer()
}
```

### MCP integration

Modules can connect to external MCP servers and expose themselves as MCP servers:

```go
// Connect external MCP server to an LLM
mcpServer := dag.Container().
    From("golang").
    WithExec([]string{"go", "install", "github.com/isaacphi/mcp-language-server@latest"}).
    AsService(dagger.ContainerAsServiceOpts{
        Args: []string{"mcp-language-server", "--workspace", "/src"},
    })

work := dag.LLM().
    WithEnv(env).
    WithMCPServer("lsp", mcpServer).
    WithPrompt("your prompt")
```

Modules with no required constructor arguments can also be exposed as MCP servers for tools like Claude Desktop or Cursor: `dagger -m <module> mcp`

See [architecture.md](references/architecture.md) for the full LLM API.

## Testing Modules

Create a test module as a dependency:

```
my-module/
├── main.go
├── dagger.json
├── tests/
│   ├── main.go
│   └── dagger.json    # depends on parent module
└── examples/
    └── go/
        ├── main.go
        └── dagger.json  # testable examples (optional)
```

```shell
cd tests && dagger init --sdk=go --name=tests --source=. && dagger install ..
```

Run tests from the parent directory with `dagger call -m tests all`, or from within the tests directory with `dagger call all`.

Test functions are Dagger Functions that exercise the module under test:

```go
type Tests struct{}

func (t *Tests) TestBuild(ctx context.Context) error {
    src := dag.CurrentModule().Source()
    _, err := dag.MyModule(dagger.MyModuleOpts{Source: src}).
        Build().
        Sync(ctx)
    return err
}

// All runs all tests — conventional entry point
func (t *Tests) All(ctx context.Context) error {
    if err := t.TestBuild(ctx); err != nil { return err }
    return nil
}
```

For parallel test execution, use `github.com/sourcegraph/conc/pool` or `golang.org/x/sync/errgroup`:

```go
import "github.com/sourcegraph/conc/pool"

func (t *Tests) All(ctx context.Context) error {
    p := pool.New().WithErrors().WithContext(ctx)
    p.Go(t.TestBuild)
    p.Go(t.TestLint)
    p.Go(t.TestUnit)
    return p.Wait()
}
```

**Testable examples**: Create an `examples/go/` module that both showcases usage and acts as integration tests. These serve as documentation on daggerverse.dev and improve module discoverability.

## Publishing

1. Add a description to `dagger.json`
2. Document functions with Go comments (they become API docs)
3. Tag with semver: `git tag v1.0.0 && git push --tags`
4. First `dagger call` from someone indexes it on [daggerverse.dev](https://daggerverse.dev)

## Changesets

Functions can return a `*dagger.Changeset` to represent file modifications that should be applied back to the source directory. This is useful for code generation, formatting, and other source-modifying operations:

```go
func (m *MyModule) Generate() *dagger.Changeset {
    generated := dag.Container().
        From("golang:1.22").
        WithDirectory("/app", m.Source).
        WithWorkdir("/app").
        WithExec([]string{"go", "generate", "./..."}).
        Directory("/app")
    return generated.Changes(m.Source)
}
```

The CLI flag `-y` / `--auto-apply` automatically applies changesets to the source directory. Functions can also return `[]*dagger.Changeset` for multiple sets of changes.

## Dagger Shell

Dagger Shell provides an interactive environment for testing and debugging modules. Pipe syntax translates to function chaining:

```shell
# Interactive mode
dagger

# Inline command
dagger -c 'build --src=. | publish ttl.sh/my-app'

# Test a function
dagger -c 'lint'

# Enter prompt mode for natural language (type > in interactive shell)
> build the project and run tests
```

Key differences from `dagger call`:
- Pipe `|` chains function calls: `build | publish` means `build().publish()`
- Required args are positional, optional args use flags
- Shell variables: `ctr=$(container | from alpine)`
- Background jobs: `test & lint & .wait`

## CI Integration

Dagger pipelines run identically locally and in CI. Write pipeline logic in Dagger Functions, keep CI YAML minimal (just install Dagger and call `dagger check` or `dagger call`). See [architecture.md](references/architecture.md) for GitHub Actions and GitLab CI examples.

## Common Patterns Summary

| Pattern | When to Use | Reference |
|---------|-------------|-----------|
| Multi-stage build | Compile in one container, run in minimal image | [cookbook.md](references/cookbook.md) |
| Multi-platform build | Build for multiple OS/arch combinations | [cookbook.md](references/cookbook.md) |
| Builder pattern | `With*` methods returning `*MyModule` | [go-patterns.md](references/go-patterns.md) |
| `With()` composition | Reusable container modification functions | [go-patterns.md](references/go-patterns.md) |
| Concurrent execution | `errgroup`/`conc/pool` for parallel operations | [cookbook.md](references/cookbook.md) |
| Custom types | Group related data and functions | [go-patterns.md](references/go-patterns.md) |
| Service binding | Test against databases, APIs | [architecture.md](references/architecture.md) |
| Cache volumes | Speed up package manager installs | [architecture.md](references/architecture.md) |
| LLM agents | AI-powered code generation/review | [architecture.md](references/architecture.md) |
| Error inspection | Handle exec failures gracefully | [go-patterns.md](references/go-patterns.md) |
| Changesets | Return source modifications from functions | SKILL.md (above) |
| Mount vs copy | `WithMountedDirectory` (fast, temp) vs `WithDirectory` (persisted) | [cookbook.md](references/cookbook.md) |
| Minimal images | `WithRootfs` for scratch-like containers | [cookbook.md](references/cookbook.md) |
| Interactive debug | `Terminal()` for shell access mid-pipeline | [cookbook.md](references/cookbook.md) |
| Registry auth | `WithRegistryAuth` for private registries | [cookbook.md](references/cookbook.md) |
| Custom telemetry | `telemetry.Tracer()` for named spans in UI | [cookbook.md](references/cookbook.md) |
| MCP servers | Expose modules to Claude/Cursor or connect external MCP | [architecture.md](references/architecture.md) |
