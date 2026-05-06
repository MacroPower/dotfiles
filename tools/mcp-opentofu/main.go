package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "0.1.0"

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run() error {
	userAgent := flag.String("user-agent", defaultUserAgent, "HTTP User-Agent header")
	proxyURL := flag.String("proxy-url", "", "HTTP proxy URL")
	logFile := flag.String("log-file", "", "path to JSON log file (append)")
	tofuBin := flag.String(
		"tofu-bin", "tofu",
		"path to the tofu binary used by the local-tofu tools (validate, init, plan);"+
			" resolved via PATH when not absolute",
	)
	sandboxFlag := flag.String(
		"sandbox", "auto",
		"sandbox mode for tofu subprocesses: auto (platform default), on (require backend), or off (debug)",
	)
	policyFile := flag.String(
		"policy-file", "",
		"path to a JSON file describing per-tool sandbox policies; required when --sandbox is auto or on",
	)

	flag.Parse()

	logger, logCloser, err := openLogger(*logFile)
	if err != nil {
		return err
	}
	defer logCloser()

	mode, err := ParseSandboxMode(*sandboxFlag)
	if err != nil {
		return err
	}

	sandbox, err := New(mode)
	if err != nil {
		return err
	}

	policies, err := loadPolicies(mode, *policyFile, logger)
	if err != nil {
		return err
	}

	allowRoot, err := resolveAllowRoot()
	if err != nil {
		return err
	}

	logSandboxEnforcementWarnings(logger, mode, policies)

	logger.Info("sandbox configured",
		slog.String("mode", string(mode)),
		slog.String("backend", sandbox.Name()),
		slog.String("policy_file", *policyFile),
		slog.String("allow_root", allowRoot),
	)

	transport := &http.Transport{}

	if *proxyURL != "" {
		u, err := url.Parse(*proxyURL)
		if err != nil {
			return fmt.Errorf("invalid proxy URL: %w", err)
		}

		transport.Proxy = http.ProxyURL(u)
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   defaultTimeout,
	}

	client := NewClient(
		WithHTTPClient(httpClient),
		WithUserAgent(*userAgent),
	)

	h := &handler{
		client:    client,
		log:       logger,
		tofu:      newExecTofu(*tofuBin, sandbox),
		policies:  policies,
		allowRoot: allowRoot,
	}

	srv := mcp.NewServer(
		&mcp.Implementation{Name: "mcp-opentofu", Version: version},
		nil,
	)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolSearch,
		Description: "Search the OpenTofu Registry to find providers, modules, resources, and data sources. Use simple terms without prefixes like 'terraform-provider-' or 'terraform-module-'.",
	}, h.handleSearch)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolProviderDetails,
		Description: "Get detailed information about a specific OpenTofu provider by namespace and name. Do NOT include 'terraform-provider-' prefix in the name.",
	}, h.handleProviderDetails)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolModuleDetails,
		Description: "Get detailed information about a specific OpenTofu module by namespace, name, and target. Use the simple module name, NOT the full repository name.",
	}, h.handleModuleDetails)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolResourceDocs,
		Description: "Get detailed documentation for a specific OpenTofu resource by provider namespace, provider name, and resource name.",
	}, h.handleResourceDocs)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolDatasourceDocs,
		Description: "Get detailed documentation for a specific OpenTofu data source by provider namespace, provider name, and data source name.",
	}, h.handleDatasourceDocs)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolValidate,
		Description: `Run "tofu validate" against a local working directory and return diagnostics. The directory must contain initialized OpenTofu / Terraform configuration; pass init=true to run "tofu init -input=false -no-color -backend=false" first when modules or providers have not yet been fetched.`,
	}, h.handleValidate)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolInit,
		Description: `Run "tofu init" against a local working directory to download providers and modules. Defaults to -backend=false (local init only); pass backend=true to also configure the backend. Pass upgrade=true to fetch the latest provider/module versions allowed by version constraints.`,
	}, h.handleInit)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolPlan,
		Description: `Run "tofu plan" against a local working directory and report whether any changes are pending. Requires that providers/modules have been fetched (run init first or pass init=true). Pass destroy=true for a destroy plan, refresh_only=true for drift detection. Output may include sensitive values; treat as confidential.`,
	}, h.handlePlan)

	addRegistryInfoResource(srv)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	err = srv.Run(ctx, &mcp.StdioTransport{})
	if err != nil {
		return fmt.Errorf("server: %w", err)
	}

	return nil
}

// loadPolicies reads the JSON policy file. The file is required when
// sandbox mode is anything other than [SandboxOff]; with --sandbox=off a
// missing file degrades to a warning and [Defaults] applies, so the
// binary stays runnable by hand for debugging. Malformed JSON is fatal
// in every mode.
func loadPolicies(mode SandboxMode, path string, logger *slog.Logger) (Policies, error) {
	if path == "" {
		if mode != SandboxOff {
			return nil, fmt.Errorf("%w: --policy-file is required when --sandbox=%s", ErrPolicy, mode)
		}

		logger.Warn("running without --policy-file; init and plan will reach no registry",
			slog.String("sandbox", string(mode)),
		)

		return Defaults(), nil
	}

	policies, err := LoadFile(path)
	if err != nil {
		if mode != SandboxOff {
			return nil, err
		}

		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}

		logger.Warn("policy file missing; falling back to defaults",
			slog.String("path", path),
			slog.String("sandbox", string(mode)),
		)

		return Defaults(), nil
	}

	for _, tool := range []string{toolValidate, toolInit, toolPlan} {
		if _, ok := policies[tool]; !ok {
			policies[tool] = Policy{}
		}
	}

	return policies, nil
}

// logSandboxEnforcementWarnings emits a single warning when any sandboxed
// tool declares an [Policy.AllowedDomains] entry. Per-domain enforcement
// has platform-specific limits — see the per-platform caveat strings
// below for the actual wording.
func logSandboxEnforcementWarnings(logger *slog.Logger, mode SandboxMode, policies Policies) {
	if mode == SandboxOff {
		return
	}

	var withDomains []string

	for tool, p := range policies {
		if len(p.AllowedDomains) > 0 {
			withDomains = append(withDomains, tool)
		}
	}

	if len(withDomains) == 0 {
		return
	}

	var caveat string

	switch runtime.GOOS {
	case "darwin":
		caveat = "sandbox-exec resolves domain names to IPs at policy-load time; CDN-fronted hosts may be flaky"
	case "linux":
		caveat = "bwrap unprivileged user namespaces cannot filter outbound traffic per-domain; allowed_domains documents intent only"
	default:
		caveat = "no sandbox backend on this platform; domain allowlist is advisory"
	}

	logger.Warn("sandbox network allowlist has limited per-domain enforcement",
		slog.String("platform", runtime.GOOS),
		slog.String("caveat", caveat),
		slog.Any("tools", withDomains),
	)
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
