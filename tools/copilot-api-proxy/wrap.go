package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.jacobcolvin.com/dotfiles/tools/copilot-api-proxy/auth"
)

// runWrap launches claude through a proxy dedicated to that instance. It binds
// an ephemeral loopback port, gates it with a per-instance secret, injects the
// proxy's address and that secret into the child's environment, and runs the
// proxy only for the lifetime of the child. Arguments are passed to claude.
func runWrap(args []string) error {
	cfg := Load()

	secret, err := randomSecret()
	if err != nil {
		return err
	}
	cfg.MasterKey = secret

	// stderrSafe=false: claude owns the terminal, so the proxy logs nothing to
	// stderr; set COPILOT_PROXY_LOG_FILE to capture a run's logs instead.
	logger, closeLog, err := newLogger(cfg, false)
	if err != nil {
		return err
	}
	defer closeLog()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("open listener: %w", err)
	}
	baseURL := "http://" + ln.Addr().String()

	managerOpts, err := managerOptions(cfg, logger)
	if err != nil {
		_ = ln.Close()
		return err
	}
	mgr, err := auth.NewManager(managerOpts...)
	if err != nil {
		_ = ln.Close()
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		_ = ln.Close()
		if errors.Is(err, auth.ErrNoGitHubToken) {
			return fmt.Errorf("%w; run `copilot-api-proxy login`", auth.ErrNoGitHubToken)
		}
		return err
	}

	httpSrv := &http.Server{Handler: NewServer(mgr, cfg, logger).Handler(), ReadHeaderTimeout: 30 * time.Second}
	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("proxy server stopped unexpectedly", "error", err)
		}
	}()

	logger.Debug("per-instance proxy ready", "addr", baseURL)
	code, err := launchClaude(args, baseURL, secret)
	logger.Debug("claude exited", "code", code)

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 3*time.Second)
	_ = httpSrv.Shutdown(shutCtx)
	shutCancel()
	cancel()

	if err != nil {
		return err
	}
	os.Exit(code)
	return nil
}

// launchClaude runs claude with the proxy environment, forwarding terminating
// signals, and returns its exit code. SIGINT is left for the terminal to
// deliver to the child when interactive, so the proxy survives Ctrl-C until
// claude itself exits.
func launchClaude(args []string, baseURL, secret string) (int, error) {
	bin := envOr("COPILOT_PROXY_CLAUDE", "claude")

	cmd := exec.Command(bin, args...)
	cmd.Env = buildChildEnv(os.Environ(), baseURL, secret)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start %s: %w", bin, err)
	}

	interactive := isTerminal(os.Stdin)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	go func() {
		for sig := range sigCh {
			if sig == os.Interrupt && interactive {
				continue // the terminal delivers SIGINT to the child directly
			}
			_ = cmd.Process.Signal(sig)
		}
	}()

	err := cmd.Wait()
	signal.Stop(sigCh)
	close(sigCh)

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	if err != nil {
		return 0, fmt.Errorf("run %s: %w", bin, err)
	}
	return 0, nil
}

// buildChildEnv returns base with the Anthropic routing variables forced to the
// proxy. Any inherited ANTHROPIC_API_KEY is dropped so the user's real key is
// never used or forwarded.
func buildChildEnv(base []string, baseURL, token string) []string {
	const (
		baseURLKey = "ANTHROPIC_BASE_URL="
		authKey    = "ANTHROPIC_AUTH_TOKEN="
		apiKey     = "ANTHROPIC_API_KEY="
	)

	out := make([]string, 0, len(base)+2)
	for _, kv := range base {
		if strings.HasPrefix(kv, baseURLKey) || strings.HasPrefix(kv, authKey) || strings.HasPrefix(kv, apiKey) {
			continue
		}
		out = append(out, kv)
	}
	return append(out, baseURLKey+baseURL, authKey+token)
}

func randomSecret() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate instance secret: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
