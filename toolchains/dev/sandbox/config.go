// Package sandbox provides configuration types and generation functions
// for the sandbox firewall. It reads a YAML config file and produces
// iptables rules, Envoy proxy config, and DNS proxy config.
package sandbox

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"go.jacobcolvin.com/niceyaml"

	_ "embed"
)

const maxFQDNLength = 255

// proxyPortBase is the base port added to destination ports to derive
// the Envoy proxy listen port. Ports above maxProxyablePort overflow
// uint16 when offset and are rejected at validation time.
const proxyPortBase = 15000

const maxProxyablePort = 65535 - proxyPortBase

var (
	// allowedMatchNameChars validates that a matchName contains only
	// DNS-safe characters. Matches Cilium's allowedMatchNameChars
	// (pkg/policy/api/fqdn.go) but uses only lowercase since
	// normalizeEgressRule lowercases before validation.
	allowedMatchNameChars = regexp.MustCompile(`^[-a-z0-9_.]+$`)

	// allowedMatchPatternChars validates that a matchPattern contains
	// only DNS-safe characters plus the wildcard '*'. Matches Cilium's
	// allowedPatternChars (pkg/fqdn/matchpattern/matchpattern.go).
	allowedMatchPatternChars = regexp.MustCompile(`^[-a-z0-9_.*]+$`)

	// ErrFQDNSelectorEmpty is returned when an [FQDNSelector] has neither
	// matchName nor matchPattern set.
	ErrFQDNSelectorEmpty = errors.New("FQDN selector must have matchName or matchPattern")

	// ErrFQDNSelectorAmbiguous is returned when an [FQDNSelector] has both
	// matchName and matchPattern set. Cilium requires exactly one.
	ErrFQDNSelectorAmbiguous = errors.New("FQDN selector must have exactly one of matchName or matchPattern, not both")

	// ErrPortEmpty is returned when a [Port] has an empty port string.
	ErrPortEmpty = errors.New("port must not be empty")

	// ErrPortInvalid is returned when a [Port] has a non-numeric, out-of-range
	// (must be 0-65535), or unknown service name port string.
	ErrPortInvalid = errors.New("port must be 0-65535 or a known service name")

	// ErrProtocolInvalid is returned when a [Port] has an unrecognized
	// protocol. Valid values are TCP, UDP, SCTP, ANY, or empty (defaults
	// to ANY).
	//
	// Under Cilium's default configuration, ANY expands to TCP and UDP.
	// SCTP requires explicit opt-in (Cilium Helm value sctp.enabled=true);
	// the sandbox matches this default by expanding ANY to TCP+UDP only.
	ErrProtocolInvalid = errors.New("invalid protocol: must be TCP, UDP, SCTP, ANY, or empty")

	// ErrEndPortInvalid is returned when a [Port] has an endPort that
	// exceeds 65535 or is less than the port.
	ErrEndPortInvalid = errors.New("endPort must be >= port and <= 65535")

	// ErrCIDRInvalid is returned when a CIDR string cannot be parsed.
	ErrCIDRInvalid = errors.New("invalid CIDR")

	// ErrTCPForwardRequiresEgress is returned when a [TCPForward] is
	// specified but egress is blocked (empty list with no rules).
	ErrTCPForwardRequiresEgress = errors.New("tcpForwards requires egress rules or unrestricted egress")

	// ErrPathInvalidRegex is returned when a path in an [HTTPRule] is not
	// a valid regular expression.
	ErrPathInvalidRegex = errors.New("path must be a valid regex")

	// ErrMethodInvalidRegex is returned when a method in an [HTTPRule] is
	// not a valid regular expression.
	ErrMethodInvalidRegex = errors.New("method must be a valid regex")

	// ErrHostInvalidRegex is returned when a host in an [HTTPRule] is
	// not a valid regular expression.
	ErrHostInvalidRegex = errors.New("host must be a valid regex")

	// ErrHTTPHeaderEmpty is returned when an [HTTPRule] Headers entry
	// is an empty string.
	ErrHTTPHeaderEmpty = errors.New("HTTP header name must not be empty")

	// ErrHeaderMatchNameEmpty is returned when a [HeaderMatch] has an
	// empty Name field.
	ErrHeaderMatchNameEmpty = errors.New("headerMatch name must not be empty")

	// ErrHeaderMatchMismatchAction is returned when a [HeaderMatch]
	// sets a Mismatch action. The sandbox cannot enforce request
	// modification semantics (LOG, ADD, DELETE, REPLACE).
	ErrHeaderMatchMismatchAction = errors.New("headerMatch mismatch actions are not supported by the sandbox")

	// ErrPortExceedsProxyRange is returned when a port exceeds the
	// maximum value that can be offset by [proxyPortBase] without
	// overflowing uint16. Ports above 50535 produce proxy listen
	// ports above 65535.
	ErrPortExceedsProxyRange = errors.New("port exceeds proxy range (max 50535)")

	// ErrInvalidTCPForward is returned when a [TCPForward] entry has a
	// non-positive port or empty host.
	ErrInvalidTCPForward = errors.New("invalid tcp forward: port must be positive and host must be non-empty")

	// ErrDuplicateTCPForwardPort is returned when two [TCPForward] entries
	// specify the same port.
	ErrDuplicateTCPForwardPort = errors.New("duplicate tcp forward port")

	// ErrFQDNRequiresPorts is returned when an [EgressRule] has toFQDNs
	// but no toPorts with non-empty ports. The sandbox requires explicit
	// ports because Envoy needs per-port listeners; Cilium itself does
	// not require toPorts with toFQDNs.
	ErrFQDNRequiresPorts = errors.New(
		"toFQDNs rules require explicit toPorts with non-empty ports (sandbox constraint: Envoy needs per-port listeners)",
	)

	// ErrExceptNotSubnet is returned when an except CIDR is not a subnet
	// of its parent CIDR. Cilium requires except entries to be contained
	// within the parent range.
	ErrExceptNotSubnet = errors.New("except CIDR must be a subnet of the parent CIDR")

	// ErrL7RequiresFQDN is returned when L7 rules are specified on a rule
	// without toFQDNs selectors. The sandbox cannot enforce L7 rules
	// without domain context for MITM; CIDR rules bypass Envoy.
	ErrL7RequiresFQDN = errors.New("L7 rules require toFQDNs selectors")

	// ErrL7RequiresTCP is returned when L7 HTTP rules are paired with
	// a non-TCP protocol. Envoy's HTTP connection manager requires TCP
	// streams; UDP, SCTP, and ANY are invalid with L7 HTTP rules.
	// Empty protocol is allowed (implies TCP). Cilium's
	// PortRule.sanitize() rejects empty too (it normalizes to ANY
	// first), but the sandbox intentionally permits it to reduce
	// boilerplate.
	ErrL7RequiresTCP = errors.New("L7 HTTP rules can only apply to TCP")

	// ErrL7WithWildcardPort is returned when L7 rules are used with
	// port 0 (wildcard). Cilium rejects this combination because L7
	// inspection requires a concrete port for proxy binding.
	ErrL7WithWildcardPort = errors.New("L7 rules cannot be used when port is 0")

	// ErrEndPortWithWildcardPort is returned when endPort is used with
	// port 0. Port 0 already means "all ports," so a range is redundant
	// and likely a misconfiguration. Cilium does not reject this
	// combination, but the sandbox is intentionally stricter to prevent
	// confusing configs (e.g. port 0 endPort 443 looks like a range but
	// means all ports).
	ErrEndPortWithWildcardPort = errors.New("endPort cannot be used with wildcard port 0")

	// ErrWildcardWithL7 is returned when a wildcard matchPattern is used
	// with L7 HTTP rules. The MITM filter chain builds filesystem paths
	// from the domain name, and wildcards in paths are invalid.
	ErrWildcardWithL7 = errors.New("wildcard matchPattern cannot be used with L7 HTTP rules")

	// ErrFQDNPatternPartialWildcard is returned when a matchPattern
	// contains a wildcard that is not the leading "*." prefix.
	// Cilium supports partial wildcards like "api.*-staging.example.com"
	// where "*" matches characters within a label, but Envoy's
	// server_names does not; they would silently fail to match.
	ErrFQDNPatternPartialWildcard = errors.New(
		"matchPattern with partial wildcard is not supported; only leading *. prefix is allowed",
	)

	// ErrFQDNNameInvalidChars is returned when a matchName contains
	// characters outside the DNS allowlist [a-z0-9._-]. Matches
	// Cilium's allowedMatchNameChars validation.
	ErrFQDNNameInvalidChars = errors.New(
		"matchName contains invalid characters: only a-z, 0-9, '.', '-', and '_' are allowed",
	)

	// ErrFQDNPatternInvalidChars is returned when a matchPattern
	// contains characters outside the pattern allowlist [a-z0-9._*-].
	// Matches Cilium's allowedPatternChars validation.
	ErrFQDNPatternInvalidChars = errors.New(
		"matchPattern contains invalid characters: only a-z, 0-9, '.', '-', '_', and '*' are allowed",
	)

	// ErrFQDNTooLong is returned when a matchName or matchPattern
	// exceeds 255 characters. Matches Cilium's MaxFQDNLength constant
	// and kubebuilder MaxLength validation.
	ErrFQDNTooLong = errors.New("FQDN selector exceeds maximum length of 255 characters")

	// ErrFQDNWithCIDR is returned when an [EgressRule] combines toFQDNs
	// with toCIDR or toCIDRSet. Under CiliumNetworkPolicy semantics,
	// toFQDNs is mutually exclusive with other L3 selectors within a
	// single rule; use separate rules instead.
	ErrFQDNWithCIDR = errors.New(
		"toFQDNs cannot be combined with toCIDR or toCIDRSet in the same rule; use separate rules",
	)

	// ErrCIDRAndCIDRSetMixed is returned when an [EgressRule] combines
	// toCIDR with toCIDRSet. These must be in separate rules.
	//
	// In Cilium, EgressCommonRule.sanitize() calls l3Members() to build
	// a map of all L3 selector fields (ToCIDR, ToCIDRSet, ToEndpoints,
	// ToEntities, ToServices, ToGroups, ToNodes) with their counts,
	// then performs a pairwise mutual-exclusivity check: if any two
	// different L3 fields both have count >0, Cilium rejects the rule
	// with "combining <field1> and <field2> is not supported yet".
	// See pkg/policy/api/egress.go (EgressCommonRule.sanitize, l3Members).
	//
	// Because our [EgressRule] only supports ToCIDR and ToCIDRSet as L3
	// selectors (we have no ToEndpoints, ToEntities, ToServices, ToGroups,
	// or ToNodes), the ToCIDR + ToCIDRSet pair is the only combination
	// that can trigger this check. ToFQDNs is handled separately by
	// [ErrFQDNWithCIDR] before we reach this point; Cilium includes
	// ToFQDNs in the same unified l3Members() pairwise check, but
	// the outcome is equivalent since both reject the combination.
	//
	// Note: Cilium's l3Members() uses countNonGeneratedCIDRRules for
	// ToCIDRSet and countNonGeneratedEndpoints for ToEndpoints, so that
	// auto-generated entries (from ToServices/ToFQDNs expansion at
	// runtime) do not count toward the mutual-exclusivity check. The
	// sandbox has no equivalent of Generated entries since we never
	// perform ToServices expansion or runtime identity resolution, so
	// this distinction is irrelevant here.
	ErrCIDRAndCIDRSetMixed = errors.New(
		"toCIDR and toCIDRSet cannot be combined in the same rule; use separate rules",
	)

	// ErrEndPortWithNamedPort is returned when a [Port] has endPort
	// set with a named port. Cilium silently ignores endPort with
	// named ports; the sandbox rejects it outright since silent
	// ignoring is confusing in a config-validation context.
	ErrEndPortWithNamedPort = errors.New("endPort not supported with named ports")

	// ErrTCPForwardPortConflict is returned when a [TCPForward] port
	// overlaps with a resolved port.
	ErrTCPForwardPortConflict = errors.New("tcp forward port conflicts with resolved ports")

	// ErrUnsupportedSelector is returned when an [EgressRule] contains a
	// CiliumNetworkPolicy selector that the sandbox does not implement.
	// The sandbox only supports toFQDNs, toPorts, toCIDR, and toCIDRSet.
	// Cilium selectors like toEndpoints, toEntities, toServices, toNodes,
	// and toGroups require cluster identity infrastructure that does not
	// exist in the sandbox.
	ErrUnsupportedSelector = errors.New("unsupported egress selector")

	// validProtocols lists the supported transport protocols. Cilium
	// also supports ICMP, ICMPv6, VRRP, and IGMP, but these are
	// IP-layer protocols without ports and cannot be expressed in the
	// sandbox's port-based model.
	validProtocols = map[string]bool{
		"": true, "TCP": true, "UDP": true, "SCTP": true, "ANY": true,
	}

	// wellKnownPorts maps IANA service names to their standard port
	// numbers. Cilium resolves named ports dynamically from Kubernetes
	// pod specs (containerPort.name); the sandbox uses this static map
	// instead since there are no pods to query.
	wellKnownPorts = map[string]uint16{
		"domain":  53,
		"dns":     53,
		"dns-tcp": 53, // Kubernetes naming convention, not IANA
		"http":    80,
		"https":   443,
	}

	//go:embed defaults.yaml
	defaultsYAML []byte
)

const (
	protoTCP  = "tcp"
	protoUDP  = "udp"
	protoSCTP = "sctp"
)

// isSvcName reports whether s is a valid IANA service name per RFC
// 6335: 1-15 characters, alphanumeric with non-consecutive hyphens,
// containing at least one letter. Matches Cilium's pkg/iana.IsSvcName.
func isSvcName(s string) bool {
	if s == "" || len(s) > 15 {
		return false
	}

	hasLetter := false
	prevHyphen := false

	for i, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			hasLetter = true
			prevHyphen = false

		case c >= '0' && c <= '9':
			prevHyphen = false
		case c == '-':
			if i == 0 || i == len(s)-1 || prevHyphen {
				return false
			}

			prevHyphen = true

		default:
			return false
		}
	}

	return hasLetter
}

// ResolvePort converts a port string to a number. It accepts numeric
// strings ("443") and well-known IANA service names ("https"). Cilium
// resolves named ports dynamically from Kubernetes pod specs; the
// sandbox uses a static [wellKnownPorts] map instead. Returns an error
// for unknown names, invalid syntax, or values outside 0-65535.
//
// Uses base 10 (not base 0) because the sandbox reads YAML config
// files, not Kubernetes API objects, so hex/octal/binary port literals
// are not meaningful.
func ResolvePort(s string) (uint16, error) {
	n, err := strconv.ParseUint(s, 10, 16)
	if err == nil {
		return uint16(n), nil
	}

	if !isSvcName(s) {
		return 0, fmt.Errorf("invalid port name syntax %q", s)
	}

	if n, ok := wellKnownPorts[strings.ToLower(s)]; ok {
		return n, nil
	}

	return 0, fmt.Errorf("unknown service name %q", s)
}

const (
	// UID is the numeric user ID for the sandboxed non-root user.
	UID = "1000"

	// GID is the numeric group ID for the sandboxed non-root user.
	GID = "1000"

	// EnvoyUID is the numeric user/group ID for the Envoy proxy process.
	EnvoyUID = "999"

	// Username is the non-root user created inside the dev container.
	Username = "dev"

	// HomeDir is the home directory for the dev container user.
	HomeDir = "/home/dev"

	// HMBin is the stable path to home-manager managed binaries.
	HMBin = HomeDir + "/.local/state/home-manager/gcroots/current-home/home-path/bin"

	// ConfigPath is the path to the sandbox YAML config file.
	ConfigPath = "/etc/sandbox/config.yaml"

	// minIPSetTTL is the minimum timeout (in seconds) for ipset entries
	// populated from DNS responses. Cilium's --tofqdns-min-ttl defaults
	// to 0 (honor upstream TTL exactly); we use 60 to avoid excessive
	// process spawning from frequent ipset add calls at very short TTLs,
	// and to provide a safety margin against transient connectivity
	// failures between DNS re-queries.
	minIPSetTTL = 60

	// maxRegexLen is the maximum allowed length for path and method
	// regex patterns. Envoy uses RE2 with a default max program size
	// of 100; extremely long patterns could pass Go validation but
	// fail in RE2.
	maxRegexLen = 1000
)

// FQDNIPSetName returns the ipset name for a FQDN rule index and
// address family. Names follow sandbox_fqdn{4,6}_R where R is the
// 0-indexed position among FQDN-bearing rules with non-TCP ports.
func FQDNIPSetName(ruleIdx int, ipv6 bool) string {
	if ipv6 {
		return fmt.Sprintf("sandbox_fqdn6_%d", ruleIdx)
	}

	return fmt.Sprintf("sandbox_fqdn4_%d", ruleIdx)
}

// EgressRule defines an egress policy with optional FQDN, port, and CIDR
// selectors. Under CiliumNetworkPolicy semantics, ToFQDNs is mutually
// exclusive with ToCIDR and ToCIDRSet within a single rule; split them
// into separate rules in the egress array. ToCIDR and ToCIDRSet are
// mutually exclusive within a single rule; split them into separate
// rules. L4 selectors (ToPorts) are AND'd with the L3 result. L7 rules
// (HTTP inspection) require ToFQDNs. MatchName and MatchPattern values
// in ToFQDNs are normalized to lowercase with trailing dots stripped.
//
// An EgressRule with no selectors set (all fields empty/nil) whitelists
// nothing, because there is no L3/L4/L7 predicate to match against.
// This is a common source of confusion: an empty rule is not a wildcard.
// In Cilium, the allow-all pattern is an EgressRule containing a
// toEndpoints selector with an empty EndpointSelector (i.e.,
// toEndpoints: [{}]), which acts as a wildcard matching all endpoints.
// The sandbox does not implement toEndpoints; using it produces an
// [ErrUnsupportedSelector] validation error. The distinction matters
// for understanding why egress: [{}] means deny-all rather than
// allow-all. See Cilium's EgressRule and EgressCommonRule structs in
// pkg/policy/api/egress.go.
//
// Unsupported Cilium selectors (toEndpoints, toEntities, toServices,
// toNodes, toGroups, toRequires, icmps, authentication) are parsed
// into stub fields and rejected during [Validate]. Additionally,
// [parseConfigRaw] enables [yaml.DisallowUnknownField] so that any
// field not present in the struct (including future Cilium additions)
// produces a parse error rather than silent data loss.
type EgressRule struct {
	// ToFQDNs selects traffic by destination hostname.
	ToFQDNs []FQDNSelector `yaml:"toFQDNs,omitempty"`
	// ToPorts restricts allowed destination ports and optional L7 rules.
	ToPorts []PortRule `yaml:"toPorts,omitempty"`
	// ToCIDR allows traffic to simple IP ranges in CIDR notation.
	ToCIDR []string `yaml:"toCIDR,omitempty"`
	// ToCIDRSet allows traffic to IP ranges with optional exceptions.
	ToCIDRSet []CIDRRule `yaml:"toCIDRSet,omitempty"`

	// Unsupported Cilium selectors. These fields exist so that the YAML
	// decoder captures them instead of relying solely on strict mode.
	// [Validate] rejects any rule where these are populated, producing
	// an actionable error message. The types are []any or any to avoid
	// replicating Cilium's full type hierarchy.
	//
	// See Cilium's EgressCommonRule and EgressRule in
	// pkg/policy/api/egress.go for the canonical definitions.

	// Authentication is a Cilium field for mutual authentication.
	// The sandbox does not support authentication policy.
	Authentication any `yaml:"authentication,omitempty"`
	// ToEndpoints is a Cilium L3 selector matching endpoints by label.
	// The sandbox has no endpoint identity system.
	ToEndpoints []any `yaml:"toEndpoints,omitempty"`
	// ToEntities is a Cilium L3 selector matching special entities
	// (world, cluster, host, etc). The sandbox has no entity resolution.
	ToEntities []any `yaml:"toEntities,omitempty"`
	// ToServices is a Cilium L3 selector matching Kubernetes services.
	// The sandbox has no service discovery.
	ToServices []any `yaml:"toServices,omitempty"`
	// ToNodes is a Cilium L3 selector matching nodes by label.
	// The sandbox has no node identity system.
	ToNodes []any `yaml:"toNodes,omitempty"`
	// ToGroups is a Cilium L3 selector matching cloud provider groups.
	// The sandbox has no cloud provider integration.
	ToGroups []any `yaml:"toGroups,omitempty"`
	// ToRequires is a deprecated Cilium field. Rejected unconditionally.
	ToRequires []any `yaml:"toRequires,omitempty"`
	// ICMPs is a Cilium selector for ICMP type filtering.
	// The sandbox does not support ICMP-level policy.
	ICMPs []any `yaml:"icmps,omitempty"`
}

// CIDRRule specifies an IP range to allow, with optional exceptions.
type CIDRRule struct {
	// CIDR is the IP range in CIDR notation (e.g. "10.0.0.0/8").
	CIDR string `yaml:"cidr"`
	// Except lists sub-ranges of CIDR to exclude.
	Except []string `yaml:"except,omitempty"`
}

// FQDNSelector matches traffic by destination hostname. Exactly one of
// MatchName or MatchPattern should be set.
type FQDNSelector struct {
	// MatchName matches an exact hostname.
	MatchName string `yaml:"matchName,omitempty"`
	// MatchPattern matches hostnames using wildcard patterns (e.g. "*.example.com").
	MatchPattern string `yaml:"matchPattern,omitempty"`
}

// PortRule restricts traffic to specific ports with optional L7 rules.
type PortRule struct {
	// Rules specifies optional L7 inspection rules.
	Rules *L7Rules `yaml:"rules,omitempty"`
	// Ports lists allowed destination ports.
	Ports []Port `yaml:"ports,omitempty"`
}

// Port specifies a destination port number with optional protocol and range.
type Port struct {
	// Port is the port number or IANA service name (e.g. "443", "https").
	Port string `yaml:"port"`
	// Protocol is the transport protocol: "TCP", "UDP", "SCTP", "ANY",
	// or empty (defaults to ANY when omitted). "ANY" matches TCP, UDP,
	// and SCTP.
	Protocol string `yaml:"protocol,omitempty"`
	// EndPort specifies the upper bound of a port range. When set, the
	// rule matches ports from Port to EndPort inclusive. Valid with CIDR
	// and open-port rules (toPorts without L3 selectors); not supported
	// with toFQDNs (Envoy needs individual listeners). Open-port TCP
	// ranges bypass Envoy via direct iptables ACCEPT.
	EndPort int `yaml:"endPort,omitempty"`
}

// L7Rules contains protocol-specific inspection rules.
type L7Rules struct {
	// HTTP specifies HTTP-level rules for MITM inspection.
	HTTP []HTTPRule `yaml:"http,omitempty"`
}

// HTTPRule specifies an allowed HTTP method, path, host, and/or
// header constraints.
type HTTPRule struct {
	// Method restricts the allowed HTTP method as an extended POSIX
	// regex (e.g. "GET", "GET|POST").
	Method string `yaml:"method,omitempty"`
	// Path restricts the allowed URL path as an extended POSIX regex
	// matched against the full path (e.g. "/v1/.*", "/api/v[12]/.*").
	Path string `yaml:"path,omitempty"`
	// Host restricts the allowed HTTP host as an extended POSIX regex
	// matched against the Host header (e.g. "api\\.example\\.com",
	// ".*\\.example\\.com").
	Host string `yaml:"host,omitempty"`
	// Headers is a list of header names that must be present in the
	// request (presence check). Each entry is a header field name;
	// the value is not inspected.
	Headers []string `yaml:"headers,omitempty"`
	// HeaderMatches specifies header name/value constraints. The
	// request must contain the named header with the specified value
	// or it is denied.
	HeaderMatches []HeaderMatch `yaml:"headerMatches,omitempty"`
}

// HeaderMatch specifies a header name/value constraint. The request
// header must have the specified value or the request is denied.
//
// Cilium also supports a Mismatch field (LOG, ADD, DELETE, REPLACE)
// for request modification instead of denial. The sandbox rejects
// configs that set Mismatch since it cannot enforce modification
// semantics.
type HeaderMatch struct {
	// Mismatch defines the action when the header value does not
	// match. The sandbox rejects any non-empty value.
	Mismatch MismatchAction `yaml:"mismatch,omitempty"`
	// Name is the header field name to match.
	Name string `yaml:"name"`
	// Value is the expected header value.
	Value string `yaml:"value,omitempty"`
}

// MismatchAction defines what happens when a [HeaderMatch] value does
// not match. The sandbox does not support mismatch actions and rejects
// configs that set one.
type MismatchAction string

const (
	// MismatchLOG logs when the header value does not match.
	MismatchLOG MismatchAction = "LOG"
	// MismatchADD adds a header when the value does not match.
	MismatchADD MismatchAction = "ADD"
	// MismatchDELETE deletes the header when it does not match.
	MismatchDELETE MismatchAction = "DELETE"
	// MismatchREPLACE replaces the header value on mismatch.
	MismatchREPLACE MismatchAction = "REPLACE"
)

// TCPForward maps a TCP port to a specific upstream host. Unlike egress
// rules (which use TLS SNI or HTTP Host filtering against the domain
// allowlist), TCP forwards create plain TCP proxy listeners with
// STRICT_DNS routing to a single host.
type TCPForward struct {
	// Host is the upstream hostname to forward traffic to.
	Host string `yaml:"host"`
	// Port is the TCP port to forward.
	Port int `yaml:"port"`
}

// SandboxConfig is the top-level YAML configuration for the sandbox firewall.
//
// The Egress field uses CiliumNetworkPolicy semantics:
//
//   - nil (absent from YAML): no egress enforcement (unrestricted).
//
//   - empty slice (egress: []): no effect, equivalent to omitting the
//     field (unrestricted); an empty list never activates enforcement.
//
//   - non-empty slice: rules apply; non-matching traffic is dropped
//     (default-deny is always active when rules are present).
//
//   - a slice containing an empty EgressRule{}: deny-all; empty selectors
//     match nothing. This matches Cilium's canonical deny-all pattern
//     described in the "Ingress/Egress Default Deny" section of the policy
//     language docs. An empty EgressRule{} has no L3 selectors (toEndpoints,
//     toCIDR, toCIDRSet, toFQDNs) and no L4/L7 selectors (toPorts), so it
//     whitelists zero traffic. The allow-all pattern in Cilium is structurally
//     different: it requires toEndpoints: [{}], where the empty
//     EndpointSelector is the wildcard that matches all endpoints.
//     See [Ingress/Egress Default Deny].
//
// [Ingress/Egress Default Deny]: https://docs.cilium.io/en/stable/security/policy/language/#ingress-egress-default-deny
type SandboxConfig struct {
	// Egress lists egress rules with FQDN, port, and CIDR selectors.
	// A nil pointer means the field was absent from YAML (unrestricted).
	// An empty slice is equivalent to nil; Cilium infers default-deny
	// from rule presence, so an empty list never activates enforcement.
	Egress *[]EgressRule `yaml:"egress,omitempty"`
	// TCPForwards lists non-TLS TCP port-to-host mappings. Each entry
	// creates a plain TCP proxy listener forwarding to the specified host.
	TCPForwards []TCPForward `yaml:"tcpForwards,omitempty"`
	// Logging enables Envoy access logs and iptables LOG targets.
	Logging bool `yaml:"logging"`
}

// EgressRules returns the egress rules slice, or nil when Egress is absent.
func (c *SandboxConfig) EgressRules() []EgressRule {
	if c.Egress == nil {
		return nil
	}

	return *c.Egress
}

// IsDefaultDenyEnabled reports whether default-deny is active for egress.
// Returns true when egress rules are present (non-nil, non-empty list).
func (c *SandboxConfig) IsDefaultDenyEnabled() bool {
	return c.Egress != nil && len(c.EgressRules()) > 0
}

// IsEgressUnrestricted reports whether egress is unrestricted,
// meaning no egress filtering should be applied.
func (c *SandboxConfig) IsEgressUnrestricted() bool {
	return c.Egress == nil || len(c.EgressRules()) == 0
}

// IsEgressBlocked reports whether all egress is blocked: default-deny
// is active and every rule has empty selectors (matching nothing).
// This is the deny-all pattern (e.g. egress: [{}]).
//
// An empty EgressRule{} contains no L3 selectors (ToFQDNs, ToCIDR, ToCIDRSet)
// and no L4 selectors (ToPorts). With no selectors present, the rule
// whitelists nothing -- it does not act as a wildcard. Cilium's own
// documentation uses egress: [{}] as the canonical deny-all example
// in the [Ingress/Egress Default Deny] section.
//
// The allow-all pattern in Cilium is structurally different and
// requires a toEndpoints selector with an empty EndpointSelector:
//
//	egress:
//	  - toEndpoints:
//	      - {}
//
// The empty EndpointSelector{} (matchLabels: {}) is the actual
// wildcard -- it matches all endpoints. This lives in Cilium's
// EgressCommonRule.ToEndpoints field (pkg/policy/api/egress.go),
// which the sandbox does not implement. The validation logic in
// pkg/policy/api/rule_validation.go (sanitize methods) confirms
// that an EgressRule with zero selectors is valid but matches no
// traffic.
//
// This method checks that every rule in the egress list has all
// selector fields empty, which is the structural equivalent of
// Cilium's deny-all.
//
// [Ingress/Egress Default Deny]: https://docs.cilium.io/en/stable/security/policy/language/#ingress-egress-default-deny
func (c *SandboxConfig) IsEgressBlocked() bool {
	if !c.IsDefaultDenyEnabled() {
		return false
	}

	rules := c.EgressRules()
	for i := range rules {
		// Unsupported selectors (ToEndpoints, ToEntities, etc.) are not
		// checked here because Validate() rejects them before this point.
		if len(rules[i].ToFQDNs) > 0 || len(rules[i].ToPorts) > 0 ||
			len(rules[i].ToCIDR) > 0 || len(rules[i].ToCIDRSet) > 0 {
			return false
		}
	}

	return true
}

// ResolvedHTTPRule is a single HTTP match pattern with an optional
// method and path. Under CiliumNetworkPolicy semantics, multiple HTTP
// rules within a toPorts entry are OR'd -- each is an independent
// match, not a cross-product.
type ResolvedHTTPRule struct {
	Method        string        // empty = any method
	Path          string        // empty = any path
	Host          string        // empty = any host
	Headers       []string      // presence-check header names
	HeaderMatches []HeaderMatch // name/value constraints (deny on mismatch)
}

// ResolvedRule bridges between the Cilium-shaped config and Envoy-shaped
// output. Each ResolvedRule represents a single domain with optional
// HTTP-level restrictions.
type ResolvedRule struct {
	Domain    string
	HTTPRules []ResolvedHTTPRule // nil = unrestricted (no L7 filtering)
}

// IsRestricted reports whether this rule requires HTTP-level inspection
// (MITM on TLS). A rule is restricted when it has non-nil HTTPRules,
// meaning L7 filtering is active.
func (r ResolvedRule) IsRestricted() bool {
	return r.HTTPRules != nil
}

// ResolvedPortProto is a resolved port with protocol and optional range.
type ResolvedPortProto struct {
	Protocol string // "tcp", "udp", "" = any (no -p flag)
	Port     int
	EndPort  int // 0 = no range
}

// ResolvedCIDR is a port-aware resolved CIDR entry. Each entry
// represents a direct IP-level allow rule that bypasses the Envoy
// proxy. Ports are inherited from the parent [EgressRule]'s toPorts;
// an empty Ports slice means any port (no L4 restriction).
// RuleIndex tracks which egress rule this CIDR came from, enabling
// per-rule iptables chains that preserve Cilium's OR semantics
// across rules.
type ResolvedCIDR struct {
	CIDR      string
	Except    []string
	Ports     []ResolvedPortProto
	RuleIndex int
}

// DefaultConfig returns a config decoded from the embedded defaults.yaml.
func DefaultConfig() *SandboxConfig {
	cfg, err := parseConfigRaw(defaultsYAML)
	if err != nil {
		panic(fmt.Sprintf("parsing embedded defaults.yaml: %v", err))
	}

	return cfg
}

// MarshalConfig returns the given config as YAML bytes.
func MarshalConfig(cfg *SandboxConfig) ([]byte, error) {
	var buf bytes.Buffer

	err := niceyaml.NewEncoder(&buf).Encode(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling config: %w", err)
	}

	return buf.Bytes(), nil
}

// MarshalDefaultConfig returns the default config as YAML bytes.
func MarshalDefaultConfig() ([]byte, error) {
	return MarshalConfig(DefaultConfig())
}

// parseConfigRaw parses a YAML sandbox config without validation.
func parseConfigRaw(data []byte) (*SandboxConfig, error) {
	var cfg SandboxConfig

	src := niceyaml.NewSourceFromBytes(data,
		niceyaml.WithDecodeOptions(yaml.DisallowUnknownField()),
	)
	dec, err := src.Decoder()
	if err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	for _, doc := range dec.Documents() {
		err := doc.Decode(&cfg)
		if err != nil {
			return nil, fmt.Errorf("parsing config: %w", err)
		}
	}

	return &cfg, nil
}

// ParseConfig parses and validates a YAML sandbox config.
func ParseConfig(data []byte) (*SandboxConfig, error) {
	cfg, err := parseConfigRaw(data)
	if err != nil {
		return nil, err
	}

	err = cfg.Validate()
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

// normalizeEgressRule mutates an egress rule in place to normalize
// protocol case, FQDN case, and trailing dots. Must be called before
// validation so that normalized values pass the validators.
func normalizeEgressRule(c *SandboxConfig, i int) {
	rule := &(*c.Egress)[i]

	for j := range rule.ToPorts {
		for k := range rule.ToPorts[j].Ports {
			rule.ToPorts[j].Ports[k].Protocol = strings.ToUpper(rule.ToPorts[j].Ports[k].Protocol)
			if isSvcName(rule.ToPorts[j].Ports[k].Port) {
				rule.ToPorts[j].Ports[k].Port = strings.ToLower(rule.ToPorts[j].Ports[k].Port)
			}
		}
	}

	for j := range rule.ToFQDNs {
		fqdn := &rule.ToFQDNs[j]
		fqdn.MatchName = strings.TrimRight(strings.ToLower(fqdn.MatchName), ".")

		fqdn.MatchPattern = strings.TrimRight(strings.ToLower(fqdn.MatchPattern), ".")
		for strings.Contains(fqdn.MatchPattern, "***") {
			fqdn.MatchPattern = strings.ReplaceAll(fqdn.MatchPattern, "***", "**")
		}
	}

	for j := range rule.ToCIDR {
		rule.ToCIDR[j] = normalizeCIDR(rule.ToCIDR[j])
	}
}

// normalizeCIDR returns s in CIDR notation. If s is already a valid
// CIDR prefix it is returned as-is. If s is a bare IP address, the
// appropriate full-length prefix is appended (/32 for IPv4, /128 for
// IPv6). If s is neither, it is returned unchanged so that downstream
// validation can produce the appropriate error.
func normalizeCIDR(s string) string {
	_, _, err := net.ParseCIDR(s)
	if err == nil {
		return s
	}

	ip := net.ParseIP(s)
	if ip == nil {
		return s
	}

	// Use string-based detection to match classifyCIDR's approach.
	// This avoids To4() returning non-nil for IPv4-mapped IPv6
	// addresses like "::ffff:10.0.0.1", which should get /128.
	if strings.Contains(s, ":") {
		return s + "/128"
	}

	return s + "/32"
}

// Validate checks that the config is internally consistent.
func (c *SandboxConfig) Validate() error {
	for i := range c.EgressRules() {
		// Reject unsupported Cilium selectors before any other
		// processing. This prevents silent data loss from fields
		// like toEntities or toEndpoints being ignored.
		// The unsupported fields don't need normalization, so this
		// can safely run first.
		err := validateUnsupportedSelectors((*c.Egress)[i], i)
		if err != nil {
			return err
		}

		// Normalize before validation so that e.g. "tcp" passes
		// the uppercase protocol check and "GitHub.COM." passes
		// FQDN validation as "github.com".
		normalizeEgressRule(c, i)

		rule := (*c.Egress)[i]

		// An empty EgressRule{} is valid: it triggers default-deny
		// with empty selectors (deny-all pattern).
		err = validateFQDNSelectors(rule, i)
		if err != nil {
			return err
		}

		hasFQDNs := len(rule.ToFQDNs) > 0

		err = validateFQDNConstraints(rule, i, hasFQDNs)
		if err != nil {
			return err
		}

		// L3 mutual exclusivity: Cilium's EgressCommonRule.sanitize()
		// rejects any rule combining two different L3 selector fields.
		// Since our EgressRule only has ToCIDR and ToCIDRSet as L3
		// selectors, this is the only pair we need to check. ToFQDNs
		// was already validated above by validateFQDNConstraints.
		if len(rule.ToCIDR) > 0 && len(rule.ToCIDRSet) > 0 {
			return fmt.Errorf("%w: rule %d", ErrCIDRAndCIDRSetMixed, i)
		}

		err = validatePorts(rule, i)
		if err != nil {
			return err
		}

		for _, cidr := range rule.ToCIDR {
			_, _, parseErr := net.ParseCIDR(cidr)
			if parseErr != nil {
				return fmt.Errorf("%w: rule %d cidr %q", ErrCIDRInvalid, i, cidr)
			}
		}

		err = validateL7Rules(rule, i, hasFQDNs)
		if err != nil {
			return err
		}

		err = validateCIDRSets(rule, i)
		if err != nil {
			return err
		}
	}

	// TCPForwards require egress rules to route traffic. If egress is
	// blocked, forwards would silently do nothing.
	if c.IsEgressBlocked() && len(c.TCPForwards) > 0 {
		return ErrTCPForwardRequiresEgress
	}

	resolvedPorts := c.ResolvePorts()

	portsSet := make(map[int]bool, len(resolvedPorts))
	for _, p := range resolvedPorts {
		if p > maxProxyablePort {
			return fmt.Errorf("%w: %d", ErrPortExceedsProxyRange, p)
		}

		portsSet[p] = true
	}

	seen := make(map[int]bool)
	for _, fwd := range c.TCPForwards {
		if fwd.Port <= 0 || fwd.Host == "" {
			return fmt.Errorf("%w: port=%d host=%q", ErrInvalidTCPForward, fwd.Port, fwd.Host)
		}

		if fwd.Port > maxProxyablePort {
			return fmt.Errorf("%w: %d", ErrPortExceedsProxyRange, fwd.Port)
		}

		if seen[fwd.Port] {
			return fmt.Errorf("%w: %d", ErrDuplicateTCPForwardPort, fwd.Port)
		}

		seen[fwd.Port] = true
		if portsSet[fwd.Port] {
			return fmt.Errorf("%w: %d", ErrTCPForwardPortConflict, fwd.Port)
		}
	}

	return nil
}

// validateFQDNSelectors checks that each FQDN selector in a rule has
// exactly one of matchName or matchPattern, and that patterns use only
// leading wildcards. Cilium supports three wildcard forms:
//
//   - "*"        -- match all FQDNs
//   - "*.suffix" -- single-label wildcard (one subdomain level)
//   - "**.suffix" -- multi-label wildcard (arbitrary subdomain depth)
//
// validateUnsupportedSelectors checks whether any Cilium selectors that
// the sandbox does not implement are present in the rule. Returns an
// error for the first unsupported selector found, with the rule index
// and field name for diagnostics.
func validateUnsupportedSelectors(rule EgressRule, ruleIdx int) error {
	type field struct {
		name string
		set  bool
	}

	fields := []field{
		{"toEndpoints", len(rule.ToEndpoints) > 0},
		{"toEntities", len(rule.ToEntities) > 0},
		{"toServices", len(rule.ToServices) > 0},
		{"toNodes", len(rule.ToNodes) > 0},
		{"toGroups", len(rule.ToGroups) > 0},
		{"toRequires", len(rule.ToRequires) > 0},
		{"icmps", len(rule.ICMPs) > 0},
		{"authentication", rule.Authentication != nil},
	}

	for _, f := range fields {
		if f.set {
			return fmt.Errorf(
				"%w: rule %d has %s, which is not implemented by the sandbox",
				ErrUnsupportedSelector, ruleIdx, f.name,
			)
		}
	}

	return nil
}

// Cilium treats 2+ stars identically ([*]{2,} in its regex), so runs
// of 3+ stars are normalized to ** before validation.
func validateFQDNSelectors(rule EgressRule, ruleIdx int) error {
	for j, fqdn := range rule.ToFQDNs {
		if fqdn.MatchName == "" && fqdn.MatchPattern == "" {
			return fmt.Errorf("%w: rule %d selector %d", ErrFQDNSelectorEmpty, ruleIdx, j)
		}

		if fqdn.MatchName != "" && fqdn.MatchPattern != "" {
			return fmt.Errorf("%w: rule %d selector %d", ErrFQDNSelectorAmbiguous, ruleIdx, j)
		}

		// Character and length validation, matching Cilium's
		// FQDNSelector.sanitize() and matchpattern.prevalidate().
		if fqdn.MatchName != "" {
			if len(fqdn.MatchName) > maxFQDNLength {
				return fmt.Errorf("%w: rule %d selector %d name %q (%d chars)",
					ErrFQDNTooLong, ruleIdx, j, fqdn.MatchName, len(fqdn.MatchName))
			}

			if !allowedMatchNameChars.MatchString(fqdn.MatchName) {
				return fmt.Errorf("%w: rule %d selector %d name %q",
					ErrFQDNNameInvalidChars, ruleIdx, j, fqdn.MatchName)
			}
		}

		if fqdn.MatchPattern != "" {
			if len(fqdn.MatchPattern) > maxFQDNLength {
				return fmt.Errorf("%w: rule %d selector %d pattern %q (%d chars)",
					ErrFQDNTooLong, ruleIdx, j, fqdn.MatchPattern, len(fqdn.MatchPattern))
			}

			if !allowedMatchPatternChars.MatchString(fqdn.MatchPattern) {
				return fmt.Errorf("%w: rule %d selector %d pattern %q",
					ErrFQDNPatternInvalidChars, ruleIdx, j, fqdn.MatchPattern)
			}
		}

		p := fqdn.MatchPattern
		if p == "" {
			continue
		}

		switch {
		case p == "*" || p == "**":
			// Bare wildcards: match all FQDNs.
		case strings.HasPrefix(p, "**."):
			// Multi-label wildcard. The remainder after "**." must be
			// wildcard-free.
			if strings.Contains(p[3:], "*") {
				return fmt.Errorf("%w: rule %d selector %d pattern %q",
					ErrFQDNPatternPartialWildcard, ruleIdx, j, fqdn.MatchPattern)
			}

		case strings.HasPrefix(p, "*."):
			// Single-label wildcard. The remainder after "*." must be
			// wildcard-free.
			if strings.Contains(p[2:], "*") {
				return fmt.Errorf("%w: rule %d selector %d pattern %q",
					ErrFQDNPatternPartialWildcard, ruleIdx, j, fqdn.MatchPattern)
			}

		case strings.Contains(p, "*"):
			// Wildcard not in a valid leading position.
			return fmt.Errorf("%w: rule %d selector %d pattern %q",
				ErrFQDNPatternPartialWildcard, ruleIdx, j, fqdn.MatchPattern)
		}
	}

	return nil
}

// validateFQDNConstraints checks cross-selector constraints when a rule
// has toFQDNs: no CIDR mixing, and explicit ports are required.
func validateFQDNConstraints(rule EgressRule, ruleIdx int, hasFQDNs bool) error {
	if !hasFQDNs {
		return nil
	}

	if len(rule.ToCIDR) > 0 || len(rule.ToCIDRSet) > 0 {
		return fmt.Errorf("%w: rule %d", ErrFQDNWithCIDR, ruleIdx)
	}

	hasExplicitPorts := false
	for _, pr := range rule.ToPorts {
		if len(pr.Ports) > 0 {
			hasExplicitPorts = true

			break
		}
	}

	if !hasExplicitPorts {
		return fmt.Errorf("%w: rule %d", ErrFQDNRequiresPorts, ruleIdx)
	}

	return nil
}

// validatePorts checks that each port entry has a valid port number,
// protocol, and endPort configuration.
func validatePorts(rule EgressRule, ruleIdx int) error {
	for _, pr := range rule.ToPorts {
		hasWildcardPort := false

		for _, p := range pr.Ports {
			if p.Port == "" {
				return fmt.Errorf("%w: rule %d", ErrPortEmpty, ruleIdx)
			}

			n, err := ResolvePort(p.Port)
			if err != nil {
				return fmt.Errorf("%w: rule %d port %q", ErrPortInvalid, ruleIdx, p.Port)
			}

			if n == 0 {
				hasWildcardPort = true

				// EndPort is nonsensical with wildcard port.
				if p.EndPort > 0 {
					return fmt.Errorf("%w: rule %d port %q", ErrEndPortWithWildcardPort, ruleIdx, p.Port)
				}
			}

			if !validProtocols[p.Protocol] {
				return fmt.Errorf("%w: rule %d port %q protocol %q", ErrProtocolInvalid, ruleIdx, p.Port, p.Protocol)
			}

			if n > 0 {
				err = validateEndPort(p, int(n), ruleIdx)
				if err != nil {
					return err
				}
			}
		}

		err := validateHTTPRules(pr, ruleIdx)
		if err != nil {
			return err
		}

		if pr.Rules != nil && len(pr.Rules.HTTP) > 0 {
			if len(pr.Ports) == 0 || hasWildcardPort {
				return fmt.Errorf("%w: rule %d", ErrL7WithWildcardPort, ruleIdx)
			}

			for _, p := range pr.Ports {
				if p.Protocol != "" && p.Protocol != "TCP" {
					return fmt.Errorf("%w: rule %d port %s protocol %s",
						ErrL7RequiresTCP, ruleIdx, p.Port, p.Protocol)
				}
			}
		}
	}

	return nil
}

// validateEndPort checks endPort constraints for a single port entry.
func validateEndPort(p Port, portNum, ruleIdx int) error {
	if p.EndPort == 0 {
		return nil
	}

	if p.EndPort > 65535 {
		return fmt.Errorf("%w: rule %d endPort %d exceeds 65535", ErrEndPortInvalid, ruleIdx, p.EndPort)
	}

	if isSvcName(p.Port) {
		return fmt.Errorf("%w: rule %d port %q", ErrEndPortWithNamedPort, ruleIdx, p.Port)
	}

	if p.EndPort < portNum {
		return fmt.Errorf("%w: rule %d port %q endPort %d", ErrEndPortInvalid, ruleIdx, p.Port, p.EndPort)
	}

	return nil
}

// validateHTTPRules checks that HTTP rule path and method patterns are
// valid regular expressions.
func validateHTTPRules(pr PortRule, ruleIdx int) error {
	if pr.Rules == nil {
		return nil
	}

	for _, h := range pr.Rules.HTTP {
		if h.Host != "" {
			if len(h.Host) > maxRegexLen {
				return fmt.Errorf(
					"%w: rule %d host too long (%d > %d)",
					ErrHostInvalidRegex,
					ruleIdx,
					len(h.Host),
					maxRegexLen,
				)
			}

			_, err := regexp.Compile(h.Host)
			if err != nil {
				return fmt.Errorf("%w: rule %d host %q", ErrHostInvalidRegex, ruleIdx, h.Host)
			}
		}

		for i, hdr := range h.Headers {
			if hdr == "" {
				return fmt.Errorf("%w: rule %d headers[%d]", ErrHTTPHeaderEmpty, ruleIdx, i)
			}
		}

		for i, hm := range h.HeaderMatches {
			if hm.Name == "" {
				return fmt.Errorf("%w: rule %d headerMatches[%d]", ErrHeaderMatchNameEmpty, ruleIdx, i)
			}

			if hm.Mismatch != "" {
				return fmt.Errorf("%w: rule %d headerMatches[%d] mismatch %q",
					ErrHeaderMatchMismatchAction, ruleIdx, i, hm.Mismatch)
			}
		}

		if h.Path != "" {
			if len(h.Path) > maxRegexLen {
				return fmt.Errorf(
					"%w: rule %d path too long (%d > %d)",
					ErrPathInvalidRegex,
					ruleIdx,
					len(h.Path),
					maxRegexLen,
				)
			}

			_, err := regexp.Compile(h.Path)
			if err != nil {
				return fmt.Errorf("%w: rule %d path %q", ErrPathInvalidRegex, ruleIdx, h.Path)
			}
		}

		if h.Method != "" {
			if len(h.Method) > maxRegexLen {
				return fmt.Errorf(
					"%w: rule %d method too long (%d > %d)",
					ErrMethodInvalidRegex,
					ruleIdx,
					len(h.Method),
					maxRegexLen,
				)
			}

			_, err := regexp.Compile(h.Method)
			if err != nil {
				return fmt.Errorf("%w: rule %d method %q", ErrMethodInvalidRegex, ruleIdx, h.Method)
			}
		}
	}

	return nil
}

// validateL7Rules checks that L7 rules are only used with toFQDNs
// and that wildcard matchPatterns are not combined with L7 rules.
func validateL7Rules(rule EgressRule, ruleIdx int, hasFQDNs bool) error {
	hasL7 := false
	for _, pr := range rule.ToPorts {
		if pr.Rules != nil && len(pr.Rules.HTTP) > 0 {
			if !hasFQDNs {
				return fmt.Errorf("%w: rule %d", ErrL7RequiresFQDN, ruleIdx)
			}

			hasL7 = true
		}
	}

	// Wildcard matchPatterns break MITM cert paths.
	if hasL7 {
		for j, fqdn := range rule.ToFQDNs {
			if strings.Contains(fqdn.MatchPattern, "*") {
				return fmt.Errorf("%w: rule %d selector %d", ErrWildcardWithL7, ruleIdx, j)
			}
		}
	}

	return nil
}

// validateCIDRSets checks that each CIDR set entry is valid and that
// except entries are subnets of the parent CIDR.
func validateCIDRSets(rule EgressRule, ruleIdx int) error {
	for _, cidr := range rule.ToCIDRSet {
		_, parentNet, err := net.ParseCIDR(cidr.CIDR)
		if err != nil {
			return fmt.Errorf("%w: rule %d cidr %q", ErrCIDRInvalid, ruleIdx, cidr.CIDR)
		}

		parentOnes, _ := parentNet.Mask.Size()
		for _, exc := range cidr.Except {
			_, excNet, err := net.ParseCIDR(exc)
			if err != nil {
				return fmt.Errorf("%w: rule %d except %q", ErrCIDRInvalid, ruleIdx, exc)
			}

			excOnes, _ := excNet.Mask.Size()
			if !parentNet.Contains(excNet.IP) || excOnes < parentOnes {
				return fmt.Errorf("%w: rule %d except %q not in %q", ErrExceptNotSubnet, ruleIdx, exc, cidr.CIDR)
			}
		}
	}

	return nil
}

// TCPForwardHosts returns a deduplicated, sorted list of hostnames from
// the config's [TCPForward] entries.
func (c *SandboxConfig) TCPForwardHosts() []string {
	seen := make(map[string]bool)

	var hosts []string
	for _, fwd := range c.TCPForwards {
		if !seen[fwd.Host] {
			seen[fwd.Host] = true
			hosts = append(hosts, fwd.Host)
		}
	}

	sort.Strings(hosts)

	return hosts
}

// ResolveRulesForPort returns resolved rules scoped to a specific port.
// Only rules whose toPorts match the given port (or that have no
// toPorts, meaning all ports) are included. L7 rules are extracted
// only from matching [PortRule] entries. When the same domain appears
// in multiple matching rules, L7 constraints are merged using OR
// semantics; if any occurrence has no L7 rules, the merged result has
// none (unrestricted wins).
//
// Note: matchPattern values (e.g. "*.example.com", "**.example.com")
// are preserved as domain keys. Envoy server_names uses suffix-based
// matching, which would match arbitrarily deep subdomains; an RBAC
// network filter (see [buildWildcardRBACFilter]) is prepended to
// wildcard filter chains to enforce the correct depth (single-label
// for *, multi-label for **), matching CiliumNetworkPolicy semantics.
func (c *SandboxConfig) ResolveRulesForPort(port int) []ResolvedRule {
	type merged struct {
		httpRules    []ResolvedHTTPRule
		unrestricted bool
	}

	byDomain := make(map[string]*merged)

	var order []string

	egressRules := c.EgressRules()
	for i := range egressRules {
		if len(egressRules[i].ToFQDNs) == 0 {
			continue
		}

		matched, hasPlainL4, httpRules := matchRuleForPort(egressRules[i], port)
		if !matched {
			continue
		}

		for _, fqdn := range egressRules[i].ToFQDNs {
			domain := fqdn.MatchName
			if domain == "" {
				domain = fqdn.MatchPattern
				// Bare "**" is equivalent to "*" (match all FQDNs).
				if domain == "**" {
					domain = "*"
				}
			}

			m, exists := byDomain[domain]
			if !exists {
				m = &merged{}
				byDomain[domain] = m
				order = append(order, domain)
			}

			if hasPlainL4 {
				// Plain L4 on this port nullifies L7 for this
				// EgressRule. Across EgressRules, unrestricted
				// wins (OR semantics).
				m.unrestricted = true
			} else {
				m.httpRules = append(m.httpRules, httpRules...)
			}
		}
	}

	sort.Strings(order)

	result := make([]ResolvedRule, 0, len(order))
	for _, d := range order {
		m := byDomain[d]
		r := ResolvedRule{Domain: d}

		if !m.unrestricted && len(m.httpRules) > 0 {
			// Deduplicate HTTP rules by {method, path, host}.
			seen := make(map[[3]string]bool, len(m.httpRules))
			for _, hr := range m.httpRules {
				k := [3]string{hr.Method, hr.Path, hr.Host}
				if !seen[k] {
					seen[k] = true

					r.HTTPRules = append(r.HTTPRules, hr)
				}
			}

			sort.Slice(r.HTTPRules, func(i, j int) bool {
				if r.HTTPRules[i].Path != r.HTTPRules[j].Path {
					return r.HTTPRules[i].Path < r.HTTPRules[j].Path
				}

				if r.HTTPRules[i].Method != r.HTTPRules[j].Method {
					return r.HTTPRules[i].Method < r.HTTPRules[j].Method
				}

				return r.HTTPRules[i].Host < r.HTTPRules[j].Host
			})
		}

		result = append(result, r)
	}

	return result
}

// matchRuleForPort determines whether an egress rule applies to the
// given port and collects L7 rules from matching toPorts entries.
//
// Two-way distinction for PortRule.Rules:
//   - Rules == nil, Rules.HTTP == nil, or len(HTTP) == 0: plain L4, no L7
//     inspection.
//   - Rules.HTTP non-empty: L7 active with rules.
//
// Cilium semantics: if ANY matching PortRule for this port has no L7
// rules (plain L4), it nullifies sibling L7 rules on the same port
// within this EgressRule.
func matchRuleForPort(rule EgressRule, port int) (bool, bool, []ResolvedHTTPRule) {
	if len(rule.ToPorts) == 0 {
		// No toPorts: domain allowed on all ports, no L7
		// restrictions from this rule.
		return true, true, nil
	}

	var (
		matched    bool
		hasPlainL4 bool
		httpRules  []ResolvedHTTPRule
	)

	for _, pr := range rule.ToPorts {
		if !portRuleMatchesPort(pr, port) {
			continue
		}

		matched = true

		if pr.Rules == nil || pr.Rules.HTTP == nil || len(pr.Rules.HTTP) == 0 {
			// Plain L4 rule on this port: nullifies all
			// sibling L7 for this EgressRule. Only
			// TCP-compatible entries can nullify L7 (HTTP
			// inspection is TCP-only). A UDP/443 entry must
			// not cancel TCP/443 L7 rules.
			if portRuleHasTCPPort(pr, port) {
				hasPlainL4 = true
			}
		} else {
			for _, h := range pr.Rules.HTTP {
				httpRules = append(httpRules, ResolvedHTTPRule(h))
			}
		}
	}

	return matched, hasPlainL4, httpRules
}

// portRuleMatchesPort reports whether a port rule matches a specific
// port number. An empty Ports list matches all ports (Cilium semantics
// for L7-only toPorts). When EndPort is set, the rule matches any port
// in the range [Port, EndPort] inclusive.
func portRuleMatchesPort(pr PortRule, port int) bool {
	if len(pr.Ports) == 0 {
		return true
	}

	for _, p := range pr.Ports {
		resolved, err := ResolvePort(p.Port)
		if err != nil {
			continue
		}

		n := int(resolved)

		// Port 0 is a wildcard: matches any target port.
		if n == 0 {
			return true
		}

		if p.EndPort > 0 && port >= n && port <= p.EndPort {
			return true
		}

		if n == port {
			return true
		}
	}

	return false
}

// portRuleHasTCPPort reports whether a [PortRule] contains a
// TCP-compatible port entry matching the given port number. An entry is
// TCP-compatible when its protocol is TCP, ANY, or empty (the default).
// This prevents non-TCP entries (UDP, SCTP) from nullifying TCP L7
// rules during intra-rule L7 resolution, matching Cilium's per-(port,
// protocol) L4Filter semantics.
func portRuleHasTCPPort(pr PortRule, port int) bool {
	if len(pr.Ports) == 0 {
		// No ports list means L7-only toPorts entry, which is
		// implicitly TCP (HTTP inspection requires TCP).
		return true
	}

	for _, p := range pr.Ports {
		proto := normalizeProtocol(p.Protocol)
		if proto != "" && proto != protoTCP {
			continue // UDP, SCTP -- skip
		}

		resolved, err := ResolvePort(p.Port)
		if err != nil {
			continue
		}

		n := int(resolved)

		if n == 0 {
			return true
		}

		if p.EndPort > 0 && port >= n && port <= p.EndPort {
			return true
		}

		if n == port {
			return true
		}
	}

	return false
}

// ResolveRules converts egress rules into a flat, deduplicated, sorted
// list of [ResolvedRule] across all resolved ports. Delegates to
// [SandboxConfig.ResolveRulesForPort] for each port from
// [SandboxConfig.ResolvePorts] and unions the results. When a domain
// appears unrestricted on any port, the global result is unrestricted.
func (c *SandboxConfig) ResolveRules() []ResolvedRule {
	ports := c.ResolvePorts()

	type merged struct {
		httpRules    map[[3]string]ResolvedHTTPRule
		unrestricted bool
	}

	byDomain := make(map[string]*merged)

	var order []string

	for _, port := range ports {
		portRules := c.ResolveRulesForPort(port)

		for _, r := range portRules {
			m, exists := byDomain[r.Domain]
			if !exists {
				m = &merged{
					httpRules: make(map[[3]string]ResolvedHTTPRule),
				}
				byDomain[r.Domain] = m
				order = append(order, r.Domain)
			}

			if !r.IsRestricted() {
				m.unrestricted = true
			}

			for _, hr := range r.HTTPRules {
				k := [3]string{hr.Method, hr.Path, hr.Host}
				m.httpRules[k] = hr
			}
		}
	}

	sort.Strings(order)

	result := make([]ResolvedRule, 0, len(order))
	for _, d := range order {
		m := byDomain[d]
		r := ResolvedRule{Domain: d}
		if !m.unrestricted && len(m.httpRules) > 0 {
			r.HTTPRules = make([]ResolvedHTTPRule, 0, len(m.httpRules))
			for _, hr := range m.httpRules {
				r.HTTPRules = append(r.HTTPRules, hr)
			}

			sort.Slice(r.HTTPRules, func(i, j int) bool {
				if r.HTTPRules[i].Path != r.HTTPRules[j].Path {
					return r.HTTPRules[i].Path < r.HTTPRules[j].Path
				}

				if r.HTTPRules[i].Method != r.HTTPRules[j].Method {
					return r.HTTPRules[i].Method < r.HTTPRules[j].Method
				}

				return r.HTTPRules[i].Host < r.HTTPRules[j].Host
			})
		}

		result = append(result, r)
	}

	return result
}

// ResolveDomains resolves all egress rules into a flat, deduplicated,
// sorted domain list.
func (c *SandboxConfig) ResolveDomains() []string {
	rules := c.ResolveRules()

	domains := make([]string, len(rules))
	for i, r := range rules {
		domains[i] = r.Domain
	}

	return domains
}

// ResolvePorts collects port numbers for Envoy listeners from egress
// rules. Returns nil when egress is unrestricted or blocked, since
// neither mode needs Envoy FQDN listeners. CIDR-only rules
// (toCIDRSet without toFQDNs) are skipped because they bypass Envoy.
// Ports that are exclusively non-TCP (e.g. UDP-only or SCTP-only) are
// excluded since Envoy only creates TCP listeners. Ports from
// FQDN-bearing rules and single-port toPorts-only rules (no L3
// selectors) both contribute to the resolved set. Open-port ranges
// (endPort > 0 on toPorts-only rules) are skipped because they
// bypass Envoy via direct iptables ACCEPT. Returns a sorted,
// deduplicated list.
//
// Non-TCP ports from FQDN rules are handled separately by
// [ResolveFQDNNonTCPPorts] and enforced via ipset-backed iptables rules.
func (c *SandboxConfig) ResolvePorts() []int {
	if c.IsEgressUnrestricted() {
		return nil
	}

	rules := c.EgressRules()
	if rules == nil {
		return nil
	}

	seen := make(map[int]bool)
	for ri := range rules {
		// Empty rules have no selectors, so they contribute
		// nothing to Envoy listeners.
		if len(rules[ri].ToFQDNs) == 0 && len(rules[ri].ToPorts) == 0 &&
			len(rules[ri].ToCIDR) == 0 && len(rules[ri].ToCIDRSet) == 0 {
			continue
		}

		// CIDR-only rules bypass Envoy; their ports don't need
		// Envoy listeners. Validation prevents FQDN+CIDR
		// combinations, so this is equivalent to "not an FQDN rule".
		if len(rules[ri].ToCIDRSet) > 0 || len(rules[ri].ToCIDR) > 0 {
			continue
		}

		isOpenPortRule := len(rules[ri].ToFQDNs) == 0

		// Explicit ports from FQDN and open-port rules contribute.
		// Validation ensures FQDN rules always have toPorts.
		// Skip ports that are exclusively non-TCP (e.g. UDP-only
		// or SCTP-only), since Envoy only handles TCP listeners.
		// Open-port ranges bypass Envoy via direct iptables ACCEPT;
		// they don't need REDIRECT listeners.
		for _, pr := range rules[ri].ToPorts {
			for _, p := range pr.Ports {
				proto := normalizeProtocol(p.Protocol)
				if proto != "" && proto != protoTCP {
					continue
				}

				if isOpenPortRule && p.EndPort > 0 {
					continue
				}

				resolved, err := ResolvePort(p.Port)
				if err == nil && resolved > 0 {
					seen[int(resolved)] = true
				}
			}
		}
	}

	result := make([]int, 0, len(seen))
	for p := range seen {
		result = append(result, p)
	}

	sort.Ints(result)
	if len(result) == 0 {
		return nil
	}

	return result
}

// ExtraPorts returns resolved ports that are not in [DefaultPorts]
// (80 and 443), since those have dedicated redirect rules.
func (c *SandboxConfig) ExtraPorts() []int {
	var extra []int
	for _, p := range c.ResolvePorts() {
		if p != 80 && p != 443 {
			extra = append(extra, p)
		}
	}

	return extra
}

// ResolvedOpenPort is a resolved open port with its normalized protocol.
type ResolvedOpenPort struct {
	Protocol string
	Port     int
	EndPort  int // 0 = no range
}

// HasUnrestrictedOpenPorts reports whether any port-only egress rule
// (no toFQDNs, toCIDR, or toCIDRSet) contains a toPorts entry with
// an empty Ports list. Under Cilium semantics, empty Ports means "all
// ports"; combined with the implicit wildcard L3 (no L3 selector),
// this allows all traffic. The sandbox represents this as an
// unrestricted ACCEPT for the user UID in iptables, which subsumes
// CIDR except DROPs and CIDR ACCEPTs from other rules (Cilium OR
// semantics across rules).
func (c *SandboxConfig) HasUnrestrictedOpenPorts() bool {
	eRules := c.EgressRules()
	for ri := range eRules {
		if len(eRules[ri].ToFQDNs) > 0 || len(eRules[ri].ToCIDR) > 0 || len(eRules[ri].ToCIDRSet) > 0 {
			continue
		}

		for _, pr := range eRules[ri].ToPorts {
			if len(pr.Ports) == 0 {
				return true
			}

			for _, p := range pr.Ports {
				n, err := ResolvePort(p.Port)
				if err != nil || n == 0 {
					return true
				}
			}
		}
	}

	return false
}

// ResolveOpenPortRules returns resolved open port entries from rules
// that have toPorts but neither toFQDNs nor toCIDRSet. These ports
// allow all destinations (passthrough without domain filtering). ANY
// protocol is expanded into separate tcp and udp entries.
func (c *SandboxConfig) ResolveOpenPortRules() []ResolvedOpenPort {
	seen := make(map[string]bool)

	var result []ResolvedOpenPort

	openRules := c.EgressRules()
	for ri := range openRules {
		if len(openRules[ri].ToFQDNs) > 0 || len(openRules[ri].ToCIDR) > 0 || len(openRules[ri].ToCIDRSet) > 0 {
			continue
		}

		for _, pr := range openRules[ri].ToPorts {
			for _, p := range pr.Ports {
				resolved, err := ResolvePort(p.Port)
				if err != nil || resolved == 0 {
					continue
				}

				n := int(resolved)
				proto := normalizeProtocol(p.Protocol)
				protos := []string{proto}
				if proto == "" {
					protos = []string{protoTCP, protoUDP}
				}

				for _, pr := range protos {
					k := pr + "/" + strconv.Itoa(n) + "/" + strconv.Itoa(p.EndPort)
					if !seen[k] {
						seen[k] = true

						result = append(result, ResolvedOpenPort{Port: n, EndPort: p.EndPort, Protocol: pr})
					}
				}
			}
		}
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Port != result[j].Port {
			return result[i].Port < result[j].Port
		}

		if result[i].EndPort != result[j].EndPort {
			return result[i].EndPort < result[j].EndPort
		}

		return result[i].Protocol < result[j].Protocol
	})

	return result
}

// ResolveOpenPorts returns sorted, deduplicated port numbers from open
// port rules. This is a convenience wrapper over [ResolveOpenPortRules]
// used by Envoy listener creation where only port numbers matter.
func (c *SandboxConfig) ResolveOpenPorts() []int {
	openRules := c.ResolveOpenPortRules()
	seen := make(map[int]bool, len(openRules))

	var result []int
	for _, r := range openRules {
		if !seen[r.Port] {
			seen[r.Port] = true
			result = append(result, r.Port)
		}
	}

	return result
}

// FQDNRulePorts groups resolved non-TCP ports for a single FQDN
// egress rule. Each rule gets its own ipset pair, matching Cilium's
// per-selector isolation semantics.
type FQDNRulePorts struct {
	Ports     []ResolvedOpenPort
	RuleIndex int
}

// ruleHasNonTCPPorts reports whether an egress rule has any ports with
// a non-TCP protocol (UDP, SCTP, or ANY which expands to UDP).
func ruleHasNonTCPPorts(rule EgressRule) bool {
	for _, pr := range rule.ToPorts {
		for _, p := range pr.Ports {
			proto := normalizeProtocol(p.Protocol)
			if proto != protoTCP {
				return true
			}
		}
	}

	return false
}

// ResolveFQDNNonTCPPorts returns resolved UDP port entries from FQDN
// rules, grouped per rule. Each qualifying rule (FQDN selectors with
// non-TCP ports) gets its own [FQDNRulePorts] entry so iptables can
// reference per-rule ipsets. These ports cannot use Envoy (TCP-only)
// and are instead enforced via ipset-backed iptables rules that
// restrict traffic to DNS-resolved IPs. ANY protocol is expanded into
// udp entries (TCP is handled by [ResolvePorts] + Envoy; SCTP
// requires explicit opt-in). Returns nil when egress is unrestricted,
// blocked, or has no FQDN rules with non-TCP ports.
func (c *SandboxConfig) ResolveFQDNNonTCPPorts() []FQDNRulePorts {
	if c.IsEgressUnrestricted() {
		return nil
	}

	rules := c.EgressRules()
	if rules == nil {
		return nil
	}

	var result []FQDNRulePorts

	ruleIdx := 0

	for ri := range rules {
		if len(rules[ri].ToFQDNs) == 0 || !ruleHasNonTCPPorts(rules[ri]) {
			continue
		}

		seen := make(map[string]bool)

		var ports []ResolvedOpenPort

		for _, pr := range rules[ri].ToPorts {
			for _, p := range pr.Ports {
				resolved, err := ResolvePort(p.Port)
				if err != nil || resolved == 0 {
					continue
				}

				n := int(resolved)
				proto := normalizeProtocol(p.Protocol)

				// TCP is handled by Envoy via ResolvePorts.
				var protos []string

				switch proto {
				case protoTCP:
					continue
				case protoUDP, protoSCTP:
					protos = []string{proto}
				case "":
					// ANY: expand to non-TCP protocols only.
					// SCTP requires explicit opt-in (Cilium: sctp.enabled=true).
					protos = []string{protoUDP}
				}

				for _, pr := range protos {
					k := pr + "/" + strconv.Itoa(n) + "/" + strconv.Itoa(p.EndPort)
					if !seen[k] {
						seen[k] = true

						ports = append(ports, ResolvedOpenPort{Port: n, EndPort: p.EndPort, Protocol: pr})
					}
				}
			}
		}

		if len(ports) > 0 {
			sort.Slice(ports, func(i, j int) bool {
				if ports[i].Port != ports[j].Port {
					return ports[i].Port < ports[j].Port
				}

				if ports[i].EndPort != ports[j].EndPort {
					return ports[i].EndPort < ports[j].EndPort
				}

				return ports[i].Protocol < ports[j].Protocol
			})

			result = append(result, FQDNRulePorts{RuleIndex: ruleIdx, Ports: ports})
		}

		ruleIdx++
	}

	return result
}

// HasFQDNNonTCPPorts reports whether the config contains any FQDN
// rules with non-TCP ports that need ipset-backed iptables rules.
func (c *SandboxConfig) HasFQDNNonTCPPorts() bool {
	return len(c.ResolveFQDNNonTCPPorts()) > 0
}

// FQDNPattern pairs an FQDN selector with its compiled regex for DNS
// response filtering. Patterns follow Cilium's matchpattern semantics:
// [FQDNSelector.MatchName] compiles to an exact match, single "*"
// matches one DNS label, "**." prefix matches one or more labels.
//
// For FQDN-form regexes (with trailing dot). For SNI/Host regexes
// without trailing dot, see [wildcardToSNIRegex] and [wildcardToHostRegex].
type FQDNPattern struct {
	Regex     *regexp.Regexp
	Original  string
	RuleIndex int
}

// CompileFQDNPatterns returns compiled regexes for all [FQDNSelector]
// entries in FQDN rules that have non-TCP ports. Each pattern carries
// a RuleIndex matching the index used by [ResolveFQDNNonTCPPorts], so
// DNS responses can populate the correct per-rule ipset. Patterns are
// deduplicated within each rule (same selector appearing twice in one
// rule produces one entry) but not across rules (same selector in two
// rules produces two entries with different RuleIndex values).
// [TCPForward] hosts are excluded (they use Envoy, not ipset
// filtering).
func (c *SandboxConfig) CompileFQDNPatterns() []FQDNPattern {
	var patterns []FQDNPattern

	ruleIdx := 0

	patRules := c.EgressRules()
	for ri := range patRules {
		if len(patRules[ri].ToFQDNs) == 0 || !ruleHasNonTCPPorts(patRules[ri]) {
			continue
		}

		seen := make(map[string]bool)

		for _, fqdn := range patRules[ri].ToFQDNs {
			var original string

			var isMatchName bool

			if fqdn.MatchName != "" {
				original = fqdn.MatchName
				isMatchName = true
			} else {
				original = fqdn.MatchPattern
			}

			if seen[original] {
				continue
			}

			seen[original] = true

			regex := patternToAnchoredRegex(original, isMatchName)
			patterns = append(patterns, FQDNPattern{
				Original:  original,
				Regex:     regexp.MustCompile(regex),
				RuleIndex: ruleIdx,
			})
		}

		ruleIdx++
	}

	return patterns
}

// patternToAnchoredRegex converts an FQDN selector value into an
// anchored regex that matches FQDN-form names (with trailing dot).
// Follows Cilium's matchpattern.ToAnchoredRegexp, including the
// "**." prefix (multi-label depth matching via Cilium's
// subdomainWildcardSpecifierPrefix in matchpattern.go).
func patternToAnchoredRegex(pattern string, isMatchName bool) string {
	if isMatchName {
		escaped := strings.ReplaceAll(pattern, ".", "[.]")

		return "^" + escaped + "[.]$"
	}

	// Collapse runs of 3+ stars to ** so that e.g. "***.example.com"
	// is treated identically to "**.example.com" (Cilium equivalence).
	for strings.Contains(pattern, "***") {
		pattern = strings.ReplaceAll(pattern, "***", "**")
	}

	if isBareWildcard(pattern) {
		return `(^([-a-zA-Z0-9_]+[.])+$)|(^[.]$)`
	}

	// "**." prefix: one or more dot-separated DNS labels followed by
	// the fixed suffix. Matches arbitrary depth (a.b.c.suffix.).
	if strings.HasPrefix(pattern, "**.") {
		suffix := pattern[3:]
		escaped := strings.ReplaceAll(suffix, ".", "[.]")

		return `^([-a-zA-Z0-9_]+([.][-a-zA-Z0-9_]+){0,})[.]` + escaped + `[.]$`
	}

	// Standard Cilium: each "." becomes "[.]", each "*" becomes
	// "[-a-zA-Z0-9_]*" (zero or more chars within a single label).
	// Mid-position "**" naturally collapses to single-label since
	// each star is expanded independently.
	result := strings.ReplaceAll(pattern, ".", "[.]")
	result = strings.ReplaceAll(result, "*", "[-a-zA-Z0-9_]*")

	return "^" + result + "[.]$"
}

// isBareWildcard reports whether pattern consists entirely of "*"
// characters (e.g. "*", "**", "***").
func isBareWildcard(pattern string) bool {
	return strings.TrimLeft(pattern, "*") == ""
}

// normalizeProtocol converts a config-level protocol string to the
// lowercase form used in iptables rules. "TCP" maps to "tcp", "UDP"
// to "udp", "SCTP" to "sctp", and empty or "ANY" to "" (any
// protocol). Under Cilium default semantics, an omitted protocol
// means TCP and UDP. SCTP requires explicit opt-in (Cilium Helm
// value sctp.enabled=true); the sandbox matches this default by
// expanding ANY to TCP+UDP only.
func normalizeProtocol(proto string) string {
	switch proto {
	case "TCP":
		return protoTCP
	case "UDP":
		return protoUDP
	case "SCTP":
		return protoSCTP
	case "", "ANY":
		return ""
	default:
		return ""
	}
}

// ResolveCIDRRules collects toCIDR and toCIDRSet entries from all
// egress rules, preserving port associations from each rule's toPorts,
// and separates them by address family. Under Cilium semantics, CIDR
// rules are direct L3 allow selectors that bypass the Envoy proxy. If
// the parent rule has toPorts, the CIDR is port-scoped (L3 AND L4);
// otherwise the CIDR allows any port.
func (c *SandboxConfig) ResolveCIDRRules() ([]ResolvedCIDR, []ResolvedCIDR) {
	var ipv4, ipv6 []ResolvedCIDR

	ruleIdx := 0

	cidrRules := c.EgressRules()
	for ri := range cidrRules {
		if len(cidrRules[ri].ToCIDRSet) == 0 && len(cidrRules[ri].ToCIDR) == 0 {
			continue
		}

		ports := resolvePortsFromRule(cidrRules[ri])

		// Combine toCIDR and toCIDRSet into a unified list.
		allCIDRs := make([]CIDRRule, 0, len(cidrRules[ri].ToCIDR)+len(cidrRules[ri].ToCIDRSet))
		for _, cidr := range cidrRules[ri].ToCIDR {
			allCIDRs = append(allCIDRs, CIDRRule{CIDR: cidr})
		}

		allCIDRs = append(allCIDRs, cidrRules[ri].ToCIDRSet...)

		for _, cidr := range allCIDRs {
			v4, v6 := classifyCIDR(cidr, ports, ruleIdx)
			if v4 != nil {
				ipv4 = append(ipv4, *v4)
			}

			if v6 != nil {
				ipv6 = append(ipv6, *v6)
			}
		}

		ruleIdx++
	}

	return ipv4, ipv6
}

// resolvePortsFromRule extracts a sorted, deduplicated list of resolved
// port-protocol pairs from a rule's toPorts. Returns nil when the rule
// has no toPorts or when any PortRule has an empty Ports list (meaning
// all ports).
func resolvePortsFromRule(rule EgressRule) []ResolvedPortProto {
	if len(rule.ToPorts) == 0 {
		return nil
	}

	seen := make(map[string]bool)

	var ports []ResolvedPortProto

	for _, pr := range rule.ToPorts {
		if len(pr.Ports) == 0 {
			// Empty Ports list means all ports.
			return nil
		}

		for _, p := range pr.Ports {
			resolved, err := ResolvePort(p.Port)
			if err != nil {
				continue
			}

			if resolved == 0 {
				// Wildcard port: equivalent to empty Ports list.
				return nil
			}

			n := int(resolved)
			proto := normalizeProtocol(p.Protocol)
			// Expand ANY protocol into separate tcp and udp entries
			// so formatPortProto always has a concrete protocol
			// for port-scoped rules. SCTP requires explicit opt-in.
			protos := []string{proto}
			if proto == "" {
				protos = []string{protoTCP, protoUDP}
			}

			for _, expandedProto := range protos {
				k := expandedProto + "/" + strconv.Itoa(n) + "/" + strconv.Itoa(p.EndPort)
				if !seen[k] {
					seen[k] = true

					ports = append(ports, ResolvedPortProto{
						Port:     n,
						EndPort:  p.EndPort,
						Protocol: expandedProto,
					})
				}
			}
		}
	}

	sort.Slice(ports, func(i, j int) bool {
		if ports[i].Port != ports[j].Port {
			return ports[i].Port < ports[j].Port
		}

		if ports[i].EndPort != ports[j].EndPort {
			return ports[i].EndPort < ports[j].EndPort
		}

		return ports[i].Protocol < ports[j].Protocol
	})

	return ports
}

// classifyCIDR parses a CIDR rule and classifies it by address family,
// filtering except entries to only include those matching the same
// family. The ruleIndex is propagated to the resulting [ResolvedCIDR]
// to track which egress rule it came from.
func classifyCIDR(cidr CIDRRule, ports []ResolvedPortProto, ruleIndex int) (*ResolvedCIDR, *ResolvedCIDR) {
	_, _, err := net.ParseCIDR(cidr.CIDR)
	if err != nil {
		return nil, nil
	}

	// Filter except entries by address family. Use string-based
	// detection to avoid Go's IPv4-mapped IPv6 normalization where
	// To4() returns non-nil for "::ffff:10.0.0.0/104".
	var v4Except, v6Except []string
	for _, exc := range cidr.Except {
		_, _, excErr := net.ParseCIDR(exc)
		if excErr != nil {
			continue
		}

		if strings.Contains(exc, ":") {
			v6Except = append(v6Except, exc)
		} else {
			v4Except = append(v4Except, exc)
		}
	}

	if strings.Contains(cidr.CIDR, ":") {
		resolved := ResolvedCIDR{CIDR: cidr.CIDR, Except: v6Except, Ports: ports, RuleIndex: ruleIndex}
		if len(resolved.Except) == 0 {
			resolved.Except = nil
		}

		return nil, &resolved
	}

	resolved := ResolvedCIDR{CIDR: cidr.CIDR, Except: v4Except, Ports: ports, RuleIndex: ruleIndex}
	if len(resolved.Except) == 0 {
		resolved.Except = nil
	}

	return &resolved, nil
}
