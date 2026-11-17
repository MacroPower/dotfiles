package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// Sentinel errors of the dispatcher.
var (
	// ErrUnknownSubcommand is returned when the dispatcher does not
	// recognize the requested subcommand.
	ErrUnknownSubcommand = errors.New("unknown subcommand")
	// ErrRelativeLedgerDir is returned when the directory of the
	// provision records does not resolve to an absolute path.
	ErrRelativeLedgerDir = errors.New("provision record directory is not absolute")
)

// hostCommand is the name of the link to the binary that runs only
// the `host` subcommands. package.nix installs the link beside the
// binary. A Lima guest reaches the host through `workmux host-exec
// mcp-kubectx-host`, and home/claude.nix allows only that command
// name. workmux checks the command name and passes the argv of the
// guest unchanged. So [dispatch] keeps every other subcommand, such
// as `serve` with its --output and --log-file paths, out of reach of
// the guest.
const hostCommand = "mcp-kubectx-host"

// Subcommand names. Centralized so the dispatch table and any error
// message that mentions them cannot drift.
const (
	subServe      = "serve"
	subHost       = "host"
	subExecPlugin = "exec-plugin"
	subList       = "list"
	subSelect     = "select"
	subToken      = "token"
	subRelease    = "release"
	subSweep      = "sweep"
)

// dispatch routes [os.Args] to the requested subcommand. The first
// positional argument selects the subcommand: "serve" runs the MCP
// stdio server (also the default when no subcommand is given, for
// backwards compatibility), "host" delegates to one of the
// host-only one-shot subcommands ("list", "select", "token",
// "release", "sweep"), and "exec-plugin" is the kubectl exec
// credential shim that talks to a running serve over a Unix
// domain socket.
//
// "exec-plugin" joins "host *" as a non-`*handler` subcommand. It
// has no path to [*handler.runHost], so it cannot wrap itself with
// `workmux host-exec`. The structural recursion guard that [doc.go]
// describes rests on that property.
//
// When the base name of args[0] is [hostCommand], dispatch passes
// the rest of args to [dispatchHost], with no `host` word in front.
// The `host` subcommands are then the only ones that the call
// reaches, and every other argv fails with [ErrUnknownSubcommand].
func dispatch(ctx context.Context, args []string) error {
	if len(args) > 0 && filepath.Base(args[0]) == hostCommand {
		return dispatchHost(ctx, args[1:])
	}

	if len(args) < 2 {
		return runServe(ctx, nil)
	}

	switch args[1] {
	case subServe:
		return runServe(ctx, args[2:])
	case subHost:
		return dispatchHost(ctx, args[2:])
	case subExecPlugin:
		return runExecPluginClient(ctx, args[2:])
	default:
		// A leading flag, such as -log-file=..., means that the
		// caller invoked the binary the old, single-mode way. So
		// dispatch passes the whole arg slice to serve as flags.
		if args[1] != "" && args[1][0] == '-' {
			return runServe(ctx, args[1:])
		}

		return fmt.Errorf("%w: %s", ErrUnknownSubcommand, args[1])
	}
}

// dispatchHost routes a `host <sub>` invocation to the right
// one-shot handler. The host subcommands never run the MCP server
// and never shell out to `workmux host-exec`; they touch the host
// filesystem and Kubernetes API directly.
//
// It points the default logger at stderr as one JSON record per
// line before it runs the subcommand. [runHostCommand] reads those
// lines and adds each record to the log of serve.
//
// dispatchHost resolves the inputs that the argv must not set before
// it runs the subcommand. It resolves the host kubeconfig with
// [resolveHostKubeconfigPath], and for `host select` and `host
// token` it loads the host config with [loadHostConfig]. A Lima
// guest runs the `host` subcommands through `workmux host-exec` with
// an argv of its own choice. So it reaches only the role, the
// apiservers, and the kubeconfig that the host side picks. The
// ledger directory must be absolute. A relative path resolves in the
// working directory, which is a worktree that the guest can write
// under `workmux host-exec`. When dispatchHost cannot resolve the
// ledger directory or the host kubeconfig, it runs no subcommand.
// For `host release`, it logs the error as a warning and returns
// nil, because [runHostRelease] exits 0 on every failure.
func dispatchHost(ctx context.Context, args []string) error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	if len(args) == 0 {
		return fmt.Errorf(
			"%w: host (missing subcommand: list|select|token|release|sweep)",
			ErrUnknownSubcommand,
		)
	}

	switch args[0] {
	case subList, subSelect, subToken, subRelease, subSweep:
	default:
		return fmt.Errorf("%w: host %s", ErrUnknownSubcommand, args[0])
	}

	if dir := hostLedgerDir(); !filepath.IsAbs(dir) {
		return hostSetupErr(ctx, args[0], fmt.Errorf("%w: %q", ErrRelativeLedgerDir, dir))
	}

	kubeconfigPath, err := resolveHostKubeconfigPath()
	if err != nil {
		return hostSetupErr(ctx, args[0], err)
	}

	switch args[0] {
	case subList:
		return runHostList(kubeconfigPath, args[1:])
	case subRelease:
		return runHostRelease(ctx, kubeconfigPath, args[1:])
	case subSweep:
		return runHostSweep(ctx, kubeconfigPath, args[1:])
	}

	cfg, err := loadHostConfig()
	if err != nil {
		return err
	}

	if args[0] == subSelect {
		return runHostSelect(ctx, kubeconfigPath, cfg, args[1:])
	}

	return runHostToken(ctx, kubeconfigPath, cfg, args[1:])
}

// hostSetupErr returns err, which [dispatchHost] hit before it ran
// the subcommand sub. For `host release`, it logs err as a warning
// and returns nil instead, so that `host release` exits 0 on every
// failure, as [runHostRelease] does.
func hostSetupErr(ctx context.Context, sub string, err error) error {
	if sub != subRelease {
		return err
	}

	slog.WarnContext(ctx, "host release skipped", slog.Any("error", err))

	return nil
}
