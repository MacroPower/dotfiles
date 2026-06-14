package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilterBetas(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		values []string
		allow  []string
		want   []string
	}{
		"drops denied advisor-tool": {
			values: []string{"claude-code-20250219,advisor-tool-2026-03-01,interleaved-thinking-2025-05-14"},
			allow:  defaultBetaAllowPrefixes,
			want:   []string{"claude-code-20250219", "interleaved-thinking-2025-05-14"},
		},
		"drops 1m context beta": {
			values: []string{"context-1m-2025-08-07,context-management-2025-06-27"},
			allow:  defaultBetaAllowPrefixes,
			want:   []string{"context-management-2025-06-27"},
		},
		"drops unknown beta not in allowlist": {
			values: []string{"some-future-beta-2099-01-01"},
			allow:  defaultBetaAllowPrefixes,
			want:   nil,
		},
		"splits multiple header lines": {
			values: []string{"effort-2025-11-24", "token-efficient-tools-2026-03-28"},
			allow:  defaultBetaAllowPrefixes,
			want:   []string{"effort-2025-11-24", "token-efficient-tools-2026-03-28"},
		},
		"dedupes and trims whitespace": {
			values: []string{" oauth-2025-04-20 , oauth-2025-04-20 "},
			allow:  defaultBetaAllowPrefixes,
			want:   []string{"oauth-2025-04-20"},
		},
		"deny wins even if allow matches": {
			// A broadened allowlist still cannot resurrect a denied beta.
			values: []string{"advisor-tool-2026-03-01"},
			allow:  []string{"advisor-tool-"},
			want:   nil,
		},
		"empty allowlist drops everything": {
			values: []string{"interleaved-thinking-2025-05-14"},
			allow:  nil,
			want:   nil,
		},
		"no beta header": {
			values: nil,
			allow:  defaultBetaAllowPrefixes,
			want:   nil,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, filterBetas(tc.values, tc.allow))
		})
	}
}
