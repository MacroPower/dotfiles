package main

import (
	"cmp"
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
func renderItems(s *Snapshot, items []*Item, offset, total int, stat string, perks bool) string {
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

	b.WriteString("\n\ninstance id | name | type | tier | power | stat total")

	if stat != "" {
		b.WriteString(" | " + strings.ToLower(stat))
	}

	b.WriteString(" | location | tag | flags")

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

func renderItem(s *Snapshot, item *Item) string {
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
func renderDuplicates(s *Snapshot, groups []WeaponGroup, offset, total int) string {
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

	for _, group := range groups {
		b.WriteString("\n\n")
		b.WriteString(renderWeaponGroup(s, group))
	}

	return b.String()
}

// renderWeaponGroup renders the copies of one weapon as a table with one
// row per copy. The name column tells a version such as "(Adept)" from the
// base weapon.
func renderWeaponGroup(s *Snapshot, group WeaponGroup) string {
	var b strings.Builder

	noun := "copies"
	if len(group.Copies) == 1 {
		noun = "copy"
	}

	fmt.Fprintf(&b, "%s: %d %s\n\n", group.Name, len(group.Copies), noun)
	b.WriteString("instance id | name | type | power | location | tag | flags | perks\n")

	for _, item := range group.Copies {
		perks := make([]string, 0, len(item.Perks))
		for _, perk := range item.Perks {
			perks = append(perks, perkLabel(perk))
		}

		b.WriteString(strings.Join([]string{
			item.InstanceID,
			item.Name,
			typeLabel(item),
			powerLabel(item),
			s.LocationLabel(item),
			item.Tag,
			strings.Join(flagsOf(item), ", "),
			strings.Join(perks, "; "),
		}, " | "))
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

// renderLoadouts lists the saved loadouts of characters as one table per
// character, with a row per loadout item.
func renderLoadouts(s *Snapshot, characters []Character) string {
	var b strings.Builder

	b.WriteString("Loadouts saved in the game. A loadout number is the position of the loadout " +
		"on the in-game loadout screen, counted from 1. The server cannot read the name, icon, " +
		"or color that the game shows for a loadout. Pass the character and the loadout number to equip_loadout.")

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

	names := make([]string, 0, len(result.Items))
	for _, item := range result.Items {
		names = append(names, fmt.Sprintf("%s (%s)", item.Name, item.InstanceID))
	}

	fmt.Fprintf(&b, "Equipped loadout %d on %s", l.Number, target)

	if len(names) > 0 {
		fmt.Fprintf(&b, ": %s", strings.Join(names, ", "))
	}

	b.WriteString(".")

	if len(result.Missing) > 0 {
		fmt.Fprintf(&b, " The inventory does not hold these loadout items: %s.", strings.Join(result.Missing, ", "))
	}

	return b.String()
}

// perkLabel names a perk and the other plugs that its socket can hold.
func perkLabel(perk Perk) string {
	if len(perk.Options) == 0 {
		return perk.Name
	}

	return perk.Name + " (can swap to " + strings.Join(perk.OptionNames(), ", ") + ")"
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

// liveRules drops the removed rules and returns the rest in evaluation
// order.
func liveRules(rules []AutotagRule) []AutotagRule {
	live := slices.DeleteFunc(slices.Clone(rules), func(rule AutotagRule) bool { return rule.Removed })

	slices.SortStableFunc(live, func(a, b AutotagRule) int { return cmp.Compare(a.Position, b.Position) })

	return live
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
