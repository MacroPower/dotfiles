package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	// ErrMissingURL is returned when the URL field is empty.
	ErrMissingURL = errors.New("url is required")

	// ErrMissingDest is returned when the dest field is empty.
	ErrMissingDest = errors.New("dest is required")

	// ErrDeniedURL is returned when the URL scheme is not
	// allowed.
	ErrDeniedURL = errors.New("url scheme not allowed")

	// ErrDeniedBranch is returned when the branch name starts
	// with a dash.
	ErrDeniedBranch = errors.New("branch must not start with '-'")

	// ErrDeniedRef is returned when the ref name starts
	// with a dash.
	ErrDeniedRef = errors.New("ref must not start with '-'")

	// ErrRefConflict is returned when both branch and ref
	// are set.
	ErrRefConflict = errors.New("branch and ref are mutually exclusive")

	// ErrDeniedSparsePath is returned when a sparse checkout
	// path is invalid.
	ErrDeniedSparsePath = errors.New("sparse path is invalid")

	// ErrDeniedDestPrefix is returned when the dest path starts
	// with a dash.
	ErrDeniedDestPrefix = errors.New("dest must not start with '-'")

	// ErrOriginMismatch is returned when the existing repo's
	// origin URL does not match the requested URL.
	ErrOriginMismatch = errors.New("origin URL mismatch")

	// ErrNoOrigin is returned when the existing repo at dest has no
	// origin remote.
	ErrNoOrigin = errors.New("dest holds a repository that has no origin remote")

	// ErrCorruptDest is returned when dest holds a .git directory that
	// [checkGitDir] rejects and the handler cannot move dest aside.
	ErrCorruptDest = errors.New("dest holds a corrupt git directory")

	// ErrClone wraps the underlying git clone failure.
	ErrClone = errors.New("git clone failed")

	// scpPattern matches SCP-style git URLs (e.g., git@github.com:org/repo).
	scpPattern = regexp.MustCompile(`^\w+@[\w.-]+:`)

	// safeSchemes lists the URL prefixes accepted by [handler.checkURL].
	safeSchemes = []string{
		"https://",
		"ssh://",
	}

	// insecureSchemes lists unencrypted URL prefixes that are only
	// accepted when allowInsecure is set.
	insecureSchemes = []string{
		"http://",
		"git://",
	}
)

// CloneInput is the JSON input schema for the git_clone tool.
type CloneInput struct {
	URL            string   `json:"url"                      jsonschema:"Repository URL to clone"`
	Dest           string   `json:"dest"                     jsonschema:"Destination directory path"`
	Branch         string   `json:"branch,omitzero"          jsonschema:"Branch to clone"`
	Ref            string   `json:"ref,omitzero"             jsonschema:"Branch or tag to check out (alias for branch)"`
	SparsePaths    []string `json:"sparse_paths,omitzero"    jsonschema:"Paths for sparse checkout (implies sparse)"`
	Depth          int      `json:"depth,omitzero"           jsonschema:"Shallow clone depth"`
	TimeoutSeconds int      `json:"timeout_seconds,omitzero" jsonschema:"Override the default per-operation timeout in seconds; 0 means use the server default"`
	SingleBranch   bool     `json:"single_branch,omitzero"   jsonschema:"Clone only the specified branch"`
	Sparse         bool     `json:"sparse,omitzero"          jsonschema:"Enable sparse checkout (only root files unless sparse_paths is set)"`
}

func (h *handler) handleClone(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input CloneInput,
) (*mcp.CallToolResult, any, error) {
	if input.URL == "" {
		return toolError(ErrMissingURL), nil, nil
	}

	if input.Dest == "" {
		return toolError(ErrMissingDest), nil, nil
	}

	if strings.HasPrefix(input.Dest, "-") {
		return toolError(ErrDeniedDestPrefix), nil, nil
	}

	// Each later step reads the absolute path. Every spelling of one
	// destination, such as a path with a trailing slash, therefore
	// names the same parent directory and the same lock file.
	//
	// [handler.checkDest] checks the absolute path, and git must write
	// that path. The OS resolves a symbolic link before the ".."
	// segment that follows it, so a path with such a segment can name
	// another directory for git. The OS also resolves a relative path
	// against the physical working directory. [os.Getwd] returns $PWD
	// when $PWD names that directory, and $PWD can pass through a
	// symbolic link.
	dest, absErr := filepath.Abs(input.Dest)
	if absErr != nil {
		return nil, nil, fmt.Errorf("resolving dest path: %w", absErr)
	}

	input.Dest = dest

	urlErr := h.checkURL(input.URL)
	if urlErr != nil {
		return toolError(urlErr), nil, nil
	}

	if input.Branch != "" && strings.HasPrefix(input.Branch, "-") {
		return toolError(ErrDeniedBranch), nil, nil
	}

	if input.Ref != "" && strings.HasPrefix(input.Ref, "-") {
		return toolError(ErrDeniedRef), nil, nil
	}

	if input.Ref != "" && input.Branch != "" {
		return toolError(ErrRefConflict), nil, nil
	}

	if input.Ref != "" {
		input.Branch = input.Ref
	}

	sparseErr := checkSparsePaths(input.SparsePaths)
	if sparseErr != nil {
		return toolError(sparseErr), nil, nil
	}

	destErr := h.checkDest(input.Dest)
	if destErr != nil {
		return toolError(destErr), nil, nil
	}

	if d := h.effectiveTimeout(input.TimeoutSeconds); d > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}

	err := os.MkdirAll(filepath.Dir(input.Dest), 0o755) //nolint:gosec // G301: dest is user-provided input.
	if err != nil {
		return nil, nil, fmt.Errorf("creating parent directory: %w", err)
	}

	cleanup, err := acquireLock(input.Dest)
	if err != nil {
		return nil, nil, err
	}
	defer cleanup()

	gitDir := filepath.Join(input.Dest, ".git")

	info, statErr := os.Stat(gitDir)
	if statErr != nil || !info.IsDir() {
		return h.clone(ctx, input, "")
	}

	repoErr := checkGitDir(ctx, gitDir)
	if repoErr == nil {
		return h.pullExisting(ctx, input)
	}

	if errors.Is(repoErr, ErrTimeout) {
		return toolError(repoErr), nil, nil
	}

	aside, asideErr := h.moveAside(input.Dest)
	if asideErr != nil {
		return toolError(asideErr), nil, nil
	}

	return h.clone(ctx, input, aside)
}

// checkGitDir reports whether git can open gitDir as a repository and
// gitDir holds a config file. A cleanup job such as systemd-tmpfiles
// on /tmp deletes old files one at a time. It can remove HEAD, config,
// or both, and keep the rest of the directory. The --git-dir flag
// stops git from walking up to a repository that encloses the
// destination.
//
// git opens a git directory that has HEAD and no config file. git
// clone writes the origin remote to the config file, so a checkout
// that lost the file has no origin to pull from.
func checkGitDir(ctx context.Context, gitDir string) error {
	_, err := gitValue(ctx, "--git-dir", gitDir, "rev-parse", "--git-dir")
	if err != nil {
		if errors.Is(err, ErrTimeout) {
			return err
		}

		return fmt.Errorf("%w: %s", ErrNotRepo, gitDir)
	}

	_, err = os.Stat(filepath.Join(gitDir, "config"))
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrNotRepo, gitDir, err)
	}

	return nil
}

// moveAside renames a corrupt checkout at dest to a timestamped
// sibling path and returns that path, so a clone can take dest. It
// renames the checkout and deletes nothing, because the working tree
// can hold files that exist nowhere else.
func (h *handler) moveAside(dest string) (string, error) {
	aside := fmt.Sprintf("%s.corrupt-%s", dest, time.Now().UTC().Format("20060102T150405.000000000Z"))

	destErr := h.checkDest(aside)
	if destErr != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrCorruptDest, dest, destErr)
	}

	err := os.Rename(dest, aside)
	if err != nil {
		return "", fmt.Errorf("%w: %s: delete it and retry: %w", ErrCorruptDest, dest, err)
	}

	return aside, nil
}

// checkURL verifies that url uses a permitted scheme. Accepted
// forms are https, ssh, and SCP-style (user@host:path).
// Unencrypted schemes (http, git) require allowInsecure. File
// URLs and local paths are rejected unless allowFileURLs is set.
func (h *handler) checkURL(url string) error {
	if h.allowFileURLs {
		return nil
	}

	for _, scheme := range safeSchemes {
		if strings.HasPrefix(url, scheme) {
			return nil
		}
	}

	if h.allowInsecure {
		for _, scheme := range insecureSchemes {
			if strings.HasPrefix(url, scheme) {
				return nil
			}
		}
	}

	if scpPattern.MatchString(url) {
		return nil
	}

	return fmt.Errorf("%w: %s", ErrDeniedURL, url)
}

//nolint:unparam // signature matches mcp.AddTool handler contract.
func (h *handler) pullExisting(ctx context.Context, input CloneInput) (*mcp.CallToolResult, any, error) {
	originErr := h.checkOrigin(ctx, input.URL, input.Dest)
	if originErr != nil {
		return toolError(originErr), nil, nil
	}

	// runRemote takes the git-directory lock that the
	// remote-operation tools use, nested under the dest lock
	// handleClone already holds; the ordering is fixed, so it
	// cannot deadlock.
	out, err := h.runRemote(ctx, remoteOp{
		repo:     input.Dest,
		remote:   defaultRemote,
		worktree: true,
		timeout:  input.TimeoutSeconds,
		args:     buildPullArgs(defaultRemote, PullInput{Repo: input.Dest}),
	})
	if err != nil {
		return toolError(fmt.Errorf("pulling latest changes in %s: %w", input.Dest, err)), nil, nil
	}

	return remoteResult(
		fmt.Sprintf("Pulled latest changes in %s", input.Dest),
		out,
	), nil, nil
}

// checkOrigin verifies that url is the URL of the origin remote of the
// existing repo at dest. A repo that has no origin remote fails with
// [ErrNoOrigin].
//
// git clone stores url as the URL of origin. git applies the
// url.<base>.insteadOf rewrites of the git config each time it reads
// the URL of a remote. The URL that it reads for origin can therefore
// differ from url, and checkOrigin applies the rewrites to url as
// well. It reads both URLs in dest, so the same config files rewrite
// each URL. It strips a trailing ".git" suffix from each URL after the
// rewrites, and then it compares the two URLs.
//
// An [ErrOriginMismatch] error names the URL that git reads for
// origin, and it names url without the rewrites. It names each URL
// without a trailing ".git" suffix.
func (h *handler) checkOrigin(ctx context.Context, url, dest string) error {
	origin, err := gitValue(ctx, "-C", dest, "remote", "get-url", "origin")
	if err != nil {
		if errors.Is(err, ErrTimeout) {
			return err
		}

		// git remote get-url exits 2 when the repository does not
		// configure the remote. The manual of git remote keeps that
		// status for a remote that git cannot find.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 2 {
			return fmt.Errorf("%w: %s: choose another dest, or delete the directory and retry", ErrNoOrigin, dest)
		}

		return fmt.Errorf("reading the origin remote of %s: %w", dest, err)
	}

	// git ls-remote --get-url prints url after the rewrites, and it
	// does not contact the remote. When the git config defines a
	// remote whose name is url, such as with git config
	// remote.<url>.url, git ls-remote --get-url prints the URL of that
	// remote instead. When url holds no '/', such as the SCP-style url
	// git@host:repo, a file named url under .git/remotes or
	// .git/branches of dest also defines such a remote. git ls-remote
	// --get-url then prints the URL of that file, or it fails when git
	// rejects the file.
	//
	// git remote add, git remote rename, and git clone --origin refuse
	// a name that holds ':', and every url that checkURL accepts
	// outside tests holds one. git writes a file under .git/remotes or
	// .git/branches only when it copies one from the template
	// directory. Only a config or a file that someone writes by hand
	// therefore defines such a remote.
	rewritten, err := gitValue(ctx, "-C", dest, "ls-remote", "--get-url", "--", url)
	if err != nil {
		if errors.Is(err, ErrTimeout) {
			return err
		}

		return fmt.Errorf("applying the URL rewrites of the git config of %s: %w", dest, err)
	}

	got := strings.TrimSuffix(origin, ".git")
	want := strings.TrimSuffix(url, ".git")

	if got != strings.TrimSuffix(rewritten, ".git") {
		return fmt.Errorf("%w: got %s, want %s", ErrOriginMismatch, got, want)
	}

	return nil
}

// clone runs git clone for input. input.Dest holds the absolute path
// that [handler.handleClone] makes from the dest input. A non-empty
// aside names the path that a corrupt checkout at the destination
// moved to, and the result message reports it.
//
// Each git command takes the options of [guardOptions] and
// [hookOptions]. A new clone holds the hook files of the template
// directory, and git runs them when it checks out the clone. git also
// runs each hook of the global config in the clone, where the remote
// chose every file. [handler.runRemote] reads the git config of an
// existing repository before it runs git. The config of a new clone
// does not exist before the clone, so clone disables the hooks of
// [readUserHooks]. The command of [sparseCheckoutArgs] runs after the
// clone, and it names the working tree itself.
//
// Each git command also takes the options of
// [handler.credentialArgs], which authenticate a clone from GitHub
// over https. A sparse clone without a depth leaves out each blob
// that its checkout does not hold. The command of [sparseCheckoutArgs]
// then fetches the blobs of the sparse paths from origin.
//
//nolint:unparam // signature matches mcp.AddTool handler contract.
func (h *handler) clone(ctx context.Context, input CloneInput, aside string) (*mcp.CallToolResult, any, error) {
	hooks, err := readUserHooks(ctx)
	if err != nil {
		return toolError(err), nil, nil
	}

	opts := slices.Concat(h.credentialArgs(), guardOptions(), hookOptions(hooks))

	_, err = h.runGit(ctx, slices.Concat(opts, buildCloneArgs(input))...)
	if err != nil {
		return toolError(fmt.Errorf("%w: %w", ErrClone, err)), nil, nil
	}

	if len(input.SparsePaths) > 0 {
		_, scErr := h.runGit(ctx, sparseCheckoutArgs(opts, input.Dest, input.SparsePaths)...)
		if scErr != nil {
			return toolError(fmt.Errorf("setting sparse checkout: %w", scErr)), nil, nil
		}
	}

	msg := fmt.Sprintf("Cloned %s into %s", input.URL, input.Dest)
	if len(input.SparsePaths) > 0 {
		msg += fmt.Sprintf(" (sparse: %s)", strings.Join(input.SparsePaths, ", "))
	}

	if aside != "" {
		msg += fmt.Sprintf(". The previous checkout had a corrupt .git directory and moved to %s", aside)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}, nil, nil
}

// sparseCheckoutArgs returns the arguments of the git command that
// sets paths as the sparse checkout of the new clone at dest. opts
// holds the options that come before the subcommand, and dest is an
// absolute path.
//
// The command runs after the clone, and the caller can write the git
// config of the clone between the two commands. core.worktree in that
// config names the working tree, and git writes the files of paths
// there. The remote chooses the content of those files. --work-tree
// wins over core.worktree, so git writes the files in dest. git
// resolves a relative --work-tree against the directory of -C, so
// dest must be absolute.
func sparseCheckoutArgs(opts []string, dest string, paths []string) []string {
	return slices.Concat(opts, []string{"--work-tree=" + dest, "-C", dest, "sparse-checkout", "set"}, paths)
}

// checkSparsePaths validates each path in paths for use with
// git sparse-checkout. Empty paths, dash prefixes, absolute
// paths, ".." segments, and control characters are rejected.
func checkSparsePaths(paths []string) error {
	for _, p := range paths {
		if p == "" {
			return fmt.Errorf("%w: empty path", ErrDeniedSparsePath)
		}

		if strings.HasPrefix(p, "-") {
			return fmt.Errorf("%w: %q starts with '-'", ErrDeniedSparsePath, p)
		}

		if strings.HasPrefix(p, "/") {
			return fmt.Errorf("%w: %q is absolute", ErrDeniedSparsePath, p)
		}

		if strings.ContainsAny(p, "\x00\n\r") {
			return fmt.Errorf("%w: %q contains control characters", ErrDeniedSparsePath, p)
		}

		if slices.Contains(strings.Split(p, "/"), "..") {
			return fmt.Errorf("%w: %q contains '..'", ErrDeniedSparsePath, p)
		}
	}

	return nil
}

// buildCloneArgs converts a [CloneInput] into the argument list for git clone.
func buildCloneArgs(input CloneInput) []string {
	args := []string{"clone", "-q"}

	if input.Depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", input.Depth))
	}

	if input.Branch != "" {
		args = append(args, "--branch", input.Branch)
	}

	if input.SingleBranch {
		args = append(args, "--single-branch")
	}

	if input.Sparse || len(input.SparsePaths) > 0 {
		args = append(args, "--sparse")

		if input.Depth == 0 {
			args = append(args, "--filter=blob:none")
		}
	}

	args = append(args, "--", input.URL, input.Dest)

	return args
}
