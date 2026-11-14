package inventory

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungietest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitionstest"
)

func fixtureSnapshot(t *testing.T) *Snapshot {
	t.Helper()

	var profile bungie.Profile

	require.NoError(t, json.Unmarshal([]byte(bungietest.ProfileJSON), &profile))

	return NewSnapshot(&profile, definitionstest.Defs(), bungietest.Now)
}
