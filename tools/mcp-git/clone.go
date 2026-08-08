package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

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
	if statErr == nil && info.IsDir() {
		return h.pullExisting(ctx, input)
	}

	return h.clone(ctx, input)
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
		repo:    input.Dest,
		remote:  defaultRemote,
		timeout: input.TimeoutSeconds,
		args:    buildPullArgs(defaultRemote, PullInput{Repo: input.Dest}),
	})
	if err != nil {
		return toolError(fmt.Errorf("pulling latest changes in %s: %w", input.Dest, err)), nil, nil
	}

	return remoteResult(
		fmt.Sprintf("Pulled latest changes in %s", input.Dest),
		out,
	), nil, nil
}

// checkOrigin verifies that the existing repo at dest has an
// origin remote URL matching url. Both sides are normalized by
// stripping a trailing ".git" suffix before comparison.
func (h *handler) checkOrigin(ctx context.Context, url, dest string) error {
	out, err := gitValue(ctx, "-C", dest, "remote", "get-url", "origin")
	if err != nil {
		if errors.Is(err, ErrTimeout) {
			return err
		}

		return fmt.Errorf("%w: reading origin: %w", ErrOriginMismatch, err)
	}

	got := strings.TrimSuffix(out, ".git")
	want := strings.TrimSuffix(url, ".git")

	if got != want {
		return fmt.Errorf("%w: got %s, want %s", ErrOriginMismatch, got, want)
	}

	return nil
}

//nolint:unparam // signature matches mcp.AddTool handler contract.
func (h *handler) clone(ctx context.Context, input CloneInput) (*mcp.CallToolResult, any, error) {
	args := h.credentialArgs(input.URL)
	args = append(args, buildCloneArgs(input)...)

	_, err := runGit(ctx, args...)
	if err != nil {
		return toolError(fmt.Errorf("%w: %w", ErrClone, err)), nil, nil
	}

	if len(input.SparsePaths) > 0 {
		scArgs := []string{"-C", input.Dest, "sparse-checkout", "set"}
		scArgs = append(scArgs, input.SparsePaths...)

		_, scErr := runGit(ctx, scArgs...)
		if scErr != nil {
			return toolError(fmt.Errorf("setting sparse checkout: %w", scErr)), nil, nil
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
