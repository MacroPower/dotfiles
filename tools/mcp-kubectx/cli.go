package main

import (
	"errors"
	"fmt"
)

// ErrUnknownSubcommand is returned when the dispatcher does not
// recognize the requested subcommand.
var ErrUnknownSubcommand = errors.New("unknown subcommand")

// dispatch routes [os.Args] to the requested subcommand. The first
// positional argument selects the subcommand: "serve" runs the MCP
// stdio server (also the default when no subcommand is given, for
// backwards compatibility), and "host" delegates to one of the
// host-only one-shot subcommands ("list", "select", "token",
// "release"). Each subcommand parses its own flag set from the
// remaining arguments.
func dispatch(args []string) error {
	if len(args) < 2 {
		return runServe(nil)
	}

	switch args[1] {
	case "serve":
		return runServe(args[2:])
	case "host":
		return dispatchHost(args[2:])
	default:
		// Any leading flag (e.g. -kubeconfig=...) means caller
		// invoked the binary the old, single-mode way: treat the
		// whole arg slice as serve flags.
		if args[1] != "" && args[1][0] == '-' {
			return runServe(args[1:])
		}

		return fmt.Errorf("%w: %s", ErrUnknownSubcommand, args[1])
	}
}

// dispatchHost routes a `host <sub>` invocation to the right
// one-shot handler. The host subcommands never run the MCP server
// and never shell out to `workmux host-exec`; they always reach
// Kubernetes directly.
func dispatchHost(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: host (missing subcommand: list|select|token|release)", ErrUnknownSubcommand)
	}

	switch args[0] {
	case "list":
		return runHostList(args[1:])
	case "select":
		return runHostSelect(args[1:])
	case "token":
		return runHostToken(args[1:])
	case "release":
		return runHostRelease(args[1:])
	default:
		return fmt.Errorf("%w: host %s", ErrUnknownSubcommand, args[0])
	}
}
