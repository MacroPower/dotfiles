package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/execplugin"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kube"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kubeconfig"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/ledger"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/serviceaccount"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/socket"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/statedir"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/statefile"
)

// allowAPIServerHostFlag is the CLI flag name shared by `serve`
// and `host select`. Centralized so the serve-side argv builder
// in [*handler.selectArgs] can never drift from what host select
// parses.
const allowAPIServerHostFlag = "allow-apiserver-host"

// Package-level indirections used by the host subcommands. Tests
// override these to inject a fake [kube.Client], to capture stdout
// without forking the binary, and to keep the provision records in
// a temporary directory.
var (
	hostKubeClient = func(kubeconfigPath, context string) (kube.Client, error) {
		return kube.NewClientset(kubeconfigPath, context)
	}
	hostStdout io.Writer = os.Stdout
	// hostLedgerDir returns the directory of the provision
	// records. The directory lies beside the sockets in
	// [socket.StateDir] and not in [statedir.Dir], because a Lima
	// guest can write to [statedir.Dir] of the host through the
	// bind mount. So only the `host` subcommands write records.
	// A guest can still run `host select` through `workmux
	// host-exec` with a --kubeconfig of its choice. So `host
	// sweep` handles only the records of the kubeconfig that it
	// sweeps.
	hostLedgerDir = func() string {
		return filepath.Join(socket.StateDir(), "ledger")
	}
)

// hostLedger returns the ledger of provision records that `host
// select` adds to, and that `host release` and `host sweep` remove
// from.
func hostLedger() *ledger.Ledger {
	return ledger.New(hostLedgerDir())
}

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

// resolveHostKubeconfigPath returns the kubeconfig path to read,
// preferring the explicit flag value, then $KUBECONFIG_HOST, then
// $KUBECONFIG, then ~/.kube/config. Used by both the host
// subcommands and the serve handler.
//
// $KUBECONFIG_HOST exists because the Claude Code launcher wrapper
// rewrites $KUBECONFIG to a per-session symlink under
// $CLAUDE_KUBECTX_DIR. The user's pre-existing $KUBECONFIG, when set,
// is preserved as $KUBECONFIG_HOST so mcp-kubectx can still read the
// source kubeconfig (contexts list, credentials for SA creation).
//
// A $KUBECONFIG that points inside $CLAUDE_KUBECTX_DIR is the scoped
// output the wrapper writes per `select`, not a source of contexts:
// it does not exist until the first select, and never enumerates the
// host's contexts. When a user relies on the default ~/.kube/config
// (no pre-existing $KUBECONFIG to preserve as $KUBECONFIG_HOST), the
// wrapper still rewrites $KUBECONFIG to that scoped path, so this
// branch is skipped to fall through to ~/.kube/config. The trailing
// path separator rejects sibling-directory confusion, matching the
// containment check in [sidecarSymlinkPath].
func resolveHostKubeconfigPath(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}

	if env := os.Getenv("KUBECONFIG_HOST"); env != "" {
		return env
	}

	if env := os.Getenv("KUBECONFIG"); env != "" && !insideClaudeKubectxDir(env) {
		return env
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".kube", "config")
	}

	return filepath.Join(home, ".kube", "config")
}

// insideClaudeKubectxDir reports whether path sits inside the
// per-session $CLAUDE_KUBECTX_DIR the Claude Code launcher wrapper
// creates for scoped kubeconfigs. Returns false when the env var is
// unset (out-of-wrapper invocation). The trailing path separator
// rejects sibling-directory confusion (e.g. CLAUDE_KUBECTX_DIR=
// /run/claude-kubectx.1 with path=/run/claude-kubectx.12/kubeconfig).
func insideClaudeKubectxDir(path string) bool {
	dir := os.Getenv("CLAUDE_KUBECTX_DIR")

	return dir != "" && strings.HasPrefix(path, dir+string(os.PathSeparator))
}

// runHostList prints the available kubeconfig contexts to stdout.
// Output is byte-identical to the historical MCP `list` tool result
// so the serve-side handler.list can pass stdout straight through.
func runHostList(args []string) error {
	fs := flag.NewFlagSet("host list", flag.ContinueOnError)

	kubeconfigPath := fs.String("kubeconfig", "", "path to host kubeconfig (default: $KUBECONFIG or ~/.kube/config)")

	err := fs.Parse(args)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrParseHostListFlags, err)
	}

	cfg, err := kubeconfig.Load(resolveHostKubeconfigPath(*kubeconfigPath))
	if err != nil {
		return err //nolint:wrapcheck // Load already wraps kubeconfig.ErrLoad
	}

	if len(cfg.Contexts) == 0 {
		_, err = fmt.Fprint(hostStdout, "No contexts found.")
		return err //nolint:wrapcheck // direct write error needs no wrapping
	}

	var b strings.Builder

	b.WriteString("Available contexts:\n")

	// writeContextLine keeps this byte-identical to the serve-side
	// merge output, which re-parses these lines.
	for _, c := range cfg.Contexts {
		writeContextLine(&b, c.Name, "", c.Name == cfg.CurrentContext)
	}

	_, err = fmt.Fprint(hostStdout, b.String())

	return err //nolint:wrapcheck // direct write error needs no wrapping
}

// HostSelectResult is the JSON payload printed by `host select` on
// success. The serve-side handler parses it back to register the
// release callback and to surface the kubeconfig path to the MCP
// caller. The binding name is not on the wire -- both sides derive
// it from [serviceaccount.BindingName].
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
	ErrSelectMissingPid = errors.New("--pid is required when --out-path is empty")
	// ErrClusterNotFound guards host select against a context whose
	// cluster entry is absent from the kubeconfig. Without the
	// guard, the SA and binding would be created and then stranded
	// behind a scoped kubeconfig whose dangling cluster reference
	// kubectl cannot resolve.
	ErrClusterNotFound       = errors.New("cluster entry not found for context")
	ErrParseHostSelectFlags  = errors.New("parse host select flags")
	ErrParseHostListFlags    = errors.New("parse host list flags")
	ErrParseHostTokenFlags   = errors.New("parse host token flags")
	ErrParseHostSweepFlags   = errors.New("parse host sweep flags")
	ErrTokenMissingSA        = errors.New("--sa is required")
	ErrTokenMissingNamespace = errors.New("--namespace is required")
	ErrAPIServerNotAllowed   = errors.New("apiserver host not in allowlist")
	ErrTokenRequest          = errors.New("token request")
	ErrBuildKubeClient       = errors.New("build kubernetes client")
	// ErrMissingHostID guards against running the sweep without a
	// host-id selector. An empty host id would either match nothing
	// (if no historical resources lack the label) or be unsafe (if
	// they do): a footgun, so refuse outright.
	ErrMissingHostID = errors.New("--host-id is required")
	// ErrInvalidHostID guards the sweep selector against injection.
	// [identity.New] produces 16 lowercase hex chars; any other
	// value is either a typo or a hand-crafted value with selector
	// metacharacters in it, so [runHostSweep] refuses to run.
	ErrInvalidHostID = errors.New("--host-id must be 16 lowercase hex characters")
)

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
//
// When the caller passes both --sa-host-id and --sa-instance-id,
// runHostSelect adds a provision record for the SA to the ledger
// before it creates the SA. A later `host sweep` of the same
// kubeconfig reads that record to delete the SA after the `serve`
// that owns it has died, on whichever cluster the SA lies. With
// both ids set, an id that is not 16 lowercase hex characters fails
// the call with [ledger.ErrInvalidID] before any cluster-side
// mutation. When the create fails, runHostSelect removes the record
// only after a failure that left nothing on the cluster, such as a
// create that RBAC forbids.
func runHostSelect(ctx context.Context, args []string) error {
	contextName, rest, err := splitLeadingPositional(args)
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet("host select", flag.ContinueOnError)

	kubeconfigPath := fs.String("kubeconfig", "", "path to host kubeconfig (default: $KUBECONFIG or ~/.kube/config)")
	outPath := fs.String(
		"out-path", "",
		"destination for the scoped kubeconfig (default: <stateDir>/kubeconfig.<pid>.<env>.yaml)",
	)
	pid := fs.Int("pid", 0, "serve process pid (required when --out-path is empty; ignored otherwise)")
	forGuest := fs.Bool(
		"for-guest",
		false,
		"path discriminator: when true, the defaulted kubeconfig and socket filenames carry the `guest` env tag rather than `host`",
	)
	socketPath := fs.String(
		"socket-path", "",
		"absolute path of the per-serve UDS the kubeconfig's exec plugin will connect to "+
			"(default: <socketStateDir>/serve.0.<env>.sock). serve passes its own resolved slot path "+
			"because in the guest case the path lives on the guest fs but host select runs host-side.",
	)
	saRole := fs.String("sa-role-name", "", "name of the Role or ClusterRole to bind (required)")
	saRoleKind := fs.String(
		"sa-role-kind", serviceaccount.RoleKindClusterRole,
		"kind of role to bind: Role or ClusterRole",
	)
	saClusterScoped := fs.Bool("sa-cluster-scoped", false, "create a ClusterRoleBinding instead of a RoleBinding")
	saNamespace := fs.String(
		"sa-namespace", "",
		"namespace for the ServiceAccount (default: context namespace or \"default\")",
	)
	saExpiration := fs.Int("sa-expiration", 0, "ServiceAccount token lifetime in seconds (default: 3600, max: 86400)")
	saInstanceID := fs.String(
		"sa-instance-id", "",
		"per-serve random identifier tagged on the SA + binding via the "+
			"mcp-kubectx/instance-id label (default: empty = label omitted)",
	)
	saHostID := fs.String(
		"sa-host-id", "",
		"persistent per-user-per-host identifier tagged on the SA + binding via the "+
			"mcp-kubectx/host-id label (default: empty = label omitted)",
	)

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

		resolvedOutPath = filepath.Join(
			statedir.Dir(),
			fmt.Sprintf("kubeconfig.%d.%s.yaml", *pid, statedir.EnvTag(*forGuest)),
		)
	}

	// Socket path defaults to socket.PathForSlot(0, *forGuest)
	// when --socket-path is empty. The default is only hit when
	// `host select` runs standalone (effectively tests); in
	// production, serve always forwards its own resolved slot path
	// via --socket-path. Slot 0 is the deterministic default and
	// matches what a single fresh serve would pick.
	resolvedSocketPath := *socketPath
	if resolvedSocketPath == "" {
		resolvedSocketPath = socket.PathForSlot(0, *forGuest)
	}

	sa := serviceaccount.Config{
		Role:          *saRole,
		RoleKind:      *saRoleKind,
		ClusterScoped: *saClusterScoped,
		Namespace:     *saNamespace,
		Expiration:    *saExpiration,
	}

	err = sa.Validate()
	if err != nil {
		return fmt.Errorf("invalid service account config: %w", err)
	}

	// The path is absolute so that `host token`, `host release`, and
	// a later `host sweep` read the same file from any working
	// directory.
	hostKubeconfig, err := filepath.Abs(resolveHostKubeconfigPath(*kubeconfigPath))
	if err != nil {
		return fmt.Errorf("resolve kubeconfig path: %w", err)
	}

	cfg, err := kubeconfig.Load(hostKubeconfig)
	if err != nil {
		return err //nolint:wrapcheck // Load already wraps kubeconfig.ErrLoad
	}

	var found *kubeconfig.NamedContext

	for i := range cfg.Contexts {
		if cfg.Contexts[i].Name == contextName {
			found = &cfg.Contexts[i]
			break
		}
	}

	if found == nil {
		return fmt.Errorf("%w: %s", ErrContextNotFound, contextName)
	}

	var cluster *kubeconfig.NamedCluster

	for i := range cfg.Clusters {
		if cfg.Clusters[i].Name == found.Context.Cluster {
			cluster = &cfg.Clusters[i]
			break
		}
	}

	// Refuse before any cluster-side mutation; see [ErrClusterNotFound].
	if cluster == nil {
		return fmt.Errorf("%w: context %q references cluster %q",
			ErrClusterNotFound, contextName, found.Context.Cluster)
	}

	if len(allowedHosts) > 0 {
		host, hostErr := kubeconfig.ServerHost(cluster.Cluster)
		if hostErr != nil {
			return hostErr //nolint:wrapcheck // ServerHost errors are self-describing
		}

		// DNS hostnames are case-insensitive, so the kubeconfig
		// author's casing must not defeat the allowlist.
		allowed := slices.ContainsFunc(allowedHosts, func(a string) bool {
			return strings.EqualFold(a, host)
		})
		if !allowed {
			return fmt.Errorf("%w: %s", ErrAPIServerNotAllowed, host)
		}
	}

	client, err := hostKubeClient(hostKubeconfig, contextName)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBuildKubeClient, err)
	}

	namespace := serviceaccount.ResolveNamespace(sa, found.Context.Namespace)

	saName, err := serviceaccount.NewName()
	if err != nil {
		return err //nolint:wrapcheck // NewName errors are self-describing
	}

	// The record goes to disk before the create call. A `host
	// select` that dies between the two steps then leaves a record
	// of an SA that does not exist, which a sweep removes, and never
	// an SA that no record names. Without both ids no `serve` owns
	// the SA, so no sweep can tell when to delete it.
	records := hostLedger()
	record := ledger.Record{
		HostID:         *saHostID,
		InstanceID:     *saInstanceID,
		Kubeconfig:     hostKubeconfig,
		Context:        contextName,
		Namespace:      namespace,
		ServiceAccount: saName,
		ClusterScoped:  sa.ClusterScoped,
	}
	recorded := *saHostID != "" && *saInstanceID != ""

	if recorded {
		err = records.Add(record)
		if err != nil {
			return err //nolint:wrapcheck // Add already wraps with its sentinel errors
		}
	}

	err = serviceaccount.CreateWithBinding(ctx, client, sa, namespace, saName, *saInstanceID, *saHostID)
	if err != nil {
		// The record goes only when the failed create left nothing
		// on the cluster. After a create call that the apiserver did
		// not refuse, the apiserver might have stored the SA or the
		// binding. AlreadyExists counts as such a call, because
		// client-go gets it when it sends the create again after an
		// attempt that stored the object. A failed rollback leaves
		// the SA. In each of these cases the record stays, so that a
		// sweep after this `serve` ends deletes whatever the cluster
		// holds.
		leftOnCluster := errors.Is(err, serviceaccount.ErrMaybeCreated) ||
			errors.Is(err, serviceaccount.ErrRollback)
		if recorded && !leftOnCluster {
			removeRecord(ctx, records, record)
		}

		return err //nolint:wrapcheck // CreateWithBinding already wraps with its sentinel errors
	}

	plugin := execplugin.New(resolvedSocketPath)

	out := kubeconfig.Config{
		APIVersion:     "v1",
		Kind:           "Config",
		CurrentContext: contextName,
		Clusters:       []kubeconfig.NamedCluster{*cluster},
		Contexts: []kubeconfig.NamedContext{{
			Name: contextName,
			Context: kubeconfig.Context{
				Cluster:   found.Context.Cluster,
				User:      saName,
				Namespace: namespace,
			},
		}},
		Users: []kubeconfig.NamedUser{{
			Name: saName,
			User: map[string]any{"exec": plugin},
		}},
	}

	data, err := out.Marshal()
	if err != nil {
		return err //nolint:wrapcheck // Marshal already wraps kubeconfig.ErrWrite
	}

	err = statefile.WriteSecure(resolvedOutPath, data)
	if err != nil {
		return fmt.Errorf("%w: %w", kubeconfig.ErrWrite, err)
	}

	result := HostSelectResult{
		Path:          resolvedOutPath,
		SAName:        saName,
		Namespace:     namespace,
		Kubeconfig:    hostKubeconfig,
		Context:       contextName,
		ClusterScoped: sa.ClusterScoped,
	}

	enc := json.NewEncoder(hostStdout)

	err = enc.Encode(&result)
	if err != nil {
		return fmt.Errorf("encode result: %w", err)
	}

	return nil
}

// runHostToken mints a fresh ServiceAccount token via TokenRequest
// and prints an [execplugin.Credential] JSON document. Invoked by
// kubectl (and other client-go consumers) through the exec auth
// plugin.
//
// Does NOT read [guestEnvVar]: the guest/host distinction lives
// only in serve. Recursion via `workmux host-exec` is impossible
// because token never constructs a *handler.
func runHostToken(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("host token", flag.ContinueOnError)

	kubeconfigPath := fs.String("kubeconfig", "", "path to host kubeconfig (default: $KUBECONFIG or ~/.kube/config)")
	contextName := fs.String("context", "", "kubeconfig context to use")
	saName := fs.String("sa", "", "ServiceAccount name (required)")
	namespace := fs.String("namespace", "", "ServiceAccount namespace (required)")
	saExpiration := fs.Int("sa-expiration", serviceaccount.DefaultExpiration, "token lifetime in seconds")

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

	// Mirror serviceaccount.Config.Validate's cap: the serve path
	// validates its expiration at startup, but this subcommand is
	// reachable directly, and an unbounded value would both violate
	// the documented 86400 cap and overflow time.Duration past
	// ~9.2e9 seconds.
	if *saExpiration > serviceaccount.MaxExpiration {
		return fmt.Errorf("%w: got %d", serviceaccount.ErrExpirationTooLong, *saExpiration)
	}

	client, err := hostKubeClient(resolveHostKubeconfigPath(*kubeconfigPath), *contextName)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBuildKubeClient, err)
	}

	expiration := time.Duration(*saExpiration) * time.Second
	if expiration <= 0 {
		expiration = time.Duration(serviceaccount.DefaultExpiration) * time.Second
	}

	token, expiry, err := client.CreateTokenRequest(ctx, *namespace, *saName, expiration)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrTokenRequest, err)
	}

	cred := execplugin.Credential{
		APIVersion: execplugin.APIVersion,
		Kind:       "ExecCredential",
		Status: execplugin.CredentialStatus{
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
// It logs each failure and always exits 0, for a transient API
// failure and for an exec failure alike. A non-zero exit makes
// serve retry the cleanup for the rest of its lifetime, and that
// retry is worse than the leak.
//
// After the cluster holds neither the SA nor the binding,
// runHostRelease removes the provision records of the SA from the
// ledger. A resource that the cluster did not hold counts as
// deleted. When a delete fails, the records stay, and a `host
// sweep` deletes the SA after the `serve` that owns it has died.
func runHostRelease(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("host release", flag.ContinueOnError)

	kubeconfigPath := fs.String("kubeconfig", "", "path to host kubeconfig (default: $KUBECONFIG or ~/.kube/config)")
	contextName := fs.String("context", "", "kubeconfig context to use")
	saName := fs.String("sa", "", "ServiceAccount name (required)")
	namespace := fs.String("namespace", "", "ServiceAccount namespace (required)")
	clusterScoped := fs.Bool("sa-cluster-scoped", false, "the binding is a ClusterRoleBinding")

	err := fs.Parse(args)
	if err != nil {
		// flag.ContinueOnError already prints the error to stderr,
		// but we still log a structured warning and return success
		// so that serve never retries the call.
		slog.WarnContext(ctx, "parse host release flags", slog.Any("error", err))
		return nil
	}

	if *saName == "" || *namespace == "" {
		slog.WarnContext(ctx, "host release missing required flags",
			slog.String("sa", *saName),
			slog.String("namespace", *namespace),
		)

		return nil
	}

	hostKubeconfig := resolveHostKubeconfigPath(*kubeconfigPath)

	client, err := hostKubeClient(hostKubeconfig, *contextName)
	if err != nil {
		slog.WarnContext(ctx, "build kube client for release",
			slog.String("sa", *saName),
			slog.Any("error", err),
		)

		return nil
	}

	err = serviceaccount.Release(ctx, client, *namespace, *saName, *clusterScoped)
	if err != nil {
		slog.WarnContext(ctx, "release service account",
			slog.String("namespace", *namespace),
			slog.String("name", *saName),
			slog.Bool("clusterScoped", *clusterScoped),
			slog.Any("error", err),
		)

		return nil
	}

	forgetServiceAccount(ctx, hostLedger(), hostKubeconfig, *contextName, *namespace, *saName)

	return nil
}
