package sarif_test

import (
	"testing"

	"dagger/nix/internal/sarif"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseVulnix(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  []sarif.VulnixPackage
		err   bool
	}{
		"single package single CVE": {
			input: `1 derivations with active advisories

------------------------------------------------------------------------
curl-8.5.0

/nix/store/abc123-curl-8.5.0.drv
CVE                                                CVSSv3
https://nvd.nist.gov/vuln/detail/CVE-2024-2398     7.5
`,
			want: []sarif.VulnixPackage{{
				Name: "curl", Version: "8.5.0",
				CVEs: []sarif.VulnixCVE{{
					ID: "CVE-2024-2398", URL: "https://nvd.nist.gov/vuln/detail/CVE-2024-2398", CVSS: 7.5,
				}},
			}},
		},
		"single package multiple CVEs": {
			input: `1 derivations with active advisories

------------------------------------------------------------------------
avahi-0.8

/nix/store/73spj0q5l2bdfgyyxv5nbrmbl2lig6vx-avahi-0.8.drv
CVE                                                CVSSv3
https://nvd.nist.gov/vuln/detail/CVE-2021-26720    7.8
https://nvd.nist.gov/vuln/detail/CVE-2025-68468    6.5
`,
			want: []sarif.VulnixPackage{{
				Name: "avahi", Version: "0.8",
				CVEs: []sarif.VulnixCVE{
					{ID: "CVE-2021-26720", URL: "https://nvd.nist.gov/vuln/detail/CVE-2021-26720", CVSS: 7.8},
					{ID: "CVE-2025-68468", URL: "https://nvd.nist.gov/vuln/detail/CVE-2025-68468", CVSS: 6.5},
				},
			}},
		},
		"multiple packages": {
			input: `2 derivations with active advisories

------------------------------------------------------------------------
curl-8.5.0

/nix/store/abc123-curl-8.5.0.drv
CVE                                                CVSSv3
https://nvd.nist.gov/vuln/detail/CVE-2024-2398     7.5

------------------------------------------------------------------------
avahi-0.8

/nix/store/def456-avahi-0.8.drv
CVE                                                CVSSv3
https://nvd.nist.gov/vuln/detail/CVE-2021-26720    7.8
`,
			want: []sarif.VulnixPackage{
				{
					Name: "curl", Version: "8.5.0",
					CVEs: []sarif.VulnixCVE{{ID: "CVE-2024-2398", URL: "https://nvd.nist.gov/vuln/detail/CVE-2024-2398", CVSS: 7.5}},
				},
				{
					Name: "avahi", Version: "0.8",
					CVEs: []sarif.VulnixCVE{{ID: "CVE-2021-26720", URL: "https://nvd.nist.gov/vuln/detail/CVE-2021-26720", CVSS: 7.8}},
				},
			},
		},
		"hyphenated package name": {
			input: `1 derivations with active advisories

------------------------------------------------------------------------
python3.11-requests-2.31.0

/nix/store/xyz-python3.11-requests-2.31.0.drv
CVE                                                CVSSv3
https://nvd.nist.gov/vuln/detail/CVE-2024-1234     5.3
`,
			want: []sarif.VulnixPackage{{
				Name: "python3.11-requests", Version: "2.31.0",
				CVEs: []sarif.VulnixCVE{{ID: "CVE-2024-1234", URL: "https://nvd.nist.gov/vuln/detail/CVE-2024-1234", CVSS: 5.3}},
			}},
		},
		"empty output": {
			input: "",
			want:  nil,
		},
		"non-empty but unparseable": {
			input: "some random text\nwith no vulnix format",
			err:   true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := sarif.ParseVulnix(tc.input)
			if tc.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestMatchesPkgRef(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		line string
		name string
		want bool
	}{
		"bare name on own line": {
			line: "    curl", name: "curl", want: true,
		},
		"bare name with trailing space": {
			line: "    curl  ", name: "curl", want: true,
		},
		"pkgs.NAME": {
			line: "    home.packages = [ pkgs.curl ];", name: "curl", want: true,
		},
		"pkgs quoted NAME": {
			line: `    pkgs."photo-cli"`, name: "photo-cli", want: true,
		},
		"substring in attribute name": {
			line: "            network = {", name: "network", want: false,
		},
		"substring in other identifier": {
			line: "            enabled = pkgs.stdenv.isDarwin;", name: "network", want: false,
		},
		"name embedded in longer word": {
			line: "    curl-impersonate", name: "curl", want: false,
		},
		"name as part of assignment": {
			line: "    curl = something;", name: "curl", want: false,
		},
		"pkgs.NAME but different package": {
			line: "    pkgs.curlFull", name: "curl", want: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, sarif.MatchesPkgRef(tc.line, tc.name))
		})
	}
}

func TestCvssToLevel(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		score float64
		want  string
	}{
		"critical 9.8": {score: 9.8, want: "error"},
		"critical 9.0": {score: 9.0, want: "error"},
		"high 7.5":     {score: 7.5, want: "warning"},
		"high 7.0":     {score: 7.0, want: "warning"},
		"medium 6.9":   {score: 6.9, want: "note"},
		"low 0.0":      {score: 0.0, want: "note"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, sarif.CvssToLevel(tc.score))
		})
	}
}

func TestBuildLog(t *testing.T) {
	t.Parallel()

	packages := []sarif.VulnixPackage{
		{
			Name: "curl", Version: "8.5.0",
			CVEs: []sarif.VulnixCVE{
				{ID: "CVE-2024-2398", URL: "https://nvd.nist.gov/vuln/detail/CVE-2024-2398", CVSS: 7.5},
				{ID: "CVE-2024-2004", URL: "https://nvd.nist.gov/vuln/detail/CVE-2024-2004", CVSS: 5.3},
			},
		},
		{
			Name: "avahi", Version: "0.8",
			CVEs: []sarif.VulnixCVE{
				{ID: "CVE-2021-26720", URL: "https://nvd.nist.gov/vuln/detail/CVE-2021-26720", CVSS: 9.8},
			},
		},
	}

	locations := map[string]sarif.SourceLocation{
		"curl": {URI: "home/tools.nix", Line: 42},
	}
	got := sarif.BuildLog(packages, locations, "flake.lock", 618)

	assert.Equal(t, sarif.Schema, got.Schema)
	assert.Equal(t, "2.1.0", got.Version)
	require.Len(t, got.Runs, 1)

	run := got.Runs[0]
	assert.Equal(t, "vulnix", run.Tool.Driver.Name)
	assert.Equal(t, "https://github.com/nix-community/vulnix", run.Tool.Driver.InformationURI)

	// 3 unique CVEs -> 3 rules.
	assert.Len(t, run.Tool.Driver.Rules, 3)

	// 3 total CVE-package pairs -> 3 results.
	require.Len(t, run.Results, 3)

	for _, r := range run.Results {
		assert.NotEmpty(t, r.RuleID)
		assert.Contains(t, []string{"error", "warning", "note"}, r.Level)
		require.Len(t, r.Locations, 1)
		assert.Equal(t, "%SRCROOT%", r.Locations[0].PhysicalLocation.ArtifactLocation.URIBaseID)
		assert.NotEmpty(t, r.PartialFingerprints["vulnixCveHash/v1"])
	}

	// curl has a source location -> points to home/tools.nix:42.
	for _, r := range run.Results {
		if r.RuleID == "CVE-2024-2398" || r.RuleID == "CVE-2024-2004" {
			assert.Equal(t, "home/tools.nix", r.Locations[0].PhysicalLocation.ArtifactLocation.URI)
			assert.Equal(t, 42, r.Locations[0].PhysicalLocation.Region.StartLine)
		}
	}

	// avahi has no source location -> falls back to flake.lock:618.
	for _, r := range run.Results {
		if r.RuleID == "CVE-2021-26720" {
			assert.Equal(t, "error", r.Level)
			assert.Equal(t, "flake.lock", r.Locations[0].PhysicalLocation.ArtifactLocation.URI)
			assert.Equal(t, 618, r.Locations[0].PhysicalLocation.Region.StartLine)
		}
	}

	// Fingerprints are deterministic.
	got2 := sarif.BuildLog(packages, locations, "flake.lock", 618)
	for i := range got.Runs[0].Results {
		assert.Equal(t,
			got.Runs[0].Results[i].PartialFingerprints,
			got2.Runs[0].Results[i].PartialFingerprints,
		)
	}
}

func TestBuildLogEmpty(t *testing.T) {
	t.Parallel()

	got := sarif.BuildLog(nil, nil, "flake.lock", 1)

	assert.Equal(t, "2.1.0", got.Version)
	require.Len(t, got.Runs, 1)
	assert.Empty(t, got.Runs[0].Results)
	assert.Empty(t, got.Runs[0].Tool.Driver.Rules)
}
