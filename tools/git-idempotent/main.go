package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

var (
	// ErrNoSubcommand is returned when no subcommand is provided.
	ErrNoSubcommand = errors.New("no subcommand given")

	// ErrUnknownSubcommand is returned when an unrecognized subcommand is provided.
	ErrUnknownSubcommand = errors.New("unknown subcommand")

	// ErrUsage is returned when required positional arguments are missing.
	ErrUsage = errors.New("usage: git-idempotent clone [flags] [--] <url> <dest>")

	// valueFlags lists git-clone flags that consume the next argument as a value.
	valueFlags = map[string]bool{
		"--depth": true, "--branch": true, "-b": true,
		"--origin": true, "-o": true, "--template": true,
		"--reference": true, "--reference-if-able": true,
		"--config": true, "-c": true, "--jobs": true, "-j": true,
		"--shallow-since": true, "--shallow-exclude": true,
		"--filter": true, "--separate-git-dir": true,
		"--server-option": true, "--bundle-uri": true,
	}
)

func main() {
	err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-idempotent: %v\n", err)
		os.Exit(1)
	}
}

func parseArgs(args []string) ([]string, []string) {
	var flags, positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)

			return flags, positional
		}

		if arg != "" && arg[0] == '-' {
			flags = append(flags, arg)
			if valueFlags[arg] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		} else {
			positional = append(positional, arg)
		}
	}

	return flags, positional
}

func run(args []string) error {
	if len(args) == 0 {
		return ErrNoSubcommand
	}

	subcmd := args[0]

	switch subcmd {
	case "clone":
		return runClone(args[1:])
	default:
		return fmt.Errorf("%w: %s", ErrUnknownSubcommand, subcmd)
	}
}

func runClone(args []string) error {
	flags, positional := parseArgs(args)

	if len(positional) < 2 {
		return ErrUsage
	}

	url := positional[0]
	dest := positional[1]

	// Ensure parent directory exists.
	err := os.MkdirAll(filepath.Dir(dest), 0o755) //nolint:gosec // G703: dest is a user-provided CLI arg.
	if err != nil {
		return fmt.Errorf("creating parent directory: %w", err)
	}

	// Acquire file lock.
	lockPath := dest + ".lock"

	lockFile, err := os.Create(lockPath) //nolint:gosec // G703: dest is a user-provided CLI arg.
	if err != nil {
		return fmt.Errorf("creating lock file: %w", err)
	}

	defer func() {
		closeErr := lockFile.Close()
		if closeErr != nil {
			slog.Warn("closing lock file", slog.Any("error", closeErr))
		}
	}()

	defer func() {
		//nolint:gosec // G703: dest is a user-provided CLI arg.
		removeErr := os.Remove(lockPath)
		if removeErr != nil {
			slog.Warn("removing lock file", slog.Any("error", removeErr))
		}
	}()

	//nolint:gosec // G115: Fd fits in int on all supported platforms.
	err = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX)
	if err != nil {
		return fmt.Errorf("acquiring lock: %w", err)
	}

	// Clone or pull.
	gitDir := filepath.Join(dest, ".git")

	ctx := context.Background()

	//nolint:gosec // G703: dest is a user-provided CLI arg.
	info, statErr := os.Stat(gitDir)
	if statErr == nil && info.IsDir() {
		//nolint:gosec // G204: dest is a user-provided CLI arg.
		cmd := exec.CommandContext(ctx, "git", "-C", dest, "pull", "--ff-only", "-q")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		// Pull failure is non-fatal (e.g. detached HEAD, dirty worktree).
		pullErr := cmd.Run()
		if pullErr != nil {
			slog.Warn("pulling latest changes", slog.Any("error", pullErr))
		}
	} else {
		cloneArgs := []string{"clone", "-q"}
		cloneArgs = append(cloneArgs, flags...)
		cloneArgs = append(cloneArgs, url, dest)

		//nolint:gosec // G702: args are user-provided CLI input.
		cmd := exec.CommandContext(ctx, "git", cloneArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err = cmd.Run()
		if err != nil {
			return fmt.Errorf("git clone: %w", err)
		}
	}

	return nil
}
