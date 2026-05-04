package main

import (
	"context"
	"errors"
	"fmt"
)

// ErrUnknownSubcommand is returned when the dispatcher does not
// recognize the requested subcommand.
var ErrUnknownSubcommand = errors.New("unknown subcommand")

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
)

// dispatch routes [os.Args] to the requested subcommand. The first
// positional argument selects the subcommand: "serve" runs the MCP
// stdio server (also the default when no subcommand is given, for
// backwards compatibility), "host" delegates to one of the
// host-only one-shot subcommands ("list", "select", "token",
// "release"), and "exec-plugin" is the kubectl exec credential
// shim that talks to a running serve over a Unix domain socket.
//
// "exec-plugin" joins "host *" as a non-`*handler` subcommand: it
// has no path to [*handler.runHost] and therefore cannot wrap
// itself with `workmux host-exec`, preserving the structural
// recursion guard documented in [doc.go].
func dispatch(ctx context.Context, args []string) error {
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
		// Any leading flag (e.g. -kubeconfig=...) means caller
		// invoked the binary the old, single-mode way: treat the
		// whole arg slice as serve flags.
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
func dispatchHost(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf(
			"%w: host (missing subcommand: list|select|token|release)",
			ErrUnknownSubcommand,
		)
	}

	switch args[0] {
	case subList:
		return runHostList(args[1:])
	case subSelect:
		return runHostSelect(ctx, args[1:])
	case subToken:
		return runHostToken(ctx, args[1:])
	case subRelease:
		return runHostRelease(ctx, args[1:])
	default:
		return fmt.Errorf("%w: host %s", ErrUnknownSubcommand, args[0])
	}
}
