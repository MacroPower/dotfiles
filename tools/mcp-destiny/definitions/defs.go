package definitions

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
	"strings"
	"sync"
	"time"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/cachefile"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/fold"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/httpbody"
)

const (
	// defsRecheck is how long [*Loader.Get] serves loaded definitions
	// before it asks the service for the manifest version again.
	defsRecheck = time.Hour

	// defsCachePrefix names cache files. The digit changes when the fields
	// of [Defs] change or when the loader fills them in another way. A
	// process therefore never decodes a stale layout.
	defsCachePrefix = "defs-v15-"

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
	artifactItemSets        = "EquipableItemSet"
	artifactRecords         = "Record"
	artifactItemSources     = "ItemSource"
	artifactSandboxPerks    = "SandboxPerk"
	artifactWithheld        = "hidden"
)

// sourceRaid is the source tag of Destiny Item Manager (DIM) that every
// raid gives the weapons and the armor that drop in it.
const sourceRaid = "raid"

// ErrDefs reports definition artifacts that are missing from the service
// index or that do not decode.
var ErrDefs = errors.New("definitions")

// ItemDef is the subset of a Bungie inventory item definition that the
// server reads. Perks, mods, and shaders are item definitions too.
// The loader keeps Description only for plugs, so the cache file stays
// small. Description is the in-game text of the plug, as [Defs] describes
// it. Categories holds the item category hashes of the definition, in
// the order of the definition. AmmoType is the Bungie DestinyAmmunitionType
// value of the equipping block, and zero when the definition has no
// equipping block. SetHash is the hash of the equipable item set of the
// equipping block, and zero when the definition names no set.
//
// Tier is the tier name that the definition displays, and TierType is the
// Bungie TierType value that DIM reads for the rarity. TierType is
// [bungie.TierTypeBasic] when the definition gives no tier type.
// DisablePrimaryStatDisplay marks a definition whose primary stat the game
// hides. Craftable marks a definition that names a recipe item, as each
// craftable weapon does.
//
// FragmentSlots is the number of fragment sockets that an aspect opens on
// its subclass, which the investment stat
// [bungie.StatAspectEnergyCapacity] of the aspect gives. It is zero for
// every definition other than an aspect.
type ItemDef struct {
	Name                      string   `json:"name"`
	Description               string   `json:"description,omitempty"`
	TypeName                  string   `json:"type,omitempty"`
	Tier                      string   `json:"tier,omitempty"`
	Categories                []uint32 `json:"categories,omitempty"`
	ItemType                  int      `json:"itemType,omitempty"`
	ClassType                 int      `json:"classType"`
	DamageType                int      `json:"damageType,omitempty"`
	AmmoType                  int      `json:"ammoType,omitempty"`
	TierType                  int      `json:"tierType,omitempty"`
	FragmentSlots             int      `json:"fragmentSlots,omitempty"`
	Bucket                    uint32   `json:"bucket,omitempty"`
	SetHash                   uint32   `json:"setHash,omitempty"`
	NonTransferable           bool     `json:"nonTransferable,omitempty"`
	Equippable                bool     `json:"equippable,omitempty"`
	DisablePrimaryStatDisplay bool     `json:"disablePrimaryStatDisplay,omitempty"`
	Craftable                 bool     `json:"craftable,omitempty"`
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
// DamageEnums maps the hash of each damage type definition to its Bungie
// enum value, which keys DamageTypes.
//
// ActivityTypes, Destinations, and Places name the activity types, the
// destinations, and the places that an [ActivityDef] points at.
// ActivityRewards names the reward items of the activities. VendorItems
// names the items and currencies that the vendors list. Both name dummy
// items, which Items leaves out.
//
// Sockets maps the item hash of a weapon or a subclass to one plug set
// hash for each socket of the item, by socket index. A zero marks a
// socket without a plug set. PlugSets maps the hash of each plug set that
// a socket of a weapon names to the hashes of the plugs that the set
// lists. The roll ratings read both maps to find the socket of a perk
// column in which a copy rolled no pick. The perks of a subclass read
// only Sockets, because the Bungie profile lists the plugs of each plug
// set of a subclass with their state.
//
// ItemSets maps the hash of each equipable item set to the sandbox perk
// hashes of the set bonuses of the set, in the order of the definition.
// ItemSetsListed records whether the service index lists the
// EquipableItemSet artifact. The web client builds no item whose set the
// artifact lacks, so the preview reads both fields.
//
// PlugCategories maps the hash of each plug of the crafting sockets that
// Deepsight reads to its plug category, [bungie.PlugCategoryResonance] or
// [bungie.PlugCategoryHarmonizer]. It leaves out the plugs of every other
// category.
//
// Patterns maps the name of each craftable weapon to the hash of the
// record that tracks its pattern. The record definitions name a pattern
// record after its weapon. Destiny Item Manager (DIM) therefore matches a
// weapon to its pattern by name, and the server does the same. Of two
// pattern records with one name, Patterns keeps the record with the
// higher hash, as DIM does. PatternsListed records whether the service
// index lists the Record artifact.
//
// RaidItems lists in ascending order the item hashes of the weapons and
// the armor that drop in a raid. The ItemSource artifact gives each
// weapon and each armor piece the source tags of Destiny Item Manager
// (DIM). RaidItems keeps each item whose tags hold the tag raid.
// SourcesListed records whether the service index lists the ItemSource
// artifact.
//
// Withheld lists in ascending order the item hashes whose definitions the
// service withholds. The service withholds dummy items, classified items,
// and items without a name, so Items lacks them. Items and Withheld
// together hold every item of the manifest version. WithheldListed
// records whether the service index lists the artifact of the withheld
// hashes.
//
// The description of a plug in Items is the text that the game shows for
// the plug. Most plug definitions hold that text. The definitions of some
// plugs do not hold any text, such as the aspects of a subclass and the
// perks of an artifact. The game then shows the text of each sandbox perk
// of the plug that it does not hide. The description joins those texts,
// as the web client and Destiny Item Manager (DIM) do. The SandboxPerk
// artifact holds the texts of the sandbox perks. Each such plug keeps an
// empty description when the service index does not list the artifact.
//
// Activities, ActivityTypes, Destinations, Places, ActivityRewards,
// Vendors, VendorItems, Sockets, PlugSets, PlugCategories, ItemSets,
// Patterns, RaidItems, and Withheld are empty when the service index does
// not list an artifact for them. PlugSets is also empty when the index
// does not list an artifact for Sockets.
//
// Lookups holds the outcome of each lookup in the live manifest of Bungie
// that a [*NameStore] made for one call, keyed by the item hash. Only the
// copy of the definitions that [*NameStore.Name] returns holds lookups,
// and the cache file of the definitions leaves them out.
type Defs struct {
	Items           map[uint32]ItemDef     `json:"items"`
	Buckets         map[uint32]BucketDef   `json:"buckets"`
	Stats           map[uint32]string      `json:"stats"`
	Sockets         map[uint32][]uint32    `json:"sockets"`
	PlugSets        map[uint32][]uint32    `json:"plugSets"`
	PlugCategories  map[uint32]uint32      `json:"plugCategories"`
	ItemSets        map[uint32][]uint32    `json:"itemSets"`
	Patterns        map[string]uint32      `json:"patterns"`
	Activities      map[uint32]ActivityDef `json:"activities"`
	ActivityTypes   map[uint32]string      `json:"activityTypes"`
	Destinations    map[uint32]string      `json:"destinations"`
	Places          map[uint32]string      `json:"places"`
	ActivityRewards map[uint32]string      `json:"activityRewards"`
	Vendors         map[uint32]VendorDef   `json:"vendors"`
	VendorItems     map[uint32]string      `json:"vendorItems"`
	DamageEnums     map[uint32]int         `json:"damageEnums"`
	DamageTypes     map[int]string         `json:"damageTypes"`
	Classes         map[int]string         `json:"classes"`
	Version         string                 `json:"version"`
	Lookups         map[uint32]Lookup      `json:"-"`
	RaidItems       []uint32               `json:"raidItems"`
	Withheld        []uint32               `json:"withheld"`
	ItemSetsListed  bool                   `json:"itemSetsListed"`
	WithheldListed  bool                   `json:"withheldListed"`
	PatternsListed  bool                   `json:"patternsListed"`
	SourcesListed   bool                   `json:"sourcesListed"`
}

// ItemName returns the name of an item hash. It reads the item definitions
// first. The service withholds the definitions of dummy items, but it
// names the dummy items that the vendors sell and the dummy rewards of the
// activities. ItemName therefore reads the vendor item names second and
// the activity reward names third. It reads the names of the lookups at
// Bungie last. It returns an empty string when none of them names the
// hash.
func (d *Defs) ItemName(hash uint32) string {
	return cmp.Or(d.Items[hash].Name, d.VendorItems[hash], d.ActivityRewards[hash], d.Lookups[hash].Name)
}

// Withholds reports whether the service withholds the definition of the
// item hash. It reports false for every hash when the service index lists
// no artifact of the withheld hashes, as WithheldListed records.
func (d *Defs) Withholds(hash uint32) bool {
	_, found := slices.BinarySearch(d.Withheld, hash)

	return found
}

// FromRaid reports whether the item hash names a weapon or an armor piece
// that drops in a raid, as RaidItems lists them. It reports false for
// every hash when the service index does not list the ItemSource
// artifact, as SourcesListed records.
func (d *Defs) FromRaid(hash uint32) bool {
	_, found := slices.BinarySearch(d.RaidItems, hash)

	return found
}

// TraitNames maps the name of every weapon trait, after [fold.Name] folds
// it, to its display name. A caller folds the name that the user passes
// in the same way and looks up the display name. The web client stores a
// perk preference under the display name and reads it back with an exact
// match. An enhanced trait counts under the name of its base trait.
func (d *Defs) TraitNames() map[string]string {
	names := map[string]string{}

	for hash := range d.Items {
		item := d.Items[hash]
		if item.TypeName != "Trait" && item.TypeName != "Enhanced Trait" {
			continue
		}

		name := BasePerkName(item.Name)
		if name != "" {
			names[fold.Name(name)] = name
		}
	}

	return names
}

// PlugSetSocket returns the lowest index of a socket of a weapon whose
// plug set lists a pick of a perk column. Hash is the item hash of the
// weapon, and column lists the plug hashes of the picks. It reports
// false when no plug set of the weapon lists a pick. A socket without a
// plug set never matches, also when PlugSets has an entry for the hash
// zero.
func (d *Defs) PlugSetSocket(hash uint32, column []uint32) (int, bool) {
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

// BasePerkName returns the name that an enhanced perk shares with its base
// perk. Bungie names some enhanced perks "X Enhanced".
func BasePerkName(name string) string {
	return strings.TrimSuffix(strings.TrimSpace(name), " Enhanced")
}

// ArtifactSource serves the definition artifacts. [*dvm.Client] implements
// it.
type ArtifactSource interface {
	Config(ctx context.Context) (dvm.Config, error)
	Artifact(ctx context.Context, file string) (io.ReadCloser, error)
}

// Option configures a [*Loader] via [NewLoader].
//
// Functions of this type:
//   - [WithClock]
//   - [WithLogger]
//   - [WithLoaded]
type Option func(*Loader)

// WithClock overrides the clock that schedules manifest version checks.
// It is an [Option].
func WithClock(now func() time.Time) Option {
	return func(l *Loader) { l.now = now }
}

// WithLogger sets the logger for cache failures. It is an [Option].
func WithLogger(log *slog.Logger) Option {
	return func(l *Loader) { l.log = log }
}

// WithLoaded seeds the loader with the definitions d and with checked as
// the time of their last check against the service. [*Loader.Get] serves d
// until the recheck interval passes after checked. It then checks the
// manifest version, as it does for the definitions that it loads itself.
// It is an [Option].
func WithLoaded(d *Defs, checked time.Time) Option {
	return func(l *Loader) {
		l.defs = d
		l.checked = checked
	}
}

// Loader builds [Defs] from the definition artifacts of the service.
//
// The item artifact is tens of megabytes of JSON, so the loader keeps the
// decoded subset in a cache file named after the manifest version. A
// process reads that file on its first call and downloads the artifacts
// only after the service publishes another manifest version.
type Loader struct {
	source   ArtifactSource
	now      func() time.Time
	log      *slog.Logger
	defs     *Defs
	checked  time.Time
	cacheDir string
	mu       sync.Mutex
}

// NewLoader returns a [*Loader] that reads artifacts from source and keeps
// its cache file in cacheDir. An empty cacheDir turns the cache file off.
func NewLoader(source ArtifactSource, cacheDir string, opts ...Option) *Loader {
	l := &Loader{
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
func (l *Loader) Get(ctx context.Context) (*Defs, error) {
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

func (l *Loader) build(ctx context.Context, index dvm.ArtifactIndex) (*Defs, error) {
	defs := &Defs{
		Version:         index.ManifestVersion,
		Items:           map[uint32]ItemDef{},
		Buckets:         map[uint32]BucketDef{},
		Stats:           map[uint32]string{},
		Sockets:         map[uint32][]uint32{},
		PlugSets:        map[uint32][]uint32{},
		PlugCategories:  map[uint32]uint32{},
		ItemSets:        map[uint32][]uint32{},
		Patterns:        map[string]uint32{},
		Activities:      map[uint32]ActivityDef{},
		ActivityTypes:   map[uint32]string{},
		Destinations:    map[uint32]string{},
		Places:          map[uint32]string{},
		ActivityRewards: map[uint32]string{},
		Vendors:         map[uint32]VendorDef{},
		VendorItems:     map[uint32]string{},
		DamageEnums:     map[uint32]int{},
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
	//
	// The Deepsight flags read the plug category of the crafting plugs,
	// which only the detail artifact gives.
	//
	// The loader also keeps the socket list of each subclass, which the
	// perks of a subclass read without the plugsets artifact. A subclass
	// without a socket list does not show any swaps.
	//
	// Only the detail artifact lists the sandbox perks of a plug. Shown
	// maps each plug whose core record does not hold a description to
	// the sandbox perks that the game shows for it.
	shown := map[uint32][]uint32{}

	err = readRecords(ctx, l, index.Files[artifactDetail], func(raw rawDetail) {
		sets := raw.plugSets()
		item := defs.Items[raw.Hash]

		if len(sets) > 0 && (item.ItemType == bungie.ItemTypeWeapon || item.Bucket == bungie.BucketSubclass) {
			defs.Sockets[raw.Hash] = sets
		}

		if item.ItemType == bungie.ItemTypePlug && item.Description == "" {
			shown[raw.Hash] = raw.shownPerks()
		}

		if raw.Plug != nil {
			switch category := raw.Plug.PlugCategoryHash; category {
			case bungie.PlugCategoryResonance, bungie.PlugCategoryHarmonizer:
				defs.PlugCategories[raw.Hash] = category
			}
		}
	})
	if err != nil {
		return nil, err
	}

	named := map[uint32]bool{}

	for hash, sets := range defs.Sockets {
		if defs.Items[hash].ItemType != bungie.ItemTypeWeapon {
			continue
		}

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

	// Only the perk descriptions of get_item read the sandbox perks, so
	// the other tools keep working without them.
	err = l.describePlugs(ctx, index.Files[artifactSandboxPerks], defs, shown)
	if err != nil {
		return nil, err
	}

	tables := map[string]func(hash uint32, row tableRow){
		artifactBuckets: func(hash uint32, row tableRow) { defs.Buckets[hash] = row.bucket() },
		artifactStats:   func(hash uint32, row tableRow) { defs.Stats[hash] = row.DisplayProperties.Name },
		artifactDamage: func(hash uint32, row tableRow) {
			defs.DamageTypes[row.EnumValue] = row.DisplayProperties.Name
			defs.DamageEnums[hash] = row.EnumValue
		},
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
	// index lists none of these artifacts. The item sets decide which items
	// the web client builds. The preview leaves out each item with a set
	// when the index lists no item sets, as the web client does. Only the
	// pattern progress reads the records, so the other tools keep working
	// without them.
	defs.ItemSetsListed = len(index.Files[artifactItemSets]) > 0
	defs.PatternsListed = len(index.Files[artifactRecords]) > 0

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
		artifactItemSets: func(r io.Reader) error {
			return decodeTable(r, func(hash uint32, row itemSetRow) { defs.ItemSets[hash] = row.perks() })
		},
		artifactRecords: func(r io.Reader) error {
			return decodeTable(r, func(hash uint32, row recordRow) {
				name := row.DisplayProperties.Name
				if !row.pattern() || name == "" {
					return
				}

				// The table decodes in no fixed order, so the
				// comparison picks the record that DIM keeps.
				kept, ok := defs.Patterns[name]
				if !ok || hash > kept {
					defs.Patterns[name] = hash
				}
			})
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

	// The artifact of the withheld hashes tells a hash that the service
	// withholds from a hash that its manifest version lacks. The other
	// tools name an item without it, so it is optional too.
	defs.WithheldListed = len(index.Files[artifactWithheld]) > 0

	err = readRecords(ctx, l, index.Files[artifactWithheld], func(hash uint32) {
		defs.Withheld = append(defs.Withheld, hash)
	})
	if err != nil {
		return nil, err
	}

	slices.Sort(defs.Withheld)

	defs.Withheld = slices.Compact(defs.Withheld)

	// Only the cost of a Deepsight activation reads the source tags, so
	// the other tools keep working without them.
	defs.SourcesListed = len(index.Files[artifactItemSources]) > 0

	err = readRecords(ctx, l, index.Files[artifactItemSources], func(row itemSourceRow) {
		if slices.Contains(row.Keys, sourceRaid) {
			defs.RaidItems = append(defs.RaidItems, row.Hash)
		}
	})
	if err != nil {
		return nil, err
	}

	slices.Sort(defs.RaidItems)

	defs.RaidItems = slices.Compact(defs.RaidItems)

	return defs, nil
}

func (l *Loader) read(ctx context.Context, file string, decode func(io.Reader) error) error {
	body, err := l.source.Artifact(ctx, file)
	if err != nil {
		return fmt.Errorf("fetching artifact %s: %w", file, err)
	}

	defer httpbody.Close(ctx, body, file)

	err = decode(body)
	if err != nil {
		return fmt.Errorf("%w: decoding artifact %s: %w", ErrDefs, file, err)
	}

	return nil
}

// rawItem is the subset of a record of the core artifact that the loader
// reads.
type rawItem struct {
	EquippingBlock *struct {
		AmmoType             int    `json:"ammoType"`
		EquipableItemSetHash uint32 `json:"equipableItemSetHash"`
	} `json:"equippingBlock"`
	DisplayProperties struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"displayProperties"`
	ItemTypeDisplayName string   `json:"itemTypeDisplayName"`
	ItemCategoryHashes  []uint32 `json:"itemCategoryHashes"`
	InvestmentStats     []struct {
		StatTypeHash uint32 `json:"statTypeHash"`
		Value        int    `json:"value"`
	} `json:"investmentStats"`
	Inventory struct {
		TierType                 *int   `json:"tierType"`
		TierTypeName             string `json:"tierTypeName"`
		BucketTypeHash           uint32 `json:"bucketTypeHash"`
		RecipeItemHash           uint32 `json:"recipeItemHash"`
		NonTransferrableOriginal bool   `json:"nonTransferrableOriginal"`
	} `json:"inventory"`
	Stats struct {
		DisablePrimaryStatDisplay bool `json:"disablePrimaryStatDisplay"`
	} `json:"stats"`
	Hash              uint32 `json:"hash"`
	ItemType          int    `json:"itemType"`
	ClassType         int    `json:"classType"`
	DefaultDamageType int    `json:"defaultDamageType"`
}

// item returns the item definition that a record of the core artifact
// holds.
func (raw rawItem) item() ItemDef {
	def := ItemDef{
		Name:                      raw.DisplayProperties.Name,
		TypeName:                  raw.ItemTypeDisplayName,
		Tier:                      raw.Inventory.TierTypeName,
		TierType:                  bungie.TierTypeBasic,
		Bucket:                    raw.Inventory.BucketTypeHash,
		ItemType:                  raw.ItemType,
		ClassType:                 raw.ClassType,
		DamageType:                raw.DefaultDamageType,
		NonTransferable:           raw.Inventory.NonTransferrableOriginal,
		Equippable:                raw.EquippingBlock != nil,
		DisablePrimaryStatDisplay: raw.Stats.DisablePrimaryStatDisplay,
		Craftable:                 raw.Inventory.RecipeItemHash != 0,
	}

	if raw.Inventory.TierType != nil {
		def.TierType = *raw.Inventory.TierType
	}

	// The cache file leaves out an empty list, so an empty list stays nil
	// in a build too.
	if len(raw.ItemCategoryHashes) > 0 {
		def.Categories = raw.ItemCategoryHashes
	}

	if raw.EquippingBlock != nil {
		def.AmmoType = raw.EquippingBlock.AmmoType
		def.SetHash = raw.EquippingBlock.EquipableItemSetHash
	}

	if raw.ItemType == bungie.ItemTypePlug {
		def.Description = raw.DisplayProperties.Description

		for _, stat := range raw.InvestmentStats {
			if stat.StatTypeHash == bungie.StatAspectEnergyCapacity {
				def.FragmentSlots = stat.Value
			}
		}
	}

	return def
}

// rawDetail is the subset of a record of the detail artifact that the
// loader reads. The record holds the socket entries of an item
// definition, in the order of the sockets of each copy. Plug is the plug
// block of a plug definition, and nil for every other definition. Perks
// lists the sandbox perks of the definition, in the order of the
// definition.
type rawDetail struct {
	Plug *struct {
		PlugCategoryHash uint32 `json:"plugCategoryHash"`
	} `json:"plug"`
	Sockets struct {
		SocketEntries []struct {
			ReusablePlugSetHash   uint32 `json:"reusablePlugSetHash"`
			RandomizedPlugSetHash uint32 `json:"randomizedPlugSetHash"`
		} `json:"socketEntries"`
	} `json:"sockets"`
	Perks []struct {
		PerkHash       uint32 `json:"perkHash"`
		PerkVisibility int    `json:"perkVisibility"`
	} `json:"perks"`
	Hash uint32 `json:"hash"`
}

// shownPerks returns the hash of each sandbox perk of the record that the
// game shows, in the order of the record. The game hides a perk whose
// visibility is [bungie.PerkVisibilityHidden].
func (raw rawDetail) shownPerks() []uint32 {
	var perks []uint32

	for _, perk := range raw.Perks {
		if perk.PerkVisibility != bungie.PerkVisibilityHidden {
			perks = append(perks, perk.PerkHash)
		}
	}

	return perks
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

// sandboxPerkRow is the subset of a record of the SandboxPerk artifact
// that the loader reads. The service build keeps each sandbox perk
// definition whole.
type sandboxPerkRow struct {
	DisplayProperties struct {
		Description string `json:"description"`
	} `json:"displayProperties"`
}

// describePlugs gives each plug of shown the texts of its sandbox perks,
// which the SandboxPerk artifact in files holds. Shown maps the item
// hash of each plug to the hashes of the sandbox perks that the game
// shows for it, in the order of the definition. The description joins
// the texts with a blank line between two of them, as the web client
// does. It keeps each text once, as DIM does. A plug keeps an empty
// description when none of its sandbox perks has a text. Every plug of
// shown keeps an empty description when files is empty.
func (l *Loader) describePlugs(ctx context.Context, files []string, defs *Defs, shown map[uint32][]uint32) error {
	if len(files) == 0 {
		return nil
	}

	texts := map[uint32]string{}

	err := l.read(ctx, files[0], func(r io.Reader) error {
		return decodeTable(r, func(hash uint32, row sandboxPerkRow) {
			texts[hash] = strings.TrimSpace(row.DisplayProperties.Description)
		})
	})
	if err != nil {
		return err
	}

	for hash, perks := range shown {
		var parts []string

		for _, perk := range perks {
			text := texts[perk]
			if text != "" && !slices.Contains(parts, text) {
				parts = append(parts, text)
			}
		}

		item := defs.Items[hash]
		item.Description = strings.Join(parts, "\n\n")
		defs.Items[hash] = item
	}

	return nil
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
func readRecords[Row any](ctx context.Context, l *Loader, files []string, add func(row Row)) error {
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
		Account:  row.Scope == bungie.BucketScopeAccount,
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

// itemSetRow is the subset of a record of the EquipableItemSet artifact
// that the loader reads. The service build keeps each set definition
// whole. Each entry of SetPerks is one set bonus.
type itemSetRow struct {
	SetPerks []struct {
		SandboxPerkHash uint32 `json:"sandboxPerkHash"`
	} `json:"setPerks"`
}

// perks returns the sandbox perk hash of each set bonus of the record, in
// the order of the record.
func (row itemSetRow) perks() []uint32 {
	perks := make([]uint32, 0, len(row.SetPerks))
	for _, perk := range row.SetPerks {
		perks = append(perks, perk.SandboxPerkHash)
	}

	return perks
}

// itemSourceRow is a record of the ItemSource artifact. The service build
// gives each weapon and each armor piece with a known source the source
// tags of Destiny Item Manager (DIM), the narrowest tag first.
type itemSourceRow struct {
	Keys []string `json:"keys"`
	Hash uint32   `json:"hash"`
}

// recordRow is the subset of a record of the Record artifact that the
// loader reads. The service build keeps each record definition whole.
type recordRow struct {
	CompletionInfo *struct {
		ToastStyle int `json:"toastStyle"`
	} `json:"completionInfo"`
	DisplayProperties struct {
		Name string `json:"name"`
	} `json:"displayProperties"`
}

// pattern reports whether the record tracks the pattern of a craftable
// weapon. Destiny Item Manager (DIM) tells such a record by the toast
// style of its completion block.
func (row recordRow) pattern() bool {
	return row.CompletionInfo != nil && row.CompletionInfo.ToastStyle == bungie.ToastCraftingRecipe
}

// decodeTable reads a JSON object that maps definition hashes to records.
// The caller picks the record type Row, such as [tableRow], [namedRow], or
// [activityRow]. The decode fails on a key that is not a decimal hash.
func decodeTable[Row any](r io.Reader, add func(hash uint32, row Row)) error {
	var table map[uint32]Row

	err := json.NewDecoder(r).Decode(&table)
	if err != nil {
		return fmt.Errorf("reading table: %w", err)
	}

	for hash, row := range table {
		add(hash, row)
	}

	return nil
}

func (l *Loader) cachePath(version string) string {
	return cachePath(l.cacheDir, defsCachePrefix, version)
}

func (l *Loader) readCache(ctx context.Context, version string) (*Defs, bool) {
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
func (l *Loader) writeCache(ctx context.Context, defs *Defs) {
	if l.cacheDir == "" {
		return
	}

	path := l.cachePath(defs.Version)

	err := cachefile.Save(l.cacheDir, path, defs)
	if err != nil {
		l.log.WarnContext(ctx, "writing definition cache",
			slog.String("path", path),
			slog.Any("error", err),
		)

		return
	}

	// The pattern matches the cache files of every layout, so a bump of
	// defsCachePrefix also removes the files of the old layout.
	cachefile.RemoveOthers(ctx, l.log, filepath.Join(l.cacheDir, "defs-v*.json"), path)
}

// cachePath returns the path of the cache file in dir for the manifest
// version. Prefix names the content and the layout of the file, such as
// [defsCachePrefix].
func cachePath(dir, prefix, version string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-':
			return r
		default:
			return '_'
		}
	}, version)

	return filepath.Join(dir, prefix+safe+".json")
}
