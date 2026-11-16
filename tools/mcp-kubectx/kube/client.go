package kube

import (
	"context"
	"errors"
	"net/http"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// ResourceRef is a minimal projection of a Kubernetes object,
// carrying only the fields the orphan-sweep classifier needs. The
// List* methods on [Client] return slices of ResourceRef rather
// than typed K8s objects so the interface stays narrow and fakes in
// tests stay small.
type ResourceRef struct {
	// Created is the creationTimestamp of the object. The apiserver
	// sets it from its own clock when it creates the object, and it
	// keeps only whole seconds.
	Created time.Time

	// Labels are the full label set on the object.
	Labels map[string]string

	// Namespace is empty for cluster-scoped resources
	// (ClusterRoleBinding).
	Namespace string

	// Name is the K8s object name.
	Name string
}

// IsNotFound reports whether err says that the object of an API
// call does not exist. It also reports true when err wraps such an
// error. A caller that deletes an object uses it to tell an object
// that is already gone from a delete that did not happen.
func IsNotFound(err error) bool {
	return apierrors.IsNotFound(err)
}

// IsRejected reports whether err says that the apiserver answered an
// API call and refused it, so that the call stored nothing. It
// reports true for a status with a 4xx code, such as Forbidden or
// Invalid, and for an error that wraps such a status.
//
// It reports false for every other error. After a transport error,
// a canceled context, a timeout status, or a 5xx status, the
// apiserver might have stored the object of a create call before
// the answer was lost. A caller that must know whether a create
// left an object on the cluster counts only a rejected call as one
// that left nothing.
//
// It also reports false for AlreadyExists and for TooManyRequests,
// because one call of the client of [NewClientset] can send a create
// more than once. The client sends the create again after an answer
// of 429, or of a 5xx code with a Retry-After header. The apiserver
// sends such a 5xx answer when its storage times out on a write
// that it might have committed. The error then holds only the
// answer to the last attempt. A retry after a stored attempt gets
// AlreadyExists, and a 429 is the last answer when the client runs
// out of retries.
func IsRejected(err error) bool {
	var status apierrors.APIStatus
	if !errors.As(err, &status) {
		return false
	}

	if apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) ||
		apierrors.IsAlreadyExists(err) || apierrors.IsTooManyRequests(err) {
		return false
	}

	code := status.Status().Code

	// A 408 comes from a proxy in front of the apiserver, which
	// might have forwarded the request before its own timeout.
	return code >= http.StatusBadRequest &&
		code < http.StatusInternalServerError &&
		code != http.StatusRequestTimeout
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
