package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const tokenFileName = "github_token"

// DefaultDataDir returns the directory used to persist the GitHub token,
// honoring XDG_DATA_HOME.
func DefaultDataDir() (string, error) {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "copilot-api-proxy"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "copilot-api-proxy"), nil
}

// LoadGitHubToken reads the persisted GitHub token from dir. A missing file
// returns an error wrapping [os.ErrNotExist].
func LoadGitHubToken(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, tokenFileName))
	if err != nil {
		return "", fmt.Errorf("read github token: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

// SaveGitHubToken writes tok to dir with owner-only permissions, creating the
// directory if needed.
func SaveGitHubToken(dir, tok string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	// MkdirAll and WriteFile only set perms when creating; tighten an existing
	// directory or file so the durable token is never left world-readable.
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("restrict data directory: %w", err)
	}
	path := filepath.Join(dir, tokenFileName)
	if err := os.WriteFile(path, []byte(strings.TrimSpace(tok)+"\n"), 0o600); err != nil {
		return fmt.Errorf("write github token: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("restrict github token: %w", err)
	}
	return nil
}
