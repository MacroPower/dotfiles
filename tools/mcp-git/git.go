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

	// ErrNoWorkTree is returned when a command that updates the
	// working tree runs in a repository that has none, such as a bare
	// repository.
	ErrNoWorkTree = errors.New("repository has no working tree")

	// ErrDeniedWorkTree is returned when the working tree of a
	// repository does not hold the git directory of the repository,
	// or when it lies inside a git directory.
	ErrDeniedWorkTree = errors.New("working tree must hold the git directory of the repository")

	// ErrDeniedRemote is returned when a remote name is not a
	// plain name.
	ErrDeniedRemote = errors.New("remote name is invalid")

	// ErrDeniedRefName is returned when a ref is not a plain
	// branch or tag name.
	ErrDeniedRefName = errors.New("ref name is invalid")

	// ErrUnknownRemote is returned when a remote name is not
	// configured in the repository.
	ErrUnknownRemote = errors.New("unknown remote")

	// ErrNoRemoteURL is returned when a remote of the repository has
	// no URL. For a push, the remote also has no push URL.
	ErrNoRemoteURL = errors.New("remote has no URL")

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

	// buildArgs, when set, returns the git args in place of args. It
	// reads the repository at the absolute path repo, so
	// [handler.runRemote] calls it after the path checks pass.
	buildArgs func(ctx context.Context, repo string) ([]string, error)

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
	push    bool // resolve the remote's push URLs, and take the options of a push

	// worktree is set for a subcommand that updates the working tree
	// or writes commits, as pull does. git then runs the filters, the
	// merge drivers, and the signing programs that the git config
	// names, so [handler.runRemote] refuses a repository whose config
	// names one. It also refuses a repository that has no working
	// tree.
	worktree bool
}

// runRemote validates the repository, bounds the call by the
// effective timeout, and locks the repository's git directory. It
// then runs the git command of op, and it returns git's combined
// output.
//
// git runs with the server's privileges, outside the Bash sandbox,
// and the caller can write the git config of the repository. runRemote
// therefore keeps git from running a command that this config names.
// It refuses a remote whose URL [handler.checkURL] denies. It refuses
// a repository whose config sets a key that git options cannot
// override, as [repoConfig.check] describes. [handler.gitOptions] and
// [handler.commandEnv] turn off the hooks and each other command.
//
// runRemote refuses a remote that the repository does not configure
// with [ErrUnknownRemote], and [remoteURLs] describes the error for a
// remote that git cannot read. runRemote refuses a remote that has no
// URL with [ErrNoRemoteURL], as [remoteLacksURL] decides.
//
// The config also names the working tree, with core.worktree.
// runRemote refuses a working tree that [handler.checkWorkTree]
// denies, and [handler.gitOptions] passes the path that runRemote
// checked to git.
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

	// core.worktree in the config of the repository names the working
	// tree. git can therefore write a working tree that lies outside
	// the path of the call.
	workTree, err := gitWorkTree(ctx, abs)
	if err != nil {
		return "", err
	}

	if workTree == "" && op.worktree {
		return "", fmt.Errorf("%w: %s", ErrNoWorkTree, op.repo)
	}

	if workTree != "" {
		err = h.checkWorkTree(ctx, abs, workTree)
		if err != nil {
			return "", fmt.Errorf("working tree of %s: %w", op.repo, err)
		}
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

	// git uses the name of a remote without a URL as its URL, and
	// checkURL refuses that name. This check comes first, so that the
	// error does not say that the remote has a URL.
	lacksURL, err := remoteLacksURL(ctx, abs, op.remote, urls[0], op.push)
	if err != nil {
		return "", err
	}

	if lacksURL {
		return "", noRemoteURLError(op.remote, op.push)
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

	args := op.args

	if op.buildArgs != nil {
		args, err = op.buildArgs(ctx, abs)
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

	opts := h.gitOptions(op, abs, workTree, cfg.hooks)

	out, err := h.runGit(ctx, slices.Concat(opts, args)...)
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
// path abs. workTree is the working tree that [handler.runRemote]
// checked, or "" for a repository that has none. hooks names each
// hook that the git config defines.
//
// The options of [handler.credentialArgs] name no URL of the remote.
// A push goes to every push URL, and git chooses the credential
// helpers for each URL that it connects to.
func (h *handler) gitOptions(op remoteOp, abs, workTree string, hooks []string) []string {
	args := slices.Concat(h.credentialArgs(), guardOptions(), hookOptions(hooks))

	if op.push {
		args = append(args, pushOptions(op.remote)...)
	}

	// --work-tree wins over core.worktree. git therefore writes the
	// working tree at the path that the server checked, whatever the
	// config holds when git reads it.
	if workTree != "" {
		args = append(args, "--work-tree="+workTree)
	}

	return append(args, "-C", abs)
}

// pushOptions returns the git options that keep a push to remote from
// following keys of the git config. A value on the command line comes
// after every config file, so it wins.
//
// push.recurseSubmodules is no, so git runs no command in a
// submodule. With on-demand or only, git starts a push in each
// submodule, and it does not pass --no-verify on to that push. With
// check, git looks for each submodule commit that the push
// references. It fetches a missing commit from the promisor remote of
// a submodule that is a partial clone. Each of these commands reads
// the git config of the submodule, which [readRepoConfig] does not
// read. That config can name a credential helper, and the fetch runs
// it. The value no also wins over submodule.recurse.
//
// push.pushOption is empty, which clears the list of push options
// that the config files set. git sends each push option to the
// remote, and the server acts on it. A GitLab server can create a
// merge request for merge_request.create, and it can merge that
// request into the branch of merge_request.target.
//
// remote.<name>.mirror is false. When it is true, git pushes every
// local ref with force, and it deletes each ref of the remote that
// the repository lacks.
func pushOptions(remote string) []string {
	return []string{
		"-c", "push.recurseSubmodules=no",
		"-c", "push.pushOption=",
		"-c", "remote." + remote + ".mirror=false",
	}
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

// gitWorkTree returns the absolute path of the working tree that git
// uses for the repository at repo. The path is the value of
// core.worktree when the config of the repository sets it. It returns
// "" for a repository that has no working tree, such as a bare
// repository. git rev-parse exits with an error there.
func gitWorkTree(ctx context.Context, repo string) (string, error) {
	out, err := gitValue(ctx, "-C", repo, "rev-parse", "--show-toplevel")
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", nil
		}

		if errors.Is(err, ErrTimeout) {
			return "", err
		}

		return "", fmt.Errorf("resolving working tree: %w", err)
	}

	return out, nil
}

// checkWorkTree checks the working tree at workTree of the repository
// at the absolute path abs. The working tree must lie under an allowed
// directory, and it must hold the git directory of the repository. It
// must also lie outside every git directory.
//
// The allowed directories can hold paths that the sandbox of the
// caller cannot write, such as other checkouts and the hooks
// directory of each git directory. core.worktree can name such a
// path, and git then writes the files of the remote there. git runs a
// file that lands in a hooks directory as a hook. A working tree
// holds the git directory when git finds that git directory from the
// working tree. git finds it through a .git directory or a .git file.
// The caller can write a .git file into a git directory whose hooks it
// cannot write. checkWorkTree therefore also refuses a working tree
// inside a git directory, as [enclosingGitDir] finds it.
func (h *handler) checkWorkTree(ctx context.Context, abs, workTree string) error {
	err := h.checkDest(workTree)
	if err != nil {
		return err
	}

	enclosing := enclosingGitDir(workTree)
	if enclosing == workTree {
		return fmt.Errorf("%w: %s is a git directory", ErrDeniedWorkTree, workTree)
	}

	if enclosing != "" {
		return fmt.Errorf("%w: %s lies inside the git directory %s", ErrDeniedWorkTree, workTree, enclosing)
	}

	gitDir, err := absoluteGitDir(ctx, abs)
	if err != nil {
		return err
	}

	found, err := absoluteGitDir(ctx, workTree)
	if errors.Is(err, ErrTimeout) {
		return err
	}

	if err != nil {
		return fmt.Errorf("%w: git finds no git directory from %s", ErrDeniedWorkTree, workTree)
	}

	if found != gitDir {
		return fmt.Errorf("%w: git finds the git directory %s from %s", ErrDeniedWorkTree, found, workTree)
	}

	return nil
}

// absoluteGitDir returns the absolute path of the git directory that
// git finds from the directory at dir. The path has no symbolic link.
// A linked worktree has a git directory of its own, inside the common
// git directory.
func absoluteGitDir(ctx context.Context, dir string) (string, error) {
	out, err := gitValue(ctx, "-C", dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		if errors.Is(err, ErrTimeout) {
			return "", err
		}

		return "", fmt.Errorf("%w: %s", ErrNotRepo, dir)
	}

	return out, nil
}

// enclosingGitDir returns the git directory that holds the directory
// at the absolute path dir, or dir when it is a git directory itself.
// It returns "" for a directory outside every git directory.
//
// A directory named .git is a git directory, whatever it holds. The
// git directory of each checkout has that name. A caller that can
// write that git directory can move aside each entry that [isGitDir]
// looks for, and git still follows a .git file there. The name
// comparison ignores case, because the file system of macOS ignores
// case and git keeps the spelling of a path that core.worktree gives.
// A directory with another name is a git directory when [isGitDir]
// reports it.
func enclosingGitDir(dir string) string {
	for {
		if strings.EqualFold(filepath.Base(dir), ".git") || isGitDir(dir) {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}

		dir = parent
	}
}

// isGitDir reports whether the directory at dir holds the entries
// that git looks for in a git directory. These are a HEAD entry, and
// either the objects and refs directories or a commondir file. git
// accepts a symbolic link as HEAD when the link points into refs/,
// even when the ref does not exist. isGitDir therefore accepts a HEAD
// entry of any type, and it does not follow a link there or read the
// entry. It also reports a directory whose HEAD git rejects.
func isGitDir(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, "HEAD"))
	if err != nil {
		return false
	}

	commonDir, err := os.Stat(filepath.Join(dir, "commondir"))
	if err == nil && commonDir.Mode().IsRegular() {
		return true
	}

	for _, name := range []string{"objects", "refs"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || !info.IsDir() {
			return false
		}
	}

	return true
}

// configValue returns the value that the git config of the repository
// at repo holds for key, or "" when the config does not set the key.
// options holds options of git config, such as --type=bool.
func configValue(ctx context.Context, repo, key string, options ...string) (string, error) {
	args := slices.Concat([]string{"-C", repo, "config"}, options, []string{"--get", key})

	out, err := gitValue(ctx, args...)
	if err != nil {
		// git config exits 1 when the config does not set the key.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", nil
		}

		if errors.Is(err, ErrTimeout) {
			return "", err
		}

		return "", fmt.Errorf("reading git config %s: %w", key, err)
	}

	return out, nil
}

// configEntry is a key of the git config and one value that the config
// stores for that key.
type configEntry struct {
	key   string
	value string
}

// configEntries returns an entry for each value that the git config of
// the repository at repo stores for a key that pattern matches. pattern
// is a regular expression of git config --get-regexp. git matches it
// against a key whose section name and variable name are in lowercase.
func configEntries(ctx context.Context, repo, pattern string) ([]configEntry, error) {
	// -z ends each value with a NUL and puts a newline between a key
	// and its value. A key holds no newline, because git allows none
	// in the name of a subsection.
	out, err := gitValue(ctx, "-C", repo, "config", "-z", "--get-regexp", pattern)
	if err != nil {
		// git config exits 1 when the config sets no key that pattern
		// matches.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil
		}

		if errors.Is(err, ErrTimeout) {
			return nil, err
		}

		return nil, fmt.Errorf("reading git config %s: %w", pattern, err)
	}

	var entries []configEntry

	for entry := range strings.SplitSeq(out, "\x00") {
		key, value, found := strings.Cut(entry, "\n")
		if found {
			entries = append(entries, configEntry{key: key, value: value})
		}
	}

	return entries, nil
}

// remoteURLs returns the URLs configured for remote in repo, with
// insteadOf and pushInsteadOf rewrites applied. When push is set it
// returns the push URLs, which can differ from the fetch URLs. A push
// goes to every push URL, and a fetch uses the first URL. The URLs
// must not appear in messages, because a URL can embed credentials.
//
// A repository that does not configure remote fails with
// [ErrUnknownRemote], as [remoteMissing] decides. git can also fail to
// read a remote that the git config defines, such as a remote whose
// remote.<name>.url key has no value. The error then says that
// remoteURLs could not read the remote, and it names remote and repo.
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

		if remoteMissing(err) {
			return nil, fmt.Errorf("%w: %s", ErrUnknownRemote, remote)
		}

		return nil, fmt.Errorf("reading the %s remote of %s: %w", remote, repo, err)
	}

	return strings.Split(out, "\n"), nil
}

// remoteMissing reports whether err is the error of a git remote
// get-url command that found no remote of the name in the repository.
// git 2.30 and later exit with status 2 then, and the manual of git
// remote keeps that status for a remote that git cannot find. git
// exits with another status for every other failure, such as a git
// config that it cannot parse.
func remoteMissing(err error) bool {
	var exitErr *exec.ExitError

	return errors.As(err, &exitErr) && exitErr.ExitCode() == 2
}

// remoteLacksURL reports whether remote has no URL in the repository
// at repo. url is the first URL that [remoteURLs] returns for the
// remote. When push is set, url is a push URL, and remoteLacksURL
// reports whether the remote has no URL and no push URL.
//
// The git config defines a remote without a URL when it stores no URL
// in remote.<name>.url and sets another key of the remote.
// remote.<name>.pushurl and remote.<name>.fetch are such keys. git
// uses the name of a remote without a URL as its URL, so git remote
// get-url prints the name and exits with status 0. git pushes to the
// push URLs of a remote, and to its URLs when it has no push URL. A
// remote that has a push URL and no URL therefore still takes a push.
//
// The git config can also store the name of the remote as its URL.
// remoteLacksURL therefore reads remote.<name>.url when url is the
// name of the remote, and for a push it also reads
// remote.<name>.pushurl. [configValue] returns the last value that the
// config stores for a key. An empty value clears the URLs that the
// config stores before it, in git 2.46 and later. The config therefore
// stores no URL under a key when configValue returns "".
//
// git applies the url.<base>.insteadOf rewrites to the name as well,
// and for a push the url.<base>.pushInsteadOf rewrites. When a rewrite
// changes the name, git reads another URL for the remote than its
// name, and it connects to that URL. remoteLacksURL then returns
// false.
//
// A file under .git/remotes or .git/branches can also store the name
// of the remote as its URL. git reads the same URL for that remote as
// for a remote without a URL, and remoteLacksURL returns true for
// both remotes.
func remoteLacksURL(ctx context.Context, repo, remote, url string, push bool) (bool, error) {
	if url != remote {
		return false, nil
	}

	keys := []string{"remote." + remote + ".url"}
	if push {
		keys = append(keys, "remote."+remote+".pushurl")
	}

	for _, key := range keys {
		stored, err := configValue(ctx, repo, key)
		if err != nil {
			return false, err
		}

		if stored != "" {
			return false, nil
		}
	}

	return true, nil
}

// noRemoteURLError returns the [ErrNoRemoteURL] error for remote. The
// error names the key of the git config that stores no URL. For a
// push, it also names the key that stores no push URL.
func noRemoteURLError(remote string, push bool) error {
	detail := "the git config stores no URL in remote." + remote + ".url"
	if push {
		detail += " and no push URL in remote." + remote + ".pushurl"
	}

	return fmt.Errorf("%w: %s: %s", ErrNoRemoteURL, remote, detail)
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

// githubCredentialHelper is the git credential helper that answers
// with the token of GH_TOKEN. The helper reads the token from the
// environment of git, so the token stays out of the arguments of
// every git command.
//
//nolint:gosec // G101: the helper holds no credential, because it reads GH_TOKEN.
const githubCredentialHelper = `!f() { echo username=x-access-token; echo password=$GH_TOKEN; }; f`

// credentialArgs returns git -c flags that give the credential helper
// of the token to each https connection to github.com. It returns nil
// when no token is set.
//
// The flags name no URL of the call. One git command can connect to
// more than one host, as a push to a remote with several push URLs
// does. git clone connects to the url input after it applies the
// url.<base>.insteadOf rewrites of the git config. A conditional
// include of the global config can hold a rewrite for the new clone
// alone. The server cannot apply that rewrite before the clone.
//
// git applies the key credential.https://github.com.helper only to a
// connection to https://github.com, after every rewrite. The empty
// value of that key removes the helpers of the config files for that
// host. Those helpers still answer for every other host.
func (h *handler) credentialArgs() []string {
	if h.token == "" {
		return nil
	}

	return []string{
		"-c", "credential.https://github.com.helper=",
		"-c", "credential.https://github.com.helper=" + githubCredentialHelper,
	}
}

// gitEnv returns the environment for git subprocesses. It preserves
// the inherited environment except GIT_CONFIG. It forces
// non-interactive behavior: no credential prompt, no editor for merge
// or rebase messages, and no SSH passphrase prompt. GIT_SSH_COMMAND is
// only set when the caller has not configured one.
//
// Each variable that gitEnv sets also keeps git from running a
// command that the git config names. git reads the editor, the SSH
// command, the program that asks for a password, and the proxy
// command of the git:// transport from the environment first. It
// reads core.editor, sequence.editor, core.sshCommand, core.askPass,
// and core.gitProxy only when the variable is not set. gitEnv
// therefore sets GIT_ASKPASS and GIT_PROXY_COMMAND when the inherited
// environment does not. GIT_ASKPASS takes the value of SSH_ASKPASS,
// which git reads after core.askPass. GIT_PROXY_COMMAND is empty,
// which runs no proxy.
//
// gitEnv removes GIT_CONFIG. With that variable set, a git config
// command without a file option reads and writes only the file that
// the variable names. Every other git command ignores the variable.
// The server reads the config of a repository with git config, as
// [readRepoConfig] and [configValue] do. It then runs git commands
// that read the config files of the repository. Without the variable,
// git config reads the same files.
func gitEnv() []string {
	// git reads an empty GIT_CONFIG as the name of a file, and git
	// config then fails. gitEnv therefore removes the variable, and it
	// does not give the variable an empty value.
	env := slices.DeleteFunc(os.Environ(), func(entry string) bool {
		return strings.HasPrefix(entry, "GIT_CONFIG=")
	})

	env = append(env,
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
//
// checkDest compares the absolute path of dest with each allowed
// directory. Each allowed directory must therefore be an absolute
// path, as [resolveAllowDirs] returns it.
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

		// A path outside dir has a relative path whose first segment
		// is "..". A name that starts with two dots, such as
		// "..owner", is another segment, and it stays under dir.
		if !filepath.IsLocal(rel) {
			continue
		}

		return nil
	}

	return fmt.Errorf(
		"%w: must be under %v",
		ErrDeniedDest, h.allowDirs,
	)
}

// maxLinks is the number of symbolic links that
// [resolveExistingPrefix] follows on one path. [filepath.EvalSymlinks]
// follows the same number.
const maxLinks = 255

// resolveExistingPrefix resolves the symbolic links of an absolute path
// as far as the path exists, and it keeps the rest of the path as
// text. [handler.checkDest] and [resolveAllowDirs] make each path
// absolute before they pass it.
//
// resolveExistingPrefix reads the path one element at a time from its
// start, as the OS does. It reads the target of each symbolic link in
// place of the link, also when that target does not exist. A path
// through the link therefore names the same directory before and
// after someone creates the target. A ".." after a symbolic link leads
// to the parent of the target of the link, whether or not the target
// or a later element exists. A ".." after an element that does not
// exist drops that element. After maxLinks links, such as on a loop of
// links, each later link stays as text.
func resolveExistingPrefix(path string) string {
	sep := string(filepath.Separator)

	// resolved holds the elements that the loop has read, with the
	// target of each link in place of the link. The OS reads a ".."
	// against that path, so filepath.Dir drops the last element as
	// text.
	resolved := sep
	links := 0

	for rest := path; rest != ""; {
		var elem string

		elem, rest, _ = strings.Cut(rest, sep)

		switch elem {
		case "", ".":
			continue
		case "..":
			resolved = filepath.Dir(resolved)

			continue
		}

		next := filepath.Join(resolved, elem)

		// os.Lstat fails for an element that does not exist, and
		// os.Readlink fails for a link that the OS cannot read. Each
		// of those elements stays as text.
		info, err := os.Lstat(next)
		if err != nil || info.Mode()&os.ModeSymlink == 0 || links == maxLinks {
			resolved = next

			continue
		}

		target, err := os.Readlink(next)
		if err != nil || target == "" {
			resolved = next

			continue
		}

		links++

		// The OS reads an absolute target from the root, and a
		// relative target against the directory that holds the link.
		// That directory is still in resolved.
		if filepath.IsAbs(target) {
			resolved = sep
		}

		rest = target + sep + rest
	}

	return resolved
}

// acquireLock takes an exclusive flock on the file at path.lock, and
// it creates that file when the path names none. It returns a function
// that removes the file and releases the lock.
//
// A call that waits for the flock opened the file of the call that
// holds the lock. The holder removes that file when it releases the
// lock. The waiting call then takes the flock of a file that the path
// does not name. That flock excludes no later call, because a later
// call creates a new file at the path and locks the new file.
// acquireLock therefore compares the file that it locked with the file
// at the path, and it opens the path again when they differ.
func acquireLock(path string) (func(), error) {
	lockPath := path + ".lock"

	for {
		lockFile, err := os.Create(lockPath) //nolint:gosec // G301: lock path is derived from validated input.
		if err != nil {
			return nil, fmt.Errorf("creating lock file: %w", err)
		}

		//nolint:gosec // G115: Fd fits in int on all supported platforms.
		err = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX)
		if err != nil {
			closeLockFile(lockFile)

			return nil, fmt.Errorf("acquiring lock: %w", err)
		}

		current, err := namesFile(lockPath, lockFile)
		if err != nil {
			closeLockFile(lockFile)

			return nil, fmt.Errorf("checking lock file: %w", err)
		}

		if current {
			return func() { releaseLock(lockPath, lockFile) }, nil
		}

		closeLockFile(lockFile)
	}
}

// releaseLock removes the lock file at lockPath, and then it closes
// lockFile. The close releases the flock. The removal comes first, so
// the caller holds the flock for as long as lockPath names the file. A
// call that waited for the flock then finds that lockPath does not
// name the file that it locked. It opens the path again, as
// [acquireLock] describes. With the other order, a waiting call can
// take the flock and compare the files before the removal. It then
// holds the flock of a file that the path does not name.
func releaseLock(lockPath string, lockFile *os.File) {
	err := os.Remove(lockPath)
	if err != nil {
		slog.Warn("removing lock file", slog.Any("error", err))
	}

	closeLockFile(lockFile)
}

// closeLockFile closes lockFile, and it logs the error of a close that
// fails.
func closeLockFile(lockFile *os.File) {
	err := lockFile.Close()
	if err != nil {
		slog.Warn("closing lock file", slog.Any("error", err))
	}
}

// namesFile reports whether path names the open file f. It reports
// false when path names no file.
func namesFile(path string, f *os.File) (bool, error) {
	opened, err := f.Stat()
	if err != nil {
		return false, fmt.Errorf("reading open file: %w", err)
	}

	named, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("reading file at path: %w", err)
	}

	return os.SameFile(opened, named), nil
}

// toolError wraps err as an MCP tool-level error result.
func toolError(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		IsError: true,
	}
}
