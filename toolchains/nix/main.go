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
		WithEnvVariable("NIX_CONFIG", "experimental-features = nix-command flakes\nmax-jobs = auto\n").
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
// the nix store path. Used by [Nix.Sbom].
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

// grypeBase returns a container with grype installed and the vulnerability
// database cached. The SBOM file is mounted for scanning.
func (m *Nix) grypeBase(ctx context.Context) (*dagger.Container, error) {
	sbom, err := m.Sbom(ctx)
	if err != nil {
		return nil, err
	}

	return m.base().
		WithMountedCache("/root/.cache/grype", dag.CacheVolume("grype-db")).
		WithExec([]string{"nix", "profile", "install", "nixpkgs#grype"}).
		WithFile("/tmp/sbom.cdx.json", sbom), nil
}

// Vulnscan checks the home-manager closure for known CVEs using grype.
func (m *Nix) Vulnscan(ctx context.Context) (string, error) {
	ctr, err := m.grypeBase(ctx)
	if err != nil {
		return "", err
	}

	out, err := ctr.
		WithExec([]string{"grype", "sbom:/tmp/sbom.cdx.json"},
			dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny}).
		Stdout(ctx)
	if err != nil {
		return "", fmt.Errorf("running grype: %w", err)
	}
	return out, nil
}

// VulnscanSarif scans the home-manager closure for known CVEs and
// returns the results as a SARIF v2.1.0 JSON file suitable for upload
// to GitHub Code Scanning. Unlike [Nix.Vulnscan], which returns raw text,
// this method produces structured output that GitHub can render as
// security alerts.
//
// Grype produces empty artifact locations when scanning SBOMs. Since
// GitHub Code Scanning requires every result to reference a file in
// the repo, results are patched: directly declared packages point to
// the .nix file where they appear; transitive dependencies fall back
// to the nixpkgs pin in flake.lock.
func (m *Nix) VulnscanSarif(ctx context.Context) (*dagger.File, error) {
	ctr, err := m.grypeBase(ctx)
	if err != nil {
		return nil, err
	}

	raw, err := ctr.
		WithExec([]string{"grype", "sbom:/tmp/sbom.cdx.json",
			"--output", "sarif",
			"--file", "/tmp/grype.sarif.json"},
			dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny}).
		File("/tmp/grype.sarif.json").
		Contents(ctx)
	if err != nil {
		return nil, fmt.Errorf("running grype: %w", err)
	}

	pkgNames := packageNamesFromSarif(raw)

	locations, err := m.findPackageLocations(ctx, pkgNames)
	if err != nil {
		return nil, fmt.Errorf("finding package locations: %w", err)
	}

	fallbackLine, err := m.findNixpkgsLine(ctx)
	if err != nil {
		fallbackLine = 1
	}

	patched, err := patchSarifLocations(raw, locations, "flake.lock", fallbackLine)
	if err != nil {
		return nil, fmt.Errorf("patching sarif locations: %w", err)
	}

	return dag.Directory().
		WithNewFile("grype.sarif.json", patched).
		File("grype.sarif.json"), nil
}

// cveRuleIDRe extracts the package name from grype's ruleId format:
// "CVE-YYYY-NNNNN-packagename" -> "packagename".
var cveRuleIDRe = regexp.MustCompile(`^CVE-\d+-\d+-(.+)$`)

// packageNamesFromSarif extracts unique package names from grype SARIF
// ruleIds. Grype uses the format "CVE-YYYY-NNNNN-pkgname".
func packageNamesFromSarif(raw string) []string {
	var doc struct {
		Runs []struct {
			Results []struct {
				RuleID string `json:"ruleId"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil
	}

	seen := make(map[string]bool)
	var names []string
	for _, run := range doc.Runs {
		for _, r := range run.Results {
			m := cveRuleIDRe.FindStringSubmatch(r.RuleID)
			if m == nil {
				continue
			}
			name := m[1]
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	return names
}

// sourceLocation maps a package name to the file and line where it is declared.
type sourceLocation struct {
	uri  string
	line int
}

// findPackageLocations greps .nix source files for each package name
// and returns the first match as a source location. Only matches Nix
// package reference patterns (pkgs.NAME, pkgs."NAME", or bare name on
// its own line) to avoid false positives from attribute names.
func (m *Nix) findPackageLocations(ctx context.Context, names []string) (map[string]sourceLocation, error) {
	locations := make(map[string]sourceLocation)
	if len(names) == 0 {
		return locations, nil
	}

	var pkgPatterns, barePatterns []string
	for _, name := range names {
		pkgPatterns = append(pkgPatterns, fmt.Sprintf(`pkgs\.%s\b`, name))
		pkgPatterns = append(pkgPatterns, fmt.Sprintf(`pkgs\."%s"`, name))
		barePatterns = append(barePatterns, fmt.Sprintf(`^\s+%s\s*$`, regexp.QuoteMeta(name)))
	}
	allPatterns := append(pkgPatterns, barePatterns...)
	pattern := strings.Join(allPatterns, "|")

	grepOut, err := m.base().
		WithExec([]string{"sh", "-c",
			fmt.Sprintf("grep -rEn --include='*.nix' '%s' /src/home/ /src/hosts/ 2>/dev/null || true", pattern)}).
		Stdout(ctx)
	if err != nil {
		return locations, nil
	}

	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
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

		for name := range nameSet {
			if _, ok := locations[name]; ok {
				continue
			}
			if matchesPkgRef(content, name) {
				locations[name] = sourceLocation{uri: file, line: lineNum}
			}
		}
	}

	return locations, nil
}

// matchesPkgRef reports whether a Nix source line contains a package
// reference for name: pkgs.NAME, pkgs."NAME", or NAME as the sole
// token on a line (bare name inside a with pkgs block).
func matchesPkgRef(line, name string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == name {
		return true
	}
	if strings.Contains(line, `pkgs."`+name+`"`) {
		return true
	}
	prefix := "pkgs." + name
	idx := strings.Index(line, prefix)
	if idx >= 0 {
		end := idx + len(prefix)
		if end >= len(line) {
			return true
		}
		next := line[end]
		if !isNixIdentChar(next) {
			return true
		}
	}
	return false
}

func isNixIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') || c == '_' || c == '\'' || c == '-'
}

// findNixpkgsLine finds the line number of the "nixpkgs" node in flake.lock.
func (m *Nix) findNixpkgsLine(ctx context.Context) (int, error) {
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

// patchSarifLocations fills in artifact locations in SARIF results.
// Results whose package has a source location point to that .nix file;
// all others fall back to fallbackURI at fallbackLine.
func patchSarifLocations(raw string, locations map[string]sourceLocation, fallbackURI string, fallbackLine int) (string, error) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return "", err
	}

	runs, _ := doc["runs"].([]any)
	for _, run := range runs {
		runMap, _ := run.(map[string]any)
		results, _ := runMap["results"].([]any)
		for _, result := range results {
			resultMap, _ := result.(map[string]any)
			ruleID, _ := resultMap["ruleId"].(string)

			uri := fallbackURI
			line := fallbackLine
			if m := cveRuleIDRe.FindStringSubmatch(ruleID); m != nil {
				if loc, ok := locations[m[1]]; ok {
					uri = loc.uri
					line = loc.line
				}
			}

			locs, _ := resultMap["locations"].([]any)
			for _, loc := range locs {
				locMap, _ := loc.(map[string]any)
				phys, _ := locMap["physicalLocation"].(map[string]any)
				if phys == nil {
					continue
				}
				art, _ := phys["artifactLocation"].(map[string]any)
				if art == nil {
					continue
				}
				art["uri"] = uri
				region, _ := phys["region"].(map[string]any)
				if region != nil {
					region["startLine"] = line
					region["startColumn"] = 1
					region["endLine"] = line
					region["endColumn"] = 1
				}
			}
		}
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
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
