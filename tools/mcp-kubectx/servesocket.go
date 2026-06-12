package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/socket"
)

// currentSA holds the descriptor of the ServiceAccount currently
// owned by `serve`. It is loaded atomically by the socket handler
// goroutine when a kubectl exec-plugin connection arrives, and
// stored atomically by [*handler.selectCtx] after a successful
// `host select`. Nil means no SA is selected yet (or the previous
// select failed before storing).
type currentSA struct {
	Kubeconfig string
	Context    string
	SAName     string
	Namespace  string
	Expiration int
}

// handleSocketConn writes the bytes of an [execplugin.Credential]
// JSON document for the currently-selected SA back to the kubectl
// shim. When no SA is selected (currentSA is nil) or the token mint
// fails, the handler logs a structured warning and closes the
// connection without writing any bytes; the shim's zero-byte check
// turns either case into a deterministic non-zero exit kubectl
// surfaces clearly. The wire protocol cannot distinguish the two,
// so [execplugin.ErrEmptyCredential]'s text names both and points
// at the serve logs.
func (h *handler) handleSocketConn(ctx context.Context, conn net.Conn) {
	defer conn.Close() //nolint:errcheck // already responded; close error has nowhere to go

	//nolint:errcheck // best-effort; kernel default applies on failure
	_ = conn.SetDeadline(time.Now().Add(socket.ConnDeadline))

	saPtr := h.currentSA.Load()
	if saPtr == nil {
		slog.WarnContext(ctx, "exec-plugin request before select",
			slog.String("remote", "uds"),
		)

		return
	}

	sa := *saPtr

	args := []string{
		"--kubeconfig", sa.Kubeconfig,
		"--context", sa.Context,
		"--sa", sa.SAName,
		"--namespace", sa.Namespace,
		fmt.Sprintf("--sa-expiration=%d", sa.Expiration),
	}

	stdout, err := h.runHost(ctx, "token", args)
	if err != nil {
		slog.WarnContext(ctx, "exec-plugin token mint",
			slog.String("sa", sa.SAName),
			slog.String("namespace", sa.Namespace),
			slog.Any("error", err),
		)

		return
	}

	if len(stdout) == 0 {
		slog.WarnContext(ctx, "exec-plugin token mint returned empty stdout",
			slog.String("sa", sa.SAName),
			slog.String("namespace", sa.Namespace),
		)

		return
	}

	_, err = conn.Write(stdout)
	if err != nil {
		slog.WarnContext(ctx, "exec-plugin write",
			slog.String("sa", sa.SAName),
			slog.Any("error", err),
		)
	}
}

// socketShutdown closes the listener and blocks until every
// in-flight connection handler returns. It is called from
// [*handler.sessionDir]'s cleanup so that no handler is mid-token
// mint when the next steps unlink the kubeconfig and the socket
// file.
func (h *handler) socketShutdown() {
	h.mu.Lock()
	l := h.socketListener
	h.socketListener = nil
	h.mu.Unlock()

	if l != nil {
		_ = l.Close() //nolint:errcheck // idempotent; ctx-cancel goroutine may have already closed
	}

	h.socketWG.Wait()
}
