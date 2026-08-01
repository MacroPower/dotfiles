package sleepguard

import (
	"encoding/json"
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"mvdan.cc/sh/v3/syntax"
)

// DefaultMaxSeconds is the ceiling applied when [Config.MaxSeconds]
// arrives non-positive.
const DefaultMaxSeconds = 10

// callSrcMaxBytes bounds how much of the offending call is quoted back
// in the deny reason.
const callSrcMaxBytes = 60

// suffixes maps a GNU sleep duration suffix to its multiplier in
// seconds.
var suffixes = map[byte]float64{
	's': 1,
	'm': 60,
	'h': 3600,
	'd': 86400,
}

// trivial names commands that do no real work on their own, so their
// presence does not stop a command from reading as pure time-passing.
// `echo` and `printf` only narrate, `true`/`:` are no-ops, and
// `jobs`/`wait` inspect a job table that is empty in a fresh tool-call
// shell.
var trivial = map[string]bool{
	"echo":   true,
	"printf": true,
	"true":   true,
	":":      true,
	"jobs":   true,
	"wait":   true,
}

// Config declares the foreground-sleep guard knobs. JSON tags are
// camelCase because builtins.toJSON in home/claude.nix emits attribute
// names verbatim and the Go struct tags must match.
//
// Enable lives inside the JSON rather than gating the flag in the
// wrapper because MaxSeconds cannot double as a disabled sentinel (its
// zero value is ambiguous between "unset" and "deny everything"), so a
// disabled guard needs its own bit. The zero value is a disabled guard.
type Config struct {
	Enable     bool    `json:"enable"`
	MaxSeconds float64 `json:"maxSeconds"`
}

// ceiling returns the effective maximum, substituting
// [DefaultMaxSeconds] when MaxSeconds is non-positive, so a Config
// built with only Enable set gets the default ceiling instead of
// denying every sleep.
func (c Config) ceiling() float64 {
	if c.MaxSeconds <= 0 {
		return DefaultMaxSeconds
	}

	return c.MaxSeconds
}

// Parse decodes the JSON object passed via --sleep-guard-config into a
// [Config]. Empty input yields a zero config (guard disabled);
// malformed JSON returns an error so wrapper misconfiguration is loud.
// A non-positive maxSeconds is left as decoded; [DefaultMaxSeconds] is
// substituted at check time.
func Parse(s string) (Config, error) {
	if s == "" {
		return Config{}, nil
	}

	var cfg Config

	err := json.Unmarshal([]byte(s), &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("decoding sleep guard config JSON: %w", err)
	}

	return cfg, nil
}

// Check walks prog and returns a deny reason for the first foreground
// sleep call whose total duration exceeds the ceiling or cannot be
// read, or -- when every sleep is under the ceiling -- for a command
// that is a filler wait: sleeps with nothing but [trivial] no-ops
// around them, passing time in the foreground at any duration. command
// is the original command text, used to quote the offending call back
// in the reason. A background call is never checked and returns
// ("", false), as does a disabled config.
func Check(prog *syntax.File, command string, background bool, cfg Config) (string, bool) {
	if !cfg.Enable || background {
		return "", false
	}

	limit := cfg.ceiling()

	var deny string

	syntax.Walk(prog, func(node syntax.Node) bool {
		// Returning false only prunes the current subtree, not the
		// whole walk, so later siblings still visit; this guard keeps
		// the first offending call's reason.
		if deny != "" {
			return false
		}

		call, ok := node.(*syntax.CallExpr)
		if !ok {
			return true
		}

		if !isSleep(call) {
			return true
		}

		secs, readable := total(call)
		if readable && secs <= limit {
			return true
		}

		deny = reason(callSrc(command, call), secs, readable, limit)

		return false
	})

	if deny == "" {
		if call, ok := fillerSleep(prog); ok {
			deny = fillerReason(callSrc(command, call))
		}
	}

	return deny, deny != ""
}

// fillerSleep reports whether prog is a filler wait: at least one
// foreground sleep that actually passes time, with no command doing
// real work around it. Commands in [trivial] do not count as real work,
// so `sleep 6`, `sleep 1; echo waiting`, and `while true; do sleep 5;
// done` all read as filler, while a settle inside real work (`kill
// "$pid"; sleep 1; pgrep -f server`) does not. Sleeps that pass no time
// (`sleep 0`, `sleep --help`) are ignored, and unreadable durations are
// not considered here because the duration walk already denies them.
func fillerSleep(prog *syntax.File) (*syntax.CallExpr, bool) {
	var first *syntax.CallExpr

	substantive := false

	syntax.Walk(prog, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok {
			return true
		}

		// Assignment-only statements (`X=5`) carry no command word and
		// do no work worth counting either way.
		if len(call.Args) == 0 {
			return true
		}

		name, ok := literalWord(call.Args[0])
		if !ok {
			substantive = true

			return true
		}

		base := path.Base(name)

		switch {
		case base == "sleep":
			secs, readable := total(call)
			if readable && secs > 0 && first == nil {
				first = call
			}
		case trivial[base]:
		default:
			substantive = true
		}

		return true
	})

	if first == nil || substantive {
		return nil, false
	}

	return first, true
}

// isSleep reports whether call's command word statically resolves to a
// name for sleep, either bare or by path (/bin/sleep). Resolving via
// [literalWord] rather than requiring an unquoted literal means quoted
// forms like `'sleep' 300` are caught too, which widens the guard
// rather than weakening it.
func isSleep(call *syntax.CallExpr) bool {
	if len(call.Args) == 0 {
		return false
	}

	name, ok := literalWord(call.Args[0])
	if !ok {
		return false
	}

	return path.Base(name) == "sleep"
}

// total sums call's duration operands per GNU sleep semantics. sleep's
// only flags (--help, --version) are skipped; negative values clamp to
// zero. Any operand that is not a literal or does not parse as an
// interval makes the whole call unreadable.
func total(call *syntax.CallExpr) (secs float64, readable bool) {
	for _, arg := range call.Args[1:] {
		lit, ok := literalWord(arg)
		if !ok {
			return 0, false
		}

		if lit == "--help" || lit == "--version" {
			continue
		}

		v, ok := parseInterval(lit)
		if !ok {
			return 0, false
		}

		if v < 0 {
			v = 0
		}

		secs += v
	}

	return secs, true
}

// parseInterval reads one GNU sleep duration operand: a float with an
// optional trailing s/m/h/d suffix. [strconv.ParseFloat] accepts
// inf/infinity, which then exceed any ceiling; NaN is rejected because
// it compares false against every ceiling and would slip through.
func parseInterval(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}

	mult := 1.0

	if m, ok := suffixes[s[len(s)-1]]; ok {
		mult = m
		s = s[:len(s)-1]

		if s == "" {
			return 0, false
		}
	}

	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) {
		return 0, false
	}

	return v * mult, true
}

// literalWord resolves word to its literal value when every part is
// statically known: plain literals, single quotes, and double quotes
// composed only of literal parts. Anything dynamic (parameter
// expansion, arithmetic, command substitution) is non-literal. Note
// [syntax.Word.Lit] is not enough here: it returns "" for `sleep '5'`,
// which would wrongly read a quoted literal as unreadable.
func literalWord(word *syntax.Word) (string, bool) {
	var b strings.Builder

	for _, part := range word.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			b.WriteString(p.Value)
		case *syntax.SglQuoted:
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, dp := range p.Parts {
				lit, ok := dp.(*syntax.Lit)
				if !ok {
					return "", false
				}

				b.WriteString(lit.Value)
			}
		default:
			return "", false
		}
	}

	return b.String(), true
}

// callSrc returns the exact source text of call from command, truncated
// to [callSrcMaxBytes] on a rune boundary so the deny reason stays
// readable.
func callSrc(command string, call *syntax.CallExpr) string {
	src := command[call.Pos().Offset():call.End().Offset()]
	if len(src) <= callSrcMaxBytes {
		return src
	}

	cut := callSrcMaxBytes
	for cut > 0 && !utf8.RuneStart(src[cut]) {
		cut--
	}

	return src[:cut] + "..."
}

// reason builds the deny message for a duration violation: a first
// line naming the offending call and why it trips the guard, then the
// shared [guidance] body.
func reason(src string, secs float64, readable bool, limit float64) string {
	maxStr := formatSeconds(limit)

	var clause string

	switch {
	case !readable:
		clause = fmt.Sprintf("has a duration this hook cannot read, so it cannot be shown to be under the %ss limit", maxStr)
	case math.IsInf(secs, 1):
		clause = "blocks this session indefinitely"
	default:
		clause = fmt.Sprintf("blocks this session for %ss (limit %ss)", formatSeconds(secs), maxStr)
	}

	return fmt.Sprintf("Foreground sleep is denied: `%s` %s.", src, clause) + "\n\n" + guidance()
}

// fillerReason builds the deny message for a filler wait: a command
// that passes time in the foreground without doing real work, at any
// duration.
func fillerReason(src string) string {
	return fmt.Sprintf("Filler wait denied: `%s` does nothing but pass time in the foreground.", src) + "\n\n" + guidance()
}

// guidance is the shared deny-message body: it names the waiting
// primitives that replace a foreground sleep, says to end the turn
// rather than idle, and bounds the one legitimate use of a short
// foreground sleep.
func guidance() string {
	return "Do not wait by sleeping, and do not bide time with no-op filler (`true`, `jobs`, bare `echo`) between tool calls. If you are waiting on a background task or notification, END YOUR TURN: the notification arrives without you idling, and idle turns only delay it. Otherwise pick the shape that matches what you need:\n" +
		"\n" +
		"- Run the long thing in the background: set run_in_background: true on the Bash call. You get one notification when it exits, and the Read tool fetches its captured output.\n" +
		"- Wait on a condition: put the poll loop inside a background Bash call, e.g. run_in_background: true with `until <check>; do sleep 1; done`. sleep is always allowed in a background call.\n" +
		"- Get one notification per event (log lines, file changes, CI steps): use the Monitor tool. It is deferred, so load it first with ToolSearch(\"select:Monitor\").\n" +
		"- Waiting on several things: start all of them first, then handle their completion notifications as they arrive. Do not spawn one, wait for it, then spawn the next -- that serializes work that could have run at once.\n" +
		"\n" +
		"A short sleep is allowed only as a settle step inside a command that does real work (e.g. `kill \"$pid\"; sleep 1; pgrep -f server`), never as the whole command."
}

// formatSeconds renders a duration without a spurious exponent or
// trailing zeros: 300 stays "300", 0.5 stays "0.5".
func formatSeconds(secs float64) string {
	return strconv.FormatFloat(secs, 'f', -1, 64)
}
