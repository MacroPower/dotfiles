package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
)

const tierExotic = "Exotic"

var (
	// ErrMove reports a move that the inventory state forbids before any
	// Bungie request, such as a transfer of an item that is not
	// transferable.
	ErrMove = errors.New("cannot move")

	// ErrLock reports a lock change that the inventory state forbids
	// before any Bungie request, such as a lock of an item that Bungie
	// does not let the player lock.
	ErrLock = errors.New("cannot change the lock")
)

// ItemMover performs the Bungie item actions that a move consists of.
type ItemMover interface {
	Transfer(ctx context.Context, action bungie.ItemAction, toVault bool) error
	Equip(ctx context.Context, action bungie.ItemAction) error
	PullFromPostmaster(ctx context.Context, action bungie.ItemAction) error
}

// ItemLocker performs the Bungie request that locks or unlocks an item.
type ItemLocker interface {
	SetLockState(ctx context.Context, action bungie.ItemAction, locked bool) error
}

// Move brings item to target, which is "vault" or a character id, and
// equips it there when equip is set.
//
// Bungie transfers items only between a character and the vault, so a move
// between two characters passes through the vault. Bungie also refuses to
// transfer an equipped item. Move therefore first equips another item that
// the character holds for the same slot, and returns that replacement.
//
// Move updates the snapshot after each step that succeeds. A move that
// fails halfway therefore leaves the snapshot matching the game.
func (s *Snapshot) Move(
	ctx context.Context,
	mover ItemMover,
	item *Item,
	target string,
	equip bool,
) (*Item, error) {
	err := s.checkMove(item, target, equip)
	if err != nil {
		return nil, err
	}

	action := bungie.ItemAction{InstanceID: item.InstanceID, ItemHash: item.Hash}

	if item.Postmaster {
		action.CharacterID = item.Owner

		err := mover.PullFromPostmaster(ctx, action)
		if err != nil {
			return nil, fmt.Errorf("pulling from the Postmaster: %w", err)
		}

		item.Postmaster = false
		item.Lost = false
	}

	var replacement *Item

	if item.Equipped && item.Owner != target {
		replacement, err = s.unequip(ctx, mover, item)
		if err != nil {
			return nil, err
		}
	}

	if item.Owner != target && item.Owner != ownerVault {
		action.CharacterID = item.Owner

		err := mover.Transfer(ctx, action, true)
		if err != nil {
			return replacement, fmt.Errorf("transferring to the vault: %w", err)
		}

		item.Owner = ownerVault
	}

	if item.Owner != target {
		action.CharacterID = target

		err := mover.Transfer(ctx, action, false)
		if err != nil {
			return replacement, fmt.Errorf("transferring to %s: %w", s.OwnerLabel(target), err)
		}

		item.Owner = target
	}

	if !equip || item.Equipped {
		return replacement, nil
	}

	action.CharacterID = target

	err = mover.Equip(ctx, action)
	if err != nil {
		return replacement, fmt.Errorf("equipping on %s: %w", s.OwnerLabel(target), err)
	}

	s.markEquipped(item)

	return replacement, nil
}

func (s *Snapshot) checkMove(item *Item, target string, equip bool) error {
	if item.InstanceID == "" {
		return fmt.Errorf("%w: %s is a stack, which the move_stack tool moves", ErrMove, item.Name)
	}

	if item.Owner == ownerAccount {
		return fmt.Errorf(
			"%w: %s sits in an account-wide bucket, and move_items moves only character and vault items",
			ErrMove, item.Name,
		)
	}

	if item.Owner != target && !item.Transferable {
		return fmt.Errorf("%w: %s is not transferable", ErrMove, item.Name)
	}

	if !equip {
		return nil
	}

	c, ok := s.Character(target)
	if !ok {
		return fmt.Errorf("%w: only a character can equip %s", ErrMove, item.Name)
	}

	if !item.Equippable {
		return fmt.Errorf("%w: %s is not equippable", ErrMove, item.Name)
	}

	if item.ClassType != bungie.ClassAny && item.ClassType != c.ClassType {
		return fmt.Errorf("%w: %s is %s gear, and the target is a %s", ErrMove, item.Name, item.Class, c.Class)
	}

	return nil
}

// unequip equips the replacement for an equipped item on the character
// that holds it, which frees the item for a transfer.
func (s *Snapshot) unequip(ctx context.Context, mover ItemMover, item *Item) (*Item, error) {
	replacement, err := s.requireReplacement(item)
	if err != nil {
		return nil, err
	}

	err = mover.Equip(ctx, bungie.ItemAction{
		InstanceID:  replacement.InstanceID,
		ItemHash:    replacement.Hash,
		CharacterID: item.Owner,
	})
	if err != nil {
		return nil, fmt.Errorf("equipping %s in its place: %w", replacement.Name, err)
	}

	s.markEquipped(replacement)

	return replacement, nil
}

// requireReplacement returns the item that replacementFor picks for an
// equipped item. It returns an [ErrMove] error when replacementFor finds
// none.
func (s *Snapshot) requireReplacement(item *Item) (*Item, error) {
	replacement := s.replacementFor(item)
	if replacement == nil {
		return nil, fmt.Errorf(
			"%w: %s is equipped on %s, which holds no other item to equip in that slot",
			ErrMove, item.Name, s.OwnerLabel(item.Owner),
		)
	}

	return replacement, nil
}

// replacementFor picks the item that takes the slot of an equipped item.
// It considers the unequipped items that the same character holds for the
// slot. A character can equip one exotic weapon and one exotic armor piece,
// so an exotic qualifies only when the departing item is exotic too. Among
// the candidates, a non-exotic item wins, then the highest power.
func (s *Snapshot) replacementFor(item *Item) *Item {
	c, ok := s.Character(item.Owner)
	if !ok {
		return nil
	}

	var candidates []*Item

	for _, other := range s.Items {
		switch {
		case other.Owner != item.Owner, other.Slot != item.Slot,
			other.Equipped, other.Postmaster,
			other.InstanceID == "", !other.Equippable,
			other.ClassType != bungie.ClassAny && other.ClassType != c.ClassType,
			other.Tier == tierExotic && item.Tier != tierExotic:
			continue
		}

		candidates = append(candidates, other)
	}

	if len(candidates) == 0 {
		return nil
	}

	return slices.MinFunc(candidates, func(a, b *Item) int {
		return cmp.Or(
			cmp.Compare(btoi(a.Tier == tierExotic), btoi(b.Tier == tierExotic)),
			cmp.Compare(b.Power, a.Power),
			compareIDs(a.InstanceID, b.InstanceID),
		)
	})
}

func btoi(b bool) int {
	if b {
		return 1
	}

	return 0
}

// markEquipped records that item took its slot from the item that the
// character had equipped there.
func (s *Snapshot) markEquipped(item *Item) {
	for _, other := range s.Items {
		if other.Owner == item.Owner && other.Equipped && other.Slot == item.Slot {
			other.Equipped = false
		}
	}

	item.Equipped = true
}

// MoveStack moves amount units of a stackable item between the vault and
// the inventory. It returns the item name and the number of units that
// moved. The direction is into the vault when toVault is set. An amount of
// zero or less moves every unit.
//
// Bungie addresses a stack by item hash and names a character on every
// transfer. Account-wide stacks belong to no character, so MoveStack names
// the most recently played one.
func (s *Snapshot) MoveStack(
	ctx context.Context,
	mover ItemMover,
	hash uint32,
	amount int,
	toVault bool,
) (string, int, error) {
	var (
		sources []*Item
		dest    *Item
		total   int
	)

	for _, item := range s.Items {
		if item.Hash != hash || item.InstanceID != "" || item.Postmaster {
			continue
		}

		if (item.Owner == ownerVault) == toVault {
			dest = item

			continue
		}

		sources = append(sources, item)
		total += item.Quantity
	}

	from := "the vault"
	if toVault {
		from = "the inventory"
	}

	if len(sources) == 0 {
		return "", 0, fmt.Errorf("%w: %s holds no stack with the item hash %d", ErrMove, from, hash)
	}

	name := sources[0].Name

	if !sources[0].Transferable {
		return name, 0, fmt.Errorf("%w: %s is not transferable", ErrMove, name)
	}

	if amount <= 0 {
		amount = total
	}

	if amount > total {
		return name, 0, fmt.Errorf("%w: %s holds only %d of %s", ErrMove, from, total, name)
	}

	if len(s.Characters) == 0 {
		return name, 0, fmt.Errorf("%w: the account has no character to name in the transfer", ErrMove)
	}

	// A stack that a transfer emptied leaves the snapshot, also when a
	// later transfer fails.
	defer func() {
		s.Items = slices.DeleteFunc(s.Items, func(item *Item) bool {
			return item.InstanceID == "" && item.Quantity <= 0
		})
	}()

	moved := 0

	for _, source := range sources {
		if moved == amount {
			break
		}

		units := min(amount-moved, source.Quantity)

		action := bungie.ItemAction{
			InstanceID:  bungie.StackInstanceID,
			ItemHash:    hash,
			CharacterID: s.Characters[0].ID,
			StackSize:   units,
		}

		_, owned := s.Character(source.Owner)
		if owned {
			action.CharacterID = source.Owner
		}

		err := mover.Transfer(ctx, action, toVault)
		if err != nil {
			return name, moved, fmt.Errorf("transferring %s: %w", name, err)
		}

		moved += units
		source.Quantity -= units

		if dest == nil {
			// The next profile fetch corrects the owner when Bungie
			// placed the units on a character.
			clone := *source
			clone.Quantity = 0
			clone.Owner = ownerAccount

			if toVault {
				clone.Owner = ownerVault
			}

			dest = &clone
			s.Items = append(s.Items, dest)
		}

		dest.Quantity += units
	}

	return name, moved, nil
}

// SetLock locks item in the live game when locked is set, and unlocks it
// otherwise. It records the new state in the snapshot when Bungie accepts
// the change.
//
// Bungie requires a character in every lock request. An item in the vault
// belongs to no character, so SetLock names the most recently played one,
// as Destiny Item Manager does.
func (s *Snapshot) SetLock(ctx context.Context, locker ItemLocker, item *Item, locked bool) error {
	if !item.Lockable {
		return fmt.Errorf("%w: %s cannot be locked", ErrLock, item.Name)
	}

	character, ok := s.requestCharacter(item)
	if !ok {
		return fmt.Errorf("%w: the account has no character to name in the request", ErrLock)
	}

	action := bungie.ItemAction{InstanceID: item.InstanceID, ItemHash: item.Hash, CharacterID: character}

	err := locker.SetLockState(ctx, action, locked)
	if err != nil {
		return fmt.Errorf("setting the lock state: %w", err)
	}

	item.Locked = locked

	return nil
}

// requestCharacter returns the character that a Bungie request about item
// names. An item on a character names that character. Every other item,
// such as one in the vault or in an account-wide bucket, names the most
// recently played one. It returns false when the account has no character.
func (s *Snapshot) requestCharacter(item *Item) (string, bool) {
	_, owned := s.Character(item.Owner)
	if owned {
		return item.Owner, true
	}

	c, ok := s.lastPlayed()

	return c.ID, ok
}

// tagChanges returns the rows that leave an item with tagID as its one
// ordinary tag. An empty tagID leaves the item untagged. The favorite tag
// is independent of the ordinary tag, so tagChanges keeps it. The row that
// adds tagID carries ruleID, the id of the autotag rule that sets the tag.
// A tag that a person sets has an empty ruleID.
func tagChanges(rows []ItemTag, instanceID string, itemHash uint32, tagID, ruleID string) []ItemTag {
	var changes []ItemTag

	for _, row := range rows {
		if row.InstanceID != instanceID || row.Removed || row.TagID == tagID || row.TagID == tagFavorite {
			continue
		}

		changes = append(changes, ItemTag{
			InstanceID: instanceID,
			ItemHash:   itemHash,
			TagID:      row.TagID,
			Removed:    true,
		})
	}

	if tagID != "" {
		changes = append(changes, ItemTag{InstanceID: instanceID, ItemHash: itemHash, TagID: tagID, RuleID: ruleID})
	}

	return changes
}
