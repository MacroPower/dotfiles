package statefile

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteSecure writes data to path with mode 0600. It first creates
// each missing parent directory with mode 0700. Then it replaces the
// file at path as [WriteAtomic] does, so it never writes through a
// symlink at path.
func WriteSecure(path string, data []byte) error {
	err := os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	return WriteAtomic(path, data, 0o600)
}

// WriteAtomic writes data to path via a tmp + rename so a
// concurrent reader never observes a torn or zero-byte file.
// WriteAtomic does not create the parent directory. The caller
// creates it, or calls [WriteSecure], which creates it.
//
// WriteAtomic creates the tmp file with O_EXCL, under a random name
// in the directory of path. So the data never goes through a symlink
// that another process plants in that directory, at path or at any
// other name. The rename replaces a symlink at path with the new
// file. WriteAtomic sets mode on the open tmp file, so the umask does
// not change it. A crash between the create and the rename leaves
// the tmp file behind, under a name that ends in .tmp.
//
//nolint:gosec // paths are caller-owned state files (operator flags / wrapper env), not untrusted input
func WriteAtomic(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}

	tmp := f.Name()

	err = fill(f, data, mode)
	if err != nil {
		_ = os.Remove(tmp) //nolint:errcheck // best-effort rollback
		return err
	}

	err = os.Rename(tmp, path)
	if err != nil {
		_ = os.Remove(tmp) //nolint:errcheck // best-effort rollback
		return fmt.Errorf("rename: %w", err)
	}

	return nil
}

// fill writes data to f, sets the mode of f, and closes f. It works
// on the open file, never on its path.
func fill(f *os.File, data []byte, mode os.FileMode) error {
	_, err := f.Write(data)
	if err == nil {
		err = f.Chmod(mode)
	}

	closeErr := f.Close()
	if err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}

	if closeErr != nil {
		return fmt.Errorf("close tmp: %w", closeErr)
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

	// Best-effort: ENOENT (no leftover) is fine. Any other
	// failure surfaces below when os.Symlink hits EEXIST.
	_ = os.Remove(tmp) //nolint:errcheck // see comment

	err = os.Symlink(target, tmp)
	if err != nil {
		return fmt.Errorf("create symlink: %w", err)
	}

	err = os.Rename(tmp, path)
	if err != nil {
		// Best-effort rollback of the tmp symlink that this call made.
		_ = os.Remove(tmp) //nolint:errcheck // see comment
		return fmt.Errorf("rename symlink: %w", err)
	}

	return nil
}
