package sandbox_test

import (
	"fmt"
	"testing"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/toolchains/dev/sandbox"
)

func TestStartRefuseDNS(t *testing.T) {
	t.Parallel()

	srv, err := sandbox.StartRefuseDNS()
	require.NoError(t, err)
	t.Cleanup(func() { _ = srv.Shutdown() })

	c := new(dns.Client)
	addr := fmt.Sprintf("127.0.0.1:%d", sandbox.RefusePort)

	// Verify REFUSED for multiple query types to ensure no state leakage.
	queries := map[string]struct {
		name  string
		qtype uint16
	}{
		"A record":    {name: "blocked.example.com.", qtype: dns.TypeA},
		"AAAA record": {name: "another.test.org.", qtype: dns.TypeAAAA},
		"MX record":   {name: "mx.example.net.", qtype: dns.TypeMX},
	}

	for name, q := range queries {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			m := new(dns.Msg)
			m.SetQuestion(q.name, q.qtype)

			r, _, err := c.Exchange(m, addr)
			require.NoError(t, err)
			assert.Equal(t, dns.RcodeRefused, r.Rcode)
		})
	}
}
