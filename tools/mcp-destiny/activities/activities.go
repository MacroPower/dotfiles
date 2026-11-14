package activities

import (
	"cmp"
	"slices"
	"strings"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
)

var (
	// droppedModes holds the Bungie DestinyActivityModeType values of Story
	// (2) and Social (40). Activities of these modes carry no loot, so the
	// web client leaves them out, and [Read] does the same.
	droppedModes = map[int]bool{2: true, 40: true}

	// droppedTypes holds the hashes of the activity types that Bungie files
	// as Story. Bungie gives some activities of these types no mode, such
	// as "Excision: Grandmaster". The web client therefore leaves them out
	// by type, and [Read] does the same.
	droppedTypes = map[uint32]bool{1838596016: true, 1686739444: true, 1299744814: true, 2694988718: true}
)

// Row is one activity that a character can play, joined with the
// definitions. Name is the name of the activity definition, and Type is
// the name of its activity type. Location names the destination of the
// activity, or its place when the destination has no name, and it is
// empty when it equals Type. Power is the recommended power, and zero
// means that Bungie recommends none. Featured marks an activity in the
// featured carousel of the Portal.
//
// BonusDrops, Focus, and Loot hold the rewards that the activity still
// offers this week, because Bungie drops a reward after the character
// claims it. BonusDrops counts the bonus drops. Focus is the focused
// reward, and it is nil when the activity has none. Loot lists the bonus
// loot, one entry for each name.
type Row struct {
	Focus      *Loot
	Name       string
	Type       string
	Location   string
	Loot       []Loot
	BonusDrops int
	Power      int
	Hash       uint32
	Featured   bool
}

// Offers reports whether the activity still offers a bonus drop, a focused
// reward, or bonus loot this week.
func (a Row) Offers() bool {
	return a.BonusDrops > 0 || a.Focus != nil || len(a.Loot) > 0
}

// matches reports whether the lowercase string lower occurs in the name,
// the type, the location, the focused reward, or a bonus loot name of the
// activity, without regard to case.
func (a Row) matches(lower string) bool {
	if containsFold(a.Name, lower) || containsFold(a.Type, lower) || containsFold(a.Location, lower) {
		return true
	}

	if a.Focus != nil && containsFold(a.Focus.Name, lower) {
		return true
	}

	return slices.ContainsFunc(a.Loot, func(loot Loot) bool { return containsFold(loot.Name, lower) })
}

// Loot is one reward item of an activity. Name is the name of its
// ActivityReward record, and Hash is its item hash. Quantity is the
// quantity that Bungie reports.
type Loot struct {
	Name     string
	Quantity int
	Hash     uint32
}

// Read joins the activities that one character can play with
// the definitions, in the order of [compareActivities]. It leaves out each
// activity that the game hides, that the definitions do not name, or that
// has a Story or Social mode or a Story type.
func Read(entries []bungie.AvailableActivity, defs *definitions.Defs) []Row {
	var rows []Row

	for _, entry := range entries {
		def, ok := defs.Activities[entry.Hash]
		if !entry.Visible || !ok || def.Name == "" || droppedTypes[def.Type] || droppedModes[def.Mode] {
			continue
		}

		rows = append(rows, newActivityRow(entry, def, defs))
	}

	slices.SortFunc(rows, compareActivities)

	return rows
}

// newActivityRow joins one activity with its definition and with the
// definitions of its type, its location, and its rewards.
func newActivityRow(entry bungie.AvailableActivity, def definitions.ActivityDef, defs *definitions.Defs) Row {
	items := entry.RewardItems()

	row := Row{
		Hash:     entry.Hash,
		Name:     def.Name,
		Type:     defs.ActivityTypes[def.Type],
		Power:    entry.RecommendedLight,
		Featured: entry.Focused,
		Focus:    focusOf(items, defs),
		Loot:     lootOf(items, defs),
	}

	for _, item := range items {
		if item.UIStyle == bungie.RewardBonusDrop {
			row.BonusDrops += item.ItemQuantity.Quantity
		}
	}

	// The web client avoids a row such as "The Crucible in The Crucible".
	location := defs.Destinations[def.Destination]
	if location == "" {
		location = defs.Places[def.Place]
	}

	if location != row.Type {
		row.Location = location
	}

	return row
}

// focusOf returns the first focused reward among items that an
// ActivityReward record names. It returns nil when no item qualifies.
func focusOf(items []bungie.ActivityRewardItem, defs *definitions.Defs) *Loot {
	for _, item := range items {
		name := defs.ActivityRewards[item.ItemQuantity.ItemHash]
		if item.UIStyle == bungie.RewardFocus && name != "" {
			return &Loot{Name: name, Quantity: item.ItemQuantity.Quantity, Hash: item.ItemQuantity.ItemHash}
		}
	}

	return nil
}

// lootOf returns the bonus loot among items. Bonus loot is an item
// without a uiStyle, with a quantity of 1 or more, and with a name in an
// ActivityReward record. Items that share a name merge into the entry of
// the first one, which sums their quantities.
func lootOf(items []bungie.ActivityRewardItem, defs *definitions.Defs) []Loot {
	var loot []Loot

	at := map[string]int{}

	for _, item := range items {
		name := defs.ActivityRewards[item.ItemQuantity.ItemHash]
		if item.UIStyle != "" || item.ItemQuantity.Quantity < 1 || name == "" {
			continue
		}

		if i, ok := at[name]; ok {
			loot[i].Quantity += item.ItemQuantity.Quantity

			continue
		}

		at[name] = len(loot)
		loot = append(loot, Loot{Name: name, Quantity: item.ItemQuantity.Quantity, Hash: item.ItemQuantity.ItemHash})
	}

	return loot
}

// compareActivities orders activities by the reward keys of the ranking
// on the Activities page of the web client. It leaves out the Quickplay
// and power keys of that ranking. More bonus drops come first. Among
// activities with equal bonus drops, those with a focused reward come
// first, then those with bonus loot, then the featured ones. Name and hash
// break the remaining ties.
func compareActivities(a, b Row) int {
	return cmp.Or(
		cmp.Compare(b.BonusDrops, a.BonusDrops),
		compareFlags(a.Focus != nil, b.Focus != nil),
		compareFlags(len(a.Loot) > 0, len(b.Loot) > 0),
		compareFlags(a.Featured, b.Featured),
		cmp.Compare(a.Name, b.Name),
		cmp.Compare(a.Hash, b.Hash),
	)
}

// compareFlags orders a set flag before a clear one.
func compareFlags(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	default:
		return 1
	}
}

// Filter returns the activities that match query and rewards,
// in their order. A query keeps each activity that [Row.matches],
// and an empty or blank query keeps every activity. A true rewards keeps
// only the activities that [Row.Offers] reports.
func Filter(rows []Row, query string, rewards bool) []Row {
	lower := strings.ToLower(strings.TrimSpace(query))

	var out []Row

	for _, row := range rows {
		if rewards && !row.Offers() {
			continue
		}

		if lower != "" && !row.matches(lower) {
			continue
		}

		out = append(out, row)
	}

	return out
}

// containsFold reports whether s contains the lowercase string lower,
// without regard to the case of s.
func containsFold(s, lower string) bool {
	return strings.Contains(strings.ToLower(s), lower)
}
