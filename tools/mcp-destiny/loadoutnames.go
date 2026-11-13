package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// loadoutNameTable is the name of the definition table of the Destiny
// manifest that names saved loadouts.
const loadoutNameTable = "DestinyLoadoutNameDefinition"

var (
	// errNoNameTable reports a manifest that lists no English loadout name
	// table.
	errNoNameTable = errors.New("the Bungie manifest lists no loadout name table")

	// errEmptyNameTable reports a loadout name table file that lists no
	// name.
	errEmptyNameTable = errors.New("the table lists no loadout name")
)

// ManifestSource serves the Destiny manifest and its content files. The
// handler adapts [*Bungie] to it.
type ManifestSource interface {
	Manifest(ctx context.Context) (map[string]string, error)
	Content(ctx context.Context, path string) (io.ReadCloser, error)
}

// LoadoutName is one Bungie DestinyLoadoutNameDefinition. Index is the
// position of the name in the list that the game offers. Redacted marks a
// name that Bungie does not show yet, and such a name might be empty.
type LoadoutName struct {
	Name     string `json:"name"`
	Index    int    `json:"index"`
	Redacted bool   `json:"redacted"`
}

// LoadoutNameState is the loadout name table, or the reason that the
// server has none. Exactly one field is set. The name hash keys Table,
// and the table holds every entry of the file, redacted ones included.
type LoadoutNameState struct {
	Table map[uint32]LoadoutName
	Err   error
}

// decodeLoadoutNames reads the body of one loadout name table file. The
// file is a JSON object that maps each name hash to its definition.
//
// A loaded table without names reports every loadout as unnamed. An
// empty file does not support that claim, so the decode rejects it.
func decodeLoadoutNames(r io.Reader) (map[uint32]LoadoutName, error) {
	var table map[uint32]LoadoutName

	err := json.NewDecoder(r).Decode(&table)
	if err != nil {
		return nil, fmt.Errorf("reading the loadout name table: %w", err)
	}

	if len(table) == 0 {
		return nil, errEmptyNameTable
	}

	return table, nil
}

// LoadoutNameStore loads the loadout name table from the Destiny manifest
// that Bungie publishes. The definition artifacts of the service leave
// that table out.
//
// Each content path carries an identifier that Bungie changes with every
// manifest release, so the path of the file identifies a version of the
// table. The store keeps the last table that it loaded in memory, with its
// path. It downloads the table only when the manifest lists another path.
//
// The store has no lock, so the caller serializes the calls to Load.
type LoadoutNameStore struct {
	source ManifestSource
	table  map[uint32]LoadoutName
	path   string
}

// NewLoadoutNameStore returns a [*LoadoutNameStore] that reads the
// manifest from source.
func NewLoadoutNameStore(source ManifestSource) *LoadoutNameStore {
	return &LoadoutNameStore{source: source}
}

// Load returns the [LoadoutNameState] at the time of the call.
//
// It reads the manifest on every call, so the state follows the manifest
// that Bungie publishes. A load that does not succeed leaves the kept
// table and its path as they were, so the next call tries again. That
// call fetches the file unless the manifest lists the kept path. Load
// never returns the table of one path while the manifest lists another.
func (s *LoadoutNameStore) Load(ctx context.Context) LoadoutNameState {
	paths, err := s.source.Manifest(ctx)
	if err != nil {
		return LoadoutNameState{Err: fmt.Errorf("reading the Bungie manifest: %w", err)}
	}

	path := paths[loadoutNameTable]
	if path == "" {
		return LoadoutNameState{Err: errNoNameTable}
	}

	if s.table != nil && s.path == path {
		return LoadoutNameState{Table: s.table}
	}

	body, err := s.source.Content(ctx, path)
	if err != nil {
		return LoadoutNameState{Err: fmt.Errorf("fetching %s: %w", path, err)}
	}

	defer closeBody(ctx, body, path)

	table, err := decodeLoadoutNames(body)
	if err != nil {
		return LoadoutNameState{Err: fmt.Errorf("decoding %s: %w", path, err)}
	}

	s.table, s.path = table, path

	return LoadoutNameState{Table: table}
}
