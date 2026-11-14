package bungie

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/httpbody"
)

// LoadoutTable is the name of a definition table of the Destiny manifest
// that identifies saved loadouts. Each saved loadout carries a hash into
// each of these tables.
type LoadoutTable string

// Loadout tables of the Destiny manifest. The definition artifacts of
// Agg's Destiny Vault Manager leave them out.
const (
	LoadoutNameTable  LoadoutTable = "DestinyLoadoutNameDefinition"
	LoadoutIconTable  LoadoutTable = "DestinyLoadoutIconDefinition"
	LoadoutColorTable LoadoutTable = "DestinyLoadoutColorDefinition"
)

// kind returns the word that messages use for an entry of the table.
func (t LoadoutTable) kind() string {
	switch t {
	case LoadoutIconTable:
		return "icon"
	case LoadoutColorTable:
		return "color"
	default:
		return "name"
	}
}

var (
	// ErrUnknownLoadoutName reports a name that no entry of the loadout
	// name table carries.
	ErrUnknownLoadoutName = errors.New("unknown loadout name")

	// errNoTable reports a manifest that lists no English file of a
	// loadout table.
	errNoTable = errors.New("the Bungie manifest lists no table")

	// errEmptyTable reports a loadout table file that lists no entry.
	errEmptyTable = errors.New("the table lists no entry")

	// errNoShownEntry reports a loadout table whose entries are all
	// redacted.
	errNoShownEntry = errors.New("the table holds no entry that the game shows")
)

// ManifestSource serves the Destiny manifest and its content files. A
// caller adapts [*Client] to it and adds the credentials to each manifest
// read.
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

// LoadoutTableState is one loadout table, or the reason that the store
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
func decodeLoadoutTable(r io.Reader, t LoadoutTable) (map[uint32]LoadoutDefinition, error) {
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
// that Bungie publishes. The definition artifacts of Agg's Destiny Vault
// Manager leave those tables out.
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
	loaded map[LoadoutTable]loadedTable
}

// NewLoadoutTableStore returns a [*LoadoutTableStore] that reads the
// manifest from source.
func NewLoadoutTableStore(source ManifestSource) *LoadoutTableStore {
	return &LoadoutTableStore{source: source, loaded: map[LoadoutTable]loadedTable{}}
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
func (s *LoadoutTableStore) Load(ctx context.Context, tables ...LoadoutTable) map[LoadoutTable]LoadoutTableState {
	out := make(map[LoadoutTable]LoadoutTableState, len(tables))

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
func (s *LoadoutTableStore) loadTable(ctx context.Context, t LoadoutTable, path string) LoadoutTableState {
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

	defer httpbody.Close(ctx, body, path)

	table, err := decodeLoadoutTable(body, t)
	if err != nil {
		return LoadoutTableState{Err: fmt.Errorf("decoding %s: %w", path, err)}
	}

	s.loaded[t] = loadedTable{table: table, path: path}

	return LoadoutTableState{Table: table}
}

// ChooseLoadoutIdentifiers returns the identifiers that a save of a loadout
// slot sends with [*Client.SnapshotLoadout]. Kept holds the identifiers of
// the loadout in the slot. The bool saved reports whether the slot holds a
// saved loadout. A non-empty name picks the entry of the name table with
// that name in any letter case. Without a name, a saved loadout keeps its
// name and an empty slot takes the first name of the table. The icon and
// the color follow the same rule without a choice. A saved loadout keeps
// its own, and an empty slot takes the first entry of each table.
//
// States holds the state of each table that the rules read. A table that
// the rules read and that did not load returns an error with the reason.
// A name that the table lacks returns an error that wraps
// [ErrUnknownLoadoutName].
func ChooseLoadoutIdentifiers(
	kept LoadoutIdentifiers,
	saved bool,
	name string,
	states map[LoadoutTable]LoadoutTableState,
) (LoadoutIdentifiers, error) {
	ids := kept

	switch {
	case name != "":
		names := states[LoadoutNameTable]
		if names.Err != nil {
			return ids, fmt.Errorf("loadout names unavailable: %w", names.Err)
		}

		hash, ok := findLoadoutName(names.Table, name)
		if !ok {
			choices := loadoutNameChoices(names.Table)
			if len(choices) == 0 {
				return ids, fmt.Errorf("%w: %q. The name table holds no name that the game shows",
					ErrUnknownLoadoutName, name)
			}

			return ids, fmt.Errorf("%w: %q. Pass one of these names in any letter case: %s",
				ErrUnknownLoadoutName, name, strings.Join(choices, ", "))
		}

		ids.NameHash = hash

	case !saved:
		hash, err := firstOf(states, LoadoutNameTable)
		if err != nil {
			return ids, err
		}

		ids.NameHash = hash
	}

	if saved {
		return ids, nil
	}

	icon, err := firstOf(states, LoadoutIconTable)
	if err != nil {
		return ids, err
	}

	color, err := firstOf(states, LoadoutColorTable)
	if err != nil {
		return ids, err
	}

	ids.IconHash, ids.ColorHash = icon, color

	return ids, nil
}

// firstOf returns the hash of the first entry of table t in states, or the
// reason that the table gives no first entry.
func firstOf(states map[LoadoutTable]LoadoutTableState, t LoadoutTable) (uint32, error) {
	state := states[t]
	if state.Err != nil {
		return 0, fmt.Errorf("loadout %ss unavailable: %w", t.kind(), state.Err)
	}

	hash, err := firstLoadoutEntry(state.Table)
	if err != nil {
		return 0, fmt.Errorf("loadout %ss unavailable: %w", t.kind(), err)
	}

	return hash, nil
}
