package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunUnknownSubcommand(t *testing.T) {
	t.Parallel()

	err := run([]string{"fetch"})
	require.ErrorIs(t, err, ErrUnknownSubcommand)
}

func TestRunNoArgs(t *testing.T) {
	t.Parallel()

	err := run([]string{})
	require.ErrorIs(t, err, ErrNoSubcommand)
}

func TestRunCloneMissingArgs(t *testing.T) {
	t.Parallel()

	err := run([]string{"clone", "only-url"})
	require.ErrorIs(t, err, ErrUsage)
}

func TestParseArgs(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		args           []string
		wantFlags      []string
		wantPositional []string
	}{
		"url and dest": {
			args:           []string{"URL", "dest"},
			wantPositional: []string{"URL", "dest"},
		},
		"depth flag": {
			args:           []string{"--depth", "1", "URL", "dest"},
			wantFlags:      []string{"--depth", "1"},
			wantPositional: []string{"URL", "dest"},
		},
		"short flag": {
			args:           []string{"-q", "URL", "dest"},
			wantFlags:      []string{"-q"},
			wantPositional: []string{"URL", "dest"},
		},
		"double dash separator": {
			args:           []string{"--", "--weird-url", "dest"},
			wantPositional: []string{"--weird-url", "dest"},
		},
		"flags then double dash": {
			args:           []string{"--depth", "1", "--", "URL", "dest"},
			wantFlags:      []string{"--depth", "1"},
			wantPositional: []string{"URL", "dest"},
		},
		"empty args": {
			args: []string{},
		},
		"url only": {
			args:           []string{"URL"},
			wantPositional: []string{"URL"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			flags, positional := parseArgs(tt.args)
			assert.Equal(t, tt.wantFlags, flags)
			assert.Equal(t, tt.wantPositional, positional)
		})
	}
}
