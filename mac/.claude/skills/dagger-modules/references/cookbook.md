# Cookbook — Common Recipes (Go SDK)

## Table of Contents

1. [Multi-Stage Build](#multi-stage-build)
2. [Multi-Platform Build](#multi-platform-build)
3. [Matrix Build](#matrix-build)
4. [Cache Dependencies](#cache-dependencies)
5. [Concurrent Execution](#concurrent-execution)
6. [Build from Dockerfile](#build-from-dockerfile)
7. [Clone Git Repository](#clone-git-repository)
8. [Mount and Copy Directories](#mount-and-copy-directories)
9. [Filter Directory Contents](#filter-directory-contents)
10. [Create Files in Containers](#create-files-in-containers)
11. [Fetch Files via HTTP](#fetch-files-via-http)
12. [Publish Container Images](#publish-container-images)
13. [OCI Annotations and Labels](#oci-annotations-and-labels)
14. [Use Secrets](#use-secrets)
15. [Export Files to Host](#export-files-to-host)
16. [Set Environment Variables](#set-environment-variables)
17. [Test Against Database Service](#test-against-database-service)
18. [Continue After Errors](#continue-after-errors)
19. [Interactive Debugging](#interactive-debugging)
20. [Custom Telemetry Spans](#custom-telemetry-spans)
21. [LLM Agents](#llm-agents)

---

## Multi-Stage Build

Compile in a full-featured container, run in a minimal image:

```go
func (m *MyModule) Build(ctx context.Context, src *dagger.Directory) (string, error) {
    // Build stage
    builder := dag.Container().
        From("golang:latest").
        WithDirectory("/src", src).
        WithWorkdir("/src").
        WithEnvVariable("CGO_ENABLED", "0").
        WithExec([]string{"go", "build", "-o", "myapp"})

    // Production stage — minimal image
    prodImage := dag.Container().
        From("alpine").
        WithFile("/bin/myapp", builder.File("/src/myapp")).
        WithEntrypoint([]string{"/bin/myapp"})

    return prodImage.Publish(ctx, "ttl.sh/myapp:latest")
}
```

---

## Multi-Platform Build

Build and publish for multiple architectures:

```go
func (m *MyModule) BuildMultiPlatform(ctx context.Context, src *dagger.Directory) (string, error) {
    platforms := []dagger.Platform{"linux/amd64", "linux/arm64"}
    var variants []*dagger.Container

    for _, platform := range platforms {
        builder := dag.Container(dagger.ContainerOpts{Platform: platform}).
            From("golang:latest").
            WithDirectory("/src", src).
            WithWorkdir("/src").
            WithEnvVariable("CGO_ENABLED", "0").
            WithExec([]string{"go", "build", "-o", "app"})

        // Minimal production image per platform
        prod := dag.Container(dagger.ContainerOpts{Platform: platform}).
            From("alpine").
            WithFile("/bin/app", builder.File("/src/app")).
            WithEntrypoint([]string{"/bin/app"})

        variants = append(variants, prod)
    }

    return dag.Container().
        Publish(ctx, "ttl.sh/my-app:latest", dagger.ContainerPublishOpts{
            PlatformVariants: variants,
        })
}
```

### Minimal containers with WithRootfs

For the smallest possible images (scratch-like), use `WithRootfs` instead of a base image. This creates a container with only the files you provide — no OS layer at all:

```go
outputDir := builder.Directory("/output")
prod := dag.Container(dagger.ContainerOpts{Platform: platform}).
    WithRootfs(outputDir).
    WithEntrypoint([]string{"/app"})
```

This is ideal for statically-compiled binaries (Go with `CGO_ENABLED=0`, Rust, etc.) where no runtime dependencies are needed.

---

## Matrix Build

Build for multiple OS/architecture combinations:

```go
func (m *MyModule) Build(ctx context.Context, src *dagger.Directory) *dagger.Directory {
    gooses := []string{"linux", "darwin"}
    goarches := []string{"amd64", "arm64"}

    outputs := dag.Directory()

    golang := dag.Container().
        From("golang:latest").
        WithDirectory("/src", src).
        WithWorkdir("/src")

    for _, goos := range gooses {
        for _, goarch := range goarches {
            path := fmt.Sprintf("build/%s/%s/", goos, goarch)
            build := golang.
                WithEnvVariable("GOOS", goos).
                WithEnvVariable("GOARCH", goarch).
                WithExec([]string{"go", "build", "-o", path})
            outputs = outputs.WithDirectory(path, build.Directory(path))
        }
    }
    return outputs
}
```

---

## Cache Dependencies

Mount cache volumes for package managers to avoid re-downloading:

```go
func (m *MyModule) Build(source *dagger.Directory) *dagger.Container {
    return dag.Container().
        From("golang:1.22").
        WithDirectory("/src", source).
        WithWorkdir("/src").
        WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-mod")).
        WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
        WithMountedCache("/root/.cache/go-build", dag.CacheVolume("go-build")).
        WithEnvVariable("GOCACHE", "/root/.cache/go-build").
        WithExec([]string{"go", "build"})
}
```

### Node.js variant

```go
func (m *MyModule) Build(source *dagger.Directory) *dagger.Container {
    return dag.Container().
        From("node:21-slim").
        WithDirectory("/src", source).
        WithWorkdir("/src").
        WithMountedCache("/root/.npm", dag.CacheVolume("npm-cache")).
        WithExec([]string{"npm", "install"}).
        WithExec([]string{"npm", "run", "build"})
}
```

---

## Concurrent Execution

Run independent operations in parallel using `errgroup`:

```go
import "golang.org/x/sync/errgroup"

func New(source *dagger.Directory) *MyModule {
    return &MyModule{Source: source}
}

type MyModule struct {
    Source *dagger.Directory
}

func (m *MyModule) Test(ctx context.Context) (string, error) {
    return m.buildEnv().
        WithExec([]string{"go", "test", "./..."}).
        Stdout(ctx)
}

func (m *MyModule) Lint(ctx context.Context) (string, error) {
    return m.buildEnv().
        WithExec([]string{"golangci-lint", "run"}).
        Stdout(ctx)
}

func (m *MyModule) Vet(ctx context.Context) (string, error) {
    return m.buildEnv().
        WithExec([]string{"go", "vet", "./..."}).
        Stdout(ctx)
}

// RunAll executes tests, lint, and vet concurrently
func (m *MyModule) RunAll(ctx context.Context) error {
    eg, gctx := errgroup.WithContext(ctx)

    eg.Go(func() error { _, err := m.Test(gctx); return err })
    eg.Go(func() error { _, err := m.Lint(gctx); return err })
    eg.Go(func() error { _, err := m.Vet(gctx); return err })

    return eg.Wait()
}

func (m *MyModule) buildEnv() *dagger.Container {
    return dag.Container().
        From("golang:1.22").
        WithDirectory("/src", m.Source).
        WithWorkdir("/src").
        WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-mod"))
}
```

---

## Build from Dockerfile

```go
func (m *MyModule) Build(
    ctx context.Context,
    // Directory containing Dockerfile
    src *dagger.Directory,
) *dagger.Container {
    return src.DockerBuild()
}
```

With custom Dockerfile path and build args:

```go
func (m *MyModule) Build(src *dagger.Directory) *dagger.Container {
    return src.DockerBuild(dagger.DirectoryDockerBuildOpts{
        Dockerfile: "deploy/Dockerfile",
        BuildArgs: []dagger.BuildArg{
            {Name: "GO_VERSION", Value: "1.22"},
        },
    })
}
```

---

## Clone Git Repository

```go
func (m *MyModule) Clone(ctx context.Context) *dagger.Directory {
    return dag.Git("https://github.com/dagger/dagger").
        Ref("main").
        Tree()
}
```

With SSH authentication:

```go
func (m *MyModule) ClonePrivate(
    ctx context.Context,
    // SSH socket for authentication
    sshSocket *dagger.Socket,
) *dagger.Directory {
    return dag.Git("git@github.com:user/private-repo.git",
        dagger.GitOpts{SSHAuthSocket: sshSocket}).
        Ref("main").
        Tree()
}
```

---

## Mount and Copy Directories

Two ways to add directories to containers — choosing correctly affects performance and caching:

### Mount directory into container

Mounts are fast, resource-efficient references. Changes made inside the container are **not** persisted in the image layer. Use for source code during build or anything not needed in the final output:

```go
func (m *MyModule) Build(src *dagger.Directory) *dagger.Container {
    return dag.Container().
        From("golang:1.22").
        WithMountedDirectory("/src", src).  // mount (reference, not persisted)
        WithWorkdir("/src").
        WithExec([]string{"go", "build"})
}
```

### Copy directory into container

Copies are persisted in the container layer. Use when the content should be part of the final image:

```go
func (m *MyModule) Build(src *dagger.Directory) *dagger.Container {
    return dag.Container().
        From("golang:1.22").
        WithDirectory("/src", src).  // copy (persisted in container layer)
        WithWorkdir("/src").
        WithExec([]string{"go", "build"})
}
```

**Rule of thumb**: use `WithMountedDirectory` for build-time inputs; use `WithDirectory` for content that should ship in the final image.

### Copy file into container

```go
func (m *MyModule) AddConfig(ctr *dagger.Container, cfg *dagger.File) *dagger.Container {
    return ctr.WithFile("/etc/app/config.yaml", cfg)
}
```

### Copy file to runtime for local processing

```go
func (m *MyModule) Process(ctx context.Context, source *dagger.File) (string, error) {
    // Export to module runtime's local filesystem
    _, err := source.Export(ctx, "input.txt")
    if err != nil { return "", err }
    // Now use standard Go I/O
    data, err := os.ReadFile("input.txt")
    return string(data), err
}
```

---

## Filter Directory Contents

### Pre-copy filtering with +ignore

Filter at the parameter level before the directory is uploaded to the engine. Uses gitignore-style patterns — order matters, and `!` negates a previous exclusion:

```go
func (m *MyModule) MarkdownOnly(
    // +ignore=["*", "!**/*.md"]
    src *dagger.Directory,
) *dagger.Directory {
    return src
}
```

Common `+ignore` patterns:
- `["**/.git", "**/node_modules"]` — exclude VCS and deps
- `["*", "!**/*.go", "!go.mod", "!go.sum"]` — Go files only
- `["**/*_test.go", "**/testdata/**"]` — exclude tests

### Exclude patterns

```go
func (m *MyModule) Source(src *dagger.Directory) *dagger.Directory {
    return src.WithoutDirectory("node_modules").
        WithoutDirectory(".git").
        WithoutFile("*.log")
}
```

### Include only specific patterns

```go
func (m *MyModule) GoFiles(src *dagger.Directory) *dagger.Directory {
    return dag.Directory().
        WithDirectory("/", src, dagger.DirectoryWithDirectoryOpts{
            Include: []string{"**/*.go", "go.mod", "go.sum"},
        })
}
```

### Exclude on copy

```go
func (m *MyModule) Build(
    src *dagger.Directory,
    // +optional
    exclude []string,
) *dagger.Container {
    return dag.Container().
        From("golang:1.22").
        WithDirectory("/src", src, dagger.ContainerWithDirectoryOpts{
            Exclude: exclude,
        })
}
```

---

## Create Files in Containers

```go
func (m *MyModule) WithConfig(ctr *dagger.Container) *dagger.Container {
    return ctr.WithNewFile("/etc/app/config.json", `{"debug": true}`)
}

// With permissions (useful for scripts)
func (m *MyModule) WithScript(ctr *dagger.Container) *dagger.Container {
    return ctr.WithNewFile("/usr/local/bin/run.sh", "#!/bin/sh\necho hello",
        dagger.ContainerWithNewFileOpts{Permissions: 0o755})
}

// Start from an empty directory and build up
func (m *MyModule) Assets() *dagger.Directory {
    return dag.Directory().
        WithNewFile("config.json", `{"version": "1.0"}`).
        WithNewFile("README.md", "# My Project")
}
```

---

## Fetch Files via HTTP

```go
func (m *MyModule) Download(ctx context.Context, url string) *dagger.File {
    return dag.HTTP(url)
}

// Use in a pipeline
func (m *MyModule) Install(ctx context.Context) *dagger.Container {
    binary := dag.HTTP("https://example.com/tool-v1.0")
    return dag.Container().
        From("alpine").
        WithFile("/usr/local/bin/tool", binary)
}
```

---

## Publish Container Images

### Publish to registry

```go
func (m *MyModule) Publish(ctx context.Context, ctr *dagger.Container) (string, error) {
    return ctr.Publish(ctx, "ttl.sh/my-app:latest")
}
```

### Publish with authentication

Use `WithRegistryAuth` to authenticate to private registries:

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

### Publish with multiple tags

```go
func (m *MyModule) Publish(
    ctx context.Context,
    ctr *dagger.Container,
    tags []string,
) ([]string, error) {
    var addresses []string
    for _, tag := range tags {
        addr, err := ctr.Publish(ctx, "ghcr.io/user/app:" + tag)
        if err != nil {
            return nil, err
        }
        addresses = append(addresses, addr)
    }
    return addresses, nil
}
```

---

## OCI Annotations and Labels

```go
// OCI annotations (on image manifest)
func (m *MyModule) Annotate(ctr *dagger.Container) *dagger.Container {
    return ctr.
        WithAnnotation("org.opencontainers.image.authors", "team@example.com").
        WithAnnotation("org.opencontainers.image.source", "https://github.com/user/repo")
}

// OCI labels (on image config)
func (m *MyModule) Label(ctr *dagger.Container) *dagger.Container {
    return ctr.
        WithLabel("org.opencontainers.image.title", "my-app").
        WithLabel("org.opencontainers.image.version", "1.0.0")
}
```

---

## Use Secrets

### Secret as environment variable

```go
func (m *MyModule) Deploy(ctx context.Context, token *dagger.Secret) (string, error) {
    return dag.Container().
        From("alpine").
        WithSecretVariable("API_TOKEN", token).
        WithExec([]string{"sh", "-c", "curl -H \"Authorization: Bearer $API_TOKEN\" https://api.example.com/deploy"}).
        Stdout(ctx)
}
```

### Secret mounted as file

```go
func (m *MyModule) Deploy(ctx context.Context, keyFile *dagger.Secret) (string, error) {
    return dag.Container().
        From("alpine").
        WithMountedSecret("/run/secrets/key", keyFile).
        WithExec([]string{"deploy", "--key-file=/run/secrets/key"}).
        Stdout(ctx)
}
```

### Secret in Dockerfile build

The secret name passed to `dag.SetSecret` must match the `--mount=type=secret,id=...` in the Dockerfile:

```go
func (m *MyModule) Build(ctx context.Context, src *dagger.Directory, token *dagger.Secret) *dagger.Container {
    val, _ := token.Plaintext(ctx)
    buildSecret := dag.SetSecret("gh-token", val)  // matches id=gh-token in Dockerfile
    return src.DockerBuild(dagger.DirectoryDockerBuildOpts{
        Secrets: []*dagger.Secret{buildSecret},
    })
}
```

---

## Export Files to Host

```go
func (m *MyModule) Build(ctx context.Context, src *dagger.Directory) (string, error) {
    return dag.Container().
        From("golang:1.22").
        WithDirectory("/src", src).
        WithWorkdir("/src").
        WithExec([]string{"go", "build", "-o", "app"}).
        File("/src/app").
        Export(ctx, "./build/app")
}
```

From CLI: `dagger call build --src=. export --path=./build/app`

To replace the entire contents of a target directory (deleting files that don't exist in the export), use `--wipe`:

```shell
dagger call build --src=. export --path=./build --wipe
```

Alternatively, return `*dagger.File` or `*dagger.Directory` from the function and let the CLI handle export.

---

## Set Environment Variables

```go
func (m *MyModule) Build(src *dagger.Directory) *dagger.Container {
    return dag.Container().
        From("golang:1.22").
        WithEnvVariable("CGO_ENABLED", "0").
        WithEnvVariable("GOOS", "linux").
        WithEnvVariable("GOARCH", "amd64").
        WithDirectory("/src", src).
        WithWorkdir("/src").
        WithExec([]string{"go", "build", "-o", "app"})
}
```

### Batch with With() pattern

```go
func EnvVariables(envs map[string]string) dagger.WithContainerFunc {
    return func(c *dagger.Container) *dagger.Container {
        for k, v := range envs {
            c = c.WithEnvVariable(k, v)
        }
        return c
    }
}

// Usage
ctr.With(EnvVariables(map[string]string{"KEY": "value", "KEY2": "value2"}))
```

---

## Test Against Database Service

```go
func (m *MyModule) Test(ctx context.Context, src *dagger.Directory) (string, error) {
    // Start a PostgreSQL service
    db := dag.Container().
        From("postgres:16").
        WithEnvVariable("POSTGRES_PASSWORD", "test").
        WithEnvVariable("POSTGRES_DB", "testdb").
        WithExposedPort(5432).
        AsService(dagger.ContainerAsServiceOpts{UseEntrypoint: true})

    // Run tests with the database service bound
    return dag.Container().
        From("golang:1.22").
        WithDirectory("/src", src).
        WithWorkdir("/src").
        WithServiceBinding("db", db).
        WithEnvVariable("DATABASE_URL", "postgres://postgres:test@db:5432/testdb?sslmode=disable").
        WithExec([]string{"go", "test", "-v", "./..."}).
        Stdout(ctx)
}
```

---

## Continue After Errors

Use `Expect: dagger.ReturnTypeAny` to allow non-zero exit codes:

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

type TestResult struct {
    Report   *dagger.File
    ExitCode int
}
```

---

## Interactive Debugging

Open a terminal session in any container for debugging:

```go
func (m *MyModule) Debug(src *dagger.Directory) *dagger.Container {
    return dag.Container().
        From("golang:1.22").
        WithDirectory("/src", src).
        WithWorkdir("/src").
        WithExec([]string{"go", "build", "./..."}).
        Terminal()  // drops into interactive shell at this point
}
```

From CLI: `dagger call debug --src=.`

Or attach a terminal to any function's output:
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

## Custom Telemetry Spans

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

Named return values (`rerr`) are used with the deferred telemetry end to capture errors.

---

## LLM Agents

### String I/O

```go
func (m *MyModule) Summarize(ctx context.Context, text string) (string, error) {
    env := dag.Env().
        WithStringInput("text", text, "the text to summarize").
        WithStringOutput("summary", "a concise summary")

    return dag.LLM().
        WithEnv(env).
        WithPrompt("Summarize the following text: $text").
        Env().
        Output("summary").
        AsString(ctx)
}
```

### Container I/O

```go
func (m *MyModule) GoProgram(assignment string) *dagger.Container {
    env := dag.Env().
        WithStringInput("assignment", assignment, "the task to complete").
        WithContainerInput("builder",
            dag.Container().From("golang").WithWorkdir("/app"),
            "a container for building Go code").
        WithContainerOutput("completed", "the finished container")

    return dag.LLM().
        WithEnv(env).
        WithPrompt("Complete the assignment: $assignment").
        Env().
        Output("completed").
        AsContainer()
}
```

### Directory I/O

```go
func (m *MyModule) ReviewCode(src *dagger.Directory) *dagger.Directory {
    env := dag.Env().
        WithDirectoryInput("source", src, "source code to review").
        WithDirectoryOutput("reviewed", "the reviewed source code with fixes")

    return dag.LLM().
        WithEnv(env).
        WithPrompt("Review the source code and fix any issues").
        Env().
        Output("reviewed").
        AsDirectory()
}
```
