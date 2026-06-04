package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
//
// instanceID is the per-`serve` random identifier tagged on every
// provisioned SA and binding via [instanceIDLabel]; hostID is the
// persistent per-user-per-host identifier tagged via [hostIDLabel].
// Both are populated by [main.runServe] before
// [*handler.acquireServeSocket] runs so the per-slot sidecar and
// the resource labels stay aligned. sweepWG tracks the background
// `host sweep` goroutine launched at startup so shutdown can wait
// for it before unlinking files.
type handler struct {
	socketListener  net.Listener
	envLookup       func(string) string
	runHost         runHostFunc
	currentSA       atomic.Pointer[currentSA]
	outputPath      string
	kubeconfigPath  string
	socketPath      string
	lastOutputPath  string
	instanceID      string
	hostID          string
	allowedAPIHosts []string
	cleanupFuncs    []func(context.Context)
	sa              saConfig
	socketWG        sync.WaitGroup
	sweepWG         sync.WaitGroup
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
// the Claude Code hook-router consults to find the active external
// kubeconfig.
//
// Preferred lookup: $CLAUDE_KUBECTX_SIDECAR. The Claude Code
// launcher wrapper sets it to "$CLAUDE_KUBECTX_DIR/kubeconfig" --
// the second entry in the merged $KUBECONFIG colon-list. Reading
// the explicit var short-circuits the containment check below,
// which cannot inspect a colon-separated $KUBECONFIG: a list never
// equals a single contained path.
//
// Fallback lookup: $KUBECONFIG, when it is a single path sitting
// inside $CLAUDE_KUBECTX_DIR. Serves an out-of-wrapper serve that
// sets a single-path $KUBECONFIG. The $CLAUDE_KUBECTX_DIR
// containment check guards against a stray $KUBECONFIG pointing at
// the user's real kubeconfig: without it, publishSidecar would
// overwrite that file with a symlink. The trailing path separator
// on the prefix rejects sibling-directory confusion (e.g.
// CLAUDE_KUBECTX_DIR=/run/claude-kubectx.1 with
// $KUBECONFIG=/run/claude-kubectx.12/kubeconfig).
//
// Fallback: <TMPDIR>/claude-kubectx/<PPID>/kubeconfig. Serves dev /
// ad-hoc serve invocations that set no wrapper env. Returns "" when
// neither path applies (PPID <= 1 and no wrapper env). hook-router
// falls back to its "no kubeconfig" denial when the symlink does
// not exist.
func sidecarSymlinkPath() string {
	if p := os.Getenv("CLAUDE_KUBECTX_SIDECAR"); p != "" {
		return p
	}

	if p := os.Getenv("KUBECONFIG"); p != "" && insideClaudeKubectxDir(p) {
		return p
	}

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
//  2. Drain the background sweep goroutine via h.sweepWG with a
//     30-second timeout. The timeout exists because a hung `host
//     sweep` subprocess would otherwise block shutdown
//     indefinitely; the existing 30-second timeout inside
//     runResourceCleanup only scopes that function's K8s release
//     calls, not the surrounding sessionDir closure.
//  3. Drain registered K8s resource cleanups with a 30-second
//     [context.Background] timeout.
//  4. Unlink the socket file at h.socketPath.
//  5. Unlink the per-slot sidecar at [sidecarPath].
//  6. Unlink the scoped kubeconfig at h.lastOutputPath. For a
//     guest serve this path lives under the writable bind mount
//     of `~/.local/state/mcp-kubectx` declared in workmux's
//     extra_mounts, so removal succeeds without any host-side
//     shell-out.
//  7. Unlink the hook-router sidecar symlink. It lives in the
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

		h.drainSweep()

		runResourceCleanup()

		h.mu.Lock()
		socketPath := h.socketPath
		outPath := h.lastOutputPath
		h.mu.Unlock()

		bestEffortRemove("serve socket", socketPath)
		bestEffortRemove("serve sidecar", sidecarPath(socketPath))
		bestEffortRemove("kubeconfig", outPath)
		// Sidecar is symlink-only: parent dir is intentionally
		// left in place because a peer serve under the same
		// Claude PPID may still depend on it. TMPDIR is reaped
		// at reboot.
		bestEffortRemove("sidecar symlink", sidecarSymlinkPath())
	}
}

// sweepDrainTimeout caps how long [*handler.drainSweep] will wait
// for the background sweep goroutine at shutdown. Package-level
// so tests can shorten it; production code never mutates it.
//
//nolint:grouper // tunable timeout, kept separate from the sentinel-error var block
var sweepDrainTimeout = 30 * time.Second

// drainSweep waits for the background `host sweep` goroutine to
// finish with a bounded timeout. Without the timeout a hung sweep
// subprocess would block process exit indefinitely. Logs a warn
// on timeout so operators can spot a stuck sweep.
func (h *handler) drainSweep() {
	doneCh := make(chan struct{})

	go func() {
		defer close(doneCh)

		h.sweepWG.Wait()
	}()

	select {
	case <-doneCh:
	case <-time.After(sweepDrainTimeout):
		slog.Warn("sweep drain timed out at shutdown")
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

// loadLocalConfig reads and parses the in-sandbox merged local
// kubeconfig at $CLAUDE_KUBECTX_LOCAL -- the first entry in the
// wrapper's merged $KUBECONFIG, owned by in-sandbox cluster tools
// and plain `kubectl config use-context`. Returns (nil, nil) when
// the var is unset (out-of-wrapper serve), so callers degrade to
// the external-only view. A read or parse failure surfaces as a
// non-nil error so the select dispatch can fall back to the
// external path rather than misclassify a context.
func loadLocalConfig() (*kubeConfig, error) {
	path := os.Getenv("CLAUDE_KUBECTX_LOCAL")
	if path == "" {
		return nil, nil //nolint:nilnil // unset var is a valid "no local file" state
	}

	return loadKubeconfig(path)
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
func loadGuestConfig() (*kubeConfig, error) {
	path := guestConfigPath()
	if path == "" {
		return nil, nil //nolint:nilnil // unset var is a valid "no guest config" state
	}

	cfg, err := loadKubeconfig(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil //nolint:nilnil // missing file is a valid "no guest config yet" state
	}

	return cfg, err
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
func localView() (current string, names []string) {
	seen := make(map[string]struct{})

	add := func(cfg *kubeConfig) {
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
// [writeFileAtomic]). An empty name clears the field. No-op when
// $CLAUDE_KUBECTX_LOCAL is unset.
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
// The write round-trips the file through [kubeConfig], so the local
// file is normalized to that modeled subset on every call:
// top-level preferences/extensions and per-context extensions are
// not preserved. Keeping cluster/user definitions out of local.yaml
// is what makes that round-trip lossless and safe -- the user's real
// guest config is never normalized through here.
func setLocalCurrentContext(name string) error {
	path := os.Getenv("CLAUDE_KUBECTX_LOCAL")
	if path == "" {
		return nil
	}

	cfg, err := loadKubeconfig(path)
	if errors.Is(err, fs.ErrNotExist) {
		cfg = &kubeConfig{APIVersion: "v1", Kind: "Config"}
	} else if err != nil {
		return err
	}

	cfg.CurrentContext = name

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrWriteKubeconfig, err)
	}

	err = os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrWriteKubeconfig, err)
	}

	return writeFileAtomic(path, data, 0o600)
}

// ListInput is the MCP input schema for the list tool.
type ListInput struct{}

// list relays the external contexts from `host list` (read from the
// admin kubeconfig) merged with the in-sandbox contexts defined in
// the local sources, tagged `(local)`. The local set is the union of
// local.yaml and, on the guest image, the guest's ~/.kube/config (see
// [localView]); a name defined in both is listed once. The single
// `(current)` marker is derived from local.yaml's current-context --
// the merged-view source of truth -- not from the admin kubeconfig's
// own current-context, which is meaningless in the merged view that
// `host list` cannot see.
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

	current, localNames := localView()

	return toolResult(mergeListOutput(string(stdout), localNames, current)), nil, nil
}

// mergeListOutput rebuilds the `host list` text into the merged
// view. External lines have their own ` (current)` suffix stripped
// (the admin kubeconfig's current-context is meaningless here), each
// local context is appended tagged `(local)`, and a single
// `(current)` marker is applied wherever a name matches current. On
// a name collision the local context wins: the external line is
// dropped so the name resolves to the local entry, matching
// client-go's first-file-wins merge. The surviving local line is
// tagged `(local, shadows external)` so the collision — which makes
// the external context unreachable for as long as the local name
// exists — stays visible instead of silently swallowing a context.
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

	// Route by union membership first: a name defined in local.yaml OR
	// the guest's ~/.kube/config takes the local path -- no host
	// shell-out, no SA. Its cluster/user/creds resolve from whichever
	// of those two merge entries defines it (local.yaml first-file-wins
	// on a collision). Only a name absent from both falls through to
	// the external SA-mint path below. The union check is load-bearing:
	// a guest cluster's apiserver is unreachable from the macOS host,
	// so minting an SA against it would fail -- it must route local.
	if _, isLocal := localContextNames()[input.Context]; isLocal {
		// The drain inside selectLocalCtx derives from
		// context.Background by design, breaking the request-ctx
		// chain on purpose; see the selectLocalCtx doc.
		return h.selectLocalCtx(input.Context) //nolint:contextcheck // see comment
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

	// The external creds live in the sidecar (second merged entry),
	// but current-context resolves first-file-wins from the local
	// file, so the selection only takes effect once written there.
	// Non-fatal: the SA already exists; a kubectl-side
	// `use-context` is the fallback.
	cerr := setLocalCurrentContext(result.Context)
	if cerr != nil {
		slog.WarnContext(ctx, "set local current-context",
			slog.String("context", result.Context),
			slog.Any("error", cerr),
		)
	}

	drainCleanups(prev)

	return toolResult(fmt.Sprintf(
		"Created ServiceAccount for context %q bound to %s.\nKubeconfig written to %s",
		result.Context, describeBinding(h.sa), result.Path,
	)), nil, nil
}

// drainCleanups runs the snapshotted prior cleanup closures (the SA
// release callbacks registered by an earlier select) with a bounded
// timeout, then returns. The timeout context derives from
// [context.Background], not the request ctx: the MCP SDK cancels the
// request ctx as soon as the tool call returns, which would abort
// the in-flight Delete* calls and re-leak the prior SA. A nil or
// empty slice is a no-op.
func drainCleanups(prev []func(context.Context)) {
	if len(prev) == 0 {
		return
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, fn := range prev {
		fn(drainCtx) //nolint:contextcheck // see doc comment
	}
}

// selectLocalCtx activates an in-sandbox context from the local
// sources: it sets current-context in local.yaml and idles the UDS
// token path. No `host select` shell-out and no ServiceAccount -- the
// context carries its own inline cluster-admin creds, which kubectl
// reads directly from whichever merge entry defines it: local.yaml
// for an in-sandbox tool's cluster, or the guest's ~/.kube/config
// (the middle merge entry) for a guest-local cluster. Only
// current-context is written, and only to local.yaml.
//
// Ordering is pinned so a partial failure never tears state:
//   - snapshot the prior cleanups but do NOT clear them yet;
//   - write current-context; on write error bail without touching
//     currentSA or the live cleanup list, so the prior selection
//     (its SA and release closure) stays intact;
//   - only on success clear currentSA (store nil) so the socket
//     goroutine stops minting tokens, register no new closure, and
//     drain the prior closures to release any prior external SA.
func (h *handler) selectLocalCtx(name string) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	prev := h.cleanupFuncs
	h.mu.Unlock()

	err := setLocalCurrentContext(name)
	if err != nil {
		// Prior selection untouched: currentSA and cleanupFuncs are
		// exactly as they were, so the previous context keeps working.
		return toolError(err), nil, nil
	}

	h.mu.Lock()
	h.cleanupFuncs = nil
	h.mu.Unlock()

	// Local creds are inline; the UDS token path must stay idle so a
	// stray exec-plugin call does not mint against a stale SA.
	h.currentSA.Store(nil)

	drainCleanups(prev)

	return toolResult(fmt.Sprintf(
		"Selected local context %q (cluster-admin, in-sandbox creds). "+
			"No ServiceAccount minted; this context shadows any external context of the same name.",
		name,
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
//
// h.instanceID and h.hostID flow through as --sa-instance-id and
// --sa-host-id so the SAs and bindings `host select` creates carry
// [instanceIDLabel] and [hostIDLabel]; empty values omit the flag
// entirely so the host select side preserves existing test
// invariants for standalone CLI use.
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

	if h.instanceID != "" {
		args = append(args, "--sa-instance-id", h.instanceID)
	}

	if h.hostID != "" {
		args = append(args, "--sa-host-id", h.hostID)
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

// writeFileAtomic writes data to path via a tmp + rename so a
// concurrent reader never observes a torn or zero-byte file.
// Parent dirs are not created here — callers ensure the dir exists
// (or use [writeFileSecure] when they want both behaviors). Mode
// is applied to the tmp file; rename preserves it.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"

	// Best-effort cleanup of any leftover tmp from a prior crash;
	// the WriteFile below would otherwise hit EEXIST on platforms
	// that surface it.
	_ = os.Remove(tmp) //nolint:errcheck // best-effort cleanup

	err := os.WriteFile(tmp, data, mode)
	if err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}

	err = os.Rename(tmp, path)
	if err != nil {
		_ = os.Remove(tmp) //nolint:errcheck // best-effort rollback
		return fmt.Errorf("rename: %w", err)
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
