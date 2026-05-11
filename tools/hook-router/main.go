package main

import (
	"bytes"
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
// Invariant: once [mainErr] finishes wiring, postImpl, commandRules,
// and formatterRules are non-nil (possibly empty), so handlers can
// call cfg.postImpl.HasLabel(...) / cfg.postImpl.BuildAskReason(...),
// cfg.commandRules.Check(...), and cfg.formatterRules.Empty() /
// cfg.formatterRules.Match(...) without nil-guards. Handler tests
// must construct all three as well (see testCatalog in plan_test.go,
// [NewCommandRules], and [NewFormatterRules]).
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
	formatterRules *FormatterRules
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
	formatterRules := flag.String("formatter-rules", "", "JSON array of file-formatter routing rules ({pathGlob, command, timeout})")
	autoAllow := flag.Bool("auto-allow", false, "emit PreToolUse \"allow\" on fall-through (use only when a sandbox is enforcing containment)")

	flag.Parse()

	err := mainErr(*logFile, *event, *tool, *dbPath, *postImplSkills, *commitSkills, *commandRules, *formatterRules, *autoAllow)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hook-router: %v\n", err)
		os.Exit(1)
	}
}

func mainErr(logFile, event, tool, dbPath, postImplSkillsJSON, commitSkillsJSON, commandRulesJSON, formatterRulesJSON string, autoAllow bool) error {
	logger, closeLog, err := openLogger(logFile)
	if err != nil {
		return err
	}
	defer closeLog()

	// 45s > 30s busy_timeout leaves headroom for JSON encode + git calls
	// even when a single DB call burns the full busy_timeout budget.
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// Read stdin once so [eventNeedsStore] can peek at the payload's
	// tool_name. The matcher-less PostToolUse hook fires hook-router
	// on every tool call (Read/Bash/Grep/...), so opening SQLite
	// unconditionally would add ~5-15ms per call to events that route
	// to no-op handlers.
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}

	var store *Store

	if dbPath != "" && eventNeedsStore(event, tool, input) {
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

	formatters, err := parseFormatterRules(formatterRulesJSON)
	if err != nil {
		return fmt.Errorf("parsing --formatter-rules: %w", err)
	}

	if formatters.Empty() {
		logger.Debug("formatter rules engine is empty")
	}

	cfg := configFromEnv()
	cfg.postImpl = catalog
	cfg.commitSkills = skills
	cfg.commandRules = rules
	cfg.formatterRules = formatters
	cfg.autoAllow = autoAllow

	return run(ctx, bytes.NewReader(input), os.Stdout, event, tool, store, cfg, logger)
}

// eventNeedsStore reports whether the dispatch for (event, tool) will
// reach a handler that requires the SQLite [*Store]. PostToolUse with
// an empty tool peeks at the stdin payload's tool_name to resolve the
// matcher-less fallback. Returns false for events whose only handler
// is a no-op default (e.g. PreToolUse:Bash, PostToolUse:Read) so
// [mainErr] can skip the SQLite open on the hot path.
func eventNeedsStore(event, tool string, input []byte) bool {
	switch event {
	case "Stop", "SessionStart", "UserPromptSubmit":
		return true
	case "PreToolUse":
		return tool == "ExitPlanMode" || tool == "EnterPlanMode"
	case "PostToolUse":
		toolName := tool
		if toolName == "" {
			parsed, err := parseHookInput(input)
			if err == nil {
				toolName = parsed.ToolName
			}
		}

		return toolName == "AskUserQuestion"
	}

	return false
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
		// home/claude.nix wires a single matcher-less PostToolUse hook
		// and lets hook-router route by tool internally. Claude Code
		// does not inject a $CLAUDE_TOOL_NAME env var, so the tool
		// name has to come from the stdin payload. The --tool flag is
		// honored as an override for ad-hoc invocations.
		toolName := tool
		if toolName == "" {
			parsed, err := parseHookInput(input)
			if err == nil {
				toolName = parsed.ToolName
			}
		}

		switch toolName {
		case "AskUserQuestion":
			if store == nil {
				return nil
			}

			return handlePostAskUserQuestion(ctx, input, store, cfg, ".", logger)

		case "Write", "Edit", "MultiEdit":
			return handlePostFileWrite(ctx, input, cfg, logger)

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
