package main

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/formatter"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/linter"
)

func TestLockFile(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		// change runs while the holder has the lock and the second
		// lockFile waits for it.
		change func(t *testing.T, path string)
		// want is the text that the second lockFile reads through the
		// file it locked.
		want string
		err  error
	}{
		"waits for the holder": {
			want: "first\n",
		},
		"locks the file that replaced the locked one": {
			change: func(t *testing.T, path string) {
				t.Helper()

				next := path + ".tmp"
				require.NoError(t, os.WriteFile(next, []byte("second\n"), 0o644))
				require.NoError(t, os.Rename(next, path))
			},
			want: "second\n",
		},
		"reports a file that the holder removed": {
			change: func(t *testing.T, path string) {
				t.Helper()

				require.NoError(t, os.Remove(path))
			},
			err: fs.ErrNotExist,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "quote.go")
			require.NoError(t, os.WriteFile(path, []byte("first\n"), 0o644))

			holder, err := lockFile(t.Context(), path, os.O_RDWR)
			require.NoError(t, err)

			type result struct {
				text string
				err  error
			}

			done := make(chan result, 1)

			go func() {
				file, err := lockFile(t.Context(), path, os.O_RDONLY)
				if err != nil {
					done <- result{err: err}
					return
				}

				text, err := io.ReadAll(file)
				done <- result{text: string(text), err: err}

				assert.NoError(t, file.Close())
			}()

			time.Sleep(5 * lockPollInterval)

			select {
			case got := <-done:
				require.Fail(t, "lockFile returned while another file held the lock", "%+v", got)
			default:
			}

			if tc.change != nil {
				tc.change(t, path)
			}

			require.NoError(t, holder.Close())

			got := <-done
			if tc.err != nil {
				require.ErrorIs(t, got.err, tc.err)
				return
			}

			require.NoError(t, got.err)
			assert.Equal(t, tc.want, got.text)
		})
	}
}

func TestLockFileStopsWhenTheContextEnds(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "quote.go")
	require.NoError(t, os.WriteFile(path, []byte("first\n"), 0o644))

	holder, err := lockFile(t.Context(), path, os.O_RDWR)
	require.NoError(t, err)

	defer func() { assert.NoError(t, holder.Close()) }()

	ctx, cancel := context.WithTimeout(t.Context(), 3*lockPollInterval)
	defer cancel()

	_, err = lockFile(ctx, path, os.O_RDWR)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestLockFileOfAMissingFile(t *testing.T) {
	t.Parallel()

	_, err := lockFile(t.Context(), filepath.Join(t.TempDir(), "missing.go"), os.O_RDWR)
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestHandlePostFileWriteFormatsUnderTheLock(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	target := filepath.Join(tmp, "doc.md")
	require.NoError(t, os.WriteFile(target, []byte("raw\n"), 0o644))

	rule := formatter.Rule{
		PathGlob: filepath.Join(tmp, "*.md"),
		Command:  []string{"sh", "-c", `printf 'formatted\n' > "$1"`, "sh"},
	}
	cfg := config{formatterRules: formatter.New([]formatter.Rule{rule}), linterRules: linter.New(nil)}
	input := escapePayload(t, "", "", "", "Write", map[string]any{"file_path": target, "content": "raw\n"}, nil)

	// The holder stands for the restore in the hook of another call.
	holder, err := lockFile(t.Context(), target, os.O_RDWR)
	require.NoError(t, err)

	done := make(chan error, 1)

	go func() {
		done <- handlePostFileWrite(t.Context(), input, cfg, slog.New(slog.DiscardHandler))
	}()

	time.Sleep(5 * lockPollInterval)

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "raw\n", string(got), "the formatter waits for the lock")

	require.NoError(t, holder.Close())
	require.NoError(t, <-done)

	got, err = os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "formatted\n", string(got))
}
