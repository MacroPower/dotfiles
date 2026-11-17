package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/identity"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/socket"
)

const version = "0.1.0"

// defaultSocketSlots matches the size of the literal allowUnixSockets
// list emitted by the Nix bundle in home/claude.nix. Both must agree
// or the rendered Claude Code sandbox allowlist will not cover every
// slot serve might bind.
const defaultSocketSlots = 16

// ErrInvalidSocketSlots is returned when --socket-slots is parsed
// successfully but holds a non-positive value. [flag.Int] does not
// validate ranges, so the check happens after Parse.
var ErrInvalidSocketSlots = errors.New("--socket-slots must be >= 1")

func main() {
	os.Exit(run())
}

// run is the testable body of main. Splitting it out lets `os.Exit`
// happen at the very top frame so deferred `cancel()` and any other
// cleanup actually run.
func run() int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	err := dispatch(ctx, os.Args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}

	return 0
}

// runServe runs the MCP stdio server. It owns the long-lived
// per-process state, such as the path of its scoped kubeconfig and
// the SA cleanup registry. It shells out to `host *` subcommands to
// touch the cluster. It runs them directly on the macOS host and
// through `workmux host-exec` in a Lima guest. The `host`
// subcommands take the role, the apiserver allowlist, and the host
// kubeconfig from the host, so serve has no flag for them. ctx
// carries the parent process's signal cancellation, and runServe
// itself does not call [signal.NotifyContext].
func runServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)

	output := fs.String(
		"output",
		"",
		"path of the scoped kubeconfig that serve writes after each select of an external context "+
			"(default: $XDG_STATE_HOME/mcp-kubectx-run/kubeconfig.<pid>.<env>.yaml, where <env> is host, "+
			"or guest in a Lima guest). serve resolves the path and writes the file on the machine that runs serve, "+
			"and it removes the file at shutdown.",
	)
	logFile := fs.String(
		"log-file", "",
		"path to JSON log file (append). serve fails to start when the last element of the path is a symlink",
	)
	socketSlots := fs.Int(
		"socket-slots",
		defaultSocketSlots,
		"number of UDS slot paths to probe at startup; serve binds the first free slot. "+
			"Each slot maps to one literal entry in the Claude Code sandbox allowlist. "+
			"Must be >= 1. Concurrent serves on the same host occupy distinct slots; "+
			"startup fails when every slot is held by a live peer.",
	)

	err := fs.Parse(args)
	if err != nil {
		return fmt.Errorf("parse serve flags: %w", err)
	}

	if *socketSlots < 1 {
		return fmt.Errorf("%w: got %d", ErrInvalidSocketSlots, *socketSlots)
	}

	logger, logCloser, err := openLogger(*logFile)
	if err != nil {
		return err
	}
	defer logCloser()

	slog.SetDefault(logger)

	instanceID, err := identity.New()
	if err != nil {
		return fmt.Errorf("generate instance id: %w", err)
	}

	// The host id is per-env (host.key vs guest.key), because the
	// sweep of each env can vouch only for the live serves of that
	// env. Each id file lies in the run dir of the machine that runs
	// serve, which workmux never mounts into a Lima guest. See
	// [identity.LoadOrCreateHost].
	guest := os.Getenv(guestEnvVar) == "1"

	hostID, err := identity.LoadOrCreateHost(guest)
	if err != nil {
		return err //nolint:wrapcheck // LoadOrCreateHost already wraps identity.ErrLoadHost
	}

	h := &handler{
		outputPath:     *output,
		lastOutputPath: *output,
		pid:            os.Getpid(),
		socketSlots:    *socketSlots,
		envLookup:      os.Getenv,
		instanceID:     instanceID,
		hostID:         hostID,
	}

	h.runHost = h.defaultRunHost

	// socket.Acquire walks slot 0..socketSlots-1 and binds the
	// first free one. Closing the listener unlinks the per-slot
	// sidecar and the socket inode, and the socketShutdown step of
	// sessionDir closes it. socket.Listen writes the instanceID into
	// the sidecar before Acquire returns. That write must happen
	// before socket.DiscoverLive runs below so the serve's own id
	// appears in the live set.
	sockPath, listener, err := socket.Acquire(ctx, guest, h.socketSlots, h.instanceID)
	if err != nil {
		return fmt.Errorf("bind serve socket: %w", err)
	}

	h.mu.Lock()
	h.socketPath = sockPath
	h.socketListener = listener
	h.mu.Unlock()

	//nolint:contextcheck // SA-release drain inside cleanup uses context.Background by design; see comment in sessionDir
	defer h.sessionDir()()

	// Discover live instances BEFORE starting the accept loop so
	// the sweep's classifier reads a coherent snapshot. The serve's
	// own slot is included because the sidecar write above completed
	// successfully and the listener is already in the LISTEN state.
	// A serve that starts after DiscoverLive probes its slot is
	// missing from the snapshot. So the sweep keeps each resource
	// that the apiserver created, and each provision record that
	// `host select` wrote, after liveAsOf. liveAsOf precedes the
	// probes, so each such serve starts after liveAsOf.
	liveAsOf := time.Now()
	liveSet := socket.DiscoverLive(ctx, h.socketSlots, guest)

	h.sweepWG.Go(func() { h.runSweep(ctx, liveSet, liveAsOf) })

	socket.Start(ctx, listener, &h.socketWG, h.handleSocketConn)

	srv := mcp.NewServer(
		&mcp.Implementation{Name: "mcp-kubectx", Version: version},
		nil,
	)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list",
		Description: "List Kubernetes contexts from the host kubeconfig.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, h.list)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "select",
		Description: "Select a Kubernetes context; writes a scoped kubeconfig.",
	}, h.selectCtx)

	err = srv.Run(ctx, &mcp.StdioTransport{})
	if err != nil {
		return fmt.Errorf("server: %w", err)
	}

	return nil
}

// openLogger creates a JSON [*slog.Logger] writing to the named file.
// Returns a discard logger and no-op closer when path is empty.
//
// openLogger creates each missing parent directory of path with mode
// 0700. home/claude.nix puts the log in mcp-kubectx-run, and the
// sockets in that directory need mode 0700 on it.
// It opens path with O_NOFOLLOW, so it fails when the last element
// of path is a symlink. A process that can change the entries of the
// log directory then cannot send the log lines through a symlink to
// another file. O_NOFOLLOW does not stop a hard link at path, so the
// log belongs in a directory that only the user's own processes
// change.
func openLogger(path string) (*slog.Logger, func(), error) {
	if path == "" {
		return slog.New(slog.DiscardHandler), func() {}, nil
	}

	err := os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		return nil, nil, fmt.Errorf("creating log directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("opening %s: %w", path, err)
	}

	logger := slog.New(slog.NewJSONHandler(f, nil))

	return logger, func() {
		err := f.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "closing log file: %v\n", err)
		}
	}, nil
}
