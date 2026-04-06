package main

import (
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

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	// ErrMissingURL is returned when the URL field is empty.
	ErrMissingURL = errors.New("url is required")

	// ErrMissingDest is returned when the dest field is empty.
	ErrMissingDest = errors.New("dest is required")

	// ErrDeniedDest is returned when dest is outside all
	// allowed directories.
	ErrDeniedDest = errors.New("dest not under any allowed directory")

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

	// ErrClone wraps the underlying git clone failure.
	ErrClone = errors.New("git clone failed")

	// scpPattern matches SCP-style git URLs (e.g., git@github.com:org/repo).
	scpPattern = regexp.MustCompile(`^\w+@[\w.-]+:`)

	// safeSchemes lists the URL prefixes accepted by [cloneHandler.checkURL].
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
	URL          string   `json:"url"                    jsonschema:"Repository URL to clone"`
	Dest         string   `json:"dest"                   jsonschema:"Destination directory path"`
	Branch       string   `json:"branch,omitzero"        jsonschema:"Branch to clone"`
	Ref          string   `json:"ref,omitzero"           jsonschema:"Branch or tag to check out (alias for branch)"`
	Depth        int      `json:"depth,omitzero"         jsonschema:"Shallow clone depth"`
	SingleBranch bool     `json:"single_branch,omitzero" jsonschema:"Clone only the specified branch"`
	Sparse       bool     `json:"sparse,omitzero"        jsonschema:"Enable sparse checkout (only root files unless sparse_paths is set)"`
	SparsePaths  []string `json:"sparse_paths,omitzero"  jsonschema:"Paths for sparse checkout (implies sparse)"`
}

// cloneHandler implements the git_clone tool handler.
type cloneHandler struct {
	allowDirs     []string
	allowInsecure bool   // permit http:// and git:// URLs
	allowFileURLs bool   // testing only: permit file:// and local path URLs
	token         string // GitHub personal access token for HTTPS auth
}

func (h *cloneHandler) handle(
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

	if err := checkSparsePaths(input.SparsePaths); err != nil {
		return toolError(err), nil, nil
	}

	destErr := h.checkDest(input.Dest)
	if destErr != nil {
		return toolError(destErr), nil, nil
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
	if statErr == nil && info.IsDir() {
		return h.pull(ctx, input.URL, input.Dest)
	}

	return h.clone(ctx, input)
}

// credentialArgs returns git -c flags that configure a
// credential helper for GitHub HTTPS URLs. It returns nil when
// no token is set or the URL is not an HTTPS GitHub URL.
func (h *cloneHandler) credentialArgs(url string) []string {
	if h.token == "" || !strings.HasPrefix(url, "https://github.com/") {
		return nil
	}

	return []string{
		"-c", "credential.helper=",
		"-c", `credential.https://github.com.helper=!f() { echo username=x-access-token; echo password=$GH_TOKEN; }; f`,
	}
}

// gitEnv returns the environment for git subprocesses. It
// preserves the inherited environment and adds
// GIT_TERMINAL_PROMPT=0 to prevent interactive prompts.
func gitEnv() []string {
	return append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
}

// checkURL verifies that url uses a permitted scheme. Accepted
// forms are https, ssh, and SCP-style (user@host:path).
// Unencrypted schemes (http, git) require allowInsecure. File
// URLs and local paths are rejected unless allowFileURLs is set.
func (h *cloneHandler) checkURL(url string) error {
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

// checkDest verifies that dest is under one of the allowed
// directories. If no directories are configured, all paths
// are accepted. Symlinks along the path are resolved to
// prevent directory escapes.
func (h *cloneHandler) checkDest(dest string) error {
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

//nolint:unparam // signature matches mcp.AddTool handler contract.
func (h *cloneHandler) pull(ctx context.Context, url, dest string) (*mcp.CallToolResult, any, error) {
	originErr := h.checkOrigin(ctx, url, dest)
	if originErr != nil {
		return toolError(originErr), nil, nil
	}

	args := h.credentialArgs(url)
	args = append(args, "-C", dest, "pull", "--ff-only", "-q")

	//nolint:gosec // G204: dest is user-provided input.
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = gitEnv()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		slog.WarnContext(ctx, "pulling latest changes", slog.Any("error", err))

		return toolError(fmt.Errorf("pulling latest changes in %s: %w", dest, err)), nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{
			Text: fmt.Sprintf("Pulled latest changes in %s", dest),
		}},
	}, nil, nil
}

// checkOrigin verifies that the existing repo at dest has an
// origin remote URL matching url. Both sides are normalized by
// stripping a trailing ".git" suffix before comparison.
func (h *cloneHandler) checkOrigin(ctx context.Context, url, dest string) error {
	//nolint:gosec // G204: dest is user-provided input.
	cmd := exec.CommandContext(ctx, "git", "-C", dest, "remote", "get-url", "origin")
	cmd.Env = gitEnv()

	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("%w: reading origin: %w", ErrOriginMismatch, err)
	}

	got := strings.TrimSuffix(strings.TrimSpace(string(out)), ".git")
	want := strings.TrimSuffix(url, ".git")

	if got != want {
		return fmt.Errorf("%w: got %s, want %s", ErrOriginMismatch, got, want)
	}

	return nil
}

//nolint:unparam // signature matches mcp.AddTool handler contract.
func (h *cloneHandler) clone(ctx context.Context, input CloneInput) (*mcp.CallToolResult, any, error) {
	args := h.credentialArgs(input.URL)
	args = append(args, buildCloneArgs(input)...)

	//nolint:gosec // G204: args are user-provided input.
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = gitEnv()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return toolError(fmt.Errorf("%w: %w", ErrClone, err)), nil, nil
	}

	if len(input.SparsePaths) > 0 {
		scArgs := []string{"-C", input.Dest, "sparse-checkout", "set"}
		scArgs = append(scArgs, input.SparsePaths...)

		//nolint:gosec // G204: sparse paths are validated by checkSparsePaths.
		scCmd := exec.CommandContext(ctx, "git", scArgs...)
		scCmd.Env = gitEnv()
		scCmd.Stdout = os.Stdout
		scCmd.Stderr = os.Stderr

		if scErr := scCmd.Run(); scErr != nil {
			return toolError(fmt.Errorf("sparse checkout failed: %w", scErr)), nil, nil
		}
	}

	msg := fmt.Sprintf("Cloned %s into %s", input.URL, input.Dest)
	if len(input.SparsePaths) > 0 {
		msg += fmt.Sprintf(" (sparse: %s)", strings.Join(input.SparsePaths, ", "))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}, nil, nil
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

		for _, seg := range strings.Split(p, "/") {
			if seg == ".." {
				return fmt.Errorf("%w: %q contains '..'", ErrDeniedSparsePath, p)
			}
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

// acquireLock takes an exclusive flock on dest.lock and returns a cleanup
// function that releases the lock and removes the file.
func acquireLock(dest string) (func(), error) {
	lockPath := dest + ".lock"

	lockFile, err := os.Create(lockPath) //nolint:gosec // G301: dest is user-provided input.
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
