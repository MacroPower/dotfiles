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
		"wildcard matchPattern uses dnsmasq wildcard syntax": {
			upstream: "8.8.8.8",
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*.github.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			want:    []string{"server=/*.github.com/8.8.8.8", "address=/#/"},
			notWant: []string{"server=8.8.8.8\n", "server=/github.com/"},
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
		"double-star wildcard uses dnsmasq wildcard syntax": {
			upstream: "8.8.8.8",
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "**.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			want:    []string{"server=/*.example.com/8.8.8.8", "address=/#/"},
			notWant: []string{"server=8.8.8.8\n", "**.example.com", "server=/example.com/"},
		},
		"wildcard matchPattern with UDP uses dnsmasq wildcard ipset": {
			upstream: "8.8.8.8",
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "5000", Protocol: "UDP"}}}},
				}),
			},
			want: []string{
				"server=/*.example.com/8.8.8.8",
				"ipset=/*.example.com/sandbox_fqdn4,sandbox_fqdn6",
				"address=/#/",
			},
			notWant: []string{"server=/example.com/", "ipset=/example.com/"},
		},
		"matchName uses plain domain syntax": {
			upstream: "8.8.8.8",
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			want:    []string{"server=/api.example.com/8.8.8.8", "address=/#/"},
			notWant: []string{"server=/*.api.example.com/"},
		},
		"mixed matchName and wildcard matchPattern": {
			upstream: "1.1.1.1",
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{
						{MatchName: "exact.example.com"},
						{MatchPattern: "*.wild.example.com"},
					},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			want: []string{
				"server=/exact.example.com/1.1.1.1",
				"server=/*.wild.example.com/1.1.1.1",
				"address=/#/",
			},
			notWant: []string{"server=/wild.example.com/"},
		},
		"matchName upgrades wildcard to non-wildcard for same domain": {
			upstream: "8.8.8.8",
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*.example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
				),
			},
			want:    []string{"server=/example.com/8.8.8.8", "address=/#/"},
			notWant: []string{"server=/*.example.com/"},
		},
		"TCPForward host upgrades wildcard for same domain": {
			upstream: "8.8.8.8",
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
				TCPForwards: []sandbox.TCPForward{{Port: 22, Host: "example.com"}},
			},
			want:    []string{"server=/example.com/8.8.8.8", "address=/#/"},
			notWant: []string{"server=/*.example.com/"},
		},
		"cache disabled in unrestricted mode": {
			upstream: "8.8.8.8",
			cfg:      &sandbox.SandboxConfig{},
			want:     []string{"cache-size=0", "server=8.8.8.8"},
		},
		"cache disabled in blocked mode": {
			upstream: "8.8.8.8",
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{}),
			},
			want: []string{"cache-size=0", "address=/#/"},
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

			// All modes must disable caching to match Cilium semantics.
			assert.Contains(t, conf, "cache-size=0")

			for _, s := range tt.want {
				assert.Contains(t, conf, s)
			}

			for _, s := range tt.notWant {
				assert.NotContains(t, conf, s)
			}
		})
	}
}
