package main

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockKubeClient implements [KubeClient] for testing. It is shared
// across host_test.go (driving runHost* directly), shellout_test.go
// (recursion-guard), and serviceaccount_test.go (saConfig coverage).
type mockKubeClient struct {
	mu sync.Mutex

	createSAErr                 error
	deleteSAErr                 error
	createRoleBindingErr        error
	deleteRoleBindingErr        error
	createClusterRoleBindingErr error
	deleteClusterRoleBindingErr error
	tokenRequestErr             error
	listSAErr                   error
	listRBErr                   error
	listCRBErr                  error

	token       string
	tokenExpiry time.Time

	createdSAs                 []string
	createdSALabels            []map[string]string
	createdRoleBindingLabels   []map[string]string
	createdCRBLabels           []map[string]string
	deletedSAs                 []string
	createdRoleBindings        []string
	deletedRoleBindings        []string
	createdClusterRoleBindings []string
	deletedClusterRoleBindings []string
	tokenRequests              []string
	listedSAs                  []string
	listedRBs                  []string
	listedCRBs                 []string

	listSAResp  []ResourceRef
	listRBResp  []ResourceRef
	listCRBResp []ResourceRef
}

func (m *mockKubeClient) CreateServiceAccount(
	_ context.Context,
	namespace, name string,
	labels map[string]string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.createdSAs = append(m.createdSAs, namespace+"/"+name)
	m.createdSALabels = append(m.createdSALabels, cloneLabels(labels))

	return m.createSAErr
}

func (m *mockKubeClient) DeleteServiceAccount(_ context.Context, namespace, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.deletedSAs = append(m.deletedSAs, namespace+"/"+name)

	return m.deleteSAErr
}

func (m *mockKubeClient) CreateRoleBinding(
	_ context.Context,
	namespace, name, _, _ string,
	_ bool,
	labels map[string]string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.createdRoleBindings = append(m.createdRoleBindings, namespace+"/"+name)
	m.createdRoleBindingLabels = append(m.createdRoleBindingLabels, cloneLabels(labels))

	return m.createRoleBindingErr
}

func (m *mockKubeClient) DeleteRoleBinding(_ context.Context, namespace, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.deletedRoleBindings = append(m.deletedRoleBindings, namespace+"/"+name)

	return m.deleteRoleBindingErr
}

func (m *mockKubeClient) CreateClusterRoleBinding(
	_ context.Context,
	name, _, _, _ string,
	labels map[string]string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.createdClusterRoleBindings = append(m.createdClusterRoleBindings, name)
	m.createdCRBLabels = append(m.createdCRBLabels, cloneLabels(labels))

	return m.createClusterRoleBindingErr
}

func (m *mockKubeClient) DeleteClusterRoleBinding(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.deletedClusterRoleBindings = append(m.deletedClusterRoleBindings, name)

	return m.deleteClusterRoleBindingErr
}

func (m *mockKubeClient) CreateTokenRequest(
	_ context.Context,
	namespace, saName string,
	_ time.Duration,
) (string, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.tokenRequests = append(m.tokenRequests, namespace+"/"+saName)

	if m.tokenRequestErr != nil {
		return "", time.Time{}, m.tokenRequestErr
	}

	return m.token, m.tokenExpiry, nil
}

func (m *mockKubeClient) ListServiceAccounts(_ context.Context, selector string) ([]ResourceRef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.listedSAs = append(m.listedSAs, selector)

	return m.listSAResp, m.listSAErr
}

func (m *mockKubeClient) ListRoleBindings(_ context.Context, selector string) ([]ResourceRef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.listedRBs = append(m.listedRBs, selector)

	return m.listRBResp, m.listRBErr
}

func (m *mockKubeClient) ListClusterRoleBindings(_ context.Context, selector string) ([]ResourceRef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.listedCRBs = append(m.listedCRBs, selector)

	return m.listCRBResp, m.listCRBErr
}

// cloneLabels returns a deep copy of labels so the mock retains
// the value passed at call time rather than a reference that the
// caller may mutate later.
func cloneLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return nil
	}

	out := make(map[string]string, len(labels))
	maps.Copy(out, labels)

	return out
}

func TestSAConfigValidate(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg saConfig
		err error
	}{
		"valid role": {
			cfg: saConfig{role: "my-role", roleKind: "Role"},
		},
		"valid cluster role": {
			cfg: saConfig{role: "my-cluster-role", roleKind: "ClusterRole"},
		},
		"valid cluster scoped": {
			cfg: saConfig{role: "my-cluster-role", roleKind: "ClusterRole", clusterScoped: true},
		},
		"defaults to ClusterRole": {
			cfg: saConfig{role: "view"},
		},
		"missing role name": {
			cfg: saConfig{},
			err: ErrMissingRole,
		},
		"invalid role kind": {
			cfg: saConfig{role: "r", roleKind: "BadKind"},
			err: ErrInvalidRoleKind,
		},
		"cluster scoped with Role kind": {
			cfg: saConfig{role: "r", roleKind: "Role", clusterScoped: true},
			err: ErrClusterScopedRole,
		},
		"expiration too long": {
			cfg: saConfig{role: "r", expiration: 100000},
			err: ErrExpirationTooLong,
		},
		"default expiration": {
			cfg: saConfig{role: "r"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := tc.cfg.validate()
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCreateSAWithBinding(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		sa         saConfig
		mock       *mockKubeClient
		ns         string
		instanceID string
		hostID     string
		err        error
		assert     func(t *testing.T, m *mockKubeClient)
	}{
		"role binding without ids": {
			sa:   saConfig{role: "view", roleKind: "Role", expiration: 3600},
			mock: &mockKubeClient{},
			ns:   "ns",
			assert: func(t *testing.T, m *mockKubeClient) {
				t.Helper()
				assert.Len(t, m.createdSAs, 1)
				assert.Len(t, m.createdRoleBindings, 1)
				assert.Empty(t, m.createdClusterRoleBindings)

				require.Len(t, m.createdSALabels, 1)
				assert.Equal(t, managedByValue, m.createdSALabels[0][managedByLabel])

				_, hasInstance := m.createdSALabels[0][instanceIDLabel]
				_, hasHost := m.createdSALabels[0][hostIDLabel]

				assert.False(t, hasInstance, "instance-id label must be omitted when empty")
				assert.False(t, hasHost, "host-id label must be omitted when empty")
			},
		},
		"role binding with ids": {
			sa:         saConfig{role: "view", roleKind: "Role", expiration: 3600},
			mock:       &mockKubeClient{},
			ns:         "ns",
			instanceID: "inst-abc",
			hostID:     "host-xyz",
			assert: func(t *testing.T, m *mockKubeClient) {
				t.Helper()
				require.Len(t, m.createdSALabels, 1)
				assert.Equal(t, "inst-abc", m.createdSALabels[0][instanceIDLabel])
				assert.Equal(t, "host-xyz", m.createdSALabels[0][hostIDLabel])

				require.Len(t, m.createdRoleBindingLabels, 1)
				assert.Equal(t, "inst-abc", m.createdRoleBindingLabels[0][instanceIDLabel])
				assert.Equal(t, "host-xyz", m.createdRoleBindingLabels[0][hostIDLabel])
			},
		},
		"cluster role binding namespaced": {
			sa:   saConfig{role: "view", roleKind: "ClusterRole", expiration: 3600},
			mock: &mockKubeClient{},
			ns:   "ns",
			assert: func(t *testing.T, m *mockKubeClient) {
				t.Helper()
				assert.Len(t, m.createdRoleBindings, 1)
				assert.Empty(t, m.createdClusterRoleBindings)
			},
		},
		"cluster scoped binding propagates labels": {
			sa:         saConfig{role: "view", roleKind: "ClusterRole", clusterScoped: true, expiration: 3600},
			mock:       &mockKubeClient{},
			ns:         "ns",
			instanceID: "inst-1",
			hostID:     "host-1",
			assert: func(t *testing.T, m *mockKubeClient) {
				t.Helper()
				assert.Len(t, m.createdClusterRoleBindings, 1)
				assert.Empty(t, m.createdRoleBindings)

				require.Len(t, m.createdCRBLabels, 1)
				assert.Equal(t, "inst-1", m.createdCRBLabels[0][instanceIDLabel])
				assert.Equal(t, "host-1", m.createdCRBLabels[0][hostIDLabel])
			},
		},
		"sa creation failure": {
			sa:   saConfig{role: "view", roleKind: "Role", expiration: 3600},
			mock: &mockKubeClient{createSAErr: errors.New("forbidden")},
			ns:   "ns",
			err:  ErrCreateSA,
		},
		"binding creation failure rolls back the SA": {
			sa:   saConfig{role: "view", roleKind: "Role", expiration: 3600},
			mock: &mockKubeClient{createRoleBindingErr: errors.New("forbidden")},
			ns:   "ns",
			err:  ErrCreateBinding,
			assert: func(t *testing.T, m *mockKubeClient) {
				t.Helper()
				// The orphaned SA would carry the live serve's own
				// instance-id and dodge every sweep; the rollback
				// delete must fire.
				require.Len(t, m.deletedSAs, 1)
				assert.Equal(t, m.createdSAs[0], m.deletedSAs[0],
					"the rollback must delete the SA that was just created")
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			saName, err := createSAWithBinding(t.Context(), tc.mock, tc.sa, tc.ns, tc.instanceID, tc.hostID)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)

				if tc.assert != nil {
					tc.assert(t, tc.mock)
				}

				return
			}

			require.NoError(t, err)
			assert.Contains(t, saName, "claude-sa-")
			assert.Equal(t, bindingNameForSA(saName), saName+"-binding")

			if tc.assert != nil {
				tc.assert(t, tc.mock)
			}
		})
	}
}

func TestResolveSANamespace(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		sa       saConfig
		ctxEntry *namedContext
		want     string
	}{
		"explicit flag": {
			sa:       saConfig{namespace: "explicit"},
			ctxEntry: &namedContext{Context: contextDetails{Namespace: "ctx-ns"}},
			want:     "explicit",
		},
		"context namespace": {
			ctxEntry: &namedContext{Context: contextDetails{Namespace: "ctx-ns"}},
			want:     "ctx-ns",
		},
		"default fallback": {
			ctxEntry: &namedContext{},
			want:     "default",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, resolveSANamespace(tc.sa, tc.ctxEntry))
		})
	}
}

// neutralizeWrapperEnv clears every Claude-launcher env var a
// selectCtx round-trip reads, and pins TMPDIR to a per-test dir.
// Without this, an ambient wrapper environment (a test run from
// inside a live Claude session) leaks into routing decisions via
// localView and — worse — publishSidecar overwrites the live
// session's real sidecar symlink.
func neutralizeWrapperEnv(t *testing.T) string {
	t.Helper()

	tmp := t.TempDir()

	t.Setenv("TMPDIR", tmp)
	t.Setenv("KUBECONFIG", "")
	t.Setenv("CLAUDE_KUBECTX_DIR", "")
	t.Setenv("CLAUDE_KUBECTX_LOCAL", "")
	t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")
	t.Setenv("CLAUDE_KUBECTX_GUEST_CONFIG", "")

	return tmp
}

// TestSelectDrainsPriorCleanupOnSuccess pins that handler.selectCtx
// drains the prior SA's release closure as soon as the new one is
// fully provisioned. Without this, concurrent Claude sessions that
// share an --output path leak every prior SA until process exit.
func TestSelectDrainsPriorCleanupOnSuccess(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	neutralizeWrapperEnv(t)

	stdout1, err := json.Marshal(HostSelectResult{
		Path: "/k", SAName: "claude-sa-1", Namespace: "ns",
		Kubeconfig: "/admin", Context: "prod",
	})
	require.NoError(t, err)

	stdout2, err := json.Marshal(HostSelectResult{
		Path: "/k", SAName: "claude-sa-2", Namespace: "ns",
		Kubeconfig: "/admin", Context: "prod",
	})
	require.NoError(t, err)

	type call struct {
		sub  string
		args []string
	}

	var (
		calls []call
		step  int
	)

	fake := func(_ context.Context, sub string, args []string) ([]byte, error) {
		calls = append(calls, call{sub: sub, args: append([]string(nil), args...)})

		switch sub {
		case "select":
			step++

			if step == 1 {
				return stdout1, nil
			}

			return stdout2, nil

		case "release":
			return nil, nil
		}

		return nil, errors.New("unexpected sub: " + sub)
	}

	h := &handler{
		kubeconfigPath: "/admin",
		outputPath:     "/k",
		envLookup:      constLookup(""),
		runHost:        fake,
		sa:             saConfig{role: "view", roleKind: "ClusterRole", expiration: 3600},
	}

	// First select registers a release closure.
	r1, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "prod"})
	require.NoError(t, err)
	require.False(t, r1.IsError, resultText(t, r1))

	require.Equal(t, []call{{sub: "select", args: calls[0].args}}, calls,
		"first select should not trigger any release calls")

	// Second select should drain the previous closure (one
	// release call) only after host select returns.
	r2, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "prod"})
	require.NoError(t, err)
	require.False(t, r2.IsError, resultText(t, r2))

	require.Len(t, calls, 3)
	assert.Equal(t, "select", calls[0].sub)
	assert.Equal(t, "select", calls[1].sub)
	assert.Equal(t, "release", calls[2].sub)
	assert.Contains(t, calls[2].args, "claude-sa-1")

	h.mu.Lock()
	fns := h.cleanupFuncs
	h.mu.Unlock()

	require.Len(t, fns, 1, "only the most recent cleanup should remain")
}

// TestSelectRestoresPrevCleanupOnFailure pins the other half of the
// drain contract: if host select fails, the previous closure must
// be restored so it still runs at process shutdown.
func TestSelectRestoresPrevCleanupOnFailure(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	neutralizeWrapperEnv(t)

	stdout1, err := json.Marshal(HostSelectResult{
		Path: "/k", SAName: "claude-sa-1", Namespace: "ns",
		Kubeconfig: "/admin", Context: "prod",
	})
	require.NoError(t, err)

	var (
		step         int
		releaseCalls int
	)

	fake := func(_ context.Context, sub string, _ []string) ([]byte, error) {
		switch sub {
		case "select":
			step++

			if step == 1 {
				return stdout1, nil
			}

			return nil, errors.New("forbidden")

		case "release":
			releaseCalls++

			return nil, nil
		}

		return nil, errors.New("unexpected sub: " + sub)
	}

	h := &handler{
		kubeconfigPath: "/admin",
		outputPath:     "/k",
		envLookup:      constLookup(""),
		runHost:        fake,
		sa:             saConfig{role: "view", roleKind: "ClusterRole", expiration: 3600},
	}

	r1, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "prod"})
	require.NoError(t, err)
	require.False(t, r1.IsError, resultText(t, r1))

	r2, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "prod"})
	require.NoError(t, err)
	require.True(t, r2.IsError, "second select should fail")

	assert.Zero(t, releaseCalls, "prev release must not run when new provision fails")

	h.mu.Lock()
	fns := h.cleanupFuncs
	h.mu.Unlock()

	require.Len(t, fns, 1, "prev cleanup must be restored after failed provision")

	// Process-shutdown cleanup runs the restored prev closure.
	for _, fn := range fns {
		fn(t.Context())
	}

	assert.Equal(t, 1, releaseCalls, "prev release must run on shutdown")
}

// TestSelectDoesNotDrainEmptyPrev guards against accidentally
// emitting a host release call when there is no prior cleanup.
func TestSelectDoesNotDrainEmptyPrev(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	neutralizeWrapperEnv(t)

	stdout, err := json.Marshal(HostSelectResult{
		Path: "/k", SAName: "claude-sa-1", Namespace: "ns",
		Kubeconfig: "/admin", Context: "prod",
	})
	require.NoError(t, err)

	subs := []string{}

	fake := func(_ context.Context, sub string, _ []string) ([]byte, error) {
		subs = append(subs, sub)

		return stdout, nil
	}

	h := &handler{
		kubeconfigPath: "/admin",
		outputPath:     "/k",
		envLookup:      constLookup(""),
		runHost:        fake,
		sa:             saConfig{role: "view", roleKind: "ClusterRole", expiration: 3600},
	}

	r, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "prod"})
	require.NoError(t, err)
	require.False(t, r.IsError, resultText(t, r))

	assert.Equal(t, []string{"select"}, subs, "no host release should be shelled when prev is empty")
}
