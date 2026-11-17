// Package statedir resolves the per-user state directory that
// mcp-kubectx files live in. It also holds the host/guest filename
// discriminator that keeps a host-side serve and a Lima-guest serve
// from colliding on shared paths.
//
// Resolution honors $XDG_STATE_HOME with the standard ~/.local/state
// fallback, in the environment of the calling process. A path that a
// guest-side serve resolves names a file on the disk of its guest,
// and a path that a `host` subcommand resolves names a file on the
// host. workmux mounts no part of [RunDir] into a guest. So the
// files that a host-side process trusts, such as the host id, the
// provision records, and the scoped kubeconfig of each host-side
// serve, lie in [RunDir].
package statedir
