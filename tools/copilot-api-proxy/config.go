package main

import (
	"os"
	"strings"

	"go.jacobcolvin.com/dotfiles/tools/copilot-api-proxy/auth"
)

// Config holds the proxy's runtime configuration, assembled from the
// environment by [Load].
type Config struct {
	ListenAddr  string
	DataDir     string
	MasterKey   string
	GitHubToken string
	APIBase     string

	// Models maps an Anthropic tier ("opus", "sonnet", "haiku", "default") to
	// the Copilot model id it is served by.
	Models map[string]string

	// BetaAllowPrefixes lists the Anthropic-Beta name prefixes forwarded
	// upstream. Copilot rejects unrecognized betas with a 400, so only betas
	// whose name begins with one of these prefixes are forwarded and the rest
	// are stripped (see [filterBetas]). Matching is by prefix because beta names
	// carry dated suffixes. Defaults to [defaultBetaAllowPrefixes];
	// COPILOT_BETA_ALLOW (comma-separated) replaces it.
	BetaAllowPrefixes []string

	Editor auth.EditorHeaders
}

// Load reads configuration from the environment, applying defaults.
func Load() Config {
	def := auth.DefaultEditorHeaders()
	return Config{
		ListenAddr:  envOr("COPILOT_PROXY_ADDR", "127.0.0.1:9876"),
		DataDir:     os.Getenv("COPILOT_PROXY_DATA_DIR"),
		MasterKey:   os.Getenv("COPILOT_PROXY_MASTER_KEY"),
		GitHubToken: firstEnv("GITHUB_TOKEN", "GH_COPILOT_TOKEN"),
		APIBase:     os.Getenv("COPILOT_API_BASE"),
		Models: map[string]string{
			"opus":    envOr("COPILOT_MODEL_OPUS", "claude-opus-4.8"),
			"sonnet":  envOr("COPILOT_MODEL_SONNET", "claude-sonnet-4.6"),
			"haiku":   envOr("COPILOT_MODEL_HAIKU", "claude-haiku-4.5"),
			"default": envOr("COPILOT_MODEL_DEFAULT", "claude-sonnet-4.6"),
		},
		BetaAllowPrefixes: betaAllowPrefixes(),
		Editor: auth.EditorHeaders{
			EditorVersion: envOr("COPILOT_EDITOR_VERSION", def.EditorVersion),
			PluginVersion: envOr("COPILOT_PLUGIN_VERSION", def.PluginVersion),
			UserAgent:     envOr("COPILOT_USER_AGENT", def.UserAgent),
			IntegrationID: envOr("COPILOT_INTEGRATION_ID", def.IntegrationID),
			APIVersion:    envOr("COPILOT_API_VERSION", def.APIVersion),
		},
	}
}

// ModelFor maps a requested Anthropic model name to a configured Copilot model
// id by tier. The tier is detected by substring; unrecognized names map to the
// default tier.
func (c Config) ModelFor(requested string) string {
	r := strings.ToLower(requested)
	switch {
	case strings.Contains(r, "opus"):
		return c.Models["opus"]
	case strings.Contains(r, "haiku"):
		return c.Models["haiku"]
	case strings.Contains(r, "sonnet"):
		return c.Models["sonnet"]
	default:
		return c.Models["default"]
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// betaAllowPrefixes returns the configured Anthropic-Beta allow prefixes,
// preferring a COPILOT_BETA_ALLOW override over the built-in default set.
func betaAllowPrefixes() []string {
	if v := os.Getenv("COPILOT_BETA_ALLOW"); v != "" {
		return splitList(v)
	}
	return defaultBetaAllowPrefixes
}

// splitList splits a comma-separated list into trimmed, non-empty tokens.
func splitList(s string) []string {
	var out []string
	for _, tok := range strings.Split(s, ",") {
		if tok = strings.TrimSpace(tok); tok != "" {
			out = append(out, tok)
		}
	}
	return out
}
