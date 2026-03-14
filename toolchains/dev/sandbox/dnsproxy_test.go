package sandbox_test

import (
	"fmt"
	"net"
	"testing"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/toolchains/dev/sandbox"
)

func TestDNSDomainMatches(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		domain sandbox.DNSDomain
		qname  string
		want   bool
	}{
		"non-wildcard exact match": {
			domain: sandbox.DNSDomain{Name: "example.com"},
			qname:  "example.com.",
			want:   true,
		},
		"non-wildcard subdomain": {
			domain: sandbox.DNSDomain{Name: "example.com"},
			qname:  "sub.example.com.",
			want:   true,
		},
		"non-wildcard deep subdomain": {
			domain: sandbox.DNSDomain{Name: "example.com"},
			qname:  "a.b.c.example.com.",
			want:   true,
		},
		"non-wildcard suffix trap": {
			domain: sandbox.DNSDomain{Name: "example.com"},
			qname:  "notexample.com.",
			want:   false,
		},
		"non-wildcard unrelated domain": {
			domain: sandbox.DNSDomain{Name: "example.com"},
			qname:  "other.org.",
			want:   false,
		},
		"wildcard subdomain match": {
			domain: sandbox.DNSDomain{Name: "example.com", Wildcard: true},
			qname:  "sub.example.com.",
			want:   true,
		},
		"wildcard deep subdomain match": {
			domain: sandbox.DNSDomain{Name: "example.com", Wildcard: true},
			qname:  "a.b.example.com.",
			want:   true,
		},
		"wildcard rejects bare parent": {
			domain: sandbox.DNSDomain{Name: "example.com", Wildcard: true},
			qname:  "example.com.",
			want:   false,
		},
		"wildcard suffix trap": {
			domain: sandbox.DNSDomain{Name: "example.com", Wildcard: true},
			qname:  "notexample.com.",
			want:   false,
		},
		"empty qname": {
			domain: sandbox.DNSDomain{Name: "example.com"},
			qname:  "",
			want:   false,
		},
		"root dot only": {
			domain: sandbox.DNSDomain{Name: "example.com"},
			qname:  ".",
			want:   false,
		},
		"case insensitive": {
			domain: sandbox.DNSDomain{Name: "example.com"},
			qname:  "EXAMPLE.COM.",
			want:   true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.domain.Matches(tt.qname))
		})
	}
}

func TestCollectDNSDomains(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg  sandbox.SandboxConfig
		want []sandbox.DNSDomain
	}{
		"matchName produces non-wildcard": {
			cfg: sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "github.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			want: []sandbox.DNSDomain{{Name: "github.com"}},
		},
		"matchPattern produces wildcard": {
			cfg: sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			want: []sandbox.DNSDomain{{Name: "example.com", Wildcard: true}},
		},
		"double-star produces wildcard": {
			cfg: sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "**.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			want: []sandbox.DNSDomain{{Name: "example.com", Wildcard: true}},
		},
		"bare wildcard passthrough": {
			cfg: sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			want: []sandbox.DNSDomain{{Name: "*"}},
		},
		"matchName upgrades wildcard for same domain": {
			cfg: sandbox.SandboxConfig{
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
			want: []sandbox.DNSDomain{{Name: "example.com"}},
		},
		"TCPForward host upgrade": {
			cfg: sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
				TCPForwards: []sandbox.TCPForward{{Port: 22, Host: "example.com"}},
			},
			want: []sandbox.DNSDomain{{Name: "example.com"}},
		},
		"TCPForward adds new host": {
			cfg: sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "github.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
				TCPForwards: []sandbox.TCPForward{{Port: 22, Host: "git.example.com"}},
			},
			want: []sandbox.DNSDomain{
				{Name: "git.example.com"},
				{Name: "github.com"},
			},
		},
		"dedup same matchName": {
			cfg: sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "80"}}}},
					},
				),
			},
			want: []sandbox.DNSDomain{{Name: "example.com"}},
		},
		"sorted output": {
			cfg: sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{
						{MatchName: "z.example.com"},
						{MatchName: "a.example.com"},
					},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			want: []sandbox.DNSDomain{
				{Name: "a.example.com"},
				{Name: "z.example.com"},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := sandbox.CollectDNSDomains(&tt.cfg)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMatchFQDNPatterns(t *testing.T) {
	t.Parallel()

	cfg := sandbox.SandboxConfig{
		Egress: egressRules(
			sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{
					{MatchName: "exact.example.com"},
					{MatchPattern: "*.wild.example.com"},
					{MatchPattern: "**.deep.example.com"},
					{MatchPattern: "*"},
				},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
			},
		),
	}
	patterns := cfg.CompileFQDNPatterns()

	tests := map[string]struct {
		qname string
		want  bool
	}{
		"exact match": {
			qname: "exact.example.com.",
			want:  true,
		},
		"exact no match wrong name": {
			qname: "other.example.com.",
			want:  true, // matches bare wildcard "*"
		},
		"single-star matches one label": {
			qname: "sub.wild.example.com.",
			want:  true,
		},
		"single-star rejects multi-label": {
			qname: "a.b.wild.example.com.",
			want:  true, // matches bare wildcard "*"
		},
		"double-star matches one label": {
			qname: "sub.deep.example.com.",
			want:  true,
		},
		"double-star matches multi-label": {
			qname: "a.b.deep.example.com.",
			want:  true,
		},
		"bare wildcard matches anything": {
			qname: "anything.anywhere.com.",
			want:  true,
		},
		"bare wildcard matches root": {
			qname: ".",
			want:  true,
		},
		"empty string no match": {
			qname: "",
			want:  false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			matched := false
			for _, p := range patterns {
				if p.Regex.MatchString(tt.qname) {
					matched = true

					break
				}
			}

			assert.Equal(t, tt.want, matched)
		})
	}
}

// TestMatchFQDNPatternsWithoutBareWildcard tests pattern matching
// without a bare wildcard to verify single-star depth restriction.
func TestMatchFQDNPatternsWithoutBareWildcard(t *testing.T) {
	t.Parallel()

	cfg := sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{
				{MatchPattern: "*.example.com"},
				{MatchPattern: "**.deep.other.com"},
			},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
		}),
	}
	patterns := cfg.CompileFQDNPatterns()

	tests := map[string]struct {
		qname string
		want  bool
	}{
		"single-star matches one label": {
			qname: "sub.example.com.",
			want:  true,
		},
		"single-star rejects multi-label": {
			qname: "a.b.example.com.",
			want:  false,
		},
		"single-star rejects bare parent": {
			qname: "example.com.",
			want:  false,
		},
		"double-star matches one label": {
			qname: "sub.deep.other.com.",
			want:  true,
		},
		"double-star matches multi-label": {
			qname: "a.b.deep.other.com.",
			want:  true,
		},
		"double-star rejects bare parent": {
			qname: "deep.other.com.",
			want:  false,
		},
		"unrelated domain": {
			qname: "other.com.",
			want:  false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			matched := false
			for _, p := range patterns {
				if p.Regex.MatchString(tt.qname) {
					matched = true

					break
				}
			}

			assert.Equal(t, tt.want, matched)
		})
	}
}

// startMockDNS starts a mock DNS server that responds to queries with
// the given A record IP. Returns the server and its address.
func startMockDNS(t *testing.T, ip string) (*dns.Server, string) {
	t.Helper()

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)

	udpAddr := pc.LocalAddr().(*net.UDPAddr)

	tcpLn, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", udpAddr.Port))
	require.NoError(t, err)

	handler := dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		resp := new(dns.Msg)
		resp.SetReply(r)
		resp.Answer = append(resp.Answer, &dns.A{
			Hdr: dns.RR_Header{
				Name:   r.Question[0].Name,
				Rrtype: dns.TypeA,
				Class:  dns.ClassINET,
				Ttl:    300,
			},
			A: net.ParseIP(ip),
		})
		_ = w.WriteMsg(resp)
	})

	udpSrv := &dns.Server{
		PacketConn: pc,
		Handler:    handler,
	}

	tcpSrv := &dns.Server{
		Listener: tcpLn,
		Handler:  handler,
	}

	udpReady := make(chan struct{})
	tcpReady := make(chan struct{})

	udpSrv.NotifyStartedFunc = func() { close(udpReady) }
	tcpSrv.NotifyStartedFunc = func() { close(tcpReady) }

	go func() { _ = udpSrv.ActivateAndServe() }()
	go func() { _ = tcpSrv.ActivateAndServe() }()

	<-udpReady
	<-tcpReady

	t.Cleanup(func() {
		_ = udpSrv.Shutdown()
		_ = tcpSrv.Shutdown()
	})

	return udpSrv, fmt.Sprintf("127.0.0.1:%d", udpAddr.Port)
}

func TestStartDNSProxy(t *testing.T) {
	t.Parallel()

	_, upstream := startMockDNS(t, "1.2.3.4")

	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchName: "match.example.com"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
		}),
	}

	proxy, err := sandbox.StartDNSProxy(cfg, upstream, "127.0.0.1:0", true)
	require.NoError(t, err)

	t.Cleanup(func() { _ = proxy.Shutdown() })

	// Query a matching domain through the proxy.
	client := &dns.Client{Net: "udp"}
	msg := new(dns.Msg)
	msg.SetQuestion("match.example.com.", dns.TypeA)

	resp, _, err := client.Exchange(msg, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)
	require.Len(t, resp.Answer, 1)

	a, ok := resp.Answer[0].(*dns.A)
	require.True(t, ok)
	assert.Equal(t, "1.2.3.4", a.A.String())

	// Query a non-matching domain -- should get REFUSED in restricted mode.
	msg2 := new(dns.Msg)
	msg2.SetQuestion("nomatch.example.com.", dns.TypeA)

	resp2, _, err := client.Exchange(msg2, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp2)
	assert.Equal(t, dns.RcodeRefused, resp2.Rcode)
}

func TestDNSProxyTCPPassthrough(t *testing.T) {
	t.Parallel()

	_, upstream := startMockDNS(t, "5.6.7.8")

	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchName: "tcp.example.com"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
		}),
	}

	proxy, err := sandbox.StartDNSProxy(cfg, upstream, "127.0.0.1:0", true)
	require.NoError(t, err)

	t.Cleanup(func() { _ = proxy.Shutdown() })

	// TCP query for allowed domain should succeed.
	client := &dns.Client{Net: "tcp"}
	msg := new(dns.Msg)
	msg.SetQuestion("tcp.example.com.", dns.TypeA)

	resp, _, err := client.Exchange(msg, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)
	require.Len(t, resp.Answer, 1)

	a, ok := resp.Answer[0].(*dns.A)
	require.True(t, ok)
	assert.Equal(t, "5.6.7.8", a.A.String())
}

func TestDNSProxyBlockedMode(t *testing.T) {
	t.Parallel()

	_, upstream := startMockDNS(t, "1.2.3.4")

	// Blocked config: egress: [{}] with default deny.
	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{}),
	}

	proxy, err := sandbox.StartDNSProxy(cfg, upstream, "127.0.0.1:0", true)
	require.NoError(t, err)

	t.Cleanup(func() { _ = proxy.Shutdown() })

	// All queries should get REFUSED.
	client := &dns.Client{Net: "udp"}
	msg := new(dns.Msg)
	msg.SetQuestion("anything.example.com.", dns.TypeA)

	resp, _, err := client.Exchange(msg, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, dns.RcodeRefused, resp.Rcode)
	assert.Empty(t, resp.Answer)
}

func TestDNSProxyRestrictedMode(t *testing.T) {
	t.Parallel()

	_, upstream := startMockDNS(t, "10.0.0.1")

	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchName: "allowed.example.com"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
		}),
	}

	proxy, err := sandbox.StartDNSProxy(cfg, upstream, "127.0.0.1:0", true)
	require.NoError(t, err)

	t.Cleanup(func() { _ = proxy.Shutdown() })

	client := &dns.Client{Net: "udp"}

	// Allowed domain should succeed.
	msg := new(dns.Msg)
	msg.SetQuestion("allowed.example.com.", dns.TypeA)

	resp, _, err := client.Exchange(msg, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)
	require.Len(t, resp.Answer, 1)

	// Subdomain of allowed domain should also succeed.
	msg2 := new(dns.Msg)
	msg2.SetQuestion("sub.allowed.example.com.", dns.TypeA)

	resp2, _, err := client.Exchange(msg2, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp2)
	assert.Equal(t, dns.RcodeSuccess, resp2.Rcode)

	// Disallowed domain should get REFUSED.
	msg3 := new(dns.Msg)
	msg3.SetQuestion("blocked.example.com.", dns.TypeA)

	resp3, _, err := client.Exchange(msg3, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp3)
	assert.Equal(t, dns.RcodeRefused, resp3.Rcode)
}

func TestDNSProxyRestrictedWildcard(t *testing.T) {
	t.Parallel()

	_, upstream := startMockDNS(t, "10.0.0.2")

	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*.example.com"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
		}),
	}

	proxy, err := sandbox.StartDNSProxy(cfg, upstream, "127.0.0.1:0", true)
	require.NoError(t, err)

	t.Cleanup(func() { _ = proxy.Shutdown() })

	client := &dns.Client{Net: "udp"}

	// Subdomain should succeed.
	msg := new(dns.Msg)
	msg.SetQuestion("sub.example.com.", dns.TypeA)

	resp, _, err := client.Exchange(msg, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)

	// Bare parent should get REFUSED (wildcard excludes bare parent).
	msg2 := new(dns.Msg)
	msg2.SetQuestion("example.com.", dns.TypeA)

	resp2, _, err := client.Exchange(msg2, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp2)
	assert.Equal(t, dns.RcodeRefused, resp2.Rcode)
}

func TestDNSProxyBareWildcard(t *testing.T) {
	t.Parallel()

	_, upstream := startMockDNS(t, "10.0.0.3")

	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
		}),
	}

	proxy, err := sandbox.StartDNSProxy(cfg, upstream, "127.0.0.1:0", true)
	require.NoError(t, err)

	t.Cleanup(func() { _ = proxy.Shutdown() })

	// Bare wildcard should forward all queries.
	client := &dns.Client{Net: "udp"}
	msg := new(dns.Msg)
	msg.SetQuestion("anything.anywhere.com.", dns.TypeA)

	resp, _, err := client.Exchange(msg, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)
	require.Len(t, resp.Answer, 1)
}

func TestDNSProxyUnrestrictedMode(t *testing.T) {
	t.Parallel()

	_, upstream := startMockDNS(t, "10.0.0.4")

	// nil config -> unrestricted.
	proxy, err := sandbox.StartDNSProxy(nil, upstream, "127.0.0.1:0", true)
	require.NoError(t, err)

	t.Cleanup(func() { _ = proxy.Shutdown() })

	client := &dns.Client{Net: "udp"}
	msg := new(dns.Msg)
	msg.SetQuestion("anything.example.com.", dns.TypeA)

	resp, _, err := client.Exchange(msg, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)
	require.Len(t, resp.Answer, 1)
}

func TestDNSProxyRulesOnlyMode(t *testing.T) {
	t.Parallel()

	_, upstream := startMockDNS(t, "10.0.0.5")

	// Rules-only: EnableDefaultDeny.Egress = false, so all queries forward.
	cfg := &sandbox.SandboxConfig{
		EnableDefaultDeny: sandbox.DefaultDenyConfig{Egress: boolPtr(false)},
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
		}),
	}

	proxy, err := sandbox.StartDNSProxy(cfg, upstream, "127.0.0.1:0", true)
	require.NoError(t, err)

	t.Cleanup(func() { _ = proxy.Shutdown() })

	// Non-matching domain should still be forwarded (rules-only mode).
	client := &dns.Client{Net: "udp"}
	msg := new(dns.Msg)
	msg.SetQuestion("other.domain.com.", dns.TypeA)

	resp, _, err := client.Exchange(msg, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)
	require.Len(t, resp.Answer, 1)
}

func TestDNSProxyTCPForwardHosts(t *testing.T) {
	t.Parallel()

	_, upstream := startMockDNS(t, "10.0.0.6")

	// Restricted mode with a TCPForward host that should be resolvable.
	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchName: "github.com"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
		}),
		TCPForwards: []sandbox.TCPForward{{Port: 22, Host: "git.example.com"}},
	}

	proxy, err := sandbox.StartDNSProxy(cfg, upstream, "127.0.0.1:0", true)
	require.NoError(t, err)

	t.Cleanup(func() { _ = proxy.Shutdown() })

	client := &dns.Client{Net: "udp"}

	// FQDN domain should resolve.
	msg := new(dns.Msg)
	msg.SetQuestion("github.com.", dns.TypeA)

	resp, _, err := client.Exchange(msg, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)

	// TCPForward host should also resolve.
	msg2 := new(dns.Msg)
	msg2.SetQuestion("git.example.com.", dns.TypeA)

	resp2, _, err := client.Exchange(msg2, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp2)
	assert.Equal(t, dns.RcodeSuccess, resp2.Rcode)

	// Unrelated domain should get REFUSED.
	msg3 := new(dns.Msg)
	msg3.SetQuestion("blocked.org.", dns.TypeA)

	resp3, _, err := client.Exchange(msg3, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp3)
	assert.Equal(t, dns.RcodeRefused, resp3.Rcode)
}
