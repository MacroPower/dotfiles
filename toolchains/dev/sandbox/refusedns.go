package sandbox

import (
	"fmt"
	"net"

	"github.com/miekg/dns"
)

// RefusePort is the port the refuse DNS server listens on.
// Dnsmasq forwards unmatched queries here instead of using address=/#/
// so that denied domains receive REFUSED (matching Cilium's default
// --tofqdns-dns-reject-response-code=refused) rather than NXDOMAIN.
const RefusePort = 5553

// StartRefuseDNS starts a DNS server on localhost:RefusePort that
// responds REFUSED to all queries. It blocks until the server is
// ready to accept connections.
func StartRefuseDNS() (*dns.Server, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", RefusePort)

	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", addr, err)
	}

	srv := &dns.Server{
		PacketConn: pc,
		Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
			resp := new(dns.Msg)
			resp.SetRcode(r, dns.RcodeRefused)
			_ = w.WriteMsg(resp)
		}),
	}

	ready := make(chan struct{})
	srv.NotifyStartedFunc = func() { close(ready) }

	go func() { _ = srv.ActivateAndServe() }()
	<-ready

	return srv, nil
}
