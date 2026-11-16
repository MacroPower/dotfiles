# Architecture & Design Patterns

Before you copy an example, match its calls to the layout of your module. A module with a generated entrypoint calls the constructors of `dagger.io/dagger/core`, such as `core.NewContainer()`. A module on the Go runtime calls the methods of the global `dag` client, such as `dag.Container()`. [Module Layouts](../SKILL.md#module-layouts) in SKILL.md maps each constructor to its `dag` method.

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
| --- | --- | --- |
| `Container` | OCI container image, chainable | `core.NewContainer().From("alpine")` |
| `Directory` | Filesystem directory reference | `core.NewDirectory()`, function args |
| `File` | Single file reference | `container.File("/app/binary")` |
| `Service` | Long-running network service | `container.AsService()` |
| `Secret` | Confidential data (never logged) | `core.SetSecret("key", "value")` |
| `CacheVolume` | Persistent filesystem cache | `core.NewCacheVolume("go-mod")` |
| `GitRepository` | Git repo reference | `core.Git("https://github.com/...")` |
| `Socket` | Unix/TCP socket | SSH auth sockets |
| `LLM` | Large Language Model interface | `core.NewLLM()` |
| `Env` | LLM environment with I/O | `dag.Env()` |
| `Changeset` | File modifications to apply to source | `dir.Changes(baseDir)` |

All types are immutable and content-addressed. Operations on them produce new instances rather than mutating state.

Additional utilities:
- `core.HTTP(url)`: fetches a file over HTTP and returns a `*dagger.File`
- `core.NewDirectory()`: creates an empty directory to build up incrementally
- `core.SetSecret(name, value)`: creates a secret programmatically

Options structs follow the naming convention `dagger.TypeMethodOpts` (for example, `dagger.ContainerAsServiceOpts`, `dagger.ContainerWithExecOpts`, and `dagger.ServiceEndpointOpts`).

---

## The DAG Model

Dagger represents workflows as a Directed Acyclic Graph (DAG). Each operation is a node that takes immutable inputs and produces immutable outputs.

**Why this matters for module design:**
- Operations are lazy — nothing executes until a scalar value is resolved (via `ctx`)
- Identical operations with identical inputs are automatically deduplicated
- The DAG engine handles parallelism — you don't need to manage concurrency for independent operations
- Content addressing means changing any input invalidates downstream caches

**Practical implication:** Build pipelines by chaining operations. Resolve intermediate values only when you need them:

```go
// Good: single pipeline, engine optimizes execution
func (m *MyModule) Build(src *dagger.Directory) *dagger.Container {
    return core.NewContainer().
        From("golang:1.22").
        WithDirectory("/src", src).
        WithWorkdir("/src").
        WithExec([]string{"go", "build", "-o", "app"})
}

// Avoid: unnecessary intermediate resolution
func (m *MyModule) Build(ctx context.Context, src *dagger.Directory) (*dagger.Container, error) {
    ctr := core.NewContainer().From("golang:1.22")
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
| --- | --- |
| (default) | Cached for 7 days |
| `// +cache="10m"` | Cached for 10 minutes |
| `// +cache="session"` | Cached within current engine session only |
| `// +cache="never"` | Always re-executes |

Cache key = module source code + argument values + parent object values. Any change invalidates.

Duration format: integer + `"s"` (seconds), `"m"` (minutes), or `"h"` (hours). Max 7 days, min 1 second.

### Layer caching

Independent of function caching. Each `WithExec`, `WithFile`, `WithDirectory` etc. is individually cached by the container engine. Even if a function re-runs (`cache="never"`), unchanged internal steps still cache.

### Secrets and caching

Secrets are never cached on disk. Functions returning values that reference secrets created via `core.SetSecret()` behave as "session" caching regardless of configured TTL.

By default, layer cache entries are keyed on the secret's plaintext value — different values invalidate cache. For rotating secrets that should share cache (such as frequently rotated tokens that are functionally equivalent), callers can set a stable `cacheKey`:

```shell
dagger call deploy --token=env://API_TOKEN?cacheKey=my-stable-key
```

### Cache volumes

Persist filesystem state across runs. Package managers use cache volumes to reuse downloads between runs:

```go
// Go module + build cache
func (m *MyModule) buildEnv(src *dagger.Directory) *dagger.Container {
    return core.NewContainer().
        From("golang:1.22").
        WithDirectory("/src", src).
        WithWorkdir("/src").
        WithMountedCache("/go/pkg/mod", core.NewCacheVolume("go-mod")).
        WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
        WithMountedCache("/root/.cache/go-build", core.NewCacheVolume("go-build")).
        WithEnvVariable("GOCACHE", "/root/.cache/go-build")
}
```

Common cache volume patterns:

| Tool | Cache Path | Volume Name | Env Var |
| --- | --- | --- | --- |
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
    return core.NewContainer().
        From("golang:1.22").
        WithDirectory("/src", src).
        WithWorkdir("/src").
        WithEnvVariable("CACHE_BUST", time.Now().String()).
        WithExec([]string{"go", "build", "./..."})
}
```

### Backwards compatibility (pre-v0.19.4)

Modules whose `engineVersion` is lower than v0.19.4 default to `"session"` caching for all functions. To keep that default, add a flag to the module config before a manual edit or a generation raises the `engineVersion` to v0.19.4 or a later version. Neither the Dagger v1.0.0-beta.25 CLI nor Go SDK v1.6.0 adds the flag. [dagger-module.toml](#dagger-moduletoml) covers how to raise the `engineVersion` and when generation raises it. The `dagger-module.toml` of a module on the Go runtime holds the flag in this form, and a 0.x `dagger.json` holds it as `"disableDefaultFunctionCaching": true`:

```toml
disableDefaultFunctionCaching = true
```

The flag preserves the session default. After you review the caching of each function and add the `// +cache` annotations that the functions need, remove the flag to opt in to the 7-day default TTL.

Do not add the flag to the manifest of a module with a generated entrypoint. Such a manifest declares no `engineVersion`, and Go SDK v1.6.0 refuses to generate a module whose entrypoint manifest holds the flag.

---

## Services

### Creating services

Any container with exposed ports can become a service. Use `AsService()` options to control startup:

```go
// Using the container's default entrypoint
func (m *MyModule) Database() *dagger.Service {
    return core.NewContainer().
        From("postgres:16").
        WithEnvVariable("POSTGRES_PASSWORD", "test").
        WithEnvVariable("POSTGRES_DB", "testdb").
        WithExposedPort(5432).
        AsService(dagger.ContainerAsServiceOpts{UseEntrypoint: true})
}

// Using a custom command
func (m *MyModule) HttpServer() *dagger.Service {
    return core.NewContainer().
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
    return core.NewContainer().
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
    return core.HTTP(endpoint).Contents(ctx)
}
```

### Exposing to host

`dagger start` runs the services of the workspace and forwards the ports that they expose to the host. The command runs until you stop it with Ctrl+C. It takes the names of the services to start, and `-l` lists the services:

```shell
dagger start             # every service of the workspace
dagger start database    # only the services that you name
dagger start -l          # list the services and start none
```

Every function that returns a `*dagger.Service` and does not require an argument is a service. The function does not need a pragma, so `dagger start` runs the `Database` and `HttpServer` functions in [Creating services](#creating-services). The objects that the main object returns can hold services too. `dagger start -l` lists the `Database` service of the object that an `Infra` function returns as `infra/database`.

The name of a service is its function name in kebab case. A service of a module that is not the workspace main module carries the install name of the module first, as in `my-module/database`.

`dagger start` also runs each function that returns a service only for other functions to bind, such as `Database`, which `Test` binds in [Binding services](#binding-services). To keep a service out of `dagger start` and `dagger start -l`, list it under `up.skip` in the `[modules.<name>]` entry of its module in `dagger.toml`:

```toml
[modules.my-module]
source = ".dagger/modules/my-module"
up.skip = ["database", "infra/database"]
```

Each entry of `up.skip` names a service without the module name, and a pattern can use `*` to match many names. 0.x projects list those services in the `ignoreServices` of the toolchain entry in `dagger.json`.

A service that no `[ports.<host-port>]` table names opens each of its ports under the same number on the host. To use other host ports, add `[ports.<host-port>]` tables to `dagger.toml`. The `backendService` key of each table names the service as `<module>:<service>`, and its `backendPort` key holds the port of the service. After a table names a service, `dagger start` opens only the host ports of the tables that name it. `dagger start` fails with `port collision detected` when two of the services that it starts claim the same host port. In that case, name the services to start, or map one of them to another host port.

0.x projects have no `dagger.toml`. Do not create one to map a port, because Dagger then loads only the modules of `dagger.toml` and stops loading the toolchains of `dagger.json`. In a 0.x project, list the mappings in the `portMappings` of the toolchain entry in `dagger.json`:

```json
{
  "toolchains": [
    {
      "name": "web-app",
      "source": "toolchains/web-app",
      "portMappings": {
        "web": ["3000:80"]
      },
      "ignoreServices": ["redis", "infra:database"]
    }
  ]
}
```

Each key of `portMappings` names a service of the toolchain without the toolchain name, and each value lists `<host-port>:<service-port>` pairs. The mappings replace the same-number ports of the service, as a `[ports.<host-port>]` table does. `ignoreServices` names services the same way, and Dagger reads it as the `up.skip` of the toolchain. A colon or a slash can separate the parts of a name, as in `infra:database`.

Dagger reads `portMappings` and `ignoreServices` only from the entries of `toolchains`. A 0.x `dagger.json` that defines a module of its own through its `name` and `sdk` keys has no key that skips or remaps the services of that module. To leave one of those services out, name the services to start, as in `dagger start web`. To give one of them another host port, run `dagger call <function> up --ports <host-port>:<service-port>`.

To expose the service that one function returns, call `up` on the service. This form also reaches a function that requires arguments, and `--ports` maps a host port to a port of the service:

```shell
dagger call database up --ports 5432:5432
```

Dagger 0.x named the command `dagger up`, and it ran only the functions that carry the `// +up` pragma. Dagger 1.0 accepts the command and the pragma as well, but `dagger --help` lists only `dagger start`, and a service does not need the pragma.

### Accepting host services

Functions can receive services already running on the host as arguments:

```go
func (m *MyModule) UserList(ctx context.Context, svc *dagger.Service) (string, error) {
    return core.NewContainer().
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
    svcA := core.NewContainer().From("nginx").
        WithExposedPort(80).
        AsService(dagger.ContainerAsServiceOpts{UseEntrypoint: true}).
        WithHostname("svca")

    svcB := core.NewContainer().From("nginx").
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
    return core.NewContainer().
        From("redis:7").
        WithMountedCache("/data", core.NewCacheVolume("redis-data")).
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
core.NewContainer().
    WithSecretVariable("API_TOKEN", token)
```

### Secret as mounted file

```go
core.NewContainer().
    WithMountedSecret("/run/secrets/key", keyFile)
```

### Creating secrets programmatically

```go
secret := core.SetSecret("my-secret", "the-value")
```

### How users pass secrets

```shell
dagger call publish --token=env://GITHUB_TOKEN              # from environment variable
dagger call publish --token=file://~/.token                 # from file
dagger call publish --token=cmd://"vault read ..."          # from command output
dagger call publish --token=op://vault/item/field           # 1Password
dagger call publish --token=vault://secret/ci/github.token  # HashiCorp Vault
dagger call publish --token=aws+sm://prod/github/token      # AWS Secrets Manager
```

Each `vault://` address has the form `vault://<mount>/<path>.<field>`. The first path segment names the KVv2 mount, and the field follows the last dot. The example reads the `token` field of the secret `ci/github` from the `secret` mount.

Always include the full `<provider>://` scheme. Dagger 1.0 rejects a bare name such as `GITHUB_TOKEN`, and it rejects the short form `env:GITHUB_TOKEN`.

Secret references (and other function arguments) can be saved as user-defaults in a local `.env` file. Dagger persists the *reference* (for example, `env://GITHUB_TOKEN`), never the plaintext, so the `.env` holds no secret material.

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
    buildSecret := core.SetSecret("gh-token", val)
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
// +generate
func (m *MyModule) Generate() *dagger.Changeset {
    generated := core.NewContainer().
        From("golang:1.22").
        WithDirectory("/app", m.Source).
        WithWorkdir("/app").
        WithExec([]string{"go", "generate", "./..."}).
        Directory("/app")
    return generated.Changes(m.Source)
}
```

The `// +generate` annotation registers the function with the `dagger generate` command, which runs every generator and merges all of their changesets into the source directory. Each `// +generate` function also gets a matching check automatically, so `dagger check` reports whether generated files are stale (committed output drifted from a fresh run). Use `dagger check --generated=false` to run only the explicit checks. To suppress the generated check of one generator, list the generator under `check.skip` in the `[modules.<name>]` entry of its module in `dagger.toml`. `check-generated = false` at the top of `dagger.toml` suppresses the generated checks of every module. 0.x projects list the checks to suppress in the `ignoreChecks` of each toolchain entry in `dagger.json`.

For changesets returned without `// +generate`, the CLI prompts the user to apply them. Use `-y` / `--auto-apply` to apply automatically (useful in CI). Functions can return an array of changesets for multiple independent sets of changes.

---

## Default Path Resolution

How `+defaultPath` resolves depends on whether the module is in a git repository. The module config directory is the directory that holds the `dagger-module.toml` of the module, or its `dagger.json` in a 0.x project.

**Git repositories:**
| Default path | Context directory | Resolved path |
| --- | --- | --- |
| `/` | Repository root (`/`) | `/` |
| `/src` | Repository root (`/`) | `/src` |
| `.` | Module config directory (`/my-module`) | `/my-module` |
| `..` | Module config directory (`/my-module`) | `/` |

**Non-git directories:**
| Default path | Context directory | Resolved path |
| --- | --- | --- |
| `/` | Module config directory (`/my-module`) | `/my-module` |
| `.` | Module config directory (`/my-module`) | `/my-module` |

In git repos, absolute paths resolve from the repo root, and relative paths resolve from the module config directory. Outside git repos, all paths resolve from the module config directory.

These rules resolve a path against the module that declares the argument, and against the Git repository that holds that module. [Toolchain Design](#toolchain-design) covers the modules whose paths resolve against the project that installs them.

---

## Toolchain Design

Toolchains are modules optimized for zero-code consumption. Instead of importing one, users install it and call its functions directly.

### Design principles

1. **Single-tool focus**: one toolchain per tool (golangci-lint, not "Go toolchain"). This enables independent updates, tool-specific customization, and mixing across language stacks
2. **Workspace input**: take a `*dagger.Workspace` argument in `New` and read the project with `ws.Directory("/")`, so the toolchain works without explicit args
3. **Checks**: expose validation functions that integrate with `dagger check`
4. **Defaults**: give each optional argument a default, so the toolchain runs with no arguments

### Example toolchain

```go
func New(
    // The project that installs the toolchain, filled by Dagger
    ws *dagger.Workspace,
) *GolangciLint {
    return &GolangciLint{
        Source: ws.Directory("/", dagger.WorkspaceDirectoryOpts{
            Exclude: []string{"**/.git", "**/vendor"},
        }),
    }
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
) error {
    _, err := core.NewContainer().
        From("golangci/golangci-lint:" + version).
        WithDirectory("/src", m.Source).
        WithWorkdir("/src").
        WithMountedCache("/root/.cache/golangci-lint", core.NewCacheVolume("golangci-lint")).
        WithExec([]string{"golangci-lint", "run"}).
        Sync(ctx)
    return err
}
```

### Check functions

Check functions are the key integration point with `dagger check`:
- Annotate with `// +check` above the function
- Must not require any arguments (optional args with defaults are fine)
- Return only `error`, and the check fails when the error is not nil
- To check a command, run the container with `Sync(ctx)` and return the error of `Sync`, as `Lint` does in the [example toolchain](#example-toolchain). `Sync` returns an error when the command exits with a non-zero code
- A module that Dagger loads at a 1.0 beta or a later version fails to load when one of its check functions returns a value, such as a `*dagger.Container`. The error is `check <Object>.<function> must return Void`, and `Void` is the Dagger return type of a Go function that returns only `error`
- Dagger does not apply that rule to a module whose `engineVersion` is lower than 1.0, such as a `dagger.json` module of a 0.x project. A check function of such a module can return a `*dagger.Container`. Dagger then runs the container, and the check fails when a command of the container exits with a non-zero code
- Checks execute in parallel for maximum performance
- Checks from toolchains are namespaced: `dagger check golangci-lint:lint`
- Pattern-based filtering: `dagger check lint-*`, `dagger check golangci-lint:*`
- List checks: `dagger check -l`
- `dagger check` (and `dagger generate`) resolve against the entire workspace (all modules and toolchains it contains), not just the current module
- `--fail-fast` cancels remaining checks on the first failure
- `// +generate` functions contribute their own checks. Skip those checks with `dagger check --generated=false` (see [Changesets](#changesets))

### Installation and usage

```shell
# Install into the workspace
dagger install github.com/you/golangci-lint-toolchain@v1.0.0

# Custom name
dagger install github.com/you/toolchain --name lint

# Use
dagger call lint lint
dagger check          # runs all checks from all toolchains
dagger check lint:*   # runs checks from "lint" toolchain only
```

`dagger install` records the module under `[modules.<name>]` in the `dagger.toml` of the project. Toolchain is the Dagger 0.x name for such a module. 0.x projects list their toolchains under `toolchains` in `dagger.json`, and Dagger 1.0 has no command that adds an entry to that list, so edit the file.

### Default path context

Dagger resolves `+defaultPath` against the Git repository that holds the module that declares it, as [Default Path Resolution](#default-path-resolution) describes. It resolves the path against the project that installs the module in two cases only:

- The module is a toolchain or the blueprint of a 0.x project.
- The `dagger.toml` entry of the module sets `legacy-default-path = true`. `dagger workspace migrate` writes that key for each toolchain and for the blueprint that it migrates.

Toolchains read the project without that key through a `*dagger.Workspace` argument, as the [example toolchain](#example-toolchain) does. Dagger fills the argument from the workspace of a call that starts outside a module, such as a `dagger call` or a `dagger check`. Dagger does not forward the workspace of a module function to the dependencies that the function calls. So a module that calls such a dependency, such as a test module, takes a `*dagger.Workspace` argument of its own and passes it to the dependency.

`+defaultPath` serves the modules that read the repository that holds them, such as a module inside the project that it builds. It also serves the toolchains and the blueprint of a 0.x project. `ws.Directory` takes `Exclude` and `Include` patterns in place of `+ignore`. `ws.File` reads one file, and `ws.FindUp` returns the workspace path of the nearest file or directory with a given name.

### Customization in dagger.json

0.x projects customize a toolchain in `dagger.json`, without forking the toolchain:

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
1. **Override argument defaults**: change default values for function arguments
2. **Override `defaultPath`**: redirect source directory for functions using `+defaultPath`
3. **Ignore checks**: skip specific checks by name (supports glob patterns, scoped to the toolchain)

1.0 workspaces hold these choices under `[modules.<name>]` in `dagger.toml`. The `settings` table sets the constructor arguments of the module, and `check.skip` lists the checks that `dagger check` leaves out. `dagger workspace migrate` converts each customization that has a setting to match.

---

## Module Dependencies

### Installing

Run the `dagger module client` commands from the directory of the module that takes the dependency. Each `add`, `update`, and `rm` regenerates that module, and `-y` applies the changes.

```shell
dagger module client add github.com/user/module@v1.0.0 --sdk=go -y
```

The command records the dependency in the `clients` list of the module in `dagger.toml`. What the SDK then generates depends on the layout of the module:

- **Generated entrypoint**: the SDK generates a client package in `internal/dagger/clients/<name>/`, where `<name>` is the module name of the dependency. It does not write the dependency to `dagger-module.toml`.
- **Go runtime**: the SDK writes the matching `[[dependencies]]` entry of `dagger-module.toml`, and it generates the bindings of the dependency in `internal/dagger/`. The next `dagger generate` replaces a manual edit of that entry, so do not edit it by hand.

### Using in code

A module with a generated entrypoint calls a dependency through its client package. The `New` function of the package takes the constructor arguments of the dependency and returns its main object:

```go
import "dagger/my-module/internal/dagger/clients/golang"

func (m *MyModule) Build(ctx context.Context, src *dagger.Directory) *dagger.Directory {
    return golang.New().Build(src)
}
```

The name of a client package is the name of its directory in lowercase without the hyphens, such as `mydep` for `clients/my-dep/`. So import such a package under that name, as in `import mydep "dagger/my-module/internal/dagger/clients/my-dep"`.

The optional arguments of a constructor go in an options struct that the package names after the dependency, as in `mydep.New(mydep.MyDepOpts{Source: src})`. The optional arguments of a function go in a struct such as `mydep.MyDepBuildOpts`.

A module on the Go runtime calls a dependency through the global `dag` client, as in `dag.Golang().Build(src)`. Its options structs live in the `internal/dagger` package, as in `dagger.MyDepOpts`.

### Local dependencies

```shell
dagger module client add ./path/to/local-module --sdk=go -y
```

Both modules must be in the same git repository. Write a local path with a leading `./` or `../`. The command rejects a bare name such as `local-module`.

### Managing dependencies

```shell
dagger module client list --sdk=go              # list the dependencies, with the TARGET that rm takes
dagger module client update --sdk=go -y         # update every dependency of the module
dagger module client rm <TARGET> --sdk=go -y    # remove
```

Module references use the format `[proto://]host/repo[/subpath][@version]`.

### Workspace modules

`dagger install` and `dagger uninstall` do not change the dependencies of a module. They act on the modules that the workspace installs, in `dagger.toml`:

```shell
dagger install github.com/user/module@v1.0.0   # install a module in the workspace
dagger module update module-name                # refresh the version of an installed module
dagger uninstall module-name                    # remove
dagger lock update                              # refresh the pins in dagger.lock
```

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
| --- | --- |
| `dag.LLM()` | Create a new LLM instance |
| `.WithModel(model)` | Set the model, such as `"claude-sonnet-4-6"` or `"gpt-4o"` |
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

Create a `tests/` subdirectory that holds a module of its own, and add the parent module as its dependency:

```shell
cd my-module
dagger module init go --name tests --path tests -y
cd tests
dagger module client add .. --sdk=go -y
```

`--path` creates the test module without installing it in the workspace, so each command that calls it loads it with `-m`. The test module calls the parent module through the client package that `dagger module client add` generates:

```go
// tests/main.go
package tests

import (
    "context"

    "dagger.io/dagger/core"

    mymodule "dagger/tests/internal/dagger/clients/my-module"
)

type Tests struct{}

func (t *Tests) TestBuild(ctx context.Context) error {
    src := core.NewCurrentModule().Source()
    _, err := mymodule.New(mymodule.MyModuleOpts{Source: src}).
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

A test module on the Go runtime declares `package main` and imports `dagger/tests/internal/dagger`. It calls `dag.MyModule(dagger.MyModuleOpts{Source: src})` in place of `mymodule.New`.

### Running tests

```shell
# From the parent module directory
dagger call -m tests all

# Or from inside the tests directory
cd tests && dagger call -m . all
```

### Testing with services

```go
func (t *Tests) TestWithDB(ctx context.Context) error {
    db := core.NewContainer().
        From("postgres:16").
        WithEnvVariable("POSTGRES_PASSWORD", "test").
        WithExposedPort(5432).
        AsService(dagger.ContainerAsServiceOpts{UseEntrypoint: true})

    _, err := mymodule.New().
        Test(mymodule.MyModuleTestOpts{Db: db}).
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
# From the directory of my-module
dagger module init go --name examples --path examples/go -y
cd examples/go
dagger module client add ../.. --sdk=go -y
```

```go
// examples/go/main.go
import mymodule "dagger/examples/internal/dagger/clients/my-module"

type Examples struct{}

func (e *Examples) MyModuleBuild(ctx context.Context) error {
    _, err := mymodule.New().Build().Sync(ctx)
    return err
}
```

---

## Monorepo Patterns

### Top-level orchestrator

```
repo/
├── dagger.toml                  # workspace config: installs the modules
├── ci/
│   ├── dagger-module.toml       # orchestrator module, the workspace main
│   └── main.go
├── services/
│   ├── api/
│   │   ├── dagger-module.toml   # API module
│   │   └── main.go
│   └── web/
│       ├── dagger-module.toml   # Web module
│       └── main.go
└── shared/
    ├── dagger-module.toml       # Shared utilities module
    └── main.go
```

The orchestrator module takes the service modules as dependencies and orchestrates cross-service operations. `dagger.toml` installs it as the workspace main, so `dagger call` takes its functions without a module name.

### Shared build patterns

Extract common build logic into a shared module that other modules depend on:

```go
// shared/main.go
type Shared struct{}

func (s *Shared) GolangBuildEnv(src *dagger.Directory) *dagger.Container {
    return core.NewContainer().
        From("golang:1.22").
        WithDirectory("/src", src).
        WithWorkdir("/src").
        WithMountedCache("/go/pkg/mod", core.NewCacheVolume("go-mod")).
        WithMountedCache("/root/.cache/go-build", core.NewCacheVolume("go-build"))
}
```

Even if unnecessary CI jobs trigger in a monorepo, Dagger's layer cache means most finish nearly instantly. CI event filters are a secondary optimization.

---

## Publishing & Versioning

1. **Describe the module** in the comment that opens `main.go`, above the `package` clause. Dagger publishes the comment as the description of a module on the Go runtime, as in this example. Go SDK v1.6.0 publishes no description for a module with a generated entrypoint.
   ```go
   // Build and test Go applications

   package main
   ```

2. **Document all functions** with Go comments, which become the API docs

3. **Commit the generated code**: run `dagger generate -y` and commit the result, because Dagger loads a published module from its committed bindings

4. **Use semantic versioning**: `git tag v1.0.0 && git push --tags`

5. **Auto-indexed** on daggerverse.dev on first remote use

6. **Create examples** as separate modules in an `examples/` directory, where they serve as both documentation and integration tests

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

### dagger-module.toml

`dagger-module.toml` is the manifest of one module. `dagger module init` creates it, and Go SDK v1.6.0 writes one of two manifests. The manifest of a module with a generated entrypoint holds an `[entrypoint]` table. The manifest of a module on the Go runtime holds a `[runtime]` table. [Module Layouts](../SKILL.md#module-layouts) in SKILL.md lists what else differs between the two layouts.

#### Generated entrypoint

A module that `dagger module init go` creates loads through an entrypoint that the SDK generates in `internal/dagger/entrypoint/`. Go SDK v1.6.0 writes this manifest for such a module:

```toml
name = "my-module"
include = []

[entrypoint]
kind = "dang"
source = "./internal/dagger/entrypoint"
```

At each generation, the SDK writes the whole manifest again from the name of the module and its entrypoint. So do not edit the manifest of a module with a generated entrypoint. These keys do not work there as they do in the manifest of a module on the Go runtime:

- `engineVersion`: the manifest declares none. Dagger v1.0.0-beta.25 ignores the field there and loads the module at the version of the engine. The SDK removes the field when it generates the module again.
- `disableDefaultFunctionCaching`: Go SDK v1.6.0 refuses to generate a module whose manifest holds the flag, and the error says that `dagger-module.toml has settings a Dang entrypoint cannot carry`. Dagger v1.0.0-beta.25 ignores the flag in that manifest, so each function without a `+cache` annotation takes the 7-day default.
- `include`: the SDK writes the list, and the next `dagger generate` replaces a manual edit. [File/directory filters](#filedirectory-filters) covers the files that such a module loads.
- `[[dependencies]]`: the manifest lists no dependency that `dagger module client add` records. The SDK generates a client package for each one, and [Module Dependencies](#module-dependencies) covers those packages.

#### Go runtime

This example is the manifest of a module on the Go runtime:

```toml
name = "my-module"
engineVersion = "v1.0.0"

[runtime]
source = "go"

[[dependencies]]
name = "golang"
source = "github.com/user/golang@v1.0.0"
```

Each time the SDK generates a module on the Go runtime, it rewrites the `[[dependencies]]` entries, and it keeps the version that `engineVersion` declares. To change the `engineVersion`, edit the field, and then run `dagger generate -y` to generate the bindings for that version.

Modules whose `engineVersion` is lower than v0.19.4 default to `"session"` caching. To keep that default, add `disableDefaultFunctionCaching = true` to the manifest before a manual edit or a generation raises the field to v0.19.4 or a later version. Neither the Dagger v1.0.0-beta.25 CLI nor Go SDK v1.6.0 adds the flag. Without the flag, each function without a `+cache` annotation takes the 7-day default. [Backwards compatibility (pre-v0.19.4)](#backwards-compatibility-pre-v0194) covers the flag.

In the manifest of a module on the Go runtime, which holds a `[runtime]` table, Go SDK v1.6.0 writes the field in these cases:

- New manifests get the Dagger release that the SDK targets, which is v1.0.0-beta.25.
- Existing manifests that declare no `engineVersion` get the version of the running engine.
- Existing manifests that declare `latest` or `current` get the version of the running engine.

Dagger loads a module whose `engineVersion` is `latest` at the version of the running engine. So after the SDK generates such a module, the module stays at the version that the SDK wrote, and it stops following the engine. The SDK also adds a missing `v` prefix to a version, and it writes v0.11.9 in place of any other value that is not a version. These two rewrites write the version at which Dagger already loads the module.

Dagger loads a module on the Go runtime at v0.9.9 when its manifest declares no `engineVersion`, and at v0.11.9 when the manifest declares `current`. So when the SDK generates such a module, it raises the field to the version of the running engine, such as v1.0.0-beta.25. Before you generate such a module, add `disableDefaultFunctionCaching = true` to keep the session caching default. Also make each check function return only `error`. A module that Dagger loads at a 1.0 beta or a later version fails to load when one of its check functions returns a value. [Check functions](#check-functions) covers the rule.

The `dagger.json` of a 0.x module holds the fields of the Go runtime example as JSON, with the `runtime` table under the name `sdk`. The `dagger.json` of a 0.x project also lists its `toolchains`.

#### Layout settings

The settings of the Go SDK select the layout of a module, and they go in a `settings` table of `dagger.toml`. An existing module keeps its layout when these settings are unset. This table gives the effect of each value in Go SDK v1.6.0:

| Setting | Unset | `true` | `false` |
| --- | --- | --- | --- |
| `dangEntrypoint` | A new module gets a generated entrypoint | Moves a module on the Go runtime to a generated entrypoint, and keeps its embedded bindings unless `unifiedClients` is `true` | Creates a new module on the Go runtime, and keeps one there |
| `globalClient` | A module with a generated entrypoint has no `dag` | Generates `dag` in a module with a generated entrypoint | Refused for a module on the Go runtime, which always has `dag` |
| `unifiedClients` | A new module with a generated entrypoint takes its types from `dagger.io/dagger/core` | Moves a module with a generated entrypoint to the types of `dagger.io/dagger/core`, and the SDK refuses it on the Go runtime | Keeps embedded bindings in each layout |

Embedded bindings declare their own types, so the core constructors do not return those types. The SDK also refuses to generate a module with a generated entrypoint and embedded bindings when the module uses another module. The error names the `unifiedClients` setting. So leave `unifiedClients` unset in a new module, and set it to `true` in a module that moves from the Go runtime.

To move a module from the Go runtime to a generated entrypoint, set `dangEntrypoint` and `unifiedClients` to `true` in the settings table of its scope. When the code of the module calls `dag`, also set `globalClient = true`:

```toml
[sdks.go.scopes."path/to/module".settings]
dangEntrypoint = true
unifiedClients = true
globalClient = true
```

Before you move a module, keep the caching that its functions need. A module with a generated entrypoint declares no `engineVersion`, so each function without a `+cache` annotation takes the 7-day default. Go SDK v1.6.0 also refuses to generate the module while its manifest holds `disableDefaultFunctionCaching`. So when the module keeps the session default through that flag or through an `engineVersion` lower than v0.19.4, review the caching of each function, as [Backwards compatibility (pre-v0.19.4)](#backwards-compatibility-pre-v0194) describes. Add `// +cache="session"` to each function that needs the session default, and remove the flag from `dagger-module.toml`.

Before you move a module, also make each check function return only `error`. Dagger loads a module with a generated entrypoint at the version of the engine, such as v1.0.0-beta.25. So when one of its check functions returns a value, such as a `*dagger.Container`, Dagger fails to load the moved module with `check <Object>.<function> must return Void`. [Check functions](#check-functions) covers the rule.

Then run `dagger generate -y`. The SDK does not move a module back to the Go runtime. It marks the `dag` that it generates as deprecated, and that `dag` also reaches each dependency, as in `dag.MyDep()`. In new code, call the core constructors.

To create a module on the Go runtime, pass `--dang-entrypoint=false` to `dagger module init go`:

```shell
dagger module init go --name my-module --dang-entrypoint=false -y
```

The command records the setting under `[sdks.go.scopes."<path>".settings]` in `dagger.toml`, where `<path>` is the directory of the module, relative to the directory of `dagger.toml`. So each later generation keeps the module on the Go runtime. To keep an existing module on the Go runtime, set `dangEntrypoint = false` and `unifiedClients = false` in the settings table of its scope.

To set `dangEntrypoint = false` for every Go module of the workspace, put the key under `[modules.<sdk>.settings]`, where `<sdk>` is the module that the `module` key of `[sdks.go]` names. That module is `go-sdk` after `dagger module install dagger.io/sdk/go`, and `dagger-go-sdk` after `dagger workspace migrate`. The SDK then refuses to generate each module of the workspace that already loads through a generated entrypoint. The settings table of a scope overrides `[modules.<sdk>.settings]`. So to keep such a module, set `dangEntrypoint = true` in the settings table of its scope.

Dagger v1.0.0-beta.25 rejects the key outside a `settings` table, such as directly under `[sdks.go]` or in the scope table itself. Dagger then fails to load the workspace, and the error names the key, such as `unknown field sdks.go.dangEntrypoint`.

### dagger.toml

`dagger.toml` is the config of a workspace. Dagger reads the nearest `dagger.toml` above the current directory, up to the root of the Git repository. The commands of this skill write the file, as in this example:

```toml
[modules.my-module]
source = ".dagger/modules/my-module"

[modules.go-sdk]
source = "dagger.io/sdk/go"

[sdks.go]
module = "go-sdk"

[sdks.go.scopes.".dagger/modules/my-module"]
is-module = true
name = "my-module"
clients = ["github.com/user/golang@v1.0.0"]
```

`[modules.<name>]` records an installed module: its `source`, its `settings`, and `main = true` for the workspace main. `[sdks.<name>]` records an installed SDK and its scopes. Each scope is a directory where the SDK generates code, and its `clients` list holds the dependencies that `dagger module client add` records. The `settings` table of a scope holds the SDK settings of its module, which [Layout settings](#layout-settings) covers.

### Lockfiles

`dagger.lock` sits beside `dagger.toml`. It pins the lookups that the workspace resolved, so that later runs reuse them. The lookups include the images that `container.From()` resolves and the commits behind Git refs. Any Dagger command can update the file, so commit it with your changes.

```shell
dagger lock list                 # print the entries of dagger.lock
dagger lock update               # refresh every entry
dagger lock update 'node:lts*'   # refresh the entries that match a selector
```

### File/directory filters

In a module on the Go runtime, the `include` field of `dagger-module.toml` lists more files and directories to include or exclude when Dagger loads the module itself. It does not filter function arguments, which use `+ignore`. By default, Dagger includes only the module config file and the files under the source directory. Patterns with a `!` prefix exclude the paths that they match. Put the field above the first table of the file:

```toml
include = ["**/*.go", "go.mod", "go.sum", "!**/*_test.go"]
```

Adding `!` exclusion patterns is useful to avoid uploading large cache or generated files that the Dagger Engine does not need.

A module with a generated entrypoint takes no such list, because the SDK writes the `include` field of its manifest. Dagger loads the directory of that module without its `.git` directory and without the files that `.gitignore` ignores. It also loads the files outside the directory that the build reads through a local `replace` directive of `go.mod`.

### Go workspaces

Go SDK v1.6.0 does not add a module of either layout to a `go.work` file above the module directory, such as a `go.work` file at the repository root. So when the repository has a `go.work` file, add each new module to it. Run `go work use` from the directory of the `go.work` file, and pass the directory of the module:

```shell
go work use .dagger/modules/my-module
```

Until `go.work` lists the module, Go treats the module as outside the workspace. `go build`, `go vet`, and `go test` then fail in the module directory. The package pattern of the command selects the error, as in these cases:

| Package pattern | Error |
| --- | --- |
| `.`, or no pattern | `current directory is contained in a module that is not one of the workspace modules listed in go.work` |
| `./...` | `pattern ./...: directory prefix . does not contain modules listed in go.work or their selected dependencies` |

`go work use` also raises the `go` line of `go.work` when the `go.mod` of the module names a later Go version. Keep the raised line. When the `go` line of `go.work` names an earlier version than a listed module requires, the `go` command fails in every module of the workspace with `module <path> listed in go.work file requires go >= <version>`.

### Private Go modules

0.x modules configure access to private Go modules via `goprivate` in the SDK config section of `dagger.json`. `dagger-module.toml` has no `config` key under `[runtime]`, so only a `dagger.json` holds this setting:

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
