package main

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

const timeLayout = "2006-01-02 15:04 MST"

// tagLabels maps the tag ids of the web client to its display labels.
var tagLabels = map[string]string{
	"keep":      "Keep",
	"pvp":       "PvP",
	"pve":       "PvE",
	"junk":      "Junk",
	"infuse":    "Infuse",
	"archive":   "Archive",
	tagFavorite: "Favorite",
}

func renderStatus(t Tokens, signedIn bool, serviceURL, tokenPath string, now time.Time) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Service: %s\n", serviceURL)
	fmt.Fprintf(&b, "Token file: %s\n", tokenPath)

	if !signedIn {
		b.WriteString("Signed in: no\n\nCall the login tool to sign in.")

		return b.String()
	}

	// The sign-in lasts as long as the refresh token. Without a refresh
	// token it ends with the access token.
	refreshes := t.RefreshToken != "" && !t.RefreshExpires.IsZero()

	expires := t.AccessExpires
	if refreshes {
		expires = t.RefreshExpires
	}

	if !now.Before(expires) {
		fmt.Fprintf(&b, "Signed in: no\n\nThe sign-in of Bungie.net membership %s expired on %s. "+
			"Call the login tool to sign in again.", t.BungieID, expires.Format(timeLayout))

		return b.String()
	}

	b.WriteString("Signed in: yes\n")
	fmt.Fprintf(&b, "Bungie.net membership: %s\n", t.BungieID)

	if !t.Membership.IsZero() {
		fmt.Fprintf(&b, "Destiny membership: %d/%s\n", t.Membership.Type, t.Membership.ID)
	}

	fmt.Fprintf(&b, "Access token expires: %s\n", t.AccessExpires.Format(timeLayout))

	if refreshes {
		fmt.Fprintf(&b, "Sign-in expires: %s", t.RefreshExpires.Format(timeLayout))
	} else {
		b.WriteString("Sign-in expires with the access token, because Bungie issued no refresh token.")
	}

	return b.String()
}

func renderCharacters(s *Snapshot) string {
	var b strings.Builder

	b.WriteString("Characters, most recently played first:\n")

	for _, c := range s.Characters {
		held, equipped, postmaster := 0, 0, 0

		for _, item := range s.Items {
			switch {
			case item.Owner != c.ID:
			case item.Postmaster:
				postmaster++
			case item.Equipped:
				equipped++
			default:
				held++
			}
		}

		fmt.Fprintf(&b,
			"- %s | id %s | power %d | last played %s | %d equipped, %d in inventory, %d at the Postmaster\n",
			c.Class, c.ID, c.Power, c.LastPlayed.Format(timeLayout), equipped, held, postmaster,
		)
		fmt.Fprintf(&b, "  Slots used: %s\n", slotsList(s, c.ID, func(bucket Bucket) bool { return !bucket.Account }))
	}

	vault := Bucket{Hash: bucketVault, Account: true}

	for _, bucket := range s.Buckets {
		if bucket.Hash == bucketVault {
			vault = bucket
		}
	}

	fmt.Fprintf(&b, "\nVault: %s slots used\n", slotsLabel(s.SlotsUsed(vault, ownerVault), vault.Capacity))

	account := slotsList(s, ownerAccount, func(bucket Bucket) bool {
		return bucket.Account && bucket.Hash != bucketVault
	})
	if account != "" {
		fmt.Fprintf(&b, "Account-wide slots used: %s\n", account)
	}

	if len(s.Currencies) == 0 {
		b.WriteString("\nCurrencies: none")

		return b.String()
	}

	b.WriteString("\nCurrencies:\n")

	for _, currency := range s.Currencies {
		fmt.Fprintf(&b, "- %s: %d\n", currency.Name, currency.Quantity)
	}

	return strings.TrimRight(b.String(), "\n")
}

// slotsList names each bucket that keep selects with the slots that the
// items of owner take there, such as "Kinetic Weapons 7/10".
func slotsList(s *Snapshot, owner string, keep func(Bucket) bool) string {
	var parts []string

	for _, bucket := range s.Buckets {
		if keep(bucket) {
			parts = append(parts, bucket.Name+" "+slotsLabel(s.SlotsUsed(bucket, owner), bucket.Capacity))
		}
	}

	return strings.Join(parts, ", ")
}

// slotsLabel writes used slots out of capacity, such as "7/10". It writes
// the used slots alone when the capacity is unknown.
func slotsLabel(used, capacity int) string {
	if capacity <= 0 {
		return strconv.Itoa(used)
	}

	return fmt.Sprintf("%d/%d", used, capacity)
}

// renderItems renders one page of items as a table. The stat total column
// is empty for every item without a stat total, such as a weapon. A nonempty
// stat adds a column with the value of that stat, and perks adds a column
// with the equipped perks.
//
// Sources holds the [RatingSource] of each game mode, and nil leaves the
// roll ratings out. With sources, the table gains one rating column for
// each game mode, between the flags and the perks. The cell of a weapon
// holds the text of [ratingLabel], and the cell of every other item is
// empty. One line for each game mode without ratings gives the reason,
// between the first line and the table. A result without a table has no
// such line.
func renderItems(
	s *Snapshot,
	items []*Item,
	offset, total int,
	stat string,
	perks bool,
	sources map[string]RatingSource,
) string {
	if total == 0 {
		return "No items match."
	}

	if len(items) == 0 {
		return fmt.Sprintf("Offset %d is past the last of %d matching items.", offset, total)
	}

	var b strings.Builder

	fmt.Fprintf(&b, "Items %d-%d of %d.", offset+1, offset+len(items), total)

	if offset+len(items) < total {
		fmt.Fprintf(&b, " Pass offset=%d for the next page.", offset+len(items))
	}

	if sources != nil {
		for _, reason := range ratingReasons(sources) {
			b.WriteString("\n" + reason)
		}
	}

	b.WriteString("\n\ninstance id | name | type | tier | power | stat total")

	if stat != "" {
		b.WriteString(" | " + strings.ToLower(stat))
	}

	b.WriteString(" | location | tag | flags")

	if sources != nil {
		for _, mode := range perkModes {
			b.WriteString(" | " + mode + " rating")
		}
	}

	if perks {
		b.WriteString(" | perks")
	}

	b.WriteString("\n")

	for _, item := range items {
		id := item.InstanceID
		if id == "" {
			id = fmt.Sprintf("stack of %d (item hash %d)", item.Quantity, item.Hash)
		}

		fields := []string{id, item.Name, typeLabel(item), item.Tier, powerLabel(item), totalLabel(item)}

		if stat != "" {
			fields = append(fields, statLabel(item, stat))
		}

		fields = append(fields, s.LocationLabel(item), item.Tag, strings.Join(flagsOf(item), ", "))

		if sources != nil {
			for _, mode := range perkModes {
				fields = append(fields, ratingCell(sources[mode], item))
			}
		}

		if perks {
			names := make([]string, 0, len(item.Perks))
			for _, perk := range item.Perks {
				names = append(names, perk.Name)
			}

			fields = append(fields, strings.Join(names, ", "))
		}

		b.WriteString(strings.Join(fields, " | "))
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

// renderPreview renders one page of the items that an autotag rule query
// matches. Total counts the matches, and owned counts the items that the
// preview evaluated. A first line gives both counts before the output of
// [renderItems].
//
// Undecided holds the items whose outcome the server cannot decide. When
// it holds an item, the first line also counts those items, and the
// section of [writeUndecided] follows after a blank line. The section
// shows at most limit items, and the offset pages only the matches.
func renderPreview(s *Snapshot, page []*Item, offset, total, owned int, undecided []undecidedItem, limit int) string {
	if total == 0 && len(undecided) == 0 {
		return "No owned item matches the query."
	}

	var b strings.Builder

	if total == 0 {
		b.WriteString("No owned item matches the query")
	} else {
		fmt.Fprintf(&b, "%d of %d owned items match", total, owned)
	}

	if len(undecided) > 0 {
		fmt.Fprintf(&b, ", and the server cannot decide %d more", len(undecided))
	}

	b.WriteString(".")

	if total > 0 {
		b.WriteString("\n" + renderItems(s, page, offset, total, "", false, nil))
	}

	if len(undecided) > 0 {
		b.WriteString("\n\n")
		writeUndecided(&b, s, undecided, limit)
	}

	return b.String()
}

// writeUndecided writes the section of a preview that lists the items
// whose outcome the server cannot decide. A heading line and a header line
// open the section. Each row gives the instance id, the name, the
// location, and the reasons of one item. The section shows the first
// limit items of undecided. A last line counts the items that it leaves
// out.
func writeUndecided(b *strings.Builder, s *Snapshot, undecided []undecidedItem, limit int) {
	b.WriteString("The server cannot decide these items:\ninstance id | name | location | reason")

	shown := undecided[:min(limit, len(undecided))]
	for _, u := range shown {
		fields := []string{u.item.InstanceID, u.item.Name, s.LocationLabel(u.item), strings.Join(u.reasons, ". ")}
		b.WriteString("\n" + strings.Join(fields, " | "))
	}

	rest := len(undecided) - len(shown)
	if rest > 0 {
		fmt.Fprintf(b, "\n%d more undecided items are not shown.", rest)
	}
}

// saveCount is the outcome of counting the owned items that the query of a
// saved autotag rule matches. It holds at most one of unsupported, err,
// noCharacter, and unrated. When it holds none of them, matches and
// undecided are the counts.
type saveCount struct {
	// err is the error of the inventory read.
	err error
	// unrated is the [ratingReason] of a game mode without ratings that
	// the query reads.
	unrated string
	// unsupported holds the terms of the query that the preview does not
	// evaluate, in the order that [compiledQuery] gives them.
	unsupported []string
	// matches is the number of owned items that the query matches.
	matches int
	// undecided is the number of owned items whose outcome the server
	// cannot decide.
	undecided int
	// noCharacter reports an account without a character.
	noCharacter bool
}

// renderSaveCount renders the line of a save_autotag_rule result that
// follows the rule line when the call passes a query.
func renderSaveCount(count saveCount) string {
	switch {
	case len(count.unsupported) > 0:
		return fmt.Sprintf(
			"The server does not evaluate %s, so it did not count the items that the query matches. "+
				"The web client might still accept those terms and run the rule.",
			strings.Join(count.unsupported, ", "),
		)

	case count.err != nil:
		return fmt.Sprintf(
			"The rule is saved, but the server could not read the inventory "+
				"to count the items that the query matches: %v",
			count.err,
		)

	case count.noCharacter:
		return "The rule is saved, but the account has no character, " +
			"so the server did not count the items that the query matches."

	case count.unrated != "":
		return "The rule is saved, but the query reads a game mode without ratings, " +
			"so the server did not count the items that the query matches. " + count.unrated

	case count.undecided > 0:
		return fmt.Sprintf(
			"The query matches %d owned items now, and the server cannot decide %d more. %s",
			count.matches, count.undecided, saveCountCaveat,
		)

	default:
		return fmt.Sprintf("The query matches %d owned items now. %s", count.matches, saveCountCaveat)
	}
}

// saveCountCaveat is the sentence that ends the count line of a
// save_autotag_rule result.
const saveCountCaveat = "The count ignores the other rules, so an earlier rule can take some of these items first."

// renderItem renders one item in full. For a weapon, ratings holds the
// [Rating] of each game mode, and the output gains the roll ratings
// block. Ratings is nil for every other item.
func renderItem(s *Snapshot, item *Item, ratings map[string]Rating) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s\n", item.Name)
	fmt.Fprintf(&b, "Instance id: %s\n", item.InstanceID)
	fmt.Fprintf(&b, "Item hash: %d\n", item.Hash)
	fmt.Fprintf(&b, "Type: %s\n", typeLabel(item))

	writeField(&b, "Tier", item.Tier)
	writeField(&b, "Slot", item.Slot)
	writeField(&b, "Class", item.Class)
	writeField(&b, "Power", powerLabel(item))

	fmt.Fprintf(&b, "Location: %s\n", s.LocationLabel(item))

	writeField(&b, "Tag", item.Tag)
	writeField(&b, "Flags", strings.Join(flagsOf(item), ", "))
	writeField(&b, "Note", item.Note)

	if len(item.Perks) > 0 {
		b.WriteString("\nPerks:\n")

		for _, perk := range item.Perks {
			fmt.Fprintf(&b, "- %s (%s)", perk.Name, perk.Type)

			if len(perk.Options) > 0 {
				fmt.Fprintf(&b, ", can swap to %s", strings.Join(perk.OptionNames(), ", "))
			}

			b.WriteString("\n")
			writeDescription(&b, perk.Description)
		}
	}

	if ratings != nil {
		b.WriteString("\nRoll ratings:\n")

		for _, mode := range perkModes {
			writeRating(&b, mode, ratings[mode])
		}
	}

	if len(item.Stats) > 0 {
		b.WriteString("\nStats:\n")

		for _, stat := range item.Stats {
			fmt.Fprintf(&b, "- %s: %d\n", stat.Name, stat.Value)
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// renderDuplicates renders one page of the weapons that have more than one
// copy. Offset and total count weapons, not copies.
//
// Sources holds the [RatingSource] of each game mode. Every table of
// the page shows the rating columns of [renderWeaponGroup]. One line
// for each game mode without ratings gives the reason, between the
// first line and the first table. The lines print one time for the
// page, also when the page holds several tables. A result without a
// table has no such line.
func renderDuplicates(
	s *Snapshot,
	groups []WeaponGroup,
	offset, total int,
	sources map[string]RatingSource,
) string {
	if total == 0 {
		return "No weapon has more than one copy."
	}

	if len(groups) == 0 {
		return fmt.Sprintf("Offset %d is past the last of %d weapons that have more than one copy.", offset, total)
	}

	var b strings.Builder

	fmt.Fprintf(&b, "Weapons %d-%d of %d that have more than one copy.", offset+1, offset+len(groups), total)

	if offset+len(groups) < total {
		fmt.Fprintf(&b, " Pass offset=%d for the next page.", offset+len(groups))
	}

	for _, reason := range ratingReasons(sources) {
		b.WriteString("\n" + reason)
	}

	for _, group := range groups {
		b.WriteString("\n\n")
		b.WriteString(renderWeaponGroup(s, group, sources))
	}

	return b.String()
}

// renderSingleGroup renders the copies of one weapon as a whole result.
// The output is the table of [renderWeaponGroup]. Before the table, one
// line for each game mode without ratings gives the reason, and a blank
// line follows those lines. When every game mode has ratings, the
// output starts with the title of the table.
func renderSingleGroup(s *Snapshot, group WeaponGroup, sources map[string]RatingSource) string {
	table := renderWeaponGroup(s, group, sources)

	reasons := ratingReasons(sources)
	if len(reasons) == 0 {
		return table
	}

	return strings.Join(reasons, "\n") + "\n\n" + table
}

// renderWeaponGroup renders the copies of one weapon as a table with one
// row per copy. The name column tells a version such as "(Adept)" from the
// base weapon.
//
// Sources holds the [RatingSource] of each game mode, with an entry for
// every mode in [perkModes]. The table has one rating column for each
// game mode, between the flags and the perks. Each rating cell holds
// the [ratingCell] of its copy. A version such as "(Adept)" therefore
// gets the rating of its own item hash, not the rating of the base
// weapon. One result can hold several tables, so the caller writes the
// reason of each game mode without ratings.
func renderWeaponGroup(s *Snapshot, group WeaponGroup, sources map[string]RatingSource) string {
	var b strings.Builder

	noun := "copies"
	if len(group.Copies) == 1 {
		noun = "copy"
	}

	fmt.Fprintf(&b, "%s: %d %s\n\n", group.Name, len(group.Copies), noun)
	b.WriteString("instance id | name | type | power | location | tag | flags")

	for _, mode := range perkModes {
		b.WriteString(" | " + mode + " rating")
	}

	b.WriteString(" | perks\n")

	for _, item := range group.Copies {
		fields := []string{
			item.InstanceID,
			item.Name,
			typeLabel(item),
			powerLabel(item),
			s.LocationLabel(item),
			item.Tag,
			strings.Join(flagsOf(item), ", "),
		}

		for _, mode := range perkModes {
			fields = append(fields, ratingCell(sources[mode], item))
		}

		perks := make([]string, 0, len(item.Perks))
		for _, perk := range item.Perks {
			perks = append(perks, perkLabel(perk))
		}

		fields = append(fields, strings.Join(perks, "; "))

		b.WriteString(strings.Join(fields, " | "))
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

// renderLoadouts lists the saved loadouts of characters as one table per
// character, with a row per loadout item. The [loadoutHeading] of each
// loadout sits above its rows. When names holds no table, one line after
// the opening paragraph gives the reason.
func renderLoadouts(s *Snapshot, characters []Character, names LoadoutTableState) string {
	var b strings.Builder

	b.WriteString("Loadouts saved in the game. A loadout number is the position of the loadout " +
		"on the in-game loadout screen, counted from 1. The server cannot show the icon or the color " +
		"of a loadout, because the game defines both only as images. " +
		"Pass the character and the loadout number to equip_loadout.")

	if names.Err != nil {
		b.WriteString("\nLoadout names unavailable: " + names.Err.Error())
	}

	for _, c := range characters {
		label := s.OwnerLabel(c.ID)
		saved := s.LoadoutsOf(c.ID)

		if len(saved) == 0 {
			fmt.Fprintf(&b, "\n\n%s (id %s): no saved loadouts", label, c.ID)

			continue
		}

		fmt.Fprintf(&b, "\n\n%s (id %s):\n", label, c.ID)
		b.WriteString("loadout | instance id | name | slot | location")

		for _, l := range saved {
			b.WriteString("\n" + loadoutHeading(l, names))

			for _, id := range l.InstanceIDs {
				item, ok := s.Find(id)
				if !ok {
					fmt.Fprintf(&b, "\n%d | %s | not in the inventory |  | ", l.Number, id)

					continue
				}

				fmt.Fprintf(&b, "\n%d | %s | %s | %s | %s",
					l.Number, item.InstanceID, item.Name, item.Slot, s.LocationLabel(item))
			}
		}
	}

	return b.String()
}

// loadoutHeading returns the heading line of one saved loadout, such as
// "Loadout 2: Raid Night". A name hash that the table lacks, or an entry
// without text, reads "unnamed". When names holds no table, the line
// gives only the number.
func loadoutHeading(l Loadout, names LoadoutTableState) string {
	if names.Table == nil {
		return fmt.Sprintf("Loadout %d", l.Number)
	}

	name := names.Table[l.NameHash].Name
	if name == "" {
		name = "unnamed"
	}

	return fmt.Sprintf("Loadout %d: %s", l.Number, name)
}

// renderLoadoutEquip describes what an equip_loadout call did. A non-nil
// err says why the call stopped.
func renderLoadoutEquip(s *Snapshot, l Loadout, result LoadoutEquip, err error) string {
	var b strings.Builder

	target := s.OwnerLabel(l.CharacterID)

	for _, move := range result.Moves {
		// A move that starts on the character of the loadout pulls the
		// item from the Postmaster.
		from := s.OwnerLabel(move.From)
		if move.From == l.CharacterID {
			from = "the Postmaster"
		}

		fmt.Fprintf(&b, "- Moved %s (%s) from %s to %s.", move.Item.Name, move.Item.InstanceID, from, target)

		if move.Replacement != nil {
			fmt.Fprintf(&b, " Equipped %s (%s) in its place on %s.",
				move.Replacement.Name, move.Replacement.InstanceID, s.OwnerLabel(move.From))
		}

		b.WriteString("\n")
	}

	if stopped := result.Stopped; stopped != nil {
		fmt.Fprintf(&b, "- %s (%s) did not reach %s. It is at %s.",
			stopped.Item.Name, stopped.Item.InstanceID, target, s.LocationLabel(stopped.Item))

		if stopped.Replacement != nil {
			fmt.Fprintf(&b, " Equipped %s (%s) in its place on %s.",
				stopped.Replacement.Name, stopped.Replacement.InstanceID, s.OwnerLabel(stopped.From))
		}

		b.WriteString("\n")
	}

	if err != nil {
		// A Bungie message ends with its own period.
		reason := strings.TrimRight(err.Error(), ".")
		fmt.Fprintf(&b, "Loadout %d is not equipped on %s: %s.", l.Number, target, reason)

		return b.String()
	}

	fmt.Fprintf(&b, "Equipped loadout %d on %s", l.Number, target)

	if len(result.Items) > 0 {
		fmt.Fprintf(&b, ": %s", itemNames(result.Items))
	}

	b.WriteString(".")

	if len(result.Missing) > 0 {
		fmt.Fprintf(&b, " The inventory does not hold these loadout items: %s.", strings.Join(result.Missing, ", "))
	}

	return b.String()
}

// renderLoadoutSave describes a save_loadout call that saved the gear of
// a character into the loadout slot number. Names is the state of the
// name table, which names the loadout by nameHash. When names holds no
// table, the call kept the name of a saved loadout, and the result gives
// the reason that the name is unknown.
func renderLoadoutSave(s *Snapshot, characterID string, number int, nameHash uint32, names LoadoutTableState) string {
	var b strings.Builder

	target := s.OwnerLabel(characterID)

	switch name := names.Table[nameHash].Name; {
	case names.Table == nil:
		fmt.Fprintf(&b, "Saved loadout %d on %s, which keeps its name.", number, target)
	case name == "":
		fmt.Fprintf(&b, "Saved loadout %d on %s without a name.", number, target)
	default:
		fmt.Fprintf(&b, "Saved loadout %d on %s with the name %s.", number, target, name)
	}

	gear := s.LoadoutGear(characterID)
	if len(gear) == 0 {
		fmt.Fprintf(&b, " The inventory shows no gear that %s has equipped.", target)
	} else {
		fmt.Fprintf(&b, " It holds the gear that %s has equipped: %s.", target, itemNames(gear))
	}

	if names.Err != nil {
		b.WriteString("\nLoadout names unavailable: " + names.Err.Error())
	}

	return b.String()
}

// renderLoadoutClear describes a clear_loadout call that emptied the slot
// of the saved loadout l.
func renderLoadoutClear(s *Snapshot, l Loadout) string {
	var (
		held    []*Item
		missing []string
	)

	for _, id := range l.InstanceIDs {
		item, ok := s.Find(id)
		if !ok {
			missing = append(missing, id)

			continue
		}

		held = append(held, item)
	}

	var b strings.Builder

	fmt.Fprintf(&b, "Cleared loadout %d on %s", l.Number, s.OwnerLabel(l.CharacterID))

	if len(held) > 0 {
		b.WriteString(", which held " + itemNames(held))
	}

	b.WriteString(".")

	if len(missing) > 0 {
		fmt.Fprintf(&b, " The inventory does not hold these loadout items: %s.", strings.Join(missing, ", "))
	}

	return b.String()
}

// itemNames lists items by name and instance id, such as
// "Fatebringer (1006), Celestial Nighthawk (1004)".
func itemNames(items []*Item) string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, fmt.Sprintf("%s (%s)", item.Name, item.InstanceID))
	}

	return strings.Join(names, ", ")
}

// renderNoJunk reports that no junk sits in bucket on character c and
// that none waits.
func renderNoJunk(s *Snapshot, c Character, bucket Bucket, plan JunkPlan) string {
	lines := []string{fmt.Sprintf("No junk is left for %s on %s.", bucket.Name, s.OwnerLabel(c.ID))}
	lines = appendSkippedJunk(lines, plan.Skipped)
	lines = append(lines, "If you moved other items out of the slot for this run, move them back with move_items.")

	return strings.Join(lines, "\n")
}

// renderStagedJunk lists the junk that bucket on character c holds after
// a stage_junk call. Plan is the plan after the moves, and failures holds
// one line for each move that did not succeed.
func renderStagedJunk(s *Snapshot, c Character, bucket Bucket, plan JunkPlan, failures []string) string {
	staged := len(plan.Staged)
	lines := []string{fmt.Sprintf(
		"Unequipped items in %s on %s after this call: %d", bucket.Name, s.OwnerLabel(c.ID), staged,
	)}

	locked := false

	for _, item := range plan.Staged {
		line := fmt.Sprintf("- %s (%s)", item.Name, item.InstanceID)
		if item.Locked {
			line += ", locked"
			locked = true
		}

		lines = append(lines, line)
	}

	if locked {
		lines = append(lines, "The game does not let the player dismantle a locked item. set_lock unlocks it.")
	}

	lines = append(lines, failures...)
	lines = append(lines, fmt.Sprintf("Junk that waits for the next call: %d", len(plan.Waiting)))
	lines = appendSkippedJunk(lines, plan.Skipped)

	if staged == 0 {
		lines = append(lines, "No junk is in the slot, so the user has nothing to dismantle. "+
			"Call stage_junk again to stage the junk that waits.")

		return strings.Join(lines, "\n")
	}

	lines = append(lines, fmt.Sprintf(
		"Each listed item is tagged junk, and the user dismantles them in the game. "+
			"Do not dismantle when the number of unequipped items that the game shows in the slot is higher than %d, "+
			"because stage_junk did not check an item that reached the slot after the last inventory fetch. "+
			"Call stage_junk again after the user dismantled them. "+
			"A call in the next %d seconds might reuse this inventory copy and list the same items.",
		staged, int(snapshotTTL/time.Second),
	))

	return strings.Join(lines, "\n")
}

// appendSkippedJunk adds the line that counts the skipped junk of a slot
// to lines. It leaves lines as they are when skipped is 0.
func appendSkippedJunk(lines []string, skipped int) []string {
	if skipped == 0 {
		return lines
	}

	return append(lines, fmt.Sprintf("Junk of this slot that stage_junk skips: %d. "+
		"Each one is equipped, at the Postmaster, not transferable, or for another class.", skipped))
}

// keeperRefusal returns the [ErrMove] error of a stage_junk call that
// finds keepers in bucket on character c. The error names each keeper
// and tells the model how to move the keepers out and back.
func keeperRefusal(s *Snapshot, c Character, bucket Bucket, keepers []*Item) error {
	names := make([]string, 0, len(keepers))
	ids := make([]string, 0, len(keepers))

	for _, item := range keepers {
		names = append(names, fmt.Sprintf("%s (%s)", item.Name, item.InstanceID))
		ids = append(ids, item.InstanceID)
	}

	//nolint:staticcheck // ST1005: The model reads the text as the tool result, which ends in a sentence.
	return fmt.Errorf(
		"%w: %s on %s holds unequipped gear that is not tagged junk: %s. stage_junk moved nothing. "+
			"Move that gear to the vault with move_items, with target vault and instance_ids %s. "+
			"Then call stage_junk again. After stage_junk reports that no junk is left, "+
			"move that gear back with move_items and target %s.",
		ErrMove, bucket.Name, s.OwnerLabel(c.ID), strings.Join(names, ", "), strings.Join(ids, ", "), c.ID,
	)
}

// perkLabel names a perk and the other plugs that its socket can hold.
func perkLabel(perk Perk) string {
	if len(perk.Options) == 0 {
		return perk.Name
	}

	return perk.Name + " (can swap to " + strings.Join(perk.OptionNames(), ", ") + ")"
}

// writeRating writes the roll rating of one game mode. The first line
// gives the tier of the weapon on the sheet of the mode, or the reason
// that the mode has no rating. The [sheetPlace] of the row follows the
// tier. For a copy whose perk columns the rating judged, the line also
// gives the tier of the copy and counts the columns that hit. One line
// for each column follows, numbered from 1 in sheet order. The line of a
// column that hit names each perk that earns the column, and it marks
// each perk that the user likes. The line of every column then names
// the picks of the column, after the words "The sheet picks". A column
// without a pick to name has no such sentence. The note of the row
// comes last, for every copy of the weapon, because the note describes
// the weapon.
func writeRating(b *strings.Builder, mode string, rating Rating) {
	if rating.Err != nil {
		fmt.Fprintf(b, "- %s\n", ratingReason(mode, rating.Err))

		return
	}

	fmt.Fprintf(b, "- %s: ", modeLabels[mode])

	label := sheetLabels[mode]

	roll := rating.Roll
	if roll == nil {
		fmt.Fprintf(b, "not on the %s\n", label)

		return
	}

	// The tier of the copy can differ from the tier of the row only
	// when the rating judged a column.
	if roll.Overall != "" && len(roll.Columns) > 0 {
		fmt.Fprintf(b, "%s for this roll, ", roll.Overall)
	}

	tier := roll.Row.Tier
	if tier == "" {
		tier = "no tier"
	}

	fmt.Fprintf(b, "%s on the %s %s", tier, label, sheetPlace(roll.Row))

	hits := 0

	for _, column := range roll.Columns {
		if !column.Missed() {
			hits++
		}
	}

	switch {
	case roll.Unknown:
		b.WriteString(", roll unknown: the profile lists no perks for this copy\n")
	case len(roll.Columns) == 0:
		b.WriteString(", which judges no perk columns\n")
	default:
		fmt.Fprintf(b, ", %d of %d perk columns hit\n", hits, len(roll.Columns))
	}

	for i, column := range roll.Columns {
		verdict := "miss"

		if !column.Missed() {
			names := make([]string, 0, len(column.Hits))
			for _, hit := range column.Hits {
				name := hit.Name
				if hit.Liked {
					name += " (liked)"
				}

				names = append(names, name)
			}

			verdict = "hit with " + strings.Join(names, ", ")
		}

		if len(column.Picks) > 0 {
			verdict += ". The sheet picks " + strings.Join(column.Picks, ", ")
		}

		fmt.Fprintf(b, "  Column %d: %s\n", i+1, verdict)
	}

	writeSheetNote(b, roll.Row.Notes)
}

// sheetPlace returns the parenthesis that places a row on its sheet. It
// reads "(rank 3 of 62 in HCs)" for the third of the 62 rows in the
// category HCs, and "(in HCs)" for a row without a rank. The category
// prints in the text of the sheet file.
func sheetPlace(row SheetRow) string {
	if row.Rank > 0 {
		return fmt.Sprintf("(rank %d of %d in %s)", row.Rank, row.Ranked, row.Category)
	}

	return fmt.Sprintf("(in %s)", row.Category)
}

// writeSheetNote writes the note of a sheet row, indented under the
// rating line of its game mode. Each nonblank line of the note becomes
// one output line, and the first carries the label "Sheet note: ". The
// function drops the blank lines, because a blank line reads as the end
// of the roll ratings block. A note without a nonblank line writes
// nothing.
func writeSheetNote(b *strings.Builder, notes string) {
	label := "Sheet note: "

	for line := range strings.Lines(notes) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fmt.Fprintf(b, "  %s%s\n", label, line)

		label = ""
	}
}

// ratingReason returns the text that says why one game mode has no
// ratings. The text is the label of the mode and the text of err, such
// as "PvP: rating unavailable: the service publishes no PvP sheet".
// Every output that reports the reason of a mode prints this text.
func ratingReason(mode string, err error) string {
	return modeLabels[mode] + ": " + err.Error()
}

// ratingReasons returns the [ratingReason] of each game mode whose
// source holds an Err, in the order of [perkModes]. It returns no line
// when every mode has ratings.
func ratingReasons(sources map[string]RatingSource) []string {
	var reasons []string

	for _, mode := range perkModes {
		err := sources[mode].Err
		if err != nil {
			reasons = append(reasons, ratingReason(mode, err))
		}
	}

	return reasons
}

// ratingCell returns the rating cell that the source of one game mode
// gives item in a table. The cell of a weapon holds the [ratingLabel] of
// its [Rating]. Only weapons have roll ratings, so the cell of every
// other item is empty, also when the mode has no ratings.
func ratingCell(source RatingSource, item *Item) string {
	if item.Kind != kindWeapon {
		return ""
	}

	return ratingLabel(rate(source, item))
}

// ratingLabel returns the text of a rating cell for one [Rating]. The
// text names the first of these states that holds:
//
//   - "unavailable": the game mode has no ratings.
//   - "not listed": the sheet does not list the weapon.
//   - "no tier": the row of the weapon has no tier.
//   - "roll unknown (sheet S)": the profile lists no perks for the copy,
//     on a row with the tier S and perk columns.
//   - "S": the roll keeps the tier S of the row.
//   - "A (sheet S)": the roll moved down from the tier S of the row.
//
// A row without a tier reads "no tier" for every copy, because each
// later text names the tier of the row.
func ratingLabel(rating Rating) string {
	roll := rating.Roll

	switch {
	case rating.Err != nil:
		return "unavailable"
	case roll == nil:
		return "not listed"
	case roll.Row.Tier == "":
		return "no tier"
	case roll.Unknown:
		return fmt.Sprintf("roll unknown (sheet %s)", roll.Row.Tier)
	case roll.Overall == roll.Row.Tier:
		return roll.Overall
	default:
		return fmt.Sprintf("%s (sheet %s)", roll.Overall, roll.Row.Tier)
	}
}

// writeDescription writes each line of a perk description indented under
// its list item. It drops the blank lines between paragraphs, so the perk
// list has no gaps.
func writeDescription(b *strings.Builder, description string) {
	for line := range strings.Lines(description) {
		line = strings.TrimSpace(line)
		if line != "" {
			fmt.Fprintf(b, "  %s\n", line)
		}
	}
}

func writeField(b *strings.Builder, label, value string) {
	if value != "" {
		fmt.Fprintf(b, "%s: %s\n", label, value)
	}
}

func typeLabel(item *Item) string {
	if item.Damage == "" {
		return item.TypeName
	}

	return item.Damage + " " + item.TypeName
}

func powerLabel(item *Item) string {
	if item.Power == 0 {
		return ""
	}

	return strconv.Itoa(item.Power)
}

func totalLabel(item *Item) string {
	total, ok := item.StatTotal()
	if !ok {
		return ""
	}

	return strconv.Itoa(total)
}

func statLabel(item *Item, name string) string {
	value, ok := item.StatValue(name)
	if !ok {
		return ""
	}

	return strconv.Itoa(value)
}

func flagsOf(item *Item) []string {
	var flags []string

	if item.Favorite {
		flags = append(flags, "favorite")
	}

	if item.Locked {
		flags = append(flags, "locked")
	}

	if item.Masterwork {
		flags = append(flags, "masterwork")
	}

	if item.Crafted {
		flags = append(flags, "crafted")
	}

	if item.Note != "" {
		flags = append(flags, "note")
	}

	if item.InstanceID != "" && !item.Transferable {
		flags = append(flags, "not transferable")
	}

	return flags
}

// historyRun is one row of the activity history. Character names the
// character that played the run.
type historyRun struct {
	Character string

	HistoryPeriod
}

// renderActivityHistory renders runs in their order. Scope names the owner
// of the runs, such as "Hunter" or "the account". Limit is the number of
// runs that the caller asked for.
func renderActivityHistory(defs *Defs, runs []historyRun, scope string, limit int) string {
	if len(runs) == 0 {
		return "The activity history of " + scope + " holds no runs."
	}

	var b strings.Builder

	if len(runs) == 1 {
		fmt.Fprintf(&b, "The newest run of %s.", scope)
	} else {
		fmt.Fprintf(&b, "The %d newest runs of %s.", len(runs), scope)
	}

	if len(runs) == limit && limit < maxHistoryLimit {
		fmt.Fprintf(&b, " Pass a larger limit, up to %d, for older runs.", maxHistoryLimit)
	}

	b.WriteString("\n\nstarted | character | activity | duration | result | kills | deaths | instance id\n")

	for _, run := range runs {
		result := "incomplete"
		if run.Value("completed") == 1 {
			result = "completed"
		}

		seconds := math.Round(run.Value("activityDurationSeconds"))

		b.WriteString(strings.Join([]string{
			run.Period.UTC().Format(timeLayout),
			run.Character,
			activityName(defs, run.Activity),
			(time.Duration(seconds) * time.Second).String(),
			result,
			strconv.Itoa(int(math.Round(run.Value("kills")))),
			strconv.Itoa(int(math.Round(run.Value("deaths")))),
			run.Activity.InstanceID,
		}, " | "))
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

// activityName names the activity of a run. It looks up the reference id
// first and the director activity hash second, in the order of the web
// client. It skips a definition without a name, and it names a run that
// neither hash names "Activity <reference id>".
func activityName(defs *Defs, activity HistoryActivity) string {
	for _, hash := range []uint32{activity.ReferenceID, activity.DirectorActivityHash} {
		def := defs.Activities[hash]
		if def.Name != "" {
			return def.Name
		}
	}

	return fmt.Sprintf("Activity %d", activity.ReferenceID)
}

// renderVendors renders vendors as a table with one row per vendor. Owner
// names the character that sees the vendors, such as "Hunter".
func renderVendors(vendors []Vendor, owner string) string {
	if len(vendors) == 0 {
		return "Bungie lists no vendors for " + owner + "."
	}

	var b strings.Builder

	if len(vendors) == 1 {
		fmt.Fprintf(&b, "The vendor of %s.", owner)
	} else {
		fmt.Fprintf(&b, "The %d vendors of %s.", len(vendors), owner)
	}

	b.WriteString(" Pass vendor with a hash or a name to list the sales of one vendor.")
	b.WriteString("\n\nname | subtitle | hash | sales | next refresh | flags\n")

	for _, v := range vendors {
		b.WriteString(strings.Join([]string{
			v.Name,
			v.Subtitle,
			strconv.FormatUint(uint64(v.Hash), 10),
			strconv.Itoa(v.SaleCount()),
			refreshLabel(v.NextRefresh),
			strings.Join(vendorFlags(v), ", "),
		}, " | "))
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

// renderVendor renders the sales of one vendor under the names of their
// categories. Owner names the character that sees the stock.
func renderVendor(v Vendor, owner string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s\n", v.Name)
	writeField(&b, "Subtitle", v.Subtitle)
	fmt.Fprintf(&b, "Vendor hash: %d\n", v.Hash)
	fmt.Fprintf(&b, "Character: %s\n", owner)
	fmt.Fprintf(&b, "Next refresh: %s\n", refreshLabel(v.NextRefresh))
	writeField(&b, "Flags", strings.Join(vendorFlags(v), ", "))
	fmt.Fprintf(&b, "Sales: %d\n", v.SaleCount())

	for _, group := range v.Groups {
		fmt.Fprintf(&b, "\n%s:\n", group.Name)

		for _, sale := range group.Sales {
			fmt.Fprintf(&b, "- %s | %s | %s\n", saleLabel(sale), costLabel(sale.Costs), statusLabel(sale))
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// refreshLabel formats the next refresh time of a vendor. Bungie reports
// the year 9999 for a vendor whose stock never rotates, and the label of
// that time is "never". The label of the zero time is "unknown".
func refreshLabel(t time.Time) string {
	switch {
	case t.IsZero():
		return "unknown"
	case t.Year() >= 9999:
		return "never"
	default:
		return t.UTC().Format(timeLayout)
	}
}

func vendorFlags(v Vendor) []string {
	if v.Disabled {
		return []string{"disabled"}
	}

	return nil
}

// saleLabel names the item of a sale, with the quantity when the sale
// gives more than one.
func saleLabel(sale Sale) string {
	if sale.Quantity > 1 {
		return fmt.Sprintf("%s x%d", sale.Name, sale.Quantity)
	}

	return sale.Name
}

// costLabel lists the costs of a sale, such as "50000 Glimmer, 10
// Legendary Shards". The label of a sale without a cost is "no cost".
func costLabel(costs []Cost) string {
	if len(costs) == 0 {
		return "no cost"
	}

	parts := make([]string, 0, len(costs))
	for _, cost := range costs {
		parts = append(parts, fmt.Sprintf("%d %s", cost.Quantity, cost.Name))
	}

	return strings.Join(parts, ", ")
}

// statusLabel says whether the character can buy the item of a sale. The
// label of an unavailable sale carries its failure strings.
func statusLabel(sale Sale) string {
	if sale.Available {
		return "available"
	}

	if len(sale.Reasons) == 0 {
		return "unavailable"
	}

	return "unavailable: " + strings.Join(sale.Reasons, "; ")
}

// renderActivities renders one page of the activities of a character as a
// table. Listed counts the activities that Bungie lists for the character,
// and kept counts the ones that [readActivities] keeps from them. Offset
// and total count the kept activities that match the query and the
// rewards flag. Owner names the character, such as "Hunter". The power and
// bonus drops columns are empty for an activity without them.
func renderActivities(rows []ActivityRow, offset, total, kept, listed int, owner string) string {
	switch {
	case listed == 0:
		return "Bungie lists no activities that " + owner + " can play."
	case kept == 0 && listed == 1:
		return "The list leaves out the one activity that Bungie lists for " + owner +
			", because the game hides it, the definitions do not name it, or it is a Story or Social activity."

	case kept == 0:
		return fmt.Sprintf("The list leaves out all %d activities that Bungie lists for %s, because the game "+
			"hides them, the definitions do not name them, or they are Story or Social activities.", listed, owner)

	case total == 0:
		return "No activities of " + owner + " match."
	case len(rows) == 0:
		return fmt.Sprintf("Offset %d is past the last of %d matching activities.", offset, total)
	}

	var b strings.Builder

	fmt.Fprintf(&b, "Activities %d-%d of %d for %s, with the rewards that each one still offers this week.",
		offset+1, offset+len(rows), total, owner)

	if offset+len(rows) < total {
		fmt.Fprintf(&b, " Pass offset=%d for the next page.", offset+len(rows))
	}

	b.WriteString("\n\nactivity | type | location | power | bonus drops | focus | bonus loot | flags | hash\n")

	for _, row := range rows {
		var power, drops, focus string

		if row.Power > 0 {
			power = strconv.Itoa(row.Power)
		}

		if row.BonusDrops > 0 {
			drops = strconv.Itoa(row.BonusDrops)
		}

		if row.Focus != nil {
			focus = lootLabel(*row.Focus)
		}

		loot := make([]string, 0, len(row.Loot))
		for _, one := range row.Loot {
			loot = append(loot, lootLabel(one))
		}

		var flags string
		if row.Featured {
			flags = "featured"
		}

		b.WriteString(strings.Join([]string{
			row.Name,
			row.Type,
			row.Location,
			power,
			drops,
			focus,
			strings.Join(loot, ", "),
			flags,
			strconv.FormatUint(uint64(row.Hash), 10),
		}, " | "))
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

// lootLabel names a reward item, with the quantity when Bungie reports
// more than one.
func lootLabel(loot Loot) string {
	if loot.Quantity > 1 {
		return fmt.Sprintf("%s x%d", loot.Name, loot.Quantity)
	}

	return loot.Name
}

func renderTags(s *Snapshot) string {
	counts := map[string]int{}
	favorites := 0

	for _, item := range s.Items {
		if item.Tag != "" {
			counts[item.Tag]++
		}

		if item.Favorite {
			favorites++
		}
	}

	var b strings.Builder

	b.WriteString("An item carries at most one tag. Favorite is a separate flag.\n\n")
	b.WriteString("tag id | label | items held\n")

	for _, id := range tagOrder {
		fmt.Fprintf(&b, "%s | %s | %d\n", id, tagLabels[id], counts[id])
	}

	fmt.Fprintf(&b, "\nFavorites: %d items", favorites)

	return b.String()
}

func renderNotes(s *Snapshot, notes []Note) string {
	var b strings.Builder

	for _, note := range notes {
		if note.Removed {
			continue
		}

		target := note.TargetID

		item, ok := s.Find(note.TargetID)
		if note.Kind == noteInstance && ok {
			target = fmt.Sprintf("%s (%s)", note.TargetID, item.Name)
		}

		fmt.Fprintf(&b, "- %s %s: %s\n", note.Kind, target, note.Body)
	}

	if b.Len() == 0 {
		return "No notes."
	}

	return strings.TrimRight(b.String(), "\n")
}

func renderPerkPreferences(prefs []PerkPreference) string {
	var b strings.Builder

	for _, mode := range perkModes {
		for _, stance := range perkStances {
			var perks []string

			for _, pref := range prefs {
				if !pref.Removed && pref.Mode == mode && pref.Stance == stance {
					perks = append(perks, pref.Perk)
				}
			}

			if len(perks) == 0 {
				continue
			}

			slices.Sort(perks)
			fmt.Fprintf(&b, "%s %s: %s\n", mode, stance, strings.Join(perks, ", "))
		}
	}

	if b.Len() == 0 {
		return "No perk preferences."
	}

	return strings.TrimRight(b.String(), "\n")
}

func renderAutotag(snapshot AutotagSnapshot) string {
	var b strings.Builder

	switch {
	case snapshot.State == nil:
		b.WriteString("Autotagging has never been turned on.\n")
	case snapshot.State.Enabled:
		b.WriteString("Autotagging is on.\n")
	default:
		b.WriteString("Autotagging is off.\n")
	}

	rules := liveRules(snapshot.Rules)
	if len(rules) == 0 {
		b.WriteString("No rules.")

		return b.String()
	}

	b.WriteString("\nRules in evaluation order. The first enabled rule that matches a new item wins.\n")

	for _, rule := range rules {
		b.WriteString(renderRule(rule))
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

func renderRule(rule AutotagRule) string {
	actions := []string{}

	if rule.TagID != "" {
		actions = append(actions, "tag "+rule.TagID)
	}

	if rule.Vault {
		actions = append(actions, "move to vault")
	}

	if len(actions) == 0 {
		actions = append(actions, "no action")
	}

	state := "enabled"
	if !rule.Enabled {
		state = "disabled"
	}

	return fmt.Sprintf(
		"- %d. [%s] %q: %s (id %s)",
		rule.Position,
		state,
		rule.Query,
		strings.Join(actions, ", "),
		rule.ID,
	)
}

// renderNewMatches renders the result of list_autotag_rules with
// matches=new. Stored is the mark that the service stores, and newest is
// the mark that the next claim stores. Evals holds the [ruleEval] of each
// live rule in rule order, and m holds what [firstMatch] gave the new
// items outside the vault. Vault holds the new items in the vault, which
// the output only counts.
//
// The output opens with the marks and says that the call claimed nothing. A
// line for each rule follows, with the items that the rule takes. The
// lines stop at the first enabled rule that blocks, which lists the items
// that reach it. The undecided items, the items that no rule takes, and
// the count of the vault items follow in sections of their own.
func renderNewMatches(s *Snapshot, stored, newest string, evals []ruleEval, m newMatches, vault []*Item) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Autotagging is on. The next claim moves the stored mark from %s to %s "+
		"and offers %d new items to the rules. Nothing was claimed.\n", stored, newest, m.offered)

	if len(evals) > 0 {
		b.WriteString("\n")
	}

	for i, e := range evals {
		line := renderRule(e.rule)

		switch {
		case !e.rule.Enabled:
			fmt.Fprintf(&b, "%s. It is disabled and takes nothing.\n", line)

		case e.class == ruleSkipped:
			fmt.Fprintf(&b, "%s. The web client skips this rule, so it takes nothing: %s.\n", line, e.reason)

		case e.class == ruleBlocked:
			fmt.Fprintf(&b, "%s. The server cannot evaluate this rule: %s. "+
				"It cannot tell which rule takes the %d new items that reach this rule:\n",
				line, e.reason, len(m.reaching))
			writeNewItems(&b, s, m.reaching)

			if i < len(evals)-1 {
				b.WriteString("The server does not evaluate the rules after it.\n")
			}

		default:
			fmt.Fprintf(&b, "%s. New items it takes: %d\n", line, len(m.taken[i]))
			writeNewItems(&b, s, m.taken[i])
		}

		if i == m.blocked {
			break
		}
	}

	if len(m.undecided) > 0 {
		fmt.Fprintf(&b, "\nNew items that the server cannot place: %d\n", len(m.undecided))

		for _, u := range m.undecided {
			rule := evals[u.rule].rule
			fmt.Fprintf(&b, "  - %s: undecided at rule %d %q: %s\n",
				newItemLabel(s, u.item), rule.Position, rule.Query, strings.Join(u.reasons, ". "))
		}
	}

	if m.blocked < 0 {
		fmt.Fprintf(&b, "\nNew items that no rule takes: %d\n", len(m.untaken))
		writeNewItems(&b, s, m.untaken)
	}

	fmt.Fprintf(&b, "\nNew items already in the vault, which the next claim covers and no rule takes: %d\n", len(vault))
	b.WriteString("\nThe web client or run_autotag_rules claims these items and applies the rules, " +
		"whichever runs first with autotagging on.")

	return b.String()
}

// claimOnce is the sentence that ends each result of run_autotag_rules
// after a claim. It also ends each result after a claim request whose
// outcome the tool cannot tell.
const claimOnce = "A claim happens at most once. No runner offers the claimed items to an autotag rule again, " +
	"also when a step after the claim failed."

// renderRunRefusal returns the text of a run_autotag_rules call that the
// causes in evals and m stop before the claim, or an empty string when no
// cause stops the claim. Evals holds the [ruleEval] of each live rule in
// rule order, and m holds what [firstMatch] gave the new items outside the
// vault.
//
// Each enabled rule that the web client skips or that blocks gets one
// line. A skipped rule names its skip reason, and a blocked rule names its
// block reason. Each undecided item gets one line after the rule lines.
func renderRunRefusal(evals []ruleEval, m newMatches) string {
	var causes []string

	for _, e := range evals {
		if !e.rule.Enabled {
			continue
		}

		line := renderRule(e.rule)

		switch e.class {
		case ruleSkipped:
			causes = append(causes, fmt.Sprintf("%s. The web client skips this rule: %s.", line, e.reason))
		case ruleBlocked:
			causes = append(causes, fmt.Sprintf("%s. The server cannot evaluate this rule: %s.", line, e.reason))
		case ruleEvaluated:
		}
	}

	for _, u := range m.undecided {
		rule := evals[u.rule].rule
		causes = append(causes, fmt.Sprintf("- %s (%s): the server cannot decide whether rule %d %q takes it: %s.",
			u.item.Name, u.item.InstanceID, rule.Position, rule.Query, strings.Join(u.reasons, ". ")))
	}

	if len(causes) == 0 {
		return ""
	}

	return "run_autotag_rules claimed nothing, because the server cannot apply every enabled rule " +
		"the way the web client does:\n" + strings.Join(causes, "\n") +
		"\nThe web client still claims these items when it runs with autotagging on."
}

// renderRun renders the result of a run_autotag_rules call whose claim
// and tag write succeeded. Stored is the mark before the claim, and newest
// is the mark after it. Evals holds the [ruleEval] of each live rule in
// rule order, and m holds what [firstMatch] gave the new items outside the
// vault. Vault holds the new items in the vault, which the output only
// counts. Vaulted holds the items of the rules with the vault flag after
// the moves, which [writeVaultMoves] reports.
func renderRun(
	s *Snapshot,
	stored, newest string,
	evals []ruleEval,
	m newMatches,
	vault []*Item,
	vaulted []vaultItem,
) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Claimed the new items. The stored mark moved from %s to %s.\n", stored, newest)

	if slices.ContainsFunc(evals, func(e ruleEval) bool { return e.rule.Enabled }) {
		b.WriteString("\n")
	}

	for i, e := range evals {
		if !e.rule.Enabled {
			continue
		}

		fmt.Fprintf(&b, "%s. New items it took: %d\n", renderRule(e.rule), len(m.taken[i]))
		writeNewItems(&b, s, m.taken[i])
	}

	fmt.Fprintf(&b, "\nNew items that no rule took: %d\n", len(m.untaken))
	writeNewItems(&b, s, m.untaken)

	fmt.Fprintf(&b, "\nNew items already in the vault, which no rule takes: %d\n", len(vault))
	writeVaultMoves(&b, s, vaulted)
	b.WriteString("\n" + claimOnce)

	return b.String()
}

// renderTagFailure renders the result of a run_autotag_rules call whose
// claim applied and whose tag write returned err. Stored and newest are
// the marks of the claim. The output names each item of tags with the tag
// that its rule gives, so that the user can set the tags with set_tag.
// Vaulted holds the items of the rules with the vault flag after the
// moves, which [writeVaultMoves] reports.
func renderTagFailure(s *Snapshot, stored, newest string, err error, tags []ruleTag, vaulted []vaultItem) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Claimed the new items. The stored mark moved from %s to %s.\n\n", stored, newest)
	fmt.Fprintf(&b, "The tag write failed: %v. Tag these items with set_tag:\n", err)

	for _, t := range tags {
		fmt.Fprintf(&b, "- %s (%s): tag %s\n", t.item.Name, t.item.InstanceID, t.rule.TagID)
	}

	writeVaultMoves(&b, s, vaulted)
	b.WriteString("\n" + claimOnce)

	return b.String()
}

// writeVaultMoves writes the parts of a run_autotag_rules result that
// report the items of the rules with the vault flag. Items holds those
// items after the moves, in the order of [compareIDs]. The first part
// lists the moved items. The second lists the items that stayed in place,
// each with its reason. The third lists the failed moves, each with the
// error and the location of the item. Each part opens with a blank line
// and a count, and writeVaultMoves leaves out a part without an item.
func writeVaultMoves(b *strings.Builder, s *Snapshot, items []vaultItem) {
	var moved, left, failed []string

	for _, v := range items {
		switch {
		case v.reason != "":
			left = append(left, fmt.Sprintf("  - %s (%s) at %s: %s",
				v.item.Name, v.item.InstanceID, s.LocationLabel(v.item), v.reason))

		case v.err != nil:
			failed = append(failed, fmt.Sprintf("  - %s (%s) did not move: %v. It is at %s.",
				v.item.Name, v.item.InstanceID, v.err, s.LocationLabel(v.item)))

		default:
			moved = append(moved, fmt.Sprintf("  - %s (%s)", v.item.Name, v.item.InstanceID))
		}
	}

	for _, part := range []struct {
		head  string
		lines []string
	}{
		{head: "Moved to the vault", lines: moved},
		{head: "Left in place", lines: left},
		{head: "Moves that failed", lines: failed},
	} {
		if len(part.lines) == 0 {
			continue
		}

		fmt.Fprintf(b, "\n%s: %d\n%s\n", part.head, len(part.lines), strings.Join(part.lines, "\n"))
	}
}

// renderUnsureClaim renders the result of a run_autotag_rules call whose
// claim request might have applied. Head opens the output. When tags holds
// an item, the output names each item with its rule and the tag of the
// rule. The user can then set the tags with set_tag. Vaulted holds the
// items of the rules with the vault flag, before any move. The output
// names each item of vaulted without a reason with its rule, so that the
// user can move the items with move_items. It leaves out each item with a
// reason, because the tool leaves such an item in place after an applied
// claim too.
func renderUnsureClaim(head string, tags []ruleTag, vaulted []vaultItem) string {
	var b strings.Builder

	b.WriteString(head)

	if len(tags) > 0 {
		b.WriteString("\n\nUnless the web client tagged them already, tag these items with set_tag:")

		for _, t := range tags {
			fmt.Fprintf(&b, "\n- %s (%s): tag %s (rule %d %q)",
				t.item.Name, t.item.InstanceID, t.rule.TagID, t.rule.Position, t.rule.Query)
		}
	}

	if slices.ContainsFunc(vaulted, func(v vaultItem) bool { return v.reason == "" }) {
		b.WriteString("\n\nUnless the web client moved them already, move these items to the vault with move_items:")

		for _, v := range vaulted {
			if v.reason != "" {
				continue
			}

			fmt.Fprintf(&b, "\n- %s (%s) (rule %d %q)", v.item.Name, v.item.InstanceID, v.rule.Position, v.rule.Query)
		}
	}

	b.WriteString("\n\n" + claimOnce)

	return b.String()
}

// writeNewItems writes one line for each item of items, as
// [newItemLabel] names it.
func writeNewItems(b *strings.Builder, s *Snapshot, items []*Item) {
	for _, item := range items {
		fmt.Fprintf(b, "  - %s\n", newItemLabel(s, item))
	}
}

// newItemLabel names one new item by its name, its instance id, and its
// location, such as "Fatebringer (1007), Hunter".
func newItemLabel(s *Snapshot, item *Item) string {
	return fmt.Sprintf("%s (%s), %s", item.Name, item.InstanceID, s.LocationLabel(item))
}

// renderOwnedCounts renders the result of list_autotag_rules with
// matches=owned. Counts holds what [ownedCounts] gave each live rule, in
// rule order. The output opens with a header line and gives one line for
// each rule. The line of a skipped rule names the skip reason, and the
// line of a blocked rule names the block reason. The line of an unknown
// rule says that the server counts nothing for it. Every other line gives
// the count of the rule and the undecided items with their reasons.
func renderOwnedCounts(counts []ruleCount) string {
	var b strings.Builder

	b.WriteString("Owned items that each rule matches after the enabled rules before it took theirs, " +
		"as the rule editor of the web client counts them:")

	for i := range counts {
		c := &counts[i]
		line := renderRule(c.eval.rule)

		switch {
		case c.eval.class == ruleSkipped:
			fmt.Fprintf(&b, "\n%s. The web client skips this rule and counts nothing for it: %s.", line, c.eval.reason)

		case c.unknown:
			fmt.Fprintf(&b, "\n%s. Not counted, because the server cannot tell which items the rules before it take.",
				line)

		case c.eval.class == ruleBlocked:
			fmt.Fprintf(&b, "\n%s. Cannot count: %s.", line, c.eval.reason)

		case c.undecided > 0:
			fmt.Fprintf(
				&b,
				"\n%s. Owned items it matches: %d. Owned items that the server cannot decide for it: %d (%s)",
				line,
				c.matches,
				c.undecided,
				strings.Join(c.reasons, ", "),
			)

		default:
			fmt.Fprintf(&b, "\n%s. Owned items it matches: %d", line, c.matches)
		}
	}

	return b.String()
}

// renderStats renders the summary of one period. Item names come from
// defs, so a weapon that the user dismantled keeps its name.
func renderStats(defs *Defs, period statPeriod, s statSummary) string {
	var b strings.Builder

	if period.length == 0 {
		fmt.Fprintf(&b, "Period: %s, up to %s.\n", period.label, s.to.Format(timeLayout))
	} else {
		fmt.Fprintf(&b, "Period: %s, %s to %s.\n", period.label, s.from.Format(timeLayout), s.to.Format(timeLayout))
	}

	// A read that starts after the epoch skips older chunks. For a read
	// that found no change, the line names the start of the read.
	switch {
	case !s.newest.IsZero():
		fmt.Fprintf(&b, "Newest recorded change: %s.\n", s.newest.Format(timeLayout))
	case s.readFrom.After(time.UnixMilli(0)):
		fmt.Fprintf(&b, "Newest recorded change since %s: none.\n", s.readFrom.Format(timeLayout))
	default:
		b.WriteString("Newest recorded change: none.\n")
	}

	b.WriteString("The web client records changes only while it is open in a browser. " +
		"It posts the changes of each 5-minute bucket after the bucket closes, " +
		"so the current bucket and every bucket that it has not posted yet are missing.\n")

	if len(s.flows) == 0 && len(s.weapons) == 0 {
		b.WriteString("\nThe web client recorded no currency, material, or weapon kill change in this period.")

		return b.String()
	}

	if len(s.flows) == 0 {
		b.WriteString("\nCurrencies and materials: no change.\n")
	} else {
		b.WriteString("\nCurrencies and materials, currencies first:\n")
		b.WriteString("name | kind | item hash | net | gained | spent\n")

		for _, flow := range s.flows {
			kind := changeStack
			if flow.currency {
				kind = changeCurrency
			}

			fmt.Fprintf(&b, "%s | %s | %d | %s | %d | %d\n",
				itemName(defs, flow.itemHash), kind, flow.itemHash, signed(flow.net), flow.gained, flow.spent)
		}
	}

	if len(s.weapons) == 0 {
		b.WriteString("\nWeapon kills: none.")

		return b.String()
	}

	b.WriteString("\nWeapon kills, most first:\n")
	b.WriteString("instance id | name | total | PvE | PvP\n")

	for _, weapon := range s.weapons {
		fmt.Fprintf(&b, "%s | %s | %d | %d | %d\n",
			weapon.instanceID, itemName(defs, weapon.itemHash), weapon.pve+weapon.pvp, weapon.pve, weapon.pvp)
	}

	return strings.TrimRight(b.String(), "\n")
}

// itemName returns the name of an item hash, or a placeholder that names
// the hash when the definitions lack it.
func itemName(defs *Defs, hash uint32) string {
	name := defs.Items[hash].Name
	if name == "" {
		return fmt.Sprintf("Unknown item %d", hash)
	}

	return name
}

// signed writes n with a plus sign when it is positive.
func signed(n int64) string {
	if n > 0 {
		return "+" + strconv.FormatInt(n, 10)
	}

	return strconv.FormatInt(n, 10)
}
