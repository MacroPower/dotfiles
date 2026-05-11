package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// config holds runtime settings resolved from the environment and
// from flag-parsed wrapper inputs.
//
// Invariant: once [mainErr] finishes wiring, postImpl and commandRules
// are non-nil (possibly empty), so handlers can call
// cfg.postImpl.HasLabel(...) / cfg.postImpl.BuildAskReason(...) and
// cfg.commandRules.Check(...) without nil-guards. Handler tests must
// construct both as well (see testCatalog in plan_test.go and
// [NewCommandRules]).
//
// commitSkills lists the wrap-up skill names (without leading slash)
// whose UserPromptSubmit invocation clears plan-guard state. A nil or
// empty slice disables the failsafe.
//
// autoAllow, when true, makes [handleBash] emit a PreToolUse "allow"
// decision on the fall-through paths (after deny + RTK delegate),
// suppressing Claude Code's static Bash analyzer prompt for shell
// expansions. Only safe when a sandbox is enforcing the actual
// containment.
//
// claudePID is the Claude Code window PID this hook subprocess was
// forked from. It scopes `pending_plans` to one window so two sessions
// in the same cwd do not collide. Empty when PPID <= 1 (no Claude
// parent, e.g. ad-hoc invocation or PID-1 container); in that case the
// pending-plans handoff is silently disabled, matching the
// kubeconfigPath empty-guard.
type config struct {
	postImpl       *PostImplCatalog
	commandRules   *CommandRules
	commitSkills   []string
	rtkRewrite     string
	kubeconfigPath string
	claudePID      string
	autoAllow      bool
}

func configFromEnv() config {
	cfg := config{
		rtkRewrite: os.Getenv("RTK_REWRITE"),
	}

	if ppid := os.Getppid(); ppid > 1 {
		cfg.claudePID = strconv.Itoa(ppid)

		p := filepath.Join(os.TempDir(), "claude-kubectx", cfg.claudePID, "kubeconfig")
		if _, err := os.Stat(p); err == nil {
			cfg.kubeconfigPath = p
		}
	}

	return cfg
}

func main() {
	logFile := flag.String("log-file", "", "path to JSON log file (append)")
	event := flag.String("event", "", "hook event (PreToolUse, PostToolUse, Stop, UserPromptSubmit)")
	tool := flag.String("tool", "", "tool name (Bash, ExitPlanMode, EnterPlanMode, AskUserQuestion)")
	dbPath := flag.String("db", "", "path to SQLite state database")
	postImplSkills := flag.String("post-impl-skills", "", "JSON array of {label, description} entries")
	commitSkills := flag.String("commit-skills", "", "JSON array of skill names whose invocation clears plan-guard state")
	commandRules := flag.String("command-rules", "", "JSON array of command deny rules ({command, args, except, reason})")
	autoAllow := flag.Bool("auto-allow", false, "emit PreToolUse \"allow\" on fall-through (use only when a sandbox is enforcing containment)")

	flag.Parse()

	err := mainErr(*logFile, *event, *tool, *dbPath, *postImplSkills, *commitSkills, *commandRules, *autoAllow)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hook-router: %v\n", err)
		os.Exit(1)
	}
}

func mainErr(logFile, event, tool, dbPath, postImplSkillsJSON, commitSkillsJSON, commandRulesJSON string, autoAllow bool) error {
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

	catalog, err := parsePostImplSkills(postImplSkillsJSON)
	if err != nil {
		return fmt.Errorf("parsing --post-impl-skills: %w", err)
	}

	if catalog.Empty() {
		logger.Warn("post-impl catalog is empty")
	}

	skills, err := parseCommitSkills(commitSkillsJSON)
	if err != nil {
		return fmt.Errorf("parsing --commit-skills: %w", err)
	}

	rules, err := parseCommandRules(commandRulesJSON)
	if err != nil {
		return fmt.Errorf("parsing --command-rules: %w", err)
	}

	if rules.Empty() {
		// A host with every rule-contributing bundle disabled is a
		// legitimate config, so log at debug rather than warn (unlike
		// the post-impl catalog above).
		logger.Debug("command rules engine is empty")
	}

	cfg := configFromEnv()
	cfg.postImpl = catalog
	cfg.commitSkills = skills
	cfg.commandRules = rules
	cfg.autoAllow = autoAllow

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

	logger.Info("dispatch",
		slog.String("event", event),
		slog.String("tool", tool),
		slog.Int("ppid", os.Getppid()),
	)

	switch event {
	case "PreToolUse":
		switch tool {
		case "Bash":
			return handleBash(ctx, input, stdout, cfg, logger)
		case "ExitPlanMode":
			if store == nil {
				return nil
			}

			return handleExitPlanModePre(ctx, input, stdout, store, cfg.claudePID, ".", logger)

		case "EnterPlanMode":
			if store == nil {
				return nil
			}

			return handleEnterPlanMode(ctx, input, store, cfg.claudePID, logger)

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

	case "SessionStart":
		if store == nil {
			return nil
		}

		return handleSessionStart(ctx, input, store, cfg.claudePID, logger)

	case "UserPromptSubmit":
		if store == nil {
			return nil
		}

		return handleUserPromptSubmit(ctx, input, store, cfg, logger)

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
