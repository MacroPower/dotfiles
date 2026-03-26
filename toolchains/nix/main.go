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
	"regexp"
	"strconv"
	"strings"

	"dagger/nix/internal/dagger"
	"dagger/nix/internal/sarif"

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

// nixImage is the pinned lix container image.
const nixImage = "ghcr.io/lix-project/lix:2.94.0@sha256:25f5eee428aa1bc217cfcfa7c6d1e072274001ba07d4bb10a92fb7e3e16d1838"

// nixSystem maps a Dagger container platform to a Nix system string.
func nixSystem(ctx context.Context, ctr *dagger.Container) (string, error) {
	plat, err := ctr.Platform(ctx)
	if err != nil {
		return "", fmt.Errorf("detecting platform: %w", err)
	}
	switch {
	case strings.Contains(string(plat), "arm64"):
		return "aarch64-linux", nil
	case strings.Contains(string(plat), "amd64"):
		return "x86_64-linux", nil
	default:
		return "", fmt.Errorf("unsupported platform: %s", plat)
	}
}

// base returns a container with nix and flakes enabled.
func (m *Nix) base() *dagger.Container {
	ctr := dag.Container().From(nixImage)
	return ctr.
		WithMountedCache("/nix/store", dag.CacheVolume("nix-store"),
			dagger.ContainerWithMountedCacheOpts{Source: ctr.Directory("/nix/store")}).
		WithMountedCache("/nix/var/nix", dag.CacheVolume("nix-var"),
			dagger.ContainerWithMountedCacheOpts{Source: ctr.Directory("/nix/var/nix")}).
		WithMountedCache("/root/.cache/nix", dag.CacheVolume("nix-eval-cache")).
		WithEnvVariable("NIX_CONFIG", "experimental-features = nix-command flakes\n").
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
  outputs = { src, ... }: builtins.removeAttrs src.outputs [ "nixosConfigurations" "inventory" ];
}`
	return m.base().
		WithNewFile("/tmp/check/flake.nix", wrapper).
		WithWorkdir("/tmp/check").
		WithExec([]string{"nix", "flake", "check", "--no-build", "--no-write-lock-file"})
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

// BuildHome builds and validates home-manager activation packages
// that match the current engine platform.
// +check
func (m *Nix) BuildHome(ctx context.Context) error {
	base := m.base()
	sys, err := nixSystem(ctx, base)
	if err != nil {
		return err
	}
	configs, err := m.homeConfigs(ctx)
	if err != nil {
		return err
	}
	eg, ctx := errgroup.WithContext(ctx)
	for _, name := range configs {
		if !strings.HasSuffix(name, sys) {
			continue
		}
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

// homeClosurePath builds the home-manager activation package and returns
// the nix store path. Used by [Nix.Sbom] and [Nix.Vulnscan].
func (m *Nix) homeClosurePath(ctx context.Context) (string, error) {
	base := m.base()
	sys, err := nixSystem(ctx, base)
	if err != nil {
		return "", err
	}
	config := fmt.Sprintf(`.#homeConfigurations."dev@%s".activationPackage`, sys)
	out, err := base.
		WithExec([]string{"nix", "build", config, "--no-link", "--print-out-paths"}).
		Stdout(ctx)
	if err != nil {
		return "", fmt.Errorf("building home closure: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// Sbom generates a CycloneDX SBOM for the home-manager closure.
func (m *Nix) Sbom(ctx context.Context) (*dagger.File, error) {
	storePath, err := m.homeClosurePath(ctx)
	if err != nil {
		return nil, err
	}

	return m.base().
		WithExec([]string{"nix", "profile", "install", "nixpkgs#sbomnix"}).
		WithExec([]string{"sbomnix", storePath,
			"--cdx", "/tmp/sbom.cdx.json"}).
		File("/tmp/sbom.cdx.json"), nil
}

// DependencySnapshot generates a GitHub dependency submission snapshot
// from the CycloneDX SBOM. The returned JSON can be POSTed directly to
// the GitHub dependency submission API. The sha and ref parameters
// identify the commit; correlator deduplicates submissions; runURL
// links back to the CI run.
func (m *Nix) DependencySnapshot(
	ctx context.Context,
	// Git commit SHA for the snapshot.
	sha string,
	// Git ref (e.g. "refs/heads/master").
	ref string,
	// Unique correlator string for deduplication.
	correlator string,
	// URL of the CI run that produced this snapshot.
	runURL string,
) (*dagger.File, error) {
	sbom, err := m.Sbom(ctx)
	if err != nil {
		return nil, err
	}

	sbomBytes, err := sbom.Contents(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading sbom: %w", err)
	}

	var cdx cdxBOM
	if err := json.Unmarshal([]byte(sbomBytes), &cdx); err != nil {
		return nil, fmt.Errorf("parsing sbom: %w", err)
	}

	snapshot := buildDependencySnapshot(cdx, sha, ref, correlator, runURL)

	out, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling snapshot: %w", err)
	}

	return dag.Directory().
		WithNewFile("snapshot.json", string(out)).
		File("snapshot.json"), nil
}

// Vulnscan checks the home-manager closure for known CVEs using vulnix.
func (m *Nix) Vulnscan(ctx context.Context) (string, error) {
	storePath, err := m.homeClosurePath(ctx)
	if err != nil {
		return "", err
	}

	out, err := m.base().
		WithExec([]string{"nix", "profile", "install", "nixpkgs#vulnix"}).
		WithExec([]string{"vulnix", storePath},
			dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny}).
		Stdout(ctx)
	if err != nil {
		return "", fmt.Errorf("running vulnix: %w", err)
	}
	return out, nil
}

// VulnscanSarif scans the home-manager closure for known CVEs and
// returns the results as a SARIF v2.1.0 JSON file suitable for upload
// to GitHub Code Scanning. Unlike [Nix.Vulnscan], which returns raw text,
// this method produces structured output that GitHub can render as
// security alerts. Results for directly declared packages point to the
// .nix file where they appear; transitive dependencies fall back to
// the nixpkgs rev in flake.lock.
func (m *Nix) VulnscanSarif(ctx context.Context) (*dagger.File, error) {
	out, err := m.Vulnscan(ctx)
	if err != nil {
		return nil, err
	}

	packages, err := sarif.ParseVulnix(out)
	if err != nil {
		return nil, fmt.Errorf("parsing vulnix output: %w", err)
	}

	locations, err := m.findPackageLocations(ctx, packages)
	if err != nil {
		return nil, fmt.Errorf("finding package locations: %w", err)
	}

	fallbackLine, err := m.findNixpkgsLine(ctx)
	if err != nil {
		fallbackLine = 1
	}

	log := sarif.BuildLog(packages, locations, "flake.lock", fallbackLine)

	data, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling sarif: %w", err)
	}

	return dag.Directory().
		WithNewFile("vulnix.sarif.json", string(data)).
		File("vulnix.sarif.json"), nil
}

// findPackageLocations greps .nix source files for each vulnerable
// package name and returns the first match as a [sarif.SourceLocation].
// Only matches Nix package reference patterns (pkgs.NAME or bare
// identifiers on their own line) to avoid false positives from
// attribute names like "network = {".
func (m *Nix) findPackageLocations(ctx context.Context, packages []sarif.VulnixPackage) (map[string]sarif.SourceLocation, error) {
	locations := make(map[string]sarif.SourceLocation)

	if len(packages) == 0 {
		return locations, nil
	}

	// Build grep -E patterns that match Nix package references:
	//   pkgs.NAME or pkgs."NAME"  (attribute access)
	//   ^\s+NAME\s*$              (bare name on own line, inside a with pkgs block)
	var pkgPatterns, barePatterns []string
	for _, pkg := range packages {
		pkgPatterns = append(pkgPatterns, fmt.Sprintf(`pkgs\.%s\b`, pkg.Name))
		pkgPatterns = append(pkgPatterns, fmt.Sprintf(`pkgs\."%s"`, pkg.Name))
		barePatterns = append(barePatterns, fmt.Sprintf(`^\s+%s\s*$`, regexp.QuoteMeta(pkg.Name)))
	}
	allPatterns := append(pkgPatterns, barePatterns...)
	pattern := strings.Join(allPatterns, "|")

	grepOut, err := m.base().
		WithExec([]string{"sh", "-c",
			fmt.Sprintf("grep -rEnl --include='*.nix' '%s' /src/home/ /src/hosts/ 2>/dev/null || true", pattern)}).
		Stdout(ctx)
	if err != nil {
		return locations, nil
	}

	// For each file that matched, get line numbers.
	files := strings.Fields(strings.TrimSpace(grepOut))
	if len(files) == 0 {
		return locations, nil
	}

	grepOut, err = m.base().
		WithExec([]string{"sh", "-c",
			fmt.Sprintf("grep -rEn --include='*.nix' '%s' %s 2>/dev/null || true",
				pattern, strings.Join(files, " "))}).
		Stdout(ctx)
	if err != nil {
		return locations, nil
	}

	for _, line := range strings.Split(grepOut, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}
		file := strings.TrimPrefix(parts[0], "/src/")
		lineNum, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		content := parts[2]

		for _, pkg := range packages {
			if _, ok := locations[pkg.Name]; ok {
				continue
			}
			if sarif.MatchesPkgRef(content, pkg.Name) {
				locations[pkg.Name] = sarif.SourceLocation{
					URI:  file,
					Line: lineNum,
				}
			}
		}
	}

	return locations, nil
}

// findNixpkgsLine finds the line number of the "nixpkgs" node in flake.lock.
func (m *Nix) findNixpkgsLine(ctx context.Context) (int, error) {
	// Match the top-level nixpkgs node (4-space indent, opening brace).
	out, err := m.base().
		WithExec([]string{"sh", "-c", `grep -n '^    "nixpkgs": {' /src/flake.lock | head -1`}).
		Stdout(ctx)
	if err != nil {
		return 1, err
	}
	parts := strings.SplitN(strings.TrimSpace(out), ":", 2)
	if len(parts) < 1 {
		return 1, fmt.Errorf("nixpkgs not found in flake.lock")
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil {
		return 1, err
	}
	return n, nil
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
