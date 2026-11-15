package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/curves"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/inventory"
)

// measureFakes returns fakes whose account holds weapons with real frame
// hashes and real stat hashes, all in the vault. Instances 3001 and 3002
// are copies of a submachine gun with the Lightweight Frame. Instance
// 3002 holds the plug hash that a crafted copy gives the frame.
// Instance 3003 is a glaive, and instance 3004 is a sword, a weapon type
// without curves.
func measureFakes(t *testing.T) *fakes {
	t.Helper()

	weapon := func(name, typeName string, damage int) definitions.ItemDef {
		return definitions.ItemDef{
			Name:       name,
			TypeName:   typeName,
			Tier:       "Legendary",
			Bucket:     bungie.BucketKinetic,
			ItemType:   bungie.ItemTypeWeapon,
			ClassType:  bungie.ClassAny,
			DamageType: damage,
			Equippable: true,
		}
	}

	plug := func(name, typeName string) definitions.ItemDef {
		return definitions.ItemDef{
			Name:      name,
			TypeName:  typeName,
			ItemType:  bungie.ItemTypePlug,
			ClassType: bungie.ClassAny,
		}
	}

	f := newFakes(t)
	f.defs.Items[500] = weapon("Multimach CCX", "Submachine Gun", 1)
	f.defs.Items[501] = weapon("Judgment of Kelgorath", "Glaive", 3)
	f.defs.Items[502] = weapon("Bequest", "Sword", 3)
	f.defs.Items[1458010786] = plug("Lightweight Frame", "Intrinsic")
	f.defs.Items[118439741] = plug("Lightweight Frame", "Enhanced Intrinsic")
	f.defs.Items[1316753551] = plug("Adaptive Glaive", "Intrinsic")
	f.defs.Items[503] = plug("Adaptive Frame", "Intrinsic")
	f.defs.Stats[curves.StatReload] = "Reload Speed"
	f.defs.Stats[curves.StatHandling] = "Handling"
	f.defs.Stats[curves.StatRange] = "Range"

	f.bungie.ProfileBody = `{"ErrorCode": 1, "Response": {
		"characters": {"data": {
			"c1": {"characterId": "c1", "classType": 1, "light": 2000, "dateLastPlayed": "2026-11-10T12:00:00Z"}
		}},
		"profileInventory": {"data": {"items": [
			{"itemHash": 500, "itemInstanceId": "3001", "lockable": true, "quantity": 1, "location": 2, "bucketHash": 138197802},
			{"itemHash": 500, "itemInstanceId": "3002", "lockable": true, "quantity": 1, "location": 2, "bucketHash": 138197802},
			{"itemHash": 501, "itemInstanceId": "3003", "lockable": true, "quantity": 1, "location": 2, "bucketHash": 138197802},
			{"itemHash": 502, "itemInstanceId": "3004", "lockable": true, "quantity": 1, "location": 2, "bucketHash": 138197802}
		]}},
		"itemComponents": {
			"instances": {"data": {
				"3001": {"damageType": 1, "primaryStat": {"statHash": 1480404414, "value": 2000}},
				"3002": {"damageType": 1, "primaryStat": {"statHash": 1480404414, "value": 2010}},
				"3003": {"damageType": 3, "primaryStat": {"statHash": 1480404414, "value": 1990}},
				"3004": {"damageType": 3, "primaryStat": {"statHash": 1480404414, "value": 1980}}
			}},
			"stats": {"data": {
				"3001": {"stats": {
					"4188031367": {"statHash": 4188031367, "value": 37},
					"943549884": {"statHash": 943549884, "value": 70},
					"1240592695": {"statHash": 1240592695, "value": 48}
				}},
				"3002": {"stats": {
					"4188031367": {"statHash": 4188031367, "value": 47},
					"943549884": {"statHash": 943549884, "value": 60},
					"1240592695": {"statHash": 1240592695, "value": 58}
				}},
				"3003": {"stats": {
					"4188031367": {"statHash": 4188031367, "value": 50},
					"943549884": {"statHash": 943549884, "value": 64},
					"1240592695": {"statHash": 1240592695, "value": 40}
				}}
			}},
			"sockets": {"data": {
				"3001": {"sockets": [{"plugHash": 1458010786, "isEnabled": true, "isVisible": true}]},
				"3002": {"sockets": [{"plugHash": 118439741, "isEnabled": true, "isVisible": true}]},
				"3003": {"sockets": [{"plugHash": 1316753551, "isEnabled": true, "isVisible": true}]},
				"3004": {"sockets": [{"plugHash": 503, "isEnabled": true, "isVisible": true}]}
			}}
		}
	}}`

	return f
}

// Lines that the measure tests share. The fake service publishes no roll
// sheet, so each game mode has no ratings.
const (
	measureHeader = "instance id | name | type | power | location | tag | flags | pve rating | pvp rating | " +
		"reload | ready | stow | aim | hip falloff | aim falloff | perks"

	glaiveReload = "Update 9.7.0 changed the reload timing of glaives, and no measurement is newer."
	glaiveAim    = "The community data has no aim-down-sights time for this weapon."
	glaiveScale  = "The community data has no aim-down-sights falloff for glaives."
	noTypeCurves = "The community data has no curves for this weapon type."
	unknownFrame = "The profile lists no perks for this copy, so the server cannot tell its frame."

	noReloadStat   = "This copy has no Reload Speed stat."
	noHandlingStat = "This copy has no Handling stat."
	noRangeStat    = "This copy has no Range stat."

	noRateStat   = "This copy has no Rounds Per Minute stat."
	noTypeDamage = "The community data has no damage for this weapon type."
)

// TestGetItemMeasures pins the block of times and falloff distances that
// get_item prints for a weapon. Each value is the curve of the frame at
// the stats of the copy.
func TestGetItemMeasures(t *testing.T) {
	t.Parallel()

	// ratings holds the lines between the perks and the stats of each
	// output.
	ratings := []string{
		"",
		"Roll ratings:",
		"- PvE: rating unavailable: the service publishes no Aegis sheet",
		"- PvP: rating unavailable: the service publishes no PvP sheet",
		"",
	}

	tests := map[string]struct {
		// edit changes the cached snapshot before the call.
		edit func(*inventory.Snapshot)
		id   string
		// head holds the lines before the roll ratings, and tail holds
		// the lines after them. Figures is the reason of the line of
		// [writeFigures], which ends the output. No copy of the fakes
		// has the stats that the line reads.
		head    []string
		tail    []string
		figures string
	}{
		"every measure of a submachine gun": {
			id: "3001",
			head: []string{
				"Multimach CCX",
				"Instance id: 3001",
				"Item hash: 500",
				"Type: Kinetic Submachine Gun",
				"Tier: Legendary",
				"Slot: Kinetic Weapons",
				"Power: 2000",
				"Location: Vault",
				"",
				"Perks:",
				"- Lightweight Frame (Intrinsic)",
			},
			tail: []string{
				"Stats:",
				"- Handling: 70",
				"- Range: 48",
				"- Reload Speed: 37",
				"",
				"Base times and damage falloff at these stats, from community measurements:",
				"- Reload: 2.00 s",
				"- Ready: 0.30 s",
				"- Stow: 0.27 s",
				"- Aim down sights: 0.23 s",
				"- Falloff start from the hip: 13.42 m",
				"- Falloff start aiming down sights: 19.46 m",
			},
			figures: noRateStat,
		},
		"frame of a crafted copy": {
			id: "3002",
			head: []string{
				"Multimach CCX",
				"Instance id: 3002",
				"Item hash: 500",
				"Type: Kinetic Submachine Gun",
				"Tier: Legendary",
				"Slot: Kinetic Weapons",
				"Power: 2010",
				"Location: Vault",
				"",
				"Perks:",
				"- Lightweight Frame (Enhanced Intrinsic)",
			},
			tail: []string{
				"Stats:",
				"- Handling: 60",
				"- Range: 58",
				"- Reload Speed: 47",
				"",
				"Base times and damage falloff at these stats, from community measurements:",
				"- Reload: 1.86 s",
				"- Ready: 0.33 s",
				"- Stow: 0.30 s",
				"- Aim down sights: 0.25 s",
				"- Falloff start from the hip: 14.07 m",
				"- Falloff start aiming down sights: 20.40 m",
			},
			figures: noRateStat,
		},
		"each unknown measure of a glaive gives its reason": {
			id: "3003",
			head: []string{
				"Judgment of Kelgorath",
				"Instance id: 3003",
				"Item hash: 501",
				"Type: Solar Glaive",
				"Tier: Legendary",
				"Slot: Kinetic Weapons",
				"Power: 1990",
				"Location: Vault",
				"",
				"Perks:",
				"- Adaptive Glaive (Intrinsic)",
			},
			tail: []string{
				"Stats:",
				"- Handling: 64",
				"- Range: 40",
				"- Reload Speed: 50",
				"",
				"Base times and damage falloff at these stats, from community measurements:",
				"- Reload: unknown. " + glaiveReload,
				"- Ready: 0.29 s",
				"- Stow: 0.33 s",
				"- Aim down sights: unknown. " + glaiveAim,
				"- Falloff start from the hip: 17.18 m",
				"- Falloff start aiming down sights: unknown. " + glaiveScale,
			},
			figures: noTypeDamage,
		},
		// Instance 3005 is a copy of instance 3001 for which the profile
		// lists no stats. Every measure is unknown, but the reasons differ,
		// so each line of the block gives the stat that its measure lacks.
		"copy without stats gives the reason of each measure": {
			edit: func(s *inventory.Snapshot) {
				base, _ := s.Find("3001")
				bare := *base
				bare.InstanceID = "3005"
				bare.Stats = nil
				s.Add(&bare)
			},
			id: "3005",
			head: []string{
				"Multimach CCX",
				"Instance id: 3005",
				"Item hash: 500",
				"Type: Kinetic Submachine Gun",
				"Tier: Legendary",
				"Slot: Kinetic Weapons",
				"Power: 2000",
				"Location: Vault",
				"",
				"Perks:",
				"- Lightweight Frame (Intrinsic)",
			},
			tail: []string{
				"Base times and damage falloff at these stats, from community measurements:",
				"- Reload: unknown. " + noReloadStat,
				"- Ready: unknown. " + noHandlingStat,
				"- Stow: unknown. " + noHandlingStat,
				"- Aim down sights: unknown. " + noHandlingStat,
				"- Falloff start from the hip: unknown. " + noRangeStat,
				"- Falloff start aiming down sights: unknown. " + noRangeStat,
			},
			figures: noRateStat,
		},
		"weapon type without curves gives one line": {
			id: "3004",
			head: []string{
				"Bequest",
				"Instance id: 3004",
				"Item hash: 502",
				"Type: Solar Sword",
				"Tier: Legendary",
				"Slot: Kinetic Weapons",
				"Power: 1980",
				"Location: Vault",
				"",
				"Perks:",
				"- Adaptive Frame (Intrinsic)",
			},
			tail: []string{
				"Times and falloff: unknown. " + noTypeCurves,
			},
			figures: noTypeDamage,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := measureFakes(t)

			if tc.edit != nil {
				s, err := f.h.loadSnapshot(t.Context(), false)
				require.NoError(t, err)
				tc.edit(s)
			}

			got, isError := call(t, f.h.handleGetItem, GetItemInput{InstanceID: tc.id})
			assert.False(t, isError)

			figures := []string{"", allFiguresUnknown + " " + tc.figures}

			assert.Equal(t, strings.Join(slices.Concat(tc.head, ratings, tc.tail, figures), "\n"), got)
		})
	}
}

// TestCompareDuplicatesMeasures pins the measure columns of
// compare_duplicates. Each copy fills the cells with the values at its
// own stats, and the lines after the title give the reason of each
// empty cell.
func TestCompareDuplicatesMeasures(t *testing.T) {
	t.Parallel()

	noRatings := []string{
		"PvE: rating unavailable: the service publishes no Aegis sheet",
		"PvP: rating unavailable: the service publishes no PvP sheet",
		"",
	}

	tests := map[string]struct {
		// edit changes the cached snapshot before the call.
		edit func(*inventory.Snapshot)
		name string
		want []string
	}{
		"two rolls of one weapon": {
			name: "multimach",
			want: []string{
				"Multimach CCX: 2 copies",
				"",
				measureHeader,
				"3001 | Multimach CCX | Kinetic Submachine Gun | 2000 | Vault |  |  | unavailable | unavailable | " +
					"2.00 s | 0.30 s | 0.27 s | 0.23 s | 13.42 m | 19.46 m | Lightweight Frame",
				"3002 | Multimach CCX | Kinetic Submachine Gun | 2010 | Vault |  |  | unavailable | unavailable | " +
					"1.86 s | 0.33 s | 0.30 s | 0.25 s | 14.07 m | 20.40 m | Lightweight Frame",
			},
		},
		"each empty column of a glaive gives its reason": {
			name: "kelgorath",
			want: []string{
				"Judgment of Kelgorath: 1 copy",
				"reload: unknown. " + glaiveReload,
				"aim: unknown. " + glaiveAim,
				"aim falloff: unknown. " + glaiveScale,
				"",
				measureHeader,
				"3003 | Judgment of Kelgorath | Solar Glaive | 1990 | Vault |  |  | unavailable | unavailable | " +
					" | 0.29 s | 0.33 s |  | 17.18 m |  | Adaptive Glaive",
			},
		},
		// Instance 3005 is a copy of instance 3001 for which the profile
		// lists no perks. The copies with a frame keep their values. The
		// line after the title gives the missing perks as the reason of
		// the empty cells of instance 3005.
		"copy without perks beside copies with a frame": {
			edit: func(s *inventory.Snapshot) {
				base, _ := s.Find("3001")
				bare := *base
				bare.InstanceID = "3005"
				bare.Perks = nil
				s.Add(&bare)
			},
			name: "multimach",
			want: []string{
				"Multimach CCX: 3 copies",
				"Times and falloff: unknown. " + unknownFrame,
				"",
				measureHeader,
				"3001 | Multimach CCX | Kinetic Submachine Gun | 2000 | Vault |  |  | unavailable | unavailable | " +
					"2.00 s | 0.30 s | 0.27 s | 0.23 s | 13.42 m | 19.46 m | Lightweight Frame",
				"3002 | Multimach CCX | Kinetic Submachine Gun | 2010 | Vault |  |  | unavailable | unavailable | " +
					"1.86 s | 0.33 s | 0.30 s | 0.25 s | 14.07 m | 20.40 m | Lightweight Frame",
				"3005 | Multimach CCX | Kinetic Submachine Gun | 2000 | Vault |  |  | unavailable | unavailable | " +
					" |  |  |  |  |  | ",
			},
		},
		"weapon type without curves gives one line": {
			name: "bequest",
			want: []string{
				"Bequest: 1 copy",
				"Times and falloff: unknown. " + noTypeCurves,
				"",
				measureHeader,
				"3004 | Bequest | Solar Sword | 1980 | Vault |  |  | unavailable | unavailable | " +
					" |  |  |  |  |  | Adaptive Frame",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := measureFakes(t)

			if tc.edit != nil {
				s, err := f.h.loadSnapshot(t.Context(), false)
				require.NoError(t, err)
				tc.edit(s)
			}

			got, isError := call(t, f.h.handleCompareDuplicates, CompareDuplicatesInput{Name: tc.name})
			assert.False(t, isError)
			assert.Equal(t, strings.Join(append(noRatings, tc.want...), "\n"), got)
		})
	}
}

// TestMeasureNotes pins the lines that explain the empty measure cells of
// a table whose copies give different reasons.
func TestMeasureNotes(t *testing.T) {
	t.Parallel()

	// row returns the measures of a copy. Reasons holds the reason of
	// each measure in the order of the table, and an empty reason marks
	// a known measure.
	row := func(reasons ...string) []measure {
		measures := make([]measure, 0, len(measureKinds))
		for i, kind := range measureKinds {
			measures = append(measures, measure{measureKind: kind, Measure: curves.Measure{Reason: reasons[i]}})
		}

		return measures
	}

	tests := map[string]struct {
		rows [][]measure
		want []string
	}{
		"every measure known": {
			rows: [][]measure{row("", "", "", "", "", ""), row("", "", "", "", "", "")},
			want: []string{},
		},
		"one reason empties every column": {
			rows: [][]measure{row("No frame.", "No frame.", "No frame.", "No frame.", "No frame.", "No frame.")},
			want: []string{"Times and falloff: unknown. No frame."},
		},
		"one reason empties two columns": {
			rows: [][]measure{row("", "", "", "", "No falloff.", "No falloff.")},
			want: []string{"hip falloff and aim falloff: unknown. No falloff."},
		},
		"one reason empties three columns": {
			rows: [][]measure{row("", "No stat.", "No stat.", "No stat.", "", "")},
			want: []string{"ready, stow, and aim: unknown. No stat."},
		},
		// The second copy adds a column to the reason of the first copy
		// and gives a reason of its own. The columns keep the order of
		// the table, and the reasons keep the order of the rows.
		"copies with different reasons": {
			rows: [][]measure{
				row("", "", "", "", "", "No scale."),
				row("No reload.", "", "", "", "No scale.", "No scale."),
			},
			want: []string{
				"hip falloff and aim falloff: unknown. No scale.",
				"reload: unknown. No reload.",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, measureNotes(tc.rows))
		})
	}
}
