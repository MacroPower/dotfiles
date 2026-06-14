package main

import "strings"

// defaultBetaAllowPrefixes are the Anthropic-Beta name prefixes Copilot's
// Anthropic endpoint accepts. Copilot validates the beta list against an
// allowlist and returns 400 on anything it does not recognize, so the proxy
// forwards only betas whose name begins with one of these prefixes and strips
// the rest.
//
// Matching is by prefix because beta identifiers carry dated suffixes that
// change over time (interleaved-thinking-2025-05-14, token-efficient-tools-
// 2026-03-28, ...). The set is the union of what VS Code Copilot Chat sends on
// the native Messages path and the additional betas Claude Code relies on that
// are empirically accepted by Copilot.
var defaultBetaAllowPrefixes = []string{
	// Sent by VS Code Copilot Chat (getExtraHeaders): the baseline Copilot's
	// edge unconditionally accepts.
	"interleaved-thinking-",
	"context-management-",
	"advanced-tool-use-",
	// Additional betas Claude Code emits that Copilot accepts.
	"claude-code-",
	"effort-",
	"prompt-caching-",
	"computer-use-",
	"pdfs-",
	"max-tokens-",
	"token-counting-",
	"compact-",
	"structured-outputs-",
	"fast-mode-",
	"mcp-client-",
	"mcp-servers-",
	"redact-thinking-",
	"web-search-",
	"task-budgets-",
	"token-efficient-tools-",
	"oauth-",
}

// deniedBetaPrefixes are betas Copilot rejects with a 400 even though a client
// may send them. They are stripped ahead of the allowlist so the request
// succeeds without the (unsupported) feature. context-1m is here because 1M
// context is selected by model id on Copilot, not by a beta header.
var deniedBetaPrefixes = []string{
	"advisor-tool-", // 400 "unsupported beta header(s): advisor-tool-..."
	"context-1m-",
	"skills-",
	"files-api-",
	"code-execution-",
	"output-128k-",
}

// filterBetas splits the client's Anthropic-Beta header values into individual
// beta tokens (each value may itself be comma-separated) and keeps only those
// matching an allow prefix and no deny prefix, preserving order and dropping
// duplicates.
func filterBetas(values, allowPrefixes []string) []string {
	var kept []string
	seen := map[string]bool{}
	for _, v := range values {
		for _, tok := range strings.Split(v, ",") {
			tok = strings.TrimSpace(tok)
			if tok == "" || seen[tok] {
				continue
			}
			if hasAnyPrefix(tok, deniedBetaPrefixes) || !hasAnyPrefix(tok, allowPrefixes) {
				continue
			}
			seen[tok] = true
			kept = append(kept, tok)
		}
	}
	return kept
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
