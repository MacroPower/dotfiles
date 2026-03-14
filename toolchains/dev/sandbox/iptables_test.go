package sandbox_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"go.jacobcolvin.com/dotfiles/toolchains/dev/sandbox"
)

func TestGenerateIptablesRules(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg                *sandbox.SandboxConfig
		wantIPv4           []string
		notWantIPv4        []string
		wantIPv6           []string
		notWantIPv6        []string
		wantRedirectCount4 int
	}{
		"CIDR rules create user ACCEPT and NAT RETURN": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{ToCIDRSet: []sandbox.CIDRRule{
						{CIDR: "0.0.0.0/0", Except: []string{
							"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
						}},
						{CIDR: "::/0", Except: []string{"fc00::/7", "fe80::/10"}},
					}},
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{
							{Port: "80"}, {Port: "443"}, {Port: "8080"},
						}}},
					},
				),
				Logging: true,
			},
			wantIPv4: []string{
				// NAT: CIDR RETURN before redirects.
				"-A OUTPUT -m owner --uid-owner 1000 -d 0.0.0.0/0 -j RETURN",
				"--to-port 15080", "--to-port 15443", "--to-port 23080",
				// FILTER: per-rule chain with except RETURN.
				"-N CIDR_4_0",
				"-A CIDR_4_0 -m owner --uid-owner 1000 -d 10.0.0.0/8 -j RETURN",
				"-A CIDR_4_0 -m owner --uid-owner 1000 -d 172.16.0.0/12 -j RETURN",
				"-A CIDR_4_0 -m owner --uid-owner 1000 -d 192.168.0.0/16 -j RETURN",
				// FILTER: CIDR ACCEPT within per-rule chain.
				"-A CIDR_4_0 -m owner --uid-owner 1000 -d 0.0.0.0/0 -j ACCEPT",
				"-A OUTPUT -j CIDR_4_0",
				"LOG",
			},
			notWantIPv4: []string{
				"-d 10.0.0.0/8 -j DROP",
				"-A OUTPUT -m owner --uid-owner 1000 -d 0.0.0.0/0 -j ACCEPT",
			},
			wantIPv6: []string{
				"-A OUTPUT -m owner --uid-owner 1000 -d ::/0 -j RETURN",
				"-N CIDR_6_0",
				"-A CIDR_6_0 -m owner --uid-owner 1000 -d fc00::/7 -j RETURN",
				"-A CIDR_6_0 -m owner --uid-owner 1000 -d fe80::/10 -j RETURN",
				"-A CIDR_6_0 -m owner --uid-owner 1000 -d ::/0 -j ACCEPT",
				"-A OUTPUT -j CIDR_6_0",
			},
			notWantIPv6: []string{
				"-d fc00::/7 -j DROP",
			},
		},
		"no CIDR rules means no IP-level rules": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}, {Port: "80"}}}},
				}),
			},
			notWantIPv4: []string{
				"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
				"-j RETURN",
			},
			notWantIPv6: []string{"fc00::/7", "fe80::/10"},
		},
		"no logging": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
				Logging: false,
			},
			notWantIPv4: []string{"LOG"},
			notWantIPv6: []string{"LOG"},
		},
		"single port": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			wantIPv4:           []string{"--to-port 15443"},
			wantRedirectCount4: 1,
		},
		"IPv6 rules": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{
						{CIDR: "0.0.0.0/0"},
						{CIDR: "::/0", Except: []string{"fc00::/7", "fe80::/10"}},
					},
				}),
			},
			wantIPv6: []string{
				"::1/128",
				"-A CIDR_6_0 -m owner --uid-owner 1000 -d fc00::/7 -j RETURN",
				"-A CIDR_6_0 -m owner --uid-owner 1000 -d fe80::/10 -j RETURN",
			},
			notWantIPv6: []string{"127.0.0.0/8"},
		},
		"tcp forwards get redirect rules": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}, {Port: "80"}}}},
				}),
				TCPForwards: []sandbox.TCPForward{
					{Port: 22, Host: "github.com"},
					{Port: 3306, Host: "db.example.com"},
				},
			},
			wantIPv4: []string{
				"--dport 22 -j REDIRECT --to-port 15022",
				"--dport 3306 -j REDIRECT --to-port 18306",
				"--to-port 15080", "--to-port 15443",
			},
			wantRedirectCount4: 4,
		},
		"tcp forwards in both ipv4 and ipv6": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
				TCPForwards: []sandbox.TCPForward{{Port: 22, Host: "github.com"}},
			},
			wantIPv4: []string{"--dport 22 -j REDIRECT --to-port 15022"},
			wantIPv6: []string{"--dport 22 -j REDIRECT --to-port 15022"},
		},
		"toCIDR produces same rules as toCIDRSet": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{ToCIDR: []string{"8.8.8.0/24"}},
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
				),
			},
			wantIPv4: []string{
				"-A OUTPUT -m owner --uid-owner 1000 -d 8.8.8.0/24 -j RETURN",
				"-A CIDR_4_0 -m owner --uid-owner 1000 -d 8.8.8.0/24 -j ACCEPT",
				"-A OUTPUT -j CIDR_4_0",
			},
		},
		"UDP CIDR rules use -p udp": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "8.8.8.0/24"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "53", Protocol: "UDP"}}}},
				}),
			},
			wantIPv4: []string{
				"-A OUTPUT -m owner --uid-owner 1000 -p udp --dport 53 -d 8.8.8.0/24 -j RETURN",
				"-A CIDR_4_0 -m owner --uid-owner 1000 -p udp --dport 53 -d 8.8.8.0/24 -j ACCEPT",
			},
		},
		"ANY protocol CIDR expands to tcp and udp": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "8.8.8.0/24"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "53", Protocol: "ANY"}}}},
				}),
			},
			wantIPv4: []string{
				"-A OUTPUT -m owner --uid-owner 1000 -p tcp --dport 53 -d 8.8.8.0/24 -j RETURN",
				"-A OUTPUT -m owner --uid-owner 1000 -p udp --dport 53 -d 8.8.8.0/24 -j RETURN",
				"-A CIDR_4_0 -m owner --uid-owner 1000 -p tcp --dport 53 -d 8.8.8.0/24 -j ACCEPT",
				"-A CIDR_4_0 -m owner --uid-owner 1000 -p udp --dport 53 -d 8.8.8.0/24 -j ACCEPT",
			},
			notWantIPv4: []string{"-m multiport", "-p sctp"},
		},
		"mixed TCP/UDP ports": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "8.8.8.0/24"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{
						{Port: "443", Protocol: "TCP"},
						{Port: "53", Protocol: "UDP"},
					}}},
				}),
			},
			wantIPv4: []string{
				"-A CIDR_4_0 -m owner --uid-owner 1000 -p udp --dport 53 -d 8.8.8.0/24 -j ACCEPT",
				"-A CIDR_4_0 -m owner --uid-owner 1000 -p tcp --dport 443 -d 8.8.8.0/24 -j ACCEPT",
			},
		},
		"port range CIDR uses --dport range syntax": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "8.8.8.0/24"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8000", EndPort: 9000}}}},
				}),
			},
			wantIPv4: []string{
				"-A OUTPUT -m owner --uid-owner 1000 -p tcp --dport 8000:9000 -d 8.8.8.0/24 -j RETURN",
				"-A OUTPUT -m owner --uid-owner 1000 -p udp --dport 8000:9000 -d 8.8.8.0/24 -j RETURN",
				"-A CIDR_4_0 -m owner --uid-owner 1000 -p tcp --dport 8000:9000 -d 8.8.8.0/24 -j ACCEPT",
				"-A CIDR_4_0 -m owner --uid-owner 1000 -p udp --dport 8000:9000 -d 8.8.8.0/24 -j ACCEPT",
			},
			notWantIPv4: []string{"-p sctp"},
		},
		"port-scoped CIDR rules": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "8.8.8.0/24"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			wantIPv4: []string{
				// NAT: port-scoped RETURN for both protocols.
				"-A OUTPUT -m owner --uid-owner 1000 -p tcp --dport 443 -d 8.8.8.0/24 -j RETURN",
				"-A OUTPUT -m owner --uid-owner 1000 -p udp --dport 443 -d 8.8.8.0/24 -j RETURN",
				// FILTER: port-scoped ACCEPT in per-rule chain.
				"-A CIDR_4_0 -m owner --uid-owner 1000 -p tcp --dport 443 -d 8.8.8.0/24 -j ACCEPT",
				"-A CIDR_4_0 -m owner --uid-owner 1000 -p udp --dport 443 -d 8.8.8.0/24 -j ACCEPT",
			},
			notWantIPv4: []string{"-p sctp"},
		},
		"port-scoped CIDR with except": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{
						CIDR:   "0.0.0.0/0",
						Except: []string{"10.0.0.0/8"},
					}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			wantIPv4: []string{
				"-A OUTPUT -m owner --uid-owner 1000 -p tcp --dport 443 -d 0.0.0.0/0 -j RETURN",
				"-A OUTPUT -m owner --uid-owner 1000 -p udp --dport 443 -d 0.0.0.0/0 -j RETURN",
				"-A CIDR_4_0 -m owner --uid-owner 1000 -p tcp --dport 443 -d 10.0.0.0/8 -j RETURN",
				"-A CIDR_4_0 -m owner --uid-owner 1000 -p udp --dport 443 -d 10.0.0.0/8 -j RETURN",
				"-A CIDR_4_0 -m owner --uid-owner 1000 -p tcp --dport 443 -d 0.0.0.0/0 -j ACCEPT",
				"-A CIDR_4_0 -m owner --uid-owner 1000 -p udp --dport 443 -d 0.0.0.0/0 -j ACCEPT",
			},
			notWantIPv4: []string{
				"-d 10.0.0.0/8 -j DROP",
				"-p sctp",
			},
		},
		"CIDR RETURN comes before REDIRECT in NAT": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{ToCIDRSet: []sandbox.CIDRRule{{CIDR: "8.8.8.0/24"}}},
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
				),
			},
			wantIPv4: []string{
				"-A OUTPUT -m owner --uid-owner 1000 -d 8.8.8.0/24 -j RETURN",
			},
		},
		"envoy can reach any IP": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			wantIPv4: []string{
				"-A OUTPUT -m owner --uid-owner 999 -j ACCEPT",
			},
		},
		// Three-mode tests.
		"unrestricted: nil egress allows all": {
			cfg: &sandbox.SandboxConfig{},
			wantIPv4: []string{
				"-A OUTPUT -j ACCEPT",
			},
			notWantIPv4: []string{
				"REDIRECT", "DROP",
				"--uid-owner 999",
			},
		},
		"empty rule triggers deny-all": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{}),
			},
			wantIPv4: []string{
				"-A OUTPUT -j DROP",
			},
			notWantIPv4: []string{
				"REDIRECT",
				"--uid-owner 999",
			},
		},
		"empty rule with FQDN+L7 has default-deny": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{},
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
						ToPorts: []sandbox.PortRule{{
							Ports: []sandbox.Port{{Port: "443"}},
							Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
						}},
					},
				),
			},
			wantIPv4: []string{
				"--to-port 15443",
				"-A OUTPUT -m owner --uid-owner 999 -j ACCEPT",
			},
			notWantIPv4: []string{
				"-A OUTPUT -j ACCEPT",
			},
		},
		"unrestricted with tcp forwards": {
			cfg: &sandbox.SandboxConfig{
				TCPForwards: []sandbox.TCPForward{{Port: 22, Host: "github.com"}},
			},
			wantIPv4: []string{
				"--dport 22 -j REDIRECT --to-port 15022",
				"-A OUTPUT -j ACCEPT",
			},
			notWantIPv4: []string{
				"DROP",
				"--to-port 15443",
				"--to-port 15080",
			},
			wantRedirectCount4: 1,
		},
		"open UDP port gets ACCEPT": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
					sandbox.EgressRule{
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "5353", Protocol: "UDP"}}}},
					},
				),
			},
			wantIPv4: []string{
				"-A OUTPUT -m owner --uid-owner 1000 -p udp --dport 5353 -j ACCEPT",
			},
		},
		"open ANY port gets redirect and UDP ACCEPT": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
					sandbox.EgressRule{ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8080"}}}}},
				),
			},
			wantIPv4: []string{
				// TCP handled by Envoy catch-all chain via REDIRECT.
				"--to-port 23080",
				// UDP gets direct ACCEPT.
				"-A OUTPUT -m owner --uid-owner 1000 -p udp --dport 8080 -j ACCEPT",
			},
			notWantIPv4: []string{"-p sctp"},
		},
		"open SCTP port gets ACCEPT": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
					sandbox.EgressRule{
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "3868", Protocol: "SCTP"}}}},
					},
				),
			},
			wantIPv4: []string{
				"-A OUTPUT -m owner --uid-owner 1000 -p sctp --dport 3868 -j ACCEPT",
			},
		},
		"enableDefaultDeny with empty egress is unrestricted": {
			cfg: &sandbox.SandboxConfig{
				EnableDefaultDeny: sandbox.DefaultDenyConfig{Egress: boolPtr(true)},
				Egress:            egressRules(),
			},
			wantIPv4: []string{
				"-A OUTPUT -j ACCEPT",
			},
			notWantIPv4: []string{
				"REDIRECT", "DROP",
				"--uid-owner 999",
			},
		},
		"rules without default-deny gets ACCEPT": {
			cfg: &sandbox.SandboxConfig{
				EnableDefaultDeny: sandbox.DefaultDenyConfig{Egress: boolPtr(false)},
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			wantIPv4: []string{
				"REDIRECT",
				"-A OUTPUT -j ACCEPT",
			},
			notWantIPv4: []string{
				"-A OUTPUT -j DROP",
			},
		},
		"empty egress is unrestricted": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(),
			},
			wantIPv4: []string{
				"-A OUTPUT -j ACCEPT",
			},
			notWantIPv4: []string{
				"REDIRECT", "DROP",
				"--uid-owner 999",
			},
		},
		"FQDN UDP port gets ipset ACCEPT": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
				}),
			},
			wantIPv4: []string{
				"-A OUTPUT -m owner --uid-owner 1000 -p udp --dport 443 -m set --match-set sandbox_fqdn4 dst -j ACCEPT",
			},
			wantIPv6: []string{
				"-A OUTPUT -m owner --uid-owner 1000 -p udp --dport 443 -m set --match-set sandbox_fqdn6 dst -j ACCEPT",
			},
			notWantIPv4: []string{
				"--to-port 15443",
			},
		},
		"FQDN SCTP port gets ipset ACCEPT": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "3868", Protocol: "SCTP"}}}},
				}),
			},
			wantIPv4: []string{
				"-A OUTPUT -m owner --uid-owner 1000 -p sctp --dport 3868 -m set --match-set sandbox_fqdn4 dst -j ACCEPT",
			},
			wantIPv6: []string{
				"-A OUTPUT -m owner --uid-owner 1000 -p sctp --dport 3868 -m set --match-set sandbox_fqdn6 dst -j ACCEPT",
			},
			notWantIPv4: []string{
				"REDIRECT",
			},
		},
		"FQDN ANY port gets redirect and ipset non-TCP ACCEPT": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			wantIPv4: []string{
				// TCP handled by Envoy via REDIRECT.
				"--to-port 15443",
				// UDP handled by ipset.
				"-A OUTPUT -m owner --uid-owner 1000 -p udp --dport 443 -m set --match-set sandbox_fqdn4 dst -j ACCEPT",
			},
			notWantIPv4: []string{"-p sctp"},
			wantIPv6: []string{
				"-A OUTPUT -m owner --uid-owner 1000 -p udp --dport 443 -m set --match-set sandbox_fqdn6 dst -j ACCEPT",
			},
			notWantIPv6:        []string{"-p sctp"},
			wantRedirectCount4: 1,
		},
		"FQDN non-TCP skipped when unrestricted open ports": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
					},
					sandbox.EgressRule{
						ToPorts: []sandbox.PortRule{{}},
					},
				),
			},
			wantIPv4: []string{
				"-A OUTPUT -m owner --uid-owner 1000 -j ACCEPT",
			},
			notWantIPv4: []string{
				"--match-set",
			},
		},
		"separate FQDN and CIDR rules": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
					sandbox.EgressRule{
						ToCIDR:  []string{"10.0.0.0/8"},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
				),
			},
			wantIPv4: []string{
				// NAT: CIDR gets RETURN (bypass Envoy).
				"-A OUTPUT -m owner --uid-owner 1000 -p tcp --dport 443 -d 10.0.0.0/8 -j RETURN",
				// NAT: FQDN port gets REDIRECT.
				"--to-port 15443",
				// FILTER: CIDR gets ACCEPT in per-rule chain.
				"-A CIDR_4_0 -m owner --uid-owner 1000 -p tcp --dport 443 -d 10.0.0.0/8 -j ACCEPT",
				"-A OUTPUT -j CIDR_4_0",
				// FILTER: Envoy UID gets ACCEPT.
				"-A OUTPUT -m owner --uid-owner 999 -j ACCEPT",
			},
			wantRedirectCount4: 1,
		},
		"cross-rule CIDR except does not block globally": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{ToCIDR: []string{"10.0.0.0/8"}},
					sandbox.EgressRule{ToCIDRSet: []sandbox.CIDRRule{{
						CIDR:   "10.0.0.0/8",
						Except: []string{"10.1.0.0/16"},
					}}},
				),
			},
			wantIPv4: []string{
				"-A CIDR_4_0 -m owner --uid-owner 1000 -d 10.0.0.0/8 -j ACCEPT",
				"-A CIDR_4_1 -m owner --uid-owner 1000 -d 10.1.0.0/16 -j RETURN",
				"-A CIDR_4_1 -m owner --uid-owner 1000 -d 10.0.0.0/8 -j ACCEPT",
			},
			notWantIPv4: []string{
				"-d 10.1.0.0/16 -j DROP",
			},
		},
		"per-rule chains for CIDR except": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{ToCIDR: []string{"10.0.0.0/8"}},
					sandbox.EgressRule{ToCIDRSet: []sandbox.CIDRRule{{
						CIDR:   "10.0.0.0/8",
						Except: []string{"10.1.0.0/16"},
					}}},
				),
			},
			wantIPv4: []string{
				"-N CIDR_4_0",
				"-N CIDR_4_1",
				"-A CIDR_4_0 -m owner --uid-owner 1000 -d 10.0.0.0/8 -j ACCEPT",
				"-A CIDR_4_1 -m owner --uid-owner 1000 -d 10.1.0.0/16 -j RETURN",
				"-A CIDR_4_1 -m owner --uid-owner 1000 -d 10.0.0.0/8 -j ACCEPT",
				"-A OUTPUT -j CIDR_4_0",
				"-A OUTPUT -j CIDR_4_1",
			},
			notWantIPv4: []string{
				"-d 10.1.0.0/16 -j DROP",
			},
		},
		"no except still uses per-rule chains": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{ToCIDR: []string{"10.0.0.0/8"}},
					sandbox.EgressRule{ToCIDR: []string{"192.168.0.0/16"}},
				),
			},
			wantIPv4: []string{
				"-N CIDR_4_0",
				"-N CIDR_4_1",
				"-A CIDR_4_0 -m owner --uid-owner 1000 -d 10.0.0.0/8 -j ACCEPT",
				"-A CIDR_4_1 -m owner --uid-owner 1000 -d 192.168.0.0/16 -j ACCEPT",
				"-A OUTPUT -j CIDR_4_0",
				"-A OUTPUT -j CIDR_4_1",
			},
		},
		"toCIDR and toCIDRSet in same rule share chain": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDR:    []string{"10.0.0.0/8"},
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "192.168.0.0/16", Except: []string{"192.168.1.0/24"}}},
				}),
			},
			wantIPv4: []string{
				"-N CIDR_4_0",
				"-A CIDR_4_0 -m owner --uid-owner 1000 -d 192.168.1.0/24 -j RETURN",
				"-A CIDR_4_0 -m owner --uid-owner 1000 -d 10.0.0.0/8 -j ACCEPT",
				"-A CIDR_4_0 -m owner --uid-owner 1000 -d 192.168.0.0/16 -j ACCEPT",
				"-A OUTPUT -j CIDR_4_0",
			},
			notWantIPv4: []string{
				"-N CIDR_4_1",
				"-d 192.168.1.0/24 -j DROP",
			},
		},
		"different port cross-rule CIDR except": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToCIDR:  []string{"10.0.0.0/8"},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
					sandbox.EgressRule{
						ToCIDRSet: []sandbox.CIDRRule{{
							CIDR:   "10.0.0.0/8",
							Except: []string{"10.1.0.0/16"},
						}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "80"}}}},
					},
				),
			},
			wantIPv4: []string{
				// Rule A: port 443 only, no excepts.
				"-A CIDR_4_0 -m owner --uid-owner 1000 -p tcp --dport 443 -d 10.0.0.0/8 -j ACCEPT",
				// Rule B: port 80, except scoped to port 80.
				"-A CIDR_4_1 -m owner --uid-owner 1000 -p tcp --dport 80 -d 10.1.0.0/16 -j RETURN",
				"-A CIDR_4_1 -m owner --uid-owner 1000 -p tcp --dport 80 -d 10.0.0.0/8 -j ACCEPT",
			},
			notWantIPv4: []string{
				// Except must NOT apply to port 443.
				"-p tcp --dport 443 -d 10.1.0.0/16 -j RETURN",
				"-d 10.1.0.0/16 -j DROP",
			},
		},
		"port-scoped cross-rule CIDR except": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToCIDR:  []string{"10.0.0.0/8"},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
					sandbox.EgressRule{
						ToCIDRSet: []sandbox.CIDRRule{{
							CIDR:   "10.0.0.0/8",
							Except: []string{"10.1.0.0/16"},
						}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
				),
			},
			wantIPv4: []string{
				"-A CIDR_4_1 -m owner --uid-owner 1000 -p tcp --dport 443 -d 10.1.0.0/16 -j RETURN",
				"-A CIDR_4_1 -m owner --uid-owner 1000 -p tcp --dport 443 -d 10.0.0.0/8 -j ACCEPT",
			},
			notWantIPv4: []string{
				"-d 10.1.0.0/16 -j DROP",
			},
		},
		"UDP open port range emits dport range": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8000", EndPort: 9000, Protocol: "UDP"}}}},
					},
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
				),
			},
			wantIPv4: []string{
				"-A OUTPUT -m owner --uid-owner 1000 -p udp --dport 8000:9000 -j ACCEPT",
			},
			notWantIPv4: []string{
				"--dport 8000 -j ACCEPT",
			},
			wantRedirectCount4: 1,
		},
		"TCP open port range bypasses Envoy with direct ACCEPT": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8000", EndPort: 9000, Protocol: "TCP"}}}},
					},
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
				),
			},
			wantIPv4: []string{
				"-A OUTPUT -m owner --uid-owner 1000 -p tcp --dport 8000:9000 -j ACCEPT",
			},
			notWantIPv4: []string{
				// Range should not get a NAT REDIRECT.
				"--to-port 23000",
			},
			wantRedirectCount4: 1,
		},
		"mixed single and range open port": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8080", Protocol: "TCP"}}}},
					},
					sandbox.EgressRule{
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8000", EndPort: 9000, Protocol: "UDP"}}}},
					},
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
				),
			},
			wantIPv4: []string{
				// Single TCP port gets REDIRECT (Envoy).
				"--to-port 23080",
				// UDP range gets direct ACCEPT.
				"-A OUTPUT -m owner --uid-owner 1000 -p udp --dport 8000:9000 -j ACCEPT",
			},
			wantRedirectCount4: 2,
		},
		"SCTP open port range emits dport range": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "5000", EndPort: 6000, Protocol: "SCTP"}}}},
					},
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
				),
			},
			wantIPv4: []string{
				"-A OUTPUT -m owner --uid-owner 1000 -p sctp --dport 5000:6000 -j ACCEPT",
			},
		},
		"endPort equal to port emits dport N:N": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8000", EndPort: 8000, Protocol: "TCP"}}}},
					},
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
				),
			},
			wantIPv4: []string{
				// endPort == port produces --dport N:N, valid iptables
				// syntax equivalent to --dport N. No special case.
				"-A OUTPUT -m owner --uid-owner 1000 -p tcp --dport 8000:8000 -j ACCEPT",
			},
			notWantIPv4: []string{
				// Should not get a NAT REDIRECT for the range port.
				"--to-port 23000",
			},
			wantRedirectCount4: 1,
		},
		"explicit SCTP CIDR still works": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "8.8.8.0/24"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "3868", Protocol: "SCTP"}}}},
				}),
			},
			wantIPv4: []string{
				"-A OUTPUT -m owner --uid-owner 1000 -p sctp --dport 3868 -d 8.8.8.0/24 -j RETURN",
				"-A CIDR_4_0 -m owner --uid-owner 1000 -p sctp --dport 3868 -d 8.8.8.0/24 -j ACCEPT",
			},
			notWantIPv4: []string{
				"-p tcp --dport 3868",
				"-p udp --dport 3868",
			},
		},
		"FQDN REDIRECT coexists with open-port TCP range ACCEPT": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8000"}}}},
					},
					sandbox.EgressRule{
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8000", EndPort: 9000, Protocol: "TCP"}}}},
					},
				),
			},
			wantIPv4: []string{
				// NAT REDIRECT for FQDN rule's port 8000 (Envoy inspection).
				"--to-port 23000",
				// Direct ACCEPT for the TCP range (bypasses Envoy).
				"-A OUTPUT -m owner --uid-owner 1000 -p tcp --dport 8000:9000 -j ACCEPT",
			},
			// FQDN port 8000 contributes one REDIRECT; the range does not.
			wantRedirectCount4: 1,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ipv4, ipv6 := sandbox.GenerateIptablesRules(tt.cfg)

			for _, s := range tt.wantIPv4 {
				assert.Contains(t, ipv4, s)
			}

			for _, s := range tt.notWantIPv4 {
				assert.NotContains(t, ipv4, s)
			}

			for _, s := range tt.wantIPv6 {
				assert.Contains(t, ipv6, s)
			}

			for _, s := range tt.notWantIPv6 {
				assert.NotContains(t, ipv6, s)
			}

			if tt.wantRedirectCount4 > 0 {
				assert.Equal(t, tt.wantRedirectCount4, strings.Count(ipv4, "REDIRECT"))
			}
		})
	}
}

func TestGenerateIptablesRulesNATOrder(t *testing.T) {
	t.Parallel()

	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(
			sandbox.EgressRule{ToCIDRSet: []sandbox.CIDRRule{{CIDR: "8.8.8.0/24"}}},
			sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
			},
		),
	}

	ipv4, _ := sandbox.GenerateIptablesRules(cfg)

	returnIdx := strings.Index(ipv4, "-d 8.8.8.0/24 -j RETURN")
	redirectIdx := strings.Index(ipv4, "-j REDIRECT")
	assert.Greater(t, redirectIdx, returnIdx,
		"CIDR RETURN must come before REDIRECT in NAT chain")
}

func TestGenerateIptablesRulesFilterOrder(t *testing.T) {
	t.Parallel()

	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToCIDRSet: []sandbox.CIDRRule{{
				CIDR:   "0.0.0.0/0",
				Except: []string{"10.0.0.0/8"},
			}},
		}),
	}

	ipv4, _ := sandbox.GenerateIptablesRules(cfg)

	// Within the per-rule chain, except RETURN comes before CIDR ACCEPT.
	returnIdx := strings.Index(ipv4, "-d 10.0.0.0/8 -j RETURN")
	acceptIdx := strings.Index(ipv4, "-d 0.0.0.0/0 -j ACCEPT")
	envoyIdx := strings.Index(ipv4, "--uid-owner 999 -j ACCEPT")

	assert.Greater(t, acceptIdx, returnIdx,
		"CIDR except RETURN must come before CIDR ACCEPT in per-rule chain")
	assert.Greater(t, envoyIdx, acceptIdx,
		"Envoy ACCEPT must come after per-rule chain ACCEPT")
}

func TestGenerateIptablesRulesUnrestrictedOpenPorts(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg         *sandbox.SandboxConfig
		wantIPv4    []string
		notWantIPv4 []string
	}{
		"unrestricted open ports": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToPorts: []sandbox.PortRule{{}},
				}),
			},
			wantIPv4: []string{
				"-A OUTPUT -m owner --uid-owner 1000 -j ACCEPT",
			},
			notWantIPv4: []string{
				"-d 10.0.0.0/8 -j DROP",
			},
		},
		"unrestricted open ports with FQDN": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
					sandbox.EgressRule{
						ToPorts: []sandbox.PortRule{{}},
					},
				),
			},
			wantIPv4: []string{
				// NAT REDIRECT for FQDN port still present.
				"--to-port 15443",
				// Broad ACCEPT for user UID in filter.
				"-A OUTPUT -m owner --uid-owner 1000 -j ACCEPT",
			},
		},
		"unrestricted open ports with CIDR except": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToCIDRSet: []sandbox.CIDRRule{{
							CIDR:   "0.0.0.0/0",
							Except: []string{"10.0.0.0/8"},
						}},
					},
					sandbox.EgressRule{
						ToPorts: []sandbox.PortRule{{}},
					},
				),
			},
			wantIPv4: []string{
				// NAT CIDR RETURN still present.
				"-A OUTPUT -m owner --uid-owner 1000 -d 0.0.0.0/0 -j RETURN",
				// Broad ACCEPT in filter.
				"-A OUTPUT -m owner --uid-owner 1000 -j ACCEPT",
			},
			notWantIPv4: []string{
				// No CIDR except DROP (subsumed by broad ACCEPT).
				"-d 10.0.0.0/8 -j DROP",
				// No CIDR ACCEPT (subsumed by broad ACCEPT).
				"-d 0.0.0.0/0 -j ACCEPT",
			},
		},
		"empty ports with CIDR": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "8.8.8.0/24"}},
					ToPorts:   []sandbox.PortRule{{}},
				}),
			},
			wantIPv4: []string{
				// Portless CIDR RETURN (no -p/--dport).
				"-A OUTPUT -m owner --uid-owner 1000 -d 8.8.8.0/24 -j RETURN",
				// Portless CIDR ACCEPT in per-rule chain.
				"-A CIDR_4_0 -m owner --uid-owner 1000 -d 8.8.8.0/24 -j ACCEPT",
			},
			notWantIPv4: []string{
				// Not unrestricted (has L3 selector), so no broad user ACCEPT.
				"-A OUTPUT -m owner --uid-owner 1000 -j ACCEPT",
			},
		},
		"port 0 open-port rule produces unrestricted ACCEPT": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "0"}}}},
				}),
			},
			wantIPv4: []string{
				"-A OUTPUT -m owner --uid-owner 1000 -j ACCEPT",
			},
			notWantIPv4: []string{
				"--dport 0",
			},
		},
		"port 0 CIDR rule produces portless iptables rules": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "10.0.0.0/8"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "0"}}}},
				}),
			},
			wantIPv4: []string{
				"-A OUTPUT -m owner --uid-owner 1000 -d 10.0.0.0/8 -j RETURN",
				"-A CIDR_4_0 -m owner --uid-owner 1000 -d 10.0.0.0/8 -j ACCEPT",
			},
			notWantIPv4: []string{
				"--dport 0",
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ipv4, _ := sandbox.GenerateIptablesRules(tt.cfg)

			for _, s := range tt.wantIPv4 {
				assert.Contains(t, ipv4, s)
			}

			for _, s := range tt.notWantIPv4 {
				assert.NotContains(t, ipv4, s)
			}
		})
	}
}
