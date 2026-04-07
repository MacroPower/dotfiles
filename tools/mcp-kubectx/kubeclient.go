package main

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

// clientGoKubeClient implements [KubeClient] using client-go.
type clientGoKubeClient struct {
	clientset kubernetes.Interface
}

// NewKubeClientFromKubeconfig returns a [KubeClient] backed by
// client-go, configured from the host kubeconfig file and targeting
// the specified context.
func NewKubeClientFromKubeconfig(kubeconfigPath, kubeContext string) (*clientGoKubeClient, error) {
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

	return &clientGoKubeClient{clientset: cs}, nil
}

func (c *clientGoKubeClient) CreateServiceAccount(
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

func (c *clientGoKubeClient) DeleteServiceAccount(ctx context.Context, namespace, name string) error {
	err := c.clientset.CoreV1().ServiceAccounts(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("delete service account: %w", err)
	}

	return nil
}

func (c *clientGoKubeClient) CreateRoleBinding(
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

func (c *clientGoKubeClient) DeleteRoleBinding(ctx context.Context, namespace, name string) error {
	err := c.clientset.RbacV1().RoleBindings(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("delete role binding: %w", err)
	}

	return nil
}

func (c *clientGoKubeClient) CreateClusterRoleBinding(
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

func (c *clientGoKubeClient) DeleteClusterRoleBinding(ctx context.Context, name string) error {
	err := c.clientset.RbacV1().ClusterRoleBindings().Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("delete cluster role binding: %w", err)
	}

	return nil
}

func (c *clientGoKubeClient) CreateTokenRequest(
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
