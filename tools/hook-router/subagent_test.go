package main

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/hook"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/state"
)

// subagentRows reads the recorded spawns for a session back through the
// [*state.Store.DB] escape hatch. Nothing in production reads
// subagent_starts yet, so there is no typed query method to use here.
func subagentRows(t *testing.T, store *state.Store, session string) [][2]string {
	t.Helper()

	rows, err := store.DB().QueryContext(t.Context(),
		`SELECT agent_type, agent_id FROM subagent_starts
		 WHERE session_id = ? ORDER BY id`, session)
	require.NoError(t, err)

	defer rows.Close()

	var got [][2]string

	for rows.Next() {
		var agentType, agentID string

		require.NoError(t, rows.Scan(&agentType, &agentID))

		got = append(got, [2]string{agentType, agentID})
	}

	require.NoError(t, rows.Err())

	return got
}

func TestHandleSubagentStart(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		input []byte
		want  [][2]string
	}{
		"spawn round-trips": {
			input: makeHookJSON(t, hook.Input{
				SessionID: "s1",
				AgentType: "Explore",
				AgentID:   "agent-abc123",
			}),
			want: [][2]string{{"Explore", "agent-abc123"}},
		},
		"missing agent fields record as empty": {
			input: makeHookJSON(t, hook.Input{SessionID: "s1"}),
			want:  [][2]string{{"", ""}},
		},
		"empty session records nothing": {
			input: makeHookJSON(t, hook.Input{AgentType: "Explore"}),
			want:  nil,
		},
		"malformed JSON records nothing": {
			input: []byte(`{"session_id":`),
			want:  nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := newTestStore(t)
			logger := slog.New(slog.DiscardHandler)

			require.NoError(t, handleSubagentStart(t.Context(), tc.input, store, logger))
			assert.Equal(t, tc.want, subagentRows(t, store, "s1"))
		})
	}
}

func TestHandleSubagentStart_RecordsEverySpawn(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)

	for _, agent := range []string{"Explore", "Plan", "Explore"} {
		input := makeHookJSON(t, hook.Input{SessionID: "s1", AgentType: agent, AgentID: "a-" + agent})
		require.NoError(t, handleSubagentStart(t.Context(), input, store, logger))
	}

	assert.Equal(t, [][2]string{
		{"Explore", "a-Explore"},
		{"Plan", "a-Plan"},
		{"Explore", "a-Explore"},
	}, subagentRows(t, store, "s1"))
}
