package cachefile

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// Save writes v as JSON to the cache file path in dir. It creates dir
// when dir does not exist. It writes a temporary file of its own in dir
// and renames it, so a reader never sees half a file. Several processes
// can share dir, and the writes of two processes never mix in one
// temporary file. A write that does not succeed removes its temporary
// file.
func Save(dir, path string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encoding cache: %w", err)
	}

	err = os.MkdirAll(dir, 0o755)
	if err != nil {
		return fmt.Errorf("creating cache directory: %w", err)
	}

	f, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating cache file: %w", err)
	}

	tmp := f.Name()

	_, err = f.Write(raw)

	err = cmp.Or(err, f.Close())
	if err == nil {
		err = os.Rename(tmp, path)
	}

	if err != nil {
		_ = os.Remove(tmp) //nolint:errcheck // The write error is the one to report.

		return fmt.Errorf("writing cache file: %w", err)
	}

	return nil
}

// RemoveOthers deletes every file that matches the glob pattern, except
// the file keep. A file that stays costs only disk space, so RemoveOthers
// logs a failure and continues.
func RemoveOthers(ctx context.Context, log *slog.Logger, pattern, keep string) {
	old, err := filepath.Glob(pattern)
	if err != nil {
		return
	}

	for _, file := range old {
		if file == keep {
			continue
		}

		err := os.Remove(file)
		if err != nil {
			log.WarnContext(ctx, "removing old cache file",
				slog.String("path", file),
				slog.Any("error", err),
			)
		}
	}
}
