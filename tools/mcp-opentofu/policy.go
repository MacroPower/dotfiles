package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// ErrPolicy is returned for failures while loading or validating sandbox
// policies, including malformed policy files and per-call AllowedPaths
// rejections.
var ErrPolicy = errors.New("policy")

// Policy describes the per-tool sandbox allowlist applied around every tofu
// invocation. Empty values mean "deny by default": no extra network, no
// extra filesystem access, no extra unix sockets. Per-call inputs (such as
// AllowedPaths on the validate and init handlers) are merged on top
// of the per-tool [Policy] before invoking the [Sandbox].
type Policy struct {
	// AllowedDomains lists DNS names that the sandbox should permit
	// outbound network access to. Enforcement honesty: on Darwin the names
	// are resolved to IPs at policy-load time and pinned (CDN-fronted
	// hosts will be flaky); on Linux an empty list disables network
	// (--unshare-net) but a non-empty list shares the host network
	// without per-domain filtering.
	AllowedDomains []string `json:"allowed_domains,omitempty"`

	// AllowUnixSockets lists absolute paths to unix domain sockets that
	// should be readable from inside the sandbox.
	AllowUnixSockets []string `json:"allow_unix_sockets,omitempty"`

	// AllowRead lists absolute filesystem paths the sandbox should mount
	// read-only (in addition to the working directory).
	AllowRead []string `json:"allow_read,omitempty"`

	// AllowWrite lists absolute filesystem paths the sandbox should mount
	// read-write (in addition to the working directory).
	AllowWrite []string `json:"allow_write,omitempty"`
}

// Policies is the per-tool [Policy] map used by [*handler]. Keys are MCP
// tool names ([toolValidate] and [toolInit]); missing keys behave
// as if the tool had a zero-value [Policy].
type Policies map[string]Policy

// Defaults returns a [Policies] map with an empty [Policy] for every
// sandboxed tool — the safe fallback used when no policy file is
// available.
func Defaults() Policies {
	return Policies{
		toolValidate: {},
		toolInit:     {},
	}
}

// LoadFile reads and parses path as a JSON object whose keys are tool names
// and whose values are [Policy] objects. The file is required when sandbox
// is enabled; callers handle the not-found case differently depending on
// the --sandbox flag.
func LoadFile(path string) (Policies, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: reading %s: %w", ErrPolicy, path, err)
	}

	var policies Policies

	err = json.Unmarshal(body, &policies)
	if err != nil {
		return nil, fmt.Errorf("%w: parsing %s: %w", ErrPolicy, path, err)
	}

	return policies, nil
}

// credentialsDeny is the set of paths that must never be exposed to a
// sandboxed tofu invocation, even when they sit under the configured
// allow-root. The list is rooted at $HOME at the time validateExtraPath
// is called and matched as a prefix against the resolved candidate path.
//
// Hard-coding the set keeps the policy file from accidentally widening
// access to credentials a tofu provider could otherwise read via
// data.local_file or data.external.
var credentialsDeny = []string{
	".ssh",
	".aws",
	".gnupg",
	".kube",
	".netrc",
	".git-credentials",
	".docker",
	".config/sops",
	".config/op",
	".terraformrc",
}

// allowRootEnv overrides the default allow-root ($HOME) for tests and
// uncommon deployments. The value must be an absolute path that exists
// on disk; symlinks are resolved once at startup.
const allowRootEnv = "MCP_OPENTOFU_ALLOW_ROOT"

// resolveAllowRoot reads [allowRootEnv] (falling back to $HOME) and
// resolves the result through [filepath.EvalSymlinks]. The caller
// invokes this once at startup and threads the resolved path to every
// [validateExtraPath] call.
func resolveAllowRoot() (string, error) {
	root := os.Getenv(allowRootEnv)
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("%w: resolving home directory: %w", ErrPolicy, err)
		}

		root = home
	}

	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("%w: allow root %q is not absolute", ErrPolicy, root)
	}

	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("%w: resolving allow root %q: %w", ErrPolicy, root, err)
	}

	return resolved, nil
}

// validateExtraPath resolves path through [filepath.EvalSymlinks], checks
// that the result lives under root, and rejects any path whose resolved
// form has a prefix in [credentialsDeny]. The resolved path is returned
// on success so callers feed the canonical form to the sandbox.
//
// The path must already exist on disk; [filepath.EvalSymlinks] is what
// gives the rest of the validation its anti-spoofing teeth, so accepting
// a not-yet-created path would defeat the symlink resolution gate.
//
// The two gates are independent: credentialsDeny is checked even when the
// candidate sits under root, since the allow-root contains every entry
// in the deny set by construction. Deny-set entries are anchored on root
// (not $HOME) so an [allowRootEnv] override still inherits credential
// protection.
func validateExtraPath(path, root string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("%w: allowed path is empty", ErrPolicy)
	}

	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: allowed path %q must be absolute", ErrPolicy, path)
	}

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("%w: resolving %q (path must exist): %w", ErrPolicy, path, err)
	}

	if !pathHasPrefix(resolved, root) {
		return "", fmt.Errorf("%w: allowed path %q resolves to %q, which is outside allow root %q",
			ErrPolicy, path, resolved, root)
	}

	for _, suffix := range credentialsDeny {
		denied := filepath.Join(root, suffix)
		if pathHasFSPrefix(resolved, denied) {
			return "", fmt.Errorf("%w: allowed path %q resolves to %q, which is inside the credentials deny set (%s)",
				ErrPolicy, path, resolved, denied)
		}
	}

	return resolved, nil
}

// pathHasPrefix reports whether path lies inside prefix. Equality counts
// as inside; the boundary check on the trailing separator guards
// against `/foo/barbaz` falsely matching `/foo/bar`. Both arguments are
// assumed absolute and cleaned.
func pathHasPrefix(path, prefix string) bool {
	return pathHasPrefixWith(path, prefix, func(a, b string) bool { return a == b })
}

// pathHasFSPrefix is [pathHasPrefix] with macOS-aware case folding:
// APFS volumes are case-insensitive by default, so `~/.SSH` and
// `~/.ssh` refer to the same directory and the credentials deny set
// must match both. On other platforms it behaves identically to
// [pathHasPrefix].
func pathHasFSPrefix(path, prefix string) bool {
	eq := func(a, b string) bool { return a == b }
	if runtime.GOOS == "darwin" {
		eq = strings.EqualFold
	}

	return pathHasPrefixWith(path, prefix, eq)
}

func pathHasPrefixWith(path, prefix string, eq func(a, b string) bool) bool {
	if eq(path, prefix) {
		return true
	}

	sep := prefix
	if !strings.HasSuffix(sep, string(filepath.Separator)) {
		sep += string(filepath.Separator)
	}

	return len(path) >= len(sep) && eq(path[:len(sep)], sep)
}

// mergeAllowRead returns a fresh slice containing base followed by every
// entry of extras not already present. Always returns a slice independent
// of base so callers can mutate the result without aliasing the source.
func mergeAllowRead(base, extras []string) []string {
	out := slices.Clone(base)
	for _, p := range extras {
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}

	return out
}
