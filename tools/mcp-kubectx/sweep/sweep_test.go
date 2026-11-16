package sweep_test

import (
	"errors"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kube"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kubetest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/serviceaccount"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/sweep"
)

// liveAsOf is the time of the live set in every test of
// [sweep.CollectOrphans]. created is the creation time of each
// resource that the tests expect the sweep to delete, an hour
// before the live set.
var (
	liveAsOf = time.Date(2026, time.March, 4, 12, 30, 0, 0, time.UTC)
	created  = liveAsOf.Add(-time.Hour)
)

// TestCollectOrphansClassification table-drives the classifier
// invariants the sweep depends on, exercised through the public
// CollectOrphans surface. The cases mirror the README's recovery
// semantics: preserve missing-label resources conservatively,
// preserve live-set resources, preserve resources that a serve
// created after the live set, and delete everything else.
func TestCollectOrphansClassification(t *testing.T) {
	t.Parallel()

	live := map[string]struct{}{
		"inst-live": {},
	}

	dead := map[string]string{serviceaccount.InstanceIDLabel: "inst-dead"}

	tests := map[string]struct {
		created time.Time
		labels  map[string]string
		want    bool
	}{
		"missing instance-id label preserves": {
			created: created,
			labels:  map[string]string{serviceaccount.ManagedByLabel: serviceaccount.ManagedByValue},
			want:    false,
		},
		"empty instance-id label preserves": {
			created: created,
			labels:  map[string]string{serviceaccount.InstanceIDLabel: ""},
			want:    false,
		},
		"whitespace-only instance-id label preserves": {
			created: created,
			labels:  map[string]string{serviceaccount.InstanceIDLabel: "   "},
			want:    false,
		},
		"live instance-id preserves": {
			created: created,
			labels:  map[string]string{serviceaccount.InstanceIDLabel: "inst-live"},
			want:    false,
		},
		"unknown instance-id deletes": {
			created: created,
			labels:  dead,
			want:    true,
		},
		"unknown instance-id created after the live set preserves": {
			created: liveAsOf.Add(time.Second),
			labels:  dead,
			want:    false,
		},
		"unknown instance-id created at the time of the live set preserves": {
			created: liveAsOf,
			labels:  dead,
			want:    false,
		},
		"unknown instance-id created within the slack preserves": {
			created: liveAsOf.Add(time.Second - sweep.CreationSlack),
			labels:  dead,
			want:    false,
		},
		"unknown instance-id created at the start of the slack preserves": {
			created: liveAsOf.Add(-sweep.CreationSlack),
			labels:  dead,
			want:    false,
		},
		"unknown instance-id created before the slack deletes": {
			created: liveAsOf.Add(-sweep.CreationSlack - time.Second),
			labels:  dead,
			want:    true,
		},
		"unknown instance-id without a creation time preserves": {
			labels: dead,
			want:   false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fake := &kubetest.Fake{
				ListSAResp: []kube.ResourceRef{{Namespace: "ns", Name: "sa", Labels: tc.labels, Created: tc.created}},
			}

			got, err := sweep.CollectOrphans(t.Context(), fake, "selector", live, liveAsOf)
			require.NoError(t, err)

			if tc.want {
				require.Len(t, got, 1)
			} else {
				assert.Empty(t, got)
			}
		})
	}
}

// TestSelectorIncludesHostAndManagedBy pins that the LabelSelector
// emitted by Selector includes both the managed-by value and the
// host id. The apiserver-side filter is the cross-host safety
// boundary: a forgotten host-id token here would let a sweep run by
// host A delete resources owned by host B against a shared cluster.
func TestSelectorIncludesHostAndManagedBy(t *testing.T) {
	t.Parallel()

	selector := sweep.Selector("host-abc")
	assert.Contains(t, selector, serviceaccount.ManagedByLabel+"="+serviceaccount.ManagedByValue)
	assert.Contains(t, selector, serviceaccount.HostIDLabel+"=host-abc")
}

// TestCollectOrphansEnumeratesAllKinds asserts that a single collect
// call lists every supported resource kind and routes each candidate
// into the right sweep slot. A late resource of each kind pins that
// the creation time keeps a resource of every kind.
func TestCollectOrphansEnumeratesAllKinds(t *testing.T) {
	t.Parallel()

	dead := map[string]string{serviceaccount.InstanceIDLabel: "dead"}
	late := liveAsOf.Add(time.Second)

	fake := &kubetest.Fake{
		ListSAResp: []kube.ResourceRef{
			{Created: created, Namespace: "ns1", Name: "sa-dead", Labels: dead},
			{
				Created:   created,
				Namespace: "ns2",
				Name:      "sa-live",
				Labels:    map[string]string{serviceaccount.InstanceIDLabel: "live"},
			},
			{Created: late, Namespace: "ns1", Name: "sa-late", Labels: dead},
		},
		ListRBResp: []kube.ResourceRef{
			{Created: created, Namespace: "ns1", Name: "rb-dead", Labels: dead},
			{Created: late, Namespace: "ns1", Name: "rb-late", Labels: dead},
		},
		ListCRBResp: []kube.ResourceRef{
			{Created: created, Name: "crb-dead", Labels: dead},
			{Created: created, Name: "crb-pre", Labels: map[string]string{}},
			{Created: late, Name: "crb-late", Labels: dead},
		},
	}

	live := map[string]struct{}{"live": {}}

	got, err := sweep.CollectOrphans(t.Context(), fake, "selector", live, liveAsOf)
	require.NoError(t, err)

	// Expect three orphans: sa-dead, rb-dead, crb-dead.
	// Preserved: sa-live (in live set), crb-pre (missing label), and
	// the three late resources (created after the live set).
	require.Len(t, got, 3)

	byKind := map[sweep.Kind][]string{}
	for _, c := range got {
		byKind[c.Kind] = append(byKind[c.Kind], filepath.Join(c.Ref.Namespace, c.Ref.Name))
	}

	assert.Equal(t, []string{"ns1/sa-dead"}, byKind[sweep.KindServiceAccount])
	assert.Equal(t, []string{"ns1/rb-dead"}, byKind[sweep.KindRoleBinding])
	assert.Equal(t, []string{"crb-dead"}, byKind[sweep.KindClusterRoleBinding])
}

// TestCollectOrphansToleratesListError pins that a Forbidden (or any
// other) list error on one kind does not abort the sweep. Each kind
// is enumerated independently.
func TestCollectOrphansToleratesListError(t *testing.T) {
	t.Parallel()

	fake := &kubetest.Fake{
		ListSAErr: errors.New("forbidden"),
		ListRBResp: []kube.ResourceRef{
			{
				Created:   created,
				Namespace: "ns",
				Name:      "rb-dead",
				Labels:    map[string]string{serviceaccount.InstanceIDLabel: "dead"},
			},
		},
		ListCRBErr: errors.New("forbidden"),
	}

	got, err := sweep.CollectOrphans(t.Context(), fake, "selector", nil, liveAsOf)
	require.NoError(t, err, "partial list failure must not surface as ErrList")
	require.Len(t, got, 1)
	assert.Equal(t, sweep.KindRoleBinding, got[0].Kind)
	assert.Equal(t, "rb-dead", got[0].Ref.Name)
}

// TestCollectOrphansSurfacesErrListWhenAllFail pins the documented
// contract: ErrList is returned only when every list call fails
// outright. Operators see this when RBAC forbids cluster-wide list
// on all three resource kinds.
func TestCollectOrphansSurfacesErrListWhenAllFail(t *testing.T) {
	t.Parallel()

	fake := &kubetest.Fake{
		ListSAErr:  errors.New("forbidden"),
		ListRBErr:  errors.New("forbidden"),
		ListCRBErr: errors.New("forbidden"),
	}

	got, err := sweep.CollectOrphans(t.Context(), fake, "selector", nil, liveAsOf)
	require.ErrorIs(t, err, sweep.ErrList)
	assert.Empty(t, got)
}

// TestDeleteOrphansIssuesEveryDelete pins that the bounded worker
// pool issues a Delete* call for every candidate even when some
// fail. ErrDelete is wrapped in the log line, never surfaced from
// the function.
func TestDeleteOrphansIssuesEveryDelete(t *testing.T) {
	t.Parallel()

	fake := &kubetest.Fake{
		DeleteSAErr:                 errors.New("not found"),
		DeleteClusterRoleBindingErr: errors.New("forbidden"),
	}

	orphans := []sweep.Candidate{
		{Ref: kube.ResourceRef{Namespace: "ns1", Name: "sa-1"}, Kind: sweep.KindServiceAccount},
		{Ref: kube.ResourceRef{Namespace: "ns2", Name: "sa-2"}, Kind: sweep.KindServiceAccount},
		{Ref: kube.ResourceRef{Namespace: "ns3", Name: "rb-1"}, Kind: sweep.KindRoleBinding},
		{Ref: kube.ResourceRef{Name: "crb-1"}, Kind: sweep.KindClusterRoleBinding},
	}

	sweep.DeleteOrphans(t.Context(), fake, orphans)

	sort.Strings(fake.DeletedSAs)
	assert.Equal(t, []string{"ns1/sa-1", "ns2/sa-2"}, fake.DeletedSAs)
	assert.Equal(t, []string{"ns3/rb-1"}, fake.DeletedRoleBindings)
	assert.Equal(t, []string{"crb-1"}, fake.DeletedClusterRoleBindings)
}
