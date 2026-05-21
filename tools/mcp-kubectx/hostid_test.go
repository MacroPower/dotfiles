package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadOrCreateHostIDFreshCreate pins that the first call into
// a state directory with no host.id file mints a fresh id,
// persists it at [hostIDPath] with mode 0600, and returns it as
// a 16-character lowercase hex string.
func TestLoadOrCreateHostIDFreshCreate(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	id, err := loadOrCreateHostID()
	require.NoError(t, err)
	assert.Len(t, id, 16, "host id must be 16 hex chars (8 bytes)")

	path := hostIDPath()

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "host.id must be 0600")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, id, string(data), "file contents must match returned id")
}

// TestLoadOrCreateHostIDIdempotent pins that calling twice returns
// the same id and does not rewrite the file.
func TestLoadOrCreateHostIDIdempotent(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	first, err := loadOrCreateHostID()
	require.NoError(t, err)

	pathBefore := hostIDPath()
	infoBefore, err := os.Stat(pathBefore)
	require.NoError(t, err)

	second, err := loadOrCreateHostID()
	require.NoError(t, err)

	assert.Equal(t, first, second, "second call must return persisted id")

	infoAfter, err := os.Stat(pathBefore)
	require.NoError(t, err)
	assert.Equal(t, infoBefore.ModTime(), infoAfter.ModTime(),
		"second call must not rewrite the file")
}

// TestLoadOrCreateHostIDTrimsWhitespace pins that whitespace and
// trailing newlines in a manually edited host.id file are
// tolerated. An operator that hand-edits the file with `echo
// >host.id` would otherwise see the trailing newline change the
// effective selector.
func TestLoadOrCreateHostIDTrimsWhitespace(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path := hostIDPath()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("  manual-id\n"), 0o600))

	id, err := loadOrCreateHostID()
	require.NoError(t, err)
	assert.Equal(t, "manual-id", id, "leading/trailing whitespace must be trimmed")
}

// TestLoadOrCreateHostIDRegeneratesEmptyFile pins that a zero-byte
// host.id (an aborted prior write that landed at the final path)
// is treated as missing and a fresh id is minted. Without this
// the sweep would refuse to run forever because the
// [ErrMissingHostID] guard would trip on the empty id.
func TestLoadOrCreateHostIDRegeneratesEmptyFile(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path := hostIDPath()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(""), 0o600))

	id, err := loadOrCreateHostID()
	require.NoError(t, err)
	assert.Len(t, id, 16, "empty host.id must be regenerated to a 16-hex id")
}
