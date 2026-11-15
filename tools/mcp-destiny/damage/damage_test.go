package damage_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/curves"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/damage"
)

// Reasons that the damage data and the package give for a number that
// they do not have.
const (
	noType      = "The community data has no damage for this weapon type."
	noFrame     = "The community data has no damage for the frame of this weapon."
	noPlugs     = "The profile lists no perks for this copy, so the server cannot tell its frame."
	noIntrinsic = "The profile lists no frame among the perks of this copy."
	noRate      = "This copy has no Rounds Per Minute stat."
	noMagazine  = "This copy has no Magazine stat."
	noReload    = "This copy has no Reload Speed stat."
	noReloadFn  = "The server has no reload time for this copy."
	noBonus     = "A copy of the tests has no reload time at this bonus."

	noPrecision = "Update 9.7.0 lowered the base damage of High-Impact auto rifles and does not state their " +
		"precision damage, and no measurement is newer."
)

// reloads maps a bonus to the Reload Speed stat of each copy of the
// tests to the reload time of the copy in seconds. A reload takes two
// seconds at the stat of the copy and 1.2 seconds at a stat 100 points
// higher.
var reloads = map[int]float64{0: 2.0, 100: 1.2}

// reloadAt returns the reload time of a copy of the tests with its Reload
// Speed stat raised by bonus. The time is unknown at a bonus that
// [reloads] lacks, so a test sees a bonus that the package must not ask
// for.
func reloadAt(bonus int) curves.Measure {
	seconds, ok := reloads[bonus]
	if !ok {
		return curves.Measure{Reason: noBonus}
	}

	return curves.Measure{Value: seconds}
}

// figures holds the numbers that a test expects of one state. A
// non-empty noPrecisionRate is the reason of an unknown damage per second
// with precision hits.
type figures struct {
	state           string
	noPrecisionRate string
	optimal         damage.Kill
	body            damage.Kill
	precisionRate   float64
	bodyRate        float64
}

// kill returns a known kill.
func kill(seconds float64, precision, body int) damage.Kill {
	return damage.Kill{Seconds: seconds, Precision: precision, Body: body}
}

// weapon returns a copy with a frame, the Rounds Per Minute stat, the
// Magazine stat, and the reload times of [reloadAt]. Plugs holds the
// plugs that follow the frame.
func weapon(typeName, frame string, rate, magazine int, plugs ...damage.Plug) damage.Weapon {
	return damage.Weapon{
		Type:   typeName,
		Plugs:  append([]damage.Plug{{Name: frame, Type: "Intrinsic"}}, plugs...),
		Stats:  map[uint32]int{damage.StatRoundsPerMinute: rate, damage.StatMagazine: magazine},
		Reload: reloadAt,
	}
}

// assertFigures compares the numbers of one state with the expected
// numbers. Times agree to a ten-thousandth of a second, and each damage
// per second agrees to a hundredth.
func assertFigures(t *testing.T, want figures, got damage.Figures) {
	t.Helper()

	assert.Equal(t, want.state, got.State)
	assertKill(t, want.optimal, got.OptimalKill)
	assertKill(t, want.body, got.BodyKill)

	if want.noPrecisionRate != "" {
		assert.Equal(t, damage.Rate{Reason: want.noPrecisionRate}, got.PrecisionRate)
	} else {
		assert.Empty(t, got.PrecisionRate.Reason)
		assert.InDelta(t, want.precisionRate, got.PrecisionRate.Value, 0.006)
	}

	assert.Empty(t, got.BodyRate.Reason)
	assert.InDelta(t, want.bodyRate, got.BodyRate.Value, 0.006)
}

// assertKill compares two kills. The times agree to a ten-thousandth of
// a second.
func assertKill(t *testing.T, want, got damage.Kill) {
	t.Helper()

	assert.Equal(t, want.Reason, got.Reason)
	assert.Equal(t, want.Precision, got.Precision, "precision hits")
	assert.Equal(t, want.Body, got.Body, "body shots")
	assert.InDelta(t, want.Seconds, got.Seconds, 0.0001)
}

// TestOfFrames pins the base numbers of every frame of the damage data.
// A Guardian has 230 health. The magazine of a copy holds 30 rounds on
// an auto rifle and a submachine gun, 12 on a hand cannon, 36 on a pulse
// rifle, 16 on a scout rifle, and 18 on a sidearm. A reload takes two
// seconds.
//
// Each expected time is the sum of the delays between the hits of the
// kill. A hand cannon with the Adaptive Frame deals 79.845 with a
// precision hit, so three hits kill. The two delays of 13 thirtieths of
// a second take 0.8667 seconds. Its 12 precision hits deal 958.14. The
// 12 delays and the reload take 7.2 seconds, so its damage per second
// is 133.08. The Rapid-Fire Frame reloads an empty magazine in 0.8 of
// the time, so its damage per second counts a reload of 1.6 seconds.
func TestOfFrames(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		weapon          string
		frame           string
		noPrecisionRate string
		optimal         damage.Kill
		body            damage.Kill
		rate            int
		magazine        int
		precisionRate   float64
		bodyRate        float64
	}{
		"auto rifle, Rapid-Fire Frame": {
			weapon: "Auto Rifle", frame: "Rapid-Fire Frame", rate: 720, magazine: 30,
			optimal: kill(0.7500, 10, 0), precisionRate: 168.75, body: kill(1.3333, 0, 17), bodyRate: 99.49,
		},
		"auto rifle, Lightweight Frame": {
			weapon: "Auto Rifle", frame: "Lightweight Frame", rate: 720, magazine: 30,
			optimal: kill(0.7500, 10, 0), precisionRate: 153.75, body: kill(1.3333, 0, 17), bodyRate: 90.65,
		},
		"auto rifle, Adaptive Frame": {
			weapon: "Auto Rifle", frame: "Adaptive Frame", rate: 600, magazine: 30,
			optimal: kill(0.8000, 8, 1), precisionRate: 162.60, body: kill(1.4000, 0, 15), bodyRate: 93.00,
		},
		"auto rifle, High-Impact Frame": {
			weapon: "Auto Rifle", frame: "High-Impact Frame", rate: 400, magazine: 30,
			optimal: damage.Kill{Reason: noPrecision}, noPrecisionRate: noPrecision,
			body: kill(1.5000, 0, 11), bodyRate: 101.54,
		},
		"auto rifle, Precision Frame": {
			weapon: "Auto Rifle", frame: "Precision Frame", rate: 450, magazine: 30,
			optimal: kill(0.8000, 7, 0), precisionRate: 170.00, body: kill(1.4667, 0, 12), bodyRate: 100.00,
		},
		"hand cannon, Adaptive Frame": {
			weapon: "Hand Cannon", frame: "Adaptive Frame", rate: 140, magazine: 12,
			optimal: kill(0.8667, 3, 0), precisionRate: 133.07, body: kill(2.1667, 0, 6), bodyRate: 74.17,
		},
		"hand cannon, Lightweight Frame": {
			weapon: "Hand Cannon", frame: "Lightweight Frame", rate: 140, magazine: 12,
			optimal: kill(0.8667, 3, 0), precisionRate: 133.07, body: kill(2.1667, 0, 6), bodyRate: 74.17,
		},
		"hand cannon, Aggressive Frame": {
			weapon: "Hand Cannon", frame: "Aggressive Frame", rate: 120, magazine: 12,
			optimal: kill(1.0000, 3, 0), precisionRate: 135.15, body: kill(2.0000, 0, 5), bodyRate: 73.05,
		},
		"hand cannon, Precision Frame": {
			weapon: "Hand Cannon", frame: "Precision Frame", rate: 180, magazine: 12,
			optimal: kill(1.0000, 2, 2), precisionRate: 141.22, body: kill(1.6667, 0, 6), bodyRate: 90.59,
		},
		"hand cannon, Heavy Burst": {
			weapon: "Hand Cannon", frame: "Heavy Burst", rate: 260, magazine: 12,
			optimal: kill(0.9333, 4, 1), precisionRate: 132.32, body: kill(1.9667, 0, 10), bodyRate: 59.99,
		},
		"pulse rifle, Adaptive Frame": {
			weapon: "Pulse Rifle", frame: "Adaptive Frame", rate: 390, magazine: 36,
			optimal: kill(0.9333, 6, 1), precisionRate: 172.63, body: kill(1.4667, 0, 11), bodyRate: 104.19,
		},
		"pulse rifle, Rapid-Fire Frame": {
			weapon: "Pulse Rifle", frame: "Rapid-Fire Frame", rate: 540, magazine: 36,
			optimal: kill(0.8000, 8, 1), precisionRate: 179.36, body: kill(1.4667, 0, 15), bodyRate: 99.64,
		},
		"pulse rifle, High-Impact Frame": {
			weapon: "Pulse Rifle", frame: "High-Impact Frame", rate: 340, magazine: 36,
			optimal: kill(0.6667, 6, 0), precisionRate: 165.28, body: kill(1.6667, 0, 11), bodyRate: 92.13,
		},
		"pulse rifle, Lightweight Frame": {
			weapon: "Pulse Rifle", frame: "Lightweight Frame", rate: 450, magazine: 36,
			optimal: kill(0.8667, 8, 0), precisionRate: 159.35, body: kill(1.6000, 0, 13), bodyRate: 99.53,
		},
		"pulse rifle, Legacy PR-55 Frame": {
			weapon: "Pulse Rifle", frame: "Legacy PR-55 Frame", rate: 450, magazine: 36,
			optimal: kill(0.8667, 8, 0), precisionRate: 159.35, body: kill(1.6000, 0, 13), bodyRate: 99.53,
		},
		"pulse rifle, Aggressive Burst": {
			weapon: "Pulse Rifle", frame: "Aggressive Burst", rate: 450, magazine: 36,
			optimal: kill(0.7333, 8, 0), precisionRate: 160.04, body: kill(1.7333, 0, 15), bodyRate: 82.03,
		},
		"pulse rifle, Heavy Burst": {
			weapon: "Pulse Rifle", frame: "Heavy Burst", rate: 325, magazine: 36,
			optimal: kill(0.8000, 5, 1), precisionRate: 178.37, body: kill(1.8333, 0, 11), bodyRate: 96.26,
		},
		"scout rifle, Lightweight Frame": {
			weapon: "Scout Rifle", frame: "Lightweight Frame", rate: 200, magazine: 16,
			optimal: kill(0.9000, 3, 1), precisionRate: 151.67, body: kill(1.8000, 0, 7), bodyRate: 89.43,
		},
		"scout rifle, Precision Frame": {
			weapon: "Scout Rifle", frame: "Precision Frame", rate: 180, magazine: 16,
			optimal: kill(1.0000, 3, 1), precisionRate: 150.73, body: kill(1.6667, 0, 6), bodyRate: 84.01,
		},
		"scout rifle, High-Impact Frame": {
			weapon: "Scout Rifle", frame: "High-Impact Frame", rate: 150, magazine: 16,
			optimal: kill(0.8000, 3, 0), precisionRate: 160.79, body: kill(2.0000, 0, 6), bodyRate: 80.00,
		},
		"scout rifle, Rapid-Fire Frame": {
			weapon: "Scout Rifle", frame: "Rapid-Fire Frame", rate: 260, magazine: 16,
			optimal: kill(0.9333, 4, 1), precisionRate: 159.75, body: kill(1.8667, 0, 9), bodyRate: 81.90,
		},
		"scout rifle, Aggressive Frame": {
			weapon: "Scout Rifle", frame: "Aggressive Frame", rate: 120, magazine: 16,
			optimal: kill(1.0000, 2, 1), precisionRate: 146.53, body: kill(2.0000, 0, 5), bodyRate: 86.39,
		},
		"sidearm, Adaptive Frame at 300 rounds per minute": {
			weapon: "Sidearm", frame: "Adaptive Frame", rate: 300, magazine: 18,
			optimal: kill(0.6000, 4, 0), precisionRate: 185.34, body: kill(1.2000, 0, 7), bodyRate: 115.98,
		},
		"sidearm, Together Forever": {
			weapon: "Sidearm", frame: "Together Forever", rate: 300, magazine: 18,
			optimal: kill(0.6000, 4, 0), precisionRate: 185.34, body: kill(1.2000, 0, 7), bodyRate: 115.98,
		},
		"sidearm, Adaptive Burst": {
			weapon: "Sidearm", frame: "Adaptive Burst", rate: 491, magazine: 18,
			optimal: kill(0.7333, 6, 1), precisionRate: 156.17, body: kill(1.2333, 0, 12), bodyRate: 86.10,
		},
		"sidearm, Adaptive Frame at 491 rounds per minute": {
			weapon: "Sidearm", frame: "Adaptive Frame", rate: 491, magazine: 18,
			optimal: kill(0.7333, 6, 1), precisionRate: 156.17, body: kill(1.2333, 0, 12), bodyRate: 86.10,
		},
		"sidearm, Lightweight Frame": {
			weapon: "Sidearm", frame: "Lightweight Frame", rate: 360, magazine: 18,
			optimal: kill(0.6667, 5, 0), precisionRate: 178.56, body: kill(1.1667, 0, 8), bodyRate: 111.74,
		},
		"sidearm, Rapid-Fire Frame": {
			weapon: "Sidearm", frame: "Rapid-Fire Frame", rate: 450, magazine: 18,
			optimal: kill(0.6667, 6, 0), precisionRate: 178.38, body: kill(1.2000, 0, 10), bodyRate: 113.01,
		},
		"sidearm, Precision Frame": {
			weapon: "Sidearm", frame: "Precision Frame", rate: 260, magazine: 18,
			optimal: kill(0.7000, 3, 1), precisionRate: 186.29, body: kill(1.1667, 0, 6), bodyRate: 116.58,
		},
		"sidearm, Heavy Burst": {
			weapon: "Sidearm", frame: "Heavy Burst", rate: 325, magazine: 18,
			optimal: kill(0.7333, 5, 0), precisionRate: 168.45, body: kill(1.2000, 0, 8), bodyRate: 105.28,
		},
		"submachine gun, Adaptive Frame": {
			weapon: "Submachine Gun", frame: "Adaptive Frame", rate: 900, magazine: 30,
			optimal: kill(0.7333, 12, 0), precisionRate: 147.95, body: kill(1.3333, 0, 21), bodyRate: 86.23,
		},
		"submachine gun, Lightweight Frame": {
			weapon: "Submachine Gun", frame: "Lightweight Frame", rate: 900, magazine: 30,
			optimal: kill(0.7333, 11, 1), precisionRate: 151.99, body: kill(1.4000, 0, 22), bodyRate: 79.50,
		},
		"submachine gun, MIDA Synergy": {
			weapon: "Submachine Gun", frame: "MIDA Synergy", rate: 900, magazine: 30,
			optimal: kill(0.7333, 11, 1), precisionRate: 151.99, body: kill(1.4000, 0, 22), bodyRate: 79.50,
		},
		"submachine gun, Aggressive Frame": {
			weapon: "Submachine Gun", frame: "Aggressive Frame", rate: 750, magazine: 30,
			optimal: kill(0.7500, 10, 0), precisionRate: 159.16, body: kill(1.3333, 0, 17), bodyRate: 90.69,
		},
		"submachine gun, Precision Frame": {
			weapon: "Submachine Gun", frame: "Precision Frame", rate: 600, magazine: 30,
			optimal: kill(0.8000, 9, 0), precisionRate: 158.69, body: kill(1.3000, 0, 14), bodyRate: 99.31,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for _, plugType := range []string{"Intrinsic", "Enhanced Intrinsic"} {
				w := weapon(tt.weapon, tt.frame, tt.rate, tt.magazine)
				w.Plugs[0].Type = plugType

				got := damage.Of(w)

				require.Empty(t, got.Reason, plugType)
				assert.Equal(t, "9.7.0", got.Patch)
				assert.InDelta(t, 230.0, got.Health, 0)
				assert.Empty(t, got.Perks)
				assert.Empty(t, got.Unmodeled)
				assert.Empty(t, got.Base.Perk)

				assertFigures(t, figures{
					optimal:         tt.optimal,
					body:            tt.body,
					precisionRate:   tt.precisionRate,
					bodyRate:        tt.bodyRate,
					noPrecisionRate: tt.noPrecisionRate,
				}, got.Base)
			}
		})
	}
}

// TestOfPerks pins the numbers of every state of every perk of the
// damage data, for the base plug and for the enhanced plug. The copies
// are the copies of [TestOfFrames].
//
// A perk scales the damage of each hit. Kill Clip scales it by 1.25, so
// the precision hit of a hand cannon with the Adaptive Frame deals
// 99.81 and its body shot deals 55.63. Two precision hits and one body
// shot then kill, and five body shots kill. Adagio also scales each
// delay by 1.2. Frenzy also raises the Reload Speed stat by 100. The
// damage per second of an auto rifle with the Adaptive Frame then counts
// a reload of 1.2 seconds. Target Lock scales each hit by the share of the
// magazine that hit before it. The eleventh hit of a submachine gun with
// the Lightweight Frame and 30 rounds then kills.
func TestOfPerks(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		weapon   string
		frame    string
		perk     string
		plugType string
		want     []figures
		rate     int
		magazine int
	}{
		"kill clip on a hand cannon": {
			weapon: "Hand Cannon", frame: "Adaptive Frame", rate: 140, magazine: 12,
			perk: "Kill Clip", plugType: "Trait",
			want: []figures{
				{
					state:         "",
					optimal:       kill(0.8667, 2, 1),
					precisionRate: 166.34,
					body:          kill(1.7333, 0, 5),
					bodyRate:      92.72,
				},
			},
		},
		"kill clip on a hand cannon, enhanced": {
			weapon: "Hand Cannon", frame: "Adaptive Frame", rate: 140, magazine: 12,
			perk: "Kill Clip", plugType: "Enhanced Trait",
			want: []figures{
				{
					state:         "",
					optimal:       kill(0.8667, 2, 1),
					precisionRate: 166.34,
					body:          kill(1.7333, 0, 5),
					bodyRate:      92.72,
				},
			},
		},
		"rampage on a hand cannon": {
			weapon: "Hand Cannon", frame: "Adaptive Frame", rate: 140, magazine: 12,
			perk: "Rampage", plugType: "Trait",
			want: []figures{
				{
					state:         "at 1 stack",
					optimal:       kill(0.8667, 3, 0),
					precisionRate: 146.38,
					body:          kill(1.7333, 0, 5),
					bodyRate:      81.59,
				},
				{
					state:         "at 3 stacks",
					optimal:       kill(0.8667, 2, 1),
					precisionRate: 177.12,
					body:          kill(1.3000, 0, 4),
					bodyRate:      98.72,
				},
			},
		},
		"rampage on a hand cannon, enhanced": {
			weapon: "Hand Cannon", frame: "Adaptive Frame", rate: 140, magazine: 12,
			perk: "Rampage", plugType: "Enhanced Trait",
			want: []figures{
				{
					state:         "at 1 stack",
					optimal:       kill(0.8667, 3, 0),
					precisionRate: 146.38,
					body:          kill(1.7333, 0, 5),
					bodyRate:      81.59,
				},
				{
					state:         "at 3 stacks",
					optimal:       kill(0.8667, 2, 1),
					precisionRate: 177.12,
					body:          kill(1.3000, 0, 4),
					bodyRate:      98.72,
				},
			},
		},
		"multikill clip on a pulse rifle": {
			weapon: "Pulse Rifle", frame: "High-Impact Frame", rate: 340, magazine: 36,
			perk: "Multikill Clip", plugType: "Trait",
			want: []figures{
				{
					state:         "after 1 final blow",
					optimal:       kill(0.6667, 4, 2),
					precisionRate: 193.38,
					body:          kill(1.6000, 0, 10),
					bodyRate:      107.79,
				},
				{
					state:         "after 3 final blows",
					optimal:       kill(0.5333, 4, 0),
					precisionRate: 247.92,
					body:          kill(1.1333, 0, 8),
					bodyRate:      138.19,
				},
			},
		},
		"multikill clip on a pulse rifle, enhanced": {
			weapon: "Pulse Rifle", frame: "High-Impact Frame", rate: 340, magazine: 36,
			perk: "Multikill Clip", plugType: "Enhanced Trait",
			want: []figures{
				{
					state:         "after 1 final blow",
					optimal:       kill(0.6667, 4, 2),
					precisionRate: 193.38,
					body:          kill(1.6000, 0, 10),
					bodyRate:      107.79,
				},
				{
					state:         "after 3 final blows",
					optimal:       kill(0.5333, 4, 0),
					precisionRate: 247.92,
					body:          kill(1.1333, 0, 8),
					bodyRate:      138.19,
				},
			},
		},
		"swashbuckler on a submachine gun": {
			weapon: "Submachine Gun", frame: "Aggressive Frame", rate: 750, magazine: 30,
			perk: "Swashbuckler", plugType: "Trait",
			want: []figures{
				{
					state:         "at 1 stack",
					optimal:       kill(0.7500, 8, 2),
					precisionRate: 169.82,
					body:          kill(1.2500, 0, 16),
					bodyRate:      96.77,
				},
				{
					state:         "at 5 stacks",
					optimal:       kill(0.5833, 7, 1),
					precisionRate: 212.16,
					body:          kill(1.0000, 0, 13),
					bodyRate:      120.89,
				},
			},
		},
		"swashbuckler on a submachine gun, enhanced": {
			weapon: "Submachine Gun", frame: "Aggressive Frame", rate: 750, magazine: 30,
			perk: "Swashbuckler", plugType: "Enhanced Trait",
			want: []figures{
				{
					state:         "at 1 stack",
					optimal:       kill(0.7500, 8, 2),
					precisionRate: 169.82,
					body:          kill(1.2500, 0, 16),
					bodyRate:      96.77,
				},
				{
					state:         "at 5 stacks",
					optimal:       kill(0.5833, 7, 1),
					precisionRate: 212.16,
					body:          kill(1.0000, 0, 13),
					bodyRate:      120.89,
				},
			},
		},
		"adrenaline junkie on a auto rifle": {
			weapon: "Auto Rifle", frame: "Precision Frame", rate: 450, magazine: 30,
			perk: "Adrenaline Junkie", plugType: "Trait",
			want: []figures{
				{
					state:         "at 1 stack",
					optimal:       kill(0.8000, 6, 1),
					precisionRate: 181.39,
					body:          kill(1.3333, 0, 11),
					bodyRate:      106.70,
				},
				{
					state:         "at 5 stacks",
					optimal:       kill(0.6667, 4, 2),
					precisionRate: 226.61,
					body:          kill(1.0667, 0, 9),
					bodyRate:      133.30,
				},
			},
		},
		"adrenaline junkie on a auto rifle, enhanced": {
			weapon: "Auto Rifle", frame: "Precision Frame", rate: 450, magazine: 30,
			perk: "Adrenaline Junkie", plugType: "Enhanced Trait",
			want: []figures{
				{
					state:         "at 1 stack",
					optimal:       kill(0.8000, 6, 1),
					precisionRate: 181.39,
					body:          kill(1.3333, 0, 11),
					bodyRate:      106.70,
				},
				{
					state:         "at 5 stacks",
					optimal:       kill(0.6667, 4, 2),
					precisionRate: 226.61,
					body:          kill(1.0667, 0, 9),
					bodyRate:      133.30,
				},
			},
		},
		"frenzy on a auto rifle": {
			weapon: "Auto Rifle", frame: "Adaptive Frame", rate: 600, magazine: 30,
			perk: "Frenzy", plugType: "Trait",
			want: []figures{
				{
					state:         "",
					optimal:       kill(0.7000, 7, 1),
					precisionRate: 222.61,
					body:          kill(1.2000, 0, 13),
					bodyRate:      127.32,
				},
			},
		},
		"frenzy on a auto rifle, enhanced": {
			weapon: "Auto Rifle", frame: "Adaptive Frame", rate: 600, magazine: 30,
			perk: "Frenzy", plugType: "Enhanced Trait",
			want: []figures{
				{
					state:         "",
					optimal:       kill(0.7000, 7, 1),
					precisionRate: 222.61,
					body:          kill(1.2000, 0, 13),
					bodyRate:      127.32,
				},
			},
		},
		"one for all on a scout rifle": {
			weapon: "Scout Rifle", frame: "High-Impact Frame", rate: 150, magazine: 16,
			perk: "One for All", plugType: "Trait",
			want: []figures{
				{
					state:         "",
					optimal:       kill(0.8000, 2, 1),
					precisionRate: 217.06,
					body:          kill(1.6000, 0, 5),
					bodyRate:      108.00,
				},
			},
		},
		"one for all on a scout rifle, enhanced": {
			weapon: "Scout Rifle", frame: "High-Impact Frame", rate: 150, magazine: 16,
			perk: "One for All", plugType: "Enhanced Trait",
			want: []figures{
				{
					state:         "",
					optimal:       kill(0.8000, 2, 1),
					precisionRate: 217.06,
					body:          kill(1.6000, 0, 5),
					bodyRate:      108.00,
				},
			},
		},
		"desperate measures on a sidearm": {
			weapon: "Sidearm", frame: "Lightweight Frame", rate: 360, magazine: 18,
			perk: "Desperate Measures", plugType: "Trait",
			want: []figures{
				{
					state:         "at 1 stack",
					optimal:       kill(0.6667, 3, 2),
					precisionRate: 196.42,
					body:          kill(1.0000, 0, 7),
					bodyRate:      122.91,
				},
				{
					state:         "at 3 stacks",
					optimal:       kill(0.5000, 3, 1),
					precisionRate: 232.13,
					body:          kill(0.8333, 0, 6),
					bodyRate:      145.26,
				},
			},
		},
		"desperate measures on a sidearm, enhanced": {
			weapon: "Sidearm", frame: "Lightweight Frame", rate: 360, magazine: 18,
			perk: "Desperate Measures", plugType: "Enhanced Trait",
			want: []figures{
				{
					state:         "at 1 stack",
					optimal:       kill(0.6667, 3, 2),
					precisionRate: 196.42,
					body:          kill(1.0000, 0, 7),
					bodyRate:      122.91,
				},
				{
					state:         "at 3 stacks",
					optimal:       kill(0.5000, 3, 1),
					precisionRate: 232.13,
					body:          kill(0.8333, 0, 6),
					bodyRate:      145.26,
				},
			},
		},
		"harmony on a scout rifle": {
			weapon: "Scout Rifle", frame: "Precision Frame", rate: 180, magazine: 16,
			perk: "Harmony", plugType: "Trait",
			want: []figures{
				{
					state:         "",
					optimal:       kill(0.6667, 3, 0),
					precisionRate: 180.87,
					body:          kill(1.3333, 0, 5),
					bodyRate:      100.82,
				},
			},
		},
		"harmony on a scout rifle, enhanced": {
			weapon: "Scout Rifle", frame: "Precision Frame", rate: 180, magazine: 16,
			perk: "Harmony", plugType: "Enhanced Trait",
			want: []figures{
				{
					state:         "",
					optimal:       kill(0.6667, 3, 0),
					precisionRate: 180.87,
					body:          kill(1.3333, 0, 5),
					bodyRate:      100.82,
				},
			},
		},
		"focused fury on a pulse rifle": {
			weapon: "Pulse Rifle", frame: "Adaptive Frame", rate: 390, magazine: 36,
			perk: "Focused Fury", plugType: "Trait",
			want: []figures{
				{
					state:         "",
					optimal:       kill(0.6000, 4, 2),
					precisionRate: 215.79,
					body:          kill(1.0667, 0, 9),
					bodyRate:      130.24,
				},
			},
		},
		"focused fury on a pulse rifle, enhanced": {
			weapon: "Pulse Rifle", frame: "Adaptive Frame", rate: 390, magazine: 36,
			perk: "Focused Fury", plugType: "Enhanced Trait",
			want: []figures{
				{
					state:         "",
					optimal:       kill(0.6000, 4, 2),
					precisionRate: 215.79,
					body:          kill(1.0667, 0, 9),
					bodyRate:      130.24,
				},
			},
		},
		"golden tricorn on a hand cannon": {
			weapon: "Hand Cannon", frame: "Aggressive Frame", rate: 120, magazine: 12,
			perk: "Golden Tricorn", plugType: "Trait",
			want: []figures{
				{
					state:         "at 1 stack",
					optimal:       kill(1.0000, 2, 1),
					precisionRate: 155.42,
					body:          kill(2.0000, 0, 5),
					bodyRate:      84.01,
				},
				{
					state:         "at 2 stacks",
					optimal:       kill(0.5000, 2, 0),
					precisionRate: 202.73,
					body:          kill(1.5000, 0, 4),
					bodyRate:      109.57,
				},
			},
		},
		"collective action on a auto rifle": {
			weapon: "Auto Rifle", frame: "Precision Frame", rate: 450, magazine: 30,
			perk: "Collective Action", plugType: "Trait",
			want: []figures{
				{
					state:         "",
					optimal:       kill(0.8000, 5, 2),
					precisionRate: 187.00,
					body:          kill(1.3333, 0, 11),
					bodyRate:      110.00,
				},
			},
		},
		"collective action on a auto rifle, enhanced": {
			weapon: "Auto Rifle", frame: "Precision Frame", rate: 450, magazine: 30,
			perk: "Collective Action", plugType: "Enhanced Trait",
			want: []figures{
				{
					state:         "",
					optimal:       kill(0.8000, 5, 2),
					precisionRate: 188.70,
					body:          kill(1.3333, 0, 11),
					bodyRate:      111.00,
				},
			},
		},
		"adagio on a scout rifle": {
			weapon: "Scout Rifle", frame: "Lightweight Frame", rate: 200, magazine: 16,
			perk: "Adagio", plugType: "Trait",
			want: []figures{
				{
					state:         "",
					optimal:       kill(0.7200, 3, 0),
					precisionRate: 172.78,
					body:          kill(1.4400, 0, 5),
					bodyRate:      101.87,
				},
			},
		},
		"adagio on a scout rifle, enhanced": {
			weapon: "Scout Rifle", frame: "Lightweight Frame", rate: 200, magazine: 16,
			perk: "Adagio", plugType: "Enhanced Trait",
			want: []figures{
				{
					state:         "",
					optimal:       kill(0.7200, 3, 0),
					precisionRate: 172.78,
					body:          kill(1.4400, 0, 5),
					bodyRate:      101.87,
				},
			},
		},
		"adagio on a hand cannon": {
			weapon: "Hand Cannon", frame: "Adaptive Frame", rate: 140, magazine: 12,
			perk: "Adagio", plugType: "Trait",
			want: []figures{
				{
					state:         "",
					optimal:       kill(1.0400, 2, 1),
					precisionRate: 147.67,
					body:          kill(2.0800, 0, 5),
					bodyRate:      82.31,
				},
			},
		},
		"adagio on a hand cannon, enhanced": {
			weapon: "Hand Cannon", frame: "Adaptive Frame", rate: 140, magazine: 12,
			perk: "Adagio", plugType: "Enhanced Trait",
			want: []figures{
				{
					state:         "",
					optimal:       kill(1.0400, 2, 1),
					precisionRate: 147.67,
					body:          kill(2.0800, 0, 5),
					bodyRate:      82.31,
				},
			},
		},
		"master of arms on a submachine gun": {
			weapon: "Submachine Gun", frame: "Precision Frame", rate: 600, magazine: 30,
			perk: "Master of Arms", plugType: "Trait",
			want: []figures{
				{
					state:         "at 1 stack",
					optimal:       kill(0.7000, 7, 1),
					precisionRate: 182.50,
					body:          kill(1.2000, 0, 13),
					bodyRate:      114.20,
				},
				{
					state:         "at 2 stacks",
					optimal:       kill(0.6000, 7, 0),
					precisionRate: 198.37,
					body:          kill(1.1000, 0, 12),
					bodyRate:      124.13,
				},
			},
		},
		"master of arms on a submachine gun, enhanced": {
			weapon: "Submachine Gun", frame: "Precision Frame", rate: 600, magazine: 30,
			perk: "Master of Arms", plugType: "Enhanced Trait",
			want: []figures{
				{
					state:         "at 1 stack",
					optimal:       kill(0.7000, 7, 1),
					precisionRate: 182.50,
					body:          kill(1.2000, 0, 13),
					bodyRate:      114.20,
				},
				{
					state:         "at 2 stacks",
					optimal:       kill(0.6000, 7, 0),
					precisionRate: 198.37,
					body:          kill(1.1000, 0, 12),
					bodyRate:      124.13,
				},
			},
		},
		"surrounded on a submachine gun": {
			weapon: "Submachine Gun", frame: "Adaptive Frame", rate: 900, magazine: 30,
			perk: "Surrounded", plugType: "Trait",
			want: []figures{
				{
					state:         "",
					optimal:       kill(0.5333, 8, 1),
					precisionRate: 207.13,
					body:          kill(0.9333, 0, 15),
					bodyRate:      120.73,
				},
			},
		},
		"surrounded on a submachine gun, enhanced": {
			weapon: "Submachine Gun", frame: "Adaptive Frame", rate: 900, magazine: 30,
			perk: "Surrounded", plugType: "Enhanced Trait",
			want: []figures{
				{
					state:         "",
					optimal:       kill(0.4667, 8, 0),
					precisionRate: 217.49,
					body:          kill(0.8667, 0, 14),
					bodyRate:      126.77,
				},
			},
		},
		"target lock on a submachine gun": {
			weapon: "Submachine Gun", frame: "Lightweight Frame", rate: 900, magazine: 30,
			perk: "Target Lock", plugType: "Trait",
			want: []figures{
				{
					state:         "",
					optimal:       kill(0.6667, 10, 1),
					precisionRate: 175.82,
					body:          kill(1.2667, 0, 20),
					bodyRate:      91.97,
				},
			},
		},
		"target lock on a submachine gun, enhanced": {
			weapon: "Submachine Gun", frame: "Lightweight Frame", rate: 900, magazine: 30,
			perk: "Target Lock", plugType: "Enhanced Trait",
			want: []figures{
				{
					state:         "",
					optimal:       kill(0.6667, 10, 1),
					precisionRate: 180.59,
					body:          kill(1.2667, 0, 20),
					bodyRate:      94.46,
				},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			plain := damage.Of(weapon(tt.weapon, tt.frame, tt.rate, tt.magazine))
			got := damage.Of(
				weapon(tt.weapon, tt.frame, tt.rate, tt.magazine, damage.Plug{Name: tt.perk, Type: tt.plugType}),
			)

			require.Empty(t, got.Reason)
			assert.Empty(t, got.Unmodeled)
			assert.Equal(t, plain.Base, got.Base, "a timed perk leaves the base numbers as they stand")
			require.Len(t, got.Perks, len(tt.want))

			for i, want := range tt.want {
				assert.Equal(t, tt.perk, got.Perks[i].Perk)
				assertFigures(t, want, got.Perks[i])
			}
		})
	}
}

// TestOfReasons pins the reason of a copy without numbers. Such a copy
// has no base numbers, no perk numbers, and no named plugs.
func TestOfReasons(t *testing.T) {
	t.Parallel()

	trait := damage.Plug{Name: "Kill Clip", Type: "Trait"}

	tests := map[string]struct {
		want   string
		weapon damage.Weapon
	}{
		"weapon type without damage": {
			weapon: weapon("Sword", "Adaptive Frame", 0, 0),
			want:   noType,
		},
		"copy without plugs": {
			weapon: damage.Weapon{
				Type:  "Hand Cannon",
				Stats: map[uint32]int{damage.StatRoundsPerMinute: 140, damage.StatMagazine: 12},
			},
			want: noPlugs,
		},
		"copy without a frame among its plugs": {
			weapon: damage.Weapon{
				Type:  "Hand Cannon",
				Plugs: []damage.Plug{trait},
				Stats: map[uint32]int{damage.StatRoundsPerMinute: 140, damage.StatMagazine: 12},
			},
			want: noIntrinsic,
		},
		"frame that the game gained after the source": {
			weapon: weapon("Hand Cannon", "Spread Shot", 140, 12),
			want:   noFrame,
		},
		"burst frame of submachine guns that the source does not hold": {
			weapon: weapon("Submachine Gun", "Aggressive Burst", 600, 30),
			want:   noFrame,
		},
		"Support Frame of auto rifles": {
			weapon: weapon("Auto Rifle", "Support Frame", 450, 40),
			want:   noFrame,
		},
		"heat weapon frame of submachine guns": {
			weapon: weapon("Submachine Gun", "Balanced Heat Weapon", 900, 30),
			want:   noFrame,
		},
		"heat weapon frame of hand cannons": {
			weapon: weapon("Hand Cannon", "Dynamic Heat Weapon", 180, 12),
			want:   noFrame,
		},
		"Micro-Missile Frame of pulse rifles": {
			weapon: weapon("Pulse Rifle", "Micro-Missile Frame", 120, 12),
			want:   noFrame,
		},
		"micro-missile sidearm that shares the name of the Drang frame": {
			weapon: weapon("Sidearm", "Together Forever", 100, 9),
			want: "This copy fires 100 rounds per minute, " +
				"and the community data has no damage for its frame at that rate.",
		},
		"frame of an exotic": {
			weapon: weapon("Hand Cannon", "Memento Mori", 140, 12),
			want:   noFrame,
		},
		"frame of another weapon type": {
			weapon: weapon("Hand Cannon", "Legacy PR-55 Frame", 450, 12),
			want:   noFrame,
		},
		"copy without the Rounds Per Minute stat": {
			weapon: damage.Weapon{
				Type:  "Hand Cannon",
				Plugs: []damage.Plug{{Name: "Adaptive Frame", Type: "Intrinsic"}},
				Stats: map[uint32]int{damage.StatMagazine: 12},
			},
			want: noRate,
		},
		"rate of fire that the data does not give the frame": {
			weapon: weapon("Auto Rifle", "Lightweight Frame", 450, 30),
			want: "This copy fires 450 rounds per minute, " +
				"and the community data has no damage for its frame at that rate.",
		},
		"rate of fire of the frame before Update 9.7.0": {
			weapon: weapon("Auto Rifle", "High-Impact Frame", 360, 30),
			want: "This copy fires 360 rounds per minute, " +
				"and the community data has no damage for its frame at that rate.",
		},
		"rate of fire past the tolerance": {
			weapon: weapon("Hand Cannon", "Adaptive Frame", 146, 12),
			want: "This copy fires 146 rounds per minute, " +
				"and the community data has no damage for its frame at that rate.",
		},
		"copy without the Magazine stat": {
			weapon: damage.Weapon{
				Type:  "Hand Cannon",
				Plugs: []damage.Plug{{Name: "Adaptive Frame", Type: "Intrinsic"}, trait},
				Stats: map[uint32]int{damage.StatRoundsPerMinute: 140},
			},
			want: noMagazine,
		},
		"magazine without rounds": {
			weapon: weapon("Hand Cannon", "Adaptive Frame", 140, 0, trait),
			want:   noMagazine,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, damage.Report{Reason: tt.want, Patch: "9.7.0", Health: 230}, damage.Of(tt.weapon))
		})
	}
}

// TestOfRateTolerance pins the Rounds Per Minute stats that select a
// frame. The game rounds the stat, so a stat within a twentieth of the
// rate of fire of the frame selects it. The Adaptive Frame of hand
// cannons fires 138.46 rounds per minute.
func TestOfRateTolerance(t *testing.T) {
	t.Parallel()

	for _, rate := range []int{132, 138, 140, 145} {
		got := damage.Of(weapon("Hand Cannon", "Adaptive Frame", rate, 12))

		require.Empty(t, got.Reason, rate)
		assertKill(t, kill(0.8667, 3, 0), got.Base.OptimalKill)
	}
}

// TestOfReload pins the numbers that read the reload time of a copy.
func TestOfReload(t *testing.T) {
	t.Parallel()

	unknown := func(int) curves.Measure { return curves.Measure{Reason: noReload} }

	t.Run("unknown reload time omits each damage per second", func(t *testing.T) {
		t.Parallel()

		w := weapon("Hand Cannon", "Adaptive Frame", 140, 12, damage.Plug{Name: "Kill Clip", Type: "Trait"})
		w.Reload = unknown

		got := damage.Of(w)

		require.Empty(t, got.Reason)
		require.Len(t, got.Perks, 1)

		for _, f := range []damage.Figures{got.Base, got.Perks[0]} {
			assert.Equal(t, damage.Rate{Reason: noReload}, f.PrecisionRate)
			assert.Equal(t, damage.Rate{Reason: noReload}, f.BodyRate)
		}

		assertKill(t, kill(0.8667, 3, 0), got.Base.OptimalKill)
		assertKill(t, kill(2.1667, 0, 6), got.Base.BodyKill)
		assertKill(t, kill(0.8667, 2, 1), got.Perks[0].OptimalKill)
		assertKill(t, kill(1.7333, 0, 5), got.Perks[0].BodyKill)
	})

	// Six body shots kill, and the magazine holds four rounds. The kill
	// takes five delays of 13 thirtieths of a second and one reload.
	t.Run("kill that empties the magazine counts a reload", func(t *testing.T) {
		t.Parallel()

		got := damage.Of(weapon("Hand Cannon", "Adaptive Frame", 140, 4))

		require.Empty(t, got.Reason)
		assertKill(t, kill(0.8667, 3, 0), got.Base.OptimalKill)
		assertKill(t, kill(4.1667, 0, 6), got.Base.BodyKill)
	})

	// The Rapid-Fire Frame reloads an empty magazine in 0.8 of the
	// time. Seventeen body shots kill, and the magazine holds ten
	// rounds. The kill takes 16 delays of 2.5 thirtieths of a second
	// and a reload of 1.6 seconds.
	t.Run("reload inside a kill reads the scale of the frame", func(t *testing.T) {
		t.Parallel()

		got := damage.Of(weapon("Auto Rifle", "Rapid-Fire Frame", 720, 10))

		require.Empty(t, got.Reason)
		assertKill(t, kill(2.9333, 0, 17), got.Base.BodyKill)
	})

	// Frenzy scales the damage of a body shot to 15.64, so 15 body shots
	// kill, and the magazine holds ten rounds. Frenzy raises the Reload
	// Speed stat by 100, and the Rapid-Fire Frame scales the reload at
	// that stat by 0.8. The kill takes 14 delays of 2.5 thirtieths of a
	// second and a reload of 0.96 seconds.
	t.Run("reload inside a kill reads the stat bonus of a perk", func(t *testing.T) {
		t.Parallel()

		got := damage.Of(weapon("Auto Rifle", "Rapid-Fire Frame", 720, 10, damage.Plug{Name: "Frenzy", Type: "Trait"}))

		require.Empty(t, got.Reason)
		require.Len(t, got.Perks, 1)
		assertKill(t, kill(2.9333, 0, 17), got.Base.BodyKill)
		assertKill(t, kill(2.1267, 0, 15), got.Perks[0].BodyKill)
	})

	// Drop Mag scales every reload by 0.9. A hand cannon with the
	// Adaptive Frame deals 958.14 with 12 precision hits, and its 12
	// delays take 5.2 seconds. The reload takes 1.8 seconds.
	t.Run("perk that always scales the reload changes the base numbers", func(t *testing.T) {
		t.Parallel()

		got := damage.Of(
			weapon("Hand Cannon", "Adaptive Frame", 140, 12, damage.Plug{Name: "Drop Mag", Type: "Magazine"}),
		)

		require.Empty(t, got.Reason)
		assert.Empty(t, got.Unmodeled)
		assert.Empty(t, got.Perks)
		assertFigures(t, figures{
			optimal:       kill(0.8667, 3, 0),
			precisionRate: 136.88,
			body:          kill(2.1667, 0, 6),
			bodyRate:      76.29,
		}, got.Base)
	})

	t.Run("copy without a reload time omits each damage per second", func(t *testing.T) {
		t.Parallel()

		w := weapon("Hand Cannon", "Adaptive Frame", 140, 12)
		w.Reload = nil

		got := damage.Of(w)

		require.Empty(t, got.Reason)
		assert.Equal(t, damage.Rate{Reason: noReloadFn}, got.Base.PrecisionRate)
		assert.Equal(t, damage.Rate{Reason: noReloadFn}, got.Base.BodyRate)
		assertKill(t, kill(0.8667, 3, 0), got.Base.OptimalKill)
	})

	t.Run("kill that takes a reload of unknown time is unknown", func(t *testing.T) {
		t.Parallel()

		w := weapon("Hand Cannon", "Adaptive Frame", 140, 4)
		w.Reload = unknown

		got := damage.Of(w)

		require.Empty(t, got.Reason)
		assertKill(t, kill(0.8667, 3, 0), got.Base.OptimalKill)
		assert.Equal(t, damage.Kill{Reason: noReload}, got.Base.BodyKill)
	})

	// Target Lock counts the hits of one magazine, so its bonus starts
	// again after a reload. The magazine holds ten rounds. Twenty body
	// shots kill, in 19 delays of 2 thirtieths of a second and one
	// reload. A bonus that kept growing through the reload spares one
	// hit.
	t.Run("ramp starts again after a reload", func(t *testing.T) {
		t.Parallel()

		got := damage.Of(weapon(
			"Submachine Gun", "Lightweight Frame", 900, 10,
			damage.Plug{Name: "Target Lock", Type: "Trait"},
		))

		require.Len(t, got.Perks, 1)
		assertKill(t, kill(3.2667, 0, 20), got.Perks[0].BodyKill)
	})
}

// TestOfPlugs pins the plugs that a report names, and the order of the
// perk numbers. A report names an active plug when the plug is not the
// frame and the damage data gives it no effect. The data also lists
// plugs without an effect, and a report names none of them.
func TestOfPlugs(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		plugs     []damage.Plug
		unmodeled []string
		perks     []string
	}{
		"trait without an effect in the data": {
			plugs:     []damage.Plug{{Name: "Kinetic Tremors", Type: "Trait"}},
			unmodeled: []string{"Kinetic Tremors"},
		},
		"plugs that the data lists without an effect": {
			plugs: []damage.Plug{
				{Name: "Hammer-Forged Rifling", Type: "Barrel"},
				{Name: "Accurized Rounds", Type: "Enhanced Magazine"},
				{Name: "High-Caliber Rounds", Type: "Magazine"},
				{Name: "Zen Moment", Type: "Enhanced Trait"},
				{Name: "Attrition Orbs", Type: "Trait"},
				{Name: "Headstone", Type: "Trait"},
				{Name: "Auto-Loading Holster", Type: "Trait"},
				{Name: "Skulking Wolf", Type: "Origin Trait"},
				{Name: "Problem Solver", Type: "Enhanced Origin Trait"},
				{Name: "Backup Mag", Type: "Weapon Mod"},
				{Name: "Bandolier", Type: "Enhanced Weapon Mod"},
			},
		},
		"plug types that the data lists without an effect": {
			plugs: []damage.Plug{
				{Name: "Red Dot 2 MOA", Type: "Scope"},
				{Name: "Fastdraw HCS", Type: "Sight"},
				{Name: "Sweaty Confetti", Type: "Combat Flair"},
			},
		},
		"enhanced mod that the source does not describe": {
			plugs:     []damage.Plug{{Name: "Backup Mag", Type: "Enhanced Weapon Mod"}},
			unmodeled: []string{"Backup Mag"},
		},
		"enhanced plug of a perk without enhanced effects": {
			plugs:     []damage.Plug{{Name: "Golden Tricorn", Type: "Enhanced Trait"}},
			unmodeled: []string{"Golden Tricorn"},
		},
		"each name one time, in the order of the plugs": {
			plugs: []damage.Plug{
				{Name: "Outlaw", Type: "Trait"},
				{Name: "Kinetic Tremors", Type: "Trait"},
				{Name: "Outlaw", Type: "Enhanced Trait"},
			},
			unmodeled: []string{"Outlaw", "Kinetic Tremors"},
		},
		"perk numbers in the order of the plugs": {
			plugs: []damage.Plug{
				{Name: "Rampage", Type: "Trait"},
				{Name: "Kinetic Tremors", Type: "Trait"},
				{Name: "Kill Clip", Type: "Enhanced Trait"},
			},
			unmodeled: []string{"Kinetic Tremors"},
			perks:     []string{"Rampage at 1 stack", "Rampage at 3 stacks", "Kill Clip "},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := damage.Of(weapon("Submachine Gun", "Lightweight Frame", 900, 30, tt.plugs...))

			require.Empty(t, got.Reason)
			assert.Equal(t, tt.unmodeled, got.Unmodeled)

			var perks []string
			for _, f := range got.Perks {
				perks = append(perks, f.Perk+" "+f.State)
			}

			assert.Equal(t, tt.perks, perks)
		})
	}
}

func TestPatch(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "9.7.0", damage.Patch())
}
