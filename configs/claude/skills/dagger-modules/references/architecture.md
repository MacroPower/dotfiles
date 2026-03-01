# Architecture & Design Patterns

## Table of Contents

1. [Core Types](#core-types)
2. [The DAG Model](#the-dag-model)
3. [Caching In Depth](#caching-in-depth)
4. [Changesets](#changesets)
5. [Default Path Resolution](#default-path-resolution)
6. [Services](#services)
7. [Secrets](#secrets)
8. [Toolchain Design](#toolchain-design)
9. [Module Dependencies](#module-dependencies)
10. [LLM Integration](#llm-integration)
11. [Testing Strategy](#testing-strategy)
12. [Monorepo Patterns](#monorepo-patterns)
13. [Publishing & Versioning](#publishing--versioning)
14. [CI Integration](#ci-integration)
15. [Module Configuration](#module-configuration)

---

## Core Types

| Type | Purpose | Example |
|------|---------|---------|
| `Container` | OCI container image, chainable | `dag.Container().From("alpine")` |
| `Directory` | Filesystem directory reference | `dag.Directory()`, function args |
| `File` | Single file reference | `container.File("/app/binary")` |
| `Service` | Long-running network service | `container.AsService()` |
| `Secret` | Confidential data (never logged) | `dag.SetSecret("key", "value")` |
| `CacheVolume` | Persistent filesystem cache | `dag.CacheVolume("go-mod")` |
| `GitRepository` | Git repo reference | `dag.Git("https://github.com/...")` |
| `Socket` | Unix/TCP socket | SSH auth sockets |
| `LLM` | Large Language Model interface | `dag.LLM()` |
| `Env` | LLM environment with I/O | `dag.Env()` |
| `Changeset` | File modifications to apply to source | `dir.Changes(baseDir)` |

All types are immutable and content-addressed. Operations on them produce new instances rather than mutating state.

Additional utilities:
- `dag.HTTP(url)` — fetch a file via HTTP, returns `*dagger.File`
- `dag.Directory()` — create an empty directory to build up incrementally
- `dag.SetSecret(name, value)` — create a secret programmatically

Options structs follow the naming convention `dagger.TypeMethodOpts` (e.g., `dagger.ContainerAsServiceOpts`, `dagger.ContainerWithExecOpts`, `dagger.ServiceEndpointOpts`).

---

## The DAG Model

Dagger represents workflows as a Directed Acyclic Graph (DAG). Each operation is a node that takes immutable inputs and produces immutable outputs.

**Why this matters for module design:**
- Operations are lazy — nothing executes until a scalar value is resolved (via `ctx`)
- Identical operations with identical inputs are automatically deduplicated
- The DAG engine handles parallelism — you don't need to manage concurrency for independent operations
- Content addressing means changing any input invalidates downstream caches

**Practical implication:** Build pipelines by chaining operations. Don't resolve intermediate values unless you need them:

```go
// Good: single pipeline, engine optimizes execution
func (m *MyModule) Build(src *dagger.Directory) *dagger.Container {
    return dag.Container().
        From("golang:1.22").
        WithDirectory("/src", src).
        WithWorkdir("/src").
        WithExec([]string{"go", "build", "-o", "app"})
}

// Avoid: unnecessary intermediate resolution
func (m *MyModule) Build(ctx context.Context, src *dagger.Directory) (*dagger.Container, error) {
    ctr := dag.Container().From("golang:1.22")
    _, err := ctr.Stdout(ctx) // forces execution prematurely
    // ...
}
```

Use `Sync(ctx)` when you need to force execution for side effects without extracting a scalar:

```go
_, err := container.Sync(ctx)  // forces the pipeline to execute, returns the container
```

---

## Caching In Depth

### Function caching

Controls whether the entire function re-executes:

| Annotation | Behavior |
|-----------|----------|
| (default) | Cached for 7 days |
| `// +cache="10m"` | Cached for 10 minutes |
| `// +cache="session"` | Cached within current engine session only |
| `// +cache="never"` | Always re-executes |

Cache key = module source code + argument values + parent object values. Any change invalidates.

Duration format: integer + `"s"` (seconds), `"m"` (minutes), or `"h"` (hours). Max 7 days, min 1 second.

### Layer caching

Independent of function caching. Each `WithExec`, `WithFile`, `WithDirectory` etc. is individually cached by the container engine. Even if a function re-runs (`cache="never"`), unchanged internal steps still cache.

### Secrets and caching

Secrets are never cached on disk. Functions returning values that reference secrets created via `dag.SetSecret()` behave as "session" caching regardless of configured TTL.

By default, layer cache entries are keyed on the secret's plaintext value — different values invalidate cache. For rotating secrets that should share cache (e.g., frequently rotated tokens that are functionally equivalent), callers can set a stable `cacheKey`:

```shell
dagger call deploy --token=env://API_TOKEN?cacheKey=my-stable-key
```

### Cache volumes

Persist filesystem state across runs. Essential for package managers:

```go
// Go module + build cache
func (m *MyModule) buildEnv(src *dagger.Directory) *dagger.Container {
    return dag.Container().
        From("golang:1.22").
        WithDirectory("/src", src).
        WithWorkdir("/src").
        WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-mod")).
        WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
        WithMountedCache("/root/.cache/go-build", dag.CacheVolume("go-build")).
        WithEnvVariable("GOCACHE", "/root/.cache/go-build")
}
```

Common cache volume patterns:

| Tool | Cache Path | Volume Name | Env Var |
|------|-----------|-------------|---------|
| Go modules | `/go/pkg/mod` | `go-mod` | `GOMODCACHE` |
| Go build | `/root/.cache/go-build` | `go-build` | `GOCACHE` |
| npm | `/root/.npm` | `npm-cache` | |
| pip | `/root/.cache/pip` | `pip-cache` | |
| Maven | `/root/.m2` | `maven-cache` | |
| Cargo | `/root/.cargo` | `cargo-cache` | |

Cache volumes are scoped to the defining module by default. Pass them explicitly to share across modules.

### Cache invalidation

To bust a specific step's cache, inject a changing input:

```go
func (m *MyModule) FreshBuild(ctx context.Context, src *dagger.Directory) *dagger.Container {
    return dag.Container().
        From("golang:1.22").
        WithDirectory("/src", src).
        WithWorkdir("/src").
        WithEnvVariable("CACHE_BUST", time.Now().String()).
        WithExec([]string{"go", "build", "./..."})
}
```

### Backwards compatibility (pre-v0.19.4)

Modules created before v0.19.4 defaulted to `"session"` caching for all functions. After upgrading and running `dagger develop`, a flag appears in `dagger.json`:

```json
{
  "disableDefaultFunctionCaching": true
}
```

This preserves the old behavior. After reviewing each function's caching needs and adding appropriate `// +cache` annotations where necessary, remove this flag to opt in to the new 7-day default TTL.

---

## Services

### Creating services

Any container with exposed ports can become a service. Use `AsService()` options to control startup:

```go
// Using the container's default entrypoint
func (m *MyModule) Database() *dagger.Service {
    return dag.Container().
        From("postgres:16").
        WithEnvVariable("POSTGRES_PASSWORD", "test").
        WithEnvVariable("POSTGRES_DB", "testdb").
        WithExposedPort(5432).
        AsService(dagger.ContainerAsServiceOpts{UseEntrypoint: true})
}

// Using a custom command
func (m *MyModule) HttpServer() *dagger.Service {
    return dag.Container().
        From("python").
        WithWorkdir("/srv").
        WithNewFile("index.html", "Hello, world!").
        WithExposedPort(8080).
        AsService(dagger.ContainerAsServiceOpts{
            Args: []string{"python", "-m", "http.server", "8080"},
        })
}
```

### Binding services

Use `WithServiceBinding` to make a service accessible from another container:

```go
func (m *MyModule) Test(ctx context.Context, src *dagger.Directory) (string, error) {
    db := m.Database()
    return dag.Container().
        From("golang:1.22").
        WithDirectory("/src", src).
        WithWorkdir("/src").
        WithServiceBinding("db", db).
        WithEnvVariable("DATABASE_URL", "postgres://postgres:test@db:5432/testdb").
        WithExec([]string{"go", "test", "./..."}).
        Stdout(ctx)
}
```

### Service endpoints

Get the service URL programmatically:

```go
func (m *MyModule) Fetch(ctx context.Context) (string, error) {
    svc := m.HttpServer()
    endpoint, err := svc.Endpoint(ctx, dagger.ServiceEndpointOpts{
        Scheme: "http",
        Port:   8080,
    })
    if err != nil {
        return "", err
    }
    return dag.HTTP(endpoint).Contents(ctx)
}
```

### Exposing to host

```shell
dagger call database up --ports 5432:5432
```

### Accepting host services

Functions can receive services already running on the host as arguments:

```go
func (m *MyModule) UserList(ctx context.Context, svc *dagger.Service) (string, error) {
    return dag.Container().
        From("mariadb:10.11.2").
        WithServiceBinding("db", svc).
        WithExec([]string{"mysql", "--user=root", "--password=secret", "--host=db", "-e", "SELECT Host, User FROM mysql.user"}).
        Stdout(ctx)
}
```

Callers pass host services as `tcp://HOST:PORT` or `udp://HOST:PORT`:
```shell
dagger call user-list --svc=tcp://localhost:3306
```

### Interdependent services

Assign custom hostnames for service-to-service communication:

```go
func (m *MyModule) Services(ctx context.Context) (*dagger.Service, error) {
    svcA := dag.Container().From("nginx").
        WithExposedPort(80).
        AsService(dagger.ContainerAsServiceOpts{UseEntrypoint: true}).
        WithHostname("svca")

    svcB := dag.Container().From("nginx").
        WithServiceBinding("svca", svcA).
        WithExposedPort(80).
        AsService(dagger.ContainerAsServiceOpts{UseEntrypoint: true}).
        WithHostname("svcb")

    // Start explicitly when services reference each other
    svcA, err := svcA.Start(ctx)
    if err != nil { return nil, err }
    svcB, err = svcB.Start(ctx)
    if err != nil { return nil, err }

    return svcB, nil
}
```

### Service lifecycle

- Started lazily when a dependent container runs
- Health-checked before clients execute
- De-duplicated within a session (same service definition = same instance)
- Stopped after a 10-second grace period when no longer referenced
- Use `Start(ctx)` / `Stop(ctx)` for explicit lifecycle control:

```go
svc, err := svc.Start(ctx)
if err != nil { return err }
defer svc.Stop(ctx)
```

### Persisting service state

Use cache volumes for service data that should survive restarts:

```go
func (m *MyModule) Redis() *dagger.Service {
    return dag.Container().
        From("redis:7").
        WithMountedCache("/data", dag.CacheVolume("redis-data")).
        WithExposedPort(6379).
        AsService(dagger.ContainerAsServiceOpts{UseEntrypoint: true})
}
```

---

## Secrets

### Accepting secrets

```go
func (m *MyModule) Publish(
    ctx context.Context,
    // Registry authentication token
    token *dagger.Secret,
    image *dagger.Container,
) (string, error) {
    return image.
        WithRegistryAuth("ghcr.io", "user", token).
        Publish(ctx, "ghcr.io/user/app:latest")
}
```

### Secret as environment variable

```go
dag.Container().
    WithSecretVariable("API_TOKEN", token)
```

### Secret as mounted file

```go
dag.Container().
    WithMountedSecret("/run/secrets/key", keyFile)
```

### Creating secrets programmatically

```go
secret := dag.SetSecret("my-secret", "the-value")
```

### How users pass secrets

```shell
dagger call publish --token=env://GITHUB_TOKEN       # from environment variable
dagger call publish --token=file://~/.token           # from file
dagger call publish --token=cmd://"vault read ..."    # from command output
dagger call publish --token=op://vault/item/field     # 1Password
dagger call publish --token=vault://path/to/secret    # HashiCorp Vault
dagger call publish --token=aws://prod/github/token   # AWS Secrets Manager
```

### Registry authentication

```go
func (m *MyModule) Publish(
    ctx context.Context,
    ctr *dagger.Container,
    token *dagger.Secret,
) (string, error) {
    return ctr.
        WithRegistryAuth("ghcr.io", "username", token).
        Publish(ctx, "ghcr.io/user/app:latest")
}
```

### Secrets in Dockerfile builds

```go
func (m *MyModule) Build(ctx context.Context, src *dagger.Directory, token *dagger.Secret) *dagger.Container {
    // Create a named secret for the Dockerfile RUN --mount=type=secret,id=gh-token
    val, _ := token.Plaintext(ctx)
    buildSecret := dag.SetSecret("gh-token", val)
    return src.DockerBuild(dagger.DirectoryDockerBuildOpts{
        Secrets: []*dagger.Secret{buildSecret},
    })
}
```

The secret name (`gh-token`) must match the `--mount=type=secret,id=gh-token` in the Dockerfile.

### Security guarantees

- Secrets are never logged or cached on disk
- Automatically scrubbed from stdout/stderr output
- Scoped to the defining module — must be explicitly passed to other modules

---

## Changesets

Functions can return `*dagger.Changeset` (or `[]*dagger.Changeset`) to represent file modifications that should be applied to the source directory. The `Directory.Changes()` method computes the diff between two directories:

```go
// Generate runs go generate and returns changes to apply
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

When a function returns a changeset, the CLI prompts the user to apply it. Use `-y` / `--auto-apply` to apply automatically (useful in CI). Functions can return an array of changesets for multiple independent sets of changes.

---

## Default Path Resolution

How `+defaultPath` resolves depends on whether the module is in a git repository:

**Git repositories:**
| Default path | Context directory | Resolved path |
|---|---|---|
| `/` | Repository root (`/`) | `/` |
| `/src` | Repository root (`/`) | `/src` |
| `.` | `dagger.json` directory (`/my-module`) | `/my-module` |
| `..` | `dagger.json` directory (`/my-module`) | `/` |

**Non-git directories:**
| Default path | Context directory | Resolved path |
|---|---|---|
| `/` | `dagger.json` directory (`/my-module`) | `/my-module` |
| `.` | `dagger.json` directory (`/my-module`) | `/my-module` |

The key distinction: in git repos, absolute paths resolve from the repo root; relative paths resolve from the `dagger.json` directory. Outside git repos, all paths resolve from the `dagger.json` directory.

---

## Toolchain Design

A toolchain is a module optimized for zero-code consumption. Instead of importing it, users install it and call functions directly.

### Design principles

1. **Single-tool focus** — one toolchain per tool (golangci-lint, not "Go toolchain"). This enables independent updates, tool-specific customization, and mixing across language stacks
2. **Default paths** — use `+defaultPath` so source arguments resolve automatically
3. **Checks** — expose validation functions that integrate with `dagger check`
4. **Sensible defaults** — work out of the box, customize via optional args

### Example toolchain

```go
func New(
    // +defaultPath="."
    // +ignore=["**/.git", "**/vendor"]
    source *dagger.Directory,
) *GolangciLint {
    return &GolangciLint{Source: source}
}

type GolangciLint struct {
    Source *dagger.Directory
}

// Lint runs golangci-lint on the source code
// +check
func (m *GolangciLint) Lint(
    ctx context.Context,
    // +optional
    // +default="v1.57.2"
    version string,
) (string, error) {
    return dag.Container().
        From("golangci/golangci-lint:" + version).
        WithDirectory("/src", m.Source).
        WithWorkdir("/src").
        WithMountedCache("/root/.cache/golangci-lint", dag.CacheVolume("golangci-lint")).
        WithExec([]string{"golangci-lint", "run"}).
        Stdout(ctx)
}
```

### Check functions

Check functions are the key integration point with `dagger check`:
- Annotate with `// +check` above the function
- Must not require any arguments (optional args with defaults are fine)
- Can return `error` (pass/fail), `*dagger.Container` (exit code determines result), or `void` (no return)
- Checks execute in parallel for maximum performance
- Checks from toolchains are namespaced: `dagger check golangci-lint:lint`
- Pattern-based filtering: `dagger check lint-*`, `dagger check golangci-lint:*`
- List checks: `dagger check -l`

### Installation and usage

```shell
# Install
dagger toolchain install github.com/you/golangci-lint-toolchain@v1.0.0

# Custom name
dagger toolchain install github.com/you/toolchain --name lint

# Use
dagger call lint lint
dagger check          # runs all checks from all toolchains
dagger check lint:*   # runs checks from "lint" toolchain only
```

### Customization in dagger.json

Users can customize toolchains without forking:

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
        },
        {
          "argument": "source",
          "defaultPath": "/my-subdirectory"
        }
      ],
      "ignoreChecks": ["experimental-*"]
    }
  ]
}
```

Three customization mechanisms:
1. **Override argument defaults** — change default values for function arguments
2. **Override `defaultPath`** — redirect source directory for functions using `+defaultPath`
3. **Ignore checks** — skip specific checks by name (supports glob patterns, scoped to the toolchain)

---

## Module Dependencies

### Installing

```shell
dagger install github.com/user/module@v1.0.0
```

### Using in code

Dependencies are accessed via `dag`:

```go
func (m *MyModule) Build(ctx context.Context, src *dagger.Directory) *dagger.Directory {
    return dag.Golang().Build(src)
}
```

### Local dependencies

```shell
dagger install ./path/to/local-module
```

Both modules must be in the same git repository.

### Managing dependencies

```shell
dagger install github.com/user/module@v1.0.0  # install or update to specific version
dagger update module-name                       # update to latest
dagger uninstall module-name                    # remove
```

Module references use the format `[proto://]host/repo[/subpath][@version]`.

---

## LLM Integration

Dagger provides a native `LLM` type for building AI agents. Modules can attach functions as tools for the LLM, and configure structured inputs/outputs through environments.

### Environment pattern

The `Env` type configures typed inputs and outputs for the LLM. Module functions attached through the environment become tools the LLM can call — their Go doc comments serve as tool descriptions.

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
        WithPrompt(`
            You are an expert Go programmer with an assignment.
            Create files in the default directory in $builder
            Always build the code to make sure it is valid
            Do not stop until your assignment is completed and the code builds
            Your assignment is: $assignment
        `)

    return work.Env().Output("completed").AsContainer()
}
```

### Input/Output types

```go
// String I/O
dag.Env().
    WithStringInput("name", value, "description").
    WithStringOutput("result", "description")

// Container I/O
dag.Env().
    WithContainerInput("builder", container, "description").
    WithContainerOutput("result", "description")

// Directory I/O
dag.Env().
    WithDirectoryInput("source", dir, "description").
    WithDirectoryOutput("output", "description")

// File I/O
dag.Env().
    WithFileInput("config", file, "description").
    WithFileOutput("report", "description")

// Custom object I/O (any module type)
dag.Env().
    WithCustomInput("tool", obj, "description").
    WithCustomOutput("result", "description")
```

### Retrieving outputs

```go
llm := dag.LLM().WithEnv(env).WithPrompt("...")

// String output
result, err := llm.Env().Output("result").AsString(ctx)

// Container output
ctr := llm.Env().Output("result").AsContainer()

// Directory output
dir := llm.Env().Output("output").AsDirectory()

// File output
file := llm.Env().Output("report").AsFile()
```

### LLM API methods

| Method | Description |
|--------|-------------|
| `dag.LLM()` | Create a new LLM instance |
| `.WithModel(model)` | Set the model (e.g., `"claude-sonnet-4-5"`, `"gpt-4o"`) |
| `.WithPrompt(text)` | Append a prompt (variables use `$name` syntax) |
| `.WithPromptFile(file)` | Append a prompt read from a `*dagger.File` |
| `.WithPromptVar(name, value)` | Add a string variable to the LLM context |
| `.WithEnv(env)` | Attach an environment with inputs/outputs |
| `.WithMCPServer(name, service)` | Connect to an external MCP server (takes a name and `*dagger.Service`) |
| `.LastReply(ctx)` | Get the last text reply |
| `.History(ctx)` | Get conversation history |
| `.Model(ctx)` | Get the current model name |
| `.Env()` | Get the current environment |

### MCP integration

Dagger supports the Model Context Protocol (MCP) in two directions:

**Expose modules as MCP servers** — Modules with no required constructor arguments can be exposed as MCP servers for tools like Claude Desktop, Cursor, or Goose:
```shell
dagger -m github.com/user/module mcp
```

**Connect to external MCP servers** — Attach MCP servers to an LLM as services:
```go
mcpServer := dag.Container().
    From("golang").
    WithExec([]string{"go", "install", "golang.org/x/tools/gopls@latest"}).
    WithExec([]string{"go", "install", "github.com/isaacphi/mcp-language-server@latest"}).
    WithWorkdir("/src").
    AsService(dagger.ContainerAsServiceOpts{
        Args: []string{"mcp-language-server", "--workspace", "/src", "--lsp", "gopls"},
    })

work := dag.LLM().
    WithEnv(env).
    WithMCPServer("lsp", mcpServer).
    WithPrompt("your prompt")
```

### Key concepts

- Module functions become LLM tools automatically — inline documentation serves as tool descriptions, so write clear function comments
- The LLM runs an internal agent loop, calling tools and iterating until complete
- Prompt variables use `$variablename` syntax
- Rate limit detection with automatic retry and exponential backoff

---

## Testing Strategy

### Test module pattern

Create a `tests/` subdirectory with its own `dagger.json` that depends on the parent module:

```shell
cd my-module/tests
dagger init --sdk=go --name=tests --source=.
dagger install ../
```

```go
// tests/main.go
package main

import (
    "context"
    "dagger/tests/internal/dagger"
)

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
    if err := t.TestBuild(ctx); err != nil {
        return err
    }
    return nil
}
```

### Running tests

```shell
# From the parent module directory
dagger call -m tests all

# Or from inside the tests directory
cd tests && dagger call all
```

### Testing with services

```go
func (t *Tests) TestWithDB(ctx context.Context) error {
    db := dag.Container().
        From("postgres:16").
        WithEnvVariable("POSTGRES_PASSWORD", "test").
        WithExposedPort(5432).
        AsService(dagger.ContainerAsServiceOpts{UseEntrypoint: true})

    _, err := dag.MyModule().
        Test(dagger.MyModuleTestOpts{Db: db}).
        Sync(ctx)
    return err
}
```

### Parallel test execution

Using `github.com/sourcegraph/conc/pool` (recommended — simpler API than errgroup):

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

Or with `golang.org/x/sync/errgroup`:

```go
import "golang.org/x/sync/errgroup"

func (t *Tests) All(ctx context.Context) error {
    eg, gctx := errgroup.WithContext(ctx)
    eg.Go(func() error { return t.TestBuild(gctx) })
    eg.Go(func() error { return t.TestLint(gctx) })
    eg.Go(func() error { return t.TestUnit(gctx) })
    return eg.Wait()
}
```

### Testable examples

Create an `examples/go/` module that both showcases usage and acts as integration tests. Example functions serve as documentation on daggerverse.dev and improve module discoverability:

```shell
mkdir -p examples/go && cd examples/go
dagger init --name=examples --sdk=go --source=.
dagger install ../..
```

```go
// examples/go/main.go
type Examples struct{}

func (e *Examples) MyModuleBuild(ctx context.Context) error {
    _, err := dag.MyModule().Build().Sync(ctx)
    return err
}
```

---

## Monorepo Patterns

### Top-level orchestrator

```
repo/
├── dagger.json           # root module (orchestrator)
├── services/
│   ├── api/
│   │   ├── dagger.json   # API module
│   │   └── main.go
│   └── web/
│       ├── dagger.json   # Web module
│       └── main.go
└── shared/
    ├── dagger.json       # Shared utilities module
    └── main.go
```

The root module installs service modules as dependencies and orchestrates cross-service operations.

### Shared build patterns

Extract common build logic into a shared module that other modules depend on:

```go
// shared/main.go
type Shared struct{}

func (s *Shared) GolangBuildEnv(src *dagger.Directory) *dagger.Container {
    return dag.Container().
        From("golang:1.22").
        WithDirectory("/src", src).
        WithWorkdir("/src").
        WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-mod")).
        WithMountedCache("/root/.cache/go-build", dag.CacheVolume("go-build"))
}
```

Even if unnecessary CI jobs trigger in a monorepo, Dagger's layer cache means most finish nearly instantly. CI event filters are a secondary optimization.

---

## Publishing & Versioning

1. **Set module description** in `dagger.json`:
   ```json
   { "name": "my-module", "description": "Build and test Go applications" }
   ```

2. **Document all functions** with Go comments — they become API docs

3. **Use semantic versioning**: `git tag v1.0.0 && git push --tags`

4. **Auto-indexed** on daggerverse.dev on first remote use

5. **Create examples** as separate modules in an `examples/` directory — they serve as both documentation and integration tests

---

## CI Integration

### GitHub Actions

```yaml
name: CI
on: [push]
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: dagger/dagger-for-github@v8
      - run: dagger check
```

### GitLab CI

```yaml
test:
  image: alpine:latest
  services:
    - docker:dind
  before_script:
    - apk add curl
    - curl -fsSL https://dl.dagger.io/dagger/install.sh | BIN_DIR=/usr/local/bin sh
  script:
    - dagger check
```

### Key principle

Write your pipeline logic in Dagger Functions, not in CI YAML. The CI config should be minimal — just install Dagger and call `dagger check` or `dagger call`. This makes pipelines portable across any CI system and testable locally.

---

## Module Configuration

### dagger.json

The central configuration file, auto-generated by `dagger init`/`dagger develop`:

```json
{
  "name": "my-module",
  "sdk": "go",
  "description": "Build and test Go applications",
  "dependencies": [],
  "toolchains": []
}
```

### File/directory filters

The `include` field specifies additional files/dirs to include or exclude when loading the module itself (not function arguments — those use `+ignore`). By default, only `dagger.json` and files under the source directory are included. Supports `!` prefix for exclusion:

```json
{
  "include": ["**/*.go", "go.mod", "go.sum", "!**/*_test.go"]
}
```

Adding `!` exclusion patterns is useful to avoid uploading large cache or generated files that the Dagger Engine doesn't need.

### Go workspaces

If a `go.work` file exists in the repository root, Dagger automatically adds new modules to it. This enables seamless IDE support and cross-module imports within the same repo.

### Private Go modules

Configure access to private Go modules via `goprivate` in the SDK config section of `dagger.json`:

```json
{
  "sdk": {
    "config": {
      "goprivate": "github.com/myorg/private-repo"
    }
  }
}
```

Multiple URLs can be comma-separated. The repository name is optional (prefix matching works). Requires `.gitconfig` entry:
```
git config --global url."git@github.com:".insteadOf "https://github.com/"
```

Note: Go vendor mode is not currently supported by Dagger.
