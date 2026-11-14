package httpbody

import (
	"context"
	"io"
	"log/slog"
)

// Close closes body. When the close fails, Close writes the error to the
// debug log with u, which names the source of the body, such as its URL.
func Close(ctx context.Context, body io.Closer, u string) {
	err := body.Close()
	if err != nil {
		slog.DebugContext(ctx, "closing response body",
			slog.String("url", u),
			slog.Any("error", err),
		)
	}
}
