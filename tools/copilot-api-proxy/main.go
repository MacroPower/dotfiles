package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.jacobcolvin.com/dotfiles/tools/copilot-api-proxy/auth"
)

func main() {
	args := os.Args[1:]
	var cmd string
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}

	var err error
	switch cmd {
	case "login":
		err = runLogin()
	case "serve":
		err = runServe(args)
	case "run":
		err = runWrap(args)
	case "", "-h", "-help", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "copilot-api-proxy: unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "copilot-api-proxy:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `copilot-api-proxy serves the Anthropic Messages API from a GitHub Copilot subscription.

Usage:
  copilot-api-proxy login            Authenticate with GitHub via the device flow.
  copilot-api-proxy run [args...]    Launch claude through a per-instance proxy; args pass to claude.
  copilot-api-proxy serve [--addr]   Run a standalone shared proxy.
`)
}

// runServe runs a standalone, long-lived proxy on a fixed address.
func runServe(args []string) error {
	cfg := Load()

	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.StringVar(&cfg.ListenAddr, "addr", cfg.ListenAddr, "listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if err := requireLoopbackOrKey(cfg); err != nil {
		return err
	}

	mgr, err := auth.NewManager(managerOptions(cfg)...)
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		if errors.Is(err, auth.ErrNoGitHubToken) {
			return fmt.Errorf("%w; run `copilot-api-proxy login`", auth.ErrNoGitHubToken)
		}
		return err
	}

	srv := NewServer(mgr, cfg)
	httpSrv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 30 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		_ = httpSrv.Shutdown(shutCtx)
	}()

	log.Printf("copilot-api-proxy: listening on %s", cfg.ListenAddr)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func runLogin() error {
	cfg := Load()

	dir := cfg.DataDir
	if dir == "" {
		d, err := auth.DefaultDataDir()
		if err != nil {
			return err
		}
		dir = d
	}

	var opts []auth.Option
	if cfg.DataDir != "" {
		opts = append(opts, auth.WithDataDir(cfg.DataDir))
	}

	tok, err := auth.Login(context.Background(), os.Stdout, opts...)
	if err != nil {
		return err
	}
	if err := auth.SaveGitHubToken(dir, tok); err != nil {
		return err
	}
	fmt.Println("Authenticated. Token saved to", dir)
	return nil
}

// requireLoopbackOrKey refuses to serve on a non-loopback address without a
// master key, so an unauthenticated proxy is never exposed beyond localhost.
func requireLoopbackOrKey(cfg Config) error {
	if cfg.MasterKey != "" {
		return nil
	}
	host, _, err := net.SplitHostPort(cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("parse listen address %q: %w", cfg.ListenAddr, err)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("refusing to bind %s without a master key; set COPILOT_PROXY_MASTER_KEY or bind a loopback address", cfg.ListenAddr)
}

func managerOptions(cfg Config) []auth.Option {
	opts := []auth.Option{auth.WithEditorHeaders(cfg.Editor)}
	if cfg.DataDir != "" {
		opts = append(opts, auth.WithDataDir(cfg.DataDir))
	}
	if cfg.GitHubToken != "" {
		opts = append(opts, auth.WithGitHubToken(cfg.GitHubToken))
	}
	if cfg.APIBase != "" {
		opts = append(opts, auth.WithAPIBaseOverride(cfg.APIBase))
	}
	return opts
}
