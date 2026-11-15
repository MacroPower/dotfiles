package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "0.2.0"

// stringSlice implements [flag.Value] for repeatable string
// flags.
type stringSlice []string

func (s *stringSlice) String() string { return fmt.Sprintf("%v", *s) }

func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// resolveAllowDirs resolves symlinks in each allow dir so that
// [handler.checkDest], which symlink-resolves the paths it checks,
// compares like with like. A dir that cannot be resolved is kept
// as given.
func resolveAllowDirs(dirs []string) []string {
	resolved := make([]string, 0, len(dirs))

	for _, dir := range dirs {
		r, err := filepath.EvalSymlinks(dir)
		if err != nil {
			r = dir
		}

		resolved = append(resolved, r)
	}

	return resolved
}

func main() {
	var allowDirs stringSlice

	flag.Var(
		&allowDirs, "allow-dir",
		"allowed repository or destination directory (repeatable)",
	)

	allowInsecure := flag.Bool(
		"allow-insecure", false,
		"permit unencrypted URL schemes (http, git)",
	)

	timeout := flag.Duration(
		"timeout", time.Minute,
		"max duration for a single git operation (0 disables)",
	)

	flag.Parse()

	h := &handler{
		allowDirs:     resolveAllowDirs(allowDirs),
		allowInsecure: *allowInsecure,
		timeout:       *timeout,
		token:         os.Getenv("GH_TOKEN"),
	}

	srv := mcp.NewServer(
		&mcp.Implementation{Name: "mcp-git", Version: version},
		nil,
	)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "git_clone",
		Description: "Clone a git repo. Idempotent: clones if the dest is absent, else fast-forward pulls. A dest whose .git directory is corrupt moves aside and is cloned again.",
	}, h.handleClone)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "git_fetch",
		Description: "Fetch from a remote of an existing local repo. Updates remote-tracking refs and never touches the working tree. Skips every hook of the repository. Never fetches a submodule, so fetch each submodule with its own call. Refuses a remote on the local file system. Refuses a repo whose own git config sets a credential helper, an http key, or the uploadpack, receivepack, or proxy key of a remote.",
	}, h.handleFetch)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "git_pull",
		Description: "Pull from a remote into an existing local repo. Fast-forward only unless rebase is set. Skips every hook of the repository. Never fetches or updates a submodule. Refuses a remote on the local file system. Also refuses a repo whose own git config sets a credential helper, an http key, the uploadpack, receivepack, or proxy key of a remote, a filter, a merge driver, a signing program, a git-lfs command, or lfs.storage. When the repo sets only a filter, a merge driver, a signing program, a git-lfs command, or lfs.storage, run git_fetch, and then merge or rebase in Bash.",
	}, h.handlePull)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "git_push",
		Description: "Push a branch or tag to a remote of an existing local repo. Updates only the ref of the same name on the remote, whatever the push config of the repo says, such as remote.<name>.push or push.default. Never sends a push option, whatever push.pushOption says. Supports set_upstream, force_with_lease, and tags. It has no bare --force and no remote-ref deletes. Skips every hook of the repository, so run the checks of a pre-push hook in Bash first. Never signs a push. Never pushes or checks a submodule, so push each new submodule commit with its own call first. Refuses a remote on the local file system. Refuses a repo whose own git config sets a credential helper, an http key, or the uploadpack, receivepack, or proxy key of a remote.",
	}, h.handlePush)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)

	err := srv.Run(ctx, &mcp.StdioTransport{})

	cancel()

	if err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
