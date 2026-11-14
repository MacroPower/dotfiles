package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadActivities(t *testing.T) {
	t.Parallel()

	reward := func(style string, hash uint32, quantity int) ActivityRewardItem {
		item := ActivityRewardItem{UIStyle: style}
		item.ItemQuantity.ItemHash = hash
		item.ItemQuantity.Quantity = quantity

		return item
	}

	entry := func(hash uint32, items ...ActivityRewardItem) AvailableActivity {
		return AvailableActivity{
			Hash:           hash,
			Visible:        true,
			VisibleRewards: []ActivityRewardMapping{{RewardItems: items}},
		}
	}

	tests := map[string]struct {
		defs    func(*Defs)
		entries []AvailableActivity
		want    []ActivityRow
	}{
		"definitions without activities": {
			defs:    func(d *Defs) { d.Activities = map[uint32]ActivityDef{} },
			entries: []AvailableActivity{entry(3022541210, reward(rewardBonusDrop, 1, 2))},
		},
		"definitions without types, locations, or rewards": {
			defs: func(d *Defs) {
				d.ActivityTypes = map[uint32]string{}
				d.Destinations = map[uint32]string{}
				d.Places = map[uint32]string{}
				d.ActivityRewards = map[uint32]string{}
			},
			entries: []AvailableActivity{
				entry(3022541210, reward(rewardBonusDrop, 1, 2), reward(rewardFocus, 601, 1), reward("", 600, 1)),
			},
			want: []ActivityRow{{Hash: 3022541210, Name: "Vault of Glass: Master", BonusDrops: 2}},
		},
		"first named focused reward wins": {
			entries: []AvailableActivity{
				entry(4003, reward(rewardFocus, 698, 1), reward(rewardFocus, 606, 1), reward(rewardFocus, 601, 1)),
			},
			want: []ActivityRow{{
				Hash:  4003,
				Name:  "Nightfall: Grandmaster",
				Type:  "Strike",
				Focus: &Loot{Name: "Adept Weapon", Quantity: 1, Hash: 606},
			}},
		},
		"focused reward with a quantity of zero": {
			entries: []AvailableActivity{entry(4003, reward(rewardFocus, 606, 0))},
			want: []ActivityRow{{
				Hash:  4003,
				Name:  "Nightfall: Grandmaster",
				Type:  "Strike",
				Focus: &Loot{Name: "Adept Weapon", Hash: 606},
			}},
		},
		"bonus loot keeps the order and the hash of the first item of a name": {
			entries: []AvailableActivity{
				entry(2222, reward("", 605, 1), reward("", 604, 1), reward("", 600, 3)),
			},
			want: []ActivityRow{{
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
			defs: func(d *Defs) {
				d.Activities[4010] = ActivityDef{Name: "Control"}
				// The lowest hash sorts last by name.
				d.Activities[3999] = ActivityDef{Name: "Duality"}
			},
			entries: []AvailableActivity{
				entry(4444),
				entry(3999),
				entry(4010),
				entry(4009),
			},
			want: []ActivityRow{
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

			defs := fixtureDefs()
			if tc.defs != nil {
				tc.defs(defs)
			}

			assert.Equal(t, tc.want, readActivities(tc.entries, defs))
		})
	}
}
