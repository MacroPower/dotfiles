package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// pkgUpdateTaskfile is the path, in the project source, of the
	// Taskfile that defines the pkg:update task.
	pkgUpdateTaskfile = "taskfiles/nix/Taskfile.yaml"

	// pkgUpdateTask is the name of the task that runs the update
	// scripts, as the Taskfile at pkgUpdateTaskfile spells it.
	pkgUpdateTask = "pkg:update"

	// updateScriptSuffix ends the path of each update script.
	updateScriptSuffix = "-update.sh"

	// updateScriptGlob matches the path of each update script from the
	// root of the project source.
	updateScriptGlob = "pkgs/*" + updateScriptSuffix

	// packageFileSuffix ends the path of each package file.
	packageFileSuffix = ".nix"

	// packageFileGlob matches the path of each package file from the
	// root of the project source.
	packageFileGlob = "pkgs/*" + packageFileSuffix
)

// updateCommandRe matches a command of the pkg:update task that runs
// one update script. Its group holds the path of the script from the
// root of the project source.
var updateCommandRe = regexp.MustCompile(`^bash (pkgs/[^/\s]+-update\.sh)$`)

// CheckPkgUpdate verifies that the pkg:update task of
// taskfiles/nix/Taskfile.yaml runs the update scripts that the project
// source holds, and no other script. It also verifies that the source
// holds the package file of each update script. An update script is a
// file that matches pkgs/*-update.sh. The package file of
// pkgs/<name>-update.sh is pkgs/<name>.nix, which the script updates.
//
// The check fails when the task does not run an update script, because
// the package of that script then stays at an old version. It also
// fails when the task runs a script that the source does not hold. Task
// stops at the command of that script, so it never runs the scripts
// after it.
//
// The check fails when the source holds an update script and does not
// hold the package file of that script. The script fails when it reads
// the missing file, so Task stops at the command of that script as
// well. The check finds the package file by the name of the script and
// does not read the script. So it does not see another file that a
// script updates.
//
// The task spells out one command for each script, and the check reads
// those commands. Write each entry of the cmds list of the task as a
// string of the form `bash pkgs/<name>-update.sh`. The check fails on
// an entry of another form, because it cannot tell which script that
// entry runs.
//
// One run of the check reports the faults of the task and each update
// script without its package file.
//
// +check
func (m *Nix) CheckPkgUpdate(ctx context.Context) error {
	scripts, err := m.Source.Glob(ctx, updateScriptGlob)
	if err != nil {
		return fmt.Errorf("listing %s: %w", updateScriptGlob, err)
	}

	packages, err := m.Source.Glob(ctx, packageFileGlob)
	if err != nil {
		return fmt.Errorf("listing %s: %w", packageFileGlob, err)
	}

	// The joined error names the faults of both comparisons. It also
	// reaches a caller in another module with its whole text. A
	// function can return an API error in a chain of single wraps, as
	// checkTaskfile does for a missing Taskfile. The Go SDK of Dagger
	// then sends that caller only the message of the API error, without
	// the context of each wrap.
	return errors.Join(m.checkTaskfile(ctx, scripts), checkPackageFiles(scripts, packages))
}

// checkTaskfile reads the Taskfile at pkgUpdateTaskfile from the
// project source and returns the error of [checkPkgUpdate] for that
// Taskfile and scripts. scripts holds the path of each update script
// from the root of the project source. The function also returns an
// error when it cannot read the Taskfile, such as when the source does
// not hold the file.
func (m *Nix) checkTaskfile(ctx context.Context, scripts []string) error {
	taskfile, err := m.Source.File(pkgUpdateTaskfile).Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading %s: %w", pkgUpdateTaskfile, err)
	}

	err = checkPkgUpdate(taskfile, scripts)
	if err != nil {
		return fmt.Errorf("checking %s: %w", pkgUpdateTaskfile, err)
	}

	return nil
}

// checkPkgUpdate returns an error that names each difference between
// the pkg:update task of taskfile and scripts. taskfile holds the
// contents of the Taskfile, and scripts holds the path of each update
// script from the root of the project source. A difference is a path of
// scripts that the task does not run, or a script that the task runs
// and scripts does not hold. The function returns the error of
// [pkgUpdateScripts] when that function cannot read the task.
func checkPkgUpdate(taskfile string, scripts []string) error {
	listed, err := pkgUpdateScripts(taskfile)
	if err != nil {
		return err
	}

	var errs []error

	for _, script := range slices.Sorted(slices.Values(scripts)) {
		if !slices.Contains(listed, script) {
			errs = append(errs, fmt.Errorf("the %s task does not run %s", pkgUpdateTask, script))
		}
	}

	for _, script := range listed {
		if !slices.Contains(scripts, script) {
			errs = append(
				errs,
				fmt.Errorf("the %s task runs %s, which the project source does not hold", pkgUpdateTask, script),
			)
		}
	}

	return errors.Join(errs...)
}

// pkgUpdateScripts decodes taskfile as YAML and returns the path of the
// script that each command of its pkg:update task runs, in the order of
// the task. It returns an error when taskfile is not YAML or does not
// define the task. It also returns an error when the task is a string
// or a list of commands, which the short syntax of Task allows. It
// returns an error that quotes each entry of the cmds list of the task
// that is not a string that updateCommandRe matches.
//
// The function decodes only the pkg:update task. So another task that
// Task reads and the function cannot decode, such as a task in the
// short syntax, does not fail the function.
func pkgUpdateScripts(taskfile string) ([]string, error) {
	var doc struct {
		Tasks map[string]yaml.Node `yaml:"tasks"`
	}

	err := yaml.Unmarshal([]byte(taskfile), &doc)
	if err != nil {
		return nil, fmt.Errorf("decoding the Taskfile: %w", err)
	}

	node, ok := doc.Tasks[pkgUpdateTask]
	if !ok {
		return nil, fmt.Errorf("the Taskfile does not define the %s task", pkgUpdateTask)
	}

	var task struct {
		Cmds []any `yaml:"cmds"`
	}

	err = node.Decode(&task)
	if err != nil {
		return nil, fmt.Errorf("decoding the %s task: %w", pkgUpdateTask, err)
	}

	var (
		scripts []string
		errs    []error
	)

	for _, cmd := range task.Cmds {
		// match stays nil for an entry that is not a string, such as a
		// mapping with a cmd key.
		var match []string

		if command, ok := cmd.(string); ok {
			match = updateCommandRe.FindStringSubmatch(command)
		}

		if match == nil {
			errs = append(errs, fmt.Errorf(
				"the %s task holds the entry %q, want a string of the form `bash pkgs/<name>-update.sh`",
				pkgUpdateTask, fmt.Sprint(cmd),
			))

			continue
		}

		scripts = append(scripts, match[1])
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	return scripts, nil
}

// checkPackageFiles returns an error that names each update script of
// scripts whose package file packages does not hold. scripts holds the
// path of each update script from the root of the project source.
// packages holds the path of each package file from that root.
func checkPackageFiles(scripts, packages []string) error {
	var errs []error

	for _, script := range slices.Sorted(slices.Values(scripts)) {
		pkg := packageFile(script)
		if !slices.Contains(packages, pkg) {
			errs = append(errs, fmt.Errorf("%s updates %s, which the project source does not hold", script, pkg))
		}
	}

	return errors.Join(errs...)
}

// packageFile returns the path of the package file of the update script
// at script, which is the file that the script updates. The package
// file of pkgs/<name>-update.sh is pkgs/<name>.nix.
func packageFile(script string) string {
	return strings.TrimSuffix(script, updateScriptSuffix) + packageFileSuffix
}
