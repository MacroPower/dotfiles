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

	cfg := sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchName: "match.example.com"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
		}),
	}
	patterns := cfg.CompileFQDNPatterns()

	proxy, err := sandbox.StartDNSProxy(patterns, upstream, "127.0.0.1:0", true, false)
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

	// Query a non-matching domain -- should still get forwarded response.
	msg2 := new(dns.Msg)
	msg2.SetQuestion("nomatch.example.com.", dns.TypeA)

	resp2, _, err := client.Exchange(msg2, proxy.Addr)
	require.NoError(t, err)
	require.NotNil(t, resp2)
	assert.Equal(t, dns.RcodeSuccess, resp2.Rcode)
	require.Len(t, resp2.Answer, 1)
}

func TestDNSProxyTCPPassthrough(t *testing.T) {
	t.Parallel()

	_, upstream := startMockDNS(t, "5.6.7.8")

	cfg := sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchName: "tcp.example.com"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
		}),
	}
	patterns := cfg.CompileFQDNPatterns()

	proxy, err := sandbox.StartDNSProxy(patterns, upstream, "127.0.0.1:0", true, false)
	require.NoError(t, err)

	t.Cleanup(func() { _ = proxy.Shutdown() })

	// TCP query should be forwarded through.
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
