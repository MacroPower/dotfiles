package main

import (
	"slices"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
)

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
// hits is a miss. Picks names the perks that the sheet picks for the
// column, on a hit and on a miss. [picksOf] builds the list.
type ColumnVerdict struct {
	Hits  []Hit
	Picks []string
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
	Columns []ColumnVerdict
	Row     SheetRow
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
// Defs names the picks of each perk column and holds the plug sets of
// each weapon, and every source with a Sheet holds it.
type RatingSource struct {
	Sheet   *Sheet
	Stances map[string]string
	Defs    *definitions.Defs
	Err     error
}

// stancesByMode returns the stance map of each game mode, keyed by the
// mode. Each map gives the stance of a perk under the perk name of its
// preference, unchanged.
//
// A removed preference has no stance. A preference whose mode is not in
// [perkModes] lands in no map. The service rejects such a mode in a
// write, and no rating reads it.
func stancesByMode(prefs []dvm.PerkPreference) map[string]map[string]string {
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
//
// The definitions of the source hold the plug sets of the weapon. The
// plug sets name the socket of a column in which the copy rolled no
// pick. A perk that the user likes can then earn the column. A source
// whose definitions hold no plug sets leaves such a column a miss.
//
// Each column verdict also names the picks of its column. The names
// come from the definitions of the source and not from the copy. A
// column that no socket of the copy matches therefore still names its
// picks.
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
			verdict := judgeColumn(source.Defs, item, column, source.Stances)
			verdict.Picks = picksOf(source.Defs, column, source.Stances)

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
// [columnSocket] finds the socket of the column. A column without a
// socket is a miss, also when the copy holds a perk that the user
// likes.
//
// That socket earns the column with each plug that the user likes, and
// with each pick that the user does not dislike. The hits name those
// plugs, one time for each name. The active plug of the socket comes
// first when it is one of them. The column is a miss when the socket
// holds no such plug.
func judgeColumn(defs *definitions.Defs, item *Item, column []uint32, stances map[string]string) ColumnVerdict {
	socket, ok := columnSocket(defs, item, column)
	if !ok {
		return ColumnVerdict{}
	}

	var hits []Hit

	for _, plug := range socketPlugs(socket) {
		stance := stances[definitions.BasePerkName(plug.Name)]

		wanted := stance == stanceUp || (stance != stanceDown && slices.Contains(column, plug.Hash))
		if !wanted {
			continue
		}

		// A base trait and its enhanced trait share one name, and the
		// active plug is also one of the reusable plugs.
		named := slices.ContainsFunc(hits, func(hit Hit) bool { return hit.Name == plug.Name })
		if !named {
			hits = append(hits, Hit{Name: plug.Name, Liked: stance == stanceUp})
		}
	}

	return ColumnVerdict{Hits: hits}
}

// columnSocket returns the socket of item that a perk column describes.
// It reports false when the copy has no such socket. Column lists the
// plug hashes that the sheet picks.
//
// The socket of the column is the first socket of the copy that holds a
// pick. The stances of the user play no part in that choice, so a
// disliked pick still claims the column for its socket. A column has no
// fixed socket index, so two columns can resolve to one socket.
//
// When no socket of the copy holds a pick, the plug sets of the weapon
// definition decide. The socket of the column is then the first socket
// of the definition whose plug set lists a pick. That socket holds no
// pick, so only a perk that the user likes can earn the column in it.
// The web client of Agg's Destiny Vault Manager resolves a column in
// the same two steps.
//
// The sockets of a copy have the indexes of the sockets of its
// definition. The column has no socket when [Item.Perks] leaves out the
// socket that the plug sets name, such as a socket that the profile
// hides. The function then tries no later socket of the definition.
func columnSocket(defs *definitions.Defs, item *Item, column []uint32) (Perk, bool) {
	for _, perk := range item.Perks {
		picked := slices.ContainsFunc(socketPlugs(perk), func(plug Plug) bool {
			return slices.Contains(column, plug.Hash)
		})
		if picked {
			return perk, true
		}
	}

	index, ok := defs.PlugSetSocket(item.Hash, column)
	if !ok {
		return Perk{}, false
	}

	i := slices.IndexFunc(item.Perks, func(perk Perk) bool { return perk.Socket == index })
	if i < 0 {
		return Perk{}, false
	}

	return item.Perks[i], true
}

// picksOf names the perks that the sheet picks for one perk column, in
// the order of the column. Column lists the plug hashes of the picks,
// and defs gives each hash its name. Stances gives the stance of the
// user toward each perk, under the name that an enhanced perk shares
// with its base perk.
//
// The list holds each name one time, so a base trait and an enhanced
// trait that share a name give one entry. It leaves out each hash that
// defs does not define and each perk that the user dislikes. A liked
// perk enters the list only when the column picks it.
//
// The sheet fills a column from every version of the weapon. A pick can
// therefore be a perk that the rated version cannot roll.
func picksOf(defs *definitions.Defs, column []uint32, stances map[string]string) []string {
	var picks []string

	for _, hash := range column {
		def, ok := defs.Items[hash]
		if !ok || slices.Contains(picks, def.Name) || stances[definitions.BasePerkName(def.Name)] == stanceDown {
			continue
		}

		picks = append(picks, def.Name)
	}

	return picks
}

// socketPlugs returns each plug that the socket of perk holds, with the
// active plug first. The active plug always counts, also when the
// profile leaves it out of the plugs that the socket can hold.
func socketPlugs(perk Perk) []Plug {
	return append([]Plug{perk.Plug}, perk.Plugs...)
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
