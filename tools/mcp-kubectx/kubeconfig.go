package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"
)

// Sentinel errors for kubeconfig and session operations.
var (
	ErrMissingContext  = errors.New("context name is required")
	ErrContextNotFound = errors.New("context not found")
	ErrLoadKubeconfig  = errors.New("load kubeconfig")
	ErrWriteKubeconfig = errors.New("write kubeconfig")
	ErrParseHostResult = errors.New("parse host select result")
)

// kubeConfig represents a minimal kubeconfig structure sufficient for
// listing contexts and extracting individual context entries. Cluster
// and user data use [any] to round-trip opaque fields without modeling
// the full schema.
type kubeConfig struct {
	APIVersion     string         `yaml:"apiVersion"`
	Kind           string         `yaml:"kind"`
	CurrentContext string         `yaml:"current-context"`
	Clusters       []namedCluster `yaml:"clusters"`
	Contexts       []namedContext `yaml:"contexts"`
	Users          []namedUser    `yaml:"users"`
}

type namedCluster struct {
	Cluster any    `yaml:"cluster"`
	Name    string `yaml:"name"`
}

type namedContext struct {
	Name    string         `yaml:"name"`
	Context contextDetails `yaml:"context"`
}

type contextDetails struct {
	Cluster   string `yaml:"cluster"`
	User      string `yaml:"user"`
	Namespace string `yaml:"namespace,omitempty"`
}

type namedUser struct {
	User any    `yaml:"user"`
	Name string `yaml:"name"`
}

// handler holds configuration parsed from command-line flags and
// the per-process state owned by serve. It is constructed only on
// the serve code path; the host * subcommands never touch it.
//
// currentSA is loaded by the UDS handler goroutine in
// [*handler.handleSocketConn] and stored by [*handler.selectCtx]
// after a successful host select. It carries the descriptor needed
// to mint a token via [runHostToken] without re-reading any flag
// state. Atomic rather than mutex-guarded so the socket goroutine
// never contends with selectCtx's existing critical sections.
//
// socketPath / socketListener / socketWG track the per-`serve` UDS
// lifecycle. socketListener is held only between [main.runServe]
// and the cleanup ordering in [*handler.sessionDir]; socketWG is
// drained by [*handler.socketShutdown]. socketSlots caps the number
// of slot paths [*handler.acquireServeSocket] probes at startup;
// each slot maps to one literal entry in Claude Code's sandbox
// allowUnixSockets allowlist.
type handler struct {
	socketListener  net.Listener
	envLookup       func(string) string
	runHost         runHostFunc
	currentSA       atomic.Pointer[currentSA]
	outputPath      string
	kubeconfigPath  string
	socketPath      string
	lastOutputPath  string
	allowedAPIHosts []string
	cleanupFuncs    []func(context.Context)
	sa              saConfig
	socketWG        sync.WaitGroup
	pid             int
	socketSlots     int
	mu              sync.Mutex
}

// registerCleanup appends a function to run during session teardown.
// Each call to the service-account tool registers its own cleanup.
func (h *handler) registerCleanup(fn func(context.Context)) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.cleanupFuncs = append(h.cleanupFuncs, fn)
}

// restoreCleanups swaps the live cleanup list back to a previously
// snapshotted slice. Used by the failure paths in [handler.selectCtx]
// to preserve the prior SA's release closure when a new provision
// fails midway, so it still runs at shutdown.
func (h *handler) restoreCleanups(prev []func(context.Context)) {
	h.mu.Lock()
	h.cleanupFuncs = prev
	h.mu.Unlock()
}

// sidecarSymlinkPath returns the per-Claude-session symlink path
// the Claude Code hook-router consults to find the active
// kubeconfig. Mirrors hook-router's lookup in main.go's
// configFromEnv: <TMPDIR>/claude-kubectx/<PPID>/kubeconfig. PPID
// here is Claude (which spawns this serve as a stdio MCP child),
// so the symlink scopes one kubeconfig to one Claude session
// without depending on the serve's own pid. Returns "" when PPID
// <= 1; without a Claude parent there is nothing for hook-router
// to scope to.
func sidecarSymlinkPath() string {
	ppid := os.Getppid()
	if ppid <= 1 {
		return ""
	}

	return filepath.Join(
		os.TempDir(), "claude-kubectx",
		strconv.Itoa(ppid), "kubeconfig",
	)
}

// sessionDir returns a cleanup closure for the per-`serve` session.
// Construction is infallible; the returned closure must be called
// once on shutdown.
//
// Shutdown ordering:
//  1. [*handler.socketShutdown] closes the UDS listener and waits
//     for in-flight connection handlers via h.socketWG. Required
//     so no handler is mid-`runHost` when later steps unlink files.
//  2. Drain registered K8s resource cleanups with a 30-second
//     [context.Background] timeout.
//  3. Unlink the socket file at h.socketPath.
//  4. Unlink the scoped kubeconfig at h.lastOutputPath. For a
//     guest serve this path lives under the writable bind mount
//     of `~/.local/state/mcp-kubectx` declared in workmux's
//     extra_mounts, so removal succeeds without any host-side
//     shell-out.
//  5. Unlink the hook-router sidecar symlink. It lives in the
//     local TMPDIR and never crosses the Lima boundary.
func (h *handler) sessionDir() func() {
	// runResourceCleanup intentionally derives from context.Background.
	// The serve's signal-rooted ctx is already canceled by the time
	// this runs (it fires *because* ctx canceled), so threading it
	// would abort in-flight Delete* calls and re-leak the SA.
	runResourceCleanup := func() { //nolint:contextcheck // see comment above
		h.mu.Lock()
		fns := h.cleanupFuncs
		h.mu.Unlock()

		if len(fns) == 0 {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		for _, fn := range fns {
			fn(ctx)
		}
	}

	return func() {
		h.socketShutdown()

		runResourceCleanup()

		h.mu.Lock()
		socketPath := h.socketPath
		outPath := h.lastOutputPath
		h.mu.Unlock()

		bestEffortRemove("serve socket", socketPath)
		bestEffortRemove("kubeconfig", outPath)
		// Sidecar is symlink-only: parent dir is intentionally
		// left in place because a peer serve under the same
		// Claude PPID may still depend on it. TMPDIR is reaped
		// at reboot.
		bestEffortRemove("sidecar symlink", sidecarSymlinkPath())
	}
}

// bestEffortRemove unlinks path, logging a warn on real errors and
// silently swallowing IsNotExist. Empty path is a no-op so callers
// can pass conditionally-set paths without an outer guard.
func bestEffortRemove(what, path string) {
	if path == "" {
		return
	}

	err := os.Remove(path)
	if err == nil || os.IsNotExist(err) {
		return
	}

	slog.Warn("cleanup",
		slog.String("kind", what),
		slog.String("path", path),
		slog.Any("error", err),
	)
}

// loadKubeconfig reads and parses a kubeconfig file.
func loadKubeconfig(path string) (*kubeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrLoadKubeconfig, err)
	}

	var cfg kubeConfig

	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrLoadKubeconfig, err)
	}

	return &cfg, nil
}

// ListInput is the MCP input schema for the list tool.
type ListInput struct{}

// list shells out to `host list` and relays its stdout as the MCP
// tool result. The host subcommand owns formatting; serve only
// surfaces it.
func (h *handler) list(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	_ ListInput,
) (*mcp.CallToolResult, any, error) {
	args := h.kubeconfigArgs()

	stdout, err := h.runHost(ctx, "list", args)
	if err != nil {
		return toolError(err), nil, nil
	}

	return toolResult(string(stdout)), nil, nil
}

// SelectInput is the MCP input schema for the select tool.
type SelectInput struct {
	Context string `json:"context" jsonschema:"Context name to select"`
}

// selectCtx shells out to `host select` and registers a release
// closure for the resulting ServiceAccount. Drain ordering:
//   - snapshot the previous cleanup list, clear the live one;
//   - run host select unlocked;
//   - on failure, [handler.restoreCleanups] swaps prev back so it
//     still runs at shutdown;
//   - on success, append a new release closure and drain prev with
//     a 30-second [context.Background] timeout.
//
// The mutex is held only across snapshot/swap and registration,
// never across the network call.
func (h *handler) selectCtx(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input SelectInput,
) (*mcp.CallToolResult, any, error) {
	if input.Context == "" {
		return toolError(ErrMissingContext), nil, nil
	}

	h.mu.Lock()
	prev := h.cleanupFuncs
	h.cleanupFuncs = nil
	h.mu.Unlock()

	prevSA := h.currentSA.Load()

	// rollback restores the snapshot taken above. Called by every
	// failure path between here and the success commit so kubectl
	// on the prior context keeps minting tokens for the prior SA.
	rollback := func() {
		h.restoreCleanups(prev)
		h.currentSA.Store(prevSA)
	}

	args := h.selectArgs(input.Context)

	stdout, err := h.runHost(ctx, "select", args)
	if err != nil {
		rollback()
		return toolError(err), nil, nil
	}

	var result HostSelectResult

	err = json.Unmarshal(stdout, &result)
	if err != nil {
		rollback()
		return toolError(fmt.Errorf("%w: %w", ErrParseHostResult, err)), nil, nil
	}

	releaseFn := h.releaseClosure(result)

	h.registerCleanup(releaseFn)

	h.mu.Lock()
	h.lastOutputPath = result.Path
	h.mu.Unlock()

	// Storing the new descriptor before draining the prior cleanups
	// is deliberate: kubectl on the new context can mint tokens for
	// the new SA while the prior SA's Delete* calls are still in
	// flight, hiding rotation latency from the user. The atomic
	// pointer guarantees the socket goroutine sees a coherent
	// old-or-new SA, never garbage.
	h.currentSA.Store(&currentSA{
		Kubeconfig: result.Kubeconfig,
		Context:    result.Context,
		SAName:     result.SAName,
		Namespace:  result.Namespace,
		Expiration: h.sa.expiration,
	})

	// result.Path is the path host select actually wrote, including
	// any host-side defaulting; using it points the symlink and
	// shutdown cleanup at the same file.
	h.publishSidecar(result.Path)

	if len(prev) > 0 {
		// drainCtx deliberately derives from context.Background --
		// the request ctx is canceled by the MCP SDK as soon as
		// this function returns, which would abort in-flight
		// Delete* calls and re-leak the SA.
		drainCtx, cancel := context.WithTimeout(
			context.Background(),
			30*time.Second,
		)

		for _, fn := range prev {
			fn(drainCtx) //nolint:contextcheck // see drainCtx comment
		}

		cancel()
	}

	return toolResult(fmt.Sprintf(
		"Created ServiceAccount for context %q bound to %s.\nKubeconfig written to %s",
		result.Context, describeBinding(h.sa), result.Path,
	)), nil, nil
}

// selectArgs builds the argv passed to `host select`. Serve owns
// the discriminator (pid + host/guest env) and forwards it as
// --pid; host select uses that, plus its own host-side
// [stateHomeDir], to resolve the kubeconfig path. When the user
// passed --output to serve, h.outputPath is non-empty and serve
// forwards it as --out-path so the user override still wins (host
// select ignores --pid in that branch). The socket path is
// forwarded directly from h.socketPath, the slot resolved once at
// serve startup by [*handler.acquireServeSocket]; selectArgs never
// re-derives the path so the kubeconfig and the bound listener
// cannot drift. Each allowed apiserver host is forwarded as a
// repeated `--allow-apiserver-host` flag; an empty list yields no
// flags and lets `host select` accept any apiserver.
func (h *handler) selectArgs(contextName string) []string {
	guest := h.isGuest()

	args := []string{contextName}
	args = append(args, h.kubeconfigArgs()...)
	args = append(args,
		"--pid", strconv.Itoa(h.pid),
		fmt.Sprintf("--for-guest=%t", guest),
		"--socket-path", h.socketPath,
		"--sa-role-name", h.sa.role,
		"--sa-role-kind", h.sa.roleKind,
		"--sa-namespace", h.sa.namespace,
		"--sa-expiration", strconv.Itoa(h.sa.expiration),
		fmt.Sprintf("--sa-cluster-scoped=%t", h.sa.clusterScoped),
	)

	if h.outputPath != "" {
		args = append(args, "--out-path", h.outputPath)
	}

	for _, host := range h.allowedAPIHosts {
		args = append(args, "--"+allowAPIServerHostFlag, host)
	}

	return args
}

// kubeconfigArgs returns the --kubeconfig flag pair for a host
// subcommand, or an empty slice when the user did not pass an
// explicit path to serve. Letting the host subcommand resolve the
// default itself avoids leaking the serve process's $HOME (e.g. a
// Lima guest's /home/user) into argv aimed at the macOS host.
func (h *handler) kubeconfigArgs() []string {
	if h.kubeconfigPath == "" {
		return nil
	}

	return []string{"--kubeconfig", h.kubeconfigPath}
}

// publishSidecar refreshes the per-Claude-session symlink at
// [sidecarSymlinkPath] to point at target (the kubeconfig path
// `host select` just wrote). The Claude Code hook-router resolves
// kubectl invocations through this symlink. Failure is non-fatal:
// hook-router falls back to its "no kubeconfig" denial, which is
// the same behavior callers see today before this fix.
func (h *handler) publishSidecar(target string) {
	sidecar := sidecarSymlinkPath()
	if sidecar == "" {
		return
	}

	err := writeSymlinkAtomic(sidecar, target)
	if err != nil {
		slog.Warn("write hook-router sidecar symlink",
			slog.String("path", sidecar),
			slog.Any("error", err),
		)

		return
	}

	slog.Info("published hook-router sidecar",
		slog.String("path", sidecar),
		slog.String("target", target),
	)
}

// releaseClosure builds the cleanup callback that shells out to
// `host release` for an SA created by a successful `host select`.
// Errors are logged but never propagated; release is best-effort.
func (h *handler) releaseClosure(result HostSelectResult) func(context.Context) {
	return func(ctx context.Context) {
		args := []string{
			"--kubeconfig", result.Kubeconfig,
			"--context", result.Context,
			"--sa", result.SAName,
			"--namespace", result.Namespace,
			fmt.Sprintf("--sa-cluster-scoped=%t", result.ClusterScoped),
		}

		_, err := h.runHost(ctx, "release", args)
		if err != nil {
			slog.WarnContext(ctx, "host release",
				slog.String("sa", result.SAName),
				slog.String("namespace", result.Namespace),
				slog.Any("error", err),
			)
		}
	}
}

// writeFileSecure writes data to path with 0600 permissions, creating
// parent directories as needed.
func writeFileSecure(path string, data []byte) error {
	err := os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	err = os.WriteFile(path, data, 0o600)
	if err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// writeSymlinkAtomic creates or replaces a symlink at path pointing
// to target. Parent dirs are created with 0o700. Replacement is
// atomic via tmp + rename, so a concurrent reader never observes
// a missing symlink.
func writeSymlinkAtomic(path, target string) error {
	err := os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	tmp := path + ".tmp"

	// Best-effort: ENOENT (no leftover) is fine; any other
	// failure surfaces below when os.Symlink hits EEXIST.
	_ = os.Remove(tmp) //nolint:errcheck // see comment

	err = os.Symlink(target, tmp)
	if err != nil {
		return fmt.Errorf("create symlink: %w", err)
	}

	err = os.Rename(tmp, path)
	if err != nil {
		// Best-effort rollback of the tmp symlink we just made.
		_ = os.Remove(tmp) //nolint:errcheck // see comment
		return fmt.Errorf("rename symlink: %w", err)
	}

	return nil
}

func toolResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}
}

func toolError(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		IsError: true,
	}
}
