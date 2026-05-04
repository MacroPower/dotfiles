package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// allowAPIServerHostFlag is the CLI flag name shared by `serve`
// and `host select`. Centralized so the serve-side argv builder
// in [*handler.selectArgs] can never drift from what host select
// parses.
const allowAPIServerHostFlag = "allow-apiserver-host"

// Package-level indirections used by the host subcommands. Tests
// override these to inject a fake [KubeClient] and to capture
// stdout without forking the binary.
var (
	hostKubeClient = func(kubeconfigPath, context string) (KubeClient, error) {
		return NewKubeClientFromKubeconfig(kubeconfigPath, context)
	}
	hostStdout io.Writer = os.Stdout
)

// splitLeadingPositional pulls the leading positional argument off
// the front of an argv slice. host select expects `<context-name>
// --flag ...`; the standard flag package would otherwise stop
// parsing at the first non-flag and leave the flags in Args().
func splitLeadingPositional(args []string) (string, []string, error) {
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		return "", nil, fmt.Errorf("%w: context name", ErrMissingContext)
	}

	return args[0], args[1:], nil
}

// stateHomeDir returns the parent directory used for per-`serve`
// kubeconfig files. Honors $XDG_STATE_HOME, falling back to
// ~/.local/state when unset. Resolved on the host side because
// the file lives on the host filesystem; a Lima-guest serve sees
// the same path through the writable bind mount declared in
// workmux's extra_mounts.
func stateHomeDir() string {
	state := os.Getenv("XDG_STATE_HOME")
	if state != "" {
		return filepath.Join(state, "mcp-kubectx")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".local", "state", "mcp-kubectx")
	}

	return filepath.Join(home, ".local", "state", "mcp-kubectx")
}

// resolveHostKubeconfigPath returns the kubeconfig path to read,
// preferring the explicit flag value, then $KUBECONFIG, then
// ~/.kube/config. Used by both the host subcommands and the serve
// handler.
func resolveHostKubeconfigPath(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}

	if env := os.Getenv("KUBECONFIG"); env != "" {
		return env
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".kube", "config")
	}

	return filepath.Join(home, ".kube", "config")
}

// runHostList prints the available kubeconfig contexts to stdout.
// Output is byte-identical to the historical MCP `list` tool result
// so the serve-side handler.list can pass stdout straight through.
func runHostList(args []string) error {
	fs := flag.NewFlagSet("host list", flag.ContinueOnError)

	kubeconfig := fs.String("kubeconfig", "", "path to host kubeconfig (default: $KUBECONFIG or ~/.kube/config)")

	err := fs.Parse(args)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrParseHostListFlags, err)
	}

	cfg, err := loadKubeconfig(resolveHostKubeconfigPath(*kubeconfig))
	if err != nil {
		return err
	}

	if len(cfg.Contexts) == 0 {
		_, err = fmt.Fprint(hostStdout, "No contexts found.")
		return err //nolint:wrapcheck // direct write error needs no wrapping
	}

	var b strings.Builder

	b.WriteString("Available contexts:\n")

	for _, c := range cfg.Contexts {
		if c.Name == cfg.CurrentContext {
			fmt.Fprintf(&b, "- %s (current)\n", c.Name)
		} else {
			fmt.Fprintf(&b, "- %s\n", c.Name)
		}
	}

	_, err = fmt.Fprint(hostStdout, b.String())

	return err //nolint:wrapcheck // direct write error needs no wrapping
}

// HostSelectResult is the JSON payload printed by `host select` on
// success. The serve-side handler parses it back to register the
// release callback and to surface the kubeconfig path to the MCP
// caller. The binding name is not on the wire -- both sides derive
// it from [bindingNameForSA].
type HostSelectResult struct {
	Path          string `json:"path"`
	SAName        string `json:"saName"`
	Namespace     string `json:"namespace"`
	Kubeconfig    string `json:"kubeconfig"`
	Context       string `json:"context"`
	ClusterScoped bool   `json:"clusterScoped"`
}

// Sentinel errors for the host subcommands. ErrSelectMissingPid
// signals that host select was invoked without --out-path and
// without --pid: when path resolution falls back to the host-side
// default, serve must supply its own pid as the discriminator so
// concurrent host + guest serves never overwrite each other.
//
//nolint:grouper // sentinels grouped separately from the test-indirection vars further up
var (
	ErrSelectMissingPid      = errors.New("--pid is required when --out-path is empty")
	ErrParseHostSelectFlags  = errors.New("parse host select flags")
	ErrParseHostListFlags    = errors.New("parse host list flags")
	ErrParseHostTokenFlags   = errors.New("parse host token flags")
	ErrTokenMissingSA        = errors.New("--sa is required")
	ErrTokenMissingNamespace = errors.New("--namespace is required")
	ErrAPIServerNotAllowed   = errors.New("apiserver host not in allowlist")
)

// clusterServerHost extracts the hostname from a kubeconfig
// cluster's `server` URL. yaml.v3 always decodes string-keyed
// maps into map[string]any, so a single type assertion covers
// every well-formed kubeconfig.
func clusterServerHost(cluster any) (string, error) {
	m, ok := cluster.(map[string]any)
	if !ok {
		return "", fmt.Errorf("cluster is not an object: %T", cluster)
	}

	server, ok := m["server"].(string)
	if !ok || server == "" {
		return "", fmt.Errorf("cluster.server missing or not a string")
	}

	u, err := url.Parse(server)
	if err != nil {
		return "", fmt.Errorf("parse cluster.server %q: %w", server, err)
	}

	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("cluster.server %q has empty host", server)
	}

	return host, nil
}

// stringSliceFlag accumulates repeated occurrences of a string
// flag into a slice.
type stringSliceFlag []string

func (s *stringSliceFlag) String() string { return strings.Join(*s, ",") }

func (s *stringSliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// runHostSelect creates an SA + binding and writes a scoped
// kubeconfig whose user.exec block points at `host token`. The
// caller owns cleanup and on-demand token minting.
func runHostSelect(args []string) error {
	contextName, rest, err := splitLeadingPositional(args)
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet("host select", flag.ContinueOnError)

	kubeconfig := fs.String("kubeconfig", "", "path to host kubeconfig (default: $KUBECONFIG or ~/.kube/config)")
	outPath := fs.String(
		"out-path", "",
		"destination for the scoped kubeconfig (default: <stateHomeDir>/kubeconfig.<pid>.<env>.yaml)",
	)
	pid := fs.Int("pid", 0, "serve process pid (required when --out-path is empty; ignored otherwise)")
	forGuest := fs.Bool("for-guest", false, "write a kubeconfig whose exec plugin uses workmux host-exec")
	saRole := fs.String("sa-role-name", "", "name of the Role or ClusterRole to bind (required)")
	saRoleKind := fs.String("sa-role-kind", roleKindClusterRole, "kind of role to bind: Role or ClusterRole")
	saClusterScoped := fs.Bool("sa-cluster-scoped", false, "create a ClusterRoleBinding instead of a RoleBinding")
	saNamespace := fs.String(
		"sa-namespace", "",
		"namespace for the ServiceAccount (default: context namespace or \"default\")",
	)
	saExpiration := fs.Int("sa-expiration", 0, "ServiceAccount token lifetime in seconds (default: 3600, max: 86400)")

	var allowedHosts stringSliceFlag

	fs.Var(
		&allowedHosts,
		allowAPIServerHostFlag,
		"hostname permitted as cluster.server (repeatable; empty = allow any)",
	)

	err = fs.Parse(rest)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrParseHostSelectFlags, err)
	}

	resolvedOutPath := *outPath
	if resolvedOutPath == "" {
		if *pid <= 0 {
			return ErrSelectMissingPid
		}

		env := "host"
		if *forGuest {
			env = "guest"
		}

		resolvedOutPath = filepath.Join(
			stateHomeDir(),
			fmt.Sprintf("kubeconfig.%d.%s.yaml", *pid, env),
		)
	}

	sa := saConfig{
		role:          *saRole,
		roleKind:      *saRoleKind,
		clusterScoped: *saClusterScoped,
		namespace:     *saNamespace,
		expiration:    *saExpiration,
	}

	err = sa.validate()
	if err != nil {
		return fmt.Errorf("invalid service account config: %w", err)
	}

	hostKubeconfig := resolveHostKubeconfigPath(*kubeconfig)

	cfg, err := loadKubeconfig(hostKubeconfig)
	if err != nil {
		return err
	}

	var found *namedContext

	for i := range cfg.Contexts {
		if cfg.Contexts[i].Name == contextName {
			found = &cfg.Contexts[i]
			break
		}
	}

	if found == nil {
		return fmt.Errorf("%w: %s", ErrContextNotFound, contextName)
	}

	var cluster *namedCluster

	for i := range cfg.Clusters {
		if cfg.Clusters[i].Name == found.Context.Cluster {
			cluster = &cfg.Clusters[i]
			break
		}
	}

	if len(allowedHosts) > 0 {
		if cluster == nil {
			return fmt.Errorf("%w: context %q has no cluster entry", ErrAPIServerNotAllowed, contextName)
		}

		host, hostErr := clusterServerHost(cluster.Cluster)
		if hostErr != nil {
			return hostErr
		}

		if !slices.Contains(allowedHosts, host) {
			return fmt.Errorf("%w: %s", ErrAPIServerNotAllowed, host)
		}
	}

	client, err := hostKubeClient(hostKubeconfig, contextName)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBuildKubeClient, err)
	}

	namespace := resolveSANamespace(sa, found)

	ctx := context.Background()

	saName, err := createSAWithBinding(ctx, client, sa, namespace)
	if err != nil {
		return err
	}

	plugin, err := buildExecPlugin(execPluginParams{
		KubeconfigPath: hostKubeconfig,
		Context:        contextName,
		SAName:         saName,
		Namespace:      namespace,
		Expiration:     sa.expiration,
		ForGuest:       *forGuest,
	})
	if err != nil {
		return err
	}

	out := kubeConfig{
		APIVersion:     "v1",
		Kind:           "Config",
		CurrentContext: contextName,
		Contexts: []namedContext{{
			Name: contextName,
			Context: contextDetails{
				Cluster:   found.Context.Cluster,
				User:      saName,
				Namespace: namespace,
			},
		}},
		Users: []namedUser{{
			Name: saName,
			User: map[string]any{"exec": plugin},
		}},
	}

	if cluster != nil {
		out.Clusters = []namedCluster{*cluster}
	}

	data, err := yaml.Marshal(&out)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrWriteKubeconfig, err)
	}

	err = writeFileSecure(resolvedOutPath, data)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrWriteKubeconfig, err)
	}

	result := HostSelectResult{
		Path:          resolvedOutPath,
		SAName:        saName,
		Namespace:     namespace,
		Kubeconfig:    hostKubeconfig,
		Context:       contextName,
		ClusterScoped: sa.clusterScoped,
	}

	enc := json.NewEncoder(hostStdout)

	err = enc.Encode(&result)
	if err != nil {
		return fmt.Errorf("encode result: %w", err)
	}

	return nil
}

// ExecCredential is the kubectl exec credential plugin output
// schema. Only the fields kubectl reads on success are populated.
type ExecCredential struct {
	APIVersion string               `json:"apiVersion"`
	Kind       string               `json:"kind"`
	Status     ExecCredentialStatus `json:"status"`
}

// ExecCredentialStatus carries the bearer token kubectl uses for
// the next API call. kubectl caches the credential by
// expirationTimestamp and only re-invokes the plugin once it expires.
type ExecCredentialStatus struct {
	ExpirationTimestamp string `json:"expirationTimestamp"`
	Token               string `json:"token"`
}

// runHostToken mints a fresh ServiceAccount token via TokenRequest
// and prints an [ExecCredential] JSON document. Invoked by kubectl
// (and other client-go consumers) through the exec auth plugin.
//
// Does NOT read [guestEnvVar]: the guest/host distinction lives
// only in serve. Recursion via `workmux host-exec` is impossible
// because token never constructs a *handler.
func runHostToken(args []string) error {
	fs := flag.NewFlagSet("host token", flag.ContinueOnError)

	kubeconfig := fs.String("kubeconfig", "", "path to host kubeconfig (default: $KUBECONFIG or ~/.kube/config)")
	contextName := fs.String("context", "", "kubeconfig context to use")
	saName := fs.String("sa", "", "ServiceAccount name (required)")
	namespace := fs.String("namespace", "", "ServiceAccount namespace (required)")
	saExpiration := fs.Int("sa-expiration", defaultExpiration, "token lifetime in seconds")

	err := fs.Parse(args)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrParseHostTokenFlags, err)
	}

	if *saName == "" {
		return ErrTokenMissingSA
	}

	if *namespace == "" {
		return ErrTokenMissingNamespace
	}

	client, err := hostKubeClient(resolveHostKubeconfigPath(*kubeconfig), *contextName)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBuildKubeClient, err)
	}

	expiration := time.Duration(*saExpiration) * time.Second
	if expiration <= 0 {
		expiration = time.Duration(defaultExpiration) * time.Second
	}

	token, expiry, err := client.CreateTokenRequest(context.Background(), *namespace, *saName, expiration)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrTokenRequest, err)
	}

	cred := ExecCredential{
		APIVersion: execAuthAPIVersion,
		Kind:       "ExecCredential",
		Status: ExecCredentialStatus{
			ExpirationTimestamp: expiry.UTC().Format(time.RFC3339),
			Token:               token,
		},
	}

	enc := json.NewEncoder(hostStdout)

	err = enc.Encode(&cred)
	if err != nil {
		return fmt.Errorf("encode credential: %w", err)
	}

	return nil
}

// runHostRelease deletes a ServiceAccount and its associated binding.
// Always exits 0 after logging any errors -- transient API hiccups,
// NotFound, and exec failures alike. Returning non-zero would
// otherwise force serve to retry the cleanup over the entire process
// lifetime, which is worse than the leak.
func runHostRelease(args []string) error {
	fs := flag.NewFlagSet("host release", flag.ContinueOnError)

	kubeconfig := fs.String("kubeconfig", "", "path to host kubeconfig (default: $KUBECONFIG or ~/.kube/config)")
	contextName := fs.String("context", "", "kubeconfig context to use")
	saName := fs.String("sa", "", "ServiceAccount name (required)")
	namespace := fs.String("namespace", "", "ServiceAccount namespace (required)")
	clusterScoped := fs.Bool("sa-cluster-scoped", false, "the binding is a ClusterRoleBinding")

	err := fs.Parse(args)
	if err != nil {
		// flag.ContinueOnError already prints the error to stderr,
		// but we still log a structured warning and return success
		// so that serve never retries the call.
		slog.Warn("parse host release flags", slog.Any("error", err))
		return nil
	}

	if *saName == "" || *namespace == "" {
		slog.Warn("host release missing required flags",
			slog.String("sa", *saName),
			slog.String("namespace", *namespace),
		)

		return nil
	}

	client, err := hostKubeClient(resolveHostKubeconfigPath(*kubeconfig), *contextName)
	if err != nil {
		slog.Warn("build kube client for release",
			slog.String("sa", *saName),
			slog.Any("error", err),
		)

		return nil
	}

	bindingName := bindingNameForSA(*saName)

	ctx := context.Background()

	// Binding and SA deletes are independent; running them in
	// parallel halves the K8s API round-trips per release.
	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()

		var err error
		if *clusterScoped {
			err = client.DeleteClusterRoleBinding(ctx, bindingName)
		} else {
			err = client.DeleteRoleBinding(ctx, *namespace, bindingName)
		}

		if err != nil {
			slog.WarnContext(ctx, "delete binding",
				slog.String("name", bindingName),
				slog.Bool("clusterScoped", *clusterScoped),
				slog.Any("error", err),
			)
		}
	}()

	go func() {
		defer wg.Done()

		err := client.DeleteServiceAccount(ctx, *namespace, *saName)
		if err != nil {
			slog.WarnContext(ctx, "delete service account",
				slog.String("name", *saName),
				slog.Any("error", err),
			)
		}
	}()

	wg.Wait()

	return nil
}
