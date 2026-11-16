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
)

// cachePragmaProbe is a probe of [checkCachePragmaProbes].
type cachePragmaProbe struct {
	// doc holds the doc comment of the function of the probe file.
	doc string

	// never reports whether Dagger reads the cache policy never from
	// doc.
	never bool
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
