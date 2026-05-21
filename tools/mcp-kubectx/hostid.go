package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Sentinel errors for host-id persistence.
var (
	// ErrLoadHostID wraps any failure to read or write the
	// persisted host id file.
	ErrLoadHostID = errors.New("load host id")
)

// hostIDPath returns the absolute path of the per-user persistent
// host id file. The directory is the standard mcp-kubectx state
// dir; the file lives alongside other per-host scoped artifacts.
func hostIDPath() string {
	return filepath.Join(stateHomeDir(), "host.id")
}

// loadOrCreateHostID returns the stable 16-hex host identifier for
// the current user. The id is persisted at [hostIDPath] with mode
// 0600 to match the socket trust boundary; on first call the file
// is created atomically (tmp + rename) so a concurrent reader
// never observes a truncated or empty file.
//
// The host id bounds the orphan sweep in [runHostSweep] to
// resources this host owns. Two operators running `serve` against
// a shared cluster never delete each other's resources because
// their host.id files differ.
func loadOrCreateHostID() (string, error) {
	path := hostIDPath()

	data, err := os.ReadFile(path)
	if err == nil {
		id := strings.TrimSpace(string(data))
		if id != "" {
			return id, nil
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("%w: read: %w", ErrLoadHostID, err)
	}

	id, err := randomHex(8)
	if err != nil {
		return "", fmt.Errorf("%w: generate: %w", ErrLoadHostID, err)
	}

	err = os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		return "", fmt.Errorf("%w: create directory: %w", ErrLoadHostID, err)
	}

	err = writeFileAtomic(path, []byte(id), 0o600)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrLoadHostID, err)
	}

	return id, nil
}
