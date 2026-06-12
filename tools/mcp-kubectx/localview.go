package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kubeconfig"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/statefile"
)

// loadLocalConfig reads and parses the in-sandbox merged local
// kubeconfig at $CLAUDE_KUBECTX_LOCAL -- the first entry in the
// wrapper's merged $KUBECONFIG, owned by in-sandbox cluster tools
// and plain `kubectl config use-context`. Returns (nil, nil) when
// the var is unset (out-of-wrapper serve), so callers degrade to
// the external-only view. A read or parse failure surfaces as a
// non-nil error so the select dispatch can fall back to the
// external path rather than misclassify a context.
func loadLocalConfig() (*kubeconfig.Config, error) {
	path := os.Getenv("CLAUDE_KUBECTX_LOCAL")
	if path == "" {
		return nil, nil //nolint:nilnil // unset var is a valid "no local file" state
	}

	return kubeconfig.Load(path) //nolint:wrapcheck // Load already wraps kubeconfig.ErrLoad
}

// guestConfigPath returns the guest's ~/.kube/config path when the
// launcher wrapper exported $CLAUDE_KUBECTX_GUEST_CONFIG, or "" when
// the var is unset. The wrapper exports it only on the Lima guest
// image (gated by the dotfiles.claude.guestKubeconfigLocal build
// flag), so a serve that never received it -- a Darwin-host direct
// run, or any test -- sees "" and treats the guest config as absent.
//
// The decision keys on the env var alone, intentionally independent
// of [*handler.isGuest] / WM_SANDBOX_GUEST: the guest-config source is
// a property of how the wrapper laid out $KUBECONFIG, not of the
// host/guest shell-out routing, so the two must not be conflated.
func guestConfigPath() string {
	return os.Getenv("CLAUDE_KUBECTX_GUEST_CONFIG")
}

// loadGuestConfig reads the guest's ~/.kube/config -- the second entry
// in the in-sandbox merged $KUBECONFIG -- which holds the cluster and
// user definitions for guest-local clusters (kind / k3d / minikube /
// Talos-in-Docker). Returns (nil, nil) when $CLAUDE_KUBECTX_GUEST_CONFIG
// is unset or the file does not exist yet (it is created the first
// time a guest cluster is provisioned), so callers degrade to the
// local.yaml-only view. A read or parse failure of an existing file
// surfaces as a non-nil error.
func loadGuestConfig() (*kubeconfig.Config, error) {
	path := guestConfigPath()
	if path == "" {
		return nil, nil //nolint:nilnil // unset var is a valid "no guest config" state
	}

	cfg, err := kubeconfig.Load(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil //nolint:nilnil // missing file is a valid "no guest config yet" state
	}

	return cfg, err //nolint:wrapcheck // Load already wraps kubeconfig.ErrLoad
}

// localView reads the in-sandbox local sources and returns the
// merged-view selection plus the union of context names that route to
// the local (cluster-admin, no SA) path.
//
// current is read from local.yaml ($CLAUDE_KUBECTX_LOCAL) only -- the
// MCP-owned selection authority, first-file-wins in the merged
// $KUBECONFIG -- so an in-sandbox selection never depends on a plain
// guest shell's current-context. names is the order-preserving deduped
// union of the contexts in local.yaml and, when set, the guest's
// ~/.kube/config; local.yaml names come first so a name in both
// resolves to the local.yaml entry (client-go first-file-wins) and
// list() output stays stable.
//
// A read/parse failure of either source degrades to whatever the other
// source provides rather than failing the caller: list() falls back to
// the external-only view and the route check simply omits the
// unreadable source's names.
func localView() (string, []string) {
	var (
		current string
		names   []string
	)

	seen := make(map[string]struct{})

	add := func(cfg *kubeconfig.Config) {
		for _, c := range cfg.Contexts {
			if _, dup := seen[c.Name]; dup {
				continue
			}

			seen[c.Name] = struct{}{}
			names = append(names, c.Name)
		}
	}

	local, lerr := loadLocalConfig()
	if lerr == nil && local != nil {
		current = local.CurrentContext

		add(local)
	}

	guest, gerr := loadGuestConfig()
	if gerr == nil && guest != nil {
		add(guest)
	}

	return current, names
}

// localContextNames returns the set of context names that route to the
// local (cluster-admin, no SA) path: the deduped union of the contexts
// in local.yaml ($CLAUDE_KUBECTX_LOCAL) and, when
// $CLAUDE_KUBECTX_GUEST_CONFIG is set, the guest's ~/.kube/config.
// [*handler.selectCtx] uses membership here to route a context away
// from the external SA-mint path. Empty when neither source defines a
// context (out-of-wrapper serve, or the bare local.yaml stub).
func localContextNames() map[string]struct{} {
	_, names := localView()

	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}

	return set
}

// setLocalCurrentContext rewrites the top-level current-context in
// the local kubeconfig and writes it back atomically (tmp+rename via
// [statefile.WriteAtomic]). An empty name clears the field. No-op
// when $CLAUDE_KUBECTX_LOCAL is unset.
//
// A reaped session dir is tolerated: across a serve restart the
// wrapper's $CLAUDE_KUBECTX_LOCAL can outlive the per-session dir it
// names (the dir is swept once its PID dies), so the stub the wrapper
// seeds on a clean start may be gone. When the file or its parent is
// missing, the stub is recreated rather than erroring -- the file
// holds only current-context, so a fresh one loses nothing.
//
// In the merged $KUBECONFIG the local file is first, so client-go
// resolves current-context first-file-wins: this file is the
// authoritative merged-view selection for external, guest-local, and
// in-sandbox-local contexts alike. select writes through here so a
// selection takes effect even when the selected context's creds live
// in another merge entry -- the sidecar for an external context, the
// guest's ~/.kube/config for a guest-local one. local.yaml itself
// holds only the current-context selection; the MCP never writes
// cluster/user entries into it.
//
// The write round-trips the file through [kubeconfig.Config], so the
// local file is normalized to that modeled subset on every call:
// top-level preferences/extensions and per-context extensions are
// not preserved. Keeping cluster/user definitions out of local.yaml
// is what makes that round-trip lossless and safe -- the user's real
// guest config is never normalized through here.
func setLocalCurrentContext(name string) error {
	path := os.Getenv("CLAUDE_KUBECTX_LOCAL")
	if path == "" {
		return nil
	}

	cfg, err := kubeconfig.Load(path)
	if errors.Is(err, fs.ErrNotExist) {
		cfg = &kubeconfig.Config{APIVersion: "v1", Kind: "Config"}
	} else if err != nil {
		return err //nolint:wrapcheck // Load already wraps kubeconfig.ErrLoad
	}

	cfg.CurrentContext = name

	data, err := cfg.Marshal()
	if err != nil {
		return err //nolint:wrapcheck // Marshal already wraps kubeconfig.ErrWrite
	}

	//nolint:gosec // $CLAUDE_KUBECTX_LOCAL names the wrapper-owned local.yaml by design
	err = os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		return fmt.Errorf("%w: %w", kubeconfig.ErrWrite, err)
	}

	return statefile.WriteAtomic(path, data, 0o600) //nolint:wrapcheck // WriteAtomic errors are self-describing
}

// mergeListOutput rebuilds the `host list` text into the merged
// view. External lines have their own ` (current)` suffix stripped
// (the admin kubeconfig's current-context is meaningless here), each
// local context is appended tagged `(local)`, and a single
// `(current)` marker is applied wherever a name matches current. On
// a name collision the local context wins: the external line is
// dropped so the name resolves to the local entry, matching
// client-go's first-file-wins merge. The surviving local line is
// tagged `(local, shadows external)` so the collision -- which makes
// the external context unreachable for as long as the local name
// exists -- stays visible instead of silently swallowing a context.
func mergeListOutput(hostOut string, localNames []string, current string) string {
	local := make(map[string]struct{}, len(localNames))
	for _, n := range localNames {
		local[n] = struct{}{}
	}

	var b strings.Builder

	b.WriteString("Available contexts:\n")

	wrote := false
	shadowed := make(map[string]struct{})

	for line := range strings.SplitSeq(hostOut, "\n") {
		if !strings.HasPrefix(line, "- ") {
			continue
		}

		name := strings.TrimSuffix(strings.TrimPrefix(line, "- "), " (current)")
		if _, isLocal := local[name]; isLocal {
			shadowed[name] = struct{}{}
			continue
		}

		writeContextLine(&b, name, "", name == current)

		wrote = true
	}

	for _, name := range localNames {
		tag := "local"
		if _, s := shadowed[name]; s {
			tag = "local, shadows external"
		}

		writeContextLine(&b, name, tag, name == current)

		wrote = true
	}

	if !wrote {
		return "No contexts found."
	}

	return b.String()
}

// writeContextLine appends one `- <name>[ (<tag>)][ (current)]` line.
// An empty tag omits the tag parenthetical.
func writeContextLine(b *strings.Builder, name, tag string, current bool) {
	b.WriteString("- ")
	b.WriteString(name)

	if tag != "" {
		b.WriteString(" (")
		b.WriteString(tag)
		b.WriteString(")")
	}

	if current {
		b.WriteString(" (current)")
	}

	b.WriteString("\n")
}
