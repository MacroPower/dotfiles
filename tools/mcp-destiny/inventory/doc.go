// Package inventory holds a snapshot of the Destiny 2 inventory of one
// account and the live changes that keep the snapshot equal to the game.
// A caller fetches a Bungie profile and builds a snapshot from it. It reads
// the items, the characters, the buckets, and the saved loadouts of the
// account from the snapshot. It also changes the game through the
// snapshot, so the snapshot records each change.
//
// # Snapshot
//
// A snapshot joins the components of a Bungie profile with the Destiny
// definitions. The profile gives each item a hash, a location, and the
// instance, the sockets, and the stats of the copy. The definitions give
// the hash a name, a type, a tier, a slot, and a class. They also name the
// perks, the stats, and the damage type of the copy. The service withholds
// the definitions of dummy items, but it names the dummy items that the
// vendors sell and the dummy rewards of the activities. The snapshot gives
// such an item that name. The caller can also pass definitions that carry
// the names of lookups in the live manifest of Bungie, and the snapshot
// reads those names last. An item that nothing names reads "Unknown item
// <hash>". The snapshot lists the characters with the most recently played
// first. It lists the vault first and the Postmaster last among the
// buckets.
//
// Some values of an item follow Destiny Item Manager (DIM), whose search
// filters the web client of Agg's Destiny Vault Manager runs. A filter
// that reads such a value therefore matches the items that it matches in
// the web client. The rarity, the power that DIM counts, and the item
// categories are such values.
//
// The web client keeps its tags and notes in the service, apart from the
// profile. A caller passes the tags and the notes that the service stores
// to the snapshot. Each item then carries the tag of the web client, the
// favorite mark, and the note of its instance. It also carries the id of
// the autotag rule that set the tag. A later call with newer tags and
// notes replaces those values.
//
// # Live Changes
//
// Bungie can serve stale state for some seconds after a change. A profile
// that a caller fetches right after a change can therefore show the old
// inventory, so the snapshot records each change itself. Each move, stack
// transfer, lock change, perk change, and loadout equip sends its Bungie
// requests through an interface that the caller implements. The snapshot
// updates itself after each Bungie step that succeeds. A change that fails
// halfway therefore leaves the snapshot equal to the game, and the caller
// keeps the snapshot.
//
// A change checks the snapshot before its first request, and it refuses a
// change that the state of the inventory forbids. Such a refusal wraps a
// sentinel error of the change, such as [ErrMove].
//
// Bungie transfers items only between a character and the vault, and it
// refuses to transfer an equipped item. A move between two characters
// therefore passes through the vault. A move of an equipped item first
// equips another item that the character holds for the slot. A loadout
// equip first moves each loadout item to the character of the loadout, as
// DIM does.
//
// The caller saves and clears a loadout through its own Bungie client. It
// then records the save or the clear in the snapshot. The stats that a
// perk change alters and the mods that a loadout sets reach the snapshot
// with the next profile fetch.
//
// # Filters
//
// A filter selects items and orders them. The zero filter selects every
// weapon and armor piece, ordered by name. A location names the vault, the
// Postmaster, the equipped items, the items on the characters, a character
// id, or a class name that exactly one character has.
//
// The text fields match without regard to letter case. The query, the
// slot, the perk, and the stat match names, and they fold each name and
// the text before they compare them. They therefore also match without
// regard to accents. The query, the slot, and the perk match a part of
// the name after [fold.Lookalike] folds both. That fold reads a digit
// that a name writes in place of a letter as that letter, so the query
// stomp matches St0mp-EE5. The stat matches the full name of a stat, as
// [*Item.StatValue] compares names. A filter that names a stat must name
// a stat that an item of the snapshot has.
//
// The default order is by name and then by instance id. Bungie issues
// ascending instance ids, so the id order is also the order in which the
// account acquired the items.
//
// # Patterns
//
// A snapshot also lists the crafting pattern of each craftable weapon whose
// pattern record the profile holds. Each copy of a craftable weapon
// carries the pattern whose record has the name of the copy, as in DIM.
// A copy of an Adept, Timelost, or Harrowed version carries the pattern
// of the base weapon, because the game counts its extractions toward that
// pattern.
//
// Each copy also carries two flags that DIM reads from the crafting
// sockets of a copy. One marks a copy with Deepsight Resonance, the red
// border of the game. The other marks a copy with an incomplete pattern
// that a Deepsight Harmonizer can give Deepsight Resonance.
//
// The Deepsight activation of a raid weapon spends Spoils of Conquest
// beside the Deepsight Harmonizer. Each item therefore records whether
// the source tags of DIM put it in a raid. A snapshot also gives the
// quantity of a material that the account holds, apart from the stacks at
// the Postmaster. A caller can then tell how many activations the account
// can pay for.
//
// A pattern filter selects patterns with the owned copies of each weapon.
// Each pattern that it selects also lists the item hashes of the
// craftable definitions with the name of the pattern. A caller can then
// find the weapon on a roll sheet. A version that the pattern does not
// craft, such as the Onslaught version of a raid weapon, is not one of
// those definitions. The query of the filter matches a part of the weapon
// name after [fold.Lookalike] folds both. The incomplete patterns come
// first, with the fewest extractions left first, and the complete
// patterns follow.
//
// # Weapon Groups
//
// A weapon group holds every copy of one weapon in every location. The
// name of the group drops the version suffix (Adept), (Timelost), or
// (Harrowed). The copies of each such version therefore share the group
// of the base weapon. A reissue shares the group of the original weapon,
// because the names match.
//
// A caller selects a group by a name that the user passes. An exact group
// name wins. A group name that equals the text after [fold.Name] folds
// both comes next, so the text can differ in letter case and in accents.
// Otherwise a group matches when its name contains the text after the same
// fold, or when the full name of one of its copies contains the text. The
// one such group wins.
//
// When no group matches after [fold.Name], the selection runs the same
// tests after [fold.Lookalike] folds each text. That fold also reads a
// digit that a name writes in place of a letter as that letter. A text
// that matches a group after [fold.Name] therefore never competes with a
// group that matches only after [fold.Lookalike]. A name that selects no
// group or more than one returns an error that wraps [ErrUnknownWeapon].
package inventory
