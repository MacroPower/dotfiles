package statedir

import (
	"os"
	"path/filepath"
)

// Subdir resolves $XDG_STATE_HOME/<sub> with the standard
// ~/.local/state fallback. Centralized so [Dir] and the socket
// state dir cannot drift on the lookup-and-fallback rules.
func Subdir(sub string) string {
	if state := os.Getenv("XDG_STATE_HOME"); state != "" {
		return filepath.Join(state, sub)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".local", "state", sub)
	}

	return filepath.Join(home, ".local", "state", sub)
}

// Dir returns the mcp-kubectx state directory used for per-`serve`
// kubeconfig files and the persistent host id. Resolved on the host
// side because the files live on the host filesystem; a Lima-guest
// serve sees the same path through the writable bind mount declared
// in workmux's extra_mounts.
func Dir() string {
	return Subdir("mcp-kubectx")
}

// EnvTag returns "guest" when forGuest is true and "host" otherwise.
// Used as the filename discriminator on the per-pid kubeconfig, the
// per-serve UDS path, and the persistent host id file, so host- and
// guest-side serves on the same machine never overwrite each other's
// state.
func EnvTag(forGuest bool) string {
	if forGuest {
		return "guest"
	}

	return "host"
}
