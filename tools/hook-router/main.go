package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"maps"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// config holds runtime settings resolved from the environment.
type config struct {
	rtkRewrite        string
	dockerProxyEnsure string
	dockerProxyPort   string
}

func configFromEnv() config {
	port := os.Getenv("DOCKER_PROXY_PORT")
	if port == "" {
		port = "2375"
	}

	return config{
		rtkRewrite:        os.Getenv("RTK_REWRITE"),
		dockerProxyEnsure: os.Getenv("DOCKER_PROXY_ENSURE"),
		dockerProxyPort:   port,
	}
}

func main() {
	logFile := flag.String("log-file", "", "path to JSON log file (append)")

	flag.Parse()

	err := mainErr(*logFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hook-router: %v\n", err)
		os.Exit(1)
	}
}

func mainErr(logFile string) error {
	logger, closeLog, err := openLogger(logFile)
	if err != nil {
		return err
	}
	defer closeLog()

	return run(os.Stdin, os.Stdout, configFromEnv(), logger)
}

func run(stdin io.Reader, stdout io.Writer, cfg config, logger *slog.Logger) error {
	input, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}

	var hook map[string]any

	err = json.Unmarshal(input, &hook)
	if err != nil {
		logger.Info("invalid JSON, delegating", slog.Any("error", err))
		return delegate(input, cfg.rtkRewrite, logger)
	}

	toolInput, ok := hook["tool_input"].(map[string]any)
	if !ok {
		return delegate(input, cfg.rtkRewrite, logger)
	}

	command, ok := toolInput["command"].(string)
	if !ok || command == "" {
		return delegate(input, cfg.rtkRewrite, logger)
	}

	logger.Info("checking command", slog.String("command", command))

	parser := syntax.NewParser()

	prog, err := parser.Parse(strings.NewReader(command), "")
	if err != nil {
		logger.Warn(
			"parse error, delegating",
			slog.String("command", command),
			slog.Any("error", err),
		)

		return delegate(input, cfg.rtkRewrite, logger)
	}

	if reason, denied := checkGitStashDenied(prog); denied {
		logger.Info(
			"denied",
			slog.String("rule", "git-stash"),
			slog.String("command", command),
			slog.String("reason", reason),
		)

		return encodeJSON(stdout, denyResponse(reason))
	}

	if reason, denied := checkK8sCliDenied(prog); denied {
		logger.Info(
			"denied",
			slog.String("rule", "k8s-cli"),
			slog.String("command", command),
			slog.String("reason", reason),
		)

		return encodeJSON(stdout, denyResponse(reason))
	}

	if info := checkDockerCommand(prog); info.found && !info.alreadyProxied {
		// Network enforcement applies regardless of proxy configuration.
		if reason, denied := applyDockerNetworkPolicy(info); denied {
			return encodeJSON(stdout, denyResponse(reason))
		}

		if cfg.dockerProxyEnsure == "" {
			return delegate(input, cfg.rtkRewrite)
		}

		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}

		//nolint:gosec // cfg.dockerProxyEnsure is a trusted path from environment config
		cmd := exec.CommandContext(context.Background(), cfg.dockerProxyEnsure, cwd)
		cmd.Stderr = os.Stderr

		err = cmd.Run()
		if err != nil {
			return encodeJSON(stdout, denyResponse(
				fmt.Sprintf("Docker proxy setup failed: %v. Run docker commands directly in your terminal.", err)))
		}

		rewritten := make(map[string]any, len(toolInput))
		maps.Copy(rewritten, toolInput)

		// Inject --network none for container-creating subcommands.
		rewrittenCmd := injectDockerNetwork(info, command)
		rewritten["command"] = fmt.Sprintf("DOCKER_HOST=tcp://127.0.0.1:%s %s", cfg.dockerProxyPort, rewrittenCmd)

		return encodeJSON(stdout, rewriteResponse(rewritten))
	}

	return delegate(input, cfg.rtkRewrite)
}

// denyResponse builds a PreToolUse hook response that blocks the tool call.
func denyResponse(reason string) map[string]any {
	return map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": reason,
		},
	}
}

func encodeJSON(w io.Writer, v any) error {
	err := json.NewEncoder(w).Encode(v)
	if err != nil {
		return fmt.Errorf("encoding output: %w", err)
	}

	return nil
}

var (
	// stashAllowed lists git stash subcommands that are
	// safe to allow through. Any stash invocation whose
	// third argument is not in this set is denied, which
	// blocks save/push forms used to shelve changes.
	stashAllowed = map[string]bool{
		"pop":    true,
		"apply":  true,
		"list":   true,
		"show":   true,
		"branch": true,
		"drop":   true,
		"clear":  true,
	}

	// k8sBlockedCmds lists CLI commands that should be routed
	// through the mcp-kubernetes MCP server instead of being
	// invoked directly.
	k8sBlockedCmds = map[string]string{
		"kubectl": "mcp__kubernetes__call_kubectl",
	}
)

// checkGitStashDenied walks the AST looking for git stash invocations that
// save/push changes. It allows read and consume subcommands (pop, apply, list,
// show, branch, drop, clear) and denies everything else.
func checkGitStashDenied(prog *syntax.File) (string, bool) {
	found := false

	syntax.Walk(prog, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}

		parts0 := call.Args[0].Parts
		parts1 := call.Args[1].Parts
		if len(parts0) != 1 || len(parts1) != 1 {
			return true
		}

		lit0, ok0 := parts0[0].(*syntax.Lit)
		lit1, ok1 := parts1[0].(*syntax.Lit)
		if !ok0 || !ok1 || lit0.Value != "git" || lit1.Value != "stash" {
			return true
		}

		// Bare "git stash" (implicit push) or unknown subcommand/flag.
		if len(call.Args) == 2 {
			found = true
			return true
		}

		parts2 := call.Args[2].Parts
		if len(parts2) != 1 {
			found = true
			return true
		}

		lit2, ok2 := parts2[0].(*syntax.Lit)
		if !ok2 || !stashAllowed[lit2.Value] {
			found = true
		}

		return true
	})

	if !found {
		return "", false
	}

	return "Do not use git stash to shelve changes. All issues in the working tree are your responsibility to fix, regardless of origin.", true
}

// checkK8sCliDenied walks the AST looking for direct invocations of kubectl.
// These should use the mcp-kubernetes MCP server.
func checkK8sCliDenied(prog *syntax.File) (string, bool) {
	var tool string

	syntax.Walk(prog, func(node syntax.Node) bool {
		if tool != "" {
			return false
		}

		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) < 1 {
			return true
		}

		parts := call.Args[0].Parts
		if len(parts) != 1 {
			return true
		}

		lit, ok := parts[0].(*syntax.Lit)
		if !ok {
			return true
		}

		if _, blocked := k8sBlockedCmds[lit.Value]; blocked {
			tool = lit.Value
		}

		return true
	})

	if tool == "" {
		return "", false
	}

	return fmt.Sprintf(
		"Direct %s usage is blocked. Use %s instead.",
		tool, k8sBlockedCmds[tool],
	), true
}

// dockerCommandInfo describes a docker CLI invocation found in the shell AST.
// [checkDockerCommand] populates it so callers can decide whether to rewrite
// or deny the command.
type dockerCommandInfo struct {
	// found is true when the AST contains a "docker" call expression.
	found bool
	// alreadyProxied is true when the call has a DOCKER_HOST env assignment,
	// indicating the command was already rewritten by a prior hook invocation.
	alreadyProxied bool
	subcommand     string // "run", "create", "build", "ps", "compose", etc.
	networkFlag    string // value of --network/--net if present
	// subcommandEnd is the byte offset in the original command string
	// where the subcommand token ends. Used for --network none injection.
	subcommandEnd uint
}

// containerCreatingSubcommands lists docker subcommands that create
// new containers and should have network restrictions applied.
// See [needsNetworkRestriction].
var containerCreatingSubcommands = map[string]bool{
	"run":    true,
	"create": true,
}

// checkDockerCommand walks prog for the first docker CLI invocation and
// returns its parsed metadata. It detects the subcommand, any
// --network/--net flag, and whether DOCKER_HOST is already set.
func checkDockerCommand(prog *syntax.File) dockerCommandInfo {
	var info dockerCommandInfo

	syntax.Walk(prog, func(node syntax.Node) bool {
		if info.found {
			return false
		}

		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) < 1 {
			return true
		}

		parts := call.Args[0].Parts
		if len(parts) != 1 {
			return true
		}

		lit, ok := parts[0].(*syntax.Lit)
		if !ok || lit.Value != "docker" {
			return true
		}

		info.found = true

		for _, assign := range call.Assigns {
			if assign.Name != nil && assign.Name.Value == "DOCKER_HOST" {
				info.alreadyProxied = true
			}
		}

		// Extract subcommand and network flag from remaining args.
		parseDockerArgs(&info, call.Args[1:])

		return false
	})

	return info
}

// parseDockerArgs scans docker arguments after the "docker" word to
// extract the subcommand and any --network/--net flag value. For
// "docker compose", it continues scanning for the compose subcommand
// (e.g. run, create). Scanning stops at the first positional argument
// after the subcommand.
func parseDockerArgs(info *dockerCommandInfo, args []*syntax.Word) {
	foundSubcommand := false

	for i := 0; i < len(args); i++ {
		word := args[i]
		if len(word.Parts) != 1 {
			continue
		}

		lit, ok := word.Parts[0].(*syntax.Lit)
		if !ok {
			continue
		}

		val := lit.Value

		// Extract --network/--net flag value.
		if strings.HasPrefix(val, "--network=") || strings.HasPrefix(val, "--net=") {
			info.networkFlag = val[strings.IndexByte(val, '=')+1:]

			continue
		}

		if val == "--network" || val == "--net" {
			if i+1 < len(args) {
				if nextLit := wordToLit(args[i+1]); nextLit != "" {
					info.networkFlag = nextLit
					i++ // skip the value arg
				}
			}

			continue
		}

		// Skip flag arguments (start with -).
		if strings.HasPrefix(val, "-") {
			continue
		}

		// First non-flag argument is the subcommand.
		if !foundSubcommand {
			foundSubcommand = true
			info.subcommand = val
			info.subcommandEnd = uint(word.End().Offset())

			// Continue scanning for --network/--net flags after
			// the subcommand, and for the compose sub-subcommand
			// when val == "compose".
			continue
		}

		// Second non-flag after "compose" is the compose subcommand.
		// Update subcommandEnd so the caller can inject after it.
		if info.subcommand == "compose" && containerCreatingSubcommands[val] {
			info.subcommandEnd = uint(word.End().Offset())
		}

		// Stop at the first positional arg after the subcommand
		// (image name, path, etc.) -- flags after this are container
		// args, not docker flags.
		return
	}
}

// wordToLit returns the literal string value of a single-part word,
// or empty string if the word is not a simple literal.
func wordToLit(w *syntax.Word) string {
	if len(w.Parts) != 1 {
		return ""
	}

	lit, ok := w.Parts[0].(*syntax.Lit)
	if !ok {
		return ""
	}

	return lit.Value
}

// needsNetworkRestriction reports whether the docker subcommand creates
// a container that should have --network none enforced.
func needsNetworkRestriction(info dockerCommandInfo) bool {
	return containerCreatingSubcommands[info.subcommand] ||
		(info.subcommand == "compose" && info.subcommandEnd > 0)
}

// applyDockerNetworkPolicy reports whether a container-creating docker
// command should be denied because it specifies a network other than
// "none". It returns the denial reason and true when the command
// violates sandbox network restrictions.
func applyDockerNetworkPolicy(info dockerCommandInfo) (string, bool) {
	if !needsNetworkRestriction(info) {
		return "", false
	}

	if info.networkFlag != "" && info.networkFlag != "none" {
		return "Docker containers must use --network none to comply with sandbox network restrictions.", true
	}

	return "", false
}

// injectDockerNetwork splices "--network none" into the command string
// after the subcommand token for container-creating commands that do
// not already have a network flag.
func injectDockerNetwork(info dockerCommandInfo, command string) string {
	if !needsNetworkRestriction(info) {
		return command
	}

	if info.networkFlag != "" {
		// Already has --network none (non-none was denied earlier).
		return command
	}

	offset := info.subcommandEnd

	return command[:offset] + " --network none" + command[offset:]
}

// rewriteResponse builds a PreToolUse hook response that updates the tool input.
func rewriteResponse(toolInput map[string]any) map[string]any {
	return map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse",
			"updatedInput":  toolInput,
		},
	}
}

// delegate forwards the raw hook input to the rtk-rewrite binary, if configured.
func delegate(input []byte, rtkRewrite string) error {
	if rtkRewrite == "" {
		return nil
	}

	logger.Info("delegating", slog.String("target", rtkRewrite))

	cmd := exec.CommandContext(context.Background(), rtkRewrite)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("delegating to %s: %w", rtkRewrite, err)
	}

	return nil
}

// openLogger creates a JSON [*slog.Logger] writing to the named file.
// Returns a discard logger and no-op closer when path is empty.
func openLogger(path string) (*slog.Logger, func(), error) {
	if path == "" {
		return slog.New(slog.DiscardHandler), func() {}, nil
	}

	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return nil, nil, fmt.Errorf("creating log directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("opening %s: %w", path, err)
	}

	logger := slog.New(slog.NewJSONHandler(f, nil))

	return logger, func() {
		err := f.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "closing log file: %v\n", err)
		}
	}, nil
}
