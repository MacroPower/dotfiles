package main

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"regexp"
	"slices"
	"strconv"
)

const (
	// profileProbeGoPath is the path that [checkProfileProbes] gives
	// each probe file.
	profileProbeGoPath = "probe.go"

	// profileFreeProbe is a Go file that runs no `nix profile` command.
	// Its comment names the command, and a checker that reads comments
	// fails the probe. Its commands run other subcommands of nix. One
	// command passes the argument profile to `nix search`, and the
	// script of another command names a package whose name starts with
	// profile. So a checker that reads the word profile anywhere after
	// nix as the subcommand fails the probe too.
	profileFreeProbe = `package main

// probe holds commands that get grype without nix profile install.
var probe = [][]string{
	{"nix", "run", "nixpkgs#grype"},
	{"nix", "build", "nixpkgs#grype", "--out-link", "/tmp/grype"},
	{"nix", "search", "nixpkgs", "profile"},
	{"sh", "-c", "nix run nixpkgs#profile-cleaner"},
}
`
)

var (
	// errProfileCommand is the error of [checkProfileCommands] for each
	// `nix profile` command.
	errProfileCommand = errors.New("`nix profile` command, which uses the profile on a cache volume")

	// profileCommandRe matches the command `nix profile` in a string,
	// such as the script of an `sh -c` command.
	profileCommandRe = regexp.MustCompile(`\bnix\s+profile\b`)

	// profileCommandProbes holds Go expressions that each hold one
	// `nix profile` command.
	profileCommandProbes = []string{
		// The arguments of a command.
		`[]string{"nix", "profile", "install", "nixpkgs#grype"}`,
		// An entry of a list of such arguments.
		`[][]string{{"nix", "run", "nixpkgs#grype"}, {"nix", "profile", "list"}}`,
		// The script of an sh -c command.
		`[]string{"sh", "-c", "nix profile install nixpkgs#grype"}`,
		// A format string with two spaces after nix.
		`fmt.Sprintf("nix  profile upgrade %s", name)`,
	}
)

// TestProfileCommands verifies that no Go file of the root module of
// the nix toolchain runs a `nix profile` command. The default profile
// of the container of the toolchain is in /nix/var/nix, which the
// toolchain mounts from a cache volume. So the profile outlives each
// run. After nixpkgs-unstable changes a package, `nix profile install`
// fails on a profile that holds the earlier copy of the package. An
// engine with an empty cache volume passes that command. So a call of
// the function that runs the command cannot show the fault there.
//
// The test reads the Go files from the default source of [Nix]. It
// fails on a composite literal that holds the strings "nix" and
// "profile" one after the other, as the arguments of a command hold
// them. It also fails on a string literal that holds the command
// `nix profile`, such as the script of an `sh -c` command.
//
// Before it reads the Go files, the test runs [checkProfileProbes] on
// fixed probe files. That self-check fails when the test passes a file
// with a `nix profile` command or fails a file without one. A check of
// the right Go files returns no error, so it cannot show that fault.
//
// +check
func (m *Tests) TestProfileCommands(ctx context.Context) error {
	err := checkProfileProbes()
	if err != nil {
		return err
	}

	goFiles, err := rootGoFiles(ctx)
	if err != nil {
		return err
	}

	err = checkProfileCommands(goFiles)
	if err != nil {
		return fmt.Errorf("checking the Go files of %s: %w", rootModulePath, err)
	}

	return nil
}

// checkProfileCommands parses goFiles, which maps the path of each Go
// file to its contents. It returns an error that wraps
// errProfileCommand and names the position of each `nix profile`
// command of the files. A command is a pair of elements of a composite
// literal that are the string literals "nix" and "profile", one after
// the other. A command is also a string literal that profileCommandRe
// matches. The function does not read comments.
func checkProfileCommands(goFiles map[string]string) error {
	fset := token.NewFileSet()

	var errs []error

	for _, file := range slices.Sorted(maps.Keys(goFiles)) {
		parsed, err := parser.ParseFile(fset, file, goFiles[file], parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", file, err)
		}

		ast.Inspect(parsed, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BasicLit:
				if profileCommandRe.MatchString(stringValue(n)) {
					errs = append(errs, fmt.Errorf("%s: %w", fset.Position(n.Pos()), errProfileCommand))
				}

			case *ast.CompositeLit:
				for i := 1; i < len(n.Elts); i++ {
					if stringValue(n.Elts[i-1]) == "nix" && stringValue(n.Elts[i]) == "profile" {
						errs = append(errs, fmt.Errorf("%s: %w", fset.Position(n.Elts[i-1].Pos()), errProfileCommand))
					}
				}
			}

			return true
		})
	}

	return errors.Join(errs...)
}

// stringValue returns the value of the string literal expr. It returns
// "" when expr is not a string literal.
func stringValue(expr ast.Expr) string {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}

	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}

	return value
}

// checkProfileProbes runs [checkProfileCommands] on probe files. It
// returns an error when checkProfileCommands fails the file
// profileFreeProbe. It also returns an error when checkProfileCommands
// passes the file of an expression of profileCommandProbes, or when
// the error for that file does not wrap errProfileCommand. A file that
// does not parse also fails checkProfileCommands, but that error does
// not wrap errProfileCommand.
func checkProfileProbes() error {
	err := checkProfileCommands(map[string]string{profileProbeGoPath: profileFreeProbe})
	if err != nil {
		return fmt.Errorf("checking the probe file %q: %w", profileFreeProbe, err)
	}

	for _, probe := range profileCommandProbes {
		file := "package main\n\nvar probe = " + probe + "\n"

		err = checkProfileCommands(map[string]string{profileProbeGoPath: file})
		if err == nil {
			return fmt.Errorf("checkProfileCommands passed the probe file %q", file)
		}

		if !errors.Is(err, errProfileCommand) {
			return fmt.Errorf("checking the probe file %q, want an error for a `nix profile` command: %w", file, err)
		}
	}

	return nil
}
