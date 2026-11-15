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
	"slices"
	"strings"
)

// sources holds the Go files of the module, which
// [Tests.TestAggregates] parses.
//
//go:embed *.go
var sources embed.FS

// TestAggregates verifies that [Tests.All] calls every test with the
// pragma +check and that [Tests.AllFull] calls every test of the
// module, directly or through [Tests.All]. An aggregate function
// reports success without running a test that it does not call.
//
// +check
func (m *Tests) TestAggregates() error {
	return checkAggregates(sources)
}

// checkAggregates parses the Go files at the root of fsys. It returns an
// error that names each test that All or AllFull must call and does not
// call. A test is a method of [Tests] whose name starts with Test.
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
			if !ok || !isTestsMethod(fn) {
				continue
			}

			switch {
			case fn.Name.Name == "All" || fn.Name.Name == "AllFull":
				called[fn.Name.Name] = receiverMethods(fn)
			case strings.HasPrefix(fn.Name.Name, "Test"):
				tests[fn.Name.Name] = carriesCheck(fn.Doc)
			}
		}
	}

	all, allFull := called["All"], called["AllFull"]

	var errs []error

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

// carriesCheck reports whether a line of doc holds only the +check
// pragma, which makes `dagger check` run the function.
func carriesCheck(doc *ast.CommentGroup) bool {
	return slices.ContainsFunc(strings.Split(doc.Text(), "\n"), func(line string) bool {
		return strings.TrimSpace(line) == "+check"
	})
}
