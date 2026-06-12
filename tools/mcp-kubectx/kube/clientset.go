package kube

import (
	"context"
	"fmt"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// roleKindClusterRole is the RBAC kind string for ClusterRole
// references in RoleBindings and ClusterRoleBindings.
const roleKindClusterRole = "ClusterRole"

// Clientset implements [Client] using client-go.
type Clientset struct {
	clientset kubernetes.Interface
}

// NewClientset returns a [Clientset] backed by client-go, configured
// from the host kubeconfig file and targeting the specified context.
func NewClientset(kubeconfigPath, kubeContext string) (*Clientset, error) {
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{CurrentContext: kubeContext},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create clientset: %w", err)
	}

	return &Clientset{clientset: cs}, nil
}

// CreateServiceAccount creates a ServiceAccount with the given
// labels.
func (c *Clientset) CreateServiceAccount(
	ctx context.Context,
	namespace, name string,
	labels map[string]string,
) error {
	_, err := c.clientset.CoreV1().ServiceAccounts(namespace).Create(ctx, &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create service account: %w", err)
	}

	return nil
}

// DeleteServiceAccount deletes a ServiceAccount by namespace and
// name.
func (c *Clientset) DeleteServiceAccount(ctx context.Context, namespace, name string) error {
	err := c.clientset.CoreV1().ServiceAccounts(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("delete service account: %w", err)
	}

	return nil
}

// CreateRoleBinding creates a RoleBinding for the named
// ServiceAccount referencing either a Role or, when clusterRole is
// true, a ClusterRole.
func (c *Clientset) CreateRoleBinding(
	ctx context.Context,
	namespace, name, roleRef, saName string,
	clusterRole bool,
	labels map[string]string,
) error {
	kind := "Role"
	if clusterRole {
		kind = roleKindClusterRole
	}

	_, err := c.clientset.RbacV1().RoleBindings(namespace).Create(ctx, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     kind,
			Name:     roleRef,
		},
		Subjects: []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      saName,
			Namespace: namespace,
		}},
	}, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create role binding: %w", err)
	}

	return nil
}

// DeleteRoleBinding deletes a RoleBinding by namespace and name.
func (c *Clientset) DeleteRoleBinding(ctx context.Context, namespace, name string) error {
	err := c.clientset.RbacV1().RoleBindings(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("delete role binding: %w", err)
	}

	return nil
}

// CreateClusterRoleBinding creates a ClusterRoleBinding for the
// named ServiceAccount referencing a ClusterRole.
func (c *Clientset) CreateClusterRoleBinding(
	ctx context.Context,
	name, clusterRoleName, namespace, saName string,
	labels map[string]string,
) error {
	_, err := c.clientset.RbacV1().ClusterRoleBindings().Create(ctx, &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     roleKindClusterRole,
			Name:     clusterRoleName,
		},
		Subjects: []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      saName,
			Namespace: namespace,
		}},
	}, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create cluster role binding: %w", err)
	}

	return nil
}

// DeleteClusterRoleBinding deletes a ClusterRoleBinding by name.
func (c *Clientset) DeleteClusterRoleBinding(ctx context.Context, name string) error {
	err := c.clientset.RbacV1().ClusterRoleBindings().Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("delete cluster role binding: %w", err)
	}

	return nil
}

// ListServiceAccounts lists ServiceAccounts across all namespaces
// filtered by labelSelector.
func (c *Clientset) ListServiceAccounts(
	ctx context.Context,
	labelSelector string,
) ([]ResourceRef, error) {
	list, err := c.clientset.CoreV1().ServiceAccounts("").List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("list service accounts: %w", err)
	}

	refs := make([]ResourceRef, 0, len(list.Items))
	for i := range list.Items {
		refs = append(refs, ResourceRef{
			Namespace: list.Items[i].Namespace,
			Name:      list.Items[i].Name,
			Labels:    list.Items[i].Labels,
		})
	}

	return refs, nil
}

// ListRoleBindings lists RoleBindings across all namespaces filtered
// by labelSelector.
func (c *Clientset) ListRoleBindings(
	ctx context.Context,
	labelSelector string,
) ([]ResourceRef, error) {
	list, err := c.clientset.RbacV1().RoleBindings("").List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("list role bindings: %w", err)
	}

	refs := make([]ResourceRef, 0, len(list.Items))
	for i := range list.Items {
		refs = append(refs, ResourceRef{
			Namespace: list.Items[i].Namespace,
			Name:      list.Items[i].Name,
			Labels:    list.Items[i].Labels,
		})
	}

	return refs, nil
}

// ListClusterRoleBindings lists ClusterRoleBindings filtered by
// labelSelector.
func (c *Clientset) ListClusterRoleBindings(
	ctx context.Context,
	labelSelector string,
) ([]ResourceRef, error) {
	list, err := c.clientset.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("list cluster role bindings: %w", err)
	}

	refs := make([]ResourceRef, 0, len(list.Items))
	for i := range list.Items {
		refs = append(refs, ResourceRef{
			Name:   list.Items[i].Name,
			Labels: list.Items[i].Labels,
		})
	}

	return refs, nil
}

// CreateTokenRequest mints a short-lived ServiceAccount token via
// the TokenRequest subresource and returns the token with its expiry.
func (c *Clientset) CreateTokenRequest(
	ctx context.Context,
	namespace, saName string,
	expiration time.Duration,
) (string, time.Time, error) {
	seconds := int64(expiration.Seconds())

	tr, err := c.clientset.CoreV1().ServiceAccounts(namespace).CreateToken(ctx, saName, &authv1.TokenRequest{
		Spec: authv1.TokenRequestSpec{
			ExpirationSeconds: &seconds,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create token: %w", err)
	}

	return tr.Status.Token, tr.Status.ExpirationTimestamp.Time, nil
}

// Compile-time guard: Clientset must implement Client.
var _ Client = (*Clientset)(nil)
