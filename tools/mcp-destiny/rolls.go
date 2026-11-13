package main

import "slices"

// Stances of a perk preference.
const (
	stanceUp   = "up"
	stanceDown = "down"
)

// Hit names one perk that earns a perk column. Liked marks a perk that
// the user likes for the game mode of the rating, whether or not the
// sheet also picks the perk.
type Hit struct {
	Name  string
	Liked bool
}

// ColumnVerdict judges one perk column of a sheet row. A column without
// hits is a miss.
type ColumnVerdict struct {
	Hits []Hit
}

// Missed reports whether the copy has no wanted perk in the column.
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

// RatingSource holds what the rating of one game mode reads. Exactly
// one of Sheet and Err is set, and Err is the reason that the mode has
// no rating. Stances maps the name of each perk that the user likes or
// dislikes for the mode to its stance. [stancesByMode] builds the map.
// A source without Stances rates a copy from the sheet picks alone.
type RatingSource struct {
	Sheet   *Sheet
	Stances map[string]string
	Err     error
}

// stancesByMode returns the stance map of each game mode, keyed by the
// mode. Each map gives the stance of a perk under the perk name of its
// preference, unchanged.
//
// A removed preference has no stance. A preference whose mode is not in
// [perkModes] lands in no map. The service rejects such a mode in a
// write, and no rating reads it.
func stancesByMode(prefs []PerkPreference) map[string]map[string]string {
	stances := make(map[string]map[string]string, len(perkModes))
	for _, mode := range perkModes {
		stances[mode] = map[string]string{}
	}

	for _, pref := range prefs {
		if pref.Removed {
			continue
		}

		byPerk, ok := stances[pref.Mode]
		if ok {
			byPerk[pref.Perk] = pref.Stance
		}
	}

	return stances
}

// rate returns the [Rating] that the source of one game mode gives item.
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
func rate(source RatingSource, item *Item) Rating {
	// The reason comes first, so only a loaded sheet can report a weapon
	// as missing from it.
	if source.Err != nil {
		return Rating{Err: source.Err}
	}

	row, ok := source.Sheet.Rows[item.Hash]
	if !ok {
		return Rating{}
	}

	roll := &Roll{Row: row}
	columns := source.Sheet.Columns[item.Hash]

	switch {
	case len(columns) == 0:
		roll.Overall = row.Tier
	case len(item.Perks) == 0:
		roll.Unknown = true
	default:
		misses := 0

		for _, column := range columns {
			verdict := judgeColumn(item.Perks, column, source.Stances)
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
// Column lists the plug hashes that the sheet picks. Stances gives the
// stance of the user toward each perk, under the name that an enhanced
// perk shares with its base perk.
//
// The socket of the column is the first socket that holds a pick. The
// stances play no part in that choice, so a disliked pick still claims
// the column for its socket. A column has no fixed socket index, so two
// columns can resolve to one socket. A column whose picks no socket
// holds is a miss, also when the copy holds a perk that the user likes.
//
// That socket earns the column with each plug that the user likes, and
// with each pick that the user does not dislike. The hits name those
// plugs, one time for each name. The active plug of the socket comes
// first when it is one of them. The column is a miss when the socket
// holds no such plug.
func judgeColumn(perks []Perk, column []uint32, stances map[string]string) ColumnVerdict {
	for _, perk := range perks {
		plugs := socketPlugs(perk)

		picked := slices.ContainsFunc(plugs, func(plug Plug) bool { return slices.Contains(column, plug.Hash) })
		if !picked {
			continue
		}

		var hits []Hit

		for _, plug := range plugs {
			stance := stances[basePerkName(plug.Name)]

			wanted := stance == stanceUp || (stance != stanceDown && slices.Contains(column, plug.Hash))
			if !wanted {
				continue
			}

			// A base trait and its enhanced trait share one name, and
			// the active plug is also one of the reusable plugs.
			named := slices.ContainsFunc(hits, func(hit Hit) bool { return hit.Name == plug.Name })
			if !named {
				hits = append(hits, Hit{Name: plug.Name, Liked: stance == stanceUp})
			}
		}

		return ColumnVerdict{Hits: hits}
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

// ratedAtLeast reports whether rating holds a roll whose tier is lowest
// or a better tier. Lowest is one of [tiers].
//
// Ratings without a roll never pass, so a game mode without ratings and
// a weapon that the sheet does not list pass no minimum. A roll without
// a tier ranks after F, so a row without a tier and an unknown roll
// pass no minimum either.
func ratedAtLeast(rating Rating, lowest string) bool {
	return rating.Roll != nil && tierRank(rating.Roll.Overall) <= tierRank(lowest)
}
