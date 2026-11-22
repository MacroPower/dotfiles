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

// patternOf returns the pattern of the item of def with the name name, or
// nil when the item has none. Destiny Item Manager (DIM) looks up the
// pattern of a definition that names a recipe item, and of a definition
// in [bungie.BucketForbidden]. It finds the record of the pattern by the
// name of the item.
//
// The game also counts the extractions of an Adept, Timelost, or Harrowed
// copy toward the pattern of the base weapon. The definitions of those
// versions name no recipe item. So when the rules of DIM find no pattern
// for an item in a weapon bucket, patternOf looks up the pattern of the
// name without its version suffix, as [baseName] gives it.
func patternOf(profile *bungie.Profile, defs *definitions.Defs, def definitions.ItemDef, name string) *Pattern {
	hash, ok := defs.Patterns[name]
	ok = ok && (def.Craftable || def.Bucket == bungie.BucketForbidden)

	if base := baseName(name); !ok && base != name && inWeaponBucket(def) {
		name = base
		hash, ok = defs.Patterns[name]
	}

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
// [ByNameAndID] orders them.
type PatternRow struct {
	Copies []*Item

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
		row := PatternRow{Pattern: p, Copies: copies[p.Hash]}

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
