package statefile

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteSecure writes data to path with 0600 permissions, creating
// parent directories with 0700 as needed.
func WriteSecure(path string, data []byte) error {
	err := os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	err = os.WriteFile(path, data, 0o600)
	if err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// WriteAtomic writes data to path via a tmp + rename so a
// concurrent reader never observes a torn or zero-byte file.
// Parent dirs are not created here -- callers ensure the dir exists
// (or use [WriteSecure] when they want both behaviors). Mode is
// applied to the tmp file; rename preserves it.
//
//nolint:gosec // paths are caller-owned state files (operator flags / wrapper env), not untrusted input
func WriteAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"

	// Best-effort cleanup of any leftover tmp from a prior crash;
	// the WriteFile below would otherwise hit EEXIST on platforms
	// that surface it.
	_ = os.Remove(tmp) //nolint:errcheck // best-effort cleanup

	err := os.WriteFile(tmp, data, mode)
	if err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}

	err = os.Rename(tmp, path)
	if err != nil {
		_ = os.Remove(tmp) //nolint:errcheck // best-effort rollback
		return fmt.Errorf("rename: %w", err)
	}

	return nil
}

// SymlinkAtomic creates or replaces a symlink at path pointing to
// target. Parent dirs are created with 0o700. Replacement is atomic
// via tmp + rename, so a concurrent reader never observes a missing
// symlink.
func SymlinkAtomic(path, target string) error {
	err := os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	tmp := path + ".tmp"

	// Best-effort: ENOENT (no leftover) is fine; any other
	// failure surfaces below when os.Symlink hits EEXIST.
	_ = os.Remove(tmp) //nolint:errcheck // see comment

	err = os.Symlink(target, tmp)
	if err != nil {
		return fmt.Errorf("create symlink: %w", err)
	}

	err = os.Rename(tmp, path)
	if err != nil {
		// Best-effort rollback of the tmp symlink we just made.
		_ = os.Remove(tmp) //nolint:errcheck // see comment
		return fmt.Errorf("rename symlink: %w", err)
	}

	return nil
}
