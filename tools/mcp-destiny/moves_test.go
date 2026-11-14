package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errStep = errors.New("step refused")

// stepMover records the Bungie actions of a move as short strings. It
// refuses the action named by fail.
type stepMover struct {
	fail  string
	steps []string
}

func (m *stepMover) step(name string) error {
	if name == m.fail {
		return errStep
	}

	m.steps = append(m.steps, name)

	return nil
}

func (m *stepMover) Transfer(_ context.Context, action ItemAction, toVault bool) error {
	name := "from vault to " + action.CharacterID
	if toVault {
		name = "to vault from " + action.CharacterID
	}

	if action.InstanceID == stackInstanceID {
		name += fmt.Sprintf(" x%d", action.StackSize)
	}

	return m.step(name)
}

func (m *stepMover) Equip(_ context.Context, action ItemAction) error {
	return m.step("equip " + action.InstanceID + " on " + action.CharacterID)
}

func (m *stepMover) PullFromPostmaster(_ context.Context, action ItemAction) error {
	return m.step("pull to " + action.CharacterID)
}

func (m *stepMover) SetLockState(_ context.Context, action ItemAction, locked bool) error {
	name := "unlock "
	if locked {
		name = "lock "
	}

	return m.step(name + action.InstanceID + " on " + action.CharacterID)
}

func TestMove(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err             error
		id              string
		target          string
		fail            string
		wantOwner       string
		wantReplacement string
		want            []string
		equip           bool
		wantEquip       bool
	}{
		"vault to character": {
			id:        "1001",
			target:    titanID,
			want:      []string{"from vault to c2"},
			wantOwner: titanID,
		},
		"character to vault": {
			id:        "1003",
			target:    ownerVault,
			want:      []string{"to vault from c1"},
			wantOwner: ownerVault,
		},
		"character to character passes through the vault": {
			id:        "1003",
			target:    titanID,
			want:      []string{"to vault from c1", "from vault to c2"},
			wantOwner: titanID,
		},
		"already at the target": {
			id:        "1001",
			target:    ownerVault,
			wantOwner: ownerVault,
		},
		"postmaster to vault pulls first": {
			id:        "1005",
			target:    ownerVault,
			want:      []string{"pull to c1", "to vault from c1"},
			wantOwner: ownerVault,
		},
		"equip from the vault": {
			id:        "1001",
			target:    hunterID,
			equip:     true,
			want:      []string{"from vault to c1", "equip 1001 on c1"},
			wantOwner: hunterID,
			wantEquip: true,
		},
		"equip an item already equipped": {
			id:        "1006",
			target:    hunterID,
			equip:     true,
			wantOwner: hunterID,
			wantEquip: true,
		},
		"second transfer fails": {
			id:        "1003",
			target:    titanID,
			fail:      "from vault to c2",
			err:       errStep,
			want:      []string{"to vault from c1"},
			wantOwner: ownerVault,
		},
		"equipped item transfers after a replacement takes its slot": {
			id:              "1006",
			target:          ownerVault,
			want:            []string{"equip 1003 on c1", "to vault from c1"},
			wantOwner:       ownerVault,
			wantReplacement: "1003",
		},
		"equipped item without a replacement stays": {
			id:        "1004",
			target:    ownerVault,
			err:       ErrMove,
			wantOwner: hunterID,
			wantEquip: true,
		},
		"replacement equip fails": {
			id:        "1006",
			target:    ownerVault,
			fail:      "equip 1003 on c1",
			err:       errStep,
			wantOwner: hunterID,
			wantEquip: true,
		},
		"transfer fails after the replacement": {
			id:              "1006",
			target:          ownerVault,
			fail:            "to vault from c1",
			err:             errStep,
			want:            []string{"equip 1003 on c1"},
			wantOwner:       hunterID,
			wantReplacement: "1003",
		},
		"vault cannot equip": {
			id:        "1001",
			target:    ownerVault,
			equip:     true,
			err:       ErrMove,
			wantOwner: ownerVault,
		},
		"wrong class cannot equip": {
			id:        "1004",
			target:    titanID,
			equip:     true,
			err:       ErrMove,
			wantOwner: hunterID,
			wantEquip: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := fixtureSnapshot(t)
			mover := &stepMover{fail: tc.fail}

			item, ok := s.Find(tc.id)
			require.True(t, ok)

			replacement, err := s.Move(t.Context(), mover, item, tc.target, tc.equip)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tc.want, mover.steps)
			assert.Equal(t, tc.wantOwner, item.Owner)
			assert.Equal(t, tc.wantEquip, item.Equipped)
			assert.False(t, item.Postmaster && err == nil)

			if tc.wantReplacement == "" {
				assert.Nil(t, replacement)

				return
			}

			require.NotNil(t, replacement)
			assert.Equal(t, tc.wantReplacement, replacement.InstanceID)
			assert.True(t, replacement.Equipped)
		})
	}
}

func TestMoveEquipReplacesTheSlot(t *testing.T) {
	t.Parallel()

	s := fixtureSnapshot(t)

	item, ok := s.Find("1003")
	require.True(t, ok)

	_, err := s.Move(t.Context(), &stepMover{}, item, hunterID, true)
	require.NoError(t, err)

	previous, ok := s.Find("1006")
	require.True(t, ok)
	assert.False(t, previous.Equipped)
	assert.True(t, item.Equipped)

	helmet, ok := s.Find("1004")
	require.True(t, ok)
	assert.True(t, helmet.Equipped, "another slot keeps its item")
}

func TestMoveRejectsStacks(t *testing.T) {
	t.Parallel()

	s := fixtureSnapshot(t)

	stacks, err := s.Filter(ItemFilter{Kind: kindOther})
	require.NoError(t, err)
	require.Len(t, stacks, 1)

	_, err = s.Move(t.Context(), &stepMover{}, stacks[0], ownerVault, false)
	require.ErrorIs(t, err, ErrMove)
	require.EqualError(t, err, "cannot move: Enhancement Core is a stack, which the move_stack tool moves")
}

func TestMoveRejectsAccountItems(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		target string
	}{
		"to a character": {target: hunterID},
		"to the vault":   {target: ownerVault},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var profile Profile

			require.NoError(t, json.Unmarshal([]byte(fixtureProfile), &profile))

			// Bungie places some instanced items in account-wide buckets.
			profile.ProfileInventory.Data.Items = append(profile.ProfileInventory.Data.Items, ProfileItem{
				InstanceID:     "2001",
				Quantity:       1,
				Location:       2,
				TransferStatus: 2,
				Hash:           300,
				BucketHash:     bucketConsumables,
			})

			s := NewSnapshot(&profile, fixtureDefs(), fixtureNow)

			item, ok := s.Find("2001")
			require.True(t, ok)
			require.Equal(t, ownerAccount, item.Owner)

			mover := &stepMover{}

			_, err := s.Move(t.Context(), mover, item, tc.target, false)
			require.ErrorIs(t, err, ErrMove)
			require.EqualError(
				t,
				err,
				"cannot move: Enhancement Core sits in an account-wide bucket, and move_items moves only character and vault items",
			)
			assert.Empty(t, mover.steps)
			assert.Equal(t, ownerAccount, item.Owner)
		})
	}
}

func TestReplacementFor(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		departingTier string
		want          string
		spareGone     bool
	}{
		"legendary prefers the legendary spare": {
			departingTier: "Legendary",
			want:          "1003",
		},
		"legendary never takes an exotic": {
			departingTier: "Legendary",
			spareGone:     true,
		},
		"exotic prefers the legendary spare over higher power": {
			departingTier: tierExotic,
			want:          "1003",
		},
		"exotic takes an exotic when nothing else fits": {
			departingTier: tierExotic,
			spareGone:     true,
			want:          "2000",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := fixtureSnapshot(t)

			departing, ok := s.Find("1006")
			require.True(t, ok)

			departing.Tier = tc.departingTier

			s.add(&Item{
				InstanceID: "2000",
				Name:       "Hawkmoon",
				Tier:       tierExotic,
				Slot:       departing.Slot,
				Owner:      hunterID,
				ClassType:  classAny,
				Power:      2100,
				Equippable: true,
			})

			// These items never qualify: another slot, another character,
			// and the wrong class.
			s.add(
				&Item{
					InstanceID: "2001",
					Slot:       "Energy Weapons",
					Owner:      hunterID,
					ClassType:  classAny,
					Equippable: true,
				},
			)
			s.add(
				&Item{InstanceID: "2002", Slot: departing.Slot, Owner: titanID, ClassType: classAny, Equippable: true},
			)
			s.add(&Item{InstanceID: "2003", Slot: departing.Slot, Owner: hunterID, ClassType: 0, Equippable: true})

			if tc.spareGone {
				spare, ok := s.Find("1003")
				require.True(t, ok)

				spare.Owner = ownerVault
			}

			got := s.replacementFor(departing)
			if tc.want == "" {
				assert.Nil(t, got)

				return
			}

			require.NotNil(t, got)
			assert.Equal(t, tc.want, got.InstanceID)
		})
	}
}

// stackUnits returns the units of the fixture stack that owner holds.
func stackUnits(s *Snapshot, owner string) int {
	units := 0

	for _, item := range s.Items {
		if item.Hash == 300 && item.Owner == owner {
			units += item.Quantity
		}
	}

	return units
}

func TestMoveStack(t *testing.T) {
	t.Parallel()

	s := fixtureSnapshot(t)
	mover := &stepMover{}

	name, moved, err := s.MoveStack(t.Context(), mover, 300, 10, true)
	require.NoError(t, err)
	assert.Equal(t, "Enhancement Core", name)
	assert.Equal(t, 10, moved)
	assert.Equal(t, 32, stackUnits(s, ownerAccount))
	assert.Equal(t, 10, stackUnits(s, ownerVault))

	_, moved, err = s.MoveStack(t.Context(), mover, 300, 0, true)
	require.NoError(t, err)
	assert.Equal(t, 32, moved, "an amount of zero moves every unit")
	assert.Equal(t, 42, stackUnits(s, ownerVault))

	stacks, err := s.Filter(ItemFilter{Kind: kindOther})
	require.NoError(t, err)
	require.Len(t, stacks, 1, "the emptied stack leaves the snapshot")

	_, moved, err = s.MoveStack(t.Context(), mover, 300, 5, false)
	require.NoError(t, err)
	assert.Equal(t, 5, moved)
	assert.Equal(t, 5, stackUnits(s, ownerAccount))
	assert.Equal(t, 37, stackUnits(s, ownerVault))

	assert.Equal(t, []string{"to vault from c1 x10", "to vault from c1 x32", "from vault to c1 x5"}, mover.steps)
}

func TestMoveStackErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err     error
		fail    string
		hash    uint32
		amount  int
		toVault bool
	}{
		"more than the stack holds": {hash: 300, amount: 43, toVault: true, err: ErrMove},
		"nothing in the vault":      {hash: 300, err: ErrMove},
		"unknown hash":              {hash: 7, toVault: true, err: ErrMove},
		"instanced item":            {hash: 100, toVault: true, err: ErrMove},
		"bungie refuses":            {hash: 300, toVault: true, fail: "to vault from c1 x42", err: errStep},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := fixtureSnapshot(t)

			_, moved, err := s.MoveStack(t.Context(), &stepMover{fail: tc.fail}, tc.hash, tc.amount, tc.toVault)
			require.ErrorIs(t, err, tc.err)
			assert.Zero(t, moved)
			assert.Equal(t, 42, stackUnits(s, ownerAccount))
		})
	}
}

func TestMoveStackStopsHalfway(t *testing.T) {
	t.Parallel()

	s := fixtureSnapshot(t)

	s.Items = append(s.Items, &Item{
		Hash:         300,
		Name:         "Enhancement Core",
		Kind:         kindOther,
		Owner:        ownerAccount,
		Quantity:     8,
		Transferable: true,
	})

	_, moved, err := s.MoveStack(t.Context(), &stepMover{fail: "to vault from c1 x8"}, 300, 0, true)
	require.ErrorIs(t, err, errStep)
	assert.Equal(t, 42, moved)
	assert.Equal(t, 42, stackUnits(s, ownerVault))
	assert.Equal(t, 8, stackUnits(s, ownerAccount))

	stacks, err := s.Filter(ItemFilter{Kind: kindOther})
	require.NoError(t, err)
	assert.Len(t, stacks, 2, "the emptied stack leaves the snapshot")
}

func TestSetLock(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err          error
		id           string
		owner        string
		fail         string
		want         []string
		locked       bool
		wantLocked   bool
		notLockable  bool
		noCharacters bool
	}{
		"vault item names the most recently played character": {
			id:         "1002",
			locked:     true,
			want:       []string{"lock 1002 on c1"},
			wantLocked: true,
		},
		"unlock a vault item": {
			id:   "1001",
			want: []string{"unlock 1001 on c1"},
		},
		"character item names the character that holds it": {
			id:         "1003",
			owner:      titanID,
			locked:     true,
			want:       []string{"lock 1003 on c2"},
			wantLocked: true,
		},
		"item that cannot be locked": {
			id:          "1002",
			locked:      true,
			notLockable: true,
			err:         ErrLock,
		},
		"account without a character": {
			id:           "1002",
			locked:       true,
			noCharacters: true,
			err:          ErrLock,
		},
		"bungie refuses a lock": {
			id:     "1002",
			locked: true,
			fail:   "lock 1002 on c1",
			err:    errStep,
		},
		"bungie refuses an unlock": {
			id:         "1001",
			fail:       "unlock 1001 on c1",
			err:        errStep,
			wantLocked: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := fixtureSnapshot(t)
			mover := &stepMover{fail: tc.fail}

			item, ok := s.Find(tc.id)
			require.True(t, ok)

			if tc.owner != "" {
				item.Owner = tc.owner
			}

			if tc.notLockable {
				item.Lockable = false
			}

			if tc.noCharacters {
				s.Characters = nil
			}

			err := s.SetLock(t.Context(), mover, item, tc.locked)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tc.want, mover.steps)
			assert.Equal(t, tc.wantLocked, item.Locked)
		})
	}
}

func TestTagChanges(t *testing.T) {
	t.Parallel()

	rows := []ItemTag{
		{InstanceID: "1001", ItemHash: 100, TagID: "keep"},
		{InstanceID: "1001", ItemHash: 100, TagID: tagFavorite},
		{InstanceID: "1001", ItemHash: 100, TagID: "pvp", Removed: true},
		{InstanceID: "1002", ItemHash: 101, TagID: "junk"},
		{InstanceID: "1003", ItemHash: 100, TagID: "junk", RuleID: "rule-1"},
	}

	tests := map[string]struct {
		// instance is the instance id of the item. It defaults to 1001.
		instance string
		tag      string
		// rule is the id of the autotag rule that sets the tag. It is
		// empty for a tag that a person sets.
		rule string
		want []ItemTag
	}{
		"replace the tag": {
			tag: "junk",
			want: []ItemTag{
				{InstanceID: "1001", ItemHash: 100, TagID: "keep", Removed: true},
				{InstanceID: "1001", ItemHash: 100, TagID: "junk"},
			},
		},
		"clear the tag": {
			tag: "",
			want: []ItemTag{
				{InstanceID: "1001", ItemHash: 100, TagID: "keep", Removed: true},
			},
		},
		"same tag writes the row again": {
			tag: "keep",
			want: []ItemTag{
				{InstanceID: "1001", ItemHash: 100, TagID: "keep"},
			},
		},
		// Only the added row carries the rule id, as in the web client.
		"a rule replaces the tag": {
			tag:  "junk",
			rule: "rule-1",
			want: []ItemTag{
				{InstanceID: "1001", ItemHash: 100, TagID: "keep", Removed: true},
				{InstanceID: "1001", ItemHash: 100, TagID: "junk", RuleID: "rule-1"},
			},
		},
		"a rule sets the tag that a person set": {
			tag:  "keep",
			rule: "rule-1",
			want: []ItemTag{
				{InstanceID: "1001", ItemHash: 100, TagID: "keep", RuleID: "rule-1"},
			},
		},
		"a person sets the tag that a rule set": {
			instance: "1003",
			tag:      "junk",
			want: []ItemTag{
				{InstanceID: "1003", ItemHash: 100, TagID: "junk"},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			instance := cmp.Or(tc.instance, "1001")
			assert.Equal(t, tc.want, tagChanges(rows, instance, 100, tc.tag, tc.rule))
		})
	}
}
