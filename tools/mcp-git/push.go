package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrPush wraps the underlying git push failure.
var ErrPush = errors.New("git push failed")

// PushInput is the JSON input schema for the git_push tool.
type PushInput struct {
	Repo           string `json:"repo"                      jsonschema:"Path to an existing local git repository"`
	Remote         string `json:"remote,omitzero"           jsonschema:"Remote name to push to (default origin)"`
	Ref            string `json:"ref,omitzero"              jsonschema:"Branch or tag name to push; omit to push the current branch"`
	SetUpstream    bool   `json:"set_upstream,omitzero"     jsonschema:"Record the remote branch as the upstream of the pushed branch"`
	ForceWithLease bool   `json:"force_with_lease,omitzero" jsonschema:"Overwrite the remote branch only if it still matches the local remote-tracking ref"`
	Tags           bool   `json:"tags,omitzero"             jsonschema:"Also push all tags"`

	// TimeoutSeconds overrides the server default for this call.
	// Zero means use the default; per-call disable is not supported.
	TimeoutSeconds int `json:"timeout_seconds,omitzero" jsonschema:"Override the default per-operation timeout in seconds; 0 means use the server default"`
}

func (h *handler) handlePush(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input PushInput,
) (*mcp.CallToolResult, any, error) {
	remote := remoteOrDefault(input.Remote)

	inputErr := checkRemoteInputs(input.Repo, remote, input.Ref)
	if inputErr != nil {
		return toolError(inputErr), nil, nil
	}

	out, err := h.runRemote(ctx, remoteOp{
		repo:    input.Repo,
		remote:  remote,
		push:    true,
		timeout: input.TimeoutSeconds,
		args:    buildPushArgs(remote, input),
	})
	if err != nil {
		return toolError(fmt.Errorf("%w: %w", ErrPush, err)), nil, nil
	}

	return remoteResult(
		fmt.Sprintf("Pushed to %s from %s", remote, input.Repo),
		out,
	), nil, nil
}

// buildPushArgs converts a [PushInput] into the argument list for
// git push, targeting the already-resolved remote name. It never
// produces a delete refspec or a bare --force: Ref is validated as
// a plain branch or tag name by [checkRefName].
func buildPushArgs(remote string, input PushInput) []string {
	args := []string{"push"}

	if input.SetUpstream {
		args = append(args, "--set-upstream")
	}

	if input.ForceWithLease {
		args = append(args, "--force-with-lease")
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
