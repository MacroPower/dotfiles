// Copilot-api-proxy is an HTTP proxy that exposes the Anthropic Messages API
// and serves it from a GitHub Copilot subscription.
//
// Claude Code (or any Anthropic client) points [ANTHROPIC_BASE_URL] at this
// proxy. The proxy authenticates to GitHub Copilot, rewrites the requested
// model to a configured Copilot model by tier, and forwards the request to
// Copilot's native Anthropic endpoint. Responses, including streaming SSE,
// are relayed back unchanged: Copilot speaks the Anthropic wire format
// natively for Claude models, so no request or response translation occurs.
//
// The upstream base URL is resolved from the token exchange and is therefore
// plan-specific: Individual, Business, and Enterprise subscriptions route to
// their respective Copilot API hosts automatically, with no configuration.
// COPILOT_API_BASE forces a base URL when the exchange cannot be relied on.
//
// Copilot's edge validates the Anthropic-Beta header against an allowlist and
// rejects unrecognized betas with a 400. The proxy forwards only betas Copilot
// is known to accept and strips the rest (such as advisor-tool and context-1m),
// so a client requesting an unsupported beta degrades gracefully instead of
// failing the whole request.
//
// # Subcommands
//
//   - login: run the GitHub device-code flow and persist the resulting OAuth
//     token. Required once before serving unless a token is supplied via the
//     environment.
//   - run [args...]: launch claude through a proxy dedicated to that instance.
//     A fresh proxy is bound to an ephemeral loopback port, gated by a
//     per-instance secret, and ANTHROPIC_BASE_URL and ANTHROPIC_AUTH_TOKEN are
//     injected into claude's environment. Arguments are passed to claude, and
//     the proxy exits when claude does.
//   - serve: run a standalone, shared proxy on a fixed address. Accepts --addr
//     to override COPILOT_PROXY_ADDR.
//
// # Environment
//
//   - GITHUB_TOKEN: a GitHub OAuth token (from the device flow). Read in
//     preference to the persisted token file. Classic PATs are not accepted
//     by Copilot's token endpoint; use the login subcommand.
//   - COPILOT_PROXY_ADDR: serve listen address (default 127.0.0.1:9876).
//   - COPILOT_PROXY_CLAUDE: the claude binary the run subcommand launches
//     (default "claude").
//   - COPILOT_PROXY_MASTER_KEY: if set, inbound requests must present it via
//     Authorization: Bearer or x-api-key. If unset, inbound requests are not
//     authenticated (bind to localhost).
//   - COPILOT_PROXY_DATA_DIR: token storage directory (default
//     $XDG_DATA_HOME/copilot-api-proxy).
//   - COPILOT_MODEL_OPUS, COPILOT_MODEL_SONNET, COPILOT_MODEL_HAIKU,
//     COPILOT_MODEL_DEFAULT: Copilot model ids each Anthropic tier maps to.
//   - COPILOT_API_BASE: override the upstream base URL (default: resolved from
//     the token exchange, which is plan-specific — Individual, Business, and
//     Enterprise each resolve to their own Copilot API host).
//   - COPILOT_BETA_ALLOW: comma-separated Anthropic-Beta name prefixes to
//     forward, replacing the built-in allowlist. Copilot 400s on betas it does
//     not recognize, so unlisted betas are stripped.
//
// # Authorization and Terms of Service
//
// The proxy presents the well-known VS Code Copilot OAuth client and editor
// identification headers. Using a Copilot subscription outside sanctioned
// clients may violate GitHub's Terms of Service. Intended for personal use.
package main
