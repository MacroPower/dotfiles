package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungietest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/inventory"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/inventorytest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/rolls"
)

func TestNewestMark(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		add  []*inventory.Item
		want string
	}{
		"the fixture, whose stack has no instance id": {
			want: "00000000000000001006",
		},
		"a longer id is a higher number, whatever its leading digit": {
			add: []*inventory.Item{
				{InstanceID: "6917529999999999999", Equippable: true},
				{InstanceID: "9999999999999999999999", Equippable: false},
			},
			want: "06917529999999999999",
		},
		"an emote with the highest id": {
			add: []*inventory.Item{
				{InstanceID: "1010", Equippable: true, Bucket: bungie.BucketEmotes, Owner: bungietest.HunterID},
				{InstanceID: "1009", Equippable: true, Bucket: bungie.BucketFinishers, Owner: bungietest.HunterID},
				{InstanceID: "1008", Equippable: true, Bucket: bungie.BucketKinetic, Owner: inventory.OwnerVault},
			},
			want: "00000000000000001008",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := inventorytest.Snapshot(t)
			for _, item := range tc.add {
				s.Add(item)
			}

			assert.Equal(t, tc.want, newestMark(s))
		})
	}

	assert.Equal(t, "00000000000000000000", newestMark(&inventory.Snapshot{}))
}

func TestNewItems(t *testing.T) {
	t.Parallel()

	item := func(id, owner string, bucket uint32) *inventory.Item {
		return &inventory.Item{InstanceID: id, Owner: owner, Bucket: bucket, Equippable: true}
	}

	s := &inventory.Snapshot{Items: []*inventory.Item{
		item("10000", bungietest.HunterID, bungie.BucketKinetic),
		item("1008", inventory.OwnerAccount, bungie.BucketKinetic),
		item("1011", inventory.OwnerVault, bungie.BucketPower),
		item("1007", bungietest.TitanID, bungie.BucketHelmet),
		item("1006", bungietest.HunterID, bungie.BucketKinetic),
		item("1009", inventory.OwnerVault, bungie.BucketKinetic),
		item("1005", bungietest.HunterID, bungie.BucketKinetic),
		item("1012", bungietest.HunterID, bungie.BucketEmotes),
		{InstanceID: "1013", Owner: bungietest.HunterID, Bucket: bungie.BucketConsumables},
		{Owner: inventory.OwnerAccount, Bucket: bungie.BucketConsumables},
	}}

	tests := map[string]struct {
		mark      string
		wantHeld  []string
		wantVault []string
	}{
		"the stored mark": {
			mark:      "00000000000000001006",
			wantHeld:  []string{"1007", "1008", "10000"},
			wantVault: []string{"1009", "1011"},
		},
		"a mark of zeros": {
			mark:      "00000000000000000000",
			wantHeld:  []string{"1005", "1006", "1007", "1008", "10000"},
			wantVault: []string{"1009", "1011"},
		},
		"a mark at the highest id": {
			mark: "00000000000000010000",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			held, vault := newItems(s, tc.mark)

			ids := func(items []*inventory.Item) []string {
				var out []string
				for _, item := range items {
					out = append(out, item.InstanceID)
				}

				return out
			}

			assert.Equal(t, tc.wantHeld, ids(held))
			assert.Equal(t, tc.wantVault, ids(vault))
		})
	}
}

func TestEvalRulesSources(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		queries []string
		want    int
	}{
		"no rule": {},
		"no rule reads a game mode": {
			queries: []string{"is:weapon", "is:armor"},
		},
		"a skipped rule reads a game mode": {
			queries: []string{"is:weapon", "count:=<2 rate:>=a"},
		},
		"a blocked rule reads a game mode": {
			queries: []string{"is:weapon", "rate:>=a foo"},
		},
		"rules read both game modes": {
			queries: []string{"is:weapon", "rate:>=a", "pvprate:>=b", "verdict:better"},
			want:    1,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var rules []dvm.AutotagRule
			for _, query := range tc.queries {
				rules = append(rules, dvm.AutotagRule{Query: query, Enabled: true})
			}

			calls := 0
			sources := func() map[string]rolls.RatingSource {
				calls++

				return nil
			}

			evals := evalRules(inventorytest.Snapshot(t), rules, sources)
			require.Len(t, evals, len(rules))
			assert.Equal(t, tc.want, calls)
		})
	}
}

func TestOwnedCounts(t *testing.T) {
	t.Parallel()

	items := []*inventory.Item{{InstanceID: "1"}, {InstanceID: "2"}, {InstanceID: "3"}}

	// test gives item 1 the reason b, item 2 the reasons a and b, and
	// matches item 3.
	test := func(item *inventory.Item) outcome {
		switch item.InstanceID {
		case "1":
			return outcome{reasons: []string{"b"}, state: undecided}
		case "2":
			return outcome{reasons: []string{"a", "b"}, state: undecided}
		default:
			return outcome{state: match}
		}
	}

	got := ownedCounts([]ruleEval{{test: test, rule: dvm.AutotagRule{Enabled: false}}}, items)
	require.Len(t, got, 1)
	assert.Equal(t, 1, got[0].matches)
	assert.Equal(t, 2, got[0].undecided)
	assert.Equal(t, []string{"b", "a"}, got[0].reasons, "each reason once, in the order of the items")
	assert.False(t, got[0].unknown)
}

func TestVaultBlock(t *testing.T) {
	t.Parallel()

	const (
		equipped        = "it is equipped"
		notTransferable = "Bungie reports it as not transferable"
		bucket          = "the web client never moves an item of its bucket"
		server          = "the server treats it as not transferable, but the web client tries the transfer"
		instanceID      = "1"
		hash            = 1
		onCharacter     = 1
	)

	weapon := definitions.ItemDef{
		Name:       "Thorn",
		Bucket:     bungie.BucketKinetic,
		ItemType:   bungie.ItemTypeWeapon,
		ClassType:  bungie.ClassAny,
		Equippable: true,
	}

	// in returns the definition of weapon in the definition bucket b.
	in := func(b uint32) definitions.ItemDef {
		def := weapon
		def.Bucket = b

		return def
	}

	held := func(b uint32, status int) bungie.ProfileItem {
		return bungie.ProfileItem{
			InstanceID: instanceID, Hash: hash, Quantity: 1,
			Location: onCharacter, BucketHash: b, TransferStatus: status,
		}
	}

	lost := func(status int) bungie.ProfileItem {
		return bungie.ProfileItem{
			InstanceID: instanceID, Hash: hash, Quantity: 1,
			Location: bungie.ItemLocationPostmaster, BucketHash: bungie.BucketPostmaster, TransferStatus: status,
		}
	}

	nonTransferable := weapon
	nonTransferable.NonTransferable = true

	nonTransferableEngram := in(bungie.BucketEngrams)
	nonTransferableEngram.NonTransferable = true

	tests := map[string]struct {
		def      definitions.ItemDef
		item     bungie.ProfileItem
		want     string
		equipped bool
	}{
		"an equipped weapon": {
			def:      weapon,
			item:     held(bungie.BucketKinetic, 1),
			equipped: true,
			want:     equipped,
		},
		"an equipped weapon with the status 2": {
			def:      weapon,
			item:     held(bungie.BucketKinetic, 2),
			equipped: true,
			want:     equipped,
		},
		"the status 2": {
			def:  weapon,
			item: held(bungie.BucketKinetic, 2),
			want: notTransferable,
		},
		"the status 2 at the Postmaster": {
			def:  weapon,
			item: lost(2),
			want: notTransferable,
		},
		"an engram with the status 2": {
			def:  in(bungie.BucketEngrams),
			item: held(bungie.BucketEngrams, 2),
			want: notTransferable,
		},
		"the forbidden bucket": {
			def:  in(bungie.BucketForbidden),
			item: held(bungie.BucketForbidden, 0),
			want: bucket,
		},
		"an engram": {
			def:  in(bungie.BucketEngrams),
			item: held(bungie.BucketEngrams, 0),
			want: bucket,
		},
		"a Lost Items definition": {
			def:  in(bungie.BucketPostmaster),
			item: lost(0),
			want: bucket,
		},
		"a message": {
			def:  in(bungie.BucketMessages),
			item: lost(0),
			want: bucket,
		},
		"a special order": {
			def:  in(bungie.BucketSpecialOrders),
			item: lost(0),
			want: bucket,
		},
		"a non-transferable engram": {
			def:  nonTransferableEngram,
			item: held(bungie.BucketEngrams, 0),
			want: bucket,
		},
		"an engram definition in another profile bucket": {
			def:  in(bungie.BucketEngrams),
			item: held(bungie.BucketKinetic, 0),
			want: bucket,
		},
		"a non-transferable definition": {
			def:  nonTransferable,
			item: held(bungie.BucketKinetic, 0),
			want: server,
		},
		"the status 6 outside the Postmaster": {
			def:  weapon,
			item: held(bungie.BucketKinetic, 6),
			want: server,
		},
		"a weapon at the Postmaster": {
			def:  weapon,
			item: lost(0),
		},
		"a weapon on a character": {
			def:  weapon,
			item: held(bungie.BucketKinetic, 0),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			defs := &definitions.Defs{Items: map[uint32]definitions.ItemDef{hash: tc.def}}
			item := inventory.NewItem(&bungie.Profile{}, defs, tc.item, bungietest.HunterID, tc.equipped)

			assert.Equal(t, tc.want, vaultBlock(item, tc.def))
		})
	}
}
