package execplugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// Per-call deadlines for the shim side of the UDS protocol.
// [ReadDeadline] is deliberately longer than the serve side's
// per-connection deadline so the server closes first cleanly.
const (
	ConnectDeadline = 5 * time.Second
	ReadDeadline    = 30 * time.Second
)

// Sentinel errors for the kubectl exec credential shim. Wrapping
// these lets kubectl-visible failure messages stay clean; tests
// match them with [errors.Is] without depending on
// platform-specific connect or read error text.
var (
	ErrConnect         = errors.New("connect to mcp-kubectx serve socket")
	ErrEmptyCredential = errors.New(
		"serve returned no credential bytes (no context selected, or the token mint failed; check serve logs)",
	)
	ErrMalformedCredential = errors.New(
		"serve returned malformed credential JSON (connection truncated?)",
	)
)

// Fetch dials the per-`serve` Unix domain socket and returns the
// bytes the server writes (a [Credential] JSON document), validated
// but otherwise unparsed so the caller can relay them verbatim.
//
// A clean EOF with zero bytes means serve declined to answer (no
// context selected, or its token mint failed); that surfaces as
// [ErrEmptyCredential] so the caller exits non-zero
// deterministically instead of letting kubectl misinterpret empty
// stdout. Partial bytes from a server-side deadline closing the
// connection mid-write surface as [ErrMalformedCredential] rather
// than relaying truncated JSON.
func Fetch(ctx context.Context, socketPath string) ([]byte, error) {
	dialer := net.Dialer{Timeout: ConnectDeadline}

	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConnect, err)
	}

	defer conn.Close() //nolint:errcheck // response already consumed; close error has nowhere to go

	//nolint:errcheck // best-effort; io.ReadAll still bounded by kernel default
	_ = conn.SetReadDeadline(time.Now().Add(ReadDeadline))

	data, err := io.ReadAll(conn)
	if err != nil {
		return nil, fmt.Errorf("read credential: %w", err)
	}

	if len(data) == 0 {
		return nil, ErrEmptyCredential
	}

	if !json.Valid(data) {
		return nil, ErrMalformedCredential
	}

	return data, nil
}
