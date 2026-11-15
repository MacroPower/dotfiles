package curves

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	_ "embed"
)

// Stat hashes of the weapon stats that the curves read.
const (
	StatReload   uint32 = 4188031367
	StatHandling uint32 = 943549884
	StatRange    uint32 = 1240592695
)

// Reasons of a weapon without curves.
const (
	reasonType  = "The community data has no curves for this weapon type."
	reasonFrame = "The community data has no curves for the frame of this weapon."
	reasonPlugs = "The profile lists no perks for this copy, so the server cannot tell its frame."
)

// statCap is the highest stat value that a curve reads when the curve
// sets no cap of its own.
const statCap = 100

var (
	// ErrData reports curve data that does not decode or that fails a
	// check, such as a curve without a source.
	ErrData = errors.New("curve data")

	// embedded holds the curve data of the package.
	//
	//go:embed curves.json
	embedded []byte

	// load decodes and checks the embedded curve data one time.
	load = sync.OnceValues(func() (*table, error) { return parse(embedded) })
)

// Weapon describes one weapon copy. Type is the English name of the item
// type, such as "Auto Rifle", and Hash is the item hash. Plugs holds the
// plug hash of each socket of the copy, and one of them is the frame.
// Stats maps the hash of each stat of the copy to its value.
type Weapon struct {
	Stats map[uint32]int
	Type  string
	Plugs []uint32
	Hash  uint32
}

// Measure is one value of a weapon copy, in seconds or in meters. Reason
// is empty for a known value. For an unknown value, Reason holds a
// sentence that says why, and Value is zero.
type Measure struct {
	Reason string
	Value  float64
}

// Known reports whether the measure holds a value.
func (m Measure) Known() bool {
	return m.Reason == ""
}

// Measures holds the times of a weapon copy in seconds and the distances
// at which its damage falloff starts in meters. Reload is the time of a
// full reload. Ready and Stow are the times to draw the weapon and to put
// it away, and Aim is the time to aim down sights. HipFalloff is the
// distance from the hip, and AimFalloff is the distance while the copy
// aims down sights.
type Measures struct {
	Reload     Measure
	Ready      Measure
	Stow       Measure
	Aim        Measure
	HipFalloff Measure
	AimFalloff Measure
}

// Of returns the [Measures] of a weapon copy at its stats. Each measure
// that the curve data cannot give carries its reason. A copy without
// plugs takes its curves only from its item hash. When the data does not
// list that hash, the reason names the missing plugs, because the frame
// of the copy is unknown.
func Of(w Weapon) Measures {
	t, err := load()
	if err != nil {
		return unknown(fmt.Sprintf("The server cannot read its curve data: %v.", err))
	}

	tt, ok := t.types[w.Type]
	if !ok {
		return unknown(reasonType)
	}

	e, ok := t.entryOf(tt, w)

	switch {
	case ok:
		return e.measures(w.Stats)
	case len(w.Plugs) == 0:
		return unknown(reasonPlugs)
	default:
		return unknown(reasonFrame)
	}
}

// unknown returns measures that all carry one reason.
func unknown(reason string) Measures {
	m := Measure{Reason: reason}

	return Measures{Reload: m, Ready: m, Stow: m, Aim: m, HipFalloff: m, AimFalloff: m}
}

// file is the curve data as curves.json stores it.
type file struct {
	Sources map[string]source `json:"sources"`
	License string            `json:"license"`
	Frames  []frame           `json:"frames"`
	Types   []weaponType      `json:"types"`
}

// source is one origin of a curve. Patch is the game update that the
// source describes.
type source struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Date  string `json:"date"`
	Patch string `json:"patch"`
}

// frame lists the plug hashes that share one frame. The game gives the
// frame of a crafted weapon a plug hash of its own, so one frame has many
// hashes.
type frame struct {
	Names  []string `json:"names"`
	Hashes []uint32 `json:"hashes"`
}

// weaponType holds the curves of one weapon type. Each weapon of Weapons
// names its curve set in Curves and its aim scale in AimScales.
type weaponType struct {
	Curves    map[string]*set  `json:"curves"`
	AimScales map[string]scale `json:"aimScales"`
	Name      string           `json:"name"`
	Weapons   []weapon         `json:"weapons"`
}

// set holds the curves that one weapon reads. Reload reads the reload
// stat. Ready, Stow, and Aim read the handling stat, and Falloff reads the
// range stat.
type set struct {
	Reload  curve `json:"reload"`
	Ready   curve `json:"ready"`
	Stow    curve `json:"stow"`
	Aim     curve `json:"aim"`
	Falloff curve `json:"falloff"`
}

// curve maps a stat value x to Quadratic*x*x + Linear*x + Constant. It
// reads a stat value from 0 to MaxStat, inclusive, and a zero MaxStat
// means [statCap]. A curve with Omitted has no coefficients, and Omitted
// holds the sentence that says why. Source keys the sources of the file.
// Checked names the last game update whose patch notes leave the curve as
// it stands.
type curve struct {
	Omitted   string  `json:"omitted"`
	Source    string  `json:"source"`
	Checked   string  `json:"checked"`
	Quadratic float64 `json:"quadratic"`
	Linear    float64 `json:"linear"`
	Constant  float64 `json:"constant"`
	MaxStat   int     `json:"maxStat"`
}

// scale is the factor between the falloff distance from the hip and the
// distance while the weapon aims down sights. Omitted, Source, and Checked
// read as they do in a [curve].
type scale struct {
	Omitted string  `json:"omitted"`
	Source  string  `json:"source"`
	Checked string  `json:"checked"`
	Scale   float64 `json:"scale"`
}

// weapon gives a curve set and an aim scale to one hash. Hash is the plug
// hash of a frame or of the intrinsic perk of an exotic, or the item hash
// of a weapon. Name is the label of the source.
type weapon struct {
	Name     string `json:"name"`
	Curves   string `json:"curves"`
	AimScale string `json:"aimScale"`
	Hash     uint32 `json:"hash"`
}

// table is the curve data in the form that a lookup reads. FrameOf maps
// each plug hash of a [frame] to the index of the frame. Listed holds each
// hash that a [weapon] of some type carries.
type table struct {
	types   map[string]*typeTable
	frameOf map[uint32]int
	listed  map[uint32]bool
}

// typeTable holds the entries of one weapon type by hash. Frames maps the
// index of a [frame] to the entry that every listed hash of the frame
// shares. It leaves out a frame whose listed hashes differ.
type typeTable struct {
	weapons map[uint32]entry
	frames  map[int]entry
}

// entry is the curve set and the aim scale of one weapon.
type entry struct {
	set      *set
	curves   string
	aimScale string
	scale    scale
}

// parse decodes curve data and checks it. An error wraps [ErrData].
func parse(data []byte) (*table, error) {
	var f file

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	err := dec.Decode(&f)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrData, err)
	}

	t := &table{
		types:   map[string]*typeTable{},
		frameOf: map[uint32]int{},
		listed:  map[uint32]bool{},
	}

	for i, fr := range f.Frames {
		for _, hash := range fr.Hashes {
			if _, ok := t.frameOf[hash]; ok {
				return nil, fmt.Errorf("%w: two frames list the hash %d", ErrData, hash)
			}

			t.frameOf[hash] = i
		}
	}

	for i := range f.Types {
		wt := &f.Types[i]
		if _, ok := t.types[wt.Name]; ok || wt.Name == "" {
			return nil, fmt.Errorf("%w: type %d has the name %q, which is empty or taken", ErrData, i, wt.Name)
		}

		tt, err := f.typeTable(wt, t)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrData, wt.Name, err)
		}

		t.types[wt.Name] = tt
	}

	return t, nil
}

// typeTable checks one weapon type and builds its lookup table. It adds
// the hashes of the type to the listed hashes of t.
func (f *file) typeTable(wt *weaponType, t *table) (*typeTable, error) {
	for name, s := range wt.Curves {
		if s == nil {
			return nil, fmt.Errorf("the set %q holds no curves", name)
		}

		for label, c := range map[string]curve{
			"reload": s.Reload, "ready": s.Ready, "stow": s.Stow, "aim": s.Aim, "falloff": s.Falloff,
		} {
			has := c.Quadratic != 0 || c.Linear != 0 || c.Constant != 0

			err := f.check(c.Omitted, c.Source, c.Checked, has)
			if err != nil {
				return nil, fmt.Errorf("the %s curve of the set %q: %w", label, name, err)
			}
		}
	}

	for name, s := range wt.AimScales {
		err := f.check(s.Omitted, s.Source, s.Checked, s.Scale != 0)
		if err != nil {
			return nil, fmt.Errorf("the aim scale %q: %w", name, err)
		}
	}

	tt := &typeTable{weapons: map[uint32]entry{}, frames: map[int]entry{}}
	mixed := map[int]bool{}

	for _, w := range wt.Weapons {
		s, hasSet := wt.Curves[w.Curves]
		sc, hasScale := wt.AimScales[w.AimScale]

		_, taken := tt.weapons[w.Hash]
		if !hasSet || !hasScale || taken {
			return nil, fmt.Errorf("the weapon %d names a set or a scale that the type lacks, or it repeats", w.Hash)
		}

		e := entry{curves: w.Curves, aimScale: w.AimScale, set: s, scale: sc}
		tt.weapons[w.Hash] = e
		t.listed[w.Hash] = true

		index, ok := t.frameOf[w.Hash]
		if !ok {
			continue
		}

		shared, ok := tt.frames[index]
		if ok && (shared.curves != e.curves || shared.aimScale != e.aimScale) {
			mixed[index] = true
		}

		tt.frames[index] = e
	}

	for index := range mixed {
		delete(tt.frames, index)
	}

	return tt, nil
}

// check reports a fault in the provenance of a curve or of a scale. Has
// says whether the value holds numbers. A value holds numbers or a reason
// for their absence, never both. It also names a source of the file and
// the last game update of its check.
func (f *file) check(omitted, src, checked string, has bool) error {
	_, known := f.Sources[src]

	switch {
	case !known:
		return fmt.Errorf("the source %q is not a source of the file", src)
	case checked == "":
		return errors.New("the value names no game update of its last check")
	case has == (omitted != ""):
		return errors.New("the value must hold numbers or a reason for their absence, and not both")
	default:
		return nil
	}
}

// entryOf finds the entry of a weapon copy. The item hash of the copy
// wins, and the hash of a plug of the copy comes next. Otherwise the
// entry comes from the [frame] that lists a plug of the copy. A plug that
// another weapon type lists as its own selects no frame, because the two
// types can give one frame name different curves.
func (t *table) entryOf(tt *typeTable, w Weapon) (entry, bool) {
	e, ok := tt.weapons[w.Hash]
	if ok {
		return e, true
	}

	for _, plug := range w.Plugs {
		e, ok := tt.weapons[plug]
		if ok {
			return e, true
		}
	}

	for _, plug := range w.Plugs {
		index, ok := t.frameOf[plug]
		if !ok || t.listed[plug] {
			continue
		}

		e, ok := tt.frames[index]
		if ok {
			return e, true
		}
	}

	return entry{}, false
}

// measures evaluates the curves of the entry at the stats of a copy.
func (e entry) measures(stats map[uint32]int) Measures {
	handling, hasHandling := stats[StatHandling]
	reload, hasReload := stats[StatReload]
	distance, hasRange := stats[StatRange]

	m := Measures{
		Reload:     e.set.Reload.at(reload, hasReload, "Reload Speed"),
		Ready:      e.set.Ready.at(handling, hasHandling, "Handling"),
		Stow:       e.set.Stow.at(handling, hasHandling, "Handling"),
		Aim:        e.set.Aim.at(handling, hasHandling, "Handling"),
		HipFalloff: e.set.Falloff.at(distance, hasRange, "Range"),
	}

	switch {
	case !m.HipFalloff.Known():
		m.AimFalloff = m.HipFalloff
	case e.scale.Omitted != "":
		m.AimFalloff = Measure{Reason: e.scale.Omitted}
	default:
		m.AimFalloff = Measure{Value: m.HipFalloff.Value * e.scale.Scale}
	}

	return m
}

// at evaluates the curve at a stat value. Has says whether the copy has
// the stat, and name is the name of the stat for the reason of a copy
// without it.
func (c curve) at(stat int, has bool, name string) Measure {
	if c.Omitted != "" {
		return Measure{Reason: c.Omitted}
	}

	if !has {
		return Measure{Reason: "This copy has no " + name + " stat."}
	}

	top := c.MaxStat
	if top == 0 {
		top = statCap
	}

	x := float64(min(max(stat, 0), top))

	return Measure{Value: c.Quadratic*x*x + c.Linear*x + c.Constant}
}
