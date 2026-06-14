package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"

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

	flag.Parse()

	logger, logCloser, err := openLogger(*logFile)
	if err != nil {
		return err
	}
	defer logCloser()

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
		client: client,
		log:    logger,
	}

	srv := mcp.NewServer(
		&mcp.Implementation{Name: "mcp-opentofu", Version: version},
		nil,
	)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolSearch,
		Description: "Search the OpenTofu Registry for providers, modules, resources, and data sources. Use bare terms (no 'terraform-provider-'/'terraform-module-' prefix).",
	}, h.handleSearch)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolProviderDetails,
		Description: "OpenTofu provider details. Omit the 'terraform-provider-' prefix in the name.",
	}, h.handleProviderDetails)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolModuleDetails,
		Description: "OpenTofu module details. Use the simple module name, not the full repository name.",
	}, h.handleModuleDetails)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolResourceDocs,
		Description: "OpenTofu resource documentation.",
	}, h.handleResourceDocs)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolDatasourceDocs,
		Description: "OpenTofu data source documentation.",
	}, h.handleDatasourceDocs)

	addRegistryInfoResource(srv)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	err = srv.Run(ctx, &mcp.StdioTransport{})
	if err != nil {
		return fmt.Errorf("server: %w", err)
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
