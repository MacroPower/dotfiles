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
// host id file for one env. The directory is the standard
// mcp-kubectx state dir; the file lives alongside other per-host
// scoped artifacts as `host.id` or `guest.id` depending on the
// env tag.
func hostIDPath(forGuest bool) string {
	return filepath.Join(stateHomeDir(), envTag(forGuest)+".id")
}

// loadOrCreateHostID returns the stable 16-hex host identifier for
// the current user and env. The id is persisted at [hostIDPath]
// with mode 0600 to match the socket trust boundary; on first call
// the file is created atomically (tmp + rename) so a concurrent
// reader never observes a truncated or empty file. Persisted
// content that fails [validHostID] (empty, torn, or hand-edited)
// is regenerated rather than returned: [runHostSweep] rejects any
// other shape, so passing it through would permanently disable the
// sweep while resources keep getting labeled with it.
//
// The host id bounds the orphan sweep in [runHostSweep] to
// resources this host+env owns. Two operators running `serve`
// against a shared cluster never delete each other's resources
// because their id files differ. Host- and guest-side serves on
// the same machine keep separate ids (`host.id` vs `guest.id`)
// for the same reason: the state dir is shared through the Lima
// bind mount but each side can only dial its own env's sockets in
// [*handler.discoverLiveInstances], so a shared id would let one
// side's sweep classify the other side's live serves as orphans
// and delete their in-use ServiceAccounts.
func loadOrCreateHostID(forGuest bool) (string, error) {
	path := hostIDPath(forGuest)

	data, err := os.ReadFile(path)
	if err == nil {
		id := strings.TrimSpace(string(data))
		if validHostID(id) {
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
