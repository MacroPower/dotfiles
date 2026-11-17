package statedir

import (
	"fmt"
	"os"
	"path/filepath"
)

// Subdir resolves $XDG_STATE_HOME/<sub> with the standard
// ~/.local/state fallback. Centralized so [Dir] and [RunDir] cannot
// drift on the lookup-and-fallback rules.
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
// process. workmux's extra_mounts mounts this directory of the host
// writable into each Lima guest at the same path. So every guest can
// change each file in it. `host select` writes the scoped kubeconfig
// of each guest-side serve here, because the guest-side serve
// reaches the file through that mount. A file that a host-side
// process trusts belongs in [RunDir].
func Dir() string {
	return Subdir("mcp-kubectx")
}

// RunDir returns the directory of the mcp-kubectx files that stay on
// the machine that resolves it. workmux mounts no part of this
// directory into a Lima guest. It holds the per-`serve` sockets, the
// persistent host id of each env, and the scoped kubeconfig of each
// host-side serve. Host-side kubectl runs the exec plugin that such
// a kubeconfig names. The startup sweep of a host-side serve deletes
// resources by the host id. So neither file goes in [Dir].
func RunDir() string {
	return Subdir("mcp-kubectx-run")
}

// KubeconfigPath returns the default path of the scoped kubeconfig
// that `host select` writes for the serve with process id pid. The
// file name is kubeconfig.<pid>.<env>.yaml, with the env tag of
// [EnvTag]. The kubeconfig of a host-side serve lies in [RunDir].
// The kubeconfig of a guest-side serve lies in [Dir]. `host select`
// runs on the host, and the guest reads the file through the mount
// of [Dir].
func KubeconfigPath(pid int, forGuest bool) string {
	dir := RunDir()
	if forGuest {
		dir = Dir()
	}

	return filepath.Join(dir, fmt.Sprintf("kubeconfig.%d.%s.yaml", pid, EnvTag(forGuest)))
}

// EnvTag returns "guest" when forGuest is true and "host" otherwise.
// The tag goes into the names of the per-pid kubeconfig, the
// per-serve UDS, and the persistent host id file. So host- and
// guest-side serves on the same machine never overwrite each other's
// state.
func EnvTag(forGuest bool) string {
	if forGuest {
		return "guest"
	}

	return "host"
}
