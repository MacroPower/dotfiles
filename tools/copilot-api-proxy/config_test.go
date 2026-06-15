package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go.jacobcolvin.com/dotfiles/tools/copilot-api-proxy/auth"
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

func TestResolveEndpoints(t *testing.T) {
	t.Parallel()

	def := auth.DefaultEndpoints()

	tests := map[string]struct {
		cfg     Config
		changed bool
		want    auth.Endpoints
	}{
		"no overrides keeps defaults": {
			cfg:     Config{},
			changed: false,
			want:    def,
		},
		"ghe host derives all three": {
			cfg:     Config{GHEHost: "ghe.example.com"},
			changed: true,
			want: auth.Endpoints{
				DeviceCode:   "https://ghe.example.com/login/device/code",
				AccessToken:  "https://ghe.example.com/login/oauth/access_token",
				CopilotToken: "https://api.ghe.example.com/copilot_internal/v2/token",
			},
		},
		"explicit token url overrides ghe host": {
			cfg:     Config{GHEHost: "ghe.example.com", CopilotTokenURL: "https://custom.example/token"},
			changed: true,
			want: auth.Endpoints{
				DeviceCode:   "https://ghe.example.com/login/device/code",
				AccessToken:  "https://ghe.example.com/login/oauth/access_token",
				CopilotToken: "https://custom.example/token",
			},
		},
		"single per-url override leaves the rest default": {
			cfg:     Config{CopilotTokenURL: "https://custom.example/token"},
			changed: true,
			want: auth.Endpoints{
				DeviceCode:   def.DeviceCode,
				AccessToken:  def.AccessToken,
				CopilotToken: "https://custom.example/token",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, changed := tc.cfg.ResolveEndpoints()
			assert.Equal(t, tc.changed, changed)
			assert.Equal(t, tc.want, got)
		})
	}
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
