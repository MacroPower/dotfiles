// Integration tests for the [Nix] module.
//
// Individual tests are annotated with +check so
// `dagger check -m toolchains/nix/tests` runs them all.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Tests provides integration tests for the [Nix] module.
type Tests struct{}

// ghSnapshot is the subset of the GitHub dependency snapshot format
// needed to validate [Nix.DependencySnapshot] output.
type ghSnapshot struct {
	Version   int                    `json:"version"`
	SHA       string                 `json:"sha"`
	Ref       string                 `json:"ref"`
	Job       ghJob                  `json:"job"`
	Detector  ghDetector             `json:"detector"`
	Manifests map[string]ghManifest  `json:"manifests"`
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
	Name     string                  `json:"name"`
	File     *ghManifestFile         `json:"file,omitempty"`
	Resolved map[string]ghDependency `json:"resolved"`
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
	if err := json.Unmarshal([]byte(contents), &snap); err != nil {
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
	if err := json.Unmarshal([]byte(contents), &bom); err != nil {
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
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

// TestVulnscanSarif verifies that [Nix.VulnscanSarif] produces valid
// SARIF v2.1.0 JSON from the vulnix scan.
//
// +check
func (m *Tests) TestVulnscanSarif(ctx context.Context) error {
	contents, err := dag.Nix().VulnscanSarif().Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading sarif: %w", err)
	}

	var log sarifLog
	if err := json.Unmarshal([]byte(contents), &log); err != nil {
		return fmt.Errorf("parsing sarif JSON: %w", err)
	}

	if log.Schema != "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/main/sarif-2.1/schema/sarif-schema-2.1.0.json" {
		return fmt.Errorf("schema = %q, want SARIF v2.1.0 schema", log.Schema)
	}
	if log.Version != "2.1.0" {
		return fmt.Errorf("version = %q, want %q", log.Version, "2.1.0")
	}
	if len(log.Runs) != 1 {
		return fmt.Errorf("runs count = %d, want 1", len(log.Runs))
	}

	run := log.Runs[0]
	if run.Tool.Driver.Name != "vulnix" {
		return fmt.Errorf("driver name = %q, want %q", run.Tool.Driver.Name, "vulnix")
	}

	// Don't assert specific CVE counts (they change), but validate structure.
	for i, r := range run.Results {
		if r.RuleID == "" {
			return fmt.Errorf("result[%d] has empty ruleId", i)
		}
		switch r.Level {
		case "error", "warning", "note":
		default:
			return fmt.Errorf("result[%d] level = %q, want error/warning/note", i, r.Level)
		}
		if len(r.Locations) == 0 {
			return fmt.Errorf("result[%d] has no locations", i)
		}
		uri := r.Locations[0].PhysicalLocation.ArtifactLocation.URI
		if uri == "" {
			return fmt.Errorf("result[%d] has empty location URI", i)
		}
		// Results point to either a .nix source file (direct deps) or flake.lock (transitive).
		if uri != "flake.lock" && !strings.HasSuffix(uri, ".nix") {
			return fmt.Errorf("result[%d] location = %q, want .nix file or flake.lock", i, uri)
		}
	}

	return nil
}
