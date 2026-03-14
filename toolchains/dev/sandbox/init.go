package sandbox

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// envoyDrainTimeout is the maximum time to wait for Envoy to exit
// after receiving SIGTERM before proceeding with shutdown.
const envoyDrainTimeout = 5 * time.Second

// ExitError carries a process exit code through the error return path
// so the CLI entrypoint can propagate it to [os.Exit].
type ExitError struct{ Code int }

// Error returns a human-readable representation of the exit status.
func (e *ExitError) Error() string {
	return fmt.Sprintf("exit status %d", e.Code)
}

var (
	// ErrNoCommand is returned when Init is called without a command to exec.
	ErrNoCommand = errors.New("no command specified")

	// ErrIptablesNotLoaded is returned when iptables REDIRECT rules are
	// not present after restore.
	ErrIptablesNotLoaded = errors.New("iptables REDIRECT rules not loaded")

	// ErrIPv6Unsecured is returned when IPv6 rules failed to load but IPv6
	// is still enabled on the host.
	ErrIPv6Unsecured = errors.New("IPv6 rules not loaded and IPv6 still enabled")

	// ErrEnvoyNotRunning is returned when the Envoy proxy process exits
	// or cannot be signaled after startup.
	ErrEnvoyNotRunning = errors.New("envoy process not running")
)

// ParseUpstreamDNS extracts the first nameserver from resolv.conf content.
func ParseUpstreamDNS(resolvConf string) string {
	scanner := bufio.NewScanner(strings.NewReader(resolvConf))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "nameserver") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return fields[1]
			}
		}
	}

	return ""
}

// CreateEnvoyUser appends envoy user/group entries to /etc/passwd and
// /etc/group.
func CreateEnvoyUser() error {
	passwdEntry := fmt.Sprintf("envoy:x:%s:%s::/tmp:/bin/false\n", EnvoyUID, EnvoyUID)

	err := appendToFile("/etc/passwd", passwdEntry)
	if err != nil {
		return fmt.Errorf("adding envoy user: %w", err)
	}

	groupEntry := fmt.Sprintf("envoy:x:%s:\n", EnvoyUID)

	err = appendToFile("/etc/group", groupEntry)
	if err != nil {
		return fmt.Errorf("adding envoy group: %w", err)
	}

	return nil
}

// Init performs the full sandbox initialization sequence: generates
// configs if needed, loads iptables rules, starts the DNS proxy and
// Envoy, then drops privileges and runs the given command as a supervised
// child process. The context is threaded to all subprocesses, allowing
// cancellation to propagate. Returns an [*ExitError] carrying the
// child's exit code on normal termination.
func Init(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return ErrNoCommand
	}

	setenvErr := os.Setenv("PATH", HMBin+":"+os.Getenv("PATH"))
	if setenvErr != nil {
		slog.DebugContext(ctx, "setting PATH", slog.Any("err", setenvErr))
	}

	// Capture upstream DNS before we replace resolv.conf.
	resolvData, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return fmt.Errorf("reading /etc/resolv.conf: %w", err)
	}

	upstream := ParseUpstreamDNS(string(resolvData))

	// Generate configs at runtime if not pre-baked.
	_, err = os.Stat("/etc/envoy-sandbox.yaml")
	if os.IsNotExist(err) {
		slog.InfoContext(ctx, "generating firewall configs")

		err := Generate(ConfigPath)
		if err != nil {
			return fmt.Errorf("generating configs: %w", err)
		}
	}

	// Install CA cert into trust store if MITM certs were generated.
	caCertPath := CADir + "/ca.pem"

	_, err = os.Stat(caCertPath)
	if err == nil {
		slog.InfoContext(ctx, "installing sandbox CA into trust store")

		err := installCA(ctx, caCertPath)
		if err != nil {
			return err
		}
	}

	// Create envoy user.
	err = CreateEnvoyUser()
	if err != nil {
		return fmt.Errorf("creating envoy user: %w", err)
	}

	// Read config to determine runtime mode.
	cfgData, err := os.ReadFile(ConfigPath)
	if err != nil {
		return fmt.Errorf("reading sandbox config: %w", err)
	}

	cfg, err := ParseConfig(cfgData)
	if err != nil {
		return fmt.Errorf("parsing sandbox config: %w", err)
	}

	needsEnvoy := len(cfg.ResolvePorts()) > 0 || len(cfg.TCPForwards) > 0

	// Create per-rule ipsets before iptables-restore, since iptables
	// rules reference ipset names in -m set --match-set directives.
	var createdIPSets []string

	for _, frp := range cfg.ResolveFQDNNonTCPPorts() {
		for _, ipv6 := range []bool{false, true} {
			name := FQDNIPSetName(frp.RuleIndex, ipv6)
			family := "inet"

			if ipv6 {
				family = "inet6"
			}

			args := []string{"ipset", "create", name, "hash:ip", "family", family, "timeout", "0"}

			//nolint:gosec // G204: args are constructed from validated config indices.
			err := exec.CommandContext(ctx, args[0], args[1:]...).Run()
			if err != nil {
				return fmt.Errorf("creating ipset %s: %w", name, err)
			}

			createdIPSets = append(createdIPSets, name)
		}
	}

	// Destroy ipsets on error return so a restart does not fail on
	// pre-existing sets (ISSUE-67).
	var ipsetCleanedUp bool

	defer func() {
		if ipsetCleanedUp {
			return
		}

		for _, name := range createdIPSets {
			//nolint:gosec // G204: name is from validated FQDNIPSetName output.
			destroyErr := exec.CommandContext(ctx, "ipset", "destroy", name).Run()
			if destroyErr != nil {
				slog.DebugContext(ctx, "destroying ipset on init failure",
					slog.String("name", name),
					slog.Any("err", destroyErr),
				)
			}
		}
	}()

	// Load iptables redirect rules and validate.
	err = runCmd(ctx, "iptables-restore", "/etc/iptables-sandbox.rules")
	if err != nil {
		return fmt.Errorf("loading iptables rules: %w", err)
	}

	if needsEnvoy {
		out, err := exec.CommandContext(ctx, "iptables-save").CombinedOutput()
		if err != nil {
			return fmt.Errorf("verifying iptables rules: %w", err)
		}

		if !strings.Contains(string(out), "REDIRECT") {
			return ErrIptablesNotLoaded
		}
	}

	// Load IPv6 rules; disable IPv6 if kernel lacks ip6tables support.
	ipv6Disabled := false

	err = runCmd(ctx, "ip6tables-restore", "/etc/ip6tables-sandbox.rules")
	if err != nil {
		slog.WarnContext(ctx, "ip6tables unavailable, disabling IPv6")
		disableIPv6(ctx)

		ipv6Disabled = true
	}

	// Verify IPv6 state unconditionally -- even without Envoy, IPv6
	// traffic could bypass iptables rules.
	err = verifyIPv6State(ctx)
	if err != nil {
		return err
	}

	// Start DNS proxy. Handles domain filtering internally (replacing
	// dnsmasq + RefuseDNS).
	dnsProxy, err := StartDNSProxy(ctx, cfg, net.JoinHostPort(upstream, "53"), "127.0.0.1:53", ipv6Disabled)
	if err != nil {
		return fmt.Errorf("starting DNS proxy: %w", err)
	}

	// Shut down DNS proxy on error return so goroutines and listeners
	// do not leak (ISSUE-43).
	var dnsProxyCleanedUp bool

	defer func() {
		if dnsProxyCleanedUp {
			return
		}

		shutdownErr := dnsProxy.Shutdown()
		if shutdownErr != nil {
			slog.DebugContext(ctx, "shutting down DNS proxy on init failure", slog.Any("err", shutdownErr))
		}
	}()

	// Point system DNS to local resolver.
	umountErr := exec.CommandContext(ctx, "umount", "/etc/resolv.conf").Run()
	if umountErr != nil {
		slog.DebugContext(ctx, "unmounting resolv.conf", slog.Any("err", umountErr))
	}

	err = os.WriteFile("/etc/resolv.conf", []byte("nameserver 127.0.0.1\nnameserver ::1\n"), 0o644)
	if err != nil {
		return fmt.Errorf("writing resolv.conf: %w", err)
	}

	// Start Envoy only when listeners are needed.
	var envoyCmd *exec.Cmd

	if needsEnvoy {
		envoyCmd = exec.CommandContext(ctx, "setpriv",
			"--reuid="+EnvoyUID, "--regid="+EnvoyUID, "--clear-groups", "--no-new-privs",
			"--", "envoy", "-c", "/etc/envoy-sandbox.yaml", "--log-level", "warning")
		envoyCmd.Stdout = os.Stdout
		envoyCmd.Stderr = os.Stderr

		err := envoyCmd.Start()
		if err != nil {
			return fmt.Errorf("starting envoy: %w", err)
		}

		// Wait on the first available listener port.
		waitPort := firstListenerPort(cfg)

		err = waitForListener(ctx, fmt.Sprintf("127.0.0.1:%d", waitPort), 10*time.Second)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrEnvoyNotRunning, err)
		}

		err = envoyCmd.Process.Signal(syscall.Signal(0))
		if err != nil {
			return fmt.Errorf("%w: %w", ErrEnvoyNotRunning, err)
		}
	}

	// Init setup succeeded; disable error-path cleanup defers.
	// From this point, cleanup is handled by the shutdown path below.
	ipsetCleanedUp = true
	dnsProxyCleanedUp = true

	// Prepare privilege drop.
	chownErr := os.Chown("/commandhistory", mustAtoi(UID), mustAtoi(GID))
	if chownErr != nil {
		slog.DebugContext(ctx, "chowning /commandhistory", slog.Any("err", chownErr))
	}

	chownErr = os.Chown("/claude-state", mustAtoi(UID), mustAtoi(GID))
	if chownErr != nil {
		slog.DebugContext(ctx, "chowning /claude-state", slog.Any("err", chownErr))
	}

	writeErr := os.WriteFile(
		"/proc/sys/net/ipv4/ping_group_range",
		[]byte("0 "+UID),
		0o644,
	)
	if writeErr != nil {
		slog.DebugContext(ctx, "setting ping group range", slog.Any("err", writeErr))
	}

	// Start user command as a supervised child process with dropped
	// privileges. It inherits PID 1's process group so terminal
	// signals (SIGINT, SIGWINCH) reach it naturally.
	userArgs := append([]string{
		"--reuid=" + UID, "--regid=" + GID, "--init-groups",
		"--no-new-privs", "--inh-caps=-all", "--bounding-set=-all", "--",
	}, args...)
	//nolint:gosec // G204: args from constants and user input.
	userCmd := exec.CommandContext(ctx, "setpriv", userArgs...)
	userCmd.Stdin = os.Stdin
	userCmd.Stdout = os.Stdout
	userCmd.Stderr = os.Stderr

	// Register signal handler before starting the user command so
	// that a SIGTERM arriving between Start() and Notify() is
	// caught instead of triggering Go's default termination.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	err = userCmd.Start()
	if err != nil {
		signal.Stop(sigCh)

		return fmt.Errorf("starting user command: %w", err)
	}

	// Wait for user command exit or signal, whichever comes first.
	waitCh := make(chan error, 1)
	go func() { waitCh <- userCmd.Wait() }()

	var waitErr error

	select {
	case waitErr = <-waitCh:
		// User command exited on its own.
	case sig := <-sigCh:
		// Forward signal to the user command (not the process group --
		// the runtime sends SIGTERM to PID 1 specifically).
		slog.InfoContext(ctx, "received signal, forwarding to user command",
			slog.Any("signal", sig),
		)

		err := userCmd.Process.Signal(sig)
		if err != nil {
			slog.WarnContext(ctx, "forwarding signal to user command",
				slog.Any("signal", sig),
				slog.Any("err", err),
			)
		}

		waitErr = <-waitCh
	}

	Shutdown(ctx, envoyCmd, dnsProxy, createdIPSets)

	// Reap any remaining zombie children (PID 1 responsibility).
	for {
		_, err := syscall.Wait4(-1, nil, syscall.WNOHANG, nil)
		if err != nil {
			break
		}
	}

	// Propagate the user command's exit code.
	exitCode := 0

	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return fmt.Errorf("waiting for user command: %w", waitErr)
		}
	}

	return &ExitError{Code: exitCode}
}

// Shutdown performs the full cleanup sequence in the correct order:
// Envoy first (with drain wait), then DNS proxy, then iptables/ipsets.
// Stopping Envoy before DNS ensures in-flight requests can still
// resolve during Envoy's drain period (ISSUE-52).
func Shutdown(ctx context.Context, envoyCmd *exec.Cmd, dnsProxy *DNSProxy, ipsets []string) {
	// Stop Envoy first so DNS remains available during drain (ISSUE-52).
	if envoyCmd != nil && envoyCmd.Process != nil {
		err := envoyCmd.Process.Signal(syscall.SIGTERM)
		if err != nil {
			slog.DebugContext(ctx, "stopping envoy", slog.Any("err", err))
		} else {
			// Wait up to 5 seconds for Envoy to exit gracefully (ISSUE-51).
			envoyDone := make(chan struct{})

			go func() {
				waitErr := envoyCmd.Wait()
				if waitErr != nil {
					slog.DebugContext(ctx, "envoy exited", slog.Any("err", waitErr))
				}

				close(envoyDone)
			}()

			select {
			case <-envoyDone:
			case <-time.After(envoyDrainTimeout):
				slog.WarnContext(ctx, "envoy did not exit within drain timeout, proceeding")
			}
		}
	}

	// Stop DNS proxy after Envoy is down.
	if dnsProxy != nil {
		err := dnsProxy.Shutdown()
		if err != nil {
			slog.DebugContext(ctx, "stopping DNS proxy", slog.Any("err", err))
		}
	}

	// Flush iptables rules and destroy ipsets so a restart in the same
	// network namespace does not fail on pre-existing resources (ISSUE-53).
	cleanupIPTables(ctx)

	for _, name := range ipsets {
		//nolint:gosec // G204: name is from validated FQDNIPSetName output.
		destroyErr := exec.CommandContext(ctx, "ipset", "destroy", name).Run()
		if destroyErr != nil {
			slog.DebugContext(ctx, "destroying ipset on shutdown",
				slog.String("name", name),
				slog.Any("err", destroyErr),
			)
		}
	}
}

// cleanupIPTables flushes the sandbox iptables chains so that a
// restart in the same network namespace starts with clean state.
func cleanupIPTables(ctx context.Context) {
	for _, cmd := range []string{"iptables", "ip6tables"} {
		for _, table := range []string{"nat", "filter"} {
			//nolint:gosec // G204: cmd and table are from hardcoded lists.
			out, err := exec.CommandContext(ctx, cmd, "-t", table, "-F").CombinedOutput()
			if err != nil {
				slog.DebugContext(ctx, "flushing iptables on shutdown",
					slog.String("cmd", cmd),
					slog.String("table", table),
					slog.String("output", string(out)),
					slog.Any("err", err),
				)
			}
		}
	}
}

// installCA copies the sandbox CA certificate into the system trust
// store and runs update-ca-certificates. Falls back to direct bundle
// injection when update-ca-certificates is unavailable.
func installCA(ctx context.Context, caCertPath string) error {
	trustDest := "/usr/local/share/ca-certificates/sandbox-ca.crt"

	err := copyFile(caCertPath, trustDest)
	if err != nil {
		return fmt.Errorf("installing CA cert: %w", err)
	}

	err = exec.CommandContext(ctx, "update-ca-certificates").Run()
	if err != nil {
		slog.WarnContext(ctx, "update-ca-certificates not available, appending to CA bundle",
			slog.Any("err", err),
		)

		err = installCAToBundle(caCertPath)
		if err != nil {
			slog.WarnContext(ctx, "installing CA to bundle",
				slog.Any("err", err),
			)
		}
	}

	return nil
}

// disableIPv6 attempts to disable IPv6 on all interfaces via sysctl.
// Failures are logged but not returned because some kernels do not
// support the sysctl knobs.
func disableIPv6(ctx context.Context) {
	err := exec.CommandContext(ctx, "sysctl", "-w", "net.ipv6.conf.all.disable_ipv6=1").Run()
	if err != nil {
		slog.DebugContext(ctx, "disabling IPv6 on all interfaces", slog.Any("err", err))
	}

	err = exec.CommandContext(ctx, "sysctl", "-w", "net.ipv6.conf.default.disable_ipv6=1").Run()
	if err != nil {
		slog.DebugContext(ctx, "disabling IPv6 on default interface", slog.Any("err", err))
	}
}

// verifyIPv6State checks that IPv6 REDIRECT rules are loaded, or that
// IPv6 has been disabled system-wide. Returns [ErrIPv6Unsecured] when
// neither condition is met.
func verifyIPv6State(ctx context.Context) error {
	ip6Out, err := exec.CommandContext(ctx, "ip6tables-save").CombinedOutput()
	if err != nil {
		slog.DebugContext(ctx, "checking ip6tables rules", slog.Any("err", err))
	}

	if strings.Contains(string(ip6Out), "REDIRECT") || strings.Contains(string(ip6Out), "DROP") {
		return nil
	}

	disabled, err := os.ReadFile(
		"/proc/sys/net/ipv6/conf/all/disable_ipv6",
	)
	if err != nil {
		slog.DebugContext(ctx, "reading IPv6 disable flag", slog.Any("err", err))
	}

	if strings.TrimSpace(string(disabled)) != "1" {
		return ErrIPv6Unsecured
	}

	return nil
}

// firstListenerPort returns the first Envoy listener port to wait on.
// Prefers 15443 when FQDN rules produce a port 443 listener, then
// checks TCPForwards, then falls back to the first resolved port.
func firstListenerPort(cfg *SandboxConfig) int {
	ports := cfg.ResolvePorts()
	if slices.Contains(ports, 443) {
		return 15443
	}

	if len(cfg.TCPForwards) > 0 {
		return proxyPortBase + cfg.TCPForwards[0].Port
	}

	if len(ports) > 0 {
		return proxyPortBase + ports[0]
	}

	return 15443
}

// waitForListener polls a TCP address until it accepts connections or
// the timeout expires.
func waitForListener(ctx context.Context, addr string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dialer := net.Dialer{Timeout: 100 * time.Millisecond}

	for {
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err == nil {
			err := conn.Close()
			if err != nil {
				slog.DebugContext(ctx, "closing connectivity check connection", slog.Any("err", err))
			}

			return nil
		}

		if ctx.Err() != nil {
			return fmt.Errorf("listener %s not ready after %v", addr, timeout)
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// runCmd runs a command with stdin from a file (for iptables-restore).
func runCmd(ctx context.Context, name, inputFile string) error {
	f, err := os.Open(inputFile)
	if err != nil {
		return fmt.Errorf("opening %s: %w", inputFile, err)
	}

	defer func() {
		err := f.Close()
		if err != nil {
			slog.DebugContext(ctx, "closing input file", slog.String("path", inputFile), slog.Any("err", err))
		}
	}()

	cmd := exec.CommandContext(ctx, name)
	cmd.Stdin = f
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("running %s: %w", name, err)
	}

	return nil
}

// installCAToBundle appends a CA certificate to the system CA bundle and
// ensures SSL_CERT_FILE points to the updated bundle. This handles systems
// without update-ca-certificates (e.g. NixOS where the bundle is a
// read-only symlink into the nix store and SSL_CERT_FILE may point there).
func installCAToBundle(caCertPath string) error {
	caData, err := os.ReadFile(caCertPath)
	if err != nil {
		return fmt.Errorf("reading CA cert: %w", err)
	}

	// Collect candidate bundle paths: SSL_CERT_FILE first (what TLS
	// clients actually use), then well-known system paths.
	var candidates []string
	if env := os.Getenv("SSL_CERT_FILE"); env != "" {
		candidates = append(candidates, env)
	}

	if env := os.Getenv("NIX_SSL_CERT_FILE"); env != "" {
		candidates = append(candidates, env)
	}

	candidates = append(candidates,
		"/etc/ssl/certs/ca-certificates.crt",
		"/etc/ssl/certs/ca-bundle.crt",
		"/etc/pki/tls/certs/ca-bundle.crt",
	)

	// Deduplicate while preserving order.
	seen := make(map[string]bool)

	var bundles []string
	for _, c := range candidates {
		if c != "" && !seen[c] {
			seen[c] = true
			bundles = append(bundles, c)
		}
	}

	for _, bundle := range bundles {
		_, statErr := os.Stat(bundle) //nolint:gosec // G703: paths from hardcoded candidates.
		if statErr != nil {
			continue
		}

		err := appendToBundle(bundle, caData)
		if err != nil {
			slog.Warn("appending CA to bundle", //nolint:gosec // G706: bundle path from hardcoded candidates.
				slog.String("bundle", bundle),
				slog.Any("err", err),
			)

			continue
		}

		// Point SSL_CERT_FILE to the writable bundle so child
		// processes (running as uid 1000) pick it up.
		envErr := os.Setenv("SSL_CERT_FILE", bundle)
		if envErr != nil {
			slog.Debug("setting SSL_CERT_FILE", slog.Any("err", envErr))
		}

		return nil
	}

	return fmt.Errorf("no system CA bundle found")
}

// appendToBundle appends caData to the bundle file. If the file is a
// symlink (e.g. into the read-only nix store), it is replaced with a
// writable copy first.
func appendToBundle(bundle string, caData []byte) error {
	fi, err := os.Lstat(bundle) //nolint:gosec // G703: path from caller.
	if err != nil {
		return fmt.Errorf("stat %s: %w", bundle, err)
	}

	// Replace symlinks with a writable copy.
	if fi.Mode()&os.ModeSymlink != 0 {
		existing, err := os.ReadFile(bundle) //nolint:gosec // G703: path from caller.
		if err != nil {
			return fmt.Errorf("reading %s: %w", bundle, err)
		}

		err = os.Remove(bundle) //nolint:gosec // G703: path from caller.
		if err != nil {
			return fmt.Errorf("removing symlink %s: %w", bundle, err)
		}

		err = os.WriteFile(bundle, existing, 0o644) //nolint:gosec // G703: replacing symlink with writable copy.
		if err != nil {
			return fmt.Errorf("writing %s: %w", bundle, err)
		}
	}

	f, err := os.OpenFile(bundle, os.O_APPEND|os.O_WRONLY, 0o644) //nolint:gosec // G703: path from caller.
	if err != nil {
		return fmt.Errorf("opening %s: %w", bundle, err)
	}

	_, err = f.Write(append([]byte("\n"), caData...))
	if err != nil {
		closeErr := f.Close()
		if closeErr != nil {
			//nolint:gosec // G706: bundle path from caller.
			slog.Debug("closing bundle file after write error",
				slog.String("path", bundle),
				slog.Any("err", closeErr),
			)
		}

		return fmt.Errorf("appending to %s: %w", bundle, err)
	}

	err = f.Close()
	if err != nil {
		return fmt.Errorf("closing %s: %w", bundle, err)
	}

	return nil
}

// copyFile copies a file from src to dst, creating parent directories.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("reading %s: %w", src, err)
	}

	err = os.MkdirAll(filepath.Dir(dst), 0o755)
	if err != nil {
		return fmt.Errorf("creating dir for %s: %w", dst, err)
	}

	err = os.WriteFile(dst, data, 0o644) //nolint:gosec // G703: path from caller.
	if err != nil {
		return fmt.Errorf("writing %s: %w", dst, err)
	}

	return nil
}
