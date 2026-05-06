package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrValidate is returned for user-facing failures of the validate tool:
// invalid working directory, missing tofu binary, or non-zero exit from
// `tofu init` or `tofu validate`. The handler surfaces these as tool-level
// errors with [*mcp.CallToolResult.IsError] set to true; transport-layer
// failures bubble up as internal errors instead.
var ErrValidate = errors.New("validate")

// ValidateInput is the input schema for the validate tool.
type ValidateInput struct {
	WorkingDirectory string   `json:"working_directory"      jsonschema:"Absolute path to the directory containing OpenTofu / Terraform configuration to validate"`
	Init             bool     `json:"init,omitzero"          jsonschema:"When true, run 'tofu init -input=false -no-color -backend=false' before validation. Use this when providers or modules have not been fetched yet"`
	AllowedPaths     []string `json:"allowed_paths,omitzero" jsonschema:"Extra absolute paths to bind read-only inside the sandbox (e.g. shared module directories outside the working directory). Symlinks must be resolved by the caller; user-supplied symlinks outside the working directory are not traversed inside the sandbox"`
	MaxLength        int      `json:"max_length,omitzero"    jsonschema:"Maximum number of characters to return (default 5000)"`
	StartIndex       int      `json:"start_index,omitzero"   jsonschema:"Character offset to start reading from (default 0)"`
}

// validateOutput is the JSON shape produced by `tofu validate -json`.
// Field order is tuned for GC pointer-scan locality (slice header first).
type validateOutput struct {
	Diagnostics  []validateDiagnostic `json:"diagnostics"`
	ErrorCount   int                  `json:"error_count"`
	WarningCount int                  `json:"warning_count"`
	Valid        bool                 `json:"valid"`
}

// validateDiagnostic is one entry in [validateOutput.Diagnostics]. Field
// order is tuned for GC pointer-scan locality (pointer first).
type validateDiagnostic struct {
	Range    *validateRange `json:"range,omitempty"`
	Severity string         `json:"severity"`
	Summary  string         `json:"summary"`
	Detail   string         `json:"detail"`
}

// validateRange is the source-location field on [validateDiagnostic].
type validateRange struct {
	Filename string           `json:"filename"`
	Start    validateRangePos `json:"start"`
}

// validateRangePos is one end of a [validateRange].
type validateRangePos struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// tofuExecutor runs a tofu subcommand in a working directory and returns its
// stdout, stderr, exit code, and any non-exit error from
// [exec.CommandContext]. Implementations must respect ctx cancellation
// and apply policy via the configured [Sandbox].
type tofuExecutor interface {
	Run(ctx context.Context, dir string, policy Policy, args ...string) (stdout, stderr []byte, exitCode int, err error)
}

// maxTofuStreamBytes caps the amount of stdout or stderr [*execTofu.Run]
// will buffer per stream. This guards against runaway providers (e.g. plan
// output for a huge stack) or interactive prompts producing unbounded
// output. The limit is generous relative to typical plan sizes but small
// enough to keep peak memory predictable.
const maxTofuStreamBytes = 16 * 1024 * 1024

// execTofu is the production [tofuExecutor] backed by [exec.CommandContext].
type execTofu struct {
	bin     string
	sandbox Sandbox
}

// newExecTofu returns an [*execTofu] that invokes bin (resolved via PATH at
// run time when not absolute) under sandbox.
func newExecTofu(bin string, sandbox Sandbox) *execTofu {
	if sandbox == nil {
		sandbox = noopSandbox{}
	}

	return &execTofu{bin: bin, sandbox: sandbox}
}

// tofuCancelGrace is the grace period between cmd.Cancel firing SIGTERM
// and the runtime escalating to SIGKILL. On Darwin SIGTERM reaches tofu
// directly; on Linux it reaches bwrap, which propagates death via
// PR_SET_PDEATHSIG, so the effective grace window is shorter there.
const tofuCancelGrace = 10 * time.Second

// Run satisfies [tofuExecutor]. A non-zero exit is reported via the exitCode
// return; the err return is non-nil only for non-exit failures (binary not
// found, context cancellation, OS errors). Each stream is capped at
// [maxTofuStreamBytes] bytes; further output is silently dropped.
func (e *execTofu) Run(ctx context.Context, dir string, policy Policy, args ...string) ([]byte, []byte, int, error) {
	// The bin path comes from the operator-supplied --tofu-bin flag and
	// args are constructed by the handlers, not from raw model input.
	// Invoking the configured tofu binary with handler-built args is the
	// whole purpose of this type.
	cmd := exec.CommandContext(ctx, e.bin, args...) //nolint:gosec // intentional: see comment
	cmd.Dir = dir

	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = tofuCancelGrace

	err := e.sandbox.Wrap(cmd, policy)
	if err != nil {
		return nil, nil, -1, fmt.Errorf("wrapping tofu in sandbox: %w", err)
	}

	stdout := &cappedBuffer{limit: maxTofuStreamBytes}
	stderr := &cappedBuffer{limit: maxTofuStreamBytes}

	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err = cmd.Run()

	var exitErr *exec.ExitError

	switch {
	case err == nil:
		return stdout.Bytes(), stderr.Bytes(), 0, nil

	case errors.As(err, &exitErr):
		return stdout.Bytes(), stderr.Bytes(), exitErr.ExitCode(), nil

	default:
		return stdout.Bytes(), stderr.Bytes(), -1, err
	}
}

// cappedBuffer is an [io.Writer] that buffers up to limit bytes and discards
// the rest. It is used by [*execTofu.Run] to keep memory bounded against
// runaway tofu output.
type cappedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	remaining := c.limit - c.buf.Len()
	if remaining <= 0 {
		return len(p), nil
	}

	if len(p) > remaining {
		c.buf.Write(p[:remaining])
		return len(p), nil
	}

	c.buf.Write(p)

	return len(p), nil
}

func (c *cappedBuffer) Bytes() []byte { return c.buf.Bytes() }

// validateWorkingDir checks that dir is non-empty, absolute, exists on disk,
// and is a directory. It returns a sentinel-wrapped error suitable for
// passing straight to [*handler.toolError]; the caller is responsible for
// surfacing the result. Pre-checking before invoking tofu yields cleaner
// error messages than tofu's own diagnostics for these common mistakes.
func validateWorkingDir(dir string, sentinel error) error {
	if dir == "" {
		return fmt.Errorf("%w: working_directory is required", sentinel)
	}

	if !filepath.IsAbs(dir) {
		return fmt.Errorf("%w: working_directory must be an absolute path, got %q", sentinel, dir)
	}

	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("%w: stat working_directory %q: %w", sentinel, dir, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("%w: working_directory %q is not a directory", sentinel, dir)
	}

	return nil
}

// runInitStep runs the canonical `tofu init -input=false -no-color
// -backend=false` prelude shared by the validate and plan tools. tool names
// the MCP tool whose handler is invoking the prelude (used for logging and
// error surfacing); sentinel is the per-tool sentinel error; policy is the
// init-step [Policy] (already merged with any per-call AllowRead extras).
//
// The return shape is a three-state signal. The first return is true when
// the caller should stop and return the second/third returns; false means
// continue. When stopping, the second return is the [*mcp.CallToolResult]
// to surface (non-nil for user-facing failures), and the third is a
// transport-layer error to bubble up.
func (h *handler) runInitStep(
	ctx context.Context,
	dir, tool string,
	sentinel error,
	policy Policy,
) (bool, *mcp.CallToolResult, error) {
	stdout, stderr, code, err := h.tofu.Run(ctx, dir, policy,
		"init", "-input=false", "-no-color", "-backend=false",
	)
	if err != nil {
		r, _, e := h.execError(ctx, tool, sentinel, "init", err)
		return true, r, e
	}

	if code != 0 {
		if r, ok := h.classifyMissingBinary(ctx, tool, sentinel, "init", stderr, code); ok {
			return true, r, nil
		}

		r, _, e := h.toolError(ctx, tool,
			fmt.Errorf("%w: 'tofu init' exited with code %d:\n%s",
				sentinel, code, combineOutput(stdout, stderr)),
		)

		return true, r, e
	}

	h.logStderr(ctx, tool, "init", stderr)

	return false, nil, nil
}

// handleValidate is the handler for the validate tool.
func (h *handler) handleValidate(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in ValidateInput,
) (*mcp.CallToolResult, any, error) {
	wdErr := validateWorkingDir(in.WorkingDirectory, ErrValidate)
	if wdErr != nil {
		return h.toolError(ctx, toolValidate, wdErr)
	}

	dir := in.WorkingDirectory

	validatePolicy, extras, pathErr := h.buildPolicy(toolValidate, in.AllowedPaths)
	if pathErr != nil {
		return h.toolError(ctx, toolValidate, fmt.Errorf("%w: %w", ErrValidate, pathErr))
	}

	if in.Init {
		initPolicy := h.policyFor(toolInit)
		initPolicy.AllowRead = mergeAllowRead(initPolicy.AllowRead, extras)

		stop, r, initErr := h.runInitStep(ctx, dir, toolValidate, ErrValidate, initPolicy)
		if stop {
			return r, nil, initErr
		}
	}

	stdout, stderr, code, err := h.tofu.Run(ctx, dir, validatePolicy, "validate", "-json", "-no-color")
	if err != nil {
		return h.execError(ctx, toolValidate, ErrValidate, "validate", err)
	}

	if r, ok := h.classifyMissingBinary(ctx, toolValidate, ErrValidate, "validate", stderr, code); ok {
		return r, nil, nil
	}

	h.logStderr(ctx, toolValidate, "validate", stderr)

	text := renderValidateOutput(dir, stdout, stderr)

	return textResult(Truncate(text, in.StartIndex, in.MaxLength)), nil, nil
}

// renderValidateOutput parses stdout as `tofu validate -json` and renders
// the result as Markdown. When the body cannot be parsed as JSON the raw
// output is dumped verbatim via [renderValidateRaw] so the model still sees
// what tofu produced.
func renderValidateOutput(dir string, stdout, stderr []byte) string {
	var out validateOutput

	err := json.Unmarshal(stdout, &out)
	if err != nil {
		return renderValidateRaw(dir, stdout, stderr)
	}

	return renderValidate(dir, &out)
}

// execError maps a non-exit error from [tofuExecutor.Run] to either a
// user-facing tool result or an internal handler error. A missing tofu
// binary is wrapped under sentinel and routed through
// [*handler.toolError] so the model sees it; everything else (context
// cancellation, OS errors) bubbles up as an internal error.
func (h *handler) execError(
	ctx context.Context,
	tool string,
	sentinel error,
	sub string,
	err error,
) (*mcp.CallToolResult, any, error) {
	if errors.Is(err, exec.ErrNotFound) {
		return h.toolError(ctx, tool,
			fmt.Errorf("%w: tofu binary not found in PATH; pass --tofu-bin to override (running %q): %w",
				sentinel, sub, err),
		)
	}

	return nil, nil, fmt.Errorf("running tofu %s: %w", sub, err)
}

// classifyMissingBinary returns a user-facing tool result when the wrapper
// exit code 127 plus "command not found" or "No such file" stderr
// together suggest the tofu binary (or the sandbox wrapper itself) was
// missing. Both shells (sandbox-exec via execvp, bwrap) emit one of
// these on a missing argv[0]. Returns ok=false when the signal does not
// match so callers fall back to their generic error rendering.
func (h *handler) classifyMissingBinary(
	ctx context.Context,
	tool string,
	sentinel error,
	sub string,
	stderr []byte,
	code int,
) (*mcp.CallToolResult, bool) {
	if code != 127 {
		return nil, false
	}

	body := string(stderr)
	if !strings.Contains(body, "command not found") && !strings.Contains(body, "No such file") {
		return nil, false
	}

	r, _, _ := h.toolError(ctx, tool,
		fmt.Errorf("%w: tofu binary not found in PATH; pass --tofu-bin to override (running %q): %s",
			sentinel, sub, strings.TrimSpace(body)),
	)

	return r, true
}

// resolveAllowedPaths runs each entry through [validateExtraPath]
// against h.allowRoot and returns the resolved paths in order. The
// returned slice is suitable for merging into a [Policy.AllowRead] list.
func (h *handler) resolveAllowedPaths(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	resolved := make([]string, 0, len(paths))

	for _, p := range paths {
		r, err := validateExtraPath(p, h.allowRoot)
		if err != nil {
			return nil, err
		}

		resolved = append(resolved, r)
	}

	return resolved, nil
}

// policyFor returns the named tool's [Policy], or a zero [Policy] when
// the tool is absent from h.policies. [Policy] is a value type, so the
// returned struct is independent of the map; the only mutation callers
// perform is reassigning AllowRead via [mergeAllowRead], which always
// returns a fresh slice — no defensive cloning needed.
func (h *handler) policyFor(tool string) Policy {
	return h.policies[tool]
}

// buildPolicy is the per-handler chokepoint that turns a tool name plus
// the caller-supplied AllowedPaths into the [Policy] handed to the
// sandbox. Resolution happens through [*handler.resolveAllowedPaths]; on
// error, the returned [Policy] is unspecified and callers must not use
// it.
func (h *handler) buildPolicy(tool string, allowedPaths []string) (Policy, []string, error) {
	extras, err := h.resolveAllowedPaths(allowedPaths)
	if err != nil {
		return Policy{}, nil, err
	}

	p := h.policyFor(tool)
	p.AllowRead = mergeAllowRead(p.AllowRead, extras)

	return p, extras, nil
}

// logStderr emits a structured warning when buf is non-empty so operators see
// anything tofu wrote to stderr alongside successful output. The tool argument
// names the MCP tool whose handler is logging; sub names the tofu subcommand.
// The recorded body is capped to keep individual log records compact even when
// tofu emits long provider-installation transcripts.
func (h *handler) logStderr(ctx context.Context, tool, sub string, buf []byte) {
	trimmed := bytes.TrimSpace(buf)
	if len(trimmed) == 0 {
		return
	}

	const logStderrCap = 4 * 1024

	body := trimmed
	if len(body) > logStderrCap {
		body = body[:logStderrCap]
	}

	h.log.WarnContext(ctx, "tofu stderr",
		slog.String("tool", tool),
		slog.String("subcommand", sub),
		slog.String("stderr", string(body)),
		slog.Int("stderr_total_bytes", len(trimmed)),
	)
}

// combineOutput renders stdout and stderr as a single block for embedding in
// error messages, with section headers when both are non-empty.
func combineOutput(stdout, stderr []byte) string {
	out := bytes.TrimSpace(stdout)
	errOut := bytes.TrimSpace(stderr)

	switch {
	case len(out) == 0 && len(errOut) == 0:
		return "(no output)"

	case len(out) == 0:
		return string(errOut)

	case len(errOut) == 0:
		return string(out)

	default:
		return fmt.Sprintf("stdout:\n%s\n\nstderr:\n%s", out, errOut)
	}
}

// appendOutputSection writes a fenced Markdown section containing the trimmed
// body under the named header. It is a no-op when body is empty after
// trimming. The leading blank line keeps consecutive sections visually
// separated when called repeatedly on the same builder.
func appendOutputSection(b *strings.Builder, header string, body []byte) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return
	}

	fmt.Fprintf(b, "\n### %s\n```\n", header)
	b.Write(trimmed)
	b.WriteString("\n```\n")
}

// renderValidate formats parsed [validateOutput] as Markdown.
func renderValidate(dir string, out *validateOutput) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## Validation: %s\n\n", dir)
	fmt.Fprintf(&b, "**Valid**: %t\n", out.Valid)
	fmt.Fprintf(&b, "**Errors**: %d\n", out.ErrorCount)
	fmt.Fprintf(&b, "**Warnings**: %d\n", out.WarningCount)

	if out.ErrorCount == 0 && out.WarningCount == 0 {
		b.WriteString("\nOpenTofu validation succeeded with no issues.\n")

		return b.String()
	}

	var errs, warns []validateDiagnostic

	for _, d := range out.Diagnostics {
		switch d.Severity {
		case "error":
			errs = append(errs, d)
		case "warning":
			warns = append(warns, d)
		}
	}

	if len(errs) > 0 {
		b.WriteString("\n### Errors\n")
		writeDiagnostics(&b, errs)
	}

	if len(warns) > 0 {
		b.WriteString("\n### Warnings\n")
		writeDiagnostics(&b, warns)
	}

	return b.String()
}

// renderValidateRaw is the parse-failure fallback: dump stdout/stderr verbatim
// so the model still sees what tofu produced.
func renderValidateRaw(dir string, stdout, stderr []byte) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## Validation: %s\n\n", dir)
	b.WriteString("Could not parse `tofu validate -json` output. Raw output follows.\n")

	appendOutputSection(&b, "stdout", stdout)
	appendOutputSection(&b, "stderr", stderr)

	return b.String()
}

// writeDiagnostics writes a Markdown bullet list of diagnostics into b.
func writeDiagnostics(b *strings.Builder, diags []validateDiagnostic) {
	for _, d := range diags {
		if d.Range != nil && d.Range.Filename != "" {
			fmt.Fprintf(b, "- `%s:%d`: %s\n", d.Range.Filename, d.Range.Start.Line, d.Summary)
		} else {
			fmt.Fprintf(b, "- %s\n", d.Summary)
		}

		if d.Detail != "" {
			for line := range strings.SplitSeq(strings.TrimRight(d.Detail, "\n"), "\n") {
				fmt.Fprintf(b, "  %s\n", line)
			}
		}
	}
}
