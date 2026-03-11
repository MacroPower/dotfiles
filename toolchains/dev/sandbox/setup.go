package sandbox

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// SetupDev creates fish history symlinks, persists claude.json to the
// cache volume, disables atuin systemd socket activation, and ensures the
// atuin data directory exists. Intended for build-time container setup.
func SetupDev() error {
	return SetupDevWithPaths(HomeDir, "/commandhistory", "/claude-state")
}

// SetupDevWithPaths is the parameterized form of [SetupDev], accepting
// custom directory paths for testing.
func SetupDevWithPaths(homeDir, historyDir, claudeStateDir string) error {
	fishDataDir := filepath.Join(homeDir, ".local", "share", "fish")

	err := os.MkdirAll(fishDataDir, 0o755)
	if err != nil {
		return fmt.Errorf("creating fish data dir: %w", err)
	}

	fishHistLink := filepath.Join(fishDataDir, "fish_history")
	rmErr := os.Remove(fishHistLink)
	if rmErr != nil && !os.IsNotExist(rmErr) {
		slog.Debug("removing fish history link",
			slog.String("path", fishHistLink),
			slog.Any("err", rmErr),
		)
	}

	err = os.Symlink(filepath.Join(historyDir, "fish_history"), fishHistLink)
	if err != nil {
		return fmt.Errorf("creating fish history symlink: %w", err)
	}

	claudeJSON := filepath.Join(homeDir, ".claude.json")
	claudeStatePath := filepath.Join(claudeStateDir, "claude.json")

	_, statErr := os.Stat(claudeStatePath)
	if os.IsNotExist(statErr) {
		src, readErr := os.ReadFile(claudeJSON)
		if readErr == nil {
			//nolint:gosec // G703: path derived from home dir.
			writeErr := os.WriteFile(claudeStatePath, src, 0o644)
			if writeErr != nil {
				return fmt.Errorf("writing claude state: %w", writeErr)
			}
		}
	}

	rmErr = os.Remove(claudeJSON)
	if rmErr != nil && !os.IsNotExist(rmErr) {
		slog.Debug("removing claude.json",
			slog.String("path", claudeJSON),
			slog.Any("err", rmErr),
		)
	}

	err = os.Symlink(claudeStatePath, claudeJSON)
	if err != nil {
		return fmt.Errorf("creating claude.json symlink: %w", err)
	}

	atuinDataDir := filepath.Join(homeDir, ".local", "share", "atuin")

	err = os.MkdirAll(atuinDataDir, 0o755)
	if err != nil {
		return fmt.Errorf("creating atuin data dir: %w", err)
	}

	atuinConfig := filepath.Join(homeDir, ".config", "atuin", "config.toml")

	data, err := os.ReadFile(atuinConfig)
	if err == nil {
		patched := strings.ReplaceAll(string(data), "systemd_socket = true", "systemd_socket = false")
		if patched != string(data) {
			//nolint:gosec // G703: path derived from home dir.
			err := os.WriteFile(atuinConfig, []byte(patched), 0o644)
			if err != nil {
				return fmt.Errorf("patching atuin config: %w", err)
			}
		}
	}

	return nil
}

// SetupUser creates the non-root sandbox user and group by appending
// entries to /etc/passwd and /etc/group, then recursively chowns the
// home directory. The nix image lacks useradd/adduser.
func SetupUser() error {
	return SetupUserWithPaths("/etc/passwd", "/etc/group", Username, UID, GID, HomeDir)
}

// SetupUserWithPaths is the parameterized form of [SetupUser], accepting
// custom file paths for testing.
func SetupUserWithPaths(passwdPath, groupPath, username, uid, gid, homeDir string) error {
	passwdEntry := fmt.Sprintf("%s:x:%s:%s::%s:/bin/sh\n", username, uid, gid, homeDir)

	err := appendToFile(passwdPath, passwdEntry)
	if err != nil {
		return fmt.Errorf("writing passwd: %w", err)
	}

	groupEntry := fmt.Sprintf("%s:x:%s:\n", username, gid)

	err = appendToFile(groupPath, groupEntry)
	if err != nil {
		return fmt.Errorf("writing group: %w", err)
	}

	uidNum, gidNum := mustAtoi(uid), mustAtoi(gid)

	err = filepath.WalkDir(homeDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		return os.Lchown(path, uidNum, gidNum)
	})
	if err != nil {
		return fmt.Errorf("chowning home dir: %w", err)
	}

	return nil
}

func appendToFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}

	defer func() {
		err := f.Close()
		if err != nil {
			slog.Debug("closing file", slog.String("path", path), slog.Any("err", err))
		}
	}()

	_, err = f.WriteString(content)
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}

func mustAtoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}

	return n
}
