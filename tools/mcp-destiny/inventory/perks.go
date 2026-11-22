package inventory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/fold"
)

// ErrPerk reports a perk change that the item forbids before any Bungie
// request, such as a perk that no socket of the item can hold.
var ErrPerk = errors.New("cannot change the perk")

// PlugInserter performs the Bungie request that inserts a plug into a
// socket of an item.
type PlugInserter interface {
	InsertSocketPlugFree(ctx context.Context, action bungie.ItemAction, socket int, plugHash uint32) error
}

// SetPerk makes the plug named name the active perk of one socket of item
// in the live game. It compares name with the names in [Perk.Options] and
// [Perk.Locked], and replace with the active perk names. Each comparison
// applies the folds of [fold.Passes] in order. The next fold applies only
// when the fold before it does not match any perk. Each of the two can
// therefore differ in letter case and in accents. It can also write a
// letter where a perk name writes a digit in its place.
//
// A non-empty replace names the active perk of the socket to change, which
// settles a name that more than one socket can hold. When every socket
// that can hold name shares one plug set and holds a plug of one name,
// such as the placeholder "Empty Fragment Socket", SetPerk changes the
// first of those sockets. A name of [Perk.Locked] sends the insert as
// well, and Bungie refuses it when the plug is locked for the character in
// the request.
//
// SetPerk returns the plug that the socket held before the change and the
// plug that it holds after. An empty replace selects every socket, and a
// non-empty replace selects only the sockets whose active perk it names.
// When a selected socket already holds name, both plugs are that one, and
// SetPerk sends no request. When Bungie accepts the change, SetPerk
// records the new plug in the snapshot. It then picks the options and the
// locked plugs of the socket by the rules of [Perk], as a profile fetch
// does. It also picks both again for each other socket that shares the
// plug set of a subclass socket. The stats of the item keep their values
// until the next profile fetch.
//
// Bungie requires a character in every insert request. An item in the
// vault belongs to no character, so SetPerk names the most recently played
// one, as Destiny Item Manager does.
func (s *Snapshot) SetPerk(
	ctx context.Context,
	inserter PlugInserter,
	item *Item,
	name, replace string,
) (Plug, Plug, error) {
	if len(item.Perks) == 0 {
		return Plug{}, Plug{}, fmt.Errorf("%w: %s has no perks", ErrPerk, item.Name)
	}

	sockets := socketsHolding(item, replace)
	if len(sockets) == 0 {
		return Plug{}, Plug{}, fmt.Errorf(
			"%w: %s has no active perk named %q. Its perks are %s",
			ErrPerk, item.Name, replace, strings.Join(perkNames(item.Perks), ", "),
		)
	}

	type match struct {
		plug Plug
		perk int
	}

	var matches []match

	for _, f := range fold.Passes() {
		text := f.Text(name)

		for _, i := range sockets {
			perk := item.Perks[i]
			if text.Equal(perk.Name) {
				return perk.Plug, perk.Plug, nil
			}

			for _, plug := range slices.Concat(perk.Options, perk.Locked) {
				if text.Equal(plug.Name) {
					matches = append(matches, match{perk: i, plug: plug})
				}
			}
		}

		if len(matches) > 0 {
			break
		}
	}

	if len(matches) == 0 {
		return Plug{}, Plug{}, noSwap(item, sockets, name, replace)
	}

	perks := make([]int, 0, len(matches))
	for _, m := range matches {
		perks = append(perks, m.perk)
	}

	if len(matches) > 1 && !interchangeable(item, perks) {
		active := make([]string, 0, len(matches))
		for _, m := range matches {
			active = append(active, item.Perks[m.perk].Name)
		}

		return Plug{}, Plug{}, fmt.Errorf(
			"%w: %d sockets of %s can swap to %s, so pass replace with the active perk of one: %s",
			ErrPerk, len(matches), item.Name, matches[0].plug.Name,
			strings.Join(active, ", "),
		)
	}

	character, ok := s.requestCharacter(item)
	if !ok {
		return Plug{}, Plug{}, fmt.Errorf("%w: the account has no character to name in the request", ErrPerk)
	}

	changed := matches[0].perk
	perk := &item.Perks[changed]
	option := matches[0].plug
	action := bungie.ItemAction{InstanceID: item.InstanceID, ItemHash: item.Hash, CharacterID: character}

	err := inserter.InsertSocketPlugFree(ctx, action, perk.Socket, option.Hash)
	if err != nil {
		return Plug{}, Plug{}, fmt.Errorf("inserting %s: %w", option.Name, err)
	}

	from := perk.Plug
	perk.Plug = option

	// The other sockets of the plug set of a subclass socket offer the
	// replaced plug and stop offering the new one.
	for i := range item.Perks {
		if i == changed || (perk.PlugSet != 0 && item.Perks[i].PlugSet == perk.PlugSet) {
			item.Perks[i].Options, item.Perks[i].Locked = swapsOf(item.Perks, i)
		}
	}

	return from, option, nil
}

// socketsHolding returns the indexes of the perks of item whose active
// perk replace names. An empty replace selects every perk. It applies the
// folds of [fold.Passes] in order, and the next fold applies only when no
// active perk has the name.
func socketsHolding(item *Item, replace string) []int {
	var sockets []int

	for _, f := range fold.Passes() {
		text := f.Text(replace)

		for i, perk := range item.Perks {
			if replace == "" || text.Equal(perk.Name) {
				sockets = append(sockets, i)
			}
		}

		if len(sockets) > 0 {
			break
		}
	}

	return sockets
}

// interchangeable reports whether the perks of item at the indexes perks
// share one plug set and hold plugs of one name. Sockets that share a plug
// set hold plugs of one name only when each holds the placeholder of an
// empty socket. The perks of an item leave out each closed socket with a
// plug set, so each such socket is open. SetPerk changes the first of
// them.
func interchangeable(item *Item, perks []int) bool {
	first := item.Perks[perks[0]]
	if first.PlugSet == 0 {
		return false
	}

	for _, i := range perks[1:] {
		if item.Perks[i].PlugSet != first.PlugSet || item.Perks[i].Name != first.Name {
			return false
		}
	}

	return true
}

// noSwap reports a perk name that none of the given sockets of item can
// swap to. It lists the swaps and the locked plugs that those sockets
// allow.
func noSwap(item *Item, sockets []int, name, replace string) error {
	subject := item.Name
	if replace != "" {
		subject += " that holds " + item.Perks[sockets[0]].Name
	}

	var labels []string

	for _, i := range sockets {
		if len(item.Perks[i].Options) > 0 || len(item.Perks[i].Locked) > 0 {
			labels = append(labels, PerkLabel(item.Perks[i]))
		}
	}

	if len(labels) == 0 {
		return fmt.Errorf("%w: no socket of %s can swap its perk", ErrPerk, subject)
	}

	return fmt.Errorf(
		"%w: no socket of %s can swap to %q. Sockets that can swap: %s",
		ErrPerk, subject, name, strings.Join(labels, "; "),
	)
}

// PerkLabel names a perk and the other plugs that its socket can hold. It
// also names the plugs of its plug set that Bungie reports as locked, such
// as "Stylish Executioner (can swap to Ascension; Bungie reports as
// locked: Threaded Specter)".
func PerkLabel(perk Perk) string {
	var parts []string

	if len(perk.Options) > 0 {
		parts = append(parts, "can swap to "+strings.Join(perk.OptionNames(), ", "))
	}

	if len(perk.Locked) > 0 {
		parts = append(parts, LockedLabel+strings.Join(perk.LockedNames(), ", "))
	}

	if len(parts) == 0 {
		return perk.Name
	}

	return perk.Name + " (" + strings.Join(parts, "; ") + ")"
}

func perkNames(perks []Perk) []string {
	names := make([]string, 0, len(perks))
	for _, perk := range perks {
		names = append(names, perk.Name)
	}

	return names
}
