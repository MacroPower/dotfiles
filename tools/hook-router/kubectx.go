package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"gopkg.in/yaml.v3"
)

// kubectxDirPrefix is the basename prefix that [handleSessionEnd]
// and [sweepKubectxDirs] use to identify per-session Claude kubectx
// directories. The Claude Code launcher wrapper writes the full path
// as $XDG_RUNTIME_DIR/claude-kubectx.<pid> (falling back to /tmp).
const kubectxDirPrefix = "claude-kubectx."

// kubeconfigContexts is the minimal kubeconfig shape the bash gate
// needs: the active current-context plus the set of locally-defined
// context names. Cluster/user/auth material is intentionally not
// modeled -- the gate only decides whether a context is selectable,
// never reads creds.
type kubeconfigContexts struct {
	CurrentContext string `yaml:"current-context"`
	Contexts       []struct {
		Name string `yaml:"name"`
	} `yaml:"contexts"`
}

// loadKubeconfigContexts reads and parses the named kubeconfig into
// the minimal [kubeconfigContexts] shape.
func loadKubeconfigContexts(path string) (*kubeconfigContexts, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read kubeconfig: %w", err)
	}

	var cfg kubeconfigContexts

	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}

	return &cfg, nil
}

// kubectxSelected returns the local merged kubeconfig path when a
// context is effectively selected for kubectl, or "" when nothing
// usable is selected. The bash gate denies kubectl on "".
//
// $CLAUDE_KUBECTX_LOCAL is the first entry in the wrapper's merged
// $KUBECONFIG and holds the authoritative current-context. A
// context counts as selected only when current-context is non-empty
// AND one of:
//   - it names a context in the union of local.yaml and, on the guest
//     image, the guest's ~/.kube/config (in-sandbox cluster-admin
//     creds, inline in whichever of those two files defines it), or
//   - the external sidecar symlink exists at $CLAUDE_KUBECTX_SIDECAR,
//     meaning mcp-kubectx published usable view-scoped creds.
//
// The compound check rejects a `kubectl config use-context
// <external>` for a context that was never MCP-selected: it sets
// current-context but leaves no sidecar, so the gate still emits the
// actionable "select a context first" deny instead of letting
// kubectl fail with a raw auth error. The bare stub (empty
// current-context) likewise denies.
//
// current-context is read from local.yaml only, preserving the
// "select first" authority even when the named context is defined in
// the guest config.
func kubectxSelected() string {
	local := os.Getenv("CLAUDE_KUBECTX_LOCAL")
	if local == "" {
		return ""
	}

	cfg, err := loadKubeconfigContexts(local)
	if err != nil || cfg.CurrentContext == "" {
		return ""
	}

	if contextInUnion(cfg, cfg.CurrentContext) {
		return local
	}

	if sidecar := os.Getenv("CLAUDE_KUBECTX_SIDECAR"); sidecar != "" {
		if _, err := os.Lstat(sidecar); err == nil {
			return local
		}
	}

	return ""
}

// contextInUnion reports whether name is defined in the local
// kubeconfig contexts or, when $CLAUDE_KUBECTX_GUEST_CONFIG is set, in
// the guest's ~/.kube/config. The guest config is loaded tolerantly:
// an unset var or a missing/unreadable file contributes no contexts,
// so off-guest the check is exactly the local-only membership test.
//
// The var is read from the hook-router's own process env, descended
// from the wrapped claude process that exported it -- the same
// provenance as $CLAUDE_KUBECTX_LOCAL / $CLAUDE_KUBECTX_SIDECAR -- so
// it is present whenever the gate evaluates a guest-local selection.
//
// This is the gate-side twin of mcp-kubectx's localContextNames union.
// The two live in separate binaries with no shared package, so the
// local+guest membership rule is maintained in both; keep them in sync.
func contextInUnion(local *kubeconfigContexts, name string) bool {
	defines := func(cfg *kubeconfigContexts) bool {
		for _, c := range cfg.Contexts {
			if c.Name == name {
				return true
			}
		}

		return false
	}

	if defines(local) {
		return true
	}

	guest := os.Getenv("CLAUDE_KUBECTX_GUEST_CONFIG")
	if guest == "" {
		return false
	}

	cfg, err := loadKubeconfigContexts(guest)
	if err != nil {
		return false
	}

	return defines(cfg)
}

// handleSessionEnd removes the per-session kubectx directory rooted
// at $CLAUDE_KUBECTX_DIR. The directory is created by the Claude
// Code launcher wrapper, populated by mcp-kubectx's
// [publishSidecar], and unused after the session exits.
//
// Cleanup is best-effort: a missing dir, a permissions failure, or
// a malformed env var all log at warn and return nil. The hook is
// fired by Claude Code as its session terminates, so a non-zero
// exit would only produce noise the user cannot act on.
//
// Containment guard: the env value must end in $kubectxDirPrefix<pid>
// or [handleSessionEnd] refuses to recurse. Without that check a
// rogue env value (e.g. CLAUDE_KUBECTX_DIR=/home/user) would let
// the hook nuke the user's home dir on session end.
func handleSessionEnd(_ context.Context, logger *slog.Logger) error {
	dir := os.Getenv("CLAUDE_KUBECTX_DIR")
	if dir == "" {
		return nil
	}

	if !isClaudeKubectxDir(dir) {
		logger.Warn("refusing to remove unrecognized CLAUDE_KUBECTX_DIR",
			slog.String("dir", dir),
		)

		return nil
	}

	err := os.RemoveAll(dir)
	if err != nil {
		logger.Warn("remove session kubectx dir",
			slog.String("dir", dir),
			slog.Any("error", err),
		)

		return nil
	}

	logger.Info("removed session kubectx dir", slog.String("dir", dir))

	return nil
}

// kubectxDirPID extracts the PID encoded in a per-session kubectx
// directory's basename, reporting ok only when name has the exact
// $kubectxDirPrefix<pid> shape the launcher wrapper produces: the
// prefix followed by a canonical positive decimal PID. Non-canonical
// suffixes (a leading sign, leading zeros, non-digits, or a
// non-positive value) are rejected so neither the SessionEnd
// containment guard nor the SessionStart sweep acts on a path that
// only superficially resembles a session dir.
func kubectxDirPID(name string) (int, bool) {
	if !strings.HasPrefix(name, kubectxDirPrefix) {
		return 0, false
	}

	suffix := name[len(kubectxDirPrefix):]

	pid, err := strconv.Atoi(suffix)
	if err != nil || pid <= 0 || strconv.Itoa(pid) != suffix {
		return 0, false
	}

	return pid, true
}

// isClaudeKubectxDir reports whether path has the
// $kubectxDirPrefix<pid> basename shape produced by the launcher
// wrapper. Pinning the shape on both the basename and a canonical
// PID suffix prevents removal of an arbitrary path injected through
// the env var.
func isClaudeKubectxDir(path string) bool {
	_, ok := kubectxDirPID(filepath.Base(path))

	return ok
}

// sweepKubectxDirs removes orphaned per-session kubectx directories
// in parent whose PID suffix is no longer alive. Runs from
// [handleSessionStart] (best-effort) so a crashed session that
// skipped its SessionEnd hook gets cleaned up the next time Claude
// starts.
//
// parent defaults to $XDG_RUNTIME_DIR (with /tmp fallback) so each
// host's tmpfs gets swept in isolation: a directory created on one
// machine via NFS would not match the local PID set anyway.
func sweepKubectxDirs(parent string, logger *slog.Logger) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Debug("sweep kubectx dirs: read parent",
				slog.String("parent", parent),
				slog.Any("error", err),
			)
		}

		return
	}

	for _, e := range entries {
		name := e.Name()

		pid, ok := kubectxDirPID(name)
		if !ok {
			continue
		}

		if pidAlive(pid) {
			continue
		}

		full := filepath.Join(parent, name)

		err = os.RemoveAll(full)
		if err != nil {
			logger.Warn("sweep kubectx dirs: remove",
				slog.String("dir", full),
				slog.Any("error", err),
			)

			continue
		}

		logger.Info("swept orphaned kubectx dir", slog.String("dir", full))
	}
}

// pidAlive reports whether pid names a live process. Uses signal 0,
// which performs the existence + permission check without delivering
// a signal: kill(pid, 0) returns ESRCH for a dead pid and EPERM for
// a live pid owned by another uid. Only ESRCH is treated as dead so
// the sweep does not delete a still-active session's dir when run
// under a different uid (defensive; in practice both sessions run as
// the same user).
func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}

	return !errors.Is(err, syscall.ESRCH)
}

// kubectxSweepParent returns the directory the launcher wrapper
// writes per-session kubectx dirs into. The wrapper bakes the
// resolved location into $CLAUDE_KUBECTX_DIR
// ($XDG_RUNTIME_DIR/claude-kubectx.<pid>, falling back to /tmp), so
// the parent of that value is the authoritative sweep root: deriving
// it keeps the sweep aligned with where the wrapper actually wrote
// even if hook-router's own $XDG_RUNTIME_DIR has since drifted.
// Falls back to the wrapper's own resolution rule when the env var
// is absent (an out-of-wrapper SessionStart).
func kubectxSweepParent() string {
	if dir := os.Getenv("CLAUDE_KUBECTX_DIR"); dir != "" {
		return filepath.Dir(dir)
	}

	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return d
	}

	return "/tmp"
}
