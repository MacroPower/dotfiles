package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrTest is returned for user-facing failures of the test tool: invalid
// working directory, missing tofu binary, or non-zero exit from a preceding
// `tofu init`. A non-zero exit from `tofu test` itself is rendered as
// regular output, since `tofu test` returns non-zero when assertions fail
// and that is normal test output rather than a tool failure.
var ErrTest = errors.New("test")

// TestInput is the input schema for the test tool.
type TestInput struct {
	WorkingDirectory string   `json:"working_directory"      jsonschema:"Absolute path to the directory containing OpenTofu / Terraform configuration with .tftest.hcl or .tofutest.hcl files"`
	Init             bool     `json:"init,omitzero"          jsonschema:"When true, run 'tofu init -input=false -no-color -backend=false' before testing. Use this when providers or modules have not been fetched yet"`
	TestDirectory    string   `json:"test_directory,omitzero" jsonschema:"Test directory passed as -test-directory; resolved relative to working_directory. When omitted, OpenTofu searches the working_directory and a sibling 'tests' directory"`
	Filter           []string `json:"filter,omitzero"        jsonschema:"Restrict execution to specific test files. Each entry becomes a -filter=<value> argument; paths are resolved relative to working_directory"`
	Verbose          bool     `json:"verbose,omitzero"       jsonschema:"When true, pass -verbose so the plan or state for each run block is printed as it executes"`
	AllowedPaths     []string `json:"allowed_paths,omitzero" jsonschema:"Extra absolute paths to bind read-only inside the sandbox (e.g. shared module directories outside the working directory). Symlinks must be resolved by the caller; user-supplied symlinks outside the working directory are not traversed inside the sandbox"`
	MaxLength        int      `json:"max_length,omitzero"    jsonschema:"Maximum number of characters to return (default 5000)"`
	StartIndex       int      `json:"start_index,omitzero"   jsonschema:"Character offset to start reading from (default 0)"`
}

// handleTest is the handler for the test tool.
func (h *handler) handleTest(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in TestInput,
) (*mcp.CallToolResult, any, error) {
	wdErr := validateWorkingDir(in.WorkingDirectory, ErrTest)
	if wdErr != nil {
		return h.toolError(ctx, toolRunTest, wdErr)
	}

	dir := in.WorkingDirectory

	testPolicy, extras, pathErr := h.buildPolicy(toolRunTest, in.AllowedPaths)
	if pathErr != nil {
		return h.toolError(ctx, toolRunTest, fmt.Errorf("%w: %w", ErrTest, pathErr))
	}

	if in.Init {
		initPolicy := h.policyFor(toolRunInit)
		initPolicy.AllowRead = mergeAllowRead(initPolicy.AllowRead, extras)

		stop, r, initErr := h.runInitStep(ctx, dir, toolRunTest, ErrTest, initPolicy)
		if stop {
			return r, nil, initErr
		}
	}

	args := buildTestArgs(in)

	stdout, stderr, code, err := h.tofu.Run(ctx, dir, testPolicy, args...)
	if err != nil {
		return h.execError(ctx, toolRunTest, ErrTest, "test", err)
	}

	if r, ok := h.classifyMissingBinary(ctx, toolRunTest, ErrTest, "test", stderr, code); ok {
		return r, nil, nil
	}

	h.logStderr(ctx, toolRunTest, "test", stderr)

	text := renderTest(dir, code, stdout, stderr)

	return textResult(Truncate(text, in.StartIndex, in.MaxLength)), nil, nil
}

// buildTestArgs assembles the argv passed to `tofu test`. Order is fixed
// (subcommand, -no-color, -test-directory, -filter*, -verbose) so tests can
// assert on the exact slice.
func buildTestArgs(in TestInput) []string {
	args := []string{"test", "-no-color"}

	if in.TestDirectory != "" {
		args = append(args, fmt.Sprintf("-test-directory=%s", in.TestDirectory))
	}

	for _, f := range in.Filter {
		args = append(args, fmt.Sprintf("-filter=%s", f))
	}

	if in.Verbose {
		args = append(args, "-verbose")
	}

	return args
}

// renderTest formats `tofu test` output as Markdown. The exit code is shown
// in the headline so the model can read pass/fail without parsing the
// transcript; non-zero codes mean test failures, not tool failures, and are
// surfaced with IsError=false.
func renderTest(dir string, exitCode int, stdout, stderr []byte) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## OpenTofu Test: %s\n\n", dir)
	fmt.Fprintf(&b, "OpenTofu test exited with code %d.\n", exitCode)

	appendOutputSection(&b, "Output", stdout)
	appendOutputSection(&b, "Notices (stderr)", stderr)

	return b.String()
}
