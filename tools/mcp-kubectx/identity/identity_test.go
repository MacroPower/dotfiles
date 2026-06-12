package identity_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/identity"
)

func TestNew(t *testing.T) {
	t.Parallel()

	id, err := identity.New()
	require.NoError(t, err)
	assert.True(t, identity.Valid(id), "fresh ids must satisfy Valid")
}

func TestValid(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		id   string
		want bool
	}{
		"well-formed":        {id: "0123456789abcdef", want: true},
		"empty":              {id: "", want: false},
		"fifteen chars":      {id: "0123456789abcde", want: false},
		"seventeen chars":    {id: "0123456789abcdef0", want: false},
		"uppercase hex":      {id: "0123456789ABCDEF", want: false},
		"non-hex char":       {id: "0123456789abcdeg", want: false},
		"selector injection": {id: "aaaaaaaaaaaaaaaa,namespace=kube-system", want: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, identity.Valid(tc.id))
		})
	}
}

// TestLoadOrCreateHostFreshCreate pins that the first call into a
// state directory with no host.id file mints a fresh id, persists it
// at [identity.HostPath] with mode 0600, and returns it as a
// 16-character lowercase hex string.
func TestLoadOrCreateHostFreshCreate(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	id, err := identity.LoadOrCreateHost(false)
	require.NoError(t, err)
	assert.Len(t, id, 16, "host id must be 16 hex chars (8 bytes)")

	path := identity.HostPath(false)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "host.id must be 0600")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, id, string(data), "file contents must match returned id")
}

// TestLoadOrCreateHostIdempotent pins that calling twice returns the
// same id and does not rewrite the file.
func TestLoadOrCreateHostIdempotent(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	first, err := identity.LoadOrCreateHost(false)
	require.NoError(t, err)

	pathBefore := identity.HostPath(false)
	infoBefore, err := os.Stat(pathBefore)
	require.NoError(t, err)

	second, err := identity.LoadOrCreateHost(false)
	require.NoError(t, err)

	assert.Equal(t, first, second, "second call must return persisted id")

	infoAfter, err := os.Stat(pathBefore)
	require.NoError(t, err)
	assert.Equal(t, infoBefore.ModTime(), infoAfter.ModTime(),
		"second call must not rewrite the file")
}

// TestLoadOrCreateHostTrimsWhitespace pins that whitespace and
// trailing newlines around a well-formed id are tolerated. An
// operator that hand-edits the file with `echo >host.id` would
// otherwise see the trailing newline change the effective selector.
func TestLoadOrCreateHostTrimsWhitespace(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path := identity.HostPath(false)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("  0123456789abcdef\n"), 0o600))

	id, err := identity.LoadOrCreateHost(false)
	require.NoError(t, err)
	assert.Equal(t, "0123456789abcdef", id, "leading/trailing whitespace must be trimmed")
}

// TestLoadOrCreateHostRegeneratesEmptyFile pins that a zero-byte
// host.id (an aborted prior write that landed at the final path) is
// treated as missing and a fresh id is minted. Without this the
// sweep would refuse to run forever because its missing-host-id
// guard would trip on the empty id.
func TestLoadOrCreateHostRegeneratesEmptyFile(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path := identity.HostPath(false)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(""), 0o600))

	id, err := identity.LoadOrCreateHost(false)
	require.NoError(t, err)
	assert.Len(t, id, 16, "empty host.id must be regenerated to a 16-hex id")
}

// TestLoadOrCreateHostRegeneratesInvalidFormat pins that persisted
// content rejected by [identity.Valid] (a hand-edited or corrupt
// host.id) is replaced with a fresh well-formed id. Returning it
// verbatim would label every resource with a value the sweep
// refuses, permanently disabling the sweep.
func TestLoadOrCreateHostRegeneratesInvalidFormat(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path := identity.HostPath(false)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("manual-id\n"), 0o600))

	id, err := identity.LoadOrCreateHost(false)
	require.NoError(t, err)
	assert.True(t, identity.Valid(id), "regenerated id must satisfy Valid")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, id, string(data), "regenerated id must be persisted")
}

// TestLoadOrCreateHostPerEnv pins the host/guest id split. The state
// dir is shared across the Lima bind mount, but each env can only
// observe its own env's live sockets, so a shared id would let one
// env's sweep delete the other env's live resources.
func TestLoadOrCreateHostPerEnv(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	assert.Equal(t, "host.id", filepath.Base(identity.HostPath(false)))
	assert.Equal(t, "guest.id", filepath.Base(identity.HostPath(true)))

	hostID, err := identity.LoadOrCreateHost(false)
	require.NoError(t, err)

	guestID, err := identity.LoadOrCreateHost(true)
	require.NoError(t, err)

	assert.NotEqual(t, hostID, guestID,
		"host and guest envs must mint distinct ids so their sweeps stay disjoint")
}
