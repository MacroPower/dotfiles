package sandbox

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"go.jacobcolvin.com/niceyaml"
)

// Envoy bootstrap config types. These model the subset of the Envoy v3
// API used by the sandbox's transparent SNI-filtering proxy.

type envoyBootstrap struct {
	OverloadManager envoyOverloadManager `yaml:"overload_manager"`
	StaticResources envoyStaticResources `yaml:"static_resources"`
}

type envoyOverloadManager struct {
	ResourceMonitors []envoyNamedTyped `yaml:"resource_monitors"`
}

type envoyStaticResources struct {
	Listeners []envoyListener `yaml:"listeners"`
	Clusters  []envoyCluster  `yaml:"clusters"`
}

type envoyListener struct {
	DefaultFilterChain *envoyFilterChain  `yaml:"default_filter_chain,omitempty"`
	Name               string             `yaml:"name"`
	Address            envoyAddress       `yaml:"address"`
	ListenerFilters    []envoyNamedTyped  `yaml:"listener_filters,omitempty"`
	FilterChains       []envoyFilterChain `yaml:"filter_chains"`
}

type envoyAddress struct {
	SocketAddress envoySocketAddress `yaml:"socket_address"`
}

type envoySocketAddress struct {
	Address   string `yaml:"address"`
	PortValue int    `yaml:"port_value"`
}

type envoyNamedTyped struct {
	TypedConfig any    `yaml:"typed_config"`
	Name        string `yaml:"name"`
}

type envoyFilterChain struct {
	FilterChainMatch *envoyFilterChainMatch `yaml:"filter_chain_match,omitempty"`
	TransportSocket  *envoyTransportSocket  `yaml:"transport_socket,omitempty"`
	Filters          []envoyFilter          `yaml:"filters"`
}

type envoyTransportSocket struct {
	TypedConfig any    `yaml:"typed_config"`
	Name        string `yaml:"name"`
}

type envoyDownstreamTlsContext struct {
	AtType           string                `yaml:"@type"`
	CommonTlsContext envoyCommonTlsContext `yaml:"common_tls_context"`
}

type envoyCommonTlsContext struct {
	TlsCertificates []envoyTlsCertificate `yaml:"tls_certificates"`
	AlpnProtocols   []string              `yaml:"alpn_protocols,omitempty"`
}

type envoyTlsCertificate struct {
	CertificateChain envoyDataSource `yaml:"certificate_chain"`
	PrivateKey       envoyDataSource `yaml:"private_key"`
}

type envoyDataSource struct {
	Filename     string `yaml:"filename,omitempty"`
	InlineString string `yaml:"inline_string,omitempty"`
}

type envoyUpstreamTlsContext struct {
	CommonTlsContext *envoyUpstreamCommonTlsContext `yaml:"common_tls_context,omitempty"`
	AtType           string                         `yaml:"@type"`
}

type envoyUpstreamCommonTlsContext struct {
	ValidationContext *envoyValidationContext `yaml:"validation_context,omitempty"`
}

type envoyValidationContext struct {
	TrustedCA envoyDataSource `yaml:"trusted_ca"`
}

type envoyFilterChainMatch struct {
	TransportProtocol string   `yaml:"transport_protocol,omitempty"`
	ServerNames       []string `yaml:"server_names,omitempty"`
}

type envoyFilter struct {
	TypedConfig any    `yaml:"typed_config"`
	Name        string `yaml:"name"`
}

type envoyTypeOnly struct {
	AtType string `yaml:"@type"`
}

type envoyDownstreamConnectionsConfig struct {
	AtType                         string `yaml:"@type"`
	MaxActiveDownstreamConnections int    `yaml:"max_active_downstream_connections"`
}

type envoySNIFilterConfig struct {
	DNSCacheConfig envoyDNSCacheConfig `yaml:"dns_cache_config"`
	AtType         string              `yaml:"@type"`
	PortValue      int                 `yaml:"port_value"`
}

type envoyDNSCacheConfig struct {
	Name            string `yaml:"name"`
	DNSLookupFamily string `yaml:"dns_lookup_family"`
}

type envoyTcpProxyConfig struct {
	AtType     string           `yaml:"@type"`
	StatPrefix string           `yaml:"stat_prefix"`
	Cluster    string           `yaml:"cluster"`
	AccessLog  []envoyAccessLog `yaml:"access_log,omitempty"`
}

type envoyAccessLog struct {
	TypedConfig any    `yaml:"typed_config"`
	Name        string `yaml:"name"`
}

// envoyStderrAccessLogConfig models the StderrAccessLog typed config with
// an optional text format. When LogFormat is set, Envoy uses the provided
// format string instead of the default access log format.
type envoyStderrAccessLogConfig struct {
	LogFormat *envoySubstitutionFormatString `yaml:"log_format,omitempty"`
	AtType    string                         `yaml:"@type"`
}

// envoySubstitutionFormatString models Envoy's SubstitutionFormatString
// with a text_format field for command-operator log formatting.
type envoySubstitutionFormatString struct {
	TextFormat string `yaml:"text_format"`
}

type envoyHTTPConnManagerConfig struct {
	NormalizePath                *bool                `yaml:"normalize_path,omitempty"`
	UseRemoteAddress             *bool                `yaml:"use_remote_address,omitempty"`
	SkipXffAppend                *bool                `yaml:"skip_xff_append,omitempty"`
	AtType                       string               `yaml:"@type"`
	StatPrefix                   string               `yaml:"stat_prefix"`
	StreamIdleTimeout            string               `yaml:"stream_idle_timeout,omitempty"`
	PathWithEscapedSlashesAction string               `yaml:"path_with_escaped_slashes_action,omitempty"`
	RouteConfig                  envoyRouteConfig     `yaml:"route_config"`
	AccessLog                    []envoyAccessLog     `yaml:"access_log,omitempty"`
	HTTPFilters                  []envoyFilter        `yaml:"http_filters"`
	UpgradeConfigs               []envoyUpgradeConfig `yaml:"upgrade_configs,omitempty"`
	MergeSlashes                 bool                 `yaml:"merge_slashes,omitempty"`
}

type envoyUpgradeConfig struct {
	UpgradeType string `yaml:"upgrade_type"`
}

type envoyRouteConfig struct {
	VirtualHosts []envoyVirtualHost `yaml:"virtual_hosts"`
}

type envoyVirtualHost struct {
	Name    string       `yaml:"name"`
	Domains []string     `yaml:"domains"`
	Routes  []envoyRoute `yaml:"routes"`
}

type envoyRoute struct {
	Route          *envoyRouteAction          `yaml:"route,omitempty"`
	DirectResponse *envoyDirectResponseAction `yaml:"direct_response,omitempty"`
	Match          envoyRouteMatch            `yaml:"match"`
}

// envoyRouteMatch models Envoy's route.RouteMatch message.
//
// When SafeRegex is set (instead of Prefix), it acts as a path_specifier
// on the RouteMatch. Envoy evaluates this via CompiledGoogleReMatcher,
// which calls re2::RE2::FullMatch -- meaning the regex must match the
// entire request path, not a substring. For example, a regex of "/v1/"
// does NOT match the path "/v1/completions"; it only matches the
// literal three-character path "/v1/". To match paths that start with
// "/v1/", the regex must be "/v1/.*".
//
// This is the same full-string match behavior that Cilium produces,
// though the two systems arrive at it differently. Cilium applies
// path restrictions via a HeaderMatcher on the ":path" pseudo-header,
// evaluated by its custom "cilium.l7policy" Envoy filter through the
// NPDS (Network Policy Discovery Service) xDS API. Both mechanisms
// ultimately call CompiledGoogleReMatcher::match(), which delegates to
// RE2::FullMatch, so the behavioral outcome is identical.
//
// Audited and confirmed: the sandbox's route-level safe_regex and
// Cilium's header-based L7 policy produce the same accept/reject
// decisions for any given path regex.
//
// Envoy source references:
//   - source/common/router/config_impl.cc: RouteEntryImplBase
//     applies safe_regex via CompiledGoogleReMatcher
//   - source/common/common/matchers.cc: CompiledGoogleReMatcher::match
//     calls re2::RE2::FullMatch
type envoyRouteMatch struct {
	SafeRegex *envoySafeRegex             `yaml:"safe_regex,omitempty"`
	Grpc      *envoyGrpcRouteMatchOptions `yaml:"grpc,omitempty"`
	Prefix    string                      `yaml:"prefix,omitempty"`
	Headers   []envoyHeaderMatcher        `yaml:"headers,omitempty"`
}

// envoyGrpcRouteMatchOptions is an empty message that, when present on a
// RouteMatch, restricts the route to gRPC requests (content-type
// application/grpc). Mirrors Envoy's route.RouteMatch.GrpcRouteMatchOptions.
type envoyGrpcRouteMatchOptions struct{}

type envoyHeaderMatcher struct {
	StringMatch  *envoyStringMatch `yaml:"string_match,omitempty"`
	PresentMatch *bool             `yaml:"present_match,omitempty"`
	Name         string            `yaml:"name"`
}

type envoyStringMatch struct {
	SafeRegex *envoySafeRegex `yaml:"safe_regex,omitempty"`
	Exact     string          `yaml:"exact,omitempty"`
}

type envoySafeRegex struct {
	Regex string `yaml:"regex"`
}

type envoyRBACConfig struct {
	Rules  envoyRBACRules `yaml:"rules"`
	AtType string         `yaml:"@type"`
}

type envoyRBACRules struct {
	Policies map[string]envoyRBACPolicy `yaml:"policies"`
	Action   string                     `yaml:"action"`
}

type envoyRBACPolicy struct {
	Permissions []envoyRBACPermission `yaml:"permissions"`
	Principals  []envoyRBACPrincipal  `yaml:"principals"`
}

// envoyRBACPermission represents a single RBAC permission check.
// Fields are mutually exclusive (Envoy oneof).
type envoyRBACPermission struct {
	RequestedServerName *envoyStringMatch   `yaml:"requested_server_name,omitempty"`
	Header              *envoyHeaderMatcher `yaml:"header,omitempty"`
}

type envoyRBACPrincipal struct {
	Any bool `yaml:"any"`
}

type envoyRouteAction struct {
	MaxStreamDuration *envoyMaxStreamDuration `yaml:"max_stream_duration,omitempty"`
	Cluster           string                  `yaml:"cluster"`
	Timeout           string                  `yaml:"timeout,omitempty"`
	AutoHostRewrite   bool                    `yaml:"auto_host_rewrite"`
}

// envoyMaxStreamDuration models Envoy's route.MaxStreamDuration message.
// GrpcTimeoutHeaderMax caps the duration extracted from the grpc-timeout
// request header. A value of "0s" means unlimited (honor the client value).
type envoyMaxStreamDuration struct {
	GrpcTimeoutHeaderMax string `yaml:"grpc_timeout_header_max"`
}

type envoyDirectResponseAction struct {
	Body   *envoyDataSource `yaml:"body,omitempty"`
	Status int              `yaml:"status"`
}

type envoyHTTPDFPFilterConfig struct {
	AtType         string              `yaml:"@type"`
	DNSCacheConfig envoyDNSCacheConfig `yaml:"dns_cache_config"`
}

type envoyCluster struct {
	ClusterType                   *envoyClusterType     `yaml:"cluster_type,omitempty"`
	TransportSocket               *envoyTransportSocket `yaml:"transport_socket,omitempty"`
	LoadAssignment                *envoyLoadAssignment  `yaml:"load_assignment,omitempty"`
	TypedExtensionProtocolOptions map[string]any        `yaml:"typed_extension_protocol_options,omitempty"`
	Name                          string                `yaml:"name"`
	ConnectTimeout                string                `yaml:"connect_timeout"`
	Type                          string                `yaml:"type,omitempty"`
	LBPolicy                      string                `yaml:"lb_policy"`
}

type envoyClusterType struct {
	TypedConfig any    `yaml:"typed_config"`
	Name        string `yaml:"name"`
}

type envoyClusterDFPConfig struct {
	AtType         string              `yaml:"@type"`
	DNSCacheConfig envoyDNSCacheConfig `yaml:"dns_cache_config"`
}

type envoyHttpProtocolOptions struct {
	UseDownstreamProtocolConfig envoyUseDownstreamProtocolConfig `yaml:"use_downstream_protocol_config"`
	AtType                      string                           `yaml:"@type"`
}

type envoyUseDownstreamProtocolConfig struct{}

type envoyLoadAssignment struct {
	ClusterName string          `yaml:"cluster_name"`
	Endpoints   []envoyEndpoint `yaml:"endpoints"`
}

type envoyEndpoint struct {
	LBEndpoints []envoyLBEndpoint `yaml:"lb_endpoints"`
}

type envoyLBEndpoint struct {
	Endpoint envoyEndpointAddress `yaml:"endpoint"`
}

type envoyEndpointAddress struct {
	Address envoyAddress `yaml:"address"`
}

// BuildAccessLog returns Envoy stderr access log config when logging is
// enabled, or nil when disabled.
func BuildAccessLog(logging bool) []envoyAccessLog {
	if !logging {
		return nil
	}

	return []envoyAccessLog{{
		Name: "envoy.access_loggers.stderr",
		TypedConfig: envoyTypeOnly{
			AtType: "type.googleapis.com/envoy.extensions.access_loggers.stream.v3.StderrAccessLog",
		},
	}}
}

func boolPtr(v bool) *bool {
	return &v
}

var sharedDNSCacheConfig = envoyDNSCacheConfig{
	Name:            "dynamic_forward_proxy_cache",
	DNSLookupFamily: "ALL",
}

// wildcardToSNIRegex converts a Cilium-style wildcard pattern into a
// regex for RBAC SNI matching. It handles two forms:
//
//   - "*." prefix (single-label): matches exactly one DNS label.
//     Example: "*.example.com" -> "^[-a-zA-Z0-9_]+\.example\.com$"
//     matches:  "sub.example.com"
//     rejects:  "a.b.example.com"
//
//   - "**." prefix (multi-label): matches one or more DNS labels at
//     arbitrary depth, mirroring Cilium's [dnsWildcardREGroup].
//     Example: "**.example.com" ->
//     "^[-a-zA-Z0-9_]+(\.[-a-zA-Z0-9_]+)*\.example\.com$"
//     matches:  "sub.example.com", "a.b.example.com"
//     rejects:  "example.com"
//
// Both forms use + (one-or-more) on the first label's character class,
// not * (zero-or-more), because empty DNS labels are invalid in SNI
// (RFC 6066 section 3). This is an intentional sandbox strictness:
// Cilium's single-label regex uses * (allowing empty labels like
// ".example.com"), but since SNI values in practice never have empty
// labels, the + quantifier is both correct and more precise.
//
// SNI values never contain trailing dots (RFC 6066 section 3), so unlike
// Cilium's regex (which uses [.] for FQDN-form names), this uses literal
// \. separators and omits the trailing dot anchor.
//
// [dnsWildcardREGroup]: Cilium pkg/fqdn/matchpattern constants.
func wildcardToSNIRegex(pattern string) string {
	if strings.HasPrefix(pattern, "**.") {
		suffix := pattern[3:]
		// One or more dot-separated labels, then the fixed suffix.
		return `^[-a-zA-Z0-9_]+(\.[-a-zA-Z0-9_]+)*\.` + regexp.QuoteMeta(suffix) + `$`
	}

	suffix := strings.TrimPrefix(pattern, "*.")

	return `^[-a-zA-Z0-9_]+\.` + regexp.QuoteMeta(suffix) + `$`
}

// wildcardToHostRegex converts a wildcard pattern into a regex for HTTP
// Host/:authority header matching. Like [wildcardToSNIRegex] but also
// accepts an optional ":port" suffix, since HTTP/1.1 Host and HTTP/2
// :authority headers may include a port (e.g. "sub.example.com:80").
//
// The quantifier is "+" (one or more) rather than Cilium's "*" (zero or
// more) because an empty DNS label is not a valid hostname and cannot
// appear in an HTTP Host header.
//
// Example: "*.example.com" -> "^[-a-zA-Z0-9_]+\.example\.com(:\d+)?$"
//
//	matches:  "sub.example.com", "sub.example.com:80"
//	rejects:  "a.b.example.com", "a.b.example.com:80"
func wildcardToHostRegex(pattern string) string {
	if strings.HasPrefix(pattern, "**.") {
		suffix := pattern[3:]
		return `^[-a-zA-Z0-9_]+(\.[-a-zA-Z0-9_]+)*\.` + regexp.QuoteMeta(suffix) + `(:\d+)?$`
	}

	suffix := strings.TrimPrefix(pattern, "*.")

	return `^[-a-zA-Z0-9_]+\.` + regexp.QuoteMeta(suffix) + `(:\d+)?$`
}

// wildcardServerName converts a domain pattern to an Envoy server_names
// entry. Both "*.example.com" and "**.example.com" use "*.example.com"
// in server_names because Envoy's suffix matching is inherently
// multi-label. The RBAC filter (via [wildcardToSNIRegex]) provides the
// correct depth restriction.
func wildcardServerName(domain string) string {
	if strings.HasPrefix(domain, "**.") {
		return "*." + domain[3:]
	}

	return domain
}

// buildWildcardRBACFilter creates an RBAC network filter that
// restricts wildcard server_names matches to the correct depth,
// matching [CiliumNetworkPolicy] toFQDNs.matchPattern semantics.
// Single-star patterns ("*.example.com") are confined to one DNS
// label; double-star patterns ("**.example.com") allow arbitrary
// subdomain depth.
//
// Envoy's server_names uses suffix-based matching, so "*.example.com"
// matches arbitrarily deep subdomains like "a.b.example.com". Cilium
// confines "*" to a single DNS label. This filter is prepended to
// passthrough filter chains that contain wildcard patterns; it checks
// the TLS SNI (requested_server_name) against per-domain regexes
// (via [wildcardToSNIRegex]) and closes the connection if the SNI
// does not match.
//
// The RBAC action is ALLOW with one permission per wildcard domain.
// Multiple permissions are OR'd: the connection is allowed if the SNI
// matches any single permission. On mismatch, Envoy closes the
// connection -- there is no fallthrough to other filter chains because
// filter chain selection is already finalized by this point.
//
// [CiliumNetworkPolicy]: https://docs.cilium.io/en/stable/policy/language/#dns-based
func buildWildcardRBACFilter(wildcardDomains []string) envoyFilter {
	var permissions []envoyRBACPermission
	for _, d := range wildcardDomains {
		permissions = append(permissions, envoyRBACPermission{
			RequestedServerName: &envoyStringMatch{
				SafeRegex: &envoySafeRegex{Regex: wildcardToSNIRegex(d)},
			},
		})
	}

	return envoyFilter{
		Name: "envoy.filters.network.rbac",
		TypedConfig: envoyRBACConfig{
			AtType: "type.googleapis.com/envoy.extensions.filters.network.rbac.v3.RBAC",
			Rules: envoyRBACRules{
				Action: "ALLOW",
				Policies: map[string]envoyRBACPolicy{
					"wildcard_depth": {
						Permissions: permissions,
						Principals:  []envoyRBACPrincipal{{Any: true}},
					},
				},
			},
		},
	}
}

// buildWildcardHTTPRBACFilter creates an HTTP RBAC filter that
// restricts wildcard domain matches to the correct depth by checking
// the :authority pseudo-header. Single-star patterns ("*.example.com")
// are confined to one DNS label; double-star patterns
// ("**.example.com") allow arbitrary subdomain depth. This is the
// HTTP-layer equivalent of [buildWildcardRBACFilter] (which checks
// TLS SNI).
//
// Cilium enforces FQDN wildcard depth via its BPF identity system
// (DNS proxy regex -> identity allocation -> BPF map lookup), not at
// the Envoy layer. This RBAC approach is an architectural substitute
// that achieves equivalent filtering semantics within the sandbox's
// Envoy-only architecture.
//
// Because the RBAC filter applies globally to the HCM (not per virtual
// host), the permissions must also allow exact domains through. Each
// wildcard gets a depth-enforcement regex (via [wildcardToHostRegex]);
// each exact domain gets a regex that matches the literal name with an
// optional port suffix. All permissions are OR'd.
func buildWildcardHTTPRBACFilter(wildcardDomains, exactDomains []string) envoyFilter {
	var permissions []envoyRBACPermission

	for _, d := range wildcardDomains {
		permissions = append(permissions, envoyRBACPermission{
			Header: &envoyHeaderMatcher{
				Name: ":authority",
				StringMatch: &envoyStringMatch{
					SafeRegex: &envoySafeRegex{
						Regex: wildcardToHostRegex(d),
					},
				},
			},
		})
	}

	for _, d := range exactDomains {
		permissions = append(permissions, envoyRBACPermission{
			Header: &envoyHeaderMatcher{
				Name: ":authority",
				StringMatch: &envoyStringMatch{
					SafeRegex: &envoySafeRegex{
						Regex: `^` + regexp.QuoteMeta(d) + `(:\d+)?$`,
					},
				},
			},
		})
	}

	return envoyFilter{
		Name: "envoy.filters.http.rbac",
		TypedConfig: envoyRBACConfig{
			AtType: "type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBAC",
			Rules: envoyRBACRules{
				Action: "ALLOW",
				Policies: map[string]envoyRBACPolicy{
					"wildcard_depth": {
						Permissions: permissions,
						Principals:  []envoyRBACPrincipal{{Any: true}},
					},
				},
			},
		},
	}
}

func buildPassthroughFilterChain(
	upstreamPort int,
	statPrefix string,
	serverNames []string,
	accessLog []envoyAccessLog,
	rbacFilter *envoyFilter,
) envoyFilterChain {
	var filters []envoyFilter
	if rbacFilter != nil {
		filters = append(filters, *rbacFilter)
	}

	filters = append(filters,
		envoyFilter{
			Name: "envoy.filters.network.sni_dynamic_forward_proxy",
			TypedConfig: envoySNIFilterConfig{
				AtType:         "type.googleapis.com/envoy.extensions.filters.network.sni_dynamic_forward_proxy.v3.FilterConfig",
				PortValue:      upstreamPort,
				DNSCacheConfig: sharedDNSCacheConfig,
			},
		},
		envoyFilter{
			Name: "envoy.filters.network.tcp_proxy",
			TypedConfig: envoyTcpProxyConfig{
				AtType:     "type.googleapis.com/envoy.extensions.filters.network.tcp_proxy.v3.TcpProxy",
				StatPrefix: statPrefix,
				Cluster:    "dynamic_forward_proxy_cluster",
				AccessLog:  accessLog,
			},
		},
	)

	fc := envoyFilterChain{
		FilterChainMatch: &envoyFilterChainMatch{
			TransportProtocol: "tls",
			ServerNames:       serverNames,
		},
		Filters: filters,
	}

	return fc
}

func buildMITMFilterChain(rule ResolvedRule, accessLog []envoyAccessLog, certsDir string) envoyFilterChain {
	sn := wildcardServerName(rule.Domain)
	certPath := fmt.Sprintf("%s/%s/cert.pem", certsDir, sn)
	keyPath := fmt.Sprintf("%s/%s/key.pem", certsDir, sn)

	vhosts, _, _ := buildHTTPVirtualHosts([]ResolvedRule{rule}, "mitm_forward_proxy_cluster")

	return envoyFilterChain{
		FilterChainMatch: &envoyFilterChainMatch{TransportProtocol: "tls", ServerNames: []string{sn}},
		TransportSocket: &envoyTransportSocket{
			Name: "envoy.transport_sockets.tls",
			TypedConfig: envoyDownstreamTlsContext{
				AtType: "type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.DownstreamTlsContext",
				CommonTlsContext: envoyCommonTlsContext{
					TlsCertificates: []envoyTlsCertificate{{
						CertificateChain: envoyDataSource{Filename: certPath},
						PrivateKey:       envoyDataSource{Filename: keyPath},
					}},
					// Advertise h2 and http/1.1 so HTTP/2 clients
					// don't fall back to HTTP/1.1 silently.
					// Note: mTLS passthrough is unsupported.
					AlpnProtocols: []string{"h2", "http/1.1"},
				},
			},
		},
		Filters: []envoyFilter{{
			Name: "envoy.filters.network.http_connection_manager",
			TypedConfig: envoyHTTPConnManagerConfig{
				AtType:                       "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager",
				StatPrefix:                   "mitm_" + rule.Domain,
				StreamIdleTimeout:            "300s",
				NormalizePath:                boolPtr(true),
				UseRemoteAddress:             boolPtr(true),
				SkipXffAppend:                boolPtr(true),
				MergeSlashes:                 true,
				PathWithEscapedSlashesAction: "UNESCAPE_AND_REDIRECT",
				RouteConfig: envoyRouteConfig{
					VirtualHosts: vhosts,
				},
				AccessLog:      accessLog,
				UpgradeConfigs: []envoyUpgradeConfig{{UpgradeType: "websocket"}},
				HTTPFilters: []envoyFilter{
					{
						Name: "envoy.filters.http.dynamic_forward_proxy",
						TypedConfig: envoyHTTPDFPFilterConfig{
							AtType:         "type.googleapis.com/envoy.extensions.filters.http.dynamic_forward_proxy.v3.FilterConfig",
							DNSCacheConfig: sharedDNSCacheConfig,
						},
					},
					{
						Name: "envoy.filters.http.router",
						TypedConfig: envoyTypeOnly{
							AtType: "type.googleapis.com/envoy.extensions.filters.http.router.v3.Router",
						},
					},
				},
			},
		}},
	}
}

func buildTLSListener(
	name string,
	listenPort, upstreamPort int,
	statPrefix string,
	rules []ResolvedRule,
	open bool,
	accessLog []envoyAccessLog,
	certsDir string,
) envoyListener {
	var (
		passthroughDomains []string
		mitmRules          []ResolvedRule
	)

	for _, r := range rules {
		if r.IsRestricted() && certsDir != "" {
			mitmRules = append(mitmRules, r)
		} else {
			passthroughDomains = append(passthroughDomains, r.Domain)
		}
	}

	// Bare wildcard "*" matches all FQDNs. Envoy does not support "*"
	// in server_names (FilterChainMatch) -- the docs say to omit
	// server_names for catch-all matching. Convert to an unrestricted
	// passthrough chain (no SNI filter), same as the open-port path.
	if slices.Contains(passthroughDomains, "*") {
		passthroughDomains = slices.DeleteFunc(passthroughDomains, func(s string) bool {
			return s == "*"
		})
		open = true
	}

	// Wildcard domains ("*.example.com" or "**.example.com") are placed
	// in a separate filter chain with an RBAC filter that enforces the
	// correct wildcard depth (single-label for *, multi-label for **),
	// matching CiliumNetworkPolicy semantics. Without this, Envoy's
	// suffix-based server_names matching would allow arbitrarily deep
	// subdomains for single-star patterns like "*.example.com".
	var exactDomains, wildcardDomains []string
	for _, d := range passthroughDomains {
		if strings.HasPrefix(d, "*.") || strings.HasPrefix(d, "**.") {
			wildcardDomains = append(wildcardDomains, d)
		} else {
			exactDomains = append(exactDomains, d)
		}
	}

	var chains []envoyFilterChain
	if len(exactDomains) > 0 {
		chains = append(chains, buildPassthroughFilterChain(upstreamPort, statPrefix, exactDomains, accessLog, nil))
	}

	if len(wildcardDomains) > 0 {
		rbac := buildWildcardRBACFilter(wildcardDomains)

		envoyNames := make([]string, len(wildcardDomains))
		for i, d := range wildcardDomains {
			envoyNames[i] = wildcardServerName(d)
		}

		chains = append(
			chains,
			buildPassthroughFilterChain(upstreamPort, statPrefix+"_wildcard", envoyNames, accessLog, &rbac),
		)
	}

	for _, r := range mitmRules {
		chains = append(chains, buildMITMFilterChain(r, accessLog, certsDir))
	}

	// Open ports get a catch-all passthrough chain (no SNI restriction).
	if open {
		chains = append(chains, buildPassthroughFilterChain(upstreamPort, statPrefix+"_open", nil, accessLog, nil))
	}

	// Default filter chain catches connections without SNI (e.g., TLS
	// by IP address). Without this, Envoy silently drops the connection
	// with no log entry, making diagnosis difficult (ISSUE-34). The
	// access log always fires (regardless of the logging flag) since
	// missing-SNI connections indicate a configuration or client issue
	// that should be visible.
	defaultChain := buildDefaultRejectFilterChain(statPrefix)

	return envoyListener{
		Name: name,
		Address: envoyAddress{SocketAddress: envoySocketAddress{
			Address: "127.0.0.1", PortValue: listenPort,
		}},
		DefaultFilterChain: &defaultChain,
		ListenerFilters: []envoyNamedTyped{{
			Name: "envoy.filters.listener.tls_inspector",
			TypedConfig: envoyTypeOnly{
				AtType: "type.googleapis.com/envoy.extensions.filters.listener.tls_inspector.v3.TlsInspector",
			},
		}},
		FilterChains: chains,
	}
}

// buildDefaultRejectFilterChain creates a filter chain that logs and
// immediately closes connections. Used as the default filter chain on
// TLS listeners to provide diagnostic output for connections without
// SNI instead of silently dropping them.
func buildDefaultRejectFilterChain(statPrefix string) envoyFilterChain {
	return envoyFilterChain{
		Filters: []envoyFilter{{
			Name: "envoy.filters.network.tcp_proxy",
			TypedConfig: envoyTcpProxyConfig{
				AtType:     "type.googleapis.com/envoy.extensions.filters.network.tcp_proxy.v3.TcpProxy",
				StatPrefix: statPrefix + "_no_sni",
				Cluster:    "missing_sni_blackhole",
				AccessLog: []envoyAccessLog{{
					Name: "envoy.access_loggers.stderr",
					TypedConfig: envoyStderrAccessLogConfig{
						AtType: "type.googleapis.com/envoy.extensions.access_loggers.stream.v3.StderrAccessLog",
						LogFormat: &envoySubstitutionFormatString{
							TextFormat: "missing_sni src=%DOWNSTREAM_REMOTE_ADDRESS% dst=%DOWNSTREAM_LOCAL_ADDRESS% %RESPONSE_FLAGS%\n",
						},
					},
				}},
			},
		}},
	}
}

// grpcRouteVariant creates a gRPC-specific copy of a forwarding route.
// The copy adds GrpcRouteMatchOptions to the match (restricting it to
// gRPC requests) and MaxStreamDuration.GrpcTimeoutHeaderMax to the
// action (honoring the grpc-timeout request header). Cilium creates
// these dedicated gRPC routes before each regular route so that gRPC
// streaming RPCs get proper timeout handling.
func grpcRouteVariant(r envoyRoute) envoyRoute {
	grpcMatch := r.Match
	grpcMatch.Grpc = &envoyGrpcRouteMatchOptions{}

	return envoyRoute{
		Match: grpcMatch,
		Route: &envoyRouteAction{
			MaxStreamDuration: &envoyMaxStreamDuration{GrpcTimeoutHeaderMax: "0s"},
			Cluster:           r.Route.Cluster,
			Timeout:           r.Route.Timeout,
			AutoHostRewrite:   r.Route.AutoHostRewrite,
		},
	}
}

func buildHTTPVirtualHosts(rules []ResolvedRule, cluster string) ([]envoyVirtualHost, []string, []string) {
	var (
		restricted   []ResolvedRule
		unrestricted []string
	)

	// Classify domains for RBAC filter generation using the original
	// domain pattern (before wildcardServerName conversion) so that
	// ** patterns produce multi-label regexes via wildcardToHostRegex.
	// Restricted domains are always exact names (never wildcards)
	// because ErrWildcardWithL7 rejects wildcard matchPatterns
	// combined with L7 rules at config validation time.
	var wildcardDomains, exactDomains []string
	for _, r := range rules {
		if r.IsRestricted() {
			restricted = append(restricted, r)
		} else {
			unrestricted = append(unrestricted, wildcardServerName(r.Domain))
			// Use the original r.Domain (not the wildcardServerName-converted
			// value) so that ** patterns retain their multi-label semantics
			// through wildcardToHostRegex.
			if strings.HasPrefix(r.Domain, "*.") || strings.HasPrefix(r.Domain, "**.") {
				wildcardDomains = append(wildcardDomains, r.Domain)
			} else if r.Domain != "*" {
				exactDomains = append(exactDomains, r.Domain)
			}
		}
	}

	// Restricted domains are exact names; include them so the RBAC
	// filter's ALLOW policy permits their traffic through.
	for _, r := range restricted {
		exactDomains = append(exactDomains, r.Domain)
	}

	var vhosts []envoyVirtualHost

	// One virtual host per restricted domain. Each HTTPRule is an
	// independent match (OR'd), not a cross-product.
	for _, r := range restricted {
		var routes []envoyRoute
		for _, hr := range r.HTTPRules {
			match := envoyRouteMatch{Prefix: "/"}
			if hr.Path != "" {
				// The path value from the CiliumNetworkPolicy HTTPRule
				// becomes a safe_regex path_specifier on the Envoy
				// RouteMatch. This uses RE2::FullMatch semantics (see
				// the envoyRouteMatch doc comment), so the regex must
				// match the entire path. A path like "/v1/" only
				// matches the literal string "/v1/", not "/v1/foo".
				match = envoyRouteMatch{SafeRegex: &envoySafeRegex{Regex: hr.Path}}
			}

			if hr.Method != "" {
				match.Headers = buildMethodHeaderMatcher([]string{hr.Method})
			}

			if hr.Host != "" {
				match.Headers = append(match.Headers, buildHostHeaderMatcher(hr.Host)...)
			}

			for _, hdr := range hr.Headers {
				match.Headers = append(match.Headers, envoyHeaderMatcher{
					Name:         hdr,
					PresentMatch: boolPtr(true),
				})
			}

			for _, hm := range hr.HeaderMatches {
				match.Headers = append(match.Headers, envoyHeaderMatcher{
					Name:        hm.Name,
					StringMatch: &envoyStringMatch{Exact: hm.Value},
				})
			}

			httpRoute := envoyRoute{
				Match: match,
				Route: &envoyRouteAction{
					Cluster:         cluster,
					AutoHostRewrite: true,
					Timeout:         "3600s",
				},
			}
			routes = append(routes, grpcRouteVariant(httpRoute), httpRoute)
		}

		// Catch-all denies everything else.
		routes = append(routes, envoyRoute{
			Match: envoyRouteMatch{Prefix: "/"},
			DirectResponse: &envoyDirectResponseAction{
				Status: 403,
				Body:   &envoyDataSource{InlineString: "Access denied"},
			},
		})
		vhosts = append(vhosts, envoyVirtualHost{
			Name:    "restricted_" + r.Domain,
			Domains: []string{wildcardServerName(r.Domain)},
			Routes:  routes,
		})
	}

	// All unrestricted domains share one virtual host.
	if len(unrestricted) > 0 {
		allowRoute := envoyRoute{
			Match: envoyRouteMatch{Prefix: "/"},
			Route: &envoyRouteAction{
				Cluster:         cluster,
				AutoHostRewrite: true,
				Timeout:         "3600s",
			},
		}
		vhosts = append(vhosts, envoyVirtualHost{
			Name:    "allowed",
			Domains: unrestricted,
			Routes:  []envoyRoute{grpcRouteVariant(allowRoute), allowRoute},
		})
	}

	return vhosts, wildcardDomains, exactDomains
}

func buildHTTPForwardListener(rules []ResolvedRule, open bool, accessLog []envoyAccessLog) envoyListener {
	vhosts, wildcardDomains, exactDomains := buildHTTPVirtualHosts(rules, "dynamic_forward_proxy_cluster")

	// Envoy allows only one virtual host with Domains: ["*"] per route
	// config. When a bare wildcard rule already produced a "*" vhost,
	// adding the open catch-all would create a duplicate and cause Envoy
	// to reject the config.
	hasCatchAll := slices.ContainsFunc(vhosts, func(vh envoyVirtualHost) bool {
		return slices.Contains(vh.Domains, "*")
	})

	if open && !hasCatchAll {
		openRoute := envoyRoute{
			Match: envoyRouteMatch{Prefix: "/"},
			Route: &envoyRouteAction{
				Cluster:         "dynamic_forward_proxy_cluster",
				AutoHostRewrite: true,
				Timeout:         "3600s",
			},
		}
		vhosts = append(vhosts, envoyVirtualHost{
			Name:    "open",
			Domains: []string{"*"},
			Routes:  []envoyRoute{grpcRouteVariant(openRoute), openRoute},
		})
	}

	// Build the HTTP filter chain. When wildcard domains are present
	// and the listener is not fully open and there is no catch-all
	// vhost, prepend an RBAC filter that enforces single-label depth
	// on the :authority header.
	httpFilters := []envoyFilter{
		{
			Name: "envoy.filters.http.dynamic_forward_proxy",
			TypedConfig: envoyHTTPDFPFilterConfig{
				AtType:         "type.googleapis.com/envoy.extensions.filters.http.dynamic_forward_proxy.v3.FilterConfig",
				DNSCacheConfig: sharedDNSCacheConfig,
			},
		},
		{
			Name: "envoy.filters.http.router",
			TypedConfig: envoyTypeOnly{
				AtType: "type.googleapis.com/envoy.extensions.filters.http.router.v3.Router",
			},
		},
	}

	if len(wildcardDomains) > 0 && !open && !hasCatchAll {
		rbacFilter := buildWildcardHTTPRBACFilter(wildcardDomains, exactDomains)
		httpFilters = append([]envoyFilter{rbacFilter}, httpFilters...)
	}

	return envoyListener{
		Name: "http_forward",
		Address: envoyAddress{SocketAddress: envoySocketAddress{
			Address: "127.0.0.1", PortValue: 15080,
		}},
		FilterChains: []envoyFilterChain{{
			Filters: []envoyFilter{{
				Name: "envoy.filters.network.http_connection_manager",
				TypedConfig: envoyHTTPConnManagerConfig{
					AtType:                       "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager",
					StatPrefix:                   "http_forward",
					StreamIdleTimeout:            "300s",
					NormalizePath:                boolPtr(true),
					UseRemoteAddress:             boolPtr(true),
					SkipXffAppend:                boolPtr(true),
					MergeSlashes:                 true,
					PathWithEscapedSlashesAction: "UNESCAPE_AND_REDIRECT",
					RouteConfig: envoyRouteConfig{
						VirtualHosts: vhosts,
					},
					AccessLog:      accessLog,
					UpgradeConfigs: []envoyUpgradeConfig{{UpgradeType: "websocket"}},
					HTTPFilters:    httpFilters,
				},
			}},
		}},
	}
}

func buildTCPForwardListener(
	name string,
	listenPort int,
	clusterName string,
	accessLog []envoyAccessLog,
) envoyListener {
	return envoyListener{
		Name: name,
		Address: envoyAddress{SocketAddress: envoySocketAddress{
			Address: "127.0.0.1", PortValue: listenPort,
		}},
		FilterChains: []envoyFilterChain{{
			Filters: []envoyFilter{{
				Name: "envoy.filters.network.tcp_proxy",
				TypedConfig: envoyTcpProxyConfig{
					AtType:     "type.googleapis.com/envoy.extensions.filters.network.tcp_proxy.v3.TcpProxy",
					StatPrefix: name,
					Cluster:    clusterName,
					AccessLog:  accessLog,
				},
			}},
		}},
	}
}

// buildMethodHeaderMatcher builds an Envoy header matcher that
// restricts the :method pseudo-header to the given HTTP methods using
// a safe_regex StringMatcher.
//
// The generated regex uses explicit ^ and $ anchors (e.g. "^GET$" or
// "^(GET|POST)$"). These anchors are technically redundant: Envoy's
// StringMatcher with safe_regex uses re2::RE2::FullMatch, which
// inherently matches the entire input string without requiring
// anchors. See Envoy source/common/common/matchers.cc
// (CompiledGoogleReMatcher::match calls RE2::FullMatch). A regex of
// just "GET" under FullMatch semantics would NOT match "GETTER" or
// "GE" -- the entire string must match.
//
// Cilium passes method regexes to Envoy without anchors (e.g. just
// "GET" not "^GET$"), relying on the FullMatch semantics described
// above. Both approaches produce identical behavior: "GE" does NOT
// match "GET" in either system.
//
// We keep the anchors because they are harmless (RE2 optimizes them
// away under FullMatch) and they make the full-match intent explicit
// when reading the generated Envoy config. This was audited and
// confirmed to produce identical matching behavior to Cilium's
// unanchored method regexes.
func buildMethodHeaderMatcher(methods []string) []envoyHeaderMatcher {
	if len(methods) == 0 {
		return nil
	}

	hm := envoyHeaderMatcher{Name: ":method"}
	if len(methods) == 1 {
		hm.StringMatch = &envoyStringMatch{SafeRegex: &envoySafeRegex{Regex: "^" + methods[0] + "$"}}
	} else {
		regex := "^(" + strings.Join(methods, "|") + ")$"
		hm.StringMatch = &envoyStringMatch{SafeRegex: &envoySafeRegex{Regex: regex}}
	}

	return []envoyHeaderMatcher{hm}
}

// buildHostHeaderMatcher creates an Envoy header matcher for the
// :authority pseudo-header using the given host regex. Envoy
// normalizes HTTP/1.1 Host into :authority for route matching. The
// regex is anchored with ^ and $ so it uses RE2::FullMatch semantics,
// matching Cilium's extended POSIX regex behavior.
//
// An optional port suffix (:\d+) is allowed after the host pattern
// because HTTP/1.1 clients may include the port in the Host header
// (e.g. "api.example.com:8443"), and Envoy preserves it in
// :authority. Cilium's Go extension strips the port before matching,
// but raw Envoy route matchers see the full value.
func buildHostHeaderMatcher(host string) []envoyHeaderMatcher {
	if host == "" {
		return nil
	}

	return []envoyHeaderMatcher{{
		Name:        ":authority",
		StringMatch: &envoyStringMatch{SafeRegex: &envoySafeRegex{Regex: "^" + host + `(:[0-9]+)?$`}},
	}}
}

// stripL7Restrictions converts restricted rules to passthrough by
// clearing their HTTPRules. This implements Cilium's OR semantics
// between open port rules and FQDN+L7 rules on the same port: the
// open port allows ALL traffic, overriding any L7 restrictions.
func stripL7Restrictions(rules []ResolvedRule) []ResolvedRule {
	result := make([]ResolvedRule, len(rules))
	for i, r := range rules {
		result[i] = ResolvedRule{Domain: r.Domain}
	}

	return result
}

func hasMITMRules(rules []ResolvedRule) bool {
	for _, r := range rules {
		if r.IsRestricted() {
			return true
		}
	}

	return false
}

func buildClusters(rules []ResolvedRule, tcpForwards []TCPForward, caBundlePath string) []envoyCluster {
	// Static cluster with no endpoints used as the upstream for the
	// default filter chain on TLS listeners. Connections routed here
	// are immediately reset (no healthy upstream), which is the desired
	// behavior for missing-SNI connections after the access log fires.
	clusters := []envoyCluster{{
		Name:           "missing_sni_blackhole",
		ConnectTimeout: "1s",
		Type:           "STATIC",
		LBPolicy:       "ROUND_ROBIN",
	}}

	// Only add the dynamic forward proxy cluster when there are FQDN rules.
	if len(rules) > 0 {
		clusters = append(clusters, envoyCluster{
			Name:           "dynamic_forward_proxy_cluster",
			ConnectTimeout: "5s",
			LBPolicy:       "CLUSTER_PROVIDED",
			ClusterType: &envoyClusterType{
				Name: "envoy.clusters.dynamic_forward_proxy",
				TypedConfig: envoyClusterDFPConfig{
					AtType:         "type.googleapis.com/envoy.extensions.clusters.dynamic_forward_proxy.v3.ClusterConfig",
					DNSCacheConfig: sharedDNSCacheConfig,
				},
			},
		})
	}

	if hasMITMRules(rules) {
		upstreamTLS := envoyUpstreamTlsContext{
			AtType: "type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext",
		}
		if caBundlePath != "" {
			upstreamTLS.CommonTlsContext = &envoyUpstreamCommonTlsContext{
				ValidationContext: &envoyValidationContext{
					TrustedCA: envoyDataSource{Filename: caBundlePath},
				},
			}
		}

		// Upstream SNI handling: auto_sni and auto_san_validation are
		// intentionally omitted here. Envoy's dynamic forward proxy
		// (DFP) cluster factory auto-enables both when
		// upstream_http_protocol_options is absent. Specifically,
		// createClusterImpl in the DFP cluster factory checks whether
		// auto_sni is already set and enables it if not, then does
		// the same for auto_san_validation. If
		// upstream_http_protocol_options IS present, the factory
		// rejects configs that lack auto_sni/auto_san_validation
		// unless allow_insecure_cluster_options is set. Since this
		// cluster omits upstream_http_protocol_options entirely, the
		// factory unconditionally enables both options. The upstream
		// TLS handshake uses SNI derived from the HTTP Host header
		// via auto_sni.
		//
		// See Envoy source:
		//   source/extensions/clusters/dynamic_forward_proxy/cluster.cc
		//   (createClusterImpl method)
		//
		// Cilium achieves correct upstream SNI through a different
		// mechanism. Rather than auto_sni, Cilium uses its custom
		// cilium.tls_wrapper transport socket, which reads the sni_
		// field from Cilium policy filter state and passes it to
		// getClientTlsContext() in the proxylib layer. Cilium
		// explicitly avoids setting auto_sni because it would
		// conflict with (and crash Envoy when combined with) the
		// Cilium Network filter's own SNI injection -- the two
		// mechanisms would race to set the SNI on the upstream
		// connection.
		//
		// Both approaches produce the same result: correct SNI on
		// the upstream TLS handshake.
		clusters = append(clusters, envoyCluster{
			Name:           "mitm_forward_proxy_cluster",
			ConnectTimeout: "5s",
			LBPolicy:       "CLUSTER_PROVIDED",
			ClusterType: &envoyClusterType{
				Name: "envoy.clusters.dynamic_forward_proxy",
				TypedConfig: envoyClusterDFPConfig{
					AtType:         "type.googleapis.com/envoy.extensions.clusters.dynamic_forward_proxy.v3.ClusterConfig",
					DNSCacheConfig: sharedDNSCacheConfig,
				},
			},
			TransportSocket: &envoyTransportSocket{
				Name:        "envoy.transport_sockets.tls",
				TypedConfig: upstreamTLS,
			},
			TypedExtensionProtocolOptions: map[string]any{
				"envoy.extensions.upstreams.http.v3.HttpProtocolOptions": envoyHttpProtocolOptions{
					AtType:                      "type.googleapis.com/envoy.extensions.upstreams.http.v3.HttpProtocolOptions",
					UseDownstreamProtocolConfig: envoyUseDownstreamProtocolConfig{},
				},
			},
		})
	}

	for _, fwd := range tcpForwards {
		name := fmt.Sprintf("tcp_forward_%d", fwd.Port)
		clusters = append(clusters, envoyCluster{
			Name:           name,
			ConnectTimeout: "5s",
			Type:           "STRICT_DNS",
			LBPolicy:       "ROUND_ROBIN",
			LoadAssignment: &envoyLoadAssignment{
				ClusterName: name,
				Endpoints: []envoyEndpoint{{
					LBEndpoints: []envoyLBEndpoint{{
						Endpoint: envoyEndpointAddress{
							Address: envoyAddress{SocketAddress: envoySocketAddress{
								Address: fwd.Host, PortValue: fwd.Port,
							}},
						},
					}},
				}},
			},
		})
	}

	return clusters
}

// GenerateEnvoyConfig builds an Envoy bootstrap YAML configuration
// using per-port rule resolution. Port 443 traffic is matched by TLS
// SNI, port 80 by HTTP Host header. Domains with path restrictions
// are MITM'd via TLS termination using certs from certsDir. Each
// [TCPForward] entry creates a plain TCP proxy listener with a
// STRICT_DNS cluster. Open ports (from toPorts-only rules) get
// catch-all passthrough chains.
func GenerateEnvoyConfig(cfg *SandboxConfig, certsDir, caBundlePath string) (string, error) {
	accessLog := BuildAccessLog(cfg.Logging)

	resolvedPorts := cfg.ResolvePorts()

	resolvedPortSet := make(map[int]bool, len(resolvedPorts))
	for _, p := range resolvedPorts {
		resolvedPortSet[p] = true
	}

	openPorts := cfg.ResolveOpenPorts()

	openPortSet := make(map[int]bool, len(openPorts))
	for _, p := range openPorts {
		openPortSet[p] = true
	}

	var listeners []envoyListener

	// Only build 443/80 listeners when ResolvePorts includes them.
	if resolvedPortSet[443] {
		rules443 := cfg.ResolveRulesForPort(443)

		if openPortSet[443] {
			rules443 = stripL7Restrictions(rules443)
		}

		listeners = append(
			listeners,
			buildTLSListener(
				"tls_passthrough",
				15443,
				443,
				"tls_passthrough",
				rules443,
				openPortSet[443],
				accessLog,
				certsDir,
			),
		)
	}

	if resolvedPortSet[80] {
		rules80 := cfg.ResolveRulesForPort(80)

		if openPortSet[80] {
			rules80 = stripL7Restrictions(rules80)
		}

		listeners = append(listeners, buildHTTPForwardListener(rules80, openPortSet[80], accessLog))
	}

	for _, fwd := range cfg.TCPForwards {
		name := fmt.Sprintf("tcp_forward_%d", fwd.Port)
		listeners = append(listeners, buildTCPForwardListener(name, proxyPortBase+fwd.Port, name, accessLog))
	}

	// Extra port listeners support both passthrough and MITM (when L7
	// rules with path/method restrictions are present and certsDir is set).
	for _, p := range cfg.ExtraPorts() {
		rulesP := cfg.ResolveRulesForPort(p)

		if openPortSet[p] {
			rulesP = stripL7Restrictions(rulesP)
		}

		listeners = append(listeners, buildTLSListener(
			fmt.Sprintf("tls_passthrough_%d", p),
			proxyPortBase+p, p,
			fmt.Sprintf("tls_passthrough_%d", p),
			rulesP, openPortSet[p], accessLog, certsDir,
		))
	}

	// Use global rules for cluster determination.
	allRules := cfg.ResolveRules()

	bootstrap := envoyBootstrap{
		OverloadManager: envoyOverloadManager{
			ResourceMonitors: []envoyNamedTyped{{
				Name: "envoy.resource_monitors.global_downstream_max_connections",
				TypedConfig: envoyDownstreamConnectionsConfig{
					AtType:                         "type.googleapis.com/envoy.extensions.resource_monitors.downstream_connections.v3.DownstreamConnectionsConfig",
					MaxActiveDownstreamConnections: 65535,
				},
			}},
		},
		StaticResources: envoyStaticResources{
			Listeners: listeners,
			Clusters:  buildClusters(allRules, cfg.TCPForwards, caBundlePath),
		},
	}

	var buf bytes.Buffer

	err := niceyaml.NewEncoder(&buf).Encode(bootstrap)
	if err != nil {
		return "", fmt.Errorf("marshaling envoy config: %w", err)
	}

	return buf.String(), nil
}
