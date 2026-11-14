package main

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
		Owner:      ownerVault,
		Kind:       kindWeapon,
		ClassType:  bungie.ClassAny,
		Power:      2010,
		Quantity:   1,
		Bucket:     bungie.BucketKinetic,
		Perks: []Perk{
			{
				Plug:    Plug{Name: "Explosive Payload", Type: "Trait", Hash: 900},
				Options: []Plug{{Name: "Rampage", Type: "Trait", Hash: 903}},
				plugs: []Plug{
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
		Stats:        []Stat{{Name: "Range", Value: 55}},
		Locked:       true,
		Masterwork:   true,
		Transferable: true,
		Equippable:   true,
		Lockable:     true,
	}, vaulted)

	stacks, err := s.Filter(ItemFilter{Kind: kindOther})
	require.NoError(t, err)
	require.Len(t, stacks, 1)
	assert.False(t, stacks[0].Lockable, "a stack is not lockable")
	assert.Equal(t, ownerAccount, stacks[0].Owner, "a Consumables stack at the vault location belongs to the account")

	helmet, ok := s.Find("1004")
	require.True(t, ok)
	assert.Equal(t, kindArmor, helmet.Kind)
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
		defs *Defs
		want []Bucket
	}{
		"equipment before items": {
			defs: fixtureDefs(),
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
			defs: &Defs{Buckets: map[uint32]BucketDef{
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
			defs: &Defs{},
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
		extra  []ProfileItem
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
			extra: []ProfileItem{
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
			target: ownerVault,
			bucket: bungie.BucketKinetic,
			owner:  bungietest.HunterID,
			want:   1,
		},
		"move to the vault takes a vault slot": {
			move:   "1003",
			target: ownerVault,
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

			var profile Profile

			require.NoError(t, json.Unmarshal([]byte(bungietest.ProfileJSON), &profile))

			hunter := profile.CharacterInventories.Data[bungietest.HunterID]
			hunter.Items = append(hunter.Items, tc.extra...)
			profile.CharacterInventories.Data[bungietest.HunterID] = hunter

			s := NewSnapshot(&profile, fixtureDefs(), bungietest.Now)

			if tc.move != "" {
				item, ok := s.Find(tc.move)
				require.True(t, ok)

				_, err := s.Move(t.Context(), &stepMover{}, item, tc.target, false)
				require.NoError(t, err)
			}

			i := slices.IndexFunc(s.Buckets, func(b Bucket) bool { return b.Hash == tc.bucket })
			require.GreaterOrEqual(t, i, 0)
			assert.Equal(t, tc.want, s.SlotsUsed(s.Buckets[i], tc.owner))
		})
	}
}

func TestAnnotate(t *testing.T) {
	t.Parallel()

	s := fixtureSnapshot(t)

	s.Annotate(
		[]ItemTag{
			{InstanceID: "1001", TagID: "junk"},
			{InstanceID: "1001", TagID: "keep"},
			{InstanceID: "1001", TagID: tagFavorite},
			{InstanceID: "1002", TagID: "pvp", Removed: true},
			{InstanceID: "1003", TagID: tagFavorite},
		},
		[]Note{
			{Kind: noteInstance, TargetID: "1001", Body: "god roll"},
			{Kind: noteInstance, TargetID: "1002", Body: "gone", Removed: true},
			{Kind: noteSet, TargetID: "1003", Body: "not an instance note"},
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
	s.Annotate([]ItemTag{
		{InstanceID: "1001", TagID: "keep"},
		{InstanceID: "1002", TagID: tagFavorite},
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
			filter: ItemFilter{Kind: kindAll, Query: "enhancement"},
			want:   []string{""},
		},
		"armor": {
			filter: ItemFilter{Kind: kindArmor},
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
		"vault": {
			filter: ItemFilter{Location: "Vault"},
			want:   []string{"1001", "1002"},
		},
		"class name as location": {
			filter: ItemFilter{Location: "hunter", Sort: sortNewest},
			want:   []string{"1006", "1005", "1004", "1003"},
		},
		"equipped": {
			filter: ItemFilter{Location: locationEquipped},
			want:   []string{"1004", "1006"},
		},
		"postmaster": {
			filter: ItemFilter{Location: locationPostmaster},
			want:   []string{"1005"},
		},
		"any character": {
			filter: ItemFilter{Location: locationCharacters, Kind: kindWeapon},
			want:   []string{"1005", "1006", "1003"},
		},
		"tag": {
			filter: ItemFilter{Tag: "keep"},
			want:   []string{"1001"},
		},
		"untagged weapons": {
			filter: ItemFilter{Tag: tagNone, Kind: kindWeapon, Location: "vault"},
			want:   []string{"1002"},
		},
		"tagged": {
			filter: ItemFilter{Tag: tagAny},
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
			filter: ItemFilter{MinPower: 1990, MaxPower: 2005, Sort: sortPower},
			want:   []string{"1003", "1004", "1002"},
		},
		"unknown kind": {
			filter: ItemFilter{Kind: "ghost"},
			err:    ErrInvalidFilter,
		},
		"unknown sort": {
			filter: ItemFilter{Sort: "random"},
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
			Kind:       kindArmor,
			Stats:      []Stat{{Name: "Grenade", Value: grenade}, {Name: "Health", Value: health}},
		}
	}

	// The items that tie on a total or a stat are out of name order. The
	// tie-breaks of the total sort and the stat sort put them in name order.
	s := &Snapshot{Items: []*Item{
		helmet("13", 20, 20),
		helmet("12", 10, 40),
		helmet("11", 30, 10),
		{InstanceID: "15", Name: "Bare Helmet", Kind: kindArmor},
		{InstanceID: "14", Name: "Ace of Spades", Kind: kindWeapon, Stats: []Stat{{Name: "Range", Value: 60}}},
	}}

	// The copies of one name are out of instance id order.
	twins := &Snapshot{Items: []*Item{
		{InstanceID: "31", Name: "Twin Helmet", Kind: kindArmor, Stats: []Stat{{Name: "Health", Value: 10}}},
		{InstanceID: "30", Name: "Twin Helmet", Kind: kindArmor, Stats: []Stat{{Name: "Health", Value: 10}}},
	}}

	// The name order of the tied items is the reverse of their instance id
	// order, so a tie-break on the instance id alone fails.
	rivals := &Snapshot{Items: []*Item{
		{InstanceID: "40", Name: "Beta Helmet", Kind: kindArmor, Stats: []Stat{{Name: "Health", Value: 10}}},
		{InstanceID: "41", Name: "Alpha Helmet", Kind: kindArmor, Stats: []Stat{{Name: "Health", Value: 10}}},
	}}

	tests := map[string]struct {
		snapshot *Snapshot
		err      error
		msg      string
		filter   ItemFilter
		want     []string
	}{
		"total sort puts items without a total last": {
			filter: ItemFilter{Sort: sortTotal},
			want:   []string{"12", "11", "13", "14", "15"},
		},
		"total sort puts a zero total ahead of items without a total": {
			snapshot: &Snapshot{Items: []*Item{
				{InstanceID: "19", Name: "Ace of Spades", Kind: kindWeapon, Stats: []Stat{{Name: "Range", Value: 60}}},
				{InstanceID: "20", Name: "Bare Helmet", Kind: kindArmor},
				{
					InstanceID: "21",
					Name:       "Zero Helmet",
					Kind:       kindArmor,
					Stats:      []Stat{{Name: "Grenade", Value: 0}, {Name: "Health", Value: 0}},
				},
			}},
			filter: ItemFilter{Sort: sortTotal},
			want:   []string{"21", "19", "20"},
		},
		"total sort orders copies of one name by instance id": {
			snapshot: twins,
			filter:   ItemFilter{Sort: sortTotal},
			want:     []string{"30", "31"},
		},
		"total sort orders a tie by name ahead of instance id": {
			snapshot: rivals,
			filter:   ItemFilter{Sort: sortTotal},
			want:     []string{"41", "40"},
		},
		"minimum total is inclusive and selects only armor": {
			filter: ItemFilter{MinTotal: 40},
			want:   []string{"11", "12", "13"},
		},
		"stat sort puts items without the stat last": {
			filter: ItemFilter{Stat: "health", Sort: sortStat},
			want:   []string{"11", "13", "12", "14", "15"},
		},
		"stat sort puts a zero value ahead of items without the stat": {
			snapshot: &Snapshot{Items: []*Item{
				{InstanceID: "17", Name: "Ace of Spades", Kind: kindWeapon, Stats: []Stat{{Name: "Range", Value: 60}}},
				{InstanceID: "18", Name: "Zero Helmet", Kind: kindArmor, Stats: []Stat{{Name: "Health", Value: 0}}},
			}},
			filter: ItemFilter{Stat: "health", Sort: sortStat},
			want:   []string{"18", "17"},
		},
		"stat sort orders copies of one name by instance id": {
			snapshot: twins,
			filter:   ItemFilter{Stat: "health", Sort: sortStat},
			want:     []string{"30", "31"},
		},
		"stat sort orders a tie by name ahead of instance id": {
			snapshot: rivals,
			filter:   ItemFilter{Stat: "health", Sort: sortStat},
			want:     []string{"41", "40"},
		},
		"minimum stat is inclusive": {
			filter: ItemFilter{Stat: "HEALTH", MinStat: 20},
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
			snapshot: &Snapshot{Items: []*Item{{InstanceID: "16", Kind: kindArmor}}},
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
			filter: ItemFilter{Sort: sortStat},
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

func TestResolveOwner(t *testing.T) {
	t.Parallel()

	s := fixtureSnapshot(t)
	s.Characters = append(s.Characters, Character{ID: "c3", Class: "Titan"})

	tests := map[string]struct {
		err  error
		name string
		want string
	}{
		"vault":            {name: "VAULT", want: ownerVault},
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
	var profile Profile

	require.NoError(t, json.Unmarshal([]byte(bungietest.ProfileJSON), &profile))

	titan := profile.Characters.Data[bungietest.TitanID]
	titan.LastPlayed = bungietest.Now
	profile.Characters.Data[bungietest.TitanID] = titan

	titanLast := NewSnapshot(&profile, fixtureDefs(), bungietest.Now)

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
		{InstanceID: "12", Hash: 110, Name: "Fatebringer", Kind: kindWeapon},
		{InstanceID: "3", Hash: 102, Name: "Midnight Coup", Kind: kindWeapon},
		{InstanceID: "9", Hash: 100, Name: "Fatebringer", Kind: kindWeapon},
		{InstanceID: "4", Hash: 120, Name: "Fatebringer (Timelost)", Kind: kindWeapon},
		{InstanceID: "5", Hash: 200, Name: "Fatebringer Helm", Kind: kindArmor},
		{Hash: 300, Name: "Enhancement Core", Kind: kindOther},
		{InstanceID: "6", Hash: 130, Name: "Austringer", Kind: kindWeapon},
		{InstanceID: "7", Hash: 140, Name: "Midnight Coup Replica", Kind: kindWeapon},
		{InstanceID: "8", Hash: 150, Name: "Ace of Spades", Kind: kindWeapon},
		{InstanceID: "10", Hash: 160, Name: "ACE OF SPADES", Kind: kindWeapon},
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

	s := groupSnapshot()

	tests := map[string]struct {
		err  error
		name string
		want []string
	}{
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
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := s.CopiesOf(tc.name)
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
			Kind:       kindWeapon,
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
func instanceItem(t *testing.T, defs *Defs, hash, bucket uint32, instance string) *Item {
	t.Helper()

	body := fmt.Sprintf(`{
		"characterInventories": {"data": {"c1": {"items": [
			{"itemHash": %d, "itemInstanceId": "1", "quantity": 1, "location": 1, "bucketHash": %d}
		]}}},
		"itemComponents": {"instances": {"data": {"1": %s}}}
	}`, hash, bucket, instance)

	var p Profile

	require.NoError(t, json.Unmarshal([]byte(body), &p))

	item, ok := NewSnapshot(&p, defs, bungietest.Now).Find("1")
	require.True(t, ok)

	return item
}

func TestNewItemElement(t *testing.T) {
	t.Parallel()

	// The definitions name the damage types of Kinetic and Arc, and they
	// lack the hash 999. Riskrunner has the default damage type Arc.
	defs := fixtureDefs()
	defs.DamageEnums = map[uint32]int{3373582085: bungie.DamageKinetic, 2303181850: bungie.DamageArc}
	defs.DamageTypes[bungie.DamageArc] = "Arc"
	defs.DamageTypes[bungie.DamageVoid] = "Void"
	defs.Items[110] = ItemDef{
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

func TestNewItemLightPower(t *testing.T) {
	t.Parallel()

	defs := fixtureDefs()
	defs.Items[120] = ItemDef{
		Name:       "Iron Banner Artifact",
		Bucket:     bungie.BucketArtifacts,
		ClassType:  bungie.ClassAny,
		Equippable: true,
	}
	defs.Items[121] = ItemDef{
		Name:       "Voidwalker",
		Bucket:     bungie.BucketSubclass,
		ClassType:  bungie.ClassWarlock,
		Equippable: true,
	}
	defs.Items[122] = ItemDef{
		Name:                      "Lightkeeper Shell",
		Bucket:                    bungie.BucketGhost,
		ClassType:                 bungie.ClassAny,
		Equippable:                true,
		DisablePrimaryStatDisplay: true,
	}

	tests := map[string]struct {
		instance  string
		hash      uint32
		bucket    uint32
		want      int
		wantPower int
	}{
		"Power": {
			instance:  `{"primaryStat": {"statHash": 1935470627, "value": 1900}}`,
			hash:      100,
			bucket:    bungie.BucketKinetic,
			want:      1900,
			wantPower: 1900,
		},
		"Attack": {
			instance:  `{"primaryStat": {"statHash": 1480404414, "value": 1890}}`,
			hash:      100,
			bucket:    bungie.BucketKinetic,
			want:      1890,
			wantPower: 1890,
		},
		"Defense": {
			instance:  `{"primaryStat": {"statHash": 3897883278, "value": 1880}}`,
			hash:      200,
			bucket:    bungie.BucketHelmet,
			want:      1880,
			wantPower: 1880,
		},
		"Attack of Destiny 1": {
			instance:  `{"primaryStat": {"statHash": 368428387, "value": 400}}`,
			hash:      100,
			bucket:    bungie.BucketKinetic,
			want:      400,
			wantPower: 400,
		},
		"another stat": {
			instance:  `{"primaryStat": {"statHash": 9, "value": 2010}}`,
			hash:      100,
			bucket:    bungie.BucketKinetic,
			wantPower: 2010,
		},
		"no primary stat": {
			instance: `{"damageType": 1}`,
			hash:     100,
			bucket:   bungie.BucketKinetic,
		},
		"Artifacts bucket": {
			instance:  `{"primaryStat": {"statHash": 1935470627, "value": 50}}`,
			hash:      120,
			bucket:    bungie.BucketArtifacts,
			wantPower: 50,
		},
		"Subclass bucket": {
			instance:  `{"primaryStat": {"statHash": 1935470627, "value": 1900}}`,
			hash:      121,
			bucket:    bungie.BucketSubclass,
			wantPower: 1900,
		},
		"hidden primary stat": {
			instance:  `{"primaryStat": {"statHash": 1935470627, "value": 1900}}`,
			hash:      122,
			bucket:    bungie.BucketGhost,
			wantPower: 1900,
		},
		"Subclass definition in another profile bucket": {
			instance:  `{"primaryStat": {"statHash": 1935470627, "value": 1900}}`,
			hash:      121,
			bucket:    bungie.BucketKinetic,
			wantPower: 1900,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			item := instanceItem(t, defs, tc.hash, tc.bucket, tc.instance)
			assert.Equal(t, tc.want, item.LightPower)
			assert.Equal(t, tc.wantPower, item.Power)
		})
	}
}

func TestAnnotateTagRule(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		rows    []ItemTag
		wantTag string
		want    string
	}{
		"keep row with a rule id": {
			rows:    []ItemTag{{InstanceID: "1001", TagID: "keep", RuleID: "rule-1"}},
			wantTag: "keep",
			want:    "rule-1",
		},
		"keep row without a rule id": {
			rows:    []ItemTag{{InstanceID: "1001", TagID: "keep"}},
			wantTag: "keep",
		},
		"removed row with a rule id": {
			rows: []ItemTag{{InstanceID: "1001", TagID: "keep", RuleID: "rule-1", Removed: true}},
		},
		"tag outside the tags of the web client": {
			rows:    []ItemTag{{InstanceID: "1001", TagID: "wishlist", RuleID: "rule-1"}},
			wantTag: "wishlist",
		},
		"rule of a tag that the item does not show": {
			rows: []ItemTag{
				{InstanceID: "1001", TagID: "junk", RuleID: "rule-1"},
				{InstanceID: "1001", TagID: "keep"},
			},
			wantTag: "keep",
		},
		"rule of the favorite mark": {
			rows: []ItemTag{
				{InstanceID: "1001", TagID: tagFavorite, RuleID: "rule-1"},
				{InstanceID: "1001", TagID: "keep"},
			},
			wantTag: "keep",
		},
		"rule of another item": {
			rows: []ItemTag{
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
			s.Annotate([]ItemTag{{InstanceID: "1001", TagID: "keep", RuleID: "rule-0"}}, nil)
			s.Annotate(tc.rows, nil)

			item, ok := s.Find("1001")
			require.True(t, ok)
			assert.Equal(t, tc.wantTag, item.Tag)
			assert.Equal(t, tc.want, item.TagRule)
		})
	}
}
