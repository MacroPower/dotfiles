package ledger_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/ledger"
)

const (
	hostA     = "aaaaaaaaaaaaaaaa"
	hostB     = "bbbbbbbbbbbbbbbb"
	instance1 = "1111111111111111"
	instance2 = "2222222222222222"
)

// record returns a record of hostA and instance1 for the
// ServiceAccount name in the namespace ns of the context prod.
func record(name string) ledger.Record {
	return ledger.Record{
		HostID:         hostA,
		InstanceID:     instance1,
		Kubeconfig:     "/kc",
		Context:        "prod",
		Namespace:      "ns",
		ServiceAccount: name,
	}
}

// TestAddThenList pins the round trip that the sweep relies on.
// Every field of an added record comes back, the time of the write
// to the nanosecond. A context name with path separators and colons
// does not change where the file lands.
func TestAddThenList(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "ledger")
	l := ledger.New(dir)

	want := ledger.Record{
		HostID:         hostA,
		InstanceID:     instance1,
		Kubeconfig:     "/home/user/.kube/config",
		Context:        "arn:aws:eks:us-east-1:123456789012:cluster/../prod",
		Namespace:      "kube-system",
		ServiceAccount: "claude-sa-0123abcd",
		ClusterScoped:  true,
		Written:        time.Date(2026, time.March, 4, 12, 30, 0, 123456789, time.UTC),
	}

	require.NoError(t, l.Add(want))

	got, err := l.List()
	require.NoError(t, err)
	assert.Equal(t, []ledger.Record{want}, got)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "the record is one file directly in the ledger directory")

	info, err := entries[0].Info()
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// TestAddIsIdempotent pins that a second Add of the same record
// leaves one record.
func TestAddIsIdempotent(t *testing.T) {
	t.Parallel()

	l := ledger.New(t.TempDir())
	r := record("claude-sa-1")

	require.NoError(t, l.Add(r))
	require.NoError(t, l.Add(r))

	got, err := l.List()
	require.NoError(t, err)
	assert.Equal(t, []ledger.Record{r}, got)
}

// TestAddReplacesTimeOfRecord pins that the time of a record does
// not name its file. A second Add of a record with the same owner
// and location replaces the time, and the ledger holds one record.
func TestAddReplacesTimeOfRecord(t *testing.T) {
	t.Parallel()

	l := ledger.New(t.TempDir())

	first := record("claude-sa-1")
	first.Written = time.Date(2026, time.March, 4, 12, 0, 0, 0, time.UTC)

	second := first
	second.Written = first.Written.Add(time.Hour)

	require.NoError(t, l.Add(first))
	require.NoError(t, l.Add(second))

	got, err := l.List()
	require.NoError(t, err)
	assert.Equal(t, []ledger.Record{second}, got)

	require.NoError(t, l.Remove(first), "Remove must find the file whatever the time of the record")

	got, err = l.List()
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestListSeparatesRecords pins the parts of the identity of a
// record. Records that differ in the host, the instance, or any
// part of the location are separate records.
func TestListSeparatesRecords(t *testing.T) {
	t.Parallel()

	l := ledger.New(t.TempDir())

	base := record("claude-sa-1")

	otherHost := base
	otherHost.HostID = hostB

	otherInstance := base
	otherInstance.InstanceID = instance2

	otherKubeconfig := base
	otherKubeconfig.Kubeconfig = "/other"

	otherContext := base
	otherContext.Context = "staging"

	otherNamespace := base
	otherNamespace.Namespace = "other"

	otherName := base
	otherName.ServiceAccount = "claude-sa-2"

	want := []ledger.Record{
		base, otherHost, otherInstance, otherKubeconfig, otherContext, otherNamespace, otherName,
	}

	for _, r := range want {
		require.NoError(t, l.Add(r))
	}

	got, err := l.List()
	require.NoError(t, err)
	assert.ElementsMatch(t, want, got)
}

// TestListMissingDirectory pins that a host that never provisioned
// has an empty ledger and no error.
func TestListMissingDirectory(t *testing.T) {
	t.Parallel()

	l := ledger.New(filepath.Join(t.TempDir(), "absent"))

	got, err := l.List()
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestListReportsUnreadableDirectory pins the sentinel of a ledger
// path that is not a directory.
func TestListReportsUnreadableDirectory(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	_, err := ledger.New(file).List()
	require.ErrorIs(t, err, ledger.ErrList)
}

// TestListSkipsForeignFiles pins how a listing treats a file that
// is not a record. Such a file neither fails the listing nor
// appears in it, and the listing leaves the file in place.
func TestListSkipsForeignFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	l := ledger.New(dir)

	want := record("claude-sa-1")
	require.NoError(t, l.Add(want))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	valid := entries[0].Name()
	key := strings.TrimSuffix(strings.TrimPrefix(valid, hostA+"."+instance1+"."), ".json")
	content := `{"kubeconfig":"/kc","context":"prod","namespace":"ns","serviceAccount":"claude-sa-1"}`

	foreign := map[string]string{
		"notes.txt":                 "not a record",
		valid + ".tmp":              content,
		valid + ".bak":              content,
		"copy-of-" + valid:          content,
		hostA + ".json":             content,
		key + ".json":               content,
		hostA + "." + key + ".json": content,
		hostA + "." + instance1 + "." + key + ".extra.json":      content,
		"AAAAAAAAAAAAAAAA." + instance1 + "." + key + ".json":    content,
		hostA + "." + instance2 + ".0123456789abcdef.json":       content,
		hostA + "." + instance2 + "." + key + ".json":            "{not json",
		hostB + "." + instance1 + "." + key + ".json":            `{"kubeconfig":"/kc","context":"staging"}`,
		hostB + "." + instance2 + "." + key + ".json/unreadable": "",
	}

	for name, data := range foreign {
		path := filepath.Join(dir, name)

		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(data), 0o600))
	}

	got, err := l.List()
	require.NoError(t, err)
	assert.Equal(t, []ledger.Record{want}, got)

	for name := range foreign {
		assert.FileExists(t, filepath.Join(dir, name))
	}
}

// TestRemove pins that Remove deletes exactly the named record and
// that a second Remove of the same record succeeds.
func TestRemove(t *testing.T) {
	t.Parallel()

	l := ledger.New(t.TempDir())

	gone := record("claude-sa-1")
	kept := record("claude-sa-2")

	require.NoError(t, l.Add(gone))
	require.NoError(t, l.Add(kept))

	require.NoError(t, l.Remove(gone))
	require.NoError(t, l.Remove(gone), "removing an absent record must succeed")

	got, err := l.List()
	require.NoError(t, err)
	assert.Equal(t, []ledger.Record{kept}, got)
}

// TestFind pins the lookup that `host token` and `host release` use
// as the proof that a `serve` provisioned a ServiceAccount. Find
// returns the stored record only when the owner and the location of
// the key both match it. The time and the binding kind of the key
// do not take part in the lookup.
func TestFind(t *testing.T) {
	t.Parallel()

	stored := record("claude-sa-1")
	stored.ClusterScoped = true
	stored.Written = time.Date(2026, time.March, 4, 12, 30, 0, 0, time.UTC)

	tests := map[string]struct {
		key   func(r ledger.Record) ledger.Record
		found bool
	}{
		"same owner and location": {
			key: func(r ledger.Record) ledger.Record {
				r.ClusterScoped = false
				r.Written = time.Time{}

				return r
			},
			found: true,
		},
		"other host": {
			key:   func(r ledger.Record) ledger.Record { r.HostID = hostB; return r },
			found: false,
		},
		"other instance": {
			key:   func(r ledger.Record) ledger.Record { r.InstanceID = instance2; return r },
			found: false,
		},
		"other kubeconfig": {
			key:   func(r ledger.Record) ledger.Record { r.Kubeconfig = "/other"; return r },
			found: false,
		},
		"other context": {
			key:   func(r ledger.Record) ledger.Record { r.Context = "staging"; return r },
			found: false,
		},
		"other namespace": {
			key:   func(r ledger.Record) ledger.Record { r.Namespace = "kube-system"; return r },
			found: false,
		},
		"other service account": {
			key:   func(r ledger.Record) ledger.Record { r.ServiceAccount = "claude-sa-2"; return r },
			found: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			l := ledger.New(t.TempDir())
			require.NoError(t, l.Add(stored))

			got, err := l.Find(tc.key(stored))
			if !tc.found {
				require.ErrorIs(t, err, ledger.ErrNotFound)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, stored, got)
		})
	}
}

// TestFindMissingDirectory pins that a ledger without a directory
// holds no record to find.
func TestFindMissingDirectory(t *testing.T) {
	t.Parallel()

	l := ledger.New(filepath.Join(t.TempDir(), "missing"))

	_, err := l.Find(record("claude-sa-1"))
	require.ErrorIs(t, err, ledger.ErrNotFound)
}

// TestRejectsInvalidIDs pins that Add, Remove, and Find never build
// a file name from a malformed id. A path separator in such an id
// leads out of the ledger directory.
func TestRejectsInvalidIDs(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		hostID     string
		instanceID string
	}{
		"empty host id":           {hostID: "", instanceID: instance1},
		"empty instance id":       {hostID: hostA, instanceID: ""},
		"host id with traversal":  {hostID: "../../etc/passwd", instanceID: instance1},
		"instance id with a path": {hostID: hostA, instanceID: "a/b"},
		"uppercase host id":       {hostID: "AAAAAAAAAAAAAAAA", instanceID: instance1},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "ledger")
			l := ledger.New(dir)

			r := record("claude-sa-1")
			r.HostID, r.InstanceID = tc.hostID, tc.instanceID

			require.ErrorIs(t, l.Add(r), ledger.ErrInvalidID)
			require.ErrorIs(t, l.Remove(r), ledger.ErrInvalidID)

			_, err := l.Find(r)
			require.ErrorIs(t, err, ledger.ErrInvalidID)
			assert.NoDirExists(t, dir, "a rejected record must not touch the filesystem")
		})
	}
}

// TestAddReportsWriteFailure pins the sentinel that `host select`
// surfaces when the ledger directory cannot hold the record.
func TestAddReportsWriteFailure(t *testing.T) {
	t.Parallel()

	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))

	l := ledger.New(filepath.Join(blocker, "ledger"))

	require.ErrorIs(t, l.Add(record("claude-sa-1")), ledger.ErrAdd)
}

// TestAddAndRemoveReportBlockedRecordFile pins the sentinels of a
// record file that the ledger cannot write or remove although the
// ledger directory exists. A non-empty directory at the path of the
// record file makes both calls fail, as root too.
func TestAddAndRemoveReportBlockedRecordFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	l := ledger.New(dir)
	r := record("claude-sa-1")

	require.NoError(t, l.Add(r))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	name := entries[0].Name()

	require.NoError(t, l.Remove(r))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, name, "x"), 0o700))

	require.ErrorIs(t, l.Add(r), ledger.ErrAdd)
	require.ErrorIs(t, l.Remove(r), ledger.ErrRemove)
	assert.DirExists(t, filepath.Join(dir, name, "x"), "the failed calls must leave the blocker in place")
}

// TestSameServiceAccount pins that the comparison covers the whole
// location of a ServiceAccount and nothing of the owner.
func TestSameServiceAccount(t *testing.T) {
	t.Parallel()

	base := record("claude-sa-1")

	tests := map[string]struct {
		change func(r *ledger.Record)
		want   bool
	}{
		"same record":      {change: func(*ledger.Record) {}, want: true},
		"other host":       {change: func(r *ledger.Record) { r.HostID = hostB }, want: true},
		"other instance":   {change: func(r *ledger.Record) { r.InstanceID = instance2 }, want: true},
		"other binding":    {change: func(r *ledger.Record) { r.ClusterScoped = true }, want: true},
		"other write time": {change: func(r *ledger.Record) { r.Written = time.Unix(1, 0) }, want: true},
		"other kubeconfig": {change: func(r *ledger.Record) { r.Kubeconfig = "/other" }},
		"other context":    {change: func(r *ledger.Record) { r.Context = "staging" }},
		"other namespace":  {change: func(r *ledger.Record) { r.Namespace = "other" }},
		"other name":       {change: func(r *ledger.Record) { r.ServiceAccount = "claude-sa-2" }},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			other := base
			tc.change(&other)

			assert.Equal(t, tc.want, base.SameServiceAccount(other))
		})
	}
}
