package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// Game modes of the roll sheets and the perk preferences.
const (
	modePvE = "pve"
	modePvP = "pvp"
)

var (
	// tiers lists the tiers of the roll sheets, best first.
	tiers = []string{"S", "A", "B", "C", "D", "E", "F"}

	// lowerTiers maps the lowercase form of each tier of tiers to the
	// tier.
	lowerTiers = lowercaseTiers()

	// sheetArtifacts names the artifact that holds the roll sheet of each
	// game mode.
	sheetArtifacts = map[string]string{modePvE: "rolls", modePvP: "pvprolls"}

	// modeLabels names each game mode for display.
	modeLabels = map[string]string{modePvE: "PvE", modePvP: "PvP"}

	// sheetLabels names the roll sheet of each game mode for display.
	sheetLabels = map[string]string{modePvE: "Aegis sheet", modePvP: "PvP sheet"}

	// ErrNoRating marks the reason that a game mode has no roll ratings.
	// The rating line of the mode prints the text of the error that wraps
	// it.
	ErrNoRating = errors.New("rating unavailable")

	// errEmptySheet reports a roll sheet file that lists no weapon.
	errEmptySheet = errors.New("the roll sheet lists no weapon")
)

// SheetRow is the row of one weapon on a roll sheet. Tier is empty on a
// row without a tier. Category names the list of the sheet that holds
// the weapon, in the text of the sheet file. Rank is the position of the
// weapon in that category, best first, and a zero Rank marks a row
// without a rank. Ranked counts the rows of the category, with and
// without a rank. Notes is the note that the sheet gives the weapon. It
// can hold several lines, and it is empty on a row without a note.
type SheetRow struct {
	Tier     string
	Category string
	Notes    string
	Rank     int
	Ranked   int
}

// SetBonus is the rating that a roll sheet gives one armor set bonus.
// Tier is a tier of [tiers], or empty for a bonus without a tier. Pieces
// is the number of pieces of the set that the bonus needs, such as 2 or
// 4.
type SetBonus struct {
	Tier   string
	Pieces int
}

// Sheet is one roll sheet. The item hash of a weapon keys Rows, Columns,
// and Versions. Each column lists the plug hashes that the sheet picks for
// one perk column. Versions holds, for each hash with columns, the hashes
// of the row that gives the hash its columns. The hash itself is one of
// them. SetBonuses holds the rated set bonuses, keyed by the sandbox perk
// hash of each bonus.
type Sheet struct {
	Rows       map[uint32]SheetRow
	Columns    map[uint32][][]uint32
	Versions   map[uint32][]uint32
	SetBonuses map[uint32]SetBonus
}

// SheetState is the sheet of one game mode, or the reason that the
// server has none. Exactly one field is set.
type SheetState struct {
	Sheet *Sheet
	Err   error
}

// rawSheet is the subset of a roll sheet file that [decodeSheet] reads.
type rawSheet struct {
	Rolls []struct {
		Category string     `json:"category"`
		Tier     string     `json:"tier"`
		Notes    string     `json:"notes"`
		Hashes   []uint32   `json:"hashes"`
		Slots    [][]uint32 `json:"slots"`
		Rank     int        `json:"rank"`
		Ranked   int        `json:"ranked"`
	} `json:"rolls"`
	SetBonuses []struct {
		Tier   string `json:"tier"`
		Hash   uint32 `json:"hash"`
		Pieces int    `json:"pieces"`
	} `json:"setBonuses"`
}

// decodeSheet reads the body of one roll sheet file.
//
// A reissue shares a row with the original weapon, so two rows can list
// one hash. The row with the better tier gives the hash its [SheetRow],
// and the later of two rows with equal tiers replaces the earlier one.
// Every field of the SheetRow of one hash therefore comes from one row
// of the file. The first row with perk columns gives the hash its
// columns, and the hashes of that row are the versions of the hash. The
// SheetRow and the columns of one hash can therefore come from different
// rows.
//
// A later set bonus with the hash of an earlier one replaces it, as the
// web client does. The web client lowercases the tier of a set bonus
// before it compares the tier. The decode therefore keeps the tier of
// [tiers] whose lowercase form equals the lowercase form of the text,
// and an empty tier for any other text.
func decodeSheet(r io.Reader) (*Sheet, error) {
	var raw rawSheet

	err := json.NewDecoder(r).Decode(&raw)
	if err != nil {
		return nil, fmt.Errorf("reading roll sheet: %w", err)
	}

	sheet := &Sheet{
		Rows:       map[uint32]SheetRow{},
		Columns:    map[uint32][][]uint32{},
		Versions:   map[uint32][]uint32{},
		SetBonuses: map[uint32]SetBonus{},
	}

	for _, roll := range raw.Rolls {
		row := SheetRow{
			Tier:     roll.Tier,
			Category: roll.Category,
			Notes:    roll.Notes,
			Rank:     roll.Rank,
			Ranked:   roll.Ranked,
		}

		// The service build writes only the listed tiers. Any other
		// text counts as no tier, so a row never shows a tier that the
		// ranking does not know.
		if !slices.Contains(tiers, row.Tier) {
			row.Tier = ""
		}

		for _, hash := range roll.Hashes {
			old, ok := sheet.Rows[hash]
			if !ok || tierRank(row.Tier) <= tierRank(old.Tier) {
				sheet.Rows[hash] = row
			}
		}

		// The row of an exotic has no perk columns.
		if len(roll.Slots) == 0 {
			continue
		}

		for _, hash := range roll.Hashes {
			_, ok := sheet.Columns[hash]
			if !ok {
				sheet.Columns[hash] = roll.Slots
				sheet.Versions[hash] = roll.Hashes
			}
		}
	}

	for _, bonus := range raw.SetBonuses {
		sheet.SetBonuses[bonus.Hash] = SetBonus{Tier: lowerTiers[strings.ToLower(bonus.Tier)], Pieces: bonus.Pieces}
	}

	// A loaded sheet without rows reports every weapon as missing from
	// the sheet. An empty file does not support that claim, so the decode
	// rejects it.
	if len(sheet.Rows) == 0 {
		return nil, errEmptySheet
	}

	return sheet, nil
}

// lowercaseTiers returns the map of lowerTiers. A lookup of a lowercase
// text in the map matches what the web client matches. Folding the case
// of the text matches more, because it also reads U+017F as s.
func lowercaseTiers() map[string]string {
	lower := make(map[string]string, len(tiers))
	for _, tier := range tiers {
		lower[strings.ToLower(tier)] = tier
	}

	return lower
}

// tierRank returns the position of tier in [tiers], best first. A missing
// tier ranks after F.
func tierRank(tier string) int {
	i := slices.Index(tiers, tier)
	if i < 0 {
		return len(tiers)
	}

	return i
}

// SheetStore loads the roll sheets from the artifacts of the service.
//
// The service build names each artifact file after a hash of its content,
// so the file name identifies a sheet. The store keeps the last sheet
// that it loaded for each game mode in memory, with its file name. It
// downloads a sheet only when the index lists another file name.
//
// The store has no lock, so the caller serializes the calls to Load.
type SheetStore struct {
	source ArtifactSource
	loaded map[string]loadedSheet
}

// loadedSheet is a sheet that a [*SheetStore] keeps, with the name of the
// file that holds it.
type loadedSheet struct {
	sheet *Sheet
	file  string
}

// NewSheetStore returns a [*SheetStore] that reads the roll sheets from
// source.
func NewSheetStore(source ArtifactSource) *SheetStore {
	return &SheetStore{source: source, loaded: map[string]loadedSheet{}}
}

// Load returns the [SheetState] of each game mode, keyed by the mode.
//
// It reads the artifact index of the service on every call, so the states
// follow the index at the time of the call. Every Err wraps [ErrNoRating].
// The store does not remember a load that did not succeed, so the next
// call fetches the file again. It also never returns the sheet of one
// file name while the index lists another.
func (s *SheetStore) Load(ctx context.Context) map[string]SheetState {
	states := make(map[string]SheetState, len(perkModes))

	cfg, err := s.source.Config(ctx)
	if err != nil {
		err = fmt.Errorf("%w: reading service config: %w", ErrNoRating, err)

		for _, mode := range perkModes {
			states[mode] = SheetState{Err: err}
		}

		return states
	}

	for _, mode := range perkModes {
		sheet, err := s.load(ctx, mode, cfg.Artifacts.Files[sheetArtifacts[mode]])
		if err != nil {
			states[mode] = SheetState{Err: fmt.Errorf("%w: %w", ErrNoRating, err)}

			continue
		}

		states[mode] = SheetState{Sheet: sheet}
	}

	return states
}

// load returns the sheet of one game mode. Files is the file list that
// the index gives the artifact of the mode. The build writes a sheet as
// one file, so load reads only the first.
func (s *SheetStore) load(ctx context.Context, mode string, files []string) (*Sheet, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("the service publishes no %s", sheetLabels[mode])
	}

	file := files[0]

	kept, ok := s.loaded[mode]
	if ok && kept.file == file {
		return kept.sheet, nil
	}

	body, err := s.source.Artifact(ctx, file)
	if err != nil {
		return nil, fmt.Errorf("fetching artifact %s: %w", file, err)
	}

	defer closeBody(ctx, body, file)

	sheet, err := decodeSheet(body)
	if err != nil {
		return nil, fmt.Errorf("decoding artifact %s: %w", file, err)
	}

	s.loaded[mode] = loadedSheet{sheet: sheet, file: file}

	return sheet, nil
}
