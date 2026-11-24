package main

import (
	"context"
	"fmt"
)

// rustPackages holds the attribute name, in the packages output of the
// flake, of each package that builds a Rust crate under tools/.
// [Nix.CheckClippy] runs clippy on the crate of each package of this
// list.
var rustPackages = []string{"prose-lint-rs"}

// CheckClippy verifies that clippy does not report a warning in any
// Rust crate under tools/. For each package of rustPackages, it builds the
// clippy test in the passthru.tests of the package. That test runs
// `cargo clippy --all-targets -- -D warnings`, so each warning fails
// the check. The check builds the tests for the system of the Dagger
// engine.
//
// +check
func (m *Nix) CheckClippy(ctx context.Context) error {
	args := []string{"nix", "build", "--no-link"}
	for _, pkg := range rustPackages {
		args = append(args, ".#"+pkg+".tests.clippy")
	}

	_, err := m.base().
		WithExec(args).
		Sync(ctx)
	if err != nil {
		return fmt.Errorf("running clippy: %w", err)
	}

	return nil
}
