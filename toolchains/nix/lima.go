package main

import (
	"context"
	"fmt"
)

// limaTestPath is the path, in the project source, of the tests of the
// image and pull tasks of taskfiles/lima/Taskfile.yaml.
const limaTestPath = "taskfiles/lima/test_image.py"

// CheckLimaTaskfile runs the tests at taskfiles/lima/test_image.py,
// which run the image and pull tasks of taskfiles/lima/Taskfile.yaml
// against fakes of nix, zstd, and oras. The check fails when a test
// fails.
//
// The tests copy Taskfile.yaml and taskfiles/ of the project source to
// a temporary directory and run task there. They skip each test when
// task is not on PATH. So the check runs them in `nix shell` with the
// go-task package of nixpkgs, which provides task. The root Taskfile
// runs hostname and the lima Taskfile runs sed to set their variables.
// The image of [Nix.base] holds neither command, so the shell also
// holds the hostname-debian and gnused packages.
//
// +check
func (m *Nix) CheckLimaTaskfile(ctx context.Context) error {
	_, err := m.base().
		WithExec([]string{
			"nix", "shell",
			"nixpkgs#python3", "nixpkgs#go-task", "nixpkgs#hostname-debian", "nixpkgs#gnused",
			"--command", "python3", limaTestPath,
		}).
		Sync(ctx)
	if err != nil {
		return fmt.Errorf("running %s: %w", limaTestPath, err)
	}

	return nil
}
