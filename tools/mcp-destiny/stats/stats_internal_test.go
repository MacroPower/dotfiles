package stats

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungietest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
)

// statStart is the period start of the stats unit tests.
var statStart = time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

// minutes returns the Unix milliseconds of statStart plus n minutes.
func minutes(n int) int64 {
	return statStart.Add(time.Duration(n) * time.Minute).UnixMilli()
}

func currency(hash uint32, at, before, after int64) dvm.StatChange {
	return dvm.StatChange{Kind: ChangeCurrency, ItemHash: hash, From: at - 15_000, At: at, Before: before, After: after}
}

func stack(hash uint32, at, before, after int64) dvm.StatChange {
	return dvm.StatChange{Kind: ChangeStack, ItemHash: hash, From: at - 15_000, At: at, Before: before, After: after}
}

func objective(instanceID string, itemHash, objectiveHash uint32, at, before, after int64) dvm.StatChange {
	return dvm.StatChange{
		Kind:          ChangeObjective,
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

	progression := dvm.StatChange{Kind: "progression", At: minutes(5), Before: 10, After: 20}

	tests := map[string]struct {
		changes []dvm.StatChange
		want    []dvm.StatChange
	}{
		"stale recorder": {
			changes: []dvm.StatChange{
				currency(bungietest.ItemGlimmer, minutes(10), 1_000, 2_000),
				currency(bungietest.ItemGlimmer, minutes(20), 2_000, 1_500),
				currency(bungietest.ItemGlimmer, minutes(40), 1_000, 1_700),
			},
			want: []dvm.StatChange{
				currency(bungietest.ItemGlimmer, minutes(10), 1_000, 2_000),
				currency(bungietest.ItemGlimmer, minutes(20), 2_000, 1_500),
				currency(bungietest.ItemGlimmer, minutes(40), 1_500, 1_700),
			},
		},
		"sorted by at": {
			changes: []dvm.StatChange{
				currency(bungietest.ItemGlimmer, minutes(40), 1_000, 1_700),
				currency(bungietest.ItemGlimmer, minutes(10), 1_000, 2_000),
			},
			want: []dvm.StatChange{
				currency(bungietest.ItemGlimmer, minutes(10), 1_000, 2_000),
				currency(bungietest.ItemGlimmer, minutes(40), 2_000, 1_700),
			},
		},
		"currency and stack of one hash are separate values": {
			changes: []dvm.StatChange{
				currency(300, minutes(10), 1, 5),
				stack(300, minutes(20), 40, 42),
			},
			want: []dvm.StatChange{
				currency(300, minutes(10), 1, 5),
				stack(300, minutes(20), 40, 42),
			},
		},
		"objectives are keyed by instance and objective": {
			changes: []dvm.StatChange{
				objective("1001", 100, bungietest.TrackerPvE, minutes(10), 0, 5),
				objective("1002", 100, bungietest.TrackerPvE, minutes(20), 50, 60),
				objective("1001", 100, bungietest.TrackerPvP, minutes(30), 7, 9),
				objective("1001", 100, bungietest.TrackerPvE, minutes(40), 0, 8),
			},
			want: []dvm.StatChange{
				objective("1001", 100, bungietest.TrackerPvE, minutes(10), 0, 5),
				objective("1002", 100, bungietest.TrackerPvE, minutes(20), 50, 60),
				objective("1001", 100, bungietest.TrackerPvP, minutes(30), 7, 9),
				objective("1001", 100, bungietest.TrackerPvE, minutes(40), 5, 8),
			},
		},
		"other kinds pass through": {
			changes: []dvm.StatChange{progression, progression},
			want:    []dvm.StatChange{progression, progression},
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
		changes []dvm.StatChange
		want    []Flow
	}{
		"stale recorder": {
			changes: []dvm.StatChange{
				currency(bungietest.ItemGlimmer, minutes(10), 1_000, 2_000),
				currency(bungietest.ItemGlimmer, minutes(20), 2_000, 1_500),
				currency(bungietest.ItemGlimmer, minutes(40), 1_000, 1_700),
			},
			want: []Flow{
				{ItemHash: bungietest.ItemGlimmer, Currency: true, Net: 700, Gained: 1_200, Spent: 500},
			},
		},
		"currencies first, then by absolute net change": {
			changes: []dvm.StatChange{
				stack(300, minutes(10), 40, 140),
				currency(bungietest.ItemBrightDust, minutes(20), 1_500, 1_490),
				currency(bungietest.ItemGlimmer, minutes(30), 2_000, 1_000),
				stack(301, minutes(40), 10, 5),
			},
			want: []Flow{
				{ItemHash: bungietest.ItemGlimmer, Currency: true, Net: -1_000, Spent: 1_000},
				{ItemHash: bungietest.ItemBrightDust, Currency: true, Net: -10, Spent: 10},
				{ItemHash: 300, Net: 100, Gained: 100},
				{ItemHash: 301, Net: -5, Spent: 5},
			},
		},
		"before the start": {
			changes: []dvm.StatChange{
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
		changes []dvm.StatChange
		want    []WeaponKills
	}{
		"PvP trackers count as PvP": {
			changes: []dvm.StatChange{
				objective("1001", 100, bungietest.TrackerPvP, minutes(10), 7, 9),
				objective("1001", 100, bungietest.TrackerPvE, minutes(20), 100, 110),
			},
			want: []WeaponKills{{InstanceID: "1001", ItemHash: 100, PvP: 2, PvE: 10}},
		},
		"Gambit and Nightfall trackers count as PvE": {
			changes: []dvm.StatChange{
				objective("1001", 100, bungietest.TrackerGambit, minutes(10), 0, 3),
				objective("1001", 100, bungietest.TrackerNightfall, minutes(20), 4, 8),
			},
			want: []WeaponKills{{InstanceID: "1001", ItemHash: 100, PvE: 7}},
		},
		"decreases add no kills": {
			changes: []dvm.StatChange{
				objective("1001", 100, bungietest.TrackerPvE, minutes(10), 100, 0),
				objective("1001", 100, bungietest.TrackerPvE, minutes(20), 0, 4),
			},
			want: []WeaponKills{{InstanceID: "1001", ItemHash: 100, PvE: 4}},
		},
		"objectives that track no kills": {
			changes: []dvm.StatChange{
				objective("1001", 100, 12345, minutes(10), 0, 50),
				currency(bungietest.ItemGlimmer, minutes(20), 0, 50),
			},
		},
		"sorted by total kills": {
			changes: []dvm.StatChange{
				objective("1001", 100, bungietest.TrackerPvE, minutes(10), 0, 5),
				objective("1002", 101, bungietest.TrackerPvP, minutes(20), 0, 9),
				objective("1003", 102, bungietest.TrackerPvE, minutes(30), 0, 3),
				objective("1001", 100, bungietest.TrackerPvP, minutes(40), 0, 2),
			},
			want: []WeaponKills{
				{InstanceID: "1002", ItemHash: 101, PvP: 9},
				{InstanceID: "1001", ItemHash: 100, PvE: 5, PvP: 2},
				{InstanceID: "1003", ItemHash: 102, PvE: 3},
			},
		},
		"before the start": {
			changes: []dvm.StatChange{objective("1001", 100, bungietest.TrackerPvE, minutes(-10), 0, 5)},
		},
		"a kill at the start counts": {
			changes: []dvm.StatChange{
				objective("1001", 100, bungietest.TrackerPvE, minutes(-10), 0, 5),
				objective("1001", 100, bungietest.TrackerPvE, minutes(0), 5, 8),
			},
			want: []WeaponKills{{InstanceID: "1001", ItemHash: 100, PvE: 3}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, weaponKillsOf(restateReadings(tc.changes), minutes(0)))
		})
	}
}

func TestSummarize(t *testing.T) {
	t.Parallel()

	to := statStart.Add(time.Hour)

	tests := map[string]struct {
		chunks  []dvm.StatChunk
		from    time.Time
		want    []Flow
		newest  int64
		weapons []WeaponKills
	}{
		"changes outside the period": {
			chunks: []dvm.StatChunk{
				{StartsAt: 0, Changes: []dvm.StatChange{
					currency(bungietest.ItemGlimmer, minutes(-1), 1_000, 1_100),
					currency(bungietest.ItemGlimmer, minutes(0), 1_100, 1_300),
					objective("1001", 100, bungietest.TrackerPvE, minutes(-1), 0, 4),
				}},
				{StartsAt: 300_000, Changes: []dvm.StatChange{
					currency(bungietest.ItemGlimmer, minutes(60), 1_300, 1_400),
					currency(bungietest.ItemGlimmer, minutes(61), 1_400, 2_000),
					objective("1001", 100, bungietest.TrackerPvE, minutes(61), 4, 9),
				}},
			},
			from:   statStart,
			want:   []Flow{{ItemHash: bungietest.ItemGlimmer, Currency: true, Net: 300, Gained: 300}},
			newest: minutes(61),
		},
		"all keeps every change": {
			chunks: []dvm.StatChunk{
				{StartsAt: 0, Changes: []dvm.StatChange{
					currency(bungietest.ItemGlimmer, 1, 0, 100),
					currency(bungietest.ItemGlimmer, minutes(-10_000), 100, 1_000),
					objective("1001", 100, bungietest.TrackerPvE, minutes(-10_000), 0, 4),
				}},
				{StartsAt: 300_000, Changes: []dvm.StatChange{
					currency(bungietest.ItemGlimmer, minutes(60), 1_000, 1_300),
				}},
			},
			from:    time.UnixMilli(0),
			want:    []Flow{{ItemHash: bungietest.ItemGlimmer, Currency: true, Net: 1_300, Gained: 1_300}},
			weapons: []WeaponKills{{InstanceID: "1001", ItemHash: 100, PvE: 4}},
			newest:  minutes(60),
		},
		"the first change takes its before from a reading before the start": {
			chunks: []dvm.StatChunk{
				{StartsAt: 0, Changes: []dvm.StatChange{
					currency(bungietest.ItemGlimmer, minutes(-90), 500, 900),
					objective("1001", 100, bungietest.TrackerPvE, minutes(-90), 10, 12),
				}},
				{StartsAt: 300_000, Changes: []dvm.StatChange{
					currency(bungietest.ItemGlimmer, minutes(10), 1_000, 2_000),
					objective("1001", 100, bungietest.TrackerPvE, minutes(10), 10, 15),
				}},
			},
			from:    statStart,
			want:    []Flow{{ItemHash: bungietest.ItemGlimmer, Currency: true, Net: 1_100, Gained: 1_100}},
			weapons: []WeaponKills{{InstanceID: "1001", ItemHash: 100, PvE: 3}},
			newest:  minutes(10),
		},
		"readings before the lookback are not restated": {
			chunks: []dvm.StatChunk{
				{StartsAt: 0, Changes: []dvm.StatChange{
					currency(bungietest.ItemGlimmer, minutes(-150), 500, 900),
				}},
				{StartsAt: 300_000, Changes: []dvm.StatChange{
					currency(bungietest.ItemGlimmer, minutes(10), 1_000, 2_000),
				}},
			},
			from:   statStart,
			want:   []Flow{{ItemHash: bungietest.ItemGlimmer, Currency: true, Net: 1_000, Gained: 1_000}},
			newest: minutes(10),
		},
		"no change": {
			from: statStart,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := Summarize(tc.chunks, tc.from, to)

			assert.Equal(t, tc.want, got.Flows)
			assert.Equal(t, tc.weapons, got.Weapons)
			assert.Equal(t, tc.from, got.From)
			assert.Equal(t, to, got.To)

			if tc.newest == 0 {
				assert.True(t, got.Newest.IsZero())
			} else {
				assert.Equal(t, time.UnixMilli(tc.newest).UTC(), got.Newest)
			}
		})
	}
}

func TestSummaryItemHashes(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		summary Summary
		want    []uint32
	}{
		"flows first, each hash one time": {
			summary: Summary{
				Flows: []Flow{{ItemHash: bungietest.ItemGlimmer}, {ItemHash: 78}, {ItemHash: 300}},
				Weapons: []WeaponKills{
					{InstanceID: "1001", ItemHash: 100},
					{InstanceID: "1002", ItemHash: 78},
					{InstanceID: "1003", ItemHash: 100},
				},
			},
			want: []uint32{bungietest.ItemGlimmer, 78, 300, 100},
		},
		"empty summary": {},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, tc.summary.ItemHashes())
		})
	}
}

func TestPeriodOf(t *testing.T) {
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

			got, err := PeriodOf(tc.id)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantID, got.id)
			assert.Equal(t, tc.wantStart, got.Start(end))
		})
	}
}

func TestCursorFor(t *testing.T) {
	t.Parallel()

	assert.Equal(t, dvm.StatCursor{UpdatedAt: statStart.Add(-3 * time.Hour).UnixMilli()}, CursorFor(statStart))
	assert.Equal(t, dvm.StatCursor{}, CursorFor(time.UnixMilli(0)))
}
