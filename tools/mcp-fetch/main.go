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
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "0.1.0"

func main() {
	args := os.Args[1:]

	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "stats":
			os.Exit(runStats(args[1:]))
		case "version":
			fmt.Println(version)
			os.Exit(0)

		case "help":
			// Hand off to runServe's FlagSet so the printed usage is
			// accurate.
			args = []string{"-h"}
		default:
			fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", args[0])
			os.Exit(2)
		}
	}

	os.Exit(runServe(args))
}

func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)

	userAgent := fs.String("user-agent", "MCP-Fetch/"+version, "HTTP User-Agent header")
	ignoreRobots := fs.Bool("ignore-robots-txt", false, "skip robots.txt checks")
	proxyURL := fs.String("proxy-url", "", "HTTP proxy URL")
	rulesFile := fs.String("rules-file", "", "path to JSON URL rules file")
	logFile := fs.String("log-file", "", "path to JSON log file (append)")
	dbPath := fs.String("db", "", "path to SQLite store (empty = recording disabled)")

	err := fs.Parse(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}

		fmt.Fprintf(os.Stderr, "%v\n", err)

		return 2
	}

	err = serve(*userAgent, *ignoreRobots, *proxyURL, *rulesFile, *logFile, *dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)

		return 1
	}

	return 0
}

func serve(userAgent string, ignoreRobots bool, proxyURL, rulesFile, logFile, dbPath string) error {
	logger, logCloser, err := openLogger(logFile)
	if err != nil {
		return err
	}
	defer logCloser()

	rules, err := LoadRules(rulesFile)
	if err != nil {
		return fmt.Errorf("loading URL rules: %w", err)
	}

	transport := &http.Transport{}

	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return fmt.Errorf("invalid proxy URL: %w", err)
		}

		transport.Proxy = http.ProxyURL(u)
	}

	var store *Store

	if dbPath != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)

		store, err = OpenStore(ctx, dbPath)

		cancel()

		if err != nil {
			return fmt.Errorf("opening store: %w", err)
		}

		defer func() { _ = store.Close() }()

		pruneCtx, pruneCancel := context.WithTimeout(context.Background(), 45*time.Second)

		ran, err := store.MaybePruneStale(pruneCtx)

		pruneCancel()

		switch {
		case err != nil:
			logger.Warn("pruning stale fetches", slog.Any("error", err))
		case ran:
			logger.Debug("pruned stale fetches")
		}
	}

	h := newFetchHandler(
		withUserAgent(userAgent),
		withCheckRobots(!ignoreRobots),
		withRules(rules),
		withLogger(logger),
		withStore(store),
	)

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
