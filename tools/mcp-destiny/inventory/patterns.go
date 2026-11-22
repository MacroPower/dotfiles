package inventory

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/fold"
)

// Status values of [PatternFilter].
const (
	PatternIncomplete = "incomplete"
	PatternComplete   = "complete"
	PatternAll        = "all"
)

// RaidSpoils is the number of Spoils of Conquest that the Deepsight
// activation of a raid weapon spends beside its one Deepsight Harmonizer.
// The activation of every other weapon spends only the Deepsight
// Harmonizer. Bungie announced both costs in This Week at Bungie of
// 2023-05-04, and they hold through Update 9.7.0.6. The Deepsight
// Resonance plugs share one material requirement. It fixes the count of
// Deepsight Harmonizers but leaves the count of Spoils of Conquest to the
// game. The profile therefore does not give the cost of one copy.
const RaidSpoils = 15

// Pattern is the crafting pattern of one weapon, as the record of the
// pattern in the Bungie profile tracks it. Name is the name of the record,
// which is the name of the weapon, and Hash is the hash of the record
// definition. Complete marks a pattern that the player can craft. Each
// entry of Objectives is one objective of the record, such as the count
// of the extractions that the pattern needs.
type Pattern struct {
	Name       string
	Objectives []Objective
	Hash       uint32
	Complete   bool
}

// Objective is the progress of one objective of a [Pattern]. Progress
// counts toward Completion.
type Objective struct {
	Progress   int
	Completion int
}

// Left returns the progress that the objectives of the pattern still
// lack, such as 2 for a pattern at 3 of 5 extractions.
func (p Pattern) Left() int {
	left := 0
	for _, o := range p.Objectives {
		left += max(o.Completion-o.Progress, 0)
	}

	return left
}

// newPattern returns the pattern that record tracks for the weapon name.
// Hash is the hash of the record definition.
func newPattern(name string, hash uint32, record bungie.ProfileRecord) Pattern {
	p := Pattern{
		Name:     name,
		Hash:     hash,
		Complete: record.State&bungie.RecordObjectiveNotCompleted == 0,
	}

	for _, o := range record.Objectives {
		p.Objectives = append(p.Objectives, Objective{Progress: o.Progress, Completion: o.CompletionValue})
	}

	return p
}

// patternsOf returns the pattern of every pattern record that the
// definitions name and the profile holds, ordered by name and then by
// hash.
func patternsOf(profile *bungie.Profile, defs *definitions.Defs) []Pattern {
	var patterns []Pattern

	for name, hash := range defs.Patterns {
		record, ok := profile.Record(hash)
		if ok {
			patterns = append(patterns, newPattern(name, hash, record))
		}
	}

	slices.SortFunc(patterns, func(a, b Pattern) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Hash, b.Hash))
	})

	return patterns
}

// patternRecord returns the name and the record hash of the pattern of
// the item of def with the name name. It reports false when the item has
// no pattern. Destiny Item Manager (DIM) looks up the pattern of a
// definition that names a recipe item, and of a definition in
// [bungie.BucketForbidden]. It finds the record of the pattern by the
// name of the item.
//
// The game also counts the extractions of an Adept, Timelost, or Harrowed
// copy toward the pattern of the base weapon. The definitions of those
// versions do not name a recipe item. The rules of DIM then do not find a
// pattern for such an item. For an item in a weapon bucket without a
// pattern, patternRecord therefore looks up the pattern of the name
// without its version suffix, as [baseName] gives it.
func patternRecord(defs *definitions.Defs, def definitions.ItemDef, name string) (string, uint32, bool) {
	hash, ok := defs.Patterns[name]
	ok = ok && (def.Craftable || def.Bucket == bungie.BucketForbidden)

	if base := baseName(name); !ok && base != name && inWeaponBucket(def) {
		name = base
		hash, ok = defs.Patterns[name]
	}

	if !ok {
		return "", 0, false
	}

	return name, hash, true
}

// patternItemsOf maps the record hash of each pattern that the
// definitions name to the item hashes of the craftable definitions with
// the name of the pattern, in ascending order. A pattern without such a
// definition has no entry.
func patternItemsOf(defs *definitions.Defs) map[uint32][]uint32 {
	items := map[uint32][]uint32{}

	for hash := range defs.Items {
		def := defs.Items[hash]

		record, ok := defs.Patterns[def.Name]
		if ok && def.Craftable {
			items[record] = append(items[record], hash)
		}
	}

	for _, hashes := range items {
		slices.Sort(hashes)
	}

	return items
}

// PatternHash returns the hash of the pattern record that a copy of the
// item with the given hash advances, such as the item of a vendor sale. A
// copy advances the pattern that [patternOf] gives it. The hash of an
// Adept, Timelost, or Harrowed version therefore returns the record of the
// base weapon. PatternHash reports false for an item that does not advance
// a pattern, such as the Onslaught version of a raid weapon. It does not
// read a profile, so it also returns a record that the profile lacks.
func PatternHash(defs *definitions.Defs, hash uint32) (uint32, bool) {
	_, record, ok := patternRecord(defs, defs.Items[hash], defs.ItemName(hash))

	return record, ok
}

// patternOf returns the pattern of the item of def with the name name, or
// nil when the item has none. It finds the record of the pattern by the
// rules of [patternRecord].
func patternOf(profile *bungie.Profile, defs *definitions.Defs, def definitions.ItemDef, name string) *Pattern {
	name, hash, ok := patternRecord(defs, def, name)
	if !ok {
		return nil
	}

	record, ok := profile.Record(hash)
	if !ok {
		return nil
	}

	p := newPattern(name, hash, record)

	return &p
}

// deepsightOf reports whether the instance of an item of def has
// Deepsight Resonance and whether a Deepsight Harmonizer can give it
// Deepsight Resonance. Pattern is the pattern of the item, or nil. The
// rules follow Destiny Item Manager (DIM):
//
//   - Only an item whose definition puts it in a weapon bucket or in
//     [bungie.BucketForbidden] can have Deepsight Resonance. Such an item
//     has it when the first socket that holds a plug of
//     [bungie.PlugCategoryResonance] is visible.
//   - A Deepsight Harmonizer can give an item with an incomplete pattern
//     Deepsight Resonance when a visible socket holds a plug of
//     [bungie.PlugCategoryHarmonizer]. The pattern of an item without a
//     pattern record counts as incomplete.
func deepsightOf(
	profile *bungie.Profile,
	defs *definitions.Defs,
	def definitions.ItemDef,
	instanceID string,
	pattern *Pattern,
) (bool, bool) {
	weapon := inWeaponBucket(def)

	var (
		resonance, harmonizer bool
		found                 bool
	)

	for _, socket := range profile.ItemComponents.Sockets.Data[instanceID].Sockets {
		switch defs.PlugCategories[socket.PlugHash] {
		case bungie.PlugCategoryResonance:
			if weapon && !found {
				resonance = socket.IsVisible
				found = true
			}

		case bungie.PlugCategoryHarmonizer:
			harmonizer = harmonizer || socket.IsVisible
		}
	}

	return resonance, harmonizer && (pattern == nil || !pattern.Complete)
}

// inWeaponBucket reports whether def puts its item in a weapon bucket or
// in [bungie.BucketForbidden].
func inWeaponBucket(def definitions.ItemDef) bool {
	return slices.Contains(
		[]uint32{bungie.BucketKinetic, bungie.BucketEnergy, bungie.BucketPower, bungie.BucketForbidden},
		def.Bucket,
	)
}

// PatternFilter selects the patterns of [*Snapshot.PatternRows]. The zero
// value selects every incomplete pattern.
type PatternFilter struct {
	// Query matches a substring of the weapon name after
	// [fold.Lookalike] folds both.
	Query string
	// Status is "incomplete", "complete", or "all". The empty Status
	// selects the incomplete patterns.
	Status string
	// Harmonizable selects only the patterns with an owned copy that a
	// Deepsight Harmonizer can give Deepsight Resonance.
	Harmonizable bool
}

// PatternRow is one pattern of [*Snapshot.PatternRows]. Copies lists the
// owned copies of the weapon of the pattern, ordered as
// [ByNameAndID] orders them. ItemHashes lists in ascending order the item
// hashes of the craftable definitions with the name of the pattern. It
// leaves out each definition with that name that does not name a recipe
// item, such as the Onslaught version of a raid weapon. It is empty when
// the definitions of the snapshot do not hold a craftable definition with
// that name.
type PatternRow struct {
	Copies     []*Item
	ItemHashes []uint32

	Pattern
}

// PatternRows returns the patterns of the snapshot that f selects, with the
// owned copies of each weapon. Each copy is an instanced item whose
// [Item.Pattern] names the record of the row. The incomplete patterns come
// first, with the fewest extractions left first. The complete patterns
// follow. Two rows in one place come in the order of their names. It
// returns [ErrInvalidFilter] for an unknown Status.
func (s *Snapshot) PatternRows(f PatternFilter) ([]PatternRow, error) {
	status := strings.ToLower(f.Status)

	switch status {
	case "", PatternIncomplete, PatternComplete, PatternAll:
	default:
		return nil, fmt.Errorf("%w: unknown status %q, pass %s, %s, or %s",
			ErrInvalidFilter, f.Status, PatternIncomplete, PatternComplete, PatternAll)
	}

	copies := map[uint32][]*Item{}

	for _, item := range s.Items {
		if item.Pattern != nil && item.InstanceID != "" {
			copies[item.Pattern.Hash] = append(copies[item.Pattern.Hash], item)
		}
	}

	query := fold.Lookalike.Text(f.Query)

	var rows []PatternRow

	for _, p := range s.Patterns {
		row := PatternRow{Pattern: p, Copies: copies[p.Hash], ItemHashes: s.patternItems[p.Hash]}

		keep := query.In(p.Name) &&
			(status == PatternAll || p.Complete == (status == PatternComplete)) &&
			(!f.Harmonizable || slices.ContainsFunc(row.Copies, func(item *Item) bool { return item.Harmonizable }))
		if !keep {
			continue
		}

		slices.SortStableFunc(row.Copies, ByNameAndID)

		rows = append(rows, row)
	}

	slices.SortStableFunc(rows, func(a, b PatternRow) int {
		return cmp.Or(
			compareComplete(a.Complete, b.Complete),
			cmp.Compare(a.Left(), b.Left()),
			cmp.Compare(a.Name, b.Name),
			cmp.Compare(a.Hash, b.Hash),
		)
	})

	return rows, nil
}

// compareComplete orders an incomplete pattern before a complete one.
func compareComplete(a, b bool) int {
	switch {
	case a == b:
		return 0
	case b:
		return -1
	default:
		return 1
	}
}
