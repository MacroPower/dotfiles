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

	// wordGap matches the text between two words of a dagger command
	// before its subcommand, as daggerCommandRe reads it. The text holds
	// spaces, tabs, newlines, and backslashes. It can also hold a
	// comment that ends in a backslash, with the newline after the
	// backslash. The shell of Task goes on with the command on the next
	// line after such a comment.
	wordGap = `(?:[\s\\]|#[^\n]*\\\n)+`

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
	// The first literal block holds two shell comments that name Plain,
	// which lacks the pragma, so a checker that reads comment lines
	// fails the probe. The first comment ends in a backslash, and the
	// line after it is still a comment. The command of the block passes
	// a flag to the function after its name, which a `dagger call`
	// command allows.
	//
	// In the other literal blocks, a backslash at the end of a line
	// splits a command. Two of those blocks split a command between
	// each two of its words, and the last block splits the name
	// twice-checked. The shell joins the lines and drops the backslash
	// and the newline. So a checker that does not join the lines, or
	// that joins them with a space, fails the probe.
	//
	// Another block ends the name plain with a backslash and indents
	// the next line. It ends with a comment line that names Plain. The
	// shell reads that indent as a space between plain and the flag,
	// and it reads the last line as a comment. So a checker that drops
	// the indent of a joined line fails the probe. A checker that reads
	// a comment line after the end of a split command fails it too.
	runnableProbeCmds = "- dagger check nix:checked\n" +
		"- cmd: dagger check nix:twice-checked\n" +
		"- dagger call nix plain\n" +
		"- dagger check nix:checked&& dagger call nix plain;\n" +
		"- dagger check nix:checked; dagger call nix plain| tee log\n" +
		"- dagger call nix plain& dagger check nix:twice-checked| tee log\n" +
		"- echo \"$(dagger check nix:checked)\" \"$(dagger call nix plain)\"\n" +
		"- dagger check nix:checked &dagger check nix:twice-checked\n" +
		"- |\n" +
		"  # dagger check nix:plain \\\n" +
		"  # dagger check nix:plain\n" +
		"  dagger call nix twice-checked --flag\n" +
		"- |\n" +
		"  dagger \\\n" +
		"    check \\\n" +
		"    nix:checked \\\n" +
		"    && echo ok\n" +
		"- |\n" +
		"  dagger \\\n" +
		"    call \\\n" +
		"    nix \\\n" +
		"    plain \\\n" +
		"    --flag\n" +
		"- |\n" +
		"  dagger call nix plain\\\n" +
		"    --flag\n" +
		"  # dagger check nix:plain\n" +
		"- |\n" +
		"  dagger call nix twice-\\\n" +
		"  checked"
)

// taskfileProbe is a probe of [checkTaskfileProbes] that the nix
// toolchain cannot run as written.
type taskfileProbe struct {
	// cmds holds the YAML of an entry of the cmds list of a task.
	cmds string

	// command holds a command of cmds as [checkTaskfileCommands] reads
	// it. The error of checkTaskfileCommands for cmds must quote the
	// command.
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
		// line. So each of these literal blocks runs one command.
		{"- |\n  dagger \\\n    check nix:plain", "dagger check nix:plain"},
		{"- |\n  dagger check nix:checked \\\n  nix:plain", "dagger check nix:checked nix:plain"},
		{"- |\n  dagger call nix \\\n    missing", "dagger call nix missing"},
		// The shell also joins the two parts of a word that a backslash
		// splits. So the first block runs dagger, and the second block
		// calls plainx.
		{"- |\n  dag\\\n  ger check nix:plain", "dagger check nix:plain"},
		{"- |\n  dagger call nix plain\\\n  x", "dagger call nix plainx"},
		// A comment line ends at its newline, even after a backslash.
		// So the shell runs the line after the comment.
		{"- |\n  # note \\\n  dagger check nix:plain", "dagger check nix:plain"},
		// After a line that ends in a backslash, a line that starts
		// with # can hold a command. The # can end a word or sit in a
		// quote that an earlier line opens. In each of these blocks, the
		// shell reads #y as the end of a word and runs the command after
		// the semicolon. In the second block, the first two backslashes
		// are one literal backslash. In the third block, the line before
		// #y holds a # in a quoted string. That # does not start a
		// comment, so both shells join the two lines.
		{
			"- |\n  dagger call nix plain --arg=x\\\n  #y; dagger check nix:plain",
			"dagger check nix:plain",
		},
		{
			"- |\n  dagger call nix plain --arg \\\\\\\n  #y; dagger check nix:plain",
			"dagger check nix:plain",
		},
		{
			"- |\n  dagger call nix plain --arg=\"#\"x\\\n  #y; dagger check nix:plain",
			"dagger check nix:plain",
		},
		// The # of this block sits in a double-quoted string that the
		// line before opens, and a space stands before the #. So the
		// shell reads the # as text and runs the command after the
		// semicolon.
		{
			"- |\n  echo \"a \\\n    #\"; dagger check nix:plain",
			"dagger check nix:plain",
		},
		// After a comment that ends in a backslash, the shell of Task
		// goes on with the command on the next line. So Task passes
		// nix:plain to this command as a second pattern.
		{
			"- |\n  dagger check nix:checked \\\n  # note \\\n  nix:plain",
			"dagger check nix:checked # note \\",
		},
		// bash ends a command at the newline of a comment, even after
		// a backslash. So bash runs the second line of this block as
		// a command.
		{
			"- |\n  dagger call nix plain # note \\\n  dagger check nix:plain",
			"dagger check nix:plain",
		},
		// shellText does not join a line that holds a #, and both
		// shells join the lines of each of these blocks. daggerCommandRe
		// still finds each command, which neither checkCommandRe nor
		// callCommandRe reads. The backslash stands before the
		// subcommand, before a flag, and before the value of a flag.
		{
			"- |\n  echo \"a # b\"; dagger \\\n    check nix:plain",
			"dagger \\\n  check nix:plain",
		},
		{
			"- |\n  echo \"a # b\"; dagger \\\n    --progress plain call nix missing",
			"dagger \\\n  --progress plain call nix missing",
		},
		{
			"- |\n  echo \"a # b\"; dagger --progress \\\n    plain call nix missing",
			"dagger --progress \\\n  plain call nix missing",
		},
		// After a comment that ends in a backslash, the shell of Task
		// goes on with the command on the next line. So Task runs each
		// of these blocks as one dagger command, with the comment
		// between two words before the subcommand. daggerCommandRe
		// reads the comment as a gap between the words, and neither
		// checkCommandRe nor callCommandRe reads a comment.
		{"- |\n  dagger \\\n    # x \\\n    check nix:plain", "dagger   # x \\\n  check nix:plain"},
		{"- |\n  dagger # x \\\n  check nix:plain", "dagger # x \\\ncheck nix:plain"},
		{
			"- |\n  dagger --progress plain \\\n    # x \\\n    call nix missing",
			"dagger --progress plain   # x \\\n  call nix missing",
		},
		{
			"- |\n  dagger \\\n    # x \\\n    --progress plain check nix:plain",
			"dagger   # x \\\n  --progress plain check nix:plain",
		},
		{
			"- |\n  dagger --progress \\\n    # x \\\n    plain check nix:plain",
			"dagger --progress   # x \\\n  plain check nix:plain",
		},
		// The shell reads two backslashes as one literal backslash, so
		// it runs the second line of this block as a command.
		{
			"- |\n  dagger call nix plain --arg \\\\\n  dagger check nix:plain",
			"dagger check nix:plain",
		},
		// In each of these blocks, the literal backslash is a word of
		// the dagger command, such as the name of a function or a
		// second pattern. The shell runs the second line as a command
		// of its own. Neither checkCommandRe nor callCommandRe reads a
		// backslash between words.
		{"- |\n  dagger \\\\\n  call nix plain", "dagger \\\\\ncall nix plain"},
		{"- |\n  dagger call \\\\\n  nix plain", "dagger call \\\\"},
		{"- |\n  dagger call nix \\\\\n  plain", "dagger call nix \\\\"},
		{"- |\n  dagger check \\\\\n  nix:checked", "dagger check \\\\"},
		{"- |\n  dagger check nix:checked \\\\\n  nix:checked", "dagger check nix:checked \\\\"},
		// A backslash at the end of an entry does not join the next
		// entry to it. With no newline after it, the backslash is a
		// literal character, so the last command names no check.
		{"- dagger call nix plain \\\n- dagger check nix:plain", "dagger check nix:plain"},
		{"- dagger check nix:checked\\", "dagger check nix:checked\\"},
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

	// commentLineRe matches a line whose first character other than a
	// space or a tab is #. The shell reads such a line of a literal
	// block scalar as a comment when the line before it does not end
	// in a backslash. The YAML decoder drops the comments of the YAML
	// itself.
	commentLineRe = regexp.MustCompile(`^[ \t]*#`)

	// daggerCommandRe matches each `dagger check` and `dagger call`
	// command, with any flags of dagger before the subcommand. It also
	// matches `dagger checks`, which Dagger runs as an alias of
	// `dagger check`. [shellText] keeps a backslash and its newline
	// where it cannot tell whether the shell joins the two lines. So
	// between the words before the subcommand, a match reads a backslash
	// and a newline as a space. A command that such a backslash splits
	// there then still gets a match. Between those words, a match also
	// reads a comment that ends in a backslash, as wordGap describes. A
	// match ends at the end of its line or at a character of
	// commandEnd. So each command of a line gets a match of its own.
	// [checkTaskfileCommands] fails each match at whose start neither
	// checkCommandRe nor callCommandRe matches. Neither of them reads
	// the alias, or a backslash or a comment between two words that it
	// matches.
	daggerCommandRe = regexp.MustCompile(
		`\bdagger(?:` + wordGap + `-\S+(?:` + wordGap + `[^\s-]\S*)?)*` + wordGap +
			`(?:checks?|call)\b[^\n` + commandEnd + `]*`,
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
// && or a pipe. The test reads the Taskfile and the Go files of the
// root module of the nix toolchain from the default source of [Nix].
//
// The shell joins a line that ends in a backslash to the next line, so
// a backslash can split a command at any place. The test joins those
// lines with [shellText] before it reads a command. So a split command
// passes or fails as the command that the shell runs.
//
// The test skips each line that starts with # after any spaces and
// tabs, because the shell reads such a line as a comment. It reads
// such a line as part of a command when the line before it ends in a
// backslash. The # of that line can then end a word or sit in a quote
// that an earlier line opens. The test cannot tell either case from a
// comment. So the test can fail a wrong dagger command in a comment
// that starts such a line, though the shell does not run that command.
//
// The test reads no command in such a comment when each of these
// conditions holds:
//
//   - The test joins the comment to the line before it. It does not
//     join the two lines when the line before holds a # or ends in two
//     backslashes.
//   - A dagger command comes before the comment on the joined line.
//   - No shell operator, quote, or backtick stands between that dagger
//     command and the dagger command in the comment.
//
// The test then reads the earlier command up to the end of the line,
// comment included.
//
// The test does not join a line that holds a # to the next line,
// because it cannot tell whether a comment ends that line. It reads
// the next line as the start of a command, and the backslash stays in
// the command of the line with the #. The test fails a `dagger check`
// command with such a backslash. It also fails a `dagger call` command
// when such a backslash comes before the space or the shell operator
// that follows the name of the function.
//
// Before it reads the Taskfile, the test runs [checkTaskfileProbes] on
// fixed probe commands. That self-check fails when the test passes a
// command that names a function that the command does not run, or a
// command of another form. A check of a right Taskfile returns no
// error, so it cannot show that fault. The probes include commands that
// YAML joins onto one line and commands that share a line with another
// command. They also include commands that go on after a quote, a
// backtick, or &>, and commands that a backslash splits. The self-check
// also fails when the test fails a right command or misreads a name of
// two words.
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

	goFiles, err := rootGoFiles(ctx)
	if err != nil {
		return err
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
// contents. The function reads the text that [taskfileText] returns for
// taskfile, which holds each string of taskfile as the shell reads its
// lines. It finds each command with daggerCommandRe. Then it matches
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
//
// The function passes each string through [shellText] before it joins
// the strings. Task runs each command of the Taskfile as a script of
// its own. So a backslash at the end of one string does not join the
// next string to it.
func taskfileText(taskfile string) (string, error) {
	var root yaml.Node

	err := yaml.Unmarshal([]byte(taskfile), &root)
	if err != nil {
		return "", fmt.Errorf("decoding the Taskfile: %w", err)
	}

	scripts := scalarValues(&root)
	for i, script := range scripts {
		scripts[i] = shellText(script)
	}

	return strings.Join(scripts, "\n"), nil
}

// shellText returns script, a string of the Taskfile, as the shell
// reads its lines. The shell joins a line that ends in a backslash to
// the next line, and it drops the backslash and the newline. So
// shellText drops them too, at any place in a command, which includes
// the middle of a word. It keeps a backslash that no newline follows,
// which the shell reads as a literal character at the end of script.
//
// The shell reads a line that commentLineRe matches as a comment, and
// the comment ends at its newline, even after a backslash. So shellText
// drops each such line. It keeps the line when the line before it ends
// in a backslash. The # of that line can then end a word, as the shell
// reads x\ on one line and #y on the next line as the word x#y. The #
// can also sit in a quoted string that an earlier line opens.
// shellText cannot tell either case from a comment, so it reads the
// line as command text. [checkTaskfileCommands] can then fail a wrong
// dagger command in a comment on such a line, though the shell does not
// run that command.
//
// shellText keeps a backslash and its newline in each of these cases.
//
//   - A second backslash stands right before the backslash. The shell
//     reads two backslashes as one literal backslash, so the line ends
//     at its newline. shellText does not count the backslashes. So it
//     also keeps the two lines apart after three backslashes, where
//     the shell goes on with the command.
//   - The line holds a #. A # can start a comment that ends in the
//     backslash. bash ends the command at the newline of such a
//     comment. The shell of Task, mvdan.cc/sh, goes on with the
//     command on the next line. When the # is part of a word, both
//     shells join the two lines. shellText cannot tell which case
//     holds.
//
// [checkTaskfileCommands] reads the line after a kept backslash as the
// start of a command, and the backslash stays in the command before it.
// That reading hides a command from daggerCommandRe when the kept
// backslash of a line that holds a # splits the word dagger or the
// subcommand, as in dag\ and ger on two lines.
func shellText(script string) string {
	var text strings.Builder

	// continued reports whether the line before ends in a backslash.
	continued := false

	for line := range strings.SplitAfterSeq(script, "\n") {
		if !continued && commentLineRe.MatchString(line) {
			continue
		}

		continued = strings.HasSuffix(line, "\\\n")
		if continued && !strings.HasSuffix(line, "\\\\\n") && !strings.Contains(line, "#") {
			line = strings.TrimSuffix(line, "\\\n")
		}

		text.WriteString(line)
	}

	return text.String()
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
