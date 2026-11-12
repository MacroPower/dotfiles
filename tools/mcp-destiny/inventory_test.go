package main

import (
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
