package dvmtest

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// Chunk is one chunk of an /api/stats page that a test serves.
type Chunk struct {
	// Changes holds the JSON objects of the changes of the chunk.
	Changes []map[string]any
	// StartsAt is the start of the 5-minute bucket of the chunk, in Unix
	// milliseconds. The page ends the bucket 5 minutes later.
	StartsAt int64
	// UpdatedAt is the service time of the write that stored the chunk,
	// in Unix milliseconds.
	UpdatedAt int64
}

// StatPage encodes an /api/stats page. Each chunk body is a JSON
// string that holds the JSON array of its changes.
func StatPage(t *testing.T, more bool, chunks ...Chunk) string {
	t.Helper()

	out := []map[string]any{}

	for _, chunk := range chunks {
		body, err := json.Marshal(chunk.Changes)
		require.NoError(t, err)

		out = append(out, map[string]any{
			"startsAt":  chunk.StartsAt,
			"endsAt":    chunk.StartsAt + 300_000,
			"body":      string(body),
			"updatedAt": chunk.UpdatedAt,
		})
	}

	raw, err := json.Marshal(map[string]any{"chunks": out, "more": more})
	require.NoError(t, err)

	return string(raw)
}
