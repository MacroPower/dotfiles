package sandbox_test

import (
	"net"
	"testing"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/toolchains/dev/sandbox"
)

func TestParseUpstreamDNS(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		resolvConf string
		want       string
	}{
		"standard": {
			resolvConf: "nameserver 8.8.8.8\nnameserver 8.8.4.4\n",
			want:       "8.8.8.8",
		},
		"multiple nameservers": {
			resolvConf: "search example.com\nnameserver 1.1.1.1\nnameserver 8.8.8.8\n",
			want:       "1.1.1.1",
		},
		"ipv6": {
			resolvConf: "nameserver ::1\nnameserver 8.8.8.8\n",
			want:       "::1",
		},
		"empty": {
			resolvConf: "search example.com\n",
		},
		"comments and whitespace": {
			resolvConf: "# comment\n  nameserver  10.0.0.1  \n",
			want:       "10.0.0.1",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, sandbox.ParseUpstreamDNS(tt.resolvConf))
		})
	}
}

func TestDNSProxyShutdownOnInitFailure(t *testing.T) {
	t.Parallel()

	// Start a mock upstream DNS server.
	upstream := startMockDNS(t, "1.2.3.4")

	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
		}),
	}

	// Start the DNS proxy (simulates the Init step that succeeds).
	proxy, err := sandbox.StartDNSProxy(t.Context(), cfg, upstream, "127.0.0.1:0", true)
	require.NoError(t, err)

	// Verify the proxy is serving queries.
	client := &dns.Client{Net: "udp"}
	msg := new(dns.Msg)
	msg.SetQuestion("example.com.", dns.TypeA)

	resp, _, err := client.Exchange(msg, proxy.Addr)
	require.NoError(t, err)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)

	// Simulate Init failure cleanup: shut down the DNS proxy (what
	// the defer in Init does on error return).
	require.NoError(t, proxy.Shutdown())

	// Verify the port is released by binding the same address.
	lc := net.ListenConfig{}
	ln, err := lc.ListenPacket(t.Context(), "udp", proxy.Addr)
	require.NoError(t, err)
	require.NoError(t, ln.Close())
}
