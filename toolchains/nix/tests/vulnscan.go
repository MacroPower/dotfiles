package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"slices"
	"strings"

	"dagger/tests/internal/dagger"
)

const (
	// neverCachePragma is the line of a doc comment that sets the cache
	// policy of a function to never. Dagger then runs the function in
	// each call and does not store its result.
	neverCachePragma = `+cache="never"`

	// jsonGrypeConfig is a .grype.yaml file that makes grype write its
	// report as JSON. The descriptor of a JSON report holds the time of
	// the scan, so the reports of two scans differ.
	jsonGrypeConfig = "output:\n  - json\n"

	// cachePragmaProbeGoPath is the path that [checkCachePragmaProbes]
	// gives each probe file.
	cachePragmaProbeGoPath = "probe.go"

	// cachePragmaProbeName is the name of the function that each probe
	// file of [checkCachePragmaProbes] declares.
	cachePragmaProbeName = "Probe"

	// grypeVolume is the key of the cache volume that holds the
	// vulnerability database of grype in the root module.
	grypeVolume = "grype-db"

	// lockedModeName is the name, in the Go bindings of Dagger, of the
	// sharing mode locked. The engine runs the steps that mount a cache
	// volume with that mode one at a time.
	lockedModeName = "CacheSharingModeLocked"

	// lockedVolumeProbeGoPath is the path that
	// [checkLockedVolumeProbes] gives each probe file.
	lockedVolumeProbeGoPath = "probe.go"

	// probeVolume is the key of the cache volume that
	// [checkLockedVolumeProbes] checks in each probe file.
	probeVolume = "probe-db"

	// otherVolume is the key of another cache volume of the probe
	// files, which [checkLockedVolumeProbes] does not check.
	otherVolume = "other-db"

	// lockedField is the Go source of the field that sets the sharing
	// mode locked in the options of a mount.
	lockedField = "Sharing: dagger." + lockedModeName
)

// cachePragmaProbe is a probe of [checkCachePragmaProbes].
type cachePragmaProbe struct {
	// doc holds the doc comment of the function of the probe file.
	doc string

	// never reports whether Dagger reads the cache policy never from
	// doc.
	never bool
}

// lockedVolumeProbe is a probe of [checkLockedVolumeProbes].
type lockedVolumeProbe struct {
	// expr holds the Go expression that the function of the probe file
	// returns.
	expr string

	// locked reports whether expr mounts the cache volume probeVolume,
	// and each use of that volume in expr is a mount with the sharing
	// mode locked.
	locked bool
}

var (
	// scanFunctions holds the Go name of each function of [Nix] that
	// scans with grype. The report of a scan depends on the
	// vulnerability database that grype downloads, which is not an
	// input of the function.
	scanFunctions = []string{"Vulnscan", "VulnscanSarif"}

	// cachePragmaProbes holds the probes of [checkCachePragmaProbes].
	cachePragmaProbes = []cachePragmaProbe{
		// The pragma stands on a line of its own.
		{"// Probe scans with grype.\n// " + neverCachePragma, true},
		{"// Probe scans with grype.\n//\n//\t" + neverCachePragma, true},
		// The comment holds no cache pragma.
		{"// Probe scans with grype.", false},
		// A word follows the name of the pragma on its line, so Dagger
		// reads no pragma from the comment.
		{"// Probe carries the +cache pragma with the value never.", false},
		// The pragma sets another policy.
		{"// Probe scans with grype.\n// +cache=\"session\"", false},
		{"// Probe scans with grype.\n// +cache=\"10m\"", false},
		// Dagger reads a pragma before an = at any place in a line, and
		// it keeps the last value of a pragma. So it keeps the result
		// of the function for one hour.
		{
			"// Probe scans with grype.\n// " + neverCachePragma +
				"\n//\n// A function with +cache=\"1h\" keeps its result for one hour.",
			false,
		},
	}

	// errUnlockedVolume is the error of [checkLockedVolume] for each
	// fault that it finds.
	errUnlockedVolume = errors.New("want a mount with the option " + lockedField)

	// lockedVolumeProbes holds the probes of [checkLockedVolumeProbes].
	lockedVolumeProbes = []lockedVolumeProbe{
		// The mount sets the locked mode.
		{mountCall("ctr", probeVolume, lockedField), true},
		// The options of the mount also set another field.
		{mountCall("ctr", probeVolume, `Owner: "root", `+lockedField), true},
		// An earlier mount of the chain keeps the default mode, and it
		// mounts another volume.
		{mountCall(mountCall("ctr", otherVolume, ""), probeVolume, lockedField), true},
		// The mount passes no options, so it keeps the default mode.
		{mountCall("ctr", probeVolume, ""), false},
		// The options of the mount do not set the mode.
		{mountCall("ctr", probeVolume, `Owner: "root"`), false},
		// The options of the mount set another mode.
		{mountCall("ctr", probeVolume, "Sharing: dagger.CacheSharingModeShared"), false},
		{mountCall("ctr", probeVolume, "Sharing: dagger.CacheSharingModePrivate"), false},
		// Only a later mount of the chain sets the locked mode, and it
		// mounts another volume.
		{mountCall(mountCall("ctr", probeVolume, ""), otherVolume, lockedField), false},
		// One of two mounts of the volume keeps the default mode.
		{mountCall(mountCall("ctr", probeVolume, lockedField), probeVolume, ""), false},
		// No call names the volume.
		{mountCall("ctr", otherVolume, lockedField), false},
		// The volume is the argument of another function, so the check
		// cannot read the mode of its mount.
		{fmt.Sprintf("lock(ctr, dag.CacheVolume(%q))", probeVolume), false},
	}
)

// TestVulnscanCachePragma verifies that the doc comments of
// [Nix.Vulnscan] and [Nix.VulnscanSarif] carry the cache pragma with
// the value never. Without the pragma, Dagger stores the result of a
// function and returns it to each later call with the same source. The
// later call then runs no grype command. So its report misses each CVE
// that the vulnerability database of grype gained after the first call.
//
// [Tests.TestVulnscanEachCall] shows that each call of Vulnscan runs
// grype. No test can show the same for VulnscanSarif, because the SARIF
// report of grype holds no value that differs between two scans. So
// this test reads the Go files of the default source of [Nix]. It fails
// when Dagger does not read the cache policy never from the doc comment
// of one of the two functions.
//
// Before it reads the Go files, the test runs [checkCachePragmaProbes]
// on fixed probe files. That self-check fails when the test passes a
// function with another cache policy or fails a function with the
// policy never. A check of the right Go files returns no error, so it
// cannot show that fault.
//
// +check
func (m *Tests) TestVulnscanCachePragma(ctx context.Context) error {
	err := checkCachePragmaProbes()
	if err != nil {
		return err
	}

	goFiles, err := rootGoFiles(ctx)
	if err != nil {
		return err
	}

	err = checkCachePragmas(goFiles, scanFunctions)
	if err != nil {
		return fmt.Errorf("checking the Go files of %s: %w", rootModulePath, err)
	}

	return nil
}

// checkCachePragmas parses goFiles, which maps the path of each Go file
// to its contents. names holds the Go names of methods of the type that
// nixTypeName names. The function returns an error that names each
// method of names that goFiles do not declare. The error also names
// each method of names whose doc comment fails [checkNeverCached].
func checkCachePragmas(goFiles map[string]string, names []string) error {
	docs := map[string]*ast.CommentGroup{}
	fset := token.NewFileSet()

	for _, file := range slices.Sorted(maps.Keys(goFiles)) {
		parsed, err := parser.ParseFile(fset, file, goFiles[file], parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", file, err)
		}

		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && receiverName(fn) == nixTypeName {
				docs[fn.Name.Name] = fn.Doc
			}
		}
	}

	var errs []error

	for _, name := range names {
		doc, ok := docs[name]
		if !ok {
			errs = append(errs, fmt.Errorf("the type %s has no method %s", nixTypeName, name))

			continue
		}

		err := checkNeverCached(doc)
		if err != nil {
			errs = append(errs, fmt.Errorf("the doc comment of %s: %w", name, err))
		}
	}

	return errors.Join(errs...)
}

// checkNeverCached returns nil when doc, the doc comment of a function,
// holds neverCachePragma on a line of its own and no cache pragma on
// another line. Dagger then reads the cache policy never from doc.
//
// Dagger finds each pragma of doc with pragmaRe, which also matches in
// the middle of a sentence. It keeps the last value of a pragma. So
// the function returns an error that quotes the line when pragmaRe
// reads a cache pragma from a line that is not neverCachePragma. It
// also returns an error when doc holds no cache pragma. Spaces and tabs
// at the start and at the end of a line do not count.
func checkNeverCached(doc *ast.CommentGroup) error {
	text := doc.Text()
	found := false

	for _, match := range pragmaRe.FindAllStringSubmatchIndex(text, -1) {
		if text[match[2]:match[3]] != "cache" {
			continue
		}

		// A match holds no newline before its end, so the line of the
		// match starts after the last newline before the match.
		start := strings.LastIndexByte(text[:match[0]], '\n') + 1
		line, _, _ := strings.Cut(text[start:], "\n")

		if strings.Trim(line, " \t") != neverCachePragma {
			return fmt.Errorf("the line %q holds a cache pragma and is not the line %s", line, neverCachePragma)
		}

		found = true
	}

	if !found {
		return fmt.Errorf("no line holds %s", neverCachePragma)
	}

	return nil
}

// checkCachePragmaProbes runs [checkCachePragmas] on the probe file of
// each probe of cachePragmaProbes. The file declares the type that
// nixTypeName names and one method, which carries the doc comment of
// the probe. The function returns an error when checkCachePragmas
// fails a doc comment from which Dagger reads the cache policy never,
// or passes another doc comment. It also returns an error when
// checkCachePragmas passes a method that the file does not declare.
func checkCachePragmaProbes() error {
	for _, probe := range cachePragmaProbes {
		file := fmt.Sprintf(
			"package main\n\ntype %s struct{}\n\n%s\nfunc (m *%s) %s() {}\n",
			nixTypeName, probe.doc, nixTypeName, cachePragmaProbeName,
		)
		goFiles := map[string]string{cachePragmaProbeGoPath: file}

		err := checkCachePragmas(goFiles, []string{cachePragmaProbeName})

		switch {
		case probe.never && err != nil:
			return fmt.Errorf("checking the probe file %q: %w", file, err)

		case !probe.never && err == nil:
			return fmt.Errorf("checkCachePragmas passed the probe file %q", file)
		}

		err = checkCachePragmas(goFiles, []string{"Missing"})
		if err == nil {
			return fmt.Errorf("checkCachePragmas passed a method that the probe file %q does not declare", file)
		}
	}

	return nil
}

// TestVulnscanDatabaseLocked verifies that the root module of the nix
// toolchain mounts the cache volume grypeVolume only with the sharing
// mode locked. The volume holds the vulnerability database of grype,
// and grype takes no lock on the database. With the default sharing
// mode, two scans that run at the same time each download a missing or
// outdated database. Each scan then replaces the database of the other
// scan. A scan fails when it opens the database between the delete and
// the rename of the other scan. With the locked mode, the engine runs
// the steps that mount the volume one at a time.
//
// A call of [Nix.Vulnscan] cannot show the fault. grype replaces the
// database only on an engine whose volume holds no database or an
// outdated one. This module cannot empty the volume, because Dagger
// gives each module its own cache volumes. So the test reads the Go
// files of the default source of [Nix], and it fails when
// [checkLockedVolume] fails those files.
//
// Before it reads the Go files, the test runs
// [checkLockedVolumeProbes] on fixed probe files. That self-check fails
// when the test passes a mount with another sharing mode or fails a
// mount with the locked mode. A check of the right Go files returns no
// error, so it cannot show that fault.
//
// +check
func (m *Tests) TestVulnscanDatabaseLocked(ctx context.Context) error {
	err := checkLockedVolumeProbes()
	if err != nil {
		return err
	}

	goFiles, err := rootGoFiles(ctx)
	if err != nil {
		return err
	}

	err = checkLockedVolume(goFiles, grypeVolume)
	if err != nil {
		return fmt.Errorf("checking the Go files of %s: %w", rootModulePath, err)
	}

	return nil
}

// checkLockedVolume parses goFiles, which maps the path of each Go file
// to its contents. It checks each use of the cache volume with the key
// volume. It returns nil when the files hold such a use, and each use
// is a mount with the sharing mode locked.
//
// A use is a call of CacheVolume whose first argument is the string
// literal volume, as [namesVolume] reports. A use passes when it is an
// argument of a call of WithMountedCache whose options set the locked
// mode, as [sharesLocked] reads them. The function returns an error
// that wraps errUnlockedVolume and gives the position of each other
// use. It also returns an error that wraps errUnlockedVolume when the
// files hold no use. So the files fail the check after someone renames
// the volume or passes its key in another way, such as in a constant.
func checkLockedVolume(goFiles map[string]string, volume string) error {
	fset := token.NewFileSet()

	// mounts holds each use that is an argument of a call of
	// WithMountedCache. It maps the use to whether the options of that
	// call set the locked mode.
	mounts := map[*ast.CallExpr]bool{}

	// uses holds each use of the volume.
	var uses []*ast.CallExpr

	for _, file := range slices.Sorted(maps.Keys(goFiles)) {
		parsed, err := parser.ParseFile(fset, file, goFiles[file], parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", file, err)
		}

		ast.Inspect(parsed, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			if namesVolume(call, volume) {
				uses = append(uses, call)
			}

			if selectorName(call.Fun) != "WithMountedCache" {
				return true
			}

			for _, arg := range call.Args {
				use, ok := arg.(*ast.CallExpr)
				if ok && namesVolume(use, volume) {
					mounts[use] = sharesLocked(call)
				}
			}

			return true
		})
	}

	if len(uses) == 0 {
		return fmt.Errorf("no call of CacheVolume names the cache volume %q: %w", volume, errUnlockedVolume)
	}

	var errs []error

	for _, use := range uses {
		var fault string

		switch locked, mounted := mounts[use]; {
		case !mounted:
			fault = "is not an argument of WithMountedCache"

		case !locked:
			fault = "has a mount that sets another sharing mode or none"

		default:
			continue
		}

		errs = append(errs, fmt.Errorf(
			"%s: the cache volume %q %s: %w",
			fset.Position(use.Pos()), volume, fault, errUnlockedVolume,
		))
	}

	return errors.Join(errs...)
}

// selectorName returns the name that the selector expression expr
// selects, such as CacheVolume for dag.CacheVolume. It returns "" when
// expr is not a selector expression.
func selectorName(expr ast.Expr) string {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return ""
	}

	return sel.Sel.Name
}

// namesVolume reports whether call is a call of a method with the name
// CacheVolume whose first argument is the string literal volume.
func namesVolume(call *ast.CallExpr, volume string) bool {
	return selectorName(call.Fun) == "CacheVolume" && len(call.Args) > 0 && stringValue(call.Args[0]) == volume
}

// sharesLocked reports whether the options of call, a call of
// WithMountedCache, set the sharing mode locked. The options are an
// argument that is a composite literal, such as a literal of the type
// dagger.ContainerWithMountedCacheOpts. They set the locked mode when
// the value of their Sharing field is a selector expression that
// selects lockedModeName. The function does not read options that the
// call passes in another way, such as in a variable.
func sharesLocked(call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		opts, ok := arg.(*ast.CompositeLit)
		if !ok {
			continue
		}

		for _, elt := range opts.Elts {
			field, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}

			key, ok := field.Key.(*ast.Ident)
			if ok && key.Name == "Sharing" && selectorName(field.Value) == lockedModeName {
				return true
			}
		}
	}

	return false
}

// mountCall returns the Go expression of a call of WithMountedCache on
// the expression recv. The call mounts the cache volume with key.
// fields holds the fields of the options of the call. When fields is
// empty, the call passes no options.
func mountCall(recv, key, fields string) string {
	call := fmt.Sprintf("%s.WithMountedCache(%q, dag.CacheVolume(%q)", recv, "/"+key, key)
	if fields != "" {
		call += ", dagger.ContainerWithMountedCacheOpts{" + fields + "}"
	}

	return call + ")"
}

// checkLockedVolumeProbes runs [checkLockedVolume] on the probe file of
// each probe of lockedVolumeProbes, for the cache volume probeVolume.
// The file declares one function, which returns the expression of the
// probe. The function returns an error when checkLockedVolume fails a
// probe whose locked field is true, or passes another probe. It also
// returns an error when the error for a probe does not wrap
// errUnlockedVolume, such as the error for a file that does not parse.
func checkLockedVolumeProbes() error {
	for _, probe := range lockedVolumeProbes {
		file := "package main\n\nfunc probe(ctr *dagger.Container) *dagger.Container {\n\treturn " +
			probe.expr + "\n}\n"

		err := checkLockedVolume(map[string]string{lockedVolumeProbeGoPath: file}, probeVolume)

		switch {
		case probe.locked && err != nil:
			return fmt.Errorf("checking the probe file %q: %w", file, err)

		case !probe.locked && err == nil:
			return fmt.Errorf("checkLockedVolume passed the probe file %q", file)

		case !probe.locked && !errors.Is(err, errUnlockedVolume):
			return fmt.Errorf("checking the probe file %q, want an error for the sharing mode: %w", file, err)
		}
	}

	return nil
}

// TestVulnscanEachCall verifies that each call of [Nix.Vulnscan] runs
// grype, even when an earlier call scanned the same source. The source
// of the test holds only the flake.nix file homeFlake and the
// .grype.yaml file jsonGrypeConfig. So the test does not build the
// flake of the repository, and each report holds the time of its scan.
//
// The test calls Vulnscan two times with that source. It fails when
// both reports hold the same time, because one scan then wrote both
// reports. Dagger returned the stored result of the first call, or the
// engine reused the result of the step that runs grype. In both cases,
// the second call did not load the vulnerability database again.
//
// +check
func (m *Tests) TestVulnscanEachCall(ctx context.Context) error {
	nix := nixWithGrypeConfig(jsonGrypeConfig)

	first, err := scanTime(ctx, nix)
	if err != nil {
		return fmt.Errorf("reading the time of the first scan: %w", err)
	}

	second, err := scanTime(ctx, nix)
	if err != nil {
		return fmt.Errorf("reading the time of the second scan: %w", err)
	}

	if first == second {
		return fmt.Errorf("two calls of Vulnscan returned the report of one scan, which ran at %s", first)
	}

	return nil
}

// scanTime calls [Nix.Vulnscan] of nix and returns the time of the scan
// from the report. The source of nix must make grype write its report
// as JSON, as jsonGrypeConfig does. The function returns an error when
// the report is not JSON or holds no time.
func scanTime(ctx context.Context, nix *dagger.Nix) (string, error) {
	report, err := nix.Vulnscan(ctx)
	if err != nil {
		return "", fmt.Errorf("scanning: %w", err)
	}

	var doc struct {
		Descriptor struct {
			Timestamp string `json:"timestamp"`
		} `json:"descriptor"`
	}

	err = json.Unmarshal([]byte(report), &doc)
	if err != nil {
		return "", fmt.Errorf("parsing the report of grype: %w", err)
	}

	if doc.Descriptor.Timestamp == "" {
		return "", errors.New("the report of grype holds no time of the scan")
	}

	return doc.Descriptor.Timestamp, nil
}
