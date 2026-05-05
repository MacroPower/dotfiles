package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/dustin/go-humanize"
)

// statsOutcomeOrder is the display order for outcomes in the summary
// report. Outcomes not in this list are appended in insertion order.
var statsOutcomeOrder = []string{
	OutcomeOK,
	OutcomeDenied,
	OutcomeRobotsDenied,
	OutcomeHTTPError,
	OutcomeFetchError,
	OutcomeInvalidURL,
	OutcomeInternalError,
}

// Summary is the structured view of stats output. The formatter
// renders this; tests assert on its fields directly so they don't
// churn on whitespace tweaks.
type Summary struct {
	Window        Window
	DBPath        string
	Outcomes      []OutcomeRow
	TopHosts      []HostCount
	Recent        []FetchRecord
	Total         int
	ResponseBytes int64
	OutputBytes   int64
	AvgDurationMs float64
	CacheHits     int
	TopLimit      int
}

// Window describes the time bounds of the rows aggregated by Summary.
type Window struct {
	Since    time.Time // zero = "all time"
	Earliest time.Time
	Latest   time.Time
}

// OutcomeRow is one row of the outcome breakdown.
type OutcomeRow struct {
	Outcome string
	Count   int
	Pct     float64
}

func runStats(args []string) int {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)

	dbPath := fs.String("db", "", "path to SQLite store (default: $XDG_STATE_HOME/mcp-fetch/fetches.db)")
	sinceStr := fs.String("since", "", "only count rows newer than this (e.g. 24h, 30m, 7d)")
	last := fs.Int("last", 0, "if >0, append the most recent N rows below the summary")
	top := fs.Int("top", 10, "how many hosts to list under Top hosts")

	err := fs.Parse(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}

		_, _ = fmt.Fprintf(os.Stderr, "%v\n", err)

		return 2
	}

	resolved, err := resolveDBPath(*dbPath)
	if err != nil {
		return bail("%s", err.Error())
	}

	_, statErr := os.Stat(resolved)
	if os.IsNotExist(statErr) {
		return bail("no fetch database at %s", resolved)
	}

	since, err := parseSinceFlag(*sinceStr)
	if err != nil {
		return bailCode(2, "invalid --since: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := OpenStore(ctx, resolved)
	if err != nil {
		return bail("opening store: %v", err)
	}

	defer func() { _ = store.Close() }()

	var sinceTime time.Time
	if since > 0 {
		sinceTime = time.Now().Add(-since)
	}

	stats, err := store.Summary(ctx, sinceTime)
	if err != nil {
		return bail("querying summary: %v", err)
	}

	hosts, err := store.TopHosts(ctx, sinceTime, *top)
	if err != nil {
		return bail("querying top hosts: %v", err)
	}

	var recent []FetchRecord
	if *last > 0 {
		recent, err = store.RecentFetches(ctx, *last)
		if err != nil {
			return bail("querying recent fetches: %v", err)
		}
	}

	summary := buildSummary(resolved, stats, hosts, recent, *top)
	summary.Window.Since = sinceTime

	formatSummary(os.Stdout, summary)

	return 0
}

// bail prints msg to stderr and returns 1.
func bail(format string, a ...any) int {
	return bailCode(1, format, a...)
}

// bailCode prints msg to stderr and returns code.
func bailCode(code int, format string, a ...any) int {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", a...)

	return code
}

// resolveDBPath honors an explicit --db, then $XDG_STATE_HOME, then
// $HOME/.local/state. Returns an error when none of those resolve.
func resolveDBPath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}

	if state := os.Getenv("XDG_STATE_HOME"); state != "" {
		return filepath.Join(state, "mcp-fetch", "fetches.db"), nil
	}

	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, ".local", "state", "mcp-fetch", "fetches.db"), nil
	}

	return "", errors.New(
		"cannot resolve default --db path: $XDG_STATE_HOME and $HOME both unset; pass --db explicitly",
	)
}

// buildSummary turns the raw store output into the display-ready
// [Summary]. No IO, so tests can hit it with fixtures.
func buildSummary(
	dbPath string,
	stats SummaryStats,
	hosts []HostCount,
	recent []FetchRecord,
	topLimit int,
) Summary {
	out := Summary{
		DBPath:        dbPath,
		Window:        Window{Earliest: stats.Earliest, Latest: stats.Latest},
		Total:         stats.Total,
		TopHosts:      hosts,
		ResponseBytes: stats.TotalResponseBytes,
		OutputBytes:   stats.TotalOutputBytes,
		AvgDurationMs: stats.AvgDurationMs,
		CacheHits:     stats.CacheHits,
		Recent:        recent,
		TopLimit:      topLimit,
	}

	out.Outcomes = orderedOutcomes(stats.OutcomeCounts, stats.Total)

	return out
}

func orderedOutcomes(counts map[string]int, total int) []OutcomeRow {
	if total == 0 {
		return nil
	}

	seen := map[string]bool{}
	out := make([]OutcomeRow, 0, len(counts))

	for _, name := range statsOutcomeOrder {
		count, ok := counts[name]
		if !ok {
			continue
		}

		out = append(out, OutcomeRow{
			Outcome: name,
			Count:   count,
			Pct:     percent(count, total),
		})

		seen[name] = true
	}

	// Append any unknown outcomes in alphabetical order so the listing
	// stays deterministic.
	var extras []string

	for name := range counts {
		if !seen[name] {
			extras = append(extras, name)
		}
	}

	sort.Strings(extras)

	for _, name := range extras {
		out = append(out, OutcomeRow{
			Outcome: name,
			Count:   counts[name],
			Pct:     percent(counts[name], total),
		})
	}

	return out
}

func percent(part, total int) float64 {
	if total == 0 {
		return 0
	}

	return float64(part) * 100 / float64(total)
}

// formatSummary writes a human-readable report to w. ASCII only,
// tab-aligned columns. Not intended as machine-readable output;
// tests assert on [Summary] directly.
func formatSummary(w io.Writer, s Summary) {
	_, _ = fmt.Fprintf(w, "DB:      %s\n", s.DBPath)
	_, _ = fmt.Fprintf(w, "Window:  %s\n\n", formatWindow(s.Window, s.Total))

	if s.Total == 0 {
		_, _ = fmt.Fprintln(w, "No records.")

		return
	}

	_, _ = fmt.Fprintln(w, "Outcomes")

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	for _, row := range s.Outcomes {
		_, _ = fmt.Fprintf(tw, "  %-14s\t%5d\t%5.1f%%\t\n", row.Outcome, row.Count, row.Pct)
	}

	_ = tw.Flush()

	if len(s.TopHosts) > 0 {
		_, _ = fmt.Fprintf(w, "\nTop hosts (%d)\n", s.TopLimit)

		tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, h := range s.TopHosts {
			_, _ = fmt.Fprintf(tw, "  %-28s\t%5d\t\n", h.Host, h.Count)
		}

		_ = tw.Flush()
	}

	_, _ = fmt.Fprintln(w, "\nTotals")
	_, _ = fmt.Fprintf(w, "  Bytes fetched   %s (response) / %s (returned)\n",
		humanize.IBytes(uint64(max(s.ResponseBytes, 0))),
		humanize.IBytes(uint64(max(s.OutputBytes, 0))))
	_, _ = fmt.Fprintf(w, "  Avg duration    %.0f ms\n", s.AvgDurationMs)
	_, _ = fmt.Fprintf(w, "  Cache hits      %d / %d (%.1f%%)\n",
		s.CacheHits, s.Total, percent(s.CacheHits, s.Total))

	if len(s.Recent) > 0 {
		_, _ = fmt.Fprintln(w, "\nRecent")

		tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "  TIMESTAMP\tOUTCOME\tSTATUS\tHOST\tURL")

		for i := range s.Recent {
			r := &s.Recent[i]
			_, _ = fmt.Fprintf(tw, "  %s\t%s\t%d\t%s\t%s\n",
				r.Timestamp.Format(time.DateTime),
				r.Outcome, r.StatusCode, r.Host, r.URL)
		}

		_ = tw.Flush()
	}
}

func formatWindow(w Window, total int) string {
	if total == 0 && w.Since.IsZero() {
		return "all time (no records)"
	}

	prefix := "all time"
	if !w.Since.IsZero() {
		prefix = fmt.Sprintf("since %s", w.Since.Format(time.DateTime))
	}

	if total == 0 {
		return fmt.Sprintf("%s (no records)", prefix)
	}

	return fmt.Sprintf("%s (%d records, %s -> %s)",
		prefix, total,
		w.Earliest.Format(time.DateTime),
		w.Latest.Format(time.DateTime))
}
