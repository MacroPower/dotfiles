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

	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/fetch"
	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/rules"
	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/stats"
	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/store"
)

const version = "0.1.0"

func main() {
	args := os.Args[1:]

	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "stats":
			os.Exit(stats.Run(args[1:]))
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
	ignoreLLMs := fs.Bool("ignore-llms-txt", false, "skip llms.txt discovery and notice")
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

	err = serve(*userAgent, *ignoreRobots, *ignoreLLMs, *proxyURL, *rulesFile, *logFile, *dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)

		return 1
	}

	return 0
}

func serve(userAgent string, ignoreRobots, ignoreLLMs bool, proxyURL, rulesFile, logFile, dbPath string) error {
	logger, logCloser, err := openLogger(logFile)
	if err != nil {
		return err
	}
	defer logCloser()

	urlRules, err := rules.Load(rulesFile)
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

	var st *store.Store

	if dbPath != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)

		st, err = store.Open(ctx, dbPath)

		cancel()

		if err != nil {
			return fmt.Errorf("opening store: %w", err)
		}

		defer func() { _ = st.Close() }()

		pruneCtx, pruneCancel := context.WithTimeout(context.Background(), 45*time.Second)

		ran, err := st.MaybePruneStale(pruneCtx)

		pruneCancel()

		switch {
		case err != nil:
			logger.Warn("pruning stale fetches", slog.Any("error", err))
		case ran:
			logger.Debug("pruned stale fetches")
		}
	}

	h := fetch.New(
		fetch.WithUserAgent(userAgent),
		fetch.WithCheckRobots(!ignoreRobots),
		fetch.WithCheckLLMs(!ignoreLLMs),
		fetch.WithRules(urlRules),
		fetch.WithLogger(logger),
		fetch.WithStore(st),
		fetch.WithTransport(transport),
	)

	srv := mcp.NewServer(
		&mcp.Implementation{Name: "mcp-fetch", Version: version},
		nil,
	)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "fetch",
		Description: "Fetch a URL, converting HTML to Markdown by default. " +
			"Pass `pattern` (RE2 regex) to grep large pages to matching lines.",
	}, h.Handle)

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
