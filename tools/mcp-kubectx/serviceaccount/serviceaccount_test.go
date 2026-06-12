package serviceaccount_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kubetest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/serviceaccount"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg serviceaccount.Config
		err error
	}{
		"valid role": {
			cfg: serviceaccount.Config{Role: "my-role", RoleKind: "Role"},
		},
		"valid cluster role": {
			cfg: serviceaccount.Config{Role: "my-cluster-role", RoleKind: "ClusterRole"},
		},
		"valid cluster scoped": {
			cfg: serviceaccount.Config{Role: "my-cluster-role", RoleKind: "ClusterRole", ClusterScoped: true},
		},
		"defaults to ClusterRole": {
			cfg: serviceaccount.Config{Role: "view"},
		},
		"missing role name": {
			cfg: serviceaccount.Config{},
			err: serviceaccount.ErrMissingRole,
		},
		"invalid role kind": {
			cfg: serviceaccount.Config{Role: "r", RoleKind: "BadKind"},
			err: serviceaccount.ErrInvalidRoleKind,
		},
		"cluster scoped with Role kind": {
			cfg: serviceaccount.Config{Role: "r", RoleKind: "Role", ClusterScoped: true},
			err: serviceaccount.ErrClusterScopedRole,
		},
		"expiration too long": {
			cfg: serviceaccount.Config{Role: "r", Expiration: 100000},
			err: serviceaccount.ErrExpirationTooLong,
		},
		"default expiration": {
			cfg: serviceaccount.Config{Role: "r"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := tc.cfg.Validate()
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCreateWithBinding(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		sa         serviceaccount.Config
		fake       *kubetest.Fake
		ns         string
		instanceID string
		hostID     string
		err        error
		assert     func(t *testing.T, m *kubetest.Fake)
	}{
		"role binding without ids": {
			sa:   serviceaccount.Config{Role: "view", RoleKind: "Role", Expiration: 3600},
			fake: &kubetest.Fake{},
			ns:   "ns",
			assert: func(t *testing.T, m *kubetest.Fake) {
				t.Helper()
				assert.Len(t, m.CreatedSAs, 1)
				assert.Len(t, m.CreatedRoleBindings, 1)
				assert.Empty(t, m.CreatedClusterRoleBindings)

				require.Len(t, m.CreatedSALabels, 1)
				assert.Equal(t, serviceaccount.ManagedByValue, m.CreatedSALabels[0][serviceaccount.ManagedByLabel])

				_, hasInstance := m.CreatedSALabels[0][serviceaccount.InstanceIDLabel]
				_, hasHost := m.CreatedSALabels[0][serviceaccount.HostIDLabel]

				assert.False(t, hasInstance, "instance-id label must be omitted when empty")
				assert.False(t, hasHost, "host-id label must be omitted when empty")
			},
		},
		"role binding with ids": {
			sa:         serviceaccount.Config{Role: "view", RoleKind: "Role", Expiration: 3600},
			fake:       &kubetest.Fake{},
			ns:         "ns",
			instanceID: "inst-abc",
			hostID:     "host-xyz",
			assert: func(t *testing.T, m *kubetest.Fake) {
				t.Helper()
				require.Len(t, m.CreatedSALabels, 1)
				assert.Equal(t, "inst-abc", m.CreatedSALabels[0][serviceaccount.InstanceIDLabel])
				assert.Equal(t, "host-xyz", m.CreatedSALabels[0][serviceaccount.HostIDLabel])

				require.Len(t, m.CreatedRoleBindingLabels, 1)
				assert.Equal(t, "inst-abc", m.CreatedRoleBindingLabels[0][serviceaccount.InstanceIDLabel])
				assert.Equal(t, "host-xyz", m.CreatedRoleBindingLabels[0][serviceaccount.HostIDLabel])
			},
		},
		"cluster role binding namespaced": {
			sa:   serviceaccount.Config{Role: "view", RoleKind: "ClusterRole", Expiration: 3600},
			fake: &kubetest.Fake{},
			ns:   "ns",
			assert: func(t *testing.T, m *kubetest.Fake) {
				t.Helper()
				assert.Len(t, m.CreatedRoleBindings, 1)
				assert.Empty(t, m.CreatedClusterRoleBindings)
			},
		},
		"cluster scoped binding propagates labels": {
			sa: serviceaccount.Config{
				Role: "view", RoleKind: "ClusterRole", ClusterScoped: true, Expiration: 3600,
			},
			fake:       &kubetest.Fake{},
			ns:         "ns",
			instanceID: "inst-1",
			hostID:     "host-1",
			assert: func(t *testing.T, m *kubetest.Fake) {
				t.Helper()
				assert.Len(t, m.CreatedClusterRoleBindings, 1)
				assert.Empty(t, m.CreatedRoleBindings)

				require.Len(t, m.CreatedCRBLabels, 1)
				assert.Equal(t, "inst-1", m.CreatedCRBLabels[0][serviceaccount.InstanceIDLabel])
				assert.Equal(t, "host-1", m.CreatedCRBLabels[0][serviceaccount.HostIDLabel])
			},
		},
		"sa creation failure": {
			sa:   serviceaccount.Config{Role: "view", RoleKind: "Role", Expiration: 3600},
			fake: &kubetest.Fake{CreateSAErr: errors.New("forbidden")},
			ns:   "ns",
			err:  serviceaccount.ErrCreateSA,
		},
		"binding creation failure rolls back the SA": {
			sa:   serviceaccount.Config{Role: "view", RoleKind: "Role", Expiration: 3600},
			fake: &kubetest.Fake{CreateRoleBindingErr: errors.New("forbidden")},
			ns:   "ns",
			err:  serviceaccount.ErrCreateBinding,
			assert: func(t *testing.T, m *kubetest.Fake) {
				t.Helper()
				// The orphaned SA would carry the live serve's own
				// instance-id and dodge every sweep; the rollback
				// delete must fire.
				require.Len(t, m.DeletedSAs, 1)
				assert.Equal(t, m.CreatedSAs[0], m.DeletedSAs[0],
					"the rollback must delete the SA that was just created")
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			saName, err := serviceaccount.CreateWithBinding(
				t.Context(), tc.fake, tc.sa, tc.ns, tc.instanceID, tc.hostID,
			)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)

				if tc.assert != nil {
					tc.assert(t, tc.fake)
				}

				return
			}

			require.NoError(t, err)
			assert.Contains(t, saName, "claude-sa-")
			assert.Equal(t, serviceaccount.BindingName(saName), saName+"-binding")

			if tc.assert != nil {
				tc.assert(t, tc.fake)
			}
		})
	}
}

func TestResolveNamespace(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		sa               serviceaccount.Config
		contextNamespace string
		want             string
	}{
		"explicit config": {
			sa:               serviceaccount.Config{Namespace: "explicit"},
			contextNamespace: "ctx-ns",
			want:             "explicit",
		},
		"context namespace": {
			contextNamespace: "ctx-ns",
			want:             "ctx-ns",
		},
		"default fallback": {
			want: "default",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, serviceaccount.ResolveNamespace(tc.sa, tc.contextNamespace))
		})
	}
}

func TestDescribe(t *testing.T) {
	t.Parallel()

	assert.Equal(t, `ClusterRole "view" (namespaced)`, serviceaccount.Describe(
		serviceaccount.Config{Role: "view", RoleKind: "ClusterRole"},
	))
	assert.Equal(t, `ClusterRole "view" (cluster-scoped)`, serviceaccount.Describe(
		serviceaccount.Config{Role: "view", RoleKind: "ClusterRole", ClusterScoped: true},
	))
}
