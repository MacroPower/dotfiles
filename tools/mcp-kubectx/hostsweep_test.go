package main

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kube"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kubetest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/serviceaccount"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/sweep"
)

// testHostID is the well-formed host id shared by every sweep
// fixture. Centralized so [identity.Valid]'s format and the
// fixtures cannot drift independently.
const testHostID = "0123456789abcdef"

// TestRunHostSweepEndToEnd exercises the full flag-parse +
// classify + delete path with a fake client. Pins the contract
// the serve goroutine relies on: passing the calling serve's id
// via --live-instance-id preserves the resources tagged with
// that id and deletes the rest.
func TestRunHostSweepEndToEnd(t *testing.T) { //nolint:paralleltest // mutates package-level state
	fake := &kubetest.Fake{
		ListSAResp: []kube.ResourceRef{
			{Namespace: "ns", Name: "sa-live", Labels: map[string]string{serviceaccount.InstanceIDLabel: "live"}},
			{Namespace: "ns", Name: "sa-dead", Labels: map[string]string{serviceaccount.InstanceIDLabel: "dead"}},
			{Namespace: "ns", Name: "sa-pre", Labels: map[string]string{}},
		},
		ListRBResp: []kube.ResourceRef{
			{Namespace: "ns", Name: "rb-dead", Labels: map[string]string{serviceaccount.InstanceIDLabel: "dead"}},
		},
		ListCRBResp: []kube.ResourceRef{
			{Name: "crb-dead", Labels: map[string]string{serviceaccount.InstanceIDLabel: "dead"}},
		},
	}
	withHostKubeClient(t, fake)

	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	require.NoError(t, runHostSweep(t.Context(), []string{
		"--kubeconfig", kubeconfigPath,
		"--context", "prod",
		"--host-id", testHostID,
		"--live-instance-id", "live",
	}))

	require.Len(t, fake.ListedSAs, 1)
	assert.Contains(t, fake.ListedSAs[0], serviceaccount.ManagedByLabel+"="+serviceaccount.ManagedByValue)
	assert.Contains(t, fake.ListedSAs[0], serviceaccount.HostIDLabel+"="+testHostID)

	assert.Equal(t, []string{"ns/sa-dead"}, fake.DeletedSAs,
		"only orphan SAs (dead instance, host-id matching) should be deleted")
	assert.Equal(t, []string{"ns/rb-dead"}, fake.DeletedRoleBindings)
	assert.Equal(t, []string{"crb-dead"}, fake.DeletedClusterRoleBindings)
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
	fake := &kubetest.Fake{}
	withHostKubeClient(t, fake)

	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	require.NoError(t, runHostSweep(t.Context(), []string{
		"--kubeconfig", kubeconfigPath,
		"--host-id", testHostID,
	}))

	require.NotEmpty(t, fake.ListedSAs, "sweep must run even without --context")
}

// TestRunHostSweepEmptyLiveSetSweepsAll pins the destructive
// "manual recovery" semantics: with zero --live-instance-id
// flags every classified resource (instance-id label present
// and non-empty) gets deleted. The README documents the
// caveat.
func TestRunHostSweepEmptyLiveSetSweepsAll(t *testing.T) { //nolint:paralleltest // mutates package-level state
	fake := &kubetest.Fake{
		ListSAResp: []kube.ResourceRef{
			{Namespace: "ns", Name: "sa-a", Labels: map[string]string{serviceaccount.InstanceIDLabel: "a"}},
			{Namespace: "ns", Name: "sa-b", Labels: map[string]string{serviceaccount.InstanceIDLabel: "b"}},
		},
	}
	withHostKubeClient(t, fake)

	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	require.NoError(t, runHostSweep(t.Context(), []string{
		"--kubeconfig", kubeconfigPath,
		"--context", "prod",
		"--host-id", testHostID,
	}))

	sort.Strings(fake.DeletedSAs)
	assert.Equal(t, []string{"ns/sa-a", "ns/sa-b"}, fake.DeletedSAs)
}

// TestRunHostSweepSkipsWhenNoContextResolved pins the no-context
// safety net: a kubeconfig with empty current-context and no
// --context flag must not crash; the sweep logs and returns 0.
func TestRunHostSweepSkipsWhenNoContextResolved(t *testing.T) { //nolint:paralleltest // mutates package-level state
	fake := &kubetest.Fake{}
	withHostKubeClient(t, fake)

	cfg := testKubeconfig()
	cfg.CurrentContext = ""

	kubeconfigPath := writeTestKubeconfig(t, cfg)

	require.NoError(t, runHostSweep(t.Context(), []string{
		"--kubeconfig", kubeconfigPath,
		"--host-id", testHostID,
	}))

	assert.Empty(t, fake.ListedSAs, "no resolved context must skip list calls entirely")
}

// TestRunHostSweepToleratesPartialListForbidden pins that
// Forbidden on one resource kind does not abort the sweep when
// other kinds list successfully -- the partial result is still
// classified and deleted.
//
//nolint:paralleltest // mutates package-level state
func TestRunHostSweepToleratesPartialListForbidden(t *testing.T) {
	fake := &kubetest.Fake{
		ListSAErr: errors.New("forbidden"),
		ListRBErr: errors.New("forbidden"),
		ListCRBResp: []kube.ResourceRef{
			{Name: "crb-dead", Labels: map[string]string{serviceaccount.InstanceIDLabel: "dead"}},
		},
	}
	withHostKubeClient(t, fake)

	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	require.NoError(t, runHostSweep(t.Context(), []string{
		"--kubeconfig", kubeconfigPath,
		"--context", "prod",
		"--host-id", testHostID,
	}))

	assert.Equal(t, []string{"crb-dead"}, fake.DeletedClusterRoleBindings,
		"the one listable kind must still get its orphans deleted")
}

// TestRunHostSweepSurfacesErrListWhenAllFail pins the
// total-failure mode: when every list call fails, runHostSweep
// returns sweep.ErrList so operators see a clean signal rather
// than an empty no-op.
//
//nolint:paralleltest // mutates package-level state
func TestRunHostSweepSurfacesErrListWhenAllFail(t *testing.T) {
	fake := &kubetest.Fake{
		ListSAErr:  errors.New("forbidden"),
		ListRBErr:  errors.New("forbidden"),
		ListCRBErr: errors.New("forbidden"),
	}
	withHostKubeClient(t, fake)

	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	err := runHostSweep(t.Context(), []string{
		"--kubeconfig", kubeconfigPath,
		"--context", "prod",
		"--host-id", testHostID,
	})
	require.ErrorIs(t, err, sweep.ErrList)
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
	fake := &kubetest.Fake{
		ListSAResp: []kube.ResourceRef{
			{Namespace: "ns", Name: "sa-a", Labels: map[string]string{serviceaccount.InstanceIDLabel: ""}},
			{Namespace: "ns", Name: "sa-b", Labels: map[string]string{serviceaccount.InstanceIDLabel: "real-id"}},
		},
	}
	withHostKubeClient(t, fake)

	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	require.NoError(t, runHostSweep(t.Context(), []string{
		"--kubeconfig", kubeconfigPath,
		"--context", "prod",
		"--host-id", testHostID,
		"--live-instance-id", "",
	}))

	// sa-a was preserved by the empty-label rule.
	// sa-b should still be deleted (real-id not in live set).
	assert.Equal(t, []string{"ns/sa-b"}, fake.DeletedSAs)
}
