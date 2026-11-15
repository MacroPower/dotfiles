package inventory

import (
	"fmt"
	"slices"
	"strings"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/fold"
)

// ResolveSlot returns the bucket that a slot name selects. The candidates
// are the buckets of [Snapshot.Buckets] that each character has and that
// hold at least one weapon or armor piece. ResolveSlot trims the spaces
// around name. It then applies the folds of [fold.Passes] in order, and
// it compares each bucket name with name after the fold folds both. A
// candidate whose full name equals name wins. Otherwise the one candidate
// whose name contains name wins, and more than one such candidate returns
// an error. The next fold applies only when no candidate matches. A name
// that folds to the empty text, such as "^", selects no slot. Each error
// wraps [ErrInvalidInput], and it names the candidates or the matches
// when there are any.
func (s *Snapshot) ResolveSlot(name string) (Bucket, error) {
	candidates := s.gearBuckets()
	if len(candidates) == 0 {
		return Bucket{}, fmt.Errorf("%w: no slot holds a weapon or an armor piece", ErrInvalidInput)
	}

	name = strings.TrimSpace(name)
	if fold.Name(name) == "" {
		return Bucket{}, fmt.Errorf("%w: pass a slot, one of %s", ErrInvalidInput, bucketNames(candidates))
	}

	for _, f := range fold.Passes() {
		text := f.Text(name)

		var matches []Bucket

		for _, b := range candidates {
			if text.Equal(b.Name) {
				return b, nil
			}

			if text.In(b.Name) {
				matches = append(matches, b)
			}
		}

		switch len(matches) {
		case 0:
			continue
		case 1:
			return matches[0], nil
		}

		return Bucket{}, fmt.Errorf(
			"%w: slot %q matches %s, so pass more of the name", ErrInvalidInput, name, bucketNames(matches),
		)
	}

	return Bucket{}, fmt.Errorf(
		"%w: slot %q matches no weapon or armor slot, so pass one of %s",
		ErrInvalidInput, name, bucketNames(candidates),
	)
}

// gearBuckets returns the buckets of [Snapshot.Buckets] that each
// character has and that a weapon or an armor piece names as its
// [Item.Bucket].
func (s *Snapshot) gearBuckets() []Bucket {
	var out []Bucket

	for _, b := range s.Buckets {
		if b.Account || b.Hash == bungie.BucketVault || b.Hash == bungie.BucketPostmaster {
			continue
		}

		if slices.ContainsFunc(s.Items, func(item *Item) bool {
			return item.Bucket == b.Hash && (item.Kind == KindWeapon || item.Kind == KindArmor)
		}) {
			out = append(out, b)
		}
	}

	return out
}

// bucketNames joins the names of buckets with a comma and a space.
func bucketNames(buckets []Bucket) string {
	names := make([]string, 0, len(buckets))
	for _, b := range buckets {
		names = append(names, b.Name)
	}

	return strings.Join(names, ", ")
}

// JunkPlan is what one call of the stage_junk tool knows about one slot
// of one character. Staged holds the unequipped junk that the character
// holds in the slot. Keepers holds the other unequipped items there that
// [*Snapshot.Move] can move to the vault. Unmovable holds the rest of the
// unequipped items there, such as a stack without an instance id or an
// item that is not transferable. Waiting holds the junk that can come to
// the slot from the vault or from another character. Skipped counts the
// junk of the slot that the tool leaves in place. Each such item is
// equipped, at the Postmaster, not transferable, or for another class.
// Room is the number of free places of the slot.
type JunkPlan struct {
	Staged    []*Item
	Waiting   []*Item
	Keepers   []*Item
	Unmovable []*Item
	Skipped   int
	Room      int
}

// JunkPlan sorts the items of bucket b into a [JunkPlan] for character c.
// An item that c holds unequipped outside the Postmaster sits in the slot,
// whatever its class and its transfer state. Such an item without the junk
// tag is a keeper when [*Snapshot.Move] can move it to the vault, and it
// is unmovable otherwise. Junk that another owner holds waits when it is
// unequipped, outside the Postmaster, transferable, and of a class that c
// can use. Every other junk item of the bucket counts as skipped. The
// lists follow the default order of [*Snapshot.Filter]. Room is the
// capacity of b minus the items that [*Snapshot.SlotsUsed] counts for c,
// the equipped item and the unmovable items included. It is never less
// than 0.
func (s *Snapshot) JunkPlan(c Character, b Bucket) JunkPlan {
	plan := JunkPlan{Room: max(b.Capacity-s.SlotsUsed(b, c.ID), 0)}

	for _, item := range s.Items {
		if item.Bucket != b.Hash {
			continue
		}

		junk := item.Tag == dvm.TagJunk

		switch {
		case item.Owner == c.ID && !item.Equipped && !item.Postmaster:
			// The stage_junk tool tells the user to move each keeper to the
			// vault with move_items, which runs the checks of Move.
			switch {
			case junk:
				plan.Staged = append(plan.Staged, item)
			case s.checkMove(item, OwnerVault, false) != nil:
				plan.Unmovable = append(plan.Unmovable, item)
			default:
				plan.Keepers = append(plan.Keepers, item)
			}

		case !junk:
			// An item without the junk tag counts only in the slot.
		case item.Owner != c.ID && !item.Equipped && !item.Postmaster && item.Transferable &&
			(item.ClassType == bungie.ClassAny || item.ClassType == c.ClassType):
			plan.Waiting = append(plan.Waiting, item)
		default:
			plan.Skipped++
		}
	}

	for _, items := range [][]*Item{plan.Staged, plan.Waiting, plan.Keepers, plan.Unmovable} {
		slices.SortStableFunc(items, ByNameAndID)
	}

	return plan
}
