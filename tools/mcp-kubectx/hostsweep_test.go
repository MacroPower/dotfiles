package main

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/identity"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kube"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kubeconfig"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kubetest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/ledger"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/serviceaccount"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/sweep"
)

// testHostID is the well-formed host id shared by every sweep
// fixture. Centralized so [identity.Valid]'s format and the
// fixtures cannot drift independently.
const testHostID = "0123456789abcdef"

// Well-formed instance ids for the fixtures that write provision
// records. testInstanceID stands for a live `serve`, and the dead
// ids stand for instances that left records behind.
const (
	testInstanceID  = "1111111111111111"
	testDeadID      = "dead000000000001"
	testOtherDeadID = "dead000000000002"
	testOtherHostID = "fedcba9876543210"
)

// testLiveAsOf is the --live-as-of time of the sweep fixtures that
// pass one. testCreated lies an hour before testLiveAsOf. It is the
// creation time of each labeled resource that a fixture expects the
// label pass to delete. It is also the time of each provision
// record that a fixture expects the record pass to release. Both
// lie before the start of every test. So the sweep uses
// testLiveAsOf as the fixture gives it. A fixture without
// --live-as-of gets the start of the sweep, which lies after
// testCreated.
var (
	testLiveAsOf = time.Date(2020, time.January, 2, 12, 30, 0, 0, time.UTC)
	testCreated  = testLiveAsOf.Add(-time.Hour)
)

// TestRunHostSweepEndToEnd exercises the full flag-parse +
// classify + delete path with a fake client. It pins the contract
// that the serve goroutine relies on. The sweep keeps the resources
// tagged with an id of --live-instance-id, such as the id of the
// calling serve. It keeps the resources that the apiserver created
// after the time of --live-as-of. It deletes the rest.
func TestRunHostSweepEndToEnd(t *testing.T) { //nolint:paralleltest // mutates package-level state
	fake := &kubetest.Fake{
		ListSAResp: []kube.ResourceRef{
			{
				Created:   testCreated,
				Namespace: "ns",
				Name:      "sa-live",
				Labels:    map[string]string{serviceaccount.InstanceIDLabel: "live"},
			},
			{
				Created:   testCreated,
				Namespace: "ns",
				Name:      "sa-dead",
				Labels:    map[string]string{serviceaccount.InstanceIDLabel: "dead"},
			},
			{Created: testCreated, Namespace: "ns", Name: "sa-pre", Labels: map[string]string{}},
			{
				Created:   testLiveAsOf.Add(time.Second),
				Namespace: "ns",
				Name:      "sa-late",
				Labels:    map[string]string{serviceaccount.InstanceIDLabel: "late"},
			},
		},
		ListRBResp: []kube.ResourceRef{
			{
				Created:   testCreated,
				Namespace: "ns",
				Name:      "rb-dead",
				Labels:    map[string]string{serviceaccount.InstanceIDLabel: "dead"},
			},
		},
		ListCRBResp: []kube.ResourceRef{
			{Created: testCreated, Name: "crb-dead", Labels: map[string]string{serviceaccount.InstanceIDLabel: "dead"}},
		},
	}
	withHostKubeClient(t, fake)

	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	require.NoError(t, runHostSweep(t.Context(), kubeconfigPath, []string{
		"--context", "prod",
		"--host-id", testHostID,
		"--live-instance-id", "live",
		"--live-as-of", testLiveAsOf.Format(time.RFC3339),
	}))

	require.Len(t, fake.ListedSAs, 1)
	assert.Contains(t, fake.ListedSAs[0], serviceaccount.ManagedByLabel+"="+serviceaccount.ManagedByValue)
	assert.Contains(t, fake.ListedSAs[0], serviceaccount.HostIDLabel+"="+identity.Digest(testHostID))
	assert.NotContains(t, fake.ListedSAs[0], testHostID, "the selector must carry the digest, not the host id")

	assert.Equal(t, []string{"ns/sa-dead"}, fake.DeletedSAs,
		"only orphan SAs (dead instance, host-id matching, created before the live set) should be deleted")
	assert.Equal(t, []string{"ns/rb-dead"}, fake.DeletedRoleBindings)
	assert.Equal(t, []string{"crb-dead"}, fake.DeletedClusterRoleBindings)
}

// TestRunHostSweepRejectsInvalidHostID pins the input-validation
// boundary at the start of [runHostSweep]: empty values fail with
// [ErrMissingHostID], any other malformed value fails with
// [ErrInvalidHostID] before the selector is built. The injection
// case covers a tampered host.key smuggling selector
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

			err := runHostSweep(t.Context(), "/dev/null", []string{
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

	require.NoError(t, runHostSweep(t.Context(), kubeconfigPath, []string{
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
			{
				Created:   testCreated,
				Namespace: "ns",
				Name:      "sa-a",
				Labels:    map[string]string{serviceaccount.InstanceIDLabel: "a"},
			},
			{
				Created:   testCreated,
				Namespace: "ns",
				Name:      "sa-b",
				Labels:    map[string]string{serviceaccount.InstanceIDLabel: "b"},
			},
		},
	}
	withHostKubeClient(t, fake)

	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	require.NoError(t, runHostSweep(t.Context(), kubeconfigPath, []string{
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

	require.NoError(t, runHostSweep(t.Context(), kubeconfigPath, []string{
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
			{Created: testCreated, Name: "crb-dead", Labels: map[string]string{serviceaccount.InstanceIDLabel: "dead"}},
		},
	}
	withHostKubeClient(t, fake)

	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	require.NoError(t, runHostSweep(t.Context(), kubeconfigPath, []string{
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

	err := runHostSweep(t.Context(), kubeconfigPath, []string{
		"--context", "prod",
		"--host-id", testHostID,
	})
	require.ErrorIs(t, err, sweep.ErrList)
}

// TestHandlerRunSweepForwardsArgs pins the argv layout the serve
// goroutine passes through runHost. The argv holds no --context,
// because that flag limits the sweep to one cluster and turns off
// its record pass. The argv holds --host-id, the time of the live
// set as --live-as-of, and one --live-instance-id flag per id. It
// holds no --kubeconfig, because the host side picks the kubeconfig.
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

	// The fraction of a second pins that the flag keeps the whole
	// time, in another zone than UTC.
	liveAsOf := time.Date(2026, time.March, 4, 13, 30, 0, 123456789, time.FixedZone("CET", 3600))

	h.runSweep(t.Context(), map[string]struct{}{"inst-a": {}, "inst-b": {}, "host-1-inst": {}}, liveAsOf)

	assert.Equal(t, subSweep, got.sub)
	assert.NotContains(t, got.args, "--context", "a --context would turn off the record pass of the sweep")
	assert.NotContains(t, got.args, "--kubeconfig", "the host side picks the kubeconfig")

	hostIDIdx := indexOf(got.args, "--host-id")
	require.GreaterOrEqual(t, hostIDIdx, 0)
	assert.Equal(t, "host-1", got.args[hostIDIdx+1])

	asOfIdx := indexOf(got.args, "--live-as-of")
	require.GreaterOrEqual(t, asOfIdx, 0)

	gotAsOf, err := time.Parse(time.RFC3339, got.args[asOfIdx+1])
	require.NoError(t, err)
	assert.True(t, liveAsOf.Equal(gotAsOf), "--live-as-of %q must name %v", got.args[asOfIdx+1], liveAsOf)

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
	h.runSweep(t.Context(), map[string]struct{}{"host-1-inst": {}}, time.Now())
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

			h.runSweep(t.Context(), tc.liveSet, time.Now())

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

	tests := map[string]struct {
		args []string
	}{
		"unknown flag": {
			args: []string{"--bogus-flag"},
		},
		"live-as-of is not an RFC 3339 time": {
			args: []string{"--host-id", testHostID, "--live-as-of", "2026-03-04 12:30:00"},
		},
		"live-as-of is empty": {
			args: []string{"--host-id", testHostID, "--live-as-of", ""},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := runHostSweep(t.Context(), "/dev/null", tc.args)
			require.ErrorIs(t, err, ErrParseHostSweepFlags)
		})
	}
}

// TestRunHostSweepKeepsServiceAccountOfLateServe pins that the label
// pass never deletes the SA of a serve that started after the live
// set. The calling serve takes the live set, and then a later serve
// creates its SA while the list call of the label pass waits on the
// cluster. The later serve is missing from the live set, but the
// apiserver created its SA after --live-as-of. The apiserver keeps
// only whole seconds of the creation time, so the fixture cuts the
// time of the late SA to the second, which can put it before
// --live-as-of.
//
//nolint:paralleltest // mutates package-level state
func TestRunHostSweepKeepsServiceAccountOfLateServe(t *testing.T) {
	prod := &hookClient{Fake: &kubetest.Fake{
		ListSAResp: []kube.ResourceRef{{
			Created:   testCreated,
			Namespace: "ns",
			Name:      "claude-sa-dead",
			Labels:    map[string]string{serviceaccount.InstanceIDLabel: testDeadID},
		}},
	}}

	prod.beforeListSAs = func() {
		prod.Lock()
		defer prod.Unlock()

		prod.ListSAResp = append(prod.ListSAResp, kube.ResourceRef{
			Created:   time.Now().Truncate(time.Second),
			Namespace: "ns",
			Name:      "claude-sa-late",
			Labels:    map[string]string{serviceaccount.InstanceIDLabel: testOtherDeadID},
		})
	}

	withHostKubeClient(t, prod)

	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	liveAsOf := time.Now()

	require.NoError(t, runHostSweep(t.Context(), kubeconfigPath, []string{
		"--context", "prod",
		"--host-id", testHostID,
		"--live-instance-id", testInstanceID,
		"--live-as-of", liveAsOf.UTC().Format(time.RFC3339Nano),
	}))

	assert.Equal(t, []string{"ns/claude-sa-dead"}, prod.DeletedSAs,
		"the SA of the late serve must survive the sweep")
}

// TestRunHostSweepLiveAsOfBoundedByStart pins the time that the
// label pass compares with the creation time of each resource when
// --live-as-of is missing or lies after the start of the sweep. The
// sweep then uses its own start. A dead SA that the apiserver
// created now survives the sweep, and a dead SA from an hour before
// does not.
//
//nolint:paralleltest // mutates package-level state
func TestRunHostSweepLiveAsOfBoundedByStart(t *testing.T) {
	tests := map[string]struct {
		args []string
	}{
		"missing": {},
		"after the start of the sweep": {
			args: []string{"--live-as-of", time.Now().Add(time.Hour).Format(time.RFC3339)},
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests mutate package-level state
		t.Run(name, func(t *testing.T) {
			dead := map[string]string{serviceaccount.InstanceIDLabel: testDeadID}

			fake := &kubetest.Fake{
				ListSAResp: []kube.ResourceRef{
					{Created: time.Now().Add(-time.Hour), Namespace: "ns", Name: "claude-sa-old", Labels: dead},
					{Created: time.Now().Truncate(time.Second), Namespace: "ns", Name: "claude-sa-new", Labels: dead},
				},
			}
			withHostKubeClient(t, fake)

			kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

			args := []string{
				"--context", "prod",
				"--host-id", testHostID,
				"--live-instance-id", testInstanceID,
			}

			require.NoError(t, runHostSweep(t.Context(), kubeconfigPath, append(args, tc.args...)))

			assert.Equal(t, []string{"ns/claude-sa-old"}, fake.DeletedSAs)
		})
	}
}

// TestRunHostSweepIgnoresEmptyLiveID pins that an empty value
// passed via --live-instance-id="" does not get inserted into
// the live set (which would then preserve resources missing the
// instance-id label twice -- once from the missing-label rule,
// once from the empty-id rule).
func TestRunHostSweepIgnoresEmptyLiveID(t *testing.T) { //nolint:paralleltest // mutates package-level state
	fake := &kubetest.Fake{
		ListSAResp: []kube.ResourceRef{
			{
				Created:   testCreated,
				Namespace: "ns",
				Name:      "sa-a",
				Labels:    map[string]string{serviceaccount.InstanceIDLabel: ""},
			},
			{
				Created:   testCreated,
				Namespace: "ns",
				Name:      "sa-b",
				Labels:    map[string]string{serviceaccount.InstanceIDLabel: "real-id"},
			},
		},
	}
	withHostKubeClient(t, fake)

	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	require.NoError(t, runHostSweep(t.Context(), kubeconfigPath, []string{
		"--context", "prod",
		"--host-id", testHostID,
		"--live-instance-id", "",
	}))

	// sa-a was preserved by the empty-label rule.
	// sa-b should still be deleted (real-id not in live set).
	assert.Equal(t, []string{"ns/sa-b"}, fake.DeletedSAs)
}

// sweepRecord returns a provision record of testHostID, under its
// digest, for the SA
// saName in the namespace ns. The record reaches its cluster
// through contextName of the kubeconfig at kubeconfigPath.
// `host select` wrote it at testCreated, before the live set of
// every sweep.
func sweepRecord(instanceID, kubeconfigPath, contextName, saName string) ledger.Record {
	return ledger.Record{
		HostID:         identity.Digest(testHostID),
		InstanceID:     instanceID,
		Kubeconfig:     kubeconfigPath,
		Context:        contextName,
		Namespace:      "ns",
		ServiceAccount: saName,
		Written:        testCreated,
	}
}

// TestRunHostSweepKeepsRecordOfLateServe pins that the record pass
// never releases the SA of a serve that started after the live set.
// The calling serve takes the live set, and then a later serve runs
// `host select` before the sweep reads the ledger. The later serve
// is missing from the live set, but `host select` wrote its record
// after --live-as-of.
//
//nolint:paralleltest // mutates package-level state
func TestRunHostSweepKeepsRecordOfLateServe(t *testing.T) {
	records := withHostLedger(t)
	withHostStdout(t)

	prod := &kubetest.Fake{}
	withHostKubeClient(t, prod)

	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	liveAsOf := time.Now()

	require.NoError(t, selectProd(t, kubeconfigPath, false,
		"--sa-host-id", testHostID,
		"--sa-instance-id", testOtherDeadID,
	))

	late := listRecords(t, records)
	require.Len(t, late, 1)

	require.NoError(t, runHostSweep(t.Context(), kubeconfigPath, []string{
		"--host-id", testHostID,
		"--live-instance-id", testInstanceID,
		"--live-as-of", liveAsOf.UTC().Format(time.RFC3339Nano),
	}))

	assert.Empty(t, prod.DeletedSAs, "the SA of the late serve must survive the sweep")
	assert.Empty(t, prod.DeletedRoleBindings, "the binding of the late serve must survive the sweep")
	assert.Equal(t, late, listRecords(t, records))
}

// TestRunHostSweepRecordPassComparesWriteTime pins the time that the
// record pass compares with the time of each record. It keeps each
// record that `host select` wrote at the time of the live set minus
// [recordSlack] or later. It releases each older record, and each
// record without a time. When --live-as-of is missing or lies after
// the start of the sweep, the sweep compares with its own start.
//
//nolint:paralleltest // mutates package-level state
func TestRunHostSweepRecordPassComparesWriteTime(t *testing.T) {
	now := time.Now().UTC()

	tests := map[string]struct {
		args     []string
		written  map[string]time.Time
		released []string
	}{
		"live set of the caller": {
			args: []string{
				"--live-instance-id", testInstanceID,
				"--live-as-of", testLiveAsOf.Format(time.RFC3339),
			},
			written: map[string]time.Time{
				"claude-sa-old":     testLiveAsOf.Add(-recordSlack - time.Second),
				"claude-sa-cutoff":  testLiveAsOf.Add(-recordSlack),
				"claude-sa-slack":   testLiveAsOf.Add(-recordSlack + time.Second),
				"claude-sa-late":    testLiveAsOf.Add(time.Second),
				"claude-sa-untimed": {},
			},
			released: []string{"ns/claude-sa-old", "ns/claude-sa-untimed"},
		},
		"live-as-of is missing": {
			args: []string{"--live-instance-id", testInstanceID},
			written: map[string]time.Time{
				"claude-sa-old": now.Add(-2 * recordSlack),
				"claude-sa-new": now,
			},
			released: []string{"ns/claude-sa-old"},
		},
		"live-as-of lies after the start of the sweep": {
			args: []string{
				"--live-instance-id", testInstanceID,
				"--live-as-of", now.Add(time.Hour).Format(time.RFC3339),
			},
			written: map[string]time.Time{
				"claude-sa-old": now.Add(-2 * recordSlack),
				"claude-sa-new": now,
			},
			released: []string{"ns/claude-sa-old"},
		},
		"no serve is alive": {
			args: []string{"--live-as-of", testLiveAsOf.Format(time.RFC3339)},
			written: map[string]time.Time{
				"claude-sa-old":  testLiveAsOf.Add(-recordSlack - time.Second),
				"claude-sa-late": testLiveAsOf.Add(time.Second),
			},
			released: []string{"ns/claude-sa-old"},
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests mutate package-level state
		t.Run(name, func(t *testing.T) {
			staging := &kubetest.Fake{}

			withHostKubeClients(t, map[string]kube.Client{"prod": &kubetest.Fake{}, "staging": staging})

			records := withHostLedger(t)
			kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

			var kept []ledger.Record

			for saName, written := range tc.written {
				r := sweepRecord(testDeadID, kubeconfigPath, "staging", saName)
				r.Written = written

				addRecords(t, records, r)

				if !slices.Contains(tc.released, "ns/"+saName) {
					kept = append(kept, r)
				}
			}

			args := []string{
				"--host-id", testHostID,
			}

			require.NoError(t, runHostSweep(t.Context(), kubeconfigPath, append(args, tc.args...)))

			assert.ElementsMatch(t, tc.released, staging.DeletedSAs)
			assert.ElementsMatch(t, kept, listRecords(t, records))
		})
	}
}

// TestRunHostSweepReleasesRecordedServiceAccounts pins the record
// pass. A sweep without --context deletes the SA and the binding of
// each record of a dead instance, on the cluster of that record,
// and then removes the record. The current-context of the swept
// kubeconfig is prod, so the label pass never reaches staging or
// dev. The sweep keeps the records of a live instance, of another
// host id, and of another kubeconfig file. It never builds a client
// from another kubeconfig file.
//
//nolint:paralleltest // mutates package-level state
func TestRunHostSweepReleasesRecordedServiceAccounts(t *testing.T) {
	prod, staging, dev := &kubetest.Fake{}, &kubetest.Fake{}, &kubetest.Fake{}

	clients := withHostKubeClients(t, map[string]kube.Client{"prod": prod, "staging": staging, "dev": dev})
	records := withHostLedger(t)

	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	// A caller whose environment resolves another host kubeconfig
	// writes a record of another file.
	otherKubeconfig := writeTestKubeconfig(t, testKubeconfig())

	deadOnStaging := sweepRecord(testDeadID, kubeconfigPath, "staging", "claude-sa-dead")

	deadOnDev := sweepRecord(testOtherDeadID, kubeconfigPath, "dev", "claude-sa-crb")
	deadOnDev.ClusterScoped = true

	otherFile := sweepRecord(testOtherDeadID, otherKubeconfig, "dev", "claude-sa-other-file")

	liveOnStaging := sweepRecord(testInstanceID, kubeconfigPath, "staging", "claude-sa-live")

	otherHost := sweepRecord(testDeadID, kubeconfigPath, "staging", "claude-sa-foreign")
	otherHost.HostID = identity.Digest(testOtherHostID)

	addRecords(t, records, deadOnStaging, deadOnDev, otherFile, liveOnStaging, otherHost)

	require.NoError(t, runHostSweep(t.Context(), kubeconfigPath, []string{
		"--host-id", testHostID,
		"--live-instance-id", testInstanceID,
	}))

	assert.Equal(t, []string{"ns/claude-sa-dead"}, staging.DeletedSAs)
	assert.Equal(t, []string{"ns/claude-sa-dead-binding"}, staging.DeletedRoleBindings)
	assert.Empty(t, staging.DeletedClusterRoleBindings)

	assert.Equal(t, []string{"ns/claude-sa-crb"}, dev.DeletedSAs)
	assert.Equal(t, []string{"claude-sa-crb-binding"}, dev.DeletedClusterRoleBindings)
	assert.Empty(t, dev.DeletedRoleBindings)

	assert.Empty(t, prod.DeletedSAs, "the label pass found nothing on the current-context cluster")

	assert.ElementsMatch(t, []ledger.Record{otherFile, liveOnStaging, otherHost}, listRecords(t, records))

	assert.ElementsMatch(t, []string{
		kubeClientCall(kubeconfigPath, "prod"),
		kubeClientCall(kubeconfigPath, "staging"),
		kubeClientCall(kubeconfigPath, "dev"),
	}, clients.Calls(), "each record must reach its cluster through the swept kubeconfig and its own context")
}

// TestRunHostSweepNeedsHostID pins that a sweep acts only on the
// resources of the host id that its caller holds. The labels and
// the provision records carry the [identity.Digest] of each host id.
// A Lima guest with a `view` token can read the digest from the
// labels. A sweep with that digest as --host-id selects the digest
// of the digest, so it deletes nothing of the owner of the digest.
//
//nolint:paralleltest // mutates package-level state
func TestRunHostSweepNeedsHostID(t *testing.T) {
	prod, staging := &kubetest.Fake{}, &kubetest.Fake{}

	withHostKubeClients(t, map[string]kube.Client{"prod": prod, "staging": staging})

	records := withHostLedger(t)

	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	dead := sweepRecord(testDeadID, kubeconfigPath, "staging", "claude-sa-dead")
	addRecords(t, records, dead)

	require.NoError(t, runHostSweep(t.Context(), kubeconfigPath, []string{
		"--host-id", identity.Digest(testHostID),
	}))

	require.Len(t, prod.ListedSAs, 1)
	assert.Contains(t, prod.ListedSAs[0], serviceaccount.HostIDLabel+"="+identity.Digest(identity.Digest(testHostID)))

	assert.Empty(t, staging.DeletedSAs, "a sweep with the digest must not release the record of the host id")
	assert.Equal(t, []ledger.Record{dead}, listRecords(t, records))
}

// TestRunHostSweepEmptyLiveSetReleasesEveryRecord pins the manual
// recovery semantics of the record pass. Without a
// --live-instance-id every record of the host id counts as dead.
// The records of another host id stay.
//
//nolint:paralleltest // mutates package-level state
func TestRunHostSweepEmptyLiveSetReleasesEveryRecord(t *testing.T) {
	staging := &kubetest.Fake{}

	withHostKubeClients(t, map[string]kube.Client{"prod": &kubetest.Fake{}, "staging": staging})

	records := withHostLedger(t)
	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	otherHost := sweepRecord(testDeadID, kubeconfigPath, "staging", "claude-sa-foreign")
	otherHost.HostID = identity.Digest(testOtherHostID)

	addRecords(t, records,
		sweepRecord(testInstanceID, kubeconfigPath, "staging", "claude-sa-a"),
		sweepRecord(testDeadID, kubeconfigPath, "staging", "claude-sa-b"),
		otherHost,
	)

	require.NoError(t, runHostSweep(t.Context(), kubeconfigPath, []string{
		"--host-id", testHostID,
	}))

	sort.Strings(staging.DeletedSAs)
	assert.Equal(t, []string{"ns/claude-sa-a", "ns/claude-sa-b"}, staging.DeletedSAs)
	assert.Equal(t, []ledger.Record{otherHost}, listRecords(t, records))
}

// TestRunHostSweepRecordFollowsCluster pins when the record pass
// removes a record. The record goes only after the cluster holds
// neither the SA nor the binding, so that the next sweep tries a
// failed release again. A failure on one cluster does not keep the
// sweep from the record of another cluster.
func TestRunHostSweepRecordFollowsCluster(t *testing.T) { //nolint:paralleltest // mutates package-level state
	tests := map[string]struct {
		staging kube.Client
		stays   bool
	}{
		"already gone": {
			staging: &kubetest.Fake{
				DeleteSAErr:          notFoundErr("serviceaccounts", "claude-sa-dead"),
				DeleteRoleBindingErr: notFoundErr("rolebindings", "claude-sa-dead-binding"),
			},
		},
		"SA delete fails": {
			staging: &kubetest.Fake{DeleteSAErr: errors.New("timeout")},
			stays:   true,
		},
		"binding delete fails": {
			staging: &kubetest.Fake{DeleteRoleBindingErr: errors.New("forbidden")},
			stays:   true,
		},
		"client cannot be built": {
			stays: true,
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests mutate package-level state
		t.Run(name, func(t *testing.T) {
			dev := &kubetest.Fake{}

			clientMap := map[string]kube.Client{"prod": &kubetest.Fake{}, "dev": dev}
			if tc.staging != nil {
				clientMap["staging"] = tc.staging
			}

			withHostKubeClients(t, clientMap)

			records := withHostLedger(t)
			kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

			onStaging := sweepRecord(testDeadID, kubeconfigPath, "staging", "claude-sa-dead")
			onDev := sweepRecord(testDeadID, kubeconfigPath, "dev", "claude-sa-dev")

			addRecords(t, records, onStaging, onDev)

			require.NoError(t, runHostSweep(t.Context(), kubeconfigPath, []string{
				"--host-id", testHostID,
				"--live-instance-id", testInstanceID,
			}))

			assert.Equal(t, []string{"ns/claude-sa-dev"}, dev.DeletedSAs)

			if tc.stays {
				assert.Equal(t, []ledger.Record{onStaging}, listRecords(t, records))
			} else {
				assert.Empty(t, listRecords(t, records))
			}
		})
	}
}

// TestRunHostSweepContextLimitsSweepToOneCluster pins that an
// explicit --context turns off the record pass. The operator named
// one cluster, so the sweep touches no other cluster and keeps the
// records.
func TestRunHostSweepContextLimitsSweepToOneCluster(t *testing.T) { //nolint:paralleltest // mutates package-level state
	staging := &kubetest.Fake{}

	clients := withHostKubeClients(t, map[string]kube.Client{"prod": &kubetest.Fake{}, "staging": staging})
	records := withHostLedger(t)

	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())
	onStaging := sweepRecord(testDeadID, kubeconfigPath, "staging", "claude-sa-dead")

	addRecords(t, records, onStaging)

	require.NoError(t, runHostSweep(t.Context(), kubeconfigPath, []string{
		"--context", "prod",
		"--host-id", testHostID,
		"--live-instance-id", testInstanceID,
	}))

	assert.Empty(t, staging.DeletedSAs)
	assert.Equal(t, []ledger.Record{onStaging}, listRecords(t, records))
	assert.Equal(t, []string{kubeClientCall(kubeconfigPath, "prod")}, clients.Calls())
}

// TestRunHostSweepLabelPassSurvivesUnreadableLedger pins that the
// label pass does not depend on the ledger. When the sweep cannot
// read the provision records, the record pass does nothing. The
// label pass still deletes the orphans of the current-context
// cluster, and the sweep returns nil.
//
//nolint:paralleltest // mutates package-level state
func TestRunHostSweepLabelPassSurvivesUnreadableLedger(t *testing.T) {
	withUnreadableHostLedger(t)

	prod := &kubetest.Fake{
		ListSAResp: []kube.ResourceRef{{
			Created:   testCreated,
			Namespace: "ns",
			Name:      "claude-sa-dead",
			Labels:    map[string]string{serviceaccount.InstanceIDLabel: testDeadID},
		}},
	}

	clients := withHostKubeClients(t, map[string]kube.Client{"prod": prod})

	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	require.NoError(t, runHostSweep(t.Context(), kubeconfigPath, []string{
		"--host-id", testHostID,
		"--live-instance-id", testInstanceID,
	}))

	assert.Equal(t, []string{"ns/claude-sa-dead"}, prod.DeletedSAs)
	assert.Equal(t, []string{kubeClientCall(kubeconfigPath, "prod")}, clients.Calls())
}

// TestRunHostSweepRecordPassSurvivesLabelPass pins that the record
// pass does not depend on the label pass. Each case stops the label
// pass in another way, and the sweep still releases the recorded SA
// and returns the error of the label pass.
func TestRunHostSweepRecordPassSurvivesLabelPass(t *testing.T) { //nolint:paralleltest // mutates package-level state
	missing := filepath.Join(t.TempDir(), "missing-kubeconfig")

	tests := map[string]struct {
		prod       *kubetest.Fake
		kubeconfig func(t *testing.T) string
		err        error
	}{
		"every list call fails": {
			prod: &kubetest.Fake{
				ListSAErr:  errors.New("forbidden"),
				ListRBErr:  errors.New("forbidden"),
				ListCRBErr: errors.New("forbidden"),
			},
			err: sweep.ErrList,
		},
		"kubeconfig of the sweep is missing": {
			kubeconfig: func(*testing.T) string { return missing },
			err:        kubeconfig.ErrLoad,
		},
		"kubeconfig has no current-context": {
			kubeconfig: func(t *testing.T) string {
				t.Helper()

				cfg := testKubeconfig()
				cfg.CurrentContext = ""

				return writeTestKubeconfig(t, cfg)
			},
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests mutate package-level state
		t.Run(name, func(t *testing.T) {
			prod := tc.prod
			if prod == nil {
				prod = &kubetest.Fake{}
			}

			staging := &kubetest.Fake{}

			withHostKubeClients(t, map[string]kube.Client{"prod": prod, "staging": staging})

			records := withHostLedger(t)

			sweptKubeconfig := writeTestKubeconfig(t, testKubeconfig())
			if tc.kubeconfig != nil {
				sweptKubeconfig = tc.kubeconfig(t)
			}

			// The factory of the test does not read the file, so
			// the record pass reaches staging whatever stopped the
			// label pass.
			addRecords(t, records, sweepRecord(testDeadID, sweptKubeconfig, "staging", "claude-sa-dead"))

			err := runHostSweep(t.Context(), sweptKubeconfig, []string{
				"--host-id", testHostID,
				"--live-instance-id", testInstanceID,
			})
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, []string{"ns/claude-sa-dead"}, staging.DeletedSAs)
			assert.Empty(t, listRecords(t, records))
		})
	}
}

// TestRunHostSweepIgnoresRecordsAddedDuringLabelPass pins that the
// record pass works on the records that existed before the first
// cluster call. The live set of the sweep is a snapshot. A serve
// that starts after the snapshot is not in it, so its record looks
// like the record of a dead instance. Such a serve adds its record
// only after the sweep has started.
//
//nolint:paralleltest // mutates package-level state
func TestRunHostSweepIgnoresRecordsAddedDuringLabelPass(t *testing.T) {
	records := withHostLedger(t)
	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	late := sweepRecord(testOtherDeadID, kubeconfigPath, "staging", "claude-sa-late")

	prod := &hookClient{Fake: &kubetest.Fake{}}
	prod.beforeListSAs = func() { addRecords(t, records, late) }

	staging := &kubetest.Fake{}

	withHostKubeClients(t, map[string]kube.Client{"prod": prod, "staging": staging})

	require.NoError(t, runHostSweep(t.Context(), kubeconfigPath, []string{
		"--host-id", testHostID,
		"--live-instance-id", testInstanceID,
	}))

	assert.Empty(t, staging.DeletedSAs, "an SA recorded after the sweep started must survive the sweep")
	assert.Equal(t, []ledger.Record{late}, listRecords(t, records))
}

// TestRunHostSweepRecordPassDoesNotWaitForLabelPass pins that the
// two passes run at the same time. The label pass of the test
// waits in its list call until the record pass has started a
// delete. A sweep that ends the label pass first leaves that list
// call waiting until its timer ends the wait.
//
//nolint:paralleltest // mutates package-level state
func TestRunHostSweepRecordPassDoesNotWaitForLabelPass(t *testing.T) {
	records := withHostLedger(t)
	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	released := make(chan struct{})

	staging := &hookClient{Fake: &kubetest.Fake{}}
	staging.beforeDeleteSA = sync.OnceFunc(func() { close(released) })

	sawRelease := false

	prod := &hookClient{Fake: &kubetest.Fake{}}
	prod.beforeListSAs = func() {
		select {
		case <-released:
			sawRelease = true
		case <-time.After(10 * time.Second):
		}
	}

	withHostKubeClients(t, map[string]kube.Client{"prod": prod, "staging": staging})

	addRecords(t, records, sweepRecord(testDeadID, kubeconfigPath, "staging", "claude-sa-dead"))

	require.NoError(t, runHostSweep(t.Context(), kubeconfigPath, []string{
		"--host-id", testHostID,
		"--live-instance-id", testInstanceID,
	}))

	assert.True(t, sawRelease, "the record pass must start while the label pass still waits")
	assert.Empty(t, listRecords(t, records))
}

// stalledClient stands for a cluster that accepts a request and
// never answers. Its SA delete returns only when the context of the
// call ends.
type stalledClient struct {
	*kubetest.Fake
}

func (c *stalledClient) DeleteServiceAccount(ctx context.Context, namespace, name string) error {
	_ = c.Fake.DeleteServiceAccount(ctx, namespace, name) //nolint:errcheck // records the attempt only

	<-ctx.Done()

	return ctx.Err() //nolint:wrapcheck // the test double returns the bare context error
}

// TestRunHostSweepRecordPassTimeout pins the cap on the time that
// the record pass spends on one cluster. A cluster that never
// answers ends the pass for its records after [recordPassTimeout].
// The pass does not start the remaining records of that cluster,
// and every record of the cluster stays for the next sweep.
func TestRunHostSweepRecordPassTimeout(t *testing.T) { //nolint:paralleltest // mutates package-level state
	prevTimeout := recordPassTimeout
	recordPassTimeout = 200 * time.Millisecond

	t.Cleanup(func() { recordPassTimeout = prevTimeout })

	staging := &stalledClient{Fake: &kubetest.Fake{}}

	withHostKubeClients(t, map[string]kube.Client{"prod": &kubetest.Fake{}, "staging": staging})

	records := withHostLedger(t)
	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	first := sweepRecord(testDeadID, kubeconfigPath, "staging", "claude-sa-first")
	second := sweepRecord(testDeadID, kubeconfigPath, "staging", "claude-sa-second")

	addRecords(t, records, first, second)

	// The deadline of the test context is far past the cap, so a
	// sweep that returns before it was ended by the cap.
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	require.NoError(t, runHostSweep(ctx, kubeconfigPath, []string{
		"--host-id", testHostID,
		"--live-instance-id", testInstanceID,
	}))

	require.NoError(t, ctx.Err(), "the cap must end the pass, not the deadline of the caller")

	// On a slow machine the cap can pass before the first record
	// starts, so the pass starts one record or none.
	assert.LessOrEqual(t, len(staging.DeletedSAs), 1, "the pass must not start a record after the cap")
	assert.ElementsMatch(t, []ledger.Record{first, second}, listRecords(t, records))
}

// meetingClient delays its SA delete until its peer has started an
// SA delete too, or until the context of the call ends. Two such
// clients finish only when their clusters run at the same time.
type meetingClient struct {
	*kubetest.Fake

	started chan struct{}
	peer    <-chan struct{}
	once    sync.Once
}

func (c *meetingClient) DeleteServiceAccount(ctx context.Context, namespace, name string) error {
	c.once.Do(func() { close(c.started) })

	select {
	case <-c.peer:
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // the test double returns the bare context error
	}

	//nolint:wrapcheck // the test double passes the error of the fake through
	return c.Fake.DeleteServiceAccount(ctx, namespace, name)
}

// TestRunHostSweepRecordPassRunsClustersInParallel pins that a
// cluster that does not answer delays no other cluster. Each
// client waits for the other one to start, so both releases succeed
// only when the record pass works on both clusters at the same
// time. A pass that takes one cluster after the other leaves the
// record of the first cluster behind after the cap.
//
//nolint:paralleltest // mutates package-level state
func TestRunHostSweepRecordPassRunsClustersInParallel(t *testing.T) {
	prevTimeout := recordPassTimeout
	recordPassTimeout = 10 * time.Second

	t.Cleanup(func() { recordPassTimeout = prevTimeout })

	stagingStarted, devStarted := make(chan struct{}), make(chan struct{})

	staging := &meetingClient{Fake: &kubetest.Fake{}, started: stagingStarted, peer: devStarted}
	dev := &meetingClient{Fake: &kubetest.Fake{}, started: devStarted, peer: stagingStarted}

	withHostKubeClients(t, map[string]kube.Client{"prod": &kubetest.Fake{}, "staging": staging, "dev": dev})

	records := withHostLedger(t)
	kubeconfigPath := writeTestKubeconfig(t, testKubeconfig())

	addRecords(t, records,
		sweepRecord(testDeadID, kubeconfigPath, "staging", "claude-sa-staging"),
		sweepRecord(testDeadID, kubeconfigPath, "dev", "claude-sa-dev"),
	)

	require.NoError(t, runHostSweep(t.Context(), kubeconfigPath, []string{
		"--host-id", testHostID,
		"--live-instance-id", testInstanceID,
	}))

	assert.Empty(t, listRecords(t, records))
}
