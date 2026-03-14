package sandbox_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/toolchains/dev/sandbox"
)

// egressRules is a test helper that returns a pointer to a slice of sandbox.EgressRule.
func egressRules(rules ...sandbox.EgressRule) *[]sandbox.EgressRule {
	return &rules
}

// boolPtr is a test helper that returns a pointer to a bool.
func boolPtr(b bool) *bool {
	return &b
}

func TestParseConfig(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		yaml           string
		wantRules      int
		wantDomains    []string
		notWantDomains []string
		err            error
	}{
		"FQDN and CIDR rules": {
			yaml: `
egress:
  - toCIDRSet:
      - cidr: 0.0.0.0/0
        except:
          - 10.0.0.0/8
  - toFQDNs:
      - matchName: "github.com"
      - matchPattern: "*.github.com"
    toPorts:
      - ports:
          - port: "443"
          - port: "80"
  - toFQDNs:
      - matchName: api.company.com
      - matchPattern: "*.internal.company.com"
    toPorts:
      - ports:
          - port: "443"
          - port: "80"
`,
			wantRules:   3,
			wantDomains: []string{"github.com", "*.github.com", "api.company.com", "*.internal.company.com"},
		},
		"single FQDN rule": {
			yaml: `
egress:
  - toFQDNs:
      - matchName: custom.example.com
    toPorts:
      - ports:
          - port: "443"
`,
			wantRules:   1,
			wantDomains: []string{"custom.example.com"},
		},
		"FQDN with L7 path restrictions": {
			yaml: `
egress:
  - toFQDNs:
      - matchName: api.example.com
    toPorts:
      - ports:
          - port: "443"
        rules:
          http:
            - path: /v1/completions
            - path: /v1/models
  - toFQDNs:
      - matchName: cdn.example.com
    toPorts:
      - ports:
          - port: "443"
`,
			wantRules:   2,
			wantDomains: []string{"api.example.com", "cdn.example.com"},
		},
		"FQDN with L7 method restrictions": {
			yaml: `
egress:
  - toFQDNs:
      - matchName: api.example.com
    toPorts:
      - ports:
          - port: "443"
        rules:
          http:
            - method: GET
            - method: POST
  - toFQDNs:
      - matchName: cdn.example.com
    toPorts:
      - ports:
          - port: "443"
`,
			wantRules:   2,
			wantDomains: []string{"api.example.com", "cdn.example.com"},
		},
		"FQDN without toPorts rejected": {
			yaml: `
egress:
  - toFQDNs:
      - matchName: example.com
`,
			err: sandbox.ErrFQDNRequiresPorts,
		},
		"FQDN selector empty": {
			yaml: `
egress:
  - toFQDNs:
      - {}
`,
			err: sandbox.ErrFQDNSelectorEmpty,
		},
		"empty egress rule is valid (deny-all)": {
			yaml: `
egress:
  - {}
`,
			wantRules: 1,
		},
		"absent egress means unrestricted": {
			yaml:      `logging: false`,
			wantRules: 0,
		},
		"empty egress list parses as unrestricted": {
			yaml:      `egress: []`,
			wantRules: 0,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg, err := sandbox.ParseConfig([]byte(tt.yaml))
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
				return
			}

			require.NoError(t, err)

			if tt.wantRules > 0 {
				assert.Len(t, cfg.EgressRules(), tt.wantRules)
			}

			domains := cfg.ResolveDomains()

			for _, d := range tt.wantDomains {
				assert.Contains(t, domains, d)
			}

			for _, d := range tt.notWantDomains {
				assert.NotContains(t, domains, d)
			}
		})
	}
}

func TestParseConfigEgressSemantics(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		yaml             string
		wantUnrestricted bool
		wantBlocked      bool
		wantRulesOnly    bool
	}{
		"absent egress": {
			yaml:             `logging: false`,
			wantUnrestricted: true,
		},
		"null egress": {
			yaml:             `egress: null`,
			wantUnrestricted: true,
		},
		"empty egress list is unrestricted": {
			yaml:             `egress: []`,
			wantUnrestricted: true,
		},
		"empty rule is deny-all": {
			yaml: `
egress:
  - {}
`,
			wantBlocked: true,
		},
		"rules with selectors": {
			yaml: `
egress:
  - toFQDNs:
      - matchName: example.com
    toPorts:
      - ports:
          - port: "443"
`,
		},
		"enableDefaultDeny true with empty egress is unrestricted": {
			yaml: `
enableDefaultDeny:
  egress: true
egress: []
`,
			wantUnrestricted: true,
		},
		"enableDefaultDeny true with absent egress is unrestricted": {
			yaml: `
enableDefaultDeny:
  egress: true
`,
			wantUnrestricted: true,
		},
		"enableDefaultDeny false with rules is rules-only": {
			yaml: `
enableDefaultDeny:
  egress: false
egress:
  - toFQDNs:
      - matchName: example.com
    toPorts:
      - ports:
          - port: "443"
`,
			wantRulesOnly: true,
		},
		"enableDefaultDeny false with empty egress is unrestricted": {
			yaml: `
enableDefaultDeny:
  egress: false
egress: []
`,
			wantUnrestricted: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg, err := sandbox.ParseConfig([]byte(tt.yaml))
			require.NoError(t, err)
			assert.Equal(t, tt.wantUnrestricted, cfg.IsEgressUnrestricted())
			assert.Equal(t, tt.wantBlocked, cfg.IsEgressBlocked())
			assert.Equal(t, tt.wantRulesOnly, cfg.IsEgressRulesOnly())
		})
	}
}

func TestIsEgressRulesOnly(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg  *sandbox.SandboxConfig
		want bool
	}{
		"nil egress": {
			cfg: &sandbox.SandboxConfig{},
		},
		"empty egress": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules()},
		},
		"empty rule": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{})},
		},
		"rules with default-deny": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
		},
		"rules without default-deny": {
			cfg: &sandbox.SandboxConfig{
				EnableDefaultDeny: sandbox.DefaultDenyConfig{Egress: boolPtr(false)},
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			want: true,
		},
		"empty egress without default-deny": {
			cfg: &sandbox.SandboxConfig{
				EnableDefaultDeny: sandbox.DefaultDenyConfig{Egress: boolPtr(false)},
				Egress:            egressRules(),
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.cfg.IsEgressRulesOnly())
		})
	}
}

func TestEmptyRuleWithFQDNSemantics(t *testing.T) {
	t.Parallel()

	// Empty rule + FQDN rule under default-deny: the empty rule
	// contributes nothing (no selectors), but the FQDN rule applies.
	cfg := &sandbox.SandboxConfig{Egress: egressRules(
		sandbox.EgressRule{},
		sandbox.EgressRule{
			ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
			ToPorts: []sandbox.PortRule{{
				Ports: []sandbox.Port{{Port: "443"}},
				Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
			}},
		},
	)}

	assert.False(t, cfg.IsEgressUnrestricted(), "empty rule triggers default-deny, not unrestricted")
	assert.False(t, cfg.IsEgressBlocked(), "FQDN sibling prevents blocked state")
	assert.Equal(t, []int{443}, cfg.ResolvePorts(), "FQDN rule contributes ports")
	assert.Equal(t, []string{"api.example.com"}, cfg.ResolveDomains(), "FQDN rule contributes domains")
}

func TestParseTCPForwards(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		yaml string
		want []sandbox.TCPForward
	}{
		"single forward": {
			yaml: `
tcpForwards:
  - port: 22
    host: github.com
`,
			want: []sandbox.TCPForward{{Port: 22, Host: "github.com"}},
		},
		"multiple forwards": {
			yaml: `
tcpForwards:
  - port: 22
    host: github.com
  - port: 3306
    host: db.internal.com
`,
			want: []sandbox.TCPForward{
				{Port: 22, Host: "github.com"},
				{Port: 3306, Host: "db.internal.com"},
			},
		},
		"no forwards": {
			yaml: `
egress:
  - toFQDNs:
      - matchName: example.com
    toPorts:
      - ports:
          - port: "443"
`,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg, err := sandbox.ParseConfig([]byte(tt.yaml))
			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.TCPForwards)
		})
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg *sandbox.SandboxConfig
		err error
	}{
		"valid with forwards": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
				TCPForwards: []sandbox.TCPForward{{Port: 22, Host: "github.com"}},
			},
		},
		"valid no forwards": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
		},
		"valid FQDN with L7 paths": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{
							{Path: "/v1/"},
							{Path: "/v2/"},
						}},
					}},
				}),
			},
		},
		"FQDN selector empty": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{}},
				}),
			},
			err: sandbox.ErrFQDNSelectorEmpty,
		},
		"FQDN without toPorts rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
				}),
			},
			err: sandbox.ErrFQDNRequiresPorts,
		},
		"FQDN with empty Ports list rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
					}},
				}),
			},
			err: sandbox.ErrFQDNRequiresPorts,
		},
		"empty egress rule is valid": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{}),
			},
		},
		"nil egress is valid": {
			cfg: &sandbox.SandboxConfig{},
		},
		"empty egress slice is valid": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(),
			},
		},
		"invalid path regex": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{
							{Path: "[unclosed"},
						}},
					}},
				}),
			},
			err: sandbox.ErrPathInvalidRegex,
		},
		"valid regex paths": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{
							{Path: "/v1/.*"},
							{Path: "/api/v[12]/.*"},
						}},
					}},
				}),
			},
		},
		"duplicate forward port": {
			cfg: &sandbox.SandboxConfig{
				TCPForwards: []sandbox.TCPForward{
					{Port: 22, Host: "github.com"},
					{Port: 22, Host: "gitlab.com"},
				},
			},
			err: sandbox.ErrDuplicateTCPForwardPort,
		},
		"forward port conflicts with resolved port": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8080"}}}},
				}),
				TCPForwards: []sandbox.TCPForward{{Port: 8080, Host: "example.com"}},
			},
			err: sandbox.ErrTCPForwardPortConflict,
		},
		"invalid zero port": {
			cfg: &sandbox.SandboxConfig{
				TCPForwards: []sandbox.TCPForward{{Port: 0, Host: "example.com"}},
			},
			err: sandbox.ErrInvalidTCPForward,
		},
		"invalid negative port": {
			cfg: &sandbox.SandboxConfig{
				TCPForwards: []sandbox.TCPForward{{Port: -1, Host: "example.com"}},
			},
			err: sandbox.ErrInvalidTCPForward,
		},
		"invalid empty host": {
			cfg: &sandbox.SandboxConfig{
				TCPForwards: []sandbox.TCPForward{{Port: 22, Host: ""}},
			},
			err: sandbox.ErrInvalidTCPForward,
		},
		"tcp forwards with blocked egress": {
			cfg: &sandbox.SandboxConfig{
				Egress:      egressRules(sandbox.EgressRule{}),
				TCPForwards: []sandbox.TCPForward{{Port: 22, Host: "github.com"}},
			},
			err: sandbox.ErrTCPForwardRequiresEgress,
		},
		"valid methods": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{
							{Method: "GET"},
							{Method: "POST"},
						}},
					}},
				}),
			},
		},
		"lowercase method is valid regex": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{
							{Method: "get"},
						}},
					}},
				}),
			},
		},
		"custom method is valid regex": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{
							{Method: "FOOBAR"},
						}},
					}},
				}),
			},
		},
		"method regex pattern": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{
							{Method: "GET|POST"},
						}},
					}},
				}),
			},
		},
		"invalid method regex": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{
							{Method: "[unclosed"},
						}},
					}},
				}),
			},
			err: sandbox.ErrMethodInvalidRegex,
		},
		"invalid empty method string": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{
							{Method: ""},
						}},
					}},
				}),
			},
			// Empty method is allowed (means "all methods").
		},
		"FQDN with toCIDR rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					ToCIDR:  []string{"10.0.0.0/8"},
				}),
			},
			err: sandbox.ErrFQDNWithCIDR,
		},
		"toCIDR with toCIDRSet rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDR:    []string{"10.0.0.0/8"},
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
				}),
			},
			err: sandbox.ErrCIDRAndCIDRSetMixed,
		},
		"toCIDR and toCIDRSet in separate rules valid": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{ToCIDR: []string{"10.0.0.0/8"}},
					sandbox.EgressRule{ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}}},
				),
			},
		},
		"FQDN with toCIDR and L7 rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToCIDR:  []string{"10.0.0.0/8"},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
					}},
				}),
			},
			err: sandbox.ErrFQDNWithCIDR,
		},
		"FQDN with toCIDRSet rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs:   []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
					}},
				}),
			},
			err: sandbox.ErrFQDNWithCIDR,
		},
		"FQDN selector both matchName and matchPattern": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com", MatchPattern: "*.example.com"}},
				}),
			},
			err: sandbox.ErrFQDNSelectorAmbiguous,
		},
		"deep wildcard accepted": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "**.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
		},
		"triple star wildcard accepted": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "***.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
		},
		"bare double star accepted": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "**"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
		},
		"mid-pattern double star rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "test.**.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			err: sandbox.ErrFQDNPatternPartialWildcard,
		},
		"bare wildcard accepted": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
		},
		"bare wildcard with specific ports": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}, {Port: "80"}}}},
				}),
			},
		},
		"partial wildcard mid-label rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "api.*-staging.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			err: sandbox.ErrFQDNPatternPartialWildcard,
		},
		"partial wildcard suffix rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "example.com.*"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			err: sandbox.ErrFQDNPatternPartialWildcard,
		},
		"multiple wildcards rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*.*.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			err: sandbox.ErrFQDNPatternPartialWildcard,
		},
		"valid leading wildcard prefix": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
		},
		"valid toCIDR": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDR: []string{"10.0.0.0/8"},
				}),
			},
		},
		"bare IPv4 in toCIDR accepted": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDR: []string{"10.0.0.1"},
				}),
			},
		},
		"bare IPv6 in toCIDR accepted": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDR: []string{"fd00::1"},
				}),
			},
		},
		"bare IPv4-mapped IPv6 in toCIDR accepted": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDR: []string{"::ffff:10.0.0.1"},
				}),
			},
		},
		"invalid toCIDR": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDR: []string{"not-a-cidr"},
				}),
			},
			err: sandbox.ErrCIDRInvalid,
		},
		"valid protocol TCP/UDP/ANY": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{
						{Port: "80", Protocol: "TCP"},
						{Port: "53", Protocol: "UDP"},
						{Port: "443", Protocol: "ANY"},
					}}},
				}),
			},
		},
		"SCTP protocol": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "80", Protocol: "SCTP"}}}},
				}),
			},
		},
		"invalid protocol": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "80", Protocol: "ICMP"}}}},
				}),
			},
			err: sandbox.ErrProtocolInvalid,
		},
		"valid endPort": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8000", EndPort: 9000}}}},
				}),
			},
		},
		"endPort less than port": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "9000", EndPort: 8000}}}},
				}),
			},
			err: sandbox.ErrEndPortInvalid,
		},
		"endPort with toFQDNs rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8000", EndPort: 9000}}}},
				}),
			},
			err: sandbox.ErrEndPortWithFQDN,
		},
		"invalid CIDR": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "not-a-cidr"}},
				}),
			},
			err: sandbox.ErrCIDRInvalid,
		},
		"invalid CIDR except": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0", Except: []string{"bad"}}},
				}),
			},
			err: sandbox.ErrCIDRInvalid,
		},
		"valid CIDR rule": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{
						CIDR:   "0.0.0.0/0",
						Except: []string{"10.0.0.0/8", "172.16.0.0/12"},
					}},
				}),
			},
		},
		"port empty string": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: ""}}}},
				}),
			},
			err: sandbox.ErrPortEmpty,
		},
		"port invalid string": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "abc"}}}},
				}),
			},
			err: sandbox.ErrPortInvalid,
		},
		"except not subnet of parent": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{
						CIDR:   "10.0.0.0/8",
						Except: []string{"192.168.0.0/16"},
					}},
				}),
			},
			err: sandbox.ErrExceptNotSubnet,
		},
		"except subnet valid": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{
						CIDR:   "10.0.0.0/8",
						Except: []string{"10.1.0.0/16"},
					}},
				}),
			},
		},
		"except equal to parent valid": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{
						CIDR:   "10.0.0.0/8",
						Except: []string{"10.0.0.0/8"},
					}},
				}),
			},
		},
		"except broader than parent": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{
						CIDR:   "10.0.0.0/16",
						Except: []string{"10.0.0.0/8"},
					}},
				}),
			},
			err: sandbox.ErrExceptNotSubnet,
		},
		"except different address family": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{
						CIDR:   "10.0.0.0/8",
						Except: []string{"fd00::/8"},
					}},
				}),
			},
			err: sandbox.ErrExceptNotSubnet,
		},
		"L7 on toPorts-only rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
					}},
				}),
			},
			err: sandbox.ErrL7RequiresFQDN,
		},
		"empty HTTP on toPorts-only valid": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{}},
					}},
				}),
			},
		},
		"toPorts-only without L7 valid": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8080"}}}},
				}),
			},
		},
		"empty ports on non-FQDN rule valid": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToPorts: []sandbox.PortRule{{}},
				}),
			},
		},
		"empty ports with CIDR valid": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
					ToPorts:   []sandbox.PortRule{{}},
				}),
			},
		},
		"wildcard matchPattern with L7 rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
					}},
				}),
			},
			err: sandbox.ErrWildcardWithL7,
		},
		"wildcard matchPattern without L7 allowed": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
		},
		"path regex too long rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{
							{Path: strings.Repeat("a", 1001)},
						}},
					}},
				}),
			},
			err: sandbox.ErrPathInvalidRegex,
		},
		"except CIDR with host bits uses network base": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{
						CIDR:   "10.0.0.0/8",
						Except: []string{"10.1.2.3/16"},
					}},
				}),
			},
		},
		"lowercase protocol tcp normalized": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "tcp"}}}},
				}),
			},
		},
		"mixed case protocol Tcp normalized": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "Tcp"}}}},
				}),
			},
		},
		"matchName case normalized": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "GitHub.COM"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
		},
		"matchName trailing dot stripped": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com."}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
		},
		"matchPattern case normalized": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*.Example.COM"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
		},
		"matchPattern trailing dot stripped": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*.example.com."}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
		},
		"matchName only dot rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "."}},
				}),
			},
			err: sandbox.ErrFQDNSelectorEmpty,
		},
		"HTTP host field rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{
							{Path: "/v1/", Host: "api.example.com"},
						}},
					}},
				}),
			},
			err: sandbox.ErrHTTPHostUnsupported,
		},
		"HTTP headers field rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{
							{Path: "/v1/", Headers: []string{"X-Custom: value"}},
						}},
					}},
				}),
			},
			err: sandbox.ErrHTTPHeadersUnsupported,
		},
		"L7 on port 8443 rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "8443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
					}},
				}),
			},
			err: sandbox.ErrL7OnUnsupportedPort,
		},
		"L7 on port 80 valid": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "80"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
					}},
				}),
			},
		},
		"L7 with UDP protocol rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
					}},
				}),
			},
			err: sandbox.ErrL7RequiresTCP,
		},
		"L7 with SCTP protocol rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "80", Protocol: "SCTP"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
					}},
				}),
			},
			err: sandbox.ErrL7RequiresTCP,
		},
		"L7 with ANY protocol rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443", Protocol: "ANY"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
					}},
				}),
			},
			err: sandbox.ErrL7RequiresTCP,
		},
		"L7 with explicit TCP protocol valid": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443", Protocol: "TCP"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
					}},
				}),
			},
		},
		"L7 with empty protocol valid": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
					}},
				}),
			},
		},
		"L7 with mixed TCP and UDP ports rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{
							{Port: "80", Protocol: "TCP"},
							{Port: "443", Protocol: "UDP"},
						},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
					}},
				}),
			},
			err: sandbox.ErrL7RequiresTCP,
		},
		"L7 with lowercase udp normalized then rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443", Protocol: "udp"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
					}},
				}),
			},
			err: sandbox.ErrL7RequiresTCP,
		},
		"empty HTTP rules with UDP protocol valid": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{}},
					}},
				}),
			},
		},
		"matchName with spaces rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example .com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			err: sandbox.ErrFQDNNameInvalidChars,
		},
		"matchName with colon rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example:8080.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			err: sandbox.ErrFQDNNameInvalidChars,
		},
		"matchName with slash rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com/path"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			err: sandbox.ErrFQDNNameInvalidChars,
		},
		"matchName with semicolon rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example;.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			err: sandbox.ErrFQDNNameInvalidChars,
		},
		"matchPattern with spaces rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*.example .com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			err: sandbox.ErrFQDNPatternInvalidChars,
		},
		"matchPattern with semicolon rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*.example;.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			err: sandbox.ErrFQDNPatternInvalidChars,
		},
		"matchPattern with colon rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*.example:8080.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			err: sandbox.ErrFQDNPatternInvalidChars,
		},
		"matchPattern with slash rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*.example.com/path"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			err: sandbox.ErrFQDNPatternInvalidChars,
		},
		"matchName exceeding 255 chars rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: strings.Repeat("a", 256)}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			err: sandbox.ErrFQDNTooLong,
		},
		"matchPattern exceeding 255 chars rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*." + strings.Repeat("a", 254)}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			err: sandbox.ErrFQDNTooLong,
		},
		"matchName at exactly 255 chars valid": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: strings.Repeat("a", 255)}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
		},
		"matchName with underscore valid": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "_dmarc.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
		},
		"matchName with hyphen valid": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "my-service.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
		},
		"punycode IDN matchName valid": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "xn--n3h.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
		},
		"raw unicode matchName rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "\u2603.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			err: sandbox.ErrFQDNNameInvalidChars,
		},
		"port 0 without L7 accepted": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "0"}}}},
				}),
			},
		},
		"port 0 with FQDN accepted": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "0"}}}},
				}),
			},
		},
		"port 0 with L7 rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "0"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
					}},
				}),
			},
			err: sandbox.ErrL7WithWildcardPort,
		},
		"port 0 with endPort rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "0", EndPort: 443}}}},
				}),
			},
			err: sandbox.ErrEndPortWithWildcardPort,
		},
		"port 0 with UDP accepted": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "0", Protocol: "UDP"}}}},
				}),
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := tt.cfg.Validate()
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestTCPForwardHosts(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		forwards []sandbox.TCPForward
		want     []string
	}{
		"deduplicated and sorted": {
			forwards: []sandbox.TCPForward{
				{Port: 22, Host: "github.com"},
				{Port: 3306, Host: "db.example.com"},
				{Port: 5432, Host: "github.com"},
			},
			want: []string{"db.example.com", "github.com"},
		},
		"empty": {},
		"single": {
			forwards: []sandbox.TCPForward{{Port: 22, Host: "gitlab.com"}},
			want:     []string{"gitlab.com"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := &sandbox.SandboxConfig{TCPForwards: tt.forwards}
			assert.Equal(t, tt.want, cfg.TCPForwardHosts())
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := sandbox.DefaultConfig()

	rules := cfg.EgressRules()
	// Single egress rule with FQDNs only (no CIDRs).
	require.Len(t, rules, 1)
	assert.Empty(t, rules[0].ToCIDRSet)
	assert.NotEmpty(t, rules[0].ToFQDNs)

	// Check some expected domains.
	domains := cfg.ResolveDomains()

	for _, want := range []string{"github.com", "golang.org", "anthropic.com"} {
		assert.Contains(t, domains, want)
	}

	assert.Nil(t, cfg.TCPForwards)
}

func TestResolveDomains(t *testing.T) {
	t.Parallel()

	cfg := &sandbox.SandboxConfig{
		Egress: egressRules(
			sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{
					{MatchName: "github.com"},
					{MatchName: "extra.com"},
				},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
			},
			sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{
					{MatchName: "github.com"},
				},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
			},
		),
	}

	domains := cfg.ResolveDomains()

	// Github.com appears in both rules but should be deduplicated.
	count := 0
	for _, d := range domains {
		if d == "github.com" {
			count++
		}
	}

	assert.Equal(t, 1, count, "github.com should appear exactly once")
	assert.True(t, sort.StringsAreSorted(domains))
}

func TestResolveRules(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg      *sandbox.SandboxConfig
		want     []sandbox.ResolvedRule
		wantNone bool
	}{
		"simple FQDN rule": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "registry.npmjs.org"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			want: []sandbox.ResolvedRule{{Domain: "registry.npmjs.org"}},
		},
		"FQDN with L7 paths": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{
						{MatchName: "api.example.com"},
						{MatchName: "cdn.example.com"},
					},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{
							{Path: "/v1/"},
							{Path: "/v2/"},
						}},
					}},
				}),
			},
			want: []sandbox.ResolvedRule{
				{Domain: "api.example.com", HTTPRules: []sandbox.ResolvedHTTPRule{{Path: "/v1/"}, {Path: "/v2/"}}},
				{Domain: "cdn.example.com", HTTPRules: []sandbox.ResolvedHTTPRule{{Path: "/v1/"}, {Path: "/v2/"}}},
			},
		},
		"merge L7 across rules": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
						ToPorts: []sandbox.PortRule{{
							Ports: []sandbox.Port{{Port: "443"}},
							Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
						}},
					},
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
						ToPorts: []sandbox.PortRule{{
							Ports: []sandbox.Port{{Port: "443"}},
							Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v2/"}}},
						}},
					},
				),
			},
			want: []sandbox.ResolvedRule{
				{Domain: "api.example.com", HTTPRules: []sandbox.ResolvedHTTPRule{{Path: "/v1/"}, {Path: "/v2/"}}},
			},
		},
		"plain L4 wins over L7 paths": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
						ToPorts: []sandbox.PortRule{{
							Ports: []sandbox.Port{{Port: "443"}},
							Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
						}},
					},
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
				),
			},
			want: []sandbox.ResolvedRule{
				{Domain: "api.example.com"},
			},
		},
		"deduplicate paths": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{
							{Path: "/v1/"},
							{Path: "/v1/"},
						}},
					}},
				}),
			},
			want: []sandbox.ResolvedRule{
				{Domain: "api.example.com", HTTPRules: []sandbox.ResolvedHTTPRule{{Path: "/v1/"}}},
			},
		},
		"methods merge across rules": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
						ToPorts: []sandbox.PortRule{{
							Ports: []sandbox.Port{{Port: "443"}},
							Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Method: "GET"}}},
						}},
					},
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
						ToPorts: []sandbox.PortRule{{
							Ports: []sandbox.Port{{Port: "443"}},
							Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Method: "POST"}}},
						}},
					},
				),
			},
			want: []sandbox.ResolvedRule{
				{Domain: "api.example.com", HTTPRules: []sandbox.ResolvedHTTPRule{{Method: "GET"}, {Method: "POST"}}},
			},
		},
		"plain L4 wins over methods": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
						ToPorts: []sandbox.PortRule{{
							Ports: []sandbox.Port{{Port: "443"}},
							Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Method: "GET"}}},
						}},
					},
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
				),
			},
			want: []sandbox.ResolvedRule{
				{Domain: "api.example.com"},
			},
		},
		"dedup methods": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{
							{Method: "GET"},
							{Method: "GET"},
						}},
					}},
				}),
			},
			want: []sandbox.ResolvedRule{
				{Domain: "api.example.com", HTTPRules: []sandbox.ResolvedHTTPRule{{Method: "GET"}}},
			},
		},
		"paths and methods paired": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{
							{Path: "/v1/", Method: "GET"},
							{Path: "/v1/", Method: "POST"},
						}},
					}},
				}),
			},
			want: []sandbox.ResolvedRule{
				{Domain: "api.example.com", HTTPRules: []sandbox.ResolvedHTTPRule{
					{Method: "GET", Path: "/v1/"},
					{Method: "POST", Path: "/v1/"},
				}},
			},
		},
		"HTTP rules are paired not cross-producted": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{
							{Method: "GET", Path: "/api"},
							{Method: "POST", Path: "/submit"},
						}},
					}},
				}),
			},
			want: []sandbox.ResolvedRule{
				{Domain: "api.example.com", HTTPRules: []sandbox.ResolvedHTTPRule{
					{Method: "GET", Path: "/api"},
					{Method: "POST", Path: "/submit"},
				}},
			},
		},
		"CIDR-only rule skipped": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}}},
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
				),
			},
			want: []sandbox.ResolvedRule{{Domain: "example.com"}},
		},
		"matchPattern used as domain": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			want: []sandbox.ResolvedRule{{Domain: "*.example.com"}},
		},
		"nil egress returns empty": {
			cfg:      &sandbox.SandboxConfig{},
			wantNone: true,
		},
		"empty egress returns empty": {
			cfg:      &sandbox.SandboxConfig{Egress: egressRules()},
			wantNone: true,
		},
		"empty HTTP propagates as unrestricted through ResolveRules": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{}},
					}},
				}),
			},
			want: []sandbox.ResolvedRule{{Domain: "api.example.com"}},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rules := tt.cfg.ResolveRules()
			if tt.wantNone {
				assert.Empty(t, rules)
			} else {
				assert.Equal(t, tt.want, rules)
			}
		})
	}
}

func TestResolveRulesForPort(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg      *sandbox.SandboxConfig
		port     int
		want     []sandbox.ResolvedRule
		wantNone bool
	}{
		"domain scoped to port 443 only - matching": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchName: "github.com"}},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
			})},
			port: 443,
			want: []sandbox.ResolvedRule{{Domain: "github.com"}},
		},
		"domain scoped to port 443 only - non-matching": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchName: "github.com"}},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
			})},
			port:     80,
			wantNone: true,
		},
		"domain with multiple ports matches each": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchName: "github.com"}},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}, {Port: "80"}, {Port: "8080"}}}},
			})},
			port: 8080,
			want: []sandbox.ResolvedRule{{Domain: "github.com"}},
		},
		"per-port L7 scoping": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(
				sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
					}},
				},
				sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "8080"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v2/"}}},
					}},
				},
			)},
			port: 443,
			want: []sandbox.ResolvedRule{
				{Domain: "api.example.com", HTTPRules: []sandbox.ResolvedHTTPRule{{Path: "/v1/"}}},
			},
		},
		"per-port L7 scoping - other port": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(
				sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
					}},
				},
				sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "8080"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v2/"}}},
					}},
				},
			)},
			port: 8080,
			want: []sandbox.ResolvedRule{
				{Domain: "api.example.com", HTTPRules: []sandbox.ResolvedHTTPRule{{Path: "/v2/"}}},
			},
		},
		"same domain same port merges L7": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(
				sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
					}},
				},
				sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v2/"}}},
					}},
				},
			)},
			port: 443,
			want: []sandbox.ResolvedRule{
				{Domain: "api.example.com", HTTPRules: []sandbox.ResolvedHTTPRule{{Path: "/v1/"}, {Path: "/v2/"}}},
			},
		},
		"empty Ports list matches all ports": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
				ToPorts: []sandbox.PortRule{
					{Ports: []sandbox.Port{{Port: "443"}}},
					{Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}}},
				},
			})},
			port: 9999,
			want: []sandbox.ResolvedRule{
				{Domain: "api.example.com", HTTPRules: []sandbox.ResolvedHTTPRule{{Path: "/v1/"}}},
			},
		},
		"plain L4 nullifies sibling L7 on same port": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
				ToPorts: []sandbox.PortRule{
					{Ports: []sandbox.Port{{Port: "443"}}},
					{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Method: "GET"}}},
					},
				},
			})},
			port: 443,
			want: []sandbox.ResolvedRule{{Domain: "api.example.com"}},
		},
		"toPorts-only rule excluded": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8080"}}}},
			})},
			port:     8080,
			wantNone: true,
		},
		"mixed rules per port": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(
				sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "always.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "80"}, {Port: "443"}}}},
				},
				sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "only443.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				},
			)},
			port: 80,
			want: []sandbox.ResolvedRule{{Domain: "always.com"}},
		},
		"empty HTTP list produces unrestricted rule": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
				ToPorts: []sandbox.PortRule{{
					Ports: []sandbox.Port{{Port: "443"}},
					Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{}},
				}},
			})},
			port: 443,
			want: []sandbox.ResolvedRule{{Domain: "api.example.com"}},
		},
		"empty HTTP merged with L7 rules is unrestricted": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(
				sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{}},
					}},
				},
				sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
					}},
				},
			)},
			port: 443,
			want: []sandbox.ResolvedRule{{Domain: "api.example.com"}},
		},
		"empty HTTP plus plain L4 is unrestricted": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(
				sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "443"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{}},
					}},
				},
				sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				},
			)},
			port: 443,
			want: []sandbox.ResolvedRule{{Domain: "api.example.com"}},
		},
		"rules nil HTTP is plain L4": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
				ToPorts: []sandbox.PortRule{{
					Ports: []sandbox.Port{{Port: "443"}},
					Rules: &sandbox.L7Rules{},
				}},
			})},
			port: 443,
			want: []sandbox.ResolvedRule{{Domain: "api.example.com"}},
		},
		"separate FQDN and CIDR rules contribute domains": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(
				sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				},
				sandbox.EgressRule{
					ToCIDR:  []string{"10.0.0.0/8"},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				},
			)},
			port: 443,
			want: []sandbox.ResolvedRule{{Domain: "api.example.com"}},
		},
		"endPort range matches port within range": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "400", EndPort: 500}}}},
			})},
			port: 450,
			want: []sandbox.ResolvedRule{{Domain: "api.example.com"}},
		},
		"endPort range does not match port outside range": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "400", EndPort: 500}}}},
			})},
			port:     501,
			wantNone: true,
		},
		"endPort range matches start port": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "400", EndPort: 500}}}},
			})},
			port: 400,
			want: []sandbox.ResolvedRule{{Domain: "api.example.com"}},
		},
		"endPort range matches end port": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "400", EndPort: 500}}}},
			})},
			port: 500,
			want: []sandbox.ResolvedRule{{Domain: "api.example.com"}},
		},
		"deep wildcard preserves double-star domain": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "**.example.com"}},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
			})},
			port: 443,
			want: []sandbox.ResolvedRule{{Domain: "**.example.com"}},
		},
		"bare double star resolves as single star": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "**"}},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
			})},
			port: 443,
			want: []sandbox.ResolvedRule{{Domain: "*"}},
		},
		"port 0 matches all target ports": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(
				sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "wildcard.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "0"}}}},
				},
				sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "specific.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				},
			)},
			port: 443,
			want: []sandbox.ResolvedRule{
				{Domain: "specific.example.com"},
				{Domain: "wildcard.example.com"},
			},
		},
		"port 0 matches non-standard ports": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(
				sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "wildcard.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "0"}}}},
				},
				sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "specific.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8080"}}}},
				},
			)},
			port: 8080,
			want: []sandbox.ResolvedRule{
				{Domain: "specific.example.com"},
				{Domain: "wildcard.example.com"},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rules := tt.cfg.ResolveRulesForPort(tt.port)
			if tt.wantNone {
				assert.Empty(t, rules)
			} else {
				assert.Equal(t, tt.want, rules)
			}
		})
	}
}

func TestResolveOpenPorts(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg  *sandbox.SandboxConfig
		want []int
	}{
		"toPorts-only rule produces open ports": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8080"}}}},
			})},
			want: []int{8080},
		},
		"rule with toFQDNs not open": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8080"}}}},
			})},
		},
		"rule with toCIDRSet not open": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
				ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8080"}}}},
			})},
		},
		"rule with toCIDR not open": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToCIDR:  []string{"0.0.0.0/0"},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8080"}}}},
			})},
		},
		"no toPorts-only rules": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
			})},
		},
		"multiple open ports": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8080"}, {Port: "9090"}}}},
			})},
			want: []int{8080, 9090},
		},
		"mixed open and domain rules": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(
				sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				},
				sandbox.EgressRule{ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "3000"}}}}},
			)},
			want: []int{3000},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := tt.cfg.ResolveOpenPorts()
			if tt.want == nil {
				assert.Empty(t, got)
			} else {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestResolveOpenPortRules(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg  *sandbox.SandboxConfig
		want []sandbox.ResolvedOpenPort
	}{
		"TCP open port": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8080", Protocol: "TCP"}}}},
			})},
			want: []sandbox.ResolvedOpenPort{{Port: 8080, Protocol: "tcp"}},
		},
		"UDP open port": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "5353", Protocol: "UDP"}}}},
			})},
			want: []sandbox.ResolvedOpenPort{{Port: 5353, Protocol: "udp"}},
		},
		"SCTP open port": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "3868", Protocol: "SCTP"}}}},
			})},
			want: []sandbox.ResolvedOpenPort{{Port: 3868, Protocol: "sctp"}},
		},
		"ANY protocol open port expands": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8080", Protocol: "ANY"}}}},
			})},
			want: []sandbox.ResolvedOpenPort{
				{Port: 8080, Protocol: "tcp"},
				{Port: 8080, Protocol: "udp"},
			},
		},
		"empty protocol open port expands": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8080"}}}},
			})},
			want: []sandbox.ResolvedOpenPort{
				{Port: 8080, Protocol: "tcp"},
				{Port: 8080, Protocol: "udp"},
			},
		},
		"rule with toFQDNs not open": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8080"}}}},
			})},
		},
		"TCP open port with endPort": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8000", EndPort: 9000, Protocol: "TCP"}}}},
			})},
			want: []sandbox.ResolvedOpenPort{
				{Port: 8000, EndPort: 9000, Protocol: "tcp"},
			},
		},
		"UDP open port with endPort": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "5000", EndPort: 6000, Protocol: "UDP"}}}},
			})},
			want: []sandbox.ResolvedOpenPort{
				{Port: 5000, EndPort: 6000, Protocol: "udp"},
			},
		},
		"ANY protocol open port with endPort expands": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8000", EndPort: 9000}}}},
			})},
			want: []sandbox.ResolvedOpenPort{
				{Port: 8000, EndPort: 9000, Protocol: "tcp"},
				{Port: 8000, EndPort: 9000, Protocol: "udp"},
			},
		},
		"endPort equal to port": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8000", EndPort: 8000, Protocol: "TCP"}}}},
			})},
			want: []sandbox.ResolvedOpenPort{
				{Port: 8000, EndPort: 8000, Protocol: "tcp"},
			},
		},
		"dedup across rules with same range": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(
				sandbox.EgressRule{
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8000", EndPort: 9000, Protocol: "TCP"}}}},
				},
				sandbox.EgressRule{
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8000", EndPort: 9000, Protocol: "TCP"}}}},
				},
			)},
			want: []sandbox.ResolvedOpenPort{
				{Port: 8000, EndPort: 9000, Protocol: "tcp"},
			},
		},
		"mixed single and range for same start port": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(
				sandbox.EgressRule{
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8000", Protocol: "TCP"}}}},
				},
				sandbox.EgressRule{
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8000", EndPort: 9000, Protocol: "TCP"}}}},
				},
			)},
			want: []sandbox.ResolvedOpenPort{
				{Port: 8000, Protocol: "tcp"},
				{Port: 8000, EndPort: 9000, Protocol: "tcp"},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := tt.cfg.ResolveOpenPortRules()
			if tt.want == nil {
				assert.Empty(t, got)
			} else {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestHasUnrestrictedOpenPorts(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg  *sandbox.SandboxConfig
		want bool
	}{
		"empty Ports list is unrestricted": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToPorts: []sandbox.PortRule{{}},
			})},
			want: true,
		},
		"port 0 counts as unrestricted": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "0"}}}},
			})},
			want: true,
		},
		"specific port is not unrestricted": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
			})},
			want: false,
		},
		"FQDN rule with port 0 not unrestricted": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "0"}}}},
			})},
			want: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.cfg.HasUnrestrictedOpenPorts())
		})
	}
}

func TestResolveFQDNNonTCPPorts(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg  *sandbox.SandboxConfig
		want []sandbox.ResolvedOpenPort
	}{
		"FQDN UDP port": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
			})},
			want: []sandbox.ResolvedOpenPort{{Port: 443, Protocol: "udp"}},
		},
		"FQDN SCTP port": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "3868", Protocol: "SCTP"}}}},
			})},
			want: []sandbox.ResolvedOpenPort{{Port: 3868, Protocol: "sctp"}},
		},
		"FQDN ANY port expands to udp": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
			})},
			want: []sandbox.ResolvedOpenPort{
				{Port: 443, Protocol: "udp"},
			},
		},
		"FQDN TCP-only returns nil": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "TCP"}}}},
			})},
		},
		"CIDR rule with UDP returns nil": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules(sandbox.EgressRule{
				ToCIDRSet: []sandbox.CIDRRule{{CIDR: "8.8.8.0/24"}},
				ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "53", Protocol: "UDP"}}}},
			})},
		},
		"unrestricted returns nil": {
			cfg: &sandbox.SandboxConfig{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := tt.cfg.ResolveFQDNNonTCPPorts()
			if tt.want == nil {
				assert.Empty(t, got)
			} else {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestIsDefaultDenyEnabled(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg  *sandbox.SandboxConfig
		want bool
	}{
		"nil egress no explicit deny": {
			cfg: &sandbox.SandboxConfig{},
		},
		"empty egress no explicit deny": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules()},
		},
		"rules present infers deny": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
			want: true,
		},
		"explicit true with nil egress is false": {
			cfg: &sandbox.SandboxConfig{
				EnableDefaultDeny: sandbox.DefaultDenyConfig{Egress: boolPtr(true)},
			},
		},
		"explicit true with empty egress is false": {
			cfg: &sandbox.SandboxConfig{
				EnableDefaultDeny: sandbox.DefaultDenyConfig{Egress: boolPtr(true)},
				Egress:            egressRules(),
			},
		},
		"explicit false overrides inference": {
			cfg: &sandbox.SandboxConfig{
				EnableDefaultDeny: sandbox.DefaultDenyConfig{Egress: boolPtr(false)},
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.cfg.IsDefaultDenyEnabled())
		})
	}
}

func TestResolvePorts(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg  *sandbox.SandboxConfig
		want []int
	}{
		"explicit ports": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{
						{Port: "80"},
						{Port: "443"},
						{Port: "8080"},
					}}},
				}),
			},
			want: []int{80, 443, 8080},
		},
		"FQDN with explicit ports": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}, {Port: "80"}}}},
				}),
			},
			want: []int{80, 443},
		},
		"nil egress returns nil": {
			cfg: &sandbox.SandboxConfig{},
		},
		"empty egress returns nil": {
			cfg: &sandbox.SandboxConfig{Egress: egressRules()},
		},
		"deduplication": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "a.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "b.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
				),
			},
			want: []int{443},
		},
		"toCIDR-only rule excluded from ports": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToCIDR:  []string{"10.0.0.0/8"},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8080"}}}},
					},
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
				),
			},
			want: []int{443},
		},
		"CIDR-only rule excluded from ports": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
						ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8080"}}}},
					},
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
				),
			},
			want: []int{443},
		},
		"empty rule returns nil ports": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{}),
			},
		},
		"CIDR-only rule returns nil": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
				}),
			},
		},
		"UDP-only port excluded": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{
						{Port: "443", Protocol: "TCP"},
						{Port: "5353", Protocol: "UDP"},
					}}},
				}),
			},
			want: []int{443},
		},
		"SCTP-only port excluded": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{
						{Port: "443", Protocol: "TCP"},
						{Port: "3868", Protocol: "SCTP"},
					}}},
				}),
			},
			want: []int{443},
		},
		"ANY protocol port included": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{
						{Port: "8080", Protocol: "ANY"},
					}}},
				}),
			},
			want: []int{8080},
		},
		"separate FQDN and CIDR rules contribute FQDN ports": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
					sandbox.EgressRule{
						ToCIDR:  []string{"10.0.0.0/8"},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
				),
			},
			want: []int{443},
		},
		"open-port range excluded from Envoy listeners": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8000", EndPort: 9000, Protocol: "TCP"}}}},
					},
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
				),
			},
			want: []int{443},
		},
		"open-port single port included in Envoy listeners": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8080", Protocol: "TCP"}}}},
					},
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
				),
			},
			want: []int{443, 8080},
		},
		"port 0 does not appear in resolved ports": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "0"}}}},
				}),
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.cfg.ResolvePorts())
		})
	}
}

func TestExtraPorts(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg  *sandbox.SandboxConfig
		want []int
	}{
		"extra ports present": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{
						{Port: "80"}, {Port: "443"}, {Port: "8080"}, {Port: "9090"},
					}}},
				}),
			},
			want: []int{8080, 9090},
		},
		"no extra ports": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{
						{Port: "80"}, {Port: "443"},
					}}},
				}),
			},
		},
		"nil egress": {
			cfg: &sandbox.SandboxConfig{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.cfg.ExtraPorts())
		})
	}
}

func TestResolveCIDRRules(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg      *sandbox.SandboxConfig
		validate bool
		wantIPv4 []sandbox.ResolvedCIDR
		wantIPv6 []sandbox.ResolvedCIDR
	}{
		"mixed IPv4 and IPv6": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{
						{CIDR: "0.0.0.0/0", Except: []string{"10.0.0.0/8", "172.16.0.0/12"}},
						{CIDR: "::/0", Except: []string{"fc00::/7"}},
					},
				}),
			},
			wantIPv4: []sandbox.ResolvedCIDR{
				{CIDR: "0.0.0.0/0", Except: []string{"10.0.0.0/8", "172.16.0.0/12"}},
			},
			wantIPv6: []sandbox.ResolvedCIDR{
				{CIDR: "::/0", Except: []string{"fc00::/7"}},
			},
		},
		"no CIDR rules": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
		},
		"port-scoped CIDR": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "8.8.8.0/24"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}, {Port: "80"}}}},
				}),
			},
			wantIPv4: []sandbox.ResolvedCIDR{
				{CIDR: "8.8.8.0/24", Ports: []sandbox.ResolvedPortProto{
					{Port: 80, Protocol: "tcp"},
					{Port: 80, Protocol: "udp"},
					{Port: 443, Protocol: "tcp"},
					{Port: 443, Protocol: "udp"},
				}},
			},
		},
		"empty Ports list means any port": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "8.8.8.0/24"}},
					ToPorts:   []sandbox.PortRule{{Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}}}},
				}),
			},
			wantIPv4: []sandbox.ResolvedCIDR{
				{CIDR: "8.8.8.0/24"},
			},
		},
		"no toPorts means any port": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "8.8.8.0/24"}},
				}),
			},
			wantIPv4: []sandbox.ResolvedCIDR{
				{CIDR: "8.8.8.0/24"},
			},
		},
		"multiple rules with different ports": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToCIDRSet: []sandbox.CIDRRule{{CIDR: "8.8.8.0/24"}},
						ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "53"}}}},
					},
					sandbox.EgressRule{
						ToCIDRSet: []sandbox.CIDRRule{{CIDR: "1.1.1.0/24"}},
						ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
				),
			},
			wantIPv4: []sandbox.ResolvedCIDR{
				{CIDR: "8.8.8.0/24", Ports: []sandbox.ResolvedPortProto{
					{Port: 53, Protocol: "tcp"},
					{Port: 53, Protocol: "udp"},
				}, RuleIndex: 0},
				{CIDR: "1.1.1.0/24", Ports: []sandbox.ResolvedPortProto{
					{Port: 443, Protocol: "tcp"},
					{Port: 443, Protocol: "udp"},
				}, RuleIndex: 1},
			},
		},
		"toCIDR without except": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDR: []string{"8.8.8.0/24"},
				}),
			},
			wantIPv4: []sandbox.ResolvedCIDR{
				{CIDR: "8.8.8.0/24"},
			},
		},
		"toCIDR and toCIDRSet in same rule share RuleIndex": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDR:    []string{"10.0.0.0/8"},
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "192.168.0.0/16", Except: []string{"192.168.1.0/24"}}},
				}),
			},
			wantIPv4: []sandbox.ResolvedCIDR{
				{CIDR: "10.0.0.0/8", RuleIndex: 0},
				{CIDR: "192.168.0.0/16", Except: []string{"192.168.1.0/24"}, RuleIndex: 0},
			},
		},
		"toCIDR and toCIDRSet in separate rules": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{ToCIDR: []string{"1.1.1.0/24"}},
					sandbox.EgressRule{
						ToCIDRSet: []sandbox.CIDRRule{{CIDR: "8.8.8.0/24", Except: []string{"8.8.8.8/32"}}},
					},
				),
			},
			wantIPv4: []sandbox.ResolvedCIDR{
				{CIDR: "1.1.1.0/24", RuleIndex: 0},
				{CIDR: "8.8.8.0/24", Except: []string{"8.8.8.8/32"}, RuleIndex: 1},
			},
		},
		"UDP port-scoped CIDR": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "8.8.8.0/24"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "53", Protocol: "UDP"}}}},
				}),
			},
			wantIPv4: []sandbox.ResolvedCIDR{
				{CIDR: "8.8.8.0/24", Ports: []sandbox.ResolvedPortProto{{Port: 53, Protocol: "udp"}}},
			},
		},
		"ANY protocol CIDR": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "8.8.8.0/24"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "53", Protocol: "ANY"}}}},
				}),
			},
			wantIPv4: []sandbox.ResolvedCIDR{
				{CIDR: "8.8.8.0/24", Ports: []sandbox.ResolvedPortProto{
					{Port: 53, Protocol: "tcp"},
					{Port: 53, Protocol: "udp"},
				}},
			},
		},
		"port range propagated": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "8.8.8.0/24"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8000", EndPort: 9000}}}},
				}),
			},
			wantIPv4: []sandbox.ResolvedCIDR{
				{CIDR: "8.8.8.0/24", Ports: []sandbox.ResolvedPortProto{
					{Port: 8000, EndPort: 9000, Protocol: "tcp"},
					{Port: 8000, EndPort: 9000, Protocol: "udp"},
				}},
			},
		},
		"IPv4-mapped IPv6 classified as IPv6": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "::ffff:10.0.0.0/104"}},
				}),
			},
			wantIPv6: []sandbox.ResolvedCIDR{
				{CIDR: "::ffff:10.0.0.0/104"},
			},
		},
		"separate FQDN and CIDR rules contribute CIDRs": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
					sandbox.EgressRule{
						ToCIDR:  []string{"10.0.0.0/8"},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
					},
				),
			},
			wantIPv4: []sandbox.ResolvedCIDR{
				{CIDR: "10.0.0.0/8", Ports: []sandbox.ResolvedPortProto{
					{Port: 443, Protocol: "tcp"},
					{Port: 443, Protocol: "udp"},
				}, RuleIndex: 0},
			},
		},
		"port 0 CIDR rule has no port restriction": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "10.0.0.0/8"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "0"}}}},
				}),
			},
			wantIPv4: []sandbox.ResolvedCIDR{
				{CIDR: "10.0.0.0/8"},
			},
		},
		"ANY omits SCTP": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "8.8.8.0/24"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "53"}}}},
				}),
			},
			wantIPv4: []sandbox.ResolvedCIDR{
				{CIDR: "8.8.8.0/24", Ports: []sandbox.ResolvedPortProto{
					{Port: 53, Protocol: "tcp"},
					{Port: 53, Protocol: "udp"},
				}},
			},
		},
		"explicit SCTP preserved": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "8.8.8.0/24"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "3868", Protocol: "SCTP"}}}},
				}),
			},
			wantIPv4: []sandbox.ResolvedCIDR{
				{CIDR: "8.8.8.0/24", Ports: []sandbox.ResolvedPortProto{
					{Port: 3868, Protocol: "sctp"},
				}},
			},
		},
		"bare IPv4 toCIDR normalizes to /32": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDR: []string{"8.8.8.8"},
				}),
			},
			validate: true,
			wantIPv4: []sandbox.ResolvedCIDR{
				{CIDR: "8.8.8.8/32"},
			},
		},
		"bare IPv6 toCIDR normalizes to /128": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDR: []string{"fd00::1"},
				}),
			},
			validate: true,
			wantIPv6: []sandbox.ResolvedCIDR{
				{CIDR: "fd00::1/128"},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if tt.validate {
				require.NoError(t, tt.cfg.Validate())
			}
			ipv4, ipv6 := tt.cfg.ResolveCIDRRules()
			assert.Equal(t, tt.wantIPv4, ipv4)
			assert.Equal(t, tt.wantIPv6, ipv6)
		})
	}
}

func TestResolvePort(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  uint16
		err   bool
	}{
		"numeric":            {input: "443", want: 443},
		"numeric zero":       {input: "0", want: 0},
		"named https":        {input: "https", want: 443},
		"named http":         {input: "http", want: 80},
		"named dns":          {input: "dns", want: 53},
		"named domain":       {input: "domain", want: 53},
		"named dns-tcp":      {input: "dns-tcp", want: 53},
		"case insensitive":   {input: "HTTP", want: 80},
		"mixed case":         {input: "Https", want: 443},
		"unknown name":       {input: "redis", err: true},
		"invalid syntax":     {input: "abc!!", err: true},
		"leading hyphen":     {input: "-http", err: true},
		"trailing hyphen":    {input: "http-", err: true},
		"consecutive hyphen": {input: "dns--tcp", err: true},
		"empty":              {input: "", err: true},
		"digits only":        {input: "123", want: 123},
		"port 65535":         {input: "65535", want: 65535},
		"port 65536":         {input: "65536", err: true},
		"port 70000":         {input: "70000", err: true},
		"large port":         {input: "100000", err: true},
		"negative":           {input: "-1", err: true},
		"negative zero":      {input: "-0", err: true},
		"hex rejected":       {input: "0x1BB", err: true},
		"octal rejected":     {input: "0o777", err: true},
		"max int rejected":   {input: "2147483647", err: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := sandbox.ResolvePort(tt.input)
			if tt.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestNamedPortValidation(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg *sandbox.SandboxConfig
		err error
	}{
		"named port https accepted": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "https"}}}},
				}),
			},
		},
		"named port http accepted": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "http"}}}},
				}),
			},
		},
		"named port dns accepted": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "dns"}}}},
				}),
			},
		},
		"named port domain accepted": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "domain"}}}},
				}),
			},
		},
		"unknown named port rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "redis"}}}},
				}),
			},
			err: sandbox.ErrPortInvalid,
		},
		"invalid syntax rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "abc!!"}}}},
				}),
			},
			err: sandbox.ErrPortInvalid,
		},
		"negative port rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "-1"}}}},
				}),
			},
			err: sandbox.ErrPortInvalid,
		},
		"endPort with named port rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "https", EndPort: 500}}}},
				}),
			},
			err: sandbox.ErrEndPortWithNamedPort,
		},
		"L7 on named port http accepted": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "http"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
					}},
				}),
			},
		},
		"L7 on named port https accepted": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "https"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
					}},
				}),
			},
		},
		"L7 on named port dns rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "dns.example.com"}},
					ToPorts: []sandbox.PortRule{{
						Ports: []sandbox.Port{{Port: "dns"}},
						Rules: &sandbox.L7Rules{HTTP: []sandbox.HTTPRule{{Path: "/v1/"}}},
					}},
				}),
			},
			err: sandbox.ErrL7OnUnsupportedPort,
		},
		"uppercase named port normalized": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "HTTPS"}}}},
				}),
			},
		},
		"port 65536 rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "65536"}}}},
				}),
			},
			err: sandbox.ErrPortInvalid,
		},
		"port 70000 rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "70000"}}}},
				}),
			},
			err: sandbox.ErrPortInvalid,
		},
		"port 65535 accepted": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "65535"}}}},
				}),
			},
		},
		"endPort 70000 rejected": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", EndPort: 70000}}}},
				}),
			},
			err: sandbox.ErrEndPortInvalid,
		},
		"port 0 accepted": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToCIDRSet: []sandbox.CIDRRule{{CIDR: "0.0.0.0/0"}},
					ToPorts:   []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "0"}}}},
				}),
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := tt.cfg.Validate()
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNamedPortResolution(t *testing.T) {
	t.Parallel()

	t.Run("ResolvePorts with named port", func(t *testing.T) {
		t.Parallel()

		cfg := &sandbox.SandboxConfig{
			Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "https"}}}},
			}),
		}
		require.NoError(t, cfg.Validate())
		assert.Equal(t, []int{443}, cfg.ResolvePorts())
	})

	t.Run("ResolveOpenPorts with named port", func(t *testing.T) {
		t.Parallel()

		cfg := &sandbox.SandboxConfig{
			Egress: egressRules(sandbox.EgressRule{
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "http"}}}},
			}),
		}
		require.NoError(t, cfg.Validate())
		assert.Equal(t, []int{80}, cfg.ResolveOpenPorts())
	})

	t.Run("ResolveRulesForPort with named port", func(t *testing.T) {
		t.Parallel()

		cfg := &sandbox.SandboxConfig{
			Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "https"}}}},
			}),
		}
		require.NoError(t, cfg.Validate())

		rules := cfg.ResolveRulesForPort(443)
		require.Len(t, rules, 1)
		assert.Equal(t, "example.com", rules[0].Domain)
	})

	t.Run("case insensitivity in resolution", func(t *testing.T) {
		t.Parallel()

		cfg := &sandbox.SandboxConfig{
			Egress: egressRules(sandbox.EgressRule{
				ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
				ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "HTTPS"}}}},
			}),
		}
		require.NoError(t, cfg.Validate())
		assert.Equal(t, []int{443}, cfg.ResolvePorts())
	})
}

func TestUnsupportedSelectors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		yaml      string
		err       error
		wantRules int
	}{
		"toEndpoints rejected": {
			yaml: `
egress:
  - toEndpoints:
      - matchLabels:
          role: backend
    toPorts:
      - ports:
          - port: "443"
`,
			err: sandbox.ErrUnsupportedSelector,
		},
		"toEntities world rejected": {
			yaml: `
egress:
  - toEntities:
      - world
`,
			err: sandbox.ErrUnsupportedSelector,
		},
		"toServices rejected": {
			yaml: `
egress:
  - toServices:
      - k8sService:
          serviceName: my-svc
          namespace: default
`,
			err: sandbox.ErrUnsupportedSelector,
		},
		"toNodes rejected": {
			yaml: `
egress:
  - toNodes:
      - matchLabels:
          node-role: worker
`,
			err: sandbox.ErrUnsupportedSelector,
		},
		"toGroups rejected": {
			yaml: `
egress:
  - toGroups:
      - aws:
          securityGroupsIds:
            - sg-123
`,
			err: sandbox.ErrUnsupportedSelector,
		},
		"toRequires rejected": {
			yaml: `
egress:
  - toRequires:
      - something
`,
			err: sandbox.ErrUnsupportedSelector,
		},
		"icmps rejected": {
			yaml: `
egress:
  - icmps:
      - fields:
          - type: 8
`,
			err: sandbox.ErrUnsupportedSelector,
		},
		"authentication rejected": {
			yaml: `
egress:
  - authentication:
      mode: required
`,
			err: sandbox.ErrUnsupportedSelector,
		},
		"empty toEntities not rejected": {
			yaml: `
egress:
  - toEntities: []
    toCIDR:
      - 10.0.0.0/8
`,
			wantRules: 1,
		},
		"null toEntities not rejected": {
			yaml: `
egress:
  - toEntities: null
    toCIDR:
      - 10.0.0.0/8
`,
			wantRules: 1,
		},
		"absent toEntities not rejected": {
			yaml: `
egress:
  - toCIDR:
      - 10.0.0.0/8
`,
			wantRules: 1,
		},
		"error message includes field name and rule index": {
			yaml: `
egress:
  - toCIDR:
      - 10.0.0.0/8
  - toEntities:
      - world
`,
			err: sandbox.ErrUnsupportedSelector,
		},
		"unknown field rejected at parse time": {
			yaml: `
egress:
  - toFQDNs:
      - matchName: example.com
    toPorts:
      - ports:
          - port: "443"
    someFutureField: true
`,
		},
		"unknown top-level field rejected": {
			yaml: `
egressPolicy:
  - toFQDNs:
      - matchName: example.com
`,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg, err := sandbox.ParseConfig([]byte(tt.yaml))
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
				return
			}

			if tt.wantRules > 0 {
				require.NoError(t, err)
				assert.Len(t, cfg.EgressRules(), tt.wantRules)
				return
			}

			// Unknown field cases: expect a parse error (not a sentinel)
			require.Error(t, err)
		})
	}

	// Verify the error message format includes field name and rule index.
	t.Run("error format", func(t *testing.T) {
		t.Parallel()

		_, err := sandbox.ParseConfig([]byte(`
egress:
  - toCIDR:
      - 10.0.0.0/8
  - toEntities:
      - world
`))
		require.ErrorIs(t, err, sandbox.ErrUnsupportedSelector)
		assert.ErrorContains(t, err, "rule 1 has toEntities")
	})
}

func TestMarshalConfigRoundtrip(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg              *sandbox.SandboxConfig
		wantUnrestricted bool
		wantBlocked      bool
	}{
		"nil egress roundtrips as unrestricted": {
			cfg:              &sandbox.SandboxConfig{},
			wantUnrestricted: true,
		},
		"empty egress roundtrips as unrestricted": {
			cfg:              &sandbox.SandboxConfig{Egress: egressRules()},
			wantUnrestricted: true,
		},
		"explicit deny with empty egress roundtrips as unrestricted": {
			cfg: &sandbox.SandboxConfig{
				EnableDefaultDeny: sandbox.DefaultDenyConfig{Egress: boolPtr(true)},
				Egress:            egressRules(),
			},
			wantUnrestricted: true,
		},
		"empty rule roundtrips as blocked": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{}),
			},
			wantBlocked: true,
		},
		"rules roundtrip": {
			cfg: &sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443"}}}},
				}),
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			data, err := sandbox.MarshalConfig(tt.cfg)
			require.NoError(t, err)

			cfg2, err := sandbox.ParseConfig(data)
			require.NoError(t, err)
			assert.Equal(t, tt.wantUnrestricted, cfg2.IsEgressUnrestricted())
			assert.Equal(t, tt.wantBlocked, cfg2.IsEgressBlocked())
		})
	}
}

func TestCompileFQDNPatterns(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg     sandbox.SandboxConfig
		want    []string
		match   map[string]bool
		noMatch map[string]bool
	}{
		"matchName exact": {
			cfg: sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
				}),
			},
			want:    []string{"api.example.com"},
			match:   map[string]bool{"api.example.com.": true},
			noMatch: map[string]bool{"evil.api.example.com.": true, "example.com.": true},
		},
		"single-star wildcard": {
			cfg: sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
				}),
			},
			want:    []string{"*.example.com"},
			match:   map[string]bool{"sub.example.com.": true},
			noMatch: map[string]bool{"a.b.example.com.": true, "example.com.": true},
		},
		"double-star wildcard": {
			cfg: sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "**.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
				}),
			},
			want:    []string{"**.example.com"},
			match:   map[string]bool{"sub.example.com.": true, "a.b.example.com.": true},
			noMatch: map[string]bool{"example.com.": true},
		},
		"bare wildcard": {
			cfg: sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "*"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
				}),
			},
			want:    []string{"*"},
			match:   map[string]bool{"anything.com.": true, "a.b.c.": true, ".": true},
			noMatch: map[string]bool{"": true},
		},
		"triple-star bare wildcard": {
			cfg: sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "***"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
				}),
			},
			want:  []string{"***"},
			match: map[string]bool{"anything.com.": true, ".": true},
		},
		"mid-position double-star falls back to single-label": {
			cfg: sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchPattern: "test.**.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
				}),
			},
			want:    []string{"test.**.example.com"},
			match:   map[string]bool{"test.sub.example.com.": true},
			noMatch: map[string]bool{"test.a.b.example.com.": true},
		},
		"excludes TCPForward hosts": {
			cfg: sandbox.SandboxConfig{
				Egress: egressRules(sandbox.EgressRule{
					ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
					ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
				}),
				TCPForwards: []sandbox.TCPForward{{Port: 22, Host: "git.example.com"}},
			},
			want: []string{"api.example.com"},
		},
		"deduplicates patterns": {
			cfg: sandbox.SandboxConfig{
				Egress: egressRules(
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "443", Protocol: "UDP"}}}},
					},
					sandbox.EgressRule{
						ToFQDNs: []sandbox.FQDNSelector{{MatchName: "api.example.com"}},
						ToPorts: []sandbox.PortRule{{Ports: []sandbox.Port{{Port: "8080", Protocol: "UDP"}}}},
					},
				),
			},
			want: []string{"api.example.com"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			patterns := tt.cfg.CompileFQDNPatterns()

			var originals []string
			for _, p := range patterns {
				originals = append(originals, p.Original)
			}

			assert.Equal(t, tt.want, originals)

			for qname := range tt.match {
				matched := false

				for _, p := range patterns {
					if p.Regex.MatchString(qname) {
						matched = true

						break
					}
				}

				assert.True(t, matched, "expected %q to match", qname)
			}

			for qname := range tt.noMatch {
				matched := false

				for _, p := range patterns {
					if p.Regex.MatchString(qname) {
						matched = true

						break
					}
				}

				assert.False(t, matched, "expected %q not to match", qname)
			}
		})
	}
}
