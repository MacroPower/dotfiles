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
	"slices"
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

	// hookSwitchEnv names the environment variable that holds the
	// value false. The --config-env option of git reads the value of
	// a config key from an environment variable, and it accepts a key
	// that holds '='. [hookOptions] uses the option to set
	// hook.<name>.enabled for a hook with any name.
	hookSwitchEnv = "MCP_GIT_HOOK_ENABLED"
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

	// skippedHook names a hook event of the subcommand, such as
	// "pre-push", whose hooks the subcommand skips. The caller must
	// run the checks of those hooks itself. When the repository has a
	// hook for the event, the output of a successful command ends
	// with a note that names each hook.
	skippedHook string

	// cleanupArgs, when set, runs best-effort after the main command
	// fails, restoring the repository to its pre-call state, such as
	// {"rebase", "--abort"} after a conflicting rebase pull.
	cleanupArgs []string

	timeout int  // per-call timeout override in seconds
	push    bool // resolve the remote's push URLs, and push no submodule

	// worktree is set for a subcommand that updates the working tree
	// or writes commits, as pull does. git then runs the filters, the
	// merge drivers, and the signing programs that the git config
	// names, so [handler.runRemote] refuses a repository whose config
	// names one.
	worktree bool
}

// runRemote validates the repository, bounds the call by the
// effective timeout, locks the repository's git directory, resolves
// the remote's URL so [handler.credentialArgs] can authenticate it,
// and returns git's combined output.
//
// git runs with the server's privileges, outside the Bash sandbox,
// and the caller can write the git config of the repository. runRemote
// therefore keeps git from running a command that this config names.
// It refuses a remote whose URL [handler.checkURL] denies. It refuses
// a repository whose config names a command that git options cannot
// turn off, as [repoConfig.check] describes. [handler.gitOptions] and
// [handler.commandEnv] turn off the hooks and each other command.
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

	urls, err := remoteURLs(ctx, abs, op.remote, op.push)
	if err != nil {
		return "", err
	}

	// git reaches a repository on the local file system by starting
	// upload-pack or receive-pack in it. That process does not inherit
	// the options of [handler.gitOptions], so it runs the hooks and the
	// config commands of the other repository. The caller can write
	// both when the repository lies under a path that the sandbox lets
	// it write. git refuses each transport that [handler.protocols]
	// does not name when it connects. This check refuses the URL first,
	// so that the error names the remote and never its URL. A push
	// goes to every push URL, and a fetch uses the first URL.
	checked := urls
	if !op.push {
		checked = urls[:1]
	}

	for _, url := range checked {
		if h.checkURL(url) != nil {
			return "", fmt.Errorf(
				"%w: remote %s has a URL that is not https, ssh, or user@host:path",
				ErrDeniedURL, op.remote,
			)
		}
	}

	var checkSubmodules bool

	if op.push {
		checkSubmodules, err = pushRecursesSubmodules(ctx, abs)
		if err != nil {
			return "", err
		}
	}

	var note string

	if op.skippedHook != "" {
		note, err = skippedHookNote(ctx, abs, op.skippedHook)
		if err != nil {
			return "", err
		}
	}

	// The config is read last, so that the caller has the least time
	// to change it before git reads it.
	cfg, err := readRepoConfig(ctx, abs)
	if err != nil {
		return "", err
	}

	err = cfg.check(op.worktree)
	if err != nil {
		return "", err
	}

	opts := h.gitOptions(op, abs, urls[0], checkSubmodules, cfg.hooks)

	out, err := h.runGit(ctx, slices.Concat(opts, op.args)...)
	if err != nil && len(op.cleanupArgs) > 0 {
		h.runCleanup(ctx, opts, op.cleanupArgs)
	}

	out = truncateOutput(out)

	if err == nil && note != "" {
		out = strings.TrimSpace(out + "\n\n" + note)
	}

	return out, err
}

// gitOptions returns the options that come before the subcommand in
// each git command that op runs in the repository at the absolute
// path abs. url is the first URL of the remote, which
// [handler.credentialArgs] authenticates. Only a push reads
// checkSubmodules, which makes git check the submodule commits that
// the push references. hooks names each hook that the git config
// defines.
func (h *handler) gitOptions(op remoteOp, abs, url string, checkSubmodules bool, hooks []string) []string {
	args := slices.Concat(h.credentialArgs(url), guardOptions(), hookOptions(hooks))

	// git does not pass --no-verify on to the push that it starts in
	// a submodule. That push runs the pre-push hooks of the
	// submodule. git applies the last value that its config sets for
	// push.recurseSubmodules or submodule.recurse. A value on the
	// command line comes after every config file, so it wins. With
	// check or no, git starts no push in a submodule. check refuses a
	// push that references a submodule commit that no remote-tracking
	// ref of the submodule reaches. A push uses check when the git
	// config asks git to recurse into submodules. Otherwise it uses
	// no, the default of git.
	if op.push {
		recurse := "no"
		if checkSubmodules {
			recurse = "check"
		}

		args = append(args, "-c", "push.recurseSubmodules="+recurse)
	}

	return append(args, "-C", abs)
}

// hookOptions returns the git options that disable each hook that
// hooks names. git 2.54 and later run each hook that the git config
// defines with hook.<name>.command, whatever the hooks directory
// holds. hook.<name>.enabled disables one hook, and a value on the
// command line wins over every config file. The name of a hook can
// hold '=', which -c reads as the end of the key. The value therefore
// comes from the environment variable hookSwitchEnv.
func hookOptions(hooks []string) []string {
	args := make([]string, 0, len(hooks))

	for _, name := range hooks {
		args = append(args, "--config-env=hook."+name+".enabled="+hookSwitchEnv)
	}

	return args
}

// guardOptions returns the git options that turn off each command
// that the git config names under a fixed key. A value on the command
// line wins over every config file, and git passes the values on to
// each git command that it starts in the same repository.
func guardOptions() []string {
	return []string{
		// A hook runs with the server's privileges, outside the Bash
		// sandbox, and the caller can edit the code that a hook runs.
		// That code is the hook file itself under an in-tree
		// core.hooksPath, and each working-tree command that a hook
		// starts. Point hooksPath at a non-directory so that no hook
		// file runs. No tool input enables hooks.
		"-c", "core.hooksPath=" + os.DevNull,

		// core.fsmonitor can name a hook that git runs when it reads
		// the index.
		"-c", "core.fsmonitor=false",

		// core.alternateRefsCommand names a command that lists the
		// refs of an alternate object store during a fetch. The
		// command true lists no ref, so a fetch can download an object
		// that the alternate already holds.
		"-c", "core.alternateRefsCommand=true",

		// A fetch, a merge, and a rebase each start git maintenance,
		// which runs the pre-auto-gc hook and gc.recentObjectsHook.
		"-c", "maintenance.auto=false",
	}
}

// skippedHookNote returns the note that tells the caller about the
// hooks for event that a command skips. The note names the hook file
// of the repository and each enabled hook that git config defines for
// event. It returns "" when repo has neither.
func skippedHookNote(ctx context.Context, repo, event string) (string, error) {
	path, err := hookFile(ctx, repo, event)
	if err != nil {
		return "", err
	}

	names, err := configHooks(ctx, repo, event)
	if err != nil {
		return "", err
	}

	var hooks []string

	if path != "" {
		hooks = append(hooks, "- the hook file "+path)
	}

	for _, name := range names {
		hooks = append(hooks, fmt.Sprintf("- the hook %s in git config (hook.%s.command)", name, name))
	}

	if len(hooks) == 0 {
		return "", nil
	}

	return fmt.Sprintf(
		"This server runs outside the sandbox, so it skipped these %s hooks:\n%s\n"+
			"Run the checks of each hook in Bash.",
		event, strings.Join(hooks, "\n"),
	), nil
}

// configHooks returns the names of the enabled hooks that git config
// defines for event in the repository at repo. The hooksPath override
// keeps the hook file out of the listing of git hook list. That
// command exits 1 when it finds no hook. Git 2.53 and earlier do not
// read hooks from git config, and they exit with an error too. A
// listing that exits with an error therefore holds no hook.
func configHooks(ctx context.Context, repo, event string) ([]string, error) {
	out, err := gitValue(ctx,
		"-c", "core.hooksPath="+os.DevNull, "-C", repo, "hook", "list", "-z", event,
	)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, nil
		}

		if errors.Is(err, ErrTimeout) {
			return nil, err
		}

		return nil, fmt.Errorf("listing %s hooks: %w", event, err)
	}

	var names []string

	// Each entry names one hook. git puts the reason and a tab before
	// the name of a hook that it does not run, such as "disabled".
	for entry := range strings.SplitSeq(out, "\x00") {
		if entry == "" || strings.Contains(entry, "\t") {
			continue
		}

		names = append(names, entry)
	}

	return names, nil
}

// pushRecursesSubmodules reports whether the git config of the
// repository at repo asks git push to recurse into submodules. Two
// keys set the recursion, and git applies the last of them that the
// config sets. push.recurseSubmodules recurses with check, on-demand,
// or only, and submodule.recurse recurses when it is true. The
// bool-or-str type makes git print each boolean value as true or
// false.
func pushRecursesSubmodules(ctx context.Context, repo string) (bool, error) {
	out, err := gitValue(ctx,
		"-C", repo, "config", "-z", "--type=bool-or-str", "--get-regexp",
		`^(push\.recursesubmodules|submodule\.recurse)$`,
	)
	if err != nil {
		// git config exits 1 when the config sets neither key.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}

		if errors.Is(err, ErrTimeout) {
			return false, err
		}

		return false, fmt.Errorf("reading submodule recursion config: %w", err)
	}

	// Each entry ends with a NUL and holds the key, a newline, and the
	// value.
	entries := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	key, value, _ := strings.Cut(entries[len(entries)-1], "\n")

	if key == "submodule.recurse" {
		return value == "true", nil
	}

	return value != "false", nil
}

// hookFile returns the path of the hook file that the repository at
// the absolute path repo holds for event, or "" when it holds none.
// Git reads hook files from core.hooksPath when the repository sets
// it, and git ignores a hook file that is not executable.
func hookFile(ctx context.Context, repo, event string) (string, error) {
	path, err := gitValue(ctx, "-C", repo, "rev-parse", "--git-path", "hooks/"+event)
	if err != nil {
		if errors.Is(err, ErrTimeout) {
			return "", err
		}

		return "", fmt.Errorf("resolving %s hook path: %w", event, err)
	}

	// git prints a path inside the repository relative to repo after
	// the OS resolves its symlinks. A join to the unresolved spelling
	// cleans each ".." against the wrong parent when repo is a symlink
	// into the working tree.
	if !filepath.IsAbs(path) {
		dir, err := filepath.EvalSymlinks(repo)
		if err != nil {
			return "", fmt.Errorf("resolving %s hook path: %w", event, err)
		}

		path = filepath.Join(dir, path)
	}

	if !isExecutableFile(path) {
		return "", nil
	}

	return path, nil
}

// isExecutableFile reports whether path names a regular file that
// has an execute permission bit. It follows symlinks.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// runCleanup best-effort runs a git command that restores the
// repository after a failed operation. The command takes opts, the
// options of [handler.gitOptions], because the cleanup fires hooks
// and writes the working tree as the failed command does. It runs on
// a fresh deadline because the main command might have consumed the
// caller's budget.
func (h *handler) runCleanup(ctx context.Context, opts, cleanupArgs []string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()

	_, err := h.runGit(cleanupCtx, slices.Concat(opts, cleanupArgs)...)
	if err != nil {
		slog.WarnContext(
			ctx,
			"running cleanup command",
			slog.Any("args", cleanupArgs),
			slog.Any("error", err),
		)
	}
}

// runGit runs git with args in the environment of
// [handler.commandEnv], and returns its combined stdout and stderr.
// It runs each git command that contacts a remote or changes a
// repository. git writes fetch and push summaries to stderr, so both
// streams go to one buffer and neither can reach the process's own
// stdout, which carries the MCP protocol. On failure the captured
// output is folded into the returned error.
func (h *handler) runGit(ctx context.Context, args ...string) (string, error) {
	var buf bytes.Buffer

	//nolint:gosec // G204: args are validated by the calling handler.
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = h.commandEnv()
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

// remoteURLs returns the URLs configured for remote in repo, with
// insteadOf and pushInsteadOf rewrites applied. When push is set it
// returns the push URLs, which can differ from the fetch URLs. A push
// goes to every push URL, and a fetch uses the first URL. The URLs
// must not appear in messages, because a URL can embed credentials.
func remoteURLs(ctx context.Context, repo, remote string, push bool) ([]string, error) {
	args := []string{"-C", repo, "remote", "get-url", "--all"}
	if push {
		args = append(args, "--push")
	}

	args = append(args, remote)

	out, err := gitValue(ctx, args...)
	if err != nil {
		if errors.Is(err, ErrTimeout) {
			return nil, err
		}

		return nil, fmt.Errorf("%w: %s", ErrUnknownRemote, remote)
	}

	return strings.Split(out, "\n"), nil
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
//
// Each variable also keeps git from running a command that the git
// config names. git reads the editor, the SSH command, the program
// that asks for a password, and the proxy command of the git://
// transport from the environment first. It reads core.editor,
// sequence.editor, core.sshCommand, core.askPass, and core.gitProxy
// only when the variable is not set. gitEnv therefore sets
// GIT_ASKPASS and GIT_PROXY_COMMAND when the inherited environment
// does not. GIT_ASKPASS takes the value of SSH_ASKPASS, which git
// reads after core.askPass. GIT_PROXY_COMMAND is empty, which runs no
// proxy.
func gitEnv() []string {
	env := append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_EDITOR=true",
		"GIT_SEQUENCE_EDITOR=true",
	)

	if os.Getenv("GIT_SSH_COMMAND") == "" {
		env = append(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}

	if _, ok := os.LookupEnv("GIT_ASKPASS"); !ok {
		env = append(env, "GIT_ASKPASS="+os.Getenv("SSH_ASKPASS"))
	}

	if _, ok := os.LookupEnv("GIT_PROXY_COMMAND"); !ok {
		env = append(env, "GIT_PROXY_COMMAND=")
	}

	return env
}

// commandEnv returns the environment of a git command that contacts a
// remote or changes a repository. It extends [gitEnv].
// GIT_ALLOW_PROTOCOL makes git refuse each transport that
// [handler.protocols] does not name, whatever the git config allows.
// The variable that hookSwitchEnv names holds the value that
// [hookOptions] gives to hook.<name>.enabled.
func (h *handler) commandEnv() []string {
	return append(gitEnv(),
		"GIT_ALLOW_PROTOCOL="+strings.Join(h.protocols(), ":"),
		hookSwitchEnv+"=false",
	)
}

// protocols returns the names of the git transports that the handler
// lets git use. They are the transports of the URLs that
// [handler.checkURL] accepts. The list never holds ext, the transport
// that runs a command from the URL. git resolves a remote again after
// [handler.runRemote] checks its URLs, and the git config can rewrite
// a URL or name a transport for a remote, so git enforces the list
// itself.
func (h *handler) protocols() []string {
	protocols := []string{"https", "ssh"}

	if h.allowInsecure {
		protocols = append(protocols, "http", "git")
	}

	if h.allowFileURLs {
		protocols = append(protocols, "file")
	}

	return protocols
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
