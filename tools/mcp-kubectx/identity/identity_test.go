package identity_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/identity"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/statedir"
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
// state directory with no host.key file mints a fresh id, persists it
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
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "host.key must be 0600")

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
// operator that hand-edits the file with `echo >host.key` would
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
// host.key (an aborted prior write that landed at the final path) is
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
	assert.Len(t, id, 16, "empty host.key must be regenerated to a 16-hex id")
}

// TestLoadOrCreateHostRegeneratesInvalidFormat pins that persisted
// content rejected by [identity.Valid] (a hand-edited or corrupt
// host.key) is replaced with a fresh well-formed id. Returning it
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

// TestLoadOrCreateHostPerEnv pins the host/guest id split. Each env
// observes only the live sockets of its own env. So when serves of
// both envs resolve the same state dir, a shared id lets the sweep of
// one env delete the live resources of the other env.
func TestLoadOrCreateHostPerEnv(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	assert.Equal(t, "host.key", filepath.Base(identity.HostPath(false)))
	assert.Equal(t, "guest.key", filepath.Base(identity.HostPath(true)))

	hostID, err := identity.LoadOrCreateHost(false)
	require.NoError(t, err)

	guestID, err := identity.LoadOrCreateHost(true)
	require.NoError(t, err)

	assert.NotEqual(t, hostID, guestID,
		"host and guest envs must mint distinct ids so their sweeps stay disjoint")
}

// TestLoadOrCreateHostIgnoresPlainIDFile pins that each env mints a
// fresh id beside the `host.id` or `guest.id` of a build before
// [identity.Digest]. Those builds put the plain id on the label of
// each resource, where every guest with a `view` token reads it. A
// guest that holds such an id acts on the resources of each serve
// that keeps it. The old file stays as it was, because the README
// reads the plain id from it to delete the resources of those
// builds.
func TestLoadOrCreateHostIgnoresPlainIDFile(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	require.NoError(t, os.MkdirAll(statedir.RunDir(), 0o700))

	tests := map[string]struct {
		forGuest bool
		oldFile  string
		oldID    string
	}{
		"host env": {
			forGuest: false,
			oldFile:  "host.id",
			oldID:    "0123456789abcdef",
		},
		"guest env": {
			forGuest: true,
			oldFile:  "guest.id",
			oldID:    "fedcba9876543210",
		},
	}

	for name, tc := range tests { //nolint:paralleltest // uses t.Setenv of the parent
		t.Run(name, func(t *testing.T) {
			oldPath := filepath.Join(statedir.RunDir(), tc.oldFile)
			require.NoError(t, os.WriteFile(oldPath, []byte(tc.oldID), 0o600))

			id, err := identity.LoadOrCreateHost(tc.forGuest)
			require.NoError(t, err)
			assert.NotEqual(t, tc.oldID, id, "the id of %s must not become the host id", tc.oldFile)

			data, err := os.ReadFile(identity.HostPath(tc.forGuest))
			require.NoError(t, err)
			assert.Equal(t, id, string(data), "the fresh id must persist at HostPath")

			data, err = os.ReadFile(oldPath)
			require.NoError(t, err)
			assert.Equal(t, tc.oldID, string(data), "%s must keep the plain id", tc.oldFile)
		})
	}
}

// TestHostPathInRunDir pins that each id file lies in
// [statedir.RunDir], which workmux does not mount into a Lima
// guest. A guest that reads the host id of host-side serves can act
// on their resources.
func TestHostPathInRunDir(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	for _, forGuest := range []bool{false, true} {
		assert.Equal(t, statedir.RunDir(), filepath.Dir(identity.HostPath(forGuest)))
	}
}

// TestDigest pins the digest of a host id. The digest passes
// [identity.Valid], so it fits a label value and the name of a
// provision record. It differs from the host id, and it does not
// change between calls, so a later `host sweep` finds the resources
// of an earlier `host select`.
func TestDigest(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		hostID string
		want   string
	}{
		"host id": {
			hostID: "0123456789abcdef",
			want:   "b72a26ab004b5654",
		},
		"other host id": {
			hostID: "fedcba9876543210",
			want:   "585ea48c63bde315",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := identity.Digest(tc.hostID)
			assert.Equal(t, tc.want, got)
			assert.True(t, identity.Valid(got), "digest %q must be a valid id", got)
			assert.NotEqual(t, tc.hostID, got)
		})
	}
}
