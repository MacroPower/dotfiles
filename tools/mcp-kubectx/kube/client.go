package kube

import (
	"context"
	"time"
)

// ResourceRef is a minimal projection of a Kubernetes object,
// carrying only the fields the orphan-sweep classifier needs. The
// List* methods on [Client] return slices of ResourceRef rather
// than typed K8s objects so the interface stays narrow and fakes in
// tests can be trivially small.
type ResourceRef struct {
	// Labels are the full label set on the object.
	Labels map[string]string

	// Namespace is empty for cluster-scoped resources
	// (ClusterRoleBinding).
	Namespace string

	// Name is the K8s object name.
	Name string
}

// Client abstracts the Kubernetes API calls needed for
// ServiceAccount provisioning, cleanup, and orphan sweeps. Tests
// substitute a fake implementation; see the kubetest package.
type Client interface {
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
