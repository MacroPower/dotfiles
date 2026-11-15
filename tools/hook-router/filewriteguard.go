package main

import (
	"io"
	"log/slog"
	"path/filepath"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/hook"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/writeguard"
)

// handleFileWritePre forces a permission prompt before a Write, Edit,
// MultiEdit, or NotebookEdit call writes a path that the sandbox denies
// to sandboxed commands. It acts only in the plan and bypassPermissions
// modes and only when cfg.writeGuardDirs is set.
//
// Claude Code's sandbox confines Bash but not the file tools. In the
// bypassPermissions mode, and in plan mode when the session has bypass
// available, Claude Code approves every file-tool write without a
// prompt. An "ask" from a hook still prompts in those modes, so the
// handler restores the prompt for writes that the sandbox stops. The
// other modes already prompt for a write outside the working
// directories. The handler leaves them alone, so a directory added with
// /add-dir keeps working there.
//
// The handler reads the settings files on every call (see
// [writeguard.Load]), so a settings.local.json that a workmux worktree
// hook wrote takes effect without a restart. A settings file that does
// not decode also produces an ask, with the error as the reason.
// Invalid JSON and a call without a path fall through.
//
// The ask goes through [writeAsk]. Under cfg.promptGuard, a Workflow
// agent's write therefore gets a deny that carries the reason of the
// ask.
func handleFileWritePre(input []byte, stdout io.Writer, cfg config, logger *slog.Logger) error {
	if cfg.writeGuardDirs == nil {
		return nil
	}

	h, err := hook.ParseInput(input)
	if err != nil {
		logger.Info("invalid JSON, falling through", slog.Any("error", err))
		return nil
	}

	if h.PermissionMode != "plan" && h.PermissionMode != "bypassPermissions" {
		return nil
	}

	path := fileToolPath(h.ToolInput)
	if path == "" {
		return nil
	}

	if !filepath.IsAbs(path) {
		path = filepath.Join(h.Cwd, path)
	}

	reason, refused := writeGuardCheck(cfg, h.Cwd, path)
	if !refused {
		return nil
	}

	logger.Info(
		"ask",
		slog.String("rule", "write-guard"),
		slog.String("tool", h.ToolName),
		slog.String("mode", h.PermissionMode),
		slog.String("path", path),
		slog.String("reason", reason),
	)

	return writeAsk(stdout, h, reason+" Confirm before writing.", cfg, logger)
}

// writeGuardCheck loads the sandbox write policy for a session in cwd
// and checks path against it. A policy that does not load refuses
// every path, with the error in the reason.
func writeGuardCheck(cfg config, cwd, path string) (string, bool) {
	dirs, err := cfg.writeGuardDirs(cwd)
	if err != nil {
		return "hook-router cannot check " + path + " against the sandbox write paths: " + err.Error() + ".", true
	}

	policy, err := writeguard.Load(dirs)
	if err != nil {
		return "hook-router cannot check " + path + " against the sandbox write paths: " + err.Error() + ".", true
	}

	return policy.Check(path)
}

// fileToolPath returns the path a file tool writes. NotebookEdit names
// it notebook_path, and the other file tools name it file_path.
func fileToolPath(toolInput map[string]any) string {
	for _, key := range []string{"file_path", "notebook_path"} {
		if path, ok := toolInput[key].(string); ok && path != "" {
			return path
		}
	}

	return ""
}
