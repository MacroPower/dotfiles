package main

import (
	"context"
	"fmt"
	"strings"

	"dagger/tests/internal/dagger"
)

const (
	// alphaUpdateScript is the path, in a probe of pkgUpdateProbes, of
	// an update script.
	alphaUpdateScript = "pkgs/alpha-update.sh"

	// alphaPackageFile is the path of the package file of the script at
	// alphaUpdateScript.
	alphaPackageFile = "pkgs/alpha.nix"

	// betaUpdateScript is the path, in a probe of pkgUpdateProbes, of a
	// second update script.
	betaUpdateScript = "pkgs/beta-update.sh"

	// betaPackageFile is the path of the package file that carries the
	// name of the script at betaUpdateScript.
	betaPackageFile = "pkgs/beta.nix"

	// betaDesktopPackageFile is the path of the second package file that
	// the text of betaDesktopScript names.
	betaDesktopPackageFile = "pkgs/beta-desktop.nix"

	// pkgUpdateProbeContents holds the contents of each file that a
	// probe of pkgUpdateProbes lists in its files field.
	// [Nix.CheckPkgUpdate] reads the paths of those files and the text
	// of each update script. This text names no package file.
	pkgUpdateProbeContents = "probe\n"

	// betaDesktopScript is the text of an update script that names two
	// package files as the update scripts of the repository do, each in
	// a path that starts with `$DIRNAME/`. In the script at
	// betaUpdateScript, the paths name the files at betaPackageFile and
	// betaDesktopPackageFile.
	betaDesktopScript = `#!/usr/bin/env bash
set -euo pipefail

DIRNAME=$(dirname "$0")
PKG_FILE="$DIRNAME/beta.nix"
DESKTOP_PKG_FILE="$DIRNAME/beta-desktop.nix"
`

	// betaSpellingsScript is the text of an update script that names
	// three package files after the file at betaPackageFile. Each of
	// the three paths spells the DIRNAME variable another way. The
	// first one puts the name in braces, the second one closes the
	// double quotes before the slash, and the third one does both.
	betaSpellingsScript = `DIRNAME=$(dirname "$0")
PKG_FILE="$DIRNAME/beta.nix"
SERVER_PKG_FILE="${DIRNAME}/beta-server.nix"
TOOLS_PKG_FILE="$DIRNAME"/beta-tools.nix
AGENT_PKG_FILE="${DIRNAME}"/beta-agent.nix
`

	// betaOtherPathsScript is the text of an update script that names
	// one package file, the file at betaPackageFile. No other path of
	// the text names a package file. The paths to the old directory, to
	// gamma.nix.orig, and to beta.json do not end in .nix. The path to
	// flake.nix leaves the directory of the script. The paths to
	// default.nix and delta.nix start with other variables.
	betaOtherPathsScript = `DIRNAME=$(dirname "$0")
OLD_DIRNAME=$DIRNAME/old
REPO_ROOT=$(git -C "$DIRNAME" rev-parse --show-toplevel)
PKG_FILE="$DIRNAME/beta.nix"
cp "$PKG_FILE" "$DIRNAME/gamma.nix.orig"
jq . "$DIRNAME/beta.json"
nix eval --file "$DIRNAME/../flake.nix"
nix eval --file "$REPO_ROOT/default.nix"
nix eval --file "$OLD_DIRNAME/delta.nix"
`
)

// pkgUpdateProbe is a project source on which
// [Tests.TestCheckPkgUpdate] runs [Nix.CheckPkgUpdate].
type pkgUpdateProbe struct {
	// name says what the source holds, for the errors of the test.
	name string

	// taskfile holds the contents of the file at nixTaskfilePath. When
	// taskfile is empty, the source does not hold that file.
	taskfile string

	// want holds each text that the error of the check must hold. The
	// check must pass a probe whose want is empty.
	want []string

	// scripts holds the text of each update script of the source whose
	// text the probe sets, by the path of the script.
	scripts map[string]string

	// files holds the path of each file of the source other than the
	// Taskfile and the scripts of scripts. Each of those files holds
	// pkgUpdateProbeContents.
	files []string
}

// pkgUpdateProbes holds the probes of [Tests.TestCheckPkgUpdate]. The
// source of each probe holds each package file of each of its update
// scripts, unless the comment of the probe says that a package file is
// missing.
var pkgUpdateProbes = []pkgUpdateProbe{
	// The task runs both update scripts, and the source holds the
	// package file of each script. No other file of the source is an
	// update script. The name of a package file does not end in
	// -update.sh, and the file in scripts/ is not in pkgs/.
	{
		name:     "a task that runs each update script",
		taskfile: pkgUpdateProbeTaskfile("pkg:update", "bash "+alphaUpdateScript, "bash "+betaUpdateScript),
		files: []string{
			alphaUpdateScript, alphaPackageFile, betaUpdateScript, betaPackageFile, "scripts/gamma-update.sh",
		},
	},
	{
		name:     "an update script that the task does not run",
		taskfile: pkgUpdateProbeTaskfile("pkg:update", "bash "+alphaUpdateScript),
		files:    []string{alphaUpdateScript, alphaPackageFile, betaUpdateScript, betaPackageFile},
		want:     []string{"does not run " + betaUpdateScript},
	},
	{
		name:     "a task that runs a script that the source does not hold",
		taskfile: pkgUpdateProbeTaskfile("pkg:update", "bash "+alphaUpdateScript, "bash "+betaUpdateScript),
		files:    []string{alphaUpdateScript, alphaPackageFile},
		want:     []string{"runs " + betaUpdateScript + ", which"},
	},
	// The task runs both update scripts, and the source does not hold
	// the package file that carries the name of the second script. The
	// text of that script names no package file. Neither of the last
	// two files is the missing package file. The name of one is longer,
	// and the other one is not in pkgs/.
	{
		name:     "an update script without its package file",
		taskfile: pkgUpdateProbeTaskfile("pkg:update", "bash "+alphaUpdateScript, "bash "+betaUpdateScript),
		files: []string{
			alphaUpdateScript, alphaPackageFile, betaUpdateScript, betaDesktopPackageFile, "scripts/beta.nix",
		},
		want: []string{betaUpdateScript + " updates " + betaPackageFile + ", which"},
	},
	// The text of the second script names two package files, and the
	// source holds both.
	{
		name:     "an update script that names two package files",
		taskfile: pkgUpdateProbeTaskfile("pkg:update", "bash "+alphaUpdateScript, "bash "+betaUpdateScript),
		scripts:  map[string]string{betaUpdateScript: betaDesktopScript},
		files:    []string{alphaUpdateScript, alphaPackageFile, betaPackageFile, betaDesktopPackageFile},
	},
	// The text of the second script names two package files, and the
	// source holds only the one that carries the name of the script.
	// The last file is not the missing package file, because it is not
	// in pkgs/.
	{
		name:     "an update script that names a package file that the source does not hold",
		taskfile: pkgUpdateProbeTaskfile("pkg:update", "bash "+alphaUpdateScript, "bash "+betaUpdateScript),
		scripts:  map[string]string{betaUpdateScript: betaDesktopScript},
		files:    []string{alphaUpdateScript, alphaPackageFile, betaPackageFile, "scripts/beta-desktop.nix"},
		want:     []string{betaUpdateScript + " updates " + betaDesktopPackageFile + ", which"},
	},
	// The text of the second script names three package files that the
	// source does not hold, each in another spelling of the path. The
	// error of the check must name each of the three files.
	{
		name:     "an update script that names package files in each spelling of the path",
		taskfile: pkgUpdateProbeTaskfile("pkg:update", "bash "+alphaUpdateScript, "bash "+betaUpdateScript),
		scripts:  map[string]string{betaUpdateScript: betaSpellingsScript},
		files:    []string{alphaUpdateScript, alphaPackageFile, betaPackageFile},
		want: []string{
			betaUpdateScript + " updates pkgs/beta-server.nix, which",
			betaUpdateScript + " updates pkgs/beta-tools.nix, which",
			betaUpdateScript + " updates pkgs/beta-agent.nix, which",
		},
	},
	// The text of the second script holds paths that name no package
	// file, and the source holds none of the files of those paths.
	{
		name:     "an update script with paths that name no package file",
		taskfile: pkgUpdateProbeTaskfile("pkg:update", "bash "+alphaUpdateScript, "bash "+betaUpdateScript),
		scripts:  map[string]string{betaUpdateScript: betaOtherPathsScript},
		files:    []string{alphaUpdateScript, alphaPackageFile, betaPackageFile},
	},
	// The source holds a directory at the path of the second script,
	// and the check cannot read a directory as a script.
	{
		name:     "a directory at the path of an update script",
		taskfile: pkgUpdateProbeTaskfile("pkg:update", "bash "+alphaUpdateScript, "bash "+betaUpdateScript),
		files:    []string{alphaUpdateScript, alphaPackageFile, betaUpdateScript + "/keep", betaPackageFile},
		want:     []string{"reading " + betaUpdateScript},
	},
	// The second entry runs its script with sh. The check reads only an
	// entry that runs its script with bash.
	{
		name:     "an entry of another form",
		taskfile: pkgUpdateProbeTaskfile("pkg:update", "bash "+alphaUpdateScript, "sh "+betaUpdateScript),
		files:    []string{alphaUpdateScript, alphaPackageFile, betaUpdateScript, betaPackageFile},
		want:     []string{"holds the entry"},
	},
	// The entry is a mapping whose cmd key holds the command. The check
	// reads only an entry that is a string.
	{
		name:     "an entry that is a mapping",
		taskfile: pkgUpdateProbeTaskfile("pkg:update", "cmd: bash "+alphaUpdateScript),
		files:    []string{alphaUpdateScript, alphaPackageFile},
		want:     []string{"holds the entry"},
	},
	// A task with another name runs the update script. The check reads
	// only the pkg:update task.
	{
		name:     "a Taskfile without the pkg:update task",
		taskfile: pkgUpdateProbeTaskfile("pkg:other", "bash "+alphaUpdateScript),
		files:    []string{alphaUpdateScript, alphaPackageFile},
		want:     []string{"does not define the pkg:update task"},
	},
	// The other task holds its command in the short syntax, which Task
	// reads. The check decodes only the pkg:update task, so the other
	// task does not fail it.
	{
		name: "a Taskfile with another task in the short syntax",
		taskfile: "version: \"3\"\n\ntasks:\n" +
			"  other: echo hi\n" +
			"  pkg:update:\n    cmds:\n      - bash " + alphaUpdateScript + "\n",
		files: []string{alphaUpdateScript, alphaPackageFile},
	},
	// The pkg:update task is a list of commands in the short syntax. The
	// check reads only a task that is a mapping with a cmds key.
	{
		name:     "a pkg:update task in the short syntax",
		taskfile: "version: \"3\"\n\ntasks:\n  pkg:update: [bash " + alphaUpdateScript + "]\n",
		files:    []string{alphaUpdateScript, alphaPackageFile},
		want:     []string{"decoding the pkg:update task"},
	},
	// The Taskfile opens a flow sequence and does not close it.
	{
		name:     "a Taskfile that is not YAML",
		taskfile: "tasks: [",
		files:    []string{alphaUpdateScript, alphaPackageFile},
		want:     []string{"decoding the Taskfile"},
	},
	// The taskfile of the probe is empty, so the source does not hold
	// the Taskfile.
	{
		name:  "no Taskfile",
		files: []string{alphaUpdateScript, alphaPackageFile},
		want:  []string{"reading " + nixTaskfilePath},
	},
	// The source holds neither the Taskfile nor the package file of its
	// update script. The error of the check must name both faults.
	{
		name:  "no Taskfile and an update script without its package file",
		files: []string{alphaUpdateScript},
		want: []string{
			"reading " + nixTaskfilePath,
			alphaUpdateScript + " updates " + alphaPackageFile + ", which",
		},
	},
}

// pkgUpdateProbeTaskfile returns a Taskfile with one task, which has
// the name task. The cmds list of the task holds each entry of cmds,
// and each entry is YAML.
func pkgUpdateProbeTaskfile(task string, cmds ...string) string {
	var taskfile strings.Builder

	taskfile.WriteString("version: \"3\"\n\ntasks:\n  " + task + ":\n    cmds:\n")

	for _, cmd := range cmds {
		taskfile.WriteString("      - " + cmd + "\n")
	}

	return taskfile.String()
}

// TestCheckPkgUpdate verifies that [Nix.CheckPkgUpdate] compares the
// pkg:update task of the Taskfile at nixTaskfilePath with the update
// scripts of the project source. It also verifies that the check
// requires each package file of each update script. Those files are
// the file that carries the name of the script and each file that the
// text of the script names in a path that starts with `$DIRNAME/`. The
// check passes on the default source of [Nix]. So the task of the
// repository runs each update script of the repository, and the
// repository holds each package file of each script.
//
// The test then runs the check on each probe of pkgUpdateProbes. The
// source of a probe holds only the files and the scripts that the probe
// lists and the Taskfile of the probe. So the result depends on nothing else. The
// check passes each of these sources:
//
//   - The task runs each update script, and the source holds the
//     package file that carries the name of each script.
//   - The Taskfile holds another task in the short syntax. The check
//     decodes only the pkg:update task.
//   - The text of an update script names two package files, and the
//     source holds both.
//   - The text of an update script holds paths that name no package
//     file. Such a path does not end in .nix, leaves the directory of
//     the script, or starts with another variable than DIRNAME.
//
// The check fails in each of these cases:
//
//   - The source holds an update script that the task does not run.
//   - The task runs a script that the source does not hold.
//   - The source holds an update script and does not hold the package
//     file that carries the name of that script.
//   - The text of an update script names a package file that the source
//     does not hold.
//   - The source holds a directory at the path of an update script, so
//     the check cannot read the script.
//   - The task holds an entry that is not a string of the form
//     `bash pkgs/<name>-update.sh`.
//   - The Taskfile does not define the pkg:update task.
//   - The pkg:update task is in the short syntax.
//   - The Taskfile is not YAML.
//   - The source does not hold the Taskfile.
//
// For each of those probes, the test requires the error of the check to
// name that fault. A check that returns the same error for each source
// therefore fails the test.
//
// The source of one probe holds neither the Taskfile nor the package
// file of its update script. The test requires the error of the check
// to name both faults. So a check that stops at the first fault fails
// the test.
//
// The script of one probe names three package files that the source
// does not hold. Each path spells the DIRNAME variable another way,
// with braces, with a double quote before the slash, or with both. The
// test requires the error of the check to name each of the three files.
// So a check that reads only the path `$DIRNAME/<file>.nix` fails the
// test, as does a check that stops at the first missing file.
//
// +check
func (m *Tests) TestCheckPkgUpdate(ctx context.Context) error {
	err := dag.Nix().CheckPkgUpdate(ctx)
	if err != nil {
		return fmt.Errorf("checking the default source: %w", err)
	}

	for _, probe := range pkgUpdateProbes {
		src := dag.Directory()
		if probe.taskfile != "" {
			src = src.WithNewFile(nixTaskfilePath, probe.taskfile)
		}

		for _, file := range probe.files {
			src = src.WithNewFile(file, pkgUpdateProbeContents)
		}

		for script, text := range probe.scripts {
			src = src.WithNewFile(script, text)
		}

		err = dag.Nix(dagger.NixOpts{Source: src}).CheckPkgUpdate(ctx)

		switch {
		case len(probe.want) == 0 && err != nil:
			return fmt.Errorf("checking a source with %s: %w", probe.name, err)

		case len(probe.want) > 0 && err == nil:
			return fmt.Errorf("CheckPkgUpdate passed a source with %s", probe.name)
		}

		for _, want := range probe.want {
			if !strings.Contains(err.Error(), want) {
				return fmt.Errorf(
					"checking a source with %s, want an error that holds %q: %w",
					probe.name, want, err,
				)
			}
		}
	}

	return nil
}
