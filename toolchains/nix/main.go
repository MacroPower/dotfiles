// Nix configuration validation toolchain
//
// Validates the nix-darwin, NixOS, and home-manager configurations of
// the flake through evaluation, static analysis, and formatting checks.
// The checks run in Linux containers and do not build a darwin
// derivation.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sync/errgroup"

	"dagger/nix/internal/dagger"
)

// Nix validates the nix-darwin, NixOS, and home-manager configurations
// of the flake.
type Nix struct {
	// Project source containing flake.nix
	Source *dagger.Directory
}

// New creates a [Nix] module with the project source directory.
//
// The source holds the Dagger modules under toolchains/, so that
// [Nix.Lint], [Nix.Format], and [Nix.Editorconfig] check their files.
// [Nix.CheckLint] reads the Go modules of this toolchain from the same
// directory. The ignore list of source leaves out the files that Dagger
// generates in those modules, which the .gitignore file of each module
// lists.
func New(
	// Project source directory
	// +defaultPath="/"
	// +ignore=["**/.git", "result", "lima/", ".devbox/", ".worktrees/", ".claude/worktrees/", "**/.claude/scheduled_tasks.lock", "**/.ruff_cache", "toolchains/**/dagger.gen.go", "toolchains/**/internal/dagger", "toolchains/**/internal/querybuilder", "toolchains/**/internal/telemetry", "toolchains/**/.env"]
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

// FlakeCheck validates the schema and the outputs of the flake with
// `nix flake check --no-build`. The command evaluates each output that
// the flake defines per system, such as checks, only for the system of
// the Dagger engine.
//
// The check skips the nixosConfigurations output of the flake, because
// [Nix.Build] evaluates each NixOS configuration.
// +check
func (m *Nix) FlakeCheck(ctx context.Context) error {
	wrapper := `{
  inputs.src.url = "path:/src";
  outputs = { src, ... }: builtins.removeAttrs src.outputs [ "nixosConfigurations" "inventory" ];
}`

	_, err := m.base().
		WithNewFile("/tmp/check/flake.nix", wrapper).
		WithWorkdir("/tmp/check").
		WithExec([]string{"nix", "flake", "check", "--no-build", "--no-write-lock-file"}).
		Sync(ctx)
	if err != nil {
		return fmt.Errorf("checking the flake: %w", err)
	}

	return nil
}

// configNames returns the name of each configuration that the flake
// defines in output, such as darwinConfigurations.
func (m *Nix) configNames(ctx context.Context, output string) ([]string, error) {
	out, err := m.base().
		WithExec([]string{
			"nix", "eval", "--json",
			".#" + output, "--apply", "builtins.attrNames",
		}).
		Stdout(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", output, err)
	}

	var names []string

	err = json.Unmarshal([]byte(out), &names)
	if err != nil {
		return nil, fmt.Errorf("parsing the names of %s: %w", output, err)
	}

	return names, nil
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
		return nil, fmt.Errorf("evaluating home configuration: %w", err)
	}

	var info homeInfo

	err = json.Unmarshal([]byte(out), &info)
	if err != nil {
		return nil, fmt.Errorf("parsing home configuration: %w", err)
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

	ctr = ctr.WithExec([]string{
		"test", "-d",
		fmt.Sprintf("%s/.local/state/home-manager/gcroots/current-home/home-path/bin", info.HomeDirectory),
	})

	_, err = ctr.Sync(ctx)
	if err != nil {
		return fmt.Errorf("home %s: %w", name, err)
	}

	return nil
}

// BuildHome builds and validates home-manager activation packages
// that match the current engine platform.
//
// A full build of each activation package is too slow for every run
// of the checks, so `dagger check` does not run this function.
// `task nix:build:home` runs it with `dagger call nix build-home`.
// In each run of the checks, [Nix.Build] evaluates each home-manager
// configuration of the flake, whatever its system is. It does not
// build an activation package.
func (m *Nix) BuildHome(ctx context.Context) error {
	base := m.base()
	sys, err := nixSystem(ctx, base)
	if err != nil {
		return err
	}

	configs, err := m.configNames(ctx, "homeConfigurations")
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

	err = eg.Wait()
	if err != nil {
		return fmt.Errorf("validating home configurations: %w", err)
	}

	return nil
}

// Build evaluates the system derivation of each darwin configuration
// and of each NixOS configuration of the flake. It also evaluates the
// derivation of the activation package of each home-manager
// configuration. It evaluates the configurations for every system,
// whatever the system of the Dagger engine is, and it does not build a
// derivation.
// +check
func (m *Nix) Build(ctx context.Context) error {
	// Each entry names a flake output that holds host configurations.
	// drvPath is the attribute path, from a configuration of that output,
	// of the store path of the derivation that builds the configuration.
	// That derivation builds the system of a darwin or a NixOS
	// configuration, and it builds the activation package of a
	// home-manager configuration.
	outputs := []struct{ name, drvPath string }{
		{name: "darwinConfigurations", drvPath: "system.drvPath"},
		{name: "nixosConfigurations", drvPath: "config.system.build.toplevel.drvPath"},
		{name: "homeConfigurations", drvPath: "activationPackage.drvPath"},
	}

	var refs []string

	for _, output := range outputs {
		names, err := m.configNames(ctx, output.name)
		if err != nil {
			return err
		}

		for _, name := range names {
			refs = append(refs, fmt.Sprintf(".#%s.%q.%s", output.name, name, output.drvPath))
		}
	}

	eg, ctx := errgroup.WithContext(ctx)
	for _, ref := range refs {
		eg.Go(func() error {
			_, err := m.base().
				WithExec([]string{"nix", "eval", ref}).
				Sync(ctx)
			if err != nil {
				return fmt.Errorf("%s: %w", ref, err)
			}

			return nil
		})
	}

	err := eg.Wait()
	if err != nil {
		return fmt.Errorf("evaluating host configurations: %w", err)
	}

	return nil
}

// Lint checks formatting and lint rules via treefmt.
//
// treefmt runs nixfmt, deadnix, statix, shfmt, prettier, and gofmt.
// +check
func (m *Nix) Lint(ctx context.Context) error {
	_, err := m.base().
		WithExec([]string{"nix", "fmt", "--", "--fail-on-change"}).
		Sync(ctx)
	if err != nil {
		return fmt.Errorf("running treefmt: %w", err)
	}

	return nil
}

// Editorconfig validates files against .editorconfig rules.
// +check
func (m *Nix) Editorconfig(ctx context.Context) error {
	// nix run rather than nix profile install: the profile lives on the
	// persistent /nix/var/nix cache volume, and reinstalling after a
	// nixpkgs-unstable bump conflicts with the previously installed copy.
	_, err := m.base().
		WithExec([]string{"nix", "run", "nixpkgs#editorconfig-checker", "--", "-no-color"}).
		Sync(ctx)
	if err != nil {
		return fmt.Errorf("running editorconfig-checker: %w", err)
	}

	return nil
}

// Format applies all auto-fixes via treefmt.
//
// treefmt runs nixfmt, deadnix, statix, shfmt, prettier, and gofmt.
// The treefmt config in flake.nix keeps gofmt off the Go files of the
// dev toolchain, because the format function of that toolchain formats
// them. `dagger generate` fails when two generators change one file.
// +generate
func (m *Nix) Format() *dagger.Changeset {
	fixed := m.base().
		WithExec([]string{"nix", "fmt"}).
		Directory("/src")

	return dag.Directory().WithDirectory(".", fixed).Changes(m.Source)
}

// sbomnixRef pins the upstream sbomnix flake. nixpkgs ships 1.7.4
// which cannot parse the `nix derivation show` JSON format produced
// by Nix 2.33+ (and Lix 2.94 used here), failing with "Deriver does
// not exist" on every closure path. v1.7.6 normalizes the new
// wrapped payload. https://github.com/tiiuae/sbomnix/issues/267
const sbomnixRef = "github:tiiuae/sbomnix/v1.7.6"

// Sbom generates a CycloneDX SBOM for the home-manager closure.
func (m *Nix) Sbom(ctx context.Context) (*dagger.File, error) {
	base := m.base()
	sys, err := nixSystem(ctx, base)
	if err != nil {
		return nil, err
	}

	config := fmt.Sprintf(`.#homeConfigurations."dev@%s".activationPackage`, sys)

	out, err := base.
		WithExec([]string{"nix", "build", config, "--no-link", "--print-out-paths"}).
		Stdout(ctx)
	if err != nil {
		return nil, fmt.Errorf("building home closure: %w", err)
	}

	storePath := strings.TrimSpace(out)

	return base.
		WithExec([]string{
			"nix", "run", sbomnixRef + "#sbomnix", "--",
			storePath, "--cdx", "/tmp/sbom.cdx.json",
		}).
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

	err = json.Unmarshal([]byte(sbomBytes), &cdx)
	if err != nil {
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

	grypeConfig := m.Source.File(".grype.yaml")

	return m.base().
		WithMountedCache("/root/.cache/grype", dag.CacheVolume("grype-db")).
		WithExec([]string{"nix", "profile", "install", "nixpkgs#grype"}).
		WithFile("/tmp/sbom.cdx.json", sbom).
		WithFile("/tmp/.grype.yaml", grypeConfig), nil
}

// Vulnscan checks the home-manager closure for known CVEs using grype.
func (m *Nix) Vulnscan(ctx context.Context) (string, error) {
	ctr, err := m.grypeBase(ctx)
	if err != nil {
		return "", err
	}

	out, err := ctr.
		WithExec([]string{"grype", "--config", "/tmp/.grype.yaml", "sbom:/tmp/sbom.cdx.json"},
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
		WithExec([]string{
			"grype", "--config", "/tmp/.grype.yaml",
			"sbom:/tmp/sbom.cdx.json",
			"--output", "sarif",
			"--file", "/tmp/grype.sarif.json",
		},
			dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny}).
		File("/tmp/grype.sarif.json").
		Contents(ctx)
	if err != nil {
		return nil, fmt.Errorf("running grype: %w", err)
	}

	pkgNames := packageNamesFromSarif(raw)

	locations := m.findPackageLocations(ctx, pkgNames)

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

	err := json.Unmarshal([]byte(raw), &doc)
	if err != nil {
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
//
// It returns no location when the grep does not run. [Nix.VulnscanSarif]
// then points each result at the nixpkgs pin in flake.lock.
func (m *Nix) findPackageLocations(ctx context.Context, names []string) map[string]sourceLocation {
	locations := make(map[string]sourceLocation)
	if len(names) == 0 {
		return locations
	}

	var pkgPatterns, barePatterns []string
	for _, name := range names {
		pkgPatterns = append(pkgPatterns, `pkgs\.`+name+`\b`, `pkgs\."`+name+`"`)
		barePatterns = append(barePatterns, `^\s+`+regexp.QuoteMeta(name)+`\s*$`)
	}

	pattern := strings.Join(slices.Concat(pkgPatterns, barePatterns), "|")

	grepOut, err := m.base().
		WithExec([]string{
			"sh", "-c",
			fmt.Sprintf("grep -rEn --include='*.nix' '%s' /src/home/ /src/hosts/ 2>/dev/null || true", pattern),
		}).
		Stdout(ctx)
	if err != nil {
		return locations
	}

	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	for line := range strings.SplitSeq(grepOut, "\n") {
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

	return locations
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
		return 1, fmt.Errorf("searching flake.lock: %w", err)
	}

	parts := strings.SplitN(strings.TrimSpace(out), ":", 2)
	if len(parts) < 1 {
		return 1, fmt.Errorf("nixpkgs not found in flake.lock")
	}

	n, err := strconv.Atoi(parts[0])
	if err != nil {
		return 1, fmt.Errorf("parsing line number: %w", err)
	}

	return n, nil
}

// jsonObject returns v as a JSON object. It returns nil when v holds
// another type, and each key of a nil object reads as nil.
func jsonObject(v any) map[string]any {
	object, ok := v.(map[string]any)
	if !ok {
		return nil
	}

	return object
}

// jsonArray returns v as a JSON array. It returns nil when v holds
// another type.
func jsonArray(v any) []any {
	array, ok := v.([]any)
	if !ok {
		return nil
	}

	return array
}

// patchSarifLocations fills in artifact locations in SARIF results.
// Results whose package has a source location point to that .nix file;
// all others fall back to fallbackURI at fallbackLine.
func patchSarifLocations(
	raw string,
	locations map[string]sourceLocation,
	fallbackURI string,
	fallbackLine int,
) (string, error) {
	var doc map[string]any

	err := json.Unmarshal([]byte(raw), &doc)
	if err != nil {
		return "", fmt.Errorf("parsing sarif: %w", err)
	}

	for _, run := range jsonArray(doc["runs"]) {
		for _, result := range jsonArray(jsonObject(run)["results"]) {
			resultMap := jsonObject(result)

			uri := fallbackURI
			line := fallbackLine

			if ruleID, ok := resultMap["ruleId"].(string); ok {
				if m := cveRuleIDRe.FindStringSubmatch(ruleID); m != nil {
					if loc, ok := locations[m[1]]; ok {
						uri = loc.uri
						line = loc.line
					}
				}
			}

			for _, loc := range jsonArray(resultMap["locations"]) {
				phys := jsonObject(jsonObject(loc)["physicalLocation"])
				if phys == nil {
					continue
				}

				art := jsonObject(phys["artifactLocation"])
				if art == nil {
					continue
				}

				art["uri"] = uri

				region := jsonObject(phys["region"])
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
		return "", fmt.Errorf("marshaling sarif: %w", err)
	}

	return string(out), nil
}

// All runs all checks concurrently.
func (m *Nix) All(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error { return m.FlakeCheck(ctx) })
	eg.Go(func() error { return m.Build(ctx) })
	eg.Go(func() error { return m.BuildHome(ctx) })
	eg.Go(func() error { return m.Lint(ctx) })
	eg.Go(func() error { return m.Editorconfig(ctx) })
	eg.Go(func() error { return m.CheckLint(ctx) })

	err := eg.Wait()
	if err != nil {
		return fmt.Errorf("running all checks: %w", err)
	}

	return nil
}
