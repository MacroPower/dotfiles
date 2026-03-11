package sandbox_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go.jacobcolvin.com/dotfiles/toolchains/dev/sandbox"
)

func TestGenerateDnsmasqConfig(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		upstream string
		cfg      *sandbox.SandboxConfig
		want     []string
		notWant  []string
	}{
		"ipv4 upstream": {
			upstream: "8.8.8.8",
			want: []string{
				"server=8.8.8.8",
				"listen-address=127.0.0.1",
				"listen-address=::1",
				"no-resolv",
				"port=53",
			},
			notWant: []string{"server=/", "address=/#/"},
		},
		"ipv6 upstream": {
			upstream: "2001:4860:4860::8888",
			want:     []string{"server=2001:4860:4860::8888", "no-resolv"},
			notWant:  []string{"server=/", "address=/#/"},
		},
		"nil config forwards all": {
			upstream: "8.8.8.8",
			want:     []string{"server=8.8.8.8"},
			notWant:  []string{"server=/", "address=/#/"},
		},
		"unrestricted mode forwards all": {
			upstream: "8.8.8.8",
			cfg:      &sandbox.SandboxConfig{},
			want:     []string{"server=8.8.8.8"},
			notWant:  []string{"server=/", "address=/#/"},
		},
		"restricted mode with FQDN domains": {
			upstream: "8.8.8.8",
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "github.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			want:    []string{"server=/github.com/8.8.8.8", "address=/#/"},
			notWant: []string{"server=8.8.8.8\n"},
		},
		"restricted mode strips wildcard prefix": {
			upstream: "8.8.8.8",
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*.github.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			want:    []string{"server=/github.com/8.8.8.8", "address=/#/"},
			notWant: []string{"server=8.8.8.8\n"},
		},
		"blocked mode returns NXDOMAIN only": {
			upstream: "8.8.8.8",
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{}),
			},
			want:    []string{"address=/#/"},
			notWant: []string{"server=/", "server=8.8.8.8"},
		},
		"includes TCPForward hosts": {
			upstream: "8.8.8.8",
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "github.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
				TCPForwards: []sandbox.TCPForward{{Port: 22, Host: "git.example.com"}},
			},
			want: []string{
				"server=/github.com/8.8.8.8",
				"server=/git.example.com/8.8.8.8",
				"address=/#/",
			},
		},
		"bare wildcard forwards all": {
			upstream: "8.8.8.8",
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			want:    []string{"server=8.8.8.8"},
			notWant: []string{"server=/", "address=/#/"},
		},
		"FQDN with UDP port adds ipset directive": {
			upstream: "8.8.8.8",
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "github.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
				}),
			},
			want: []string{
				"server=/github.com/8.8.8.8",
				"ipset=/github.com/sandbox_fqdn4,sandbox_fqdn6",
			},
		},
		"TCP-only FQDN has no ipset directive": {
			upstream: "8.8.8.8",
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "github.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "TCP"}}}},
				}),
			},
			want:    []string{"server=/github.com/8.8.8.8"},
			notWant: []string{"ipset="},
		},
		"bare wildcard with UDP adds catch-all ipset": {
			upstream: "8.8.8.8",
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
				}),
			},
			want: []string{
				"server=8.8.8.8",
				"ipset=/#/sandbox_fqdn4,sandbox_fqdn6",
			},
		},
		"rules-only mode forwards all": {
			upstream: "8.8.8.8",
			cfg: &sandbox.SandboxConfig{
				EnableDefaultDeny: sandbox.DefaultDenyConfig{Egress: boolPtr(false)},
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			want:    []string{"server=8.8.8.8"},
			notWant: []string{"server=/", "address=/#/"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			conf := sandbox.GenerateDnsmasqConfig(tt.upstream, tt.cfg)

			for _, s := range tt.want {
				assert.Contains(t, conf, s)
			}

			for _, s := range tt.notWant {
				assert.NotContains(t, conf, s)
			}
		})
	}
}
