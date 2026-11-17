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
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/identity"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kube"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kubeconfig"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/ledger"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/serviceaccount"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/statedir"
)

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
	// records. The directory lies in [statedir.RunDir], which
	// workmux never mounts into a Lima guest. `host token` and
	// `host release` trust a record as the proof that a `serve`
	// provisioned its ServiceAccount, so only the `host`
	// subcommands must write the directory.
	hostLedgerDir = func() string {
		return filepath.Join(statedir.RunDir(), "ledger")
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

// resolveHostKubeconfigPath returns the absolute path of the host
// kubeconfig. It takes $KUBECONFIG_HOST, then $KUBECONFIG, then
// ~/.kube/config. No flag sets the path, because a Lima guest runs
// the `host` subcommands through `workmux host-exec` with an argv
// of its own choice. client-go runs the exec credential plugin of
// each kubeconfig that it loads, so a kubeconfig of the guest would
// run a command of the guest on the host. `workmux host-exec` passes
// none of these variables, so a `host` subcommand that a guest
// starts reads ~/.kube/config.
//
// $KUBECONFIG_HOST exists because the Claude Code launcher wrapper
// rewrites $KUBECONFIG to a per-session symlink under
// $CLAUDE_KUBECTX_DIR. The wrapper keeps the user's pre-existing
// $KUBECONFIG, when set, as $KUBECONFIG_HOST. So mcp-kubectx can
// still read the source kubeconfig, which holds the contexts and the
// credentials for SA creation.
//
// A $KUBECONFIG that points inside $CLAUDE_KUBECTX_DIR is the scoped
// output that the wrapper writes for each `select`. It is not a
// source of contexts, because it does not exist until the first
// select and never lists the host's contexts. A user can rely on the
// default ~/.kube/config with no pre-existing $KUBECONFIG. The
// wrapper then still rewrites $KUBECONFIG to that scoped path, so
// the resolution skips it and falls through to ~/.kube/config. The
// trailing path separator of the containment check rejects
// sibling-directory confusion, as in [sidecarSymlinkPath].
//
// It returns [ErrNoHome] when it falls through to ~/.kube/config and
// cannot resolve an absolute home directory. A relative path would
// resolve in the working directory of the subcommand, which is a
// worktree that the guest can write under `workmux host-exec`.
func resolveHostKubeconfigPath() (string, error) {
	path := os.Getenv("KUBECONFIG_HOST")

	if env := os.Getenv("KUBECONFIG"); path == "" && env != "" && !insideClaudeKubectxDir(env) {
		path = env
	}

	if path == "" {
		home, err := absHome()
		if err != nil {
			return "", err
		}

		return filepath.Join(home, ".kube", "config"), nil
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve kubeconfig path: %w", err)
	}

	return abs, nil
}

// insideClaudeKubectxDir reports whether path sits inside the
// per-session $CLAUDE_KUBECTX_DIR the Claude Code launcher wrapper
// creates for scoped kubeconfigs. Returns false when the env var is
// unset (out-of-wrapper invocation). The trailing path separator
// rejects sibling-directory confusion (for example
// CLAUDE_KUBECTX_DIR=/run/claude-kubectx.1 with
// path=/run/claude-kubectx.12/kubeconfig).
func insideClaudeKubectxDir(path string) bool {
	dir := os.Getenv("CLAUDE_KUBECTX_DIR")

	return dir != "" && strings.HasPrefix(path, dir+string(os.PathSeparator))
}

// runHostList prints the contexts of the host kubeconfig at
// kubeconfigPath to stdout. Output is byte-identical to the
// historical MCP `list` tool result so the serve-side handler.list
// can pass stdout straight through.
func runHostList(kubeconfigPath string, args []string) error {
	fs := flag.NewFlagSet("host list", flag.ContinueOnError)

	err := fs.Parse(args)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrParseHostListFlags, err)
	}

	cfg, err := kubeconfig.Load(kubeconfigPath)
	if err != nil {
		return err //nolint:wrapcheck // Load already wraps kubeconfig.ErrLoad
	}

	if len(cfg.Contexts) == 0 {
		_, err = fmt.Fprint(hostStdout, "No contexts found.")
		return err //nolint:wrapcheck // a direct write error does not need wrapping
	}

	var b strings.Builder

	b.WriteString("Available contexts:\n")

	// writeContextLine keeps this byte-identical to the serve-side
	// merge output, which re-parses these lines.
	for _, c := range cfg.Contexts {
		writeContextLine(&b, c.Name, "", c.Name == cfg.CurrentContext)
	}

	_, err = fmt.Fprint(hostStdout, b.String())

	return err //nolint:wrapcheck // a direct write error does not need wrapping
}

// HostSelectResult is the JSON payload printed by `host select` on
// success. `serve` builds its scoped kubeconfig from it and registers
// the release callback of the ServiceAccount. The result leaves out
// the binding name, because both sides derive it from
// [serviceaccount.BindingName].
type HostSelectResult struct {
	// Cluster is the cluster entry of the selected context, copied
	// from the host kubeconfig.
	Cluster kubeconfig.NamedCluster `json:"cluster"`
	// SAName is the name of the created ServiceAccount.
	SAName string `json:"saName"`
	// Namespace is the namespace of the ServiceAccount.
	Namespace string `json:"namespace"`
	// Context is the name of the selected context.
	Context string `json:"context"`
	// Binding describes the role binding of the ServiceAccount, as
	// [serviceaccount.Describe] does.
	Binding string `json:"binding"`
}

// Sentinel errors for the host subcommands.
//
//nolint:grouper // sentinels grouped separately from the test-indirection vars further up
var (
	// ErrClusterNotFound guards host select against a context whose
	// cluster entry is absent from the kubeconfig. Without the
	// guard, host select creates the SA and binding, and `serve`
	// strands them behind a scoped kubeconfig whose dangling
	// cluster reference kubectl cannot resolve.
	ErrClusterNotFound      = errors.New("cluster entry not found for context")
	ErrParseHostSelectFlags = errors.New("parse host select flags")
	ErrParseHostListFlags   = errors.New("parse host list flags")
	ErrParseHostTokenFlags  = errors.New("parse host token flags")
	ErrParseHostSweepFlags  = errors.New("parse host sweep flags")
	ErrTokenMissingSA       = errors.New("--sa is required")
	ErrTokenMissingNS       = errors.New("--namespace is required")
	ErrAPIServerNotAllowed  = errors.New("apiserver host not in allowlist")
	ErrTokenRequest         = errors.New("token request")
	ErrBuildKubeClient      = errors.New("build kubernetes client")
	// ErrNotProvisioned guards `host token` against a ServiceAccount
	// that no `serve` provisioned. The host kubeconfig can mint a
	// token for any ServiceAccount of a cluster. So `host token`
	// mints only for a ServiceAccount whose provision record names
	// the host id and the instance id of the caller.
	ErrNotProvisioned = errors.New("service account has no provision record of the caller")
	// ErrMissingHostID guards against running the sweep without a
	// host-id selector. A sweep without the id has no owner to
	// select, so it refuses to run.
	ErrMissingHostID = errors.New("--host-id is required")
	// ErrInvalidHostID guards the label selectors and the names of
	// the provision records. [identity.New] produces 16 lowercase
	// hex chars, and any other value is either a typo or a
	// hand-crafted value.
	ErrInvalidHostID = errors.New("host id must be 16 lowercase hex characters")
	// ErrInvalidInstanceID guards the labels and the names of the
	// provision records in the same way as [ErrInvalidHostID].
	ErrInvalidInstanceID = errors.New("instance id must be 16 lowercase hex characters")
)

// stringSliceFlag accumulates repeated occurrences of a string
// flag into a slice.
type stringSliceFlag []string

func (s *stringSliceFlag) String() string { return strings.Join(*s, ",") }

func (s *stringSliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// checkOwner returns an error when hostID or instanceID is not an id
// that [identity.New] can produce. hostFlag and instanceFlag name
// the flags of the two ids in the error.
func checkOwner(hostFlag, hostID, instanceFlag, instanceID string) error {
	if !identity.Valid(hostID) {
		return fmt.Errorf("%w: %s %q", ErrInvalidHostID, hostFlag, hostID)
	}

	if !identity.Valid(instanceID) {
		return fmt.Errorf("%w: %s %q", ErrInvalidInstanceID, instanceFlag, instanceID)
	}

	return nil
}

// runHostSelect creates an SA + binding on the cluster of a context
// of the host kubeconfig at kubeconfigPath, and prints a
// [HostSelectResult]. It writes no kubeconfig. `serve` writes the
// scoped kubeconfig from the result on its own machine.
//
// The host config cfg sets the role, the binding, the namespace, and
// the apiserver allowlist. The argv sets only the context, the host
// id, and the instance id. A Lima guest can run `host select` with
// any argv through `workmux host-exec`, so it gets only the role
// that cfg grants, on an apiserver that cfg allows.
//
// runHostSelect requires --sa-host-id and --sa-instance-id. It adds
// a provision record for the SA to the ledger before it creates the
// SA. The record carries the [identity.Digest] of the host id, and
// so do the labels of the SA and the binding. A later `host token`
// and `host release` of the same caller act on the SA through the
// record. A later `host sweep` of the same kubeconfig reads the
// record to delete the SA after the `serve` that owns it has died,
// on whichever cluster the SA lies. The record carries the time of
// its write. A `host sweep` keeps the record when its caller took
// the live set before that time, because that live set might miss
// the `serve`. When the create fails, runHostSelect removes the
// record only after a failure that left nothing on the cluster,
// such as a create that RBAC forbids.
func runHostSelect(ctx context.Context, kubeconfigPath string, cfg hostConfig, args []string) error {
	contextName, rest, err := splitLeadingPositional(args)
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet("host select", flag.ContinueOnError)

	saInstanceID := fs.String(
		"sa-instance-id", "",
		"per-serve random identifier tagged on the SA + binding via the "+
			"mcp-kubectx/instance-id label (required)",
	)
	saHostID := fs.String(
		"sa-host-id", "",
		"persistent per-user-per-host identifier; its digest is tagged on the SA + binding via the "+
			"mcp-kubectx/host-id label (required)",
	)

	err = fs.Parse(rest)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrParseHostSelectFlags, err)
	}

	err = checkOwner("--sa-host-id", *saHostID, "--sa-instance-id", *saInstanceID)
	if err != nil {
		return err
	}

	sa := cfg.Config

	err = sa.Validate()
	if err != nil {
		return fmt.Errorf("invalid service account config: %w", err)
	}

	hostConfigFile, err := kubeconfig.Load(kubeconfigPath)
	if err != nil {
		return err //nolint:wrapcheck // Load already wraps kubeconfig.ErrLoad
	}

	var found *kubeconfig.NamedContext

	for i := range hostConfigFile.Contexts {
		if hostConfigFile.Contexts[i].Name == contextName {
			found = &hostConfigFile.Contexts[i]
			break
		}
	}

	if found == nil {
		return fmt.Errorf("%w: %s", ErrContextNotFound, contextName)
	}

	var cluster *kubeconfig.NamedCluster

	for i := range hostConfigFile.Clusters {
		if hostConfigFile.Clusters[i].Name == found.Context.Cluster {
			cluster = &hostConfigFile.Clusters[i]
			break
		}
	}

	// Refuse before any cluster-side mutation. [ErrClusterNotFound]
	// says why.
	if cluster == nil {
		return fmt.Errorf("%w: context %q references cluster %q",
			ErrClusterNotFound, contextName, found.Context.Cluster)
	}

	if len(cfg.AllowedAPIServerHosts) > 0 {
		host, hostErr := kubeconfig.ServerHost(cluster.Cluster)
		if hostErr != nil {
			return hostErr //nolint:wrapcheck // ServerHost errors are self-describing
		}

		// DNS hostnames are case-insensitive, so the kubeconfig
		// author's casing must not defeat the allowlist.
		allowed := slices.ContainsFunc(cfg.AllowedAPIServerHosts, func(a string) bool {
			return strings.EqualFold(a, host)
		})
		if !allowed {
			return fmt.Errorf("%w: %s", ErrAPIServerNotAllowed, host)
		}
	}

	client, err := hostKubeClient(kubeconfigPath, contextName)
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
	// an SA that no record names. The time of the write tells a
	// sweep whose live set predates the `serve` to keep the record.
	ownerDigest := identity.Digest(*saHostID)
	records := hostLedger()
	record := ledger.Record{
		HostID:         ownerDigest,
		InstanceID:     *saInstanceID,
		Kubeconfig:     kubeconfigPath,
		Context:        contextName,
		Namespace:      namespace,
		ServiceAccount: saName,
		ClusterScoped:  sa.ClusterScoped,
		Written:        time.Now().UTC(),
	}

	err = records.Add(record)
	if err != nil {
		return err //nolint:wrapcheck // Add already wraps with its sentinel errors
	}

	err = serviceaccount.CreateWithBinding(ctx, client, sa, namespace, saName, *saInstanceID, ownerDigest)
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
		if !leftOnCluster {
			removeRecord(ctx, records, record)
		}

		return err //nolint:wrapcheck // CreateWithBinding already wraps with its sentinel errors
	}

	result := HostSelectResult{
		Cluster:   *cluster,
		SAName:    saName,
		Namespace: namespace,
		Context:   contextName,
		Binding:   serviceaccount.Describe(sa),
	}

	enc := json.NewEncoder(hostStdout)

	err = enc.Encode(&result)
	if err != nil {
		return fmt.Errorf("encode result: %w", err)
	}

	return nil
}

// ownedServiceAccount returns the provision record of the
// ServiceAccount saName in namespace on the cluster of contextName
// in the host kubeconfig at kubeconfigPath. The record must belong
// to the `serve` of hostID and instanceID. It returns an error that
// wraps [ledger.ErrNotFound] when the ledger holds no such record.
func ownedServiceAccount(
	kubeconfigPath, contextName, namespace, saName, hostID, instanceID string,
) (ledger.Record, error) {
	record, err := hostLedger().Find(ledger.Record{
		HostID:         identity.Digest(hostID),
		InstanceID:     instanceID,
		Kubeconfig:     kubeconfigPath,
		Context:        contextName,
		Namespace:      namespace,
		ServiceAccount: saName,
	})
	if err != nil {
		return ledger.Record{}, fmt.Errorf("%s/%s: %w", namespace, saName, err)
	}

	return record, nil
}

// runHostToken mints a fresh ServiceAccount token via TokenRequest
// and prints an [execplugin.Credential] JSON document. `serve` runs
// it for each request of the kubectl exec credential plugin. The
// host kubeconfig at kubeconfigPath can mint a token for any
// ServiceAccount, so runHostToken mints only for a ServiceAccount
// whose provision record names the --host-id and --instance-id of
// the caller. Without such a record, it returns
// [ErrNotProvisioned]. The lifetime of the token comes from the
// host config cfg.
//
// It does not read [guestEnvVar], because only serve tells the guest
// from the host. Recursion through `workmux host-exec` is impossible,
// because token never constructs a *handler.
func runHostToken(ctx context.Context, kubeconfigPath string, cfg hostConfig, args []string) error {
	fs := flag.NewFlagSet("host token", flag.ContinueOnError)

	contextName := fs.String("context", "", "kubeconfig context to use")
	saName := fs.String("sa", "", "ServiceAccount name (required)")
	namespace := fs.String("namespace", "", "ServiceAccount namespace (required)")
	hostID := fs.String("host-id", "", "host id of the serve that owns the ServiceAccount (required)")
	instanceID := fs.String("instance-id", "", "instance id of the serve that owns the ServiceAccount (required)")

	err := fs.Parse(args)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrParseHostTokenFlags, err)
	}

	if *saName == "" {
		return ErrTokenMissingSA
	}

	if *namespace == "" {
		return ErrTokenMissingNS
	}

	err = checkOwner("--host-id", *hostID, "--instance-id", *instanceID)
	if err != nil {
		return err
	}

	sa := cfg.Config

	err = sa.Validate()
	if err != nil {
		return fmt.Errorf("invalid service account config: %w", err)
	}

	_, err = ownedServiceAccount(kubeconfigPath, *contextName, *namespace, *saName, *hostID, *instanceID)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrNotProvisioned, err)
	}

	client, err := hostKubeClient(kubeconfigPath, *contextName)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBuildKubeClient, err)
	}

	expiration := time.Duration(sa.Expiration) * time.Second

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
// retry is worse than the leak. Each failure is a warning, and a
// `serve` that runs the release adds that warning to its own log
// through [runHostCommand].
//
// runHostRelease deletes only a ServiceAccount whose provision
// record names the --host-id and --instance-id of the caller. The
// record also says whether the binding is a ClusterRoleBinding.
// Without such a record, it logs a warning and deletes nothing, so
// no caller deletes a ServiceAccount of another owner through the
// host kubeconfig at kubeconfigPath.
//
// After the cluster holds neither the SA nor the binding,
// runHostRelease removes the provision records of the SA from the
// ledger. A resource that the cluster did not hold counts as
// deleted. When a delete fails, the records stay, and a `host
// sweep` deletes the SA after the `serve` that owns it has died.
func runHostRelease(ctx context.Context, kubeconfigPath string, args []string) error {
	fs := flag.NewFlagSet("host release", flag.ContinueOnError)

	contextName := fs.String("context", "", "kubeconfig context to use")
	saName := fs.String("sa", "", "ServiceAccount name (required)")
	namespace := fs.String("namespace", "", "ServiceAccount namespace (required)")
	hostID := fs.String("host-id", "", "host id of the serve that owns the ServiceAccount (required)")
	instanceID := fs.String("instance-id", "", "instance id of the serve that owns the ServiceAccount (required)")

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

	err = checkOwner("--host-id", *hostID, "--instance-id", *instanceID)
	if err != nil {
		slog.WarnContext(ctx, "host release owner", slog.Any("error", err))

		return nil
	}

	record, err := ownedServiceAccount(kubeconfigPath, *contextName, *namespace, *saName, *hostID, *instanceID)
	if err != nil {
		slog.WarnContext(ctx, "host release skipped a service account without a provision record of the caller",
			slog.String("context", *contextName),
			slog.String("namespace", *namespace),
			slog.String("name", *saName),
			slog.Any("error", err),
		)

		return nil
	}

	client, err := hostKubeClient(kubeconfigPath, *contextName)
	if err != nil {
		slog.WarnContext(ctx, "build kube client for release",
			slog.String("sa", *saName),
			slog.Any("error", err),
		)

		return nil
	}

	err = serviceaccount.Release(ctx, client, *namespace, *saName, record.ClusterScoped)
	if err != nil {
		slog.WarnContext(ctx, "release service account",
			slog.String("namespace", *namespace),
			slog.String("name", *saName),
			slog.Bool("clusterScoped", record.ClusterScoped),
			slog.Any("error", err),
		)

		return nil
	}

	forgetServiceAccount(ctx, hostLedger(), kubeconfigPath, *contextName, *namespace, *saName)

	return nil
}
