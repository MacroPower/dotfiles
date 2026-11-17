package serviceaccount

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

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
	// provisioned from. Its value is the [identity.Digest] of the
	// host id, never the host id itself. The sweep selector pins on
	// this label so two operators against a shared cluster never
	// delete each other's resources. See
	// [identity.LoadOrCreateHost].
	HostIDLabel = "mcp-kubectx/host-id"
)

// Sentinel errors for ServiceAccount operations.
var (
	ErrMissingRole       = errors.New("role is required")
	ErrInvalidRoleKind   = errors.New("roleKind must be Role or ClusterRole")
	ErrClusterScopedRole = errors.New("clusterScoped requires roleKind ClusterRole")
	ErrExpirationTooLong = errors.New("expiration exceeds maximum (86400)")
	ErrCreateSA          = errors.New("create service account")
	ErrCreateBinding     = errors.New("create role binding")
	// ErrMaybeCreated marks a [CreateWithBinding] failure after which
	// the cluster might hold the ServiceAccount or its binding. A
	// create call failed, and [kube.IsRejected] does not count its
	// error as a refusal, so the apiserver might have stored the
	// object.
	ErrMaybeCreated = errors.New("create might have succeeded")
	// ErrRollback marks a [CreateWithBinding] failure that left the
	// ServiceAccount on the cluster, because the delete that rolls
	// the ServiceAccount back also failed.
	ErrRollback = errors.New("roll back service account")
	// ErrDeleteSA wraps a [Release] failure to delete the
	// ServiceAccount.
	ErrDeleteSA = errors.New("delete service account")
	// ErrDeleteBinding wraps a [Release] failure to delete the
	// binding.
	ErrDeleteBinding = errors.New("delete role binding")
)

// Config holds the ServiceAccount configuration. The JSON keys are
// the keys of the host config file that the `host` subcommands read.
type Config struct {
	// Role is the name of the Role or ClusterRole to bind.
	Role string `json:"role"`

	// RoleKind is "ClusterRole" (default) or "Role".
	RoleKind string `json:"roleKind"`

	// Namespace is the namespace for the ServiceAccount; empty
	// defers to the context namespace, then "default" (see
	// [ResolveNamespace]).
	Namespace string `json:"namespace"`

	// Expiration is the token lifetime in seconds.
	Expiration int `json:"expiration"`

	// ClusterScoped creates a ClusterRoleBinding instead of a
	// RoleBinding. Requires RoleKind ClusterRole.
	ClusterScoped bool `json:"clusterScoped"`
}

// IsClusterRole reports whether the configured role kind is
// ClusterRole.
func (c *Config) IsClusterRole() bool {
	return c.RoleKind == RoleKindClusterRole
}

// Validate checks the consistency of the fields and applies
// defaults.
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

// NewName returns a fresh ServiceAccount name, which is
// `claude-sa-` and 8 random hex characters. The caller picks the
// name before [CreateWithBinding] runs, so that it can record the
// ServiceAccount before the cluster holds it.
func NewName() (string, error) {
	suffix, err := identity.RandomHex(4)
	if err != nil {
		return "", fmt.Errorf("generate name: %w", err)
	}

	return "claude-sa-" + suffix, nil
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

// CreateWithBinding creates the ServiceAccount saName and a Role or
// ClusterRole binding for it. The caller picks the name with
// [NewName] and the namespace with [ResolveNamespace]. The function
// calls only the cluster. It mints no token, writes no kubeconfig,
// and registers no cleanup. The binding name follows [BindingName],
// so callers derive it from the SA name.
//
// The created resources carry instanceID as [InstanceIDLabel] and
// hostID as [HostIDLabel], so that the orphan sweep attributes them
// to one `serve` and one host. `host select` passes the
// [identity.Digest] of the host id as hostID. An empty string for
// either parameter omits that label.
//
// A failure leaves nothing on the cluster when its error wraps
// neither [ErrMaybeCreated] nor [ErrRollback]. The error wraps
// [ErrMaybeCreated] when a create call failed without a refusal
// that [kube.IsRejected] accepts, for example after a lost
// connection, a timeout, or an AlreadyExists answer. The cluster
// might then hold the ServiceAccount, the binding, or both. The
// error wraps [ErrRollback] when the binding fails and the delete
// that rolls the ServiceAccount back fails too, so the
// ServiceAccount stays.
func CreateWithBinding(
	ctx context.Context,
	client kube.Client,
	sa Config,
	namespace, saName, instanceID, hostID string,
) error {
	labels := map[string]string{ManagedByLabel: ManagedByValue}

	if instanceID != "" {
		labels[InstanceIDLabel] = instanceID
	}

	if hostID != "" {
		labels[HostIDLabel] = hostID
	}

	err := client.CreateServiceAccount(ctx, namespace, saName, labels)
	if err != nil {
		return createErr(ErrCreateSA, err)
	}

	err = createBinding(ctx, client, sa, namespace, BindingName(saName), saName, labels)
	if err != nil {
		bindingErr := createErr(ErrCreateBinding, err)

		// Roll back the SA best-effort. The SA carries the
		// instance-id of the live serve, and that id shields it from
		// every sweep for as long as the serve runs. A failed select
		// registers no release closure. Without the rollback, the SA
		// stays until a sweep after the serve ends.
		delErr := client.DeleteServiceAccount(ctx, namespace, saName)
		if delErr != nil && !kube.IsNotFound(delErr) {
			slog.WarnContext(ctx, "rollback service account after binding failure",
				slog.String("namespace", namespace),
				slog.String("name", saName),
				slog.Any("error", delErr),
			)

			return fmt.Errorf("%w: %w: %w", bindingErr, ErrRollback, delErr)
		}

		return bindingErr
	}

	return nil
}

// createErr wraps the error err of a create call with the sentinel
// of the step. It adds [ErrMaybeCreated] unless [kube.IsRejected]
// reports a refusal, because the apiserver might then have stored
// the object.
//
// An AlreadyExists answer gets [ErrMaybeCreated] too. After some
// answers client-go sends a create again, and a retry after an
// attempt that stored the object gets AlreadyExists. The
// ServiceAccount name ends in 8 random hex characters, so an object
// of another owner with that name is far rarer than such a retry.
func createErr(sentinel, err error) error {
	if kube.IsRejected(err) {
		return fmt.Errorf("%w: %w", sentinel, err)
	}

	return fmt.Errorf("%w: %w: %w", sentinel, ErrMaybeCreated, err)
}

// Release deletes the ServiceAccount saName and its binding. The
// two deletes do not depend on each other, so Release runs them in
// parallel, and a failure of one does not stop the other. A
// resource that the cluster does not hold counts as deleted.
//
// Release returns nil when the cluster holds neither resource
// afterward. Otherwise the error wraps [ErrDeleteSA],
// [ErrDeleteBinding], or both.
func Release(
	ctx context.Context,
	client kube.Client,
	namespace, saName string,
	clusterScoped bool,
) error {
	bindingName := BindingName(saName)

	var (
		wg         sync.WaitGroup
		bindingErr error
		saErr      error
	)

	wg.Go(func() {
		if clusterScoped {
			bindingErr = client.DeleteClusterRoleBinding(ctx, bindingName)
		} else {
			bindingErr = client.DeleteRoleBinding(ctx, namespace, bindingName)
		}
	})

	wg.Go(func() {
		saErr = client.DeleteServiceAccount(ctx, namespace, saName)
	})

	wg.Wait()

	var errs []error

	if bindingErr != nil && !kube.IsNotFound(bindingErr) {
		errs = append(errs, fmt.Errorf("%w: %w", ErrDeleteBinding, bindingErr))
	}

	if saErr != nil && !kube.IsNotFound(saErr) {
		errs = append(errs, fmt.Errorf("%w: %w", ErrDeleteSA, saErr))
	}

	return errors.Join(errs...)
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
