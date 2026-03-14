package sandbox

import (
	"fmt"
	"os"
)

// CertsDir is the directory where MITM leaf certificates are stored.
const CertsDir = "/etc/sandbox/certs"

// CADir is the directory where the sandbox CA cert and key are stored.
const CADir = "/etc/sandbox/ca"

// Generate reads the sandbox YAML config at configPath, resolves domains
// and ports, generates MITM certs for path-restricted rules, and writes
// iptables and Envoy config files to /etc.
func Generate(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	cfg, err := ParseConfig(data)
	if err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	// Collect domains that need MITM certs (restricted on any TLS port).
	tlsPorts := []int{443}
	tlsPorts = append(tlsPorts, cfg.ExtraPorts()...)
	mitmSeen := make(map[string]bool)

	var mitmRules []ResolvedRule
	for _, port := range tlsPorts {
		portRules := cfg.ResolveRulesForPort(port)

		for _, r := range portRules {
			if r.IsRestricted() && !mitmSeen[r.Domain] {
				mitmSeen[r.Domain] = true
				mitmRules = append(mitmRules, r)
			}
		}
	}

	certsDir := ""
	if len(mitmRules) > 0 {
		err := GenerateCerts(mitmRules, CADir, CertsDir)
		if err != nil {
			return fmt.Errorf("generating certs: %w", err)
		}

		certsDir = CertsDir
	}

	caBundlePath := findCABundle()
	envoyConf, err := GenerateEnvoyConfig(cfg, certsDir, caBundlePath)
	if err != nil {
		return fmt.Errorf("generating envoy config: %w", err)
	}

	ipv4Rules, ipv6Rules := GenerateIptablesRules(cfg)

	files := map[string]string{
		"/etc/envoy-sandbox.yaml":      envoyConf,
		"/etc/iptables-sandbox.rules":  ipv4Rules,
		"/etc/ip6tables-sandbox.rules": ipv6Rules,
	}

	for path, content := range files {
		err := os.WriteFile(path, []byte(content), 0o644)
		if err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}

	return nil
}

// GenerateEnvoyFromConfig resolves rules from a [SandboxConfig] and
// generates the Envoy bootstrap YAML. This is a convenience wrapper
// for callers outside the sandbox package that cannot construct
// unexported [ResolvedRule] values directly.
func GenerateEnvoyFromConfig(cfg *SandboxConfig, certsDir, caBundlePath string) (string, error) {
	return GenerateEnvoyConfig(cfg, certsDir, caBundlePath)
}

// findCABundle returns the path to the system CA certificate bundle.
// Checks SSL_CERT_FILE and NIX_SSL_CERT_FILE env vars first, then
// well-known filesystem paths.
func findCABundle() string {
	candidates := []string{
		os.Getenv("SSL_CERT_FILE"),
		os.Getenv("NIX_SSL_CERT_FILE"),
		"/etc/ssl/certs/ca-certificates.crt",
		"/etc/ssl/certs/ca-bundle.crt",
		"/etc/pki/tls/certs/ca-bundle.crt",
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}

		_, err := os.Stat(c) //nolint:gosec // G703: paths are hardcoded candidates.
		if err == nil {
			return c
		}
	}

	return ""
}
