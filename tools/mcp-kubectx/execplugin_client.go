package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"time"
)

// Sentinel errors for the kubectl exec credential shim. Wrapping
// these lets kubectl-visible failure messages stay clean; tests
// match them with [errors.Is] without depending on platform-specific
// connect or read error text.
var (
	ErrConnectExecPlugin = errors.New("connect to mcp-kubectx serve socket")
	ErrEmptyCredential   = errors.New(
		"serve returned no credential bytes (no context selected, or the token mint failed; check serve logs)",
	)
	ErrMalformedCredential = errors.New(
		"serve returned malformed credential JSON (connection truncated?)",
	)
	ErrParseExecPluginFlags    = errors.New("parse exec-plugin flags")
	ErrExecPluginMissingSocket = errors.New("--socket is required")
)

// runExecPluginClient is the body of the `exec-plugin` subcommand.
// It is a tiny UDS client: dial the per-`serve` Unix domain socket,
// read the bytes the server writes (an [ExecCredential] JSON
// document), and relay them to stdout. The recursion-guard property
// of the `host *` subcommands carries over verbatim: this entry
// point never constructs a [*handler] and so cannot reach
// [*handler.runHost].
//
// The shim deliberately ignores [KUBERNETES_EXEC_INFO] -- the
// server already has the kubeconfig path and context name in its
// in-memory [currentSA] descriptor; re-deriving them from kubectl's
// per-call hint would invite drift.
func runExecPluginClient(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("exec-plugin", flag.ContinueOnError)

	socket := fs.String(
		"socket", "",
		"path to the mcp-kubectx serve Unix domain socket (required)",
	)

	err := fs.Parse(args)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrParseExecPluginFlags, err)
	}

	if *socket == "" {
		return ErrExecPluginMissingSocket
	}

	dialer := net.Dialer{Timeout: execPluginConnectDeadline}

	conn, err := dialer.DialContext(ctx, "unix", *socket)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrConnectExecPlugin, err)
	}

	defer conn.Close() //nolint:errcheck // shim already exited stdout-side; close error has nowhere to go

	//nolint:errcheck // best-effort; io.ReadAll still bounded by kernel default
	_ = conn.SetReadDeadline(time.Now().Add(execPluginReadDeadline))

	data, err := io.ReadAll(conn)
	if err != nil {
		return fmt.Errorf("read credential: %w", err)
	}

	// A clean EOF with zero bytes means serve declined to answer
	// (no currentSA, or its token mint failed). Returning success
	// here would let kubectl interpret an empty stdout as a
	// malformed ExecCredential and fail with a confusing error;
	// surface the non-zero exit deterministically.
	if len(data) == 0 {
		return ErrEmptyCredential
	}

	// Validate before relaying: a server-side deadline can close
	// the connection mid-write, and a clean EOF after partial bytes
	// would otherwise exit 0 with truncated JSON on stdout.
	if !json.Valid(data) {
		return ErrMalformedCredential
	}

	_, err = hostStdout.Write(data)
	if err != nil {
		return fmt.Errorf("write credential: %w", err)
	}

	return nil
}
