// Integration tests for the [Nix] module.
//
// Individual tests are annotated with +check so
// `dagger check -m toolchains/nix/tests` runs them all.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/sync/errgroup"

	"dagger/tests/internal/dagger"
)

// Tests provides integration tests for the [Nix] module.
type Tests struct{}

// All runs every test that carries +check in parallel.
// [Tests.TestAggregates] fails when a test with that pragma is missing
// here.
func (m *Tests) All(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(m.TestAggregates)
	eg.Go(func() error { return m.TestCheckLintRootModule(ctx) })
	eg.Go(func() error { return m.TestCheckLintTestModule(ctx) })
	eg.Go(func() error { return m.TestDependencySnapshot(ctx) })
	eg.Go(func() error { return m.TestEditorconfigToolchains(ctx) })
	eg.Go(func() error { return m.TestFormatSkipsDevGo(ctx) })
	eg.Go(func() error { return m.TestFormatToolchains(ctx) })
	eg.Go(func() error { return m.TestLintToolchains(ctx) })
	eg.Go(func() error { return m.TestSbom(ctx) })
	eg.Go(func() error { return m.TestSourceToolchains(ctx) })
	eg.Go(func() error { return m.TestVulnscanSarif(ctx) })

	err := eg.Wait()
	if err != nil {
		return fmt.Errorf("running the +check tests: %w", err)
	}

	return nil
}

// AllFull runs every test of the module.
// `task toolchains:integration:full` calls it in the test module of
// each toolchain. Every test of this module carries +check, so the
// function calls only [Tests.All]. It must also call each test that
// someone adds without the pragma. [Tests.TestAggregates] fails when
// a test is missing here.
func (m *Tests) AllFull(ctx context.Context) error {
	return m.All(ctx)
}

const (
	// toolchainGoPath is the path, in the project source, of a Go file
	// under toolchains/.
	toolchainGoPath = "toolchains/nix/main.go"

	// devGoPath is the path, in the project source, of a Go file of the
	// dev toolchain.
	devGoPath = "toolchains/dev/main.go"

	// devTestsGoPath is the path, in the project source, of a Go file of
	// the test module of the dev toolchain.
	devTestsGoPath = "toolchains/dev/tests/main.go"

	// unformattedFunc is a function declaration that gofmt changes,
	// because two spaces follow the func keyword.
	unformattedFunc = "\nfunc  unformatted() {}\n"

	// formattedFunc is unformattedFunc as gofmt writes it.
	formattedFunc = "\nfunc unformatted() {}\n"

	// trailingSpaceLine is a comment line that breaks the .editorconfig
	// rules of the repository, because it ends with a space.
	trailingSpaceLine = "// trailing space \n"

	// rootModulePath is the path, in the project source, of the root
	// module of the nix toolchain.
	rootModulePath = "toolchains/nix"

	// testModulePath is the path, in the project source, of the test
	// module of the nix toolchain.
	testModulePath = "toolchains/nix/tests"

	// lintProbeName is the name of the Go file that a test adds to a
	// module of the nix toolchain.
	lintProbeName = "lint_probe.go"

	// unwrappedProbe is a Go file of the root module whose function
	// returns the error of another package unwrapped. golangci-lint
	// reports that return with wrapcheck and reports nothing else in
	// the file.
	unwrappedProbe = `package main

import "strconv"

// lintProbe returns the error of another package unwrapped.
func lintProbe(s string) (int, error) {
	return strconv.Atoi(s)
}

var _ = lintProbe
`

	// undocumentedProbe is a Go file of the test module whose exported
	// method has no doc comment. golangci-lint reports that method with
	// godoclint and reports nothing else in the file.
	undocumentedProbe = "package main\n\nfunc (m *Tests) LintProbe() {}\n"
)

// sourceFile returns the contents of the file at path in the default
// source of [Nix]. It returns an error when the default source leaves
// out toolchains/ and path is under toolchains/. Each test that reads
// such a path with it therefore fails in that case. Dagger applies the
// ignore list of the source argument only to the default source. A
// source that a test passes to [Nix] therefore cannot show that fault.
func sourceFile(ctx context.Context, path string) (string, error) {
	contents, err := dag.Nix().Source().File(path).Contents(ctx)
	if err != nil {
		return "", fmt.Errorf("reading %s from the project source: %w", path, err)
	}

	return contents, nil
}

// nixWithFile returns the [Nix] module whose source is the default
// source with contents in the file at path.
func nixWithFile(path, contents string) *dagger.Nix {
	return dag.Nix(dagger.NixOpts{
		Source: dag.Nix().Source().WithNewFile(path, contents),
	})
}

// nixWithToolchainGo returns the [Nix] module whose source is the default
// source with contents in the file at toolchainGoPath.
func nixWithToolchainGo(contents string) *dagger.Nix {
	return nixWithFile(toolchainGoPath, contents)
}

// editorconfigSource returns a directory that holds the .editorconfig
// file of the default source of [Nix] and contents in the file at
// toolchainGoPath. The directory holds no other file, so a file outside
// toolchains/ cannot fail a check of it.
func editorconfigSource(contents string) *dagger.Directory {
	return dag.Directory().
		WithFile(".editorconfig", dag.Nix().Source().File(".editorconfig")).
		WithNewFile(toolchainGoPath, contents)
}

// TestSourceToolchains verifies that the default source of [Nix] holds
// the Dagger modules under toolchains/ and leaves out the files that
// Dagger generates in them. A checkout holds the generated files only
// after Dagger wrote them there, such as when `dagger develop` ran. In
// another checkout, the test proves only the first claim.
//
// +check
func (m *Tests) TestSourceToolchains(ctx context.Context) error {
	src := dag.Nix().Source()

	got, err := src.Glob(ctx, "toolchains/**/main.go")
	if err != nil {
		return fmt.Errorf("listing main.go files: %w", err)
	}

	for _, want := range []string{
		"toolchains/dev/main.go",
		"toolchains/dev/tests/main.go",
		"toolchains/nix/main.go",
		"toolchains/nix/tests/main.go",
	} {
		if !slices.Contains(got, want) {
			return fmt.Errorf("main.go files = %q, want them to hold %q", got, want)
		}
	}

	for _, pattern := range []string{
		"toolchains/**/dagger.gen.go",
		"toolchains/**/internal/dagger",
		"toolchains/**/internal/querybuilder",
		"toolchains/**/internal/telemetry",
		"toolchains/**/.env",
	} {
		got, err := src.Glob(ctx, pattern)
		if err != nil {
			return fmt.Errorf("listing %s: %w", pattern, err)
		}

		if len(got) > 0 {
			return fmt.Errorf("source holds generated paths %q, want none that match %s", got, pattern)
		}
	}

	return nil
}

// TestLintToolchains verifies that [Nix.Lint] checks the Go files under
// toolchains/. Lint passes when the source holds the unchanged file at
// toolchainGoPath. It fails when that file also holds a function that
// gofmt changes.
//
// +check
func (m *Tests) TestLintToolchains(ctx context.Context) error {
	formatted, err := sourceFile(ctx, toolchainGoPath)
	if err != nil {
		return err
	}

	err = nixWithToolchainGo(formatted).Lint(ctx)
	if err != nil {
		return fmt.Errorf("linting the unchanged %s: %w", toolchainGoPath, err)
	}

	err = nixWithToolchainGo(formatted + unformattedFunc).Lint(ctx)
	if err == nil {
		return fmt.Errorf("lint passed %s with a function that gofmt changes", toolchainGoPath)
	}

	return nil
}

// TestFormatToolchains verifies that [Nix.Format] formats the Go files
// under toolchains/. When the file at toolchainGoPath also holds a
// function that gofmt changes, the changeset lists the file as modified.
// The changeset holds the function as gofmt writes it and leaves the
// other contents of the file unchanged.
//
// +check
func (m *Tests) TestFormatToolchains(ctx context.Context) error {
	formatted, err := sourceFile(ctx, toolchainGoPath)
	if err != nil {
		return err
	}

	changes := nixWithToolchainGo(formatted + unformattedFunc).Format()

	modified, err := changes.ModifiedPaths(ctx)
	if err != nil {
		return fmt.Errorf("reading modified paths: %w", err)
	}

	if !slices.Contains(modified, toolchainGoPath) {
		return fmt.Errorf("modified paths = %q, want them to hold %q", modified, toolchainGoPath)
	}

	contents, err := changes.After().File(toolchainGoPath).Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading %s after format: %w", toolchainGoPath, err)
	}

	got, ok := strings.CutPrefix(contents, formatted)
	if !ok {
		return fmt.Errorf("format changed the formatted part of %s", toolchainGoPath)
	}

	if want := formattedFunc; got != want {
		return fmt.Errorf("function after format = %q, want %q", got, want)
	}

	return nil
}

// TestFormatSkipsDevGo verifies that [Nix.Format] leaves the Go files of
// the dev toolchain and of its test module to the format function of
// that toolchain. Both format functions are generators, and
// `dagger generate` fails when two generators change the same file. The
// test adds a function that gofmt changes to the files at
// toolchainGoPath, devGoPath, and devTestsGoPath. It fails when the
// changeset of Format lists the file at devGoPath or the file at
// devTestsGoPath as modified. It also fails when the changeset leaves
// out the file at toolchainGoPath, because then Format did not run
// gofmt.
//
// +check
func (m *Tests) TestFormatSkipsDevGo(ctx context.Context) error {
	devFiles := []string{devGoPath, devTestsGoPath}
	src := dag.Nix().Source()

	for _, file := range append([]string{toolchainGoPath}, devFiles...) {
		contents, err := sourceFile(ctx, file)
		if err != nil {
			return err
		}

		src = src.WithNewFile(file, contents+unformattedFunc)
	}

	modified, err := dag.Nix(dagger.NixOpts{Source: src}).Format().ModifiedPaths(ctx)
	if err != nil {
		return fmt.Errorf("reading modified paths: %w", err)
	}

	if !slices.Contains(modified, toolchainGoPath) {
		return fmt.Errorf("modified paths = %q, want them to hold %q", modified, toolchainGoPath)
	}

	for _, file := range devFiles {
		if slices.Contains(modified, file) {
			return fmt.Errorf("modified paths = %q, want them to leave out %q", modified, file)
		}
	}

	return nil
}

// TestEditorconfigToolchains verifies that [Nix.Editorconfig] checks the
// files under toolchains/. The test runs Editorconfig on the source that
// [editorconfigSource] returns, so that the result depends only on the
// file at toolchainGoPath. Editorconfig passes when that file is
// unchanged. It fails when a line of that file ends with a space.
//
// +check
func (m *Tests) TestEditorconfigToolchains(ctx context.Context) error {
	clean, err := sourceFile(ctx, toolchainGoPath)
	if err != nil {
		return err
	}

	err = dag.Nix(dagger.NixOpts{Source: editorconfigSource(clean)}).Editorconfig(ctx)
	if err != nil {
		return fmt.Errorf("checking the unchanged %s: %w", toolchainGoPath, err)
	}

	err = dag.Nix(dagger.NixOpts{Source: editorconfigSource(clean + trailingSpaceLine)}).Editorconfig(ctx)
	if err == nil {
		return fmt.Errorf("editorconfig passed %s with a line that ends with a space", toolchainGoPath)
	}

	return nil
}

// checkLintProbe verifies that [Nix.CheckLint] lints the module of the
// nix toolchain at probed. It returns an error when the check fails on
// the default source of [Nix]. Then it adds a file with contents, in
// which golangci-lint reports a finding, to the module at probed. It
// returns an error when the check passes on that source. CheckLint
// names the module of each failed lint by its path from the root of the
// project source. So checkLintProbe also returns an error unless the
// error of the second check names the module at probed and leaves out
// the module at other.
func checkLintProbe(ctx context.Context, probed, other, contents string) error {
	err := dag.Nix().CheckLint(ctx)
	if err != nil {
		return fmt.Errorf("linting the default source: %w", err)
	}

	probe := probed + "/" + lintProbeName

	err = nixWithFile(probe, contents).CheckLint(ctx)
	if err == nil {
		return fmt.Errorf("CheckLint passed with a finding in %s", probe)
	}

	want := "linting " + probed + ":"
	if !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "linting "+other+":") {
		return fmt.Errorf("expected only the lint of %s to fail, got: %w", probed, err)
	}

	return nil
}

// TestCheckLintRootModule verifies that [Nix.CheckLint] lints the root
// module of the nix toolchain and reports an error of another package
// that a function returns unwrapped. The check passes on the default
// source of [Nix]. It fails after the test adds a file with such a
// function to the root module. The test requires the error of that
// check to name the lint of the root module and no other lint.
//
// +check
func (m *Tests) TestCheckLintRootModule(ctx context.Context) error {
	return checkLintProbe(ctx, rootModulePath, testModulePath, unwrappedProbe)
}

// TestCheckLintTestModule verifies that [Nix.CheckLint] lints the test
// module of the nix toolchain and reports an exported method that has
// no doc comment. The check passes on the default source of [Nix]. It
// fails after the test adds a file with such a method to the test
// module. The test requires the error of that check to name the lint of
// the test module and no other lint.
//
// +check
func (m *Tests) TestCheckLintTestModule(ctx context.Context) error {
	return checkLintProbe(ctx, testModulePath, rootModulePath, undocumentedProbe)
}

// ghSnapshot is the subset of the GitHub dependency snapshot format
// needed to validate [Nix.DependencySnapshot] output.
type ghSnapshot struct {
	Manifests map[string]ghManifest `json:"manifests"`
	Job       ghJob                 `json:"job"`
	Detector  ghDetector            `json:"detector"`
	SHA       string                `json:"sha"`
	Ref       string                `json:"ref"`
	Version   int                   `json:"version"`
}

type ghJob struct {
	ID         string `json:"id"`
	Correlator string `json:"correlator"`
	HTMLURL    string `json:"html_url"`
}

type ghDetector struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	URL     string `json:"url"`
}

type ghManifest struct {
	File     *ghManifestFile         `json:"file,omitempty"`
	Resolved map[string]ghDependency `json:"resolved"`
	Name     string                  `json:"name"`
}

type ghManifestFile struct {
	SourceLocation string `json:"source_location"`
}

type ghDependency struct {
	PackageURL   string `json:"package_url"`
	Relationship string `json:"relationship"`
}

// TestDependencySnapshot verifies that [Nix.DependencySnapshot] produces
// valid GitHub dependency submission JSON from the CycloneDX SBOM.
//
// +check
func (m *Tests) TestDependencySnapshot(ctx context.Context) error {
	f := dag.Nix().DependencySnapshot(
		"abc123def456",
		"refs/heads/master",
		"test-correlator",
		"https://github.com/example/repo/actions/runs/1",
	)

	contents, err := f.Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading snapshot: %w", err)
	}

	var snap ghSnapshot

	err = json.Unmarshal([]byte(contents), &snap)
	if err != nil {
		return fmt.Errorf("parsing snapshot JSON: %w", err)
	}

	if snap.Version != 0 {
		return fmt.Errorf("version = %d, want 0", snap.Version)
	}

	if snap.SHA != "abc123def456" {
		return fmt.Errorf("sha = %q, want %q", snap.SHA, "abc123def456")
	}

	if snap.Ref != "refs/heads/master" {
		return fmt.Errorf("ref = %q, want %q", snap.Ref, "refs/heads/master")
	}

	if snap.Job.Correlator != "test-correlator" {
		return fmt.Errorf("correlator = %q, want %q", snap.Job.Correlator, "test-correlator")
	}

	if snap.Detector.Name != "sbomnix" {
		return fmt.Errorf("detector = %q, want %q", snap.Detector.Name, "sbomnix")
	}

	manifest, ok := snap.Manifests["nix-closure"]
	if !ok {
		return fmt.Errorf("missing nix-closure manifest")
	}

	if manifest.File == nil || manifest.File.SourceLocation != "flake.nix" {
		return fmt.Errorf("manifest file source_location should be flake.nix")
	}

	if len(manifest.Resolved) == 0 {
		return fmt.Errorf("expected resolved dependencies, got none")
	}

	for purl, dep := range manifest.Resolved {
		if dep.PackageURL != purl {
			return fmt.Errorf("dependency key %q != package_url %q", purl, dep.PackageURL)
		}

		if dep.Relationship != "direct" {
			return fmt.Errorf("relationship = %q, want %q", dep.Relationship, "direct")
		}
	}

	return nil
}

// TestSbom verifies that [Nix.Sbom] produces valid CycloneDX JSON.
//
// +check
func (m *Tests) TestSbom(ctx context.Context) error {
	contents, err := dag.Nix().Sbom().Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading sbom: %w", err)
	}

	var bom struct {
		BOMFormat   string `json:"bomFormat"`
		SpecVersion string `json:"specVersion"`
		Components  []struct {
			Name string `json:"name"`
			Purl string `json:"purl"`
		} `json:"components"`
	}

	err = json.Unmarshal([]byte(contents), &bom)
	if err != nil {
		return fmt.Errorf("parsing sbom: %w", err)
	}

	if bom.BOMFormat != "CycloneDX" {
		return fmt.Errorf("bomFormat = %q, want %q", bom.BOMFormat, "CycloneDX")
	}

	if len(bom.Components) == 0 {
		return fmt.Errorf("expected components in SBOM, got none")
	}

	// Verify at least some components have purls (sbomnix should produce them).
	purlCount := 0
	for _, c := range bom.Components {
		if c.Purl != "" {
			purlCount++
		}
	}

	if purlCount == 0 {
		return fmt.Errorf("no components have purls")
	}

	return nil
}

// sarifLog is the subset of SARIF v2.1.0 needed to validate
// [Nix.VulnscanSarif] output.
type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name  string      `json:"name"`
	Rules []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID string `json:"id"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Locations []sarifLocation `json:"locations"`
}

type sarifLocation struct {
	PhysicalLocation struct {
		ArtifactLocation struct {
			URI string `json:"uri"`
		} `json:"artifactLocation"`
	} `json:"physicalLocation"`
}

// TestVulnscanSarif verifies that [Nix.VulnscanSarif] produces valid
// SARIF v2.1.0 JSON from the grype scan.
//
// +check
func (m *Tests) TestVulnscanSarif(ctx context.Context) error {
	contents, err := dag.Nix().VulnscanSarif().Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading sarif: %w", err)
	}

	var log sarifLog

	err = json.Unmarshal([]byte(contents), &log)
	if err != nil {
		return fmt.Errorf("parsing sarif JSON: %w", err)
	}

	if log.Schema != "https://docs.oasis-open.org/sarif/sarif/v2.1.0/errata01/os/schemas/sarif-schema-2.1.0.json" {
		return fmt.Errorf("schema = %q, want SARIF v2.1.0 schema", log.Schema)
	}

	if log.Version != "2.1.0" {
		return fmt.Errorf("version = %q, want %q", log.Version, "2.1.0")
	}

	if len(log.Runs) != 1 {
		return fmt.Errorf("runs count = %d, want 1", len(log.Runs))
	}

	run := log.Runs[0]
	if run.Tool.Driver.Name != "grype" {
		return fmt.Errorf("driver name = %q, want %q", run.Tool.Driver.Name, "grype")
	}

	// Do not assert specific CVE counts (they change), but validate structure.
	for i, r := range run.Results {
		if r.RuleID == "" {
			return fmt.Errorf("result[%d] has empty ruleId", i)
		}

		switch r.Level {
		case "error", "warning", "note":
		default:
			return fmt.Errorf("result[%d] level = %q, want error/warning/note", i, r.Level)
		}

		// Verify empty artifact URIs were patched (GitHub rejects empty URIs).
		for _, loc := range r.Locations {
			if loc.PhysicalLocation.ArtifactLocation.URI == "" {
				return fmt.Errorf("result[%d] has empty location URI", i)
			}
		}
	}

	return nil
}
