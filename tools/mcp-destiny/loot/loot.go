package loot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
// drops. [Load] returns the table of the embedded data.
type Table struct {
	drops map[string][]string
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

// key folds a weapon name for a lookup.
func key(name string) string {
	return apostrophes.Replace(fold.Name(name))
}

// file is the loot data as loot.json stores it. Patch names the last game
// update that changed a drop of the data.
type file struct {
	Sources map[string]source `json:"sources"`
	Patch   string            `json:"patch"`
	Weapons []weapon          `json:"weapons"`
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
// least one source of the file.
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

	return t, nil
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
