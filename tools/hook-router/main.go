package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// config holds runtime settings resolved from the environment.
type config struct {
	rtkRewrite string
}

func configFromEnv() config {
	return config{
		rtkRewrite: os.Getenv("RTK_REWRITE"),
	}
}

func main() {
	logFile := flag.String("log-file", "", "path to JSON log file (append)")
	event := flag.String("event", "", "hook event (PreToolUse, PostToolUse, Stop)")
	tool := flag.String("tool", "", "tool name (Bash, ExitPlanMode, EnterPlanMode)")
	dbPath := flag.String("db", "", "path to SQLite state database")

	flag.Parse()

	err := mainErr(*logFile, *event, *tool, *dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hook-router: %v\n", err)
		os.Exit(1)
	}
}

func mainErr(logFile, event, tool, dbPath string) error {
	logger, closeLog, err := openLogger(logFile)
	if err != nil {
		return err
	}
	defer closeLog()

	var store *Store

	if dbPath != "" {
		store, err = OpenStore(dbPath)
		if err != nil {
			return fmt.Errorf("opening store: %w", err)
		}
		defer store.Close()
	}

	return run(os.Stdin, os.Stdout, event, tool, store, configFromEnv(), logger)
}

func run(stdin io.Reader, stdout io.Writer, event, tool string, store *Store, cfg config, logger *slog.Logger) error {
	input, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}

	logger.Info("dispatch", slog.String("event", event), slog.String("tool", tool))

	switch event {
	case "PreToolUse":
		switch tool {
		case "Bash":
			return handleBash(input, stdout, cfg, logger)
		case "ExitPlanMode":
			if store == nil {
				return nil
			}

			return handleExitPlanModePre(input, stdout, store, ".", logger)
		case "EnterPlanMode":
			if store == nil {
				return nil
			}

			return handleEnterPlanMode(input, store, logger)
		default:
			return nil
		}
	case "PostToolUse":
		return nil
	case "Stop":
		if store == nil {
			return nil
		}

		return handleStop(input, stdout, store, ".", logger)
	default:
		// Backward compat: no --event flag, treat as Bash PreToolUse.
		return handleBash(input, stdout, cfg, logger)
	}
}

// openLogger creates a JSON [*slog.Logger] writing to the named file.
// Returns a discard logger and no-op closer when path is empty.
func openLogger(path string) (*slog.Logger, func(), error) {
	if path == "" {
		return slog.New(slog.DiscardHandler), func() {}, nil
	}

	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return nil, nil, fmt.Errorf("creating log directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("opening %s: %w", path, err)
	}

	logger := slog.New(slog.NewJSONHandler(f, nil))

	return logger, func() {
		err := f.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "closing log file: %v\n", err)
		}
	}, nil
}
