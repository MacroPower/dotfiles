//go:build !darwin && !linux

package main

import "fmt"

// newPlatformSandbox is the fallback for platforms with no real sandbox
// backend. SandboxOn is a fatal error; SandboxAuto and SandboxOff return
// [noopSandbox] so the binary still runs.
func newPlatformSandbox(mode SandboxMode) (Sandbox, error) {
	if mode == SandboxOn {
		return nil, fmt.Errorf("%w: no sandbox backend for this platform", ErrSandbox)
	}

	return noopSandbox{}, nil
}
