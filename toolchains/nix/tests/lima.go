package main

import (
	"context"
	"errors"
	"fmt"

	"dagger/tests/internal/dagger"
)

const (
	// limaTestPath is the path, in the project source, of the tests of
	// the lima Taskfile, which [Nix.CheckLimaTaskfile] runs.
	limaTestPath = "taskfiles/lima/test_image.py"

	// taskOnPathTest is a Python test file whose one test fails when
	// task is not on PATH. The tests at limaTestPath skip in that case.
	taskOnPathTest = `import shutil
import unittest


class ProbeTest(unittest.TestCase):
    def test_task_on_path(self):
        self.assertIsNotNone(shutil.which("task"))


unittest.main()
`

	// failingTest is a Python test file whose one test fails.
	failingTest = `import unittest


class ProbeTest(unittest.TestCase):
    def test_fails(self):
        self.fail("probe")


unittest.main()
`
)

// nixWithLimaTest returns the [Nix] module whose source holds only a
// file at limaTestPath with contents.
func nixWithLimaTest(contents string) *dagger.Nix {
	return dag.Nix(dagger.NixOpts{
		Source: dag.Directory().WithNewFile(limaTestPath, contents),
	})
}

// TestCheckLimaTaskfile verifies that [Nix.CheckLimaTaskfile] runs the
// tests at limaTestPath with task on PATH and fails when a test fails.
// The check passes the default source. It also passes a source that
// holds only taskOnPathTest at limaTestPath, whose test fails when task
// is not on PATH. So the tests of the default source ran with task and
// did not skip. The check fails a source that holds only failingTest at
// limaTestPath.
//
// +check
func (m *Tests) TestCheckLimaTaskfile(ctx context.Context) error {
	err := dag.Nix().CheckLimaTaskfile(ctx)
	if err != nil {
		return fmt.Errorf("checking the default source: %w", err)
	}

	err = nixWithLimaTest(taskOnPathTest).CheckLimaTaskfile(ctx)
	if err != nil {
		return fmt.Errorf("checking a test that fails when task is not on PATH: %w", err)
	}

	err = nixWithLimaTest(failingTest).CheckLimaTaskfile(ctx)
	if err == nil {
		return errors.New("CheckLimaTaskfile passed a test that fails")
	}

	return nil
}
