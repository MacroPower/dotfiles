package main

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheck(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		deny  []compiledDeny
		allow []compiledMatch
		url   string
		want  string
	}{
		"deny by host": {
			deny: mustDeny(t, DenyRule{
				URLMatch: URLMatch{Host: `raw\.githubusercontent\.com`},
				Reason:   "blocked",
			}),
			url:  "https://raw.githubusercontent.com/o/r/main/main.go",
			want: "blocked",
		},
		"deny by host with path exception": {
			deny: mustDeny(t, DenyRule{
				URLMatch: URLMatch{Host: `raw\.githubusercontent\.com`},
				Except:   []URLMatch{{Path: `.*\.md`}},
				Reason:   "blocked",
			}),
			url:  "https://raw.githubusercontent.com/o/r/main/README.md",
			want: "",
		},
		"deny by host, path does not match exception": {
			deny: mustDeny(t, DenyRule{
				URLMatch: URLMatch{Host: `raw\.githubusercontent\.com`},
				Except:   []URLMatch{{Path: `.*\.md`}},
				Reason:   "blocked",
			}),
			url:  "https://raw.githubusercontent.com/o/r/main/main.go",
			want: "blocked",
		},
		"deny matching host and path": {
			deny: mustDeny(t, DenyRule{
				URLMatch: URLMatch{Host: `example\.com`, Path: `/secret/.*`},
				Reason:   "no secrets",
			}),
			url:  "https://example.com/secret/file.txt",
			want: "no secrets",
		},
		"deny host+path, path does not match": {
			deny: mustDeny(t, DenyRule{
				URLMatch: URLMatch{Host: `example\.com`, Path: `/secret/.*`},
				Reason:   "no secrets",
			}),
			url:  "https://example.com/public/file.txt",
			want: "",
		},
		"allow list, host matches": {
			allow: mustAllow(t, AllowRule{URLMatch: URLMatch{Host: `example\.com`}}),
			url:   "https://example.com/page",
			want:  "",
		},
		"allow list, no match": {
			allow: mustAllow(t, AllowRule{URLMatch: URLMatch{Host: `example\.com`}}),
			url:   "https://other.com/page",
			want:  "URL not in allow list",
		},
		"nil rules": {
			url:  "https://anything.com",
			want: "",
		},
		"multiple deny rules, first match wins": {
			deny: mustDeny(t,
				DenyRule{URLMatch: URLMatch{Host: `first\.com`}, Reason: "first"},
				DenyRule{URLMatch: URLMatch{Host: `.*`}, Reason: "catch-all"},
			),
			url:  "https://first.com/page",
			want: "first",
		},
		"deny takes precedence over allow": {
			deny: mustDeny(t, DenyRule{
				URLMatch: URLMatch{Host: `evil\.com`},
				Reason:   "denied",
			}),
			allow: mustAllow(t, AllowRule{URLMatch: URLMatch{Host: `.*`}}),
			url:   "https://evil.com/page",
			want:  "denied",
		},
		"query string does not interfere with path matching": {
			deny: mustDeny(t, DenyRule{
				URLMatch: URLMatch{Host: `raw\.githubusercontent\.com`},
				Except:   []URLMatch{{Path: `.*\.md`}},
				Reason:   "blocked",
			}),
			url:  "https://raw.githubusercontent.com/o/r/main/README.md?token=abc",
			want: "",
		},
		"anchoring prevents substring host match": {
			deny: mustDeny(t, DenyRule{
				URLMatch: URLMatch{Host: `example\.com`},
				Reason:   "blocked",
			}),
			url:  "https://evil-example.com/page",
			want: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var r *Rules
			if tt.deny != nil || tt.allow != nil {
				r = &Rules{deny: tt.deny, allow: tt.allow}
			}

			u, err := url.ParseRequestURI(tt.url)
			require.NoError(t, err)

			got := r.Check(u)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoadRules(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		path    string
		content string
		err     string
		want    func(t *testing.T, r *Rules)
	}{
		"empty path returns empty rules": {
			want: func(t *testing.T, r *Rules) {
				t.Helper()
				require.NotNil(t, r)
				assert.Empty(t, r.deny)
				assert.Empty(t, r.allow)
			},
		},
		"valid JSON": {
			content: `{"deny":[{"host":"example\\.com","reason":"blocked"}]}`,
			want: func(t *testing.T, r *Rules) {
				t.Helper()
				require.NotNil(t, r)
				require.Len(t, r.deny, 1)
				assert.Equal(t, "blocked", r.deny[0].reason)
			},
		},
		"malformed JSON": {
			content: `{invalid`,
			err:     "parsing rules file",
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

			r, err := LoadRules(path)

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

// mustDeny compiles deny rules for test setup.
func mustDeny(t *testing.T, rules ...DenyRule) []compiledDeny {
	t.Helper()

	var out []compiledDeny

	for _, d := range rules {
		cm, err := compileURLMatch(d.URLMatch)
		require.NoError(t, err)

		cd := compiledDeny{match: cm, reason: d.Reason}

		for _, ex := range d.Except {
			exc, err := compileURLMatch(ex)
			require.NoError(t, err)

			cd.except = append(cd.except, exc)
		}

		out = append(out, cd)
	}

	return out
}

// mustAllow compiles allow rules for test setup.
func mustAllow(t *testing.T, rules ...AllowRule) []compiledMatch {
	t.Helper()

	var out []compiledMatch

	for _, a := range rules {
		cm, err := compileURLMatch(a.URLMatch)
		require.NoError(t, err)

		out = append(out, cm)
	}

	return out
}
