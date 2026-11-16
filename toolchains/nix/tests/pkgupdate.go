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

	// betaUpdateScript is the path, in a probe of pkgUpdateProbes, of a
	// second update script.
	betaUpdateScript = "pkgs/beta-update.sh"

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

	// want holds a text that the error of the check must hold. The
	// check must pass a probe whose want is empty.
	want string

	// files holds the path of each file of the source other than the
	// Taskfile.
	files []string
}

// pkgUpdateProbes holds the probes of [Tests.TestCheckPkgUpdate].
var pkgUpdateProbes = []pkgUpdateProbe{
	// The task runs both update scripts. Neither of the other two
	// files is an update script. The name of one does not end in
	// -update.sh, and the other one is not in pkgs/.
	{
		name:     "a task that runs each update script",
		taskfile: pkgUpdateProbeTaskfile("pkg:update", "bash "+alphaUpdateScript, "bash "+betaUpdateScript),
		files:    []string{alphaUpdateScript, betaUpdateScript, "pkgs/alpha.nix", "scripts/gamma-update.sh"},
	},
	{
		name:     "an update script that the task does not run",
		taskfile: pkgUpdateProbeTaskfile("pkg:update", "bash "+alphaUpdateScript),
		files:    []string{alphaUpdateScript, betaUpdateScript},
		want:     "does not run " + betaUpdateScript,
	},
	{
		name:     "a task that runs a script that the source does not hold",
		taskfile: pkgUpdateProbeTaskfile("pkg:update", "bash "+alphaUpdateScript, "bash "+betaUpdateScript),
		files:    []string{alphaUpdateScript},
		want:     "runs " + betaUpdateScript + ", which",
	},
	// The second entry runs its script with sh. The check reads only an
	// entry that runs its script with bash.
	{
		name:     "an entry of another form",
		taskfile: pkgUpdateProbeTaskfile("pkg:update", "bash "+alphaUpdateScript, "sh "+betaUpdateScript),
		files:    []string{alphaUpdateScript, betaUpdateScript},
		want:     "holds the entry",
	},
	// The entry is a mapping whose cmd key holds the command. The check
	// reads only an entry that is a string.
	{
		name:     "an entry that is a mapping",
		taskfile: pkgUpdateProbeTaskfile("pkg:update", "cmd: bash "+alphaUpdateScript),
		files:    []string{alphaUpdateScript},
		want:     "holds the entry",
	},
	// A task with another name runs the update script. The check reads
	// only the pkg:update task.
	{
		name:     "a Taskfile without the pkg:update task",
		taskfile: pkgUpdateProbeTaskfile("pkg:other", "bash "+alphaUpdateScript),
		files:    []string{alphaUpdateScript},
		want:     "does not define the pkg:update task",
	},
	// The other task holds its command in the short syntax, which Task
	// reads. The check decodes only the pkg:update task, so the other
	// task does not fail it.
	{
		name: "a Taskfile with another task in the short syntax",
		taskfile: "version: \"3\"\n\ntasks:\n" +
			"  other: echo hi\n" +
			"  pkg:update:\n    cmds:\n      - bash " + alphaUpdateScript + "\n",
		files: []string{alphaUpdateScript},
	},
	// The pkg:update task is a list of commands in the short syntax. The
	// check reads only a task that is a mapping with a cmds key.
	{
		name:     "a pkg:update task in the short syntax",
		taskfile: "version: \"3\"\n\ntasks:\n  pkg:update: [bash " + alphaUpdateScript + "]\n",
		files:    []string{alphaUpdateScript},
		want:     "decoding the pkg:update task",
	},
	// The Taskfile opens a flow sequence and does not close it.
	{
		name:     "a Taskfile that is not YAML",
		taskfile: "tasks: [",
		files:    []string{alphaUpdateScript},
		want:     "decoding the Taskfile",
	},
	// The taskfile of the probe is empty, so the source does not hold
	// the Taskfile.
	{
		name:  "no Taskfile",
		files: []string{alphaUpdateScript},
		want:  "reading " + nixTaskfilePath,
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
// scripts of the project source. The check passes on the default source
// of [Nix], so the task of the repository runs each update script of
// the repository.
//
// The test then runs the check on each probe of pkgUpdateProbes. The
// source of a probe holds only the files that the probe lists and the
// Taskfile of the probe. So the result depends on nothing else. The
// check passes a source whose task runs each update script. It also
// passes a Taskfile that holds another task in the short syntax,
// because it decodes only the pkg:update task. The check fails in each
// of these cases:
//
//   - The source holds an update script that the task does not run.
//   - The task runs a script that the source does not hold.
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
		case probe.want == "" && err != nil:
			return fmt.Errorf("checking a source with %s: %w", probe.name, err)

		case probe.want != "" && err == nil:
			return fmt.Errorf("CheckPkgUpdate passed a source with %s", probe.name)

		case probe.want != "" && !strings.Contains(err.Error(), probe.want):
			return fmt.Errorf(
				"checking a source with %s, want an error that holds %q: %w",
				probe.name, probe.want, err,
			)
		}
	}

	return nil
}
