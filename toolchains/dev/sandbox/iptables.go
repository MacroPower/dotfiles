package sandbox

import (
	"fmt"
	"strings"
)

// formatPortProto formats a [ResolvedPortProto] as iptables flags.
// Protocol must be non-empty ("tcp", "udp", or "sctp"); ANY protocol
// entries are expanded into separate tcp/udp entries by
// [ResolveCIDRRules] before reaching this function. SCTP rules are
// only generated when explicitly specified (not from ANY expansion),
// matching Cilium's default behavior where SCTP requires opt-in.
func formatPortProto(pp ResolvedPortProto) string {
	if pp.Protocol == "" {
		panic("formatPortProto called with empty protocol; ANY must be expanded to tcp+udp before calling")
	}

	dport := fmt.Sprintf("%d", pp.Port)
	if pp.EndPort > 0 {
		dport = fmt.Sprintf("%d:%d", pp.Port, pp.EndPort)
	}

	return fmt.Sprintf("-p %s --dport %s", pp.Protocol, dport)
}

// groupCIDRsByRule groups resolved CIDRs by their RuleIndex,
// preserving order of first appearance.
func groupCIDRsByRule(cidrs []ResolvedCIDR) [][]ResolvedCIDR {
	if len(cidrs) == 0 {
		return nil
	}

	idxOrder := make([]int, 0)
	groups := make(map[int][]ResolvedCIDR)

	for _, c := range cidrs {
		if _, seen := groups[c.RuleIndex]; !seen {
			idxOrder = append(idxOrder, c.RuleIndex)
		}

		groups[c.RuleIndex] = append(groups[c.RuleIndex], c)
	}

	result := make([][]ResolvedCIDR, len(idxOrder))
	for i, idx := range idxOrder {
		result[i] = groups[idx]
	}

	return result
}

// GenerateIptablesRules builds iptables-restore format rules that redirect
// user traffic to Envoy and restrict outbound access. Operates in three
// modes based on CiliumNetworkPolicy semantics:
//
//   - Unrestricted (Egress nil or contains an allow-all rule): NAT has
//     only TCPForward REDIRECTs; FILTER allows all traffic (no DROP).
//   - Blocked (Egress rules with empty selectors, e.g. egress: [{}]):
//     NAT is empty; FILTER drops all user egress (no Envoy, no REDIRECT).
//     Empty selectors whitelist nothing because there is no L3/L4/L7
//     predicate to match against; see [SandboxConfig.IsEgressBlocked].
//   - Rules (non-empty Egress with selectors): current behavior with
//     REDIRECT to Envoy and per-rule CIDR allows.
//
// Returns both IPv4 and IPv6 rule sets.
func GenerateIptablesRules(cfg *SandboxConfig) (string, string) {
	if cfg.IsEgressUnrestricted() {
		return generateUnrestrictedIptables(cfg)
	}

	if cfg.IsEgressBlocked() {
		return generateBlockedIptables(cfg)
	}

	return generateRulesIptables(cfg)
}

// writeBaseFilterRules emits the shared INPUT and OUTPUT base rules
// that appear in all three iptables modes. INPUT rules act as Cilium's
// ingress policy gate: loopback and reply traffic are allowed, all
// unsolicited inbound traffic is dropped. OUTPUT rules allow loopback,
// loopback CIDR, ESTABLISHED traffic for non-sandboxed UIDs, per-type
// ICMP RELATED matching Cilium's BPF conntrack set, and root DNS
// queries. The ipv6 parameter selects the correct ICMP type set:
// three IPv4 types (destination-unreachable, time-exceeded,
// parameter-problem) or four ICMPv6 types (plus packet-too-big).
func writeBaseFilterRules(b *strings.Builder, loopbackCIDR string, ipv6 bool) {
	b.WriteString("*filter\n")
	// INPUT: allow loopback and replies to outbound connections.
	// Drop everything else (no exposed ports; matches Cilium's
	// default ingress deny).
	b.WriteString("-A INPUT -i lo -j ACCEPT\n")
	b.WriteString("-A INPUT -m state --state ESTABLISHED,RELATED -j ACCEPT\n")
	b.WriteString("-A INPUT -j DROP\n")
	// OUTPUT: base rules shared by all modes.
	b.WriteString("-A OUTPUT -o lo -j ACCEPT\n")
	fmt.Fprintf(b, "-A OUTPUT -d %s -j ACCEPT\n", loopbackCIDR)
	// ESTABLISHED for non-sandboxed UIDs only. UID 1000 must
	// traverse per-UID rules; kernel-generated packets (no owner)
	// still match due to ! negation semantics.
	fmt.Fprintf(b,
		"-A OUTPUT -m state --state ESTABLISHED -m owner ! --uid-owner %s -j ACCEPT\n",
		UID,
	)
	// ICMP error RELATED: per-type rules matching Cilium's BPF
	// conntrack RELATED types. Not UID-scoped since ICMP errors
	// are legitimate responses for all connections.
	if ipv6 {
		// ICMPv6 types: DEST_UNREACH (1), PKT_TOOBIG (2),
		// TIME_EXCEED (3), PARAMPROB (4).
		for _, icmpType := range []string{
			"destination-unreachable",
			"packet-too-big",
			"time-exceeded",
			"parameter-problem",
		} {
			fmt.Fprintf(b,
				"-A OUTPUT -p icmpv6 --icmpv6-type %s "+
					"-m state --state RELATED -j ACCEPT\n",
				icmpType)
		}
	} else {
		// IPv4 ICMP types: DEST_UNREACH (3), TIME_EXCEEDED (11),
		// PARAMETERPROB (12).
		for _, icmpType := range []string{
			"destination-unreachable",
			"time-exceeded",
			"parameter-problem",
		} {
			fmt.Fprintf(b,
				"-A OUTPUT -p icmp --icmp-type %s "+
					"-m state --state RELATED -j ACCEPT\n",
				icmpType)
		}
	}
	b.WriteString("-A OUTPUT -m owner --uid-owner 0 -p udp --dport 53 -j ACCEPT\n")
	b.WriteString("-A OUTPUT -m owner --uid-owner 0 -p tcp --dport 53 -j ACCEPT\n")
}

// generateUnrestrictedIptables produces rules that allow all egress.
// NAT contains only TCPForward REDIRECTs; FILTER has standard base
// rules plus ACCEPT (no DROP).
func generateUnrestrictedIptables(cfg *SandboxConfig) (string, string) {
	writeNat := func(b *strings.Builder) {
		b.WriteString("*nat\n")

		for _, fwd := range cfg.TCPForwards {
			fmt.Fprintf(
				b,
				"-A OUTPUT -m owner --uid-owner %s -p tcp --dport %d -j REDIRECT --to-port %d\n",
				UID,
				fwd.Port,
				15000+fwd.Port,
			)
		}

		b.WriteString("COMMIT\n")
	}

	writeFilter := func(b *strings.Builder, loopbackCIDR string, ipv6 bool) {
		writeBaseFilterRules(b, loopbackCIDR, ipv6)

		if cfg.Logging {
			b.WriteString("-A OUTPUT -j LOG --log-prefix \"SANDBOX_ALLOW: \"\n")
		}

		b.WriteString("-A OUTPUT -j ACCEPT\n")
		b.WriteString("COMMIT\n")
	}

	var nat4, filter4, nat6, filter6 strings.Builder
	writeNat(&nat4)
	writeNat(&nat6)
	writeFilter(&filter4, "127.0.0.0/8", false)
	writeFilter(&filter6, "::1/128", true)

	return nat4.String() + filter4.String(), nat6.String() + filter6.String()
}

// generateBlockedIptables produces rules that block all user egress.
// NAT is empty; FILTER has base rules then DROP.
func generateBlockedIptables(cfg *SandboxConfig) (string, string) {
	writeNat := func(b *strings.Builder) {
		b.WriteString("*nat\n")
		b.WriteString("COMMIT\n")
	}

	writeFilter := func(b *strings.Builder, loopbackCIDR string, ipv6 bool) {
		writeBaseFilterRules(b, loopbackCIDR, ipv6)

		if cfg.Logging {
			b.WriteString("-A OUTPUT -j LOG --log-prefix \"SANDBOX_DROP: \"\n")
		}

		b.WriteString("-A OUTPUT -j DROP\n")
		b.WriteString("COMMIT\n")
	}

	var nat4, filter4, nat6, filter6 strings.Builder
	writeNat(&nat4)
	writeNat(&nat6)
	writeFilter(&filter4, "127.0.0.0/8", false)
	writeFilter(&filter6, "::1/128", true)

	return nat4.String() + filter4.String(), nat6.String() + filter6.String()
}

// generateRulesIptables is the standard rules-mode generation with
// Envoy REDIRECT and per-rule CIDR allows. The final verdict is
// always DROP (default-deny).
func generateRulesIptables(cfg *SandboxConfig) (string, string) {
	resolvedPorts := cfg.ResolvePorts()
	cidr4, cidr6 := cfg.ResolveCIDRRules()
	openPortRules := cfg.ResolveOpenPortRules()
	fqdnRulePorts := cfg.ResolveFQDNNonTCPPorts()

	var (
		nat4, filter4 strings.Builder
		nat6, filter6 strings.Builder
	)

	// writeCIDRReturn emits NAT RETURN rules for allowed CIDRs so their
	// traffic bypasses the Envoy redirect. Excepted subnets also match
	// these broad RETURN rules, taking a NAT RETURN -> FILTER DROP path
	// instead of being dropped earlier. This is cosmetically inefficient
	// but correct: iptables has no clean way to exclude sub-CIDRs from
	// a broader match in the NAT table without duplicating the except
	// logic here.
	writeCIDRReturn := func(b *strings.Builder, cidrs []ResolvedCIDR) {
		for _, rule := range cidrs {
			if len(rule.Ports) == 0 {
				fmt.Fprintf(b, "-A OUTPUT -m owner --uid-owner %s -d %s -j RETURN\n", UID, rule.CIDR)
			} else {
				for _, pp := range rule.Ports {
					fmt.Fprintf(
						b,
						"-A OUTPUT -m owner --uid-owner %s %s -d %s -j RETURN\n",
						UID,
						formatPortProto(pp),
						rule.CIDR,
					)
				}
			}
		}
	}

	writeNatRules := func(b *strings.Builder, cidrs []ResolvedCIDR) {
		b.WriteString("*nat\n")
		// CIDR allow: RETURN for user traffic to allowed CIDRs
		// (skips redirect to Envoy). Must come before REDIRECT rules.
		writeCIDRReturn(b, cidrs)
		// Envoy redirect: REDIRECT rules for all resolved ports.
		for _, p := range resolvedPorts {
			fmt.Fprintf(
				b,
				"-A OUTPUT -m owner --uid-owner %s -p tcp --dport %d -j REDIRECT --to-port %d\n",
				UID,
				p,
				15000+p,
			)
		}

		for _, fwd := range cfg.TCPForwards {
			fmt.Fprintf(
				b,
				"-A OUTPUT -m owner --uid-owner %s -p tcp --dport %d -j REDIRECT --to-port %d\n",
				UID,
				fwd.Port,
				15000+fwd.Port,
			)
		}

		b.WriteString("COMMIT\n")
	}

	writeNatRules(&nat4, cidr4)
	writeNatRules(&nat6, cidr6)

	unrestricted := cfg.HasUnrestrictedOpenPorts()

	// writeCIDRChains emits per-rule iptables chains that preserve
	// Cilium's OR semantics across egress rules. Each chain evaluates
	// one rule's CIDRs and excepts independently: RETURN for except
	// hits (try next rule), ACCEPT for CIDR hits (allow packet).
	writeCIDRChains := func(b *strings.Builder, cidrs []ResolvedCIDR, af string) {
		groups := groupCIDRsByRule(cidrs)

		// Declare all chains before any -A references.
		for i := range groups {
			fmt.Fprintf(b, "-N CIDR_%s_%d\n", af, i)
		}

		// Populate each per-rule chain.
		for i, group := range groups {
			chainName := fmt.Sprintf("CIDR_%s_%d", af, i)

			// Except RETURNs scoped to this rule only.
			for _, rule := range group {
				for _, exc := range rule.Except {
					if len(rule.Ports) == 0 {
						fmt.Fprintf(b, "-A %s -m owner --uid-owner %s -d %s -j RETURN\n", chainName, UID, exc)
					} else {
						for _, pp := range rule.Ports {
							fmt.Fprintf(b, "-A %s -m owner --uid-owner %s %s -d %s -j RETURN\n",
								chainName, UID, formatPortProto(pp), exc)
						}
					}
				}
			}

			// CIDR ACCEPTs scoped to this rule.
			for _, rule := range group {
				if len(rule.Ports) == 0 {
					fmt.Fprintf(b, "-A %s -m owner --uid-owner %s -d %s -j ACCEPT\n", chainName, UID, rule.CIDR)
				} else {
					for _, pp := range rule.Ports {
						fmt.Fprintf(b, "-A %s -m owner --uid-owner %s %s -d %s -j ACCEPT\n",
							chainName, UID, formatPortProto(pp), rule.CIDR)
					}
				}
			}
		}

		// Jump from OUTPUT to each per-rule chain in sequence.
		for i := range groups {
			fmt.Fprintf(b, "-A OUTPUT -j CIDR_%s_%d\n", af, i)
		}
	}

	writeFilterRules := func(b *strings.Builder, loopbackCIDR string, cidrs []ResolvedCIDR, af string, ipv6 bool) {
		writeBaseFilterRules(b, loopbackCIDR, ipv6)
		if !unrestricted {
			// Per-rule CIDR chains preserve OR semantics: each
			// rule's excepts only block within that rule's chain.
			writeCIDRChains(b, cidrs, af)
		}

		// Envoy accept: Envoy can reach any IP (domain allowlist
		// in Envoy config provides security).
		fmt.Fprintf(b, "-A OUTPUT -m owner --uid-owner %s -j ACCEPT\n", EnvoyUID)
		if unrestricted {
			// Unrestricted open ports: ACCEPT all user traffic.
			// FQDN-port combinations are still intercepted by NAT
			// REDIRECT rules above, preserving Envoy L7 filtering.
			fmt.Fprintf(b, "-A OUTPUT -m owner --uid-owner %s -j ACCEPT\n", UID)
		} else {
			// Non-TCP open ports: ACCEPT for user UID on UDP and SCTP
			// ports that have no destination restriction (toPorts-only
			// rules). Single TCP open ports are handled by Envoy
			// catch-all chains; TCP port ranges bypass Envoy via
			// direct ACCEPT (Envoy cannot create listeners for
			// arbitrary port ranges).
			for _, op := range openPortRules {
				dport := fmt.Sprintf("%d", op.Port)
				if op.EndPort > 0 {
					dport = fmt.Sprintf("%d:%d", op.Port, op.EndPort)
				}

				if op.Protocol == "udp" || op.Protocol == "sctp" {
					fmt.Fprintf(
						b,
						"-A OUTPUT -m owner --uid-owner %s -p %s --dport %s -j ACCEPT\n",
						UID,
						op.Protocol,
						dport,
					)
				}

				if op.Protocol == "tcp" && op.EndPort > 0 {
					fmt.Fprintf(
						b,
						"-A OUTPUT -m owner --uid-owner %s -p tcp --dport %s -j ACCEPT\n",
						UID,
						dport,
					)
				}
			}

			// Non-TCP FQDN ports: per-rule ipset pairs. Each FQDN
			// rule gets its own ipset so cross-rule IP leakage is
			// prevented. ESTABLISHED allows packets on flows whose
			// initial packet was accepted by the ipset rule below,
			// implementing zombie/CT semantics: conntrack keeps flows
			// alive past ipset TTL expiry, matching Cilium's
			// DNSZombieMappings behavior. The ipset rule gates first
			// packets of new flows, requiring DNS resolution before
			// establishment.
			for _, frp := range fqdnRulePorts {
				ipsetName := FQDNIPSetName(frp.RuleIndex, ipv6)

				for _, fp := range frp.Ports {
					fmt.Fprintf(b,
						"-A OUTPUT -m owner --uid-owner %s -p %s --dport %d "+
							"-m state --state ESTABLISHED -j ACCEPT\n",
						UID, fp.Protocol, fp.Port)
					fmt.Fprintf(b,
						"-A OUTPUT -m owner --uid-owner %s -p %s --dport %d "+
							"-m set --match-set %s dst -j ACCEPT\n",
						UID, fp.Protocol, fp.Port, ipsetName)
				}
			}
		}

		if cfg.Logging {
			b.WriteString("-A OUTPUT -j LOG --log-prefix \"SANDBOX_DROP: \"\n")
		}

		b.WriteString("-A OUTPUT -j DROP\n")

		b.WriteString("COMMIT\n")
	}

	writeFilterRules(&filter4, "127.0.0.0/8", cidr4, "4", false)
	writeFilterRules(&filter6, "::1/128", cidr6, "6", true)

	return nat4.String() + filter4.String(), nat6.String() + filter6.String()
}
