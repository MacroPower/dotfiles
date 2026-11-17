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

// Dir returns the mcp-kubectx state directory of the calling
// process. `host select` writes the per-`serve` kubeconfig files
// there, and `serve` keeps its persistent host id there. `host
// select` always runs on the host. So a Lima-guest serve reaches its
// kubeconfig file at the same path, through the writable bind mount
// that workmux's extra_mounts declares. A Lima-guest serve resolves
// Dir in the guest, so its host id file lies on the disk of the
// guest.
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
