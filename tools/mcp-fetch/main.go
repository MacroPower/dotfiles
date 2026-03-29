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
	"path/filepath"
	"os/signal"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/temoto/robotstxt"
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
	userAgent := flag.String("user-agent", "MCP-Fetch/"+version, "HTTP User-Agent header")
	ignoreRobots := flag.Bool("ignore-robots-txt", false, "skip robots.txt checks")
	proxyURL := flag.String("proxy-url", "", "HTTP proxy URL")
	rulesFile := flag.String("rules-file", "", "path to JSON URL rules file")
	logFile := flag.String("log-file", "", "path to JSON log file (append)")

	flag.Parse()

	logger, logCloser, err := openLogger(*logFile)
	if err != nil {
		return err
	}
	defer logCloser()

	rules, err := LoadRules(*rulesFile)
	if err != nil {
		return fmt.Errorf("loading URL rules: %w", err)
	}

	transport := &http.Transport{}

	if *proxyURL != "" {
		u, err := url.Parse(*proxyURL)
		if err != nil {
			return fmt.Errorf("invalid proxy URL: %w", err)
		}

		transport.Proxy = http.ProxyURL(u)
	}

	h := &fetchHandler{
		userAgent:    *userAgent,
		checkRobots:  !*ignoreRobots,
		rules:        rules,
		log:          logger,
		robotsCache:  expirable.NewLRU[string, *robotstxt.RobotsData](128, nil, time.Hour),
		contentCache: expirable.NewLRU[string, string](64, nil, time.Hour),
	}

	h.client = &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}

			err := h.validateURL(req.URL)
			if err != nil {
				return err
			}

			if h.checkRobots && req.URL.Host != via[len(via)-1].URL.Host {
				return h.checkRobotsURL(req.Context(), req.URL)
			}

			return nil
		},
	}

	srv := mcp.NewServer(
		&mcp.Implementation{Name: "mcp-fetch", Version: version},
		nil,
	)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "fetch",
		Description: "Fetch a URL and return its content. HTML is converted to Markdown by default.",
	}, h.handle)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)

	err = srv.Run(ctx, &mcp.StdioTransport{})

	cancel()

	if err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

// openLogger creates a JSON [*slog.Logger] writing to the named file.
// Returns a discard logger and no-op closer when path is empty.
func openLogger(path string) (*slog.Logger, func(), error) {
	if path == "" {
		return slog.New(slog.DiscardHandler), func() {}, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
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
