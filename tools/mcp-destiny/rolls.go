package main

import "slices"

// Hit names one perk that earns a perk column.
type Hit struct {
	Name string
}

// ColumnVerdict judges one perk column of a sheet row. A column without
// hits is a miss.
type ColumnVerdict struct {
	Hits []Hit
}

// Missed reports whether the copy has none of the picks of the column.
func (v ColumnVerdict) Missed() bool {
	return len(v.Hits) == 0
}

// Roll rates one copy against the row of its weapon. Overall is the tier
// of the copy. It is empty when the row has no tier and when Unknown is
// set. Columns holds one verdict for each column of the row. It is empty
// when the row has no columns and when Unknown is set. Unknown marks a
// copy without perks on a row that has columns.
type Roll struct {
	Overall string
	Row     SheetRow
	Columns []ColumnVerdict
	Unknown bool
}

// Rating is what one game mode says about one copy. Err is the reason
// that the mode has no rating. A nil Roll without an Err means that the
// loaded sheet does not list the weapon.
type Rating struct {
	Err  error
	Roll *Roll
}

// rate returns the [Rating] that the sheet state of one game mode gives
// item. State comes from [SheetStore.Load], which sets exactly one of
// its fields.
//
// The tier of a copy starts at the tier of its row and moves down one
// step for each column that missed. A row without perk columns, such as
// the row of an exotic, keeps its tier for a copy without perks. A row
// with columns has no verdict for such a copy, because the profile does
// not say which perks the copy rolled.
//
// The rating reads [Item.Perks], so it sees the sockets and the plugs
// that the snapshot keeps. Destiny Item Manager builds another socket
// list, and the package documentation lists where the two differ.
func rate(state SheetState, item *Item) Rating {
	// The reason comes first, so only a loaded sheet can report a weapon
	// as missing from it.
	if state.Err != nil {
		return Rating{Err: state.Err}
	}

	row, ok := state.Sheet.Rows[item.Hash]
	if !ok {
		return Rating{}
	}

	roll := &Roll{Row: row}
	columns := state.Sheet.Columns[item.Hash]

	switch {
	case len(columns) == 0:
		roll.Overall = row.Tier
	case len(item.Perks) == 0:
		roll.Unknown = true
	default:
		misses := 0

		for _, column := range columns {
			verdict := judgeColumn(item.Perks, column)
			if verdict.Missed() {
				misses++
			}

			roll.Columns = append(roll.Columns, verdict)
		}

		roll.Overall = stepTier(row.Tier, misses)
	}

	return Rating{Roll: roll}
}

// judgeColumn judges one perk column against the sockets of a copy.
// Column lists the plug hashes that the sheet picks.
//
// The socket of the column is the first socket that holds a pick. The
// hits name each pick that this socket holds, one time for each name.
// The active plug of the socket comes first when it is a pick. A column
// has no fixed socket index, so two columns can resolve to one socket. A
// column whose picks no socket holds is a miss.
func judgeColumn(perks []Perk, column []uint32) ColumnVerdict {
	for _, perk := range perks {
		var hits []Hit

		for _, plug := range socketPlugs(perk) {
			if !slices.Contains(column, plug.Hash) {
				continue
			}

			// A base trait and its enhanced trait share one name, and
			// the active plug is also one of the reusable plugs.
			named := slices.ContainsFunc(hits, func(hit Hit) bool { return hit.Name == plug.Name })
			if !named {
				hits = append(hits, Hit{Name: plug.Name})
			}
		}

		if len(hits) > 0 {
			return ColumnVerdict{Hits: hits}
		}
	}

	return ColumnVerdict{}
}

// socketPlugs returns each plug that the socket of perk holds, with the
// active plug first. The active plug always counts, also when the
// profile leaves it out of the plugs that the socket can hold.
func socketPlugs(perk Perk) []Plug {
	return append([]Plug{perk.Plug}, perk.plugs...)
}

// stepTier returns the tier that lies the given count of steps down from
// tier in [tiers], and F when the steps pass the last tier. A row
// without a tier stays without one.
func stepTier(tier string, steps int) string {
	i := slices.Index(tiers, tier)
	if i < 0 {
		return ""
	}

	return tiers[min(i+steps, len(tiers)-1)]
}
