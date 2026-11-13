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
	assert.Equal(t, hunterID, s.Characters[0].ID, "most recently played character comes first")
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
		ClassType:  classAny,
		Power:      2010,
		Quantity:   1,
		Bucket:     bucketKinetic,
		Perks: []Perk{
			{Name: "Explosive Payload", Type: "Trait", Options: []string{"Rampage"}},
			{
				Name:        "Firefly",
				Type:        "Trait",
				Description: "Precision final blows cause targets to explode.\n\n Explosions deal Solar damage. ",
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

func TestSlotBuckets(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		defs *Defs
		want []Bucket
	}{
		"equipment before items": {
			defs: fixtureDefs(),
			want: []Bucket{
				{Name: "Vault", Hash: bucketVault, Capacity: 700, Account: true},
				{Name: "Kinetic Weapons", Hash: bucketKinetic, Capacity: 10},
				{Name: "Power Weapons", Hash: bucketPower, Capacity: 10},
				{Name: "Helmet", Hash: bucketHelmet, Capacity: 10},
				{Name: "Consumables", Hash: bucketConsumables, Capacity: 50, Account: true},
				{Name: "Engrams", Hash: bucketEngrams, Capacity: 10},
				{Name: "Postmaster", Hash: bucketPostmaster, Capacity: 21},
			},
		},
		"unnamed buckets and the Postmaster stay out of the middle": {
			defs: &Defs{Buckets: map[uint32]BucketDef{
				bucketVault: {
					Name:     "General",
					Capacity: 700,
					Category: bucketCategoryItem,
					Order:    30,
					Account:  true,
				},
				bucketPostmaster: {Name: "Lost Items", Capacity: 21, Category: bucketCategoryItem, Order: 10},
				bucketKinetic: {
					Name:     "Kinetic Weapons",
					Capacity: 10,
					Category: bucketCategoryEquippable,
					Order:    20,
				},
				3796357825: {Capacity: 10, Category: bucketCategoryEquippable, Order: 120},
			}},
			want: []Bucket{
				{Name: "Vault", Hash: bucketVault, Capacity: 700, Account: true},
				{Name: "Kinetic Weapons", Hash: bucketKinetic, Capacity: 10},
				{Name: "Postmaster", Hash: bucketPostmaster, Capacity: 21},
			},
		},
		"definitions without buckets": {
			defs: &Defs{},
			want: []Bucket{
				{Name: "Vault", Hash: bucketVault, Account: true},
				{Name: "Postmaster", Hash: bucketPostmaster},
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
			bucket: bucketKinetic,
			owner:  hunterID,
			want:   2,
		},
		"another character": {
			bucket: bucketKinetic,
			owner:  titanID,
		},
		"postmaster": {
			bucket: bucketPostmaster,
			owner:  hunterID,
			want:   1,
		},
		"messages take no Postmaster slot": {
			extra: []ProfileItem{
				{Hash: 300, Quantity: 1, Location: itemLocationPostmaster, BucketHash: bucketMessages},
				{Hash: 300, Quantity: 2, Location: itemLocationPostmaster, BucketHash: bucketMessages},
			},
			bucket: bucketPostmaster,
			owner:  hunterID,
			want:   1,
		},
		"vault ignores the owner": {
			bucket: bucketVault,
			owner:  hunterID,
			want:   2,
		},
		"account bucket counts stacks": {
			bucket: bucketConsumables,
			owner:  hunterID,
			want:   1,
		},
		"move to the vault frees the slot": {
			move:   "1003",
			target: ownerVault,
			bucket: bucketKinetic,
			owner:  hunterID,
			want:   1,
		},
		"move to the vault takes a vault slot": {
			move:   "1003",
			target: ownerVault,
			bucket: bucketVault,
			want:   3,
		},
		"pull frees the Postmaster slot": {
			move:   "1005",
			target: hunterID,
			bucket: bucketPostmaster,
			owner:  hunterID,
		},
		"pulled item takes a slot of its bucket": {
			move:   "1005",
			target: hunterID,
			bucket: bucketKinetic,
			owner:  hunterID,
			want:   3,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var profile Profile

			require.NoError(t, json.Unmarshal([]byte(fixtureProfile), &profile))

			hunter := profile.CharacterInventories.Data[hunterID]
			hunter.Items = append(hunter.Items, tc.extra...)
			profile.CharacterInventories.Data[hunterID] = hunter

			s := NewSnapshot(&profile, fixtureDefs(), fixtureNow)

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
		"unique class":     {name: "Hunter", want: hunterID},
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

	require.NoError(t, json.Unmarshal([]byte(fixtureProfile), &profile))

	titan := profile.Characters.Data[titanID]
	titan.LastPlayed = fixtureNow
	profile.Characters.Data[titanID] = titan

	titanLast := NewSnapshot(&profile, fixtureDefs(), fixtureNow)

	tests := map[string]struct {
		err      error
		snapshot *Snapshot
		name     string
		want     string
	}{
		"character id":                {snapshot: s, name: "c2", want: titanID},
		"unique class":                {snapshot: s, name: "HUNTER", want: hunterID},
		"most recently played hunter": {snapshot: s, name: "", want: hunterID},
		"most recently played titan":  {snapshot: titanLast, name: "", want: titanID},
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
