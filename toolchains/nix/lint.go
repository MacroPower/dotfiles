package main

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sync"

	"dagger/nix/internal/dagger"
)

const (
	// golangciLintVersion is the golangci-lint image tag used by
	// [Nix.CheckLint].
	golangciLintVersion = "v2.11" // renovate: datasource=github-releases depName=golangci/golangci-lint

	// toolchainSubpath is the path of this toolchain from the root of
	// the project source.
	toolchainSubpath = "toolchains/nix"

	// lintDir is the directory of the lint container that holds the
	// source of the toolchain.
	lintDir = "/src"
)

// goModules holds the path of each Go module of the toolchain from the
// root of the toolchain. Each Go module is also a Dagger module.
// golangci-lint does not lint a Go module that is nested in the module
// of its working directory. [Nix.CheckLint] therefore runs it in each
// module of this list.
var goModules = []string{".", "tests"}

// lintSource returns the directory of this toolchain in the project
// source, with the generated bindings of each Go module. The code of a
// module imports its bindings, so golangci-lint cannot check the types
// of the module without them. The ignore list of the project source
// leaves the bindings out, so the function generates them.
func (m *Nix) lintSource() *dagger.Directory {
	src := m.Source.Directory(toolchainSubpath)
	withBindings := src

	for _, mod := range goModules {
		bindings := src.
			AsModuleSource(dagger.DirectoryAsModuleSourceOpts{SourceRootPath: mod}).
			GeneratedContextDirectory()
		withBindings = withBindings.WithDirectory("/", bindings)
	}

	return withBindings
}

// lintBase returns a golangci-lint container whose working directory,
// [lintDir], holds the directory that [Nix.lintSource] returns and the
// linter configuration of the project source. golangci-lint reads that
// configuration in each Go module of the toolchain, because it searches
// the parents of its working directory. The container mounts the caches
// of Go and of golangci-lint.
func (m *Nix) lintBase() *dagger.Container {
	return dag.Container().
		From("golangci/golangci-lint:"+golangciLintVersion).
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-modules")).
		WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
		WithMountedCache("/go/build-cache", dag.CacheVolume("go-build")).
		WithEnvVariable("GOCACHE", "/go/build-cache").
		WithMountedDirectory(lintDir, m.lintSource()).
		WithFile(lintDir+"/.golangci.yaml", m.Source.File(".golangci.yaml")).
		WithWorkdir(lintDir).
		WithMountedCache("/root/.cache/golangci-lint", dag.CacheVolume("golangci-lint-"+golangciLintVersion))
}

// CheckLint verifies that golangci-lint passes on each Go module of
// this toolchain, which includes the test module. It reads the modules
// from toolchains/nix in the project source and the linter
// configuration from .golangci.yaml at the root of that source. It
// lints the modules at the same time and returns after every lint ends,
// so one run reports the findings of each module.
//
// +check
func (m *Nix) CheckLint(ctx context.Context) error {
	base := m.lintBase()

	// No run of golangci-lint depends on another, so the runs start at
	// the same time. errs keeps the order of goModules, whichever run
	// ends first.
	errs := make([]error, len(goModules))

	var wg sync.WaitGroup

	for i, mod := range goModules {
		wg.Go(func() {
			_, err := base.
				WithWorkdir(path.Join(lintDir, mod)).
				WithExec([]string{"golangci-lint", "run"}).
				Sync(ctx)
			if err != nil {
				errs[i] = fmt.Errorf("linting %s: %w", path.Join(toolchainSubpath, mod), err)
			}
		})
	}

	wg.Wait()

	return errors.Join(errs...)
}
