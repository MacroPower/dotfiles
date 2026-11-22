package main

import (
	"fmt"
	"slices"
	"strings"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/activities"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/inventory"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/loot"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/vendors"
)

// weekSources holds the sources that one character can draw on this week
// to advance the patterns. Owner names the character, such as "Hunter".
// Featured lists the names of the featured activities of the character,
// in the order of [activities.Read], with each name one time.
//
// Vendors maps the record hash of each pattern to the vendors that sell a
// copy that advances the pattern now, as [inventory.PatternHash] finds
// the pattern of a sale. Each entry names one vendor, as [vendorLabel]
// names it, and it ends with " (unavailable)" when the character cannot
// buy any such copy from that vendor. Activities maps the record hash of
// each pattern to the names of the featured activities that drop the
// weapon of the pattern.
type weekSources struct {
	Vendors    map[uint32][]string
	Activities map[uint32][]string
	Owner      string
	Featured   []string
}

// Has reports whether a vendor or a featured activity is a source of the
// pattern with the given record hash.
func (w *weekSources) Has(record uint32) bool {
	return len(w.Vendors[record]) > 0 || len(w.Activities[record]) > 0
}

// featuredLabel names the featured activities of the character in one
// sentence.
func (w *weekSources) featuredLabel() string {
	switch len(w.Featured) {
	case 0:
		return "Bungie does not list a featured activity for " + w.Owner + "."
	case 1:
		return "Featured activity of " + w.Owner + ": " + w.Featured[0] + "."
	default:
		return "Featured activities of " + w.Owner + ": " + strings.Join(w.Featured, ", ") + "."
	}
}

// vendorLabel names vendor v in a cell of the "sold by" column. Names
// counts the vendors of the character with each name. The vendor
// argument of list_vendors selects a vendor by its name only when no
// other vendor of the character has that name. So the label is the name
// of v when names counts that name one time. Otherwise the label adds the
// hash of v in parentheses, as the ambiguity error of [vendors.Select]
// does, such as "Focused Decoding (444807002)".
func vendorLabel(v vendors.Vendor, names map[string]int) string {
	if names[v.Name] > 1 {
		return fmt.Sprintf("%s (%d)", v.Name, v.Hash)
	}

	return v.Name
}

// newWeekSources joins the vendors and the activities of one character
// with the patterns of rows. Owner names the character. Listed holds the
// vendors that [vendors.Read] returns, and the join skips each disabled
// vendor. Played holds the activities that [activities.Read] returns.
//
// A featured activity is a source of a pattern when it belongs to a place
// that drops the weapon of the pattern. Table gives the places that drop
// each weapon, and [*loot.Table.Place] gives the place of each activity.
func newWeekSources(
	owner string,
	listed []vendors.Vendor,
	played []activities.Row,
	rows []inventory.PatternRow,
	table *loot.Table,
	defs *definitions.Defs,
) *weekSources {
	w := &weekSources{
		Owner:      owner,
		Vendors:    map[uint32][]string{},
		Activities: map[uint32][]string{},
	}

	// A disabled vendor still counts for the names, because list_vendors
	// selects among every vendor that Bungie returns.
	names := map[string]int{}

	for _, v := range listed {
		names[v.Name]++
	}

	for _, v := range listed {
		if v.Disabled {
			continue
		}

		// Available maps the record hash of each pattern that v sells to
		// whether the character can buy at least one copy that advances
		// the pattern.
		available := map[uint32]bool{}

		for _, group := range v.Groups {
			for _, sale := range group.Sales {
				record, ok := inventory.PatternHash(defs, sale.ItemHash)
				if ok {
					available[record] = available[record] || sale.Available
				}
			}
		}

		label := vendorLabel(v, names)

		for record, canBuy := range available {
			if canBuy {
				w.Vendors[record] = append(w.Vendors[record], label)
			} else {
				w.Vendors[record] = append(w.Vendors[record], label+" (unavailable)")
			}
		}
	}

	featured := map[string][]string{}

	for _, row := range played {
		if !row.Featured || slices.Contains(w.Featured, row.Name) {
			continue
		}

		w.Featured = append(w.Featured, row.Name)

		place, ok := table.Place(row.Name)
		if ok {
			featured[place] = append(featured[place], row.Name)
		}
	}

	for _, row := range rows {
		for _, place := range table.Drops(row.Name) {
			if len(featured[place]) > 0 {
				w.Activities[row.Hash] = append(w.Activities[row.Hash], featured[place]...)
			}
		}
	}

	return w
}
