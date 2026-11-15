package main

import (
	"fmt"
	"strings"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/curves"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/inventory"
)

// allMeasuresUnknown opens the line of a weapon copy without any measure.
// The reason follows it.
const allMeasuresUnknown = "Times and falloff: unknown."

// measureKinds names the measures of a weapon copy in the order in which
// the tools print them.
var measureKinds = [...]measureKind{
	{label: "Reload", column: "reload", unit: "s"},
	{label: "Ready", column: "ready", unit: "s"},
	{label: "Stow", column: "stow", unit: "s"},
	{label: "Aim down sights", column: "aim", unit: "s"},
	{label: "Falloff start from the hip", column: "hip falloff", unit: "m"},
	{label: "Falloff start aiming down sights", column: "aim falloff", unit: "m"},
}

// measureKind holds the names that the tools print for one measure. Label
// opens the line of the measure in the get_item output. Column is the
// header of the measure in the table of [renderWeaponGroup]. Unit is "s"
// for seconds or "m" for meters.
type measureKind struct {
	label  string
	column string
	unit   string
}

// measure is one [curves.Measure] of a weapon copy with its
// [measureKind].
type measure struct {
	measureKind
	curves.Measure
}

// text returns the value with its unit, such as "1.87 s". It returns an
// empty string for an unknown value.
func (m measure) text() string {
	if !m.Known() {
		return ""
	}

	return fmt.Sprintf("%.2f %s", m.Value, m.unit)
}

// measuresOf returns the measures of a weapon copy in the order of
// [measureKinds]. The curves read the type, the item hash, the plug of
// each perk socket, and the stats of the copy.
func measuresOf(item *inventory.Item) []measure {
	weapon := curves.Weapon{
		Type:  item.TypeName,
		Hash:  item.Hash,
		Stats: make(map[uint32]int, len(item.Stats)),
	}

	for _, perk := range item.Perks {
		weapon.Plugs = append(weapon.Plugs, perk.Hash)
	}

	for _, stat := range item.Stats {
		weapon.Stats[stat.Hash] = stat.Value
	}

	m := curves.Of(weapon)
	values := [len(measureKinds)]curves.Measure{m.Reload, m.Ready, m.Stow, m.Aim, m.HipFalloff, m.AimFalloff}
	measures := make([]measure, 0, len(values))

	for i, value := range values {
		measures = append(measures, measure{measureKind: measureKinds[i], Measure: value})
	}

	return measures
}

// soleReason returns the reason that every measure shares. It reports
// false when a measure is known or when two reasons differ.
func soleReason(measures []measure) (string, bool) {
	reason := measures[0].Reason
	for _, m := range measures {
		if m.Known() || m.Reason != reason {
			return "", false
		}
	}

	return reason, true
}

// writeMeasures writes the measures of a weapon copy as a block of the
// get_item output. The block has one line for each measure. The line of
// an unknown measure gives the reason. One line replaces the block when
// every measure is unknown for the same reason.
func writeMeasures(b *strings.Builder, item *inventory.Item) {
	measures := measuresOf(item)

	reason, ok := soleReason(measures)
	if ok {
		fmt.Fprintf(b, "\n%s %s\n", allMeasuresUnknown, reason)

		return
	}

	b.WriteString("\nBase times and damage falloff at these stats, from community measurements:\n")

	for _, m := range measures {
		if m.Known() {
			fmt.Fprintf(b, "- %s: %s\n", m.label, m.text())
		} else {
			fmt.Fprintf(b, "- %s: unknown. %s\n", m.label, m.Reason)
		}
	}
}

// measureNotes returns one line for each reason that leaves a measure
// cell empty in the table of [renderWeaponGroup]. Rows holds the measures
// of each copy of the table. A line names the columns that the reason
// empties, in the order of the table, and then gives the reason. The line
// of a reason that empties every measure column opens with
// [allMeasuresUnknown] in place of the column names. The lines keep the
// order in which the rows first give their reasons.
func measureNotes(rows [][]measure) []string {
	var reasons []string

	emptied := map[string]map[string]bool{}

	for _, row := range rows {
		for _, m := range row {
			if m.Known() {
				continue
			}

			if emptied[m.Reason] == nil {
				emptied[m.Reason] = map[string]bool{}

				reasons = append(reasons, m.Reason)
			}

			emptied[m.Reason][m.column] = true
		}
	}

	notes := make([]string, 0, len(reasons))

	for _, reason := range reasons {
		var columns []string

		for _, kind := range measureKinds {
			if emptied[reason][kind.column] {
				columns = append(columns, kind.column)
			}
		}

		if len(columns) == len(measureKinds) {
			notes = append(notes, allMeasuresUnknown+" "+reason)

			continue
		}

		notes = append(notes, fmt.Sprintf("%s: unknown. %s", listWithAnd(columns), reason))
	}

	return notes
}
