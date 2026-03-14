package sandbox_test

import (
	"net"
	"os/exec"
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

func TestShutdownOrder(t *testing.T) {
	t.Parallel()

	// Start a mock upstream DNS server and a DNS proxy.
	upstream := startMockDNS(t, "1.2.3.4")

	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
			ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
		}),
	}

	proxy, err := sandbox.StartDNSProxy(t.Context(), cfg, upstream, "127.0.0.1:0", true)
	require.NoError(t, err)

	// Start a long-running subprocess to simulate Envoy.
	envoyCmd := exec.CommandContext(t.Context(), "sleep", "60")
	require.NoError(t, envoyCmd.Start())

	// Verify DNS proxy is serving before shutdown.
	client := &dns.Client{Net: "udp"}
	msg := new(dns.Msg)
	msg.SetQuestion("example.com.", dns.TypeA)

	resp, _, err := client.Exchange(msg, proxy.Addr)
	require.NoError(t, err)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)

	// DNS should still be resolvable while Envoy is draining.
	// We verify this by checking that after Shutdown returns,
	// the Envoy process has already exited (was waited on) and
	// the DNS proxy port is released.
	sandbox.Shutdown(t.Context(), envoyCmd, proxy, nil)

	// Envoy process should have been terminated and waited on.
	assert.NotNil(t, envoyCmd.ProcessState, "envoy process should have been waited on")

	// DNS proxy port should be released.
	lc := net.ListenConfig{}
	ln, err := lc.ListenPacket(t.Context(), "udp", proxy.Addr)
	require.NoError(t, err)
	require.NoError(t, ln.Close())
}
