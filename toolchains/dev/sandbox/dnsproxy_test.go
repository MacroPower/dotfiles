package sandbox_test

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

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
			want:   false,
		},
		"non-wildcard deep subdomain": {
			domain: sandbox.DNSDomain{Name: "example.com"},
			qname:  "a.b.c.example.com.",
			want:   false,
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
		"wildcard rejects deep subdomain": {
			domain: sandbox.DNSDomain{Name: "example.com", Wildcard: true},
			qname:  "a.b.example.com.",
			want:   false,
		},
		"multi-level wildcard deep subdomain": {
			domain: sandbox.DNSDomain{Name: "example.com", Wildcard: true, MultiLevel: true},
			qname:  "a.b.example.com.",
			want:   true,
		},
		"multi-level wildcard single subdomain": {
			domain: sandbox.DNSDomain{Name: "example.com", Wildcard: true, MultiLevel: true},
			qname:  "sub.example.com.",
			want:   true,
		},
		"multi-level wildcard rejects bare parent": {
			domain: sandbox.DNSDomain{Name: "example.com", Wildcard: true, MultiLevel: true},
			qname:  "example.com.",
			want:   false,
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
		"double-star produces multi-level wildcard": {
			cfg: sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "**.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			want: []sandbox.DNSDomain{{Name: "example.com", Wildcard: true, MultiLevel: true}},
		},
		"multi-level upgrades single-level for same domain": {
			cfg: sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*.example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "**.example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
				),
			},
			want: []sandbox.DNSDomain{{Name: "example.com", Wildcard: true, MultiLevel: true}},
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
func startMockDNS(t *testing.T, ip string) string {
	t.Helper()

	lc := net.ListenConfig{}

	pc, err := lc.ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)

	udpAddr, ok := pc.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)

	tcpLn, err := lc.Listen(t.Context(), "tcp", fmt.Sprintf("127.0.0.1:%d", udpAddr.Port))
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
		assert.NoError(t, w.WriteMsg(resp))
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

	go func() {
		err := udpSrv.ActivateAndServe()
		if err != nil {
			slog.Debug("mock dns udp server exited", slog.Any("err", err))
		}
	}()

	go func() {
		err := tcpSrv.ActivateAndServe()
		if err != nil {
			slog.Debug("mock dns tcp server exited", slog.Any("err", err))
		}
	}()

	<-udpReady
	<-tcpReady

	t.Cleanup(func() {
		assert.NoError(t, udpSrv.Shutdown())
		assert.NoError(t, tcpSrv.Shutdown())
	})

	return fmt.Sprintf("127.0.0.1:%d", udpAddr.Port)
}

func TestStartDNSProxy(t *testing.T) {
	t.Parallel()

	upstream := startMockDNS(t, "1.2.3.4")

	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchName: "match.example.com"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
		}),
	}

	proxy, err := sandbox.StartDNSProxy(t.Context(), cfg, upstream, "127.0.0.1:0", true)
	require.NoError(t, err)

	t.Cleanup(func() { assert.NoError(t, proxy.Shutdown()) })

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

	upstream := startMockDNS(t, "5.6.7.8")

	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchName: "tcp.example.com"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
		}),
	}

	proxy, err := sandbox.StartDNSProxy(t.Context(), cfg, upstream, "127.0.0.1:0", true)
	require.NoError(t, err)

	t.Cleanup(func() { assert.NoError(t, proxy.Shutdown()) })

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

	upstream := startMockDNS(t, "1.2.3.4")

	// Blocked config: egress: [{}] with default deny.
	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{}),
	}

	proxy, err := sandbox.StartDNSProxy(t.Context(), cfg, upstream, "127.0.0.1:0", true)
	require.NoError(t, err)

	t.Cleanup(func() { assert.NoError(t, proxy.Shutdown()) })

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

	upstream := startMockDNS(t, "10.0.0.1")

	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchName: "allowed.example.com"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
		}),
	}

	proxy, err := sandbox.StartDNSProxy(t.Context(), cfg, upstream, "127.0.0.1:0", true)
	require.NoError(t, err)

	t.Cleanup(func() { assert.NoError(t, proxy.Shutdown()) })

	client := &dns.Client{Net: "udp"}

	// Allowed domain should succeed.
	msg := new(dns.Msg)
	msg.SetQuestion("allowed.example.com.", dns.TypeA)

	resp, _, err := client.Exchange(msg, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)
	require.Len(t, resp.Answer, 1)

	// Subdomain of allowed domain should get REFUSED (exact match only).
	msg2 := new(dns.Msg)
	msg2.SetQuestion("sub.allowed.example.com.", dns.TypeA)

	resp2, _, err := client.Exchange(msg2, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp2)
	assert.Equal(t, dns.RcodeRefused, resp2.Rcode)

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

	upstream := startMockDNS(t, "10.0.0.2")

	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*.example.com"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
		}),
	}

	proxy, err := sandbox.StartDNSProxy(t.Context(), cfg, upstream, "127.0.0.1:0", true)
	require.NoError(t, err)

	t.Cleanup(func() { assert.NoError(t, proxy.Shutdown()) })

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

	// Multi-label subdomain should get REFUSED (single-star depth).
	msg3 := new(dns.Msg)
	msg3.SetQuestion("a.b.example.com.", dns.TypeA)

	resp3, _, err := client.Exchange(msg3, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp3)
	assert.Equal(t, dns.RcodeRefused, resp3.Rcode)
}

func TestDNSProxyBareWildcard(t *testing.T) {
	t.Parallel()

	upstream := startMockDNS(t, "10.0.0.3")

	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
		}),
	}

	proxy, err := sandbox.StartDNSProxy(t.Context(), cfg, upstream, "127.0.0.1:0", true)
	require.NoError(t, err)

	t.Cleanup(func() { assert.NoError(t, proxy.Shutdown()) })

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

	upstream := startMockDNS(t, "10.0.0.4")

	// nil config -> unrestricted.
	proxy, err := sandbox.StartDNSProxy(t.Context(), nil, upstream, "127.0.0.1:0", true)
	require.NoError(t, err)

	t.Cleanup(func() { assert.NoError(t, proxy.Shutdown()) })

	client := &dns.Client{Net: "udp"}
	msg := new(dns.Msg)
	msg.SetQuestion("anything.example.com.", dns.TypeA)

	resp, _, err := client.Exchange(msg, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)
	require.Len(t, resp.Answer, 1)
}

func TestDNSProxyTCPPopulatesIPSet(t *testing.T) {
	t.Parallel()

	upstream := startMockDNS(t, "10.20.30.40")

	var (
		mu       sync.Mutex
		recorded []string
	)

	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchName: "tcp-ipset.example.com"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
		}),
	}

	proxy, err := sandbox.StartDNSProxy(t.Context(), cfg, upstream, "127.0.0.1:0", true,
		sandbox.WithIPSetFunc(func(_ context.Context, commands string) error {
			mu.Lock()
			defer mu.Unlock()

			recorded = append(recorded, commands)

			return nil
		}),
	)
	require.NoError(t, err)

	t.Cleanup(func() { assert.NoError(t, proxy.Shutdown()) })

	// TCP query should populate ipset.
	client := &dns.Client{Net: "tcp"}
	msg := new(dns.Msg)
	msg.SetQuestion("tcp-ipset.example.com.", dns.TypeA)

	resp, _, err := client.Exchange(msg, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)
	require.Len(t, resp.Answer, 1)

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, recorded, 1)
	assert.Contains(t, recorded[0], "add sandbox_fqdn4_0 10.20.30.40 timeout ")
}

func TestDNSProxyTruncatedUDPThenTCPRetry(t *testing.T) {
	t.Parallel()

	// Mock upstream that returns a truncated UDP response,
	// then a full TCP response with A records.
	lc := net.ListenConfig{}

	pc, err := lc.ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)

	udpAddr, ok := pc.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)

	tcpLn, err := lc.Listen(t.Context(), "tcp", fmt.Sprintf("127.0.0.1:%d", udpAddr.Port))
	require.NoError(t, err)

	// UDP handler: return truncated response with no answers.
	udpHandler := dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		resp := new(dns.Msg)
		resp.SetReply(r)

		resp.Truncated = true

		assert.NoError(t, w.WriteMsg(resp))
	})

	// TCP handler: return full response with A records.
	tcpHandler := dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		resp := new(dns.Msg)
		resp.SetReply(r)

		resp.Answer = append(resp.Answer,
			&dns.A{
				Hdr: dns.RR_Header{
					Name:   r.Question[0].Name,
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    300,
				},
				A: net.ParseIP("1.1.1.1"),
			},
			&dns.A{
				Hdr: dns.RR_Header{
					Name:   r.Question[0].Name,
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    300,
				},
				A: net.ParseIP("2.2.2.2"),
			},
		)
		assert.NoError(t, w.WriteMsg(resp))
	})

	udpSrv := &dns.Server{PacketConn: pc, Handler: udpHandler}
	tcpSrv := &dns.Server{Listener: tcpLn, Handler: tcpHandler}

	udpReady := make(chan struct{})
	tcpReady := make(chan struct{})
	udpSrv.NotifyStartedFunc = func() { close(udpReady) }
	tcpSrv.NotifyStartedFunc = func() { close(tcpReady) }

	go func() {
		err := udpSrv.ActivateAndServe()
		if err != nil {
			slog.Debug("mock dns udp server exited", slog.Any("err", err))
		}
	}()

	go func() {
		err := tcpSrv.ActivateAndServe()
		if err != nil {
			slog.Debug("mock dns tcp server exited", slog.Any("err", err))
		}
	}()

	<-udpReady
	<-tcpReady

	t.Cleanup(func() {
		assert.NoError(t, udpSrv.Shutdown())
		assert.NoError(t, tcpSrv.Shutdown())
	})

	upstreamAddr := fmt.Sprintf("127.0.0.1:%d", udpAddr.Port)

	var (
		mu       sync.Mutex
		recorded []string
	)

	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchName: "truncated.example.com"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
		}),
	}

	proxy, err := sandbox.StartDNSProxy(t.Context(), cfg, upstreamAddr, "127.0.0.1:0", true,
		sandbox.WithIPSetFunc(func(_ context.Context, commands string) error {
			mu.Lock()
			defer mu.Unlock()

			recorded = append(recorded, commands)

			return nil
		}),
	)
	require.NoError(t, err)

	t.Cleanup(func() { assert.NoError(t, proxy.Shutdown()) })

	// Step 1: UDP query gets truncated response (no IPs to populate).
	udpClient := &dns.Client{Net: "udp"}
	msg := new(dns.Msg)
	msg.SetQuestion("truncated.example.com.", dns.TypeA)

	resp, _, err := udpClient.Exchange(msg, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Truncated)

	mu.Lock()
	assert.Empty(t, recorded, "truncated UDP response should not populate ipset")
	mu.Unlock()

	// Step 2: TCP retry gets full response with A records.
	tcpClient := &dns.Client{Net: "tcp"}

	resp2, _, err := tcpClient.Exchange(msg, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp2)
	assert.Equal(t, dns.RcodeSuccess, resp2.Rcode)
	require.Len(t, resp2.Answer, 2)

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, recorded, 1, "TCP retry should populate ipset")
	assert.Contains(t, recorded[0], "add sandbox_fqdn4_0 1.1.1.1 timeout ")
	assert.Contains(t, recorded[0], "add sandbox_fqdn4_0 2.2.2.2 timeout ")
}

// startOversizedMockDNS starts a mock DNS server that returns many A
// records, producing a response that exceeds 512 bytes uncompressed.
func startOversizedMockDNS(t *testing.T) string {
	t.Helper()

	lc := net.ListenConfig{}

	pc, err := lc.ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)

	udpAddr, ok := pc.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)

	tcpLn, err := lc.Listen(t.Context(), "tcp", fmt.Sprintf("127.0.0.1:%d", udpAddr.Port))
	require.NoError(t, err)

	handler := dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		resp := new(dns.Msg)
		resp.SetReply(r)

		resp.Compress = true // Compress upstream so all records fit over UDP.

		// Add enough A records to exceed 512 bytes uncompressed.
		for i := range 20 {
			resp.Answer = append(resp.Answer, &dns.A{
				Hdr: dns.RR_Header{
					Name:   r.Question[0].Name,
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    300,
				},
				A: net.ParseIP(fmt.Sprintf("10.0.%d.%d", i/256, i%256)),
			})
		}

		assert.NoError(t, w.WriteMsg(resp))
	})

	udpSrv := &dns.Server{PacketConn: pc, Handler: handler}
	tcpSrv := &dns.Server{Listener: tcpLn, Handler: handler}

	udpReady := make(chan struct{})
	tcpReady := make(chan struct{})

	udpSrv.NotifyStartedFunc = func() { close(udpReady) }
	tcpSrv.NotifyStartedFunc = func() { close(tcpReady) }

	go func() {
		err := udpSrv.ActivateAndServe()
		if err != nil {
			slog.Debug("mock dns udp server exited", slog.Any("err", err))
		}
	}()

	go func() {
		err := tcpSrv.ActivateAndServe()
		if err != nil {
			slog.Debug("mock dns tcp server exited", slog.Any("err", err))
		}
	}()

	<-udpReady
	<-tcpReady

	t.Cleanup(func() {
		assert.NoError(t, udpSrv.Shutdown())
		assert.NoError(t, tcpSrv.Shutdown())
	})

	return fmt.Sprintf("127.0.0.1:%d", udpAddr.Port)
}

func TestDNSProxyUDPCompressionOversized(t *testing.T) {
	t.Parallel()

	upstream := startOversizedMockDNS(t)

	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchName: "compress.example.com"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
		}),
	}

	proxy, err := sandbox.StartDNSProxy(t.Context(), cfg, upstream, "127.0.0.1:0", true)
	require.NoError(t, err)

	t.Cleanup(func() { assert.NoError(t, proxy.Shutdown()) })

	// UDP query for a domain that returns an oversized response.
	// Without compression, this would exceed the 512-byte UDP limit.
	// The proxy should compress it so all 20 records arrive intact.
	client := &dns.Client{Net: "udp"}
	msg := new(dns.Msg)
	msg.SetQuestion("compress.example.com.", dns.TypeA)

	resp, _, err := client.Exchange(msg, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)
	assert.Len(t, resp.Answer, 20, "all 20 A records should arrive via compressed UDP")
}

func TestDNSProxyTCPNoCompression(t *testing.T) {
	t.Parallel()

	upstream := startOversizedMockDNS(t)

	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchName: "compress.example.com"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
		}),
	}

	proxy, err := sandbox.StartDNSProxy(t.Context(), cfg, upstream, "127.0.0.1:0", true)
	require.NoError(t, err)

	t.Cleanup(func() { assert.NoError(t, proxy.Shutdown()) })

	// TCP query should succeed with all records regardless of size.
	// Compression should NOT be applied for TCP (proto != "udp").
	client := &dns.Client{Net: "tcp"}
	msg := new(dns.Msg)
	msg.SetQuestion("compress.example.com.", dns.TypeA)

	resp, _, err := client.Exchange(msg, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)
	assert.Len(t, resp.Answer, 20, "all 20 A records should arrive via TCP")
	assert.False(t, resp.Compress, "TCP response should not have Compress flag set")
}

func TestDNSProxyTCPForwardHosts(t *testing.T) {
	t.Parallel()

	upstream := startMockDNS(t, "10.0.0.6")

	// Restricted mode with a TCPForward host that should be resolvable.
	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchName: "github.com"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
		}),
		TCPForwards: []sandbox.TCPForward{{Port: 22, Host: "git.example.com"}},
	}

	proxy, err := sandbox.StartDNSProxy(t.Context(), cfg, upstream, "127.0.0.1:0", true)
	require.NoError(t, err)

	t.Cleanup(func() { assert.NoError(t, proxy.Shutdown()) })

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

// startCNAMEMockDNS starts a mock DNS server that returns a CNAME
// chain followed by an A record. The CNAME has cnameTTL and the A
// record has aTTL.
func startCNAMEMockDNS(t *testing.T, cnameTTL, aTTL uint32) string {
	t.Helper()

	lc := net.ListenConfig{}

	pc, err := lc.ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)

	udpAddr, ok := pc.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)

	tcpLn, err := lc.Listen(t.Context(), "tcp", fmt.Sprintf("127.0.0.1:%d", udpAddr.Port))
	require.NoError(t, err)

	handler := dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		resp := new(dns.Msg)
		resp.SetReply(r)

		resp.Answer = append(resp.Answer,
			&dns.CNAME{
				Hdr: dns.RR_Header{
					Name:   r.Question[0].Name,
					Rrtype: dns.TypeCNAME,
					Class:  dns.ClassINET,
					Ttl:    cnameTTL,
				},
				Target: "target.cdn.example.com.",
			},
			&dns.A{
				Hdr: dns.RR_Header{
					Name:   "target.cdn.example.com.",
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    aTTL,
				},
				A: net.ParseIP("10.99.0.1"),
			},
		)
		assert.NoError(t, w.WriteMsg(resp))
	})

	udpSrv := &dns.Server{PacketConn: pc, Handler: handler}
	tcpSrv := &dns.Server{Listener: tcpLn, Handler: handler}

	udpReady := make(chan struct{})
	tcpReady := make(chan struct{})

	udpSrv.NotifyStartedFunc = func() { close(udpReady) }
	tcpSrv.NotifyStartedFunc = func() { close(tcpReady) }

	go func() {
		err := udpSrv.ActivateAndServe()
		if err != nil {
			slog.Debug("mock dns udp server exited", slog.Any("err", err))
		}
	}()

	go func() {
		err := tcpSrv.ActivateAndServe()
		if err != nil {
			slog.Debug("mock dns tcp server exited", slog.Any("err", err))
		}
	}()

	<-udpReady
	<-tcpReady

	t.Cleanup(func() {
		assert.NoError(t, udpSrv.Shutdown())
		assert.NoError(t, tcpSrv.Shutdown())
	})

	return fmt.Sprintf("127.0.0.1:%d", udpAddr.Port)
}

func TestDNSProxyCNAMEMinTTL(t *testing.T) {
	t.Parallel()

	// CNAME TTL=30, A TTL=300 -- ipset should use TTL=60
	// (30 is below minIPSetTTL=60).
	upstream := startCNAMEMockDNS(t, 30, 300)

	var (
		mu       sync.Mutex
		recorded []string
	)

	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchName: "cname.example.com"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
		}),
	}

	proxy, err := sandbox.StartDNSProxy(t.Context(), cfg, upstream, "127.0.0.1:0", true,
		sandbox.WithIPSetFunc(func(_ context.Context, commands string) error {
			mu.Lock()
			defer mu.Unlock()

			recorded = append(recorded, commands)

			return nil
		}),
	)
	require.NoError(t, err)

	t.Cleanup(func() { assert.NoError(t, proxy.Shutdown()) })

	client := &dns.Client{Net: "udp"}
	msg := new(dns.Msg)
	msg.SetQuestion("cname.example.com.", dns.TypeA)

	resp, _, err := client.Exchange(msg, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, recorded, 1)

	// The minimum TTL across CNAME(30) and A(300) is 30, clamped
	// to minIPSetTTL=60.
	assert.Contains(t, recorded[0], "timeout 60")
	assert.Contains(t, recorded[0], "10.99.0.1")
}

func TestDNSProxyCNAMEHigherTTLUsesATTL(t *testing.T) {
	t.Parallel()

	// CNAME TTL=300, A TTL=120 -- ipset should use TTL=120.
	upstream := startCNAMEMockDNS(t, 300, 120)

	var (
		mu       sync.Mutex
		recorded []string
	)

	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchName: "cname2.example.com"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
		}),
	}

	proxy, err := sandbox.StartDNSProxy(t.Context(), cfg, upstream, "127.0.0.1:0", true,
		sandbox.WithIPSetFunc(func(_ context.Context, commands string) error {
			mu.Lock()
			defer mu.Unlock()

			recorded = append(recorded, commands)

			return nil
		}),
	)
	require.NoError(t, err)

	t.Cleanup(func() { assert.NoError(t, proxy.Shutdown()) })

	client := &dns.Client{Net: "udp"}
	msg := new(dns.Msg)
	msg.SetQuestion("cname2.example.com.", dns.TypeA)

	resp, _, err := client.Exchange(msg, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, recorded, 1)

	// Minimum TTL is A(120), above minIPSetTTL so used directly.
	assert.Contains(t, recorded[0], "timeout 120")
	assert.Contains(t, recorded[0], "10.99.0.1")
}

func TestDNSProxyUpstreamTimeoutSilentDrop(t *testing.T) {
	t.Parallel()

	// Start a mock DNS server that never responds, causing upstream
	// timeout.
	lc := net.ListenConfig{}

	pc, err := lc.ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)

	udpAddr, ok := pc.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)

	t.Cleanup(func() { assert.NoError(t, pc.Close()) })

	upstreamAddr := fmt.Sprintf("127.0.0.1:%d", udpAddr.Port)

	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchName: "timeout.example.com"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
		}),
	}

	proxy, err := sandbox.StartDNSProxy(t.Context(), cfg, upstreamAddr, "127.0.0.1:0", true,
		sandbox.WithClientTimeout(100*time.Millisecond),
	)
	require.NoError(t, err)

	t.Cleanup(func() { assert.NoError(t, proxy.Shutdown()) })

	// Query with a short client timeout. The proxy should not send
	// a response (silent drop), so the client itself times out.
	client := &dns.Client{
		Net:     "udp",
		Timeout: 500 * time.Millisecond,
	}

	msg := new(dns.Msg)
	msg.SetQuestion("timeout.example.com.", dns.TypeA)

	resp, _, err := client.Exchange(msg, proxy.Addr)

	// The client should get a timeout error (no response from proxy).
	require.Error(t, err, "expected client timeout due to silent drop")
	assert.Nil(t, resp)
}

func TestDNSProxyUpstreamConnectionRefusedSERVFAIL(t *testing.T) {
	t.Parallel()

	// Find a port that is not listening to trigger connection refused.
	lc := net.ListenConfig{}

	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	addr := ln.Addr().String()
	require.NoError(t, ln.Close()) // Close immediately so the port is not listening.

	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchName: "refused.example.com"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
		}),
	}

	proxy, err := sandbox.StartDNSProxy(t.Context(), cfg, addr, "127.0.0.1:0", true,
		sandbox.WithClientTimeout(2*time.Second),
	)
	require.NoError(t, err)

	t.Cleanup(func() { assert.NoError(t, proxy.Shutdown()) })

	// Use TCP so connection-refused is reliably detected.
	client := &dns.Client{
		Net:     "tcp",
		Timeout: 5 * time.Second,
	}

	msg := new(dns.Msg)
	msg.SetQuestion("refused.example.com.", dns.TypeA)

	resp, _, err := client.Exchange(msg, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, dns.RcodeServerFailure, resp.Rcode, "non-timeout upstream error should produce SERVFAIL")
}
