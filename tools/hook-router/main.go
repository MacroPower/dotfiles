package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// config holds runtime settings resolved from the environment and
// from flag-parsed wrapper inputs.
//
// Invariant: after [mainErr] finishes wiring, postImpl is non-nil
// (possibly an empty catalog). Handlers can call
// cfg.postImpl.HasLabel(...) and cfg.postImpl.BuildAskReason(...)
// without a nil-guard. Tests that exercise handlers must construct a
// catalog too (see testCatalog in plan_test.go).
type config struct {
	postImpl   *PostImplCatalog
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
	tool := flag.String("tool", "", "tool name (Bash, ExitPlanMode, EnterPlanMode, AskUserQuestion)")
	dbPath := flag.String("db", "", "path to SQLite state database")
	postImplAgents := flag.String("post-impl-agents", "", "JSON array of {label, aliases?, description} entries")

	flag.Parse()

	err := mainErr(*logFile, *event, *tool, *dbPath, *postImplAgents)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hook-router: %v\n", err)
		os.Exit(1)
	}
}

func mainErr(logFile, event, tool, dbPath, postImplAgentsJSON string) error {
	logger, closeLog, err := openLogger(logFile)
	if err != nil {
		return err
	}
	defer closeLog()

	// 45s > 30s busy_timeout leaves headroom for JSON encode + git calls
	// even when a single DB call burns the full busy_timeout budget.
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	var store *Store

	if dbPath != "" {
		store, err = OpenStore(ctx, dbPath)
		if err != nil {
			return fmt.Errorf("opening store: %w", err)
		}
		defer store.Close()

		if ran, err := store.MaybePruneStale(ctx); ran {
			if err != nil {
				logger.Debug("prune stale sessions failed", slog.Any("error", err))
			} else {
				logger.Debug("pruned stale sessions")
			}
		}
	}

	catalog, err := parsePostImplAgents(postImplAgentsJSON)
	if err != nil {
		return fmt.Errorf("parsing --post-impl-agents: %w", err)
	}

	if catalog.Empty() {
		logger.Warn("post-impl catalog is empty")
	}

	cfg := configFromEnv()
	cfg.postImpl = catalog

	return run(ctx, os.Stdin, os.Stdout, event, tool, store, cfg, logger)
}

func run(
	ctx context.Context,
	stdin io.Reader,
	stdout io.Writer,
	event, tool string,
	store *Store,
	cfg config,
	logger *slog.Logger,
) error {
	input, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}

	logger.Info("dispatch", slog.String("event", event), slog.String("tool", tool))

	switch event {
	case "PreToolUse":
		switch tool {
		case "Bash":
			return handleBash(ctx, input, stdout, cfg, logger)
		case "ExitPlanMode":
			if store == nil {
				return nil
			}

			return handleExitPlanModePre(ctx, input, stdout, store, ".", logger)

		case "EnterPlanMode":
			if store == nil {
				return nil
			}

			return handleEnterPlanMode(ctx, input, store, logger)

		default:
			return nil
		}
	case "PostToolUse":
		switch tool {
		case "AskUserQuestion":
			if store == nil {
				return nil
			}

			return handlePostAskUserQuestion(ctx, input, store, cfg, ".", logger)

		default:
			return nil
		}

	case "Stop":
		if store == nil {
			return nil
		}

		return handleStop(ctx, input, stdout, store, cfg, ".", logger)

	default:
		// Backward compat: no --event flag, treat as Bash PreToolUse.
		return handleBash(ctx, input, stdout, cfg, logger)
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
