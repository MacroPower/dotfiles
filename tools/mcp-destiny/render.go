package main

import (
	"cmp"
	"fmt"
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
	}

	vault := 0

	for _, item := range s.Items {
		if item.Owner == ownerVault {
			vault++
		}
	}

	fmt.Fprintf(&b, "\nVault: %d items", vault)

	return b.String()
}

func renderItems(s *Snapshot, items []*Item, offset, total int, perks bool) string {
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

	b.WriteString("\n\ninstance id | name | type | tier | power | location | tag | flags")

	if perks {
		b.WriteString(" | perks")
	}

	b.WriteString("\n")

	for _, item := range items {
		id := item.InstanceID
		if id == "" {
			id = fmt.Sprintf("stack of %d (item hash %d)", item.Quantity, item.Hash)
		}

		fields := []string{
			id,
			item.Name,
			typeLabel(item),
			item.Tier,
			powerLabel(item),
			s.LocationLabel(item),
			item.Tag,
			strings.Join(flagsOf(item), ", "),
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
				fmt.Fprintf(&b, ", can swap to %s", strings.Join(perk.Options, ", "))
			}

			b.WriteString("\n")
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
