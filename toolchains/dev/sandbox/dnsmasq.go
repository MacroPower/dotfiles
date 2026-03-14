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
	if slices.ContainsFunc(domains, func(d dnsDomain) bool {
		return d.Name == "*"
	}) {
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
		fmt.Fprintf(&b, "server=/%s/%s\n", d.dnsmasqDomain(), upstream)
	}

	// Populate ipset with resolved IPs from FQDN domains for non-TCP
	// port enforcement. The iptables rules restrict by port/protocol,
	// so extra IPs in the ipset are harmless. dnsmasq logs errors when
	// adding IPv4 to an inet6 set (and vice versa); this is cosmetic.
	if hasNonTCPFQDN {
		for _, d := range domains {
			fmt.Fprintf(&b, "ipset=/%s/%s,%s\n", d.dnsmasqDomain(), IPSetFQDN4, IPSetFQDN6)
		}
	}

	return b.String()
}

// dnsDomain is a domain entry for dnsmasq configuration. Wildcard
// entries (from matchPattern "*.example.com" or "**.example.com")
// need the dnsmasq wildcard syntax (/*.example.com/) to avoid
// matching the bare parent domain; exact entries use the plain
// (/example.com/) form.
type dnsDomain struct {
	// Name is the domain without any wildcard prefix.
	Name string
	// Wildcard is true when the entry originated from a matchPattern
	// with a leading wildcard prefix ("*." or "**."), requiring
	// dnsmasq's /*.domain/ syntax to exclude the bare parent domain.
	Wildcard bool
}

// dnsmasqDomain returns the domain string formatted for use in dnsmasq
// server= and ipset= directives. Wildcard entries use the /*.domain/
// form to exclude the bare parent domain; exact entries use the plain
// /domain/ form. Note that dnsmasq's /*.domain/ still matches all
// subdomain depths (not just single-label); single-label enforcement
// for Cilium's "*" pattern happens at the Envoy RBAC layer.
func (d dnsDomain) dnsmasqDomain() string {
	if d.Wildcard {
		return "*." + d.Name
	}

	return d.Name
}

// collectDNSDomains returns a sorted, deduplicated list of domains
// that should be forwarded in restricted mode. Includes FQDN domains
// (preserving wildcard vs exact distinction for correct dnsmasq
// syntax) and TCPForward hosts. The bare wildcard "*" pattern is
// included as-is for the caller to handle.
func collectDNSDomains(cfg *SandboxConfig) []dnsDomain {
	seen := make(map[string]bool)
	var result []dnsDomain

	for _, rule := range cfg.EgressRules() {
		for _, fqdn := range rule.ToFQDNs {
			var d dnsDomain

			if fqdn.MatchName != "" {
				d = dnsDomain{Name: fqdn.MatchName}
			} else {
				// Strip all leading "*" characters then the
				// following "." to extract the base domain.
				// This handles both "*.example.com" and
				// "**.example.com" uniformly.
				stripped := strings.TrimLeft(fqdn.MatchPattern, "*")
				stripped = strings.TrimPrefix(stripped, ".")
				if stripped == "" {
					// Bare wildcard "*", "**", etc.: pass
					// through for catch-all handling.
					if !seen["*"] {
						seen["*"] = true
						result = append(result, dnsDomain{Name: "*"})
					}

					continue
				}

				d = dnsDomain{Name: stripped, Wildcard: true}
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
			// domain, upgrade to non-wildcard so dnsmasq uses
			// /domain/ (bare + subdomains) instead of
			// /*.domain/ (subdomains only).
			for i := range result {
				if result[i].Name == host && result[i].Wildcard {
					result[i].Wildcard = false
					break
				}
			}

			continue
		}

		seen[host] = true
		result = append(result, dnsDomain{Name: host})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}
