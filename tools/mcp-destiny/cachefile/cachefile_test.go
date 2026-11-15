package cachefile_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/cachefile"
)

func TestSave(t *testing.T) {
	t.Parallel()

	// The cache directory does not exist before the first write.
	dir := filepath.Join(t.TempDir(), "cache")
	path := filepath.Join(dir, "names-v1-a.json")

	require.NoError(t, cachefile.Save(dir, path, map[string]int{"answers": 1}))
	require.NoError(t, cachefile.Save(dir, path, map[string]int{"answers": 2}))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, `{"answers": 2}`, string(raw))

	files, err := filepath.Glob(filepath.Join(dir, "*"))
	require.NoError(t, err)
	assert.Equal(t, []string{path}, files, "a write leaves no temporary file")
}

// TestSaveConcurrentWrites pins that each write of several processes to
// one cache file replaces the whole file and leaves no temporary file.
func TestSaveConcurrentWrites(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "names-v1-a.json")

	// Each write has its own length, so a file that mixes two writes does
	// not decode as either of them.
	var writes []string
	for i := range 8 {
		writes = append(writes, strings.Repeat("x", 4096*(i+1)))
	}

	var wg sync.WaitGroup

	for _, write := range writes {
		wg.Go(func() {
			assert.NoError(t, cachefile.Save(dir, path, write))
		})
	}

	wg.Wait()

	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	var got string

	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Contains(t, writes, got)

	files, err := filepath.Glob(filepath.Join(dir, "*"))
	require.NoError(t, err)
	assert.Equal(t, []string{path}, files)
}

// TestSaveFailedWrite pins that a write that fails removes its temporary
// file.
func TestSaveFailedWrite(t *testing.T) {
	t.Parallel()

	// A directory that holds a file stands at the path of the cache file,
	// so the rename of the temporary file fails.
	dir := t.TempDir()
	path := filepath.Join(dir, "target.json")
	require.NoError(t, os.Mkdir(path, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(path, "keep"), nil, 0o644))

	require.ErrorContains(t, cachefile.Save(dir, path, "v"), "writing cache file")

	files, err := filepath.Glob(filepath.Join(dir, "*"))
	require.NoError(t, err)
	assert.Equal(t, []string{path}, files)
}

// TestSaveUnwritableDirectory pins the error of a write whose cache
// directory the process cannot create.
func TestSaveUnwritableDirectory(t *testing.T) {
	t.Parallel()

	// A regular file stands where the cache directory belongs.
	dir := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(dir, nil, 0o644))

	err := cachefile.Save(dir, filepath.Join(dir, "names-v1-a.json"), "v")
	require.ErrorContains(t, err, "creating cache directory")
}

func TestRemoveOthers(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	keep := filepath.Join(dir, "names-v2-b.json")
	other := filepath.Join(dir, "defs-v1-a.json")

	// A directory that holds a file matches the pattern, and the remove
	// of such a directory fails.
	stuck := filepath.Join(dir, "names-v0-a.json")
	require.NoError(t, os.Mkdir(stuck, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stuck, "keep"), nil, 0o644))

	for _, file := range []string{keep, other, filepath.Join(dir, "names-v1-a.json"), filepath.Join(dir, "names-v2-a.json")} {
		require.NoError(t, os.WriteFile(file, []byte(`{}`), 0o644))
	}

	var buf bytes.Buffer

	log := slog.New(slog.NewJSONHandler(&buf, nil))
	cachefile.RemoveOthers(t.Context(), log, filepath.Join(dir, "names-v*.json"), keep)

	files, err := filepath.Glob(filepath.Join(dir, "*"))
	require.NoError(t, err)
	assert.Equal(t, []string{other, stuck, keep}, files, "the call removes each other file that matches the pattern")

	var record struct {
		Level string `json:"level"`
		Msg   string `json:"msg"`
		Path  string `json:"path"`
	}

	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
	assert.Equal(t, "WARN", record.Level)
	assert.Equal(t, "removing old cache file", record.Msg)
	assert.Equal(t, stuck, record.Path)
}
