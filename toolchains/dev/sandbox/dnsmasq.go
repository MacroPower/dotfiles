package sandbox

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// GenerateDnsmasqConfig builds a dnsmasq configuration that forwards
// DNS queries to the given upstream server. When cfg specifies FQDN
// rules with default-deny active, forwarding is restricted to only
// the allowed domains (plus [TCPForward] hosts); all other queries
// return NXDOMAIN. When cfg is nil, unrestricted, or rules-only,
// all queries are forwarded (existing behavior).
//
// Dnsmasq is used as a local resolver because iptables only allows
// uid 0 to reach external DNS -- the sandboxed user (uid 1000) needs
// a forwarder on localhost.
func GenerateDnsmasqConfig(upstream string, cfg *SandboxConfig) string {
	var b strings.Builder
	b.WriteString("listen-address=127.0.0.1\n")
	b.WriteString("listen-address=::1\n")
	b.WriteString("bind-interfaces\n")
	b.WriteString("port=53\n")
	b.WriteString("no-resolv\n")
	b.WriteString("user=root\n")
	b.WriteString("pid-file=/var/run/dnsmasq.pid\n")
	b.WriteString("\n")

	if cfg == nil || cfg.IsEgressUnrestricted() || cfg.IsEgressRulesOnly() {
		// Unrestricted or rules-only: forward everything.
		fmt.Fprintf(&b, "server=%s\n", upstream)
		return b.String()
	}

	if cfg.IsEgressBlocked() {
		// Deny-all: return NXDOMAIN for everything.
		b.WriteString("address=/#/\n")
		return b.String()
	}

	// Restricted mode: NXDOMAIN catch-all, then per-domain forwards.
	domains := collectDNSDomains(cfg)

	hasNonTCPFQDN := cfg.HasFQDNNonTCPPorts()

	// Bare wildcard "*" matches all FQDNs (Cilium semantics). All DNS
	// queries must resolve so the proxy can see the traffic, even though
	// port/L7 filtering still applies. Forward everything rather than
	// restricting to per-domain entries.
	if slices.Contains(domains, "*") {
		fmt.Fprintf(&b, "server=%s\n", upstream)
		// Populate ipset with all resolved IPs for non-TCP FQDN ports.
		// dnsmasq logs errors when adding IPv4 to an inet6 set (and
		// vice versa); this is cosmetic noise.
		if hasNonTCPFQDN {
			fmt.Fprintf(&b, "ipset=/#/%s,%s\n", IPSetFQDN4, IPSetFQDN6)
		}

		return b.String()
	}

	b.WriteString("address=/#/\n")

	for _, d := range domains {
		fmt.Fprintf(&b, "server=/%s/%s\n", d, upstream)
	}

	// Populate ipset with resolved IPs from FQDN domains for non-TCP
	// port enforcement. The iptables rules restrict by port/protocol,
	// so extra IPs in the ipset are harmless. dnsmasq logs errors when
	// adding IPv4 to an inet6 set (and vice versa); this is cosmetic.
	if hasNonTCPFQDN {
		for _, d := range domains {
			fmt.Fprintf(&b, "ipset=/%s/%s,%s\n", d, IPSetFQDN4, IPSetFQDN6)
		}
	}

	return b.String()
}

// collectDNSDomains returns a sorted, deduplicated list of domains
// that should be forwarded in restricted mode. Includes resolved FQDN
// domains (stripping wildcard prefixes since dnsmasq matches subdomains
// by default) and TCPForward hosts.
func collectDNSDomains(cfg *SandboxConfig) []string {
	seen := make(map[string]bool)

	for _, rule := range cfg.EgressRules() {
		for _, fqdn := range rule.ToFQDNs {
			d := fqdn.MatchName
			if d == "" {
				d = fqdn.MatchPattern
			}

			// Strip leading wildcard prefix: *.example.com,
			// **.example.com, and ***.example.com all become example.com.
			// Dnsmasq's server=/ entries match all subdomains by default,
			// which is correct for both wildcard forms. Bare wildcards
			// (* or **) normalize to "*" for the catch-all check.
			d = strings.TrimLeft(d, "*")
			d = strings.TrimPrefix(d, ".")
			if d == "" {
				d = "*"
			}
			if d != "" && !seen[d] {
				seen[d] = true
			}
		}
	}

	for _, host := range cfg.TCPForwardHosts() {
		if !seen[host] {
			seen[host] = true
		}
	}

	result := make([]string, 0, len(seen))
	for d := range seen {
		result = append(result, d)
	}

	sort.Strings(result)

	return result
}
