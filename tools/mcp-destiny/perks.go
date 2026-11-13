package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrPerk reports a perk change that the item forbids before any Bungie
// request, such as a perk that no socket of the item can hold.
var ErrPerk = errors.New("cannot change the perk")

// PlugInserter performs the Bungie request that inserts a plug into a
// socket of an item.
type PlugInserter interface {
	InsertSocketPlugFree(ctx context.Context, action ItemAction, socket int, plugHash uint32) error
}

// SetPerk makes the plug named name the active perk of one socket of item
// in the live game. It compares name with the names in [Perk.Options]
// without regard to case. A non-empty replace names the active perk of the
// socket to change, which settles a name that more than one socket can
// hold.
//
// SetPerk returns the plug that the socket held before the change and the
// plug that it holds after. An empty replace selects every socket, and a
// non-empty replace selects only the sockets whose active perk it names.
// When a selected socket already holds name, both plugs are that one, and
// SetPerk sends no request. When Bungie accepts the change, SetPerk
// records the new plug in the snapshot and picks the options of the
// socket by the rules of [Perk], as a profile fetch does. The stats of the
// item keep their values until the next profile fetch.
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

	var sockets []int

	for i, perk := range item.Perks {
		if replace == "" || strings.EqualFold(perk.Name, replace) {
			sockets = append(sockets, i)
		}
	}

	if len(sockets) == 0 {
		return Plug{}, Plug{}, fmt.Errorf(
			"%w: %s has no active perk named %q. Its perks are %s",
			ErrPerk, item.Name, replace, strings.Join(perkNames(item.Perks), ", "),
		)
	}

	type match struct{ perk, option int }

	var matches []match

	for _, i := range sockets {
		perk := item.Perks[i]
		if strings.EqualFold(perk.Name, name) {
			return perk.Plug, perk.Plug, nil
		}

		for j, option := range perk.Options {
			if strings.EqualFold(option.Name, name) {
				matches = append(matches, match{perk: i, option: j})
			}
		}
	}

	if len(matches) == 0 {
		return Plug{}, Plug{}, noSwap(item, sockets, name, replace)
	}

	if len(matches) > 1 {
		active := make([]string, 0, len(matches))
		for _, m := range matches {
			active = append(active, item.Perks[m.perk].Name)
		}

		return Plug{}, Plug{}, fmt.Errorf(
			"%w: %d sockets of %s can swap to %s, so pass replace with the active perk of one: %s",
			ErrPerk, len(matches), item.Name, item.Perks[matches[0].perk].Options[matches[0].option].Name,
			strings.Join(active, ", "),
		)
	}

	character, ok := s.requestCharacter(item)
	if !ok {
		return Plug{}, Plug{}, fmt.Errorf("%w: the account has no character to name in the request", ErrPerk)
	}

	perk := &item.Perks[matches[0].perk]
	option := perk.Options[matches[0].option]
	action := ItemAction{InstanceID: item.InstanceID, ItemHash: item.Hash, CharacterID: character}

	err := inserter.InsertSocketPlugFree(ctx, action, perk.Socket, option.Hash)
	if err != nil {
		return Plug{}, Plug{}, fmt.Errorf("inserting %s: %w", option.Name, err)
	}

	from := perk.Plug
	perk.Plug = option
	perk.Options = optionsOf(perk.plugs, option)

	return from, option, nil
}

// noSwap reports a perk name that none of the given sockets of item can
// swap to. It lists the swaps that those sockets allow.
func noSwap(item *Item, sockets []int, name, replace string) error {
	subject := item.Name
	if replace != "" {
		subject += " that holds " + item.Perks[sockets[0]].Name
	}

	var labels []string

	for _, i := range sockets {
		if len(item.Perks[i].Options) > 0 {
			labels = append(labels, perkLabel(item.Perks[i]))
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

func perkNames(perks []Perk) []string {
	names := make([]string, 0, len(perks))
	for _, perk := range perks {
		names = append(names, perk.Name)
	}

	return names
}
