package damage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"

	_ "embed"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/curves"
)

// Stat hashes of the weapon stats that the numbers read.
const (
	StatRoundsPerMinute uint32 = 4284893193
	StatMagazine        uint32 = 3871231066
)

// Reasons of a weapon copy without numbers.
const (
	reasonType     = "The community data has no damage for this weapon type."
	reasonFrame    = "The community data has no damage for the frame of this weapon."
	reasonPlugs    = "The profile lists no perks for this copy, so the server cannot tell its frame."
	reasonNoFrame  = "The profile lists no frame among the perks of this copy."
	reasonRate     = "This copy has no Rounds Per Minute stat."
	reasonMagazine = "This copy has no Magazine stat."
	reasonReload   = "The server has no reload time for this copy."
)

const (
	// tick is the time unit of the firing times in the data, in seconds.
	tick = 1.0 / 30

	// rateTolerance is the largest relative distance between the Rounds
	// Per Minute stat of a copy and the rate of fire that the data gives
	// its frame. The game rounds the stat, so 140 stands for a rate of
	// 138.46.
	rateTolerance = 0.05

	// maxHits caps the hits of one kill, as Oracle Engine caps them.
	maxHits = 50

	// enhancedPrefix opens the type of an enhanced plug, such as
	// "Enhanced Trait".
	enhancedPrefix = "Enhanced"
)

var (
	// ErrData reports damage data that does not decode or that fails a
	// check, such as a frame without a source.
	ErrData = errors.New("damage data")

	// embedded holds the damage data of the package.
	//
	//go:embed damage.json
	embedded []byte

	// load decodes and checks the embedded damage data one time.
	load = sync.OnceValues(func() (*table, error) { return parse(embedded) })

	// frameTypes holds the plug types of a frame.
	frameTypes = []string{"Intrinsic", "Enhanced Intrinsic"}
)

// Plug is one active plug of a weapon copy. Name is the display name of
// the plug, such as "Kill Clip". Type is the type that its definition
// displays, such as "Trait" or "Enhanced Trait".
type Plug struct {
	Name string
	Type string
}

// Weapon describes one weapon copy. Type is the English name of the item
// type, such as "Auto Rifle". Plugs holds the active plug of each socket
// of the copy, and one of them is the frame. Stats maps the hash of each
// stat of the copy to its value.
//
// Reload returns the time of a full reload of the copy with its Reload
// Speed stat raised by bonus. The curves package gives the time. The base
// numbers read the time at a bonus of zero. A perk that raises the stat
// while it is active, such as Frenzy, reads the time at its bonus. Every
// reload time is unknown when Reload is nil.
type Weapon struct {
	Stats  map[uint32]int
	Reload func(bonus int) curves.Measure
	Type   string
	Plugs  []Plug
}

// Kill is the time in which a weapon copy defeats a Guardian. Seconds
// runs from the first hit to the last one. Precision and Body count the
// precision hits and the body shots of the kill. Reason is empty for a
// known kill. For an unknown kill, Reason holds a sentence that says why,
// and the other fields are zero.
type Kill struct {
	Reason    string
	Seconds   float64
	Precision int
	Body      int
}

// Known reports whether the kill holds a time.
func (k Kill) Known() bool {
	return k.Reason == ""
}

// Rate is a damage per second. Reason is empty for a known rate. For an
// unknown rate, Reason holds a sentence that says why, and Value is zero.
type Rate struct {
	Reason string
	Value  float64
}

// Known reports whether the rate holds a value.
func (r Rate) Known() bool {
	return r.Reason == ""
}

// Figures holds the numbers of a weapon copy in one state. Perk names the
// perk of the state, and State says how strong the perk is, such as "at 3
// stacks". Both are empty for the base state, in which no timed perk is
// active.
//
// OptimalKill is the fastest kill, in which every hit is a precision hit
// unless a body shot kills as fast. BodyKill is the kill with body shots
// alone. PrecisionRate and BodyRate are the damage per second over one
// magazine and its reload, with precision hits and with body shots.
type Figures struct {
	Perk          string
	State         string
	PrecisionRate Rate
	BodyRate      Rate
	OptimalKill   Kill
	BodyKill      Kill
}

// Report holds the numbers of a weapon copy against a Guardian. Reason is
// empty for a copy with numbers. For a copy without them, Reason holds a
// sentence that says why. Patch is the game update that the numbers
// match, and Health is the health of the Guardian.
//
// Base holds the numbers without a timed perk. Perks holds the numbers of
// each state of each active perk that the data models, in the order of
// the plugs. Unmodeled names each active plug whose effect the numbers
// leave out.
type Report struct {
	Reason    string
	Patch     string
	Perks     []Figures
	Unmodeled []string
	Base      Figures
	Health    float64
}

// Patch returns the game update that the numbers match, such as "9.7.0".
// It returns an empty string when the damage data does not load.
func Patch() string {
	t, err := load()
	if err != nil {
		return ""
	}

	return t.patch
}

// Of returns the [Report] of a weapon copy at its stats and its plugs. A
// copy that the damage data cannot describe carries the reason.
func Of(w Weapon) Report {
	t, err := load()
	if err != nil {
		return Report{Reason: fmt.Sprintf("The server cannot read its damage data: %v.", err)}
	}

	report := Report{Patch: t.patch, Health: t.health}

	f, reason := t.frameOf(w)
	if reason != "" {
		report.Reason = reason

		return report
	}

	magazine, ok := w.Stats[StatMagazine]
	if !ok || magazine <= 0 {
		report.Reason = reasonMagazine

		return report
	}

	reload := w.Reload
	if reload == nil {
		reload = func(int) curves.Measure { return curves.Measure{Reason: reasonReload} }
	}

	g := gun{frame: f, health: t.health, magazine: magazine, reload: reload}
	base := effect{}

	type timed struct {
		name   string
		states []state
	}

	var perks []timed

	for _, plug := range w.Plugs {
		p, states, modeled := t.perkOf(plug)

		switch {
		case modeled:
			base = base.with(p.Always, w.Type)

			if len(states) > 0 {
				perks = append(perks, timed{name: p.Name, states: states})
			}

		case slices.Contains(frameTypes, plug.Type), t.neutral(plug):

		case !slices.Contains(report.Unmodeled, plug.Name):
			report.Unmodeled = append(report.Unmodeled, plug.Name)
		}
	}

	report.Base = g.figures(base)

	for _, p := range perks {
		for _, s := range p.states {
			figures := g.figures(base.with(&s.effect, w.Type))
			figures.Perk = p.name
			figures.State = s.Label

			report.Perks = append(report.Perks, figures)
		}
	}

	return report
}

// file is the damage data as damage.json stores it. Patch is the game
// update that the numbers match.
type file struct {
	Sources map[string]source `json:"sources"`
	License string            `json:"license"`
	Patch   string            `json:"patch"`
	Types   []weaponType      `json:"types"`
	Perks   []perk            `json:"perks"`
	Neutral []neutral         `json:"neutral"`
	Health  health            `json:"health"`
}

// source is one origin of a value. Patch is the game update that the
// source describes.
type source struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Date  string `json:"date"`
	Patch string `json:"patch"`
}

// origin records where a value comes from. Source keys the sources of the
// file. Checked names the last game update whose patch notes leave the
// value as it stands. Entry and Uploaded identify the entry of the
// Clarity database that gives the value, with the date of its last
// upload. Note explains the value to a reader of the file.
type origin struct {
	Source   string `json:"source"`
	Checked  string `json:"checked"`
	Uploaded string `json:"uploaded"`
	Note     string `json:"note"`
	Entry    uint32 `json:"entry"`
}

// health is the health of a Guardian.
type health struct {
	origin

	Value float64 `json:"value"`
}

// weaponType holds the frames of one weapon type.
type weaponType struct {
	Name   string  `json:"name"`
	Frames []frame `json:"frames"`
}

// frame holds the damage and the firing times of the frames with one of
// Names. Body and Precision are the damage of a body shot and of a
// precision hit against a Guardian. A frame with PrecisionOmitted has no
// Precision, and PrecisionOmitted holds the sentence that says why.
//
// A trigger pull fires BurstSize rounds. BurstDelay is the time from the
// last round of a burst to the first round of the next burst, and
// InnerBurstDelay is the time between two rounds of one burst. Both count
// thirtieths of a second.
type frame struct {
	PrecisionOmitted string   `json:"precisionOmitted"`
	Damage           origin   `json:"damage"`
	Timing           origin   `json:"timing"`
	Names            []string `json:"names"`
	Body             float64  `json:"body"`
	Precision        float64  `json:"precision"`
	BurstDelay       float64  `json:"burstDelay"`
	InnerBurstDelay  float64  `json:"innerBurstDelay"`
	BurstSize        int      `json:"burstSize"`
}

// roundsPerMinute returns the rate of fire that the firing times give.
func (f *frame) roundsPerMinute() float64 {
	cycle := f.BurstDelay + f.InnerBurstDelay*float64(f.BurstSize-1)

	return 60 * float64(f.BurstSize) / (cycle * tick)
}

// perk holds the effect of the plug with one name. Always holds the
// effect that the plug always has. States holds one effect for each
// strength of a timed perk that the reports show. Enhanced describes the
// enhanced plug of the name, and a nil Enhanced means that the data does
// not model the enhanced plug.
type perk struct {
	Always   *effect  `json:"always"`
	Enhanced *variant `json:"enhanced"`
	origin

	Name   string  `json:"name"`
	States []state `json:"states"`
}

// variant describes the enhanced plug of a perk. Same marks an enhanced
// plug with the effects of the base plug. Otherwise States holds its
// effects.
type variant struct {
	origin

	States []state `json:"states"`
	Same   bool    `json:"same"`
}

// state is the effect of a timed perk at one strength. Label says how
// strong the perk is, such as "at 3 stacks", and it is empty for a perk
// with one strength.
type state struct {
	Label string `json:"label"`
	effect
}

// effect changes the numbers of a weapon copy. Damage scales the damage
// of each hit, and DamageByType replaces Damage for the weapon types that
// it names. Delay scales the time between two rounds, and Reload scales
// the reload time. A zero scale stands for one. ReloadStat raises the
// Reload Speed stat of the copy, and the reload time then comes from the
// raised stat. Ramp scales the damage of each hit by the hits that the
// magazine landed before it.
type effect struct {
	DamageByType map[string]float64 `json:"damageByType"`
	Ramp         *ramp              `json:"ramp"`
	Damage       float64            `json:"damage"`
	Delay        float64            `json:"delay"`
	Reload       float64            `json:"reload"`
	ReloadStat   int                `json:"reloadStat"`
}

// ramp is a damage bonus that grows with the share of the magazine that
// hit the target, as Oracle Engine models Target Lock. The bonus is zero
// for a share less than After. From there it follows a smoothstep curve
// between Low and High, which the curve reaches at the shares Start and
// End. The bonus stays at High past End.
type ramp struct {
	After float64 `json:"after"`
	Low   float64 `json:"low"`
	High  float64 `json:"high"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// at returns the damage scale of a hit. Landed counts the hits that the
// magazine landed before it.
func (r *ramp) at(landed, magazine int) float64 {
	share := float64(landed) / float64(magazine)

	switch {
	case share < r.After:
		return 1
	case share > r.End:
		return 1 + r.High
	}

	x := (share - r.Start) / (r.End - r.Start)
	step := 3*x*x - 2*x*x*x

	return 1 + r.Low + (r.High-r.Low)*step
}

// with returns the effect e combined with other for a weapon type. The
// scales multiply, and the stat bonuses add up. A nil other leaves e as
// it stands.
func (e effect) with(other *effect, weaponType string) effect {
	if other == nil {
		return e
	}

	damage := other.Damage
	if byType, ok := other.DamageByType[weaponType]; ok {
		damage = byType
	}

	e.Damage = scaleOf(e.Damage) * scaleOf(damage)
	e.Delay = scaleOf(e.Delay) * scaleOf(other.Delay)
	e.Reload = scaleOf(e.Reload) * scaleOf(other.Reload)
	e.ReloadStat += other.ReloadStat
	e.DamageByType = nil

	if other.Ramp != nil {
		e.Ramp = other.Ramp
	}

	return e
}

// scaleOf returns a scale of an [effect], in which zero stands for one.
func scaleOf(scale float64) float64 {
	if scale == 0 {
		return 1
	}

	return scale
}

// damageAt returns the damage scale of a hit. Landed counts the hits that
// the magazine landed before it.
func (e effect) damageAt(landed, magazine int) float64 {
	scale := scaleOf(e.Damage)
	if e.Ramp != nil {
		scale *= e.Ramp.at(landed, magazine)
	}

	return scale
}

// neutral lists plugs without an effect on the numbers. A plug belongs to
// the group when Names holds its name or Types holds its type. A group
// with PlugTypes holds a name only for a plug of one of these types.
// Reason says why the plugs have no effect.
type neutral struct {
	origin

	Reason    string   `json:"reason"`
	Names     []string `json:"names"`
	Types     []string `json:"types"`
	PlugTypes []string `json:"plugTypes"`
}

// table is the damage data in the form that a lookup reads.
type table struct {
	types    map[string][]frame
	perks    map[string]*perk
	patch    string
	neutrals []neutral
	health   float64
}

// parse decodes damage data and checks it. An error wraps [ErrData].
func parse(data []byte) (*table, error) {
	var f file

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	err := dec.Decode(&f)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrData, err)
	}

	t := &table{
		types:    map[string][]frame{},
		perks:    map[string]*perk{},
		patch:    f.Patch,
		neutrals: f.Neutral,
		health:   f.Health.Value,
	}

	err = f.checkHead()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrData, err)
	}

	for i := range f.Types {
		wt := &f.Types[i]
		if _, ok := t.types[wt.Name]; ok || wt.Name == "" {
			return nil, fmt.Errorf("%w: type %d has the name %q, which is empty or taken", ErrData, i, wt.Name)
		}

		err := f.checkFrames(wt.Frames)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrData, wt.Name, err)
		}

		t.types[wt.Name] = wt.Frames
	}

	for i := range f.Perks {
		p := &f.Perks[i]
		if _, ok := t.perks[p.Name]; ok || p.Name == "" {
			return nil, fmt.Errorf("%w: perk %d has the name %q, which is empty or taken", ErrData, i, p.Name)
		}

		err := f.checkPerk(p)
		if err != nil {
			return nil, fmt.Errorf("%w: the perk %s: %w", ErrData, p.Name, err)
		}

		t.perks[p.Name] = p
	}

	for i := range f.Neutral {
		err := f.checkNeutral(&f.Neutral[i], t.perks)
		if err != nil {
			return nil, fmt.Errorf("%w: neutral group %d: %w", ErrData, i, err)
		}
	}

	return t, nil
}

// checkHead reports a fault in the values that hold for every weapon.
func (f *file) checkHead() error {
	if f.Patch == "" {
		return errors.New("the data names no game update that its numbers match")
	}

	if f.Health.Value <= 0 {
		return errors.New("the health of a Guardian must be more than zero")
	}

	err := f.check(f.Health.origin)
	if err != nil {
		return fmt.Errorf("the health of a Guardian: %w", err)
	}

	return nil
}

// checkFrames reports a fault in the frames of one weapon type. Two
// frames that share a name must differ in their rate of fire by more than
// twice [rateTolerance]. The Rounds Per Minute stat of a copy then
// selects one of them.
func (f *file) checkFrames(frames []frame) error {
	for i := range frames {
		fr := &frames[i]
		label := strings.Join(fr.Names, ", ")

		switch {
		case len(fr.Names) == 0:
			return fmt.Errorf("frame %d has no name", i)
		case fr.Body <= 0 || fr.BurstSize < 1 || fr.BurstDelay <= 0:
			return fmt.Errorf("the frame %s lacks a body damage, a burst size, or a burst delay", label)
		case fr.BurstSize > 1 && fr.InnerBurstDelay <= 0:
			return fmt.Errorf("the frame %s fires bursts without an inner burst delay", label)
		case (fr.Precision > 0) == (fr.PrecisionOmitted != ""):
			return fmt.Errorf(
				"the frame %s must hold a precision damage or a reason for its absence, and not both",
				label,
			)

		case fr.Precision != 0 && fr.Precision < fr.Body:
			return fmt.Errorf("the frame %s has a precision damage less than its body damage", label)
		}

		for part, o := range map[string]origin{"damage": fr.Damage, "timing": fr.Timing} {
			err := f.check(o)
			if err != nil {
				return fmt.Errorf("the %s of the frame %s: %w", part, label, err)
			}
		}

		for j := range frames[:i] {
			other := &frames[j]
			shared := slices.ContainsFunc(
				fr.Names,
				func(name string) bool { return slices.Contains(other.Names, name) },
			)

			if shared && !apart(fr.roundsPerMinute(), other.roundsPerMinute()) {
				return fmt.Errorf("two frames with the name of %s fire at nearly the same rate", label)
			}
		}
	}

	return nil
}

// apart reports whether no Rounds Per Minute stat is within
// [rateTolerance] of both rates of fire.
func apart(a, b float64) bool {
	low, high := min(a, b), max(a, b)

	return low*(1+rateTolerance) < high*(1-rateTolerance)
}

// checkPerk reports a fault in one perk. A perk holds an effect that it
// always has, the states of a timed effect, or both. Its enhanced plug
// has the effects of the base plug or states of its own.
func (f *file) checkPerk(p *perk) error {
	err := f.check(p.origin)
	if err != nil {
		return err
	}

	if p.Always == nil && len(p.States) == 0 {
		return errors.New("the perk has no effect")
	}

	if p.Always != nil {
		err := p.Always.check()
		if err != nil {
			return err
		}
	}

	err = checkStates(p.States)
	if err != nil {
		return err
	}

	if p.Enhanced == nil {
		return nil
	}

	err = f.check(p.Enhanced.origin)
	if err != nil {
		return fmt.Errorf("the enhanced plug: %w", err)
	}

	if p.Enhanced.Same == (len(p.Enhanced.States) > 0) {
		return errors.New("the enhanced plug must have the effects of the base plug or states of its own, and not both")
	}

	err = checkStates(p.Enhanced.States)
	if err != nil {
		return fmt.Errorf("the enhanced plug: %w", err)
	}

	return nil
}

// checkStates reports a fault in the states of a perk. Two states of one
// perk have different labels.
func checkStates(states []state) error {
	labels := map[string]bool{}

	for _, s := range states {
		if labels[s.Label] {
			return fmt.Errorf("two states have the label %q", s.Label)
		}

		labels[s.Label] = true

		err := s.check()
		if err != nil {
			return err
		}
	}

	return nil
}

// check reports an effect that changes nothing, a scale that is not a
// positive number, or a stat bonus that is less than zero.
func (e *effect) check() error {
	scales := []float64{e.Damage, e.Delay, e.Reload}
	for _, scale := range e.DamageByType {
		scales = append(scales, scale)
	}

	if slices.ContainsFunc(scales, func(scale float64) bool { return scale < 0 }) {
		return errors.New("an effect has a negative scale")
	}

	if e.ReloadStat < 0 {
		return errors.New("an effect has a negative stat bonus")
	}

	if e.Ramp != nil && (e.Ramp.End <= e.Ramp.Start || e.Ramp.High < e.Ramp.Low) {
		return errors.New("a ramp must rise from its start to its end")
	}

	scaled := slices.ContainsFunc(scales, func(scale float64) bool { return scale > 0 })
	if e.Ramp == nil && e.ReloadStat == 0 && !scaled {
		return errors.New("an effect changes nothing")
	}

	return nil
}

// checkNeutral reports a fault in one group of plugs without an effect.
// A plug cannot be in the group and have an effect as a perk.
func (f *file) checkNeutral(n *neutral, perks map[string]*perk) error {
	err := f.check(n.origin)
	if err != nil {
		return err
	}

	switch {
	case n.Reason == "":
		return errors.New("the group gives no reason")
	case len(n.Names) == 0 && len(n.Types) == 0:
		return errors.New("the group lists no plug")
	}

	for _, name := range n.Names {
		if _, ok := perks[name]; ok {
			return fmt.Errorf("the plug %s also has an effect as a perk", name)
		}
	}

	return nil
}

// check reports a fault in the provenance of a value. The value names a
// source of the file and the last game update of its check.
func (f *file) check(o origin) error {
	_, known := f.Sources[o.Source]

	switch {
	case !known:
		return fmt.Errorf("the source %q is not a source of the file", o.Source)
	case o.Checked == "":
		return errors.New("the value names no game update of its last check")
	default:
		return nil
	}
}

// frameOf finds the frame of a weapon copy. The names of the frame hold
// the name of the frame plug of the copy. The rate of fire of the frame
// is within [rateTolerance] of the Rounds Per Minute stat of the copy.
// frameOf returns a reason for a copy without such a frame.
func (t *table) frameOf(w Weapon) (*frame, string) {
	frames, ok := t.types[w.Type]
	if !ok {
		return nil, reasonType
	}

	if len(w.Plugs) == 0 {
		return nil, reasonPlugs
	}

	i := slices.IndexFunc(w.Plugs, func(plug Plug) bool { return slices.Contains(frameTypes, plug.Type) })
	if i < 0 {
		return nil, reasonNoFrame
	}

	var named []*frame

	for j := range frames {
		if slices.Contains(frames[j].Names, w.Plugs[i].Name) {
			named = append(named, &frames[j])
		}
	}

	if len(named) == 0 {
		return nil, reasonFrame
	}

	rate, ok := w.Stats[StatRoundsPerMinute]
	if !ok {
		return nil, reasonRate
	}

	for _, f := range named {
		if math.Abs(float64(rate)-f.roundsPerMinute()) <= rateTolerance*f.roundsPerMinute() {
			return f, ""
		}
	}

	return nil, fmt.Sprintf(
		"This copy fires %d rounds per minute, and the community data has no damage for its frame at that rate.",
		rate,
	)
}

// perkOf finds the perk of a plug and the states of the plug. It reports
// false for a plug without a perk, and for an enhanced plug whose perk
// does not model the enhanced plug.
func (t *table) perkOf(plug Plug) (*perk, []state, bool) {
	p, ok := t.perks[plug.Name]
	if !ok {
		return nil, nil, false
	}

	if !strings.HasPrefix(plug.Type, enhancedPrefix) {
		return p, p.States, true
	}

	switch {
	case p.Enhanced == nil:
		return nil, nil, false
	case p.Enhanced.Same:
		return p, p.States, true
	default:
		return p, p.Enhanced.States, true
	}
}

// neutral reports whether the data lists a plug as one without an effect
// on the numbers.
func (t *table) neutral(plug Plug) bool {
	return slices.ContainsFunc(t.neutrals, func(n neutral) bool {
		if slices.Contains(n.Types, plug.Type) {
			return true
		}

		if len(n.PlugTypes) > 0 && !slices.Contains(n.PlugTypes, plug.Type) {
			return false
		}

		return slices.Contains(n.Names, plug.Name)
	})
}

// gun is a weapon copy in the form that the calculation reads. Reload
// returns the time of a full reload with the Reload Speed stat raised by
// a bonus.
type gun struct {
	frame    *frame
	reload   func(bonus int) curves.Measure
	health   float64
	magazine int
}

// reloadTime returns the time of a full reload under an effect. The
// effect raises the Reload Speed stat by its bonus and then scales the
// time at the raised stat.
func (g gun) reloadTime(e effect) curves.Measure {
	m := g.reload(e.ReloadStat)
	if m.Known() {
		m.Value *= scaleOf(e.Reload)
	}

	return m
}

// figures returns the numbers of the gun under an effect.
func (g gun) figures(e effect) Figures {
	figures := Figures{
		BodyKill: g.bodyKill(e),
		BodyRate: g.rate(g.frame.Body, e),
	}

	if g.frame.PrecisionOmitted != "" {
		figures.OptimalKill = Kill{Reason: g.frame.PrecisionOmitted}
		figures.PrecisionRate = Rate{Reason: g.frame.PrecisionOmitted}

		return figures
	}

	figures.OptimalKill = g.optimalKill(e)
	figures.PrecisionRate = g.rate(g.frame.Precision, e)

	return figures
}

// delay returns the time in seconds between a round and the round before
// it. Fired counts the rounds that the magazine fired before the round. A
// round that opens a burst follows a burst delay, and every other round
// follows an inner burst delay.
func (g gun) delay(fired int, e effect) float64 {
	delay := g.frame.InnerBurstDelay
	if fired%g.frame.BurstSize == 0 {
		delay = g.frame.BurstDelay
	}

	return delay * tick * scaleOf(e.Delay)
}

// kill fires rounds of one damage until their sum reaches the health of
// a Guardian. It returns the time from the first hit to the last one and
// the damage scale of each hit. A reload follows an empty magazine. The
// reason is not empty when the kill takes more than [maxHits] hits, or
// when it takes a reload whose time is unknown.
func (g gun) kill(damage float64, e effect) (float64, []float64, string) {
	var (
		dealt, elapsed float64
		scales         []float64
	)

	for hit := range maxHits {
		fired := hit % g.magazine

		if hit > 0 {
			elapsed += g.delay(fired, e)
		}

		if hit > 0 && fired == 0 {
			reload := g.reloadTime(e)
			if !reload.Known() {
				return 0, nil, reload.Reason
			}

			elapsed += reload.Value
		}

		scale := e.damageAt(fired, g.magazine)
		scales = append(scales, scale)
		dealt += damage * scale

		if dealt >= g.health {
			return elapsed, scales, ""
		}
	}

	return 0, nil, fmt.Sprintf("This weapon does not defeat a Guardian in %d hits.", maxHits)
}

// bodyKill returns the kill with body shots alone.
func (g gun) bodyKill(e effect) Kill {
	seconds, scales, reason := g.kill(g.frame.Body, e)
	if reason != "" {
		return Kill{Reason: reason}
	}

	return Kill{Seconds: seconds, Body: len(scales)}
}

// optimalKill returns the kill with precision hits. A hit counts as a
// body shot when the kill takes no more hits with it. The search turns
// the hits into body shots from the first hit on and stops at the first
// hit that must stay a precision hit, as Oracle Engine does.
func (g gun) optimalKill(e effect) Kill {
	seconds, scales, reason := g.kill(g.frame.Precision, e)
	if reason != "" {
		return Kill{Reason: reason}
	}

	var dealt float64
	for _, scale := range scales {
		dealt += g.frame.Precision * scale
	}

	kill := Kill{Seconds: seconds, Precision: len(scales)}

	for _, scale := range scales {
		dealt -= (g.frame.Precision - g.frame.Body) * scale
		if dealt < g.health {
			break
		}

		kill.Precision--
		kill.Body++
	}

	return kill
}

// rate returns the damage per second of rounds of one damage over one
// magazine and its reload. Every round of the magazine hits, and the
// time of the magazine counts the delay after each round, as Oracle
// Engine counts it.
func (g gun) rate(damage float64, e effect) Rate {
	reload := g.reloadTime(e)
	if !reload.Known() {
		return Rate{Reason: reload.Reason}
	}

	var dealt, elapsed float64

	for fired := range g.magazine {
		dealt += damage * e.damageAt(fired, g.magazine)
		elapsed += g.delay(fired, e)
	}

	return Rate{Value: dealt / (elapsed + reload.Value)}
}
