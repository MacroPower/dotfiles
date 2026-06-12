// Package statedir resolves the per-user state directories that
// mcp-kubectx files live in, and the host/guest filename
// discriminator that keeps a host-side serve and a Lima-guest serve
// from colliding on shared paths.
//
// Resolution honors $XDG_STATE_HOME with the standard ~/.local/state
// fallback. The directory matters more than usual here: the
// mcp-kubectx state dir is bind-mounted into the Lima guest by
// workmux's extra_mounts, so both sides of the sandbox boundary must
// derive the same absolute path from the host's environment.
package statedir
