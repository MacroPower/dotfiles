package clarity

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/claritytest"
)

// Characters of the Clarity text that the decode rewrites or removes.
const (
	enhancedArrow = "\U0001F845"
	heavyArrow    = "\U0001F81A"
	doubleArrow   = "\U000021D2"
	quoteOpen     = "\U0000259B"
	quoteClose    = "\U0000259F"
	wordJoiner    = "\U00002060"
	byteOrderMark = "\U0000FEFF"
	noBreakSpace  = "\U000000A0"
)

// Characters that the decoded text holds.
const (
	bullet     = "\U00002022"
	rightArrow = "\U00002192"
)

func TestDecode(t *testing.T) {
	t.Parallel()

	uploaded := time.Date(2026, time.January, 17, 1, 42, 0, 914_000_000, time.UTC)

	// entry returns a descriptions file with one entry for plug 900,
	// whose English lines are the JSON text lines.
	entry := func(lines string) string {
		return `{"900": {"hash": 900, "name": "Kill Clip", "type": "Weapon Trait", "lastUpload": 1768614120914,
			"uploadedBy": "tester", "descriptions": {"en": [` + lines + `]}}}`
	}

	// described returns the database that holds lines for plug 900.
	described := func(lines ...string) map[uint32]Description {
		return map[uint32]Description{900: {Name: "Kill Clip", Type: "Weapon Trait", Uploaded: uploaded, Lines: lines}}
	}

	tests := map[string]struct {
		err  error
		want map[uint32]Description
		raw  string
	}{
		"a spacer parts two lines and leaves no line": {
			raw: entry(`{"linesContent": [{"text": "Reloading after a kill grants 25% damage."}]},
				{"classNames": ["spacer"]},
				{"linesContent": [{"text": "` + bullet + ` Lasts 5 seconds."}]}`),
			want: described("Reloading after a kill grants 25% damage.", bullet+" Lasts 5 seconds."),
		},
		"a run without text stands for an icon": {
			raw: entry(`{"linesContent": [{"text": "Refills the magazine while "}, {"classNames": ["arc"]},
				{"text": "Amplified."}]}`),
			want: described("Refills the magazine while Amplified."),
		},
		"a line of icons leaves no line": {
			raw: entry(`{"linesContent": [{"classNames": ["solar"]}, {"text": " "}]},
				{"linesContent": [{"text": "Scorches the target."}]}`),
			want: described("Scorches the target."),
		},
		"a PvP value names PvP inside its bracket": {
			raw: entry(`{"linesContent": [{"text": "Heals for "}, {"text": "40", "classNames": ["pve"]}, {"text": " "},
				{"text": "[35]", "classNames": ["pvp"]}, {"text": " HP each second."}]}`),
			want: described("Heals for 40 [PvP 35] HP each second."),
		},
		"a PvP run without a bracket keeps its text": {
			raw:  entry(`{"classNames": ["pvp"], "linesContent": [{"text": "PvP: 3% damage", "classNames": ["pvp"]}]}`),
			want: described("PvP: 3% damage"),
		},
		"the enhanced arrow becomes the enhanced mark": {
			raw: entry(
				`{"linesContent": [{"text": "Lasts "}, {"text": "` + enhancedArrow + `", "classNames": ["enhancedArrow"]},
				{"text": "5.5 seconds."}]},
				{"linesContent": [{"text": "` + enhancedArrow + ` The duration is 0.5 seconds longer."}]}`,
			),
			want: described(
				"Lasts "+EnhancedMark+"5.5 seconds.",
				EnhancedMark+" The duration is 0.5 seconds longer.",
			),
		},
		"the other arrows become a right arrow": {
			raw: entry(`{"linesContent": [{"text": "Radius 8` + heavyArrow + `12 meters (triple kill ` + doubleArrow +
				` 3 stacks)."}]}`),
			want: described("Radius 8" + rightArrow + "12 meters (triple kill " + rightArrow + " 3 stacks)."),
		},
		"the block characters become quotation marks": {
			raw: entry(`{"linesContent": [{"text": "` + quoteOpen + ` "}, {"text": "A quote.", "classNames": ["bold"]},
				{"text": " ` + quoteClose + `"}]},
				{"linesContent": [{"text": "` + quoteOpen + `Another.` + quoteClose + `"}]}`),
			want: described(`"A quote."`, `"Another."`),
		},
		"white space and characters without a width": {
			raw: entry(`{"linesContent": [{"text": "  Grants` + noBreakSpace + ` 20  Hand` + wordJoiner + `ling` +
				byteOrderMark + ` "}, {"text": " while sprinting. "}]}`),
			want: described("Grants 20 Handling while sprinting."),
		},
		"a link and a title keep their text": {
			raw: entry(`{"linesContent": [{"text": "Refills", "classNames": ["title"],
				"title": [{"linesContent": [{"text": "Loads bullets without a reload."}]}]},
				{"text": " the magazine against "},
				{"text": "Bosses", "link": "https://example.invalid/combatants", "classNames": ["link"]}, {"text": "."}]}`),
			want: described("Refills the magazine against Bosses."),
		},
		"an exotic entry keeps the name of its item": {
			raw: `{"901": {"name": "Sun Blast", "itemName": "Sunshot", "lastUpload": 1768614120914,
				"descriptions": {"en": [{"linesContent": [{"text": "Targets explode."}]}]}}}`,
			want: map[uint32]Description{
				901: {Name: "Sun Blast", Item: "Sunshot", Uploaded: uploaded, Lines: []string{"Targets explode."}},
			},
		},
		"an entry without English text and a malformed entry leave no description": {
			raw: `{
				"900": {"name": "Kill Clip", "type": "Weapon Trait", "lastUpload": 1768614120914,
					"descriptions": {"en": [{"linesContent": [{"text": "Grants damage."}]}]}},
				"901": {"name": "No descriptions", "descriptions": null},
				"902": {"name": "Only a spacer", "descriptions": {"en": [{"classNames": ["spacer"]}]}},
				"903": {"name": "Another language", "descriptions": {"de": [{"linesContent": [{"text": "Schaden."}]}]}},
				"904": {"name": "Upload time that is no number", "lastUpload": "yesterday",
					"descriptions": {"en": [{"linesContent": [{"text": "Text."}]}]}},
				"905": {"name": "Lines that are no list", "descriptions": {"en": "Text."}},
				"906": "Entry that is no object",
				"not-a-hash": {"name": "No hash", "descriptions": {"en": [{"linesContent": [{"text": "Text."}]}]}},
				"4294967296": {"name": "Past 32 bits", "descriptions": {"en": [{"linesContent": [{"text": "Text."}]}]}}
			}`,
			want: described("Grants damage."),
		},
		"a file without a description": {
			raw: `{"901": {"name": "No descriptions", "descriptions": null}}`,
			err: errNoDescriptions,
		},
		"an empty file": {
			raw: `{}`,
			err: errNoDescriptions,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			db, err := Decode("2.5", []byte(tc.raw))
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				assert.Nil(t, db)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, "2.5", db.Version)
			assert.Equal(t, tc.want, db.Descriptions)
		})
	}
}

// TestDecodeEnhancedUpload pins the upload time that the decode gives the
// entry of an enhanced perk. It also pins what [Description.Outdated]
// reports for that time. Each case decodes the entry of the enhanced
// perk with the hash 910. A case that gives a base type also decodes an
// entry of its base perk with the hash 920.
func TestDecodeEnhancedUpload(t *testing.T) {
	t.Parallel()

	day := func(year int, month time.Month, d int) time.Time {
		return time.Date(year, month, d, 0, 0, 0, 0, time.UTC)
	}

	update := Patch{Version: "9.7.0", Date: day(2026, time.June, 9)}

	tests := map[string]struct {
		enhanced     time.Time
		base         time.Time
		want         time.Time
		change       Change
		enhancedType string
		baseType     string
		outdated     bool
	}{
		"a base perk that Clarity updated after the patch": {
			enhancedType: "Weapon Trait Enhanced",
			enhanced:     day(2024, time.February, 29),
			baseType:     "Weapon Trait",
			base:         day(2026, time.July, 7),
			want:         day(2026, time.July, 7),
		},
		"a base perk that Clarity last updated before the patch": {
			enhancedType: "Weapon Trait Enhanced",
			enhanced:     day(2024, time.February, 29),
			baseType:     "Weapon Trait",
			base:         day(2024, time.January, 17),
			want:         day(2024, time.February, 29),
			change:       Change{Name: "Focused Fury", Patch: update},
			outdated:     true,
		},
		"an enhanced perk that Clarity updated after its base perk": {
			enhancedType: "Weapon Trait Enhanced",
			enhanced:     day(2026, time.July, 7),
			baseType:     "Weapon Trait Origin",
			base:         day(2024, time.January, 17),
			want:         day(2026, time.July, 7),
		},
		"an enhanced perk without a base perk": {
			enhancedType: "Weapon Trait Enhanced",
			enhanced:     day(2024, time.February, 29),
			want:         day(2024, time.February, 29),
			change:       Change{Name: "Focused Fury", Patch: update},
			outdated:     true,
		},
		"an enhanced weapon frame": {
			enhancedType: "Weapon Frame Enhanced",
			enhanced:     day(2024, time.February, 29),
			baseType:     "Weapon Frame",
			base:         day(2026, time.July, 7),
			want:         day(2024, time.February, 29),
			change:       Change{Name: "Focused Fury", Patch: update},
			outdated:     true,
		},
		"a base perk of an exotic item": {
			enhancedType: "Weapon Trait Enhanced",
			enhanced:     day(2024, time.February, 29),
			baseType:     "Weapon Trait Exotic",
			base:         day(2026, time.July, 7),
			want:         day(2024, time.February, 29),
			change:       Change{Name: "Focused Fury", Patch: update},
			outdated:     true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			perks := []claritytest.Perk{{
				Hash:     910,
				Name:     "Focused Fury",
				Type:     tc.enhancedType,
				Uploaded: tc.enhanced,
				Lines:    []string{"Precision hits grant 25% damage for " + enhancedArrow + "12 seconds."},
			}}
			if tc.baseType != "" {
				perks = append(perks, claritytest.Perk{
					Hash:     920,
					Name:     "Focused Fury",
					Type:     tc.baseType,
					Uploaded: tc.base,
					Lines:    []string{"Precision hits grant 25% damage for 11 seconds."},
				})
			}

			db, err := Decode("2.5", []byte(claritytest.Descriptions(t, perks...)))
			require.NoError(t, err)

			desc, ok := db.Description(910)
			require.True(t, ok)
			assert.Equal(t, tc.want, desc.Uploaded)

			change, outdated := desc.Outdated()
			assert.Equal(t, tc.outdated, outdated)
			assert.Equal(t, tc.change, change)

			if tc.baseType != "" {
				base, ok := db.Description(920)
				require.True(t, ok)
				assert.Equal(t, tc.base, base.Uploaded, "the base perk keeps the time of its own upload")
			}
		})
	}
}

func TestDecodeRejectsOtherContent(t *testing.T) {
	t.Parallel()

	db, err := Decode("2.5", []byte(`<html>Not found</html>`))

	var syntax *json.SyntaxError

	require.ErrorAs(t, err, &syntax)
	require.ErrorContains(t, err, "decoding the descriptions file")
	assert.Nil(t, db)
}

func TestDatabaseDescription(t *testing.T) {
	t.Parallel()

	kept := Description{Name: "Kill Clip", Lines: []string{"Grants damage."}}
	db := &Database{Descriptions: map[uint32]Description{900: kept}}

	got, ok := db.Description(900)
	assert.True(t, ok)
	assert.Equal(t, kept, got)

	_, ok = db.Description(901)
	assert.False(t, ok, "the database holds no description of plug 901")

	var none *Database

	_, ok = none.Description(900)
	assert.False(t, ok, "a nil database holds no description")
}

func TestDatabaseBase(t *testing.T) {
	t.Parallel()

	// entry returns the entry of a descriptions file for a perk with the
	// name, the type, and one line of text.
	entry := func(hash, name, typeName, text string) string {
		return `"` + hash + `": {"name": "` + name + `", "type": "` + typeName + `",
			"descriptions": {"en": [{"linesContent": [{"text": "` + text + `"}]}]}}`
	}

	db, err := Decode("2.5", []byte(`{`+strings.Join([]string{
		entry("910", "Gun and Run", "Weapon Trait Enhanced", "Sprinting grants "+enhancedArrow+"25 Handling."),
		entry("930", "Gun and Run", "Weapon Trait Origin", "Sprinting grants 20 Handling."),
		entry("950", "Psychohack", "Weapon Trait Origin", "The origin trait of a later weapon."),
		entry("940", "Psychohack", "Weapon Trait Origin", "Hits apply Suppression."),
		entry("905", "Rewind Rounds", "Weapon Trait Exotic", "The trait of Outbreak Perfected."),
		entry("970", "Rewind Rounds", "Weapon Trait", "Refills the magazine from reserves."),
		entry("990", "Alloy Magazine", "Weapon Perk", "Reloads faster from empty."),
		entry("980", "Precision Frame", "Weapon Frame", "Recoil is more vertical."),
		entry("995", "Reaper", "Armor Mod General", "Kills grant class ability energy."),
	}, ",")+`}`))
	require.NoError(t, err)

	tests := map[string]struct {
		name string
		want []string
	}{
		"an enhanced entry stands for no base perk": {
			name: "Gun and Run",
			want: []string{"Sprinting grants 20 Handling."},
		},
		"the name folds": {
			name: "G\u00fan AND run",
			want: []string{"Sprinting grants 20 Handling."},
		},
		"two entries with one name give the entry with the lower hash": {
			name: "Psychohack",
			want: []string{"Hits apply Suppression."},
		},
		"an exotic entry stands for no base perk": {
			name: "Rewind Rounds",
			want: []string{"Refills the magazine from reserves."},
		},
		"a barrel or a magazine": {
			name: "Alloy Magazine",
			want: []string{"Reloads faster from empty."},
		},
		"a weapon frame": {
			name: "Precision Frame",
		},
		"an armor mod": {
			name: "Reaper",
		},
		"a name without an entry": {
			name: "Kill Clip",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, ok := db.Base(tc.name)
			if tc.want == nil {
				assert.False(t, ok)

				return
			}

			require.True(t, ok)
			assert.Equal(t, tc.want, got.Lines)
		})
	}
}

func TestDatabaseBaseOfLiteral(t *testing.T) {
	t.Parallel()

	kept := Description{Name: "Gun and Run", Type: "Weapon Trait Origin", Lines: []string{"Grants Handling."}}
	db := &Database{Descriptions: map[uint32]Description{930: kept}}

	got, ok := db.Base("Gun and Run")
	assert.True(t, ok)
	assert.Equal(t, kept, got)

	var none *Database

	_, ok = none.Base("Gun and Run")
	assert.False(t, ok, "a nil database holds no description")
}
