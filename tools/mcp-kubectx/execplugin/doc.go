// Package execplugin implements both halves of the kubectl exec
// credential plugin protocol mcp-kubectx speaks: composing the
// `user.exec` block written into scoped kubeconfigs, and the tiny
// Unix-domain-socket client that block invokes to fetch an
// [Credential] JSON document from a running `serve`.
//
// The plugin block is a function only of the socket path. Both
// host- and guest-side serves write the same shape, so kubectl never
// sees the workmux host-exec wrapper; the guest/host routing happens
// server-side after the shim's bytes cross the socket.
//
// [Fetch] validates before relaying: a clean EOF with zero bytes
// means the serve declined to answer, and partial bytes from a
// server-side deadline must not reach kubectl's stdout as torn JSON.
// Both cases surface as sentinel errors so the caller exits non-zero
// deterministically.
package execplugin
