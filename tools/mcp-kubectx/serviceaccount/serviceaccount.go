package serviceaccount

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/identity"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kube"
)

const (
	// DefaultExpiration is the default token lifetime in seconds.
	DefaultExpiration = 3600

	// MaxExpiration is the maximum allowed token lifetime in seconds.
	MaxExpiration = 86400

	// RoleKindClusterRole is the RBAC kind string for ClusterRole
	// references.
	RoleKindClusterRole = "ClusterRole"

	// ManagedByLabel identifies resources created by this tool.
	ManagedByLabel = "app.kubernetes.io/managed-by"

	// ManagedByValue is the label value for resources created by
	// this tool.
	ManagedByValue = "mcp-kubectx"

	// InstanceIDLabel identifies the specific `serve` process that
	// provisioned a resource. Bounded by socket-slot liveness
	// discovery at sweep time so resources owned by a live serve are
	// never reaped.
	InstanceIDLabel = "mcp-kubectx/instance-id"

	// HostIDLabel identifies the persistent host the resource was
	// provisioned from. The sweep selector pins on this label so two
	// operators against a shared cluster never delete each other's
	// resources. See [identity.LoadOrCreateHost].
	HostIDLabel = "mcp-kubectx/host-id"
)

// Sentinel errors for ServiceAccount operations.
var (
	ErrMissingRole       = errors.New("--sa-role-name is required")
	ErrInvalidRoleKind   = errors.New("--sa-role-kind must be Role or ClusterRole")
	ErrClusterScopedRole = errors.New("--sa-cluster-scoped requires --sa-role-kind=ClusterRole")
	ErrExpirationTooLong = errors.New("--sa-expiration exceeds maximum (86400)")
	ErrCreateSA          = errors.New("create service account")
	ErrCreateBinding     = errors.New("create role binding")
)

// Config holds ServiceAccount configuration parsed from flags.
type Config struct {
	// Role is the name of the Role or ClusterRole to bind.
	Role string

	// RoleKind is "ClusterRole" (default) or "Role".
	RoleKind string

	// Namespace is the namespace for the ServiceAccount; empty
	// defers to the context namespace, then "default" (see
	// [ResolveNamespace]).
	Namespace string

	// Expiration is the token lifetime in seconds.
	Expiration int

	// ClusterScoped creates a ClusterRoleBinding instead of a
	// RoleBinding. Requires RoleKind ClusterRole.
	ClusterScoped bool
}

// IsClusterRole reports whether the configured role kind is
// ClusterRole.
func (c *Config) IsClusterRole() bool {
	return c.RoleKind == RoleKindClusterRole
}

// Validate checks flag consistency and applies defaults.
func (c *Config) Validate() error {
	if c.Role == "" {
		return ErrMissingRole
	}

	if c.RoleKind == "" {
		c.RoleKind = RoleKindClusterRole
	}

	if c.RoleKind != "Role" && c.RoleKind != RoleKindClusterRole {
		return ErrInvalidRoleKind
	}

	if c.ClusterScoped && !c.IsClusterRole() {
		return ErrClusterScopedRole
	}

	if c.Expiration > MaxExpiration {
		return ErrExpirationTooLong
	}

	if c.Expiration <= 0 {
		c.Expiration = DefaultExpiration
	}

	return nil
}

// randomSuffix returns the 8-hex random suffix used in generated
// ServiceAccount names.
func randomSuffix() (string, error) {
	return identity.RandomHex(4) //nolint:wrapcheck // RandomHex errors are self-describing
}

// createBinding creates either a ClusterRoleBinding or a RoleBinding
// based on the ServiceAccount configuration.
func createBinding(
	ctx context.Context,
	client kube.Client,
	sa Config,
	namespace, bindingName, saName string,
	labels map[string]string,
) error {
	if sa.ClusterScoped {
		err := client.CreateClusterRoleBinding(ctx, bindingName, sa.Role, namespace, saName, labels)
		if err != nil {
			return fmt.Errorf("cluster role binding: %w", err)
		}

		return nil
	}

	err := client.CreateRoleBinding(ctx, namespace, bindingName, sa.Role, saName, sa.IsClusterRole(), labels)
	if err != nil {
		return fmt.Errorf("role binding: %w", err)
	}

	return nil
}

// ResolveNamespace picks the namespace to create the ServiceAccount
// in: explicit config value, then the selected context's namespace,
// then "default".
func ResolveNamespace(sa Config, contextNamespace string) string {
	if sa.Namespace != "" {
		return sa.Namespace
	}

	if contextNamespace != "" {
		return contextNamespace
	}

	return "default"
}

// BindingName returns the deterministic binding name for an SA. The
// single source of truth for the convention; called both at create
// time ([CreateWithBinding]) and release time so the two cannot
// drift.
func BindingName(saName string) string {
	return saName + "-binding"
}

// CreateWithBinding creates a fresh ServiceAccount and a Role or
// ClusterRole binding for it. The caller selects the namespace via
// [ResolveNamespace] and passes it in. Network IO only; no token
// minting, no kubeconfig writing, no cleanup registration. The
// binding name follows [BindingName] so callers can derive it from
// the returned SA name.
//
// instanceID and hostID are tagged on the created resources via
// [InstanceIDLabel] and [HostIDLabel] so the orphan sweep can
// attribute them to a specific live serve and host. An empty string
// for either parameter omits that label entirely; this is the
// contract the standalone `host select` CLI invocation relies on.
func CreateWithBinding(
	ctx context.Context,
	client kube.Client,
	sa Config,
	namespace, instanceID, hostID string,
) (string, error) {
	suffix, err := randomSuffix()
	if err != nil {
		return "", fmt.Errorf("generate name: %w", err)
	}

	saName := "claude-sa-" + suffix

	labels := map[string]string{ManagedByLabel: ManagedByValue}

	if instanceID != "" {
		labels[InstanceIDLabel] = instanceID
	}

	if hostID != "" {
		labels[HostIDLabel] = hostID
	}

	err = client.CreateServiceAccount(ctx, namespace, saName, labels)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrCreateSA, err)
	}

	err = createBinding(ctx, client, sa, namespace, BindingName(saName), saName, labels)
	if err != nil {
		// Roll back the SA best-effort: it carries the live serve's
		// own instance-id, which shields it from every sweep for as
		// long as this serve runs, and a failed select registers no
		// release closure -- without the rollback it would strand
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

// Describe returns a short human-readable description of the
// configured role binding, e.g. `ClusterRole "view" (cluster-scoped)`.
func Describe(sa Config) string {
	scope := "namespaced"
	if sa.ClusterScoped {
		scope = "cluster-scoped"
	}

	return fmt.Sprintf("%s %q (%s)", sa.RoleKind, sa.Role, scope)
}
