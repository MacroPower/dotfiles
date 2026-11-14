package inventory

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungietest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitionstest"
)

// Plugs of the fixture definitions.
var (
	plugExplosivePayload  = Plug{Name: "Explosive Payload", Type: "Trait", Hash: 900}
	plugRampage           = Plug{Name: "Rampage", Type: "Trait", Hash: 903}
	plugKillClip          = Plug{Name: "Kill Clip", Type: "Trait", Hash: 904}
	plugKillClipEnhanced  = Plug{Name: "Kill Clip", Type: "Enhanced Trait", Hash: 905}
	plugChillClip         = Plug{Name: "Chill Clip", Type: "Trait", Hash: 906}
	plugChillClipEnhanced = Plug{Name: "Chill Clip", Type: "Enhanced Trait", Hash: 908}
)

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
			}, perksOf(&profile, definitionstest.Defs(), "7"))
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
		"active perk sends no request": {
			id:         "1001",
			perk:       "EXPLOSIVE PAYLOAD",
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
