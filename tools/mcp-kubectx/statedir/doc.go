// Package statedir resolves the per-user state directories that
// mcp-kubectx files live in. It also holds the host/guest filename
// discriminator that keeps a host-side serve and a Lima-guest serve
// from colliding on shared paths.
//
// Resolution honors $XDG_STATE_HOME with the standard ~/.local/state
// fallback, in the environment of the calling process.
//
// The two directories differ in who can write them. workmux's
// extra_mounts bind-mounts the [Dir] of the host writable into the
// Lima guest at the same absolute path. So a path in that directory
// that the host resolves, such as the scoped kubeconfig that
// `host select` writes for a guest-side serve, names the same file in
// the guest. Every guest can also rewrite each file of that
// directory, so no host-side process trusts a file there. workmux
// mounts no part of [RunDir] into a guest. So the files that a
// host-side process trusts, such as the host id and the scoped
// kubeconfig of each host-side serve, lie in [RunDir]. A path that a
// guest-side serve resolves names a file in a state directory of the
// guest, outside the mount.
package statedir
