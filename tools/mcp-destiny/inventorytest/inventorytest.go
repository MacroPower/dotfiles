package inventorytest

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungietest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitionstest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/inventory"
)

// typeTrait is the plug type of the weapon traits of the fixture
// definitions.
const typeTrait = "Trait"

// Plugs of the fixture definitions of package definitionstest.
var (
	// PlugExplosivePayload is the trait Explosive Payload.
	PlugExplosivePayload = inventory.Plug{Name: "Explosive Payload", Type: typeTrait, Hash: 900}
	// PlugRampage is the trait Rampage.
	PlugRampage = inventory.Plug{Name: "Rampage", Type: typeTrait, Hash: 903}
	// PlugKillClip is the trait Kill Clip.
	PlugKillClip = inventory.Plug{Name: "Kill Clip", Type: typeTrait, Hash: 904}
	// PlugKillClipEnhanced is the enhanced trait of Kill Clip.
	PlugKillClipEnhanced = inventory.Plug{Name: "Kill Clip", Type: "Enhanced Trait", Hash: 905}
	// PlugFirefly is the trait Firefly. It leaves out the description that
	// the fixture definitions give the trait.
	PlugFirefly = inventory.Plug{Name: "Firefly", Type: typeTrait, Hash: 901}
)

// RolledPerks returns the perks of a copy that holds Explosive Payload
// with Rampage beside it in one socket, and Firefly alone in another.
func RolledPerks() []inventory.Perk {
	return []inventory.Perk{
		{
			Plug:  PlugExplosivePayload,
			Plugs: []inventory.Plug{PlugExplosivePayload, PlugRampage},
		},
		{Plug: PlugFirefly, Plugs: []inventory.Plug{PlugFirefly}, Socket: 1},
	}
}

// Snapshot returns the inventory of the Bungie fixture profile at
// [bungietest.Now], resolved against the definitions of package
// definitionstest. It builds a new value on each call, so a test can
// change the result without changing the snapshot of another test.
func Snapshot(t *testing.T) *inventory.Snapshot {
	t.Helper()

	var profile bungie.Profile

	require.NoError(t, json.Unmarshal([]byte(bungietest.ProfileJSON), &profile))

	return inventory.NewSnapshot(&profile, definitionstest.Defs(), bungietest.Now)
}

// IDs returns the instance id of each item, in order.
func IDs(items []*inventory.Item) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.InstanceID)
	}

	return out
}
