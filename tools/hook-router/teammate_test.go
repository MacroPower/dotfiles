package main

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/hook"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/state"
)

// seedPlan gives a session the plan_path that arms the post-impl gate,
// standing in for the ExitPlanMode approval that writes it in
// production.
func seedPlan(t *testing.T, store *state.Store, session string) {
	t.Helper()

	require.NoError(t, store.SetPlanPath(t.Context(), session, "/plans/p.md", "abc123"))
}

func TestHandleTeammateIdle_AllowsThrough(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		input []byte
	}{
		"empty session": {
			input: makeHookJSON(t, hook.Input{TeammateName: "researcher"}),
		},
		"no plan path": {
			input: makeHookJSON(t, hook.Input{SessionID: "s1", TeammateName: "researcher"}),
		},
		"malformed JSON": {
			input: []byte(`{"session_id":`),
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := newTestStore(t)
			logger := slog.New(slog.DiscardHandler)

			require.NoError(t, handleTeammateIdle(t.Context(), tc.input, store, cfg, logger))
		})
	}
}

func TestHandleTeammateIdle_BlocksOncePerTeammate(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)

	seedPlan(t, store, "s1")

	input := makeHookJSON(t, hook.Input{SessionID: "s1", TeammateName: "researcher"})

	err := handleTeammateIdle(t.Context(), input, store, cfg, logger)

	var blocked *blockError

	require.ErrorAs(t, err, &blocked)
	assert.Contains(t, blocked.Reason, "/review-implementation")
	assert.Contains(t, blocked.Reason, "/plans/p.md")

	// The second attempt is the bound: TeammateIdle has no
	// stop_hook_active, so an unbounded gate would loop the teammate.
	require.NoError(t, handleTeammateIdle(t.Context(), input, store, cfg, logger))
}

func TestHandleTeammateIdle_GatesEachTeammateSeparately(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)

	seedPlan(t, store, "s1")

	var blocked *blockError

	first := makeHookJSON(t, hook.Input{SessionID: "s1", TeammateName: "researcher"})
	require.ErrorAs(t, handleTeammateIdle(t.Context(), first, store, cfg, logger), &blocked)

	// A second teammate in the same session gets its own block; keying
	// the mark on session_id alone would wave this one through.
	second := makeHookJSON(t, hook.Input{SessionID: "s1", TeammateName: "reviewer"})
	require.ErrorAs(t, handleTeammateIdle(t.Context(), second, store, cfg, logger), &blocked)
}

func TestHandleTeammateIdle_ClearSessionRearmsGate(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)

	seedPlan(t, store, "s1")

	input := makeHookJSON(t, hook.Input{SessionID: "s1", TeammateName: "researcher"})

	var blocked *blockError

	require.ErrorAs(t, handleTeammateIdle(t.Context(), input, store, cfg, logger), &blocked)
	require.NoError(t, store.ClearSession(t.Context(), "s1"))

	// The next plan cycle re-arms the gate for the same teammate.
	seedPlan(t, store, "s1")
	require.ErrorAs(t, handleTeammateIdle(t.Context(), input, store, cfg, logger), &blocked)
}

func TestHandleTeammateIdle_StoreErrorAllowsThrough(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)

	seedPlan(t, store, "s1")
	require.NoError(t, store.Close())

	input := makeHookJSON(t, hook.Input{SessionID: "s1", TeammateName: "researcher"})

	// Fail-open, unlike handleStop: a teammate has no stop_hook_active
	// escape hatch, so a store outage must not wedge it.
	require.NoError(t, handleTeammateIdle(t.Context(), input, store, cfg, logger))
}
