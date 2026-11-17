package statedir

import (
	"fmt"
	"os"
	"path/filepath"
)

// Subdir resolves $XDG_STATE_HOME/<sub> with the standard
// ~/.local/state fallback. Centralized so [RunDir] and its callers
// cannot drift on the lookup-and-fallback rules.
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

// RunDir returns the directory of the mcp-kubectx files that stay on
// the machine that resolves it. workmux mounts no part of this
// directory into a Lima guest. It holds the per-`serve` sockets, the
// persistent host id of each env, the provision records, and the
// scoped kubeconfig of each serve. home/claude.nix also passes a
// path in this directory as the --log-file of serve.
func RunDir() string {
	return Subdir("mcp-kubectx-run")
}

// KubeconfigPath returns the default path of the scoped kubeconfig
// that the serve with process id pid writes. The file lies in
// [RunDir] of the machine that runs the serve, and its name is
// kubeconfig.<pid>.<env>.yaml, with the env tag of [EnvTag].
func KubeconfigPath(pid int, forGuest bool) string {
	return filepath.Join(RunDir(), fmt.Sprintf("kubeconfig.%d.%s.yaml", pid, EnvTag(forGuest)))
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
