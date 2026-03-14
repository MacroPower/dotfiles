package sandbox

import (
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"
)

// CertsDir is the directory where MITM leaf certificates are stored.
const CertsDir = "/etc/sandbox/certs"

// CADir is the directory where the sandbox CA cert and key are stored.
const CADir = "/etc/sandbox/ca"

// Generate reads the sandbox YAML config at configPath, resolves domains
// and ports, generates MITM certs for path-restricted rules, and writes
// iptables and Envoy config files to /etc. The parsed [*SandboxConfig] is
// returned so callers can reuse it without re-parsing.
func Generate(ctx context.Context, configPath string) (*SandboxConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg, err := ParseConfig(data)
	if err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
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
			return nil, fmt.Errorf("generating certs: %w", err)
		}

		certsDir = CertsDir
	}

	caBundlePath := findCABundle()
	envoyConf, err := GenerateEnvoyConfig(cfg, certsDir, caBundlePath)
	if err != nil {
		return nil, fmt.Errorf("generating envoy config: %w", err)
	}

	ipv4Rules, ipv6Rules := GenerateIptablesRules(cfg)

	err = validateIptablesRules(ctx, "iptables-restore", ipv4Rules)
	if err != nil {
		return nil, fmt.Errorf("validating IPv4 rules: %w", err)
	}

	err = validateIptablesRules(ctx, "ip6tables-restore", ipv6Rules)
	if err != nil {
		return nil, fmt.Errorf("validating IPv6 rules: %w", err)
	}

	files := map[string]string{
		"/etc/envoy-sandbox.yaml":      envoyConf,
		"/etc/iptables-sandbox.rules":  ipv4Rules,
		"/etc/ip6tables-sandbox.rules": ipv6Rules,
	}

	for _, path := range slices.Sorted(maps.Keys(files)) {
		err := os.WriteFile(path, []byte(files[path]), 0o644)
		if err != nil {
			return nil, fmt.Errorf("writing %s: %w", path, err)
		}
	}

	return cfg, nil
}

// GenerateEnvoyFromConfig resolves rules from a [SandboxConfig] and
// generates the Envoy bootstrap YAML. This is a convenience wrapper
// for callers outside the sandbox package that cannot construct
// unexported [ResolvedRule] values directly.
func GenerateEnvoyFromConfig(cfg *SandboxConfig, certsDir, caBundlePath string) (string, error) {
	return GenerateEnvoyConfig(cfg, certsDir, caBundlePath)
}

// validateIptablesRules runs iptables-restore --test to validate rule
// syntax without applying. If validation fails, the error includes the
// invalid rules for debugging.
func validateIptablesRules(ctx context.Context, restoreCmd, rules string) error {
	//nolint:gosec // G204: restoreCmd is a hardcoded binary name from Generate.
	cmd := exec.CommandContext(ctx, restoreCmd, "--test")
	cmd.Stdin = strings.NewReader(rules)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s --test: %w\noutput: %s\nrules:\n%s", restoreCmd, err, out, rules)
	}

	return nil
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
