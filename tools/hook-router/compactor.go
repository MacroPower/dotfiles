package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Compaction defaults applied by [NewCompactor] when a knob arrives
// non-positive. The minRunLength default is large enough that a lone line
// is never collapsed; a smaller value passed explicitly is still bounded
// by the final length gate in [*Compactor.Compact], which drops any marker
// that would not net-shorten the output. minBytes gates on the raw
// pre-strip output size so a small-but-noisy output is left alone.
const (
	defaultMinRunLength = 3
	defaultMinBytes     = 2048
)

// compactStreams is the set of tool_response stream names the compactor
// recognizes and the single source of truth for what [parseCompactConfig]
// accepts in [CompactConfig.Streams]. Adding an entry here is all it takes
// to make a stream selectable.
var compactStreams = []string{"stdout", "stderr"}

// ansiRegexp matches a single ANSI/VT escape sequence: an OSC string
// (ESC ] ... terminated by BEL, ST, or the 8-bit ST), or a CSI/SGR-style
// sequence introduced by ESC or the 8-bit CSI byte. Adapted from
// chalk/ansi-regex; RE2-safe (no backreferences), so it compiles under
// Go's stdlib regexp.
var ansiRegexp = regexp.MustCompile(
	`\x1b\][\s\S]*?(?:\x07|\x1b\\|\x9c)|[\x1b\x9b][\[\]()#;?]*(?:\d{1,4}(?:[;:]\d{0,4})*)?[\dA-PR-TZcf-nq-uy=><~]`,
)

// CompactConfig declares the PostToolUse:Bash output-compaction knobs.
// JSON tags are camelCase because [builtins.toJSON] in home/claude.nix
// emits attribute names verbatim and the Go struct tags must match.
//
// MinBytes gates on the raw (pre-strip) output length, so an output that
// is small but mostly ANSI escapes is left untouched rather than
// stripped. Streams selects which tool_response fields to rewrite; each
// entry must be one of [compactStreams] ("stdout", "stderr"). An empty
// Streams list compacts nothing (so the compactor reports [*Compactor.Empty]).
type CompactConfig struct {
	Enable       bool     `json:"enable"`
	StripAnsi    bool     `json:"stripAnsi"`
	MinRunLength int      `json:"minRunLength"`
	MinBytes     int      `json:"minBytes"`
	Streams      []string `json:"streams"`
}

// Compactor rewrites verbose-but-successful Bash output by stripping
// ANSI escapes and collapsing consecutive byte-identical line runs.
// Construct with [NewCompactor] or [parseCompactConfig]. A nil receiver
// is treated as disabled by [*Compactor.Empty] and [*Compactor.Streams]
// so handlers can gate on Empty() before touching any other method.
type Compactor struct {
	cfg CompactConfig
}

// NewCompactor builds a [*Compactor] from cfg, substituting defaults for
// non-positive numeric knobs ([defaultMinRunLength], [defaultMinBytes]).
// Other knobs are taken verbatim; the Streams list is not defaulted, so
// an unset (nil) list yields an empty compactor. home/claude.nix always
// emits every knob via [builtins.toJSON].
func NewCompactor(cfg CompactConfig) *Compactor {
	if cfg.MinRunLength <= 0 {
		cfg.MinRunLength = defaultMinRunLength
	}

	if cfg.MinBytes <= 0 {
		cfg.MinBytes = defaultMinBytes
	}

	return &Compactor{cfg: cfg}
}

// Empty reports whether the compactor would never rewrite output: a nil
// receiver, a disabled config, or a config that selects no streams. The
// nil-receiver guard is load-bearing (mirrors [*FormatterRules.Empty]):
// cfg.compactor is a nil *Compactor in the bare config{} literals across
// the handler tests, and [handlePostBashCompact] gates on Empty() before
// calling any other method, so those tests stay green without
// constructing a compactor.
func (c *Compactor) Empty() bool {
	if c == nil {
		return true
	}

	return !c.cfg.Enable || len(c.cfg.Streams) == 0
}

// Streams returns the tool_response stream names selected for rewriting,
// in configuration order. A nil receiver or a disabled config returns nil.
// The result aliases internal state and must not be mutated.
func (c *Compactor) Streams() []string {
	if c == nil || !c.cfg.Enable {
		return nil
	}

	return c.cfg.Streams
}

// Compact applies the configured transforms to s and returns the result
// with a changed flag. It returns (s, false) unchanged when the
// compactor is disabled, when len(s) is below MinBytes (checked against
// the raw pre-strip length), or when the transformed output would not be
// strictly shorter than s. That final length gate is the single bloat
// guard: a collapse marker on a tiny run in otherwise-incompressible
// output is never emitted, so the rewrite can only ever shrink output.
func (c *Compactor) Compact(s string) (string, bool) {
	if c.Empty() || len(s) < c.cfg.MinBytes {
		return s, false
	}

	work := s

	if c.cfg.StripAnsi {
		work = stripANSI(work)
	}

	work = collapseRuns(work, c.cfg.MinRunLength)

	if len(work) < len(s) {
		return work, true
	}

	return s, false
}

// stripANSI removes every ANSI/VT escape sequence from s. It returns s
// unchanged when s contains no escape-introducer byte (ESC or the 8-bit
// CSI), avoiding a regexp scan over the common escape-free output.
func stripANSI(s string) string {
	// The cutset is the ESC byte plus the 8-bit CSI rune (U+009B); one
	// ContainsAny scan beats two ContainsRune passes on escape-free output.
	if !strings.ContainsAny(s, "\x1b\u009b") {
		return s
	}

	return ansiRegexp.ReplaceAllString(s, "")
}

// collapseRuns rewrites each maximal run of minRun-or-more consecutive
// byte-identical lines (journald / `uniq -c` semantics: consecutive
// only, order-preserving) into the line once plus a [compactMarker].
// Shorter runs pass through verbatim. Lines are split and rejoined on
// "\n", so a trailing newline round-trips exactly (its trailing ""
// element rejoins cleanly) and a CR is treated as ordinary line content.
func collapseRuns(s string, minRun int) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))

	for i := 0; i < len(lines); {
		j := i + 1
		for j < len(lines) && lines[j] == lines[i] {
			j++
		}

		if runLen := j - i; runLen >= minRun {
			out = append(out, lines[i], compactMarker(runLen-1))
		} else {
			out = append(out, lines[i:j]...)
		}

		i = j
	}

	return strings.Join(out, "\n")
}

// compactMarker renders the one-line marker that replaces a collapsed
// run's dropped repeats. n is the count of additional identical lines
// (run length minus the one kept line). The leading indent visually sets
// it apart from real output.
func compactMarker(n int) string {
	return fmt.Sprintf("    [hook-router: +%d identical lines]", n)
}

// parseCompactConfig decodes the JSON object passed via
// --compaction-config into a [*Compactor]. Empty input yields a disabled
// compactor (so [*Compactor.Empty] reports true); malformed JSON returns
// an error so wrapper misconfiguration is loud.
func parseCompactConfig(s string) (*Compactor, error) {
	if s == "" {
		return NewCompactor(CompactConfig{}), nil
	}

	var cfg CompactConfig

	err := json.Unmarshal([]byte(s), &cfg)
	if err != nil {
		return nil, fmt.Errorf("decoding compaction config JSON: %w", err)
	}

	for _, stream := range cfg.Streams {
		if !slices.Contains(compactStreams, stream) {
			return nil, fmt.Errorf(
				"compaction config: unknown stream %q (want one of %v)",
				stream, compactStreams,
			)
		}
	}

	return NewCompactor(cfg), nil
}
