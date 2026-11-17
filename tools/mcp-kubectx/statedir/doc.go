// Package statedir resolves the per-user state directories that
// mcp-kubectx files live in, and the host/guest filename
// discriminator that keeps a host-side serve and a Lima-guest serve
// from colliding on shared paths.
//
// Resolution honors $XDG_STATE_HOME with the standard ~/.local/state
// fallback, in the environment of the calling process. workmux's
// extra_mounts bind-mounts the mcp-kubectx state dir of the host into
// the Lima guest at the same absolute path. So a path in that dir
// that the host resolves, such as the scoped kubeconfig that
// `host select` writes, names the same file in the guest. A path
// that a guest-side serve resolves names a file in the state dir of
// the guest, outside the mount.
package statedir
