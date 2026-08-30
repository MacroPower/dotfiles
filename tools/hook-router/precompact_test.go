package main

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/hook"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/state"
)

// compactMarker reads the session's compaction stamp back through the
// [*state.Store.DB] escape hatch; nothing reads it in production yet.
// The second return value reports whether a marker was written at all.
func compactMarker(t *testing.T, store *state.Store, session string) (string, bool) {
	t.Helper()

	var trigger, at string

	err := store.DB().QueryRowContext(t.Context(),
		`SELECT last_compact_trigger, last_compact_at FROM sessions
		 WHERE session_id = ?`, session).Scan(&trigger, &at)
	if err != nil {
		return "", false
	}

	return trigger, at != ""
}

func TestHandlePreCompact(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		input   []byte
		want    string
		stamped bool
	}{
		"manual trigger lands on the row": {
			input:   makeHookJSON(t, hook.Input{SessionID: "s1", Trigger: "manual"}),
			want:    "manual",
			stamped: true,
		},
		"auto trigger lands on the row": {
			input:   makeHookJSON(t, hook.Input{SessionID: "s1", Trigger: "auto"}),
			want:    "auto",
			stamped: true,
		},
		"empty session records nothing": {
			input: makeHookJSON(t, hook.Input{Trigger: "manual"}),
		},
		"malformed JSON records nothing": {
			input: []byte(`{"session_id":`),
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := newTestStore(t)
			logger := slog.New(slog.DiscardHandler)

			require.NoError(t, handlePreCompact(t.Context(), tc.input, store, logger))

			trigger, stamped := compactMarker(t, store, "s1")
			assert.Equal(t, tc.want, trigger)
			assert.Equal(t, tc.stamped, stamped)
		})
	}
}
