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
| --- | --- | --- |
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

Secrets are never cached on disk. Functions returning values that reference secrets created via `dag.SetSecret()` behave as "session" caching regardless of configured TTL.

By default, layer cache entries are keyed on the secret's plaintext value — different values invalidate cache. For rotating secrets that should share cache (such as frequently rotated tokens that are functionally equivalent), callers can set a stable `cacheKey`:

```shell
dagger call deploy --token=env://API_TOKEN?cacheKey=my-stable-key
```

### Cache volumes

Persist filesystem state across runs. Package managers use cache volumes to reuse downloads between runs:

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
    return dag.Container().
        From("golang:1.22").
        WithDirectory("/src", src).
        WithWorkdir("/src").
        WithEnvVariable("CACHE_BUST", time.Now().String()).
        WithExec([]string{"go", "build", "./..."})
}
```

### Backwards compatibility (pre-v0.19.4)

Modules whose `engineVersion` is lower than v0.19.4 default to `"session"` caching for all functions. To keep that default when the `engineVersion` rises, Dagger writes a flag into the module config. `dagger-module.toml` holds the flag in this form, and a 0.x `dagger.json` holds it as `"disableDefaultFunctionCaching": true`:

```toml
disableDefaultFunctionCaching = true
```

The flag preserves the session default. After you review the caching of each function and add the `// +cache` annotations that the functions need, remove the flag to opt in to the 7-day default TTL.

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
    _, err := dag.Container().
        From("golangci/golangci-lint:" + version).
        WithDirectory("/src", m.Source).
        WithWorkdir("/src").
        WithMountedCache("/root/.cache/golangci-lint", dag.CacheVolume("golangci-lint")).
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
- Dagger fails to load a module whose `engineVersion` is a 1.0 beta or a later version when one of its check functions returns a value, such as a `*dagger.Container`. The error is `check <Object>.<function> must return Void`, and `Void` is the Dagger return type of a Go function that returns only `error`
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

The command records the dependency in `dagger.toml`, and the SDK writes the matching `[[dependencies]]` entry of `dagger-module.toml`. The next `dagger generate` replaces a manual edit of that entry, so do not edit it by hand.

### Using in code

Dependencies are accessed via `dag`:

```go
func (m *MyModule) Build(ctx context.Context, src *dagger.Directory) *dagger.Directory {
    return dag.Golang().Build(src)
}
```

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

Create a `tests/` subdirectory with its own `dagger-module.toml` that depends on the parent module:

```shell
cd my-module
dagger module init go --name tests --path tests -y
cd tests
dagger module client add .. --sdk=go -y
```

`--path` creates the test module without installing it in the workspace, so each command that calls it loads it with `-m`.

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
cd tests && dagger call -m . all
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
# From the directory of my-module
dagger module init go --name examples --path examples/go -y
cd examples/go
dagger module client add ../.. --sdk=go -y
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

1. **Describe the module** in the comment that opens `main.go`, above `package main`:
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

`dagger-module.toml` is the manifest of one module. `dagger module init` creates it, and the SDK rewrites `engineVersion` and the `[[dependencies]]` entries each time it generates the module:

```toml
name = "my-module"
engineVersion = "v1.0.0"

[runtime]
source = "go"

[[dependencies]]
name = "golang"
source = "github.com/user/golang@v1.0.0"
```

The `dagger.json` of a 0.x module holds the same fields as JSON, with the `runtime` table under the name `sdk`. The `dagger.json` of a 0.x project also lists its `toolchains`.

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

`[modules.<name>]` records an installed module: its `source`, its `settings`, and `main = true` for the workspace main. `[sdks.<name>]` records an installed SDK and its scopes. Each scope is a directory where the SDK generates code, and its `clients` list holds the dependencies that `dagger module client add` records.

### Lockfiles

`dagger.lock` sits beside `dagger.toml`. It pins the lookups that the workspace resolved, so that later runs reuse them. The lookups include the images that `container.From()` resolves and the commits behind Git refs. Any Dagger command can update the file, so commit it with your changes.

```shell
dagger lock list                 # print the entries of dagger.lock
dagger lock update               # refresh every entry
dagger lock update 'node:lts*'   # refresh the entries that match a selector
```

### File/directory filters

The `include` field of `dagger-module.toml` lists more files and directories to include or exclude when Dagger loads the module itself. It does not filter function arguments, which use `+ignore`. By default, Dagger includes only the module config file and the files under the source directory. Patterns with a `!` prefix exclude the paths that they match. Put the field above the first table of the file:

```toml
include = ["**/*.go", "go.mod", "go.sum", "!**/*_test.go"]
```

Adding `!` exclusion patterns is useful to avoid uploading large cache or generated files that the Dagger Engine doesn't need.

### Go workspaces

If a `go.work` file exists in the repository root, Dagger automatically adds new modules to it. This enables seamless IDE support and cross-module imports within the same repo.

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
