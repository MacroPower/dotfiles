package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// lockPollInterval is how long [lockFile] sleeps between two tries to
// take a lock that another process holds.
const lockPollInterval = 10 * time.Millisecond

// lockFile opens filePath with flag and takes an exclusive flock on the
// open file. Closing the returned file releases the lock.
//
// The hooks of the calls in one message can work on one file at the
// same time. In the main conversation each hook runs in its own process
// in the background. The hooks of a message wake together when Claude
// Code writes the transcript entries of that message. Each step that
// reads the file and writes it back holds the lock, so the restore of
// the escapes and the formatter take turns. One hook then cannot write
// back text that another hook has not restored yet.
//
// A writer that replaced the file through a rename left the lock on the
// file that the path named before. The function then locks the file
// that the path names now. Its error wraps [io/fs.ErrNotExist] when the
// path names no file. It wraps the error of ctx when ctx ends before
// the lock is free.
func lockFile(ctx context.Context, filePath string, flag int) (*os.File, error) {
	for {
		file, err := os.OpenFile(filePath, flag, 0)
		if err != nil {
			return nil, fmt.Errorf("opening file to lock: %w", err)
		}

		current, err := lockOpenFile(ctx, file, filePath)
		if err == nil && current {
			return file, nil
		}

		closeErr := file.Close()
		if err != nil {
			return nil, err
		}

		if closeErr != nil {
			return nil, fmt.Errorf("closing replaced file: %w", closeErr)
		}
	}
}

// lockOpenFile takes the lock on file and reports whether filePath
// still names file.
func lockOpenFile(ctx context.Context, file *os.File, filePath string) (bool, error) {
	err := flock(ctx, file)
	if err != nil {
		return false, err
	}

	held, err := file.Stat()
	if err != nil {
		return false, fmt.Errorf("reading locked file: %w", err)
	}

	named, err := os.Stat(filePath)
	if err != nil {
		return false, fmt.Errorf("reading file after lock: %w", err)
	}

	return os.SameFile(held, named), nil
}

// flock takes an exclusive flock on file. It tries every
// [lockPollInterval] until the lock is free or ctx ends. A lock that
// never blocks cannot leave the call stuck in the kernel past ctx.
func flock(ctx context.Context, file *os.File) error {
	conn, err := file.SyscallConn()
	if err != nil {
		return fmt.Errorf("locking file: %w", err)
	}

	for {
		var lockErr error

		err = conn.Control(func(fd uintptr) {
			handle := int(fd) //nolint:gosec // a file descriptor fits in an int
			lockErr = syscall.Flock(handle, syscall.LOCK_EX|syscall.LOCK_NB)
		})
		if err != nil {
			return fmt.Errorf("locking file: %w", err)
		}

		if lockErr == nil {
			return nil
		}

		if !errors.Is(lockErr, syscall.EWOULDBLOCK) && !errors.Is(lockErr, syscall.EINTR) {
			return fmt.Errorf("locking file: %w", lockErr)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for the file lock: %w", ctx.Err())
		case <-time.After(lockPollInterval):
		}
	}
}
