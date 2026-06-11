package rules_test

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/rules"
)

func TestCheck(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		deny   []rules.DenyRule
		allow  []rules.AllowRule
		reason string
		url    string
		want   string
	}{
		"deny by host": {
			deny: []rules.DenyRule{{
				URLMatch: rules.URLMatch{Host: `raw\.githubusercontent\.com`},
				Reason:   "blocked",
			}},
			url:  "https://raw.githubusercontent.com/o/r/main/main.go",
			want: "blocked",
		},
		"deny by host with path exception": {
			deny: []rules.DenyRule{{
				URLMatch: rules.URLMatch{Host: `raw\.githubusercontent\.com`},
				Except:   []rules.URLMatch{{Path: `.*\.md`}},
				Reason:   "blocked",
			}},
			url:  "https://raw.githubusercontent.com/o/r/main/README.md",
			want: "",
		},
		"deny by host, path does not match exception": {
			deny: []rules.DenyRule{{
				URLMatch: rules.URLMatch{Host: `raw\.githubusercontent\.com`},
				Except:   []rules.URLMatch{{Path: `.*\.md`}},
				Reason:   "blocked",
			}},
			url:  "https://raw.githubusercontent.com/o/r/main/main.go",
			want: "blocked",
		},
		"deny matching host and path": {
			deny: []rules.DenyRule{{
				URLMatch: rules.URLMatch{Host: `example\.com`, Path: `/secret/.*`},
				Reason:   "no secrets",
			}},
			url:  "https://example.com/secret/file.txt",
			want: "no secrets",
		},
		"deny host+path, path does not match": {
			deny: []rules.DenyRule{{
				URLMatch: rules.URLMatch{Host: `example\.com`, Path: `/secret/.*`},
				Reason:   "no secrets",
			}},
			url:  "https://example.com/public/file.txt",
			want: "",
		},
		"allow list, host matches": {
			allow: []rules.AllowRule{{URLMatch: rules.URLMatch{Host: `example\.com`}}},
			url:   "https://example.com/page",
			want:  "",
		},
		"allow list, no match": {
			allow: []rules.AllowRule{{URLMatch: rules.URLMatch{Host: `example\.com`}}},
			url:   "https://other.com/page",
			want:  "URL not in allow list",
		},
		"nil rules": {
			url:  "https://anything.com",
			want: "",
		},
		"multiple deny rules, first match wins": {
			deny: []rules.DenyRule{
				{URLMatch: rules.URLMatch{Host: `first\.com`}, Reason: "first"},
				{URLMatch: rules.URLMatch{Host: `.*`}, Reason: "catch-all"},
			},
			url:  "https://first.com/page",
			want: "first",
		},
		"deny takes precedence over allow": {
			deny: []rules.DenyRule{{
				URLMatch: rules.URLMatch{Host: `evil\.com`},
				Reason:   "denied",
			}},
			allow: []rules.AllowRule{{URLMatch: rules.URLMatch{Host: `.*`}}},
			url:   "https://evil.com/page",
			want:  "denied",
		},
		"query string does not interfere with path matching": {
			deny: []rules.DenyRule{{
				URLMatch: rules.URLMatch{Host: `raw\.githubusercontent\.com`},
				Except:   []rules.URLMatch{{Path: `.*\.md`}},
				Reason:   "blocked",
			}},
			url:  "https://raw.githubusercontent.com/o/r/main/README.md?token=abc",
			want: "",
		},
		"allow list, no match, custom reason": {
			allow:  []rules.AllowRule{{URLMatch: rules.URLMatch{Host: `example\.com`}}},
			reason: "ask the user for permission first",
			url:    "https://other.com/page",
			want:   "ask the user for permission first",
		},
		"anchoring prevents substring host match": {
			deny: []rules.DenyRule{{
				URLMatch: rules.URLMatch{Host: `example\.com`},
				Reason:   "blocked",
			}},
			url:  "https://evil-example.com/page",
			want: "",
		},
		"deny github API with path": {
			deny: []rules.DenyRule{{
				URLMatch: rules.URLMatch{Host: `api\.github\.com`},
				Reason:   "use mcp",
			}},
			url:  "https://api.github.com/repos/owner/repo/issues",
			want: "use mcp",
		},
		"deny github API bare host": {
			deny: []rules.DenyRule{{
				URLMatch: rules.URLMatch{Host: `api\.github\.com`},
				Reason:   "use mcp",
			}},
			url:  "https://api.github.com/",
			want: "use mcp",
		},
		"deny github issue page": {
			deny: []rules.DenyRule{{
				URLMatch: rules.URLMatch{Host: `github\.com`, Path: `/[^/]+/[^/]+/issues(/.*)?`},
				Reason:   "use mcp",
			}},
			url:  "https://github.com/owner/repo/issues/123",
			want: "use mcp",
		},
		"deny github PR singular": {
			deny: []rules.DenyRule{{
				URLMatch: rules.URLMatch{Host: `github\.com`, Path: `/[^/]+/[^/]+/pulls?(/.*)?`},
				Reason:   "use mcp",
			}},
			url:  "https://github.com/owner/repo/pull/456",
			want: "use mcp",
		},
		"deny github PRs plural": {
			deny: []rules.DenyRule{{
				URLMatch: rules.URLMatch{Host: `github\.com`, Path: `/[^/]+/[^/]+/pulls?(/.*)?`},
				Reason:   "use mcp",
			}},
			url:  "https://github.com/owner/repo/pulls",
			want: "use mcp",
		},
		"deny github blob page": {
			deny: []rules.DenyRule{{
				URLMatch: rules.URLMatch{Host: `github\.com`, Path: `/[^/]+/[^/]+/(blob|tree)(/.*)?`},
				Reason:   "use mcp",
			}},
			url:  "https://github.com/owner/repo/blob/main/file.go",
			want: "use mcp",
		},
		"deny github releases bare": {
			deny: []rules.DenyRule{{
				URLMatch: rules.URLMatch{Host: `github\.com`, Path: `/[^/]+/[^/]+/releases(/.*)?`},
				Reason:   "use mcp",
			}},
			url:  "https://github.com/owner/repo/releases",
			want: "use mcp",
		},
		"deny github compare page": {
			deny: []rules.DenyRule{{
				URLMatch: rules.URLMatch{Host: `github\.com`, Path: `/[^/]+/[^/]+/(commit|compare)(/.*)?`},
				Reason:   "use mcp",
			}},
			url:  "https://github.com/owner/repo/compare/main...feature",
			want: "use mcp",
		},
		"deny github search, query string ignored": {
			deny: []rules.DenyRule{{
				URLMatch: rules.URLMatch{Host: `github\.com`, Path: `/search(/.*)?`},
				Reason:   "use mcp",
			}},
			url:  "https://github.com/search?q=test&type=code",
			want: "use mcp",
		},
		"allow github repo root": {
			deny: []rules.DenyRule{{
				URLMatch: rules.URLMatch{Host: `github\.com`, Path: `/[^/]+/[^/]+/issues(/.*)?`},
				Reason:   "use mcp",
			}},
			url:  "https://github.com/owner/repo",
			want: "",
		},
		"allow github top-level page": {
			deny: []rules.DenyRule{{
				URLMatch: rules.URLMatch{Host: `github\.com`, Path: `/[^/]+/[^/]+/issues(/.*)?`},
				Reason:   "use mcp",
			}},
			url:  "https://github.com/features",
			want: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var r *rules.Rules

			if tt.deny != nil || tt.allow != nil || tt.reason != "" {
				var err error

				r, err = rules.Compile(tt.reason, tt.deny, tt.allow)
				require.NoError(t, err)
			}

			u, err := url.ParseRequestURI(tt.url)
			require.NoError(t, err)

			got := r.Check(u)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCompileInvalidRegex(t *testing.T) {
	t.Parallel()

	_, err := rules.Compile("", []rules.DenyRule{{
		URLMatch: rules.URLMatch{Host: "[invalid"},
		Reason:   "bad",
	}}, nil)
	require.ErrorContains(t, err, "deny rule 0")
}

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		path    string
		content string
		err     string
		want    func(t *testing.T, r *rules.Rules)
	}{
		"empty path returns empty rules": {
			want: func(t *testing.T, r *rules.Rules) {
				t.Helper()
				require.NotNil(t, r)

				u, err := url.ParseRequestURI("https://anything.com/page")
				require.NoError(t, err)
				assert.Empty(t, r.Check(u))
			},
		},
		"valid JSON": {
			content: `{"deny":[{"host":"example\\.com","reason":"blocked"}]}`,
			want: func(t *testing.T, r *rules.Rules) {
				t.Helper()

				u, err := url.ParseRequestURI("https://example.com/page")
				require.NoError(t, err)
				assert.Equal(t, "blocked", r.Check(u))
			},
		},
		"malformed JSON": {
			content: `{invalid`,
			err:     "parsing rules file",
		},
		"reason field loaded": {
			content: `{"reason":"custom msg","allow":[{"host":"example\\.com"}]}`,
			want: func(t *testing.T, r *rules.Rules) {
				t.Helper()

				u, err := url.ParseRequestURI("https://other.com/page")
				require.NoError(t, err)
				assert.Equal(t, "custom msg", r.Check(u))
			},
		},
		"invalid regex": {
			content: `{"deny":[{"host":"[invalid","reason":"bad"}]}`,
			err:     "deny rule 0",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := tt.path
			if tt.content != "" {
				p := filepath.Join(t.TempDir(), "rules.json")
				require.NoError(t, os.WriteFile(p, []byte(tt.content), 0o644))

				path = p
			}

			r, err := rules.Load(path)

			if tt.err != "" {
				require.ErrorContains(t, err, tt.err)
				return
			}

			require.NoError(t, err)

			if tt.want != nil {
				tt.want(t, r)
			}
		})
	}
}
