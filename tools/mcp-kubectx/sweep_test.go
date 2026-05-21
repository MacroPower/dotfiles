package main

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testHostID is the well-formed host id shared by every sweep
// fixture. Centralized so [validHostID]'s format and the
// fixtures cannot drift independently.
const testHostID = "0123456789abcdef"

// TestShouldDeleteClassification table-drives the classifier
// invariants the sweep depends on. The cases mirror the README's
// recovery semantics: preserve missing-label resources
// conservatively, preserve live-set resources, delete everything
// else.
func TestShouldDeleteClassification(t *testing.T) {
	t.Parallel()

	live := map[string]struct{}{
		"inst-live": {},
	}

	tests := map[string]struct {
		labels map[string]string
		want   bool
	}{
		"missing instance-id label preserves": {
			labels: map[string]string{managedByLabel: managedByValue},
			want:   false,
		},
		"empty instance-id label preserves": {
			labels: map[string]string{instanceIDLabel: ""},
			want:   false,
		},
		"whitespace-only instance-id label preserves": {
			labels: map[string]string{instanceIDLabel: "   "},
			want:   false,
		},
		"live instance-id preserves": {
			labels: map[string]string{instanceIDLabel: "inst-live"},
			want:   false,
		},
		"unknown instance-id deletes": {
			labels: map[string]string{instanceIDLabel: "inst-dead"},
			want:   true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := shouldDelete(ResourceRef{Labels: tc.labels}, live)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestSweepSelectorIncludesHostAndManagedBy pins that the
// LabelSelector emitted by sweepSelector includes both the
// managed-by value and the host id. The apiserver-side filter is
// the cross-host safety boundary: a forgotten host-id token here
// would let a sweep run by host A delete resources owned by host
// B against a shared cluster.
func TestSweepSelectorIncludesHostAndManagedBy(t *testing.T) {
	t.Parallel()

	selector := sweepSelector("host-abc")
	assert.Contains(t, selector, managedByLabel+"="+managedByValue)
	assert.Contains(t, selector, hostIDLabel+"=host-abc")
}

// TestCollectSweepOrphansEnumeratesAllKinds asserts that a single
// collect call lists every supported resource kind and routes
// each candidate into the right sweep slot.
func TestCollectSweepOrphansEnumeratesAllKinds(t *testing.T) {
	t.Parallel()

	mock := &mockKubeClient{
		listSAResp: []ResourceRef{
			{Namespace: "ns1", Name: "sa-dead", Labels: map[string]string{instanceIDLabel: "dead"}},
			{Namespace: "ns2", Name: "sa-live", Labels: map[string]string{instanceIDLabel: "live"}},
		},
		listRBResp: []ResourceRef{
			{Namespace: "ns1", Name: "rb-dead", Labels: map[string]string{instanceIDLabel: "dead"}},
		},
		listCRBResp: []ResourceRef{
			{Name: "crb-dead", Labels: map[string]string{instanceIDLabel: "dead"}},
			{Name: "crb-pre", Labels: map[string]string{}},
		},
	}

	live := map[string]struct{}{"live": {}}

	got, err := collectSweepOrphans(t.Context(), mock, "selector", live)
	require.NoError(t, err)

	// Expect three orphans: sa-dead, rb-dead, crb-dead.
	// Preserved: sa-live (in live set), crb-pre (missing label).
	require.Len(t, got, 3)

	byKind := map[resourceKind][]string{}
	for _, c := range got {
		byKind[c.kind] = append(byKind[c.kind], filepath.Join(c.ref.Namespace, c.ref.Name))
	}

	assert.Equal(t, []string{"ns1/sa-dead"}, byKind[resourceServiceAccount])
	assert.Equal(t, []string{"ns1/rb-dead"}, byKind[resourceRoleBinding])
	assert.Equal(t, []string{"crb-dead"}, byKind[resourceClusterRoleBinding])
}

// TestCollectSweepOrphansToleratesListError pins that a Forbidden
// (or any other) list error on one kind does not abort the sweep.
// Each kind is enumerated independently.
func TestCollectSweepOrphansToleratesListError(t *testing.T) {
	t.Parallel()

	mock := &mockKubeClient{
		listSAErr: errors.New("forbidden"),
		listRBResp: []ResourceRef{
			{Namespace: "ns", Name: "rb-dead", Labels: map[string]string{instanceIDLabel: "dead"}},
		},
		listCRBErr: errors.New("forbidden"),
	}

	got, err := collectSweepOrphans(t.Context(), mock, "selector", nil)
	require.NoError(t, err, "partial list failure must not surface as ErrSweepList")
	require.Len(t, got, 1)
	assert.Equal(t, resourceRoleBinding, got[0].kind)
	assert.Equal(t, "rb-dead", got[0].ref.Name)
}

// TestCollectSweepOrphansSurfacesErrSweepListWhenAllFail pins
// the documented contract: ErrSweepList is returned only when
// every list call fails outright. Operators see this when RBAC
// forbids cluster-wide list on all three resource kinds.
func TestCollectSweepOrphansSurfacesErrSweepListWhenAllFail(t *testing.T) {
	t.Parallel()

	mock := &mockKubeClient{
		listSAErr:  errors.New("forbidden"),
		listRBErr:  errors.New("forbidden"),
		listCRBErr: errors.New("forbidden"),
	}

	got, err := collectSweepOrphans(t.Context(), mock, "selector", nil)
	require.ErrorIs(t, err, ErrSweepList)
	assert.Empty(t, got)
}

// TestDeleteSweepOrphansIssuesEveryDelete pins that the bounded
// worker pool issues a Delete* call for every candidate even when
// some fail. ErrSweepDelete is wrapped in the log line, never
// surfaced from the function.
func TestDeleteSweepOrphansIssuesEveryDelete(t *testing.T) {
	t.Parallel()

	mock := &mockKubeClient{
		deleteSAErr:                 errors.New("not found"),
		deleteClusterRoleBindingErr: errors.New("forbidden"),
	}

	orphans := []sweepCandidate{
		{ref: ResourceRef{Namespace: "ns1", Name: "sa-1"}, kind: resourceServiceAccount},
		{ref: ResourceRef{Namespace: "ns2", Name: "sa-2"}, kind: resourceServiceAccount},
		{ref: ResourceRef{Namespace: "ns3", Name: "rb-1"}, kind: resourceRoleBinding},
		{ref: ResourceRef{Name: "crb-1"}, kind: resourceClusterRoleBinding},
	}

	deleteSweepOrphans(t.Context(), mock, orphans)

	sort.Strings(mock.deletedSAs)
	assert.Equal(t, []string{"ns1/sa-1", "ns2/sa-2"}, mock.deletedSAs)
	assert.Equal(t, []string{"ns3/rb-1"}, mock.deletedRoleBindings)
	assert.Equal(t, []string{"crb-1"}, mock.deletedClusterRoleBindings)
}

// TestRunHostSweepEndToEnd exercises the full flag-parse +
// classify + delete path with a fake client. Pins the contract
// the serve goroutine relies on: passing the calling serve's id
// via --live-instance-id preserves the resources tagged with
// that id and deletes the rest.
func TestRunHostSweepEndToEnd(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &mockKubeClient{
		listSAResp: []ResourceRef{
			{Namespace: "ns", Name: "sa-live", Labels: map[string]string{instanceIDLabel: "live"}},
			{Namespace: "ns", Name: "sa-dead", Labels: map[string]string{instanceIDLabel: "dead"}},
			{Namespace: "ns", Name: "sa-pre", Labels: map[string]string{}},
		},
		listRBResp: []ResourceRef{
			{Namespace: "ns", Name: "rb-dead", Labels: map[string]string{instanceIDLabel: "dead"}},
		},
		listCRBResp: []ResourceRef{
			{Name: "crb-dead", Labels: map[string]string{instanceIDLabel: "dead"}},
		},
	}
	withHostKubeClient(t, mock)

	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	require.NoError(t, runHostSweep(t.Context(), []string{
		"--kubeconfig", kubeconfigPath,
		"--context", "prod",
		"--host-id", testHostID,
		"--live-instance-id", "live",
	}))

	require.Len(t, mock.listedSAs, 1)
	assert.Contains(t, mock.listedSAs[0], managedByLabel+"="+managedByValue)
	assert.Contains(t, mock.listedSAs[0], hostIDLabel+"="+testHostID)

	assert.Equal(t, []string{"ns/sa-dead"}, mock.deletedSAs,
		"only orphan SAs (dead instance, host-id matching) should be deleted")
	assert.Equal(t, []string{"ns/rb-dead"}, mock.deletedRoleBindings)
	assert.Equal(t, []string{"crb-dead"}, mock.deletedClusterRoleBindings)
}

// TestRunHostSweepRejectsInvalidHostID pins the input-validation
// boundary at the start of [runHostSweep]: empty values fail with
// [ErrMissingHostID], any other malformed value fails with
// [ErrInvalidHostID] before the selector is built. The injection
// case covers a tampered host.id smuggling selector
// metacharacters past the validator.
func TestRunHostSweepRejectsInvalidHostID(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		hostID string
		err    error
	}{
		"empty":              {hostID: "", err: ErrMissingHostID},
		"fifteen chars":      {hostID: "0123456789abcde", err: ErrInvalidHostID},
		"seventeen chars":    {hostID: "0123456789abcdef0", err: ErrInvalidHostID},
		"uppercase hex":      {hostID: "0123456789ABCDEF", err: ErrInvalidHostID},
		"non-hex char":       {hostID: "0123456789abcdeg", err: ErrInvalidHostID},
		"selector injection": {hostID: "aaaaaaaaaaaaaaaa,namespace=kube-system", err: ErrInvalidHostID},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := runHostSweep(t.Context(), []string{
				"--kubeconfig", "/dev/null",
				"--host-id", tc.hostID,
			})
			require.ErrorIs(t, err, tc.err)
		})
	}
}

// TestRunHostSweepResolvesCurrentContextWhenEmpty pins that the
// serve goroutine's argv layout (no --context) resolves the
// kubeconfig's current-context. Without this fallback the sweep
// would crash on context-not-found at every startup because the
// MCP `select` has not happened yet.
//
//nolint:paralleltest // mutates package-level state
func TestRunHostSweepResolvesCurrentContextWhenEmpty(t *testing.T) {
	mock := &mockKubeClient{}
	withHostKubeClient(t, mock)

	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	require.NoError(t, runHostSweep(t.Context(), []string{
		"--kubeconfig", kubeconfigPath,
		"--host-id", testHostID,
	}))

	require.NotEmpty(t, mock.listedSAs, "sweep must run even without --context")
}

// TestRunHostSweepEmptyLiveSetSweepsAll pins the destructive
// "manual recovery" semantics: with zero --live-instance-id
// flags every classified resource (instance-id label present
// and non-empty) gets deleted. The README documents the
// caveat.
func TestRunHostSweepEmptyLiveSetSweepsAll(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &mockKubeClient{
		listSAResp: []ResourceRef{
			{Namespace: "ns", Name: "sa-a", Labels: map[string]string{instanceIDLabel: "a"}},
			{Namespace: "ns", Name: "sa-b", Labels: map[string]string{instanceIDLabel: "b"}},
		},
	}
	withHostKubeClient(t, mock)

	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	require.NoError(t, runHostSweep(t.Context(), []string{
		"--kubeconfig", kubeconfigPath,
		"--context", "prod",
		"--host-id", testHostID,
	}))

	sort.Strings(mock.deletedSAs)
	assert.Equal(t, []string{"ns/sa-a", "ns/sa-b"}, mock.deletedSAs)
}

// TestRunHostSweepSkipsWhenNoContextResolved pins the no-context
// safety net: a kubeconfig with empty current-context and no
// --context flag must not crash; the sweep logs and returns 0.
func TestRunHostSweepSkipsWhenNoContextResolved(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &mockKubeClient{}
	withHostKubeClient(t, mock)

	cfg := testKubeconfig()
	cfg.CurrentContext = ""

	kubeconfigPath := writeTestKubeconfig(t, cfg)

	require.NoError(t, runHostSweep(t.Context(), []string{
		"--kubeconfig", kubeconfigPath,
		"--host-id", testHostID,
	}))

	assert.Empty(t, mock.listedSAs, "no resolved context must skip list calls entirely")
}

// TestRunHostSweepToleratesPartialListForbidden pins that
// Forbidden on one resource kind does not abort the sweep when
// other kinds list successfully — the partial result is still
// classified and deleted.
//
//nolint:paralleltest // mutates package-level state
func TestRunHostSweepToleratesPartialListForbidden(t *testing.T) {
	mock := &mockKubeClient{
		listSAErr: errors.New("forbidden"),
		listRBErr: errors.New("forbidden"),
		listCRBResp: []ResourceRef{
			{Name: "crb-dead", Labels: map[string]string{instanceIDLabel: "dead"}},
		},
	}
	withHostKubeClient(t, mock)

	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	require.NoError(t, runHostSweep(t.Context(), []string{
		"--kubeconfig", kubeconfigPath,
		"--context", "prod",
		"--host-id", testHostID,
	}))

	assert.Equal(t, []string{"crb-dead"}, mock.deletedClusterRoleBindings,
		"the one listable kind must still get its orphans deleted")
}

// TestRunHostSweepSurfacesErrSweepListWhenAllFail pins the
// total-failure mode: when every list call fails, runHostSweep
// returns ErrSweepList so operators see a clean signal rather
// than an empty no-op.
//
//nolint:paralleltest // mutates package-level state
func TestRunHostSweepSurfacesErrSweepListWhenAllFail(t *testing.T) {
	mock := &mockKubeClient{
		listSAErr:  errors.New("forbidden"),
		listRBErr:  errors.New("forbidden"),
		listCRBErr: errors.New("forbidden"),
	}
	withHostKubeClient(t, mock)

	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	err := runHostSweep(t.Context(), []string{
		"--kubeconfig", kubeconfigPath,
		"--context", "prod",
		"--host-id", testHostID,
	})
	require.ErrorIs(t, err, ErrSweepList)
}

// TestHandlerRunSweepForwardsArgs pins the argv layout the serve
// goroutine passes through runHost. Missing --context is
// deliberate (sweep resolves cfg.CurrentContext itself);
// --host-id and one --live-instance-id flag per id must be
// present; --kubeconfig is forwarded only when set.
func TestHandlerRunSweepForwardsArgs(t *testing.T) {
	t.Parallel()

	type call struct {
		sub  string
		args []string
	}

	var got call

	h := &handler{
		hostID:     "host-1",
		instanceID: "host-1-inst",
		envLookup:  constLookup(""),
		runHost: func(_ context.Context, sub string, args []string) ([]byte, error) {
			got = call{sub: sub, args: append([]string(nil), args...)}
			return nil, nil
		},
	}

	h.runSweep(t.Context(), map[string]struct{}{"inst-a": {}, "inst-b": {}, "host-1-inst": {}})

	assert.Equal(t, subSweep, got.sub)
	assert.NotContains(t, got.args, "--context", "context resolution lives in runHostSweep")
	assert.NotContains(t, got.args, "--kubeconfig", "kubeconfig is omitted when handler kubeconfigPath is empty")

	hostIDIdx := indexOf(got.args, "--host-id")
	require.GreaterOrEqual(t, hostIDIdx, 0)
	assert.Equal(t, "host-1", got.args[hostIDIdx+1])

	// Count repeated --live-instance-id flags rather than asserting
	// order; map iteration is unspecified.
	count := 0

	for i, a := range got.args {
		if a == "--live-instance-id" {
			count++

			assert.Contains(t, []string{"inst-a", "inst-b", "host-1-inst"}, got.args[i+1])
		}
	}

	assert.Equal(t, 3, count)
}

// TestHandlerRunSweepIncludesKubeconfigWhenSet pins the optional
// --kubeconfig forwarding path.
func TestHandlerRunSweepIncludesKubeconfigWhenSet(t *testing.T) {
	t.Parallel()

	var args []string

	h := &handler{
		kubeconfigPath: "/admin/kube",
		hostID:         "host-1",
		instanceID:     "host-1-inst",
		envLookup:      constLookup(""),
		runHost: func(_ context.Context, _ string, a []string) ([]byte, error) {
			args = append([]string(nil), a...)
			return nil, nil
		},
	}

	h.runSweep(t.Context(), map[string]struct{}{"host-1-inst": {}})

	idx := indexOf(args, "--kubeconfig")
	require.GreaterOrEqual(t, idx, 0)
	assert.Equal(t, "/admin/kube", args[idx+1])
}

// TestHandlerRunSweepLogsErrorWithoutPropagation pins that a
// runHost failure inside the sweep is swallowed. The serve
// goroutine never blocks startup on the result.
func TestHandlerRunSweepLogsErrorWithoutPropagation(t *testing.T) {
	t.Parallel()

	h := &handler{
		hostID:     "host-1",
		instanceID: "host-1-inst",
		envLookup:  constLookup(""),
		runHost: func(_ context.Context, _ string, _ []string) ([]byte, error) {
			return nil, errors.New("shell out failed")
		},
	}

	// No panic, no return value to assert; reaching this line
	// means the error did not propagate.
	h.runSweep(t.Context(), map[string]struct{}{"host-1-inst": {}})
}

// TestHandlerRunSweepSkipsDegradedDiscovery pins both
// discovery-glitch guards: an empty live set, or a non-empty
// live set missing h.instanceID. Either case would otherwise
// shell out with zero --live-instance-id flags and trip
// [runHostSweep]'s destructive manual-recovery semantics.
func TestHandlerRunSweepSkipsDegradedDiscovery(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		liveSet    map[string]struct{}
		instanceID string
	}{
		"empty live set": {
			instanceID: "host-1-inst",
			liveSet:    map[string]struct{}{},
		},
		"own id missing from live set": {
			instanceID: "my-id",
			liveSet:    map[string]struct{}{"someone-else": {}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			called := false
			h := &handler{
				hostID:     "host-1",
				instanceID: tc.instanceID,
				envLookup:  constLookup(""),
				runHost: func(_ context.Context, _ string, _ []string) ([]byte, error) {
					called = true
					return nil, nil
				},
			}

			h.runSweep(t.Context(), tc.liveSet)

			assert.False(t, called, "degraded discovery must refuse to shell out")
		})
	}
}

// indexOf returns the index of needle in s, or -1.
func indexOf(s []string, needle string) int {
	for i, v := range s {
		if v == needle {
			return i
		}
	}

	return -1
}

// TestRunHostSweepFlagParseError pins the [ErrParseHostSweepFlags]
// wrapping when flag.Parse fails. Ensures the sentinel is
// reachable.
func TestRunHostSweepFlagParseError(t *testing.T) {
	t.Parallel()

	err := runHostSweep(t.Context(), []string{"--bogus-flag"})
	require.ErrorIs(t, err, ErrParseHostSweepFlags)
}

// TestRunHostSweepIgnoresEmptyLiveID pins that an empty value
// passed via --live-instance-id="" does not get inserted into
// the live set (which would then preserve resources missing the
// instance-id label twice -- once from the missing-label rule,
// once from the empty-id rule).
func TestRunHostSweepIgnoresEmptyLiveID(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &mockKubeClient{
		listSAResp: []ResourceRef{
			{Namespace: "ns", Name: "sa-a", Labels: map[string]string{instanceIDLabel: ""}},
			{Namespace: "ns", Name: "sa-b", Labels: map[string]string{instanceIDLabel: "real-id"}},
		},
	}
	withHostKubeClient(t, mock)

	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	require.NoError(t, runHostSweep(t.Context(), []string{
		"--kubeconfig", kubeconfigPath,
		"--context", "prod",
		"--host-id", testHostID,
		"--live-instance-id", "",
	}))

	// sa-a was preserved by the empty-label rule.
	// sa-b should still be deleted (real-id not in live set).
	assert.Equal(t, []string{"ns/sa-b"}, mock.deletedSAs)
}
