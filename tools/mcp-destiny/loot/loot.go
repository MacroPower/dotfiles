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
	// check, such as a drop without a source.
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
	drops      map[string][]string
	activities []activity
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
// in the order of the data. It returns nil for a weapon that the data
// does not list. The lookup folds letter case and diacritics as
// [fold.Name] does, and it reads a curly apostrophe as a straight one.
func (t *Table) Drops(name string) []string {
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
// update that changed a drop of the data. Activities maps each place that
// is an activity to the name that the Destiny definitions give the
// activity.
type file struct {
	Sources    map[string]source `json:"sources"`
	Activities map[string]string `json:"activities"`
	Patch      string            `json:"patch"`
	Weapons    []weapon          `json:"weapons"`
}

// source is one origin of the drops. Patch is the game update that the
// source describes.
type source struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Date  string `json:"date"`
	Patch string `json:"patch"`
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

// parse decodes the loot data and checks it. Each weapon must have a name
// of its own, at least one drop, and the update of its last check. Each drop
// must name a place that the weapon does not repeat. It must also name at
// least one source of the file. Each activity must belong to a place that
// a drop names, and it must have a name that no other activity has.
func parse(data []byte) (*Table, error) {
	var f file

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	err := dec.Decode(&f)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrData, err)
	}

	t := &Table{drops: map[string][]string{}}

	for i, w := range f.Weapons {
		k := key(w.Name)
		if _, ok := t.drops[k]; ok || w.Name == "" {
			return nil, fmt.Errorf("%w: weapon %d has the name %q, which is empty or taken", ErrData, i, w.Name)
		}

		places, err := f.places(w)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrData, w.Name, err)
		}

		t.drops[k] = places
	}

	activities, err := f.activities()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrData, err)
	}

	t.activities = activities

	return t, nil
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

// places checks the drops of one weapon and returns the place of each
// drop, in the order of the data.
func (f *file) places(w weapon) ([]string, error) {
	if w.Checked == "" {
		return nil, errors.New("the weapon names no game update of its last check")
	}

	if len(w.Drops) == 0 {
		return nil, errors.New("the weapon has no drop")
	}

	places := make([]string, 0, len(w.Drops))

	for _, d := range w.Drops {
		if d.From == "" || slices.Contains(places, d.From) {
			return nil, fmt.Errorf("the drop from %q is empty or repeats", d.From)
		}

		if len(d.Sources) == 0 {
			return nil, fmt.Errorf("the drop from %q names no source", d.From)
		}

		for _, s := range d.Sources {
			if _, ok := f.Sources[s]; !ok {
				return nil, fmt.Errorf("the drop from %q names %q, which is not a source of the file", d.From, s)
			}
		}

		places = append(places, d.From)
	}

	return places, nil
}
