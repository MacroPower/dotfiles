package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/clarity"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/claritytest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/inventory"
)

// clarityCreditLine is the line that follows a perk list with a
// description of the Clarity database.
const clarityCreditLine = `Under each perk, the lines before "Clarity:" are the in-game text of Bungie, ` +
	`and the lines from "Clarity:" on are the tested description of the Clarity database (d2clarity.com), ` +
	`except a line that starts with "Might predate the final patch", which the server adds.`

func TestWritePerks(t *testing.T) {
	t.Parallel()

	plug := func(hash uint32, name, typeName, description string) inventory.Plug {
		return inventory.Plug{Hash: hash, Name: name, Type: typeName, Description: description}
	}

	uploaded := time.Date(2026, time.January, 17, 0, 0, 0, 0, time.UTC)
	current := time.Date(2026, time.July, 9, 0, 0, 0, 0, time.UTC)

	// The perks of an exotic rocket launcher. Clarity describes the
	// first three, and it describes the base perk of the enhanced origin
	// trait. The definitions hold a text of Bungie for the first and the
	// last.
	perks := []inventory.Perk{
		{Plug: plug(10, "Prototype Trueseeker", "Intrinsic", "Rockets track their target.")},
		{
			Plug:    plug(11, "Focused Fury", "Trait", ""),
			Options: []inventory.Plug{plug(13, "Kill Clip", "Trait", ""), plug(14, "Rampage", "Trait", "")},
		},
		{Plug: plug(12, "Kill Clip", "Enhanced Trait", "")},
		{Plug: plug(16, "Gun and Run Enhanced", "Enhanced Origin Trait", "")},
		{Plug: plug(15, "Quick Launch", "Barrel", "Greatly increases handling.\n\n Increases velocity. ")},
	}

	database := &clarity.Database{
		Version: "2.5",
		Descriptions: map[uint32]clarity.Description{
			10: {
				Name:     "Prototype Trueseeker",
				Item:     "Truth",
				Uploaded: uploaded,
				Lines:    []string{"Rockets track and detonate within 3 meters of a target."},
			},
			11: {
				Name:     "Focused Fury",
				Uploaded: uploaded,
				Lines:    []string{"Precision hits grant 20% damage.", "\U00002022 Lasts 12 seconds."},
			},
			12: {
				Name:     "Kill Clip",
				Uploaded: uploaded,
				Lines:    []string{"Grants 25% [PvP 20%] damage for " + clarity.EnhancedMark + "5.5 seconds."},
			},
			99: {Name: "Rampage", Uploaded: current, Lines: []string{"Kills grant damage."}},
			// The base perks of plug 16 and plug 15. Plug 16 is
			// enhanced, so it shows the description of its base perk,
			// and plug 15 does not.
			97: {
				Name:     "Gun and Run",
				Type:     "Weapon Trait Origin",
				Uploaded: current,
				Lines:    []string{"Sprinting grants 20 Handling for 3 seconds."},
			},
			98: {Name: "Quick Launch", Type: "Weapon Perk", Uploaded: current, Lines: []string{"Grants 15 Handling."}},
		},
	}

	// The perk list with only the text of Bungie.
	const bungieOnly = "- Prototype Trueseeker (Intrinsic)\n" +
		"  Rockets track their target.\n" +
		"- Focused Fury (Trait), can swap to Kill Clip, Rampage\n" +
		"- Kill Clip (Enhanced Trait)\n" +
		"- Gun and Run Enhanced (Enhanced Origin Trait)\n" +
		"- Quick Launch (Barrel)\n" +
		"  Greatly increases handling.\n" +
		"  Increases velocity.\n"

	tests := map[string]struct {
		tested clarity.State
		want   string
	}{
		"database that describes four of the perks": {
			tested: clarity.State{Database: database},
			want: "- Prototype Trueseeker (Intrinsic)\n" +
				"  Rockets track their target.\n" +
				"  Clarity: Rockets track and detonate within 3 meters of a target.\n" +
				"  Might predate the final patch: Destiny 2 Update 9.7.0.2 (2026-06-23) changed Truth " +
				"after Clarity last updated this text.\n" +
				"- Focused Fury (Trait), can swap to Kill Clip, Rampage\n" +
				"  Clarity: Precision hits grant 20% damage.\n" +
				"  \U00002022 Lasts 12 seconds.\n" +
				"  Might predate the final patch: Destiny 2 Update 9.7.0 (2026-06-09) changed Focused Fury " +
				"after Clarity last updated this text.\n" +
				"- Kill Clip (Enhanced Trait)\n" +
				"  Clarity: Grants 25% [PvP 20%] damage for \U000021915.5 seconds.\n" +
				"- Gun and Run Enhanced (Enhanced Origin Trait)\n" +
				"  Clarity: Sprinting grants 20 Handling for 3 seconds.\n" +
				"- Quick Launch (Barrel)\n" +
				"  Greatly increases handling.\n" +
				"  Increases velocity.\n" +
				clarityCreditLine + "\n",
		},
		"database that describes none of the perks": {
			tested: clarity.State{Database: &clarity.Database{
				Version:      "2.5",
				Descriptions: map[uint32]clarity.Description{99: database.Descriptions[99]},
			}},
			want: bungieOnly,
		},
		"no database": {
			tested: clarity.State{
				Err: fmt.Errorf("%w: %w", clarity.ErrUnavailable, errors.New("the site returned status 503")),
			},
			want: bungieOnly +
				"Clarity descriptions unavailable: the site returned status 503. " +
				"The perks show only the in-game text of Bungie.\n",
		},
		"state of an item whose perks the server did not look up": {
			want: bungieOnly,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var b strings.Builder

			writePerks(&b, perks, tc.tested)
			assert.Equal(t, tc.want, b.String())
		})
	}
}

func TestWritePerksSubclass(t *testing.T) {
	t.Parallel()

	aspect := func(hash uint32, name string, slots int) inventory.Plug {
		return inventory.Plug{Hash: hash, Name: name, Type: "Prismatic Hunter Aspect", FragmentSlots: slots}
	}

	fragment := func(hash uint32, name string) inventory.Plug {
		return inventory.Plug{Hash: hash, Name: name, Type: "Prismatic Fragment"}
	}

	// The perks of a subclass. Each aspect, active, a swap, or locked,
	// gives its fragment slots. A super and a fragment open no fragment
	// slot. The locked plugs follow the swaps, and a socket without a
	// swap lists only its locked plugs. The placeholder of an empty
	// socket has no type.
	perks := []inventory.Perk{
		{
			Plug:    inventory.Plug{Hash: 10, Name: "Silence and Squall", Type: "Prismatic Super"},
			Options: []inventory.Plug{{Hash: 11, Name: "Storm's Edge", Type: "Prismatic Super"}},
		},
		{
			Plug:    aspect(20, "Stylish Executioner", 2),
			Options: []inventory.Plug{aspect(22, "Ascension", 3), aspect(23, "Flowing Grace", 1)},
			Locked:  []inventory.Plug{aspect(24, "Threaded Specter", 2)},
		},
		{Plug: aspect(21, "Winter's Shroud", 2)},
		{
			Plug:    fragment(30, "Facet of Protection"),
			Options: []inventory.Plug{fragment(31, "Facet of Courage")},
		},
		{
			Plug:   inventory.Plug{Hash: 39, Name: "Empty Fragment Socket"},
			Locked: []inventory.Plug{fragment(31, "Facet of Courage"), fragment(32, "Facet of Dominance")},
		},
	}

	var b strings.Builder

	writePerks(&b, perks, clarity.State{})
	assert.Equal(t, "- Silence and Squall (Prismatic Super), can swap to Storm's Edge\n"+
		"- Stylish Executioner (Prismatic Hunter Aspect, 2 fragment slots), "+
		"can swap to Ascension (3 fragment slots), Flowing Grace (1 fragment slot); "+
		"Bungie reports as locked: Threaded Specter (2 fragment slots)\n"+
		"- Winter's Shroud (Prismatic Hunter Aspect, 2 fragment slots)\n"+
		"- Facet of Protection (Prismatic Fragment), can swap to Facet of Courage\n"+
		"- Empty Fragment Socket, Bungie reports as locked: Facet of Courage, Facet of Dominance\n", b.String())
}

// TestGetItemClarity pins the perk descriptions of get_item for each
// state of the Clarity database. Instance 1001 holds Explosive Payload
// (900) and Firefly (901), and the fixture definitions give only Firefly
// a text of Bungie.
func TestGetItemClarity(t *testing.T) {
	t.Parallel()

	const (
		payload = "- Explosive Payload (Trait), can swap to Rampage"
		firefly = "- Firefly (Trait)\n" +
			"  Precision final blows cause targets to explode.\n" +
			"  Explosions deal Solar damage."
	)

	beforeUpdate := time.Date(2026, time.January, 17, 0, 0, 0, 0, time.UTC)

	tests := map[string]struct {
		// site changes the fake Clarity site before the call.
		site func(t *testing.T, site *claritytest.Server)
		// defs changes the fixture definitions before the call.
		defs func(*definitions.Defs)
		// want lists the lines of the perk block after its heading.
		// "{clarity}" stands for the base URL of the fake Clarity site.
		want []string
	}{
		"description of Clarity for a covered perk and text of Bungie for the rest": {
			want: []string{
				payload,
				"  Clarity: Each bullet deals half of its damage as an explosion.",
				"  \U00002022 The explosion ignores damage falloff.",
				firefly,
				clarityCreditLine,
			},
		},
		"description of Clarity under the text of Bungie": {
			site: func(t *testing.T, site *claritytest.Server) {
				t.Helper()

				site.Descriptions = claritytest.Descriptions(t, claritytest.Perk{
					Hash:     901,
					Name:     "Firefly",
					Uploaded: time.Date(2026, time.July, 9, 0, 0, 0, 0, time.UTC),
					Lines:    []string{"Precision kills explode in a radius of 3.5 meters."},
				})
			},
			want: []string{
				payload,
				firefly,
				"  Clarity: Precision kills explode in a radius of 3.5 meters.",
				clarityCreditLine,
			},
		},
		"database that describes no perk of the item": {
			site: func(t *testing.T, site *claritytest.Server) {
				t.Helper()

				site.Descriptions = claritytest.Descriptions(t, claritytest.Perk{
					Hash:  904,
					Name:  "Kill Clip",
					Lines: []string{"Reloading after a kill grants 25% damage."},
				})
			},
			want: []string{payload, firefly},
		},
		"site that does not answer": {
			site: func(_ *testing.T, site *claritytest.Server) { site.Status = http.StatusServiceUnavailable },
			want: []string{
				payload,
				firefly,
				"Clarity descriptions unavailable: reading the version: " +
					"{clarity}/versions.json returned status 503. " +
					"The perks show only the in-game text of Bungie.",
			},
		},
		"descriptions file without a description": {
			site: func(_ *testing.T, site *claritytest.Server) { site.Descriptions = `{}` },
			want: []string{
				payload,
				firefly,
				"Clarity descriptions unavailable: the descriptions file holds no description. " +
					"The perks show only the in-game text of Bungie.",
			},
		},
		"description of the base perk for an enhanced perk without one": {
			defs: func(d *definitions.Defs) {
				d.Items[900] = definitions.ItemDef{
					Name:      "Explosive Payload",
					TypeName:  "Enhanced Trait",
					ItemType:  bungie.ItemTypePlug,
					ClassType: bungie.ClassAny,
				}
			},
			site: func(t *testing.T, site *claritytest.Server) {
				t.Helper()

				site.Descriptions = claritytest.Descriptions(t, claritytest.Perk{
					Hash:     950,
					Name:     "Explosive Payload",
					Type:     "Weapon Trait",
					Uploaded: time.Date(2026, time.July, 9, 0, 0, 0, 0, time.UTC),
					Lines:    []string{"Each bullet deals half of its damage as an explosion."},
				})
			},
			want: []string{
				"- Explosive Payload (Enhanced Trait), can swap to Rampage",
				"  Clarity: Each bullet deals half of its damage as an explosion.",
				firefly,
				clarityCreditLine,
			},
		},
		"description that predates a patch that changed the perk": {
			// Update 9.7.0 changed Focused Fury, so plug 900 takes that
			// name.
			defs: func(d *definitions.Defs) {
				d.Items[900] = definitions.ItemDef{
					Name:      "Focused Fury",
					TypeName:  "Trait",
					ItemType:  bungie.ItemTypePlug,
					ClassType: bungie.ClassAny,
				}
			},
			site: func(t *testing.T, site *claritytest.Server) {
				t.Helper()

				site.Descriptions = claritytest.Descriptions(t, claritytest.Perk{
					Hash:     900,
					Name:     "Focused Fury",
					Uploaded: beforeUpdate,
					Lines:    []string{"Half of the magazine as precision hits grants 20% damage."},
				})
			},
			want: []string{
				"- Focused Fury (Trait), can swap to Rampage",
				"  Clarity: Half of the magazine as precision hits grants 20% damage.",
				"  Might predate the final patch: Destiny 2 Update 9.7.0 (2026-06-09) changed Focused Fury " +
					"after Clarity last updated this text.",
				firefly,
				clarityCreditLine,
			},
		},
		"enhanced perk whose base perk Clarity updated after the patch": {
			// Clarity updates the text of an enhanced perk with the text
			// of its base perk. The entry of the enhanced perk keeps the
			// time of its own upload.
			defs: func(d *definitions.Defs) {
				d.Items[900] = definitions.ItemDef{
					Name:      "Focused Fury",
					TypeName:  "Enhanced Trait",
					ItemType:  bungie.ItemTypePlug,
					ClassType: bungie.ClassAny,
				}
			},
			site: func(t *testing.T, site *claritytest.Server) {
				t.Helper()

				site.Descriptions = claritytest.Descriptions(t,
					claritytest.Perk{
						Hash:     900,
						Name:     "Focused Fury",
						Type:     "Weapon Trait Enhanced",
						Uploaded: time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC),
						Lines: []string{
							"Precision hits grant 25% damage for " + clarity.EnhancedMark + "12 seconds.",
						},
					},
					claritytest.Perk{
						Hash:     950,
						Name:     "Focused Fury",
						Type:     "Weapon Trait",
						Uploaded: time.Date(2026, time.July, 7, 0, 0, 0, 0, time.UTC),
						Lines:    []string{"Precision hits grant 25% damage for 11 seconds."},
					},
				)
			},
			want: []string{
				"- Focused Fury (Enhanced Trait), can swap to Rampage",
				"  Clarity: Precision hits grant 25% damage for " + clarity.EnhancedMark + "12 seconds.",
				firefly,
				clarityCreditLine,
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)

			if tc.site != nil {
				tc.site(t, f.clarity)
			}

			if tc.defs != nil {
				tc.defs(f.defs)
			}

			got, isError := call(t, f.h.handleGetItem, GetItemInput{InstanceID: "1001"})
			require.False(t, isError, "a call without the tested descriptions still shows the item")

			want := strings.ReplaceAll(strings.Join(tc.want, "\n"), "{clarity}", f.clarity.URL)
			assert.Contains(t, got, "\n\nPerks:\n"+want+"\n\nRoll ratings:\n")
		})
	}
}

// TestGetItemClarityRequests pins the requests that get_item sends to the
// Clarity site. An item without perks costs none. The first item with
// perks costs one check of the version and one download, and the calls
// of the next hour cost none.
func TestGetItemClarityRequests(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	// The profile lists no perks for instance 1003.
	got, isError := call(t, f.h.handleGetItem, GetItemInput{InstanceID: "1003"})
	require.False(t, isError)
	assert.NotContains(t, got, "Clarity")
	assert.Empty(t, f.clarity.All())

	for range 2 {
		got, isError = call(t, f.h.handleGetItem, GetItemInput{InstanceID: "1001"})
		require.False(t, isError)
		assert.Contains(t, got, "  Clarity: Each bullet deals half of its damage as an explosion.\n")
	}

	assert.Equal(t, 1, f.clarity.Checks())
	assert.Equal(t, 1, f.clarity.Downloads())
}

// TestGetItemClarityCache pins that a server process reads the tested
// descriptions from the cache file of an earlier process while the
// Clarity site does not answer.
func TestGetItemClarityCache(t *testing.T) {
	t.Parallel()

	site := claritytest.NewServer(t)
	dir := t.TempDir()

	// getItem returns the get_item output of a new server process that
	// reads site and keeps its Clarity cache file in dir.
	getItem := func() string {
		f := newFakes(t)
		f.h.clarity = clarity.NewStore(clarity.NewClient(clarity.WithURL(site.URL)), dir, clarity.WithClock(f.h.now))

		got, isError := call(t, f.h.handleGetItem, GetItemInput{InstanceID: "1001"})
		require.False(t, isError)

		return got
	}

	assert.Contains(t, getItem(), "  Clarity: Each bullet deals half of its damage as an explosion.\n")
	assert.Equal(t, 1, site.Downloads())

	site.Status = http.StatusServiceUnavailable

	got := getItem()
	assert.Contains(t, got, "  Clarity: Each bullet deals half of its damage as an explosion.\n")
	assert.Contains(t, got, clarityCreditLine+"\n")
	assert.Equal(t, 2, site.Checks())
	assert.Equal(t, 1, site.Downloads())
}
