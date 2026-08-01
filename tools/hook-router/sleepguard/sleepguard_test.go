package sleepguard_test

import (
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mvdan.cc/sh/v3/syntax"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/sleepguard"
)

// enabledConfig is a guard with an explicit 10s ceiling.
func enabledConfig() sleepguard.Config {
	return sleepguard.Config{Enable: true, MaxSeconds: 10}
}

// mustParse parses command into a shell AST, failing the test on a
// parse error.
func mustParse(t *testing.T, command string) *syntax.File {
	t.Helper()

	prog, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	require.NoError(t, err)

	return prog
}

// TestCheck pins the duration and matching semantics under the default
// enabled config; config-variant behavior lives in [TestCheckConfig].
func TestCheck(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		command    string
		background bool
		deny       bool
	}{
		"under ceiling inside real work": {command: "kill 123; sleep 5; pgrep -f server"},
		"at ceiling inside real work":    {command: "kill 123; sleep 10; pgrep -f server"},
		"over ceiling":                   {command: "sleep 11", deny: true},
		"over ceiling inside real work":  {command: "kill 123; sleep 11; pgrep -f server", deny: true},
		"classic wait then command":      {command: "sleep 300; echo done", deny: true},
		"sequenced with and":             {command: "sleep 300 && foo", deny: true},
		"minute suffix":                  {command: "sleep 5m", deny: true},
		"second suffix inside real work": {command: "foo; sleep 9s"},
		"hour suffix":                    {command: "sleep 1h", deny: true},
		"day suffix":                     {command: "sleep 1d", deny: true},
		"fractional inside real work":    {command: "foo; sleep 0.5"},
		"fractional over":                {command: "sleep 10.5", deny: true},
		"single-quoted short literal":    {command: "foo; sleep '5'"},
		"double-quoted short literal":    {command: `foo; sleep "5"`},
		"quoted command word":            {command: "'sleep' 300", deny: true},
		"operands sum over":              {command: "sleep 5 10", deny: true},
		"operands sum inside real work":  {command: "foo; sleep 2 3"},
		"parameter expansion":            {command: "sleep $VAR", deny: true},
		"arithmetic expansion":           {command: "sleep $((5*60))", deny: true},
		"command substitution":           {command: "sleep $(cat n)", deny: true},
		"expansion inside quotes":        {command: `sleep "${DELAY}s"`, deny: true},
		"infinity":                       {command: "sleep infinity", deny: true},
		"inf":                            {command: "sleep inf", deny: true},
		"nan":                            {command: "sleep nan", deny: true},
		"unparseable operand":            {command: "sleep 5x", deny: true},
		"empty operand":                  {command: `sleep ""`, deny: true},
		"bare suffix operand":            {command: "sleep m", deny: true},
		"negative clamps to zero":        {command: "sleep -600 300", deny: true},
		"bare sleep":                     {command: "sleep"},
		"help flag only":                 {command: "sleep --help"},
		"zero duration is not filler":    {command: "sleep 0"},
		"absolute path":                  {command: "/bin/sleep 300", deny: true},
		"inside command substitution":    {command: "echo $(sleep 300)", deny: true},
		"bare short sleep is filler":     {command: "sleep 6", deny: true},
		"filler sleep with narration":    {command: `sleep 1; echo "session stays free, waiting"`, deny: true},
		"filler chain of short sleeps":   {command: "sleep 4 && sleep 4", deny: true},
		"filler sleep with job inspection": {
			command: "jobs; sleep 3",
			deny:    true,
		},
		"sleep as an argument, not a command": {
			command: "echo sleep 300",
		},
		"foreground busy-wait loop is filler": {
			command: "while true; do sleep 5; done",
			deny:    true,
		},
		"foreground poll loop with substantive check is a documented non-goal": {
			command: "until [ -f /tmp/x ]; do sleep 1; done",
		},
		"foreground loop with long sleep": {
			command: "while true; do sleep 60; done",
			deny:    true,
		},
		"background call is exempt": {
			command:    "sleep 300",
			background: true,
		},
		"background filler sleep is exempt": {
			command:    "sleep 6",
			background: true,
		},
		"background poll loop is exempt": {
			command:    "until grep -q x log; do sleep 1; done",
			background: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, deny := sleepguard.Check(mustParse(t, tc.command), tc.command, tc.background, enabledConfig())
			assert.Equal(t, tc.deny, deny)
		})
	}
}

// TestCheckConfig pins how the Config knobs steer [sleepguard.Check].
func TestCheckConfig(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		command string
		cfg     sleepguard.Config
		deny    bool
	}{
		"disabled config allows everything": {
			command: "sleep 300",
			cfg:     sleepguard.Config{},
		},
		"enabled with zero ceiling falls back to the default": {
			command: "foo; sleep 5",
			cfg:     sleepguard.Config{Enable: true},
		},
		"retuned ceiling": {
			command: "foo; sleep 30",
			cfg:     sleepguard.Config{Enable: true, MaxSeconds: 60},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, deny := sleepguard.Check(mustParse(t, tc.command), tc.command, false, tc.cfg)
			assert.Equal(t, tc.deny, deny)
		})
	}
}

// TestCheckReason pins the deny message contents: the offending call is
// quoted back, the measured duration and ceiling are interpolated, the
// waiting alternatives are named, and the text is pure ASCII (the repo's
// typography policy applies to hook output too).
func TestCheckReason(t *testing.T) {
	t.Parallel()

	t.Run("over ceiling", func(t *testing.T) {
		t.Parallel()

		command := "sleep 300; echo done"

		reason, deny := sleepguard.Check(mustParse(t, command), command, false, enabledConfig())
		require.True(t, deny)

		assert.Contains(t, reason, "`sleep 300`")
		assert.Contains(t, reason, "for 300s (limit 10s)")
		assert.Contains(t, reason, "run_in_background")
		assert.Contains(t, reason, `ToolSearch("select:Monitor")`)
		assert.Contains(t, reason, "settle step inside a command that does real work")
		assert.Equal(t, -1, strings.IndexFunc(reason, func(r rune) bool { return r > unicode.MaxASCII }),
			"deny reason must be pure ASCII")
	})

	t.Run("filler wait", func(t *testing.T) {
		t.Parallel()

		command := `sleep 6`

		reason, deny := sleepguard.Check(mustParse(t, command), command, false, enabledConfig())
		require.True(t, deny)

		assert.Contains(t, reason, "Filler wait denied")
		assert.Contains(t, reason, "`sleep 6`")
		assert.Contains(t, reason, "END YOUR TURN")
		assert.Contains(t, reason, "run_in_background")
		assert.Equal(t, -1, strings.IndexFunc(reason, func(r rune) bool { return r > unicode.MaxASCII }),
			"deny reason must be pure ASCII")
	})

	t.Run("unreadable duration", func(t *testing.T) {
		t.Parallel()

		command := "sleep $DELAY"

		reason, deny := sleepguard.Check(mustParse(t, command), command, false, enabledConfig())
		require.True(t, deny)

		assert.Contains(t, reason, "`sleep $DELAY`")
		assert.Contains(t, reason, "cannot read")
		assert.Contains(t, reason, "run_in_background")
	})

	t.Run("infinite duration", func(t *testing.T) {
		t.Parallel()

		command := "sleep infinity"

		reason, deny := sleepguard.Check(mustParse(t, command), command, false, enabledConfig())
		require.True(t, deny)

		assert.Contains(t, reason, "indefinitely")
		assert.NotContains(t, reason, "+Inf")
	})

	t.Run("first offending call wins", func(t *testing.T) {
		t.Parallel()

		command := "sleep 300; sleep 999"

		reason, deny := sleepguard.Check(mustParse(t, command), command, false, enabledConfig())
		require.True(t, deny)

		assert.Contains(t, reason, "`sleep 300`")
		assert.NotContains(t, reason, "999")
	})

	t.Run("long call is truncated", func(t *testing.T) {
		t.Parallel()

		command := "sleep 300" + strings.Repeat(" 300", 20)

		reason, deny := sleepguard.Check(mustParse(t, command), command, false, enabledConfig())
		require.True(t, deny)

		assert.Contains(t, reason, "...")
		assert.NotContains(t, reason, command)
	})
}

func TestParse(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		input string
		want  sleepguard.Config
		err   bool
	}{
		"empty input yields disabled zero config": {
			input: "",
			want:  sleepguard.Config{},
		},
		"full object round-trips": {
			input: `{"enable":true,"maxSeconds":30}`,
			want:  sleepguard.Config{Enable: true, MaxSeconds: 30},
		},
		"omitted maxSeconds stays zero, the ceiling applies the default": {
			input: `{"enable":true}`,
			want:  sleepguard.Config{Enable: true},
		},
		"malformed JSON errors": {
			input: `{"enable":`,
			err:   true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := sleepguard.Parse(tc.input)
			if tc.err {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
