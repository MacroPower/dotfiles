package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/activities"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/auth"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/autotag"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/clarity"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/inventory"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/rolls"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/search"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/stats"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/vendors"
)

// timeLayout is the layout of every time that a tool prints. It ends with
// the abbreviation of the time zone.
const timeLayout = "2006-01-02 15:04 MST"

// formatTime formats t in zone with [timeLayout]. Every time that a tool
// prints goes through it. The times of one result therefore share one
// zone, whatever zone each source gave its time in.
func formatTime(t time.Time, zone *time.Location) string {
	return t.In(zone).Format(timeLayout)
}

// tagLabels maps the tag ids of the web client to its display labels.
var tagLabels = map[string]string{
	"keep":          "Keep",
	"pvp":           "PvP",
	"pve":           "PvE",
	"junk":          "Junk",
	"infuse":        "Infuse",
	"archive":       "Archive",
	dvm.TagFavorite: "Favorite",
}

func renderStatus(
	t auth.Tokens,
	signedIn bool,
	serviceURL, tokenPath string,
	now time.Time,
	zone *time.Location,
) string {
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
			"Call the login tool to sign in again.", t.BungieID, formatTime(expires, zone))

		return b.String()
	}

	b.WriteString("Signed in: yes\n")
	fmt.Fprintf(&b, "Bungie.net membership: %s\n", t.BungieID)

	if !t.Membership.IsZero() {
		fmt.Fprintf(&b, "Destiny membership: %d/%s\n", t.Membership.Type, t.Membership.ID)
	}

	fmt.Fprintf(&b, "Access token expires: %s\n", formatTime(t.AccessExpires, zone))

	if refreshes {
		fmt.Fprintf(&b, "Sign-in expires: %s", formatTime(t.RefreshExpires, zone))
	} else {
		b.WriteString("Sign-in expires with the access token, because Bungie issued no refresh token.")
	}

	return b.String()
}

// signedInSentences returns the sentences that report a completed sign-in
// of the Bungie.net membership in t. A second sentence names the end of
// the sign-in when t holds the expiry of its refresh token. Each caller
// joins the sentences with its own separator. The second sentence gives
// the time in zone.
func signedInSentences(t auth.Tokens, zone *time.Location) []string {
	sentences := []string{"Signed in as Bungie.net membership " + t.BungieID + "."}
	if !t.RefreshExpires.IsZero() {
		sentences = append(sentences, "The sign-in lasts until "+formatTime(t.RefreshExpires, zone)+".")
	}

	return sentences
}

func renderCharacters(s *inventory.Snapshot, zone *time.Location) string {
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
			c.Class, c.ID, c.Power, formatTime(c.LastPlayed, zone), equipped, held, postmaster,
		)
		fmt.Fprintf(
			&b,
			"  Slots used: %s\n",
			slotsList(s, c.ID, func(bucket inventory.Bucket) bool { return !bucket.Account }),
		)
	}

	vault := inventory.Bucket{Hash: bungie.BucketVault, Account: true}

	for _, bucket := range s.Buckets {
		if bucket.Hash == bungie.BucketVault {
			vault = bucket
		}
	}

	fmt.Fprintf(&b, "\nVault: %s slots used\n", slotsLabel(s.SlotsUsed(vault, inventory.OwnerVault), vault.Capacity))

	account := slotsList(s, inventory.OwnerAccount, func(bucket inventory.Bucket) bool {
		return bucket.Account && bucket.Hash != bungie.BucketVault
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
func slotsList(s *inventory.Snapshot, owner string, keep func(inventory.Bucket) bool) string {
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
// Sources holds the [rolls.RatingSource] of each game mode, and nil leaves the
// roll ratings out. With sources, the table gains one rating column for
// each game mode, between the flags and the perks. The cell of a weapon
// holds the text of [ratingLabel], and the cell of every other item is
// empty. One line for each game mode without ratings gives the reason,
// between the first line and the table. A result without a table has no
// such line.
func renderItems(
	s *inventory.Snapshot,
	items []*inventory.Item,
	offset, total int,
	stat string,
	perks bool,
	sources map[string]rolls.RatingSource,
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
		for _, mode := range rolls.Modes {
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
			for _, mode := range rolls.Modes {
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

// noCharacterRefusal is the text of a preview_autotag_rule call on an
// account without a character. The web client then has no current
// character, and its search matches every item.
const noCharacterRefusal = "the account has no character, so the server previews nothing. " +
	"The web client matches every item when it has no current character."

// unsupportedRefusal returns the text of a preview_autotag_rule call whose
// query holds terms that the preview does not evaluate. The text names
// terms and every filter that the preview evaluates.
func unsupportedRefusal(terms []string) string {
	return fmt.Sprintf(
		"unsupported query: the server does not evaluate %s, so it previews nothing. "+
			"The web client might still accept these terms. The server evaluates %s. "+
			"Each is: word also works as not:<word>.",
		strings.Join(terms, ", "), listWithAnd(search.SupportedKeywords()),
	)
}

// unratedRefusal returns the text of a preview_autotag_rule call whose
// query reads a game mode without ratings. Reason is the
// [rolls.RatingReason] of that mode.
func unratedRefusal(reason string) string {
	return "the query reads a game mode without ratings. " + reason
}

// listWithAnd joins words with commas and puts "and" before the last word.
func listWithAnd(words []string) string {
	switch len(words) {
	case 0:
		return ""
	case 1:
		return words[0]
	case 2:
		return words[0] + " and " + words[1]
	default:
		return strings.Join(words[:len(words)-1], ", ") + ", and " + words[len(words)-1]
	}
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
func renderPreview(
	s *inventory.Snapshot,
	page []*inventory.Item,
	offset, total, owned int,
	undecided []search.UndecidedItem,
	limit int,
) string {
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
func writeUndecided(b *strings.Builder, s *inventory.Snapshot, undecided []search.UndecidedItem, limit int) {
	b.WriteString("The server cannot decide these items:\ninstance id | name | location | reason")

	shown := undecided[:min(limit, len(undecided))]
	for _, u := range shown {
		fields := []string{u.Item.InstanceID, u.Item.Name, s.LocationLabel(u.Item), strings.Join(u.Reasons, ". ")}
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
	// unrated is the [rolls.RatingReason] of a game mode without ratings that
	// the query reads.
	unrated string
	// unsupported holds the terms of the query that the preview does not
	// evaluate, in the order that [search.CompiledQuery] gives them.
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
// [rolls.Rating] of each game mode, and the output gains the roll ratings
// block. Ratings is nil for every other item. For an item with perks,
// tested holds the [clarity.State] of the call, which [writePerks] reads.
// The output of a weapon ends with the block of [writeMeasures].
func renderItem(
	s *inventory.Snapshot,
	item *inventory.Item,
	ratings map[string]rolls.Rating,
	tested clarity.State,
) string {
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
		writePerks(&b, item.Perks, tested)
	}

	if ratings != nil {
		b.WriteString("\nRoll ratings:\n")

		for _, mode := range rolls.Modes {
			writeRating(&b, mode, ratings[mode])
		}
	}

	if len(item.Stats) > 0 {
		b.WriteString("\nStats:\n")

		for _, stat := range item.Stats {
			fmt.Fprintf(&b, "- %s: %d\n", stat.Name, stat.Value)
		}
	}

	if item.Kind == inventory.KindWeapon {
		writeMeasures(&b, item)
	}

	return strings.TrimRight(b.String(), "\n")
}

// renderDuplicates renders one page of the weapons that have more than one
// copy. Offset and total count weapons, not copies.
//
// Sources holds the [rolls.RatingSource] of each game mode. Every table of
// the page shows the rating columns of [renderWeaponGroup]. One line
// for each game mode without ratings gives the reason, between the
// first line and the first table. The lines print one time for the
// page, also when the page holds several tables. A result without a
// table has no such line.
func renderDuplicates(
	s *inventory.Snapshot,
	groups []inventory.WeaponGroup,
	offset, total int,
	sources map[string]rolls.RatingSource,
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
func renderSingleGroup(
	s *inventory.Snapshot,
	group inventory.WeaponGroup,
	sources map[string]rolls.RatingSource,
) string {
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
// Sources holds the [rolls.RatingSource] of each game mode, with an entry for
// every mode in [rolls.Modes]. The table has one rating column for each
// game mode, between the flags and the perks. Each rating cell holds
// the [ratingCell] of its copy. A version such as "(Adept)" therefore
// gets the rating of its own item hash, not the rating of the base
// weapon. One result can hold several tables, so the caller writes the
// reason of each game mode without ratings.
//
// After the rating columns, the table has one column for each kind of
// [measureKinds], with the value of [measure.text] in each cell. The
// lines of [measureNotes] follow the title and give the reason of each
// empty cell of those columns.
func renderWeaponGroup(
	s *inventory.Snapshot,
	group inventory.WeaponGroup,
	sources map[string]rolls.RatingSource,
) string {
	var b strings.Builder

	noun := "copies"
	if len(group.Copies) == 1 {
		noun = "copy"
	}

	fmt.Fprintf(&b, "%s: %d %s\n", group.Name, len(group.Copies), noun)

	measures := make([][]measure, 0, len(group.Copies))
	for _, item := range group.Copies {
		measures = append(measures, measuresOf(item))
	}

	for _, note := range measureNotes(measures) {
		b.WriteString(note + "\n")
	}

	b.WriteString("\ninstance id | name | type | power | location | tag | flags")

	for _, mode := range rolls.Modes {
		b.WriteString(" | " + mode + " rating")
	}

	for _, kind := range measureKinds {
		b.WriteString(" | " + kind.column)
	}

	b.WriteString(" | perks\n")

	for i, item := range group.Copies {
		fields := []string{
			item.InstanceID,
			item.Name,
			typeLabel(item),
			powerLabel(item),
			s.LocationLabel(item),
			item.Tag,
			strings.Join(flagsOf(item), ", "),
		}

		for _, mode := range rolls.Modes {
			fields = append(fields, ratingCell(sources[mode], item))
		}

		for _, m := range measures[i] {
			fields = append(fields, m.text())
		}

		perks := make([]string, 0, len(item.Perks))
		for _, perk := range item.Perks {
			perks = append(perks, inventory.PerkLabel(perk))
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
func renderLoadouts(s *inventory.Snapshot, characters []inventory.Character, names bungie.LoadoutTableState) string {
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
func loadoutHeading(l inventory.Loadout, names bungie.LoadoutTableState) string {
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
func renderLoadoutEquip(s *inventory.Snapshot, l inventory.Loadout, result inventory.LoadoutEquip, err error) string {
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
		fmt.Fprintf(&b, "- %s (%s) did not reach %s. %s",
			stopped.Item.Name, stopped.Item.InstanceID, target, locationSentence(s, stopped.Item))

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
func renderLoadoutSave(
	s *inventory.Snapshot,
	characterID string,
	number int,
	nameHash uint32,
	names bungie.LoadoutTableState,
) string {
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
func renderLoadoutClear(s *inventory.Snapshot, l inventory.Loadout) string {
	var (
		held    []*inventory.Item
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
func itemNames(items []*inventory.Item) string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, fmt.Sprintf("%s (%s)", item.Name, item.InstanceID))
	}

	return strings.Join(names, ", ")
}

// renderNoJunk reports that no junk sits in bucket on character c and
// that none waits.
func renderNoJunk(
	s *inventory.Snapshot,
	c inventory.Character,
	bucket inventory.Bucket,
	plan inventory.JunkPlan,
) string {
	lines := []string{fmt.Sprintf("No junk is left for %s on %s.", bucket.Name, s.OwnerLabel(c.ID))}
	lines = appendSkippedJunk(lines, plan.Skipped)
	lines = append(lines, "If you moved other items out of the slot for this run, move them back with move_items.")

	return strings.Join(lines, "\n")
}

// renderStagedJunk lists the junk that bucket on character c holds after
// a stage_junk call. Plan is the plan after the moves, and failures holds
// one line for each move that did not succeed. The output names the
// unmovable items of the plan as items that the user keeps, and the count
// that guards the dismantle includes them.
func renderStagedJunk(
	s *inventory.Snapshot,
	c inventory.Character,
	bucket inventory.Bucket,
	plan inventory.JunkPlan,
	failures []string,
) string {
	staged := len(plan.Staged)
	lines := []string{fmt.Sprintf(
		"Unequipped junk in %s on %s after this call: %d", bucket.Name, s.OwnerLabel(c.ID), staged,
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

	switch {
	case len(plan.Unmovable) == 1:
		lines = append(lines, fmt.Sprintf(
			"The slot also holds an unequipped item that is not tagged junk and that move_items cannot move, "+
				"so stage_junk leaves it in place: %s. The user keeps it.",
			unmovableLabels(plan.Unmovable),
		))

	case len(plan.Unmovable) > 1:
		lines = append(lines, fmt.Sprintf(
			"The slot also holds unequipped items that are not tagged junk and that move_items cannot move, "+
				"so stage_junk leaves them in place: %s. The user keeps them.",
			unmovableLabels(plan.Unmovable),
		))
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
		"The items in the list under the first line are tagged junk, and the user dismantles them in the game. "+
			"The user keeps every other unequipped item of the slot. "+
			"Do not dismantle when the number of unequipped items that the game shows in the slot is higher than %d, "+
			"because stage_junk did not check an item that reached the slot after the last inventory fetch. "+
			"Call stage_junk again after the user dismantled them. "+
			"A call in the next %d seconds might reuse this inventory copy and list the same items.",
		staged+len(plan.Unmovable), int(snapshotTTL/time.Second),
	))

	return strings.Join(lines, "\n")
}

// unmovableLabels names the items that stage_junk leaves in a slot, such
// as "Fatebringer (1007), Unknown item 373750294 (stack of 1, item hash
// 373750294)". Each name adds the instance id, or the size and the item
// hash of a stack without an instance id.
func unmovableLabels(items []*inventory.Item) string {
	labels := make([]string, 0, len(items))

	for _, item := range items {
		if item.InstanceID == "" {
			labels = append(labels, fmt.Sprintf("%s (stack of %d, item hash %d)", item.Name, item.Quantity, item.Hash))

			continue
		}

		labels = append(labels, fmt.Sprintf("%s (%s)", item.Name, item.InstanceID))
	}

	return strings.Join(labels, ", ")
}

// fullSlotRefusal returns the text of a stage_junk call that finds no
// junk and no free place in bucket on character c. The text opens with
// the text of [inventory.ErrMove] and names the unmovable items, which
// the user keeps.
func fullSlotRefusal(
	s *inventory.Snapshot,
	c inventory.Character,
	bucket inventory.Bucket,
	unmovable []*inventory.Item,
) string {
	format := "%s: %s on %s has no free place for junk. " +
		"Unequipped items that are not tagged junk and that move_items cannot move fill the slot: %s. " +
		"The user keeps them, so stage_junk cannot stage the junk that waits. stage_junk moved nothing."
	if len(unmovable) == 1 {
		format = "%s: %s on %s has no free place for junk. " +
			"An unequipped item that is not tagged junk and that move_items cannot move fills the slot: %s. " +
			"The user keeps it, so stage_junk cannot stage the junk that waits. stage_junk moved nothing."
	}

	return fmt.Sprintf(format, inventory.ErrMove, bucket.Name, s.OwnerLabel(c.ID), unmovableLabels(unmovable))
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

// keeperRefusal returns the text of a stage_junk call that finds keepers
// in bucket on character c. The text opens with the text of
// [inventory.ErrMove], names each keeper, and tells the model how to move
// the keepers out and back.
func keeperRefusal(
	s *inventory.Snapshot,
	c inventory.Character,
	bucket inventory.Bucket,
	keepers []*inventory.Item,
) string {
	names := make([]string, 0, len(keepers))
	ids := make([]string, 0, len(keepers))

	for _, item := range keepers {
		names = append(names, fmt.Sprintf("%s (%s)", item.Name, item.InstanceID))
		ids = append(ids, item.InstanceID)
	}

	return fmt.Sprintf(
		"%s: %s on %s holds unequipped gear that is not tagged junk: %s. stage_junk moved nothing. "+
			"Move that gear to the vault with move_items, with target vault and instance_ids %s. "+
			"Then call stage_junk again. After stage_junk reports that no junk is left, "+
			"move that gear back with move_items and target %s.",
		inventory.ErrMove, bucket.Name, s.OwnerLabel(c.ID), strings.Join(names, ", "), strings.Join(ids, ", "), c.ID,
	)
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
func writeRating(b *strings.Builder, mode string, rating rolls.Rating) {
	if rating.Err != nil {
		fmt.Fprintf(b, "- %s\n", rolls.RatingReason(mode, rating.Err))

		return
	}

	fmt.Fprintf(b, "- %s: ", rolls.ModeLabels[mode])

	label := rolls.SheetLabels[mode]

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
func sheetPlace(row rolls.SheetRow) string {
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

// ratingReasons returns the [rolls.RatingReason] of each game mode whose
// source holds an Err, in the order of [rolls.Modes]. It returns no line
// when every mode has ratings.
func ratingReasons(sources map[string]rolls.RatingSource) []string {
	var reasons []string

	for _, mode := range rolls.Modes {
		err := sources[mode].Err
		if err != nil {
			reasons = append(reasons, rolls.RatingReason(mode, err))
		}
	}

	return reasons
}

// ratingCell returns the rating cell that the source of one game mode
// gives item in a table. The cell of a weapon holds the [ratingLabel] of
// its [rolls.Rating]. Only weapons have roll ratings, so the cell of every
// other item is empty, also when the mode has no ratings.
func ratingCell(source rolls.RatingSource, item *inventory.Item) string {
	if item.Kind != inventory.KindWeapon {
		return ""
	}

	return ratingLabel(rolls.Rate(source, item))
}

// ratingLabel returns the text of a rating cell for one [rolls.Rating]. The
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
func ratingLabel(rating rolls.Rating) string {
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

func typeLabel(item *inventory.Item) string {
	if item.Damage == "" {
		return item.TypeName
	}

	return item.Damage + " " + item.TypeName
}

func powerLabel(item *inventory.Item) string {
	if item.Power == 0 {
		return ""
	}

	return strconv.Itoa(item.Power)
}

func totalLabel(item *inventory.Item) string {
	total, ok := item.StatTotal()
	if !ok {
		return ""
	}

	return strconv.Itoa(total)
}

func statLabel(item *inventory.Item, name string) string {
	value, ok := item.StatValue(name)
	if !ok {
		return ""
	}

	return strconv.Itoa(value)
}

func flagsOf(item *inventory.Item) []string {
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

	bungie.HistoryPeriod
}

// renderActivityHistory renders runs in their order. Scope names the owner
// of the runs, such as "Hunter" or "the account". Limit is the number of
// runs that the caller asked for.
func renderActivityHistory(
	defs *definitions.Defs,
	runs []historyRun,
	scope string,
	limit int,
	zone *time.Location,
) string {
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
			formatTime(run.Period, zone),
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
func activityName(defs *definitions.Defs, activity bungie.HistoryActivity) string {
	for _, hash := range []uint32{activity.ReferenceID, activity.DirectorActivityHash} {
		def := defs.Activities[hash]
		if def.Name != "" {
			return def.Name
		}
	}

	return fmt.Sprintf("Activity %d", activity.ReferenceID)
}

// renderVendors renders every listed vendor as a table with one row per
// vendor. Owner names the character that sees the vendors, such as "Hunter".
func renderVendors(listed []vendors.Vendor, owner string, zone *time.Location) string {
	if len(listed) == 0 {
		return "Bungie lists no vendors for " + owner + "."
	}

	var b strings.Builder

	if len(listed) == 1 {
		fmt.Fprintf(&b, "The vendor of %s.", owner)
	} else {
		fmt.Fprintf(&b, "The %d vendors of %s.", len(listed), owner)
	}

	b.WriteString(vendorHint)
	writeVendorTable(&b, listed, zone)

	return strings.TrimRight(b.String(), "\n")
}

// renderGroupedVendors renders the vendors that [vendors.Grouped] keeps as
// a table with one row per vendor. Total counts every vendor that Bungie
// returns, and the intro points at the all input when the table leaves out
// some of them. Owner names the character that sees the vendors.
func renderGroupedVendors(grouped []vendors.Vendor, total int, owner string, zone *time.Location) string {
	if total == 0 {
		return "Bungie lists no vendors for " + owner + "."
	}

	var b strings.Builder

	switch len(grouped) {
	case 0:
		fmt.Fprintf(&b, "No vendor of %s that Bungie groups for its Companion app has a sale.", owner)
	case 1:
		fmt.Fprintf(&b, "The vendor of %s that Bungie groups for its Companion app and that has a sale.", owner)
	default:
		fmt.Fprintf(&b, "The %d vendors of %s that Bungie groups for its Companion app and that have a sale.",
			len(grouped), owner)
	}

	switch {
	case total == len(grouped):
	case total == 1:
		b.WriteString(" Pass all=true to list the one vendor that Bungie returns.")
	default:
		fmt.Fprintf(&b, " Pass all=true to list all %d vendors that Bungie returns.", total)
	}

	b.WriteString(vendorHint)

	if len(grouped) > 0 {
		writeVendorTable(&b, grouped, zone)
	}

	return strings.TrimRight(b.String(), "\n")
}

// vendorHint follows the intro of a vendor list.
const vendorHint = " Pass vendor with a hash or a name to list the sales of one vendor."

// writeVendorTable writes a blank line and a table with one row per vendor.
// The refresh times are in zone.
func writeVendorTable(b *strings.Builder, listed []vendors.Vendor, zone *time.Location) {
	b.WriteString("\n\nname | subtitle | hash | sales | next refresh | flags\n")

	for _, v := range listed {
		b.WriteString(strings.Join([]string{
			v.Name,
			v.Subtitle,
			strconv.FormatUint(uint64(v.Hash), 10),
			strconv.Itoa(v.SaleCount()),
			refreshLabel(v.NextRefresh, zone),
			strings.Join(vendorFlags(v), ", "),
		}, " | "))
		b.WriteString("\n")
	}
}

// renderVendor renders the sales of one vendor under the names of their
// categories. Owner names the character that sees the stock.
func renderVendor(v vendors.Vendor, owner string, zone *time.Location) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s\n", v.Name)
	writeField(&b, "Subtitle", v.Subtitle)
	fmt.Fprintf(&b, "Vendor hash: %d\n", v.Hash)
	fmt.Fprintf(&b, "Character: %s\n", owner)
	fmt.Fprintf(&b, "Next refresh: %s\n", refreshLabel(v.NextRefresh, zone))
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
// that time is "never". The label of the zero time is "unknown". Every
// other label gives the time in zone.
func refreshLabel(t time.Time, zone *time.Location) string {
	switch {
	case t.IsZero():
		return "unknown"
	case t.Year() >= 9999:
		return "never"
	default:
		return formatTime(t, zone)
	}
}

func vendorFlags(v vendors.Vendor) []string {
	if v.Disabled {
		return []string{"disabled"}
	}

	return nil
}

// saleLabel names the item of a sale, with the quantity when the sale
// gives more than one.
func saleLabel(sale vendors.Sale) string {
	if sale.Quantity > 1 {
		return fmt.Sprintf("%s x%d", sale.Name, sale.Quantity)
	}

	return sale.Name
}

// costLabel lists the costs of a sale, such as "50000 Glimmer, 10
// Legendary Shards". The label of a sale without a cost is "no cost".
func costLabel(costs []vendors.Cost) string {
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
func statusLabel(sale vendors.Sale) string {
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
// and kept counts the ones that [activities.Read] keeps from them. Offset
// and total count the kept activities that match the query and the
// rewards flag. Owner names the character, such as "Hunter". The power and
// bonus drops columns are empty for an activity without them.
func renderActivities(rows []activities.Row, offset, total, kept, listed int, owner string) string {
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
func lootLabel(loot activities.Loot) string {
	if loot.Quantity > 1 {
		return fmt.Sprintf("%s x%d", loot.Name, loot.Quantity)
	}

	return loot.Name
}

func renderTags(s *inventory.Snapshot) string {
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

	for _, id := range dvm.TagOrder {
		fmt.Fprintf(&b, "%s | %s | %d\n", id, tagLabels[id], counts[id])
	}

	fmt.Fprintf(&b, "\nFavorites: %s", itemCount(favorites))

	return b.String()
}

func renderNotes(s *inventory.Snapshot, notes []dvm.Note) string {
	var b strings.Builder

	for _, note := range notes {
		if note.Removed {
			continue
		}

		target := note.TargetID

		item, ok := s.Find(note.TargetID)
		if note.Kind == dvm.NoteInstance && ok {
			target = fmt.Sprintf("%s (%s)", note.TargetID, item.Name)
		}

		fmt.Fprintf(&b, "- %s %s: %s\n", note.Kind, target, note.Body)
	}

	if b.Len() == 0 {
		return "No notes."
	}

	return strings.TrimRight(b.String(), "\n")
}

func renderPerkPreferences(prefs []dvm.PerkPreference) string {
	var b strings.Builder

	for _, mode := range rolls.Modes {
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

func renderAutotag(snapshot dvm.AutotagSnapshot) string {
	var b strings.Builder

	switch {
	case snapshot.State == nil:
		b.WriteString("Autotagging has never been turned on.\n")
	case snapshot.State.Enabled:
		b.WriteString("Autotagging is on.\n")
	default:
		b.WriteString("Autotagging is off.\n")
	}

	rules := autotag.LiveRules(snapshot.Rules)
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

func renderRule(rule dvm.AutotagRule) string {
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
// the mark that the next claim stores. Evals holds the [autotag.RuleEval] of
// each live rule in rule order, and m holds what [autotag.FirstMatch] gave the
// new items outside the vault. Vault holds the new items in the vault, which
// the output only counts.
//
// The output opens with the marks and says that the call claimed nothing. A
// line for each rule follows, with the items that the rule takes. The
// lines stop at the first enabled rule that blocks, which lists the items
// that reach it. The undecided items, the items that no rule takes, and
// the count of the vault items follow in sections of their own.
func renderNewMatches(
	s *inventory.Snapshot,
	stored, newest string,
	evals []autotag.RuleEval,
	m autotag.NewMatches,
	vault []*inventory.Item,
) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Autotagging is on. The next claim moves the stored mark from %s to %s "+
		"and offers %d new items to the rules. Nothing was claimed.\n", stored, newest, m.Offered)

	if len(evals) > 0 {
		b.WriteString("\n")
	}

	for i, e := range evals {
		line := renderRule(e.Rule)

		switch {
		case !e.Rule.Enabled:
			fmt.Fprintf(&b, "%s. It is disabled and takes nothing.\n", line)

		case e.Class == autotag.RuleSkipped:
			fmt.Fprintf(&b, "%s. The web client skips this rule, so it takes nothing: %s.\n", line, e.Reason)

		case e.Class == autotag.RuleBlocked:
			fmt.Fprintf(&b, "%s. The server cannot evaluate this rule: %s. "+
				"It cannot tell which rule takes the %d new items that reach this rule:\n",
				line, e.Reason, len(m.Reaching))
			writeNewItems(&b, s, m.Reaching)

			if i < len(evals)-1 {
				b.WriteString("The server does not evaluate the rules after it.\n")
			}

		default:
			fmt.Fprintf(&b, "%s. New items it takes: %d\n", line, len(m.Taken[i]))
			writeNewItems(&b, s, m.Taken[i])
		}

		if i == m.Blocked {
			break
		}
	}

	if len(m.Undecided) > 0 {
		fmt.Fprintf(&b, "\nNew items that the server cannot place: %d\n", len(m.Undecided))

		for _, u := range m.Undecided {
			rule := evals[u.Rule].Rule
			fmt.Fprintf(&b, "  - %s: undecided at rule %d %q: %s\n",
				newItemLabel(s, u.Item), rule.Position, rule.Query, strings.Join(u.Reasons, ". "))
		}
	}

	if m.Blocked < 0 {
		fmt.Fprintf(&b, "\nNew items that no rule takes: %d\n", len(m.Untaken))
		writeNewItems(&b, s, m.Untaken)
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
// cause stops the claim. Evals holds the [autotag.RuleEval] of each live rule
// in rule order, and m holds what [autotag.FirstMatch] gave the new items
// outside the vault.
//
// Each enabled rule that the web client skips or that blocks gets one
// line. A skipped rule names its skip reason, and a blocked rule names its
// block reason. Each undecided item gets one line after the rule lines.
func renderRunRefusal(evals []autotag.RuleEval, m autotag.NewMatches) string {
	var causes []string

	for _, e := range evals {
		if !e.Rule.Enabled {
			continue
		}

		line := renderRule(e.Rule)

		switch e.Class {
		case autotag.RuleSkipped:
			causes = append(causes, fmt.Sprintf("%s. The web client skips this rule: %s.", line, e.Reason))
		case autotag.RuleBlocked:
			causes = append(causes, fmt.Sprintf("%s. The server cannot evaluate this rule: %s.", line, e.Reason))
		case autotag.RuleEvaluated:
		}
	}

	for _, u := range m.Undecided {
		rule := evals[u.Rule].Rule
		causes = append(causes, fmt.Sprintf("- %s (%s): the server cannot decide whether rule %d %q takes it: %s.",
			u.Item.Name, u.Item.InstanceID, rule.Position, rule.Query, strings.Join(u.Reasons, ". ")))
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
// is the mark after it. Evals holds the [autotag.RuleEval] of each live rule in
// rule order, and m holds what [autotag.FirstMatch] gave the new items outside
// the vault. Vault holds the new items in the vault, which the output only
// counts. Vaulted holds the items of the rules with the vault flag after
// the moves, which [writeVaultMoves] reports.
func renderRun(
	s *inventory.Snapshot,
	stored, newest string,
	evals []autotag.RuleEval,
	m autotag.NewMatches,
	vault []*inventory.Item,
	vaulted []autotag.VaultItem,
) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Claimed the new items. The stored mark moved from %s to %s.\n", stored, newest)

	if slices.ContainsFunc(evals, func(e autotag.RuleEval) bool { return e.Rule.Enabled }) {
		b.WriteString("\n")
	}

	for i, e := range evals {
		if !e.Rule.Enabled {
			continue
		}

		fmt.Fprintf(&b, "%s. New items it took: %d\n", renderRule(e.Rule), len(m.Taken[i]))
		writeNewItems(&b, s, m.Taken[i])
	}

	fmt.Fprintf(&b, "\nNew items that no rule took: %d\n", len(m.Untaken))
	writeNewItems(&b, s, m.Untaken)

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
func renderTagFailure(
	s *inventory.Snapshot,
	stored, newest string,
	err error,
	tags []autotag.RuleTag,
	vaulted []autotag.VaultItem,
) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Claimed the new items. The stored mark moved from %s to %s.\n\n", stored, newest)
	fmt.Fprintf(&b, "The tag write failed: %v. Tag these items with set_tag:\n", err)

	for _, t := range tags {
		fmt.Fprintf(&b, "- %s (%s): tag %s\n", t.Item.Name, t.Item.InstanceID, t.Rule.TagID)
	}

	writeVaultMoves(&b, s, vaulted)
	b.WriteString("\n" + claimOnce)

	return b.String()
}

// writeVaultMoves writes the parts of a run_autotag_rules result that
// report the items of the rules with the vault flag. Items holds those
// items after the moves, in the order of [inventory.CompareIDs]. The first part
// lists the moved items. The second lists the items that stayed in place,
// each with its reason. The third lists the failed moves, each with the
// error and the location of the item. Each part opens with a blank line
// and a count, and writeVaultMoves leaves out a part without an item.
func writeVaultMoves(b *strings.Builder, s *inventory.Snapshot, items []autotag.VaultItem) {
	var moved, left, failed []string

	for _, v := range items {
		switch {
		case v.Reason != "":
			left = append(left, fmt.Sprintf("  - %s (%s) at %s: %s",
				v.Item.Name, v.Item.InstanceID, s.LocationLabel(v.Item), v.Reason))

		case v.Err != nil:
			failed = append(failed, "  - "+moveFailure(s, v.Item, v.Err))

		default:
			moved = append(moved, fmt.Sprintf("  - %s (%s)", v.Item.Name, v.Item.InstanceID))
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

// moveFailure returns the sentence that reports a move of item that
// returned err. The sentence names the item, the error, and the location
// of the item in s after the move. It has no list prefix, so each caller
// adds the prefix of its own list.
func moveFailure(s *inventory.Snapshot, item *inventory.Item, err error) string {
	// A Bungie message ends with its own period.
	reason := strings.TrimRight(err.Error(), ".")

	return fmt.Sprintf("%s (%s) did not move: %s. %s",
		item.Name, item.InstanceID, reason, locationSentence(s, item))
}

// locationSentence returns the sentence that says where item is in s. It
// reads "It is in the vault." for an item in the vault and "It is on
// Hunter." for an item in the inventory of the Hunter.
func locationSentence(s *inventory.Snapshot, item *inventory.Item) string {
	switch item.Owner {
	case inventory.OwnerVault:
		return "It is in the vault."
	case inventory.OwnerAccount:
		return "It is in an account-wide bucket."
	}

	character := s.OwnerLabel(item.Owner)

	switch {
	case item.Postmaster:
		return "It is at the Postmaster for " + character + "."
	case item.Equipped:
		return "It is equipped on " + character + "."
	default:
		return "It is on " + character + "."
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
func renderUnsureClaim(head string, tags []autotag.RuleTag, vaulted []autotag.VaultItem) string {
	var b strings.Builder

	b.WriteString(head)

	if len(tags) > 0 {
		b.WriteString("\n\nUnless the web client tagged them already, tag these items with set_tag:")

		for _, t := range tags {
			fmt.Fprintf(&b, "\n- %s (%s): tag %s (rule %d %q)",
				t.Item.Name, t.Item.InstanceID, t.Rule.TagID, t.Rule.Position, t.Rule.Query)
		}
	}

	if slices.ContainsFunc(vaulted, func(v autotag.VaultItem) bool { return v.Reason == "" }) {
		b.WriteString("\n\nUnless the web client moved them already, move these items to the vault with move_items:")

		for _, v := range vaulted {
			if v.Reason != "" {
				continue
			}

			fmt.Fprintf(&b, "\n- %s (%s) (rule %d %q)", v.Item.Name, v.Item.InstanceID, v.Rule.Position, v.Rule.Query)
		}
	}

	b.WriteString("\n\n" + claimOnce)

	return b.String()
}

// writeNewItems writes one line for each item of items, as
// [newItemLabel] names it.
func writeNewItems(b *strings.Builder, s *inventory.Snapshot, items []*inventory.Item) {
	for _, item := range items {
		fmt.Fprintf(b, "  - %s\n", newItemLabel(s, item))
	}
}

// newItemLabel names one new item by its name, its instance id, and its
// location, such as "Fatebringer (1007), Hunter".
func newItemLabel(s *inventory.Snapshot, item *inventory.Item) string {
	return fmt.Sprintf("%s (%s), %s", item.Name, item.InstanceID, s.LocationLabel(item))
}

// renderOwnedCounts renders the result of list_autotag_rules with
// matches=owned. Counts holds what [autotag.OwnedCounts] gave each live rule,
// in rule order. The output opens with a header line and gives one line for
// each rule. The line of a skipped rule names the skip reason, and the
// line of a blocked rule names the block reason. The line of an unknown
// rule says that the server counts nothing for it. Every other line gives
// the count of the rule and the undecided items with their reasons.
func renderOwnedCounts(counts []autotag.RuleCount) string {
	var b strings.Builder

	b.WriteString("Owned items that each rule matches after the enabled rules before it took theirs, " +
		"as the rule editor of the web client counts them:")

	for i := range counts {
		c := &counts[i]
		line := renderRule(c.Eval.Rule)

		switch {
		case c.Eval.Class == autotag.RuleSkipped:
			fmt.Fprintf(&b, "\n%s. The web client skips this rule and counts nothing for it: %s.", line, c.Eval.Reason)

		case c.Unknown:
			fmt.Fprintf(&b, "\n%s. Not counted, because the server cannot tell which items the rules before it take.",
				line)

		case c.Eval.Class == autotag.RuleBlocked:
			fmt.Fprintf(&b, "\n%s. Cannot count: %s.", line, c.Eval.Reason)

		case c.Undecided > 0:
			fmt.Fprintf(
				&b,
				"\n%s. Owned items it matches: %d. Owned items that the server cannot decide for it: %d (%s)",
				line,
				c.Matches,
				c.Undecided,
				strings.Join(c.Reasons, ", "),
			)

		default:
			fmt.Fprintf(&b, "\n%s. Owned items it matches: %d", line, c.Matches)
		}
	}

	return b.String()
}

// renderStats renders the summary of one period. Item names come from
// defs, so a weapon that the user dismantled keeps its name. Defs also
// carry the names of the lookups at Bungie. A row whose item hash defs do
// not name shows a placeholder with the hash, and the sentences of
// [unnamedNote] follow the tables.
func renderStats(defs *definitions.Defs, period stats.Period, s stats.Summary, zone *time.Location) string {
	var b strings.Builder

	if period.Length == 0 {
		fmt.Fprintf(&b, "Period: %s, up to %s.\n", period.Label, formatTime(s.To, zone))
	} else {
		fmt.Fprintf(&b, "Period: %s, %s to %s.\n", period.Label, formatTime(s.From, zone), formatTime(s.To, zone))
	}

	// A read that starts after the epoch skips older chunks. For a read
	// that found no change, the line names the start of the read.
	switch {
	case !s.Newest.IsZero():
		fmt.Fprintf(&b, "Newest recorded change: %s.\n", formatTime(s.Newest, zone))
	case s.ReadFrom.After(time.UnixMilli(0)):
		fmt.Fprintf(&b, "Newest recorded change since %s: none.\n", formatTime(s.ReadFrom, zone))
	default:
		b.WriteString("Newest recorded change: none.\n")
	}

	b.WriteString("The web client records changes only while it is open in a browser. " +
		"It posts the changes of each 5-minute bucket after the bucket closes, " +
		"so the current bucket and every bucket that it has not posted yet are missing.\n")

	if len(s.Flows) == 0 && len(s.Weapons) == 0 {
		b.WriteString("\nThe web client recorded no currency, material, or weapon kill change in this period.")

		return b.String()
	}

	// Unnamed holds each hash that the definitions do not name one time, in
	// the order of its first row.
	var unnamed []uint32

	name := func(hash uint32) string {
		if defs.ItemName(hash) == "" && !slices.Contains(unnamed, hash) {
			unnamed = append(unnamed, hash)
		}

		return inventory.ItemName(defs, hash)
	}

	if len(s.Flows) == 0 {
		b.WriteString("\nCurrencies and materials: no change.\n")
	} else {
		b.WriteString("\nCurrencies and materials, currencies first:\n")
		b.WriteString("name | kind | item hash | net | gained | spent\n")

		for _, flow := range s.Flows {
			kind := stats.ChangeStack
			if flow.Currency {
				kind = stats.ChangeCurrency
			}

			fmt.Fprintf(&b, "%s | %s | %d | %s | %d | %d\n",
				name(flow.ItemHash), kind, flow.ItemHash, signed(flow.Net), flow.Gained, flow.Spent)
		}
	}

	if len(s.Weapons) == 0 {
		b.WriteString("\nWeapon kills: none.\n")
	} else {
		b.WriteString("\nWeapon kills, most first:\n")
		b.WriteString("instance id | name | total | PvE | PvP\n")

		for _, weapon := range s.Weapons {
			fmt.Fprintf(&b, "%s | %s | %d | %d | %d\n",
				weapon.InstanceID, name(weapon.ItemHash), weapon.PvE+weapon.PvP, weapon.PvE, weapon.PvP)
		}
	}

	if len(unnamed) > 0 {
		b.WriteString("\n" + unnamedNote(defs, unnamed))
	}

	return strings.TrimRight(b.String(), "\n")
}

// unnamedNote returns the sentences that say why neither the definitions
// nor Bungie name the item hashes in unnamed. An answer of Bungie gives the
// reason of its hash. For a hash without an answer, the note gives the
// reason of the definitions of the service and then the reason that the
// lookup at Bungie did not answer.
func unnamedNote(defs *definitions.Defs, unnamed []uint32) string {
	var unanswered, undefined, classified, nameless []uint32

	for _, hash := range unnamed {
		lookup, ok := defs.Lookups[hash]

		switch {
		case !ok || !lookup.Answered():
			unanswered = append(unanswered, hash)
		case !lookup.Defined:
			undefined = append(undefined, hash)
		case lookup.Redacted:
			classified = append(classified, hash)
		default:
			nameless = append(nameless, hash)
		}
	}

	sentences := serviceSentences(defs, unanswered)
	sentences = append(sentences, lookupSentences(defs, unanswered)...)

	if len(undefined) > 0 {
		sentences = append(sentences, fmt.Sprintf(
			"The definitions of manifest version %s and the live manifest of Bungie both lack %s, "+
				"so the server cannot name %s.",
			defs.Version, hashPhrase(undefined), pronounOf(undefined)))
	}

	if len(classified) > 0 {
		sentences = append(sentences, fmt.Sprintf(
			"Bungie classifies %s and does not show a name yet, so the server cannot name %s.",
			hashPhrase(classified), pronounOf(classified)))
	}

	if len(nameless) > 0 {
		sentences = append(sentences, fmt.Sprintf("Bungie defines %s without a name.", hashPhrase(nameless)))
	}

	return strings.Join(sentences, " ")
}

// serviceSentences returns the sentences that say why the definitions of
// the service name none of the item hashes in hashes. The service index
// lists the item hashes whose definitions the service withholds. A hash
// that neither the item definitions nor that list holds is missing from
// the manifest version of the definitions. Without that list, the
// sentences give both causes.
func serviceSentences(defs *definitions.Defs, hashes []uint32) []string {
	if len(hashes) == 0 {
		return nil
	}

	if !defs.WithheldListed {
		return []string{fmt.Sprintf("The definitions of manifest version %s lack %s, so the server cannot name %s. "+
			"The service withholds the definitions of dummy items, classified items, and items without a name, "+
			"and the game can hold an item that Bungie added in a later manifest version. "+
			"The service publishes no list of the hashes that it withholds, "+
			"so the server cannot tell which cause applies.",
			defs.Version, hashPhrase(hashes), pronounOf(hashes))}
	}

	var withheld, missing []uint32

	for _, hash := range hashes {
		if defs.Withholds(hash) {
			withheld = append(withheld, hash)
		} else {
			missing = append(missing, hash)
		}
	}

	var sentences []string

	if len(withheld) > 0 {
		sentences = append(sentences, fmt.Sprintf(
			"The service withholds the definitions of dummy items, classified items, and items without a name, "+
				"so the server cannot name %s.",
			hashPhrase(withheld)))
	}

	if len(missing) > 0 {
		sentences = append(sentences, fmt.Sprintf(
			"The definitions of manifest version %s lack %s, so the server cannot name %s. "+
				"The game can hold an item that Bungie added in a later manifest version.",
			defs.Version, hashPhrase(missing), pronounOf(missing)))
	}

	return sentences
}

// lookupSentences returns the sentences that say why the lookups of the
// item hashes in hashes at Bungie did not answer. The lookups that failed
// for one reason of [lookupReason] share a sentence. A hash without a
// lookup gets no sentence.
func lookupSentences(defs *definitions.Defs, hashes []uint32) []string {
	var (
		reasons []string
		failed  = map[string][]uint32{}
		skipped []uint32
	)

	for _, hash := range hashes {
		lookup, ok := defs.Lookups[hash]

		switch {
		case !ok:
		case lookup.Skipped:
			skipped = append(skipped, hash)
		case lookup.Err != nil:
			reason := lookupReason(hash, lookup.Err)
			if failed[reason] == nil {
				reasons = append(reasons, reason)
			}

			failed[reason] = append(failed[reason], hash)
		}
	}

	var sentences []string

	for _, reason := range reasons {
		sentences = append(
			sentences,
			fmt.Sprintf("The lookup of %s at Bungie failed: %s. A later call looks %s up again.",
				hashPhrase(failed[reason]), reason, pronounOf(failed[reason])),
		)
	}

	if len(skipped) > 0 {
		sentences = append(sentences, fmt.Sprintf(
			"This call reached the limit of its lookups at Bungie before %s, so it did not look %s up. "+
				"A later call looks %s up.",
			hashPhrase(skipped), pronounOf(skipped), pronounOf(skipped)))
	}

	return sentences
}

// lookupReason returns the reason of the lookup of the item hash at Bungie
// that failed with err. The error of a request names its URL or its path,
// and each item hash has its own path. The reason leaves both out, so the
// lookups that failed for one cause get one reason.
func lookupReason(hash uint32, err error) string {
	var (
		status *bungie.StatusError
		uerr   *url.Error
	)

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "the lookups ran out of time"
	case errors.As(err, &status):
		return fmt.Sprintf("Bungie answered with the HTTP status %d and no platform response", status.Status)
	case errors.As(err, &uerr):
		return uerr.Err.Error()
	default:
		// A body that the client cannot read or decode gives an error
		// that quotes the path of the request. A Bungie message ends with
		// its own period.
		reason := strings.ReplaceAll(err.Error(), bungie.ItemDefinitionPath(hash), "the item definition")

		return strings.TrimRight(reason, ".")
	}
}

// hashPhrase names the items with the hashes in hashes, such as "the
// items with the hashes 1, 2, and 3".
func hashPhrase(hashes []uint32) string {
	if len(hashes) == 1 {
		return fmt.Sprintf("the item with the hash %d", hashes[0])
	}

	words := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		words = append(words, strconv.FormatUint(uint64(hash), 10))
	}

	return "the items with the hashes " + listWithAnd(words)
}

// pronounOf returns the pronoun that stands for the items of
// [hashPhrase].
func pronounOf(hashes []uint32) string {
	if len(hashes) == 1 {
		return "it"
	}

	return "them"
}

// signed writes n with a plus sign when it is positive.
func signed(n int64) string {
	if n > 0 {
		return "+" + strconv.FormatInt(n, 10)
	}

	return strconv.FormatInt(n, 10)
}
