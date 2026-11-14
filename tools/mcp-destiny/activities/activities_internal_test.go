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
		"blank query":                        {query: "  ", want: []uint32{1, 2, 3, 4}},
		"query that matches nothing":         {query: "gambit"},
		"rewards":                            {rewards: true, want: []uint32{2, 3}},
		"query and rewards":                  {query: "vu", rewards: true},
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
