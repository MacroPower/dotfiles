package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/curves"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/damage"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
)

// figureFakes returns fakes whose account holds weapons with real frame
// hashes and real stat hashes, all in the vault. Each copy has the Rounds
// Per Minute stat and the Magazine stat unless its comment says
// otherwise.
//
//   - Instance 4001 is a submachine gun with the Lightweight Frame. Its
//     perks are a barrel, Kinetic Tremors, Target Lock, and Skulking Wolf.
//   - Instance 4002 is a hand cannon with the Adaptive Frame. Its perks
//     are the enhanced Kill Clip, Rampage, and Outlaw.
//   - Instance 4003 is an auto rifle with the High-Impact Frame of a
//     crafted copy.
//   - Instance 4004 is a hand cannon with the Adaptive Frame and without
//     the Reload Speed stat.
//   - Instance 4005 is an auto rifle with the High-Impact Frame whose
//     Rounds Per Minute stat is the rate of the frame before Update
//     9.7.0.
//   - Instance 4006 is an auto rifle with the High-Impact Frame and
//     without the Reload Speed stat.
//   - Instance 4007 is an auto rifle with the Adaptive Frame, Frenzy, and
//     a Reload Speed stat of 30.
func figureFakes(t *testing.T) *fakes {
	t.Helper()

	weapon := func(name, typeName string) definitions.ItemDef {
		return definitions.ItemDef{
			Name:       name,
			TypeName:   typeName,
			Tier:       "Legendary",
			Bucket:     bungie.BucketKinetic,
			ItemType:   bungie.ItemTypeWeapon,
			ClassType:  bungie.ClassAny,
			DamageType: 1,
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
	f.defs.Items[600] = weapon("Multimach CCX", "Submachine Gun")
	f.defs.Items[601] = weapon("Fatebringer", "Hand Cannon")
	f.defs.Items[602] = weapon("Abyss Defiant", "Auto Rifle")
	f.defs.Items[1458010786] = plug("Lightweight Frame", "Intrinsic")
	f.defs.Items[1294026524] = plug("Adaptive Frame", "Intrinsic")
	f.defs.Items[1019291327] = plug("High-Impact Frame", "Enhanced Intrinsic")
	f.defs.Items[620] = plug("Hammer-Forged Rifling", "Barrel")
	f.defs.Items[621] = plug("Kinetic Tremors", "Trait")
	f.defs.Items[622] = plug("Target Lock", "Trait")
	f.defs.Items[623] = plug("Skulking Wolf", "Origin Trait")
	f.defs.Items[624] = plug("Kill Clip", "Enhanced Trait")
	f.defs.Items[625] = plug("Rampage", "Trait")
	f.defs.Items[626] = plug("Outlaw", "Trait")
	f.defs.Items[627] = plug("Frenzy", "Trait")
	f.defs.Stats[curves.StatReload] = "Reload Speed"
	f.defs.Stats[damage.StatRoundsPerMinute] = "Rounds Per Minute"
	f.defs.Stats[damage.StatMagazine] = "Magazine"

	f.bungie.ProfileBody = `{"ErrorCode": 1, "Response": {
		"characters": {"data": {
			"c1": {"characterId": "c1", "classType": 1, "light": 2000, "dateLastPlayed": "2026-11-10T12:00:00Z"}
		}},
		"profileInventory": {"data": {"items": [
			{"itemHash": 600, "itemInstanceId": "4001", "lockable": true, "quantity": 1, "location": 2, "bucketHash": 138197802},
			{"itemHash": 601, "itemInstanceId": "4002", "lockable": true, "quantity": 1, "location": 2, "bucketHash": 138197802},
			{"itemHash": 602, "itemInstanceId": "4003", "lockable": true, "quantity": 1, "location": 2, "bucketHash": 138197802},
			{"itemHash": 601, "itemInstanceId": "4004", "lockable": true, "quantity": 1, "location": 2, "bucketHash": 138197802},
			{"itemHash": 602, "itemInstanceId": "4005", "lockable": true, "quantity": 1, "location": 2, "bucketHash": 138197802},
			{"itemHash": 602, "itemInstanceId": "4006", "lockable": true, "quantity": 1, "location": 2, "bucketHash": 138197802},
			{"itemHash": 602, "itemInstanceId": "4007", "lockable": true, "quantity": 1, "location": 2, "bucketHash": 138197802}
		]}},
		"itemComponents": {
			"instances": {"data": {
				"4001": {"damageType": 1, "primaryStat": {"statHash": 1480404414, "value": 2000}},
				"4002": {"damageType": 1, "primaryStat": {"statHash": 1480404414, "value": 2000}},
				"4003": {"damageType": 1, "primaryStat": {"statHash": 1480404414, "value": 2000}},
				"4004": {"damageType": 1, "primaryStat": {"statHash": 1480404414, "value": 2000}},
				"4005": {"damageType": 1, "primaryStat": {"statHash": 1480404414, "value": 2000}},
				"4006": {"damageType": 1, "primaryStat": {"statHash": 1480404414, "value": 2000}},
				"4007": {"damageType": 1, "primaryStat": {"statHash": 1480404414, "value": 2000}}
			}},
			"stats": {"data": {
				"4001": {"stats": {
					"4188031367": {"statHash": 4188031367, "value": 37},
					"4284893193": {"statHash": 4284893193, "value": 900},
					"3871231066": {"statHash": 3871231066, "value": 30}
				}},
				"4002": {"stats": {
					"4188031367": {"statHash": 4188031367, "value": 50},
					"4284893193": {"statHash": 4284893193, "value": 140},
					"3871231066": {"statHash": 3871231066, "value": 12}
				}},
				"4003": {"stats": {
					"4188031367": {"statHash": 4188031367, "value": 40},
					"4284893193": {"statHash": 4284893193, "value": 400},
					"3871231066": {"statHash": 3871231066, "value": 30}
				}},
				"4004": {"stats": {
					"4284893193": {"statHash": 4284893193, "value": 140},
					"3871231066": {"statHash": 3871231066, "value": 12}
				}},
				"4005": {"stats": {
					"4188031367": {"statHash": 4188031367, "value": 40},
					"4284893193": {"statHash": 4284893193, "value": 360},
					"3871231066": {"statHash": 3871231066, "value": 30}
				}},
				"4006": {"stats": {
					"4284893193": {"statHash": 4284893193, "value": 400},
					"3871231066": {"statHash": 3871231066, "value": 30}
				}},
				"4007": {"stats": {
					"4188031367": {"statHash": 4188031367, "value": 30},
					"4284893193": {"statHash": 4284893193, "value": 600},
					"3871231066": {"statHash": 3871231066, "value": 30}
				}}
			}},
			"sockets": {"data": {
				"4001": {"sockets": [
					{"plugHash": 1458010786, "isEnabled": true, "isVisible": true},
					{"plugHash": 620, "isEnabled": true, "isVisible": true},
					{"plugHash": 621, "isEnabled": true, "isVisible": true},
					{"plugHash": 622, "isEnabled": true, "isVisible": true},
					{"plugHash": 623, "isEnabled": true, "isVisible": true}
				]},
				"4002": {"sockets": [
					{"plugHash": 1294026524, "isEnabled": true, "isVisible": true},
					{"plugHash": 624, "isEnabled": true, "isVisible": true},
					{"plugHash": 625, "isEnabled": true, "isVisible": true},
					{"plugHash": 626, "isEnabled": true, "isVisible": true}
				]},
				"4003": {"sockets": [{"plugHash": 1019291327, "isEnabled": true, "isVisible": true}]},
				"4004": {"sockets": [{"plugHash": 1294026524, "isEnabled": true, "isVisible": true}]},
				"4005": {"sockets": [{"plugHash": 1019291327, "isEnabled": true, "isVisible": true}]},
				"4006": {"sockets": [{"plugHash": 1019291327, "isEnabled": true, "isVisible": true}]},
				"4007": {"sockets": [
					{"plugHash": 1294026524, "isEnabled": true, "isVisible": true},
					{"plugHash": 627, "isEnabled": true, "isVisible": true}
				]}
			}}
		}
	}}`

	return f
}

// Lines that the figure tests share.
const (
	figuresHeader = "Time to kill a Guardian with 230 health, and damage per second while the copy fires one " +
		"magazine and reloads, from community measurements and Bungie's patch notes at Update 9.7.0:"

	noPrecisionDamage = "Update 9.7.0 lowered the base damage of High-Impact auto rifles and does not state their " +
		"precision damage, and no measurement is newer."
)

// TestGetItemFigures pins the block of the time to kill and the damage
// per second that ends the get_item output of a weapon.
//
// The submachine gun deals 20.265 with a precision hit and 10.6 with a
// body shot, and it fires a round every 2 thirtieths of a second. Twelve
// hits kill, and one of them can be a body shot. Its magazine deals
// 607.95 with precision hits in two seconds, and its reload takes 2.003
// seconds. Target Lock raises the damage of each hit after the fourth
// one, so eleven hits kill.
//
// The hand cannon deals 79.845 with a precision hit and 44.504 with a
// body shot, and it fires a round every 13 thirtieths of a second. Its
// magazine deals 958.14 with precision hits in 5.2 seconds, and its
// reload takes 2.561 seconds.
//
// The auto rifle with Frenzy deals 813 with the precision hits of its
// magazine in three seconds. Its reload takes 2.158 seconds at its Reload
// Speed stat of 30. Frenzy scales the damage by 1.15 and raises the stat
// to its cap of 100. The reload then takes 1.242 seconds.
func TestGetItemFigures(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		id   string
		want []string
	}{
		"base numbers, a perk with numbers, and a perk without them": {
			id: "4001",
			want: []string{
				figuresHeader,
				"- Base:",
				"  Time to kill: 0.73 s with 11 precision hits and 1 body shot, 1.40 s with 22 body shots",
				"  Damage per second: 152 with precision hits, 79 with body shots",
				"- With Target Lock:",
				"  Time to kill: 0.67 s with 10 precision hits and 1 body shot, 1.27 s with 20 body shots",
				"  Damage per second: 176 with precision hits, 92 with body shots",
				"- Perks that the numbers leave out: Kinetic Tremors",
			},
		},
		"each state of each perk in the order of the perks": {
			id: "4002",
			want: []string{
				figuresHeader,
				"- Base:",
				"  Time to kill: 0.87 s with 3 precision hits, 2.17 s with 6 body shots",
				"  Damage per second: 123 with precision hits, 69 with body shots",
				"- With Kill Clip:",
				"  Time to kill: 0.87 s with 2 precision hits and 1 body shot, 1.73 s with 5 body shots",
				"  Damage per second: 154 with precision hits, 86 with body shots",
				"- With Rampage at 1 stack:",
				"  Time to kill: 0.87 s with 3 precision hits, 1.73 s with 5 body shots",
				"  Damage per second: 136 with precision hits, 76 with body shots",
				"- With Rampage at 3 stacks:",
				"  Time to kill: 0.87 s with 2 precision hits and 1 body shot, 1.30 s with 4 body shots",
				"  Damage per second: 164 with precision hits, 92 with body shots",
				"- Perks that the numbers leave out: Outlaw",
			},
		},
		"frame without precision damage gives the reason one time": {
			id: "4003",
			want: []string{
				figuresHeader,
				"- Base:",
				"  Time to kill: 1.50 s with 11 body shots",
				"  Damage per second: 102 with body shots",
				"  Time to kill and damage per second with precision hits: unknown. " + noPrecisionDamage,
			},
		},
		"copy without a reload time has no damage per second": {
			id: "4004",
			want: []string{
				figuresHeader,
				"- Base:",
				"  Time to kill: 0.87 s with 3 precision hits, 2.17 s with 6 body shots",
				"  Damage per second: unknown. " + noReloadStat,
			},
		},
		"perk that raises the Reload Speed stat": {
			id: "4007",
			want: []string{
				figuresHeader,
				"- Base:",
				"  Time to kill: 0.80 s with 8 precision hits and 1 body shot, 1.40 s with 15 body shots",
				"  Damage per second: 158 with precision hits, 90 with body shots",
				"- With Frenzy:",
				"  Time to kill: 0.70 s with 7 precision hits and 1 body shot, 1.20 s with 13 body shots",
				"  Damage per second: 220 with precision hits, 126 with body shots",
			},
		},
		"rate of fire that the data does not give the frame": {
			id: "4005",
			want: []string{
				allFiguresUnknown + " This copy fires 360 rounds per minute, " +
					"and the community data has no damage for its frame at that rate.",
			},
		},
		"each reason names its values": {
			id: "4006",
			want: []string{
				figuresHeader,
				"- Base:",
				"  Time to kill: 1.50 s with 11 body shots",
				"  Time to kill and damage per second with precision hits: unknown. " + noPrecisionDamage,
				"  Damage per second with body shots: unknown. " + noReloadStat,
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, isError := call(t, figureFakes(t).h.handleGetItem, GetItemInput{InstanceID: tc.id})
			assert.False(t, isError)

			// The block follows the last blank line of the output.
			_, block, found := cutLast(got, "\n\n")
			require.True(t, found)
			assert.Equal(t, strings.Join(tc.want, "\n"), block)
		})
	}
}

// cutLast cuts s around the last instance of sep.
func cutLast(s, sep string) (string, string, bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return s, "", false
	}

	return s[:i], s[i+len(sep):], true
}

// TestWriteFigureLines pins the lines of one state. A line names the
// values that are unknown for one reason, and it counts one hit in the
// singular.
func TestWriteFigureLines(t *testing.T) {
	t.Parallel()

	const (
		first  = "First reason."
		second = "Second reason."
	)

	known := damage.Kill{Seconds: 1.5, Precision: 1, Body: 1}
	body := damage.Kill{Seconds: 2, Body: 4}

	tests := map[string]struct {
		figures damage.Figures
		want    []string
	}{
		"every value": {
			figures: damage.Figures{
				OptimalKill:   known,
				BodyKill:      body,
				PrecisionRate: damage.Rate{Value: 150.4},
				BodyRate:      damage.Rate{Value: 99.5},
			},
			want: []string{
				"- Base:",
				"  Time to kill: 1.50 s with 1 precision hit and 1 body shot, 2.00 s with 4 body shots",
				"  Damage per second: 150 with precision hits, 100 with body shots",
			},
		},
		"kill with one precision hit": {
			figures: damage.Figures{
				OptimalKill:   damage.Kill{Precision: 1},
				BodyKill:      damage.Kill{Body: 1},
				PrecisionRate: damage.Rate{Value: 150},
				BodyRate:      damage.Rate{Value: 100},
			},
			want: []string{
				"- Base:",
				"  Time to kill: 0.00 s with 1 precision hit, 0.00 s with 1 body shot",
				"  Damage per second: 150 with precision hits, 100 with body shots",
			},
		},
		"both times to kill unknown for one reason": {
			figures: damage.Figures{
				OptimalKill:   damage.Kill{Reason: first},
				BodyKill:      damage.Kill{Reason: first},
				PrecisionRate: damage.Rate{Value: 150},
				BodyRate:      damage.Rate{Value: 100},
			},
			want: []string{
				"- Base:",
				"  Damage per second: 150 with precision hits, 100 with body shots",
				"  Time to kill: unknown. " + first,
			},
		},
		"every value unknown for one reason": {
			figures: damage.Figures{
				OptimalKill:   damage.Kill{Reason: first},
				BodyKill:      damage.Kill{Reason: first},
				PrecisionRate: damage.Rate{Reason: first},
				BodyRate:      damage.Rate{Reason: first},
			},
			want: []string{
				"- Base:",
				"  Time to kill and damage per second: unknown. " + first,
			},
		},
		"one time to kill and both rates unknown for one reason": {
			figures: damage.Figures{
				OptimalKill:   known,
				BodyKill:      damage.Kill{Reason: first},
				PrecisionRate: damage.Rate{Reason: first},
				BodyRate:      damage.Rate{Reason: first},
			},
			want: []string{
				"- Base:",
				"  Time to kill: 1.50 s with 1 precision hit and 1 body shot",
				"  Time to kill with body shots and damage per second: unknown. " + first,
			},
		},
		"values of one number unknown for two reasons": {
			figures: damage.Figures{
				OptimalKill:   damage.Kill{Reason: first},
				BodyKill:      damage.Kill{Reason: second},
				PrecisionRate: damage.Rate{Value: 150},
				BodyRate:      damage.Rate{Value: 100},
			},
			want: []string{
				"- Base:",
				"  Damage per second: 150 with precision hits, 100 with body shots",
				"  Time to kill with precision hits: unknown. " + first,
				"  Time to kill with body shots: unknown. " + second,
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var b strings.Builder

			writeFigureLines(&b, "Base", &tc.figures)

			assert.Equal(t, strings.Join(tc.want, "\n")+"\n", b.String())
		})
	}
}
