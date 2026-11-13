package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// loadoutTable is the name of a definition table of the Destiny manifest
// that identifies saved loadouts. Each saved loadout carries a hash into
// each of these tables.
type loadoutTable string

// Loadout tables of the Destiny manifest. The definition artifacts of the
// service leave them out.
const (
	loadoutNameTable  loadoutTable = "DestinyLoadoutNameDefinition"
	loadoutIconTable  loadoutTable = "DestinyLoadoutIconDefinition"
	loadoutColorTable loadoutTable = "DestinyLoadoutColorDefinition"
)

// kind returns the word that messages use for an entry of the table.
func (t loadoutTable) kind() string {
	switch t {
	case loadoutIconTable:
		return "icon"
	case loadoutColorTable:
		return "color"
	default:
		return "name"
	}
}

var (
	// errNoTable reports a manifest that lists no English file of a
	// loadout table.
	errNoTable = errors.New("the Bungie manifest lists no table")

	// errEmptyTable reports a loadout table file that lists no entry.
	errEmptyTable = errors.New("the table lists no entry")

	// errNoShownEntry reports a loadout table whose entries are all
	// redacted.
	errNoShownEntry = errors.New("the table holds no entry that the game shows")
)

// ManifestSource serves the Destiny manifest and its content files. The
// handler adapts [*Bungie] to it.
type ManifestSource interface {
	Manifest(ctx context.Context) (map[string]string, error)
	Content(ctx context.Context, path string) (io.ReadCloser, error)
}

// LoadoutDefinition is one entry of a loadout table, such as a Bungie
// DestinyLoadoutNameDefinition. Index is the position of the entry in the
// list that the game offers. Redacted marks an entry that Bungie does not
// show yet. Name holds the text of a name entry, and a redacted name might
// be empty. The game defines each icon and each color only as an image,
// so Name is empty in their entries.
type LoadoutDefinition struct {
	Name     string `json:"name"`
	Index    int    `json:"index"`
	Redacted bool   `json:"redacted"`
}

// LoadoutTableState is one loadout table, or the reason that the server
// has none. Exactly one field is set. The hash keys Table, and the table
// holds every entry of the file, redacted ones included.
type LoadoutTableState struct {
	Table map[uint32]LoadoutDefinition
	Err   error
}

// decodeLoadoutTable reads the body of the file of table t. The file is a
// JSON object that maps each hash to its definition.
//
// A loaded name table without names reports every loadout as unnamed. An
// empty file does not support that claim, so the decode rejects it.
func decodeLoadoutTable(r io.Reader, t loadoutTable) (map[uint32]LoadoutDefinition, error) {
	var table map[uint32]LoadoutDefinition

	err := json.NewDecoder(r).Decode(&table)
	if err != nil {
		return nil, fmt.Errorf("reading the loadout %s table: %w", t.kind(), err)
	}

	if len(table) == 0 {
		return nil, errEmptyTable
	}

	return table, nil
}

// shownLoadoutEntries returns the hashes of the entries of table that are
// not redacted, ordered by index. The game offers those entries in that
// order. Entries with the same index follow their hashes.
func shownLoadoutEntries(table map[uint32]LoadoutDefinition) []uint32 {
	var out []uint32

	for hash, entry := range table {
		if !entry.Redacted {
			out = append(out, hash)
		}
	}

	slices.SortFunc(out, func(a, b uint32) int {
		return cmp.Or(cmp.Compare(table[a].Index, table[b].Index), cmp.Compare(a, b))
	})

	return out
}

// firstLoadoutEntry returns the hash of the first entry of table, which is
// the entry with the lowest index among the entries that are not
// redacted. Destiny Item Manager gives the same entry to a loadout that
// it saves into an empty slot.
func firstLoadoutEntry(table map[uint32]LoadoutDefinition) (uint32, error) {
	shown := shownLoadoutEntries(table)
	if len(shown) == 0 {
		return 0, errNoShownEntry
	}

	return shown[0], nil
}

// findLoadoutName returns the hash of the entry of the name table whose
// name is name in any letter case. Only the entries that are not redacted
// count, and the first match in index order wins.
func findLoadoutName(table map[uint32]LoadoutDefinition, name string) (uint32, bool) {
	for _, hash := range shownLoadoutEntries(table) {
		if strings.EqualFold(table[hash].Name, name) {
			return hash, true
		}
	}

	return 0, false
}

// loadoutNameChoices returns the names of the entries of the name table
// that the game shows, in index order.
func loadoutNameChoices(table map[uint32]LoadoutDefinition) []string {
	var out []string

	for _, hash := range shownLoadoutEntries(table) {
		if name := table[hash].Name; name != "" {
			out = append(out, name)
		}
	}

	return out
}

// loadedTable is the last version of one loadout table that a
// [*LoadoutTableStore] loaded, with the content path of its file.
type loadedTable struct {
	table map[uint32]LoadoutDefinition
	path  string
}

// LoadoutTableStore loads the loadout tables from the Destiny manifest
// that Bungie publishes. The definition artifacts of the service leave
// those tables out.
//
// Each content path carries an identifier that Bungie changes with every
// manifest release, so the path of a file identifies a version of its
// table. The store keeps the last version of each table that it loaded in
// memory, with its path. It downloads a table only when the manifest lists
// another path for it.
//
// The store has no lock, so the caller serializes the calls to Load.
type LoadoutTableStore struct {
	source ManifestSource
	loaded map[loadoutTable]loadedTable
}

// NewLoadoutTableStore returns a [*LoadoutTableStore] that reads the
// manifest from source.
func NewLoadoutTableStore(source ManifestSource) *LoadoutTableStore {
	return &LoadoutTableStore{source: source, loaded: map[loadoutTable]loadedTable{}}
}

// Load returns the [LoadoutTableState] of each table in tables at the time
// of the call, keyed by table.
//
// It reads the manifest once on every call, so each state follows the
// manifest that Bungie publishes. When that read does not succeed, every
// state gives its reason. A table that does not load leaves its kept
// version and path as they were, so the next call tries again. That call
// fetches the file unless the manifest lists the kept path. Load never
// returns the table of one path while the manifest lists another.
func (s *LoadoutTableStore) Load(ctx context.Context, tables ...loadoutTable) map[loadoutTable]LoadoutTableState {
	out := make(map[loadoutTable]LoadoutTableState, len(tables))

	paths, err := s.source.Manifest(ctx)
	if err != nil {
		err = fmt.Errorf("reading the Bungie manifest: %w", err)

		for _, t := range tables {
			out[t] = LoadoutTableState{Err: err}
		}

		return out
	}

	for _, t := range tables {
		out[t] = s.loadTable(ctx, t, paths[string(t)])
	}

	return out
}

// loadTable returns the state of table t, whose file the manifest lists at
// path. An empty path means that the manifest lists no file.
func (s *LoadoutTableStore) loadTable(ctx context.Context, t loadoutTable, path string) LoadoutTableState {
	if path == "" {
		return LoadoutTableState{Err: fmt.Errorf("%w of loadout %ss", errNoTable, t.kind())}
	}

	if kept, ok := s.loaded[t]; ok && kept.path == path {
		return LoadoutTableState{Table: kept.table}
	}

	body, err := s.source.Content(ctx, path)
	if err != nil {
		return LoadoutTableState{Err: fmt.Errorf("fetching %s: %w", path, err)}
	}

	defer closeBody(ctx, body, path)

	table, err := decodeLoadoutTable(body, t)
	if err != nil {
		return LoadoutTableState{Err: fmt.Errorf("decoding %s: %w", path, err)}
	}

	s.loaded[t] = loadedTable{table: table, path: path}

	return LoadoutTableState{Table: table}
}
