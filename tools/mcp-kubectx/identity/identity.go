package identity

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/statedir"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/statefile"
)

// ErrLoadHost wraps any failure to read or write the persisted host
// id file.
var ErrLoadHost = errors.New("load host id")

// New returns a fresh random 16-hex identifier. Wide enough to
// prevent intra-host collisions across long-running operators.
func New() (string, error) {
	return RandomHex(8)
}

// RandomHex returns a hex-encoded random string with the given
// number of bytes. Shared by [New] and the ServiceAccount name
// suffix so both pull from the same crypto/rand source and surface
// a consistent error shape.
func RandomHex(n int) (string, error) {
	b := make([]byte, n)

	_, err := rand.Read(b)
	if err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}

	return hex.EncodeToString(b), nil
}

// Valid reports whether s matches the format [New] produces: exactly
// 16 lowercase hex characters. The check guards the sweep's
// LabelSelector against injection -- any other shape is either a
// typo or a hand-crafted value with selector metacharacters in it.
func Valid(s string) bool {
	if len(s) != 16 {
		return false
	}

	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}

	return true
}

// HostPath returns the absolute path of the per-user persistent host
// id file for one env. The directory is the standard mcp-kubectx
// state dir; the file lives alongside other per-host scoped
// artifacts as `host.id` or `guest.id` depending on the env tag.
func HostPath(forGuest bool) string {
	return filepath.Join(statedir.Dir(), statedir.EnvTag(forGuest)+".id")
}

// LoadOrCreateHost returns the stable 16-hex host identifier for the
// current user and env. The id is persisted at [HostPath] with mode
// 0600 to match the socket trust boundary; on first call the file is
// created atomically (tmp + rename) so a concurrent reader never
// observes a truncated or empty file. Persisted content that fails
// [Valid] (empty, torn, or hand-edited) is regenerated rather than
// returned: the sweep rejects any other shape, so passing it through
// would permanently disable the sweep while resources keep getting
// labeled with it.
//
// The host id bounds the orphan sweep to resources this host+env
// owns. Two operators running `serve` against a shared cluster never
// delete each other's resources because their id files differ.
// Host- and guest-side serves on the same machine keep separate ids
// (`host.id` vs `guest.id`) for the reason laid out in the package
// doc.
func LoadOrCreateHost(forGuest bool) (string, error) {
	path := HostPath(forGuest)

	data, err := os.ReadFile(path)
	if err == nil {
		id := strings.TrimSpace(string(data))
		if Valid(id) {
			return id, nil
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("%w: read: %w", ErrLoadHost, err)
	}

	id, err := New()
	if err != nil {
		return "", fmt.Errorf("%w: generate: %w", ErrLoadHost, err)
	}

	err = os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		return "", fmt.Errorf("%w: create directory: %w", ErrLoadHost, err)
	}

	err = statefile.WriteAtomic(path, []byte(id), 0o600)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrLoadHost, err)
	}

	return id, nil
}
