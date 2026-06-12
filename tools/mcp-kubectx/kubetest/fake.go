package kubetest

import (
	"context"
	"maps"
	"sync"
	"time"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kube"
)

// Fake implements [kube.Client] for testing. Every call appends to
// the corresponding recording slice and returns the configured
// error (nil by default). The embedded mutex serializes concurrent
// callers (release deletes run in parallel goroutines); tests that
// read the recording fields while calls may still be in flight must
// hold it.
type Fake struct {
	CreateSAErr                 error
	DeleteSAErr                 error
	CreateRoleBindingErr        error
	DeleteRoleBindingErr        error
	CreateClusterRoleBindingErr error
	DeleteClusterRoleBindingErr error
	TokenRequestErr             error
	ListSAErr                   error
	ListRBErr                   error
	ListCRBErr                  error

	Token       string
	TokenExpiry time.Time

	CreatedSAs                 []string
	CreatedSALabels            []map[string]string
	CreatedRoleBindingLabels   []map[string]string
	CreatedCRBLabels           []map[string]string
	DeletedSAs                 []string
	CreatedRoleBindings        []string
	DeletedRoleBindings        []string
	CreatedClusterRoleBindings []string
	DeletedClusterRoleBindings []string
	TokenRequests              []string
	ListedSAs                  []string

	ListSAResp  []kube.ResourceRef
	ListRBResp  []kube.ResourceRef
	ListCRBResp []kube.ResourceRef

	mu sync.Mutex
}

// CreateServiceAccount records the namespaced SA name and its labels.
func (m *Fake) CreateServiceAccount(
	_ context.Context,
	namespace, name string,
	labels map[string]string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.CreatedSAs = append(m.CreatedSAs, namespace+"/"+name)
	m.CreatedSALabels = append(m.CreatedSALabels, cloneLabels(labels))

	return m.CreateSAErr
}

// DeleteServiceAccount records the namespaced SA name.
func (m *Fake) DeleteServiceAccount(_ context.Context, namespace, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.DeletedSAs = append(m.DeletedSAs, namespace+"/"+name)

	return m.DeleteSAErr
}

// CreateRoleBinding records the namespaced binding name and its
// labels.
func (m *Fake) CreateRoleBinding(
	_ context.Context,
	namespace, name, _, _ string,
	_ bool,
	labels map[string]string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.CreatedRoleBindings = append(m.CreatedRoleBindings, namespace+"/"+name)
	m.CreatedRoleBindingLabels = append(m.CreatedRoleBindingLabels, cloneLabels(labels))

	return m.CreateRoleBindingErr
}

// DeleteRoleBinding records the namespaced binding name.
func (m *Fake) DeleteRoleBinding(_ context.Context, namespace, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.DeletedRoleBindings = append(m.DeletedRoleBindings, namespace+"/"+name)

	return m.DeleteRoleBindingErr
}

// CreateClusterRoleBinding records the binding name and its labels.
func (m *Fake) CreateClusterRoleBinding(
	_ context.Context,
	name, _, _, _ string,
	labels map[string]string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.CreatedClusterRoleBindings = append(m.CreatedClusterRoleBindings, name)
	m.CreatedCRBLabels = append(m.CreatedCRBLabels, cloneLabels(labels))

	return m.CreateClusterRoleBindingErr
}

// DeleteClusterRoleBinding records the binding name.
func (m *Fake) DeleteClusterRoleBinding(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.DeletedClusterRoleBindings = append(m.DeletedClusterRoleBindings, name)

	return m.DeleteClusterRoleBindingErr
}

// CreateTokenRequest records the namespaced SA name and returns the
// configured token and expiry.
func (m *Fake) CreateTokenRequest(
	_ context.Context,
	namespace, saName string,
	_ time.Duration,
) (string, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.TokenRequests = append(m.TokenRequests, namespace+"/"+saName)

	if m.TokenRequestErr != nil {
		return "", time.Time{}, m.TokenRequestErr
	}

	return m.Token, m.TokenExpiry, nil
}

// ListServiceAccounts records the selector and returns the
// configured response.
func (m *Fake) ListServiceAccounts(_ context.Context, selector string) ([]kube.ResourceRef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ListedSAs = append(m.ListedSAs, selector)

	return m.ListSAResp, m.ListSAErr
}

// ListRoleBindings returns the configured response.
func (m *Fake) ListRoleBindings(_ context.Context, _ string) ([]kube.ResourceRef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.ListRBResp, m.ListRBErr
}

// ListClusterRoleBindings returns the configured response.
func (m *Fake) ListClusterRoleBindings(_ context.Context, _ string) ([]kube.ResourceRef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.ListCRBResp, m.ListCRBErr
}

// Lock acquires the fake's mutex. Tests that read the recording
// fields while calls may still be in flight hold it around the read.
func (m *Fake) Lock() { m.mu.Lock() }

// Unlock releases the fake's mutex.
func (m *Fake) Unlock() { m.mu.Unlock() }

// cloneLabels returns a deep copy of labels so the fake retains the
// value passed at call time rather than a reference that the caller
// may mutate later.
func cloneLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return nil
	}

	out := make(map[string]string, len(labels))
	maps.Copy(out, labels)

	return out
}

// Compile-time guard: Fake must implement kube.Client.
var _ kube.Client = (*Fake)(nil)
