package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeSheet(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		want *Sheet
		body string
	}{
		"row carries the category, the rank, the category size, and the note": {
			body: `{"rolls": [
				{"category": "HCs", "tier": "S", "rank": 3, "ranked": 62, "hashes": [1],
				 "notes": "Add clear.\n\nPairs with Firefly."}
			]}`,
			want: &Sheet{
				Rows: map[uint32]SheetRow{
					1: {Tier: "S", Category: "HCs", Notes: "Add clear.\n\nPairs with Firefly.", Rank: 3, Ranked: 62},
				},
				Columns:    map[uint32][][]uint32{},
				Versions:   map[uint32][]uint32{},
				SetBonuses: map[uint32]SetBonus{},
			},
		},
		"row without a rank has the rank 0": {
			body: `{"rolls": [
				{"category": "Exotic Weapons", "tier": "A", "ranked": 40, "hashes": [1], "notes": "Boss damage."}
			]}`,
			want: &Sheet{
				Rows: map[uint32]SheetRow{
					1: {Tier: "A", Category: "Exotic Weapons", Notes: "Boss damage.", Ranked: 40},
				},
				Columns:    map[uint32][][]uint32{},
				Versions:   map[uint32][]uint32{},
				SetBonuses: map[uint32]SetBonus{},
			},
		},
		"row without a note has an empty note": {
			body: `{"rolls": [{"category": "HCs", "tier": "S", "rank": 3, "ranked": 62, "hashes": [1]}]}`,
			want: &Sheet{
				Rows:       map[uint32]SheetRow{1: {Tier: "S", Category: "HCs", Rank: 3, Ranked: 62}},
				Columns:    map[uint32][][]uint32{},
				Versions:   map[uint32][]uint32{},
				SetBonuses: map[uint32]SetBonus{},
			},
		},
		"earlier row with the better tier keeps every field": {
			body: `{"rolls": [
				{"category": "HCs", "tier": "A", "rank": 3, "ranked": 62, "hashes": [1], "notes": "first"},
				{"category": "Reissues", "tier": "C", "rank": 7, "ranked": 9, "hashes": [1], "notes": "second"}
			]}`,
			want: &Sheet{
				Rows:       map[uint32]SheetRow{1: {Tier: "A", Category: "HCs", Notes: "first", Rank: 3, Ranked: 62}},
				Columns:    map[uint32][][]uint32{},
				Versions:   map[uint32][]uint32{},
				SetBonuses: map[uint32]SetBonus{},
			},
		},
		"later row with the better tier gives every field": {
			body: `{"rolls": [
				{"category": "HCs", "tier": "C", "rank": 3, "ranked": 62, "hashes": [1], "notes": "first"},
				{"category": "Reissues", "tier": "A", "rank": 7, "ranked": 9, "hashes": [1], "notes": "second"}
			]}`,
			want: &Sheet{
				Rows: map[uint32]SheetRow{
					1: {Tier: "A", Category: "Reissues", Notes: "second", Rank: 7, Ranked: 9},
				},
				Columns:    map[uint32][][]uint32{},
				Versions:   map[uint32][]uint32{},
				SetBonuses: map[uint32]SetBonus{},
			},
		},
		"later of two rows with equal tiers gives every field": {
			body: `{"rolls": [
				{"category": "HCs", "tier": "B", "rank": 3, "ranked": 62, "hashes": [1], "notes": "first"},
				{"category": "Reissues", "tier": "B", "rank": 7, "ranked": 9, "hashes": [1], "notes": "second"}
			]}`,
			want: &Sheet{
				Rows: map[uint32]SheetRow{
					1: {Tier: "B", Category: "Reissues", Notes: "second", Rank: 7, Ranked: 9},
				},
				Columns:    map[uint32][][]uint32{},
				Versions:   map[uint32][]uint32{},
				SetBonuses: map[uint32]SetBonus{},
			},
		},
		"later of two rows without a tier gives every field": {
			body: `{"rolls": [
				{"category": "HCs", "rank": 3, "ranked": 62, "hashes": [1], "notes": "first"},
				{"category": "Reissues", "rank": 7, "ranked": 9, "hashes": [1], "notes": "second"}
			]}`,
			want: &Sheet{
				Rows:       map[uint32]SheetRow{1: {Category: "Reissues", Notes: "second", Rank: 7, Ranked: 9}},
				Columns:    map[uint32][][]uint32{},
				Versions:   map[uint32][]uint32{},
				SetBonuses: map[uint32]SetBonus{},
			},
		},
		"row without a tier replaces a row with a tier outside S to F": {
			body: `{"rolls": [
				{"category": "HCs", "tier": "SS", "rank": 3, "ranked": 62, "hashes": [1], "notes": "first"},
				{"category": "Reissues", "rank": 7, "ranked": 9, "hashes": [1], "notes": "second"}
			]}`,
			want: &Sheet{
				Rows:       map[uint32]SheetRow{1: {Category: "Reissues", Notes: "second", Rank: 7, Ranked: 9}},
				Columns:    map[uint32][][]uint32{},
				Versions:   map[uint32][]uint32{},
				SetBonuses: map[uint32]SetBonus{},
			},
		},
		// The row that wins has no rank and no note. The hash does not
		// keep the rank or the note of the row that loses.
		"row with a tier replaces every field of a row without one": {
			body: `{"rolls": [
				{"category": "HCs", "rank": 3, "ranked": 62, "hashes": [1], "notes": "first"},
				{"category": "Reissues", "tier": "F", "ranked": 9, "hashes": [1]}
			]}`,
			want: &Sheet{
				Rows:       map[uint32]SheetRow{1: {Tier: "F", Category: "Reissues", Ranked: 9}},
				Columns:    map[uint32][][]uint32{},
				Versions:   map[uint32][]uint32{},
				SetBonuses: map[uint32]SetBonus{},
			},
		},
		"row without a tier replaces no field of a row with one": {
			body: `{"rolls": [
				{"category": "HCs", "tier": "F", "ranked": 62, "hashes": [1]},
				{"category": "Reissues", "rank": 7, "ranked": 9, "hashes": [1], "notes": "second"}
			]}`,
			want: &Sheet{
				Rows:       map[uint32]SheetRow{1: {Tier: "F", Category: "HCs", Ranked: 62}},
				Columns:    map[uint32][][]uint32{},
				Versions:   map[uint32][]uint32{},
				SetBonuses: map[uint32]SetBonus{},
			},
		},
		"lone row without a tier has an empty tier": {
			body: `{"rolls": [{"hashes": [1]}]}`,
			want: &Sheet{
				Rows:       map[uint32]SheetRow{1: {}},
				Columns:    map[uint32][][]uint32{},
				Versions:   map[uint32][]uint32{},
				SetBonuses: map[uint32]SetBonus{},
			},
		},
		"tier outside S to F reads as no tier": {
			body: `{"rolls": [
				{"tier": "G", "hashes": [1]},
				{"tier": "s", "hashes": [2]},
				{"tier": "A+", "hashes": [3]}
			]}`,
			want: &Sheet{
				Rows:       map[uint32]SheetRow{1: {}, 2: {}, 3: {}},
				Columns:    map[uint32][][]uint32{},
				Versions:   map[uint32][]uint32{},
				SetBonuses: map[uint32]SetBonus{},
			},
		},
		"row with a tier outside S to F does not replace a row with a tier": {
			body: `{"rolls": [
				{"tier": "F", "hashes": [1]},
				{"tier": "SS", "hashes": [1]}
			]}`,
			want: &Sheet{
				Rows:       map[uint32]SheetRow{1: {Tier: "F"}},
				Columns:    map[uint32][][]uint32{},
				Versions:   map[uint32][]uint32{},
				SetBonuses: map[uint32]SetBonus{},
			},
		},
		"first row with columns sets the columns": {
			body: `{"rolls": [
				{"tier": "B", "hashes": [1], "slots": [[10], [11, 12]]},
				{"tier": "B", "hashes": [1], "slots": [[13]]}
			]}`,
			want: &Sheet{
				Rows:       map[uint32]SheetRow{1: {Tier: "B"}},
				Columns:    map[uint32][][]uint32{1: {{10}, {11, 12}}},
				Versions:   map[uint32][]uint32{1: {1}},
				SetBonuses: map[uint32]SetBonus{},
			},
		},
		"row without columns sets none": {
			body: `{"rolls": [
				{"tier": "B", "hashes": [1, 2], "slots": []},
				{"tier": "B", "hashes": [3]},
				{"tier": "B", "hashes": [1], "slots": [[13]]}
			]}`,
			want: &Sheet{
				Rows:       map[uint32]SheetRow{1: {Tier: "B"}, 2: {Tier: "B"}, 3: {Tier: "B"}},
				Columns:    map[uint32][][]uint32{1: {{13}}},
				Versions:   map[uint32][]uint32{1: {1}},
				SetBonuses: map[uint32]SetBonus{},
			},
		},
		"tier and columns of one hash come from different rows": {
			body: `{"rolls": [
				{"tier": "D", "hashes": [1], "slots": [[10], [11]]},
				{"tier": "S", "hashes": [1], "slots": [[13]]}
			]}`,
			want: &Sheet{
				Rows:       map[uint32]SheetRow{1: {Tier: "S"}},
				Columns:    map[uint32][][]uint32{1: {{10}, {11}}},
				Versions:   map[uint32][]uint32{1: {1}},
				SetBonuses: map[uint32]SetBonus{},
			},
		},
		"fields and columns of one hash come from different rows": {
			body: `{"rolls": [
				{"category": "HCs", "tier": "D", "rank": 3, "ranked": 62, "hashes": [1],
				 "slots": [[10], [11]], "notes": "first"},
				{"category": "Reissues", "tier": "S", "rank": 7, "ranked": 9, "hashes": [1], "notes": "second"}
			]}`,
			want: &Sheet{
				Rows: map[uint32]SheetRow{
					1: {Tier: "S", Category: "Reissues", Notes: "second", Rank: 7, Ranked: 9},
				},
				Columns:    map[uint32][][]uint32{1: {{10}, {11}}},
				Versions:   map[uint32][]uint32{1: {1}},
				SetBonuses: map[uint32]SetBonus{},
			},
		},
		// The sheet file has more fields than the decode reads.
		"one row with two hashes gives both hashes the same fields": {
			body: `{"source": "aegis", "captured": "2026-08-22", "setBonuses": [],
				"rolls": [
					{"name": "Reissued", "category": "Pulses", "tier": "B", "rank": 2, "ranked": 59,
					 "hashes": [1, 2], "slots": [[10], [11]], "notes": "both", "season": "Echoes"}
				],
				"perks": [{"name": "Slice", "hashes": [10], "kind": "perk", "rank": 9}]}`,
			want: &Sheet{
				Rows: map[uint32]SheetRow{
					1: {Tier: "B", Category: "Pulses", Notes: "both", Rank: 2, Ranked: 59},
					2: {Tier: "B", Category: "Pulses", Notes: "both", Rank: 2, Ranked: 59},
				},
				Columns:    map[uint32][][]uint32{1: {{10}, {11}}, 2: {{10}, {11}}},
				Versions:   map[uint32][]uint32{1: {1, 2}, 2: {1, 2}},
				SetBonuses: map[uint32]SetBonus{},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := decodeSheet(strings.NewReader(tc.body))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDecodeSheetSetBonuses(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		want map[uint32]SetBonus
		// bonuses holds the setBonuses array of the sheet file.
		bonuses string
	}{
		"a bonus keeps its tier and pieces": {
			bonuses: `[{"name": "Iron Truage", "hash": 10, "set": "Iron", "pieces": 4, "rank": 1, "tier": "S"}]`,
			want:    map[uint32]SetBonus{10: {Tier: "S", Pieces: 4}},
		},
		"a later bonus with the same hash replaces the earlier one": {
			bonuses: `[{"hash": 10, "pieces": 2, "tier": "C"}, {"hash": 10, "pieces": 4, "tier": "A"}]`,
			want:    map[uint32]SetBonus{10: {Tier: "A", Pieces: 4}},
		},
		"a lowercase tier reads as its uppercase form": {
			bonuses: `[{"hash": 10, "pieces": 2, "tier": "s"}]`,
			want:    map[uint32]SetBonus{10: {Tier: "S", Pieces: 2}},
		},
		"a tier outside S to F reads as no tier": {
			bonuses: `[{"hash": 10, "pieces": 2, "tier": "x"}, {"hash": 11, "pieces": 4, "tier": "SS"}]`,
			want:    map[uint32]SetBonus{10: {Pieces: 2}, 11: {Pieces: 4}},
		},
		"a bonus without a tier has no tier": {
			bonuses: `[{"hash": 10, "pieces": 2, "tier": null}, {"hash": 11, "pieces": 4}]`,
			want:    map[uint32]SetBonus{10: {Pieces: 2}, 11: {Pieces: 4}},
		},
		// The lowercase form of U+017F is U+017F itself, so the web client
		// does not read it as the tier S.
		"a letter that folds to s is not a tier": {
			bonuses: `[{"hash": 10, "pieces": 2, "tier": "\u017f"}]`,
			want:    map[uint32]SetBonus{10: {Pieces: 2}},
		},
		"no bonus": {
			bonuses: `[]`,
			want:    map[uint32]SetBonus{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			body := `{"rolls": [{"tier": "S", "hashes": [1]}], "setBonuses": ` + tc.bonuses + `}`

			got, err := decodeSheet(strings.NewReader(body))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.SetBonuses)
		})
	}
}

func TestDecodeSheetVersions(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		want map[uint32][]uint32
		body string
	}{
		"each hash of a row with columns gets the hashes of the row": {
			body: `{"rolls": [{"tier": "B", "hashes": [1, 2], "slots": [[10]]}]}`,
			want: map[uint32][]uint32{1: {1, 2}, 2: {1, 2}},
		},
		"a hash first listed on a row without columns gets the row with columns": {
			body: `{"rolls": [
				{"tier": "A", "hashes": [1, 3], "slots": []},
				{"tier": "B", "hashes": [1, 2], "slots": [[10]]}
			]}`,
			want: map[uint32][]uint32{1: {1, 2}, 2: {1, 2}},
		},
		"the first row with columns gives the versions": {
			body: `{"rolls": [
				{"tier": "B", "hashes": [1, 2], "slots": [[10]]},
				{"tier": "S", "hashes": [1, 3], "slots": [[11]]}
			]}`,
			want: map[uint32][]uint32{1: {1, 2}, 2: {1, 2}, 3: {1, 3}},
		},
		"a hash listed only on a row without columns has no versions": {
			body: `{"rolls": [{"tier": "A", "hashes": [1], "slots": []}, {"tier": "A", "hashes": [2]}]}`,
			want: map[uint32][]uint32{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := decodeSheet(strings.NewReader(tc.body))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.Versions)
		})
	}
}

func TestDecodeSheetErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		// err is a sentinel that the error wraps. It is nil for a body
		// that encoding/json rejects with an error of its own type.
		err  error
		body string
		// want opens the text of the error.
		want string
	}{
		"body is an array": {
			body: `[]`,
			want: "reading roll sheet: ",
		},
		"body ends early": {
			body: `{"rolls": [{"tier": "S", "hashes": [1]`,
			err:  io.ErrUnexpectedEOF,
			want: "reading roll sheet: ",
		},
		"category is a number": {
			body: `{"rolls": [{"category": 7, "tier": "S", "hashes": [1]}]}`,
			want: "reading roll sheet: ",
		},
		"notes is a list": {
			body: `{"rolls": [{"tier": "S", "hashes": [1], "notes": ["Add clear."]}]}`,
			want: "reading roll sheet: ",
		},
		"rank is a string": {
			body: `{"rolls": [{"tier": "S", "rank": "3", "ranked": 62, "hashes": [1]}]}`,
			want: "reading roll sheet: ",
		},
		"ranked is a string": {
			body: `{"rolls": [{"tier": "S", "rank": 3, "ranked": "62", "hashes": [1]}]}`,
			want: "reading roll sheet: ",
		},
		"set bonus hash is a string": {
			body: `{"rolls": [{"tier": "S", "hashes": [1]}], "setBonuses": [{"hash": "10", "tier": "S"}]}`,
			want: "reading roll sheet: ",
		},
		"body is an empty object": {
			body: `{}`,
			err:  errEmptySheet,
			want: "the roll sheet lists no weapon",
		},
		"rolls array is empty": {
			body: `{"rolls": []}`,
			err:  errEmptySheet,
			want: "the roll sheet lists no weapon",
		},
		"rows list no hash": {
			body: `{"rolls": [{"tier": "S", "hashes": [], "slots": [[10]]}, {"tier": "A"}]}`,
			err:  errEmptySheet,
			want: "the roll sheet lists no weapon",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := decodeSheet(strings.NewReader(tc.body))
			require.Error(t, err)
			assert.Nil(t, got)
			assert.Truef(t, strings.HasPrefix(err.Error(), tc.want), "got %q", err)

			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			}
		})
	}
}

func TestTierRank(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		tier string
		want int
	}{
		"best tier":    {tier: "S", want: 0},
		"worst tier":   {tier: "F", want: 6},
		"missing tier": {tier: "", want: 7},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, tierRank(tc.tier))
		})
	}
}

// fakeSheets is an [ArtifactSource] for the store tests. It wraps
// [fakeArtifacts] and counts the Config calls. A set configErr makes
// Config return that error.
type fakeSheets struct {
	*fakeArtifacts

	configErr error
	configs   int
}

// newFakeSheets returns a source that serves four sheet files and whose
// index lists none of them. Each file gives weapon 100 its own tier, so a
// test can tell the loaded files apart.
func newFakeSheets() *fakeSheets {
	f := &fakeSheets{fakeArtifacts: newFakeArtifacts()}

	f.files["rolls.a.json"] = sheetBody("S")
	f.files["rolls.b.json"] = sheetBody("B")
	f.files["pvprolls.a.json"] = sheetBody("A")
	f.files["pvprolls.b.json"] = sheetBody("C")

	return f
}

func (f *fakeSheets) Config(ctx context.Context) (ServiceConfig, error) {
	f.configs++

	if f.configErr != nil {
		return ServiceConfig{}, f.configErr
	}

	return f.fakeArtifacts.Config(ctx)
}

// sheetBody returns a sheet file body that gives weapon 100 the tier.
func sheetBody(tier string) string {
	return fmt.Sprintf(`{"rolls": [{"tier": %q, "hashes": [100], "slots": [[900]]}]}`, tier)
}

// wantState is the expected [SheetState] of one game mode. A set err is
// the text of Err, and sentinels lists the errors that Err wraps besides
// [ErrNoRating]. Without err, tier is the tier that the sheet of the state
// gives weapon 100. Kept expects the sheet that the store returned for the
// mode the last time that the mode had one.
type wantState struct {
	err       string
	tier      string
	sentinels []error
	kept      bool
}

// loadStep is one Load of a store case. Edit changes the source before the
// Load. Fetched lists the files that the Load fetches, and want holds the
// expected state of each game mode.
type loadStep struct {
	edit    func(f *fakeSheets)
	want    map[string]wantState
	fetched []string
}

func TestSheetStoreLoad(t *testing.T) {
	t.Parallel()

	const (
		noAegis    = "rating unavailable: the service publishes no Aegis sheet"
		noPvP      = "rating unavailable: the service publishes no PvP sheet"
		noDownload = "rating unavailable: fetching artifact rolls.x.json: service"
		noDecode   = "rating unavailable: decoding artifact rolls.x.json: reading roll sheet: unexpected EOF"
		noConfig   = "rating unavailable: reading service config: service: /api/config returned status 503"
	)

	// both lists one file for each sheet and loads the two files.
	both := loadStep{
		edit: func(f *fakeSheets) {
			f.index["rolls"] = []string{"rolls.a.json"}
			f.index["pvprolls"] = []string{"pvprolls.a.json"}
		},
		fetched: []string{"rolls.a.json", "pvprolls.a.json"},
		want:    map[string]wantState{modePvE: {tier: "S"}, modePvP: {tier: "A"}},
	}

	// kept expects the two sheets of both again, without a fetch.
	kept := map[string]wantState{modePvE: {tier: "S", kept: true}, modePvP: {tier: "A", kept: true}}

	// dropped removes the Aegis sheet from the index.
	dropped := loadStep{
		edit: func(f *fakeSheets) { delete(f.index, "rolls") },
		want: map[string]wantState{modePvE: {err: noAegis}, modePvP: {tier: "A", kept: true}},
	}

	// missing lists an Aegis file that the source does not serve.
	missing := loadStep{
		edit: func(f *fakeSheets) {
			f.index["rolls"] = []string{"rolls.x.json"}
			f.index["pvprolls"] = []string{"pvprolls.a.json"}
		},
		fetched: []string{"rolls.x.json", "pvprolls.a.json"},
		want: map[string]wantState{
			modePvE: {err: noDownload, sentinels: []error{ErrService}},
			modePvP: {tier: "A"},
		},
	}

	// replaced moves the loaded Aegis sheet to a file that the source does
	// not serve.
	replaced := loadStep{
		edit:    func(f *fakeSheets) { f.index["rolls"] = []string{"rolls.x.json"} },
		fetched: []string{"rolls.x.json"},
		want: map[string]wantState{
			modePvE: {err: noDownload, sentinels: []error{ErrService}},
			modePvP: {tier: "A", kept: true},
		},
	}

	tests := map[string]struct {
		steps []loadStep
	}{
		"index lists neither sheet": {
			steps: []loadStep{{
				want: map[string]wantState{modePvE: {err: noAegis}, modePvP: {err: noPvP}},
			}},
		},
		"both sheets load": {
			steps: []loadStep{both},
		},
		"second load returns the same sheets": {
			steps: []loadStep{both, {want: kept}},
		},
		"another file name for one mode fetches only that file": {
			steps: []loadStep{both, {
				edit:    func(f *fakeSheets) { f.index["pvprolls"] = []string{"pvprolls.b.json"} },
				fetched: []string{"pvprolls.b.json"},
				want:    map[string]wantState{modePvE: {tier: "S", kept: true}, modePvP: {tier: "C"}},
			}},
		},
		"index lists two files for one sheet": {
			steps: []loadStep{{
				edit: func(f *fakeSheets) {
					f.index["rolls"] = []string{"rolls.a.json", "rolls.b.json"}
					f.index["pvprolls"] = []string{"pvprolls.a.json"}
				},
				fetched: []string{"rolls.a.json", "pvprolls.a.json"},
				want:    map[string]wantState{modePvE: {tier: "S"}, modePvP: {tier: "A"}},
			}},
		},
		"index drops a listed sheet": {
			steps: []loadStep{both, dropped},
		},
		"index lists the kept file again after it dropped the sheet": {
			steps: []loadStep{both, dropped, {
				edit: func(f *fakeSheets) { f.index["rolls"] = []string{"rolls.a.json"} },
				want: kept,
			}},
		},
		"download of one sheet does not succeed": {
			steps: []loadStep{missing},
		},
		"load after that download fetches the file again": {
			steps: []loadStep{missing, {
				edit:    func(f *fakeSheets) { f.files["rolls.x.json"] = sheetBody("D") },
				fetched: []string{"rolls.x.json"},
				want:    map[string]wantState{modePvE: {tier: "D"}, modePvP: {tier: "A", kept: true}},
			}},
		},
		"body of one sheet does not decode": {
			steps: []loadStep{
				{
					edit: func(f *fakeSheets) {
						f.index["rolls"] = []string{"rolls.x.json"}
						f.index["pvprolls"] = []string{"pvprolls.a.json"}
						f.files["rolls.x.json"] = `{"rolls": [`
					},
					fetched: []string{"rolls.x.json", "pvprolls.a.json"},
					want: map[string]wantState{
						modePvE: {err: noDecode, sentinels: []error{io.ErrUnexpectedEOF}},
						modePvP: {tier: "A"},
					},
				},
				{
					fetched: []string{"rolls.x.json"},
					want: map[string]wantState{
						modePvE: {err: noDecode, sentinels: []error{io.ErrUnexpectedEOF}},
						modePvP: {tier: "A", kept: true},
					},
				},
			},
		},
		"replacement file of a loaded sheet does not load": {
			steps: []loadStep{both, replaced},
		},
		"index names the older file again after that replacement": {
			steps: []loadStep{both, replaced, {
				edit: func(f *fakeSheets) { f.index["rolls"] = []string{"rolls.a.json"} },
				want: kept,
			}},
		},
		"config read does not succeed": {
			steps: []loadStep{
				both,
				{
					edit: func(f *fakeSheets) {
						f.configErr = fmt.Errorf("%w: /api/config returned status 503", ErrService)
					},
					want: map[string]wantState{
						modePvE: {err: noConfig, sentinels: []error{ErrService}},
						modePvP: {err: noConfig, sentinels: []error{ErrService}},
					},
				},
				{
					edit: func(f *fakeSheets) { f.configErr = nil },
					want: kept,
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := newFakeSheets()
			store := NewSheetStore(source)
			last := map[string]*Sheet{}

			for i, step := range tc.steps {
				if step.edit != nil {
					step.edit(source)
				}

				source.fetched = nil

				got := store.Load(t.Context())
				assert.Equalf(t, step.fetched, source.fetched, "fetches of load %d", i)
				assert.Equalf(t, i+1, source.configs, "every load reads the index, load %d", i)
				require.Lenf(t, got, len(perkModes), "load %d", i)

				for _, mode := range perkModes {
					state := got[mode]
					want := step.want[mode]

					require.NotEqualf(t, state.Sheet == nil, state.Err == nil,
						"exactly one field of the %s state is set, load %d", mode, i)

					if want.err != "" {
						require.EqualErrorf(t, state.Err, want.err, "%s, load %d", mode, i)
						require.ErrorIsf(t, state.Err, ErrNoRating, "%s, load %d", mode, i)

						for _, target := range want.sentinels {
							require.ErrorIsf(t, state.Err, target, "%s, load %d", mode, i)
						}

						continue
					}

					require.NoErrorf(t, state.Err, "%s, load %d", mode, i)
					assert.Equalf(t, want.tier, state.Sheet.Rows[100].Tier, "%s, load %d", mode, i)

					if want.kept {
						assert.Samef(t, last[mode], state.Sheet, "%s, load %d", mode, i)
					}

					last[mode] = state.Sheet
				}
			}
		})
	}
}
