package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSummary_FromSeededStore(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	require.NoError(t, store.Record(ctx, FetchRecord{
		URL: "https://a.test/", Host: "a.test",
		Outcome: OutcomeOK, StatusCode: 200,
		ResponseBytes: 1000, OutputBytes: 500, DurationMs: 100,
	}))
	require.NoError(t, store.Record(ctx, FetchRecord{
		URL: "https://a.test/2", Host: "a.test",
		Outcome: OutcomeOK, StatusCode: 200, CacheHit: 1,
		ResponseBytes: 0, OutputBytes: 500,
	}))
	require.NoError(t, store.Record(ctx, FetchRecord{
		URL: "https://b.test/", Host: "b.test",
		Outcome: OutcomeDenied, Error: "blocked",
	}))

	stats, err := store.Summary(ctx, time.Time{})
	require.NoError(t, err)

	hosts, err := store.TopHosts(ctx, time.Time{}, 10)
	require.NoError(t, err)

	recent, err := store.RecentFetches(ctx, 5)
	require.NoError(t, err)

	got := buildSummary("/path/to.db", stats, hosts, recent, 10)

	assert.Equal(t, "/path/to.db", got.DBPath)
	assert.Equal(t, 3, got.Total)
	assert.Equal(t, int64(1000), got.ResponseBytes)
	assert.Equal(t, int64(1000), got.OutputBytes)
	assert.Equal(t, 1, got.CacheHits)
	assert.Equal(t, 10, got.TopLimit)

	require.Len(t, got.Outcomes, 2)
	assert.Equal(t, OutcomeOK, got.Outcomes[0].Outcome)
	assert.Equal(t, 2, got.Outcomes[0].Count)
	assert.InDelta(t, 66.67, got.Outcomes[0].Pct, 0.01)
	assert.Equal(t, OutcomeDenied, got.Outcomes[1].Outcome)

	require.Len(t, got.TopHosts, 2)
	assert.Equal(t, "a.test", got.TopHosts[0].Host)
	assert.Equal(t, 2, got.TopHosts[0].Count)

	require.Len(t, got.Recent, 3)
}

func TestBuildSummary_Empty(t *testing.T) {
	t.Parallel()

	got := buildSummary("/x.db", SummaryStats{OutcomeCounts: map[string]int{}}, nil, nil, 10)

	assert.Equal(t, 0, got.Total)
	assert.Empty(t, got.Outcomes)
	assert.Empty(t, got.TopHosts)
	assert.Empty(t, got.Recent)
}

func TestOrderedOutcomes_Order(t *testing.T) {
	t.Parallel()

	counts := map[string]int{
		OutcomeDenied:    2,
		OutcomeOK:        5,
		OutcomeHTTPError: 1,
	}

	got := orderedOutcomes(counts, 8)

	require.Len(t, got, 3)
	// The display order is fixed: ok > denied > robots_denied > http_error.
	assert.Equal(t, OutcomeOK, got[0].Outcome)
	assert.Equal(t, OutcomeDenied, got[1].Outcome)
	assert.Equal(t, OutcomeHTTPError, got[2].Outcome)
}

func TestOrderedOutcomes_UnknownAppendedAlpha(t *testing.T) {
	t.Parallel()

	counts := map[string]int{
		OutcomeOK: 1,
		"zebra":   1,
		"apple":   1,
	}

	got := orderedOutcomes(counts, 3)

	require.Len(t, got, 3)
	assert.Equal(t, OutcomeOK, got[0].Outcome)
	assert.Equal(t, "apple", got[1].Outcome)
	assert.Equal(t, "zebra", got[2].Outcome)
}

func TestFormatSummary_KeySectionsPresent(t *testing.T) {
	t.Parallel()

	now := time.Now()
	s := Summary{
		DBPath: "/x.db",
		Window: Window{
			Earliest: now.Add(-2 * time.Hour),
			Latest:   now,
		},
		Total: 10,
		Outcomes: []OutcomeRow{
			{Outcome: OutcomeOK, Count: 8, Pct: 80.0},
			{Outcome: OutcomeDenied, Count: 2, Pct: 20.0},
		},
		TopHosts: []HostCount{
			{Host: "example.com", Count: 6},
			{Host: "github.com", Count: 4},
		},
		ResponseBytes: 5 * 1024 * 1024,
		OutputBytes:   1 * 1024 * 1024,
		AvgDurationMs: 312.7,
		CacheHits:     3,
		TopLimit:      10,
	}

	var buf bytes.Buffer

	formatSummary(&buf, s)

	out := buf.String()

	// Section headers.
	assert.Contains(t, out, "DB:      /x.db")
	assert.Contains(t, out, "Window:  all time")
	assert.Contains(t, out, "Outcomes")
	assert.Contains(t, out, "Top hosts (10)")
	assert.Contains(t, out, "Totals")

	// Outcome rows.
	assert.Contains(t, out, "ok")
	assert.Contains(t, out, "8")
	assert.Contains(t, out, "80.0%")
	assert.Contains(t, out, "denied")

	// Top hosts.
	assert.Contains(t, out, "example.com")
	assert.Contains(t, out, "github.com")

	// Totals.
	assert.Contains(t, out, "5.0 MiB")
	assert.Contains(t, out, "1.0 MiB")
	assert.Contains(t, out, "313 ms")
	assert.Contains(t, out, "3 / 10")

	// No "Recent" section when empty.
	assert.NotContains(t, out, "Recent")
}

func TestFormatSummary_RecentTable(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 5, 5, 10, 23, 45, 0, time.UTC)

	s := Summary{
		DBPath: "/x.db",
		Window: Window{Earliest: ts, Latest: ts},
		Total:  1,
		Outcomes: []OutcomeRow{
			{Outcome: OutcomeOK, Count: 1, Pct: 100},
		},
		Recent: []FetchRecord{
			{
				Timestamp:  ts,
				URL:        "https://example.com/foo",
				Host:       "example.com",
				Outcome:    OutcomeOK,
				StatusCode: 200,
			},
		},
		TopLimit: 10,
	}

	var buf bytes.Buffer

	formatSummary(&buf, s)

	out := buf.String()

	assert.Contains(t, out, "Recent")
	assert.Contains(t, out, "TIMESTAMP")
	assert.Contains(t, out, "https://example.com/foo")
	assert.Contains(t, out, "2026-05-05 10:23:45")
	assert.Contains(t, out, "200")
}

func TestFormatSummary_Empty(t *testing.T) {
	t.Parallel()

	s := Summary{DBPath: "/x.db"}

	var buf bytes.Buffer

	formatSummary(&buf, s)

	out := buf.String()

	assert.Contains(t, out, "No records.")
	assert.NotContains(t, out, "Outcomes")
}

func TestResolveDBPath_Explicit(t *testing.T) {
	t.Parallel()

	got, err := resolveDBPath("/explicit/path.db")
	require.NoError(t, err)
	assert.Equal(t, "/explicit/path.db", got)
}

func TestResolveDBPath_XDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/state")
	t.Setenv("HOME", "/home/u")

	got, err := resolveDBPath("")
	require.NoError(t, err)
	assert.Equal(t, "/state/mcp-fetch/fetches.db", got)
}

func TestResolveDBPath_HomeFallback(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/u")

	got, err := resolveDBPath("")
	require.NoError(t, err)
	assert.Equal(t, "/home/u/.local/state/mcp-fetch/fetches.db", got)
}

func TestResolveDBPath_BothUnset(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")

	_, err := resolveDBPath("")
	require.Error(t, err)
}
