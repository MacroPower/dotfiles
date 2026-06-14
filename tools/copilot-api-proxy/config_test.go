package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModelFor(t *testing.T) {
	t.Parallel()

	cfg := Config{Models: map[string]string{
		"opus":    "OPUS",
		"sonnet":  "SONNET",
		"haiku":   "HAIKU",
		"default": "DEFAULT",
	}}

	tests := map[string]struct {
		requested string
		want      string
	}{
		"opus dashed":         {"claude-opus-4-8", "OPUS"},
		"opus bracket suffix": {"claude-opus-4-8[1m]", "OPUS"},
		"opus uppercase":      {"CLAUDE-OPUS", "OPUS"},
		"sonnet dated":        {"claude-sonnet-4-5-20250929", "SONNET"},
		"sonnet legacy":       {"claude-3-5-sonnet", "SONNET"},
		"haiku":               {"claude-haiku-4-5", "HAIKU"},
		"unknown":             {"gpt-4o", "DEFAULT"},
		"empty":               {"", "DEFAULT"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, cfg.ModelFor(tc.requested))
		})
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("COPILOT_PROXY_ADDR", "")

	cfg := Load()
	assert.Equal(t, "127.0.0.1:9876", cfg.ListenAddr)
	assert.Equal(t, "claude-opus-4.8", cfg.Models["opus"])
	assert.Equal(t, "claude-sonnet-4.6", cfg.Models["sonnet"])
	assert.Equal(t, "claude-haiku-4.5", cfg.Models["haiku"])
	assert.Equal(t, "vscode-chat", cfg.Editor.IntegrationID)
}

func TestRequireLoopbackOrKey(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		addr    string
		master  string
		wantErr bool
	}{
		"loopback without key":       {"127.0.0.1:9876", "", false},
		"localhost without key":      {"localhost:9876", "", false},
		"ipv6 loopback without key":  {"[::1]:9876", "", false},
		"all interfaces without key": {"0.0.0.0:9876", "", true},
		"empty host without key":     {":9876", "", true},
		"all interfaces with key":    {"0.0.0.0:9876", "sec", false},
		"unparseable address":        {"not-an-address", "", true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := requireLoopbackOrKey(Config{ListenAddr: tc.addr, MasterKey: tc.master})
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
