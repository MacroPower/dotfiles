// Package stats sums the profile changes that the web client of Agg's
// Destiny Vault Manager records. It ports statData.ts of the web client.
//
// The web client records profile changes only while it is open in a
// browser. It diffs each Bungie profile that it fetches against the one
// before. After each 5-minute bucket closes, it posts the changes of the
// bucket to the service as one chunk. The service stamps each chunk with
// the time of the post, and the dvm client reads the chunks back in the
// order of those stamps.
//
// # Summaries
//
// A caller looks up a period by the id that the get_stats tool takes, such
// as 24h or all. The period gives its start for an end time that the
// caller passes, and the whole recorded history starts at the Unix epoch.
// The caller reads the chunks from the cursor of that start and passes
// them to the summary with the start and the end. It also records the
// start of its read in the summary, so the output can name that time when
// the read finds no change.
//
// The cursor reaches back a lookback before the start of the period. The
// first change of the period therefore takes its before value from the
// last reading before the period. The cursor also reaches back a margin
// for a difference between the Bungie clock and the service clock.
//
// Two recorders whose baselines differ log the same gain twice. The
// summary therefore restates the readings first. It replaces the before
// value of each change with the after value of the previous change of the
// same currency, stack, or objective. It then sums the gains and the
// spends of each currency and each stack. For each weapon instance, it
// also sums the PvE kills and the PvP kills that the kill trackers of the
// weapon count. Gambit kills count with PvE.
package stats
