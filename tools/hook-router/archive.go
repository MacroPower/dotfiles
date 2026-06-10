package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// compactionOutputTTL bounds how long an archived uncompacted-output
// file lives before [sweepCompactionOutputs] removes it on the next
// SessionStart. Seven days comfortably outlasts a single session, so a
// pointer left in context rarely outlives the file it names; a
// longer-lived session holding a stale pointer can simply re-run the
// command. Tunable.
const compactionOutputTTL = 7 * 24 * time.Hour

// compactionRemintRetries bounds the O_EXCL collision retries in
// [*OutputArchive.Annotate]. A collision needs two independent randHex
// draws to land the same 16-hex name in the same directory, so any
// positive bound is astronomically more than enough.
const compactionRemintRetries = 5

// OutputArchive persists the uncompacted content of a compacted Bash
// stream to its own file under dir, so a lossy compaction stays
// recoverable: [*OutputArchive.Annotate] writes the raw stream and
// returns the compacted text plus a one-line pointer naming the file,
// which the model reads only if it needs the dropped detail. Construct
// with [NewOutputArchive].
//
// A nil receiver or an empty dir is treated as disabled by
// [*OutputArchive.Empty], so handlers can gate on Empty() before
// touching any other method (mirrors [*Compactor.Empty]). [*OutputArchive.Dir]
// and [*OutputArchive.Annotate] are likewise nil-safe.
type OutputArchive struct {
	dir string
}

// NewOutputArchive builds an [*OutputArchive] writing to dir. An empty
// dir yields a disabled archive (so [*OutputArchive.Empty] reports
// true), matching the wrapper passing no --compaction-output-dir.
func NewOutputArchive(dir string) *OutputArchive {
	return &OutputArchive{dir: dir}
}

// Empty reports whether the archive would never write a file: a nil
// receiver or an empty dir (archiving disabled). The nil-receiver guard
// is load-bearing -- cfg.outputArchive is a nil *OutputArchive in the
// bare config{} literals across the handler tests, and
// [handlePostBashCompact] gates on Empty() before calling any other
// method, so those tests stay green without constructing an archive.
func (a *OutputArchive) Empty() bool {
	return a == nil || a.dir == ""
}

// Dir returns the directory archived files are written to, or "" for a
// nil receiver or a disabled archive. It is the single source of the
// sweep root for the SessionStart [sweepCompactionOutputs] call, so the
// dir string is never duplicated into config.
func (a *OutputArchive) Dir() string {
	if a == nil {
		return ""
	}

	return a.dir
}

// Annotate archives original (the stream as the hook received it, ANSI
// escapes and repeated lines intact) to its own file under the archive
// dir, then returns compacted with a one-line pointer marker appended
// and true. It returns ("", false) when archiving is disabled, when the
// pointer would not net-shorten the output, or when the file could not
// be durably written -- in every false case the caller keeps the full
// original stream, so a compaction is never surfaced without a recovery
// path and a pointer never dangles.
//
// sessionID and stream form the filename prefix; sessionID is external
// input and is sanitized ([sanitizeForFilename]) before it touches the
// path. Uniqueness comes from a [randHex] suffix written with O_EXCL.
func (a *OutputArchive) Annotate(
	sessionID, stream, original, compacted string,
	logger *slog.Logger,
) (string, bool) {
	if a.Empty() {
		return "", false
	}

	prefix := sanitizeForFilename(sessionID) + "-" + sanitizeForFilename(stream) + "-"

	// mint rolls a fresh fixed-width hex suffix and returns the candidate
	// path with its pointer marker. Every suffix is the same width, so
	// every path -- and every marker -- has the same length: the gate
	// below clears once and no remint can invalidate it. Reused for the
	// initial draw and each O_EXCL collision, so the filename layout lives
	// in exactly one place.
	mint := func() (path, marker string, ok bool) {
		suffix, err := randHex()
		if err != nil {
			logger.Warn("minting compaction output filename", slog.Any("error", err))
			return "", "", false
		}

		path = filepath.Join(a.dir, prefix+suffix+".log")

		return path, compactionPointerMarker(stream, path, len(original)), true
	}

	path, marker, ok := mint()
	if !ok {
		return "", false
	}

	// Gate before writing: a stream that would not net-shorten once its
	// pointer is appended writes no file (zero orphans). The marker length
	// is deterministic (see mint), so a later remint cannot invalidate
	// this already-cleared gate.
	if len(compacted)+1+len(marker) >= len(original) {
		return "", false
	}

	if err := os.MkdirAll(a.dir, 0o755); err != nil {
		logger.Warn("creating compaction output dir",
			slog.String("dir", a.dir),
			slog.Any("error", err),
		)

		return "", false
	}

	var f *os.File

	for attempt := 0; ; attempt++ {
		var err error

		f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			break
		}

		if !errors.Is(err, fs.ErrExist) || attempt >= compactionRemintRetries {
			logger.Warn("creating compaction output file",
				slog.String("path", path),
				slog.Any("error", err),
			)

			return "", false
		}

		// Collision: re-roll the hex (same width, so the gate still holds).
		path, marker, ok = mint()
		if !ok {
			return "", false
		}
	}

	if _, err := f.WriteString(original); err != nil {
		_ = f.Close()
		_ = os.Remove(path)

		logger.Warn("writing compaction output file",
			slog.String("path", path),
			slog.Any("error", err),
		)

		return "", false
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(path)

		logger.Warn("closing compaction output file",
			slog.String("path", path),
			slog.Any("error", err),
		)

		return "", false
	}

	return compacted + "\n" + marker, true
}

// compactionPointerMarker renders the one-line pointer appended to a
// compacted stream. It carries the same four-space indent and
// [hook-router: ...] namespace as [compactMarker] so the model
// recognizes it as injected metadata rather than command output.
//
// "uncompacted" means "before hook-router's compaction"; n is the byte
// length of the stream as the hook received it (already capped at the
// pre-hook BASH_MAX_OUTPUT_LENGTH), so the count never implies recovery
// of bytes Claude Code truncated before the hook ran. Reporting bytes
// (not lines) sidesteps the trailing-newline line-count off-by-one.
func compactionPointerMarker(stream, path string, n int) string {
	return fmt.Sprintf("    [hook-router: uncompacted %s saved to %s (%d bytes)]", stream, path, n)
}

// sanitizeForFilename maps every byte of s outside [A-Za-z0-9_-] to '_'
// so external input (session_id, stream) is safe as a single path
// element. An empty result becomes "nosession", and a final
// [filepath.Base] is a traversal backstop: no surviving byte can form a
// separator, but Base guarantees the result is one element regardless.
func sanitizeForFilename(s string) string {
	b := []byte(s)
	for i := range b {
		c := b[i]
		if (c >= 'A' && c <= 'Z') ||
			(c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') ||
			c == '_' || c == '-' {
			continue
		}

		b[i] = '_'
	}

	out := string(b)
	if out == "" {
		out = "nosession"
	}

	return filepath.Base(out)
}

// randHex returns 8 cryptographically random bytes as a fixed-width
// 16-character hex string. The fixed width is load-bearing for the
// [*OutputArchive.Annotate] gate: a remint must not change the path
// length.
func randHex() (string, error) {
	var buf [8]byte

	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}

	return hex.EncodeToString(buf[:]), nil
}

// sweepCompactionOutputs removes archived uncompacted-output files under
// dir whose mtime is older than ttl. Runs in [run]'s SessionStart arm
// (best-effort), independent of the store, so files outlive at most one
// extra session window.
// Mirrors [sweepKubectxDirs]: an empty dir or a missing dir
// ([fs.ErrNotExist]) is a silent no-op; a remove that races a concurrent
// sweep to [fs.ErrNotExist] is treated as success; other errors warn and
// continue so one bad entry does not abort the sweep.
func sweepCompactionOutputs(dir string, ttl time.Duration, logger *slog.Logger) {
	if dir == "" {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			logger.Debug("sweep compaction outputs: read dir",
				slog.String("dir", dir),
				slog.Any("error", err),
			)
		}

		return
	}

	for _, e := range entries {
		full := filepath.Join(dir, e.Name())

		info, err := os.Stat(full)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				logger.Warn("sweep compaction outputs: stat entry",
					slog.String("file", full),
					slog.Any("error", err),
				)
			}

			continue
		}

		if time.Since(info.ModTime()) <= ttl {
			continue
		}

		err = os.Remove(full)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			logger.Warn("sweep compaction outputs: remove",
				slog.String("file", full),
				slog.Any("error", err),
			)
		}
	}
}
