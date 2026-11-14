package inventory

import (
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungietest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
)

func TestResolveSlot(t *testing.T) {
	t.Parallel()

	const candidates = "Kinetic Weapons, Power Weapons, Helmet"

	// withEngram adds an item that is neither a weapon nor an armor piece
	// to a bucket that each character has.
	withEngram := func(s *Snapshot) {
		s.Add(
			&Item{
				InstanceID: "1",
				Name:       "Legendary Engram",
				Kind:       KindOther,
				Owner:      bungietest.HunterID,
				Bucket:     bungie.BucketEngrams,
			},
		)
	}

	withoutGear := func(s *Snapshot) {
		s.Items = slices.DeleteFunc(s.Items, func(item *Item) bool { return item.Kind != KindOther })
		withEngram(s)
	}

	tests := map[string]struct {
		edit func(*Snapshot)
		name string
		want string
		err  string
	}{
		"full name in another letter case": {name: "kinetic weapons", want: "Kinetic Weapons"},
		"substring":                        {name: "helm", want: "Helmet"},
		"substring in another letter case": {name: "HELM", want: "Helmet"},
		"spaces around the name":           {name: " helmet ", want: "Helmet"},
		"full name wins over a longer name": {
			edit: func(s *Snapshot) {
				s.Buckets = []Bucket{
					{Name: "Power Weapons", Hash: bungie.BucketPower, Capacity: 10},
					{Name: "Power", Hash: bungie.BucketHelmet, Capacity: 10},
				}
				s.Items = []*Item{
					{InstanceID: "1", Kind: KindWeapon, Bucket: bungie.BucketPower},
					{InstanceID: "2", Kind: KindArmor, Bucket: bungie.BucketHelmet},
				}
			},
			name: "power",
			want: "Power",
		},
		"several matches": {
			name: "weapons",
			err:  `invalid input: slot "weapons" matches Kinetic Weapons, Power Weapons, so pass more of the name`,
		},
		"no match": {
			name: "boots",
			err: `invalid input: slot "boots" matches no weapon or armor slot, ` +
				"so pass one of " + candidates,
		},
		"empty": {
			err: "invalid input: pass a slot, one of " + candidates,
		},
		"spaces only": {
			name: "   ",
			err:  "invalid input: pass a slot, one of " + candidates,
		},
		"bucket without weapons or armor": {
			edit: withEngram,
			name: "engrams",
			err: `invalid input: slot "engrams" matches no weapon or armor slot, ` +
				"so pass one of " + candidates,
		},
		"account bucket": {
			edit: func(s *Snapshot) {
				s.Add(&Item{InstanceID: "1", Kind: KindWeapon, Bucket: bungie.BucketConsumables})
			},
			name: "consumables",
			err: `invalid input: slot "consumables" matches no weapon or armor slot, ` +
				"so pass one of " + candidates,
		},
		"vault": {
			edit: func(s *Snapshot) {
				s.Add(&Item{InstanceID: "1", Kind: KindWeapon, Bucket: bungie.BucketVault})
				s.Add(&Item{InstanceID: "2", Kind: KindWeapon, Bucket: bungie.BucketPostmaster})
			},
			name: "vault",
			err: `invalid input: slot "vault" matches no weapon or armor slot, ` +
				"so pass one of " + candidates,
		},
		"Postmaster": {
			edit: func(s *Snapshot) {
				s.Add(&Item{InstanceID: "1", Kind: KindWeapon, Bucket: bungie.BucketVault})
				s.Add(&Item{InstanceID: "2", Kind: KindWeapon, Bucket: bungie.BucketPostmaster})
			},
			name: "postmaster",
			err: `invalid input: slot "postmaster" matches no weapon or armor slot, ` +
				"so pass one of " + candidates,
		},
		"no candidates with an empty name": {
			edit: withoutGear,
			err:  "invalid input: no slot holds a weapon or an armor piece",
		},
		"no candidates with a name": {
			edit: withoutGear,
			name: "kinetic",
			err:  "invalid input: no slot holds a weapon or an armor piece",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := fixtureSnapshot(t)
			if tc.edit != nil {
				tc.edit(s)
			}

			got, err := s.ResolveSlot(tc.name)
			if tc.err != "" {
				require.ErrorIs(t, err, ErrInvalidInput)
				require.EqualError(t, err, tc.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got.Name)
		})
	}
}

// junkIDs is a [JunkPlan] without its Room, with the instance id of each
// item in place of the item.
type junkIDs struct {
	Staged  []string
	Waiting []string
	Keepers []string
	Skipped int
}

func junkIDsOf(plan JunkPlan) junkIDs {
	idsOf := func(items []*Item) []string {
		if len(items) == 0 {
			return nil
		}

		return ids(items)
	}

	return junkIDs{
		Staged:  idsOf(plan.Staged),
		Waiting: idsOf(plan.Waiting),
		Keepers: idsOf(plan.Keepers),
		Skipped: plan.Skipped,
	}
}

// junkSnapshot returns a snapshot of the fixture characters that holds
// items and no other item. Each item without a bucket takes the kinetic
// slot, and each item without a name takes the name Fatebringer.
func junkSnapshot(items []Item) *Snapshot {
	s := &Snapshot{
		byID: map[string]*Item{},
		Characters: []Character{
			{ID: bungietest.HunterID, Class: "Hunter", ClassType: bungie.ClassHunter},
			{ID: bungietest.TitanID, Class: "Titan", ClassType: bungie.ClassTitan},
		},
		Buckets: []Bucket{
			{Name: "Kinetic Weapons", Hash: bungie.BucketKinetic, Capacity: 10},
			{Name: "Power Weapons", Hash: bungie.BucketPower, Capacity: 10},
		},
	}

	items = slices.Clone(items)

	for i := range items {
		item := &items[i]
		if item.Bucket == 0 {
			item.Bucket = bungie.BucketKinetic
		}

		if item.Name == "" {
			item.Name = "Fatebringer"
		}

		item.Kind = KindWeapon
		s.Add(item)
	}

	return s
}

func TestJunkPlan(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		items []Item
		want  junkIDs
	}{
		"junk in the slot": {
			items: []Item{
				{
					InstanceID:   "1",
					Owner:        bungietest.HunterID,
					Tag:          dvm.TagJunk,
					ClassType:    bungie.ClassAny,
					Transferable: true,
				},
			},
			want: junkIDs{Staged: []string{"1"}},
		},
		"junk in the slot that is not transferable": {
			items: []Item{{InstanceID: "1", Owner: bungietest.HunterID, Tag: dvm.TagJunk, ClassType: bungie.ClassAny}},
			want:  junkIDs{Staged: []string{"1"}},
		},
		"junk in the slot of another class": {
			items: []Item{
				{
					InstanceID:   "1",
					Owner:        bungietest.HunterID,
					Tag:          dvm.TagJunk,
					ClassType:    bungie.ClassTitan,
					Transferable: true,
				},
			},
			want: junkIDs{Staged: []string{"1"}},
		},
		"junk in the vault": {
			items: []Item{
				{InstanceID: "1", Owner: OwnerVault, Tag: dvm.TagJunk, ClassType: bungie.ClassAny, Transferable: true},
			},
			want: junkIDs{Waiting: []string{"1"}},
		},
		"junk on another character": {
			items: []Item{
				{
					InstanceID:   "1",
					Owner:        bungietest.TitanID,
					Tag:          dvm.TagJunk,
					ClassType:    bungie.ClassAny,
					Transferable: true,
				},
			},
			want: junkIDs{Waiting: []string{"1"}},
		},
		"junk in the vault of the class of the character": {
			items: []Item{
				{
					InstanceID:   "1",
					Owner:        OwnerVault,
					Tag:          dvm.TagJunk,
					ClassType:    bungie.ClassHunter,
					Transferable: true,
				},
			},
			want: junkIDs{Waiting: []string{"1"}},
		},
		"junk in the vault of another class": {
			items: []Item{
				{
					InstanceID:   "1",
					Owner:        OwnerVault,
					Tag:          dvm.TagJunk,
					ClassType:    bungie.ClassTitan,
					Transferable: true,
				},
			},
			want: junkIDs{Skipped: 1},
		},
		"junk in the vault that is not transferable": {
			items: []Item{{InstanceID: "1", Owner: OwnerVault, Tag: dvm.TagJunk, ClassType: bungie.ClassAny}},
			want:  junkIDs{Skipped: 1},
		},
		"junk equipped on another character": {
			items: []Item{
				{
					InstanceID:   "1",
					Owner:        bungietest.TitanID,
					Tag:          dvm.TagJunk,
					ClassType:    bungie.ClassAny,
					Transferable: true,
					Equipped:     true,
				},
			},
			want: junkIDs{Skipped: 1},
		},
		"junk equipped on the character": {
			items: []Item{
				{
					InstanceID:   "1",
					Owner:        bungietest.HunterID,
					Tag:          dvm.TagJunk,
					ClassType:    bungie.ClassAny,
					Transferable: true,
					Equipped:     true,
				},
			},
			want: junkIDs{Skipped: 1},
		},
		"junk at the Postmaster of another character": {
			items: []Item{
				{
					InstanceID:   "1",
					Owner:        bungietest.TitanID,
					Tag:          dvm.TagJunk,
					ClassType:    bungie.ClassAny,
					Transferable: true,
					Postmaster:   true,
				},
			},
			want: junkIDs{Skipped: 1},
		},
		"junk at the Postmaster of the character": {
			items: []Item{
				{
					InstanceID:   "1",
					Owner:        bungietest.HunterID,
					Tag:          dvm.TagJunk,
					ClassType:    bungie.ClassAny,
					Transferable: true,
					Postmaster:   true,
				},
			},
			want: junkIDs{Skipped: 1},
		},
		"item with the tag keep in the slot": {
			items: []Item{
				{
					InstanceID:   "1",
					Owner:        bungietest.HunterID,
					Tag:          "keep",
					ClassType:    bungie.ClassAny,
					Transferable: true,
				},
			},
			want: junkIDs{Keepers: []string{"1"}},
		},
		"item without a tag in the slot": {
			items: []Item{
				{InstanceID: "1", Owner: bungietest.HunterID, ClassType: bungie.ClassAny, Transferable: true},
			},
			want: junkIDs{Keepers: []string{"1"}},
		},
		"item without a tag that the character has equipped": {
			items: []Item{
				{
					InstanceID:   "1",
					Owner:        bungietest.HunterID,
					ClassType:    bungie.ClassAny,
					Transferable: true,
					Equipped:     true,
				},
			},
		},
		"item without a tag at the Postmaster of the character": {
			items: []Item{
				{
					InstanceID:   "1",
					Owner:        bungietest.HunterID,
					ClassType:    bungie.ClassAny,
					Transferable: true,
					Postmaster:   true,
				},
			},
		},
		"item without a tag in the vault or on another character": {
			items: []Item{
				{InstanceID: "1", Owner: OwnerVault, ClassType: bungie.ClassAny, Transferable: true},
				{InstanceID: "2", Owner: bungietest.TitanID, ClassType: bungie.ClassAny, Transferable: true},
			},
		},
		"item with the tag keep that another character has equipped": {
			items: []Item{
				{
					InstanceID:   "1",
					Owner:        bungietest.TitanID,
					Tag:          "keep",
					ClassType:    bungie.ClassAny,
					Transferable: true,
					Equipped:     true,
				},
			},
		},
		"item with the tags keep and favorite in the slot": {
			items: []Item{
				{
					InstanceID:   "1",
					Owner:        bungietest.HunterID,
					Tag:          "keep",
					ClassType:    bungie.ClassAny,
					Transferable: true,
					Favorite:     true,
				},
			},
			want: junkIDs{Keepers: []string{"1"}},
		},
		"junk with the favorite mark in the slot": {
			items: []Item{
				{
					InstanceID:   "1",
					Owner:        bungietest.HunterID,
					Tag:          dvm.TagJunk,
					ClassType:    bungie.ClassAny,
					Transferable: true,
					Favorite:     true,
				},
			},
			want: junkIDs{Staged: []string{"1"}},
		},
		"junk of another bucket": {
			items: []Item{
				{
					InstanceID: "1",
					Owner:      bungietest.HunterID,
					Tag:        dvm.TagJunk,
					Bucket:     bungie.BucketPower,
					ClassType:  bungie.ClassAny,
				},
				{
					InstanceID: "2",
					Owner:      OwnerVault,
					Tag:        dvm.TagJunk,
					Bucket:     bungie.BucketPower,
					ClassType:  bungie.ClassAny,
				},
			},
		},
		"item without a tag of another bucket": {
			items: []Item{
				{InstanceID: "1", Owner: bungietest.HunterID, Bucket: bungie.BucketPower, ClassType: bungie.ClassAny},
			},
		},
		"order": {
			items: []Item{
				{
					InstanceID: "5",
					Name:       "Midnight Coup",
					Owner:      bungietest.HunterID,
					Tag:        dvm.TagJunk,
					ClassType:  bungie.ClassAny,
				},
				{InstanceID: "1000", Owner: bungietest.HunterID, Tag: dvm.TagJunk, ClassType: bungie.ClassAny},
				{InstanceID: "999", Owner: bungietest.HunterID, Tag: dvm.TagJunk, ClassType: bungie.ClassAny},
				{
					InstanceID:   "6",
					Name:         "Midnight Coup",
					Owner:        OwnerVault,
					Tag:          dvm.TagJunk,
					ClassType:    bungie.ClassAny,
					Transferable: true,
				},
				{
					InstanceID:   "1001",
					Owner:        OwnerVault,
					Tag:          dvm.TagJunk,
					ClassType:    bungie.ClassAny,
					Transferable: true,
				},
				{
					InstanceID:   "998",
					Owner:        bungietest.TitanID,
					Tag:          dvm.TagJunk,
					ClassType:    bungie.ClassAny,
					Transferable: true,
				},
				{InstanceID: "7", Name: "Midnight Coup", Owner: bungietest.HunterID, ClassType: bungie.ClassAny},
				{InstanceID: "1002", Owner: bungietest.HunterID, ClassType: bungie.ClassAny},
				{InstanceID: "997", Owner: bungietest.HunterID, ClassType: bungie.ClassAny},
			},
			want: junkIDs{
				Staged:  []string{"999", "1000", "5"},
				Waiting: []string{"998", "1001", "6"},
				Keepers: []string{"997", "1002", "7"},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := junkSnapshot(tc.items)
			hunter, ok := s.Character(bungietest.HunterID)
			require.True(t, ok)

			assert.Equal(t, tc.want, junkIDsOf(s.JunkPlan(hunter, s.Buckets[0])))
		})
	}
}

func TestJunkPlanRoom(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		unequipped int
		postmaster int
		want       int
		equipped   bool
	}{
		"room with an equipped item":                  {equipped: true, unequipped: 2, want: 7},
		"room without an equipped item":               {unequipped: 3, want: 7},
		"Postmaster items take no room":               {unequipped: 3, postmaster: 2, want: 7},
		"full slot":                                   {equipped: true, unequipped: 9},
		"full slot with more items than its capacity": {equipped: true, unequipped: 10},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var items []Item

			add := func(item Item) {
				item.InstanceID = strconv.Itoa(len(items) + 1)
				item.Owner = bungietest.HunterID
				item.ClassType = bungie.ClassAny
				items = append(items, item)
			}

			if tc.equipped {
				add(Item{Equipped: true})
			}

			for range tc.unequipped {
				add(Item{})
			}

			for range tc.postmaster {
				add(Item{Postmaster: true})
			}

			// Items of another character and of the vault take no room.
			items = append(items,
				Item{InstanceID: "100", Owner: bungietest.TitanID, ClassType: bungie.ClassAny},
				Item{InstanceID: "101", Owner: OwnerVault, ClassType: bungie.ClassAny},
			)

			s := junkSnapshot(items)
			hunter, ok := s.Character(bungietest.HunterID)
			require.True(t, ok)

			assert.Equal(t, tc.want, s.JunkPlan(hunter, s.Buckets[0]).Room)
		})
	}
}
