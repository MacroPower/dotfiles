package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "0.2.0"

// ErrEmptyAllowDir reports an --allow-dir flag with an empty value.
var ErrEmptyAllowDir = errors.New("--allow-dir must not be empty")

// stringSlice implements [flag.Value] for repeatable string
// flags.
type stringSlice []string

func (s *stringSlice) String() string { return fmt.Sprintf("%v", *s) }

func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// resolveAllowDirs makes each allowed directory absolute and resolves
// its symbolic links, so that [handler.checkDest] compares two
// absolute, resolved paths. Both functions resolve a path with
// [resolveExistingPrefix]. The two paths therefore also match for an
// allowed directory that does not exist when the server starts and
// that has a symbolic link on its path.
//
// resolveAllowDirs passes an absolute directory to
// [resolveExistingPrefix] as the flag gave it. A ".." after a symbolic
// link therefore leads to the parent of the target of the link,
// whether or not the target or the elements after the ".." exist.
// [handler.checkDest] cleans a path as text before it resolves the
// links. A ".." in that path therefore drops the element before it,
// also when that element is a symbolic link.
//
// resolveAllowDirs reads a relative directory with [filepath.Abs], as
// [handler.checkDest] reads a relative path. [filepath.Abs] joins the
// directory to the working directory that [os.Getwd] reports, which
// is $PWD when $PWD names the working directory. It then cleans the
// path as text, so each ".." drops the element before it, also when
// that element is a symbolic link. A ".." at the start of the
// directory therefore leads to the parent of $PWD. resolveAllowDirs
// returns an error when [filepath.Abs] fails for a relative directory.
//
// resolveAllowDirs returns [ErrEmptyAllowDir] for an empty directory.
// A shell passes an empty value when it expands an unset variable in
// quotes, as in --allow-dir "$DIR", or after an equals sign, as in
// --allow-dir=$DIR. resolveAllowDirs therefore does not read an empty
// value as the working directory.
func resolveAllowDirs(dirs []string) ([]string, error) {
	resolved := make([]string, 0, len(dirs))

	for _, dir := range dirs {
		if dir == "" {
			return nil, ErrEmptyAllowDir
		}

		// checkDest makes a relative path absolute with filepath.Abs.
		// A relative dir takes the same step, so that checkDest allows
		// a relative path that starts with dir, such as dir/repo.
		if !filepath.IsAbs(dir) {
			abs, err := filepath.Abs(dir)
			if err != nil {
				return nil, fmt.Errorf("resolving --allow-dir %q: %w", dir, err)
			}

			dir = abs
		}

		resolved = append(resolved, resolveExistingPrefix(dir))
	}

	return resolved, nil
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

	dirs, err := resolveAllowDirs(allowDirs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcp-git: %v\n", err)
		os.Exit(1)
	}

	h := &handler{
		allowDirs:     dirs,
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

	err = srv.Run(ctx, &mcp.StdioTransport{})

	cancel()

	if err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
