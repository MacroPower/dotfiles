package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// DNSProxy is a filtering DNS proxy that forwards queries to dnsmasq
// and populates ipsets with TTL-aware timeouts from matching responses.
type DNSProxy struct {
	patterns []FQDNPattern
	upstream string
	logging  bool

	// Addr is the IPv4 address the proxy is listening on. Useful for
	// tests that bind to port 0 and need the kernel-assigned port.
	Addr string

	udp4 *dns.Server
	udp6 *dns.Server
	tcp4 *dns.Server
	tcp6 *dns.Server

	cancel context.CancelFunc
	ctx    context.Context
}

// StartDNSProxy starts the proxy on listenAddr (and optionally [::1]
// at the same port when ipv6Disabled is false). UDP queries are
// handled with pattern-filtered ipset population. TCP queries are
// proxied to upstream without ipset filtering (TCP DNS fallback is
// rare; the preceding UDP query already populated the ipset).
// Forwards all queries to upstream (dnsmasq on [DnsmasqProxyPort]).
// Blocks until ready.
//
// IPv6 listener: if ipv6Disabled is true, only IPv4 listeners are
// created. If ipv6Disabled is false and binding [::1] fails, startup
// returns an error (IPv6 bypass risk).
func StartDNSProxy(patterns []FQDNPattern, upstream string, listenAddr string, ipv6Disabled bool, logging bool) (*DNSProxy, error) {
	ctx, cancel := context.WithCancel(context.Background())

	p := &DNSProxy{
		patterns: patterns,
		upstream: upstream,
		logging:  logging,
		cancel:   cancel,
		ctx:      ctx,
	}

	// Collect servers and listeners for cleanup on error.
	type closer interface{ Close() error }

	var closers []closer

	cleanup := func() {
		cancel()

		for _, c := range closers {
			_ = c.Close()
		}
	}

	// UDP IPv4.
	udp4PC, err := net.ListenPacket("udp", listenAddr)
	if err != nil {
		cleanup()

		return nil, fmt.Errorf("listening UDP %s: %w", listenAddr, err)
	}

	closers = append(closers, udp4PC)

	// Resolve actual address (port may be 0).
	udpAddr := udp4PC.LocalAddr().(*net.UDPAddr)
	p.Addr = udpAddr.String()

	p.udp4 = &dns.Server{
		PacketConn: udp4PC,
		Handler:    dns.HandlerFunc(p.handleUDPQuery),
	}

	// TCP IPv4 on the same port.
	tcp4Ln, err := net.Listen("tcp", p.Addr)
	if err != nil {
		cleanup()

		return nil, fmt.Errorf("listening TCP %s: %w", p.Addr, err)
	}

	closers = append(closers, tcp4Ln)

	p.tcp4 = &dns.Server{
		Listener: tcp4Ln,
		Handler:  dns.HandlerFunc(p.handleTCPQuery),
	}

	// IPv6 listeners.
	if !ipv6Disabled {
		addr6 := fmt.Sprintf("[::1]:%d", udpAddr.Port)

		udp6PC, err := net.ListenPacket("udp", addr6)
		if err != nil {
			cleanup()

			return nil, fmt.Errorf("listening UDP %s: %w", addr6, err)
		}

		closers = append(closers, udp6PC)

		p.udp6 = &dns.Server{
			PacketConn: udp6PC,
			Handler:    dns.HandlerFunc(p.handleUDPQuery),
		}

		tcp6Ln, err := net.Listen("tcp", addr6)
		if err != nil {
			cleanup()

			return nil, fmt.Errorf("listening TCP %s: %w", addr6, err)
		}

		closers = append(closers, tcp6Ln)

		p.tcp6 = &dns.Server{
			Listener: tcp6Ln,
			Handler:  dns.HandlerFunc(p.handleTCPQuery),
		}
	}

	// Start all servers and wait for ready.
	var wg sync.WaitGroup

	for _, s := range []*dns.Server{p.udp4, p.tcp4, p.udp6, p.tcp6} {
		if s == nil {
			continue
		}

		wg.Add(1)

		s.NotifyStartedFunc = sync.OnceFunc(func() { wg.Done() })

		go func() { _ = s.ActivateAndServe() }()
	}

	wg.Wait()

	return p, nil
}

// Shutdown gracefully stops the proxy. In-flight queries are dropped
// (acceptable for a short-lived sandbox).
func (p *DNSProxy) Shutdown() error {
	p.cancel()

	var errs []error

	for _, s := range []*dns.Server{p.udp4, p.udp6, p.tcp4, p.tcp6} {
		if s != nil {
			if err := s.Shutdown(); err != nil {
				errs = append(errs, err)
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutting down DNS proxy: %v", errs)
	}

	return nil
}

// handleUDPQuery forwards the query to dnsmasq, filters matching
// responses through ipset population, and returns the response.
// ipset add completes before the response is delivered to the client.
func (p *DNSProxy) handleUDPQuery(w dns.ResponseWriter, r *dns.Msg) {
	client := &dns.Client{
		Net:     "udp",
		Timeout: 10 * time.Second,
	}

	resp, _, err := client.ExchangeContext(p.ctx, r, p.upstream)
	if err != nil {
		fail := new(dns.Msg)
		fail.SetRcode(r, dns.RcodeServerFailure)
		_ = w.WriteMsg(fail)

		return
	}

	if len(r.Question) > 0 {
		qname := strings.ToLower(r.Question[0].Name)

		if matchesFQDNPatterns(p.patterns, qname) {
			p.populateIPSets(qname, resp)
		}

		if p.logging {
			slog.Info("dns query",
				slog.String("name", qname),
				slog.Int("answers", len(resp.Answer)),
			)
		}
	}

	_ = w.WriteMsg(resp)
}

// handleTCPQuery proxies TCP DNS queries to upstream without ipset
// filtering. TCP DNS is triggered by truncated UDP responses; the
// preceding UDP query already populated the ipset.
func (p *DNSProxy) handleTCPQuery(w dns.ResponseWriter, r *dns.Msg) {
	client := &dns.Client{
		Net:     "tcp",
		Timeout: 10 * time.Second,
	}

	resp, _, err := client.ExchangeContext(p.ctx, r, p.upstream)
	if err != nil {
		fail := new(dns.Msg)
		fail.SetRcode(r, dns.RcodeServerFailure)
		_ = w.WriteMsg(fail)

		return
	}

	_ = w.WriteMsg(resp)
}

// matchesFQDNPatterns reports whether the FQDN-form query name
// matches any of the compiled patterns.
func matchesFQDNPatterns(patterns []FQDNPattern, qname string) bool {
	for _, pat := range patterns {
		if pat.Regex.MatchString(qname) {
			return true
		}
	}

	return false
}

// populateIPSets extracts A and AAAA records from the DNS response
// and batch-adds them to the appropriate ipsets using ipset restore.
// TTLs are clamped to a minimum of [minIPSetTTL].
func (p *DNSProxy) populateIPSets(qname string, resp *dns.Msg) {
	var commands strings.Builder

	for _, rr := range resp.Answer {
		switch a := rr.(type) {
		case *dns.A:
			ttl := max(int(a.Hdr.Ttl), minIPSetTTL)
			fmt.Fprintf(&commands, "add %s %s timeout %d\n", IPSetFQDN4, a.A.String(), ttl)
		case *dns.AAAA:
			ttl := max(int(a.Hdr.Ttl), minIPSetTTL)
			fmt.Fprintf(&commands, "add %s %s timeout %d\n", IPSetFQDN6, a.AAAA.String(), ttl)
		}
	}

	if commands.Len() == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	//nolint:gosec // G204: command is a constant.
	cmd := exec.CommandContext(ctx, "ipset", "restore", "-exist")
	cmd.Stdin = strings.NewReader(commands.String())

	err := cmd.Run()
	if err != nil {
		slog.Debug("ipset restore",
			slog.String("qname", qname),
			slog.Any("err", err),
		)
	}
}
