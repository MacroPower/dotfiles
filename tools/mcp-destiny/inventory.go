package main

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Owner values of items that no character holds.
const (
	ownerVault   = "vault"
	ownerAccount = "account"
)

// Kind values that classify an [Item].
const (
	kindWeapon = "weapon"
	kindArmor  = "armor"
	kindOther  = "other"
)

// Filter keywords that [ItemFilter] accepts beside literal values.
const (
	kindGear = "gear"
	kindAll  = "all"

	locationEquipped   = "equipped"
	locationCharacters = "characters"
	locationPostmaster = "postmaster"

	tagNone = "none"
	tagAny  = "any"

	sortName   = "name"
	sortPower  = "power"
	sortNewest = "newest"
)

// tagFavorite is the tag id that marks a favorite. An item carries it
// beside its one ordinary tag.
const tagFavorite = "favorite"

var (
	// tagOrder lists the tag ids that the web client defines, in its
	// display order. The web client ignores stored tag definitions.
	tagOrder = []string{"keep", "pvp", "pve", "junk", "infuse", "archive"}

	// ErrInvalidFilter reports an [ItemFilter] field with an unknown value.
	ErrInvalidFilter = errors.New("invalid filter")

	// ErrUnknownTarget reports a location name that matches neither the
	// vault nor exactly one character.
	ErrUnknownTarget = errors.New("unknown location")
)

// Perk is one plug in a visible socket of an item, such as a weapon trait
// or an armor mod. Description is the text of the plug definition, which
// is empty for some plugs. Options names the other plugs that the socket
// can hold.
type Perk struct {
	Name        string
	Type        string
	Description string
	Options     []string
}

// Stat is one named stat value of an item instance.
type Stat struct {
	Name  string
	Value int
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
// a character id, "vault", or "account" for account-wide stacks.
type Item struct {
	InstanceID   string
	Name         string
	TypeName     string
	Tier         string
	Slot         string
	Class        string
	Damage       string
	Owner        string
	Kind         string
	Tag          string
	Note         string
	Perks        []Perk
	Stats        []Stat
	ClassType    int
	Power        int
	Quantity     int
	Hash         uint32
	Equipped     bool
	Postmaster   bool
	Locked       bool
	Masterwork   bool
	Crafted      bool
	Transferable bool
	Equippable   bool
	Lockable     bool
	Favorite     bool
}

// Snapshot is the inventory of every character and the vault at one moment.
// Moves and lock changes update it in place, because Bungie can serve the
// state from before a change for some seconds after the change.
type Snapshot struct {
	Fetched    time.Time
	byID       map[string]*Item
	Characters []Character
	Items      []*Item
}

// NewSnapshot resolves a Bungie profile against the definitions.
func NewSnapshot(profile *Profile, defs *Defs, fetched time.Time) *Snapshot {
	s := &Snapshot{
		Fetched: fetched,
		byID:    map[string]*Item{},
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

	for _, pi := range profile.ProfileInventory.Data.Items {
		owner := ownerAccount
		if pi.BucketHash == bucketVault || pi.Location == itemLocationVault {
			owner = ownerVault
		}

		s.add(newItem(profile, defs, pi, owner, false))
	}

	for id, inv := range profile.CharacterInventories.Data {
		for _, pi := range inv.Items {
			s.add(newItem(profile, defs, pi, id, false))
		}
	}

	for id, inv := range profile.CharacterEquipment.Data {
		for _, pi := range inv.Items {
			s.add(newItem(profile, defs, pi, id, true))
		}
	}

	return s
}

func (s *Snapshot) add(item *Item) {
	s.Items = append(s.Items, item)

	if item.InstanceID != "" {
		s.byID[item.InstanceID] = item
	}
}

func newItem(profile *Profile, defs *Defs, pi ProfileItem, owner string, equipped bool) *Item {
	def, ok := defs.Items[pi.Hash]
	if !ok {
		def = ItemDef{Name: fmt.Sprintf("Unknown item %d", pi.Hash), ClassType: classAny}
	}

	postmaster := pi.BucketHash == bucketPostmaster || pi.Location == itemLocationPostmaster

	// Bungie can report an item as not transferable for as long as it sits
	// in the Postmaster. A move pulls the item first, so the definition
	// alone decides whether a Postmaster item transfers.
	transferable := !def.NonTransferable &&
		(postmaster || pi.TransferStatus&transferNotTransferable == 0)

	slot := pi.BucketHash
	if slot == bucketVault || slot == bucketPostmaster {
		slot = def.Bucket
	}

	item := &Item{
		InstanceID:   pi.InstanceID,
		Hash:         pi.Hash,
		Name:         def.Name,
		TypeName:     def.TypeName,
		Tier:         def.Tier,
		Slot:         defs.Buckets[slot],
		Kind:         kindOf(def),
		ClassType:    def.ClassType,
		Owner:        owner,
		Quantity:     pi.Quantity,
		Equipped:     equipped,
		Postmaster:   postmaster,
		Locked:       pi.State&stateLocked != 0,
		Masterwork:   pi.State&stateMasterwork != 0,
		Crafted:      pi.State&stateCrafted != 0,
		Transferable: transferable,
		Equippable:   def.Equippable,
		Lockable:     pi.Lockable,
	}

	if def.ClassType != classAny {
		item.Class = defs.Classes[def.ClassType]
	}

	damage := def.DamageType

	inst, ok := profile.ItemComponents.Instances.Data[pi.InstanceID]
	if ok {
		if inst.PrimaryStat != nil {
			item.Power = inst.PrimaryStat.Value
		}

		if inst.DamageType != 0 {
			damage = inst.DamageType
		}
	}

	if damage != 0 {
		item.Damage = defs.DamageTypes[damage]
	}

	item.Perks = perksOf(profile, defs, pi.InstanceID)
	item.Stats = statsOf(profile, defs, pi.InstanceID)

	return item
}

func kindOf(def ItemDef) string {
	switch def.ItemType {
	case itemTypeWeapon:
		return kindWeapon
	case itemTypeArmor:
		return kindArmor
	default:
		return kindOther
	}
}

func perksOf(profile *Profile, defs *Defs, instanceID string) []Perk {
	var perks []Perk

	options := profile.ItemComponents.ReusablePlugs.Data[instanceID].Plugs

	for i, socket := range profile.ItemComponents.Sockets.Data[instanceID].Sockets {
		plug, ok := defs.Items[socket.PlugHash]
		if !socket.IsVisible || !ok || !isPerk(plug) {
			continue
		}

		perk := Perk{Name: plug.Name, Type: plug.TypeName, Description: plug.Description}

		for _, option := range options[strconv.Itoa(i)] {
			def, ok := defs.Items[option.Hash]
			if !ok || def.Name == plug.Name || slices.Contains(perk.Options, def.Name) {
				continue
			}

			perk.Options = append(perk.Options, def.Name)
		}

		perks = append(perks, perk)
	}

	return perks
}

// isPerk reports whether a plug changes how an item plays. It rejects
// cosmetic plugs and the placeholders of empty sockets.
func isPerk(plug ItemDef) bool {
	if plug.Name == "" || plug.TypeName == "" || strings.HasPrefix(plug.Name, "Empty ") {
		return false
	}

	switch plug.TypeName {
	case "Shader", "Memento", "Restore Defaults", "Transmat Effect", "Ghost Projection", "Emote":
		return false
	}

	return !strings.Contains(plug.TypeName, "Ornament")
}

func statsOf(profile *Profile, defs *Defs, instanceID string) []Stat {
	var stats []Stat

	for _, stat := range profile.ItemComponents.Stats.Data[instanceID].Stats {
		name := defs.Stats[stat.Hash]
		if name == "" {
			continue
		}

		stats = append(stats, Stat{Name: name, Value: stat.Value})
	}

	slices.SortFunc(stats, func(a, b Stat) int { return cmp.Compare(a.Name, b.Name) })

	return stats
}

// Annotate replaces the tag, favorite flag, and note of every item with the
// values that the service stores.
func (s *Snapshot) Annotate(tags []ItemTag, notes []Note) {
	held := map[string][]string{}

	for _, row := range tags {
		if !row.Removed {
			held[row.InstanceID] = append(held[row.InstanceID], row.TagID)
		}
	}

	bodies := map[string]string{}

	for _, note := range notes {
		if note.Kind == noteInstance && !note.Removed {
			bodies[note.TargetID] = note.Body
		}
	}

	for _, item := range s.Items {
		ids := held[item.InstanceID]

		item.Favorite = slices.Contains(ids, tagFavorite)
		item.Tag = primaryTag(ids)
		item.Note = bodies[item.InstanceID]
	}
}

// primaryTag returns the one ordinary tag that the web client shows for an
// item. Storage allows several, so the web client display order decides.
func primaryTag(ids []string) string {
	for _, id := range tagOrder {
		if slices.Contains(ids, id) {
			return id
		}
	}

	rest := slices.DeleteFunc(slices.Clone(ids), func(id string) bool { return id == tagFavorite })
	if len(rest) == 0 {
		return ""
	}

	return slices.Min(rest)
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

// ResolveOwner turns a location name into an owner value. A name is
// "vault", a character id, or a class name that exactly one character has.
func (s *Snapshot) ResolveOwner(name string) (string, error) {
	if strings.EqualFold(name, ownerVault) {
		return ownerVault, nil
	}

	var matches []string

	for _, c := range s.Characters {
		if c.ID == name {
			return c.ID, nil
		}

		if strings.EqualFold(c.Class, name) {
			matches = append(matches, c.ID)
		}
	}

	if len(matches) == 1 {
		return matches[0], nil
	}

	if len(matches) > 1 {
		return "", fmt.Errorf(
			"%w: %q matches characters %s, so pass a character id",
			ErrUnknownTarget, name, strings.Join(matches, ", "),
		)
	}

	return "", fmt.Errorf("%w: %q is not \"vault\", a character id, or a class name", ErrUnknownTarget, name)
}

// OwnerLabel names an owner value for display.
func (s *Snapshot) OwnerLabel(owner string) string {
	switch owner {
	case ownerVault:
		return "Vault"
	case ownerAccount:
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
// case.
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
	// Sort is "name", "power", or "newest".
	Sort string
	// MinPower and MaxPower bound the power level, inclusive. Zero turns a
	// bound off.
	MinPower int
	MaxPower int
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
	case "", sortName:
		slices.SortStableFunc(out, func(a, b *Item) int {
			return cmp.Or(cmp.Compare(a.Name, b.Name), compareIDs(a.InstanceID, b.InstanceID))
		})

	case sortPower:
		slices.SortStableFunc(out, func(a, b *Item) int {
			return cmp.Or(cmp.Compare(b.Power, a.Power), cmp.Compare(a.Name, b.Name))
		})

	case sortNewest:
		slices.SortStableFunc(out, func(a, b *Item) int {
			return compareIDs(b.InstanceID, a.InstanceID)
		})

	default:
		return nil, fmt.Errorf("%w: sort %q is not name, power, or newest", ErrInvalidFilter, f.Sort)
	}

	return out, nil
}

// compareIDs orders instance ids by numeric value. Bungie issues ascending
// ids, so the order is also the acquisition order.
func compareIDs(a, b string) int {
	return cmp.Or(cmp.Compare(len(a), len(b)), cmp.Compare(a, b))
}

func (s *Snapshot) matcher(f ItemFilter) (func(*Item) bool, error) {
	kind := strings.ToLower(f.Kind)

	switch kind {
	case "":
		kind = kindGear
	case kindWeapon, kindArmor, kindGear, kindOther, kindAll:
	default:
		return nil, fmt.Errorf("%w: kind %q is not weapon, armor, gear, other, or all", ErrInvalidFilter, f.Kind)
	}

	location := strings.ToLower(f.Location)

	owner := ""

	switch location {
	case "", locationEquipped, locationCharacters, locationPostmaster:
	default:
		resolved, err := s.ResolveOwner(f.Location)
		if err != nil {
			return nil, err
		}

		owner = resolved
	}

	query := strings.ToLower(f.Query)
	slot := strings.ToLower(f.Slot)
	perk := strings.ToLower(f.Perk)
	tag := strings.ToLower(f.Tag)

	return func(item *Item) bool {
		switch {
		case kind == kindGear && item.Kind == kindOther,
			kind != kindGear && kind != kindAll && item.Kind != kind,
			owner != "" && item.Owner != owner,
			location == locationEquipped && !item.Equipped,
			location == locationPostmaster && !item.Postmaster,
			location == locationCharacters && (item.Owner == ownerVault || item.Owner == ownerAccount),
			query != "" && !containsFold(item.Name, query) && !containsFold(item.TypeName, query),
			slot != "" && !containsFold(item.Slot, slot),
			f.Tier != "" && !strings.EqualFold(item.Tier, f.Tier),
			f.Class != "" && !strings.EqualFold(item.Class, f.Class),
			f.Damage != "" && !strings.EqualFold(item.Damage, f.Damage),
			f.MinPower > 0 && item.Power < f.MinPower,
			f.MaxPower > 0 && item.Power > f.MaxPower,
			f.Favorite && !item.Favorite,
			tag == tagNone && item.Tag != "",
			tag == tagAny && item.Tag == "",
			tag != "" && tag != tagNone && tag != tagAny && item.Tag != tag:
			return false
		}

		return perk == "" || slices.ContainsFunc(item.Perks, func(p Perk) bool {
			return containsFold(p.Name, perk)
		})
	}, nil
}

// containsFold reports whether s contains the lowercase string lower,
// without regard to the case of s.
func containsFold(s, lower string) bool {
	return strings.Contains(strings.ToLower(s), lower)
}
