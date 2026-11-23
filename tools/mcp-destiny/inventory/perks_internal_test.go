package inventory

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungietest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitionstest"
)

// Plugs of the fixture definitions, and of the subclass that
// [subclassDefs] adds to them.
var (
	plugExplosivePayload  = Plug{Name: "Explosive Payload", Type: "Trait", Hash: 900}
	plugRampage           = Plug{Name: "Rampage", Type: "Trait", Hash: 903}
	plugKillClip          = Plug{Name: "Kill Clip", Type: "Trait", Hash: 904}
	plugKillClipEnhanced  = Plug{Name: "Kill Clip", Type: "Enhanced Trait", Hash: 905}
	plugChillClip         = Plug{Name: "Chill Clip", Type: "Trait", Hash: 906}
	plugChillClipEnhanced = Plug{Name: "Chill Clip", Type: "Enhanced Trait", Hash: 908}

	plugSilenceAndSquall   = Plug{Name: "Silence and Squall", Type: "Prismatic Super", Hash: 810}
	plugStormsEdge         = Plug{Name: "Storm's Edge", Type: "Prismatic Super", Hash: 811}
	plugStylishExecutioner = aspectPlug("Stylish Executioner", 2, 820)
	plugWintersShroud      = aspectPlug("Winter's Shroud", 2, 821)
	plugAscension          = aspectPlug("Ascension", 3, 822)
	plugThreadedSpecter    = aspectPlug("Threaded Specter", 2, 823)
	plugEmptyAspect        = Plug{Name: "Empty Aspect Socket", Hash: 829}
	plugFacetOfProtection  = Plug{Name: "Facet of Protection", Type: "Prismatic Fragment", Hash: 830}
	plugFacetOfCourage     = Plug{Name: "Facet of Courage", Type: "Prismatic Fragment", Hash: 831}
	plugFacetOfDominance   = Plug{Name: "Facet of Dominance", Type: "Prismatic Fragment", Hash: 832}
	plugEmptyFragment      = Plug{Name: "Empty Fragment Socket", Hash: 839}
	subclassSuperPlugs     = []Plug{plugSilenceAndSquall, plugStormsEdge}
	subclassAspectPlugs    = []Plug{plugStylishExecutioner, plugWintersShroud, plugAscension}
	subclassFragmentPlugs  = []Plug{plugFacetOfProtection, plugFacetOfCourage}
)

// aspectPlug returns the plug of an aspect of the subclass of
// [subclassDefs] that opens slots fragment sockets.
func aspectPlug(name string, slots int, hash uint32) Plug {
	return Plug{Name: name, Type: "Prismatic Hunter Aspect", FragmentSlots: slots, Hash: hash}
}

func TestPerksOf(t *testing.T) {
	t.Parallel()

	// Socket 0 holds a shader, so each perk keeps the index of its socket
	// rather than its position in the perk list. Socket 1 holds Explosive
	// Payload in every case. Socket 2 holds the plug in active, and plugs
	// is the JSON list of the reusable plugs of socket 2. want lists the
	// options of socket 2, and wantInsert lists every plug that Bungie
	// allows socket 2 to hold.
	tests := map[string]struct {
		plugs      string
		want       []Plug
		wantInsert []Plug
		active     Plug
	}{
		"options skip the active name, a duplicate, and an unknown plug": {
			active: plugKillClip,
			plugs: `[{"plugItemHash": 905, "canInsert": true}, {"plugItemHash": 906, "canInsert": true},
				{"plugItemHash": 999, "canInsert": true}, {"plugItemHash": 906, "canInsert": true}]`,
			want:       []Plug{plugChillClip},
			wantInsert: []Plug{plugKillClipEnhanced, plugChillClip, plugChillClip},
		},
		"enhanced variant after the base replaces it": {
			active:     plugKillClip,
			plugs:      `[{"plugItemHash": 906, "canInsert": true}, {"plugItemHash": 908, "canInsert": true}]`,
			want:       []Plug{plugChillClipEnhanced},
			wantInsert: []Plug{plugChillClip, plugChillClipEnhanced},
		},
		"base after the enhanced variant keeps the enhanced one": {
			active:     plugKillClip,
			plugs:      `[{"plugItemHash": 908, "canInsert": true}, {"plugItemHash": 906, "canInsert": true}]`,
			want:       []Plug{plugChillClipEnhanced},
			wantInsert: []Plug{plugChillClipEnhanced, plugChillClip},
		},
		"plug that bungie will not insert is left out": {
			active:     plugKillClip,
			plugs:      `[{"plugItemHash": 906, "canInsert": false}, {"plugItemHash": 903, "canInsert": true}]`,
			want:       []Plug{plugRampage},
			wantInsert: []Plug{plugRampage},
		},
		"enhanced variant that bungie will not insert leaves the base": {
			active:     plugKillClip,
			plugs:      `[{"plugItemHash": 908}, {"plugItemHash": 906, "canInsert": true}]`,
			want:       []Plug{plugChillClip},
			wantInsert: []Plug{plugChillClip},
		},
		"enhanced active perk offers the enhanced variant of each name": {
			active: plugKillClipEnhanced,
			plugs: `[{"plugItemHash": 904, "canInsert": true}, {"plugItemHash": 905, "canInsert": true},
				{"plugItemHash": 906, "canInsert": true}, {"plugItemHash": 908, "canInsert": true}]`,
			want:       []Plug{plugChillClipEnhanced},
			wantInsert: []Plug{plugKillClip, plugKillClipEnhanced, plugChillClip, plugChillClipEnhanced},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var profile bungie.Profile

			defs := definitionstest.Defs()

			require.NoError(t, json.Unmarshal(fmt.Appendf(nil, `{"itemComponents": {
				"sockets": {"data": {"7": {"sockets": [
					{"plugHash": 902, "isVisible": true},
					{"plugHash": 900, "isVisible": true},
					{"plugHash": %d, "isVisible": true}
				]}}},
				"reusablePlugs": {"data": {"7": {"plugs": {
					"1": [{"plugItemHash": 900, "canInsert": true}, {"plugItemHash": 903, "canInsert": true}],
					"2": %s
				}}}}
			}}`, tc.active.Hash, tc.plugs), &profile))

			assert.Equal(t, []Perk{
				{
					Plug:    plugExplosivePayload,
					Options: []Plug{plugRampage},
					Plugs:   []Plug{plugExplosivePayload, plugRampage},
					Socket:  1,
				},
				{Plug: tc.active, Options: tc.want, Plugs: tc.wantInsert, Socket: 2},
			}, perksOf(&profile, defs, defs.Items[100], bungie.ProfileItem{InstanceID: "7", Hash: 100}, ""))
		})
	}
}

func TestPerksOfCosmetic(t *testing.T) {
	t.Parallel()

	defs := definitionstest.Defs()
	defs.Items[940] = definitions.ItemDef{
		Name:     "Iron Truage Casque",
		TypeName: "Helmet",
		ItemType: bungie.ItemTypeArmor,
	}
	defs.Items[941] = definitions.ItemDef{Name: "Kill Tracker", ItemType: bungie.ItemTypePlug}

	// Socket 0 of the item holds the plug of each case and can hold only
	// that plug. Socket 1 holds Explosive Payload. Item 100 is a weapon,
	// and item 200 is a helmet.
	tests := map[string]struct {
		item uint32
		plug uint32
	}{
		"shader":            {item: 100, plug: 902},
		"untyped tracker":   {item: 100, plug: 941},
		"armor appearance":  {item: 200, plug: 940},
		"weapon appearance": {item: 100, plug: 102},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var profile bungie.Profile

			require.NoError(t, json.Unmarshal(fmt.Appendf(nil, `{"itemComponents": {
				"sockets": {"data": {"7": {"sockets": [
					{"plugHash": %[1]d, "isVisible": true, "isEnabled": true},
					{"plugHash": 900, "isVisible": true, "isEnabled": true}
				]}}},
				"reusablePlugs": {"data": {"7": {"plugs": {
					"0": [{"plugItemHash": %[1]d, "canInsert": true}]
				}}}}
			}}`, tc.plug), &profile))

			assert.Equal(t, []Perk{{Plug: plugExplosivePayload, Socket: 1}},
				perksOf(&profile, defs, defs.Items[tc.item], bungie.ProfileItem{InstanceID: "7", Hash: tc.item}, ""),
				"the perks leave out a socket whose plug changes only how the item looks")
		})
	}
}

// subclassDefs returns the fixture definitions with the subclass
// Prismatic Hunter, item 800, and the plugs of its sockets. Socket 0
// takes a super from plug set 60. Sockets 1 and 2 take aspects from plug
// set 61, and sockets 3 and 4 take fragments from plug set 62. Socket 5
// has no plug set. The definitions lack the aspect 824. The placeholders
// of empty sockets have no type, as in the Destiny definitions.
func subclassDefs() *definitions.Defs {
	defs := definitionstest.Defs()

	defs.Items[800] = definitions.ItemDef{
		Name:       "Prismatic Hunter",
		TypeName:   "Hunter Subclass",
		Bucket:     bungie.BucketSubclass,
		ItemType:   16,
		ClassType:  bungie.ClassHunter,
		Equippable: true,
	}

	for _, plug := range []Plug{
		plugSilenceAndSquall, plugStormsEdge,
		plugStylishExecutioner, plugWintersShroud, plugAscension, plugThreadedSpecter, plugEmptyAspect,
		plugFacetOfProtection, plugFacetOfCourage, plugFacetOfDominance, plugEmptyFragment,
	} {
		defs.Items[plug.Hash] = definitions.ItemDef{
			Name:          plug.Name,
			TypeName:      plug.Type,
			ItemType:      bungie.ItemTypePlug,
			ClassType:     bungie.ClassHunter,
			FragmentSlots: plug.FragmentSlots,
		}
	}

	defs.Sockets = map[uint32][]uint32{800: {60, 61, 61, 62, 62, 0}}

	return defs
}

// subclassInstance is the instance id of the subclass of
// [subclassProfileJSON].
const subclassInstance = "8"

// subclassProfileJSON is a profile in which the hunter c1 holds the
// subclass of [subclassDefs] as instance 8. Socket 4 is an empty fragment
// socket, and socket 5 holds a shader. Every socket is open.
//
// The profile lists the supers and the aspects with the profile, and the
// fragments with the profile and with each character. Each plug set
// lists the placeholder of its empty socket with canInsert set. Bungie
// reports the aspect Threaded Specter as locked. The
// profile also reports Facet of Courage as locked, from the view of
// another character, but c1 lists it as unlocked.
const subclassProfileJSON = `{
	"itemComponents": {"sockets": {"data": {"8": {"sockets": [
		{"plugHash": 810, "isVisible": true, "isEnabled": true},
		{"plugHash": 820, "isVisible": true, "isEnabled": true},
		{"plugHash": 821, "isVisible": true, "isEnabled": true},
		{"plugHash": 830, "isVisible": true, "isEnabled": true},
		{"plugHash": 839, "isVisible": true, "isEnabled": true},
		{"plugHash": 902, "isVisible": true, "isEnabled": true}
	]}}}},
	"profilePlugSets": {"data": {"plugs": {
		"60": [{"plugItemHash": 810, "canInsert": true}, {"plugItemHash": 811, "canInsert": true}],
		"61": [{"plugItemHash": 829, "canInsert": true}, {"plugItemHash": 820, "canInsert": true},
			{"plugItemHash": 821, "canInsert": true}, {"plugItemHash": 822, "canInsert": true},
			{"plugItemHash": 823, "canInsert": false}, {"plugItemHash": 824, "canInsert": true}],
		"62": [{"plugItemHash": 831, "canInsert": false}]
	}}},
	"characterPlugSets": {"data": {
		"c1": {"plugs": {"62": [{"plugItemHash": 839, "canInsert": true}, {"plugItemHash": 830, "canInsert": true},
			{"plugItemHash": 831, "canInsert": true}]}},
		"c2": {"plugs": {"62": [{"plugItemHash": 832, "canInsert": true}]}}
	}}
}`

// subclassProfile decodes [subclassProfileJSON] and then puts the plug
// with the hash sockets[i] into socket i of the subclass. It also closes
// each socket of the subclass whose index closed lists.
func subclassProfile(t *testing.T, sockets map[int]uint32, closed []int) *bungie.Profile {
	t.Helper()

	var profile bungie.Profile

	require.NoError(t, json.Unmarshal([]byte(subclassProfileJSON), &profile))

	state := profile.ItemComponents.Sockets.Data[subclassInstance].Sockets

	for i, hash := range sockets {
		state[i].PlugHash = hash
	}

	for _, i := range closed {
		state[i].IsEnabled = false
	}

	return &profile
}

func TestPerksOfSubclass(t *testing.T) {
	t.Parallel()

	subclass := subclassDefs().Items[800]
	other := subclass
	other.Bucket = bungie.BucketKinetic

	super := Perk{
		Plug:    plugSilenceAndSquall,
		Options: []Plug{plugStormsEdge},
		Plugs:   subclassSuperPlugs,
		Socket:  0,
		PlugSet: 60,
	}

	// aspect returns the perk of the aspect socket socket that holds
	// active. Bungie reports Threaded Specter as locked in every aspect
	// socket.
	aspect := func(socket int, active Plug, options ...Plug) Perk {
		return Perk{
			Plug:        active,
			Options:     options,
			Locked:      []Plug{plugThreadedSpecter},
			Plugs:       subclassAspectPlugs,
			LockedPlugs: []Plug{plugThreadedSpecter},
			Socket:      socket,
			PlugSet:     61,
		}
	}

	// fragment returns the perk of the fragment socket socket that holds
	// active. The profile reports Facet of Courage as locked in every
	// fragment socket.
	fragment := func(socket int, active Plug, plugs, options, locked []Plug) Perk {
		return Perk{
			Plug:        active,
			Options:     options,
			Locked:      locked,
			Plugs:       plugs,
			LockedPlugs: []Plug{plugFacetOfCourage},
			Socket:      socket,
			PlugSet:     62,
		}
	}

	tests := map[string]struct {
		sockets map[int]uint32
		def     definitions.ItemDef
		owner   string
		closed  []int
		want    []Perk
	}{
		// Each aspect socket leaves out the aspect that the other one
		// holds. Threaded Specter follows the swaps as locked. The aspect
		// 824 stays out, because the definitions lack it. Facet of
		// Courage counts as unlocked, because c1 lists it so. The empty
		// fragment socket offers the fragments that socket 3 does not
		// hold, and the shader socket does not hold a perk. No socket
		// offers a placeholder.
		"subclass offers the unlocked plugs of each plug set": {
			def:   subclass,
			owner: bungietest.HunterID,
			want: []Perk{
				super,
				aspect(1, plugStylishExecutioner, plugAscension),
				aspect(2, plugWintersShroud, plugAscension),
				fragment(3, plugFacetOfProtection, subclassFragmentPlugs, []Plug{plugFacetOfCourage}, nil),
				fragment(4, plugEmptyFragment, subclassFragmentPlugs, []Plug{plugFacetOfCourage}, nil),
			},
		},
		// The profile reports Facet of Courage as locked, and c2 does not
		// list it, so the fragment sockets of c2 list it as locked.
		"fragments come from the plug sets of the owner": {
			def:   subclass,
			owner: bungietest.TitanID,
			want: []Perk{
				super,
				aspect(1, plugStylishExecutioner, plugAscension),
				aspect(2, plugWintersShroud, plugAscension),
				fragment(3, plugFacetOfProtection, []Plug{plugFacetOfDominance},
					[]Plug{plugFacetOfDominance}, []Plug{plugFacetOfCourage}),
				fragment(4, plugEmptyFragment, []Plug{plugFacetOfDominance},
					[]Plug{plugFacetOfDominance}, []Plug{plugFacetOfCourage}),
			},
		},
		"empty sockets offer the plugs that fill them": {
			sockets: map[int]uint32{2: plugEmptyAspect.Hash, 3: plugEmptyFragment.Hash},
			def:     subclass,
			owner:   bungietest.HunterID,
			want: []Perk{
				super,
				aspect(1, plugStylishExecutioner, plugWintersShroud, plugAscension),
				aspect(2, plugEmptyAspect, plugWintersShroud, plugAscension),
				fragment(3, plugEmptyFragment, subclassFragmentPlugs, subclassFragmentPlugs, nil),
				fragment(4, plugEmptyFragment, subclassFragmentPlugs, subclassFragmentPlugs, nil),
			},
		},
		// Both fragment sockets hold the same placeholder, but only socket
		// 3 is open. The closed socket 4 stays out, and the open socket
		// offers every fragment.
		"closed empty socket stays out": {
			sockets: map[int]uint32{3: plugEmptyFragment.Hash},
			def:     subclass,
			owner:   bungietest.HunterID,
			closed:  []int{4},
			want: []Perk{
				super,
				aspect(1, plugStylishExecutioner, plugAscension),
				aspect(2, plugWintersShroud, plugAscension),
				fragment(3, plugEmptyFragment, subclassFragmentPlugs, subclassFragmentPlugs, nil),
			},
		},
		// An aspect change closed socket 4, which still holds Facet of
		// Courage. The game does not apply it there, so socket 4 stays
		// out, and the open socket 3 offers Facet of Courage.
		"closed socket that holds a fragment stays out": {
			sockets: map[int]uint32{3: plugEmptyFragment.Hash, 4: plugFacetOfCourage.Hash},
			def:     subclass,
			owner:   bungietest.HunterID,
			closed:  []int{4},
			want: []Perk{
				super,
				aspect(1, plugStylishExecutioner, plugAscension),
				aspect(2, plugWintersShroud, plugAscension),
				fragment(3, plugEmptyFragment, subclassFragmentPlugs, subclassFragmentPlugs, nil),
			},
		},
		// Only a subclass reads the plug sets, so an item of another
		// bucket offers a swap only from its reusable plugs. It leaves out
		// each empty socket.
		"item outside the subclass bucket does not read a plug set": {
			def:   other,
			owner: bungietest.HunterID,
			want: []Perk{
				{Plug: plugSilenceAndSquall, Socket: 0},
				{Plug: plugStylishExecutioner, Socket: 1},
				{Plug: plugWintersShroud, Socket: 2},
				{Plug: plugFacetOfProtection, Socket: 3},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			profile := subclassProfile(t, tc.sockets, tc.closed)
			pi := bungie.ProfileItem{InstanceID: subclassInstance, Hash: 800}

			assert.Equal(t, tc.want, perksOf(profile, subclassDefs(), tc.def, pi, tc.owner))
		})
	}
}

func TestSetPerkSubclass(t *testing.T) {
	t.Parallel()

	// Each case sets the perk of the subclass of [subclassProfileJSON],
	// after it puts the plug with the hash sockets[i] into socket i and
	// closes each socket that closed lists. The
	// character owner holds the subclass, and the hunter c1 holds it when
	// owner is empty. The perks of the subclass are the super, the two
	// aspects, and the two fragments. wantOptions and wantLocked list the
	// names of the options and of the locked plugs of each perk after the
	// call. A case that does not change the subclass on c1 expects active,
	// options, and locked, the names before any change.
	active := []string{
		"Silence and Squall", "Stylish Executioner", "Winter's Shroud", "Facet of Protection", "Empty Fragment Socket",
	}
	options := [][]string{{"Storm's Edge"}, {"Ascension"}, {"Ascension"}, {"Facet of Courage"}, {"Facet of Courage"}}
	locked := [][]string{{}, {"Threaded Specter"}, {"Threaded Specter"}, {}, {}}

	tests := map[string]struct {
		sockets     map[int]uint32
		owner       string
		closed      []int
		perk        string
		replace     string
		fail        string
		err         error
		wantErr     string
		want        []string
		wantActive  []string
		wantOptions [][]string
		wantLocked  [][]string
	}{
		"aspect that the other aspect socket holds": {
			perk:    "Winter's Shroud",
			replace: "Stylish Executioner",
			err:     ErrPerk,
			wantErr: `no socket of Prismatic Hunter that holds Stylish Executioner can swap to "Winter's Shroud". ` +
				"Sockets that can swap: Stylish Executioner (can swap to Ascension; Bungie reports as locked: Threaded Specter)",
			wantActive:  active,
			wantOptions: options,
			wantLocked:  locked,
		},
		// c2 lists only Facet of Dominance as unlocked, and socket 4 holds
		// it. Socket 3 then does not offer any swap, but the error still
		// names the fragment that Bungie reports as locked, because
		// set_perk tries that fragment too.
		"fragment socket with only locked plugs": {
			sockets: map[int]uint32{4: plugFacetOfDominance.Hash},
			owner:   bungietest.TitanID,
			perk:    "Facet of Joy",
			replace: "Facet of Protection",
			err:     ErrPerk,
			wantErr: `no socket of Prismatic Hunter that holds Facet of Protection can swap to "Facet of Joy". ` +
				"Sockets that can swap: Facet of Protection (Bungie reports as locked: Facet of Courage)",
			wantActive: []string{
				"Silence and Squall",
				"Stylish Executioner",
				"Winter's Shroud",
				"Facet of Protection",
				"Facet of Dominance",
			},
			wantOptions: [][]string{{"Storm's Edge"}, {"Ascension"}, {"Ascension"}, {}, {}},
			wantLocked: [][]string{
				{}, {"Threaded Specter"}, {"Threaded Specter"}, {"Facet of Courage"}, {"Facet of Courage"},
			},
		},
		// Both aspect sockets then offer the replaced aspect, and neither
		// offers the aspect that the other one holds.
		"aspect swap": {
			perk:    "Ascension",
			replace: "Stylish Executioner",
			want:    []string{"insert 822 into socket 1 of 8 on c1"},
			wantActive: []string{
				"Silence and Squall", "Ascension", "Winter's Shroud", "Facet of Protection", "Empty Fragment Socket",
			},
			wantOptions: [][]string{
				{"Storm's Edge"},
				{"Stylish Executioner"},
				{"Stylish Executioner"},
				{"Facet of Courage"},
				{"Facet of Courage"},
			},
			wantLocked: locked,
		},
		// Bungie can report an owned aspect as locked, so the insert goes
		// out, and Bungie accepts it.
		"aspect that Bungie reports as locked": {
			perk:    "threaded specter",
			replace: "Winter's Shroud",
			want:    []string{"insert 823 into socket 2 of 8 on c1"},
			wantActive: []string{
				"Silence and Squall",
				"Stylish Executioner",
				"Threaded Specter",
				"Facet of Protection",
				"Empty Fragment Socket",
			},
			wantOptions: [][]string{
				{"Storm's Edge"},
				{"Winter's Shroud", "Ascension"},
				{"Winter's Shroud", "Ascension"},
				{"Facet of Courage"},
				{"Facet of Courage"},
			},
			wantLocked: [][]string{{}, {}, {}, {}, {}},
		},
		"aspect that Bungie refuses": {
			perk:        "Threaded Specter",
			replace:     "Winter's Shroud",
			fail:        "insert 823 into socket 2 of 8 on c1",
			err:         bungietest.ErrStep,
			wantErr:     "inserting Threaded Specter: step refused",
			wantActive:  active,
			wantOptions: options,
			wantLocked:  locked,
		},
		"fragment that a full and an empty socket can take": {
			perk: "Facet of Courage",
			err:  ErrPerk,
			wantErr: "2 sockets of Prismatic Hunter can swap to Facet of Courage, " +
				"so pass replace with the active perk of one: Facet of Protection, Empty Fragment Socket",
			wantActive:  active,
			wantOptions: options,
			wantLocked:  locked,
		},
		"fragment into the empty fragment socket": {
			perk:    "Facet of Courage",
			replace: "Empty Fragment Socket",
			want:    []string{"insert 831 into socket 4 of 8 on c1"},
			wantActive: []string{
				"Silence and Squall",
				"Stylish Executioner",
				"Winter's Shroud",
				"Facet of Protection",
				"Facet of Courage",
			},
			wantOptions: [][]string{{"Storm's Edge"}, {"Ascension"}, {"Ascension"}, {}, {}},
			wantLocked:  locked,
		},
		// Both fragment sockets are empty, so the fragment goes into the
		// first of them.
		"fragment into the first of two empty fragment sockets": {
			sockets: map[int]uint32{3: plugEmptyFragment.Hash},
			perk:    "Facet of Courage",
			want:    []string{"insert 831 into socket 3 of 8 on c1"},
			wantActive: []string{
				"Silence and Squall",
				"Stylish Executioner",
				"Winter's Shroud",
				"Facet of Courage",
				"Empty Fragment Socket",
			},
			wantOptions: [][]string{
				{"Storm's Edge"}, {"Ascension"}, {"Ascension"}, {"Facet of Protection"}, {"Facet of Protection"},
			},
			wantLocked: locked,
		},
		"aspect into the empty aspect socket": {
			sockets: map[int]uint32{2: plugEmptyAspect.Hash},
			perk:    "Ascension",
			replace: "Empty Aspect Socket",
			want:    []string{"insert 822 into socket 2 of 8 on c1"},
			wantActive: []string{
				"Silence and Squall",
				"Stylish Executioner",
				"Ascension",
				"Facet of Protection",
				"Empty Fragment Socket",
			},
			wantOptions: [][]string{
				{"Storm's Edge"}, {"Winter's Shroud"}, {"Winter's Shroud"}, {"Facet of Courage"}, {"Facet of Courage"},
			},
			wantLocked: locked,
		},
		// The aspects open only socket 3 of the fragment sockets, so the
		// closed socket 4 is not a perk, and no insert goes out.
		"fragment into the closed fragment socket": {
			closed:  []int{4},
			perk:    "Facet of Courage",
			replace: "Empty Fragment Socket",
			err:     ErrPerk,
			wantErr: `Prismatic Hunter has no active perk named "Empty Fragment Socket". ` +
				"Its perks are Silence and Squall, Stylish Executioner, Winter's Shroud, Facet of Protection",
			wantActive:  active[:4],
			wantOptions: options[:4],
			wantLocked:  locked[:4],
		},
		// Both fragment sockets are empty, but socket 3 is closed, so the
		// fragment goes into socket 4.
		"fragment into the open one of two empty fragment sockets": {
			sockets: map[int]uint32{3: plugEmptyFragment.Hash},
			closed:  []int{3},
			perk:    "Facet of Courage",
			want:    []string{"insert 831 into socket 4 of 8 on c1"},
			wantActive: []string{
				"Silence and Squall",
				"Stylish Executioner",
				"Winter's Shroud",
				"Facet of Courage",
			},
			wantOptions: [][]string{{"Storm's Edge"}, {"Ascension"}, {"Ascension"}, {"Facet of Protection"}},
			wantLocked:  locked[:4],
		},
		// The closed socket 4 holds Facet of Courage, which the game does
		// not apply there, so the fragment goes into the open socket 3.
		"fragment that a closed socket holds": {
			sockets: map[int]uint32{3: plugEmptyFragment.Hash, 4: plugFacetOfCourage.Hash},
			closed:  []int{4},
			perk:    "Facet of Courage",
			replace: "Empty Fragment Socket",
			want:    []string{"insert 831 into socket 3 of 8 on c1"},
			wantActive: []string{
				"Silence and Squall",
				"Stylish Executioner",
				"Winter's Shroud",
				"Facet of Courage",
			},
			wantOptions: [][]string{{"Storm's Edge"}, {"Ascension"}, {"Ascension"}, {"Facet of Protection"}},
			wantLocked:  locked[:4],
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			owner := tc.owner
			if owner == "" {
				owner = bungietest.HunterID
			}

			pi := bungie.ProfileItem{InstanceID: subclassInstance, Hash: 800}
			item := NewItem(subclassProfile(t, tc.sockets, tc.closed), subclassDefs(), pi, owner, true)
			s := fixtureSnapshot(t)
			mover := &bungietest.StepMover{Fail: tc.fail}

			_, _, err := s.SetPerk(t.Context(), mover, item, tc.perk, tc.replace)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				assert.Contains(t, err.Error(), tc.wantErr)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tc.want, mover.Steps)

			var gotActive []string

			gotOptions := make([][]string, 0, len(item.Perks))
			gotLocked := make([][]string, 0, len(item.Perks))

			for _, perk := range item.Perks {
				gotActive = append(gotActive, perk.Name)
				gotOptions = append(gotOptions, perk.OptionNames())
				gotLocked = append(gotLocked, perk.LockedNames())
			}

			assert.Equal(t, tc.wantActive, gotActive)
			assert.Equal(t, tc.wantOptions, gotOptions)
			assert.Equal(t, tc.wantLocked, gotLocked)
		})
	}
}

func TestSetPerk(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err          error
		setup        func(*Item)
		id           string
		owner        string
		perk         string
		replace      string
		fail         string
		wantErr      string
		wantFrom     string
		wantTo       string
		want         []string
		wantActive   []string
		noCharacters bool
	}{
		"vault item names the most recently played character": {
			id:         "1001",
			perk:       "rampage",
			want:       []string{"insert 903 into socket 0 of 1001 on c1"},
			wantFrom:   "Explosive Payload",
			wantTo:     "Rampage",
			wantActive: []string{"Rampage", "Firefly"},
		},
		"character item names the character that holds it": {
			id:         "1001",
			owner:      bungietest.TitanID,
			perk:       "Rampage",
			want:       []string{"insert 903 into socket 0 of 1001 on c2"},
			wantFrom:   "Explosive Payload",
			wantTo:     "Rampage",
			wantActive: []string{"Rampage", "Firefly"},
		},
		"perk and replace with accents": {
			id:         "1001",
			perk:       "Rámpage",
			replace:    "EXPLÓSIVE PAYLOAD",
			want:       []string{"insert 903 into socket 0 of 1001 on c1"},
			wantFrom:   "Explosive Payload",
			wantTo:     "Rampage",
			wantActive: []string{"Rampage", "Firefly"},
		},
		"perk and replace with digits for letters": {
			id:         "1001",
			perk:       "Rampag3",
			replace:    "EXPL0S1VE PAYL0AD",
			want:       []string{"insert 903 into socket 0 of 1001 on c1"},
			wantFrom:   "Explosive Payload",
			wantTo:     "Rampage",
			wantActive: []string{"Rampage", "Firefly"},
		},
		"active perk with a digit for a letter sends no request": {
			id:         "1001",
			perk:       "F1refly",
			wantFrom:   "Firefly",
			wantTo:     "Firefly",
			wantActive: []string{"Explosive Payload", "Firefly"},
		},
		"perk in letters before a perk with a digit": {
			id: "1001",
			// The second socket can swap to a perk that differs from Rampage
			// only in a digit that stands for a letter.
			setup: func(item *Item) {
				item.Perks[1].Options = []Plug{{Name: "Rampag3", Type: "Trait", Hash: 950}}
			},
			perk:       "rampage",
			want:       []string{"insert 903 into socket 0 of 1001 on c1"},
			wantFrom:   "Explosive Payload",
			wantTo:     "Rampage",
			wantActive: []string{"Rampage", "Firefly"},
		},
		"perk with a digit before a perk in letters": {
			id: "1001",
			setup: func(item *Item) {
				item.Perks[1].Options = []Plug{{Name: "Rampag3", Type: "Trait", Hash: 950}}
				item.Perks[1].Socket = 3
			},
			perk:       "RAMPAG3",
			want:       []string{"insert 950 into socket 3 of 1001 on c1"},
			wantFrom:   "Firefly",
			wantTo:     "Rampag3",
			wantActive: []string{"Explosive Payload", "Rampag3"},
		},
		"perks that differ only in a digit and a letter": {
			id: "1001",
			setup: func(item *Item) {
				item.Perks[1].Options = []Plug{{Name: "Rampag3", Type: "Trait", Hash: 950}}
			},
			perk:       "rampa9e",
			err:        ErrPerk,
			wantErr:    "2 sockets of Fatebringer can swap to Rampage, so pass replace with the active perk of one: Explosive Payload, Firefly",
			wantActive: []string{"Explosive Payload", "Firefly"},
		},
		"replace in letters before an active perk with a digit": {
			id: "1001",
			// Both sockets can swap to Rampage, and their active perks differ
			// only in a digit that stands for a letter.
			setup: func(item *Item) {
				item.Perks[1].Plug = Plug{Name: "Expl0sive Payload", Type: "Trait", Hash: 951}
				item.Perks[1].Options = []Plug{plugRampage}
			},
			perk:       "Rampage",
			replace:    "explosive payload",
			want:       []string{"insert 903 into socket 0 of 1001 on c1"},
			wantFrom:   "Explosive Payload",
			wantTo:     "Rampage",
			wantActive: []string{"Rampage", "Expl0sive Payload"},
		},
		"active perk sends no request": {
			id:         "1001",
			perk:       "EXPLOSIVE PAYLOAD",
			wantFrom:   "Explosive Payload",
			wantTo:     "Explosive Payload",
			wantActive: []string{"Explosive Payload", "Firefly"},
		},
		"active perk with an accent sends no request": {
			id:         "1001",
			perk:       "EXPLÓSIVE PAYLOAD",
			wantFrom:   "Explosive Payload",
			wantTo:     "Explosive Payload",
			wantActive: []string{"Explosive Payload", "Firefly"},
		},
		"replace that names the active perk sends no request": {
			id:         "1001",
			perk:       "Firefly",
			replace:    "FIREFLY",
			wantFrom:   "Firefly",
			wantTo:     "Firefly",
			wantActive: []string{"Explosive Payload", "Firefly"},
		},
		"perk active in a socket that replace does not select": {
			id:         "1001",
			perk:       "Firefly",
			replace:    "Explosive Payload",
			err:        ErrPerk,
			wantErr:    `no socket of Fatebringer that holds Explosive Payload can swap to "Firefly". Sockets that can swap: Explosive Payload (can swap to Rampage)`,
			wantActive: []string{"Explosive Payload", "Firefly"},
		},
		"replace selects one of two sockets": {
			id: "1001",
			// The second perk sits in socket 3, so the request must name the
			// socket index rather than the position in the perk list.
			setup: func(item *Item) {
				item.Perks[1].Options = []Plug{plugRampage}
				item.Perks[1].Socket = 3
			},
			perk:       "Rampage",
			replace:    "firefly",
			want:       []string{"insert 903 into socket 3 of 1001 on c1"},
			wantFrom:   "Firefly",
			wantTo:     "Rampage",
			wantActive: []string{"Explosive Payload", "Rampage"},
		},
		"two sockets can swap to the perk": {
			id:         "1001",
			setup:      func(item *Item) { item.Perks[1].Options = []Plug{plugRampage} },
			perk:       "Rampage",
			err:        ErrPerk,
			wantErr:    "2 sockets of Fatebringer can swap to Rampage, so pass replace with the active perk of one: Explosive Payload, Firefly",
			wantActive: []string{"Explosive Payload", "Firefly"},
		},
		// SetPerk picks the first of several sockets only when they share
		// the plug set of a subclass. Two weapon sockets that hold one perk
		// therefore still ask for replace.
		"two sockets without a plug set hold one perk": {
			id: "1001",
			setup: func(item *Item) {
				item.Perks = []Perk{
					{Plug: plugRampage, Options: []Plug{plugKillClip}, Socket: 1},
					{Plug: plugRampage, Options: []Plug{plugKillClip}, Socket: 2},
				}
			},
			perk:       "Kill Clip",
			err:        ErrPerk,
			wantErr:    "2 sockets of Fatebringer can swap to Kill Clip, so pass replace with the active perk of one: Rampage, Rampage",
			wantActive: []string{"Rampage", "Rampage"},
		},
		"no socket can swap to the perk": {
			id:         "1001",
			perk:       "Kill Clip",
			err:        ErrPerk,
			wantErr:    `no socket of Fatebringer can swap to "Kill Clip". Sockets that can swap: Explosive Payload (can swap to Rampage)`,
			wantActive: []string{"Explosive Payload", "Firefly"},
		},
		"replaced socket cannot swap": {
			id:         "1001",
			perk:       "Rampage",
			replace:    "Firefly",
			err:        ErrPerk,
			wantErr:    "no socket of Fatebringer that holds Firefly can swap its perk",
			wantActive: []string{"Explosive Payload", "Firefly"},
		},
		"replace names no active perk": {
			id:         "1001",
			perk:       "Rampage",
			replace:    "Kill Clip",
			err:        ErrPerk,
			wantErr:    `Fatebringer has no active perk named "Kill Clip". Its perks are Explosive Payload, Firefly`,
			wantActive: []string{"Explosive Payload", "Firefly"},
		},
		"item without perks": {
			id:      "1002",
			perk:    "Rampage",
			err:     ErrPerk,
			wantErr: "Gjallarhorn has no perks",
		},
		"account without a character": {
			id:           "1001",
			perk:         "Rampage",
			noCharacters: true,
			err:          ErrPerk,
			wantErr:      "the account has no character to name in the request",
			wantActive:   []string{"Explosive Payload", "Firefly"},
		},
		"bungie refuses": {
			id:         "1001",
			perk:       "Rampage",
			fail:       "insert 903 into socket 0 of 1001 on c1",
			err:        bungietest.ErrStep,
			wantErr:    "inserting Rampage: step refused",
			wantActive: []string{"Explosive Payload", "Firefly"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := fixtureSnapshot(t)
			mover := &bungietest.StepMover{Fail: tc.fail}

			item, ok := s.Find(tc.id)
			require.True(t, ok)

			if tc.owner != "" {
				item.Owner = tc.owner
			}

			if tc.setup != nil {
				tc.setup(item)
			}

			if tc.noCharacters {
				s.Characters = nil
			}

			from, to, err := s.SetPerk(t.Context(), mover, item, tc.perk, tc.replace)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				assert.Contains(t, err.Error(), tc.wantErr)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tc.wantFrom, from.Name)
			assert.Equal(t, tc.wantTo, to.Name)
			assert.Equal(t, tc.want, mover.Steps)

			var active []string
			for _, perk := range item.Perks {
				active = append(active, perk.Name)
			}

			assert.Equal(t, tc.wantActive, active)
		})
	}
}

func TestSetPerkRecordsTheSwap(t *testing.T) {
	t.Parallel()

	// Each case sets socket 0 of item 1001 to perk, or keeps the fixture
	// when perk is nil. It swaps the socket to "to" and then back to back.
	// want lists the options of the socket after the first swap, and
	// wantSteps lists the requests of both swaps.
	tests := map[string]struct {
		perk      *Perk
		to        string
		back      string
		want      []Plug
		wantSteps []string
		wantTo    Plug
		wantBack  Plug
	}{
		"replaced plug becomes an option": {
			to:        "Rampage",
			back:      "Explosive Payload",
			want:      []Plug{plugExplosivePayload},
			wantSteps: []string{"insert 903 into socket 0 of 1001 on c1", "insert 900 into socket 0 of 1001 on c1"},
			wantTo:    plugRampage,
			wantBack:  plugExplosivePayload,
		},
		"swap back from a base trait inserts the enhanced trait": {
			perk: &Perk{
				Plug:    plugKillClip,
				Options: []Plug{plugChillClipEnhanced},
				Plugs:   []Plug{plugKillClip, plugKillClipEnhanced, plugChillClip, plugChillClipEnhanced},
			},
			to:        "Chill Clip",
			back:      "Kill Clip",
			want:      []Plug{plugKillClipEnhanced},
			wantSteps: []string{"insert 908 into socket 0 of 1001 on c1", "insert 905 into socket 0 of 1001 on c1"},
			wantTo:    plugChillClipEnhanced,
			wantBack:  plugKillClipEnhanced,
		},
		"active plug that bungie will not insert does not become an option": {
			perk: &Perk{
				Plug:    plugKillClip,
				Options: []Plug{plugChillClip, plugRampage},
				Plugs:   []Plug{plugChillClip, plugRampage},
			},
			to:        "Chill Clip",
			back:      "Rampage",
			want:      []Plug{plugRampage},
			wantSteps: []string{"insert 906 into socket 0 of 1001 on c1", "insert 903 into socket 0 of 1001 on c1"},
			wantTo:    plugChillClip,
			wantBack:  plugRampage,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := fixtureSnapshot(t)
			mover := &bungietest.StepMover{}

			item, ok := s.Find("1001")
			require.True(t, ok)

			if tc.perk != nil {
				item.Perks[0] = *tc.perk
			}

			from := item.Perks[0].Plug

			before, to, err := s.SetPerk(t.Context(), mover, item, tc.to, "")
			require.NoError(t, err)
			assert.Equal(t, from, before)
			assert.Equal(t, tc.wantTo, to)
			assert.Equal(t, tc.wantTo, item.Perks[0].Plug)
			assert.Equal(
				t,
				tc.want,
				item.Perks[0].Options,
				"the options of the socket follow the rules of a profile fetch",
			)

			before, to, err = s.SetPerk(t.Context(), mover, item, tc.back, "")
			require.NoError(t, err)
			assert.Equal(t, tc.wantTo, before)
			assert.Equal(t, tc.wantBack, to)
			assert.Equal(t, tc.wantSteps, mover.Steps)
		})
	}
}
