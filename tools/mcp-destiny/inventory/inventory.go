package inventory

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/fold"
)

// Owner values of items that no character holds.
const (
	OwnerVault   = "vault"
	OwnerAccount = "account"
)

// Kind values that classify an [Item].
const (
	KindWeapon = "weapon"
	KindArmor  = "armor"
	KindOther  = "other"
)

// Filter keywords that [ItemFilter] accepts beside literal values.
const (
	KindGear = "gear"
	KindAll  = "all"

	LocationEquipped   = "equipped"
	LocationCharacters = "characters"
	LocationPostmaster = "postmaster"

	TagNone = "none"
	TagAny  = "any"

	SortName   = "name"
	SortPower  = "power"
	SortNewest = "newest"
	SortTotal  = "total"
	SortStat   = "stat"
)

// Rarity names that Destiny Item Manager (DIM) gives a Bungie TierType
// value. [RarityName] maps each value to its name.
const (
	RarityUnknown   = "Unknown"
	RarityCurrency  = "Currency"
	RarityCommon    = "Common"
	RarityUncommon  = "Uncommon"
	RarityRare      = "Rare"
	RarityLegendary = "Legendary"
	RarityExotic    = "Exotic"
)

// Stat hashes of the primary stats that DIM counts as power. DIM names
// statAttackD1 Attack in its Destiny 1 table.
const (
	statAttack   uint32 = 1480404414
	statDefense  uint32 = 3897883278
	statPower    uint32 = 1935470627
	statAttackD1 uint32 = 368428387
)

var (
	// rarityNames holds the rarity name of DIM for each Bungie TierType
	// value, by value.
	rarityNames = []string{
		RarityUnknown, RarityCurrency, RarityCommon, RarityUncommon, RarityRare, RarityLegendary, RarityExotic,
	}

	// lightStats lists the hashes of the primary stats that DIM counts as
	// power. DIM joins its Destiny 2 list and its Destiny 1 list, and the
	// Destiny 1 list adds statAttackD1.
	lightStats = []uint32{statAttack, statDefense, statPower, statAttackD1}

	// ErrInvalidFilter reports an [ItemFilter] field with an unknown value,
	// such as a Stat that no item has. It also reports a positive MinStat
	// or a Sort of "stat" in a filter without a Stat.
	ErrInvalidFilter = errors.New("invalid filter")

	// ErrInvalidInput reports an argument that [*Snapshot.ResolveSlot]
	// rejects, such as a slot name that selects no bucket.
	ErrInvalidInput = errors.New("invalid input")

	// ErrUnknownTarget reports a location or character name that the caller
	// does not accept, such as a class name that several characters share.
	// [*Snapshot.ResolveCharacter] also rejects the vault with it.
	ErrUnknownTarget = errors.New("unknown location")

	// ErrUnknownWeapon reports a weapon name that selects no
	// [WeaponGroup] or more than one.
	ErrUnknownWeapon = errors.New("unknown weapon")

	// versionSuffixes end the names of the weapon versions that drop from
	// harder activities. Destiny Item Manager counts a copy of such a
	// version as a copy of the base weapon, and [WeaponGroup] counts it the
	// same way.
	versionSuffixes = []string{"(Adept)", "(Timelost)", "(Harrowed)"}
)

// maxSuggestions caps the names that an [ErrUnknownWeapon] error suggests.
const maxSuggestions = 10

// Plug is one plug that a socket of an item holds or can hold. Hash is the
// item hash of the plug definition, and Description is the text of that
// definition, which is empty for some plugs.
type Plug struct {
	Name        string
	Type        string
	Description string
	Hash        uint32
}

// Perk is the active plug in one visible socket of an item, such as a
// weapon trait or an armor mod. Socket is the index of the socket in the
// socket list of the item. Options lists the other plugs that Bungie
// allows the socket to hold, one for each name. A name that has both a
// base and an enhanced plug keeps the enhanced one.
type Perk struct {
	Plug

	Options []Plug

	// Plugs lists each plug that Bungie allows the socket to hold, and
	// [optionsOf] picks Options from them. A perk change picks Options
	// again, so they match the Options of the next profile fetch.
	Plugs []Plug

	Socket int
}

// OptionNames returns the name of each plug in [Perk.Options].
func (p Perk) OptionNames() []string {
	names := make([]string, 0, len(p.Options))
	for _, option := range p.Options {
		names = append(names, option.Name)
	}

	return names
}

// Stat is one named stat value of an item instance. Hash is the hash of
// the stat definition.
type Stat struct {
	Name  string
	Value int
	Hash  uint32
}

// Character is one character of the signed-in account.
type Character struct {
	LastPlayed time.Time
	ID         string
	Class      string
	ClassType  int
	Power      int
}

// Item is one inventory entry with its hashes resolved to names. Owner is
// a character id, "vault", or "account" for an item in an account-wide
// bucket other than the vault, such as a Consumables stack. Bucket is the
// hash of the bucket that Slot names, which the item occupies when it is
// outside the vault and the Postmaster. Postmaster marks every item at
// the Postmaster, such as a lost item or a message. Lost marks an item in
// the Lost Items bucket of the Postmaster, the only Postmaster bucket that
// [Snapshot.Buckets] lists. Categories holds the item category hashes of
// the definition after the changes that [itemCategories] makes. AmmoType
// is the Bungie DestinyAmmunitionType value of the definition.
//
// Tier is the tier name that the definition displays. TierType is the
// Bungie TierType value of the definition, which [RarityName] maps to the
// rarity of Destiny Item Manager (DIM).
//
// Damage names the damage type of the instance, or the default damage
// type of the definition when the instance has none. Element is the
// Bungie DamageType value of the damage type of the instance alone, and 0
// when the instance has none or the definitions lack it.
//
// Power is the value that DIM counts as the power of the instance, as
// [lightPower] decides. It is 0 for an item without power, such as a
// vehicle, whose primary stat is its speed, or a subclass.
//
// TransferStatus is the Bungie TransferStatuses value that the profile
// gives the item. Transferable marks an item that the server transfers.
// It reads the transfer status and the definition. It ignores the
// transfer status of an item at the Postmaster, because a move pulls such
// an item first.
//
// TagRule is the id of the autotag rule that set Tag. It is empty when a
// person set the tag, and when Tag is outside the tags that the web
// client defines.
//
// SetHash is the hash of the equipable item set of the definition, and 0
// for an item without a set. SetKnown marks an item whose set the
// definitions hold, and SetPerks then holds the sandbox perk hashes of the
// set bonuses of the set.
type Item struct {
	Categories     []int64
	InstanceID     string
	Name           string
	TypeName       string
	Tier           string
	Slot           string
	Class          string
	Damage         string
	Owner          string
	Kind           string
	Tag            string
	TagRule        string
	Note           string
	Perks          []Perk
	Stats          []Stat
	SetPerks       []uint32
	ClassType      int
	AmmoType       int
	TierType       int
	TransferStatus int
	Element        int
	Power          int
	Quantity       int
	Hash           uint32
	Bucket         uint32
	SetHash        uint32
	Equipped       bool
	Postmaster     bool
	Lost           bool
	Locked         bool
	Masterwork     bool
	Crafted        bool
	Transferable   bool
	Equippable     bool
	Lockable       bool
	Favorite       bool
	SetKnown       bool
}

// StatTotal returns the sum of the stat values of an armor piece. It reports
// false for an item without a stat total. Items other than armor have no
// stat total, and neither does armor whose stats the profile omits.
func (i *Item) StatTotal() (int, bool) {
	if i.Kind != KindArmor || len(i.Stats) == 0 {
		return 0, false
	}

	total := 0
	for _, stat := range i.Stats {
		total += stat.Value
	}

	return total, true
}

// StatValue returns the value of the stat with the given name. It compares
// the names after [fold.Name] folds both. It reports false when the item
// lacks the stat.
func (i *Item) StatValue(name string) (int, bool) {
	return i.foldedStatValue(fold.Name(name))
}

// foldedStatValue returns the value of the stat whose name equals folded
// after [fold.Name] folds the stat name. The caller folds the name that it
// looks up, so a loop over many items folds that name only one time.
func (i *Item) foldedStatValue(folded string) (int, bool) {
	for _, stat := range i.Stats {
		if fold.Name(stat.Name) == folded {
			return stat.Value, true
		}
	}

	return 0, false
}

// Bucket is an inventory bucket that holds a fixed number of items, such as
// the kinetic weapons of a character or the vault. Every item and every
// stack in a bucket takes one slot. Account marks a bucket that every
// character shares. A zero Capacity means that the definitions do not give
// the capacity.
type Bucket struct {
	Name     string
	Capacity int
	Hash     uint32
	Account  bool
}

// Currency is the balance of one currency of the account, such as glimmer.
type Currency struct {
	Name     string
	Quantity int
}

// Snapshot is the inventory of every character and the vault at one moment,
// with the loadouts that the characters saved in the game. Bungie can
// serve stale state for some seconds after a change. Moves, lock changes,
// perk changes, and the equips, saves, and clears of loadouts therefore
// update the snapshot in place.
//
// Buckets lists the vault, the buckets that hold the equipment and the items
// that a player manages, and the Postmaster. Each character has its own copy
// of every bucket that is not an account bucket.
//
// LoadoutSlots holds the number of loadout slots of each character, keyed
// by character id. The count includes the empty slots, which Loadouts
// leaves out.
//
// ItemSetsListed records whether the service index listed the
// EquipableItemSet artifact for the definitions of the snapshot.
type Snapshot struct {
	Fetched        time.Time
	byID           map[string]*Item
	LoadoutSlots   map[string]int
	Characters     []Character
	Items          []*Item
	Buckets        []Bucket
	Currencies     []Currency
	Loadouts       []Loadout
	ItemSetsListed bool
}

// NewSnapshot resolves a Bungie profile against the definitions.
func NewSnapshot(profile *bungie.Profile, defs *definitions.Defs, fetched time.Time) *Snapshot {
	s := &Snapshot{
		Fetched:        fetched,
		byID:           map[string]*Item{},
		Buckets:        slotBuckets(defs),
		ItemSetsListed: defs.ItemSetsListed,
	}

	for _, c := range profile.Characters.Data {
		s.Characters = append(s.Characters, Character{
			ID:         c.ID,
			Class:      defs.Classes[c.ClassType],
			ClassType:  c.ClassType,
			Power:      c.Light,
			LastPlayed: c.LastPlayed,
		})
	}

	slices.SortFunc(s.Characters, func(a, b Character) int {
		return cmp.Or(b.LastPlayed.Compare(a.LastPlayed), cmp.Compare(a.ID, b.ID))
	})

	// Bungie gives the vault location to every profile item, also to the
	// stacks in account-wide buckets such as Consumables. Only the bucket
	// marks a vault item.
	for _, pi := range profile.ProfileInventory.Data.Items {
		owner := OwnerAccount
		if pi.BucketHash == bungie.BucketVault {
			owner = OwnerVault
		}

		s.Add(NewItem(profile, defs, pi, owner, false))
	}

	for id, inv := range profile.CharacterInventories.Data {
		for _, pi := range inv.Items {
			s.Add(NewItem(profile, defs, pi, id, false))
		}
	}

	for id, inv := range profile.CharacterEquipment.Data {
		for _, pi := range inv.Items {
			s.Add(NewItem(profile, defs, pi, id, true))
		}
	}

	for _, pi := range profile.ProfileCurrencies.Data.Items {
		s.Currencies = append(s.Currencies, Currency{Name: ItemName(defs, pi.Hash), Quantity: pi.Quantity})
	}

	s.Loadouts = loadoutsOf(profile)
	s.LoadoutSlots = loadoutSlotsOf(profile)

	return s
}

// slotBuckets returns the buckets of [Snapshot.Buckets]. The vault comes
// first and the Postmaster last, and both take the names that the location
// labels use. Between them come the named buckets with a capacity whose
// category holds equipment or items, the equipment buckets first.
func slotBuckets(defs *definitions.Defs) []Bucket {
	var managed []Bucket

	for hash, def := range defs.Buckets {
		switch {
		case hash == bungie.BucketVault, hash == bungie.BucketPostmaster, def.Name == "", def.Capacity <= 0:
		case def.Category == bungie.BucketCategoryEquippable, def.Category == bungie.BucketCategoryItem:
			managed = append(managed, Bucket{Name: def.Name, Hash: hash, Capacity: def.Capacity, Account: def.Account})
		}
	}

	slices.SortFunc(managed, func(a, b Bucket) int {
		da, db := defs.Buckets[a.Hash], defs.Buckets[b.Hash]

		return cmp.Or(
			cmp.Compare(db.Category, da.Category),
			cmp.Compare(da.Order, db.Order),
			cmp.Compare(a.Name, b.Name),
			cmp.Compare(a.Hash, b.Hash),
		)
	})

	out := []Bucket{
		{Name: "Vault", Hash: bungie.BucketVault, Capacity: defs.Buckets[bungie.BucketVault].Capacity, Account: true},
	}
	out = append(out, managed...)

	return append(out, Bucket{
		Name:     "Postmaster",
		Hash:     bungie.BucketPostmaster,
		Capacity: defs.Buckets[bungie.BucketPostmaster].Capacity,
	})
}

// SlotsUsed returns the number of slots that items take in bucket. For a
// bucket that each character has, it counts the items of the character with
// the id owner, equipped items included. An account bucket ignores owner.
func (s *Snapshot) SlotsUsed(bucket Bucket, owner string) int {
	used := 0

	for _, item := range s.Items {
		if occupies(item, bucket, owner) {
			used++
		}
	}

	return used
}

// occupies reports whether item takes a slot of bucket. An item in the vault
// or in the Lost Items bucket takes a slot there and not in the bucket of its
// slot. Every other item at the Postmaster, such as a message, occupies no
// bucket that [Snapshot.Buckets] lists.
func occupies(item *Item, bucket Bucket, owner string) bool {
	switch {
	case bucket.Hash == bungie.BucketVault:
		return item.Owner == OwnerVault
	case bucket.Hash == bungie.BucketPostmaster:
		return item.Owner == owner && item.Lost
	case bucket.Account:
		owner = OwnerAccount
	}

	return item.Owner == owner && !item.Postmaster && item.Bucket == bucket.Hash
}

// Add appends item to Items. When item has an instance id,
// [*Snapshot.Find] then finds it by that id.
func (s *Snapshot) Add(item *Item) {
	s.Items = append(s.Items, item)

	if item.InstanceID != "" {
		s.byID[item.InstanceID] = item
	}
}

// ItemName returns the name that [*definitions.Defs.ItemName] gives an
// item hash. It returns "Unknown item <hash>" when the definitions do not
// name the hash.
func ItemName(defs *definitions.Defs, hash uint32) string {
	name := defs.ItemName(hash)
	if name == "" {
		return fmt.Sprintf("Unknown item %d", hash)
	}

	return name
}

// NewItem resolves the profile item pi against defs. It reads the
// instance, the perks, and the stats of pi from the item components of
// profile. The item belongs to owner, which is a character id,
// [OwnerVault], or [OwnerAccount]. The equipped flag marks an item that
// its character has equipped.
func NewItem(
	profile *bungie.Profile,
	defs *definitions.Defs,
	pi bungie.ProfileItem,
	owner string,
	equipped bool,
) *Item {
	def, ok := defs.Items[pi.Hash]
	if !ok {
		def = definitions.ItemDef{ClassType: bungie.ClassAny}
	}

	def.Name = ItemName(defs, pi.Hash)

	postmaster := pi.BucketHash == bungie.BucketPostmaster || pi.Location == bungie.ItemLocationPostmaster

	// Bungie can report an item as not transferable for as long as it sits
	// in the Postmaster. A move pulls the item first, so the definition
	// alone decides whether a Postmaster item transfers.
	transferable := !def.NonTransferable &&
		(postmaster || pi.TransferStatus&bungie.TransferNotTransferable == 0)

	slot := pi.BucketHash
	if slot == bungie.BucketVault || slot == bungie.BucketPostmaster {
		slot = def.Bucket
	}

	item := &Item{
		InstanceID:     pi.InstanceID,
		Hash:           pi.Hash,
		Name:           def.Name,
		TypeName:       def.TypeName,
		Tier:           def.Tier,
		TierType:       def.TierType,
		Slot:           defs.Buckets[slot].Name,
		Bucket:         slot,
		Kind:           kindOf(def),
		Categories:     itemCategories(def.Categories),
		ClassType:      def.ClassType,
		AmmoType:       def.AmmoType,
		SetHash:        def.SetHash,
		Owner:          owner,
		Quantity:       pi.Quantity,
		Equipped:       equipped,
		Postmaster:     postmaster,
		Lost:           pi.BucketHash == bungie.BucketPostmaster,
		Locked:         pi.State&bungie.StateLocked != 0,
		Masterwork:     pi.State&bungie.StateMasterwork != 0,
		Crafted:        pi.State&bungie.StateCrafted != 0,
		Transferable:   transferable,
		TransferStatus: pi.TransferStatus,
		Equippable:     def.Equippable,
		Lockable:       pi.Lockable,
	}

	if def.ClassType != bungie.ClassAny {
		item.Class = defs.Classes[def.ClassType]
	}

	perks, ok := defs.ItemSets[def.SetHash]
	if def.SetHash != 0 && ok {
		item.SetKnown = true
		item.SetPerks = perks
	}

	damage := def.DamageType

	inst, ok := profile.ItemComponents.Instances.Data[pi.InstanceID]
	if ok {
		if inst.PrimaryStat != nil {
			item.Power = lightPower(def, *inst.PrimaryStat)
		}

		if inst.DamageType != 0 {
			damage = inst.DamageType
		}

		element, known := defs.DamageEnums[inst.DamageTypeHash]
		if inst.DamageTypeHash != 0 && known {
			item.Element = element
		}
	}

	if damage != 0 {
		item.Damage = defs.DamageTypes[damage]
	}

	item.Perks = perksOf(profile, defs, pi.InstanceID)
	item.Stats = statsOf(profile, defs, pi.InstanceID)

	return item
}

// lightPower returns the power that DIM counts for an item of def whose
// instance has the primary stat stat. DIM keeps the primary stat only
// when the definition shows it and its bucket is neither Subclass nor
// Artifacts. It then counts the value when the stat is one of lightStats.
// Every other primary stat counts as 0.
func lightPower(def definitions.ItemDef, stat bungie.ProfileStat) int {
	shown := def.Bucket != bungie.BucketSubclass && def.Bucket != bungie.BucketArtifacts &&
		!def.DisablePrimaryStatDisplay
	if !shown || !slices.Contains(lightStats, stat.Hash) {
		return 0
	}

	return stat.Value
}

// RarityName returns the rarity name that DIM gives the Bungie TierType
// value tierType. DIM names each value after the rarity that the game
// shows, so the Bungie value Basic is Common and Superior is Legendary.
// DIM names a value outside 0 to 6, inclusive, Common.
func RarityName(tierType int) string {
	if tierType < 0 || tierType >= len(rarityNames) {
		return RarityCommon
	}

	return rarityNames[tierType]
}

// itemCategories returns the item category hashes of a definition with
// the two changes that Destiny Item Manager (DIM) makes to them, in the
// order that DIM makes them.
//
// First, a special grenade launcher gets the negative of
// [bungie.CategoryGrenadeLaunchers] in place of that hash. DIM marks an
// item as a special grenade launcher when its categories hold
// [bungie.CategoryWeapon] and [bungie.CategoryGrenadeLaunchers] and lack
// [bungie.CategoryDummies] and [bungie.CategoryPowerWeapon]. Second, a
// linear fusion rifle loses the first [bungie.CategoryFusionRifle], so a
// search for fusion rifles leaves out linear fusion rifles.
//
// DIM also adds categories to a few item hashes from a table of its own.
// No search filter of the preview reads those categories, so
// itemCategories leaves the table out.
func itemCategories(categories []uint32) []int64 {
	var out []int64
	for _, category := range categories {
		out = append(out, int64(category))
	}

	special := slices.Contains(out, bungie.CategoryWeapon) &&
		!slices.Contains(out, bungie.CategoryDummies) &&
		slices.Contains(out, bungie.CategoryGrenadeLaunchers) &&
		!slices.Contains(out, bungie.CategoryPowerWeapon)
	if special {
		out = slices.DeleteFunc(out, func(category int64) bool { return category == bungie.CategoryGrenadeLaunchers })
		out = append(out, -bungie.CategoryGrenadeLaunchers)
	}

	if slices.Contains(out, bungie.CategoryLinearFusionRifles) {
		i := slices.Index(out, bungie.CategoryFusionRifle)
		if i >= 0 {
			out = slices.Delete(out, i, i+1)
		}
	}

	return out
}

func kindOf(def definitions.ItemDef) string {
	switch def.ItemType {
	case bungie.ItemTypeWeapon:
		return KindWeapon
	case bungie.ItemTypeArmor:
		return KindArmor
	default:
		return KindOther
	}
}

func perksOf(profile *bungie.Profile, defs *definitions.Defs, instanceID string) []Perk {
	var perks []Perk

	options := profile.ItemComponents.ReusablePlugs.Data[instanceID].Plugs

	for i, socket := range profile.ItemComponents.Sockets.Data[instanceID].Sockets {
		plug, ok := defs.Items[socket.PlugHash]
		if !socket.IsVisible || !ok || !isPerk(plug) {
			continue
		}

		perk := Perk{Plug: plugOf(plug, socket.PlugHash), Socket: i}

		for _, option := range options[strconv.Itoa(i)] {
			def, ok := defs.Items[option.Hash]
			if ok && option.CanInsert {
				perk.Plugs = append(perk.Plugs, plugOf(def, option.Hash))
			}
		}

		perk.Options = optionsOf(perk.Plugs, perk.Plug)
		perks = append(perks, perk)
	}

	return perks
}

func plugOf(def definitions.ItemDef, hash uint32) Plug {
	return Plug{Name: def.Name, Type: def.TypeName, Description: def.Description, Hash: hash}
}

// optionsOf returns the plugs that a socket that holds active can swap to,
// one for each name other than the name of active. A name that has both a
// base and an enhanced plug keeps the enhanced one.
func optionsOf(plugs []Plug, active Plug) []Plug {
	var options []Plug

	for _, plug := range plugs {
		if plug.Name == active.Name {
			continue
		}

		kept := slices.IndexFunc(options, func(p Plug) bool { return p.Name == plug.Name })
		switch {
		case kept < 0:
			options = append(options, plug)
		case isEnhanced(plug):
			options[kept] = plug
		}
	}

	return options
}

// isEnhanced reports whether a plug is the enhanced variant of a perk. The
// type of an enhanced plug starts with "Enhanced", such as "Enhanced Trait".
func isEnhanced(plug Plug) bool {
	return strings.HasPrefix(plug.Type, "Enhanced")
}

// isPerk reports whether a plug changes how an item plays. It rejects
// cosmetic plugs and the placeholders of empty sockets.
func isPerk(plug definitions.ItemDef) bool {
	if plug.Name == "" || plug.TypeName == "" || strings.HasPrefix(plug.Name, "Empty ") {
		return false
	}

	switch plug.TypeName {
	case "Shader", "Memento", "Restore Defaults", "Transmat Effect", "Ghost Projection", "Emote":
		return false
	}

	return !strings.Contains(plug.TypeName, "Ornament")
}

func statsOf(profile *bungie.Profile, defs *definitions.Defs, instanceID string) []Stat {
	var stats []Stat

	for _, stat := range profile.ItemComponents.Stats.Data[instanceID].Stats {
		name := defs.Stats[stat.Hash]
		if name == "" {
			continue
		}

		stats = append(stats, Stat{Name: name, Value: stat.Value, Hash: stat.Hash})
	}

	slices.SortFunc(stats, func(a, b Stat) int { return cmp.Compare(a.Name, b.Name) })

	return stats
}

// Annotate replaces the tag, the autotag rule of the tag, the favorite
// flag, and the note of every item with the values that the service
// stores. The rule is the rule id of the row that holds the tag, as the
// web client reads it. The web client reads a rule only for a tag that it
// defines. A row without a rule id marks a tag that a person set.
func (s *Snapshot) Annotate(tags []dvm.ItemTag, notes []dvm.Note) {
	held := map[string][]string{}

	// rules maps the instance id and the tag id of each row to the rule id
	// of the row.
	rules := map[[2]string]string{}

	for _, row := range tags {
		if row.Removed {
			continue
		}

		held[row.InstanceID] = append(held[row.InstanceID], row.TagID)

		if row.RuleID != "" {
			rules[[2]string{row.InstanceID, row.TagID}] = row.RuleID
		}
	}

	bodies := map[string]string{}

	for _, note := range notes {
		if note.Kind == dvm.NoteInstance && !note.Removed {
			bodies[note.TargetID] = note.Body
		}
	}

	for _, item := range s.Items {
		ids := held[item.InstanceID]

		item.Favorite = slices.Contains(ids, dvm.TagFavorite)
		item.Tag = dvm.PrimaryTag(ids)
		item.TagRule = ""
		item.Note = bodies[item.InstanceID]

		if slices.Contains(dvm.TagOrder, item.Tag) {
			item.TagRule = rules[[2]string{item.InstanceID, item.Tag}]
		}
	}
}

// Find returns the item instance with the given id.
func (s *Snapshot) Find(instanceID string) (*Item, bool) {
	item, ok := s.byID[instanceID]

	return item, ok
}

// Character returns the character with the given id.
func (s *Snapshot) Character(id string) (Character, bool) {
	for _, c := range s.Characters {
		if c.ID == id {
			return c, true
		}
	}

	return Character{}, false
}

// LastPlayed returns the most recently played character, which
// [NewSnapshot] puts first in Characters. It returns false when the account
// has no character.
func (s *Snapshot) LastPlayed() (Character, bool) {
	if len(s.Characters) == 0 {
		return Character{}, false
	}

	return s.Characters[0], true
}

// ResolveOwner turns a location name into an owner value. A name is
// "vault", a character id, or a class name that exactly one character has.
func (s *Snapshot) ResolveOwner(name string) (string, error) {
	if strings.EqualFold(name, OwnerVault) {
		return OwnerVault, nil
	}

	c, err := s.matchCharacter(name, `"vault", a character id, or a class name`)
	if err != nil {
		return "", err
	}

	return c.ID, nil
}

// ResolveCharacter returns the character that name selects. A name is a
// character id or a class name that exactly one character has, in any
// letter case. An empty name selects the most recently played character.
func (s *Snapshot) ResolveCharacter(name string) (Character, error) {
	if name == "" {
		c, ok := s.LastPlayed()
		if !ok {
			return Character{}, fmt.Errorf("%w: the account has no characters", ErrUnknownTarget)
		}

		return c, nil
	}

	if strings.EqualFold(name, OwnerVault) {
		return Character{}, fmt.Errorf(
			"%w: the vault is not a character, so pass a character id or a class name", ErrUnknownTarget,
		)
	}

	return s.matchCharacter(name, "a character id or a class name")
}

// matchCharacter returns the character whose id is name, or the one
// character whose class is name in any letter case. Accepted describes the
// names that the caller takes, for the error that an unknown name returns.
func (s *Snapshot) matchCharacter(name, accepted string) (Character, error) {
	var matches []Character

	for _, c := range s.Characters {
		if c.ID == name {
			return c, nil
		}

		if strings.EqualFold(c.Class, name) {
			matches = append(matches, c)
		}
	}

	switch len(matches) {
	case 0:
		return Character{}, fmt.Errorf("%w: %q is not %s", ErrUnknownTarget, name, accepted)
	case 1:
		return matches[0], nil
	}

	ids := make([]string, 0, len(matches))
	for _, c := range matches {
		ids = append(ids, c.ID)
	}

	return Character{}, fmt.Errorf(
		"%w: %q matches characters %s, so pass a character id",
		ErrUnknownTarget, name, strings.Join(ids, ", "),
	)
}

// OwnerLabel names an owner value for display.
func (s *Snapshot) OwnerLabel(owner string) string {
	switch owner {
	case OwnerVault:
		return "Vault"
	case OwnerAccount:
		return "Account"
	}

	c, ok := s.Character(owner)
	if !ok || c.Class == "" {
		return "Character " + owner
	}

	return c.Class
}

// LocationLabel says where an item is, such as "Vault" or "Hunter
// (equipped)".
func (s *Snapshot) LocationLabel(item *Item) string {
	label := s.OwnerLabel(item.Owner)

	switch {
	case item.Postmaster:
		return label + " (postmaster)"
	case item.Equipped:
		return label + " (equipped)"
	default:
		return label
	}
}

// ItemFilter selects and orders items. The zero value selects every weapon
// and armor piece, ordered by name. String fields match without regard to
// case. Query, Slot, Perk, and Stat match names. Each compares a name with
// the field after [fold.Name] folds both, so it also matches without
// regard to accents.
type ItemFilter struct {
	// Query matches a substring of the item name or type name.
	Query string
	// Kind is "weapon", "armor", "gear" for both, "other", or "all".
	Kind string
	// Slot matches a substring of the slot name, such as "kinetic".
	Slot string
	// Tier is a rarity name, such as "exotic".
	Tier string
	// Class is the class that armor belongs to.
	Class string
	// Damage is a damage type name, such as "void".
	Damage string
	// Location is "vault", "postmaster", "equipped", "characters", a
	// character id, or a class name.
	Location string
	// Tag is a tag id, "none" for untagged items, or "any".
	Tag string
	// Perk matches a substring of a perk name.
	Perk string
	// Stat names the stat that MinStat and the "stat" sort read, such as
	// "health". It matches the full name of a stat that an item in the
	// snapshot has.
	Stat string
	// Sort is "name", "power" for the highest power first, "newest",
	// "total" for the highest stat total first, or "stat" for the highest
	// value of Stat first. The "power" sort puts the items without power
	// last.
	Sort string
	// MinPower and MaxPower bound the power level, inclusive. An item
	// without power passes neither bound. Zero turns a bound off.
	MinPower int
	MaxPower int
	// MinTotal is the lowest stat total to include. Only armor has a stat
	// total, so a positive MinTotal selects only armor. Zero turns the bound
	// off.
	MinTotal int
	// MinStat is the lowest value of Stat to include. A positive MinStat
	// rejects every item that lacks the stat. Zero turns the bound off.
	MinStat int
	// Favorite selects only favorites.
	Favorite bool
}

// Filter returns the items that match f, in the order that f.Sort names.
func (s *Snapshot) Filter(f ItemFilter) ([]*Item, error) {
	match, err := s.matcher(f)
	if err != nil {
		return nil, err
	}

	var out []*Item

	for _, item := range s.Items {
		if match(item) {
			out = append(out, item)
		}
	}

	switch strings.ToLower(f.Sort) {
	case "", SortName:
		slices.SortStableFunc(out, ByNameAndID)

	case SortPower:
		slices.SortStableFunc(out, func(a, b *Item) int {
			return cmp.Or(cmp.Compare(b.Power, a.Power), cmp.Compare(a.Name, b.Name))
		})

	case SortNewest:
		slices.SortStableFunc(out, func(a, b *Item) int {
			return CompareIDs(b.InstanceID, a.InstanceID)
		})

	case SortTotal:
		slices.SortStableFunc(out, func(a, b *Item) int {
			return cmp.Or(
				cmp.Compare(totalRank(b), totalRank(a)),
				cmp.Compare(a.Name, b.Name),
				CompareIDs(a.InstanceID, b.InstanceID),
			)
		})

	case SortStat:
		if f.Stat == "" {
			return nil, fmt.Errorf("%w: sort %q needs a stat name", ErrInvalidFilter, f.Sort)
		}

		stat := fold.Name(f.Stat)

		ranks := make(map[*Item]int, len(out))
		for _, item := range out {
			ranks[item] = statRank(item, stat)
		}

		slices.SortStableFunc(out, func(a, b *Item) int {
			return cmp.Or(
				cmp.Compare(ranks[b], ranks[a]),
				cmp.Compare(a.Name, b.Name),
				CompareIDs(a.InstanceID, b.InstanceID),
			)
		})

	default:
		return nil, fmt.Errorf(
			"%w: sort %q is not name, power, newest, total, or stat", ErrInvalidFilter, f.Sort,
		)
	}

	return out, nil
}

// totalRank returns the stat total of item. An item without a stat total
// ranks below every item that has one, including armor with a total of 0.
func totalRank(item *Item) int {
	total, ok := item.StatTotal()
	if !ok {
		return math.MinInt
	}

	return total
}

// statRank returns the value of the stat of item whose name equals folded
// after [fold.Name] folds the stat name. An item that lacks the stat ranks
// below every item that has it.
func statRank(item *Item, folded string) int {
	value, ok := item.foldedStatValue(folded)
	if !ok {
		return math.MinInt
	}

	return value
}

// statNames returns the names of the stats that the items of the snapshot
// have, in alphabetical order.
func (s *Snapshot) statNames() []string {
	var names []string

	for _, item := range s.Items {
		for _, stat := range item.Stats {
			if !slices.Contains(names, stat.Name) {
				names = append(names, stat.Name)
			}
		}
	}

	slices.Sort(names)

	return names
}

// checkStat reports an [ErrInvalidFilter] error when no item of the
// snapshot has a stat named name. An empty name passes.
func (s *Snapshot) checkStat(name string) error {
	if name == "" {
		return nil
	}

	names := s.statNames()

	switch {
	case slices.ContainsFunc(names, func(n string) bool { return fold.Equal(n, name) }):
		return nil
	case len(names) == 0:
		return fmt.Errorf("%w: no item in the inventory has stats", ErrInvalidFilter)
	default:
		return fmt.Errorf(
			"%w: no item in the inventory has a stat named %q, so pass one of %s",
			ErrInvalidFilter, name, strings.Join(names, ", "),
		)
	}
}

// CompareIDs orders instance ids by numeric value. Bungie issues ascending
// ids, so the order is also the acquisition order.
func CompareIDs(a, b string) int {
	return cmp.Or(cmp.Compare(len(a), len(b)), cmp.Compare(a, b))
}

// ByNameAndID orders items by name and then by instance id, as the default
// sort of list_items orders them.
func ByNameAndID(a, b *Item) int {
	return cmp.Or(cmp.Compare(a.Name, b.Name), CompareIDs(a.InstanceID, b.InstanceID))
}

func (s *Snapshot) matcher(f ItemFilter) (func(*Item) bool, error) {
	kind := strings.ToLower(f.Kind)

	switch kind {
	case "":
		kind = KindGear
	case KindWeapon, KindArmor, KindGear, KindOther, KindAll:
	default:
		return nil, fmt.Errorf("%w: kind %q is not weapon, armor, gear, other, or all", ErrInvalidFilter, f.Kind)
	}

	location := strings.ToLower(f.Location)

	owner := ""

	switch location {
	case "", LocationEquipped, LocationCharacters, LocationPostmaster:
	default:
		resolved, err := s.ResolveOwner(f.Location)
		if err != nil {
			return nil, err
		}

		owner = resolved
	}

	if f.MinStat > 0 && f.Stat == "" {
		return nil, fmt.Errorf("%w: a minimum stat value needs a stat name", ErrInvalidFilter)
	}

	err := s.checkStat(f.Stat)
	if err != nil {
		return nil, err
	}

	query := fold.Name(f.Query)
	slot := fold.Name(f.Slot)
	perk := fold.Name(f.Perk)
	stat := fold.Name(f.Stat)
	tag := strings.ToLower(f.Tag)

	return func(item *Item) bool {
		switch {
		case kind == KindGear && item.Kind == KindOther,
			kind != KindGear && kind != KindAll && item.Kind != kind,
			owner != "" && item.Owner != owner,
			location == LocationEquipped && !item.Equipped,
			location == LocationPostmaster && !item.Postmaster,
			location == LocationCharacters && (item.Owner == OwnerVault || item.Owner == OwnerAccount),
			query != "" && !containsFolded(item.Name, query) && !containsFolded(item.TypeName, query),
			slot != "" && !containsFolded(item.Slot, slot),
			f.Tier != "" && !strings.EqualFold(item.Tier, f.Tier),
			f.Class != "" && !strings.EqualFold(item.Class, f.Class),
			f.Damage != "" && !strings.EqualFold(item.Damage, f.Damage),
			f.MinPower > 0 && item.Power < f.MinPower,
			f.MaxPower > 0 && (item.Power == 0 || item.Power > f.MaxPower),
			f.MinTotal > 0 && totalRank(item) < f.MinTotal,
			f.MinStat > 0 && statRank(item, stat) < f.MinStat,
			f.Favorite && !item.Favorite,
			tag == TagNone && item.Tag != "",
			tag == TagAny && item.Tag == "",
			tag != "" && tag != TagNone && tag != TagAny && item.Tag != tag:
			return false
		}

		return perk == "" || slices.ContainsFunc(item.Perks, func(p Perk) bool {
			return containsFolded(p.Name, perk)
		})
	}, nil
}

// containsFolded reports whether s contains folded after [fold.Name] folds
// s. The caller folds the text that it looks for, so a filter over many
// items folds that text only one time.
func containsFolded(s, folded string) bool {
	return strings.Contains(fold.Name(s), folded)
}

// WeaponGroup holds every copy of one weapon. Name is the weapon name
// without a version suffix such as "(Adept)". A reissue has its own item
// hashes, but its copies share the group of the original weapon because
// the names match.
type WeaponGroup struct {
	Name   string
	Copies []*Item
}

// WeaponGroups returns the weapons of the snapshot grouped by
// [WeaponGroup.Name] and ordered by that name. The copies of each group
// follow the default order of [*Snapshot.Filter].
func (s *Snapshot) WeaponGroups() []WeaponGroup {
	var groups []WeaponGroup

	index := map[string]int{}

	for _, item := range s.Items {
		if item.Kind != KindWeapon {
			continue
		}

		name := baseName(item.Name)

		i, ok := index[name]
		if !ok {
			i = len(groups)
			index[name] = i
			groups = append(groups, WeaponGroup{Name: name})
		}

		groups[i].Copies = append(groups[i].Copies, item)
	}

	slices.SortFunc(groups, func(a, b WeaponGroup) int { return cmp.Compare(a.Name, b.Name) })

	for _, g := range groups {
		slices.SortFunc(g.Copies, ByNameAndID)
	}

	return groups
}

// CopiesOf returns the [WeaponGroup] that name selects. It removes a
// version suffix such as "(Adept)" from name in any letter case and with
// or without accents. It compares the rest with each group name. A group
// whose name equals the rest comes first. A group whose name equals the
// rest after [fold.Name] folds both comes second, so name can differ in
// letter case and in accents. When no group has the name, CopiesOf
// selects the one group whose name contains name without its suffix, or
// that holds a copy whose full name contains name. Both tests compare the
// texts after the same fold. A bare suffix such as "(Adept)" thus selects
// only a group that holds a copy of that version. Neither test takes a
// text that folds to the empty text, such as "^".
func (s *Snapshot) CopiesOf(name string) (WeaponGroup, error) {
	base := baseName(name)
	query := strings.TrimSpace(name)
	foldedBase := fold.Name(base)
	foldedQuery := fold.Name(query)

	var folded, similar []WeaponGroup

	for _, g := range s.WeaponGroups() {
		switch {
		case g.Name == base:
			return g, nil
		case fold.Equal(g.Name, base):
			folded = append(folded, g)
		case foldedBase != "" && fold.Contains(g.Name, base),
			foldedQuery != "" && slices.ContainsFunc(g.Copies, func(c *Item) bool { return fold.Contains(c.Name, query) }):
			similar = append(similar, g)
		}
	}

	if len(folded) > 0 {
		similar = folded
	}

	if len(similar) == 0 {
		return WeaponGroup{}, fmt.Errorf(
			"%w: no weapon in the inventory has a name that contains %q", ErrUnknownWeapon, name,
		)
	}

	if len(similar) == 1 {
		return similar[0], nil
	}

	names := make([]string, 0, min(len(similar), maxSuggestions))
	for _, g := range similar[:min(len(similar), maxSuggestions)] {
		names = append(names, g.Name)
	}

	return WeaponGroup{}, fmt.Errorf(
		"%w: %q matches %d weapons, so pass a full name, such as %s",
		ErrUnknownWeapon, name, len(similar), strings.Join(names, ", "),
	)
}

// GroupOf returns the [WeaponGroup] that holds item. An item that no group
// holds, such as a piece of armor, forms a group of its own.
func (s *Snapshot) GroupOf(item *Item) WeaponGroup {
	for _, g := range s.WeaponGroups() {
		if slices.Contains(g.Copies, item) {
			return g
		}
	}

	return WeaponGroup{Name: baseName(item.Name), Copies: []*Item{item}}
}

// baseName removes a version suffix from a weapon name. The suffix starts
// at the last opening parenthesis of the name, and baseName compares it
// with each version suffix after [fold.Name] folds both. The suffix can
// therefore differ in letter case and in accents.
func baseName(name string) string {
	name = strings.TrimSpace(name)

	i := strings.LastIndexByte(name, '(')
	if i < 0 {
		return name
	}

	suffix := fold.Name(name[i:])
	if slices.ContainsFunc(versionSuffixes, func(v string) bool { return fold.Name(v) == suffix }) {
		return strings.TrimSpace(name[:i])
	}

	return name
}
