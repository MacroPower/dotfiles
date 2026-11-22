package inventory

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungietest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitionstest"
)

// Hashes of the pattern fixtures of [patternDefs].
const (
	plugResonance       uint32 = 910
	plugHarmonizer      uint32 = 911
	plugResonanceOther  uint32 = 912
	itemForbidden       uint32 = 120
	itemTimelost        uint32 = 105
	itemAdeptNoPattern  uint32 = 106
	itemHarrowedHelmet  uint32 = 107
	recordFatebringer   uint32 = 5001
	recordMidnightCoup  uint32 = 5002
	recordGjallarhorn   uint32 = 5003
	recordForbidden     uint32 = 5004
	recordWithoutRecord uint32 = 5005
)

// patternDefs returns the fixture definitions with the plugs of the two
// crafting sockets that Deepsight reads and with pattern records.
// Fatebringer (100), Gjallarhorn (101), Hawkmoon (103), and Austringer
// (104) name a recipe item. Midnight Coup (102) names none, although a
// pattern record carries its name. Item 120 sits in the forbidden bucket.
// No pattern record carries the name of Austringer. Item 105 is the
// weapon Fatebringer (Timelost), and item 106 is the weapon Austringer
// (Adept). Neither names a recipe item. Item 107, Fatebringer (Harrowed),
// names no recipe item and sits in the helmet bucket.
func patternDefs() *definitions.Defs {
	defs := definitionstest.Defs()

	defs.PlugCategories = map[uint32]uint32{
		plugResonance:      bungie.PlugCategoryResonance,
		plugHarmonizer:     bungie.PlugCategoryHarmonizer,
		plugResonanceOther: bungie.PlugCategoryResonance,
	}
	defs.Patterns = map[string]uint32{
		"Fatebringer":    recordFatebringer,
		"Midnight Coup":  recordMidnightCoup,
		"Gjallarhorn":    recordGjallarhorn,
		"Forbidden Coup": recordForbidden,
		"Hawkmoon":       recordWithoutRecord,
	}
	defs.PatternsListed = true

	defs.Items[103] = definitions.ItemDef{Name: "Hawkmoon", Bucket: bungie.BucketKinetic}
	defs.Items[104] = definitions.ItemDef{Name: "Austringer", Bucket: bungie.BucketKinetic}
	defs.Items[itemTimelost] = definitions.ItemDef{
		Name:     "Fatebringer (Timelost)",
		Bucket:   bungie.BucketKinetic,
		ItemType: bungie.ItemTypeWeapon,
	}
	defs.Items[itemAdeptNoPattern] = definitions.ItemDef{
		Name:     "Austringer (Adept)",
		Bucket:   bungie.BucketKinetic,
		ItemType: bungie.ItemTypeWeapon,
	}
	defs.Items[itemHarrowedHelmet] = definitions.ItemDef{Name: "Fatebringer (Harrowed)", Bucket: bungie.BucketHelmet}

	for _, hash := range []uint32{100, 101, 103, 104} {
		def := defs.Items[hash]
		def.Craftable = true
		def.ItemType = bungie.ItemTypeWeapon
		def.ClassType = bungie.ClassAny
		defs.Items[hash] = def
	}

	defs.Items[itemForbidden] = definitions.ItemDef{
		Name:       "Forbidden Coup",
		TypeName:   "Hand Cannon",
		Bucket:     bungie.BucketForbidden,
		ItemType:   bungie.ItemTypeWeapon,
		ClassType:  bungie.ClassAny,
		Equippable: true,
	}

	return defs
}

// patternProfile returns a profile in which the Hunter holds one copy of
// the item hash with the instance id "1" and the sockets of the JSON
// array sockets. The profile records hold the pattern of Fatebringer at
// 3 of 5 with the state fatebringer and the complete pattern of Midnight
// Coup. The records of the Hunter hold the pattern of Gjallarhorn at 1 of
// 2, and those of the Titan hold the pattern of Forbidden Coup.
func patternProfile(t *testing.T, hash uint32, sockets string, fatebringer int) *bungie.Profile {
	t.Helper()

	body := fmt.Sprintf(`{
		"characters": {"data": {
			"c1": {"characterId": "c1", "classType": 1, "dateLastPlayed": "2026-11-10T12:00:00Z"},
			"c2": {"characterId": "c2", "classType": 0, "dateLastPlayed": "2026-11-01T12:00:00Z"}
		}},
		"characterInventories": {"data": {"c1": {"items": [
			{"itemHash": %[1]d, "itemInstanceId": "1", "quantity": 1, "location": 1, "bucketHash": %[2]d}
		]}}},
		"itemComponents": {"sockets": {"data": {"1": {"sockets": %[3]s}}}},
		"profileRecords": {"data": {"records": {
			"%[4]d": {"state": %[5]d, "objectives": [{"objectiveHash": 1, "progress": 3, "completionValue": 5}]},
			"%[6]d": {"state": 0, "objectives": [{"objectiveHash": 1, "progress": 4, "completionValue": 4}]}
		}}},
		"characterRecords": {"data": {
			"c1": {"records": {"%[7]d": {"state": 4, "objectives": [{"objectiveHash": 1, "progress": 1, "completionValue": 2}]}}},
			"c2": {"records": {"%[8]d": {"state": 4, "objectives": []}}}
		}}
	}`, hash, bungie.BucketKinetic, sockets, recordFatebringer, fatebringer, recordMidnightCoup,
		recordGjallarhorn, recordForbidden)

	var p bungie.Profile

	require.NoError(t, json.Unmarshal([]byte(body), &p))

	return &p
}

func TestNewItemDeepsight(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		sockets          string
		hash             uint32
		state            int
		wantDeepsight    bool
		wantHarmonizable bool
	}{
		"visible resonance socket": {
			hash:          100,
			sockets:       `[{"plugHash": 900, "isVisible": true}, {"plugHash": 910, "isVisible": true}]`,
			state:         bungie.RecordObjectiveNotCompleted,
			wantDeepsight: true,
		},
		"hidden resonance socket": {
			hash:    100,
			sockets: `[{"plugHash": 910, "isVisible": false}]`,
			state:   bungie.RecordObjectiveNotCompleted,
		},
		// DIM reads the visibility of the first resonance socket alone.
		"hidden resonance socket before a visible one": {
			hash:    100,
			sockets: `[{"plugHash": 910, "isVisible": false}, {"plugHash": 912, "isVisible": true}]`,
			state:   bungie.RecordObjectiveNotCompleted,
		},
		"resonance socket of a complete pattern": {
			hash:          100,
			sockets:       `[{"plugHash": 910, "isVisible": true}]`,
			wantDeepsight: true,
		},
		"resonance socket of an item outside the weapon buckets": {
			hash:    200,
			sockets: `[{"plugHash": 910, "isVisible": true}]`,
		},
		"resonance socket of an item in the forbidden bucket": {
			hash:          itemForbidden,
			sockets:       `[{"plugHash": 910, "isVisible": true}]`,
			wantDeepsight: true,
		},
		"visible harmonizer socket of an incomplete pattern": {
			hash:             100,
			sockets:          `[{"plugHash": 911, "isVisible": true}]`,
			state:            bungie.RecordObjectiveNotCompleted,
			wantHarmonizable: true,
		},
		"visible harmonizer socket of a complete pattern": {
			hash:    100,
			sockets: `[{"plugHash": 911, "isVisible": true}]`,
		},
		// Midnight Coup names no recipe item, so it has no pattern.
		"visible harmonizer socket of an item without a pattern": {
			hash:             102,
			sockets:          `[{"plugHash": 911, "isVisible": true}]`,
			wantHarmonizable: true,
		},
		"hidden harmonizer socket": {
			hash:    100,
			sockets: `[{"plugHash": 911, "isVisible": false}]`,
			state:   bungie.RecordObjectiveNotCompleted,
		},
		"visible harmonizer socket after a hidden one": {
			hash:             100,
			sockets:          `[{"plugHash": 911, "isVisible": false}, {"plugHash": 911, "isVisible": true}]`,
			state:            bungie.RecordObjectiveNotCompleted,
			wantHarmonizable: true,
		},
		"visible socket without a plug": {
			hash:    100,
			sockets: `[{"isVisible": true}]`,
			state:   bungie.RecordObjectiveNotCompleted,
		},
		"resonance socket of a Timelost copy": {
			hash:          itemTimelost,
			sockets:       `[{"plugHash": 910, "isVisible": true}]`,
			state:         bungie.RecordObjectiveNotCompleted,
			wantDeepsight: true,
		},
		// The Timelost copy carries the pattern of Fatebringer.
		"visible harmonizer socket of a Timelost copy of an incomplete pattern": {
			hash:             itemTimelost,
			sockets:          `[{"plugHash": 911, "isVisible": true}]`,
			state:            bungie.RecordObjectiveNotCompleted,
			wantHarmonizable: true,
		},
		"visible harmonizer socket of a Timelost copy of a complete pattern": {
			hash:    itemTimelost,
			sockets: `[{"plugHash": 911, "isVisible": true}]`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := NewSnapshot(patternProfile(t, tc.hash, tc.sockets, tc.state), patternDefs(), bungietest.Now)

			item, ok := s.Find("1")
			require.True(t, ok)
			assert.Equal(t, tc.wantDeepsight, item.Deepsight, "deepsight")
			assert.Equal(t, tc.wantHarmonizable, item.Harmonizable, "harmonizable")
		})
	}
}

func TestNewItemRaid(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		raid      []uint32
		listed    bool
		wantRaid  bool
		wantKnown bool
	}{
		"weapon that the source tags put in a raid": {
			raid:      []uint32{100},
			listed:    true,
			wantRaid:  true,
			wantKnown: true,
		},
		"weapon that the source tags put elsewhere": {
			raid:      []uint32{101},
			listed:    true,
			wantKnown: true,
		},
		"index without source tags": {},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			defs := patternDefs()
			defs.RaidItems = tc.raid
			defs.SourcesListed = tc.listed

			s := NewSnapshot(patternProfile(t, 100, `[]`, 0), defs, bungietest.Now)

			item, ok := s.Find("1")
			require.True(t, ok)
			assert.Equal(t, tc.wantRaid, item.Raid, "raid")
			assert.Equal(t, tc.wantKnown, item.SourceKnown, "source known")
		})
	}
}

func TestNewItemPattern(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		want  *Pattern
		hash  uint32
		state int
	}{
		"craftable weapon with an incomplete pattern": {
			hash:  100,
			state: bungie.RecordObjectiveNotCompleted,
			want: &Pattern{
				Name:       "Fatebringer",
				Hash:       recordFatebringer,
				Objectives: []Objective{{Progress: 3, Completion: 5}},
			},
		},
		"craftable weapon with a complete pattern": {
			hash: 100,
			want: &Pattern{
				Name:       "Fatebringer",
				Hash:       recordFatebringer,
				Objectives: []Objective{{Progress: 3, Completion: 5}},
				Complete:   true,
			},
		},
		"pattern in the records of the character that played last": {
			hash: 101,
			want: &Pattern{
				Name:       "Gjallarhorn",
				Hash:       recordGjallarhorn,
				Objectives: []Objective{{Progress: 1, Completion: 2}},
			},
		},
		"definition without a recipe item": {
			hash: 102,
		},
		"pattern in the records of a character that did not play last": {
			hash: itemForbidden,
		},
		"craftable weapon whose pattern record the profile lacks": {
			hash: 103,
		},
		"craftable weapon whose name no pattern record has": {
			hash: 104,
		},
		"Timelost copy of a weapon with an incomplete pattern": {
			hash:  itemTimelost,
			state: bungie.RecordObjectiveNotCompleted,
			want: &Pattern{
				Name:       "Fatebringer",
				Hash:       recordFatebringer,
				Objectives: []Objective{{Progress: 3, Completion: 5}},
			},
		},
		"Timelost copy of a weapon with a complete pattern": {
			hash: itemTimelost,
			want: &Pattern{
				Name:       "Fatebringer",
				Hash:       recordFatebringer,
				Objectives: []Objective{{Progress: 3, Completion: 5}},
				Complete:   true,
			},
		},
		"Adept copy of a weapon whose name no pattern record has": {
			hash: itemAdeptNoPattern,
		},
		"version suffix outside the weapon buckets": {
			hash: itemHarrowedHelmet,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := NewSnapshot(patternProfile(t, tc.hash, `[]`, tc.state), patternDefs(), bungietest.Now)

			item, ok := s.Find("1")
			require.True(t, ok)
			assert.Equal(t, tc.want, item.Pattern)
		})
	}
}

// TestPatternHash pins the record of each item hash of [patternDefs]. The
// lookup follows the rules of [TestNewItemPattern], but it does not read
// a profile.
func TestPatternHash(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		hash uint32
		want uint32
	}{
		"craftable weapon": {
			hash: 100,
			want: recordFatebringer,
		},
		"definition without a recipe item": {
			hash: 102,
		},
		"item in the forbidden bucket": {
			hash: itemForbidden,
			want: recordForbidden,
		},
		"craftable weapon whose pattern record the profile might lack": {
			hash: 103,
			want: recordWithoutRecord,
		},
		"craftable weapon whose name no pattern record has": {
			hash: 104,
		},
		"Timelost version of a weapon with a pattern": {
			hash: itemTimelost,
			want: recordFatebringer,
		},
		"Adept version of a weapon whose name no pattern record has": {
			hash: itemAdeptNoPattern,
		},
		"version suffix outside the weapon buckets": {
			hash: itemHarrowedHelmet,
		},
		"hash that the definitions do not hold": {
			hash: 9999,
		},
	}

	defs := patternDefs()

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, ok := PatternHash(defs, tc.hash)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.want != 0, ok)
		})
	}
}

func TestNewItemPatternOfForbiddenBucket(t *testing.T) {
	t.Parallel()

	// The Titan did not play last, so the record of Forbidden Coup in its
	// records stays out of reach. A record of the profile reaches the item
	// in the forbidden bucket, although its definition names no recipe.
	p := patternProfile(t, itemForbidden, `[]`, 0)
	p.ProfileRecords.Data.Records[recordForbidden] = bungie.ProfileRecord{State: bungie.RecordObjectiveNotCompleted}

	item, ok := NewSnapshot(p, patternDefs(), bungietest.Now).Find("1")
	require.True(t, ok)
	assert.Equal(t, &Pattern{Name: "Forbidden Coup", Hash: recordForbidden}, item.Pattern)
}

func TestPatternRowsOfTimelostCopy(t *testing.T) {
	t.Parallel()

	// The Timelost copy names no recipe item, and the pattern record of
	// Fatebringer has the name of the base weapon.
	p := patternProfile(t, itemTimelost, `[{"plugHash": 910, "isVisible": true}]`, bungie.RecordObjectiveNotCompleted)

	rows, err := NewSnapshot(p, patternDefs(), bungietest.Now).PatternRows(PatternFilter{Query: "fatebringer"})
	require.NoError(t, err)
	assert.Equal(t, []string{"Fatebringer [1]"}, rowNames(rows))
	assert.True(t, rows[0].Copies[0].Deepsight)
}

func TestPatternRowsItemHashes(t *testing.T) {
	t.Parallel()

	// Item 99 is a second definition of Fatebringer that names a recipe
	// item. Item 98 is a version of Fatebringer that names none, as the
	// Onslaught version of a raid weapon does. The Timelost version names
	// none either, and so does Midnight Coup.
	defs := patternDefs()
	defs.Items[99] = defs.Items[100]

	version := defs.Items[100]
	version.Craftable = false
	defs.Items[98] = version

	rows, err := NewSnapshot(patternProfile(t, 100, `[]`, 0), defs, bungietest.Now).
		PatternRows(PatternFilter{Status: PatternAll})
	require.NoError(t, err)

	got := map[string][]uint32{}
	for _, row := range rows {
		got[row.Name] = row.ItemHashes
	}

	assert.Equal(t, map[string][]uint32{
		"Fatebringer":   {99, 100},
		"Gjallarhorn":   {101},
		"Midnight Coup": nil,
	}, got)
}

func TestNewSnapshotPatterns(t *testing.T) {
	t.Parallel()

	s := NewSnapshot(patternProfile(t, 100, `[]`, 0), patternDefs(), bungietest.Now)

	// Hawkmoon has no record, and the record of Forbidden Coup sits with
	// the Titan, which did not play last.
	assert.Equal(t, []Pattern{
		{
			Name:       "Fatebringer",
			Hash:       recordFatebringer,
			Objectives: []Objective{{Progress: 3, Completion: 5}},
			Complete:   true,
		},
		{Name: "Gjallarhorn", Hash: recordGjallarhorn, Objectives: []Objective{{Progress: 1, Completion: 2}}},
		{
			Name:       "Midnight Coup",
			Hash:       recordMidnightCoup,
			Objectives: []Objective{{Progress: 4, Completion: 4}},
			Complete:   true,
		},
	}, s.Patterns)
	assert.True(t, s.PatternsListed)

	unlisted := patternDefs()
	unlisted.PatternsListed = false

	assert.False(t, NewSnapshot(patternProfile(t, 100, `[]`, 0), unlisted, bungietest.Now).PatternsListed)
}

func TestPatternLeft(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		objectives []Objective
		want       int
	}{
		"one objective":                 {objectives: []Objective{{Progress: 3, Completion: 5}}, want: 2},
		"objective past its completion": {objectives: []Objective{{Progress: 6, Completion: 5}}},
		"several objectives": {
			objectives: []Objective{{Progress: 1, Completion: 2}, {Completion: 3}},
			want:       4,
		},
		"several objectives, one past its end": {
			objectives: []Objective{{Progress: 4, Completion: 2}, {Completion: 3}},
			want:       3,
		},
		"no objective": {},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, Pattern{Objectives: tc.objectives}.Left())
		})
	}
}

// rowsSnapshot returns a snapshot with five patterns and the copies of
// three weapons. Austringer needs one more extraction, and Fatebringer
// and Hawkmoon need two each. Midnight Coup and Sunshot are complete.
// Fatebringer has a copy with Deepsight Resonance and a copy that a
// Deepsight Harmonizer can give Deepsight Resonance. Hawkmoon has a copy
// of each kind too, and a stack without an instance. Austringer has no
// copy.
func rowsSnapshot() *Snapshot {
	pattern := func(name string, hash uint32, progress, completion int, complete bool) Pattern {
		return Pattern{
			Name:       name,
			Hash:       hash,
			Objectives: []Objective{{Progress: progress, Completion: completion}},
			Complete:   complete,
		}
	}

	fatebringer := pattern("Fatebringer", 1, 3, 5, false)
	hawkmoon := pattern("Hawkmoon", 2, 0, 2, false)
	coup := pattern("Midnight Coup", 3, 4, 4, true)

	s := &Snapshot{
		byID: map[string]*Item{},
		Patterns: []Pattern{
			pattern("Austringer", 4, 1, 2, false),
			fatebringer,
			hawkmoon,
			coup,
			pattern("Sunshot", 5, 3, 3, true),
		},
	}

	for _, item := range []*Item{
		{InstanceID: "13", Name: "Fatebringer", Pattern: &fatebringer, Harmonizable: true},
		{InstanceID: "11", Name: "Fatebringer", Pattern: &fatebringer, Deepsight: true},
		{InstanceID: "12", Name: "Fatebringer", Pattern: &fatebringer},
		{InstanceID: "21", Name: "Hawkmoon", Pattern: &hawkmoon, Deepsight: true, Harmonizable: true},
		{Name: "Hawkmoon", Pattern: &hawkmoon, Quantity: 2},
		{InstanceID: "31", Name: "Midnight Coup", Pattern: &coup},
		{InstanceID: "41", Name: "Gjallarhorn"},
	} {
		s.Add(item)
	}

	return s
}

// rowNames returns the name of each row, each followed by the instance
// ids of its copies in parentheses.
func rowNames(rows []PatternRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, fmt.Sprintf("%s %v", row.Name, ids(row.Copies)))
	}

	return out
}

func TestPatternRows(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err    error
		filter PatternFilter
		want   []string
	}{
		"zero filter selects the incomplete patterns, the fewest extractions left first": {
			want: []string{"Austringer []", "Fatebringer [11 12 13]", "Hawkmoon [21]"},
		},
		"incomplete status": {
			filter: PatternFilter{Status: "Incomplete"},
			want:   []string{"Austringer []", "Fatebringer [11 12 13]", "Hawkmoon [21]"},
		},
		"complete status orders by name": {
			filter: PatternFilter{Status: PatternComplete},
			want:   []string{"Midnight Coup [31]", "Sunshot []"},
		},
		"every status puts the complete patterns last": {
			filter: PatternFilter{Status: PatternAll},
			want: []string{
				"Austringer []", "Fatebringer [11 12 13]", "Hawkmoon [21]", "Midnight Coup [31]", "Sunshot []",
			},
		},
		"harmonizable copies": {
			filter: PatternFilter{Harmonizable: true},
			want:   []string{"Fatebringer [11 12 13]", "Hawkmoon [21]"},
		},
		"query that no name contains": {
			filter: PatternFilter{Query: "Austringer Prime", Status: PatternAll},
		},
		"query with a lookalike digit": {
			filter: PatternFilter{Query: "m1dn", Status: PatternAll},
			want:   []string{"Midnight Coup [31]"},
		},
		"query with accents": {
			filter: PatternFilter{Query: "fàte"},
			want:   []string{"Fatebringer [11 12 13]"},
		},
		"unknown status": {
			filter: PatternFilter{Status: "done"},
			err:    ErrInvalidFilter,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rows, err := rowsSnapshot().PatternRows(tc.filter)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)

				return
			}

			require.NoError(t, err)

			if tc.want == nil {
				assert.Empty(t, rows)

				return
			}

			assert.Equal(t, tc.want, rowNames(rows))
		})
	}
}
