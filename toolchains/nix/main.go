// Nix configuration validation toolchain
//
// Validates nix-darwin flake configuration through evaluation,
// static analysis, and formatting checks in Linux containers
// without needing to build darwin derivations.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"dagger/nix/internal/dagger"

	"golang.org/x/sync/errgroup"
)

// Nix validates nix-darwin and home-manager flake configurations.
type Nix struct {
	// Project source containing flake.nix
	Source *dagger.Directory
}

func New(
	// Project source directory
	// +defaultPath="/"
	// +ignore=["**/.git", "result", "toolchains/"]
	source *dagger.Directory,
) *Nix {
	return &Nix{Source: source}
}

// nixImage is the pinned nixos/nix container image.
const nixImage = "nixos/nix:2.32.6@sha256:8b7cc7ccc4c6a3b7852d81db9c4d0875b5a98867729351ed6fbfbf2839f1fa25"

// base returns a container with nix and flakes enabled.
func (m *Nix) base() *dagger.Container {
	ctr := dag.Container().From(nixImage)
	return ctr.
		WithMountedCache("/nix/store", dag.CacheVolume("nix-store"),
			dagger.ContainerWithMountedCacheOpts{Source: ctr.Directory("/nix/store")}).
		WithMountedCache("/nix/var/nix", dag.CacheVolume("nix-var"),
			dagger.ContainerWithMountedCacheOpts{Source: ctr.Directory("/nix/var/nix")}).
		WithMountedCache("/root/.cache/nix", dag.CacheVolume("nix-eval-cache")).
		WithEnvVariable("NIX_CONFIG", "experimental-features = nix-command flakes\nfilter-syscalls = false\n").
		WithDirectory("/src", m.Source).
		WithWorkdir("/src")
}

// FlakeCheck validates the flake schema and outputs.
//
// nixosConfigurations are excluded because they may depend on host-specific
// files (e.g., OrbStack's /etc/nixos/orbstack.nix) unavailable in CI.
// NixOS configs are still validated by the Build check via nix eval.
// +check
func (m *Nix) FlakeCheck() *dagger.Container {
	wrapper := `{
  inputs.src.url = "path:/src";
  outputs = { src, ... }: builtins.removeAttrs src.outputs [ "nixosConfigurations" ];
}`
	return m.base().
		WithNewFile("/tmp/check/flake.nix", wrapper).
		WithWorkdir("/tmp/check").
		WithExec([]string{"nix", "flake", "check", "--no-build", "--all-systems", "--no-write-lock-file"})
}

// hosts returns the list of darwinConfigurations defined in the flake.
func (m *Nix) hosts(ctx context.Context) ([]string, error) {
	out, err := m.base().
		WithExec([]string{"nix", "eval", "--json",
			".#darwinConfigurations", "--apply", "builtins.attrNames"}).
		Stdout(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	if err := json.Unmarshal([]byte(out), &names); err != nil {
		return nil, err
	}
	return names, nil
}

// homeConfigs returns the list of homeConfigurations defined in the flake.
func (m *Nix) homeConfigs(ctx context.Context) ([]string, error) {
	out, err := m.base().
		WithExec([]string{"nix", "eval", "--json",
			".#homeConfigurations", "--apply", "builtins.attrNames"}).
		Stdout(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	if err := json.Unmarshal([]byte(out), &names); err != nil {
		return nil, err
	}
	return names, nil
}

// evalAttrNames evaluates a nix expression and returns builtins.attrNames as []string.
func (m *Nix) evalAttrNames(ctx context.Context, ctr *dagger.Container, expr string) ([]string, error) {
	out, err := ctr.
		WithExec([]string{"nix", "eval", "--json", expr, "--apply", "builtins.attrNames"}).
		Stdout(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	if err := json.Unmarshal([]byte(out), &names); err != nil {
		return nil, err
	}
	return names, nil
}

// evalString evaluates a nix expression producing a bare string.
func (m *Nix) evalString(ctx context.Context, ctr *dagger.Container, expr string) (string, error) {
	out, err := ctr.
		WithExec([]string{"nix", "eval", "--raw", expr}).
		Stdout(ctx)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// homeInfo holds the fields extracted from a home-manager configuration
// in a single nix eval call.
type homeInfo struct {
	HomeDirectory string   `json:"homeDirectory"`
	Username      string   `json:"username"`
	XdgFiles      []string `json:"xdgFiles"`
	HomeFiles     []string `json:"homeFiles"`
}

// evalHomeInfo evaluates all required home-manager fields in one nix call.
func (m *Nix) evalHomeInfo(ctx context.Context, ctr *dagger.Container, name string) (*homeInfo, error) {
	ref := fmt.Sprintf(".#homeConfigurations.%q", name)
	out, err := ctr.
		WithExec([]string{"nix", "eval", "--json", ref, "--apply", `hm: {
			homeDirectory = hm.config.home.homeDirectory;
			username = hm.config.home.username;
			xdgFiles = builtins.filter (n: hm.config.xdg.configFile.${n}.enable) (builtins.attrNames hm.config.xdg.configFile);
			homeFiles = builtins.filter (n: hm.config.home.file.${n}.enable) (builtins.attrNames hm.config.home.file);
		}`}).
		Stdout(ctx)
	if err != nil {
		return nil, err
	}
	var info homeInfo
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// validateHome builds, activates, and verifies a single home-manager config.
func (m *Nix) validateHome(ctx context.Context, name string) error {
	ctr := m.base()
	ref := fmt.Sprintf(".#homeConfigurations.%q", name)

	info, err := m.evalHomeInfo(ctx, ctr, name)
	if err != nil {
		return fmt.Errorf("home %s: eval info: %w", name, err)
	}

	ctr = ctr.WithExec([]string{"nix", "build", ref + ".activationPackage"})

	ctr = ctr.
		WithEnvVariable("HOME", info.HomeDirectory).
		WithEnvVariable("USER", info.Username).
		WithExec([]string{"mkdir", "-p", info.HomeDirectory}).
		WithExec([]string{"mkdir", "-p", fmt.Sprintf("%s/.local/state/nix/profiles", info.HomeDirectory)}).
		WithExec([]string{"mkdir", "-p", fmt.Sprintf("/nix/var/nix/profiles/per-user/%s", info.Username)}).
		WithExec([]string{"./result/activate"})

	for _, f := range info.XdgFiles {
		path := fmt.Sprintf("%s/.config/%s", info.HomeDirectory, f)
		if strings.HasPrefix(f, info.HomeDirectory+"/") {
			path = f
		}
		ctr = ctr.WithExec([]string{"test", "-e", path})
	}
	for _, f := range info.HomeFiles {
		path := fmt.Sprintf("%s/%s", info.HomeDirectory, f)
		if strings.HasPrefix(f, info.HomeDirectory+"/") {
			path = f
		}
		ctr = ctr.WithExec([]string{"test", "-e", path})
	}

	ctr = ctr.WithExec([]string{"test", "-d",
		fmt.Sprintf("%s/.local/state/home-manager/gcroots/current-home/home-path/bin", info.HomeDirectory)})

	_, err = ctr.Sync(ctx)
	if err != nil {
		return fmt.Errorf("home %s: %w", name, err)
	}
	return nil
}

// BuildHome builds and validates all home-manager activation packages.
// +check
func (m *Nix) BuildHome(ctx context.Context) error {
	configs, err := m.homeConfigs(ctx)
	if err != nil {
		return err
	}
	eg, ctx := errgroup.WithContext(ctx)
	for _, name := range configs {
		eg.Go(func() error {
			return m.validateHome(ctx, name)
		})
	}
	return eg.Wait()
}

// Build proves all host configurations evaluate without errors.
// +check
func (m *Nix) Build(ctx context.Context) error {
	hosts, err := m.hosts(ctx)
	if err != nil {
		return err
	}

	eg, ctx := errgroup.WithContext(ctx)
	for _, host := range hosts {
		eg.Go(func() error {
			_, err := m.base().
				WithExec([]string{
					"nix", "eval",
					fmt.Sprintf(".#darwinConfigurations.%q.system", host),
				}).
				Sync(ctx)
			if err != nil {
				return fmt.Errorf("host %s: %w", host, err)
			}
			return nil
		})
	}
	return eg.Wait()
}

// Lint checks formatting and lint rules via treefmt (nixfmt, deadnix, statix).
// +check
func (m *Nix) Lint(ctx context.Context) error {
	_, err := m.base().
		WithExec([]string{"nix", "fmt", "--", "--fail-on-change"}).
		Sync(ctx)
	return err
}

// Format applies all auto-fixes via treefmt (nixfmt, deadnix, statix).
// +generate
func (m *Nix) Format() *dagger.Changeset {
	fixed := m.base().
		WithExec([]string{"nix", "fmt"}).
		Directory("/src")
	return dag.Directory().WithDirectory(".", fixed).Changes(m.Source)
}

// All runs all checks concurrently.
func (m *Nix) All(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		_, err := m.FlakeCheck().Sync(ctx)
		return err
	})
	eg.Go(func() error { return m.Build(ctx) })
	eg.Go(func() error { return m.BuildHome(ctx) })
	eg.Go(func() error { return m.Lint(ctx) })

	return eg.Wait()
}
