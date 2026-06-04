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

	id, err := loadOrCreateHostID(false)
	require.NoError(t, err)
	assert.Len(t, id, 16, "host id must be 16 hex chars (8 bytes)")

	path := hostIDPath(false)

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

	first, err := loadOrCreateHostID(false)
	require.NoError(t, err)

	pathBefore := hostIDPath(false)
	infoBefore, err := os.Stat(pathBefore)
	require.NoError(t, err)

	second, err := loadOrCreateHostID(false)
	require.NoError(t, err)

	assert.Equal(t, first, second, "second call must return persisted id")

	infoAfter, err := os.Stat(pathBefore)
	require.NoError(t, err)
	assert.Equal(t, infoBefore.ModTime(), infoAfter.ModTime(),
		"second call must not rewrite the file")
}

// TestLoadOrCreateHostIDTrimsWhitespace pins that whitespace and
// trailing newlines around a well-formed id are tolerated. An
// operator that hand-edits the file with `echo >host.id` would
// otherwise see the trailing newline change the effective selector.
func TestLoadOrCreateHostIDTrimsWhitespace(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path := hostIDPath(false)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("  0123456789abcdef\n"), 0o600))

	id, err := loadOrCreateHostID(false)
	require.NoError(t, err)
	assert.Equal(t, "0123456789abcdef", id, "leading/trailing whitespace must be trimmed")
}

// TestLoadOrCreateHostIDRegeneratesEmptyFile pins that a zero-byte
// host.id (an aborted prior write that landed at the final path)
// is treated as missing and a fresh id is minted. Without this
// the sweep would refuse to run forever because the
// [ErrMissingHostID] guard would trip on the empty id.
func TestLoadOrCreateHostIDRegeneratesEmptyFile(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path := hostIDPath(false)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(""), 0o600))

	id, err := loadOrCreateHostID(false)
	require.NoError(t, err)
	assert.Len(t, id, 16, "empty host.id must be regenerated to a 16-hex id")
}

// TestLoadOrCreateHostIDRegeneratesInvalidFormat pins that
// persisted content rejected by [validHostID] (a hand-edited or
// corrupt host.id) is replaced with a fresh well-formed id.
// Returning it verbatim would label every resource with a value
// [runHostSweep] refuses, permanently disabling the sweep.
func TestLoadOrCreateHostIDRegeneratesInvalidFormat(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path := hostIDPath(false)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("manual-id\n"), 0o600))

	id, err := loadOrCreateHostID(false)
	require.NoError(t, err)
	assert.True(t, validHostID(id), "regenerated id must satisfy validHostID")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, id, string(data), "regenerated id must be persisted")
}

// TestLoadOrCreateHostIDPerEnv pins the host/guest id split. The
// state dir is shared across the Lima bind mount, but each env can
// only observe its own env's live sockets, so a shared id would
// let one env's sweep delete the other env's live resources.
func TestLoadOrCreateHostIDPerEnv(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	assert.Equal(t, "host.id", filepath.Base(hostIDPath(false)))
	assert.Equal(t, "guest.id", filepath.Base(hostIDPath(true)))

	hostID, err := loadOrCreateHostID(false)
	require.NoError(t, err)

	guestID, err := loadOrCreateHostID(true)
	require.NoError(t, err)

	assert.NotEqual(t, hostID, guestID,
		"host and guest envs must mint distinct ids so their sweeps stay disjoint")
}
