package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "0.1.0"

func main() {
	err := dispatch(os.Args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

// runServe runs the MCP stdio server. It owns the long-lived
// per-process state (kubeconfig path, SA cleanup registry) and
// shells out to `host *` subcommands -- directly when on the
// macOS host, via `workmux host-exec` when in a Lima guest -- to
// touch the cluster.
func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)

	kubeconfig := fs.String(
		"kubeconfig", "",
		"path to host kubeconfig (default: $KUBECONFIG or ~/.kube/config)",
	)
	output := fs.String(
		"output", "",
		"path where scoped kubeconfig is written (default: <host's $XDG_STATE_HOME>/mcp-kubectx/kubeconfig.<pid>.<env>.yaml). "+
			"In a Lima guest, an explicit path must be host-resolvable; shutdown cleanup uses a local os.Remove on this "+
			"branch (not host cleanup), so the path must also be writable from the guest -- typically only true under a "+
			"writable bind mount. Default operation does not need a writable mount.",
	)
	saRole := fs.String(
		"sa-role-name", "",
		"name of the Role or ClusterRole to bind the ServiceAccount to (required)",
	)
	saRoleKind := fs.String(
		"sa-role-kind", roleKindClusterRole,
		"kind of role to bind: Role or ClusterRole",
	)
	saClusterScoped := fs.Bool(
		"sa-cluster-scoped", false,
		"create a ClusterRoleBinding instead of a RoleBinding (requires ClusterRole kind)",
	)
	saNamespace := fs.String(
		"sa-namespace", "",
		"namespace for the ServiceAccount (default: context namespace or \"default\")",
	)
	saExpiration := fs.Int(
		"sa-expiration", 0,
		"ServiceAccount token lifetime in seconds (default: 3600, max: 86400)",
	)
	logFile := fs.String("log-file", "", "path to JSON log file (append)")

	var allowedAPIHosts stringSliceFlag

	fs.Var(
		&allowedAPIHosts,
		allowAPIServerHostFlag,
		"hostname permitted as cluster.server when selecting a context "+
			"(repeatable; empty = allow any)",
	)

	err := fs.Parse(args)
	if err != nil {
		return fmt.Errorf("parse serve flags: %w", err)
	}

	logger, logCloser, err := openLogger(*logFile)
	if err != nil {
		return err
	}
	defer logCloser()

	slog.SetDefault(logger)

	sa := saConfig{
		role:          *saRole,
		roleKind:      *saRoleKind,
		clusterScoped: *saClusterScoped,
		namespace:     *saNamespace,
		expiration:    *saExpiration,
	}

	err = sa.validate()
	if err != nil {
		return fmt.Errorf("invalid service account config: %w", err)
	}

	h := &handler{
		kubeconfigPath:  *kubeconfig,
		outputPath:      *output,
		lastOutputPath:  *output,
		allowedAPIHosts: allowedAPIHosts,
		pid:             os.Getpid(),
		sa:              sa,
		envLookup:       os.Getenv,
	}

	h.runHost = h.defaultRunHost

	cleanup, err := h.sessionDir()
	if err != nil {
		return fmt.Errorf("session directory: %w", err)
	}
	defer cleanup()

	srv := mcp.NewServer(
		&mcp.Implementation{Name: "mcp-kubectx", Version: version},
		nil,
	)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list",
		Description: "List available Kubernetes contexts from the host kubeconfig.",
	}, h.list)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "select",
		Description: "Select a Kubernetes context and write a scoped kubeconfig to the configured output path.",
	}, h.selectCtx)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	err = srv.Run(ctx, &mcp.StdioTransport{})

	cancel()

	if err != nil {
		return fmt.Errorf("server: %w", err)
	}

	return nil
}

// openLogger creates a JSON [*slog.Logger] writing to the named file.
// Returns a discard logger and no-op closer when path is empty.
func openLogger(path string) (*slog.Logger, func(), error) {
	if path == "" {
		return slog.New(slog.DiscardHandler), func() {}, nil
	}

	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return nil, nil, fmt.Errorf("creating log directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
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
