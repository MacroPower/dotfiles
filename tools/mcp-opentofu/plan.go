package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrPlan is returned for user-facing failures of the plan tool: invalid
// working directory, missing tofu binary, or non-zero exit from `tofu init`
// or `tofu plan`. The handler surfaces these as tool-level errors with
// [*mcp.CallToolResult.IsError] set to true; transport-layer failures bubble
// up as internal errors instead.
var ErrPlan = errors.New("plan")

// PlanInput is the input schema for the plan tool.
type PlanInput struct {
	WorkingDirectory string   `json:"working_directory"      jsonschema:"Absolute path to the directory containing OpenTofu / Terraform configuration to plan"`
	Init             bool     `json:"init,omitzero"          jsonschema:"When true, run 'tofu init -input=false -no-color -backend=false' before planning. Use when providers or modules have not yet been fetched"`
	Destroy          bool     `json:"destroy,omitzero"       jsonschema:"When true, plan a destroy run (-destroy) instead of a normal apply plan"`
	RefreshOnly      bool     `json:"refresh_only,omitzero"  jsonschema:"When true, run a refresh-only plan (-refresh-only) that detects drift without proposing configuration changes"`
	AllowedPaths     []string `json:"allowed_paths,omitzero" jsonschema:"Extra absolute paths to bind read-only inside the sandbox (e.g. shared module directories outside the working directory). Symlinks must be resolved by the caller; user-supplied symlinks outside the working directory are not traversed inside the sandbox"`
	MaxLength        int      `json:"max_length,omitzero"    jsonschema:"Maximum number of characters to return (default 5000)"`
	StartIndex       int      `json:"start_index,omitzero"   jsonschema:"Character offset to start reading from (default 0)"`
}

func (h *handler) handlePlan(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in PlanInput,
) (*mcp.CallToolResult, any, error) {
	wdErr := validateWorkingDir(in.WorkingDirectory, ErrPlan)
	if wdErr != nil {
		return h.toolError(ctx, toolPlan, wdErr)
	}

	dir := in.WorkingDirectory

	planPolicy, extras, pathErr := h.buildPolicy(toolPlan, in.AllowedPaths)
	if pathErr != nil {
		return h.toolError(ctx, toolPlan, fmt.Errorf("%w: %w", ErrPlan, pathErr))
	}

	if in.Init {
		initPolicy := h.policyFor(toolInit)
		initPolicy.AllowRead = mergeAllowRead(initPolicy.AllowRead, extras)

		stop, r, initErr := h.runInitStep(ctx, dir, toolPlan, ErrPlan, initPolicy)
		if stop {
			return r, nil, initErr
		}
	}

	args := []string{"plan", "-input=false", "-no-color", "-detailed-exitcode"}
	if in.Destroy {
		args = append(args, "-destroy")
	}

	if in.RefreshOnly {
		args = append(args, "-refresh-only")
	}

	stdout, stderr, code, err := h.tofu.Run(ctx, dir, planPolicy, args...)
	if err != nil {
		return h.execError(ctx, toolPlan, ErrPlan, "plan", err)
	}

	h.logStderr(ctx, toolPlan, "plan", stderr)

	var hasChanges bool

	switch code {
	case 0:
		hasChanges = false
	case 2:
		hasChanges = true
	default:
		if r, ok := h.classifyMissingBinary(ctx, toolPlan, ErrPlan, "plan", stderr, code); ok {
			return r, nil, nil
		}

		return h.toolError(ctx, toolPlan,
			fmt.Errorf("%w: 'tofu plan' exited with code %d:\n%s",
				ErrPlan, code, combineOutput(stdout, stderr)),
		)
	}

	text := renderPlan(dir, in, hasChanges, stdout, stderr)

	return textResult(Truncate(text, in.StartIndex, in.MaxLength)), nil, nil
}

// renderPlan formats the rendered plan output as Markdown. hasChanges is
// derived from the `-detailed-exitcode` exit code (0 = no changes, 2 =
// changes / drift); under -refresh-only it is reported as drift detection
// instead of a pending apply.
func renderPlan(dir string, in PlanInput, hasChanges bool, stdout, stderr []byte) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## OpenTofu Plan: %s\n\n", dir)

	switch {
	case in.Destroy:
		b.WriteString("**Mode**: destroy\n")
	case in.RefreshOnly:
		b.WriteString("**Mode**: refresh-only\n")
	}

	if in.RefreshOnly {
		fmt.Fprintf(&b, "**Drift detected**: %t\n", hasChanges)
	} else {
		fmt.Fprintf(&b, "**Changes pending**: %t\n", hasChanges)
	}

	appendOutputSection(&b, "Plan output", stdout)
	appendOutputSection(&b, "Notices (stderr)", stderr)

	return b.String()
}
