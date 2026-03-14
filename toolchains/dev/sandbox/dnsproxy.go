package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// dnsMode determines how the DNS proxy handles queries.
type dnsMode int

const (
	// dnsModeForwardAll forwards all queries to the upstream resolver.
	// Used for unrestricted and bare wildcard configs.
	dnsModeForwardAll dnsMode = iota

	// dnsModeRefuseAll returns REFUSED for all queries without
	// contacting upstream. Used for blocked configs (egress: [{}]).
	dnsModeRefuseAll

	// dnsModeAllowlist forwards queries matching the allowed domain
	// list and returns REFUSED for everything else.
	dnsModeAllowlist
)

// DNSDomain is an allowed domain entry for DNS filtering. Wildcard
// entries (from matchPattern "*.example.com" or "**.example.com")
// match subdomains only; exact entries match the domain itself and
// all subdomains.
type DNSDomain struct {
	// Name is the domain without any wildcard prefix.
	Name string
	// Wildcard is true when the entry originated from a matchPattern
	// with a leading wildcard prefix ("*." or "**."), restricting
	// matches to subdomains only (excluding the bare parent domain).
	Wildcard bool
	// MultiLevel is true for "**." patterns, allowing matches at
	// arbitrary subdomain depth. When false (single-star "*."
	// pattern), only one label before the suffix is allowed. This
	// mirrors Cilium's depth restriction for single-star wildcards.
	MultiLevel bool
}

// Matches reports whether qname (in FQDN wire format with trailing
// dot) matches this domain entry. Non-wildcard entries match the
// domain and all subdomains (like dnsmasq /domain/). Wildcard entries
// match subdomains only, not the bare parent (like dnsmasq /*.domain/).
// The leading-dot check prevents false positives (notexample.com vs
// example.com).
func (d DNSDomain) Matches(qname string) bool {
	q := strings.TrimSuffix(qname, ".")
	if q == "" {
		return false
	}

	q = strings.ToLower(q)

	if d.Wildcard {
		suffix := "." + d.Name
		if !strings.HasSuffix(q, suffix) {
			return false
		}

		if !d.MultiLevel {
			// Single-star: exactly one label before the suffix.
			prefix := q[:len(q)-len(suffix)]

			return !strings.Contains(prefix, ".")
		}

		return true
	}

	return q == d.Name
}

// CollectDNSDomains returns a sorted, deduplicated list of domains
// that should be forwarded in restricted mode. Includes FQDN domains
// (preserving wildcard vs exact distinction for correct filtering)
// and [TCPForward] hosts. The bare wildcard "*" pattern is included
// as-is for the caller to handle.
func CollectDNSDomains(cfg *SandboxConfig) []DNSDomain {
	seen := make(map[string]bool)
	var result []DNSDomain

	for _, rule := range cfg.EgressRules() {
		for _, fqdn := range rule.ToFQDNs {
			var d DNSDomain

			if fqdn.MatchName != "" {
				d = DNSDomain{Name: fqdn.MatchName}
			} else {
				// Detect multi-level ("**.") before stripping.
				multiLevel := strings.HasPrefix(fqdn.MatchPattern, "**.")

				// Strip all leading "*" characters then the
				// following "." to extract the base domain.
				stripped := strings.TrimLeft(fqdn.MatchPattern, "*")
				stripped = strings.TrimPrefix(stripped, ".")
				if stripped == "" {
					// Bare wildcard "*", "**", etc.: pass
					// through for catch-all handling.
					if !seen["*"] {
						seen["*"] = true
						result = append(result, DNSDomain{Name: "*"})
					}

					continue
				}

				d = DNSDomain{Name: stripped, Wildcard: true, MultiLevel: multiLevel}
			}

			if seen[d.Name] {
				// If previously seen as a wildcard, and this is
				// an exact matchName for the same domain, upgrade
				// to non-wildcard so the bare domain also resolves.
				if !d.Wildcard {
					for i := range result {
						if result[i].Name == d.Name && result[i].Wildcard {
							result[i].Wildcard = false
							break
						}
					}
				}

				// If previously seen as single-level wildcard,
				// and this is multi-level for the same domain,
				// upgrade to multi-level (superset).
				if d.Wildcard && d.MultiLevel {
					for i := range result {
						if result[i].Name == d.Name && result[i].Wildcard && !result[i].MultiLevel {
							result[i].MultiLevel = true
							break
						}
					}
				}

				continue
			}

			seen[d.Name] = true
			result = append(result, d)
		}
	}

	for _, host := range cfg.TCPForwardHosts() {
		if seen[host] {
			// TCPForward hosts need the bare domain to resolve.
			// If a wildcard FQDN entry exists for the same
			// domain, upgrade to non-wildcard so both the bare
			// domain and subdomains resolve.
			for i := range result {
				if result[i].Name == host && result[i].Wildcard {
					result[i].Wildcard = false
					break
				}
			}

			continue
		}

		seen[host] = true
		result = append(result, DNSDomain{Name: host})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// DNSProxy is a filtering DNS proxy that handles domain-level
// filtering and ipset population. It forwards allowed queries to the
// real upstream resolver and returns REFUSED for blocked domains,
// replacing the previous dnsmasq + RefuseDNS two-hop chain with a
// single process.
type DNSProxy struct {
	mode     dnsMode
	domains  []DNSDomain
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

// StartDNSProxy starts the DNS proxy on listenAddr (and optionally
// [::1] at the same port when ipv6Disabled is false). The proxy
// determines its filtering mode from cfg:
//
//   - nil/unrestricted/bare-wildcard: forward all queries
//   - blocked (egress: [{}]): return REFUSED for all queries
//   - restricted with specific domains: forward allowed, refuse others
//
// When cfg has FQDN rules with non-TCP ports, UDP responses matching
// the compiled patterns populate ipsets with TTL-aware timeouts.
// TCP queries are proxied without ipset filtering (TCP DNS fallback
// is rare; the preceding UDP query already populated the ipset).
// Blocks until ready.
//
// IPv6 listener: if ipv6Disabled is true, only IPv4 listeners are
// created. If ipv6Disabled is false and binding [::1] fails, startup
// returns an error (IPv6 bypass risk).
func StartDNSProxy(cfg *SandboxConfig, upstream, listenAddr string, ipv6Disabled bool) (*DNSProxy, error) {
	ctx, cancel := context.WithCancel(context.Background())

	p := &DNSProxy{
		upstream: upstream,
		cancel:   cancel,
		ctx:      ctx,
	}

	// Determine filtering mode.
	switch {
	case cfg == nil || cfg.IsEgressUnrestricted():
		p.mode = dnsModeForwardAll
	case cfg.IsEgressBlocked():
		p.mode = dnsModeRefuseAll
	default:
		domains := CollectDNSDomains(cfg)
		if slices.ContainsFunc(domains, func(d DNSDomain) bool {
			return d.Name == "*"
		}) {
			p.mode = dnsModeForwardAll
		} else {
			p.mode = dnsModeAllowlist
			p.domains = domains
		}
	}

	// Compile FQDN patterns for ipset population (non-TCP ports only).
	if cfg != nil && cfg.HasFQDNNonTCPPorts() {
		p.patterns = cfg.CompileFQDNPatterns()
	}

	if cfg != nil {
		p.logging = cfg.Logging
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

// handleUDPQuery handles UDP DNS queries with mode-aware filtering
// and ipset population for matching responses.
func (p *DNSProxy) handleUDPQuery(w dns.ResponseWriter, r *dns.Msg) {
	p.handleQuery(w, r, "udp")
}

// handleTCPQuery handles TCP DNS queries with mode-aware filtering.
// ipset population is skipped for TCP (the preceding UDP query
// already populated the ipset).
func (p *DNSProxy) handleTCPQuery(w dns.ResponseWriter, r *dns.Msg) {
	p.handleQuery(w, r, "tcp")
}

// handleQuery is the unified query handler for both UDP and TCP.
// It applies mode-based filtering, forwards allowed queries to
// upstream, populates ipsets (UDP only), and logs when enabled.
func (p *DNSProxy) handleQuery(w dns.ResponseWriter, r *dns.Msg, proto string) {
	if len(r.Question) == 0 {
		fail := new(dns.Msg)
		fail.SetRcode(r, dns.RcodeServerFailure)
		_ = w.WriteMsg(fail)

		return
	}

	qname := strings.ToLower(r.Question[0].Name)

	// Blocked mode: refuse everything without contacting upstream.
	if p.mode == dnsModeRefuseAll {
		resp := new(dns.Msg)
		resp.SetRcode(r, dns.RcodeRefused)

		if p.logging {
			slog.Info("dns query refused",
				slog.String("name", qname),
			)
		}

		_ = w.WriteMsg(resp)

		return
	}

	// Allowlist mode: refuse queries that don't match any allowed domain.
	if p.mode == dnsModeAllowlist && !p.domainAllowed(qname) {
		resp := new(dns.Msg)
		resp.SetRcode(r, dns.RcodeRefused)

		if p.logging {
			slog.Info("dns query refused",
				slog.String("name", qname),
			)
		}

		_ = w.WriteMsg(resp)

		return
	}

	// Forward to upstream.
	client := &dns.Client{
		Net:     proto,
		Timeout: 10 * time.Second,
	}

	resp, _, err := client.ExchangeContext(p.ctx, r, p.upstream)
	if err != nil {
		fail := new(dns.Msg)
		fail.SetRcode(r, dns.RcodeServerFailure)
		_ = w.WriteMsg(fail)

		return
	}

	// ipset population (UDP only, when patterns exist).
	if proto == "udp" && resp.Rcode == dns.RcodeSuccess {
		if indices := matchingFQDNRuleIndices(p.patterns, qname); len(indices) > 0 {
			p.populateIPSets(qname, resp, indices)
		}
	}

	if p.logging {
		slog.Info("dns query",
			slog.String("name", qname),
			slog.Int("answers", len(resp.Answer)),
		)
	}

	_ = w.WriteMsg(resp)
}

// domainAllowed reports whether qname matches any domain in the
// allowlist.
func (p *DNSProxy) domainAllowed(qname string) bool {
	for _, d := range p.domains {
		if d.Matches(qname) {
			return true
		}
	}

	return false
}

// matchingFQDNRuleIndices returns the deduplicated rule indices whose
// compiled patterns match qname.
func matchingFQDNRuleIndices(patterns []FQDNPattern, qname string) []int {
	seen := make(map[int]bool)

	var indices []int

	for _, pat := range patterns {
		if pat.Regex.MatchString(qname) && !seen[pat.RuleIndex] {
			seen[pat.RuleIndex] = true
			indices = append(indices, pat.RuleIndex)
		}
	}

	return indices
}

// populateIPSets extracts A and AAAA records from the DNS response
// and batch-adds them to the per-rule ipsets for each matching rule
// index using ipset restore. TTLs are clamped to a minimum of
// [minIPSetTTL].
func (p *DNSProxy) populateIPSets(qname string, resp *dns.Msg, ruleIndices []int) {
	var commands strings.Builder

	for _, rr := range resp.Answer {
		switch a := rr.(type) {
		case *dns.A:
			ttl := max(int(a.Hdr.Ttl), minIPSetTTL)

			for _, idx := range ruleIndices {
				fmt.Fprintf(&commands, "add %s %s timeout %d\n", FQDNIPSetName(idx, false), a.A.String(), ttl)
			}
		case *dns.AAAA:
			ttl := max(int(a.Hdr.Ttl), minIPSetTTL)

			for _, idx := range ruleIndices {
				fmt.Fprintf(&commands, "add %s %s timeout %d\n", FQDNIPSetName(idx, true), a.AAAA.String(), ttl)
			}
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
