package main

import "time"

// CycloneDX-to-GitHub dependency snapshot conversion.
//
// The types and conversion function in this file are intentionally
// free of Dagger imports so they can be unit tested without the
// Dagger runtime. See snapshot_test.go.

// cdxBOM is the subset of CycloneDX BOM fields we need.
type cdxBOM struct {
	Components []cdxComponent `json:"components"`
}

// cdxComponent is a single CycloneDX component.
type cdxComponent struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Purl    string `json:"purl"`
}

// ghSnapshot is the GitHub dependency submission API payload.
// https://docs.github.com/en/rest/dependency-graph/dependency-submission
type ghSnapshot struct {
	Version   int                    `json:"version"`
	Scanned   string                 `json:"scanned"`
	SHA       string                 `json:"sha"`
	Ref       string                 `json:"ref"`
	Job       ghJob                  `json:"job"`
	Detector  ghDetector             `json:"detector"`
	Manifests map[string]ghManifest  `json:"manifests"`
}

// ghJob identifies the CI run that produced the snapshot.
type ghJob struct {
	ID         string `json:"id"`
	Correlator string `json:"correlator"`
	HTMLURL    string `json:"html_url"`
}

// ghDetector identifies the tool that produced the snapshot.
type ghDetector struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	URL     string `json:"url"`
}

// ghManifest groups resolved dependencies under a logical name.
type ghManifest struct {
	Name     string                  `json:"name"`
	File     *ghManifestFile         `json:"file,omitempty"`
	Resolved map[string]ghDependency `json:"resolved"`
}

// ghManifestFile points to the source file that declared the dependencies.
type ghManifestFile struct {
	SourceLocation string `json:"source_location"`
}

// ghDependency is a single resolved dependency in the snapshot.
type ghDependency struct {
	PackageURL   string `json:"package_url"`
	Relationship string `json:"relationship"`
}

// buildDependencySnapshot converts a CycloneDX BOM into a GitHub
// dependency submission snapshot. Components without a purl are skipped.
func buildDependencySnapshot(cdx cdxBOM, sha, ref, correlator, runURL string) ghSnapshot {
	resolved := make(map[string]ghDependency, len(cdx.Components))
	for _, c := range cdx.Components {
		if c.Purl == "" {
			continue
		}
		resolved[c.Purl] = ghDependency{
			PackageURL:   c.Purl,
			Relationship: "direct",
		}
	}

	return ghSnapshot{
		Version: 0,
		Scanned: time.Now().UTC().Format(time.RFC3339),
		SHA:     sha,
		Ref:     ref,
		Job: ghJob{
			ID:         correlator,
			Correlator: correlator,
			HTMLURL:    runURL,
		},
		Detector: ghDetector{
			Name:    "sbomnix",
			Version: "1.0.0",
			URL:     "https://github.com/tiiuae/sbomnix",
		},
		Manifests: map[string]ghManifest{
			"nix-closure": {
				Name: "nix-closure",
				File: &ghManifestFile{SourceLocation: "flake.nix"},
				Resolved: resolved,
			},
		},
	}
}
