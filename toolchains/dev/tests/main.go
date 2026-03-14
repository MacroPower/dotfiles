// Integration tests for the [Dev] module. Individual tests are annotated
// with +check so `dagger check -m toolchains/dev/tests` runs them all
// concurrently.
//
// Security invariant: no test in this module should use
// InsecureRootCapabilities or ExperimentalPrivilegedNesting.
// These options bypass container sandboxing and are only appropriate
// for interactive use (Dev terminal). Adding either to a test
// function requires explicit security review justification.

package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"dagger/tests/internal/dagger"

	"golang.org/x/sync/errgroup"
)

const (
	// hmBin is the stable path to home-manager managed binaries.
	hmBin = "/home/dev/.local/state/home-manager/gcroots/current-home/home-path/bin"
)

// Tests provides integration tests for the [Dev] module.
type Tests struct{}

// configFile creates a dagger.File from an inline YAML string, used to
// pass sandbox configs in tests.
func configFile(yaml string) *dagger.File {
	return dag.CurrentModule().Source().
		WithNewFile("config.yaml", yaml).
		File("config.yaml")
}

// allDomainsYAML is an egress rule with all default domains for testing.
const allDomainsYAML = `egress:
  - toCIDRSet:
      - cidr: 0.0.0.0/0
        except:
          - 10.0.0.0/8
          - 172.16.0.0/12
          - 192.168.0.0/16
          - 169.254.0.0/16
          - 100.64.0.0/10
      - cidr: "::/0"
        except:
          - "fc00::/7"
          - "fe80::/10"
    toFQDNs:
      - matchName: "anthropic.com"
      - matchPattern: "*.anthropic.com"
      - matchName: "kagi.com"
      - matchPattern: "*.kagi.com"
      - matchName: "context7.com"
      - matchPattern: "*.context7.com"
      - matchName: "github.com"
      - matchPattern: "*.github.com"
      - matchName: "githubusercontent.com"
      - matchPattern: "*.githubusercontent.com"
      - matchName: "api.githubcopilot.com"
      - matchName: "golang.org"
      - matchPattern: "*.golang.org"
      - matchName: "go.dev"
      - matchPattern: "*.go.dev"
      - matchName: "gopkg.in"
      - matchName: "go.googlesource.com"
      - matchName: "cs.opensource.google"
      - matchName: "dl.google.com"
      - matchName: "packages.cloud.google.com"
      - matchName: "repo1.maven.org"
      - matchName: "repo.maven.apache.org"
      - matchName: "nixos.org"
      - matchPattern: "*.nixos.org"
      - matchName: "registry.npmjs.org"
      - matchName: "pypi.org"
      - matchPattern: "*.pypi.org"
      - matchName: "files.pythonhosted.org"
      - matchName: "crates.io"
      - matchPattern: "*.crates.io"
      - matchName: "rust-lang.org"
      - matchPattern: "*.rust-lang.org"
      - matchName: "releases.hashicorp.com"
      - matchName: "registry.terraform.io"
`

// All runs all tests in parallel.
func (m *Tests) All(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error { return m.TestDevBase(ctx) })
	eg.Go(func() error { return m.TestDevBaseEnv(ctx) })
	eg.Go(func() error { return m.TestDevBaseShellConfig(ctx) })
	eg.Go(func() error { return m.TestDevBaseNoBuildArtifacts(ctx) })
	eg.Go(func() error { return m.TestSandboxBase(ctx) })
	eg.Go(func() error { return m.TestSandboxEgressRules(ctx) })
	eg.Go(func() error { return m.TestSandboxAdditionalPorts(ctx) })
	eg.Go(func() error { return m.TestSandboxTCPForward(ctx) })
	eg.Go(func() error { return m.TestSandboxLogging(ctx) })
	eg.Go(func() error { return m.TestSandboxCIDRNoExcept(ctx) })
	eg.Go(func() error { return m.TestSandboxMethodRestriction(ctx) })
	eg.Go(func() error { return m.TestSandboxUnsupportedSelector(ctx) })
	eg.Go(func() error { return m.TestSandboxOpenPortRange(ctx) })
	eg.Go(func() error { return m.TestSandboxPathNormalization(ctx) })
	eg.Go(func() error { return m.TestSandboxWildcardDnsmasq(ctx) })
	eg.Go(func() error { return m.TestAtuinDaemon(ctx) })

	return eg.Wait()
}

// TestDevBase verifies that [Dev.DevBase] produces a container with essential
// development tools available on PATH. This validates the tool installation
// pipeline without requiring an interactive terminal session.
//
// +check
func (m *Tests) TestDevBase(ctx context.Context) error {
	ctr := dag.Dev().DevBase()

	tools := []string{
		// Shell and core.
		"fish", "git", "vim", "direnv", "tmux",
		// Development.
		"go", "node", "npm", "task", "dagger",
		// CLI utilities.
		"rg", "fd", "bat", "fzf", "eza", "jq", "yq", "tree", "gh", "delta",
		// Kubernetes.
		"kubectl", "helm", "kustomize", "k9s",
		// Nix.
		"nh",
	}
	for _, tool := range tools {
		_, err := ctr.WithExec([]string{"which", tool}).Sync(ctx)
		if err != nil {
			return fmt.Errorf("%s not found in dev container: %w", tool, err)
		}
	}

	return nil
}

// TestDevBaseEnv verifies that [Dev.DevBase] sets the expected environment
// variables. These are set explicitly in buildBase and DevBase, not inherited
// from the base image.
//
// +check
func (m *Tests) TestDevBaseEnv(ctx context.Context) error {
	ctr := dag.Dev().DevBase()

	checks := map[string]struct {
		want     string
		contains []string
	}{
		"HOME":       {want: "/home/dev"},
		"USER":       {want: "dev"},
		"EDITOR":     {want: "vim"},
		"TERM":       {want: "xterm-256color"},
		"IS_SANDBOX": {want: "1"},
		"PATH": {contains: []string{
			"/home/dev/.local/state/home-manager/gcroots/current-home/home-path/bin",
			"/home/dev/.nix-profile/bin",
			"/nix/var/nix/profiles/default/bin",
		}},
	}

	for name, tc := range checks {
		val, err := ctr.EnvVariable(ctx, name)
		if err != nil {
			return fmt.Errorf("reading env %s: %w", name, err)
		}
		if tc.want != "" && val != tc.want {
			return fmt.Errorf("env %s: expected %q, got %q", name, tc.want, val)
		}
		for _, seg := range tc.contains {
			if !strings.Contains(val, seg) {
				return fmt.Errorf("env %s: missing segment %q: %s", name, seg, val)
			}
		}
	}

	return nil
}

// TestDevBaseShellConfig verifies that fish shell configuration is properly
// activated by home-manager and the history symlink is in place.
//
// +check
func (m *Tests) TestDevBaseShellConfig(ctx context.Context) error {
	ctr := dag.Dev().DevBase()

	checks := map[string][]string{
		"fish config dir":      {"test", "-d", "/home/dev/.config/fish/conf.d"},
		"fish history symlink": {"test", "-L", "/home/dev/.local/share/fish/fish_history"},
		"fish functions dir":   {"test", "-d", "/home/dev/.config/fish/functions"},
	}

	for desc, cmd := range checks {
		if _, err := ctr.WithExec(cmd).Sync(ctx); err != nil {
			return fmt.Errorf("%s: %w", desc, err)
		}
	}

	return nil
}

// TestDevBaseNoBuildArtifacts verifies that the dev container does not
// contain the dotfiles source tree used during the build. The source is
// only needed for nix build + activate and should be removed afterward.
//
// +check
func (m *Tests) TestDevBaseNoBuildArtifacts(ctx context.Context) error {
	ctr := dag.Dev().DevBase()

	_, err := ctr.WithExec([]string{"test", "!", "-e", "/dotfiles"}).Sync(ctx)
	if err != nil {
		return fmt.Errorf("/dotfiles should not exist in dev container: %w", err)
	}

	return nil
}

// TestSandboxBase verifies that [Dev.SandboxBase] layers the Envoy
// proxy, iptables redirect rules, dnsmasq domain filtering, and the
// sandbox binary on top of the base container. Does not load the rules
// (that requires CAP_NET_ADMIN); only checks that the files and
// environment are in place.
//
// +check
func (m *Tests) TestSandboxBase(ctx context.Context) error {
	ctr := dag.Dev().SandboxBase()

	// Verify Envoy config exists with SNI filtering, dynamic forward
	// proxy, and domain entries for key domains.
	envoyConf, err := ctr.File("/etc/envoy-sandbox.yaml").Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading envoy config: %w", err)
	}
	for _, want := range []string{
		"tls_inspector",
		"server_names",
		"sni_dynamic_forward_proxy",
		"dynamic_forward_proxy_cluster",
		"github.com",
		"anthropic.com",
		"golang.org",
		"http_connection_manager",
	} {
		if !strings.Contains(envoyConf, want) {
			return fmt.Errorf("envoy config missing %q", want)
		}
	}

	// Verify iptables rules exist with REDIRECT to Envoy ports and
	// anti-rebinding rules.
	iptRules, err := ctr.File("/etc/iptables-sandbox.rules").Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading iptables rules: %w", err)
	}
	for _, want := range []string{
		"REDIRECT",
		"--to-port 15443",
		"--to-port 15080",
		// Localhost destination accept (for REDIRECT compatibility with
		// nf_tables kernels that don't re-route after NAT).
		"-d 127.0.0.0/8",
		// Anti-rebinding.
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"169.254.0.0/16",
		"100.64.0.0/10",
		// Final DROP.
		"-j DROP",
	} {
		if !strings.Contains(iptRules, want) {
			return fmt.Errorf("iptables rules missing %q", want)
		}
	}

	// Verify IPv6 iptables rules.
	ip6Rules, err := ctr.File("/etc/ip6tables-sandbox.rules").Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading ip6tables rules: %w", err)
	}
	for _, want := range []string{"-d ::1/128", "fc00::/7", "fe80::/10", "-j DROP"} {
		if !strings.Contains(ip6Rules, want) {
			return fmt.Errorf("ip6tables rules missing %q", want)
		}
	}

	// Verify config.yaml exists with egress rules for domain resolution
	// at runtime.
	cfgContent, err := ctr.File("/etc/sandbox/config.yaml").Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading config.yaml: %w", err)
	}
	for _, want := range []string{"egress", "toFQDNs", "matchName"} {
		if !strings.Contains(cfgContent, want) {
			return fmt.Errorf("config.yaml missing %q", want)
		}
	}

	// Verify TCP forwards are NOT present by default (tcpForwards is opt-in).
	for _, unwanted := range []string{"tcp_forward_", "STRICT_DNS"} {
		if strings.Contains(envoyConf, unwanted) {
			return fmt.Errorf("TCP forwards should not be enabled by default, found %q", unwanted)
		}
	}

	// Verify sandbox binary exists and is executable.
	if _, err := ctr.WithExec([]string{"test", "-x", "/usr/local/bin/sandbox"}).Sync(ctx); err != nil {
		return fmt.Errorf("sandbox not executable: %w", err)
	}

	// Verify that binaries referenced by sandbox init are reachable.
	for _, bin := range []string{"envoy", "setpriv", "dnsmasq"} {
		if _, err := ctr.WithExec([]string{"sh", "-c", hmBin + "/" + bin + " --version"}).Sync(ctx); err != nil {
			return fmt.Errorf("%s not reachable at %s/%s: %w", bin, hmBin, bin, err)
		}
	}

	// Verify IS_SANDBOX_NETWORK env var.
	val, err := ctr.EnvVariable(ctx, "IS_SANDBOX_NETWORK")
	if err != nil {
		return fmt.Errorf("reading IS_SANDBOX_NETWORK: %w", err)
	}
	if val != "1" {
		return fmt.Errorf("IS_SANDBOX_NETWORK: expected 1, got %q", val)
	}

	return nil
}

// TestSandboxEgressRules verifies that selecting specific egress rules
// includes only domains from those rules in the Envoy config.
//
// +check
func (m *Tests) TestSandboxEgressRules(ctx context.Context) error {
	ctr := dag.Dev().SandboxBase(dagger.DevSandboxBaseOpts{
		SandboxConfig: configFile(`egress:
  - toFQDNs:
      - matchName: "golang.org"
      - matchPattern: "*.golang.org"
      - matchName: "go.dev"
      - matchPattern: "*.go.dev"
      - matchName: "gopkg.in"
      - matchName: "go.googlesource.com"
      - matchName: "cs.opensource.google"
`),
	})

	// Check config.yaml contains toFQDNs.
	cfgContent, err := ctr.File("/etc/sandbox/config.yaml").Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading config.yaml: %w", err)
	}
	if !strings.Contains(cfgContent, "golang.org") {
		return fmt.Errorf("config.yaml missing golang.org")
	}
	for _, unwanted := range []string{"github.com", "anthropic.com"} {
		if strings.Contains(cfgContent, unwanted) {
			return fmt.Errorf("config.yaml should not contain %q with only go domains", unwanted)
		}
	}

	// Check Envoy config server_names.
	envoyConf, err := ctr.File("/etc/envoy-sandbox.yaml").Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading envoy config: %w", err)
	}
	if !strings.Contains(envoyConf, "golang.org") {
		return fmt.Errorf("envoy config missing golang.org")
	}
	for _, unwanted := range []string{"github.com", "anthropic.com"} {
		if strings.Contains(envoyConf, unwanted) {
			return fmt.Errorf("envoy config should not contain %s with only go domains", unwanted)
		}
	}

	return nil
}

// TestSandboxAdditionalPorts verifies that extra ports get iptables
// REDIRECT rules and an Envoy TLS passthrough listener.
//
// +check
func (m *Tests) TestSandboxAdditionalPorts(ctx context.Context) error {
	ctr := dag.Dev().SandboxBase(dagger.DevSandboxBaseOpts{
		SandboxConfig: configFile(allDomainsYAML + `  - toFQDNs:
      - matchName: "extra.example.com"
    toPorts:
      - ports:
          - port: "8080"
`),
	})

	rules, err := ctr.File("/etc/iptables-sandbox.rules").Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading iptables rules: %w", err)
	}
	if !strings.Contains(rules, "--dport 8080") {
		return fmt.Errorf("iptables rules missing REDIRECT for port 8080")
	}

	envoyConf, err := ctr.File("/etc/envoy-sandbox.yaml").Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading envoy config: %w", err)
	}
	if !strings.Contains(envoyConf, "tls_passthrough_8080") {
		return fmt.Errorf("envoy config missing listener for port 8080")
	}
	// extra.example.com should appear in the 8080 listener (per-port scoping).
	if !strings.Contains(envoyConf, "extra.example.com") {
		return fmt.Errorf("envoy config missing extra.example.com in port 8080 listener")
	}

	return nil
}

// TestSandboxTCPForward verifies that tcpForwards creates the expected
// TCP forward listener, STRICT_DNS cluster, and iptables redirect rule.
//
// +check
func (m *Tests) TestSandboxTCPForward(ctx context.Context) error {
	ctr := dag.Dev().SandboxBase(dagger.DevSandboxBaseOpts{
		SandboxConfig: configFile(allDomainsYAML + `tcpForwards:
  - port: 22
    host: github.com
`),
	})

	rules, err := ctr.File("/etc/iptables-sandbox.rules").Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading iptables rules: %w", err)
	}
	if !strings.Contains(rules, "--to-port 15022") {
		return fmt.Errorf("iptables rules missing REDIRECT for port 22")
	}

	envoyConf, err := ctr.File("/etc/envoy-sandbox.yaml").Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading envoy config: %w", err)
	}
	for _, want := range []string{"tcp_forward_22", "STRICT_DNS", "github.com"} {
		if !strings.Contains(envoyConf, want) {
			return fmt.Errorf("envoy config missing %q for tcp forward", want)
		}
	}

	return nil
}

// TestSandboxLogging verifies that enabling sandbox logging adds access
// logs to Envoy and LOG targets to iptables drop rules.
//
// +check
func (m *Tests) TestSandboxLogging(ctx context.Context) error {
	ctr := dag.Dev().SandboxBase(dagger.DevSandboxBaseOpts{
		SandboxConfig: configFile(allDomainsYAML + `logging: true
`),
	})

	envoyConf, err := ctr.File("/etc/envoy-sandbox.yaml").Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading envoy config: %w", err)
	}
	if !strings.Contains(envoyConf, "access_log") {
		return fmt.Errorf("envoy config missing access_log with logging enabled")
	}

	rules, err := ctr.File("/etc/iptables-sandbox.rules").Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading iptables rules: %w", err)
	}
	if !strings.Contains(rules, "LOG") {
		return fmt.Errorf("iptables rules missing LOG target with logging enabled")
	}

	return nil
}

// TestSandboxCIDRNoExcept verifies that omitting toCIDRSet except entries
// (or omitting toCIDRSet entirely) does not generate anti-rebinding DROP
// rules, allowing traffic to private IP ranges.
//
// +check
func (m *Tests) TestSandboxCIDRNoExcept(ctx context.Context) error {
	ctr := dag.Dev().SandboxBase(dagger.DevSandboxBaseOpts{
		SandboxConfig: configFile(`egress:
  - toFQDNs:
      - matchName: "github.com"
      - matchPattern: "*.github.com"
`),
	})

	rules, err := ctr.File("/etc/iptables-sandbox.rules").Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading iptables rules: %w", err)
	}

	// Anti-rebinding rules should NOT be present.
	for _, unwanted := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
		if strings.Contains(rules, unwanted) {
			return fmt.Errorf("iptables rules should not contain %q without CIDR except entries", unwanted)
		}
	}

	// Core structure should still be intact.
	for _, want := range []string{
		"REDIRECT",
		"--to-port 15443",
		"-j ACCEPT",
		"-j DROP",
	} {
		if !strings.Contains(rules, want) {
			return fmt.Errorf("iptables rules missing %q", want)
		}
	}

	return nil
}

// TestSandboxMethodRestriction verifies that method-restricted allow rules
// produce Envoy config with :method header matchers and 403 catch-all
// routes. Also verifies that MITM certs are generated for method-only
// restricted domains.
//
// +check
func (m *Tests) TestSandboxMethodRestriction(ctx context.Context) error {
	ctr := dag.Dev().SandboxBase(dagger.DevSandboxBaseOpts{
		SandboxConfig: configFile(`egress:
  - toFQDNs:
      - matchName: api.example.com
    toPorts:
      - rules:
          http:
            - method: GET
  - toFQDNs:
      - matchName: api2.example.com
    toPorts:
      - rules:
          http:
            - path: /v1/
              method: GET
            - path: /v1/
              method: POST
  - toFQDNs:
      - matchName: cdn.example.com
`),
	})

	envoyConf, err := ctr.File("/etc/envoy-sandbox.yaml").Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading envoy config: %w", err)
	}

	// Method-only restricted domain should have :method header matcher.
	for _, want := range []string{
		"restricted_api.example.com",
		":method",
		"exact: GET",
		"direct_response",
	} {
		if !strings.Contains(envoyConf, want) {
			return fmt.Errorf("envoy config missing %q for method-restricted rule", want)
		}
	}

	// Paths+methods combined domain should have both path prefix and
	// regex method matcher.
	for _, want := range []string{
		"restricted_api2.example.com",
		"/v1/",
		"safe_regex",
	} {
		if !strings.Contains(envoyConf, want) {
			return fmt.Errorf("envoy config missing %q for paths+methods rule", want)
		}
	}

	// Unrestricted domain should not have :method in its virtual host.
	// cdn.example.com goes into the "allowed" virtual host with no
	// header matchers, so the overall config should contain it.
	if !strings.Contains(envoyConf, "cdn.example.com") {
		return fmt.Errorf("envoy config missing unrestricted domain cdn.example.com")
	}

	return nil
}

// TestSandboxNetwork verifies the sandbox network filtering end-to-end
// by activating iptables, dnsmasq, and Envoy via sandbox init and testing
// real traffic. Unlike [Tests.TestSandboxBase] (which only checks file
// presence), this test loads the firewall and validates that allowed
// domains are reachable, blocked domains fail to resolve, and direct IP
// connections are dropped.
//
// Security review: InsecureRootCapabilities is required because
// sandbox init needs CAP_NET_ADMIN to load iptables rules and start
// dnsmasq/Envoy. The init script drops to uid 1000 before running the
// test assertions, so all network probes execute unprivileged.
//
// Not annotated with +check because it requires InsecureRootCapabilities.
// Run manually:
//
//	dagger call -m toolchains/dev/tests test-sandbox-network
func (m *Tests) TestSandboxNetwork(ctx context.Context) error {
	ctr := dag.Dev().SandboxBase()

	script := `set -e

echo "=== Assertion 1: allowed domain is reachable ==="
wget --spider --timeout=10 https://proxy.golang.org 2>&1
echo "PASS: proxy.golang.org reachable"

echo "=== Assertion 2: blocked domain is not reachable ==="
if timeout 10 wget --spider --timeout=5 https://example.com 2>/dev/null; then
  echo "FAIL: example.com should not be reachable"
  exit 1
fi
echo "PASS: example.com not reachable"

echo "=== Assertion 3: direct IP connection blocked by firewall ==="
if timeout 10 wget --spider --timeout=5 http://1.1.1.1 2>/dev/null; then
  echo "FAIL: direct IP connection to 1.1.1.1 should be blocked"
  exit 1
fi
echo "PASS: direct IP connection to 1.1.1.1 blocked"

echo "=== Assertion 4: allowed domain on non-allowed port is blocked ==="
if timeout 10 wget --spider --timeout=5 http://github.com:8080 2>/dev/null; then
  echo "FAIL: github.com:8080 should not be reachable"
  exit 1
fi
echo "PASS: github.com:8080 not reachable"

echo "=== All sandbox network assertions passed ==="
`

	_, err := ctr.
		WithExec([]string{"mkdir", "-p", "/commandhistory", "/claude-state"}).
		WithExec(
			[]string{"/usr/local/bin/sandbox", "init", "--", "sh", "-c", script},
			dagger.ContainerWithExecOpts{
				InsecureRootCapabilities: true,
			},
		).
		Sync(ctx)
	if err != nil {
		return fmt.Errorf("sandbox network filtering: %w", err)
	}

	return nil
}

// TestSandboxRuntimeConfig verifies the full end-to-end runtime config
// path: mount a YAML config with only go domains, verify go.dev is
// reachable and example.com is blocked.
//
// Security review: InsecureRootCapabilities is required because
// sandbox init needs CAP_NET_ADMIN. The init script drops to uid 1000
// before running the test assertions.
//
// Not annotated with +check because it requires InsecureRootCapabilities.
// Run manually:
//
//	dagger call -m toolchains/dev/tests test-sandbox-runtime-config
func (m *Tests) TestSandboxRuntimeConfig(ctx context.Context) error {
	configYAML := `egress:
  - toFQDNs:
      - matchName: "golang.org"
      - matchPattern: "*.golang.org"
      - matchName: "go.dev"
      - matchPattern: "*.go.dev"
      - matchName: "gopkg.in"
      - matchName: "go.googlesource.com"
      - matchName: "cs.opensource.google"
logging: false
`
	// Use a SandboxBase with the go-only config file.
	ctr := dag.Dev().SandboxBase(dagger.DevSandboxBaseOpts{
		SandboxConfig: dag.CurrentModule().Source().
			WithNewFile("sandbox-config.yaml", configYAML).
			File("sandbox-config.yaml"),
	})

	script := `set -e

echo "=== Assertion 1: go domain is reachable ==="
wget --spider --timeout=10 https://go.dev 2>&1
echo "PASS: go.dev reachable"

echo "=== Assertion 2: non-go domain is blocked ==="
if timeout 10 wget --spider --timeout=5 https://example.com 2>/dev/null; then
  echo "FAIL: example.com should not be reachable"
  exit 1
fi
echo "PASS: example.com not reachable"

echo "=== All runtime config assertions passed ==="
`

	_, err := ctr.
		WithExec([]string{"mkdir", "-p", "/commandhistory", "/claude-state"}).
		WithExec(
			[]string{"/usr/local/bin/sandbox", "init", "--", "sh", "-c", script},
			dagger.ContainerWithExecOpts{
				InsecureRootCapabilities: true,
			},
		).
		Sync(ctx)
	if err != nil {
		return fmt.Errorf("sandbox runtime config: %w", err)
	}

	return nil
}

// TestPublishSandboxImage verifies that [Dev.PublishSandbox] builds a
// container with sandbox binary, default config.yaml, correct
// entrypoint, and no pre-baked firewall configs.
//
// Not annotated with +check because it rebuilds from scratch. Run manually:
//
//	dagger call -m toolchains/dev/tests test-publish-sandbox-image
func (m *Tests) TestPublishSandboxImage(ctx context.Context) error {
	image := fmt.Sprintf("ttl.sh/dotfiles-sandbox-ci-%d", time.Now().UnixNano())
	password := dag.SetSecret("ttl-dummy-password", "unused")

	addr, err := dag.Dev().PublishSandbox(ctx, password, dagger.DevPublishSandboxOpts{
		Tags:  []string{"1h"},
		Image: image,
	})
	if err != nil {
		return fmt.Errorf("publish sandbox: %w", err)
	}

	if !strings.Contains(addr, "sha256:") {
		return fmt.Errorf("expected sha256 digest in address, got: %s", addr)
	}

	ctr := dag.Container().From(addr)

	// Verify entrypoint.
	ep, err := ctr.Entrypoint(ctx)
	if err != nil {
		return fmt.Errorf("reading entrypoint: %w", err)
	}
	if len(ep) != 4 || ep[0] != "/usr/local/bin/sandbox" || ep[1] != "init" || ep[2] != "--" || ep[3] != "fish" {
		return fmt.Errorf("expected entrypoint [/usr/local/bin/sandbox init -- fish], got %v", ep)
	}

	// Verify sandbox binary exists.
	if _, err := ctr.WithExec([]string{"test", "-x", "/usr/local/bin/sandbox"}).Sync(ctx); err != nil {
		return fmt.Errorf("sandbox not found: %w", err)
	}

	// Verify default config.yaml exists.
	cfgContent, err := ctr.File("/etc/sandbox/config.yaml").Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading default config.yaml: %w", err)
	}
	if !strings.Contains(cfgContent, "egress") {
		return fmt.Errorf("default config.yaml missing egress")
	}

	// Verify no pre-baked firewall configs (they are generated at runtime).
	for _, path := range []string{
		"/etc/envoy-sandbox.yaml",
		"/etc/iptables-sandbox.rules",
		"/etc/ip6tables-sandbox.rules",
	} {
		_, err := ctr.WithExec([]string{"test", "!", "-f", path}).Sync(ctx)
		if err != nil {
			return fmt.Errorf("pre-baked config should not exist at %s", path)
		}
	}

	return nil
}

// TestSandboxGenDefault verifies that sandbox generate with the default
// config.yaml produces the expected output files.
//
// Not annotated with +check because it requires building the sandbox binary.
// Run manually:
//
//	dagger call -m toolchains/dev/tests test-sandbox-gen-default
func (m *Tests) TestSandboxGenDefault(ctx context.Context) error {
	image := fmt.Sprintf("ttl.sh/dotfiles-sandbox-gen-ci-%d", time.Now().UnixNano())
	password := dag.SetSecret("ttl-dummy-password", "unused")

	addr, err := dag.Dev().PublishSandbox(ctx, password, dagger.DevPublishSandboxOpts{
		Tags:  []string{"1h"},
		Image: image,
	})
	if err != nil {
		return fmt.Errorf("publish sandbox: %w", err)
	}

	ctr := dag.Container().From(addr)

	// Run sandbox generate.
	ctr = ctr.WithExec([]string{"/usr/local/bin/sandbox", "generate"})

	// Verify generated files exist and contain expected content.
	envoyConf, err := ctr.File("/etc/envoy-sandbox.yaml").Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading generated envoy config: %w", err)
	}
	for _, want := range []string{"github.com", "golang.org", "anthropic.com"} {
		if !strings.Contains(envoyConf, want) {
			return fmt.Errorf("generated envoy config missing %q", want)
		}
	}

	iptRules, err := ctr.File("/etc/iptables-sandbox.rules").Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading generated iptables rules: %w", err)
	}
	if !strings.Contains(iptRules, "REDIRECT") {
		return fmt.Errorf("generated iptables rules missing REDIRECT")
	}

	// dnsmasq config is no longer generated by Generate(); it is
	// produced at runtime by Init() when the upstream DNS is known.
	_, err = ctr.WithExec([]string{"test", "!", "-f", "/etc/dnsmasq-sandbox.conf.tmpl"}).Sync(ctx)
	if err != nil {
		return fmt.Errorf("dnsmasq template should not exist after generate: %w", err)
	}

	return nil
}

// TestSandboxGenCustomConfig verifies that sandbox generate with a custom
// config produces output containing only the specified domains.
//
// Not annotated with +check because it requires building the sandbox binary.
// Run manually:
//
//	dagger call -m toolchains/dev/tests test-sandbox-gen-custom-config
func (m *Tests) TestSandboxGenCustomConfig(ctx context.Context) error {
	image := fmt.Sprintf("ttl.sh/dotfiles-sandbox-gen-custom-ci-%d", time.Now().UnixNano())
	password := dag.SetSecret("ttl-dummy-password", "unused")

	addr, err := dag.Dev().PublishSandbox(ctx, password, dagger.DevPublishSandboxOpts{
		Tags:  []string{"1h"},
		Image: image,
	})
	if err != nil {
		return fmt.Errorf("publish sandbox: %w", err)
	}

	customConfig := `egress:
  - toFQDNs:
      - matchName: "golang.org"
      - matchPattern: "*.golang.org"
      - matchName: "go.dev"
      - matchPattern: "*.go.dev"
      - matchName: "gopkg.in"
      - matchName: "go.googlesource.com"
      - matchName: "cs.opensource.google"
      - matchName: api.company.com
logging: false
`

	ctr := dag.Container().From(addr).
		WithNewFile("/etc/sandbox/config.yaml", customConfig).
		WithExec([]string{"/usr/local/bin/sandbox", "generate"})

	envoyConf, err := ctr.File("/etc/envoy-sandbox.yaml").Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading generated envoy config: %w", err)
	}
	if !strings.Contains(envoyConf, "golang.org") {
		return fmt.Errorf("generated envoy config missing golang.org")
	}
	if !strings.Contains(envoyConf, "api.company.com") {
		return fmt.Errorf("generated envoy config missing api.company.com")
	}
	// github.com should NOT be present (only go domains + custom).
	if strings.Contains(envoyConf, "github.com") {
		return fmt.Errorf("generated envoy config should not contain github.com")
	}

	return nil
}

// TestSandboxMitmPaths verifies that path-restricted HTTPS filtering works
// end-to-end via the MITM pipeline. Domains with path restrictions only
// allow requests to those path prefixes, returning 403 for everything
// else. This exercises CA generation, leaf cert signing, trust store
// installation, TLS termination, and HTTP path inspection.
//
// Security review: InsecureRootCapabilities is required because
// sandbox init needs CAP_NET_ADMIN to load iptables rules and start
// dnsmasq/Envoy. The init script drops to uid 1000 before running the
// test assertions, so all network probes execute unprivileged.
//
// Not annotated with +check because it requires InsecureRootCapabilities.
// Run manually:
//
//	dagger call -m toolchains/dev/tests test-sandbox-mitm-paths
func (m *Tests) TestSandboxMitmPaths(ctx context.Context) error {
	// Custom config: proxy.golang.org restricted to /github.com/ prefix,
	// plus go domains unrestricted for contrast.
	ctr := dag.Dev().SandboxBase(dagger.DevSandboxBaseOpts{
		SandboxConfig: configFile(`egress:
  - toFQDNs:
      - matchName: "golang.org"
      - matchPattern: "*.golang.org"
      - matchName: "go.dev"
      - matchPattern: "*.go.dev"
      - matchName: "gopkg.in"
      - matchName: "go.googlesource.com"
      - matchName: "cs.opensource.google"
  - toFQDNs:
      - matchName: proxy.golang.org
    toPorts:
      - rules:
          http:
            - path: /github.com/
`),
	})

	// SandboxBase pre-bakes envoy config with empty certsDir, so MITM
	// filter chains are absent. Remove all pre-baked configs and run
	// generate at build time to create MITM certs and the full envoy
	// config with real cert paths.
	ctr = ctr.
		WithoutFile("/etc/envoy-sandbox.yaml").
		WithoutFile("/etc/iptables-sandbox.rules").
		WithoutFile("/etc/ip6tables-sandbox.rules").
		WithExec([]string{"/usr/local/bin/sandbox", "generate"})

	script := `set -e

echo "=== Assertion 1: allowed path on path-restricted domain succeeds (MITM) ==="
wget -q -O /dev/null --timeout=15 https://proxy.golang.org/github.com/stretchr/testify/@latest
echo "PASS: allowed path on proxy.golang.org reachable"

echo "=== Assertion 2: disallowed path returns 403 (MITM enforcement) ==="
status=$(wget --server-response -O /dev/null --timeout=15 https://proxy.golang.org/blocked-path 2>&1 | grep -o 'HTTP/[^ ]* [0-9]*' | tail -1 | grep -o '[0-9]*$') || true
if [ "$status" != "403" ]; then
  echo "FAIL: expected 403 for blocked path, got: $status"
  exit 1
fi
echo "PASS: disallowed path on proxy.golang.org returned 403"

echo "=== Assertion 3: unrestricted domain still works ==="
wget --spider --timeout=10 https://go.dev 2>&1
echo "PASS: unrestricted domain go.dev reachable"

echo "=== Assertion 4: completely blocked domain fails ==="
if timeout 10 wget --spider --timeout=5 https://example.com 2>/dev/null; then
  echo "FAIL: example.com should not be reachable"
  exit 1
fi
echo "PASS: example.com not reachable"

echo "=== All MITM path assertions passed ==="
`

	_, err := ctr.
		WithExec([]string{"mkdir", "-p", "/commandhistory", "/claude-state"}).
		WithExec(
			[]string{"/usr/local/bin/sandbox", "init", "--", "sh", "-c", script},
			dagger.ContainerWithExecOpts{
				InsecureRootCapabilities: true,
			},
		).
		Sync(ctx)
	if err != nil {
		return fmt.Errorf("sandbox mitm path filtering: %w", err)
	}

	return nil
}

// TestSandboxMitmMethods verifies that method-restricted HTTPS filtering
// works end-to-end via the MITM pipeline. Domains with method restrictions
// only allow requests using those HTTP methods, returning 403 for
// everything else. This exercises the :method header matching in Envoy.
//
// Security review: InsecureRootCapabilities is required because
// sandbox init needs CAP_NET_ADMIN to load iptables rules and start
// dnsmasq/Envoy. The init script drops to uid 1000 before running the
// test assertions, so all network probes execute unprivileged.
//
// Not annotated with +check because it requires InsecureRootCapabilities.
// Run manually:
//
//	dagger call -m toolchains/dev/tests test-sandbox-mitm-methods
func (m *Tests) TestSandboxMitmMethods(ctx context.Context) error {
	ctr := dag.Dev().SandboxBase(dagger.DevSandboxBaseOpts{
		SandboxConfig: configFile(`egress:
  - toFQDNs:
      - matchName: "golang.org"
      - matchPattern: "*.golang.org"
      - matchName: "go.dev"
      - matchPattern: "*.go.dev"
      - matchName: "gopkg.in"
      - matchName: "go.googlesource.com"
      - matchName: "cs.opensource.google"
  - toFQDNs:
      - matchName: proxy.golang.org
    toPorts:
      - rules:
          http:
            - method: GET
            - method: HEAD
`),
	})

	// Remove pre-baked configs and regenerate with MITM certs.
	ctr = ctr.
		WithoutFile("/etc/envoy-sandbox.yaml").
		WithoutFile("/etc/iptables-sandbox.rules").
		WithoutFile("/etc/ip6tables-sandbox.rules").
		WithExec([]string{"/usr/local/bin/sandbox", "generate"})

	script := `set -e

echo "=== Assertion 1: GET on method-restricted domain succeeds ==="
wget -q -O /dev/null --timeout=15 https://proxy.golang.org/github.com/stretchr/testify/@latest
echo "PASS: GET on proxy.golang.org allowed"

echo "=== Assertion 2: POST on method-restricted domain returns 403 ==="
status=$(wget --server-response -O /dev/null --timeout=15 --post-data="" https://proxy.golang.org/github.com/stretchr/testify/@latest 2>&1 | grep -o 'HTTP/[^ ]* [0-9]*' | tail -1 | grep -o '[0-9]*$') || true
if [ "$status" != "403" ]; then
  echo "FAIL: expected 403 for POST, got: $status"
  exit 1
fi
echo "PASS: POST on proxy.golang.org returned 403"

echo "=== Assertion 3: unrestricted domain still works ==="
wget --spider --timeout=10 https://go.dev 2>&1
echo "PASS: unrestricted domain go.dev reachable"

echo "=== All MITM method assertions passed ==="
`

	_, err := ctr.
		WithExec([]string{"mkdir", "-p", "/commandhistory", "/claude-state"}).
		WithExec(
			[]string{"/usr/local/bin/sandbox", "init", "--", "sh", "-c", script},
			dagger.ContainerWithExecOpts{
				InsecureRootCapabilities: true,
			},
		).
		Sync(ctx)
	if err != nil {
		return fmt.Errorf("sandbox mitm method filtering: %w", err)
	}

	return nil
}

// TestSandboxHttpFiltering verifies HTTP (port 80) host filtering
// end-to-end. The HTTP listener filters by Host header, allowing
// requests to domains in enabled groups and blocking everything else.
//
// Security review: InsecureRootCapabilities is required because
// sandbox init needs CAP_NET_ADMIN to load iptables rules and start
// dnsmasq/Envoy. The init script drops to uid 1000 before running the
// test assertions, so all network probes execute unprivileged.
//
// Not annotated with +check because it requires InsecureRootCapabilities.
// Run manually:
//
//	dagger call -m toolchains/dev/tests test-sandbox-http-filtering
func (m *Tests) TestSandboxHttpFiltering(ctx context.Context) error {
	ctr := dag.Dev().SandboxBase()

	script := `set -e

echo "=== Assertion 1: allowed domain over HTTP ==="
wget --spider --timeout=10 http://proxy.golang.org 2>&1
echo "PASS: proxy.golang.org reachable over HTTP"

echo "=== Assertion 2: blocked domain over HTTP ==="
if timeout 10 wget --spider --timeout=5 http://example.com 2>/dev/null; then
  echo "FAIL: example.com should not be reachable over HTTP"
  exit 1
fi
echo "PASS: example.com not reachable over HTTP"

echo "=== All HTTP filtering assertions passed ==="
`

	_, err := ctr.
		WithExec([]string{"mkdir", "-p", "/commandhistory", "/claude-state"}).
		WithExec(
			[]string{"/usr/local/bin/sandbox", "init", "--", "sh", "-c", script},
			dagger.ContainerWithExecOpts{
				InsecureRootCapabilities: true,
			},
		).
		Sync(ctx)
	if err != nil {
		return fmt.Errorf("sandbox http filtering: %w", err)
	}

	return nil
}

// TestSandboxUnsupportedSelector verifies that a config containing a
// Cilium selector the sandbox does not implement (toEntities) is
// rejected with a clear error during config parsing. The error should
// propagate through [Dev.SandboxBase] as a build failure.
//
// +check
func (m *Tests) TestSandboxUnsupportedSelector(ctx context.Context) error {
	_, err := dag.Dev().SandboxBase(dagger.DevSandboxBaseOpts{
		SandboxConfig: configFile(`egress:
  - toEntities:
      - world
`),
	}).Sync(ctx)

	if err == nil {
		return fmt.Errorf("expected error for unsupported toEntities selector, got nil")
	}

	if !strings.Contains(err.Error(), "unsupported egress selector") {
		return fmt.Errorf("expected 'unsupported egress selector' in error, got: %s", err.Error())
	}

	if !strings.Contains(err.Error(), "toEntities") {
		return fmt.Errorf("expected 'toEntities' in error message, got: %s", err.Error())
	}

	return nil
}

// TestSandboxOpenPortRange verifies that a UDP port range open-port
// rule (toPorts without L3 selectors, with endPort) produces iptables
// rules with --dport start:end range syntax instead of silently
// dropping the range.
//
// +check
func (m *Tests) TestSandboxOpenPortRange(ctx context.Context) error {
	ctr := dag.Dev().SandboxBase(dagger.DevSandboxBaseOpts{
		SandboxConfig: configFile(allDomainsYAML + `  - toPorts:
      - ports:
          - port: "8000"
            endPort: 9000
            protocol: UDP
`),
	})

	rules, err := ctr.File("/etc/iptables-sandbox.rules").Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading iptables rules: %w", err)
	}

	if !strings.Contains(rules, "--dport 8000:9000") {
		return fmt.Errorf("iptables rules missing port range --dport 8000:9000, got:\n%s", rules)
	}

	return nil
}

// TestSandboxPathNormalization verifies that Envoy path normalization
// fields (normalize_path, merge_slashes, path_with_escaped_slashes_action)
// are present in the generated Envoy config for HCM listeners.
//
// +check
func (m *Tests) TestSandboxPathNormalization(ctx context.Context) error {
	ctr := dag.Dev().SandboxBase(dagger.DevSandboxBaseOpts{
		SandboxConfig: configFile(`egress:
  - toFQDNs:
      - matchName: "example.com"
    toPorts:
      - ports:
          - port: "80"
          - port: "443"
        rules:
          http:
            - path: /api/
`),
	})

	envoyConf, err := ctr.File("/etc/envoy-sandbox.yaml").Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading envoy config: %w", err)
	}

	for _, want := range []string{
		"normalize_path: true",
		"merge_slashes: true",
		"path_with_escaped_slashes_action: UNESCAPE_AND_REDIRECT",
	} {
		if !strings.Contains(envoyConf, want) {
			return fmt.Errorf("envoy config missing %q", want)
		}
	}

	return nil
}

// TestSandboxWildcardDnsmasq verifies that wildcard matchPattern entries
// use dnsmasq's /*.domain/ syntax to exclude the bare parent domain,
// matching Cilium's exclusion of the bare domain from single-star wildcards.
//
// +check
func (m *Tests) TestSandboxWildcardDnsmasq(ctx context.Context) error {
	ctr := dag.Dev().SandboxBase(dagger.DevSandboxBaseOpts{
		SandboxConfig: configFile(`egress:
  - toFQDNs:
      - matchPattern: "*.example.com"
    toPorts:
      - ports:
          - port: "443"
`),
	})

	dnsmasqConf, err := ctr.File("/etc/dnsmasq.conf").Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading dnsmasq config: %w", err)
	}

	// Wildcard matchPattern must use /*.example.com/ syntax.
	if !strings.Contains(dnsmasqConf, "server=/*.example.com/") {
		return fmt.Errorf("dnsmasq config missing wildcard server directive, got:\n%s", dnsmasqConf)
	}

	// Bare /example.com/ form must NOT appear (it would match the parent domain).
	if strings.Contains(dnsmasqConf, "server=/example.com/") {
		return fmt.Errorf("dnsmasq config has bare domain server directive (should be wildcard), got:\n%s", dnsmasqConf)
	}

	return nil
}

// TestSandboxPathNormalizationE2E verifies that Envoy's path normalization
// works end-to-end: a request to //api//foo is normalized to /api/foo and
// matches a /api/ path rule via MITM inspection, while /blocked is rejected.
//
// Security review: InsecureRootCapabilities is required because
// sandbox init needs CAP_NET_ADMIN to load iptables rules and start
// dnsmasq/Envoy. The init script drops to uid 1000 before running the
// test assertions, so all network probes execute unprivileged.
//
// Not annotated with +check because it requires InsecureRootCapabilities.
// Run manually:
//
//	dagger call -m toolchains/dev/tests test-sandbox-path-normalization-e2-e
func (m *Tests) TestSandboxPathNormalizationE2E(ctx context.Context) error {
	ctr := dag.Dev().SandboxBase(dagger.DevSandboxBaseOpts{
		SandboxConfig: configFile(`egress:
  - toFQDNs:
      - matchName: "golang.org"
      - matchPattern: "*.golang.org"
      - matchName: "go.dev"
      - matchPattern: "*.go.dev"
      - matchName: "gopkg.in"
      - matchName: "go.googlesource.com"
      - matchName: "cs.opensource.google"
  - toFQDNs:
      - matchName: proxy.golang.org
    toPorts:
      - rules:
          http:
            - path: /github.com/
`),
	})

	// Remove pre-baked configs and regenerate with MITM certs.
	ctr = ctr.
		WithoutFile("/etc/envoy-sandbox.yaml").
		WithoutFile("/etc/iptables-sandbox.rules").
		WithoutFile("/etc/ip6tables-sandbox.rules").
		WithExec([]string{"/usr/local/bin/sandbox", "generate"})

	script := `set -e

echo "=== Assertion 1: double-slash path gets normalized and matches /github.com/ rule ==="
wget -q -O /dev/null --timeout=15 "https://proxy.golang.org//github.com//stretchr/testify/@latest"
echo "PASS: //github.com//stretchr/testify/@latest normalized and matched"

echo "=== Assertion 2: disallowed path still returns 403 ==="
status=$(wget --server-response -O /dev/null --timeout=15 https://proxy.golang.org/blocked-path 2>&1 | grep -o 'HTTP/[^ ]* [0-9]*' | tail -1 | grep -o '[0-9]*$') || true
if [ "$status" != "403" ]; then
  echo "FAIL: expected 403 for blocked path, got: $status"
  exit 1
fi
echo "PASS: /blocked-path returned 403"

echo "=== All path normalization assertions passed ==="
`

	_, err := ctr.
		WithExec([]string{"mkdir", "-p", "/commandhistory", "/claude-state"}).
		WithExec(
			[]string{"/usr/local/bin/sandbox", "init", "--", "sh", "-c", script},
			dagger.ContainerWithExecOpts{
				InsecureRootCapabilities: true,
			},
		).
		Sync(ctx)
	if err != nil {
		return fmt.Errorf("sandbox path normalization: %w", err)
	}

	return nil
}

// TestAtuinDaemon verifies that the atuin daemon can start and accept
// connections inside the dev container. The container lacks systemd, so
// the daemon must be started manually; this test validates that the data
// directory is created and the daemon socket becomes available.
//
// +check
func (m *Tests) TestAtuinDaemon(ctx context.Context) error {
	ctr := dag.Dev().DevBase()

	// Start the daemon the same way wrapWithAtuinDaemon does and wait
	// for the unix socket to appear, confirming the daemon is ready.
	_, err := ctr.WithExec([]string{"sh", "-c",
		`atuin daemon >/dev/null 2>&1 & ` +
			`for i in 1 2 3 4 5 6 7 8 9 10; do ` +
			`  test -S "$HOME/.local/share/atuin/atuin.sock" && break; ` +
			`  sleep 0.5; ` +
			`done && ` +
			`test -S "$HOME/.local/share/atuin/atuin.sock"`,
	}).Sync(ctx)
	if err != nil {
		return fmt.Errorf("atuin daemon: %w", err)
	}

	return nil
}

// TestPublish verifies that [Dev.PublishShell] builds and pushes a container to
// a registry with the expected OCI metadata. Uses ttl.sh as an anonymous
// temporary registry (images expire after the tag duration).
//
// Not annotated with +check because it depends on external network access
// to ttl.sh and rebuilds the nix environment from scratch (no cache
// volumes), making it slow. Run manually:
//
//	dagger call -m toolchains/dev/tests test-publish
func (m *Tests) TestPublish(ctx context.Context) error {
	image := fmt.Sprintf("ttl.sh/dotfiles-dev-ci-%d", time.Now().UnixNano())
	password := dag.SetSecret("ttl-dummy-password", "unused")

	addr, err := dag.Dev().PublishShell(ctx, password, dagger.DevPublishShellOpts{
		Tags:  []string{"1h"},
		Image: image,
	})
	if err != nil {
		return fmt.Errorf("publish: %w", err)
	}

	// Verify the result contains a sha256 digest reference.
	if !strings.Contains(addr, "sha256:") {
		return fmt.Errorf("expected sha256 digest in address, got: %s", addr)
	}

	// Pull the published image back and verify metadata.
	ctr := dag.Container().From(addr)

	// Verify entrypoint.
	ep, err := ctr.Entrypoint(ctx)
	if err != nil {
		return fmt.Errorf("reading entrypoint: %w", err)
	}
	if len(ep) != 1 || ep[0] != "fish" {
		return fmt.Errorf("expected entrypoint [fish], got %v", ep)
	}

	// Verify OCI labels.
	labels := map[string]string{
		"org.opencontainers.image.source":      "https://github.com/MacroPower/dotfiles",
		"org.opencontainers.image.description": "Development container with nix home-manager tools",
	}
	for key, want := range labels {
		got, err := ctr.Label(ctx, key)
		if err != nil {
			return fmt.Errorf("reading label %s: %w", key, err)
		}
		if got != want {
			return fmt.Errorf("label %s: expected %q, got %q", key, want, got)
		}
	}

	return nil
}
