package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/execplugin"
)

// Sentinel errors for the exec-plugin subcommand's flag handling.
// Connection- and protocol-level failures surface as the
// [execplugin] package's sentinels.
var (
	ErrParseExecPluginFlags    = errors.New("parse exec-plugin flags")
	ErrExecPluginMissingSocket = errors.New("--socket is required")
)

// runExecPluginClient is the body of the `exec-plugin` subcommand.
// It is a tiny UDS client: dial the per-`serve` Unix domain socket
// via [execplugin.Fetch], and relay the returned bytes (an
// [execplugin.Credential] JSON document) to stdout. The
// recursion-guard property of the `host *` subcommands carries over
// verbatim: this entry point never constructs a [*handler] and so
// cannot reach [*handler.runHost].
//
// The shim deliberately ignores KUBERNETES_EXEC_INFO -- the server
// already has the kubeconfig path and context name in its in-memory
// [currentSA] descriptor; re-deriving them from kubectl's per-call
// hint would invite drift.
func runExecPluginClient(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("exec-plugin", flag.ContinueOnError)

	socketPath := fs.String(
		"socket", "",
		"path to the mcp-kubectx serve Unix domain socket (required)",
	)

	err := fs.Parse(args)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrParseExecPluginFlags, err)
	}

	if *socketPath == "" {
		return ErrExecPluginMissingSocket
	}

	data, err := execplugin.Fetch(ctx, *socketPath)
	if err != nil {
		return err //nolint:wrapcheck // Fetch already wraps with its sentinel errors
	}

	_, err = hostStdout.Write(data)
	if err != nil {
		return fmt.Errorf("write credential: %w", err)
	}

	return nil
}
