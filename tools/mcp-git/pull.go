package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrPull wraps the underlying git pull failure.
var ErrPull = errors.New("git pull failed")

// PullInput is the JSON input schema for the git_pull tool.
type PullInput struct {
	Repo   string `json:"repo"            jsonschema:"Path to an existing local git repository"`
	Remote string `json:"remote,omitzero" jsonschema:"Remote name to pull from (default origin)"`
	Branch string `json:"branch,omitzero" jsonschema:"Branch to pull; omit to use the current branch's upstream"`
	Rebase bool   `json:"rebase,omitzero" jsonschema:"Replay local commits on top of the fetched branch instead of requiring a fast-forward"`

	// TimeoutSeconds overrides the server default for this call.
	// Zero means use the default; per-call disable is not supported.
	TimeoutSeconds int `json:"timeout_seconds,omitzero" jsonschema:"Override the default per-operation timeout in seconds; 0 means use the server default"`
}

func (h *handler) handlePull(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input PullInput,
) (*mcp.CallToolResult, any, error) {
	remote := remoteOrDefault(input.Remote)

	inputErr := checkRemoteInputs(input.Repo, remote, input.Branch)
	if inputErr != nil {
		return toolError(inputErr), nil, nil
	}

	op := remoteOp{
		repo:    input.Repo,
		remote:  remote,
		timeout: input.TimeoutSeconds,
		args:    buildPullArgs(remote, input),
	}

	// A conflicting rebase exits non-zero and would leave the
	// repository mid-rebase; restore it so the caller never has to
	// know about rebase state.
	if input.Rebase {
		op.cleanupArgs = []string{"rebase", "--abort"}
	}

	out, err := h.runRemote(ctx, op)
	if err != nil {
		return toolError(fmt.Errorf("%w: %w", ErrPull, err)), nil, nil
	}

	return remoteResult(
		fmt.Sprintf("Pulled from %s in %s", remote, input.Repo),
		out,
	), nil, nil
}

// buildPullArgs converts a [PullInput] into the argument list for
// git pull, targeting the already-resolved remote name. Without
// Rebase the pull is fast-forward only, matching the pull that
// git_clone performs on an existing destination.
func buildPullArgs(remote string, input PullInput) []string {
	args := []string{"pull"}

	if input.Rebase {
		args = append(args, "--rebase")
	} else {
		args = append(args, "--ff-only")
	}

	args = append(args, "--", remote)

	if input.Branch != "" {
		args = append(args, input.Branch)
	}

	return args
}
