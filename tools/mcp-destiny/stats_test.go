package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungietest"
)

// statStart is the period start of the stats unit tests.
var statStart = time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

// testChunk is one chunk of an /api/stats page that a test serves.
type testChunk struct {
	changes   []map[string]any
	startsAt  int64
	updatedAt int64
}

// statPageJSON encodes an /api/stats page. Each chunk body is a JSON
// string that holds the JSON array of its changes.
func statPageJSON(t *testing.T, more bool, chunks ...testChunk) string {
	t.Helper()

	out := []map[string]any{}

	for _, chunk := range chunks {
		body, err := json.Marshal(chunk.changes)
		require.NoError(t, err)

		out = append(out, map[string]any{
			"startsAt":  chunk.startsAt,
			"endsAt":    chunk.startsAt + 300_000,
			"body":      string(body),
			"updatedAt": chunk.updatedAt,
		})
	}

	raw, err := json.Marshal(map[string]any{"chunks": out, "more": more})
	require.NoError(t, err)

	return string(raw)
}

// minutes returns the Unix milliseconds of statStart plus n minutes.
func minutes(n int) int64 {
	return statStart.Add(time.Duration(n) * time.Minute).UnixMilli()
}

func currency(hash uint32, at, before, after int64) StatChange {
	return StatChange{Kind: changeCurrency, ItemHash: hash, From: at - 15_000, At: at, Before: before, After: after}
}

func stack(hash uint32, at, before, after int64) StatChange {
	return StatChange{Kind: changeStack, ItemHash: hash, From: at - 15_000, At: at, Before: before, After: after}
}

func objective(instanceID string, itemHash, objectiveHash uint32, at, before, after int64) StatChange {
	return StatChange{
		Kind:          changeObjective,
		InstanceID:    instanceID,
		ItemHash:      itemHash,
		ObjectiveHash: objectiveHash,
		From:          at - 15_000,
		At:            at,
		Before:        before,
		After:         after,
	}
}

func TestRestateReadings(t *testing.T) {
	t.Parallel()

	progression := StatChange{Kind: "progression", At: minutes(5), Before: 10, After: 20}

	tests := map[string]struct {
		changes []StatChange
		want    []StatChange
	}{
		"stale recorder": {
			changes: []StatChange{
				currency(bungietest.ItemGlimmer, minutes(10), 1_000, 2_000),
				currency(bungietest.ItemGlimmer, minutes(20), 2_000, 1_500),
				currency(bungietest.ItemGlimmer, minutes(40), 1_000, 1_700),
			},
			want: []StatChange{
				currency(bungietest.ItemGlimmer, minutes(10), 1_000, 2_000),
				currency(bungietest.ItemGlimmer, minutes(20), 2_000, 1_500),
				currency(bungietest.ItemGlimmer, minutes(40), 1_500, 1_700),
			},
		},
		"sorted by at": {
			changes: []StatChange{
				currency(bungietest.ItemGlimmer, minutes(40), 1_000, 1_700),
				currency(bungietest.ItemGlimmer, minutes(10), 1_000, 2_000),
			},
			want: []StatChange{
				currency(bungietest.ItemGlimmer, minutes(10), 1_000, 2_000),
				currency(bungietest.ItemGlimmer, minutes(40), 2_000, 1_700),
			},
		},
		"currency and stack of one hash are separate values": {
			changes: []StatChange{
				currency(300, minutes(10), 1, 5),
				stack(300, minutes(20), 40, 42),
			},
			want: []StatChange{
				currency(300, minutes(10), 1, 5),
				stack(300, minutes(20), 40, 42),
			},
		},
		"objectives are keyed by instance and objective": {
			changes: []StatChange{
				objective("1001", 100, bungietest.TrackerPvE, minutes(10), 0, 5),
				objective("1002", 100, bungietest.TrackerPvE, minutes(20), 50, 60),
				objective("1001", 100, bungietest.TrackerPvP, minutes(30), 7, 9),
				objective("1001", 100, bungietest.TrackerPvE, minutes(40), 0, 8),
			},
			want: []StatChange{
				objective("1001", 100, bungietest.TrackerPvE, minutes(10), 0, 5),
				objective("1002", 100, bungietest.TrackerPvE, minutes(20), 50, 60),
				objective("1001", 100, bungietest.TrackerPvP, minutes(30), 7, 9),
				objective("1001", 100, bungietest.TrackerPvE, minutes(40), 5, 8),
			},
		},
		"other kinds pass through": {
			changes: []StatChange{progression, progression},
			want:    []StatChange{progression, progression},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, restateReadings(tc.changes))
		})
	}
}

func TestFlowsOf(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		changes []StatChange
		want    []statFlow
	}{
		"stale recorder": {
			changes: []StatChange{
				currency(bungietest.ItemGlimmer, minutes(10), 1_000, 2_000),
				currency(bungietest.ItemGlimmer, minutes(20), 2_000, 1_500),
				currency(bungietest.ItemGlimmer, minutes(40), 1_000, 1_700),
			},
			want: []statFlow{
				{itemHash: bungietest.ItemGlimmer, currency: true, net: 700, gained: 1_200, spent: 500},
			},
		},
		"currencies first, then by absolute net change": {
			changes: []StatChange{
				stack(300, minutes(10), 40, 140),
				currency(bungietest.ItemBrightDust, minutes(20), 1_500, 1_490),
				currency(bungietest.ItemGlimmer, minutes(30), 2_000, 1_000),
				stack(301, minutes(40), 10, 5),
			},
			want: []statFlow{
				{itemHash: bungietest.ItemGlimmer, currency: true, net: -1_000, spent: 1_000},
				{itemHash: bungietest.ItemBrightDust, currency: true, net: -10, spent: 10},
				{itemHash: 300, net: 100, gained: 100},
				{itemHash: 301, net: -5, spent: 5},
			},
		},
		"before the start": {
			changes: []StatChange{
				currency(bungietest.ItemGlimmer, minutes(-10), 1_000, 2_000),
				objective("1001", 100, bungietest.TrackerPvE, minutes(10), 0, 5),
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, flowsOf(restateReadings(tc.changes), minutes(0)))
		})
	}
}

func TestWeaponKillsOf(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		changes []StatChange
		want    []weaponKills
	}{
		"PvP trackers count as PvP": {
			changes: []StatChange{
				objective("1001", 100, bungietest.TrackerPvP, minutes(10), 7, 9),
				objective("1001", 100, bungietest.TrackerPvE, minutes(20), 100, 110),
			},
			want: []weaponKills{{instanceID: "1001", itemHash: 100, pvp: 2, pve: 10}},
		},
		"Gambit and Nightfall trackers count as PvE": {
			changes: []StatChange{
				objective("1001", 100, bungietest.TrackerGambit, minutes(10), 0, 3),
				objective("1001", 100, bungietest.TrackerNightfall, minutes(20), 4, 8),
			},
			want: []weaponKills{{instanceID: "1001", itemHash: 100, pve: 7}},
		},
		"decreases add no kills": {
			changes: []StatChange{
				objective("1001", 100, bungietest.TrackerPvE, minutes(10), 100, 0),
				objective("1001", 100, bungietest.TrackerPvE, minutes(20), 0, 4),
			},
			want: []weaponKills{{instanceID: "1001", itemHash: 100, pve: 4}},
		},
		"objectives that track no kills": {
			changes: []StatChange{
				objective("1001", 100, 12345, minutes(10), 0, 50),
				currency(bungietest.ItemGlimmer, minutes(20), 0, 50),
			},
		},
		"sorted by total kills": {
			changes: []StatChange{
				objective("1001", 100, bungietest.TrackerPvE, minutes(10), 0, 5),
				objective("1002", 101, bungietest.TrackerPvP, minutes(20), 0, 9),
				objective("1003", 102, bungietest.TrackerPvE, minutes(30), 0, 3),
				objective("1001", 100, bungietest.TrackerPvP, minutes(40), 0, 2),
			},
			want: []weaponKills{
				{instanceID: "1002", itemHash: 101, pvp: 9},
				{instanceID: "1001", itemHash: 100, pve: 5, pvp: 2},
				{instanceID: "1003", itemHash: 102, pve: 3},
			},
		},
		"before the start": {
			changes: []StatChange{objective("1001", 100, bungietest.TrackerPvE, minutes(-10), 0, 5)},
		},
		"a kill at the start counts": {
			changes: []StatChange{
				objective("1001", 100, bungietest.TrackerPvE, minutes(-10), 0, 5),
				objective("1001", 100, bungietest.TrackerPvE, minutes(0), 5, 8),
			},
			want: []weaponKills{{instanceID: "1001", itemHash: 100, pve: 3}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, weaponKillsOf(restateReadings(tc.changes), minutes(0)))
		})
	}
}

func TestSummarizeStats(t *testing.T) {
	t.Parallel()

	to := statStart.Add(time.Hour)

	tests := map[string]struct {
		chunks  []StatChunk
		from    time.Time
		want    []statFlow
		newest  int64
		weapons []weaponKills
	}{
		"changes outside the period": {
			chunks: []StatChunk{
				{StartsAt: 0, Changes: []StatChange{
					currency(bungietest.ItemGlimmer, minutes(-1), 1_000, 1_100),
					currency(bungietest.ItemGlimmer, minutes(0), 1_100, 1_300),
					objective("1001", 100, bungietest.TrackerPvE, minutes(-1), 0, 4),
				}},
				{StartsAt: 300_000, Changes: []StatChange{
					currency(bungietest.ItemGlimmer, minutes(60), 1_300, 1_400),
					currency(bungietest.ItemGlimmer, minutes(61), 1_400, 2_000),
					objective("1001", 100, bungietest.TrackerPvE, minutes(61), 4, 9),
				}},
			},
			from:   statStart,
			want:   []statFlow{{itemHash: bungietest.ItemGlimmer, currency: true, net: 300, gained: 300}},
			newest: minutes(61),
		},
		"all keeps every change": {
			chunks: []StatChunk{
				{StartsAt: 0, Changes: []StatChange{
					currency(bungietest.ItemGlimmer, 1, 0, 100),
					currency(bungietest.ItemGlimmer, minutes(-10_000), 100, 1_000),
					objective("1001", 100, bungietest.TrackerPvE, minutes(-10_000), 0, 4),
				}},
				{StartsAt: 300_000, Changes: []StatChange{
					currency(bungietest.ItemGlimmer, minutes(60), 1_000, 1_300),
				}},
			},
			from:    time.UnixMilli(0),
			want:    []statFlow{{itemHash: bungietest.ItemGlimmer, currency: true, net: 1_300, gained: 1_300}},
			weapons: []weaponKills{{instanceID: "1001", itemHash: 100, pve: 4}},
			newest:  minutes(60),
		},
		"the first change takes its before from a reading before the start": {
			chunks: []StatChunk{
				{StartsAt: 0, Changes: []StatChange{
					currency(bungietest.ItemGlimmer, minutes(-90), 500, 900),
					objective("1001", 100, bungietest.TrackerPvE, minutes(-90), 10, 12),
				}},
				{StartsAt: 300_000, Changes: []StatChange{
					currency(bungietest.ItemGlimmer, minutes(10), 1_000, 2_000),
					objective("1001", 100, bungietest.TrackerPvE, minutes(10), 10, 15),
				}},
			},
			from:    statStart,
			want:    []statFlow{{itemHash: bungietest.ItemGlimmer, currency: true, net: 1_100, gained: 1_100}},
			weapons: []weaponKills{{instanceID: "1001", itemHash: 100, pve: 3}},
			newest:  minutes(10),
		},
		"readings before the lookback are not restated": {
			chunks: []StatChunk{
				{StartsAt: 0, Changes: []StatChange{
					currency(bungietest.ItemGlimmer, minutes(-150), 500, 900),
				}},
				{StartsAt: 300_000, Changes: []StatChange{
					currency(bungietest.ItemGlimmer, minutes(10), 1_000, 2_000),
				}},
			},
			from:   statStart,
			want:   []statFlow{{itemHash: bungietest.ItemGlimmer, currency: true, net: 1_000, gained: 1_000}},
			newest: minutes(10),
		},
		"no change": {
			from: statStart,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := summarizeStats(tc.chunks, tc.from, to)

			assert.Equal(t, tc.want, got.flows)
			assert.Equal(t, tc.weapons, got.weapons)
			assert.Equal(t, tc.from, got.from)
			assert.Equal(t, to, got.to)

			if tc.newest == 0 {
				assert.True(t, got.newest.IsZero())
			} else {
				assert.Equal(t, time.UnixMilli(tc.newest).UTC(), got.newest)
			}
		})
	}
}

func TestStatPeriodOf(t *testing.T) {
	t.Parallel()

	end := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

	tests := map[string]struct {
		err       error
		id        string
		wantID    string
		wantStart time.Time
	}{
		"default": {
			wantID:    "24h",
			wantStart: end.Add(-24 * time.Hour),
		},
		"7d in any letter case": {
			id:        " 7D ",
			wantID:    "7d",
			wantStart: end.Add(-7 * 24 * time.Hour),
		},
		"30d": {
			id:        "30d",
			wantID:    "30d",
			wantStart: end.Add(-30 * 24 * time.Hour),
		},
		"all": {
			id:        "all",
			wantID:    "all",
			wantStart: time.UnixMilli(0).UTC(),
		},
		"unknown": {
			id:  "1y",
			err: ErrInvalidInput,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := statPeriodOf(tc.id)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantID, got.id)
			assert.Equal(t, tc.wantStart, got.start(end))
		})
	}
}

func TestStatCursorFor(t *testing.T) {
	t.Parallel()

	assert.Equal(t, StatCursor{UpdatedAt: statStart.Add(-3 * time.Hour).UnixMilli()}, statCursorFor(statStart))
	assert.Equal(t, StatCursor{}, statCursorFor(time.UnixMilli(0)))
}
