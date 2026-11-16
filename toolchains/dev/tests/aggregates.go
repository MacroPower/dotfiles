// The test module of each toolchain holds a copy of this file. Apply
// each change to every copy.

package main

import (
	"embed"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing/fstest"
)

// aggregateProbeGo is the Go file of a stand-in for a test module,
// without the method of a probe. [checkAggregateProbes] puts the doc
// comment of that method in place of the word DOC and its name in
// place of the word NAME. All calls the method, and AllFull calls
// All. So All and AllFull call each test that they must call, and
// [checkAggregates] fails the file only for the doc comment of the
// method.
const aggregateProbeGo = `package main

type Tests struct{}

// All runs each test that carries +check, such as TestChecked.
func (m *Tests) All() {
	m.TestChecked()
	m.NAME()
}

// AllFull runs every test. It adds to All the tests that do not carry
// +check, such as TestPlain.
func (m *Tests) AllFull() {
	m.All()
	m.TestPlain()
}

// TestChecked is a check.
//
// +check
func (m *Tests) TestChecked() {}

// TestPlain is not a check.
func (m *Tests) TestPlain() {}

DOC
func (m *Tests) NAME() {}
`

// aggregateProbe is a probe of [checkAggregateProbes]. It holds the
// method that completes the stand-in module of aggregateProbeGo.
type aggregateProbe struct {
	// name holds the name of the method.
	name string

	// doc holds the doc comment of the method.
	doc string

	// line holds the line of doc that [carriesCheck] must reject.
	// Dagger reads the pragma +check from the line, and the line holds
	// more than the pragma. The error of [checkAggregates] must name
	// the method and quote the line. An empty line means that
	// checkAggregates must return no error.
	line string
}

var (
	// sources holds the Go files of the module, which
	// [Tests.TestAggregates] parses.
	//
	//go:embed *.go
	sources embed.FS

	// pragmaRe matches a pragma in a comment as Dagger does. It is a
	// copy of pragmaCommentRegexp, which Dagger v0.20.8 declares in
	// cmd/codegen/generator/go/templates/modules.go. The pattern
	// matches a +, the word after it, and then an = or the end of the
	// line. Its first group captures the word, which is the name of
	// the pragma. The pattern has no anchor at the start of a line, so
	// it also matches in the middle of a sentence.
	pragmaRe = regexp.MustCompile(`[ \t]*\+[ \t]*(\S+?)(?:(=[ \t]*)|(?:\r?\n|$))`)

	// aggregateProbes holds the probes of [checkAggregateProbes].
	aggregateProbes = []aggregateProbe{
		// The pragma stands on a line of its own.
		{"TestProbe", "// TestProbe is a check.\n//\n// +check", ""},
		// A word or a comma follows +check on its line, so Dagger reads
		// no pragma from the comment.
		{"TestProbe", "// TestProbe does not carry +check because it needs root.", ""},
		{"TestProbe", "// TestProbe does not carry +check, so only AllFull calls it.", ""},
		// Dagger does not publish an unexported method, so it does not
		// read the doc comment of the method.
		{"probe", "// probe is a helper, so Dagger does not read +check\n// from this comment.", ""},
		// A sentence wraps after +check, so Dagger makes the method a
		// check. A checker that reads the pragma only from a line of
		// its own finds no pragma in the comment, and it passes the
		// probe.
		{
			"TestProbe",
			"// TestProbe needs root, so it does not carry +check\n// and only AllFull calls it.",
			"TestProbe needs root, so it does not carry +check",
		},
		// Dagger publishes each exported method, whatever its name. So
		// it also reads the pragma from the doc comment of a method
		// that is not a test, such as AllFull.
		{
			"Probe",
			"// Probe runs each test that does not carry +check\n// in parallel.",
			"Probe runs each test that does not carry +check",
		},
		// Dagger reads a pragma before an = at any place in a line.
		// Here it reads the value false, so the method is not a check.
		{
			"TestProbe",
			"// TestProbe sets +check=false in a sentence.",
			"TestProbe sets +check=false in a sentence.",
		},
		// Dagger also reads the pragma when a space follows the + sign.
		{"TestProbe", "// TestProbe is a check.\n//\n// + check", "+ check"},
		// The method carries the pragma on a line of its own. Dagger
		// still drops the +check that ends the first line from the
		// description of the method.
		{
			"TestProbe",
			"// TestProbe is a check, and its last line holds +check\n// alone.\n//\n// +check",
			"TestProbe is a check, and its last line holds +check",
		},
	}
)

// TestAggregates verifies that [Tests.All] calls every test with the
// pragma +check and that [Tests.AllFull] calls every test of the
// module, directly or through [Tests.All]. An aggregate function
// reports success without running a test that it does not call.
//
// Dagger reads +check at the end of any line of a doc comment and
// before any equals sign, even in the middle of a sentence. So a
// sentence that wraps after +check makes its function a check, and
// Dagger removes the word from the description of the function. The
// test therefore also fails when Dagger reads the pragma from a line
// that holds more than +check, in the doc comment of an exported
// method of [Tests].
//
// Before it reads the module, the test runs [checkAggregateProbes] on
// fixed probe methods. That self-check fails when the test passes a
// doc comment with such a line, or fails a doc comment without one.
// A check of a right module returns no error, so it cannot show that
// fault.
//
// +check
func (m *Tests) TestAggregates() error {
	err := checkAggregateProbes()
	if err != nil {
		return err
	}

	return checkAggregates(sources)
}

// checkAggregateProbes runs [checkAggregates] on the stand-in module
// of each probe of aggregateProbes. It returns an error when
// checkAggregates fails a probe with an empty line. It also returns an
// error when checkAggregates passes a probe with a line, or when the
// error for that probe does not name the method and quote the line.
func checkAggregateProbes() error {
	for _, probe := range aggregateProbes {
		source := strings.NewReplacer("DOC", probe.doc, "NAME", probe.name).Replace(aggregateProbeGo)

		err := checkAggregates(fstest.MapFS{"probe.go": {Data: []byte(source)}})

		if probe.line == "" {
			if err != nil {
				return fmt.Errorf("checking the probe module %q: %w", source, err)
			}

			continue
		}

		want := fmt.Sprintf("the doc comment of %s: the line %q", probe.name, probe.line)

		if err == nil {
			return fmt.Errorf("checkAggregates passed the probe module %q, want an error that holds %q", source, want)
		}

		if !strings.Contains(err.Error(), want) {
			return fmt.Errorf("checking the probe module %q, want an error that holds %q: %w", source, want, err)
		}
	}

	return nil
}

// checkAggregates parses the Go files at the root of fsys. It returns an
// error that names each test that All or AllFull must call and does not
// call. A test is a method of [Tests] whose name starts with Test.
//
// The error also names each exported method of Tests whose doc comment
// [carriesCheck] rejects. Dagger publishes each exported method as a
// function, and it reads the pragmas of a function from the doc
// comment. So the function reads the doc comment of All, of AllFull,
// and of each other exported method, not only of the tests. It counts
// a test with such a doc comment as a test without the pragma, which
// AllFull must still call.
func checkAggregates(fsys fs.FS) error {
	names, err := fs.Glob(fsys, "*.go")
	if err != nil {
		return fmt.Errorf("listing sources: %w", err)
	}

	// tests maps the name of each test to whether the test carries the
	// pragma +check.
	tests := map[string]bool{}
	// called maps All and AllFull to the methods that each one calls.
	called := map[string]map[string]bool{}
	fset := token.NewFileSet()

	var errs []error

	for _, name := range names {
		src, err := fs.ReadFile(fsys, name)
		if err != nil {
			return fmt.Errorf("reading %s: %w", name, err)
		}

		file, err := parser.ParseFile(fset, name, src, parser.ParseComments)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", name, err)
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !isTestsMethod(fn) || !fn.Name.IsExported() {
				continue
			}

			isCheck, err := carriesCheck(fn.Doc)
			if err != nil {
				errs = append(errs, fmt.Errorf("reading the doc comment of %s: %w", fn.Name.Name, err))
			}

			switch {
			case fn.Name.Name == "All" || fn.Name.Name == "AllFull":
				called[fn.Name.Name] = receiverMethods(fn)
			case strings.HasPrefix(fn.Name.Name, "Test"):
				tests[fn.Name.Name] = isCheck
			}
		}
	}

	all, allFull := called["All"], called["AllFull"]

	for _, name := range slices.Sorted(maps.Keys(tests)) {
		switch {
		case tests[name] && !all[name]:
			errs = append(errs, fmt.Errorf("All does not call %s, which carries +check", name))
		case !allFull[name] && (!allFull["All"] || !all[name]):
			errs = append(errs, fmt.Errorf("AllFull does not call %s", name))
		}
	}

	return errors.Join(errs...)
}

// isTestsMethod reports whether fn is a method of [Tests].
func isTestsMethod(fn *ast.FuncDecl) bool {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return false
	}

	typ := fn.Recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}

	ident, ok := typ.(*ast.Ident)

	return ok && ident.Name == "Tests"
}

// receiverMethods returns the names that the body of the method fn
// selects on the receiver of fn, as a call or as a method value.
func receiverMethods(fn *ast.FuncDecl) map[string]bool {
	methods := map[string]bool{}

	recv := fn.Recv.List[0].Names
	if len(recv) != 1 || fn.Body == nil {
		return methods
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		if x, ok := sel.X.(*ast.Ident); ok && x.Name == recv[0].Name {
			methods[sel.Sel.Name] = true
		}

		return true
	})

	return methods
}

// carriesCheck reports whether doc, the doc comment of a function,
// carries the pragma +check on a line of its own. Dagger makes such a
// function a check, which `dagger check` runs.
//
// Dagger finds each pragma of doc with pragmaRe, which also matches in
// the middle of a sentence. So carriesCheck returns an error when
// pragmaRe reads the pragma from a line that holds more than the
// pragma, such as the line of a sentence that wraps after +check. The
// error quotes the line. Spaces and tabs at the start of a line do
// not count as more. For each doc without such a line, carriesCheck
// and Dagger agree on whether the function is a check.
func carriesCheck(doc *ast.CommentGroup) (bool, error) {
	text := doc.Text()
	carries := false

	for _, match := range pragmaRe.FindAllStringSubmatchIndex(text, -1) {
		if text[match[2]:match[3]] != "check" {
			continue
		}

		// A match holds no newline before its end, so the line of the
		// match starts after the last newline before the match.
		start := strings.LastIndexByte(text[:match[0]], '\n') + 1
		line, _, _ := strings.Cut(text[start:], "\n")

		if strings.Trim(line, " \t") != "+check" {
			return false, fmt.Errorf(
				"the line %q holds more than +check, and Dagger reads +check as the pragma"+
					" at the end of a line or before =",
				line,
			)
		}

		carries = true
	}

	return carries, nil
}
