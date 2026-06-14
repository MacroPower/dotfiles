// Package auth handles GitHub Copilot authentication.
//
// Two credentials are involved. The durable credential is a GitHub OAuth
// token obtained once through the device-code flow ([Login]). The runtime
// credential is a short-lived (~30 minute) Copilot session token obtained by
// exchanging the GitHub token at Copilot's internal token endpoint.
//
// A [Manager] owns one GitHub account's credentials. It performs the initial
// exchange, keeps the session token fresh with a background refresh loop, and
// hands out a valid bearer token and the plan-specific API base URL via
// [Manager.Current]. After an upstream 401, [Manager.ForceRefresh] mints a new
// session token, coalescing concurrent callers so only one exchange runs.
//
// Multi-account rotation is not implemented; a pool of managers selected per
// request slots in above this package without changing it.
package auth
