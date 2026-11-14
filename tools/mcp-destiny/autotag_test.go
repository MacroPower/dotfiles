package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewestMark(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		add  []*Item
		want string
	}{
		"the fixture, whose stack has no instance id": {
			want: "00000000000000001006",
		},
		"a longer id is a higher number, whatever its leading digit": {
			add: []*Item{
				{InstanceID: "6917529999999999999", Equippable: true},
				{InstanceID: "9999999999999999999999", Equippable: false},
			},
			want: "06917529999999999999",
		},
		"an emote with the highest id": {
			add: []*Item{
				{InstanceID: "1010", Equippable: true, Bucket: bucketEmotes, Owner: hunterID},
				{InstanceID: "1009", Equippable: true, Bucket: bucketFinishers, Owner: hunterID},
				{InstanceID: "1008", Equippable: true, Bucket: bucketKinetic, Owner: ownerVault},
			},
			want: "00000000000000001008",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := fixtureSnapshot(t)
			for _, item := range tc.add {
				s.add(item)
			}

			assert.Equal(t, tc.want, newestMark(s))
		})
	}

	assert.Equal(t, "00000000000000000000", newestMark(&Snapshot{}))
}

func TestNewItems(t *testing.T) {
	t.Parallel()

	item := func(id, owner string, bucket uint32) *Item {
		return &Item{InstanceID: id, Owner: owner, Bucket: bucket, Equippable: true}
	}

	s := &Snapshot{Items: []*Item{
		item("10000", hunterID, bucketKinetic),
		item("1008", ownerAccount, bucketKinetic),
		item("1011", ownerVault, bucketPower),
		item("1007", titanID, bucketHelmet),
		item("1006", hunterID, bucketKinetic),
		item("1009", ownerVault, bucketKinetic),
		item("1005", hunterID, bucketKinetic),
		item("1012", hunterID, bucketEmotes),
		{InstanceID: "1013", Owner: hunterID, Bucket: bucketConsumables},
		{Owner: ownerAccount, Bucket: bucketConsumables},
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

			ids := func(items []*Item) []string {
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

			var rules []AutotagRule
			for _, query := range tc.queries {
				rules = append(rules, AutotagRule{Query: query, Enabled: true})
			}

			calls := 0
			sources := func() map[string]RatingSource {
				calls++

				return nil
			}

			evals := evalRules(fixtureSnapshot(t), rules, sources)
			require.Len(t, evals, len(rules))
			assert.Equal(t, tc.want, calls)
		})
	}
}

func TestOwnedCounts(t *testing.T) {
	t.Parallel()

	items := []*Item{{InstanceID: "1"}, {InstanceID: "2"}, {InstanceID: "3"}}

	// test gives item 1 the reason b, item 2 the reasons a and b, and
	// matches item 3.
	test := func(item *Item) outcome {
		switch item.InstanceID {
		case "1":
			return outcome{reasons: []string{"b"}, state: undecided}
		case "2":
			return outcome{reasons: []string{"a", "b"}, state: undecided}
		default:
			return outcome{state: match}
		}
	}

	got := ownedCounts([]ruleEval{{test: test, rule: AutotagRule{Enabled: false}}}, items)
	require.Len(t, got, 1)
	assert.Equal(t, 1, got[0].matches)
	assert.Equal(t, 2, got[0].undecided)
	assert.Equal(t, []string{"b", "a"}, got[0].reasons, "each reason once, in the order of the items")
	assert.False(t, got[0].unknown)
}

func TestVaultBlock(t *testing.T) {
	t.Parallel()

	const (
		equipped    = "it is equipped"
		bungie      = "Bungie reports it as not transferable"
		bucket      = "the web client never moves an item of its bucket"
		server      = "the server treats it as not transferable, but the web client tries the transfer"
		instanceID  = "1"
		hash        = 1
		onCharacter = 1
	)

	weapon := ItemDef{
		Name:       "Thorn",
		Bucket:     bucketKinetic,
		ItemType:   itemTypeWeapon,
		ClassType:  classAny,
		Equippable: true,
	}

	// in returns the definition of weapon in the definition bucket b.
	in := func(b uint32) ItemDef {
		def := weapon
		def.Bucket = b

		return def
	}

	held := func(b uint32, status int) ProfileItem {
		return ProfileItem{
			InstanceID: instanceID, Hash: hash, Quantity: 1,
			Location: onCharacter, BucketHash: b, TransferStatus: status,
		}
	}

	lost := func(status int) ProfileItem {
		return ProfileItem{
			InstanceID: instanceID, Hash: hash, Quantity: 1,
			Location: itemLocationPostmaster, BucketHash: bucketPostmaster, TransferStatus: status,
		}
	}

	nonTransferable := weapon
	nonTransferable.NonTransferable = true

	nonTransferableEngram := in(bucketEngrams)
	nonTransferableEngram.NonTransferable = true

	tests := map[string]struct {
		def      ItemDef
		item     ProfileItem
		want     string
		equipped bool
	}{
		"an equipped weapon": {
			def:      weapon,
			item:     held(bucketKinetic, 1),
			equipped: true,
			want:     equipped,
		},
		"an equipped weapon with the status 2": {
			def:      weapon,
			item:     held(bucketKinetic, 2),
			equipped: true,
			want:     equipped,
		},
		"the status 2": {
			def:  weapon,
			item: held(bucketKinetic, 2),
			want: bungie,
		},
		"the status 2 at the Postmaster": {
			def:  weapon,
			item: lost(2),
			want: bungie,
		},
		"an engram with the status 2": {
			def:  in(bucketEngrams),
			item: held(bucketEngrams, 2),
			want: bungie,
		},
		"the forbidden bucket": {
			def:  in(bucketForbidden),
			item: held(bucketForbidden, 0),
			want: bucket,
		},
		"an engram": {
			def:  in(bucketEngrams),
			item: held(bucketEngrams, 0),
			want: bucket,
		},
		"a Lost Items definition": {
			def:  in(bucketPostmaster),
			item: lost(0),
			want: bucket,
		},
		"a message": {
			def:  in(bucketMessages),
			item: lost(0),
			want: bucket,
		},
		"a special order": {
			def:  in(bucketSpecialOrders),
			item: lost(0),
			want: bucket,
		},
		"a non-transferable engram": {
			def:  nonTransferableEngram,
			item: held(bucketEngrams, 0),
			want: bucket,
		},
		"an engram definition in another profile bucket": {
			def:  in(bucketEngrams),
			item: held(bucketKinetic, 0),
			want: bucket,
		},
		"a non-transferable definition": {
			def:  nonTransferable,
			item: held(bucketKinetic, 0),
			want: server,
		},
		"the status 6 outside the Postmaster": {
			def:  weapon,
			item: held(bucketKinetic, 6),
			want: server,
		},
		"a weapon at the Postmaster": {
			def:  weapon,
			item: lost(0),
		},
		"a weapon on a character": {
			def:  weapon,
			item: held(bucketKinetic, 0),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			defs := &Defs{Items: map[uint32]ItemDef{hash: tc.def}}
			item := newItem(&Profile{}, defs, tc.item, hunterID, tc.equipped)

			assert.Equal(t, tc.want, vaultBlock(item, tc.def))
		})
	}
}
