---
name: dagger-modules
description: >-
  ALWAYS load BEFORE creating or editing Dagger modules, Dagger Functions, or
  Dagger-based CI pipelines.
---

# Dagger Modules — Go SDK

This skill covers the full lifecycle of building Dagger modules in Go: initialization, function design, types, caching, services, secrets, LLM integration, testing, dependencies, toolchains, and publishing.

This skill targets the Dagger **v1.0.0-beta.25** CLI, and each command in it is a command of that version. Dagger 1.0 splits the `dagger.json` of Dagger 0.x into two config files, and this skill teaches those two files.

| File | Role |
| --- | --- |
| `dagger.toml` | Workspace config: the modules that a repository installs, their settings, and its SDKs |
| `dagger-module.toml` | Manifest of one module: its name, engine version, runtime, and dependencies |

Dagger 1.0 also runs a project that keeps its 0.x `dagger.json`. But in that project, the commands that install or remove modules, change the dependencies of a module, or create a module fail until `dagger workspace migrate` converts the project. Read [Dagger 0.x](#dagger-0x) before you change a project that holds a `dagger.json`.

Dagger detects a coding agent from its environment. Under an agent, the commands in this list run only when a flag says what to do with the changes that they return. `-y` (`--auto-apply`) applies the changes, and `--no-apply` shows them and writes nothing.

- `dagger generate`
- `dagger module init`
- `dagger workspace migrate` and `dagger module migrate`
- `dagger module client add`, `update`, and `rm`, which take `-y` and not `--no-apply`

`dagger call` and `dagger functions` are the 0.x names of `dagger api call` and `dagger api functions`. Dagger 1.0 runs both names, and `dagger --help` lists only the `dagger api` names.

The Go SDK is a Dagger module, `dagger.io/sdk/go`, that a workspace installs. The `go.mod` that the SDK generates requires **Go 1.26+**.

## Quick Reference

| Topic | Reference |
| --- | --- |
| Go code patterns and examples | [go-patterns.md](references/go-patterns.md) |
| Architecture, types, caching, services, LLM | [architecture.md](references/architecture.md) |
| Common recipes (builds, containers, etc.) | [cookbook.md](references/cookbook.md) |

Read these reference files when you need detailed code examples or deeper guidance on a specific topic.

## Module Creation Workflow

### 1. Initialize

Run these commands inside a Git repository.

```shell
# Once per workspace: install the Go SDK
dagger module install dagger.io/sdk/go

# Create the module
dagger module init go --name my-module -y
```

`dagger module install` records the SDK in `dagger.toml`, and it creates that file when the repository has none. `dagger init` creates only `dagger.toml`, so it does not create a module.

`dagger module init` creates the module in `.dagger/modules/my-module`, beside `dagger.toml`, and installs it in the workspace under the name `my-module`. It writes these files:
- `dagger-module.toml`: the module manifest
- `main.go`: entry point with the main object
- `go.mod` and `go.sum`: standard Go module files
- `dagger.gen.go` and `internal/`: generated bindings

With `--path <dir>`, the command creates the module in `<dir>`, relative to the current directory, and does not install it in the workspace. Add `--install` to install it, or `--main` to install it as the workspace main.

The main object name must match the module name in PascalCase. For `my-module`, the struct is `MyModule`.

### 2. Structure

```
.dagger/modules/my-module/
├── dagger-module.toml
├── go.mod
├── go.sum
├── main.go          # Main object + functions
├── build.go         # Additional functions (same package)
├── dagger.gen.go    # Generated code (do not edit)
└── internal/        # Generated code (do not edit)
    └── dagger/      # dagger.gen.go, core/, and one <dep>.gen.go per dependency
```

Commit the generated code. Dagger loads a module that has a `dagger-module.toml` from its committed bindings, so the module fails to load without them.

Each installed dependency gets its own kebab-cased bindings file in `internal/dagger/` (for example, `my-dep.gen.go`), which keeps the diff of `dagger generate` small when dependencies change.

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

A minimal module:

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
# Regenerate the bindings, and run every // +generate function of the workspace
dagger generate -y

# List the functions of an installed module
dagger functions my-module

# Call a function of an installed module
dagger call my-module build --src=.

# Call a function of a module that the workspace does not install, such as tests/
dagger call -m tests all

# Debug: drop into container shell at any point
dagger call my-module build --src=. terminal
```

Run `dagger generate -y` after you add a function or change a signature. Commit the regenerated bindings with the change.

`dagger call` and `dagger functions` take the install name of a module before the function name. The functions of the workspace main module take no module name, and the functions of a module that `-m <path>` loads take none either. The other examples in this skill leave the module name out.

### 5. Install Dependencies

Run the `dagger module client` commands from the module directory.

```shell
# Add a module dependency
dagger module client add github.com/user/module@version --sdk=go -y

# List the dependencies, with the TARGET that rm takes
dagger module client list --sdk=go

# Update the dependencies
dagger module client update --sdk=go -y

# Remove a dependency
dagger module client rm <TARGET> --sdk=go -y

# Access via dag in code
dag.ModuleName().FunctionName(ctx)
```

Each `add`, `update`, and `rm` regenerates the module, and the SDK then writes the `[[dependencies]]` entries of `dagger-module.toml`. The next `dagger generate` replaces a manual edit of those entries, so do not edit them by hand.

`dagger install <ref>` and `dagger uninstall <name>` do not change a module. They add a module to the workspace and remove one, in `dagger.toml`. `dagger module update <name>` refreshes the version of an installed module, and `dagger lock update` refreshes the pins in `dagger.lock`.

## Dagger 0.x

Dagger 1.0 removed or changed several commands of Dagger 0.x, and it replaced `dagger.json`. This section maps each of those commands to its 1.0 form, and it covers a project that keeps its `dagger.json`.

### Commands

Do not run a command from the left column with the 1.0 CLI. It fails, or it does something else.

| Dagger 0.x | Dagger 1.0 |
| --- | --- |
| `dagger init --sdk=go --name=<name>` creates a module | `dagger module init go --name <name>` creates a module. `dagger init` rejects the `--sdk` and `--name` flags and creates only `dagger.toml`. |
| `dagger develop` regenerates the module code | Removed. `dagger generate` regenerates the bindings. |
| `dagger install <ref>` adds a dependency to a module | `dagger module client add <ref> --sdk=go` adds a dependency. `dagger install <ref>` installs a module in the workspace. |
| `dagger uninstall <name>` removes a dependency | `dagger module client rm <TARGET> --sdk=go` removes a dependency. `dagger uninstall <name>` removes a module from the workspace. |
| `dagger update <name>` updates a dependency | Fails. `dagger module client update` updates the dependencies of a module, `dagger module update` updates an installed module, and `dagger lock update` refreshes `dagger.lock`. |
| `dagger toolchain install <ref>` | Removed. `dagger install <ref>` installs the module in the workspace. |
| `dagger up` starts the services of the functions marked `// +up` | `dagger start` starts the service of every function that returns a `*dagger.Service` and does not require an argument, with or without the pragma. `dagger up` is a hidden alias of `dagger start`. |
| `dagger` with no arguments starts the interactive shell | Prints usage. `dagger -c '...'` and `dagger <file>` run Dagger Shell scripts. |

### Projects

A 0.x project holds a `dagger.json` and no `dagger.toml`. Dagger 1.0 runs the project without a migration, and it prints this notice:

```text
No workspace config found, inferring from dagger.json.
Run 'dagger workspace migrate' when ready. More info: https://docs.dagger.io/reference/upgrade-to-workspaces
```

The commands of this skill behave as follows in a 0.x project:

- `dagger call`, `dagger functions`, `dagger check`, `dagger generate`, and `dagger start` work. Dagger loads the modules and the toolchains from `dagger.json`.
- Dagger regenerates the bindings of a `dagger.json` module each time it loads the module. Changed functions do not need a generate step, and the `.gitignore` of the module keeps the generated files out of git.
- The commands that install or remove modules, or that change the dependencies of a module, fail with `workspace is using legacy dagger.json config; run dagger workspace migrate first`. `dagger install`, `dagger uninstall`, `dagger module install`, and `dagger module client add` are among them. `dagger module init go` fails with `unknown command "go" for "dagger module init"`, because the CLI reads the SDKs of `dagger module init` from `dagger.toml`.
- `dagger workspace migrate` works, and it writes `dagger.toml` and the `dagger-module.toml` of each module that it converts. [Migration](#migration) says when to run it.
- `dagger module migrate` runs for a module whose `dagger.json` names an SDK and no toolchains or blueprint, and it writes the `dagger-module.toml` of that module. But in a project that has no `dagger.toml`, the command registers no SDK for the module, and `dagger generate` generates bindings only for the SDKs that `dagger.toml` registers. So the converted module fails to load until its bindings exist, and no command can generate them. Run `dagger module migrate` only after `dagger workspace migrate`.
- To add a dependency or a toolchain, edit `dagger.json`. A module lists its dependencies under `dependencies`, and the project lists its toolchains under `toolchains`. Each entry holds a `name` and a `source`.
- No 1.0 command creates a module in a 0.x project. To add one, copy `dagger.json`, `go.mod`, `go.sum`, `.gitignore`, and `main.go` from an existing module of the project. Write the new name into the `name` of `dagger.json`, the module path `dagger/<name>` of `go.mod`, the import of `dagger/<name>/internal/dagger` in `main.go`, and the main object. Then list the new module in the `dependencies` or the `toolchains` of the module or the project that uses it. For a test module, list the module under test in the `dependencies` of the test module instead.

### Migration

You should migrate a 0.x project only when the user asks for the migration. The migration replaces the `dagger.json` of the project and of each local module that the project installs. So it does not belong in a change that has another purpose.

1. Preview the migration with `dagger workspace migrate --no-apply`.
2. Apply it with `dagger workspace migrate -y`.
3. Generate the bindings with `dagger generate -y`. A migrated module fails to load until its bindings exist.
4. Commit `dagger.toml`, each `dagger-module.toml`, the generated code, and `dagger.lock`.

The migration installs each toolchain in `dagger.toml` as a module, and it removes the generated files from the `.gitignore` of each module. With `-y`, it skips each `dagger.json` module that the project does not install, such as a test module. After the workspace migration, `dagger module migrate <path> -y` converts one of those skipped modules and registers its SDK scope in `dagger.toml`. Then run `dagger generate -y`.

When the source of a module is the repository root, the migration converts its `dagger.json` in place and does not install the module in `dagger.toml`. Plain `dagger call` then stops reaching the functions of the module, and the migration explains the change in `.dagger/migration-report.md`. Load the module with `-m .`, as in `dagger call -m . <function>`.

## Key Design Principles

### Functions are the building blocks
Each function should do one thing well. Accept typed inputs, return typed outputs. Functions run in sandboxed containers — they have no implicit access to the host.

### Lazy evaluation
Dagger operations are lazy — nothing executes until a scalar value is resolved (via `ctx`). Build pipelines by chaining operations. Resolve intermediate values only when you need them. Use `Sync(ctx)` when you need to force execution for side effects without extracting a value.

### Immutability drives caching
Every Dagger operation takes immutable inputs and produces immutable outputs. This content-addressing enables automatic caching. Embrace the functional, pipeline-style approach.

### Chain, don't orchestrate
Instead of imperative step-by-step scripting, compose operations into pipelines using method chaining. The DAG engine handles execution order, parallelism, and caching.

### Constructor for module-wide config
Use a `New()` constructor to accept module-wide configuration (source directory, base image, flags). Store config in exported struct fields so it serializes correctly.

```go
func New(
    // Project source directory, which defaults to the repository root
    // +optional
    // +defaultPath="/"
    // +ignore=["**/.git", "**/node_modules"]
    source *dagger.Directory,
) *MyModule {
    return &MyModule{Source: source}
}

type MyModule struct {
    Source *dagger.Directory
}
```

`dagger module init` puts the module in `.dagger/modules/<name>`. So in a Git repository, `+defaultPath="."` names the module directory, and `+defaultPath="/"` names the repository root. A module that another repository installs reads the project through a `*dagger.Workspace` argument instead, because its `/` names the root of its own repository. [Toolchain Design](references/architecture.md#toolchain-design) shows an example.

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
    // Source code directory, which defaults to the repository root
    // +defaultPath="/"
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
- `// +optional`: argument is not required. When omitted, Go scalars receive their zero value (`""`, `0`, `false`), and pointer and object types receive `nil`
- `// +default="value"`: default value for scalar arguments
- `// +defaultPath="."`: default path for Directory/File args. In git repos: absolute paths resolve from the repo root, relative paths from the directory of the module config file. Outside git repos: all paths resolve from the directory of the module config file
- `// +defaultAddress="image:tag"`: default image for Container args (for example, `"alpine:latest"` or `"golang:1.22"`)
- `// +ignore=["pattern", ...]`: exclude files from Directory/File args using gitignore-style patterns. Supports negation (`!`) to re-include. Order matters: `["*", "!**/*.go"]` includes only Go files
- `// +private`: hide a struct field from the API (still serialized)

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

Modules whose `engineVersion` is lower than v0.19.4 default to `"session"` caching. To keep that default when the `engineVersion` rises, Dagger writes `disableDefaultFunctionCaching = true` into the module config. Remove the flag after you review the caching of each function, and the functions take the 7-day default.

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
# Users install your toolchain into their workspace
dagger install github.com/you/my-toolchain

# And use it directly
dagger call my-toolchain build
dagger check
```

`dagger install` records the module under `[modules.my-toolchain]` in the `dagger.toml` of the project. Toolchain is the Dagger 0.x name for such a module. 0.x projects list their toolchains under `toolchains` in `dagger.json`, and [Dagger 0.x](#dagger-0x) covers how to add one.

Dagger resolves `+defaultPath` against the Git repository that holds the module that declares it. So in a toolchain from another repository, `/` names the root of the toolchain repository, not the project. Dagger resolves the path against the project that installs the module in two cases only. Either the module is a toolchain or the blueprint of a 0.x project, or its `dagger.toml` entry sets `legacy-default-path = true`. `dagger workspace migrate` writes that key for each toolchain and for the blueprint that it migrates. Toolchains read the project without that key through a `*dagger.Workspace` argument, which Dagger fills from the workspace. [Toolchain Design](references/architecture.md#toolchain-design) shows an example.

A good toolchain:
- Focuses on a single tool (eslint, pytest, golangci-lint) rather than being monolithic
- Takes a `*dagger.Workspace` argument in `New` and reads the project with `ws.Directory("/")`, so it works without explicit args
- Provides check functions (annotated with `// +check`) that integrate with `dagger check`
- Includes sensible defaults with customization via optional arguments

### Writing check functions

Annotate each check function with `// +check`. A check function takes no required argument, but it can take optional arguments that have defaults. It returns only `error`, and the check fails when the error is not nil. To check a command, run the container with `Sync(ctx)` and return the error of `Sync`. `Sync` returns an error when the command exits with a non-zero code. Each check in this example runs a command that way:

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

// Build verifies the project compiles
// +check
func (m *MyModule) Build(ctx context.Context) error {
    _, err := dag.Container().
        From("golang:1.22").
        WithDirectory("/src", m.Source).
        WithWorkdir("/src").
        WithExec([]string{"go", "build", "./..."}).
        Sync(ctx)
    return err
}
```

Do not return a `*dagger.Container` or another value from a check function. Dagger applies this rule to each module whose `engineVersion` is a 1.0 beta or a later version, and every module that `dagger module init` creates carries such a version. When a check function of such a module returns a value, Dagger fails to load the module with `check <Object>.<function> must return Void`. `Void` is the Dagger return type of a Go function that returns only `error`.

Dagger does not apply the rule to a module whose `engineVersion` is lower than 1.0, such as a `dagger.json` module of a 0.x project. A check function of such a module can return a `*dagger.Container`. Dagger then runs the container, and the check fails when a command of the container exits with a non-zero code. Return only `error` from each check function that you add to such a module, so that the module still loads after its `engineVersion` rises to 1.0.

Checks from toolchains are namespaced: `dagger check my-toolchain:lint`. Run all checks from a toolchain with `dagger check my-toolchain:*`. List available checks with `dagger check -l`. `dagger check` resolves against the entire workspace (all modules and toolchains it contains), not just the current module. Pass `--fail-fast` to cancel remaining checks on the first failure.

### Toolchain customization

0.x projects customize a toolchain in its entry of the `toolchains` list in `dagger.json`:

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

1.0 workspaces hold these choices under `[modules.<name>]` in `dagger.toml`. The `settings` table sets the constructor arguments of the module, and `check.skip` lists the checks that `dagger check` leaves out. `dagger workspace migrate` converts each customization that has a setting to match.

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
- **Expose to host**: `dagger start` runs every function that returns a `*dagger.Service` and does not require an argument, and `dagger start -l` lists those functions
- **Expose one service**: `dagger call my-service up --ports 8080:80`
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
dagger call deploy --token=op://vault/item/field            # 1Password
dagger call deploy --token=vault://secret/deploy/api.token  # HashiCorp Vault
dagger call deploy --token=aws+sm://prod/github/token       # AWS Secrets Manager
```

Each `vault://` address has the form `vault://<mount>/<path>.<field>`. The first path segment names the KVv2 mount, and the field follows the last dot.

Always pass a full `<provider>://` scheme. Dagger 1.0 rejects a bare name such as `API_TOKEN`, and it rejects the short form `env:API_TOKEN`.

Function arguments (including secret references) can be persisted as user-defaults in a local `.env` file (named `MODULE_ARG` or `TYPE_FUNC_ARG`, case-insensitive). Dagger stores the *reference* (for example, `env://API_TOKEN`), never the raw secret value, so the `.env` holds no secret material.

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
├── dagger-module.toml
├── tests/
│   ├── main.go
│   └── dagger-module.toml    # depends on parent module
└── examples/
    └── go/
        ├── main.go
        └── dagger-module.toml  # testable examples (optional)
```

```shell
# From the directory of my-module
dagger module init go --name tests --path tests -y
cd tests && dagger module client add .. --sdk=go -y
```

`--path` creates the test module without installing it in the workspace, so load it with `-m`. Run tests from the parent directory with `dagger call -m tests all`, or from within the tests directory with `dagger call -m . all`.

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

1. Describe the module in the comment that opens `main.go`, above `package main`
2. Document functions with Go comments (they become API docs)
3. Run `dagger generate -y` and commit the generated code, because Dagger loads a published module from its committed bindings
4. Tag with semver: `git tag v1.0.0 && git push --tags`
5. First `dagger call` from someone indexes it on [daggerverse.dev](https://daggerverse.dev)

## Changesets

Functions can return a `*dagger.Changeset` to represent file modifications that should be applied back to the source directory. This is useful for code generation, formatting, and other source-modifying operations. Annotate a generator with `// +generate` to register it with the `dagger generate` command:

```go
// Generate runs go generate and returns changes to apply
// +generate
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

`dagger generate` runs every `// +generate` function and merges their changesets into the source directory. Each `// +generate` function also gets a matching check automatically, so `dagger check` reports stale generated files. To run only the explicit checks, run `dagger check --generated=false`. To suppress the generated check of one generator, list the generator under `check.skip` in the `[modules.<name>]` entry of its module in `dagger.toml`. `check-generated = false` at the top of `dagger.toml` suppresses the generated checks of every module. 0.x projects list the checks to suppress in the `ignoreChecks` of each toolchain entry in `dagger.json`.

When a function returns a changeset directly (not via `// +generate`), the CLI flag `-y` / `--auto-apply` applies it to the source directory. Functions can also return `[]*dagger.Changeset` for multiple sets of changes.

## Dagger Shell

Dagger Shell is a bash-style language for testing and debugging modules. Pipe syntax translates to function chaining. Pass a script inline with `-c`, or pass the path of a script file:

```shell
# Inline script
dagger -c 'build --src=. | publish ttl.sh/my-app'

# Test a function
dagger -c 'lint'

# Script file
dagger ./ci.dsh
```

`dagger` with no arguments prints usage. It does not start an interactive shell.

Key differences from `dagger call`:
- Pipe `|` chains function calls: `build | publish` means `build().publish()`
- Required args are positional, optional args use flags
- Shell variables: `ctr=$(container | from alpine)`
- Background jobs: `test & lint & .wait`

## CI Integration

Dagger pipelines run identically locally and in CI. Write pipeline logic in Dagger Functions, keep CI YAML minimal (just install Dagger and call `dagger check` or `dagger call`). See [architecture.md](references/architecture.md) for GitHub Actions and GitLab CI examples.

## Common Patterns Summary

| Pattern | When to Use | Reference |
| --- | --- | --- |
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
