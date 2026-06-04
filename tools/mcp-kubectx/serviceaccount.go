package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const (
	// defaultExpiration is the default token lifetime.
	defaultExpiration = 3600

	// maxExpiration is the maximum allowed token lifetime.
	maxExpiration = 86400

	// managedByLabel identifies resources created by this tool.
	managedByLabel = "app.kubernetes.io/managed-by"

	// managedByValue is the label value for resources created by this tool.
	managedByValue = "mcp-kubectx"

	// instanceIDLabel identifies the specific [*handler] (one per
	// `serve` process) that provisioned a resource. Bounded by
	// [discoverLiveInstances] at sweep time so resources owned by a
	// live serve are never reaped.
	instanceIDLabel = "mcp-kubectx/instance-id"

	// hostIDLabel identifies the persistent host the resource was
	// provisioned from. The sweep selector pins on this label so two
	// operators against a shared cluster never delete each other's
	// resources. See [loadOrCreateHostID].
	hostIDLabel = "mcp-kubectx/host-id"
)

// Sentinel errors for ServiceAccount operations.
var (
	ErrMissingRole       = errors.New("--sa-role-name is required")
	ErrInvalidRoleKind   = errors.New("--sa-role-kind must be Role or ClusterRole")
	ErrClusterScopedRole = errors.New("--sa-cluster-scoped requires --sa-role-kind=ClusterRole")
	ErrExpirationTooLong = errors.New("--sa-expiration exceeds maximum (86400)")
	ErrCreateSA          = errors.New("create service account")
	ErrCreateBinding     = errors.New("create role binding")
	ErrTokenRequest      = errors.New("token request")
	ErrBuildKubeClient   = errors.New("build kubernetes client")
)

// ResourceRef is a minimal projection of a Kubernetes object,
// carrying only the fields the sweep classifier in [runHostSweep]
// needs. The List* methods on [KubeClient] return slices of
// ResourceRef rather than typed K8s objects so the interface stays
// narrow and fakes in tests can be trivially small.
type ResourceRef struct {
	// Labels are the full label set on the object. The sweep
	// classifier reads [instanceIDLabel] (and indirectly
	// [hostIDLabel] via the list selector).
	Labels map[string]string

	// Namespace is empty for cluster-scoped resources
	// (ClusterRoleBinding).
	Namespace string

	// Name is the K8s object name.
	Name string
}

// KubeClient abstracts the Kubernetes API calls needed for
// ServiceAccount provisioning, cleanup, and orphan sweeps. Tests
// substitute a fake implementation.
type KubeClient interface {
	CreateServiceAccount(ctx context.Context, namespace, name string, labels map[string]string) error
	DeleteServiceAccount(ctx context.Context, namespace, name string) error
	CreateRoleBinding(
		ctx context.Context,
		namespace, name, roleRef, saName string,
		clusterRole bool,
		labels map[string]string,
	) error
	DeleteRoleBinding(ctx context.Context, namespace, name string) error
	CreateClusterRoleBinding(
		ctx context.Context,
		name, clusterRole, namespace, saName string,
		labels map[string]string,
	) error
	DeleteClusterRoleBinding(ctx context.Context, name string) error
	CreateTokenRequest(
		ctx context.Context,
		namespace, saName string,
		expiration time.Duration,
	) (token string, expiry time.Time, err error)
	ListServiceAccounts(ctx context.Context, labelSelector string) ([]ResourceRef, error)
	ListRoleBindings(ctx context.Context, labelSelector string) ([]ResourceRef, error)
	ListClusterRoleBindings(ctx context.Context, labelSelector string) ([]ResourceRef, error)
}

// saConfig holds ServiceAccount configuration parsed from flags.
type saConfig struct {
	role          string
	roleKind      string // "ClusterRole" (default) or "Role"
	namespace     string
	expiration    int
	clusterScoped bool
}

// isClusterRole reports whether the configured role kind is ClusterRole.
func (c *saConfig) isClusterRole() bool {
	return c.roleKind == roleKindClusterRole
}

// validate checks flag consistency and applies defaults.
func (c *saConfig) validate() error {
	if c.role == "" {
		return ErrMissingRole
	}

	if c.roleKind == "" {
		c.roleKind = roleKindClusterRole
	}

	if c.roleKind != "Role" && c.roleKind != roleKindClusterRole {
		return ErrInvalidRoleKind
	}

	if c.clusterScoped && !c.isClusterRole() {
		return ErrClusterScopedRole
	}

	if c.expiration > maxExpiration {
		return ErrExpirationTooLong
	}

	if c.expiration <= 0 {
		c.expiration = defaultExpiration
	}

	return nil
}

func randomSuffix() (string, error) {
	return randomHex(4)
}

// randomInstanceID returns a 16-hex random identifier used for
// [instanceIDLabel]. Wider than [randomSuffix] to prevent
// intra-host collisions across long-running operators.
func randomInstanceID() (string, error) {
	return randomHex(8)
}

// randomHex returns a hex-encoded random string with the given
// number of bytes. Internal helper shared between [randomSuffix]
// and [randomInstanceID] so both pull from the same crypto/rand
// source and surface a consistent error shape.
func randomHex(n int) (string, error) {
	b := make([]byte, n)

	_, err := rand.Read(b)
	if err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}

	return hex.EncodeToString(b), nil
}

// createBinding creates either a ClusterRoleBinding or a RoleBinding
// based on the ServiceAccount configuration.
func createBinding(
	ctx context.Context,
	client KubeClient,
	sa saConfig,
	namespace, bindingName, saName string,
	labels map[string]string,
) error {
	if sa.clusterScoped {
		err := client.CreateClusterRoleBinding(ctx, bindingName, sa.role, namespace, saName, labels)
		if err != nil {
			return fmt.Errorf("cluster role binding: %w", err)
		}

		return nil
	}

	err := client.CreateRoleBinding(ctx, namespace, bindingName, sa.role, saName, sa.isClusterRole(), labels)
	if err != nil {
		return fmt.Errorf("role binding: %w", err)
	}

	return nil
}

// resolveSANamespace picks the namespace to create the ServiceAccount
// in: explicit flag value, then the context's namespace, then "default".
func resolveSANamespace(sa saConfig, ctxEntry *namedContext) string {
	if sa.namespace != "" {
		return sa.namespace
	}

	if ctxEntry != nil && ctxEntry.Context.Namespace != "" {
		return ctxEntry.Context.Namespace
	}

	return "default"
}

// bindingNameForSA returns the deterministic binding name for an SA.
// The single source of truth for the convention; called both at
// create time ([createSAWithBinding]) and release time
// ([runHostRelease]) so the two cannot drift.
func bindingNameForSA(saName string) string {
	return saName + "-binding"
}

// createSAWithBinding creates a fresh ServiceAccount and a Role or
// ClusterRole binding for it. The caller selects the namespace via
// [resolveSANamespace] and passes it in. Network IO only; no token
// minting, no kubeconfig writing, no cleanup registration. The
// binding name follows [bindingNameForSA] so callers can derive it
// from the returned SA name.
//
// instanceID and hostID are tagged on the created resources via
// [instanceIDLabel] and [hostIDLabel] so the orphan sweep in
// [runHostSweep] can attribute them to a specific live serve and
// host. An empty string for either parameter omits that label
// entirely; this is the contract the standalone `host select` CLI
// invocation relies on so its existing tests stay green.
func createSAWithBinding(
	ctx context.Context,
	client KubeClient,
	sa saConfig,
	namespace, instanceID, hostID string,
) (string, error) {
	suffix, err := randomSuffix()
	if err != nil {
		return "", fmt.Errorf("generate name: %w", err)
	}

	saName := "claude-sa-" + suffix

	labels := map[string]string{managedByLabel: managedByValue}

	if instanceID != "" {
		labels[instanceIDLabel] = instanceID
	}

	if hostID != "" {
		labels[hostIDLabel] = hostID
	}

	err = client.CreateServiceAccount(ctx, namespace, saName, labels)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrCreateSA, err)
	}

	err = createBinding(ctx, client, sa, namespace, bindingNameForSA(saName), saName, labels)
	if err != nil {
		// Roll back the SA best-effort: it carries the live serve's
		// own instance-id, which shields it from every sweep for as
		// long as this serve runs, and a failed select registers no
		// release closure — without the rollback it would strand
		// until the next serve's startup sweep.
		delErr := client.DeleteServiceAccount(ctx, namespace, saName)
		if delErr != nil {
			slog.WarnContext(ctx, "rollback service account after binding failure",
				slog.String("namespace", namespace),
				slog.String("name", saName),
				slog.Any("error", delErr),
			)
		}

		return "", fmt.Errorf("%w: %w", ErrCreateBinding, err)
	}

	return saName, nil
}

// describeBinding returns a short human-readable description of the
// configured role binding, e.g. `ClusterRole "view" (cluster-scoped)`.
func describeBinding(sa saConfig) string {
	scope := "namespaced"
	if sa.clusterScoped {
		scope = "cluster-scoped"
	}

	return fmt.Sprintf("%s %q (%s)", sa.roleKind, sa.role, scope)
}
