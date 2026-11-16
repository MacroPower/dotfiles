package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/identity"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/statefile"
)

// Sentinel errors for the provision records.
var (
	// ErrInvalidID rejects a record whose host id or instance id
	// does not match the format that [identity.New] produces. Both
	// ids become part of a file name, so the ledger refuses any
	// other value before the value reaches the filesystem.
	ErrInvalidID = errors.New("provision record id must be 16 lowercase hex characters")
	// ErrAdd wraps a failure to write a record file.
	ErrAdd = errors.New("add provision record")
	// ErrList wraps a failure to read the ledger directory.
	ErrList = errors.New("list provision records")
	// ErrRemove wraps a failure to remove a record file.
	ErrRemove = errors.New("remove provision record")
)

// recordExt is the suffix of every record file.
const recordExt = ".json"

// Record says that the `serve` instance InstanceID of the host
// HostID provisioned, or was about to provision, one
// ServiceAccount. The fields Kubeconfig, Context, Namespace, and
// ServiceAccount locate the ServiceAccount. Written says when the
// record came to the ledger.
type Record struct {
	// HostID is the persistent host id of the `serve` that owns
	// the ServiceAccount. It comes from the name of the record
	// file.
	HostID string `json:"-"`

	// InstanceID is the instance id of the `serve` that owns the
	// ServiceAccount. It comes from the name of the record file.
	InstanceID string `json:"-"`

	// Kubeconfig is the absolute path of the kubeconfig file that
	// defines Context.
	Kubeconfig string `json:"kubeconfig"`

	// Context is the name of the kubeconfig context of the cluster
	// that holds the ServiceAccount.
	Context string `json:"context"`

	// Namespace is the namespace of the ServiceAccount.
	Namespace string `json:"namespace"`

	// ServiceAccount is the name of the ServiceAccount.
	ServiceAccount string `json:"serviceAccount"`

	// ClusterScoped says that the binding of the ServiceAccount is
	// a ClusterRoleBinding. When it is false, the binding is a
	// RoleBinding in Namespace.
	ClusterScoped bool `json:"clusterScoped"`

	// Written is the time at which `host select` wrote the record,
	// on the clock of the host. The orphan sweep compares it with
	// the time of its set of live instances. It keeps each record
	// that `host select` wrote after that time, because the `serve`
	// of such a record might be alive but missing from the set. The
	// zero time lies before every set.
	Written time.Time `json:"written"`
}

// SameServiceAccount reports whether r and other locate the same
// ServiceAccount. The comparison ignores the owner and the time of
// each record. A caller that knows only the location of a
// ServiceAccount uses it to find every record of that
// ServiceAccount.
func (r Record) SameServiceAccount(other Record) bool {
	return r.Kubeconfig == other.Kubeconfig &&
		r.Context == other.Context &&
		r.Namespace == other.Namespace &&
		r.ServiceAccount == other.ServiceAccount
}

// Ledger reads and writes the provision records of one directory.
// Create instances with [New].
type Ledger struct {
	dir string
}

// New returns a [*Ledger] that keeps its records in dir. The
// directory does not have to exist, because [Ledger.Add] creates
// it.
func New(dir string) *Ledger {
	return &Ledger{dir: dir}
}

// Add writes the record r. The owner and the location of a record
// name its file. So a record with the owner and the location of a
// record that the ledger already holds replaces that record and
// leaves one file. The write goes through a temporary file and a
// rename, so [Ledger.List] never reads a partial record.
//
// Add returns [ErrInvalidID] when the host id or the instance id of
// r is malformed, and an error that wraps [ErrAdd] when the write
// fails.
func (l *Ledger) Add(r Record) error {
	name, err := fileName(r)
	if err != nil {
		return err
	}

	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAdd, err)
	}

	err = os.MkdirAll(l.dir, 0o700)
	if err != nil {
		return fmt.Errorf("%w: create directory: %w", ErrAdd, err)
	}

	err = statefile.WriteAtomic(filepath.Join(l.dir, name), data, 0o600)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAdd, err)
	}

	return nil
}

// List returns every record of the ledger, sorted by file name. A
// ledger directory that does not exist holds no records. List skips
// every file whose name or content is not a record, and it skips a
// file that another process removes during the call.
func (l *Ledger) List() ([]Record, error) {
	entries, err := os.ReadDir(l.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrList, err)
	}

	var records []Record

	for _, entry := range entries {
		r, ok := l.read(entry.Name())
		if ok {
			records = append(records, r)
		}
	}

	return records, nil
}

// Remove deletes the record r. Removing a record that the ledger
// does not hold succeeds.
//
// Remove returns [ErrInvalidID] when the host id or the instance id
// of r is malformed, and an error that wraps [ErrRemove] when the
// delete fails.
func (l *Ledger) Remove(r Record) error {
	name, err := fileName(r)
	if err != nil {
		return err
	}

	err = os.Remove(filepath.Join(l.dir, name))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %w", ErrRemove, err)
	}

	return nil
}

// read loads the record in the file name. It reports false when the
// name or the content of the file is not a record.
func (l *Ledger) read(name string) (Record, bool) {
	base, ok := strings.CutSuffix(name, recordExt)
	if !ok {
		return Record{}, false
	}

	parts := strings.Split(base, ".")
	if len(parts) != 3 {
		return Record{}, false
	}

	//nolint:gosec // name comes from a listing of the ledger directory, so it holds no path separator
	data, err := os.ReadFile(filepath.Join(l.dir, name))
	if err != nil {
		return Record{}, false
	}

	r := Record{}

	err = json.Unmarshal(data, &r)
	if err != nil {
		return Record{}, false
	}

	r.HostID, r.InstanceID = parts[0], parts[1]

	// The name must be the one that [Ledger.Add] gives the record,
	// so that [Ledger.Remove] deletes the same file. The comparison
	// also rejects malformed ids and a key that does not belong to
	// the content.
	want, err := fileName(r)
	if err != nil || want != name {
		return Record{}, false
	}

	return r, true
}

// fileName returns the name of the file that holds r.
func fileName(r Record) (string, error) {
	if !identity.Valid(r.HostID) || !identity.Valid(r.InstanceID) {
		return "", fmt.Errorf("%w: host %q, instance %q", ErrInvalidID, r.HostID, r.InstanceID)
	}

	location := strings.Join([]string{r.Kubeconfig, r.Context, r.Namespace, r.ServiceAccount}, "\x00")
	sum := sha256.Sum256([]byte(location))

	return r.HostID + "." + r.InstanceID + "." + hex.EncodeToString(sum[:8]) + recordExt, nil
}
