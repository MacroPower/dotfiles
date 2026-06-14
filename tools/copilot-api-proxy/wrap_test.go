package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildChildEnv(t *testing.T) {
	t.Parallel()

	base := []string{
		"PATH=/usr/bin",
		"HOME=/home/x",
		"ANTHROPIC_API_KEY=sk-ant-real",
		"ANTHROPIC_BASE_URL=http://stale",
		"ANTHROPIC_AUTH_TOKEN=stale",
	}

	got := buildChildEnv(base, "http://127.0.0.1:5000", "secret123")

	assert.Contains(t, got, "PATH=/usr/bin")
	assert.Contains(t, got, "HOME=/home/x")
	assert.Contains(t, got, "ANTHROPIC_BASE_URL=http://127.0.0.1:5000")
	assert.Contains(t, got, "ANTHROPIC_AUTH_TOKEN=secret123")

	for _, kv := range got {
		assert.False(t, strings.HasPrefix(kv, "ANTHROPIC_API_KEY="), "inherited api key must be dropped")
	}
	assert.Equal(t, 1, countPrefix(got, "ANTHROPIC_BASE_URL="), "no duplicate base url")
	assert.Equal(t, 1, countPrefix(got, "ANTHROPIC_AUTH_TOKEN="), "no duplicate auth token")
}

func countPrefix(env []string, prefix string) int {
	n := 0
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			n++
		}
	}
	return n
}

func TestRandomSecret(t *testing.T) {
	t.Parallel()

	a, err := randomSecret()
	require.NoError(t, err)
	b, err := randomSecret()
	require.NoError(t, err)

	assert.NotEqual(t, a, b)
	assert.Len(t, a, 48) // 24 random bytes, hex-encoded
}
