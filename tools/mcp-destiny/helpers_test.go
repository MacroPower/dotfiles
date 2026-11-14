package main

import (
	"context"
	"log/slog"
	"maps"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/auth"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungietest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitionstest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvmtest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/rolls"
)

// fakes is a handler wired to a fake Agg's Destiny Vault Manager service
// and a fake Bungie platform. The definition loader of the handler serves
// defs, which holds [definitionstest.Defs], until a test calls [fakes.dropDefs]. A
// test edits defs before its first call to change the definitions that the
// handler reads.
type fakes struct {
	h       *handler
	service *dvmtest.Server
	bungie  *bungietest.Server
	defs    *definitions.Defs
}

func newFakes(t *testing.T) *fakes {
	t.Helper()

	f := &fakes{
		service: dvmtest.NewServer(t),
		bungie:  bungietest.NewServer(t),
		defs:    definitionstest.Defs(),
	}

	// The bodies name items of the Bungie fixture profile, which the fake
	// service does not know.
	maps.Copy(f.service.Data, map[string]string{
		"/api/tags": `{"defs": [], "items": [
			{"instanceId": "1001", "itemHash": 100, "tagId": "keep", "removed": false, "updatedAt": 5},
			{"instanceId": "1001", "itemHash": 100, "tagId": "favorite", "removed": false, "updatedAt": 5},
			{"instanceId": "1002", "itemHash": 101, "tagId": "junk", "removed": true, "updatedAt": 5}
		], "now": 10}`,
		"/api/notes": `{"notes": [
			{"kind": "instance", "targetId": "1001", "itemHash": 100, "body": "god roll", "removed": false, "updatedAt": 5},
			{"kind": "set", "targetId": "set-key", "body": "farm this", "removed": false, "updatedAt": 5}
		], "now": 10}`,
		"/api/perk-preferences": `{"preferences": [
			{"mode": "pve", "perk": "Firefly", "stance": "up", "removed": false, "updatedAt": 5},
			{"mode": "pvp", "perk": "Rampage", "stance": "down", "removed": false, "updatedAt": 5},
			{"mode": "pvp", "perk": "Kill Clip", "stance": "up", "removed": true, "updatedAt": 5}
		], "now": 10}`,
		"/api/autotag": `{"rules": [
			{"id": "r2", "query": "is:dupe", "vault": true, "enabled": false, "position": 1, "removed": false, "updatedAt": 5},
			{"id": "r1", "query": "rate:>=a", "tagId": "pve", "vault": false, "enabled": true, "position": 0, "removed": false, "updatedAt": 5},
			{"id": "r0", "query": "old", "vault": false, "enabled": true, "position": 2, "removed": true, "updatedAt": 5}
		], "state": {"enabled": true, "mark": "00000000000000001006", "updatedAt": 5}, "now": 10}`,
	})

	now := func() time.Time { return bungietest.Now }

	store := auth.NewTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	require.NoError(t, store.Save(auth.Tokens{
		AccessToken:    "access-1",
		AccessExpires:  bungietest.Now.Add(time.Hour),
		RefreshToken:   "refresh-1",
		RefreshExpires: bungietest.Now.Add(24 * time.Hour),
		Session:        "session-1",
		BungieID:       "42",
	}))

	client := dvm.New(dvm.WithURL(f.service.URL))

	defs := definitions.NewLoader(
		client,
		"",
		definitions.WithClock(now),
		definitions.WithLoaded(f.defs, bungietest.Now),
	)

	f.h = &handler{
		service: client,
		bungie:  bungie.New(bungie.WithURL(f.bungie.URL), bungie.WithContentURL(f.bungie.URL)),
		auth:    auth.New(store, client, auth.WithClock(now)),
		defs:    defs,
		sheets:  rolls.NewSheetStore(client),
		log:     slog.New(slog.DiscardHandler),
		now:     now,
		zone:    time.UTC,
	}
	f.h.loadoutTables = bungie.NewLoadoutTableStore(bungieManifest{h: f.h})

	return f
}

// dropDefs gives the handler a definition loader that holds no definitions.
// The next read of the definitions therefore builds them from the artifacts
// of the fake service. The read fails when the artifact index of the fake
// service lacks the item artifact.
func (f *fakes) dropDefs() {
	f.h.defs = definitions.NewLoader(f.h.service, "", definitions.WithClock(f.h.now))
}

// text returns the text of a tool result and whether it is an error.
func text(t *testing.T, result *mcp.CallToolResult, err error) (string, bool) {
	t.Helper()

	require.NoError(t, err)
	require.Len(t, result.Content, 1)

	content, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	return content.Text, result.IsError
}

// call runs a tool handler and returns the text of its result and whether
// the result is an error.
func call[In any](
	t *testing.T,
	fn func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, any, error),
	in In,
) (string, bool) {
	t.Helper()

	result, _, err := fn(t.Context(), nil, in)

	return text(t, result, err)
}
