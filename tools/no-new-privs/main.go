// Package main implements a minimal wrapper that sets the Linux
// PR_SET_NO_NEW_PRIVS flag on itself then execs into the given command.
// Once set, the kernel ignores setuid/setgid bits for this process and
// all descendants, preventing privilege escalation through sudo et al.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: no-new-privs command [args...]")
		os.Exit(1)
	}

	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		fmt.Fprintf(os.Stderr, "prctl(PR_SET_NO_NEW_PRIVS): %v\n", err)
		os.Exit(1)
	}

	bin, err := exec.LookPath(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", os.Args[1], err)
		os.Exit(127)
	}

	if err := syscall.Exec(bin, os.Args[1:], os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", os.Args[1], err)
		os.Exit(127)
	}
}
