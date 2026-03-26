// Package sarif converts vulnix text output into SARIF v2.1.0 JSON
// for upload to GitHub Code Scanning.
//
// [ParseVulnix] extracts structured vulnerability data from vulnix's
// human-readable output, and [BuildLog] assembles it into a complete
// SARIF [Log] ready for JSON serialization.
//
// This package is intentionally free of Dagger imports so it can be
// unit tested without the Dagger runtime.
package sarif

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// VulnixPackage represents a single package block parsed from vulnix output,
// containing the package identity and its associated [VulnixCVE] entries.
type VulnixPackage struct {
	Name    string
	Version string
	CVEs    []VulnixCVE
}

// VulnixCVE is a single CVE entry within a [VulnixPackage] block.
type VulnixCVE struct {
	ID   string  // e.g. "CVE-2024-2398"
	URL  string  // full NVD URL
	CVSS float64 // CVSSv3 score
}

var (
	separatorRe = regexp.MustCompile(`^-{10,}$`)
	// Version starts at the last hyphen-separated segment beginning with a digit.
	pkgVersionRe = regexp.MustCompile(`^(.+)-(\d[^\s]*)$`)
	cveLineRe    = regexp.MustCompile(`^(https://\S+/(CVE-\S+))\s+(\S+)`)
	drvPathRe    = regexp.MustCompile(`^/nix/store/`)
	cveHeaderRe  = regexp.MustCompile(`(?i)^CVE\s+`)
)

// ParseVulnix parses vulnix's human-readable text output into a slice of
// [VulnixPackage] values. It returns nil, nil when output is empty (no
// vulnerabilities found) and an error when the output is non-empty but
// contains no parseable package blocks.
func ParseVulnix(output string) ([]VulnixPackage, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return nil, nil
	}

	var packages []VulnixPackage
	lines := strings.Split(output, "\n")

	i := 0
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])

		if !separatorRe.MatchString(line) {
			i++
			continue
		}
		i++

		// Skip blank lines after separator.
		for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
			i++
		}
		if i >= len(lines) {
			break
		}

		// Package name-version line.
		pkgLine := strings.TrimSpace(lines[i])
		i++

		m := pkgVersionRe.FindStringSubmatch(pkgLine)
		if m == nil {
			continue
		}
		pkg := VulnixPackage{Name: m[1], Version: m[2]}

		// Consume remaining lines in this block until next separator or EOF.
		for i < len(lines) {
			line = strings.TrimSpace(lines[i])
			if separatorRe.MatchString(line) {
				break
			}
			i++

			// Skip .drv path, CVE/CVSS header, and blank lines.
			if line == "" || drvPathRe.MatchString(line) || cveHeaderRe.MatchString(line) {
				continue
			}

			cm := cveLineRe.FindStringSubmatch(line)
			if cm == nil {
				continue
			}
			score, err := strconv.ParseFloat(cm[3], 64)
			if err != nil {
				continue
			}
			pkg.CVEs = append(pkg.CVEs, VulnixCVE{
				ID:   cm[2],
				URL:  cm[1],
				CVSS: score,
			})
		}

		packages = append(packages, pkg)
	}

	if len(packages) == 0 {
		return nil, fmt.Errorf("vulnix output not empty but no packages parsed")
	}
	return packages, nil
}

// Schema is the SARIF v2.1.0 JSON schema URL, referenced by [Log.Schema].
const Schema = "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/main/sarif-2.1/schema/sarif-schema-2.1.0.json"

// Log is the top-level SARIF v2.1.0 envelope returned by [BuildLog].
type Log struct {
	Schema  string `json:"$schema"`
	Version string `json:"version"`
	Runs    []Run  `json:"runs"`
}

// Run is a single SARIF run containing tool info and results.
type Run struct {
	Tool    Tool     `json:"tool"`
	Results []Result `json:"results"`
}

// Tool identifies the analysis tool.
type Tool struct {
	Driver Driver `json:"driver"`
}

// Driver is the tool's primary component.
type Driver struct {
	Name           string `json:"name"`
	InformationURI string `json:"informationUri"`
	Rules          []Rule `json:"rules"`
}

// Rule describes a single analysis rule (one per CVE).
type Rule struct {
	ID               string         `json:"id"`
	ShortDescription Message        `json:"shortDescription"`
	HelpURI          string         `json:"helpUri"`
	Properties       RuleProperties `json:"properties,omitempty"`
}

// RuleProperties holds SARIF rule metadata.
type RuleProperties struct {
	SecuritySeverity string   `json:"security-severity,omitempty"`
	Tags             []string `json:"tags,omitempty"`
}

// Result is a single SARIF finding.
type Result struct {
	RuleID              string            `json:"ruleId"`
	RuleIndex           int               `json:"ruleIndex"`
	Level               string            `json:"level"`
	Message             Message           `json:"message"`
	Locations           []Location        `json:"locations"`
	PartialFingerprints map[string]string `json:"partialFingerprints"`
}

// Message is a SARIF text message.
type Message struct {
	Text string `json:"text"`
}

// Location is a SARIF result location.
type Location struct {
	PhysicalLocation PhysicalLocation `json:"physicalLocation"`
}

// PhysicalLocation points to a file and region.
type PhysicalLocation struct {
	ArtifactLocation ArtifactLocation `json:"artifactLocation"`
	Region           Region           `json:"region"`
}

// ArtifactLocation identifies a file.
type ArtifactLocation struct {
	URI       string `json:"uri"`
	URIBaseID string `json:"uriBaseId"`
}

// Region identifies a location within a file.
type Region struct {
	StartLine int `json:"startLine"`
}

// MatchesPkgRef reports whether a Nix source line contains a package
// reference for name. It matches three patterns:
//   - pkgs.NAME (attribute access)
//   - pkgs."NAME" (quoted attribute access)
//   - NAME as the sole token on a line (bare name inside a with pkgs block)
func MatchesPkgRef(line, name string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == name {
		return true
	}
	// pkgs."NAME" (quoted attribute access).
	if strings.Contains(line, `pkgs."`+name+`"`) {
		return true
	}
	// pkgs.NAME followed by a non-identifier character (or end of string).
	prefix := "pkgs." + name
	idx := strings.Index(line, prefix)
	if idx >= 0 {
		end := idx + len(prefix)
		if end >= len(line) {
			return true
		}
		// Nix identifiers contain [a-zA-Z0-9_'-]. If the next char is
		// not one of those, this is a complete match.
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

// CvssToLevel maps a CVSSv3 score to a SARIF level string:
// "error" for critical (>= 9.0), "warning" for high (>= 7.0),
// and "note" for everything below.
func CvssToLevel(score float64) string {
	switch {
	case score >= 9.0:
		return "error"
	case score >= 7.0:
		return "warning"
	default:
		return "note"
	}
}

// SourceLocation maps a package name to the file and line where it is
// declared. Used by [BuildLog] to produce more specific result locations.
type SourceLocation struct {
	URI  string // repo-relative path, e.g. "home/tools.nix"
	Line int
}

// BuildLog converts parsed [VulnixPackage] values into a SARIF v2.1.0 [Log].
// Each unique CVE becomes a [Rule], and each package/CVE combination becomes
// a [Result]. When locations contains an entry for the package name, the
// result points to that source file; otherwise it falls back to
// fallbackURI/fallbackLine (typically the nixpkgs rev in flake.lock).
func BuildLog(packages []VulnixPackage, locations map[string]SourceLocation, fallbackURI string, fallbackLine int) Log {
	ruleIndex := make(map[string]int)
	var rules []Rule
	var results []Result

	for _, pkg := range packages {
		for _, cve := range pkg.CVEs {
			if _, ok := ruleIndex[cve.ID]; !ok {
				ruleIndex[cve.ID] = len(rules)
				rules = append(rules, Rule{
					ID:               cve.ID,
					ShortDescription: Message{Text: fmt.Sprintf("%s in %s-%s", cve.ID, pkg.Name, pkg.Version)},
					HelpURI:          cve.URL,
					Properties: RuleProperties{
						SecuritySeverity: strconv.FormatFloat(cve.CVSS, 'f', 1, 64),
						Tags:             []string{"security", "vulnerability"},
					},
				})
			}

			h := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%s", cve.ID, pkg.Name, pkg.Version)))

			loc := ArtifactLocation{URI: fallbackURI, URIBaseID: "%SRCROOT%"}
			line := fallbackLine
			if sl, ok := locations[pkg.Name]; ok {
				loc.URI = sl.URI
				line = sl.Line
			}

			results = append(results, Result{
				RuleID:    cve.ID,
				RuleIndex: ruleIndex[cve.ID],
				Level:     CvssToLevel(cve.CVSS),
				Message:   Message{Text: fmt.Sprintf("%s affects %s-%s (CVSS %.1f)", cve.ID, pkg.Name, pkg.Version, cve.CVSS)},
				Locations: []Location{{
					PhysicalLocation: PhysicalLocation{
						ArtifactLocation: loc,
						Region:           Region{StartLine: line},
					},
				}},
				PartialFingerprints: map[string]string{
					"vulnixCveHash/v1": hex.EncodeToString(h[:]),
				},
			})
		}
	}

	return Log{
		Schema:  Schema,
		Version: "2.1.0",
		Runs: []Run{{
			Tool: Tool{
				Driver: Driver{
					Name:           "vulnix",
					InformationURI: "https://github.com/nix-community/vulnix",
					Rules:          rules,
				},
			},
			Results: results,
		}},
	}
}
