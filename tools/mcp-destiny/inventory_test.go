package main

import (
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
	assert.Equal(t, "Kinetic Weapons", lost.Slot, "a Postmaster item takes the slot of its definition")
	assert.Equal(t, "Hunter (postmaster)", s.LocationLabel(lost))
	assert.True(t, lost.Transferable, "the transfer status of a Postmaster item does not block a move")
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
