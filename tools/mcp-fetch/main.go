package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/temoto/robotstxt"
)

const version = "0.1.0"

func main() {
	userAgent := flag.String("user-agent", "MCP-Fetch/"+version, "HTTP User-Agent header")
	ignoreRobots := flag.Bool("ignore-robots-txt", false, "skip robots.txt checks")
	proxyURL := flag.String("proxy-url", "", "HTTP proxy URL")
	rulesFile := flag.String("rules-file", "", "path to JSON URL rules file")

	flag.Parse()

	rules, err := LoadRules(*rulesFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "loading URL rules: %v\n", err)
		os.Exit(1)
	}

	transport := &http.Transport{}

	if *proxyURL != "" {
		u, err := url.Parse(*proxyURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid proxy URL: %v\n", err)
			os.Exit(1)
		}

		transport.Proxy = http.ProxyURL(u)
	}

	h := &fetchHandler{
		userAgent:    *userAgent,
		checkRobots:  !*ignoreRobots,
		rules:        rules,
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
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
