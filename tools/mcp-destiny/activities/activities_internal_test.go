package activities

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitionstest"
)

func TestRead(t *testing.T) {
	t.Parallel()

	reward := func(style string, hash uint32, quantity int) bungie.ActivityRewardItem {
		item := bungie.ActivityRewardItem{UIStyle: style}
		item.ItemQuantity.ItemHash = hash
		item.ItemQuantity.Quantity = quantity

		return item
	}

	entry := func(hash uint32, items ...bungie.ActivityRewardItem) bungie.AvailableActivity {
		return bungie.AvailableActivity{
			Hash:           hash,
			Visible:        true,
			VisibleRewards: []bungie.ActivityRewardMapping{{RewardItems: items}},
		}
	}

	challenged := func(a bungie.AvailableActivity) bungie.AvailableActivity {
		a.Challenges = []bungie.ActivityChallenge{{Objective: bungie.ProfileObjective{Hash: 950, CompletionValue: 1}}}

		return a
	}

	focused := func(a bungie.AvailableActivity) bungie.AvailableActivity {
		a.Focused = true

		return a
	}

	tests := map[string]struct {
		defs    func(*definitions.Defs)
		entries []bungie.AvailableActivity
		want    []Row
	}{
		"definitions without activities": {
			defs:    func(d *definitions.Defs) { d.Activities = map[uint32]definitions.ActivityDef{} },
			entries: []bungie.AvailableActivity{entry(3022541210, reward(bungie.RewardBonusDrop, 1, 2))},
		},
		"definitions without types, locations, or rewards": {
			defs: func(d *definitions.Defs) {
				d.ActivityTypes = map[uint32]string{}
				d.Destinations = map[uint32]string{}
				d.Places = map[uint32]string{}
				d.ActivityRewards = map[uint32]string{}
			},
			entries: []bungie.AvailableActivity{
				entry(
					3022541210,
					reward(bungie.RewardBonusDrop, 1, 2),
					reward(bungie.RewardFocus, 601, 1),
					reward("", 600, 1),
				),
			},
			want: []Row{{Hash: 3022541210, Name: "Vault of Glass: Master", BonusDrops: 2}},
		},
		"first named focused reward wins": {
			entries: []bungie.AvailableActivity{
				entry(
					4003,
					reward(bungie.RewardFocus, 698, 1),
					reward(bungie.RewardFocus, 606, 1),
					reward(bungie.RewardFocus, 601, 1),
				),
			},
			want: []Row{{
				Hash:  4003,
				Name:  "Nightfall: Grandmaster",
				Type:  "Strike",
				Focus: &Loot{Name: "Adept Weapon", Quantity: 1, Hash: 606},
			}},
		},
		"focused reward with a quantity of zero": {
			entries: []bungie.AvailableActivity{entry(4003, reward(bungie.RewardFocus, 606, 0))},
			want: []Row{{
				Hash:  4003,
				Name:  "Nightfall: Grandmaster",
				Type:  "Strike",
				Focus: &Loot{Name: "Adept Weapon", Hash: 606},
			}},
		},
		"bonus loot keeps the order and the hash of the first item of a name": {
			entries: []bungie.AvailableActivity{
				entry(2222, reward("", 605, 1), reward("", 604, 1), reward("", 600, 3)),
			},
			want: []Row{{
				Hash: 2222,
				Name: "Vanguard Ops",
				Type: "Strike",
				Loot: []Loot{
					{Name: "Raid Gear", Quantity: 4, Hash: 605},
					{Name: "Enhancement Prism", Quantity: 1, Hash: 604},
				},
			}},
		},
		// Bungie never focuses a raid or a dungeon, so a challenge on one
		// marks it as featured. A challenge on an activity of another type
		// does not.
		"featured activities": {
			entries: []bungie.AvailableActivity{
				challenged(entry(3022541210)),
				entry(4004),
				challenged(entry(4001)),
				challenged(entry(4003)),
				focused(entry(2222)),
			},
			want: []Row{
				{Hash: 2222, Name: "Vanguard Ops", Type: "Strike", Featured: true},
				{Hash: 3022541210, Name: "Vault of Glass: Master", Type: "Raid", Location: "Venus", Featured: true},
				{Hash: 4001, Name: "Warlord's Ruin", Type: "Dungeon", Location: "Earth", Featured: true},
				{Hash: 4003, Name: "Nightfall: Grandmaster", Type: "Strike"},
				{Hash: 4004, Name: "Pantheon", Type: "Raid"},
			},
		},
		"equal rewards order by name and then by hash": {
			defs: func(d *definitions.Defs) {
				d.Activities[4010] = definitions.ActivityDef{Name: "Control"}
				// The lowest hash sorts last by name.
				d.Activities[3999] = definitions.ActivityDef{Name: "Duality"}
			},
			entries: []bungie.AvailableActivity{
				entry(4444),
				entry(3999),
				entry(4010),
				entry(4009),
			},
			want: []Row{
				{Hash: 4009, Name: "Battleground: Oracle", Type: "Strike"},
				{Hash: 4010, Name: "Control"},
				{Hash: 4444, Name: "Control", Type: "The Crucible"},
				{Hash: 3999, Name: "Duality"},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			defs := definitionstest.Defs()
			if tc.defs != nil {
				tc.defs(defs)
			}

			assert.Equal(t, tc.want, Read(tc.entries, defs))
		})
	}
}

func TestFilter(t *testing.T) {
	t.Parallel()

	rows := []Row{
		{Hash: 1, Name: "Déjà Vu", Type: "Raid", Location: "Neomuna"},
		{Hash: 2, Name: "Pit", Type: "Strike", Location: "Côte", Focus: &Loot{Name: "Mataiodoxía"}},
		{Hash: 3, Name: "Control", Type: "The Crucible", Loot: []Loot{{Name: "Xûr Engram"}}},
		{Hash: 4, Name: "Deja Vu", Type: "Dúngeon"},
		{Hash: 5, Name: "K1 Logistics", Type: "Lost Sector", Location: "Moon", Focus: &Loot{Name: "St0mp-EE5"}},
		{
			Hash:     6,
			Name:     "Bunker E15",
			Type:     "Lost Sector",
			Location: "Europa",
			Loot:     []Loot{{Name: "Fr0st-EE5"}, {Name: "S733k Pack"}},
		},
	}

	tests := map[string]struct {
		query   string
		want    []uint32
		rewards bool
	}{
		"name without its accents":           {query: "deja", want: []uint32{1, 4}},
		"name with accents":                  {query: " DÉJÀ ", want: []uint32{1, 4}},
		"type in another letter case":        {query: "RAID", want: []uint32{1}},
		"type without its accents":           {query: "dungeon", want: []uint32{4}},
		"location without its accents":       {query: "cote", want: []uint32{2}},
		"focused reward without its accents": {query: "doxia", want: []uint32{2}},
		"bonus loot without its accents":     {query: "xur", want: []uint32{3}},
		"blank query":                        {query: "  ", want: []uint32{1, 2, 3, 4, 5, 6}},
		"query that matches nothing":         {query: "gambit"},
		"rewards":                            {rewards: true, want: []uint32{2, 3, 5, 6}},
		"query and rewards":                  {query: "vu", rewards: true},
		"focused reward in letters":          {query: "stomp", want: []uint32{5}},
		"focused reward as the game has it":  {query: "ST0MP-EE5", want: []uint32{5}},
		"bonus loot in letters":              {query: "frost-ees", want: []uint32{6}},
		"name in letters":                    {query: "ki logistics", want: []uint32{5}},
		"name as the game has it":            {query: "k1 log", want: []uint32{5}},
		"type with a digit for a letter":     {query: "l0st sector", want: []uint32{5, 6}},
		"location with a digit for a letter": {query: "eur0pa", want: []uint32{6}},
		"another digit":                      {query: "st9mp"},
		"bonus loot with a seven for l":      {query: "sleek pack", want: []uint32{6}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var got []uint32
			for _, row := range Filter(rows, tc.query, tc.rewards) {
				got = append(got, row.Hash)
			}

			assert.Equal(t, tc.want, got)
		})
	}
}
