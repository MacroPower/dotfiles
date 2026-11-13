package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
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
	defsCachePrefix = "defs-v7-"

	artifactCore            = "core"
	artifactDetail          = "detail"
	artifactPlugSets        = "plugsets"
	artifactBuckets         = "InventoryBucket"
	artifactDamage          = "DamageType"
	artifactClasses         = "Class"
	artifactStats           = "Stat"
	artifactActivities      = "Activity"
	artifactActivityTypes   = "ActivityType"
	artifactDestinations    = "Destination"
	artifactPlaces          = "Place"
	artifactActivityRewards = "ActivityReward"
	artifactVendors         = "Vendor"
	artifactVendorItems     = "VendorItem"
)

// Bungie DestinyItemType values that classify gear and plugs. Bungie
// names the plug type Mod, and weapon perks carry it too.
const (
	itemTypeArmor  = 2
	itemTypeWeapon = 3
	itemTypePlug   = 19
)

// Bungie DestinyClass values. classAny is the value of an item that every
// class can use.
const (
	classTitan   = 0
	classHunter  = 1
	classWarlock = 2
	classAny     = 3
)

// Bungie BucketCategory values of the buckets that hold items that a player
// sees and manages.
const (
	bucketCategoryItem       = 1
	bucketCategoryEquippable = 3
)

// bucketScopeAccount is the Bungie BucketScope value of a bucket that every
// character shares.
const bucketScopeAccount = 1

// ErrDefs reports definition artifacts that are missing from the service
// index or that do not decode.
var ErrDefs = errors.New("definitions")

// ItemDef is the subset of a Bungie inventory item definition that the
// server reads. Perks, mods, and shaders are item definitions too.
// The loader keeps Description only for plugs, so the cache file stays
// small.
type ItemDef struct {
	Name            string `json:"name"`
	Description     string `json:"description,omitempty"`
	TypeName        string `json:"type,omitempty"`
	Tier            string `json:"tier,omitempty"`
	ItemType        int    `json:"itemType,omitempty"`
	ClassType       int    `json:"classType"`
	DamageType      int    `json:"damageType,omitempty"`
	Bucket          uint32 `json:"bucket,omitempty"`
	NonTransferable bool   `json:"nonTransferable,omitempty"`
	Equippable      bool   `json:"equippable,omitempty"`
}

// BucketDef is the subset of a Bungie inventory bucket definition that the
// server reads. Capacity is the number of items that the bucket holds, and
// each stack takes one of them. Category is the Bungie BucketCategory value,
// and Order is the position of the bucket in the display order of the game.
// Account marks a bucket that every character shares, such as the vault.
type BucketDef struct {
	Name     string `json:"name"`
	Capacity int    `json:"capacity,omitempty"`
	Category int    `json:"category,omitempty"`
	Order    int    `json:"order,omitempty"`
	Account  bool   `json:"account,omitempty"`
}

// ActivityDef is the subset of a Bungie activity definition that the server
// reads. Name is the full name that the definition stores, with its
// difficulty labels, such as "Vault of Glass: Master". Type, Destination,
// and Place hold the hashes of the activity type, the destination, and the
// place of the activity. Each one is zero when the definition lacks it.
// Mode is the Bungie DestinyActivityModeType value of the activity itself,
// such as 2 for Story.
type ActivityDef struct {
	Name        string `json:"name"`
	Mode        int    `json:"mode,omitempty"`
	Type        uint32 `json:"type,omitempty"`
	Destination uint32 `json:"destination,omitempty"`
	Place       uint32 `json:"place,omitempty"`
}

// VendorDef is the subset of a Bungie vendor definition that the server
// reads. The failure indexes of a sale pick the reasons that the sale
// fails from FailureStrings, and some of its entries are empty.
// Categories maps the index of each display category to its name.
type VendorDef struct {
	Categories     map[int]string `json:"categories,omitempty"`
	Name           string         `json:"name,omitempty"`
	Subtitle       string         `json:"subtitle,omitempty"`
	FailureStrings []string       `json:"failureStrings,omitempty"`
}

// Defs holds the Destiny definitions that turn hashes into names. The
// hashes come from a Bungie profile, the activity history, the activities
// of a character, and the vendor stock. The Bungie enum value keys
// DamageTypes and Classes, and the definition hash keys the other maps.
//
// ActivityTypes, Destinations, and Places name the activity types, the
// destinations, and the places that an [ActivityDef] points at.
// ActivityRewards names the reward items of the activities. VendorItems
// names the items and currencies that the vendors list. Both name dummy
// items, which Items leaves out.
//
// Sockets maps the item hash of a weapon to one plug set hash for each
// socket of the weapon, by socket index. A zero marks a socket without a
// plug set. PlugSets maps the hash of each plug set that Sockets names to
// the hashes of the plugs that the set lists. The roll ratings read both
// maps to find the socket of a perk column in which a copy rolled no
// pick. Only weapons have roll ratings, so Sockets holds only weapons.
//
// Activities, ActivityTypes, Destinations, Places, ActivityRewards,
// Vendors, VendorItems, Sockets, and PlugSets are empty when the service
// index lists no artifact for them. PlugSets is also empty when the index
// lists no artifact for Sockets.
type Defs struct {
	Items           map[uint32]ItemDef     `json:"items"`
	Buckets         map[uint32]BucketDef   `json:"buckets"`
	Stats           map[uint32]string      `json:"stats"`
	Sockets         map[uint32][]uint32    `json:"sockets"`
	PlugSets        map[uint32][]uint32    `json:"plugSets"`
	Activities      map[uint32]ActivityDef `json:"activities"`
	ActivityTypes   map[uint32]string      `json:"activityTypes"`
	Destinations    map[uint32]string      `json:"destinations"`
	Places          map[uint32]string      `json:"places"`
	ActivityRewards map[uint32]string      `json:"activityRewards"`
	Vendors         map[uint32]VendorDef   `json:"vendors"`
	VendorItems     map[uint32]string      `json:"vendorItems"`
	DamageTypes     map[int]string         `json:"damageTypes"`
	Classes         map[int]string         `json:"classes"`
	Version         string                 `json:"version"`
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

// plugSetSocket returns the lowest index of a socket of a weapon whose
// plug set lists a pick of a perk column. Hash is the item hash of the
// weapon, and column lists the plug hashes of the picks. It reports
// false when no plug set of the weapon lists a pick. A socket without a
// plug set never matches, also when PlugSets has an entry for the hash
// zero.
func (d *Defs) plugSetSocket(hash uint32, column []uint32) (int, bool) {
	for i, set := range d.Sockets[hash] {
		if set == 0 {
			continue
		}

		listed := slices.ContainsFunc(d.PlugSets[set], func(plug uint32) bool { return slices.Contains(column, plug) })
		if listed {
			return i, true
		}
	}

	return 0, false
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
		Version:         index.ManifestVersion,
		Items:           map[uint32]ItemDef{},
		Buckets:         map[uint32]BucketDef{},
		Stats:           map[uint32]string{},
		Sockets:         map[uint32][]uint32{},
		PlugSets:        map[uint32][]uint32{},
		Activities:      map[uint32]ActivityDef{},
		ActivityTypes:   map[uint32]string{},
		Destinations:    map[uint32]string{},
		Places:          map[uint32]string{},
		ActivityRewards: map[uint32]string{},
		Vendors:         map[uint32]VendorDef{},
		VendorItems:     map[uint32]string{},
		DamageTypes:     map[int]string{},
		Classes:         map[int]string{},
	}

	files := index.Files[artifactCore]
	if len(files) == 0 {
		return nil, fmt.Errorf("%w: the service index lists no %s artifact", ErrDefs, artifactCore)
	}

	err := readRecords(ctx, l, files, func(raw rawItem) { defs.Items[raw.Hash] = raw.item() })
	if err != nil {
		return nil, err
	}

	// The roll ratings read the plug sets only for a perk column in which
	// a copy rolled no pick. They therefore keep working on a deployment
	// whose index lists no detail artifact or no plugsets artifact.
	//
	// The loader keeps the socket list of each weapon of the core
	// artifact, and then each plug set that those socket lists name. The
	// detail read therefore follows the core read, and the plugsets read
	// follows the detail read.
	err = readRecords(ctx, l, index.Files[artifactDetail], func(raw rawDetail) {
		sets := raw.plugSets()
		if len(sets) > 0 && defs.Items[raw.Hash].ItemType == itemTypeWeapon {
			defs.Sockets[raw.Hash] = sets
		}
	})
	if err != nil {
		return nil, err
	}

	named := map[uint32]bool{}

	for _, sets := range defs.Sockets {
		for _, set := range sets {
			if set != 0 {
				named[set] = true
			}
		}
	}

	err = readRecords(ctx, l, index.Files[artifactPlugSets], func(raw rawPlugSet) {
		if named[raw.Hash] {
			defs.PlugSets[raw.Hash] = raw.plugs()
		}
	})
	if err != nil {
		return nil, err
	}

	tables := map[string]func(hash uint32, row tableRow){
		artifactBuckets: func(hash uint32, row tableRow) { defs.Buckets[hash] = row.bucket() },
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

	// The inventory tools read neither the activity definitions nor the
	// vendor definitions. They therefore keep working on a deployment whose
	// index lists none of these artifacts.
	optional := map[string]func(io.Reader) error{
		artifactActivities: func(r io.Reader) error {
			return decodeTable(r, func(hash uint32, row activityRow) { defs.Activities[hash] = row.activity() })
		},
		artifactActivityTypes: func(r io.Reader) error {
			return decodeTable(r, func(hash uint32, row namedRow) { defs.ActivityTypes[hash] = row.Name })
		},
		artifactDestinations: func(r io.Reader) error {
			return decodeTable(r, func(hash uint32, row namedRow) { defs.Destinations[hash] = row.Name })
		},
		artifactPlaces: func(r io.Reader) error {
			return decodeTable(r, func(hash uint32, row namedRow) { defs.Places[hash] = row.Name })
		},
		artifactVendors: func(r io.Reader) error {
			return decodeTable(r, func(hash uint32, row vendorRow) { defs.Vendors[hash] = row.vendor() })
		},
	}

	for name, decode := range optional {
		files := index.Files[name]
		if len(files) == 0 {
			continue
		}

		err := l.read(ctx, files[0], decode)
		if err != nil {
			return nil, err
		}
	}

	// The core artifact leaves out dummy items, and the rewards of the
	// activities are dummies, so only the ActivityReward artifact names
	// them.
	err = readRecords(ctx, l, index.Files[artifactActivityRewards], func(row hashedRow) {
		if row.Name != "" {
			defs.ActivityRewards[row.Hash] = row.Name
		}
	})
	if err != nil {
		return nil, err
	}

	err = readRecords(ctx, l, index.Files[artifactVendorItems], func(row hashedRow) {
		if row.Name != "" {
			defs.VendorItems[row.Hash] = row.Name
		}
	})
	if err != nil {
		return nil, err
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

// rawItem is the subset of a record of the core artifact that the loader
// reads.
type rawItem struct {
	EquippingBlock    *struct{} `json:"equippingBlock"`
	DisplayProperties struct {
		Name        string `json:"name"`
		Description string `json:"description"`
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

// item returns the item definition that a record of the core artifact
// holds.
func (raw rawItem) item() ItemDef {
	def := ItemDef{
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

	if raw.ItemType == itemTypePlug {
		def.Description = raw.DisplayProperties.Description
	}

	return def
}

// rawDetail is the subset of a record of the detail artifact that the
// loader reads. The record holds the socket entries of an item
// definition, in the order of the sockets of each copy.
type rawDetail struct {
	Sockets struct {
		SocketEntries []struct {
			ReusablePlugSetHash   uint32 `json:"reusablePlugSetHash"`
			RandomizedPlugSetHash uint32 `json:"randomizedPlugSetHash"`
		} `json:"socketEntries"`
	} `json:"sockets"`
	Hash uint32 `json:"hash"`
}

// plugSets returns the plug set hash of each socket entry of the record,
// and zero for an entry without a plug set. An entry takes its reusable
// plug set, or its randomized plug set when it has no reusable one, as
// Destiny Item Manager does.
func (raw rawDetail) plugSets() []uint32 {
	entries := raw.Sockets.SocketEntries
	sets := make([]uint32, len(entries))

	for i, entry := range entries {
		sets[i] = cmp.Or(entry.ReusablePlugSetHash, entry.RandomizedPlugSetHash)
	}

	return sets
}

// rawPlugSet is the subset of a record of the plugsets artifact that the
// loader reads.
type rawPlugSet struct {
	ReusablePlugItems []struct {
		PlugItemHash uint32 `json:"plugItemHash"`
	} `json:"reusablePlugItems"`
	Hash uint32 `json:"hash"`
}

// plugs returns the hash of each plug that the plug set lists, in the
// order of the record and one time each.
func (raw rawPlugSet) plugs() []uint32 {
	plugs := make([]uint32, 0, len(raw.ReusablePlugItems))

	for _, plug := range raw.ReusablePlugItems {
		if !slices.Contains(plugs, plug.PlugItemHash) {
			plugs = append(plugs, plug.PlugItemHash)
		}
	}

	return plugs
}

// readRecords reads the files of an array artifact in order and streams
// each record into add. The service build splits a large array artifact
// into several files, and the index lists each one. The caller picks the
// record type Row, such as [rawItem] or [hashedRow]. It reads nothing when
// files is empty.
func readRecords[Row any](ctx context.Context, l *DefsLoader, files []string, add func(row Row)) error {
	for _, file := range files {
		err := l.read(ctx, file, func(r io.Reader) error {
			return decodeRecords(r, add)
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// decodeRecords streams a JSON array of records into add, so the full
// array never sits in memory.
func decodeRecords[Row any](r io.Reader, add func(row Row)) error {
	dec := json.NewDecoder(r)

	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("reading array start: %w", err)
	}

	if tok != json.Delim('[') {
		return fmt.Errorf("%w: artifact is not a JSON array", ErrDefs)
	}

	for dec.More() {
		var row Row

		err := dec.Decode(&row)
		if err != nil {
			return fmt.Errorf("reading record: %w", err)
		}

		add(row)
	}

	return nil
}

// tableRow is the subset of a full definition table record that the
// loader reads. The record keeps its name under displayProperties.
type tableRow struct {
	DisplayProperties struct {
		Name string `json:"name"`
	} `json:"displayProperties"`
	EnumValue   int `json:"enumValue"`
	ClassType   int `json:"classType"`
	Scope       int `json:"scope"`
	Category    int `json:"category"`
	BucketOrder int `json:"bucketOrder"`
	ItemCount   int `json:"itemCount"`
}

// bucket returns the bucket definition that an inventory bucket record
// holds.
func (row tableRow) bucket() BucketDef {
	return BucketDef{
		Name:     row.DisplayProperties.Name,
		Capacity: row.ItemCount,
		Category: row.Category,
		Order:    row.BucketOrder,
		Account:  row.Scope == bucketScopeAccount,
	}
}

// namedRow is the subset of a projected definition table record that the
// loader reads. The service build projects some tables, such as
// ActivityType, to records that carry the name at the top level.
type namedRow struct {
	Name string `json:"name"`
}

// activityRow is the subset of a record of the Activity artifact that the
// loader reads. The service build projects each activity definition to a
// record with top-level fields. The record carries the name and the hashes
// of the type, the destination, and the place. ModeType is the direct mode
// type of the definition.
type activityRow struct {
	Name             string `json:"name"`
	ModeType         int    `json:"modeType"`
	ActivityTypeHash uint32 `json:"activityTypeHash"`
	DestinationHash  uint32 `json:"destinationHash"`
	PlaceHash        uint32 `json:"placeHash"`
}

// activity returns the activity definition that a record of the Activity
// artifact holds.
func (row activityRow) activity() ActivityDef {
	return ActivityDef{
		Name:        row.Name,
		Mode:        row.ModeType,
		Type:        row.ActivityTypeHash,
		Destination: row.DestinationHash,
		Place:       row.PlaceHash,
	}
}

// hashedRow is the subset of a projected record of an array artifact that
// the loader reads. The service build projects some arrays, such as
// VendorItem and ActivityReward, to records that carry the hash and the
// name at the top level.
type hashedRow struct {
	Name string `json:"name"`
	Hash uint32 `json:"hash"`
}

// vendorRow is the subset of a record of the Vendor artifact that the
// loader reads. The service build projects each vendor definition to a
// record that keeps the name, the subtitle, the failure strings, and the
// display categories.
type vendorRow struct {
	DisplayProperties struct {
		Name     string `json:"name"`
		Subtitle string `json:"subtitle"`
	} `json:"displayProperties"`
	FailureStrings    []string `json:"failureStrings"`
	DisplayCategories []struct {
		Name  string `json:"name"`
		Index int    `json:"index"`
	} `json:"displayCategories"`
}

// vendor returns the vendor definition that a record of the Vendor
// artifact holds.
func (row vendorRow) vendor() VendorDef {
	def := VendorDef{
		Name:     row.DisplayProperties.Name,
		Subtitle: row.DisplayProperties.Subtitle,
	}

	// The cache file leaves out an empty list, so an empty list stays nil
	// in a build too.
	if len(row.FailureStrings) > 0 {
		def.FailureStrings = row.FailureStrings
	}

	for _, category := range row.DisplayCategories {
		if def.Categories == nil {
			def.Categories = map[int]string{}
		}

		def.Categories[category.Index] = category.Name
	}

	return def
}

// decodeTable reads a JSON object that maps definition hashes to records.
// The caller picks the record type Row, such as [tableRow], [namedRow], or
// [activityRow].
func decodeTable[Row any](r io.Reader, add func(hash uint32, row Row)) error {
	var table map[string]Row

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

// writeCache saves defs and deletes every other cache file, both those of
// other manifest versions and those of other layouts. A cache failure
// costs only the next process a download, so writeCache logs it and
// continues.
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

	// The pattern matches the cache files of every layout, so a bump of
	// defsCachePrefix also removes the files of the old layout.
	old, err := filepath.Glob(filepath.Join(l.cacheDir, "defs-v*.json"))
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
