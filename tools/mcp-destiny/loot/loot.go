package loot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	_ "embed"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/fold"
)

var (
	// ErrData reports loot data that does not decode or that fails a
	// check, such as a drop without a source or a place without a red
	// border note.
	ErrData = errors.New("loot data")

	// embedded holds the loot data of the package.
	//
	//go:embed loot.json
	embedded []byte

	// load decodes and checks the embedded loot data one time.
	load = sync.OnceValues(func() (*Table, error) { return parse(embedded) })

	// apostrophes reads each curly apostrophe as a straight one.
	apostrophes = strings.NewReplacer("\u2018", "'", "\u2019", "'")
)

// Table maps each weapon of the loot data to the places where the weapon
// drops. It also maps each activity of the loot data to its place.
// [Load] returns the table of the embedded data.
type Table struct {
	drops      map[string][]Drop
	activities []activity
}

// Drop is one place where a weapon drops. Place names the place, such as
// "Vault of Glass raid" or "Xûr". RedBorders tells how a player gets
// copies with Deepsight Resonance, the red border of the game, at the
// place, such as with a modifier of an activity or with a Deepsight
// Harmonizer. Every weapon that drops at one place shares its note.
type Drop struct {
	Place      string
	RedBorders string
}

// activity is the activity of one place, such as the Vault of Glass raid.
// Name is the name that the Destiny definitions give the activity, as
// key folds it.
type activity struct {
	place string
	name  string
}

// Load returns the [*Table] of the embedded loot data. It decodes and
// checks the data on the first call. It returns [ErrData] when the data
// does not decode or fails a check.
func Load() (*Table, error) {
	return load()
}

// Drops returns the places where the weapon with the given name drops,
// in the order of the data, each with its red border note. It returns nil
// for a weapon that the data does not list. The lookup folds letter case
// and diacritics as [fold.Name] does, and it reads a curly apostrophe as
// a straight one.
func (t *Table) Drops(name string) []Drop {
	return t.drops[key(name)]
}

// Place returns the place of the table that holds the activity with the
// given name. An activity belongs to a place when its name equals the
// activity name of the place, or when it continues that name with ": " or
// " (". The activity "Vault of Glass: Master" therefore belongs to the
// place "Vault of Glass raid". The comparison folds both names as
// [*Table.Drops] folds a weapon name. When the names of several places
// match, the longest name wins. Place reports false when no place
// matches, as for a strike or a vendor.
func (t *Table) Place(name string) (string, bool) {
	name = key(name)

	var (
		best  activity
		found bool
	)

	for _, a := range t.activities {
		match := name == a.name || strings.HasPrefix(name, a.name+": ") || strings.HasPrefix(name, a.name+" (")
		if match && len(a.name) > len(best.name) {
			best, found = a, true
		}
	}

	return best.place, found
}

// key folds a weapon name or an activity name for a lookup.
func key(name string) string {
	return apostrophes.Replace(fold.Name(name))
}

// file is the loot data as loot.json stores it. Patch names the last game
// update that changed a drop or a red border note of the data. Places
// holds the red border note of each place that a drop names. Activities
// maps each place that is an activity to the name that the Destiny
// definitions give the activity.
type file struct {
	Sources    map[string]source `json:"sources"`
	Places     map[string]place  `json:"places"`
	Activities map[string]string `json:"activities"`
	Patch      string            `json:"patch"`
	Weapons    []weapon          `json:"weapons"`
}

// source is one origin of the drops or of the red border notes. Patch is
// the game update that the source describes.
type source struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Date  string `json:"date"`
	Patch string `json:"patch"`
}

// place tells how a player gets red borders at one place. Each entry of
// Sources keys the sources of the file. Checked names the last game update
// whose patch notes leave the note as it stands.
type place struct {
	RedBorders string   `json:"redBorders"`
	Checked    string   `json:"checked"`
	Sources    []string `json:"sources"`
}

// weapon lists the places where one weapon drops. Checked names the last
// game update whose patch notes leave the drops as they stand. Note
// explains a choice between sources that disagree, and it is empty for
// most weapons.
type weapon struct {
	Name    string `json:"name"`
	Note    string `json:"note"`
	Checked string `json:"checked"`
	Drops   []drop `json:"drops"`
}

// drop names one place where a weapon drops. Each entry of Sources keys
// the sources of the file.
type drop struct {
	From    string   `json:"from"`
	Sources []string `json:"sources"`
}

// parse decodes the loot data and checks it. Each place must have a red
// border note, at least one source of the file, and the update of its last
// check. At least one weapon must drop at each place. Each weapon must
// have a name of its own, at least one drop, and the update of its last
// check. Each drop must name a place of the file that the weapon does not
// repeat. It must also name at least one source of the file. Each activity
// must belong to a place that a drop names, and it must have a name that
// no other activity has.
func parse(data []byte) (*Table, error) {
	var f file

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	err := dec.Decode(&f)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrData, err)
	}

	for _, name := range slices.Sorted(maps.Keys(f.Places)) {
		err := f.checkPlace(f.Places[name])
		if err != nil {
			return nil, fmt.Errorf("%w: place %q: %w", ErrData, name, err)
		}
	}

	t := &Table{drops: map[string][]Drop{}}
	used := map[string]bool{}

	for i, w := range f.Weapons {
		k := key(w.Name)
		if _, ok := t.drops[k]; ok || w.Name == "" {
			return nil, fmt.Errorf("%w: weapon %d has the name %q, which is empty or taken", ErrData, i, w.Name)
		}

		drops, err := f.drops(w)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrData, w.Name, err)
		}

		for _, d := range drops {
			used[d.Place] = true
		}

		t.drops[k] = drops
	}

	for _, name := range slices.Sorted(maps.Keys(f.Places)) {
		if !used[name] {
			return nil, fmt.Errorf("%w: place %q: no weapon drops there", ErrData, name)
		}
	}

	activities, err := f.activities()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrData, err)
	}

	t.activities = activities

	return t, nil
}

// checkPlace checks the red border note of one place.
func (f *file) checkPlace(p place) error {
	switch {
	case p.RedBorders == "":
		return errors.New("the place has no red border note")
	case p.Checked == "":
		return errors.New("the place names no game update of its last check")
	}

	err := f.checkSources(p.Sources)
	if err != nil {
		return fmt.Errorf("the place %w", err)
	}

	return nil
}

// activities checks the activities of the file and returns them, ordered
// by place.
func (f *file) activities() ([]activity, error) {
	places := map[string]bool{}

	for _, w := range f.Weapons {
		for _, d := range w.Drops {
			places[d.From] = true
		}
	}

	var activities []activity

	names := map[string]string{}

	for _, place := range slices.Sorted(maps.Keys(f.Activities)) {
		name := key(f.Activities[place])

		switch other, taken := names[name]; {
		case !places[place]:
			return nil, fmt.Errorf("the activity of %q belongs to a place that no drop names", place)
		case name == "":
			return nil, fmt.Errorf("the activity of %q has no name", place)
		case taken:
			return nil, fmt.Errorf("the activities of %q and %q share the name %q", other, place, f.Activities[place])
		}

		names[name] = place
		activities = append(activities, activity{place: place, name: name})
	}

	return activities, nil
}

// drops checks the drops of one weapon and returns them in the order of
// the data, each with the red border note of its place.
func (f *file) drops(w weapon) ([]Drop, error) {
	if w.Checked == "" {
		return nil, errors.New("the weapon names no game update of its last check")
	}

	if len(w.Drops) == 0 {
		return nil, errors.New("the weapon has no drop")
	}

	drops := make([]Drop, 0, len(w.Drops))

	for _, d := range w.Drops {
		repeats := slices.ContainsFunc(drops, func(o Drop) bool { return o.Place == d.From })
		if d.From == "" || repeats {
			return nil, fmt.Errorf("the drop from %q is empty or repeats", d.From)
		}

		p, ok := f.Places[d.From]
		if !ok {
			return nil, fmt.Errorf("the drop from %q names a place that the file does not describe", d.From)
		}

		err := f.checkSources(d.Sources)
		if err != nil {
			return nil, fmt.Errorf("the drop from %q %w", d.From, err)
		}

		drops = append(drops, Drop{Place: d.From, RedBorders: p.RedBorders})
	}

	return drops, nil
}

// checkSources checks that names holds at least one source and that each
// entry keys a source of the file. The text of its error opens with a
// verb, so the caller puts the subject before it.
func (f *file) checkSources(names []string) error {
	if len(names) == 0 {
		return errors.New("names no source")
	}

	for _, s := range names {
		if _, ok := f.Sources[s]; !ok {
			return fmt.Errorf("names %q, which is not a source of the file", s)
		}
	}

	return nil
}
