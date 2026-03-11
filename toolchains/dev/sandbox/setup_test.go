package sandbox_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/toolchains/dev/sandbox"
)

func TestSetupDev(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()
	historyDir := t.TempDir()
	claudeStateDir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, ".local", "share"), 0o755))

	claudeJSON := filepath.Join(homeDir, ".claude.json")
	require.NoError(t, os.WriteFile(claudeJSON, []byte(`{"key":"val"}`), 0o644))

	atuinDir := filepath.Join(homeDir, ".config", "atuin")
	require.NoError(t, os.MkdirAll(atuinDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(atuinDir, "config.toml"),
		[]byte("foo = true\nsystemd_socket = true\nbar = 1\n"), 0o644))

	require.NoError(t, sandbox.SetupDevWithPaths(homeDir, historyDir, claudeStateDir))

	// Fish history symlink.
	target, err := os.Readlink(filepath.Join(homeDir, ".local", "share", "fish", "fish_history"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(historyDir, "fish_history"), target)

	// Claude state file copied.
	stateData, err := os.ReadFile(filepath.Join(claudeStateDir, "claude.json"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"key":"val"}`, string(stateData))

	// Claude.json is now a symlink.
	claudeTarget, err := os.Readlink(claudeJSON)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(claudeStateDir, "claude.json"), claudeTarget)

	// Atuin config patched.
	atuinData, err := os.ReadFile(filepath.Join(atuinDir, "config.toml"))
	require.NoError(t, err)
	assert.NotContains(t, string(atuinData), "systemd_socket = true")
	assert.Contains(t, string(atuinData), "systemd_socket = false")

	// Atuin data dir created.
	_, err = os.Stat(filepath.Join(homeDir, ".local", "share", "atuin"))
	assert.NoError(t, err)
}

func TestSetupDevIdempotent(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()
	historyDir := t.TempDir()
	claudeStateDir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, ".local", "share"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, ".claude.json"), []byte(`{}`), 0o644))

	require.NoError(t, sandbox.SetupDevWithPaths(homeDir, historyDir, claudeStateDir))
	require.NoError(t, sandbox.SetupDevWithPaths(homeDir, historyDir, claudeStateDir))
}

func TestSetupUserPasswdGroup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	passwdPath := filepath.Join(dir, "passwd")
	groupPath := filepath.Join(dir, "group")

	require.NoError(t, os.WriteFile(passwdPath, []byte("root:x:0:0::/root:/bin/sh\n"), 0o644))
	require.NoError(t, os.WriteFile(groupPath, []byte("root:x:0:\n"), 0o644))

	homeDir := filepath.Join(dir, "home", "testuser")
	require.NoError(t, os.MkdirAll(homeDir, 0o755))

	uid := fmt.Sprintf("%d", os.Getuid())
	gid := fmt.Sprintf("%d", os.Getgid())

	require.NoError(t, sandbox.SetupUserWithPaths(passwdPath, groupPath, "testuser", uid, gid, homeDir))

	passwdData, err := os.ReadFile(passwdPath)
	require.NoError(t, err)

	want := fmt.Sprintf("testuser:x:%s:%s::%s:/bin/sh", uid, gid, homeDir)
	assert.Contains(t, string(passwdData), want)

	groupData, err := os.ReadFile(groupPath)
	require.NoError(t, err)
	assert.Contains(t, string(groupData), "testuser:x:"+gid+":")
}
