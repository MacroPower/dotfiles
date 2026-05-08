package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrInit is returned for user-facing failures of the init tool: invalid
// working directory, missing tofu binary, or non-zero exit from `tofu init`.
// The handler surfaces these as tool-level errors with
// [*mcp.CallToolResult.IsError] set to true; transport-layer failures bubble
// up as internal errors instead.
var ErrInit = errors.New("init")

// InitInput is the input schema for the init tool.
type InitInput struct {
	WorkingDirectory string   `json:"working_directory"      jsonschema:"Absolute path to the directory containing OpenTofu / Terraform configuration to initialize"`
	Backend          bool     `json:"backend,omitzero"       jsonschema:"When true, configure the backend (default false: passes -backend=false to keep init local and avoid credential prompts)"`
	Upgrade          bool     `json:"upgrade,omitzero"       jsonschema:"When true, pass -upgrade to fetch the latest module/provider versions allowed by version constraints"`
	AllowedPaths     []string `json:"allowed_paths,omitzero" jsonschema:"Extra absolute paths to bind read-only inside the sandbox (e.g. shared module directories outside the working directory). Symlinks must be resolved by the caller; user-supplied symlinks outside the working directory are not traversed inside the sandbox"`
}

func (h *handler) handleInit(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in InitInput,
) (*mcp.CallToolResult, any, error) {
	wdErr := validateWorkingDir(in.WorkingDirectory, ErrInit)
	if wdErr != nil {
		return h.toolError(ctx, toolRunInit, wdErr)
	}

	dir := in.WorkingDirectory

	policy, _, pathErr := h.buildPolicy(toolRunInit, in.AllowedPaths)
	if pathErr != nil {
		return h.toolError(ctx, toolRunInit, fmt.Errorf("%w: %w", ErrInit, pathErr))
	}

	args := []string{"init", "-input=false", "-no-color", fmt.Sprintf("-backend=%t", in.Backend)}
	if in.Upgrade {
		args = append(args, "-upgrade")
	}

	stdout, stderr, code, err := h.tofu.Run(ctx, dir, policy, args...)
	if err != nil {
		return h.execError(ctx, toolRunInit, ErrInit, "init", err)
	}

	if code != 0 {
		if r, ok := h.classifyMissingBinary(ctx, toolRunInit, ErrInit, "init", stderr, code); ok {
			return r, nil, nil
		}

		return h.toolError(ctx, toolRunInit,
			fmt.Errorf("%w: 'tofu init' exited with code %d:\n%s",
				ErrInit, code, combineOutput(stdout, stderr)),
		)
	}

	h.logStderr(ctx, toolRunInit, "init", stderr)

	return textResult(renderInit(dir, stdout, stderr)), nil, nil
}

func renderInit(dir string, stdout, stderr []byte) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## OpenTofu Init: %s\n\n", dir)
	b.WriteString("OpenTofu initialization succeeded.\n")

	appendOutputSection(&b, "Output", stdout)
	appendOutputSection(&b, "Notices (stderr)", stderr)

	return b.String()
}
