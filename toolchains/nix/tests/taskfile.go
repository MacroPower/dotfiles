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
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// nixTaskfilePath is the path, in the project source, of the Taskfile
	// whose tasks run the functions of the nix toolchain.
	nixTaskfilePath = "taskfiles/nix/Taskfile.yaml"

	// nixTypeName is the name of the main object of the nix toolchain.
	// Dagger publishes each exported method of that type as a function
	// of the toolchain.
	nixTypeName = "Nix"

	// taskfileProbeGoPath is the path that [checkTaskfileProbes] gives
	// the Go file taskfileProbeGo.
	taskfileProbeGoPath = "probe.go"

	// taskfileProbeGo is a Go file of a stand-in for the nix toolchain.
	// Its method Plain lacks the +check pragma, and its methods Checked
	// and TwiceChecked carry the pragma. The dagger CLI name of
	// TwiceChecked is twice-checked, and the file declares no function
	// named twice.
	taskfileProbeGo = `package main

type Nix struct{}

// Plain is not a check.
func (m *Nix) Plain() {}

// Checked is a check.
//
// +check
func (m *Nix) Checked() {}

// TwiceChecked is a check with a name of two words.
//
// +check
func (m *Nix) TwiceChecked() {}
`

	// commandEnd holds the characters other than a newline that end a
	// match of daggerCommandRe and each name that checkCommandRe and
	// callCommandRe capture. They are the quotes, the backtick, the
	// parentheses, and the shell operators ;, &, and |. The shell ends a
	// command at only some of them. So checkCommandRe and callCommandRe
	// each list the characters that can follow the name in a command.
	commandEnd = "'\"`;&|()"

	// runnableProbeCmds holds entries of the cmds list of a task. Each
	// command of the entries names a function of taskfileProbeGo that
	// the command runs. A pattern of [checkTaskfileCommands] that ends a
	// name at a hyphen reads twice-checked as twice. So the commands
	// that name twice-checked fail that pattern.
	//
	// The entries that hold shell operators end a command of each kind
	// at each of the operators ;, &, |, and ). A pattern that does not
	// end a command at one of these operators fails an entry. Each
	// operator follows a name with no space between them, so a pattern
	// that reads an operator into a name fails too. In one entry, a
	// command follows an & with no space between them. A checker that
	// reads the first character of that command into the match of the
	// command before the & fails the entry.
	//
	// The literal block holds a shell comment that names Plain, which
	// lacks the pragma, so a checker that reads comment lines fails the
	// probe. Its last command passes a flag to the function after its
	// name, which a `dagger call` command allows.
	runnableProbeCmds = "- dagger check nix:checked\n" +
		"- cmd: dagger check nix:twice-checked\n" +
		"- dagger call nix plain\n" +
		"- dagger check nix:checked&& dagger call nix plain;\n" +
		"- dagger check nix:checked; dagger call nix plain| tee log\n" +
		"- dagger call nix plain& dagger check nix:twice-checked| tee log\n" +
		"- echo \"$(dagger check nix:checked)\" \"$(dagger call nix plain)\"\n" +
		"- dagger check nix:checked &dagger check nix:twice-checked\n" +
		"- |\n" +
		"  # dagger check nix:plain\n" +
		"  dagger call nix twice-checked --flag"
)

// taskfileProbe is a probe of [checkTaskfileProbes] that the nix
// toolchain cannot run as written.
type taskfileProbe struct {
	// cmds holds the YAML of an entry of the cmds list of a task.
	cmds string

	// command holds a command of cmds as Task runs it. The error of
	// [checkTaskfileCommands] for cmds must quote the command.
	command string
}

var (
	// unrunnableProbes holds probes whose command names a function of
	// taskfileProbeGo that the command does not run.
	unrunnableProbes = []taskfileProbe{
		// Plain lacks the +check pragma.
		{"- dagger check nix:plain", "dagger check nix:plain"},
		// taskfileProbeGo declares no function named missing.
		{"- dagger check nix:missing", "dagger check nix:missing"},
		{"- dagger call nix missing", "dagger call nix missing"},
		// The dagger CLI name of TwiceChecked holds a hyphen between
		// its words, so Dagger finds no function named twicechecked.
		{"- dagger check nix:twicechecked", "dagger check nix:twicechecked"},
		{"- dagger call nix twicechecked", "dagger call nix twicechecked"},
		// Dagger passes a command of several patterns when one pattern
		// matches a check, so the command passes without running
		// Plain.
		{"- dagger check nix:checked nix:plain", "dagger check nix:checked nix:plain"},
		// YAML joins the lines of a plain scalar, and of a folded block
		// scalar, into one command.
		{"- dagger check nix:checked\n  nix:plain", "dagger check nix:checked nix:plain"},
		{"- >\n  dagger check nix:checked\n  nix:plain", "dagger check nix:checked nix:plain"},
		// A flag before the pattern hides the pattern from
		// checkCommandRe.
		{"- dagger check --fail-fast nix:plain", "dagger check --fail-fast nix:plain"},
		// A flag of dagger before the subcommand hides the command
		// from callCommandRe.
		{"- dagger --progress plain call nix missing", "dagger --progress plain call nix missing"},
		// Dagger runs `dagger checks` as an alias of `dagger check`, and
		// checkCommandRe does not read the alias.
		{"- dagger checks nix:plain", "dagger checks nix:plain"},
		// The shell joins a line that ends in a backslash to the next
		// line. So each of these literal blocks runs one command, which
		// checkCommandRe does not read.
		{"- |\n  dagger \\\n    check nix:plain", "dagger \\\n  check nix:plain"},
		{
			"- |\n  dagger \\\n    --progress \\\n    plain \\\n    check nix:plain",
			"dagger \\\n  --progress \\\n  plain \\\n  check nix:plain",
		},
		// The shell also runs a second command on the line of a first
		// command. The second command follows a shell operator, or it
		// sits in a command substitution.
		{
			"- dagger call nix plain && dagger check nix:checked nix:plain",
			"dagger check nix:checked nix:plain",
		},
		{
			"- dagger call nix plain; dagger check nix:checked nix:plain",
			"dagger check nix:checked nix:plain",
		},
		{
			"- dagger call nix plain | dagger check nix:checked nix:plain",
			"dagger check nix:checked nix:plain",
		},
		{"- dagger check nix:checked &dagger check nix:plain", "dagger check nix:plain"},
		// A right command later on the line does not stand in for the
		// command before it.
		{"- dagger checks nix:plain; dagger check nix:checked", "dagger checks nix:plain"},
		{
			"- dagger --progress plain call nix missing; dagger call nix plain",
			"dagger --progress plain call nix missing",
		},
		{
			"- dagger call nix plain $(dagger check nix:checked nix:plain)",
			"dagger check nix:checked nix:plain",
		},
		{
			"- dagger call nix plain `dagger check nix:checked nix:plain`",
			"dagger check nix:checked nix:plain",
		},
		// The shell reads more words of a command after a quote, a
		// backtick, or the redirect &>. So each of these commands
		// passes a second pattern or a flag to `dagger check`.
		{"- dagger check nix:checked 'nix:plain'", "dagger check nix:checked"},
		{"- dagger check nix:checked \"--fail-fast\"", "dagger check nix:checked"},
		{"- dagger check nix:checked `echo nix:plain`", "dagger check nix:checked"},
		{"- dagger check nix:checked &>/dev/null nix:plain", "dagger check nix:checked"},
		// A quote right after the name of a function adds the quoted
		// text to the name, so the command calls plainx.
		{"- dagger call nix plain'x'", "dagger call nix plain"},
	}

	// commentLineRe matches each line whose first character other than
	// a space or a tab is #. The shell reads such a line of a literal
	// block scalar as a comment. The YAML decoder drops the comments of
	// the YAML itself.
	commentLineRe = regexp.MustCompile(`(?m)^[ \t]*#.*$`)

	// daggerCommandRe matches each `dagger check` and `dagger call`
	// command, with any flags of dagger before the subcommand. It also
	// matches `dagger checks`, which Dagger runs as an alias of
	// `dagger check`. The shell joins a line that ends in a backslash to
	// the next line. So between the words before the subcommand, a match
	// reads a backslash and a newline as a space. A match ends at the
	// end of its line or at a character of commandEnd. So each command
	// of a line gets a match of its own.
	// [checkTaskfileCommands] fails each match at whose start neither
	// checkCommandRe nor callCommandRe matches. Neither of them reads the
	// alias or a backslash between words.
	daggerCommandRe = regexp.MustCompile(
		`\bdagger(?:[\s\\]+-\S+(?:[\s\\]+[^\s-]\S*)?)*[\s\\]+(?:checks?|call)\b[^\n` + commandEnd + `]*`,
	)

	// checkCommandRe matches a command that selects one check of the nix
	// toolchain by name. Its group holds the name. The name ends the
	// command. After the name and any spaces and tabs comes the end of
	// the line or one of the shell operators ;, |, ), and &, but not the
	// redirect &>. The shell reads more words of the command after a
	// quote, a backtick, or &>, such as a second pattern. checkCommandRe
	// cannot tell a quote that ends a command from one that opens a word
	// of it. So it does not match a command that ends at a quote, such as
	// the command in sh -c 'dagger check nix:lint'. It matches only at the
	// start of a text.
	checkCommandRe = regexp.MustCompile(
		`(?m)\Adagger[ \t]+check[ \t]+nix:([^\s` + commandEnd + `]+)[ \t]*(?:$|[;|)]|&(?:$|[^>]))`,
	)

	// callCommandRe matches a command that calls a function of the nix
	// toolchain. Its group holds the name of the function. After the name
	// comes a space, a tab, the end of the line, or one of the shell
	// operators ;, &, |, and ). A quote or a backtick right after the
	// name adds the quoted text to the name, so callCommandRe does not
	// match such a command. A `dagger call` command passes flags and
	// chained calls after the name. It matches only at the start of a
	// text.
	callCommandRe = regexp.MustCompile(
		`(?m)\Adagger[ \t]+call[ \t]+nix[ \t]+([^\s` + commandEnd + `]+)(?:$|[\s;&|)])`,
	)
)

// TestTaskfileCommands verifies that each dagger command of the Taskfile
// at nixTaskfilePath names a function of [Nix] that the command runs.
// A `dagger check nix:<name>` command must name a function that carries
// the +check pragma. The pragma makes a function a check, and a
// `dagger check` pattern selects only checks. A
// `dagger call nix <name>` command must name an exported method of
// [Nix]. Each <name> must spell the function as the dagger CLI lists
// it, such as build-home for BuildHome.
//
// The test fails on each `dagger check` or `dagger call` command of
// another form, such as a command with a flag before the name of the
// function or a command that runs the alias `dagger checks`. A
// `dagger check` command must also end at its one pattern.
// Dagger passes a command of several patterns when one pattern matches
// a check. So a second pattern can name a function that the command
// does not run.
//
// In a `dagger check` command, the end of the line or a shell operator,
// such as && or a pipe, must follow the pattern. In a `dagger call`
// command, a space or a shell operator must follow the name of the
// function. The shell reads more words of a command after a quote, a
// backtick, or the redirect &>. The test cannot tell a quote that ends
// a command from one that opens a word of it. So it also fails a
// command that ends at a quote, such as the command in
// sh -c 'dagger check nix:lint'.
//
// The test reads each command as Task runs it. It decodes the Taskfile
// as YAML, which joins the lines of each plain or folded scalar. Then it
// reads every string of the result, such as the desc of a task. A
// command ends at the end of its line or at a shell operator, such as
// && or a pipe. The test skips each line that starts with # after any
// spaces and tabs, because the shell reads such a line as a comment. It
// reads the Taskfile and the Go files of the root module of the nix
// toolchain from the default source of [Nix].
//
// Before it reads the Taskfile, the test runs [checkTaskfileProbes] on
// fixed probe commands. That self-check fails when the test passes a
// command that names a function that the command does not run, or a
// command of another form. A check of a right Taskfile returns no
// error, so it cannot show that fault. The probes include commands that
// YAML joins onto one line and commands that share a line with another
// command. They also include commands that go on after a quote, a
// backtick, or &>. The self-check also fails when the test fails a
// right command or misreads a name of two words.
//
// +check
func (m *Tests) TestTaskfileCommands(ctx context.Context) error {
	err := checkTaskfileProbes()
	if err != nil {
		return err
	}

	taskfile, err := sourceFile(ctx, nixTaskfilePath)
	if err != nil {
		return err
	}

	pattern := rootModulePath + "/*.go"

	files, err := dag.Nix().Source().Glob(ctx, pattern)
	if err != nil {
		return fmt.Errorf("listing %s: %w", pattern, err)
	}

	goFiles := make(map[string]string, len(files))

	for _, file := range files {
		goFiles[file], err = sourceFile(ctx, file)
		if err != nil {
			return err
		}
	}

	err = checkTaskfileCommands(taskfile, goFiles)
	if err != nil {
		return fmt.Errorf("checking %s: %w", nixTaskfilePath, err)
	}

	return nil
}

// checkTaskfileCommands returns an error that names each dagger command
// of taskfile that the nix toolchain cannot run as written. goFiles maps
// the path of each Go file of the root module of the toolchain to its
// contents. The function reads the strings that [taskfileText] returns
// for taskfile, and it skips each line that commentLineRe matches. It
// finds each command with daggerCommandRe. Then it matches
// checkCommandRe and callCommandRe at the start of each command, not
// across the whole text. A match of checkCommandRe that ends at & holds
// the first character of the next command. So a scan of the whole text
// misses a command that follows an & with no space between them. A
// `dagger check nix:<name>` command passes only when checkCommandRe
// matches at its start and <name> is the [cliName] of a function that
// carries the pragma. A `dagger call nix <name>` command passes only
// when callCommandRe matches at its start and <name> is the cliName of
// a function of the toolchain. Each other command fails. A Taskfile in
// which daggerCommandRe finds no command gives the function nothing to
// verify, so the function returns an error for that Taskfile too.
func checkTaskfileCommands(taskfile string, goFiles map[string]string) error {
	functions, err := nixFunctions(goFiles)
	if err != nil {
		return err
	}

	text, err := taskfileText(taskfile)
	if err != nil {
		return err
	}

	text = commentLineRe.ReplaceAllString(text, "")

	commands := daggerCommandRe.FindAllStringIndex(text, -1)
	if len(commands) == 0 {
		return errors.New("did not find a `dagger check` or `dagger call` command")
	}

	var errs []error

	for _, command := range commands {
		rest := text[command[0]:]

		if match := checkCommandRe.FindStringSubmatch(rest); match != nil {
			name := match[1]

			isCheck, ok := functions[name]
			switch {
			case !ok:
				errs = append(
					errs,
					fmt.Errorf("`dagger check nix:%s` does not name a function of the nix toolchain", name),
				)

			case !isCheck:
				errs = append(
					errs,
					fmt.Errorf("`dagger check nix:%s` names a function without the +check pragma", name),
				)
			}

			continue
		}

		if match := callCommandRe.FindStringSubmatch(rest); match != nil {
			name := match[1]

			if _, ok := functions[name]; !ok {
				errs = append(
					errs,
					fmt.Errorf("`dagger call nix %s` does not name a function of the nix toolchain", name),
				)
			}

			continue
		}

		errs = append(errs, fmt.Errorf(
			"`%s` starts a command that is not a `dagger check nix:<name>` command that ends at its one pattern"+
				" or a `dagger call nix <name>` command",
			strings.TrimSpace(text[command[0]:command[1]]),
		))
	}

	return errors.Join(errs...)
}

// taskfileText decodes taskfile as YAML and returns the strings of it,
// one after another, with a newline between each two. The strings are
// the values of the scalars of taskfile, such as each command and each
// task name. YAML joins the lines of a plain or a folded scalar, so
// each string holds a command as Task runs it. The function skips each
// alias, because it reads the node of the alias at its anchor.
func taskfileText(taskfile string) (string, error) {
	var root yaml.Node

	err := yaml.Unmarshal([]byte(taskfile), &root)
	if err != nil {
		return "", fmt.Errorf("decoding the Taskfile: %w", err)
	}

	return strings.Join(scalarValues(&root), "\n"), nil
}

// scalarValues returns the value of each scalar of the YAML tree at
// node, in the order of the tree.
func scalarValues(node *yaml.Node) []string {
	if node.Kind == yaml.ScalarNode {
		return []string{node.Value}
	}

	var values []string

	for _, child := range node.Content {
		values = append(values, scalarValues(child)...)
	}

	return values
}

// checkTaskfileProbes runs [checkTaskfileCommands] on probe Taskfiles
// against the Go file taskfileProbeGo. It returns an error when
// checkTaskfileCommands fails the Taskfile of runnableProbeCmds. It
// also returns an error when checkTaskfileCommands passes the Taskfile
// of a probe of unrunnableProbes, or when the error for that Taskfile
// does not quote the command of the probe. A Taskfile in which
// daggerCommandRe finds no command also fails checkTaskfileCommands,
// but that error quotes no command. So the self-check fails when
// daggerCommandRe misses the command of a probe.
func checkTaskfileProbes() error {
	goFiles := map[string]string{taskfileProbeGoPath: taskfileProbeGo}

	taskfile := probeTaskfile(runnableProbeCmds)

	err := checkTaskfileCommands(taskfile, goFiles)
	if err != nil {
		return fmt.Errorf("checking the probe Taskfile %q: %w", taskfile, err)
	}

	for _, probe := range unrunnableProbes {
		taskfile = probeTaskfile(probe.cmds)

		err = checkTaskfileCommands(taskfile, goFiles)
		if err == nil {
			return fmt.Errorf("checkTaskfileCommands passed the probe Taskfile %q, want an error that quotes %q",
				taskfile, probe.command)
		}

		if !strings.Contains(err.Error(), "`"+probe.command+"`") {
			return fmt.Errorf("checking the probe Taskfile %q, want an error that quotes %q: %w",
				taskfile, probe.command, err)
		}
	}

	return nil
}

// probeTaskfile returns a Taskfile with one task, whose cmds list holds
// the entries in the YAML cmds.
func probeTaskfile(cmds string) string {
	const indent = "      "

	return "tasks:\n  probe:\n    cmds:\n" + indent + strings.ReplaceAll(cmds, "\n", "\n"+indent) + "\n"
}

// nixFunctions parses goFiles, which maps the path of each Go file to
// its contents. It returns the functions of the nix toolchain, which are
// the exported methods of the type that nixTypeName names. Each key is
// the [cliName] of a function, and its value reports whether the
// function carries the +check pragma.
func nixFunctions(goFiles map[string]string) (map[string]bool, error) {
	functions := map[string]bool{}
	fset := token.NewFileSet()

	for _, file := range slices.Sorted(maps.Keys(goFiles)) {
		parsed, err := parser.ParseFile(fset, file, goFiles[file], parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", file, err)
		}

		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() || receiverName(fn) != nixTypeName {
				continue
			}

			name, err := cliName(fn.Name.Name)
			if err != nil {
				return nil, fmt.Errorf("deriving the dagger CLI name of a function in %s: %w", file, err)
			}

			functions[name] = carriesCheck(fn.Doc)
		}
	}

	return functions, nil
}

// cliName returns the name that the dagger CLI lists for the function
// with the Go name name. Dagger converts the Go name to lower camel
// case and then to kebab case. For a Go name of ASCII letters, the two
// conversions put each letter in lower case, with a hyphen before each
// upper-case letter that follows a lower-case letter. So BuildHome
// becomes build-home, and VulnscanSARIF becomes vulnscan-sarif.
// Dagger splits a name with a digit or an underscore by other rules.
// So cliName returns an error for a name that holds a character other
// than an ASCII letter. cliName also misspells a few whole names that
// Dagger registers as acronyms, such as JSON.
func cliName(name string) (string, error) {
	var cli strings.Builder

	for i := range len(name) {
		c := name[i]

		switch {
		case 'a' <= c && c <= 'z':
			cli.WriteByte(c)
		case 'A' <= c && c <= 'Z':
			if i > 0 && 'a' <= name[i-1] && name[i-1] <= 'z' {
				cli.WriteByte('-')
			}

			cli.WriteByte(c - 'A' + 'a')

		default:
			return "", fmt.Errorf("%s holds a character other than an ASCII letter", name)
		}
	}

	return cli.String(), nil
}

// receiverName returns the name of the receiver type of the method fn,
// without the pointer. It returns "" when fn is not a method.
func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return ""
	}

	typ := fn.Recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}

	ident, ok := typ.(*ast.Ident)
	if !ok {
		return ""
	}

	return ident.Name
}
