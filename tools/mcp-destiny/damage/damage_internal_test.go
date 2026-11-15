package damage

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lastUpdate is the last update of the game with public patch notes.
const lastUpdate = "9.7.0.6"

// TestEmbeddedData pins the provenance of the damage data. The data
// passes the checks of the loader, so each value names a source. Each
// value also names the last update of the game as the update of its last
// check. A value from the Clarity database names its entry and the
// upload date of the entry.
func TestEmbeddedData(t *testing.T) {
	t.Parallel()

	_, err := load()
	require.NoError(t, err)

	var f file
	require.NoError(t, json.Unmarshal(embedded, &f))

	assert.NotEmpty(t, f.License)

	for key, src := range f.Sources {
		assert.NotEmpty(t, src.Title, key)
		assert.NotEmpty(t, src.URL, key)
		assert.NotEmpty(t, src.Date, key)
		assert.NotEmpty(t, src.Patch, key)
	}

	checked := func(o origin, label string) {
		assert.Equal(t, lastUpdate, o.Checked, label)
	}

	checked(f.Health.origin, "health")

	for _, wt := range f.Types {
		assert.NotEmpty(t, wt.Frames, wt.Name)

		for _, fr := range wt.Frames {
			label := wt.Name + ", " + strings.Join(fr.Names, ", ")
			checked(fr.Damage, label)
			checked(fr.Timing, label)
		}
	}

	for _, p := range f.Perks {
		checked(p.origin, p.Name)
		assert.Equal(t, p.Source == "clarity", p.Entry != 0, p.Name)
		assert.Equal(t, p.Source == "clarity", p.Uploaded != "", p.Name)

		if p.Enhanced != nil {
			checked(p.Enhanced.origin, p.Name+", enhanced")
			assert.Equal(t, p.Enhanced.Source == "clarity", p.Enhanced.Entry != 0, p.Name+", enhanced")
			assert.Equal(t, p.Enhanced.Source == "clarity", p.Enhanced.Uploaded != "", p.Name+", enhanced")
		}
	}

	for _, n := range f.Neutral {
		checked(n.origin, n.Reason)
	}
}

// TestTargetLockRamp compares the ramp of Target Lock with the bonuses
// that the Clarity database gives against players. The bonus starts at
// 10.46% after an eighth of the magazine and ends at 25%, which it
// reaches past the size of the magazine. It starts at 12.55% and ends at
// 30% for the enhanced perk.
func TestTargetLockRamp(t *testing.T) {
	t.Parallel()

	data, err := load()
	require.NoError(t, err)

	perk := data.perks["Target Lock"]
	require.NotNil(t, perk)
	require.NotNil(t, perk.Enhanced)

	tests := map[string]struct {
		ramp  *ramp
		first float64
		last  float64
	}{
		"base perk":     {ramp: perk.States[0].Ramp, first: 1.1046, last: 1.25},
		"enhanced perk": {ramp: perk.Enhanced.States[0].Ramp, first: 1.1255, last: 1.30},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.NotNil(t, tt.ramp)

			// The magazine holds 80 rounds, so ten hits are an eighth
			// of it.
			assert.InDelta(t, 1.0, tt.ramp.at(0, 80), 0)
			assert.InDelta(t, 1.0, tt.ramp.at(9, 80), 0)
			assert.InDelta(t, tt.first, tt.ramp.at(10, 80), 0.0005)
			assert.Greater(t, tt.ramp.at(40, 80), tt.ramp.at(10, 80))
			assert.Greater(t, tt.ramp.at(79, 80), tt.ramp.at(40, 80))
			assert.Less(t, tt.ramp.at(79, 80), tt.ramp.at(120, 80))
			assert.InDelta(t, tt.last, tt.ramp.at(120, 80), 0.0005)
			assert.InDelta(t, tt.last, tt.ramp.at(800, 80), 0.0005)
		})
	}
}

func TestParse(t *testing.T) {
	t.Parallel()

	const (
		origin = `"source": "oracle", "checked": "9.7.0.6"`
		health = `{"value": 230, ` + origin + `}`
		frame  = `{"names": ["Adaptive Frame"], "body": 40, "precision": 70, "burstSize": 1, "burstDelay": 13, ` +
			`"innerBurstDelay": 0, "damage": {` + origin + `}, "timing": {` + origin + `}}`
		perk    = `{"name": "Kill Clip", "states": [{"damage": 1.25}], ` + origin + `}`
		neutral = `{"reason": "The plug changes only stats.", "names": ["Smallbore"], ` + origin + `}`
	)

	// doc returns damage data with one weapon type.
	doc := func(patch, health, frames, perks, neutral string) string {
		return `{
			"license": "MIT",
			"patch": "` + patch + `",
			"health": ` + health + `,
			"sources": {"oracle": {"title": "Oracle Engine", "url": "https://example.com", "date": "2025-05-06", "patch": "8.2.6"}},
			"types": [{"name": "Hand Cannon", "frames": [` + frames + `]}],
			"perks": [` + perks + `],
			"neutral": [` + neutral + `]
		}`
	}

	// framed returns damage data whose one frame replaces a part of
	// the valid frame.
	framed := func(old, replacement string) string {
		return doc("9.7.0", health, strings.Replace(frame, old, replacement, 1), perk, neutral)
	}

	// perked returns damage data with one perk.
	perked := func(perk string) string {
		return doc("9.7.0", health, frame, perk, neutral)
	}

	tests := map[string]struct {
		data string
		err  string
	}{
		"valid data": {
			data: doc("9.7.0", health, frame, perk, neutral),
		},
		"field that the format lacks": {
			data: `{"license": "MIT", "extra": true}`,
			err:  "unknown field",
		},
		"data without a game update": {
			data: doc("", health, frame, perk, neutral),
			err:  "names no game update that its numbers match",
		},
		"health of zero": {
			data: doc("9.7.0", `{"value": 0, `+origin+`}`, frame, perk, neutral),
			err:  "the health of a Guardian must be more than zero",
		},
		"health from a source that the file lacks": {
			data: doc("9.7.0", `{"value": 230, "source": "guess", "checked": "9.7.0.6"}`, frame, perk, neutral),
			err:  `the health of a Guardian: the source "guess" is not a source of the file`,
		},
		"two types with one name": {
			data: strings.Replace(
				doc("9.7.0", health, frame, perk, neutral),
				`"types": [`, `"types": [{"name": "Hand Cannon", "frames": []}, `, 1,
			),
			err: `type 1 has the name "Hand Cannon", which is empty or taken`,
		},
		"frame without a name": {
			data: framed(`"names": ["Adaptive Frame"]`, `"names": []`),
			err:  "Hand Cannon: frame 0 has no name",
		},
		"frame without body damage": {
			data: framed(`"body": 40`, `"body": 0`),
			err:  "the frame Adaptive Frame lacks a body damage, a burst size, or a burst delay",
		},
		"frame without a burst size": {
			data: framed(`"burstSize": 1`, `"burstSize": 0`),
			err:  "the frame Adaptive Frame lacks a body damage, a burst size, or a burst delay",
		},
		"burst without an inner delay": {
			data: framed(`"burstSize": 1`, `"burstSize": 3`),
			err:  "the frame Adaptive Frame fires bursts without an inner burst delay",
		},
		"frame without precision damage and without a reason": {
			data: framed(`"precision": 70, `, ``),
			err:  "must hold a precision damage or a reason for its absence, and not both",
		},
		"frame with precision damage and a reason": {
			data: framed(`"precision": 70`, `"precision": 70, "precisionOmitted": "No source."`),
			err:  "must hold a precision damage or a reason for its absence, and not both",
		},
		"precision damage less than body damage": {
			data: framed(`"precision": 70`, `"precision": 30`),
			err:  "the frame Adaptive Frame has a precision damage less than its body damage",
		},
		"frame damage from a source that the file lacks": {
			data: framed(`"damage": {"source": "oracle"`, `"damage": {"source": "guess"`),
			err:  `the damage of the frame Adaptive Frame: the source "guess" is not a source of the file`,
		},
		"frame timing without a check": {
			data: framed(`"timing": {`+origin+`}`, `"timing": {"source": "oracle"}`),
			err:  "the timing of the frame Adaptive Frame: the value names no game update of its last check",
		},
		"two frames with one name and nearly one rate": {
			data: doc(
				"9.7.0",
				health,
				frame+`, `+strings.Replace(frame, `"burstDelay": 13`, `"burstDelay": 12`, 1),
				perk,
				neutral,
			),
			err: "two frames with the name of Adaptive Frame fire at nearly the same rate",
		},
		"two frames with one name and rates far apart": {
			data: doc(
				"9.7.0",
				health,
				frame+`, `+strings.Replace(frame, `"burstDelay": 13`, `"burstDelay": 10`, 1),
				perk,
				neutral,
			),
		},
		"two perks with one name": {
			data: perked(perk + `, ` + perk),
			err:  `perk 1 has the name "Kill Clip", which is empty or taken`,
		},
		"perk without an effect": {
			data: perked(`{"name": "Kill Clip", ` + origin + `}`),
			err:  "the perk Kill Clip: the perk has no effect",
		},
		"perk from a source that the file lacks": {
			data: perked(
				`{"name": "Kill Clip", "states": [{"damage": 1.25}], "source": "guess", "checked": "9.7.0.6"}`,
			),
			err: `the perk Kill Clip: the source "guess" is not a source of the file`,
		},
		"two states with one label": {
			data: perked(`{"name": "Rampage", "states": [{"label": "at 1 stack", "damage": 1.1}, ` +
				`{"label": "at 1 stack", "damage": 1.2}], ` + origin + `}`),
			err: `two states have the label "at 1 stack"`,
		},
		"state with a negative scale": {
			data: perked(`{"name": "Kill Clip", "states": [{"damage": -1.25}], ` + origin + `}`),
			err:  "an effect has a negative scale",
		},
		"state that changes nothing": {
			data: perked(`{"name": "Kill Clip", "states": [{"label": "at 1 stack"}], ` + origin + `}`),
			err:  "an effect changes nothing",
		},
		"state that raises only the Reload Speed stat": {
			data: perked(`{"name": "Frenzy", "states": [{"reloadStat": 100}], ` + origin + `}`),
		},
		"state that lowers the Reload Speed stat": {
			data: perked(`{"name": "Frenzy", "states": [{"damage": 1.15, "reloadStat": -10}], ` + origin + `}`),
			err:  "an effect has a negative stat bonus",
		},
		"effect that the perk always has and that changes nothing": {
			data: perked(`{"name": "Rapid-Fire Frame", "always": {}, ` + origin + `}`),
			err:  "an effect changes nothing",
		},
		"ramp that falls": {
			data: perked(`{"name": "Target Lock", "states": [{"ramp": {"after": 0.125, "low": 0.3, "high": 0.1, ` +
				`"start": -0.35, "end": 1.135}}], ` + origin + `}`),
			err: "a ramp must rise from its start to its end",
		},
		"enhanced plug with the effects of the base plug": {
			data: perked(`{"name": "Kill Clip", "states": [{"damage": 1.25}], "enhanced": {"same": true, ` +
				origin + `}, ` + origin + `}`),
		},
		"enhanced plug without effects": {
			data: perked(
				`{"name": "Kill Clip", "states": [{"damage": 1.25}], "enhanced": {` + origin + `}, ` + origin + `}`,
			),
			err: "the enhanced plug must have the effects of the base plug or states of its own, and not both",
		},
		"enhanced plug with both kinds of effects": {
			data: perked(`{"name": "Kill Clip", "states": [{"damage": 1.25}], "enhanced": {"same": true, ` +
				`"states": [{"damage": 1.3}], ` + origin + `}, ` + origin + `}`),
			err: "the enhanced plug must have the effects of the base plug or states of its own, and not both",
		},
		"enhanced plug without a source": {
			data: perked(
				`{"name": "Kill Clip", "states": [{"damage": 1.25}], "enhanced": {"same": true}, ` + origin + `}`,
			),
			err: `the enhanced plug: the source "" is not a source of the file`,
		},
		"neutral group without a reason": {
			data: doc("9.7.0", health, frame, perk, `{"names": ["Smallbore"], `+origin+`}`),
			err:  "neutral group 0: the group gives no reason",
		},
		"neutral group without a plug": {
			data: doc("9.7.0", health, frame, perk, `{"reason": "The plug changes only stats.", `+origin+`}`),
			err:  "neutral group 0: the group lists no plug",
		},
		"neutral plug that is also a perk": {
			data: doc("9.7.0", health, frame, perk, strings.Replace(neutral, "Smallbore", "Kill Clip", 1)),
			err:  "neutral group 0: the plug Kill Clip also has an effect as a perk",
		},
		"neutral group without a check": {
			data: doc(
				"9.7.0",
				health,
				frame,
				perk,
				`{"reason": "The plug changes only stats.", "types": ["Sight"], "source": "oracle"}`,
			),
			err: "neutral group 0: the value names no game update of its last check",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := parse([]byte(tt.data))
			if tt.err == "" {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, ErrData)
			assert.ErrorContains(t, err, tt.err)
		})
	}
}

// TestGunLimits pins the kills that a gun cannot give. A frame of the
// damage data kills in fewer than 50 hits, so the test builds its own
// frame.
func TestGunLimits(t *testing.T) {
	t.Parallel()

	weak := &frame{Body: 1, Precision: 2, BurstSize: 1, BurstDelay: 2}

	t.Run("kill of more than 50 hits", func(t *testing.T) {
		t.Parallel()

		g := gun{frame: weak, health: 230, magazine: 200}
		want := "This weapon does not defeat a Guardian in 50 hits."

		assert.Equal(t, Kill{Reason: want}, g.bodyKill(effect{}))
		assert.Equal(t, Kill{Reason: want}, g.optimalKill(effect{}))
	})

	t.Run("exactly 50 hits kill", func(t *testing.T) {
		t.Parallel()

		g := gun{frame: weak, health: 50, magazine: 200}

		got := g.bodyKill(effect{})

		assert.Empty(t, got.Reason)
		assert.Equal(t, 50, got.Body)
		assert.InDelta(t, 49*2*tick, got.Seconds, 1e-9)
	})
}
