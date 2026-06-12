package socket

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"time"
)

// ConnHandler handles one accepted connection. The handler owns the
// connection and is responsible for closing it.
type ConnHandler func(ctx context.Context, conn net.Conn)

// Start launches [Serve] on its own goroutine with the accept loop
// itself registered on wg. Holding a wg token for the loop's entire
// lifetime makes the per-connection wg.Add inside Serve safe: the
// counter cannot reach zero (so a concurrent shutdown Wait cannot
// return) until the loop has exited, after which no further Add can
// occur. Without the loop token, an Accept racing the shutdown's
// Close could Add from a zero counter concurrently with Wait -- the
// one ordering [sync.WaitGroup] forbids -- letting the shutdown
// unlink session files under an in-flight token mint.
func Start(ctx context.Context, l net.Listener, wg *sync.WaitGroup, handle ConnHandler) {
	wg.Go(func() {
		Serve(ctx, l, wg, handle)
	})
}

// Serve runs the accept loop for the per-`serve` Unix domain socket.
// It returns when ctx is canceled (the goroutine that watches ctx
// closes the listener, which surfaces as [net.ErrClosed] in Accept).
// Each accepted connection is handled in its own goroutine tracked
// by wg so the shutdown path can drain in-flight handlers before the
// higher-level cleanup unlinks the socket file and the kubeconfig.
// Callers that drain wg concurrently must enter through [Start] so
// the loop holds its own wg token before the first Accept.
func Serve(ctx context.Context, l net.Listener, wg *sync.WaitGroup, handle ConnHandler) {
	go func() {
		<-ctx.Done()

		_ = l.Close() //nolint:errcheck // best-effort listener close on ctx cancel
	}()

	const acceptBackoff = 100 * time.Millisecond

	for {
		conn, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}

			slog.WarnContext(ctx, "serve socket accept",
				slog.Any("error", err),
			)

			// Tiny backoff on persistent errors (e.g. EMFILE)
			// so the loop does not spin hot. Bail early if ctx
			// has been canceled in the meantime.
			select {
			case <-ctx.Done():
				return
			case <-time.After(acceptBackoff):
			}

			continue
		}

		wg.Add(1)

		go func(c net.Conn) {
			defer wg.Done()

			handle(ctx, c)
		}(conn)
	}
}
