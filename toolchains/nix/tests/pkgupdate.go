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

	// betaPackageFile is the path of the package file of the script at
	// betaUpdateScript.
	betaPackageFile = "pkgs/beta.nix"

	// pkgUpdateProbeContents holds the contents of each file of a probe
	// of pkgUpdateProbes other than the Taskfile. [Nix.CheckPkgUpdate]
	// reads only the paths of those files.
	pkgUpdateProbeContents = "probe\n"
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

	// files holds the path of each file of the source other than the
	// Taskfile.
	files []string
}

// pkgUpdateProbes holds the probes of [Tests.TestCheckPkgUpdate]. The
// source of each probe holds the package file of each of its update
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
	// the package file of the second script. Neither of the last two
	// files is that package file. The name of one is longer, and the
	// other one is not in pkgs/.
	{
		name:     "an update script without its package file",
		taskfile: pkgUpdateProbeTaskfile("pkg:update", "bash "+alphaUpdateScript, "bash "+betaUpdateScript),
		files: []string{
			alphaUpdateScript, alphaPackageFile, betaUpdateScript, "pkgs/beta-desktop.nix", "scripts/beta.nix",
		},
		want: []string{betaUpdateScript + " updates " + betaPackageFile + ", which"},
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
// requires the package file of each update script. The check passes on
// the default source of [Nix]. So the task of the repository runs each
// update script of the repository, and the repository holds the package
// file of each script.
//
// The test then runs the check on each probe of pkgUpdateProbes. The
// source of a probe holds only the files that the probe lists and the
// Taskfile of the probe. So the result depends on nothing else. The
// check passes a source whose task runs each update script and that
// holds the package file of each script. It also passes a Taskfile that
// holds another task in the short syntax, because it decodes only the
// pkg:update task. The check fails in each of these cases:
//
//   - The source holds an update script that the task does not run.
//   - The task runs a script that the source does not hold.
//   - The source holds an update script and does not hold the package
//     file of that script.
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
