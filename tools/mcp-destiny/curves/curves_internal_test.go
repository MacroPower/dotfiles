package curves

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lastUpdate is the last update of the game with public patch notes.
const lastUpdate = "9.7.0.6"

// TestEmbeddedData pins the provenance of the curve data. The data
// passes the checks of the loader, so each curve and each scale names a
// source. Each one also names the last update of the game as the update
// of its last check.
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

	for _, wt := range f.Types {
		assert.NotEmpty(t, wt.Weapons, wt.Name)

		for name, s := range wt.Curves {
			for _, c := range []curve{s.Reload, s.Ready, s.Stow, s.Aim, s.Falloff} {
				assert.Equal(t, lastUpdate, c.Checked, "%s, set %s", wt.Name, name)
			}
		}

		for name, s := range wt.AimScales {
			assert.Equal(t, lastUpdate, s.Checked, "%s, scale %s", wt.Name, name)
		}
	}
}

func TestParse(t *testing.T) {
	t.Parallel()

	// doc returns curve data with one weapon type. Reload is the reload
	// curve of its set, scale is its aim scale, and weapons lists its
	// weapons.
	doc := func(reload, scale, weapons string) string {
		line := `{"linear": -0.002, "constant": 0.5, "source": "oracle", "checked": "9.7.0.6"}`

		return `{
			"license": "MIT",
			"sources": {"oracle": {"title": "Oracle Engine", "url": "https://example.com", "date": "2025-05-06", "patch": "8.2.6"}},
			"frames": [{"names": ["Adaptive Frame"], "hashes": [10, 11]}],
			"types": [{
				"name": "Auto Rifle",
				"curves": {"default": {
					"reload": ` + reload + `,
					"ready": ` + line + `, "stow": ` + line + `, "aim": ` + line + `, "falloff": ` + line + `
				}},
				"aimScales": {"default": ` + scale + `},
				"weapons": [` + weapons + `]
			}]
		}`
	}

	const (
		reload = `{"quadratic": 0.0001, "linear": -0.02, "constant": 2.8, "source": "oracle", "checked": "9.7.0.6"}`
		scale  = `{"scale": 1.6, "source": "oracle", "checked": "9.7.0.6"}`
		weapon = `{"hash": 10, "name": "Adaptive Frame", "curves": "default", "aimScale": "default"}`
	)

	tests := map[string]struct {
		data string
		err  string
	}{
		"valid": {
			data: doc(reload, scale, weapon),
		},
		"omitted curve with a reason": {
			data: doc(`{"omitted": "A patch changed it.", "source": "oracle", "checked": "9.7.0.6"}`, scale, weapon),
		},
		"not JSON": {
			data: "{",
			err:  "curve data: unexpected EOF",
		},
		"unknown field": {
			data: strings.Replace(doc(reload, scale, weapon), `"license"`, `"terms"`, 1),
			err:  `curve data: json: unknown field "terms"`,
		},
		"curve with an unknown source": {
			data: doc(`{"linear": -0.02, "constant": 2.8, "source": "forum", "checked": "9.7.0.6"}`, scale, weapon),
			err: `curve data: Auto Rifle: the reload curve of the set "default": ` +
				`the source "forum" is not a source of the file`,
		},
		"curve without the update of its last check": {
			data: doc(`{"linear": -0.02, "constant": 2.8, "source": "oracle"}`, scale, weapon),
			err: `curve data: Auto Rifle: the reload curve of the set "default": ` +
				`the value names no game update of its last check`,
		},
		"curve without numbers and without a reason": {
			data: doc(`{"source": "oracle", "checked": "9.7.0.6"}`, scale, weapon),
			err: `curve data: Auto Rifle: the reload curve of the set "default": ` +
				`the value must hold numbers or a reason for their absence, and not both`,
		},
		"curve with numbers and a reason": {
			data: doc(
				`{"omitted": "A patch changed it.", "constant": 2.8, "source": "oracle", "checked": "9.7.0.6"}`,
				scale, weapon,
			),
			err: `curve data: Auto Rifle: the reload curve of the set "default": ` +
				`the value must hold numbers or a reason for their absence, and not both`,
		},
		"scale without numbers and without a reason": {
			data: doc(reload, `{"source": "oracle", "checked": "9.7.0.6"}`, weapon),
			err: `curve data: Auto Rifle: the aim scale "default": ` +
				`the value must hold numbers or a reason for their absence, and not both`,
		},
		"set without curves": {
			data: strings.Replace(
				doc(reload, scale, weapon),
				`"aimScales"`,
				`"curves": {"default": null}, "aimScales"`,
				1,
			),
			err: `curve data: Auto Rifle: the set "default" holds no curves`,
		},
		"weapon with an unknown set": {
			data: doc(
				reload,
				scale,
				`{"hash": 10, "name": "Adaptive Frame", "curves": "heavy", "aimScale": "default"}`,
			),
			err: "curve data: Auto Rifle: " +
				"the weapon 10 names a set or a scale that the type lacks, or it repeats",
		},
		"weapon with an unknown scale": {
			data: doc(
				reload,
				scale,
				`{"hash": 10, "name": "Adaptive Frame", "curves": "default", "aimScale": "burst"}`,
			),
			err: "curve data: Auto Rifle: " +
				"the weapon 10 names a set or a scale that the type lacks, or it repeats",
		},
		"weapon twice": {
			data: doc(reload, scale, weapon+","+weapon),
			err: "curve data: Auto Rifle: " +
				"the weapon 10 names a set or a scale that the type lacks, or it repeats",
		},
		"hash in two frames": {
			data: strings.Replace(
				doc(reload, scale, weapon),
				`"frames": [`, `"frames": [{"names": ["Adaptive Burst"], "hashes": [11]},`, 1,
			),
			err: "curve data: two frames list the hash 11",
		},
		"type without a name": {
			data: strings.Replace(doc(reload, scale, weapon), `"name": "Auto Rifle"`, `"name": ""`, 1),
			err:  `curve data: type 0 has the name "", which is empty or taken`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := parse([]byte(tc.data))
			if tc.err != "" {
				require.ErrorIs(t, err, ErrData)
				require.EqualError(t, err, tc.err)

				return
			}

			require.NoError(t, err)
			assert.Contains(t, got.types, "Auto Rifle")
		})
	}
}
