package serviceaccount_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kubetest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/serviceaccount"
)

// testSAName is the ServiceAccount name that the create and release
// cases pass in.
const testSAName = "claude-sa-0123abcd"

// notFound returns the error that the client-go client returns for
// a delete of an object that does not exist. It carries the same
// wrapping as the errors of kube.Clientset.
func notFound(resource, name string) error {
	return fmt.Errorf("delete: %w", apierrors.NewNotFound(schema.GroupResource{Resource: resource}, name))
}

// alreadyExists returns the error that the client-go client returns
// for a create of a name that the cluster already holds. It carries
// the same wrapping as the errors of kube.Clientset.
func alreadyExists(resource, name string) error {
	return fmt.Errorf("create: %w", apierrors.NewAlreadyExists(schema.GroupResource{Resource: resource}, name))
}

// forbidden returns the error that the client-go client returns for
// a create that RBAC refuses. It carries the same wrapping as the
// errors of kube.Clientset.
func forbidden(resource string) error {
	return fmt.Errorf("create: %w", apierrors.NewForbidden(
		schema.GroupResource{Resource: resource}, testSAName, errors.New("denied"),
	))
}

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
			fake: &kubetest.Fake{CreateSAErr: forbidden("serviceaccounts")},
			ns:   "ns",
			err:  serviceaccount.ErrCreateSA,
		},
		"binding creation failure rolls back the SA": {
			sa:   serviceaccount.Config{Role: "view", RoleKind: "Role", Expiration: 3600},
			fake: &kubetest.Fake{CreateRoleBindingErr: forbidden("rolebindings")},
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

			err := serviceaccount.CreateWithBinding(
				t.Context(), tc.fake, tc.sa, tc.ns, testSAName, tc.instanceID, tc.hostID,
			)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				require.NotErrorIs(t, err, serviceaccount.ErrRollback,
					"a failure that leaves nothing on the cluster must not report a failed rollback")
				require.NotErrorIs(t, err, serviceaccount.ErrMaybeCreated,
					"a refused create must not report that the cluster might hold the object")

				if tc.assert != nil {
					tc.assert(t, tc.fake)
				}

				return
			}

			require.NoError(t, err)
			assert.Equal(t, []string{tc.ns + "/" + testSAName}, tc.fake.CreatedSAs,
				"the SA must carry the name that the caller picked")

			if tc.assert != nil {
				tc.assert(t, tc.fake)
			}
		})
	}
}

// TestCreateWithBindingReportsFailedRollback pins how the error
// tells a caller whether the ServiceAccount stayed on the cluster
// after a binding failure. The error wraps
// [serviceaccount.ErrRollback] only when the rollback delete failed
// for a ServiceAccount that still exists.
func TestCreateWithBindingReportsFailedRollback(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		deleteErr error
		stays     bool
	}{
		"rollback delete fails": {
			deleteErr: errors.New("connection reset"),
			stays:     true,
		},
		"rollback finds the SA gone": {
			deleteErr: notFound("serviceaccounts", testSAName),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fake := &kubetest.Fake{
				CreateRoleBindingErr: forbidden("rolebindings"),
				DeleteSAErr:          tc.deleteErr,
			}

			err := serviceaccount.CreateWithBinding(
				t.Context(), fake,
				serviceaccount.Config{Role: "view", RoleKind: "Role", Expiration: 3600},
				"ns", testSAName, "", "",
			)
			require.ErrorIs(t, err, serviceaccount.ErrCreateBinding)

			if tc.stays {
				require.ErrorIs(t, err, serviceaccount.ErrRollback)
			} else {
				require.NotErrorIs(t, err, serviceaccount.ErrRollback)
			}
		})
	}
}

// TestCreateWithBindingReportsUncertainCreate pins when the error
// tells a caller that the cluster might hold an object of a failed
// create. The error wraps [serviceaccount.ErrMaybeCreated] after
// every create failure that the apiserver did not refuse. After
// such a failure the apiserver might have stored the object. An
// AlreadyExists answer counts as such a failure. AlreadyExists
// answers a create that client-go sends again after an attempt that
// stored the object. The error of a refused create does not wrap it.
func TestCreateWithBindingReportsUncertainCreate(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		fake          *kubetest.Fake
		clusterScoped bool
		err           error
		maybeCreated  bool
	}{
		"SA create loses its connection": {
			fake:         &kubetest.Fake{CreateSAErr: errors.New("read tcp: connection reset by peer")},
			err:          serviceaccount.ErrCreateSA,
			maybeCreated: true,
		},
		"SA create times out": {
			fake: &kubetest.Fake{
				CreateSAErr: apierrors.NewTimeoutError("request did not complete", 0),
			},
			err:          serviceaccount.ErrCreateSA,
			maybeCreated: true,
		},
		"SA create is forbidden": {
			fake: &kubetest.Fake{CreateSAErr: forbidden("serviceaccounts")},
			err:  serviceaccount.ErrCreateSA,
		},
		"SA create finds the name taken": {
			fake: &kubetest.Fake{
				CreateSAErr: alreadyExists("serviceaccounts", testSAName),
			},
			err:          serviceaccount.ErrCreateSA,
			maybeCreated: true,
		},
		"SA create runs out of retries on throttling": {
			fake: &kubetest.Fake{
				CreateSAErr: fmt.Errorf("create: %w", apierrors.NewTooManyRequests("throttled", 1)),
			},
			err:          serviceaccount.ErrCreateSA,
			maybeCreated: true,
		},
		"role binding create loses its connection": {
			fake:         &kubetest.Fake{CreateRoleBindingErr: errors.New("read tcp: connection reset by peer")},
			err:          serviceaccount.ErrCreateBinding,
			maybeCreated: true,
		},
		"cluster role binding create loses its connection": {
			fake:          &kubetest.Fake{CreateClusterRoleBindingErr: errors.New("http2: server sent GOAWAY")},
			clusterScoped: true,
			err:           serviceaccount.ErrCreateBinding,
			maybeCreated:  true,
		},
		"role binding create finds the name taken": {
			fake:         &kubetest.Fake{CreateRoleBindingErr: alreadyExists("rolebindings", testSAName+"-binding")},
			err:          serviceaccount.ErrCreateBinding,
			maybeCreated: true,
		},
		"cluster role binding create finds the name taken": {
			fake: &kubetest.Fake{
				CreateClusterRoleBindingErr: alreadyExists("clusterrolebindings", testSAName+"-binding"),
			},
			clusterScoped: true,
			err:           serviceaccount.ErrCreateBinding,
			maybeCreated:  true,
		},
		"cluster role binding create is forbidden": {
			fake:          &kubetest.Fake{CreateClusterRoleBindingErr: forbidden("clusterrolebindings")},
			clusterScoped: true,
			err:           serviceaccount.ErrCreateBinding,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sa := serviceaccount.Config{Role: "view", RoleKind: "Role", Expiration: 3600}
			if tc.clusterScoped {
				sa = serviceaccount.Config{Role: "view", RoleKind: "ClusterRole", ClusterScoped: true, Expiration: 3600}
			}

			err := serviceaccount.CreateWithBinding(t.Context(), tc.fake, sa, "ns", testSAName, "", "")
			require.ErrorIs(t, err, tc.err)
			require.NotErrorIs(t, err, serviceaccount.ErrRollback)

			if tc.maybeCreated {
				require.ErrorIs(t, err, serviceaccount.ErrMaybeCreated)
			} else {
				require.NotErrorIs(t, err, serviceaccount.ErrMaybeCreated)
			}
		})
	}
}

// TestNewName pins the format of a generated name and that two
// calls return two names.
func TestNewName(t *testing.T) {
	t.Parallel()

	first, err := serviceaccount.NewName()
	require.NoError(t, err)
	assert.Regexp(t, `^claude-sa-[0-9a-f]{8}$`, first)

	second, err := serviceaccount.NewName()
	require.NoError(t, err)
	assert.NotEqual(t, first, second)
}

// TestRelease pins which deletes [serviceaccount.Release] issues
// and how it reports their outcome. A delete that finds nothing
// counts as done, and every other failure comes back under the
// sentinel of its resource.
func TestRelease(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		fake          *kubetest.Fake
		clusterScoped bool
		errs          []error
		notErrs       []error
	}{
		"role binding": {
			fake: &kubetest.Fake{},
		},
		"cluster role binding": {
			fake:          &kubetest.Fake{},
			clusterScoped: true,
		},
		"both already gone": {
			fake: &kubetest.Fake{
				DeleteSAErr:          notFound("serviceaccounts", testSAName),
				DeleteRoleBindingErr: notFound("rolebindings", testSAName+"-binding"),
			},
		},
		"cluster role binding already gone": {
			fake: &kubetest.Fake{
				DeleteClusterRoleBindingErr: notFound("clusterrolebindings", testSAName+"-binding"),
			},
			clusterScoped: true,
		},
		"SA delete fails": {
			fake:    &kubetest.Fake{DeleteSAErr: errors.New("timeout")},
			errs:    []error{serviceaccount.ErrDeleteSA},
			notErrs: []error{serviceaccount.ErrDeleteBinding},
		},
		"binding delete fails": {
			fake:    &kubetest.Fake{DeleteRoleBindingErr: errors.New("timeout")},
			errs:    []error{serviceaccount.ErrDeleteBinding},
			notErrs: []error{serviceaccount.ErrDeleteSA},
		},
		"cluster binding delete fails": {
			fake:          &kubetest.Fake{DeleteClusterRoleBindingErr: errors.New("timeout")},
			clusterScoped: true,
			errs:          []error{serviceaccount.ErrDeleteBinding},
			notErrs:       []error{serviceaccount.ErrDeleteSA},
		},
		"both deletes fail": {
			fake: &kubetest.Fake{
				DeleteSAErr:          errors.New("timeout"),
				DeleteRoleBindingErr: errors.New("forbidden"),
			},
			errs: []error{serviceaccount.ErrDeleteSA, serviceaccount.ErrDeleteBinding},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := serviceaccount.Release(t.Context(), tc.fake, "ns", testSAName, tc.clusterScoped)

			if len(tc.errs) == 0 {
				require.NoError(t, err)
			}

			for _, want := range tc.errs {
				require.ErrorIs(t, err, want)
			}

			for _, unwanted := range tc.notErrs {
				require.NotErrorIs(t, err, unwanted)
			}

			// Both deletes run whatever the other one returns.
			assert.Equal(t, []string{"ns/" + testSAName}, tc.fake.DeletedSAs)

			if tc.clusterScoped {
				assert.Equal(t, []string{testSAName + "-binding"}, tc.fake.DeletedClusterRoleBindings)
				assert.Empty(t, tc.fake.DeletedRoleBindings)
			} else {
				assert.Equal(t, []string{"ns/" + testSAName + "-binding"}, tc.fake.DeletedRoleBindings)
				assert.Empty(t, tc.fake.DeletedClusterRoleBindings)
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
