package inventory

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
)

// emptyLoadoutItem is the instance id of a loadout item that holds
// nothing.
const emptyLoadoutItem = "0"

var (
	// loadoutBuckets lists the buckets whose equipped item a saved loadout
	// holds, in the order of the in-game loadout. Destiny Item Manager
	// saves the same buckets.
	loadoutBuckets = []uint32{
		bungie.BucketSubclass,
		bungie.BucketArtifacts,
		bungie.BucketKinetic,
		bungie.BucketEnergy,
		bungie.BucketPower,
		bungie.BucketHelmet,
		bungie.BucketGauntlets,
		bungie.BucketChestArmor,
		bungie.BucketLegArmor,
		bungie.BucketClassArmor,
	}

	// ErrUnknownLoadout reports a loadout number that names no saved
	// loadout of the character.
	ErrUnknownLoadout = errors.New("unknown loadout")

	// ErrNoLoadoutSlot reports a loadout number outside the loadout slots
	// of the character.
	ErrNoLoadoutSlot = errors.New("no loadout slot")

	// ErrEmptyLoadoutSlot reports a loadout slot that holds no saved
	// loadout.
	ErrEmptyLoadoutSlot = errors.New("empty loadout slot")
)

// Loadout is one loadout that a character saved in the game. Number is the
// position of the loadout on the in-game loadout screen, counted from 1.
// InstanceIDs lists the items of the loadout in the order that Bungie
// serves them. An id that the snapshot does not hold names an item that
// left the inventory.
//
// NameHash, IconHash, and ColorHash identify the name, the icon, and the
// color that the game shows for the loadout. The definition artifacts of
// the service leave out the tables that these hashes key, and a
// [*bungie.LoadoutTableStore] reads them from the Destiny manifest.
// Several loadouts can share a name, so the character and the number
// identify a loadout.
type Loadout struct {
	CharacterID string
	InstanceIDs []string
	Number      int
	NameHash    uint32
	IconHash    uint32
	ColorHash   uint32
}

// LoadoutEquipper performs the Bungie actions that equipping a saved
// loadout consists of. The item actions bring the loadout items to the
// character first.
type LoadoutEquipper interface {
	ItemMover
	EquipLoadout(ctx context.Context, action bungie.LoadoutAction) error
}

// LoadoutMove is one item that [*Snapshot.EquipLoadout] moved to the
// character of the loadout. From is the owner value of the item before the
// move. Replacement is the item that took its slot on the character that
// had it equipped.
type LoadoutMove struct {
	Item        *Item
	Replacement *Item
	From        string
}

// LoadoutEquip reports what [*Snapshot.EquipLoadout] did. Items holds the
// loadout items that the snapshot holds, and Missing holds the instance
// ids of the others. Moves lists the moves that completed, in order.
// Stopped holds the move that failed, and its Replacement is the item that
// the move equipped before the failure. Changed reports whether any
// request changed the game, which includes a step of a move that then
// failed. Equipped reports whether Bungie equipped the loadout.
type LoadoutEquip struct {
	Stopped  *LoadoutMove
	Items    []*Item
	Missing  []string
	Moves    []LoadoutMove
	Changed  bool
	Equipped bool
}

// loadoutSlotsOf returns the number of loadout slots of each character of
// a profile, keyed by character id. Bungie lists every slot that the
// character unlocked, the empty slots included.
func loadoutSlotsOf(profile *bungie.Profile) map[string]int {
	out := make(map[string]int, len(profile.CharacterLoadouts.Data))

	for id, set := range profile.CharacterLoadouts.Data {
		out[id] = len(set.Loadouts)
	}

	return out
}

// loadoutsOf returns the saved loadouts of a profile, ordered by character
// id, then number. It drops each loadout that holds nothing and keeps the
// number of every other loadout. A loadout holds nothing when every item
// has the instance id "0" or no instance id.
func loadoutsOf(profile *bungie.Profile) []Loadout {
	var out []Loadout

	for id, set := range profile.CharacterLoadouts.Data {
		for i, saved := range set.Loadouts {
			l := Loadout{
				CharacterID: id,
				Number:      i + 1,
				NameHash:    saved.NameHash,
				IconHash:    saved.IconHash,
				ColorHash:   saved.ColorHash,
			}

			for _, item := range saved.Items {
				if item.InstanceID != "" && item.InstanceID != emptyLoadoutItem {
					l.InstanceIDs = append(l.InstanceIDs, item.InstanceID)
				}
			}

			if len(l.InstanceIDs) > 0 {
				out = append(out, l)
			}
		}
	}

	// The loop appends the loadouts of each character in number order, and
	// the stable sort keeps that order.
	slices.SortStableFunc(out, func(a, b Loadout) int {
		return cmp.Compare(a.CharacterID, b.CharacterID)
	})

	return out
}

// LoadoutsOf returns the saved loadouts of one character, ordered by
// number.
func (s *Snapshot) LoadoutsOf(characterID string) []Loadout {
	var out []Loadout

	for _, l := range s.Loadouts {
		if l.CharacterID == characterID {
			out = append(out, l)
		}
	}

	return out
}

// FindLoadout returns the saved loadout of the character with the given
// number.
func (s *Snapshot) FindLoadout(characterID string, number int) (Loadout, error) {
	saved := s.LoadoutsOf(characterID)

	for _, l := range saved {
		if l.Number == number {
			return l, nil
		}
	}

	label := s.OwnerLabel(characterID)

	if len(saved) == 0 {
		return Loadout{}, fmt.Errorf("%w: %s has no saved loadouts", ErrUnknownLoadout, label)
	}

	numbers := make([]string, 0, len(saved))
	for _, l := range saved {
		numbers = append(numbers, strconv.Itoa(l.Number))
	}

	return Loadout{}, fmt.Errorf(
		"%w: %s has no saved loadout with the number %d. Its saved loadouts have the numbers %s",
		ErrUnknownLoadout, label, number, strings.Join(numbers, ", "),
	)
}

// LoadoutSlot returns the loadout in slot number of the character,
// counted from 1. The bool reports whether the slot holds a saved
// loadout. For an empty slot, the loadout carries only the character and
// the number. A number outside the slots of the character returns an
// error that wraps [ErrNoLoadoutSlot].
func (s *Snapshot) LoadoutSlot(characterID string, number int) (Loadout, bool, error) {
	slots := s.LoadoutSlots[characterID]

	if number < 1 || number > slots {
		return Loadout{}, false, s.noLoadoutSlot(characterID, number, slots)
	}

	for _, l := range s.LoadoutsOf(characterID) {
		if l.Number == number {
			return l, true, nil
		}
	}

	return Loadout{CharacterID: characterID, Number: number}, false, nil
}

// noLoadoutSlot returns the error of a loadout number outside the slots
// of the character. The character has the given number of slots.
func (s *Snapshot) noLoadoutSlot(characterID string, number, slots int) error {
	label := s.OwnerLabel(characterID)

	switch slots {
	case 0:
		return fmt.Errorf("%w: %s has no loadout slots", ErrNoLoadoutSlot, label)

	case 1:
		return fmt.Errorf(
			"%w: %s has no loadout slot with the number %d. Its only loadout slot has the number 1",
			ErrNoLoadoutSlot, label, number,
		)
	}

	return fmt.Errorf(
		"%w: %s has no loadout slot with the number %d. Its %d loadout slots have the numbers 1 to %d",
		ErrNoLoadoutSlot, label, number, slots, slots,
	)
}

// LoadoutGear returns the items that the character has equipped in the
// buckets that a saved loadout holds, in the order of the in-game
// loadout.
func (s *Snapshot) LoadoutGear(characterID string) []*Item {
	var out []*Item

	for _, bucket := range loadoutBuckets {
		for _, item := range s.Items {
			if item.Owner == characterID && item.Equipped && item.Bucket == bucket {
				out = append(out, item)
			}
		}
	}

	return out
}

// RecordLoadoutSave records a save of the equipped gear of the character
// into slot number, with the identifiers ids. The loadout in the slot then
// holds the instance ids of [*Snapshot.LoadoutGear] and replaces any
// loadout that the slot held. Without equipped gear, the slot holds
// nothing, so the snapshot drops its loadout as [loadoutsOf] does.
func (s *Snapshot) RecordLoadoutSave(characterID string, number int, ids bungie.LoadoutIdentifiers) {
	s.RecordLoadoutClear(characterID, number)

	l := Loadout{
		CharacterID: characterID,
		Number:      number,
		NameHash:    ids.NameHash,
		IconHash:    ids.IconHash,
		ColorHash:   ids.ColorHash,
	}

	for _, item := range s.LoadoutGear(characterID) {
		l.InstanceIDs = append(l.InstanceIDs, item.InstanceID)
	}

	if len(l.InstanceIDs) == 0 {
		return
	}

	// Loadouts stays ordered by character id, then number.
	i, _ := slices.BinarySearchFunc(s.Loadouts, l, func(a, b Loadout) int {
		return cmp.Or(cmp.Compare(a.CharacterID, b.CharacterID), cmp.Compare(a.Number, b.Number))
	})

	s.Loadouts = slices.Insert(s.Loadouts, i, l)
}

// RecordLoadoutClear records that slot number of the character holds no
// saved loadout.
func (s *Snapshot) RecordLoadoutClear(characterID string, number int) {
	s.Loadouts = slices.DeleteFunc(s.Loadouts, func(l Loadout) bool {
		return l.CharacterID == characterID && l.Number == number
	})
}

// EquipLoadout equips a saved loadout on its character.
//
// The game does not pull a loadout item from another character.
// EquipLoadout therefore first moves every loadout item that the character
// does not hold to that character, as Destiny Item Manager does. Before the
// first request, it checks that each of those items is transferable. It
// also checks that each other character with one of those items equipped
// holds another item to equip in that slot. It stops at the first move
// that fails, reports that move in [LoadoutEquip.Stopped], and does not
// equip the loadout.
//
// EquipLoadout records each move in the snapshot. After the equip it marks
// every loadout item as equipped. The plugs that the loadout sets, such as
// armor mods, reach the snapshot with the next profile fetch.
func (s *Snapshot) EquipLoadout(ctx context.Context, equipper LoadoutEquipper, l Loadout) (LoadoutEquip, error) {
	var (
		out   LoadoutEquip
		moves []*Item
	)

	for _, id := range l.InstanceIDs {
		item, ok := s.Find(id)
		if !ok {
			out.Missing = append(out.Missing, id)

			continue
		}

		out.Items = append(out.Items, item)

		if item.Owner != l.CharacterID || item.Postmaster {
			err := s.checkMove(item, l.CharacterID, false)
			if err != nil {
				return out, err
			}

			// A loadout holds one item per slot, so the earlier moves
			// leave the replacement candidates of this slot as they are.
			if item.Equipped && item.Owner != l.CharacterID {
				_, err := s.requireReplacement(item)
				if err != nil {
					return out, err
				}
			}

			moves = append(moves, item)
		}
	}

	for _, item := range moves {
		from, postmaster, equipped := item.Owner, item.Postmaster, item.Equipped

		replacement, err := s.Move(ctx, equipper, item, l.CharacterID, false)

		if replacement != nil || item.Owner != from || item.Postmaster != postmaster || item.Equipped != equipped {
			out.Changed = true
		}

		if err != nil {
			out.Stopped = &LoadoutMove{Item: item, Replacement: replacement, From: from}

			return out, fmt.Errorf(
				"moving %s (%s) to %s: %w", item.Name, item.InstanceID, s.OwnerLabel(l.CharacterID), err,
			)
		}

		out.Moves = append(out.Moves, LoadoutMove{Item: item, Replacement: replacement, From: from})
	}

	err := equipper.EquipLoadout(ctx, bungie.LoadoutAction{CharacterID: l.CharacterID, Index: l.Number - 1})
	if err != nil {
		return out, fmt.Errorf("equipping the loadout: %w", err)
	}

	for _, item := range out.Items {
		s.markEquipped(item)
	}

	out.Changed = true
	out.Equipped = true

	return out, nil
}
