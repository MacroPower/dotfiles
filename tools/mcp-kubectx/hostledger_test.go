package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/identity"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kube"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kubetest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/ledger"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/serviceaccount"
)

// hookClient runs a hook before a call reaches the embedded fake.
// A test uses the hook to act at one exact point of a host
// subcommand. The hook reads or changes the ledger there, or it
// tells another call that this point came.
type hookClient struct {
	*kubetest.Fake

	beforeCreateSA func()
	beforeListSAs  func()
	beforeDeleteSA func()
}

func (c *hookClient) CreateServiceAccount(
	ctx context.Context,
	namespace, name string,
	labels map[string]string,
) error {
	if c.beforeCreateSA != nil {
		c.beforeCreateSA()
	}

	//nolint:wrapcheck // the test double passes the error of the fake through
	return c.Fake.CreateServiceAccount(ctx, namespace, name, labels)
}

func (c *hookClient) DeleteServiceAccount(ctx context.Context, namespace, name string) error {
	if c.beforeDeleteSA != nil {
		c.beforeDeleteSA()
	}

	//nolint:wrapcheck // the test double passes the error of the fake through
	return c.Fake.DeleteServiceAccount(ctx, namespace, name)
}

func (c *hookClient) ListServiceAccounts(ctx context.Context, selector string) ([]kube.ResourceRef, error) {
	if c.beforeListSAs != nil {
		c.beforeListSAs()
	}

	//nolint:wrapcheck // the test double passes the error of the fake through
	return c.Fake.ListServiceAccounts(ctx, selector)
}

// selectProd runs `host select prod` against kubeconfigPath with
// the ServiceAccount namespace ns and the extra flags. With
// clusterScoped, the host config asks for a ClusterRoleBinding.
func selectProd(t *testing.T, kubeconfigPath string, clusterScoped bool, extra ...string) error {
	t.Helper()

	cfg := testHostConfig()
	cfg.Namespace = "ns"
	cfg.ClusterScoped = clusterScoped

	return runHostSelect(t.Context(), kubeconfigPath, cfg, append([]string{"prod"}, extra...))
}

// listRecords returns the records of l. It fails the test when the
// read of the ledger fails.
func listRecords(t *testing.T, l *ledger.Ledger) []ledger.Record {
	t.Helper()

	records, err := l.List()
	require.NoError(t, err)

	return records
}

// addRecords adds each record to l.
func addRecords(t *testing.T, l *ledger.Ledger, records ...ledger.Record) {
	t.Helper()

	for _, r := range records {
		require.NoError(t, l.Add(r))
	}
}

// TestHostSelectRecordsServiceAccount pins the provision record
// that `host select` writes for a `serve`. The record locates the
// created SA, names its binding kind, and holds the time of the
// call. It is on disk before the create call reaches the cluster.
func TestHostSelectRecordsServiceAccount(t *testing.T) { //nolint:paralleltest // mutates package-level state
	tests := map[string]struct {
		clusterScoped bool
	}{
		"role binding": {},
		"cluster role binding": {
			clusterScoped: true,
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests mutate package-level state
		t.Run(name, func(t *testing.T) {
			records := withHostLedger(t)

			var atCreate []ledger.Record

			fake := &hookClient{Fake: &kubetest.Fake{}}
			fake.beforeCreateSA = func() { atCreate = listRecords(t, records) }

			withHostKubeClient(t, fake)
			withHostStdout(t)

			path := writeTestKubeconfig(t, testKubeconfig())

			before := time.Now()

			require.NoError(t, selectProd(t, path, tc.clusterScoped,
				"--sa-instance-id", testInstanceID,
				"--sa-host-id", testHostID,
			))

			after := time.Now()

			require.Len(t, fake.CreatedSAs, 1)

			got := listRecords(t, records)
			require.Len(t, got, 1)

			written := got[0].Written
			assert.False(t, written.Before(before), "record written at %v, before the call at %v", written, before)
			assert.False(t, written.After(after), "record written at %v, after the call at %v", written, after)

			want := []ledger.Record{{
				HostID:         identity.Digest(testHostID),
				InstanceID:     testInstanceID,
				Kubeconfig:     path,
				Context:        "prod",
				Namespace:      "ns",
				ServiceAccount: strings.TrimPrefix(fake.CreatedSAs[0], "ns/"),
				ClusterScoped:  tc.clusterScoped,
				Written:        written,
			}}

			assert.Equal(t, want, got)
			assert.Equal(t, want, atCreate, "the record must be on disk before the create call")
		})
	}
}

// TestHostSelectCreatesNothingWithoutRecord pins that `host select`
// stops before the first cluster-side mutation when it cannot write
// the provision record. An SA without a record is one that no sweep
// finds on a cluster other than the current-context.
func TestHostSelectCreatesNothingWithoutRecord(t *testing.T) { //nolint:paralleltest // mutates package-level state
	tests := map[string]struct {
		blockLedger    bool
		readOnlyLedger bool
		instanceID     string
		hostID         string
		err            error
	}{
		"malformed instance id": {
			instanceID: "instance-xyz",
			hostID:     testHostID,
			err:        ErrInvalidInstanceID,
		},
		"malformed host id": {
			instanceID: testInstanceID,
			hostID:     "host-abc",
			err:        ErrInvalidHostID,
		},
		"ledger directory cannot be created": {
			blockLedger: true,
			instanceID:  testInstanceID,
			hostID:      testHostID,
			err:         ledger.ErrAdd,
		},
		"record file cannot be written": {
			readOnlyLedger: true,
			instanceID:     testInstanceID,
			hostID:         testHostID,
			err:            ledger.ErrAdd,
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests mutate package-level state
		t.Run(name, func(t *testing.T) {
			withHostLedger(t)

			if tc.blockLedger {
				// A regular file where the parent of the ledger
				// directory belongs makes the directory impossible
				// to create.
				blocker := filepath.Join(t.TempDir(), "file")
				require.NoError(t, os.WriteFile(blocker, nil, 0o600))

				hostLedgerDir = func() string { return filepath.Join(blocker, "ledger") }
			}

			if tc.readOnlyLedger {
				if os.Geteuid() == 0 {
					t.Skip("write-permission failure cannot be provoked as root")
				}

				// The ledger directory exists, so the failure comes
				// from the write of the record file and not from the
				// creation of the directory.
				dir := filepath.Join(t.TempDir(), "ledger")
				require.NoError(t, os.Mkdir(dir, 0o500))
				//nolint:errcheck // best-effort restore so TempDir cleanup can remove dir
				t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

				hostLedgerDir = func() string { return dir }
			}

			fake := &kubetest.Fake{}

			withHostKubeClient(t, fake)
			withHostStdout(t)

			err := selectProd(t, writeTestKubeconfig(t, testKubeconfig()), false,
				"--sa-instance-id", tc.instanceID,
				"--sa-host-id", tc.hostID,
			)
			require.ErrorIs(t, err, tc.err)

			assert.Empty(t, fake.CreatedSAs, "no SA may exist without its record")
		})
	}
}

// TestHostSelectRecordAfterFailedCreate pins what happens to the
// record when the create fails. The record goes when the apiserver
// refused the create, because the failure then left nothing on the
// cluster. It stays after a create call that failed without a
// refusal, because the apiserver might have stored the SA or the
// binding. AlreadyExists counts as such a failure, because
// client-go gets it when it sends a create again after an attempt
// that stored the object. The record also stays when the rollback
// of the SA failed, because the SA is then still there.
func TestHostSelectRecordAfterFailedCreate(t *testing.T) { //nolint:paralleltest // mutates package-level state
	tests := map[string]struct {
		fake          *kubetest.Fake
		err           error
		stays         bool
		clusterScoped bool
	}{
		"SA create is forbidden": {
			fake: &kubetest.Fake{CreateSAErr: forbiddenErr("serviceaccounts", "claude-sa-1")},
			err:  serviceaccount.ErrCreateSA,
		},
		"SA create finds the name taken": {
			fake: &kubetest.Fake{
				CreateSAErr: fmt.Errorf("create: %w",
					apierrors.NewAlreadyExists(schema.GroupResource{Resource: "serviceaccounts"}, "claude-sa-1")),
			},
			err:   serviceaccount.ErrCreateSA,
			stays: true,
		},
		"SA create loses its connection": {
			fake:  &kubetest.Fake{CreateSAErr: errors.New("read tcp: connection reset by peer")},
			err:   serviceaccount.ErrCreateSA,
			stays: true,
		},
		"binding is forbidden and the rollback works": {
			fake: &kubetest.Fake{CreateRoleBindingErr: forbiddenErr("rolebindings", "claude-sa-1-binding")},
			err:  serviceaccount.ErrCreateBinding,
		},
		"binding loses its connection and the rollback works": {
			fake:  &kubetest.Fake{CreateRoleBindingErr: errors.New("read tcp: connection reset by peer")},
			err:   serviceaccount.ErrCreateBinding,
			stays: true,
		},
		"binding finds the name taken and the rollback works": {
			fake: &kubetest.Fake{
				CreateRoleBindingErr: fmt.Errorf("create: %w",
					apierrors.NewAlreadyExists(schema.GroupResource{Resource: "rolebindings"}, "claude-sa-1-binding")),
			},
			err:   serviceaccount.ErrCreateBinding,
			stays: true,
		},
		"cluster role binding loses its connection": {
			fake:          &kubetest.Fake{CreateClusterRoleBindingErr: errors.New("http2: server sent GOAWAY")},
			clusterScoped: true,
			err:           serviceaccount.ErrCreateBinding,
			stays:         true,
		},
		"binding is forbidden and the rollback fails": {
			fake: &kubetest.Fake{
				CreateRoleBindingErr: forbiddenErr("rolebindings", "claude-sa-1-binding"),
				DeleteSAErr:          errors.New("connection reset"),
			},
			err:   serviceaccount.ErrCreateBinding,
			stays: true,
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests mutate package-level state
		t.Run(name, func(t *testing.T) {
			records := withHostLedger(t)

			withHostKubeClient(t, tc.fake)
			withHostStdout(t)

			err := selectProd(t, writeTestKubeconfig(t, testKubeconfig()), tc.clusterScoped,
				"--sa-instance-id", testInstanceID,
				"--sa-host-id", testHostID,
			)
			require.ErrorIs(t, err, tc.err)

			got := listRecords(t, records)

			if !tc.stays {
				assert.Empty(t, got)

				return
			}

			require.Len(t, got, 1)
			require.Len(t, tc.fake.CreatedSAs, 1)
			assert.Equal(t, tc.fake.CreatedSAs[0], "ns/"+got[0].ServiceAccount)
			assert.Equal(t, tc.clusterScoped, got[0].ClusterScoped,
				"the record must name the binding kind that the create tried")
		})
	}
}

// TestHostReleaseRemovesRecordsOfTheServiceAccount pins which
// records `host release` removes after a clean delete. It removes
// every record of the released SA, whichever `serve` owns the
// record, and it leaves the records of every other SA.
//
//nolint:paralleltest // mutates package-level state
func TestHostReleaseRemovesRecordsOfTheServiceAccount(t *testing.T) {
	records := withHostLedger(t)

	withHostKubeClient(t, &kubetest.Fake{})

	released := ledger.Record{
		HostID:         identity.Digest(testHostID),
		InstanceID:     testInstanceID,
		Kubeconfig:     "/admin/kube",
		Context:        "prod",
		Namespace:      "ns",
		ServiceAccount: "claude-sa-1",
	}

	otherOwner := released
	otherOwner.HostID, otherOwner.InstanceID = testOtherHostID, testDeadID

	otherName := released
	otherName.ServiceAccount = "claude-sa-2"

	otherNamespace := released
	otherNamespace.Namespace = "other"

	otherContext := released
	otherContext.Context = "staging"

	otherKubeconfig := released
	otherKubeconfig.Kubeconfig = "/other/kube"

	addRecords(t, records, released, otherOwner, otherName, otherNamespace, otherContext, otherKubeconfig)

	require.NoError(t, runHostRelease(t.Context(), "/admin/kube", append([]string{
		"--context", "prod",
		"--sa", "claude-sa-1",
		"--namespace", "ns",
	}, ownerFlags()...)))

	assert.ElementsMatch(t,
		[]ledger.Record{otherName, otherNamespace, otherContext, otherKubeconfig},
		listRecords(t, records),
	)
}

// TestHostReleaseRecordFollowsCluster pins when `host release`
// removes the record. The record goes only after the cluster holds
// neither the SA nor the binding. A failed delete keeps the record,
// so that a sweep deletes the SA after its `serve` has died.
func TestHostReleaseRecordFollowsCluster(t *testing.T) { //nolint:paralleltest // mutates package-level state
	tests := map[string]struct {
		fake  *kubetest.Fake
		stays bool
	}{
		"both deleted": {
			fake: &kubetest.Fake{},
		},
		"both already gone": {
			fake: &kubetest.Fake{
				DeleteSAErr:          notFoundErr("serviceaccounts", "claude-sa-1"),
				DeleteRoleBindingErr: notFoundErr("rolebindings", "claude-sa-1-binding"),
			},
		},
		"SA delete fails": {
			fake:  &kubetest.Fake{DeleteSAErr: errors.New("timeout")},
			stays: true,
		},
		"binding delete fails": {
			fake:  &kubetest.Fake{DeleteRoleBindingErr: errors.New("timeout")},
			stays: true,
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests mutate package-level state
		t.Run(name, func(t *testing.T) {
			records := withHostLedger(t)

			withHostKubeClient(t, tc.fake)

			record := ledger.Record{
				HostID:         identity.Digest(testHostID),
				InstanceID:     testInstanceID,
				Kubeconfig:     "/admin/kube",
				Context:        "prod",
				Namespace:      "ns",
				ServiceAccount: "claude-sa-1",
			}
			addRecords(t, records, record)

			require.NoError(t, runHostRelease(t.Context(), "/admin/kube", append([]string{
				"--context", "prod",
				"--sa", "claude-sa-1",
				"--namespace", "ns",
			}, ownerFlags()...)))

			if tc.stays {
				assert.Equal(t, []ledger.Record{record}, listRecords(t, records))
			} else {
				assert.Empty(t, listRecords(t, records))
			}
		})
	}
}

// TestHostReleaseUnreadableLedger pins that `host release` deletes
// nothing, and exits 0, when it cannot read the ledger. Without the
// record, it cannot tell that the caller owns the SA. A sweep that
// can read the ledger later deletes the SA after its `serve` has
// died.
//
//nolint:paralleltest // mutates package-level state
func TestHostReleaseUnreadableLedger(t *testing.T) {
	withUnreadableHostLedger(t)

	fake := &kubetest.Fake{}
	withHostKubeClient(t, fake)

	require.NoError(t, runHostRelease(t.Context(), "/admin/kube", append([]string{
		"--context", "prod",
		"--sa", "claude-sa-1",
		"--namespace", "ns",
	}, ownerFlags()...)))

	assert.Empty(t, fake.DeletedSAs)
	assert.Empty(t, fake.DeletedRoleBindings)
}
