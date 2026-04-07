package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
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
	ErrSessionDir      = errors.New("session directory")
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
type handler struct {
	envLookup       func(string) string
	runHost         runHostFunc
	cleanupFuncs    []func(context.Context)
	kubeconfigPath  string
	outputPath      string
	allowedAPIHosts []string
	sa              saConfig
	mu              sync.Mutex
	pid             int
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

// stateHomeDir returns the parent directory used for per-`serve`
// kubeconfig files. Honors $XDG_STATE_HOME, falling back to
// ~/.local/state when unset.
func stateHomeDir() string {
	state := os.Getenv("XDG_STATE_HOME")
	if state != "" {
		return filepath.Join(state, "mcp-kubectx")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".local", "state", "mcp-kubectx")
	}

	return filepath.Join(home, ".local", "state", "mcp-kubectx")
}

// resolveOutputPath returns the path where the scoped kubeconfig is
// written. When no flag is set, the path is keyed by the serve
// process pid and the host/guest environment so concurrent serve
// instances (potentially across the Lima boundary, which shares
// $HOME) never overwrite each other.
func (h *handler) resolveOutputPath() string {
	if h.outputPath != "" {
		return h.outputPath
	}

	env := "host"
	if h.isGuest() {
		env = "guest"
	}

	pid := h.pid
	if pid <= 0 {
		pid = os.Getpid()
	}

	return filepath.Join(
		stateHomeDir(),
		fmt.Sprintf("kubeconfig.%s.%s.yaml", strconv.Itoa(pid), env),
	)
}

// sessionDir creates the parent directory for the per-`serve`
// kubeconfig file and returns a cleanup function. Cleanup runs
// every registered K8s resource cleanup (with a 30-second timeout
// from [context.Background]) and then removes the kubeconfig file.
//
// Each serve owns one file inside the shared $XDG_STATE_HOME/mcp-kubectx
// parent. Concurrent serve processes share that directory but never
// share filenames -- the path is keyed by pid and host/guest env.
func (h *handler) sessionDir() (func(), error) {
	runResourceCleanup := func() {
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

	outPath := h.resolveOutputPath()

	if h.outputPath == "" {
		err := os.MkdirAll(filepath.Dir(outPath), 0o700)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrSessionDir, err)
		}
	}

	return func() {
		runResourceCleanup()

		err := os.Remove(outPath)
		if err != nil && !os.IsNotExist(err) {
			slog.Warn("cleanup kubeconfig",
				slog.String("path", outPath),
				slog.Any("error", err),
			)
		}
	}, nil
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

	args := h.selectArgs(input.Context)

	stdout, err := h.runHost(ctx, "select", args)
	if err != nil {
		h.restoreCleanups(prev)
		return toolError(err), nil, nil
	}

	var result HostSelectResult

	err = json.Unmarshal(stdout, &result)
	if err != nil {
		h.restoreCleanups(prev)
		return toolError(fmt.Errorf("%w: %w", ErrParseHostResult, err)), nil, nil
	}

	releaseFn := h.releaseClosure(result)

	h.registerCleanup(releaseFn)

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

// selectArgs builds the argv passed to `host select`. The serve
// process always supplies the resolved out-path so the host
// subcommand never has to make path policy decisions. Each
// allowed apiserver host is forwarded as a repeated
// `--allow-apiserver-host` flag; an empty list yields no flags
// and lets `host select` accept any apiserver.
func (h *handler) selectArgs(contextName string) []string {
	args := []string{contextName}
	args = append(args, h.kubeconfigArgs()...)
	args = append(args,
		"--out-path", h.resolveOutputPath(),
		fmt.Sprintf("--for-guest=%t", h.isGuest()),
		"--sa-role-name", h.sa.role,
		"--sa-role-kind", h.sa.roleKind,
		"--sa-namespace", h.sa.namespace,
		"--sa-expiration", strconv.Itoa(h.sa.expiration),
		fmt.Sprintf("--sa-cluster-scoped=%t", h.sa.clusterScoped),
	)

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
