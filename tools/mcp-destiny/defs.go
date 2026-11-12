package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// defsRecheck is how long [*DefsLoader.Get] serves loaded definitions
	// before it asks the service for the manifest version again.
	defsRecheck = time.Hour

	// defsCachePrefix names cache files. The digit changes when the fields
	// of [Defs] change, so a process never decodes a stale layout.
	defsCachePrefix = "defs-v1-"

	artifactCore    = "core"
	artifactBuckets = "InventoryBucket"
	artifactDamage  = "DamageType"
	artifactClasses = "Class"
	artifactStats   = "Stat"
)

// Bungie DestinyItemType values that classify gear.
const (
	itemTypeArmor  = 2
	itemTypeWeapon = 3
)

// classAny is the Bungie DestinyClass value of an item that every class can
// use.
const classAny = 3

// ErrDefs reports definition artifacts that are missing from the service
// index or that do not decode.
var ErrDefs = errors.New("definitions")

// ItemDef is the subset of a Bungie inventory item definition that the
// server reads. Perks, mods, and shaders are item definitions too.
type ItemDef struct {
	Name            string `json:"name"`
	TypeName        string `json:"type,omitempty"`
	Tier            string `json:"tier,omitempty"`
	ItemType        int    `json:"itemType,omitempty"`
	ClassType       int    `json:"classType"`
	DamageType      int    `json:"damageType,omitempty"`
	Bucket          uint32 `json:"bucket,omitempty"`
	NonTransferable bool   `json:"nonTransferable,omitempty"`
	Equippable      bool   `json:"equippable,omitempty"`
}

// Defs holds the Destiny definitions that turn the hashes of a Bungie
// profile into names. The Bungie enum value keys DamageTypes and Classes,
// and the definition hash keys the other maps.
type Defs struct {
	Items       map[uint32]ItemDef `json:"items"`
	Buckets     map[uint32]string  `json:"buckets"`
	Stats       map[uint32]string  `json:"stats"`
	DamageTypes map[int]string     `json:"damageTypes"`
	Classes     map[int]string     `json:"classes"`
	Version     string             `json:"version"`
}

// TraitNames maps the lowercase name of every weapon trait to its display
// name. The web client stores a perk preference under the display name and
// reads it back with an exact match. An enhanced trait counts under the
// name of its base trait.
func (d *Defs) TraitNames() map[string]string {
	names := map[string]string{}

	for _, item := range d.Items {
		if item.TypeName != "Trait" && item.TypeName != "Enhanced Trait" {
			continue
		}

		name := basePerkName(item.Name)
		if name != "" {
			names[strings.ToLower(name)] = name
		}
	}

	return names
}

// basePerkName returns the name that an enhanced perk shares with its base
// perk. Bungie names some enhanced perks "X Enhanced".
func basePerkName(name string) string {
	return strings.TrimSuffix(strings.TrimSpace(name), " Enhanced")
}

// ArtifactSource serves the definition artifacts. [*Service] implements it.
type ArtifactSource interface {
	Config(ctx context.Context) (ServiceConfig, error)
	Artifact(ctx context.Context, file string) (io.ReadCloser, error)
}

// DefsOption configures a [*DefsLoader]. See [WithDefsClock] and
// [WithDefsLogger].
type DefsOption func(*DefsLoader)

// WithDefsClock overrides the clock that schedules manifest version checks.
// It is a [DefsOption].
func WithDefsClock(now func() time.Time) DefsOption {
	return func(l *DefsLoader) { l.now = now }
}

// WithDefsLogger sets the logger for cache failures. It is a [DefsOption].
func WithDefsLogger(log *slog.Logger) DefsOption {
	return func(l *DefsLoader) { l.log = log }
}

// DefsLoader builds [Defs] from the definition artifacts of the service.
//
// The item artifact is tens of megabytes of JSON, so the loader keeps the
// decoded subset in a cache file named after the manifest version. A
// process reads that file on its first call and downloads the artifacts
// only after the service publishes another manifest version.
type DefsLoader struct {
	source   ArtifactSource
	now      func() time.Time
	log      *slog.Logger
	defs     *Defs
	checked  time.Time
	cacheDir string
	mu       sync.Mutex
}

// NewDefsLoader returns a [*DefsLoader] that reads artifacts from source
// and keeps its cache file in cacheDir. An empty cacheDir turns the cache
// file off.
func NewDefsLoader(source ArtifactSource, cacheDir string, opts ...DefsOption) *DefsLoader {
	l := &DefsLoader{
		source:   source,
		cacheDir: cacheDir,
		now:      time.Now,
		log:      slog.New(slog.DiscardHandler),
	}
	for _, opt := range opts {
		opt(l)
	}

	return l
}

// Get returns the definitions for the manifest version that the service
// publishes.
func (l *DefsLoader) Get(ctx context.Context) (*Defs, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if l.defs != nil && now.Sub(l.checked) < defsRecheck {
		return l.defs, nil
	}

	cfg, err := l.source.Config(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading service config: %w", err)
	}

	index := cfg.Artifacts
	if l.defs != nil && l.defs.Version == index.ManifestVersion {
		l.checked = now

		return l.defs, nil
	}

	defs, ok := l.readCache(ctx, index.ManifestVersion)
	if !ok {
		defs, err = l.build(ctx, index)
		if err != nil {
			return nil, err
		}

		l.writeCache(ctx, defs)
	}

	l.defs = defs
	l.checked = now

	return defs, nil
}

func (l *DefsLoader) build(ctx context.Context, index ArtifactIndex) (*Defs, error) {
	defs := &Defs{
		Version:     index.ManifestVersion,
		Items:       map[uint32]ItemDef{},
		Buckets:     map[uint32]string{},
		Stats:       map[uint32]string{},
		DamageTypes: map[int]string{},
		Classes:     map[int]string{},
	}

	files := index.Files[artifactCore]
	if len(files) == 0 {
		return nil, fmt.Errorf("%w: the service index lists no %s artifact", ErrDefs, artifactCore)
	}

	for _, file := range files {
		err := l.read(ctx, file, func(r io.Reader) error {
			return decodeItems(r, defs.Items)
		})
		if err != nil {
			return nil, err
		}
	}

	tables := map[string]func(hash uint32, row tableRow){
		artifactBuckets: func(hash uint32, row tableRow) { defs.Buckets[hash] = row.DisplayProperties.Name },
		artifactStats:   func(hash uint32, row tableRow) { defs.Stats[hash] = row.DisplayProperties.Name },
		artifactDamage:  func(_ uint32, row tableRow) { defs.DamageTypes[row.EnumValue] = row.DisplayProperties.Name },
		artifactClasses: func(_ uint32, row tableRow) { defs.Classes[row.ClassType] = row.DisplayProperties.Name },
	}

	for name, add := range tables {
		files := index.Files[name]
		if len(files) == 0 {
			return nil, fmt.Errorf("%w: the service index lists no %s artifact", ErrDefs, name)
		}

		err := l.read(ctx, files[0], func(r io.Reader) error {
			return decodeTable(r, add)
		})
		if err != nil {
			return nil, err
		}
	}

	return defs, nil
}

func (l *DefsLoader) read(ctx context.Context, file string, decode func(io.Reader) error) error {
	body, err := l.source.Artifact(ctx, file)
	if err != nil {
		return fmt.Errorf("fetching artifact %s: %w", file, err)
	}

	defer closeBody(ctx, body, file)

	err = decode(body)
	if err != nil {
		return fmt.Errorf("%w: decoding artifact %s: %w", ErrDefs, file, err)
	}

	return nil
}

// rawItem is the subset of an artifact item record that [decodeItems]
// reads.
type rawItem struct {
	EquippingBlock    *struct{} `json:"equippingBlock"`
	DisplayProperties struct {
		Name string `json:"name"`
	} `json:"displayProperties"`
	ItemTypeDisplayName string `json:"itemTypeDisplayName"`
	Inventory           struct {
		TierTypeName             string `json:"tierTypeName"`
		BucketTypeHash           uint32 `json:"bucketTypeHash"`
		NonTransferrableOriginal bool   `json:"nonTransferrableOriginal"`
	} `json:"inventory"`
	Hash              uint32 `json:"hash"`
	ItemType          int    `json:"itemType"`
	ClassType         int    `json:"classType"`
	DefaultDamageType int    `json:"defaultDamageType"`
}

// decodeItems streams a JSON array of item records into items, so the
// full artifact never sits in memory.
func decodeItems(r io.Reader, items map[uint32]ItemDef) error {
	dec := json.NewDecoder(r)

	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("reading array start: %w", err)
	}

	if tok != json.Delim('[') {
		return fmt.Errorf("%w: item artifact is not a JSON array", ErrDefs)
	}

	for dec.More() {
		var raw rawItem

		err := dec.Decode(&raw)
		if err != nil {
			return fmt.Errorf("reading item record: %w", err)
		}

		items[raw.Hash] = ItemDef{
			Name:            raw.DisplayProperties.Name,
			TypeName:        raw.ItemTypeDisplayName,
			Tier:            raw.Inventory.TierTypeName,
			Bucket:          raw.Inventory.BucketTypeHash,
			ItemType:        raw.ItemType,
			ClassType:       raw.ClassType,
			DamageType:      raw.DefaultDamageType,
			NonTransferable: raw.Inventory.NonTransferrableOriginal,
			Equippable:      raw.EquippingBlock != nil,
		}
	}

	return nil
}

// tableRow is the subset of a definition table record that [decodeTable]
// reads.
type tableRow struct {
	DisplayProperties struct {
		Name string `json:"name"`
	} `json:"displayProperties"`
	EnumValue int `json:"enumValue"`
	ClassType int `json:"classType"`
}

// decodeTable reads a JSON object that maps definition hashes to records.
func decodeTable(r io.Reader, add func(hash uint32, row tableRow)) error {
	var table map[string]tableRow

	err := json.NewDecoder(r).Decode(&table)
	if err != nil {
		return fmt.Errorf("reading table: %w", err)
	}

	for key, row := range table {
		hash, err := strconv.ParseUint(key, 10, 32)
		if err != nil {
			return fmt.Errorf("parsing table key %q: %w", key, err)
		}

		add(uint32(hash), row)
	}

	return nil
}

func (l *DefsLoader) cachePath(version string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-':
			return r
		default:
			return '_'
		}
	}, version)

	return filepath.Join(l.cacheDir, defsCachePrefix+safe+".json")
}

func (l *DefsLoader) readCache(ctx context.Context, version string) (*Defs, bool) {
	if l.cacheDir == "" {
		return nil, false
	}

	path := l.cachePath(version)

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	var defs Defs

	err = json.Unmarshal(raw, &defs)
	if err != nil || defs.Version != version {
		l.log.WarnContext(ctx, "ignoring unreadable definition cache",
			slog.String("path", path),
			slog.Any("error", err),
		)

		return nil, false
	}

	return &defs, true
}

// writeCache saves defs and deletes the cache files of other manifest
// versions. A cache failure costs only the next process a download, so
// writeCache logs it and continues.
func (l *DefsLoader) writeCache(ctx context.Context, defs *Defs) {
	if l.cacheDir == "" {
		return
	}

	path := l.cachePath(defs.Version)

	err := l.saveCache(path, defs)
	if err != nil {
		l.log.WarnContext(ctx, "writing definition cache",
			slog.String("path", path),
			slog.Any("error", err),
		)

		return
	}

	old, err := filepath.Glob(filepath.Join(l.cacheDir, defsCachePrefix+"*.json"))
	if err != nil {
		return
	}

	for _, file := range old {
		if file == path {
			continue
		}

		err := os.Remove(file)
		if err != nil {
			l.log.WarnContext(ctx, "removing old definition cache",
				slog.String("path", file),
				slog.Any("error", err),
			)
		}
	}
}

func (l *DefsLoader) saveCache(path string, defs *Defs) error {
	raw, err := json.Marshal(defs)
	if err != nil {
		return fmt.Errorf("encoding definitions: %w", err)
	}

	err = os.MkdirAll(l.cacheDir, 0o755)
	if err != nil {
		return fmt.Errorf("creating cache directory: %w", err)
	}

	tmp := path + ".tmp"

	err = os.WriteFile(tmp, raw, 0o644)
	if err != nil {
		return fmt.Errorf("writing cache file: %w", err)
	}

	err = os.Rename(tmp, path)
	if err != nil {
		return fmt.Errorf("replacing cache file: %w", err)
	}

	return nil
}
