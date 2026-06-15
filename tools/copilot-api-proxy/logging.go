package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// newLogger builds the proxy's logger from configuration.
//
// Destination: COPILOT_PROXY_LOG_FILE when set (JSON, append-created),
// otherwise stderr (text) when stderrSafe is true, otherwise discarded. The
// run subcommand passes stderrSafe=false because claude owns the terminal, so
// without a log file run stays silent and never corrupts the TUI; serve and
// login pass true and log to stderr. The returned closer flushes the file (if
// any) and should be deferred.
//
// Level: COPILOT_PROXY_LOG_LEVEL (debug|info|warn|error, default info).
// Per-request and token-refresh detail is emitted at debug, so info keeps a
// readable one-line-per-request summary and debug (ideally paired with a log
// file) is the lever when diagnosing.
func newLogger(cfg Config, stderrSafe bool) (*slog.Logger, func(), error) {
	opts := &slog.HandlerOptions{Level: parseLevel(cfg.LogLevel)}

	if cfg.LogFile != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.LogFile), 0o755); err != nil {
			return nil, nil, fmt.Errorf("create log directory: %w", err)
		}
		f, err := os.OpenFile(cfg.LogFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
		if err != nil {
			return nil, nil, fmt.Errorf("open log file %s: %w", cfg.LogFile, err)
		}
		return slog.New(slog.NewJSONHandler(f, opts)), func() { _ = f.Close() }, nil
	}

	if !stderrSafe {
		return slog.New(slog.DiscardHandler), func() {}, nil
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts)), func() {}, nil
}

// parseLevel maps a level name to a [slog.Level], defaulting to info for empty
// or unrecognized values.
func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
