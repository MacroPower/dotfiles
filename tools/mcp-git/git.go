package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// defaultRemote is the remote used when a caller omits one.
	defaultRemote = "origin"

	// maxOutputBytes bounds how much git output a tool result
	// echoes back.
	maxOutputBytes = 16 << 10

	// cleanupTimeout bounds a best-effort cleanup command that runs
	// after the main git invocation may already have consumed its
	// budget.
	cleanupTimeout = 30 * time.Second
)

var (
	// ErrDeniedDest is returned when a path is outside all
	// allowed directories.
	ErrDeniedDest = errors.New("path not under any allowed directory")

	// ErrTimeout is returned when a git operation exceeds the
	// configured timeout.
	ErrTimeout = errors.New("git operation timed out")

	// ErrMissingRepo is returned when the repo field is empty.
	ErrMissingRepo = errors.New("repo is required")

	// ErrDeniedRepoPrefix is returned when the repo path starts
	// with a dash.
	ErrDeniedRepoPrefix = errors.New("repo must not start with '-'")

	// ErrNotRepo is returned when a path is not inside a git
	// repository.
	ErrNotRepo = errors.New("not a git repository")

	// ErrDeniedRemote is returned when a remote name is not a
	// plain name.
	ErrDeniedRemote = errors.New("remote name is invalid")

	// ErrDeniedRefName is returned when a ref is not a plain
	// branch or tag name.
	ErrDeniedRefName = errors.New("ref name is invalid")

	// ErrUnknownRemote is returned when a remote name is not
	// configured in the repository.
	ErrUnknownRemote = errors.New("unknown remote")

	// remoteNamePattern matches the remote names accepted by
	// [checkRemoteName]. It is deliberately narrower than git's own
	// rules so that no URL form can pass as a remote name.
	remoteNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

	// refNamePattern matches the plain branch and tag names accepted
	// by [checkRefName]. Excluding ':' and '+' is load-bearing: a
	// refspec like ":main" deletes the remote branch and "+main" is
	// a force push, and neither must be expressible through a ref
	// field.
	refNamePattern = regexp.MustCompile(`^[A-Za-z0-9._][A-Za-z0-9._/-]*$`)
)

// handler implements the tool handlers for the git MCP server.
type handler struct {
	token         string        // GitHub personal access token for HTTPS auth
	allowDirs     []string      // repository and destination paths must resolve under one of these
	timeout       time.Duration // timeout bounds each git invocation; 0 disables.
	allowInsecure bool          // permit http:// and git:// URLs
	allowFileURLs bool          // testing only: permit file:// and local path URLs
}

// remoteOp describes one git subcommand that contacts a remote.
type remoteOp struct {
	repo   string   // local repository path, checked against allowDirs
	remote string   // validated remote name
	args   []string // git args after "-C <repo>", e.g. {"fetch", "--", "origin"}

	// cleanupArgs, when set, runs best-effort after the main command
	// fails, restoring the repository to its pre-call state (e.g.
	// {"rebase", "--abort"} after a conflicting rebase pull).
	cleanupArgs []string

	timeout int  // per-call timeout override in seconds
	push    bool // resolve the remote's push URL, not its fetch URL
}

// runRemote validates the repository, bounds the call by the
// effective timeout, locks the repository's git directory, resolves
// the remote's URL so [handler.credentialArgs] can authenticate it,
// and returns git's combined output.
func (h *handler) runRemote(ctx context.Context, op remoteOp) (string, error) {
	if d := h.effectiveTimeout(op.timeout); d > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}

	abs, err := filepath.Abs(op.repo)
	if err != nil {
		return "", fmt.Errorf("resolving repo path: %w", err)
	}

	// The input path is checked in addition to the resolved common
	// dir below: a gitdir file outside the allowed directories can
	// point at an allowed git directory while the working tree that
	// git -C mutates stays outside every allowed directory.
	destErr := h.checkDest(abs)
	if destErr != nil {
		return "", destErr
	}

	commonDir, err := gitCommonDir(ctx, abs)
	if err != nil {
		return "", err
	}

	// rev-parse walks upward, so a path under an allowed directory
	// can resolve to a repository above it.
	destErr = h.checkDest(commonDir)
	if destErr != nil {
		return "", destErr
	}

	cleanup, err := acquireLock(filepath.Join(commonDir, "mcp-git"))
	if err != nil {
		return "", err
	}
	defer cleanup()

	url, err := remoteURL(ctx, abs, op.remote, op.push)
	if err != nil {
		return "", err
	}

	args := h.credentialArgs(url)
	args = append(args, "-C", abs)
	args = append(args, op.args...)

	out, err := runGit(ctx, args...)
	if err != nil && len(op.cleanupArgs) > 0 {
		h.runCleanup(ctx, abs, op.cleanupArgs)
	}

	return truncateOutput(out), err
}

// runCleanup best-effort runs a git command that restores the
// repository after a failed operation. It runs on a fresh deadline
// because the main command may have consumed the caller's budget.
func (h *handler) runCleanup(ctx context.Context, repo string, cleanupArgs []string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()

	args := append([]string{"-C", repo}, cleanupArgs...)

	_, err := runGit(cleanupCtx, args...)
	if err != nil {
		slog.WarnContext(
			ctx,
			"running cleanup command",
			slog.Any("args", cleanupArgs),
			slog.Any("error", err),
		)
	}
}

// runGit runs git with args and returns its combined stdout and
// stderr. git writes fetch and push summaries to stderr, so both
// streams go to one buffer and neither can reach the process's own
// stdout, which carries the MCP protocol. On failure the captured
// output is folded into the returned error.
func runGit(ctx context.Context, args ...string) (string, error) {
	var buf bytes.Buffer

	//nolint:gosec // G204: args are validated by the calling handler.
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = gitEnv()
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	out := strings.TrimSpace(buf.String())

	if err != nil {
		tErr := timeoutError(ctx)
		if tErr != nil {
			return out, tErr
		}

		if out == "" {
			return out, fmt.Errorf("running git: %w", err)
		}

		return out, fmt.Errorf("running git: %w: %s", err, truncateOutput(out))
	}

	return out, nil
}

// gitValue runs git with args and returns its trimmed stdout.
// Stderr is discarded and never folded into the returned error, so
// callers can parse stdout as a value and error messages cannot leak
// remote URLs.
func gitValue(ctx context.Context, args ...string) (string, error) {
	//nolint:gosec // G204: args are validated by the calling handler.
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = gitEnv()

	out, err := cmd.Output()
	if err != nil {
		tErr := timeoutError(ctx)
		if tErr != nil {
			return "", tErr
		}

		return "", fmt.Errorf("running git: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

// gitCommonDir returns the absolute common git directory for repo.
// It doubles as the repository check: git walks up from repo, so a
// path inside a repository resolves to that repository, a linked
// worktree resolves to the main checkout's git directory, and a
// non-repository path errors with [ErrNotRepo].
func gitCommonDir(ctx context.Context, repo string) (string, error) {
	out, err := gitValue(ctx,
		"-C", repo, "rev-parse", "--path-format=absolute", "--git-common-dir",
	)
	if err != nil {
		if errors.Is(err, ErrTimeout) {
			return "", err
		}

		return "", fmt.Errorf("%w: %s", ErrNotRepo, repo)
	}

	return out, nil
}

// remoteURL returns the URL configured for remote in repo. When push
// is set it returns the push URL, which git allows to differ from
// the fetch URL. The URL is only ever passed to
// [handler.credentialArgs]; it must not appear in messages because
// it can embed credentials.
func remoteURL(ctx context.Context, repo, remote string, push bool) (string, error) {
	args := []string{"-C", repo, "remote", "get-url"}
	if push {
		args = append(args, "--push")
	}

	args = append(args, remote)

	out, err := gitValue(ctx, args...)
	if err != nil {
		if errors.Is(err, ErrTimeout) {
			return "", err
		}

		return "", fmt.Errorf("%w: %s", ErrUnknownRemote, remote)
	}

	return out, nil
}

// checkRemoteInputs validates the fields shared by the remote
// operation tools: the repository path, the remote name, and an
// optional branch or tag name.
func checkRemoteInputs(repo, remote, ref string) error {
	if repo == "" {
		return ErrMissingRepo
	}

	if strings.HasPrefix(repo, "-") {
		return ErrDeniedRepoPrefix
	}

	err := checkRemoteName(remote)
	if err != nil {
		return err
	}

	if ref != "" {
		return checkRefName(ref)
	}

	return nil
}

// checkRemoteName verifies that remote is a plain remote name, never
// a URL or an option.
func checkRemoteName(remote string) error {
	if !remoteNamePattern.MatchString(remote) {
		return fmt.Errorf("%w: %q", ErrDeniedRemote, remote)
	}

	return nil
}

// checkRefName verifies that ref is a plain branch or tag name,
// never a refspec (no ':' or '+', so remote deletes and force pushes
// are unreachable) and never a form git reserves.
func checkRefName(ref string) error {
	if !refNamePattern.MatchString(ref) {
		return fmt.Errorf("%w: %q", ErrDeniedRefName, ref)
	}

	if strings.Contains(ref, "..") ||
		strings.HasSuffix(ref, ".lock") ||
		strings.HasSuffix(ref, "/") ||
		strings.HasSuffix(ref, ".") {
		return fmt.Errorf("%w: %q", ErrDeniedRefName, ref)
	}

	return nil
}

// remoteOrDefault returns remote, or [defaultRemote] when it is
// empty.
func remoteOrDefault(remote string) string {
	if remote == "" {
		return defaultRemote
	}

	return remote
}

// truncateOutput trims s to [maxOutputBytes], keeping the tail
// because git prints the ref-update summary last, and prefixes a
// marker when bytes are dropped.
func truncateOutput(s string) string {
	if len(s) <= maxOutputBytes {
		return s
	}

	return "[output truncated]\n" + s[len(s)-maxOutputBytes:]
}

// remoteResult formats a successful remote operation as a tool
// result, appending git's captured output when there is any.
func remoteResult(action, out string) *mcp.CallToolResult {
	msg := action
	if out != "" {
		msg += "\n\n" + out
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}
}

// effectiveTimeout returns the timeout to apply for a single
// git invocation. A non-zero perCallSecs overrides h.timeout;
// otherwise h.timeout is used (and 0 disables the bound).
func (h *handler) effectiveTimeout(perCallSecs int) time.Duration {
	if perCallSecs > 0 {
		return time.Duration(perCallSecs) * time.Second
	}

	return h.timeout
}

// timeoutError returns an [ErrTimeout]-wrapped error when ctx
// has expired, and nil otherwise. Use it inside an existing
// `if err != nil` branch around a git subprocess to convert a
// generic subprocess failure into a recognizable timeout.
func timeoutError(ctx context.Context) error {
	ctxErr := ctx.Err()
	if ctxErr == nil {
		return nil
	}

	return fmt.Errorf("%w: %w", ErrTimeout, ctxErr)
}

// credentialArgs returns git -c flags that configure a
// credential helper for GitHub HTTPS URLs. It returns nil when
// no token is set or the URL is not an HTTPS GitHub URL.
func (h *handler) credentialArgs(url string) []string {
	if h.token == "" || !strings.HasPrefix(url, "https://github.com/") {
		return nil
	}

	return []string{
		"-c", "credential.helper=",
		"-c", `credential.https://github.com.helper=!f() { echo username=x-access-token; echo password=$GH_TOKEN; }; f`,
	}
}

// gitEnv returns the environment for git subprocesses. It preserves
// the inherited environment and forces non-interactive behavior: no
// credential prompt, no editor for merge or rebase messages, and no
// SSH passphrase prompt. GIT_SSH_COMMAND is only set when the caller
// has not configured one.
func gitEnv() []string {
	env := append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_EDITOR=true",
		"GIT_SEQUENCE_EDITOR=true",
	)

	if os.Getenv("GIT_SSH_COMMAND") == "" {
		env = append(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}

	return env
}

// checkDest verifies that dest is under one of the allowed
// directories. If no directories are configured, all paths
// are accepted. Symlinks along the path are resolved to
// prevent directory escapes.
func (h *handler) checkDest(dest string) error {
	if len(h.allowDirs) == 0 {
		return nil
	}

	abs, err := filepath.Abs(dest)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDeniedDest, err)
	}

	abs = resolveExistingPrefix(abs)

	for _, dir := range h.allowDirs {
		rel, relErr := filepath.Rel(dir, abs)
		if relErr != nil {
			continue
		}

		if len(rel) >= 2 && rel[:2] == ".." {
			continue
		}

		return nil
	}

	return fmt.Errorf(
		"%w: must be under %v",
		ErrDeniedDest, h.allowDirs,
	)
}

// resolveExistingPrefix resolves symlinks for the longest
// existing prefix of path and appends the remaining
// unresolved suffix.
func resolveExistingPrefix(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved
	}

	dir := filepath.Dir(path)
	base := filepath.Base(path)

	if dir == path {
		return path
	}

	return filepath.Join(resolveExistingPrefix(dir), base)
}

// acquireLock takes an exclusive flock on path.lock and returns a cleanup
// function that releases the lock and removes the file.
func acquireLock(path string) (func(), error) {
	lockPath := path + ".lock"

	lockFile, err := os.Create(lockPath) //nolint:gosec // G301: lock path is derived from validated input.
	if err != nil {
		return nil, fmt.Errorf("creating lock file: %w", err)
	}

	//nolint:gosec // G115: Fd fits in int on all supported platforms.
	err = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX)
	if err != nil {
		closeErr := lockFile.Close()
		if closeErr != nil {
			slog.Warn("closing lock file after flock failure", slog.Any("error", closeErr))
		}

		return nil, fmt.Errorf("acquiring lock: %w", err)
	}

	cleanup := func() {
		closeErr := lockFile.Close()
		if closeErr != nil {
			slog.Warn("closing lock file", slog.Any("error", closeErr))
		}

		removeErr := os.Remove(lockPath)
		if removeErr != nil {
			slog.Warn("removing lock file", slog.Any("error", removeErr))
		}
	}

	return cleanup, nil
}

// toolError wraps err as an MCP tool-level error result.
func toolError(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		IsError: true,
	}
}
