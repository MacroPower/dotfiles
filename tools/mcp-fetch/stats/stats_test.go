package stats_test

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/stats"
	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")

	st, err := store.Open(t.Context(), dbPath)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, st.Close()) })

	return st
}

func TestBuild_FromSeededStore(t *testing.T) {
	t.Parallel()

	st := newTestStore(t)
	ctx := t.Context()

	require.NoError(t, st.Record(ctx, store.FetchRecord{
		URL: "https://a.test/", Host: "a.test",
		Outcome: store.OutcomeOK, StatusCode: 200,
		ResponseBytes: 1000, OutputBytes: 500, DurationMs: 100,
	}))
	require.NoError(t, st.Record(ctx, store.FetchRecord{
		URL: "https://a.test/2", Host: "a.test",
		Outcome: store.OutcomeOK, StatusCode: 200, CacheHit: 1,
		ResponseBytes: 0, OutputBytes: 500,
	}))
	require.NoError(t, st.Record(ctx, store.FetchRecord{
		URL: "https://b.test/", Host: "b.test",
		Outcome: store.OutcomeDenied, Error: "blocked",
	}))

	summaryStats, err := st.Summary(ctx, time.Time{})
	require.NoError(t, err)

	hosts, err := st.TopHosts(ctx, time.Time{}, 10)
	require.NoError(t, err)

	recent, err := st.RecentFetches(ctx, 5)
	require.NoError(t, err)

	got := stats.Build("/path/to.db", summaryStats, hosts, recent, 10)

	assert.Equal(t, "/path/to.db", got.DBPath)
	assert.Equal(t, 3, got.Total)
	assert.Equal(t, int64(1000), got.ResponseBytes)
	assert.Equal(t, int64(1000), got.OutputBytes)
	assert.Equal(t, 1, got.CacheHits)
	assert.Equal(t, 10, got.TopLimit)

	require.Len(t, got.Outcomes, 2)
	assert.Equal(t, store.OutcomeOK, got.Outcomes[0].Outcome)
	assert.Equal(t, 2, got.Outcomes[0].Count)
	assert.InDelta(t, 66.67, got.Outcomes[0].Pct, 0.01)
	assert.Equal(t, store.OutcomeDenied, got.Outcomes[1].Outcome)

	require.Len(t, got.TopHosts, 2)
	assert.Equal(t, "a.test", got.TopHosts[0].Host)
	assert.Equal(t, 2, got.TopHosts[0].Count)

	require.Len(t, got.Recent, 3)
}

func TestBuild_Empty(t *testing.T) {
	t.Parallel()

	got := stats.Build("/x.db", store.SummaryStats{OutcomeCounts: map[string]int{}}, nil, nil, 10)

	assert.Equal(t, 0, got.Total)
	assert.Empty(t, got.Outcomes)
	assert.Empty(t, got.TopHosts)
	assert.Empty(t, got.Recent)
}

func TestBuild_OutcomeOrder(t *testing.T) {
	t.Parallel()

	counts := map[string]int{
		store.OutcomeDenied:    2,
		store.OutcomeOK:        5,
		store.OutcomeHTTPError: 1,
	}

	got := stats.Build("/x.db", store.SummaryStats{Total: 8, OutcomeCounts: counts}, nil, nil, 10)

	require.Len(t, got.Outcomes, 3)
	// The display order is fixed: ok > denied > robots_denied > http_error.
	assert.Equal(t, store.OutcomeOK, got.Outcomes[0].Outcome)
	assert.Equal(t, store.OutcomeDenied, got.Outcomes[1].Outcome)
	assert.Equal(t, store.OutcomeHTTPError, got.Outcomes[2].Outcome)
}

func TestBuild_UnknownOutcomeAppendedAlpha(t *testing.T) {
	t.Parallel()

	counts := map[string]int{
		store.OutcomeOK: 1,
		"zebra":         1,
		"apple":         1,
	}

	got := stats.Build("/x.db", store.SummaryStats{Total: 3, OutcomeCounts: counts}, nil, nil, 10)

	require.Len(t, got.Outcomes, 3)
	assert.Equal(t, store.OutcomeOK, got.Outcomes[0].Outcome)
	assert.Equal(t, "apple", got.Outcomes[1].Outcome)
	assert.Equal(t, "zebra", got.Outcomes[2].Outcome)
}

func TestFormat_KeySectionsPresent(t *testing.T) {
	t.Parallel()

	now := time.Now()
	s := stats.Summary{
		DBPath: "/x.db",
		Window: stats.Window{
			Earliest: now.Add(-2 * time.Hour),
			Latest:   now,
		},
		Total: 10,
		Outcomes: []stats.OutcomeRow{
			{Outcome: store.OutcomeOK, Count: 8, Pct: 80.0},
			{Outcome: store.OutcomeDenied, Count: 2, Pct: 20.0},
		},
		TopHosts: []store.HostCount{
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

	stats.Format(&buf, s)

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

func TestFormat_RecentTable(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 5, 5, 10, 23, 45, 0, time.UTC)

	s := stats.Summary{
		DBPath: "/x.db",
		Window: stats.Window{Earliest: ts, Latest: ts},
		Total:  1,
		Outcomes: []stats.OutcomeRow{
			{Outcome: store.OutcomeOK, Count: 1, Pct: 100},
		},
		Recent: []store.FetchRecord{
			{
				Timestamp:  ts,
				URL:        "https://example.com/foo",
				Host:       "example.com",
				Outcome:    store.OutcomeOK,
				StatusCode: 200,
			},
		},
		TopLimit: 10,
	}

	var buf bytes.Buffer

	stats.Format(&buf, s)

	out := buf.String()

	assert.Contains(t, out, "Recent")
	assert.Contains(t, out, "TIMESTAMP")
	assert.Contains(t, out, "https://example.com/foo")
	assert.Contains(t, out, "2026-05-05 10:23:45")
	assert.Contains(t, out, "200")
}

func TestFormat_Empty(t *testing.T) {
	t.Parallel()

	s := stats.Summary{DBPath: "/x.db"}

	var buf bytes.Buffer

	stats.Format(&buf, s)

	out := buf.String()

	assert.Contains(t, out, "No records.")
	assert.NotContains(t, out, "Outcomes")
}

func TestResolveDBPath_Explicit(t *testing.T) {
	t.Parallel()

	got, err := stats.ResolveDBPath("/explicit/path.db")
	require.NoError(t, err)
	assert.Equal(t, "/explicit/path.db", got)
}

func TestResolveDBPath_XDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/state")
	t.Setenv("HOME", "/home/u")

	got, err := stats.ResolveDBPath("")
	require.NoError(t, err)
	assert.Equal(t, "/state/mcp-fetch/fetches.db", got)
}

func TestResolveDBPath_HomeFallback(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/u")

	got, err := stats.ResolveDBPath("")
	require.NoError(t, err)
	assert.Equal(t, "/home/u/.local/state/mcp-fetch/fetches.db", got)
}

func TestResolveDBPath_BothUnset(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")

	_, err := stats.ResolveDBPath("")
	require.Error(t, err)
}

func TestParseSince(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  time.Duration
		err   bool
	}{
		"empty all time":   {input: "", want: 0},
		"hours":            {input: "24h", want: 24 * time.Hour},
		"minutes":          {input: "30m", want: 30 * time.Minute},
		"integer days":     {input: "7d", want: 168 * time.Hour},
		"single day":       {input: "1d", want: 24 * time.Hour},
		"composite no day": {input: "7d12h", err: true},
		"fractional day":   {input: "1.5d", err: true},
		"capital D":        {input: "7D", err: true},
		"unparseable":      {input: "abc", err: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := stats.ParseSince(tt.input)
			if tt.err {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
