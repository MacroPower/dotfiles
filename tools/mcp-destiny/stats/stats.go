package stats

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
)

// Change kinds that the stats tool reads. The web client records more.
const (
	ChangeCurrency  = "currency"
	ChangeStack     = "stack"
	ChangeObjective = "objective"
)

// Game modes of the kill trackers. Gambit kills count with PvE.
const (
	killPvP    = "pvp"
	killPvE    = "pve"
	killGambit = "gambit"
)

const (
	// statLookback is how far before the period start the stats tool reads
	// changes. The first change of a period takes its before value from the
	// last reading in that window. It is the LOOKBACK of the Stats page of
	// the web client.
	statLookback = 2 * time.Hour

	// statClockMargin widens the read of chunks for a difference between
	// the Bungie clock, which stamps each change, and the service clock,
	// which stamps each chunk.
	statClockMargin = time.Hour

	// defaultStatPeriod is the period that the stats tool reports when the
	// input names none.
	defaultStatPeriod = "24h"
)

var (
	// ErrInvalidInput reports a period id that names no period of the stats
	// tool.
	ErrInvalidInput = errors.New("invalid input")

	// statPeriods lists the periods of the Stats page of the web client. A
	// zero length covers the whole recorded history.
	statPeriods = []Period{
		{id: defaultStatPeriod, Label: "Last 24 hours", Length: 24 * time.Hour},
		{id: "7d", Label: "Last 7 days", Length: 7 * 24 * time.Hour},
		{id: "30d", Label: "Last 30 days", Length: 30 * 24 * time.Hour},
		{id: "all", Label: "All time"},
	}

	// killTrackers maps the objective hash of each kill tracker to the game
	// mode that it counts. It copies killTrackerObjectivesByHash from
	// src/app/search/d2-known-values.ts of Destiny Item Manager (DIM).
	killTrackers = map[uint32]string{
		1501870536: killPvP,    // "Crucible Opponents Defeated" in 2285636663 "Crucible Tracker"
		2439952408: killPvP,    // "Crucible Opponents Defeated" in 3244015567 "Crucible Tracker"
		74070459:   killPvP,    // "Crucible Opponents Defeated" in 38912240 "Crucible Tracker"
		890482414:  killPvP,    // "Crucible opponents defeated" in 1187045864 "Crucible Memento Tracker"
		90275515:   killPvE,    // "Enemies Defeated" in 2240097604 "Kill Tracker"
		2579044636: killPvE,    // "Enemies Defeated" in 2302094943 "Kill Tracker"
		73837075:   killPvE,    // "Enemies Defeated" in 905869860 "Kill Tracker"
		345540971:  killGambit, // "Gambit targets defeated" in 3915764593 "Gambit Memento Tracker"
		3387796140: killPvE,    // "Nightfall combatants defeated" in 3915764594 "Nightfall Memento Tracker"
		2109364169: killPvP,    // "Trials opponents defeated" in 3915764595 "Trials Memento Tracker"
	}
)

// Period is one period that the stats tool reports.
type Period struct {
	id     string
	Label  string
	Length time.Duration
}

// PeriodOf returns the period with the given id. An empty id selects
// [defaultStatPeriod].
func PeriodOf(id string) (Period, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		id = defaultStatPeriod
	}

	ids := make([]string, 0, len(statPeriods))

	for _, period := range statPeriods {
		if period.id == id {
			return period, nil
		}

		ids = append(ids, period.id)
	}

	return Period{}, fmt.Errorf("%w: period %q is not one of %s", ErrInvalidInput, id, strings.Join(ids, ", "))
}

// Start returns the start of the period that ends at end. The whole
// recorded history starts at the Unix epoch.
func (p Period) Start(end time.Time) time.Time {
	if p.Length == 0 {
		return time.UnixMilli(0).In(end.Location())
	}

	return end.Add(-p.Length)
}

// CursorFor returns the cursor of the first chunk that can hold a
// change of a period that starts at from.
//
// The web client records every change of a chunk before it posts the
// chunk. The service stamps the chunk with the time of the post. A
// chunk that the service stamped before a time therefore holds only
// changes from before that time, when the Bungie clock and the service
// clock agree within [statClockMargin].
func CursorFor(from time.Time) dvm.StatCursor {
	return dvm.StatCursor{UpdatedAt: max(0, from.Add(-statLookback-statClockMargin).UnixMilli())}
}

// Flow is the change of one currency or stack over a period.
type Flow struct {
	Net      int64
	Gained   int64
	Spent    int64
	ItemHash uint32
	Currency bool
}

// WeaponKills counts the kills of one weapon instance over a period.
type WeaponKills struct {
	InstanceID string
	PvE        int64
	PvP        int64
	ItemHash   uint32
}

// Summary holds what the stats tool reports for one period. The
// ReadFrom field holds the service time from which the stats tool read
// chunks. It is the Unix epoch or the zero time when the tool read the
// whole recorded history.
type Summary struct {
	From     time.Time
	To       time.Time
	Newest   time.Time
	ReadFrom time.Time
	Flows    []Flow
	Weapons  []WeaponKills
}

// Summarize counts the changes of chunks whose At falls between from
// and to, inclusive. It restates the changes from [statLookback] before
// from. The first change of the period thus takes its before value from
// the last reading before the period. The Newest field holds the At of the
// newest change of chunks, and it is zero when chunks hold no change.
func Summarize(chunks []dvm.StatChunk, from, to time.Time) Summary {
	lo := from.Add(-statLookback).UnixMilli()
	hi := to.UnixMilli()

	var (
		changes []dvm.StatChange
		newest  int64
		found   bool
	)

	for _, chunk := range chunks {
		for _, change := range chunk.Changes {
			if !found || change.At > newest {
				newest = change.At
				found = true
			}

			if change.At >= lo && change.At <= hi {
				changes = append(changes, change)
			}
		}
	}

	changes = restateReadings(changes)
	start := from.UnixMilli()

	summary := Summary{
		From:    from,
		To:      to,
		Flows:   flowsOf(changes, start),
		Weapons: weaponKillsOf(changes, start),
	}

	if found {
		summary.Newest = time.UnixMilli(newest).In(to.Location())
	}

	return summary
}

// readingKey returns the key of the value that change reads, and false for
// a kind that the stats tool does not restate. The key of a currency or
// stack names its kind and item hash. The key of an objective names its
// instance id and objective hash.
func readingKey(change dvm.StatChange) (string, bool) {
	switch change.Kind {
	case ChangeCurrency, ChangeStack:
		return fmt.Sprintf("%s/%d", change.Kind, change.ItemHash), true
	case ChangeObjective:
		return fmt.Sprintf("%s/%s/%d", change.Kind, change.InstanceID, change.ObjectiveHash), true
	default:
		return "", false
	}
}

// restateReadings returns changes sorted by At, with each Before replaced
// by the previous After of the same value. Two recorders whose baselines
// differ log the same gain twice, and the restatement counts it once. It
// ports restateReadings from statData.ts of the web client.
func restateReadings(changes []dvm.StatChange) []dvm.StatChange {
	out := slices.Clone(changes)
	slices.SortStableFunc(out, func(a, b dvm.StatChange) int { return cmp.Compare(a.At, b.At) })

	last := map[string]int64{}

	for i, change := range out {
		key, ok := readingKey(change)
		if !ok {
			continue
		}

		if before, ok := last[key]; ok {
			out[i].Before = before
		}

		last[key] = change.After
	}

	return out
}

// flowsOf sums the currency and stack changes at or after from per item
// hash. It returns the currencies first and then sorts by the absolute net
// change, largest first. It ports balancesOf from statData.ts of the web
// client.
func flowsOf(changes []dvm.StatChange, from int64) []Flow {
	var flows []Flow

	index := map[uint32]int{}

	for _, change := range changes {
		if (change.Kind != ChangeCurrency && change.Kind != ChangeStack) || change.At < from {
			continue
		}

		i, ok := index[change.ItemHash]
		if !ok {
			i = len(flows)
			index[change.ItemHash] = i
			flows = append(flows, Flow{ItemHash: change.ItemHash, Currency: change.Kind == ChangeCurrency})
		}

		delta := change.After - change.Before
		flows[i].Net += delta

		if delta > 0 {
			flows[i].Gained += delta
		} else {
			flows[i].Spent -= delta
		}
	}

	slices.SortStableFunc(flows, func(a, b Flow) int {
		if a.Currency != b.Currency {
			if a.Currency {
				return -1
			}

			return 1
		}

		return cmp.Compare(abs(b.Net), abs(a.Net))
	})

	return flows
}

// killChange reports whether change raised the count of a kill tracker.
func killChange(change dvm.StatChange) bool {
	_, tracker := killTrackers[change.ObjectiveHash]

	return change.Kind == ChangeObjective && tracker && change.After > change.Before
}

// weaponKillsOf sums the kills at or after from per weapon instance. It
// sorts the weapons by total kills, most first. It ports weaponKillsOf from
// statData.ts of the web client.
func weaponKillsOf(changes []dvm.StatChange, from int64) []WeaponKills {
	var weapons []WeaponKills

	index := map[string]int{}

	for _, change := range changes {
		if !killChange(change) || change.At < from {
			continue
		}

		i, ok := index[change.InstanceID]
		if !ok {
			i = len(weapons)
			index[change.InstanceID] = i
			weapons = append(weapons, WeaponKills{InstanceID: change.InstanceID, ItemHash: change.ItemHash})
		}

		kills := change.After - change.Before

		if killTrackers[change.ObjectiveHash] == killPvP {
			weapons[i].PvP += kills
		} else {
			weapons[i].PvE += kills
		}
	}

	slices.SortStableFunc(weapons, func(a, b WeaponKills) int {
		return cmp.Compare(b.PvE+b.PvP, a.PvE+a.PvP)
	})

	return weapons
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}

	return n
}
