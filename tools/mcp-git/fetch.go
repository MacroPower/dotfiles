package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrFetch wraps the underlying git fetch failure.
var ErrFetch = errors.New("git fetch failed")

// FetchInput is the JSON input schema for the git_fetch tool.
type FetchInput struct {
	Repo   string `json:"repo"            jsonschema:"Path to an existing local git repository"`
	Remote string `json:"remote,omitzero" jsonschema:"Remote name to fetch from (default origin)"`
	Ref    string `json:"ref,omitzero"    jsonschema:"Single branch or tag name to fetch; omit to fetch the remote's configured refspec"`
	Prune  bool   `json:"prune,omitzero"  jsonschema:"Delete remote-tracking refs that no longer exist on the remote"`
	Tags   bool   `json:"tags,omitzero"   jsonschema:"Also fetch all tags from the remote"`

	// TimeoutSeconds overrides the server default for this call.
	// Zero means use the default; per-call disable is not supported.
	TimeoutSeconds int `json:"timeout_seconds,omitzero" jsonschema:"Override the default per-operation timeout in seconds; 0 means use the server default"`
}

func (h *handler) handleFetch(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input FetchInput,
) (*mcp.CallToolResult, any, error) {
	remote := remoteOrDefault(input.Remote)

	inputErr := checkRemoteInputs(input.Repo, remote, input.Ref)
	if inputErr != nil {
		return toolError(inputErr), nil, nil
	}

	out, err := h.runRemote(ctx, remoteOp{
		repo:    input.Repo,
		remote:  remote,
		timeout: input.TimeoutSeconds,
		args:    buildFetchArgs(remote, input),
	})
	if err != nil {
		return toolError(fmt.Errorf("%w: %w", ErrFetch, err)), nil, nil
	}

	return remoteResult(
		fmt.Sprintf("Fetched from %s in %s", remote, input.Repo),
		out,
	), nil, nil
}

// buildFetchArgs converts a [FetchInput] into the argument list for
// git fetch, targeting the already-resolved remote name.
//
// The list always holds the options of [fetchGuardArgs].
func buildFetchArgs(remote string, input FetchInput) []string {
	args := append([]string{"fetch"}, fetchGuardArgs()...)

	if input.Prune {
		args = append(args, "--prune")
	}

	if input.Tags {
		args = append(args, "--tags")
	}

	args = append(args, "--", remote)

	if input.Ref != "" {
		args = append(args, input.Ref)
	}

	return args
}

// fetchGuardArgs returns the options that git fetch and git pull take
// so that the fetch runs no command from the git config of a
// repository.
//
// --no-recurse-submodules keeps git from fetching a submodule. git
// fetches a submodule with a git command of its own, which reads the
// git config of the submodule. That config defines hooks that
// [handler.gitOptions] does not know by name. For a pull, the option
// also keeps git from updating the working tree of a submodule.
//
// --upload-pack names the program that serves the fetch. Its value is
// the default of git, and it wins over remote.<name>.uploadpack. git
// runs that program on the host of an ssh remote.
func fetchGuardArgs() []string {
	return []string{"--no-recurse-submodules", "--upload-pack=git-upload-pack"}
}
