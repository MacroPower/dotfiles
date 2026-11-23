package inventory

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungietest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitionstest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
)

func ids(items []*Item) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.InstanceID)
	}

	return out
}

func TestNewSnapshot(t *testing.T) {
	t.Parallel()

	s := fixtureSnapshot(t)

	require.Len(t, s.Characters, 2)
	assert.Equal(t, bungietest.HunterID, s.Characters[0].ID, "most recently played character comes first")
	assert.Equal(t, "Hunter", s.Characters[0].Class)
	assert.Equal(t, "Titan", s.Characters[1].Class)

	vaulted, ok := s.Find("1001")
	require.True(t, ok)
	assert.Equal(t, &Item{
		InstanceID: "1001",
		Hash:       100,
		Name:       "Fatebringer",
		TypeName:   "Hand Cannon",
		Tier:       "Legendary",
		Slot:       "Kinetic Weapons",
		Damage:     "Kinetic",
		Owner:      OwnerVault,
		Kind:       KindWeapon,
		ClassType:  bungie.ClassAny,
		AmmoType:   bungie.AmmoPrimary,
		Power:      2010,
		Quantity:   1,
		Bucket:     bungie.BucketKinetic,
		Perks: []Perk{
			{
				Plug:    Plug{Name: "Explosive Payload", Type: "Trait", Hash: 900},
				Options: []Plug{{Name: "Rampage", Type: "Trait", Hash: 903}},
				Plugs: []Plug{
					{Name: "Explosive Payload", Type: "Trait", Hash: 900},
					{Name: "Rampage", Type: "Trait", Hash: 903},
				},
			},
			{
				Plug: Plug{
					Name:        "Firefly",
					Type:        "Trait",
					Description: "Precision final blows cause targets to explode.\n\n Explosions deal Solar damage. ",
					Hash:        901,
				},
				Socket: 1,
			},
		},
		Stats:        []Stat{{Name: "Range", Value: 55, Hash: 1}},
		Locked:       true,
		Masterwork:   true,
		Transferable: true,
		Equippable:   true,
		Lockable:     true,
	}, vaulted)

	stacks, err := s.Filter(ItemFilter{Kind: KindOther})
	require.NoError(t, err)
	require.Len(t, stacks, 1)
	assert.False(t, stacks[0].Lockable, "a stack is not lockable")
	assert.Equal(t, OwnerAccount, stacks[0].Owner, "a Consumables stack at the vault location belongs to the account")

	helmet, ok := s.Find("1004")
	require.True(t, ok)
	assert.Equal(t, KindArmor, helmet.Kind)
	assert.Equal(t, "Hunter", helmet.Class)
	assert.Empty(t, helmet.Damage)
	assert.True(t, helmet.Equipped)
	assert.Equal(t, "Hunter (equipped)", s.LocationLabel(helmet))

	lost, ok := s.Find("1005")
	require.True(t, ok)
	assert.True(t, lost.Postmaster)
	assert.True(t, lost.Lost)
	assert.Equal(t, "Kinetic Weapons", lost.Slot, "a Postmaster item takes the slot of its definition")
	assert.Equal(t, "Hunter (postmaster)", s.LocationLabel(lost))
	assert.True(t, lost.Transferable, "the transfer status of a Postmaster item does not block a move")
}

func TestNewItemTransferStatus(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		instanceID string
		want       int
	}{
		"the equipped hand cannon": {instanceID: "1006", want: 1},
		"the lost hand cannon":     {instanceID: "1005", want: 2},
		"the vault hand cannon":    {instanceID: "1001", want: 0},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			item, ok := fixtureSnapshot(t).Find(tc.instanceID)
			require.True(t, ok)
			assert.Equal(t, tc.want, item.TransferStatus)
		})
	}
}

func TestSlotBuckets(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		defs *definitions.Defs
		want []Bucket
	}{
		"equipment before items": {
			defs: definitionstest.Defs(),
			want: []Bucket{
				{Name: "Vault", Hash: bungie.BucketVault, Capacity: 700, Account: true},
				{Name: "Kinetic Weapons", Hash: bungie.BucketKinetic, Capacity: 10},
				{Name: "Power Weapons", Hash: bungie.BucketPower, Capacity: 10},
				{Name: "Helmet", Hash: bungie.BucketHelmet, Capacity: 10},
				{Name: "Consumables", Hash: bungie.BucketConsumables, Capacity: 50, Account: true},
				{Name: "Engrams", Hash: bungie.BucketEngrams, Capacity: 10},
				{Name: "Postmaster", Hash: bungie.BucketPostmaster, Capacity: 21},
			},
		},
		"unnamed buckets and the Postmaster stay out of the middle": {
			defs: &definitions.Defs{Buckets: map[uint32]definitions.BucketDef{
				bungie.BucketVault: {
					Name:     "General",
					Capacity: 700,
					Category: bungie.BucketCategoryItem,
					Order:    30,
					Account:  true,
				},
				bungie.BucketPostmaster: {
					Name:     "Lost Items",
					Capacity: 21,
					Category: bungie.BucketCategoryItem,
					Order:    10,
				},
				bungie.BucketKinetic: {
					Name:     "Kinetic Weapons",
					Capacity: 10,
					Category: bungie.BucketCategoryEquippable,
					Order:    20,
				},
				3796357825: {Capacity: 10, Category: bungie.BucketCategoryEquippable, Order: 120},
			}},
			want: []Bucket{
				{Name: "Vault", Hash: bungie.BucketVault, Capacity: 700, Account: true},
				{Name: "Kinetic Weapons", Hash: bungie.BucketKinetic, Capacity: 10},
				{Name: "Postmaster", Hash: bungie.BucketPostmaster, Capacity: 21},
			},
		},
		"definitions without buckets": {
			defs: &definitions.Defs{},
			want: []Bucket{
				{Name: "Vault", Hash: bungie.BucketVault, Account: true},
				{Name: "Postmaster", Hash: bungie.BucketPostmaster},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, slotBuckets(tc.defs))
		})
	}
}

func TestSlotsUsed(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		move   string
		target string
		owner  string
		extra  []bungie.ProfileItem
		bucket uint32
		want   int
	}{
		"equipped items take slots": {
			bucket: bungie.BucketKinetic,
			owner:  bungietest.HunterID,
			want:   2,
		},
		"another character": {
			bucket: bungie.BucketKinetic,
			owner:  bungietest.TitanID,
		},
		"postmaster": {
			bucket: bungie.BucketPostmaster,
			owner:  bungietest.HunterID,
			want:   1,
		},
		"messages take no Postmaster slot": {
			extra: []bungie.ProfileItem{
				{Hash: 300, Quantity: 1, Location: bungie.ItemLocationPostmaster, BucketHash: bungie.BucketMessages},
				{Hash: 300, Quantity: 2, Location: bungie.ItemLocationPostmaster, BucketHash: bungie.BucketMessages},
			},
			bucket: bungie.BucketPostmaster,
			owner:  bungietest.HunterID,
			want:   1,
		},
		"vault ignores the owner": {
			bucket: bungie.BucketVault,
			owner:  bungietest.HunterID,
			want:   2,
		},
		"account bucket counts stacks": {
			bucket: bungie.BucketConsumables,
			owner:  bungietest.HunterID,
			want:   1,
		},
		"move to the vault frees the slot": {
			move:   "1003",
			target: OwnerVault,
			bucket: bungie.BucketKinetic,
			owner:  bungietest.HunterID,
			want:   1,
		},
		"move to the vault takes a vault slot": {
			move:   "1003",
			target: OwnerVault,
			bucket: bungie.BucketVault,
			want:   3,
		},
		"pull frees the Postmaster slot": {
			move:   "1005",
			target: bungietest.HunterID,
			bucket: bungie.BucketPostmaster,
			owner:  bungietest.HunterID,
		},
		"pulled item takes a slot of its bucket": {
			move:   "1005",
			target: bungietest.HunterID,
			bucket: bungie.BucketKinetic,
			owner:  bungietest.HunterID,
			want:   3,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var profile bungie.Profile

			require.NoError(t, json.Unmarshal([]byte(bungietest.ProfileJSON), &profile))

			hunter := profile.CharacterInventories.Data[bungietest.HunterID]
			hunter.Items = append(hunter.Items, tc.extra...)
			profile.CharacterInventories.Data[bungietest.HunterID] = hunter

			s := NewSnapshot(&profile, definitionstest.Defs(), bungietest.Now)

			if tc.move != "" {
				item, ok := s.Find(tc.move)
				require.True(t, ok)

				_, err := s.Move(t.Context(), &bungietest.StepMover{}, item, tc.target, false)
				require.NoError(t, err)
			}

			i := slices.IndexFunc(s.Buckets, func(b Bucket) bool { return b.Hash == tc.bucket })
			require.GreaterOrEqual(t, i, 0)
			assert.Equal(t, tc.want, s.SlotsUsed(s.Buckets[i], tc.owner))
		})
	}
}

func TestHeld(t *testing.T) {
	t.Parallel()

	s := &Snapshot{byID: map[string]*Item{}}
	for _, item := range []*Item{
		{Hash: 1, Quantity: 2, Owner: OwnerAccount},
		{Hash: 1, Quantity: 3, Owner: OwnerAccount},
		{Hash: 1, Quantity: 1, Owner: bungietest.HunterID, Postmaster: true},
		{Hash: 2, Quantity: 40, Owner: OwnerAccount},
	} {
		s.Add(item)
	}

	tests := map[string]struct {
		hash           uint32
		wantHeld       int
		wantPostmaster int
	}{
		"stacks outside and at the Postmaster": {hash: 1, wantHeld: 5, wantPostmaster: 1},
		"stack outside the Postmaster":         {hash: 2, wantHeld: 40},
		"item that the account lacks":          {hash: 3},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			held, postmaster := s.Held(tc.hash)
			assert.Equal(t, tc.wantHeld, held, "held")
			assert.Equal(t, tc.wantPostmaster, postmaster, "postmaster")
		})
	}
}

func TestAnnotate(t *testing.T) {
	t.Parallel()

	s := fixtureSnapshot(t)

	s.Annotate(
		[]dvm.ItemTag{
			{InstanceID: "1001", TagID: "junk"},
			{InstanceID: "1001", TagID: "keep"},
			{InstanceID: "1001", TagID: dvm.TagFavorite},
			{InstanceID: "1002", TagID: "pvp", Removed: true},
			{InstanceID: "1003", TagID: dvm.TagFavorite},
		},
		[]dvm.Note{
			{Kind: dvm.NoteInstance, TargetID: "1001", Body: "god roll"},
			{Kind: dvm.NoteInstance, TargetID: "1002", Body: "gone", Removed: true},
			{Kind: dvm.NoteSet, TargetID: "1003", Body: "not an instance note"},
		},
	)

	tests := map[string]struct {
		wantTag      string
		wantNote     string
		wantFavorite bool
	}{
		"1001": {wantTag: "keep", wantNote: "god roll", wantFavorite: true},
		"1002": {},
		"1003": {wantFavorite: true},
	}

	for id, tc := range tests {
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			item, ok := s.Find(id)
			require.True(t, ok)
			assert.Equal(t, tc.wantTag, item.Tag)
			assert.Equal(t, tc.wantNote, item.Note)
			assert.Equal(t, tc.wantFavorite, item.Favorite)
		})
	}
}

func TestFilter(t *testing.T) {
	t.Parallel()

	s := fixtureSnapshot(t)
	s.Annotate([]dvm.ItemTag{
		{InstanceID: "1001", TagID: "keep"},
		{InstanceID: "1002", TagID: dvm.TagFavorite},
	}, nil)

	tests := map[string]struct {
		err    error
		filter ItemFilter
		want   []string
	}{
		"default lists gear by name": {
			want: []string{"1004", "1001", "1005", "1006", "1002", "1003"},
		},
		"all kinds include stacks": {
			filter: ItemFilter{Kind: KindAll, Query: "enhancement"},
			want:   []string{""},
		},
		"armor": {
			filter: ItemFilter{Kind: KindArmor},
			want:   []string{"1004"},
		},
		"query matches the type name": {
			filter: ItemFilter{Query: "ROCKET"},
			want:   []string{"1002"},
		},
		"slot": {
			filter: ItemFilter{Slot: "power"},
			want:   []string{"1002"},
		},
		"tier": {
			filter: ItemFilter{Tier: "exotic"},
			want:   []string{"1004", "1002"},
		},
		"class": {
			filter: ItemFilter{Class: "hunter"},
			want:   []string{"1004"},
		},
		"damage": {
			filter: ItemFilter{Damage: "solar"},
			want:   []string{"1002"},
		},
		"ammo in any letter case": {
			filter: ItemFilter{Ammo: "HEAVY"},
			want:   []string{"1002"},
		},
		// The helmet has no ammo type, so it does not match any ammo name.
		"ammo with every kind": {
			filter: ItemFilter{Ammo: "primary", Kind: KindAll},
			want:   []string{"1001", "1005", "1006", "1003"},
		},
		"ammo that no item has": {
			filter: ItemFilter{Ammo: "special"},
			want:   []string{},
		},
		"vault": {
			filter: ItemFilter{Location: "Vault"},
			want:   []string{"1001", "1002"},
		},
		"class name as location": {
			filter: ItemFilter{Location: "hunter", Sort: SortNewest},
			want:   []string{"1006", "1005", "1004", "1003"},
		},
		"equipped": {
			filter: ItemFilter{Location: LocationEquipped},
			want:   []string{"1004", "1006"},
		},
		"postmaster": {
			filter: ItemFilter{Location: LocationPostmaster},
			want:   []string{"1005"},
		},
		"any character": {
			filter: ItemFilter{Location: LocationCharacters, Kind: KindWeapon},
			want:   []string{"1005", "1006", "1003"},
		},
		"tag": {
			filter: ItemFilter{Tag: "keep"},
			want:   []string{"1001"},
		},
		"untagged weapons": {
			filter: ItemFilter{Tag: TagNone, Kind: KindWeapon, Location: "vault"},
			want:   []string{"1002"},
		},
		"tagged": {
			filter: ItemFilter{Tag: TagAny},
			want:   []string{"1001"},
		},
		"favorite": {
			filter: ItemFilter{Favorite: true},
			want:   []string{"1002"},
		},
		"perk": {
			filter: ItemFilter{Perk: "fire"},
			want:   []string{"1001"},
		},
		"power bounds are inclusive": {
			filter: ItemFilter{MinPower: 1990, MaxPower: 2005, Sort: SortPower},
			want:   []string{"1003", "1004", "1002"},
		},
		// The consumables stack has no power, so it passes no bound.
		"maximum power without power": {
			filter: ItemFilter{Kind: KindAll, MaxPower: 1995},
			want:   []string{"1005", "1006", "1002"},
		},
		"unknown kind": {
			filter: ItemFilter{Kind: "ghost"},
			err:    ErrInvalidFilter,
		},
		"unknown sort": {
			filter: ItemFilter{Sort: "random"},
			err:    ErrInvalidFilter,
		},
		"unknown ammo": {
			filter: ItemFilter{Ammo: "power"},
			err:    ErrInvalidFilter,
		},
		"unknown location": {
			filter: ItemFilter{Location: "warlock"},
			err:    ErrUnknownTarget,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := s.Filter(tc.filter)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, ids(got))
		})
	}
}

func TestFilterStats(t *testing.T) {
	t.Parallel()

	helmet := func(id string, health, grenade int) *Item {
		return &Item{
			InstanceID: id,
			Name:       "Helmet " + id,
			Kind:       KindArmor,
			Stats:      []Stat{{Name: "Grenade", Value: grenade}, {Name: "Health", Value: health}},
		}
	}

	// The items that tie on a total or a stat are out of name order. The
	// tie-breaks of the total sort and the stat sort put them in name order.
	s := &Snapshot{Items: []*Item{
		helmet("13", 20, 20),
		helmet("12", 10, 40),
		helmet("11", 30, 10),
		{InstanceID: "15", Name: "Bare Helmet", Kind: KindArmor},
		{InstanceID: "14", Name: "Ace of Spades", Kind: KindWeapon, Stats: []Stat{{Name: "Range", Value: 60}}},
	}}

	// The copies of one name are out of instance id order.
	twins := &Snapshot{Items: []*Item{
		{InstanceID: "31", Name: "Twin Helmet", Kind: KindArmor, Stats: []Stat{{Name: "Health", Value: 10}}},
		{InstanceID: "30", Name: "Twin Helmet", Kind: KindArmor, Stats: []Stat{{Name: "Health", Value: 10}}},
	}}

	// The name order of the tied items is the reverse of their instance id
	// order, so a tie-break on the instance id alone fails.
	rivals := &Snapshot{Items: []*Item{
		{InstanceID: "40", Name: "Beta Helmet", Kind: KindArmor, Stats: []Stat{{Name: "Health", Value: 10}}},
		{InstanceID: "41", Name: "Alpha Helmet", Kind: KindArmor, Stats: []Stat{{Name: "Health", Value: 10}}},
	}}

	tests := map[string]struct {
		snapshot *Snapshot
		err      error
		msg      string
		filter   ItemFilter
		want     []string
	}{
		"total sort puts items without a total last": {
			filter: ItemFilter{Sort: SortTotal},
			want:   []string{"12", "11", "13", "14", "15"},
		},
		"total sort puts a zero total ahead of items without a total": {
			snapshot: &Snapshot{Items: []*Item{
				{InstanceID: "19", Name: "Ace of Spades", Kind: KindWeapon, Stats: []Stat{{Name: "Range", Value: 60}}},
				{InstanceID: "20", Name: "Bare Helmet", Kind: KindArmor},
				{
					InstanceID: "21",
					Name:       "Zero Helmet",
					Kind:       KindArmor,
					Stats:      []Stat{{Name: "Grenade", Value: 0}, {Name: "Health", Value: 0}},
				},
			}},
			filter: ItemFilter{Sort: SortTotal},
			want:   []string{"21", "19", "20"},
		},
		"total sort orders copies of one name by instance id": {
			snapshot: twins,
			filter:   ItemFilter{Sort: SortTotal},
			want:     []string{"30", "31"},
		},
		"total sort orders a tie by name ahead of instance id": {
			snapshot: rivals,
			filter:   ItemFilter{Sort: SortTotal},
			want:     []string{"41", "40"},
		},
		"minimum total is inclusive and selects only armor": {
			filter: ItemFilter{MinTotal: 40},
			want:   []string{"11", "12", "13"},
		},
		"stat sort puts items without the stat last": {
			filter: ItemFilter{Stat: "health", Sort: SortStat},
			want:   []string{"11", "13", "12", "14", "15"},
		},
		"stat sort puts a zero value ahead of items without the stat": {
			snapshot: &Snapshot{Items: []*Item{
				{InstanceID: "17", Name: "Ace of Spades", Kind: KindWeapon, Stats: []Stat{{Name: "Range", Value: 60}}},
				{InstanceID: "18", Name: "Zero Helmet", Kind: KindArmor, Stats: []Stat{{Name: "Health", Value: 0}}},
			}},
			filter: ItemFilter{Stat: "health", Sort: SortStat},
			want:   []string{"18", "17"},
		},
		"stat sort orders copies of one name by instance id": {
			snapshot: twins,
			filter:   ItemFilter{Stat: "health", Sort: SortStat},
			want:     []string{"30", "31"},
		},
		"stat sort orders a tie by name ahead of instance id": {
			snapshot: rivals,
			filter:   ItemFilter{Stat: "health", Sort: SortStat},
			want:     []string{"41", "40"},
		},
		"minimum stat is inclusive": {
			filter: ItemFilter{Stat: "HEALTH", MinStat: 20},
			want:   []string{"11", "13"},
		},
		"minimum stat with an accent": {
			filter: ItemFilter{Stat: "Héalth", MinStat: 20},
			want:   []string{"11", "13"},
		},
		"minimum stat of a weapon": {
			filter: ItemFilter{Stat: "range", MinStat: 50},
			want:   []string{"14"},
		},
		"stat alone does not filter": {
			filter: ItemFilter{Stat: "grenade"},
			want:   []string{"14", "15", "11", "12", "13"},
		},
		"unknown stat": {
			filter: ItemFilter{Stat: "mobility"},
			err:    ErrInvalidFilter,
			msg:    `invalid filter: no item in the inventory has a stat named "mobility", so pass one of Grenade, Health, Range`,
		},
		"stat in an inventory without stats": {
			snapshot: &Snapshot{Items: []*Item{{InstanceID: "16", Kind: KindArmor}}},
			filter:   ItemFilter{Stat: "health"},
			err:      ErrInvalidFilter,
			msg:      "invalid filter: no item in the inventory has stats",
		},
		"minimum stat without a stat": {
			filter: ItemFilter{MinStat: 10},
			err:    ErrInvalidFilter,
			msg:    "invalid filter: a minimum stat value needs a stat name",
		},
		"stat sort without a stat": {
			filter: ItemFilter{Sort: SortStat},
			err:    ErrInvalidFilter,
			msg:    `invalid filter: sort "stat" needs a stat name`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			snapshot := cmp.Or(tc.snapshot, s)

			got, err := snapshot.Filter(tc.filter)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				assert.EqualError(t, err, tc.msg)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, ids(got))
		})
	}
}

func TestFilterNames(t *testing.T) {
	t.Parallel()

	s := &Snapshot{Items: []*Item{
		{InstanceID: "1", Name: "Mataiodoxía", TypeName: "Chest Armor", Kind: KindArmor, Slot: "Chest Armor"},
		{
			InstanceID: "2",
			Name:       "Fatebringer",
			TypeName:   "Hand Cannon",
			Kind:       KindWeapon,
			Slot:       "Kinétic Weapons",
			Perks:      []Perk{{Plug: Plug{Name: "Explósive Payload"}}},
			Stats:      []Stat{{Name: "Stábility", Value: 40}},
		},
		{
			InstanceID: "3",
			Name:       "Gjallarhorn",
			TypeName:   "Röcket Launcher",
			Kind:       KindWeapon,
			Slot:       "Power Weapons",
			Stats:      []Stat{{Name: "Stábility", Value: 50}},
		},
	}}

	// The stat names and the filters carry accents on different letters,
	// so a stat matches only when the filter folds both texts. Gjallarhorn
	// has the higher stat value but comes after Fatebringer in name order.
	tests := map[string]struct {
		filter ItemFilter
		want   []string
	}{
		"name without its accents":      {filter: ItemFilter{Query: "mataiodoxia"}, want: []string{"1"}},
		"name with accents":             {filter: ItemFilter{Query: "FÁTE"}, want: []string{"2"}},
		"type name without its accents": {filter: ItemFilter{Query: "rocket"}, want: []string{"3"}},
		"slot without its accents":      {filter: ItemFilter{Slot: "kinetic"}, want: []string{"2"}},
		"slot with other accents":       {filter: ItemFilter{Slot: "KINETÍC"}, want: []string{"2"}},
		"perk without its accents":      {filter: ItemFilter{Perk: "explosive"}, want: []string{"2"}},
		"perk with other accents":       {filter: ItemFilter{Perk: "EXPLOSÍVE"}, want: []string{"2"}},
		"query that matches nothing":    {filter: ItemFilter{Query: "mataiodoxie"}, want: []string{}},
		"minimum stat with other accents": {
			filter: ItemFilter{Stat: "STABILÍTY", MinStat: 45},
			want:   []string{"3"},
		},
		"stat sort with other accents": {
			filter: ItemFilter{Stat: "stabílity", Sort: SortStat},
			want:   []string{"3", "2", "1"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := s.Filter(tc.filter)
			require.NoError(t, err)
			assert.Equal(t, tc.want, ids(got))
		})
	}
}

func TestFilterLookalikes(t *testing.T) {
	t.Parallel()

	// The game writes a digit in place of a letter in the names of the
	// first three items, of the last two items, and of the perk. It writes
	// the name of item 6 with a number.
	s := &Snapshot{Items: []*Item{
		{InstanceID: "1", Name: "St0mp-EE5", TypeName: "Leg Armor", Kind: KindArmor, Slot: "Leg Armor"},
		{InstanceID: "2", Name: "Fr0st-EE5", TypeName: "Leg Armor", Kind: KindArmor, Slot: "Leg Armor"},
		{InstanceID: "3", Name: "BrayTech Sn0Boots", TypeName: "Leg Armor", Kind: KindArmor, Slot: "Leg Armor"},
		{
			InstanceID: "4",
			Name:       "Hardcase Stompers",
			TypeName:   "Leg Armor",
			Kind:       KindArmor,
			Slot:       "Leg Armor",
			Perks:      []Perk{{Plug: Plug{Name: "M1R Distribution Matrix"}}},
		},
		{
			InstanceID: "5",
			Name:       "Permafrost",
			TypeName:   "Grenade Launcher",
			Kind:       KindWeapon,
			Slot:       "Power Weapons",
			Stats:      []Stat{{Name: "Velocity", Value: 60}},
		},
		{
			InstanceID: "6",
			Name:       "1000 Yard Stare",
			TypeName:   "Sniper Rifle",
			Kind:       KindWeapon,
			Slot:       "Kinetic Weapons",
			Stats:      []Stat{{Name: "Velocity", Value: 40}},
		},
		{InstanceID: "7", Name: "S733k Pack", TypeName: "Ghost Shell", Kind: KindOther},
		{InstanceID: "8", Name: "Se7enth", TypeName: "Emblem", Kind: KindOther},
	}}

	tests := map[string]struct {
		filter ItemFilter
		want   []string
	}{
		"letters for a digit of a name":    {filter: ItemFilter{Query: "stomp"}, want: []string{"4", "1"}},
		"letters for a seven that is l":    {filter: ItemFilter{Kind: KindAll, Query: "sleek"}, want: []string{"7"}},
		"letters for a seven that is v":    {filter: ItemFilter{Kind: KindAll, Query: "seventh"}, want: []string{"8"}},
		"seven as the name writes it":      {filter: ItemFilter{Kind: KindAll, Query: "S733K"}, want: []string{"7"}},
		"letters for every digit":          {filter: ItemFilter{Query: "Stomp-EES"}, want: []string{"1"}},
		"letters and digits":               {filter: ItemFilter{Query: "stomp-ee5"}, want: []string{"1"}},
		"digits as the name writes them":   {filter: ItemFilter{Query: "ST0MP-EE5"}, want: []string{"1"}},
		"digit for a letter of a name":     {filter: ItemFilter{Query: "st0mpers"}, want: []string{"4"}},
		"letters for a digit of two names": {filter: ItemFilter{Query: "frost"}, want: []string{"2", "5"}},
		"letters inside a word":            {filter: ItemFilter{Query: "snoboots"}, want: []string{"3"}},
		"type name with a digit": {
			filter: ItemFilter{Query: "l3g arm0r"},
			want:   []string{"3", "2", "4", "1"},
		},
		"slot with a digit":            {filter: ItemFilter{Slot: "p0wer"}, want: []string{"5"}},
		"perk in letters":              {filter: ItemFilter{Perk: "mir distribution"}, want: []string{"4"}},
		"perk as the name writes it":   {filter: ItemFilter{Perk: "m1r"}, want: []string{"4"}},
		"number as the name writes it": {filter: ItemFilter{Query: "1000 yard"}, want: []string{"6"}},
		"letters for a number":         {filter: ItemFilter{Query: "iooo"}, want: []string{"6"}},
		"another digit":                {filter: ItemFilter{Query: "st9mp"}, want: []string{}},
		"letters without the hyphen":   {filter: ItemFilter{Query: "stompees"}, want: []string{}},
		"minimum stat with a digit": {
			filter: ItemFilter{Stat: "vel0city", MinStat: 50},
			want:   []string{"5"},
		},
		"stat sort with digits": {
			filter: ItemFilter{Kind: KindWeapon, Stat: "V3L0C1TY", Sort: SortStat},
			want:   []string{"5", "6"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := s.Filter(tc.filter)
			require.NoError(t, err)
			assert.Equal(t, tc.want, ids(got))
		})
	}
}

func TestAmmoName(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		want     string
		ammoType int
	}{
		"primary":         {ammoType: bungie.AmmoPrimary, want: AmmoPrimary},
		"special":         {ammoType: bungie.AmmoSpecial, want: AmmoSpecial},
		"heavy":           {ammoType: bungie.AmmoHeavy, want: AmmoHeavy},
		"no ammo type":    {ammoType: 0, want: ""},
		"bungie unknown":  {ammoType: 4, want: ""},
		"negative number": {ammoType: -1, want: ""},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, AmmoName(tc.ammoType))
		})
	}
}

func TestStatValue(t *testing.T) {
	t.Parallel()

	item := &Item{Stats: []Stat{{Name: "Grenade", Value: 10}, {Name: "Stábility", Value: 40}}}

	// The first stat of rivals differs from the second only in a digit
	// that stands for a letter.
	rivals := &Item{Stats: []Stat{{Name: "Gr3nade", Value: 5}, {Name: "Grenade", Value: 10}}}

	// The stat name and the first case carry accents on different
	// letters, so the case matches only when StatValue folds both texts.
	tests := map[string]struct {
		item  *Item
		name  string
		want  int
		found bool
	}{
		"name with other accents":       {name: "STABILÍTY", want: 40, found: true},
		"name without its accents":      {name: "stability", want: 40, found: true},
		"name that differs in case":     {name: "grenade", want: 10, found: true},
		"missing stat":                  {name: "range"},
		"digits for letters":            {name: "5tab1l1ty", want: 40, found: true},
		"missing stat with a digit":     {name: "grenad3s"},
		"name in letters before digits": {item: rivals, name: "GRENADE", want: 10, found: true},
		"name in digits":                {item: rivals, name: "gr3nade", want: 5, found: true},
		"first stat with a digit":       {item: rivals, name: "9renade", want: 5, found: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, ok := cmp.Or(tc.item, item).StatValue(tc.name)
			assert.Equal(t, tc.found, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestResolveOwner(t *testing.T) {
	t.Parallel()

	s := fixtureSnapshot(t)
	s.Characters = append(s.Characters, Character{ID: "c3", Class: "Titan"})

	tests := map[string]struct {
		err  error
		name string
		want string
	}{
		"vault":            {name: "VAULT", want: OwnerVault},
		"character id":     {name: "c2", want: "c2"},
		"unique class":     {name: "Hunter", want: bungietest.HunterID},
		"ambiguous class":  {name: "titan", err: ErrUnknownTarget},
		"unknown location": {name: "tower", err: ErrUnknownTarget},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := s.ResolveOwner(tc.name)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestResolveCharacter(t *testing.T) {
	t.Parallel()

	s := fixtureSnapshot(t)

	shared := fixtureSnapshot(t)
	shared.Characters = append(shared.Characters, Character{ID: "c3", Class: "Titan"})

	// The fixture Hunter has the lowest character id and the latest play
	// time. The Titan in titanLast has the latest play time instead, so the
	// empty name must follow the play time and not the id.
	var profile bungie.Profile

	require.NoError(t, json.Unmarshal([]byte(bungietest.ProfileJSON), &profile))

	titan := profile.Characters.Data[bungietest.TitanID]
	titan.LastPlayed = bungietest.Now
	profile.Characters.Data[bungietest.TitanID] = titan

	titanLast := NewSnapshot(&profile, definitionstest.Defs(), bungietest.Now)

	tests := map[string]struct {
		err      error
		snapshot *Snapshot
		name     string
		want     string
	}{
		"character id":                {snapshot: s, name: "c2", want: bungietest.TitanID},
		"unique class":                {snapshot: s, name: "HUNTER", want: bungietest.HunterID},
		"most recently played hunter": {snapshot: s, name: "", want: bungietest.HunterID},
		"most recently played titan":  {snapshot: titanLast, name: "", want: bungietest.TitanID},
		"vault":                       {snapshot: s, name: "Vault", err: ErrUnknownTarget},
		"unknown name":                {snapshot: s, name: "tower", err: ErrUnknownTarget},
		"shared class":                {snapshot: shared, name: "titan", err: ErrUnknownTarget},
		"no characters":               {snapshot: &Snapshot{}, name: "", err: ErrUnknownTarget},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := tc.snapshot.ResolveCharacter(tc.name)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got.ID)
		})
	}
}

// groupSnapshot holds a reissue of Fatebringer under its own item hash and
// a Timelost copy. It also holds a weapon whose name contains the name of
// another weapon, two weapons whose names differ only in letter case, and
// items that are not weapons.
func groupSnapshot() *Snapshot {
	return &Snapshot{Items: []*Item{
		{InstanceID: "12", Hash: 110, Name: "Fatebringer", Kind: KindWeapon},
		{InstanceID: "3", Hash: 102, Name: "Midnight Coup", Kind: KindWeapon},
		{InstanceID: "9", Hash: 100, Name: "Fatebringer", Kind: KindWeapon},
		{InstanceID: "4", Hash: 120, Name: "Fatebringer (Timelost)", Kind: KindWeapon},
		{InstanceID: "5", Hash: 200, Name: "Fatebringer Helm", Kind: KindArmor},
		{Hash: 300, Name: "Enhancement Core", Kind: KindOther},
		{InstanceID: "6", Hash: 130, Name: "Austringer", Kind: KindWeapon},
		{InstanceID: "7", Hash: 140, Name: "Midnight Coup Replica", Kind: KindWeapon},
		{InstanceID: "8", Hash: 150, Name: "Ace of Spades", Kind: KindWeapon},
		{InstanceID: "10", Hash: 160, Name: "ACE OF SPADES", Kind: KindWeapon},
	}}
}

func TestWeaponGroups(t *testing.T) {
	t.Parallel()

	got := map[string][]string{}

	var names []string

	for _, group := range groupSnapshot().WeaponGroups() {
		names = append(names, group.Name)
		got[group.Name] = ids(group.Copies)
	}

	assert.Equal(t, []string{
		"ACE OF SPADES", "Ace of Spades", "Austringer", "Fatebringer", "Midnight Coup", "Midnight Coup Replica",
	}, names)
	assert.Equal(t, map[string][]string{
		"ACE OF SPADES":         {"10"},
		"Ace of Spades":         {"8"},
		"Austringer":            {"6"},
		"Fatebringer":           {"9", "12", "4"},
		"Midnight Coup":         {"3"},
		"Midnight Coup Replica": {"7"},
	}, got, "a reissue and a Timelost copy join the base weapon, ordered by name and then by acquisition")
}

func TestCopiesOf(t *testing.T) {
	t.Parallel()

	accented := &Snapshot{Items: []*Item{
		{InstanceID: "21", Name: "Déjà Vu", Kind: KindWeapon},
		{InstanceID: "22", Name: "Déjà Vu Prime", Kind: KindWeapon},
		{InstanceID: "23", Name: "Crépuscule (Adept)", Kind: KindWeapon},
	}}
	single := &Snapshot{Items: []*Item{
		{InstanceID: "31", Name: "Fatebringer", Kind: KindWeapon},
	}}

	// The game writes the first three names with numbers.
	numbered := &Snapshot{Items: []*Item{
		{InstanceID: "41", Name: "1000 Yard Stare", Kind: KindWeapon},
		{InstanceID: "42", Name: "DEADHORSE 04", Kind: KindWeapon},
		{InstanceID: "43", Name: "Wendigo GL3", Kind: KindWeapon},
		{InstanceID: "44", Name: "Wendigo GL3 (Adept)", Kind: KindWeapon},
	}}

	// The last name writes a letter where Wendigo GL3 writes a digit.
	rivals := &Snapshot{Items: append(slices.Clone(numbered.Items),
		&Item{InstanceID: "45", Name: "Wendigo GLE", Kind: KindWeapon},
	)}

	tests := map[string]struct {
		err      error
		snapshot *Snapshot
		name     string
		want     []string
	}{
		"exact name with accents": {
			snapshot: accented,
			name:     "Déjà Vu",
			want:     []string{"21"},
		},
		"name without its accents beats a longer match": {
			snapshot: accented,
			name:     "deja vu",
			want:     []string{"21"},
		},
		"part of a name without its accents": {
			snapshot: accented,
			name:     "deja vu p",
			want:     []string{"22"},
		},
		"name and version suffix without accents": {
			snapshot: accented,
			name:     "crepuscule (adept)",
			want:     []string{"23"},
		},
		"group name part without accents and a missing version": {
			snapshot: accented,
			name:     "deja vu p (adept)",
			want:     []string{"22"},
		},
		"copy name part without accents": {
			snapshot: accented,
			name:     "crepuscule (ad",
			want:     []string{"23"},
		},
		"version suffix with an accent": {
			snapshot: accented,
			name:     "Déjà Vu (Ádept)",
			want:     []string{"21"},
		},
		"digits for the letters of a name": {
			snapshot: numbered,
			name:     "w3nd1g0 gl3",
			want:     []string{"43", "44"},
		},
		"letters for the digits of a name": {
			snapshot: numbered,
			name:     "iooo yard stare",
			want:     []string{"41"},
		},
		"part of a name with a digit for a letter": {
			snapshot: numbered,
			name:     "wend1g",
			want:     []string{"43", "44"},
		},
		"part of a name with a letter for a digit": {
			snapshot: numbered,
			name:     "deadhorse o",
			want:     []string{"42"},
		},
		"version suffix with a digit for a letter": {
			snapshot: numbered,
			name:     "Wendigo GL3 (Ad3pt)",
			want:     []string{"43", "44"},
		},
		"version suffix with a digit for a letter and no copy of the version": {
			snapshot: &Snapshot{Items: []*Item{
				{InstanceID: "43", Name: "Wendigo GL3", Kind: KindWeapon},
			}},
			name: "Wendigo GL3 (Ad3pt)",
			want: []string{"43"},
		},
		"bare suffix with a digit for a letter": {
			snapshot: numbered,
			name:     "(ad3pt)",
			want:     []string{"43", "44"},
		},
		"number as the name writes it": {
			snapshot: numbered,
			name:     "1000 yard",
			want:     []string{"41"},
		},
		"name in digits before a name in letters": {
			snapshot: rivals,
			name:     "wendigo gl3",
			want:     []string{"43", "44"},
		},
		"name in letters before a name in digits": {
			snapshot: rivals,
			name:     "wendigo gle",
			want:     []string{"45"},
		},
		"part of a name in digits before a name in letters": {
			snapshot: rivals,
			name:     "go gl3",
			want:     []string{"43", "44"},
		},
		"part of a name in letters before a name in digits": {
			snapshot: rivals,
			name:     "go gle",
			want:     []string{"45"},
		},
		"part of a name in letters before a whole name in digits": {
			snapshot: &Snapshot{Items: []*Item{
				{InstanceID: "51", Name: "Wendigo GL3", Kind: KindWeapon},
				{InstanceID: "52", Name: "Wendigo GLE Prime", Kind: KindWeapon},
			}},
			name: "wendigo gle",
			want: []string{"52"},
		},
		"names that differ only in a digit and a letter": {
			snapshot: rivals,
			name:     "wend1go gle",
			err:      ErrUnknownWeapon,
		},
		"exact name in any case": {
			name: "FATEBRINGER",
			want: []string{"9", "12", "4"},
		},
		"version suffix": {
			name: "fatebringer (timelost)",
			want: []string{"9", "12", "4"},
		},
		"unique substring": {
			name: "austr",
			want: []string{"6"},
		},
		"exact name beats a longer match": {
			name: "midnight coup",
			want: []string{"3"},
		},
		"exact case beats another case": {
			name: "Ace of Spades",
			want: []string{"8"},
		},
		"exact case in capitals": {
			name: "ACE OF SPADES",
			want: []string{"10"},
		},
		"names that differ only in case": {
			name: "ace of spades",
			err:  ErrUnknownWeapon,
		},
		"text of a version suffix": {
			name: "timelost",
			want: []string{"9", "12", "4"},
		},
		"bare suffix that a copy has": {
			name: "(timelost)",
			want: []string{"9", "12", "4"},
		},
		"bare suffix that no copy has": {
			name: "(adept)",
			err:  ErrUnknownWeapon,
		},
		"ambiguous substring": {
			name: "ringer",
			err:  ErrUnknownWeapon,
		},
		"armor does not count": {
			name: "helm",
			err:  ErrUnknownWeapon,
		},
		"no match": {
			name: "thorn",
			err:  ErrUnknownWeapon,
		},
		"accent marks only": {
			snapshot: single,
			name:     "^",
			err:      ErrUnknownWeapon,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := cmp.Or(tc.snapshot, groupSnapshot()).CopiesOf(tc.name)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, ids(got.Copies))
		})
	}
}

func TestCopiesOfSuggestions(t *testing.T) {
	t.Parallel()

	// Twelve weapons share a substring, which is more than the error names.
	many := &Snapshot{}
	for i := range 12 {
		many.Items = append(many.Items, &Item{
			InstanceID: strconv.Itoa(i + 1),
			Name:       fmt.Sprintf("Ringer %02d", i+1),
			Kind:       KindWeapon,
		})
	}

	tests := map[string]struct {
		s    *Snapshot
		name string
		want string
	}{
		"two matches": {
			s:    groupSnapshot(),
			name: "ringer",
			want: `unknown weapon: "ringer" matches 2 weapons, so pass a full name, such as Austringer, Fatebringer`,
		},
		"names that differ only in case": {
			s:    groupSnapshot(),
			name: "ace of spades",
			want: `unknown weapon: "ace of spades" matches 2 weapons, so pass a full name, ` +
				`such as ACE OF SPADES, Ace of Spades`,
		},
		"more matches than the error names": {
			s:    many,
			name: "ringer",
			want: `unknown weapon: "ringer" matches 12 weapons, so pass a full name, such as ` +
				`Ringer 01, Ringer 02, Ringer 03, Ringer 04, Ringer 05, ` +
				`Ringer 06, Ringer 07, Ringer 08, Ringer 09, Ringer 10`,
		},
		"bare suffix": {
			s:    groupSnapshot(),
			name: "(Adept)",
			want: `unknown weapon: no weapon in the inventory has a name that contains "(Adept)"`,
		},
		"names that differ only in a digit and a letter": {
			s: &Snapshot{Items: []*Item{
				{InstanceID: "1", Name: "Wendigo GL3", Kind: KindWeapon},
				{InstanceID: "2", Name: "Wendigo GLE", Kind: KindWeapon},
			}},
			name: "wend1go gle",
			want: `unknown weapon: "wend1go gle" matches 2 weapons, so pass a full name, ` +
				`such as Wendigo GL3, Wendigo GLE`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := tc.s.CopiesOf(tc.name)
			require.EqualError(t, err, tc.want)
		})
	}
}

func TestGroupOf(t *testing.T) {
	t.Parallel()

	s := groupSnapshot()

	tests := map[string]struct {
		id       string
		wantName string
		want     []string
	}{
		"version copy": {
			id:       "4",
			wantName: "Fatebringer",
			want:     []string{"9", "12", "4"},
		},
		"names that differ only in case": {
			id:       "8",
			wantName: "Ace of Spades",
			want:     []string{"8"},
		},
		"capitals": {
			id:       "10",
			wantName: "ACE OF SPADES",
			want:     []string{"10"},
		},
		"armor": {
			id:       "5",
			wantName: "Fatebringer Helm",
			want:     []string{"5"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			item := s.Items[slices.IndexFunc(s.Items, func(item *Item) bool { return item.InstanceID == tc.id })]

			got := s.GroupOf(item)
			assert.Equal(t, tc.wantName, got.Name)
			assert.Equal(t, tc.want, ids(got.Copies))
		})
	}
}

func TestBaseName(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		name string
		want string
	}{
		"version suffix": {
			name: "Fatebringer (Timelost)",
			want: "Fatebringer",
		},
		"suffix in another letter case and with an accent": {
			name: "fatebringer (ÁDEPT)",
			want: "fatebringer",
		},
		"suffix without a space and spaces around": {
			name: " Fatebringer(adept) ",
			want: "Fatebringer",
		},
		"suffix with a digit for a letter": {
			name: "Wendigo GL3 (Ad3pt)",
			want: "Wendigo GL3",
		},
		"parenthesis that is not a version suffix": {
			name: "Midnight Coup (Replica)",
			want: "Midnight Coup (Replica)",
		},
		"parenthesis before a version suffix": {
			name: "Midnight Coup (Replica) (Adept)",
			want: "Midnight Coup (Replica)",
		},
		"no parenthesis": {
			name: "Fatebringer",
			want: "Fatebringer",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, baseName(tc.name))
		})
	}
}

func TestItemCategories(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		categories []uint32
		want       []int64
	}{
		"no categories": {},
		"special grenade launcher": {
			categories: []uint32{1, 153950757},
			want:       []int64{1, -153950757},
		},
		"special grenade launcher keeps the order of the other categories": {
			categories: []uint32{1, 153950757, 3},
			want:       []int64{1, 3, -153950757},
		},
		"heavy grenade launcher": {
			categories: []uint32{1, 153950757, 4},
			want:       []int64{1, 153950757, 4},
		},
		"dummy grenade launcher": {
			categories: []uint32{1, 3109687656, 153950757},
			want:       []int64{1, 3109687656, 153950757},
		},
		"grenade launcher category without the weapon category": {
			categories: []uint32{153950757},
			want:       []int64{153950757},
		},
		"linear fusion rifle": {
			categories: []uint32{1, 9, 1504945536},
			want:       []int64{1, 1504945536},
		},
		"linear fusion rifle loses only the first fusion rifle category": {
			categories: []uint32{9, 1504945536, 9},
			want:       []int64{1504945536, 9},
		},
		"fusion rifle": {
			categories: []uint32{1, 9},
			want:       []int64{1, 9},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, itemCategories(tc.categories))
		})
	}
}

// instanceItem returns the item that [NewSnapshot] builds from defs for a
// held item of the definition hash in the profile bucket bucket. Instance
// is the JSON object of the instance component of the item.
func instanceItem(t *testing.T, defs *definitions.Defs, hash, bucket uint32, instance string) *Item {
	t.Helper()

	body := fmt.Sprintf(`{
		"characterInventories": {"data": {"c1": {"items": [
			{"itemHash": %d, "itemInstanceId": "1", "quantity": 1, "location": 1, "bucketHash": %d}
		]}}},
		"itemComponents": {"instances": {"data": {"1": %s}}}
	}`, hash, bucket, instance)

	var p bungie.Profile

	require.NoError(t, json.Unmarshal([]byte(body), &p))

	item, ok := NewSnapshot(&p, defs, bungietest.Now).Find("1")
	require.True(t, ok)

	return item
}

func TestNewItemElement(t *testing.T) {
	t.Parallel()

	// The definitions name the damage types of Kinetic and Arc, and they
	// lack the hash 999. Riskrunner has the default damage type Arc.
	defs := definitionstest.Defs()
	defs.DamageEnums = map[uint32]int{3373582085: bungie.DamageKinetic, 2303181850: bungie.DamageArc}
	defs.DamageTypes[bungie.DamageArc] = "Arc"
	defs.DamageTypes[bungie.DamageVoid] = "Void"
	defs.Items[110] = definitions.ItemDef{
		Name:       "Riskrunner",
		TypeName:   "Submachine Gun",
		Bucket:     bungie.BucketEnergy,
		ItemType:   bungie.ItemTypeWeapon,
		ClassType:  bungie.ClassAny,
		DamageType: bungie.DamageArc,
		Equippable: true,
	}

	tests := map[string]struct {
		instance   string
		want       int
		wantDamage string
	}{
		"damage type hash of Arc": {
			instance:   `{"damageType": 2, "damageTypeHash": 2303181850}`,
			want:       bungie.DamageArc,
			wantDamage: "Arc",
		},
		"damage type hash of Kinetic": {
			instance:   `{"damageType": 1, "damageTypeHash": 3373582085}`,
			want:       bungie.DamageKinetic,
			wantDamage: "Kinetic",
		},
		"no damage type hash": {
			instance:   `{"damageType": 0}`,
			wantDamage: "Arc",
		},
		"null damage type hash": {
			instance:   `{"damageType": 0, "damageTypeHash": null}`,
			wantDamage: "Arc",
		},
		"damage type hash that the definitions lack": {
			instance:   `{"damageType": 4, "damageTypeHash": 999}`,
			wantDamage: "Void",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			item := instanceItem(t, defs, 110, bungie.BucketEnergy, tc.instance)
			assert.Equal(t, tc.want, item.Element)
			assert.Equal(t, tc.wantDamage, item.Damage)
		})
	}
}

func TestNewItemPower(t *testing.T) {
	t.Parallel()

	defs := definitionstest.Defs()
	defs.Items[120] = definitions.ItemDef{
		Name:       "Iron Banner Artifact",
		Bucket:     bungie.BucketArtifacts,
		ClassType:  bungie.ClassAny,
		Equippable: true,
	}
	defs.Items[121] = definitions.ItemDef{
		Name:       "Voidwalker",
		Bucket:     bungie.BucketSubclass,
		ClassType:  bungie.ClassWarlock,
		Equippable: true,
	}
	defs.Items[122] = definitions.ItemDef{
		Name:                      "Lightkeeper Shell",
		Bucket:                    bungie.BucketGhost,
		ClassType:                 bungie.ClassAny,
		Equippable:                true,
		DisablePrimaryStatDisplay: true,
	}
	defs.Items[123] = definitions.ItemDef{
		Name:       "Cryptomnesia",
		TypeName:   "Vehicle",
		Bucket:     bungie.BucketVehicle,
		ClassType:  bungie.ClassAny,
		Equippable: true,
	}

	tests := map[string]struct {
		instance string
		hash     uint32
		bucket   uint32
		want     int
	}{
		"Power": {
			instance: `{"primaryStat": {"statHash": 1935470627, "value": 1900}}`,
			hash:     100,
			bucket:   bungie.BucketKinetic,
			want:     1900,
		},
		"Attack": {
			instance: `{"primaryStat": {"statHash": 1480404414, "value": 1890}}`,
			hash:     100,
			bucket:   bungie.BucketKinetic,
			want:     1890,
		},
		"Defense": {
			instance: `{"primaryStat": {"statHash": 3897883278, "value": 1880}}`,
			hash:     200,
			bucket:   bungie.BucketHelmet,
			want:     1880,
		},
		"Attack of Destiny 1": {
			instance: `{"primaryStat": {"statHash": 368428387, "value": 400}}`,
			hash:     100,
			bucket:   bungie.BucketKinetic,
			want:     400,
		},
		"another stat": {
			instance: `{"primaryStat": {"statHash": 9, "value": 2010}}`,
			hash:     100,
			bucket:   bungie.BucketKinetic,
		},
		"speed of a vehicle": {
			instance: `{"primaryStat": {"statHash": 1501155019, "value": 190}}`,
			hash:     123,
			bucket:   bungie.BucketVehicle,
		},
		"no primary stat": {
			instance: `{"damageType": 1}`,
			hash:     100,
			bucket:   bungie.BucketKinetic,
		},
		"Artifacts bucket": {
			instance: `{"primaryStat": {"statHash": 1935470627, "value": 50}}`,
			hash:     120,
			bucket:   bungie.BucketArtifacts,
		},
		"Subclass bucket": {
			instance: `{"primaryStat": {"statHash": 1935470627, "value": 1900}}`,
			hash:     121,
			bucket:   bungie.BucketSubclass,
		},
		"negative primary stat of a subclass": {
			instance: `{"primaryStat": {"statHash": 1935470627, "value": -10}}`,
			hash:     121,
			bucket:   bungie.BucketSubclass,
		},
		"hidden primary stat": {
			instance: `{"primaryStat": {"statHash": 1935470627, "value": 1900}}`,
			hash:     122,
			bucket:   bungie.BucketGhost,
		},
		"Subclass definition in another profile bucket": {
			instance: `{"primaryStat": {"statHash": 1935470627, "value": 1900}}`,
			hash:     121,
			bucket:   bungie.BucketKinetic,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			item := instanceItem(t, defs, tc.hash, tc.bucket, tc.instance)
			assert.Equal(t, tc.want, item.Power)
		})
	}
}

func TestAnnotateTagRule(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		rows    []dvm.ItemTag
		wantTag string
		want    string
	}{
		"keep row with a rule id": {
			rows:    []dvm.ItemTag{{InstanceID: "1001", TagID: "keep", RuleID: "rule-1"}},
			wantTag: "keep",
			want:    "rule-1",
		},
		"keep row without a rule id": {
			rows:    []dvm.ItemTag{{InstanceID: "1001", TagID: "keep"}},
			wantTag: "keep",
		},
		"removed row with a rule id": {
			rows: []dvm.ItemTag{{InstanceID: "1001", TagID: "keep", RuleID: "rule-1", Removed: true}},
		},
		"tag outside the tags of the web client": {
			rows:    []dvm.ItemTag{{InstanceID: "1001", TagID: "wishlist", RuleID: "rule-1"}},
			wantTag: "wishlist",
		},
		"rule of a tag that the item does not show": {
			rows: []dvm.ItemTag{
				{InstanceID: "1001", TagID: "junk", RuleID: "rule-1"},
				{InstanceID: "1001", TagID: "keep"},
			},
			wantTag: "keep",
		},
		"rule of the favorite mark": {
			rows: []dvm.ItemTag{
				{InstanceID: "1001", TagID: dvm.TagFavorite, RuleID: "rule-1"},
				{InstanceID: "1001", TagID: "keep"},
			},
			wantTag: "keep",
		},
		"rule of another item": {
			rows: []dvm.ItemTag{
				{InstanceID: "1002", TagID: "keep", RuleID: "rule-1"},
				{InstanceID: "1001", TagID: "keep"},
			},
			wantTag: "keep",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := fixtureSnapshot(t)

			// An earlier annotation sets a rule, which the next one
			// replaces.
			s.Annotate([]dvm.ItemTag{{InstanceID: "1001", TagID: "keep", RuleID: "rule-0"}}, nil)
			s.Annotate(tc.rows, nil)

			item, ok := s.Find("1001")
			require.True(t, ok)
			assert.Equal(t, tc.wantTag, item.Tag)
			assert.Equal(t, tc.want, item.TagRule)
		})
	}
}

func TestNewSnapshotNames(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		lookups map[uint32]definitions.Lookup
		want    string
		hash    uint32
	}{
		"item definition": {
			hash: 300,
			want: "Enhancement Core",
		},
		"withheld item that a vendor sells": {
			hash: 501,
			want: "Legendary Shards",
		},
		"withheld reward of an activity": {
			hash: 600,
			want: "Raid Gear",
		},
		"withheld item that no table names": {
			hash: definitionstest.ItemWithheld,
			want: "Unknown item 78",
		},
		"item that the manifest version lacks": {
			hash: 77,
			want: "Unknown item 77",
		},
		"withheld item that a lookup at Bungie names": {
			hash:    definitionstest.ItemWithheld,
			lookups: map[uint32]definitions.Lookup{78: {Name: "Wish-Keeper Snare Refit", Defined: true}},
			want:    "Wish-Keeper Snare Refit",
		},
		"item of a later manifest version that a lookup at Bungie names": {
			hash:    77,
			lookups: map[uint32]definitions.Lookup{77: {Name: "Strange Coin", Defined: true}},
			want:    "Strange Coin",
		},
		"item that Bungie classifies": {
			hash:    77,
			lookups: map[uint32]definitions.Lookup{77: {Defined: true, Redacted: true}},
			want:    "Unknown item 77",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			defs := definitionstest.Defs()
			defs.Lookups = tc.lookups

			body := fmt.Sprintf(`{
				"profileCurrencies": {"data": {"items": [{"itemHash": %[1]d, "quantity": 3}]}},
				"characterInventories": {"data": {"c1": {"items": [
					{"itemHash": %[1]d, "quantity": 2, "location": 1, "bucketHash": %[2]d}
				]}}}
			}`, tc.hash, bungie.BucketPower)

			var p bungie.Profile

			require.NoError(t, json.Unmarshal([]byte(body), &p))

			s := NewSnapshot(&p, defs, bungietest.Now)

			require.Len(t, s.Items, 1)
			assert.Equal(t, tc.want, s.Items[0].Name)
			assert.Equal(t, []Currency{{Name: tc.want, Quantity: 3}}, s.Currencies)
			assert.Equal(t, tc.want, ItemName(defs, tc.hash))
		})
	}
}
