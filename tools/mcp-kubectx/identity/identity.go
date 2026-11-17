package identity

import (
	"crypto/rand"
	"crypto/sha256"
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
// the same error.
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
// LabelSelector against injection. Any other value is either a
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

// digestDomain prefixes each host id that [Digest] hashes. So the
// digest of a host id differs from each other SHA-256 hash of the
// same bytes.
const digestDomain = "mcp-kubectx/host-id\x00"

// Digest returns the value that stands for hostID on the cluster
// and in the provision records. The value is the first 16 hex
// characters of a SHA-256 hash of hostID, so [Valid] accepts it.
//
// The `host` subcommands take a host id and derive its digest. The
// labels of a cluster and the names of the provision records carry
// only the digest. A process that reads them, such as a Lima guest
// with a `view` token, cannot find the host id from its digest. So
// it can act on the resources of a host id only when it holds the
// host id itself.
func Digest(hostID string) string {
	sum := sha256.Sum256([]byte(digestDomain + hostID))

	return hex.EncodeToString(sum[:8])
}

// HostPath returns the absolute path of the per-user persistent host
// id file for one env. The file lies in [statedir.RunDir] as
// `host.key` or `guest.key`, depending on the env tag. workmux
// mounts no part of that directory into a Lima guest. A guest
// reaches the host only through the `host` subcommands, and none of
// them writes a file at a path that its caller picks. So no guest
// can read or rewrite the id of the host.
//
// Builds of mcp-kubectx before [Digest] kept the id in `host.id` or
// `guest.id` and wrote the plain id on the label of each resource.
// Every guest with a `view` token can read those labels. So HostPath
// names a file of another name, and the first serve of each env
// mints an id that no label has shown. [LoadOrCreateHost] never
// reads the old file, which holds the plain id that the labels of
// those builds carry.
func HostPath(forGuest bool) string {
	return filepath.Join(statedir.RunDir(), statedir.EnvTag(forGuest)+".key")
}

// LoadOrCreateHost returns the stable 16-hex host identifier for the
// current user and env. The id is persisted at [HostPath] with mode
// 0600 to match the socket trust boundary; on first call the file is
// created atomically (tmp + rename) so a concurrent reader never
// observes a truncated or empty file. Persisted content that fails
// [Valid] (empty, torn, or hand-edited) is regenerated rather than
// returned: the sweep rejects any other value, so passing it through
// would permanently disable the sweep while resources keep getting
// labeled with it.
//
// The host id bounds the orphan sweep to resources this host+env
// owns. The `host` subcommands act on the resources of the host id
// that their caller passes, and the cluster and the provision
// records hold only its [Digest]. So only a holder of the id file
// can act on those resources. Two operators running `serve` against
// a shared cluster never delete each other's resources, and no Lima
// guest deletes the resources of host-side serves. Host- and
// guest-side serves on the same machine keep separate ids (`host.key`
// vs `guest.key`) for the reason laid out in the package doc.
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
