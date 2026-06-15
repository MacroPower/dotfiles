package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLevel(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		in   string
		want slog.Level
	}{
		"debug":          {"debug", slog.LevelDebug},
		"uppercase":      {"DEBUG", slog.LevelDebug},
		"padded info":    {"  info  ", slog.LevelInfo},
		"empty defaults": {"", slog.LevelInfo},
		"warn":           {"warn", slog.LevelWarn},
		"warning alias":  {"warning", slog.LevelWarn},
		"error":          {"error", slog.LevelError},
		"unknown":        {"bogus", slog.LevelInfo},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, parseLevel(tc.in))
		})
	}
}

func TestNewLoggerWritesJSONFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "proxy.log")
	logger, closeLog, err := newLogger(Config{LogFile: path, LogLevel: "debug"}, false)
	require.NoError(t, err)

	logger.Debug("hello", "k", "v")
	closeLog()

	b, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"msg":"hello"`)
	assert.Contains(t, string(b), `"k":"v"`)
}

func TestNewLoggerDiscardsWhenStderrUnsafe(t *testing.T) {
	t.Parallel()

	// run passes stderrSafe=false; with no log file the logger must be inert so
	// it cannot corrupt claude's terminal.
	logger, closeLog, err := newLogger(Config{}, false)
	require.NoError(t, err)
	defer closeLog()

	assert.False(t, logger.Enabled(context.Background(), slog.LevelError))
}

func TestNewLoggerLevelFiltersFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "proxy.log")
	logger, closeLog, err := newLogger(Config{LogFile: path, LogLevel: "warn"}, false)
	require.NoError(t, err)

	logger.Info("suppressed")
	logger.Warn("kept")
	closeLog()

	b, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "suppressed")
	assert.Contains(t, string(b), "kept")
}
