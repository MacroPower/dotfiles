package rmguard

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"mvdan.cc/sh/v3/syntax"
)

// ReasonPrefix opens the deny reason [Check] returns for a target that
// starts with a possibly-empty variable.
const ReasonPrefix = "Variable-path rm is denied:"

// UnresolvablePrefix opens the deny reason [Check] returns for a target
// Claude Code cannot resolve before the command runs.
const UnresolvablePrefix = "Unresolvable rm target is denied:"

// srcMaxBytes bounds how much of the offending call or operand is
// quoted back in the deny reason.
const srcMaxBytes = 60

// commands names the executables the guard inspects.
var commands = map[string]bool{
	"rm":    true,
	"rmdir": true,
}

// rootChild matches the text after the leading `$VAR/` when it names a
// top-level directory, optionally followed by glob segments. The
// directory list comes from the Claude Code binary.
var rootChild = regexp.MustCompile(`^(?:bin|boot|dev|etc|home|lib|lib32|lib64|libx32|media|mnt|opt|proc|root|run|sbin|srv|sys|tmp|usr|var|snap|nix|lost\+found|private|cores|Applications|Library|System|Users|Volumes|Windows|ProgramData|cygdrive)(?:/+\*+)*/*$`)

// plainName matches a parameter that is an ordinary variable name.
var plainName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// guardable matches the parameters the guard treats as possibly empty:
// variable names, positional parameters, and `@`, `*`, `!`.
var guardable = regexp.MustCompile(`^(?:[A-Za-z_][A-Za-z0-9_]*|[0-9]+|[@*!])$`)

// changeDir names the commands that move the shell to another
// directory, which makes a later relative target unresolvable.
var changeDir = map[string]bool{
	"cd":    true,
	"pushd": true,
	"popd":  true,
}

// globTail matches the trailing `/*` segments of a target shape and
// captures them.
var globTail = regexp.MustCompile(`((?:/\*+)+)/*$`)

// parentsFlag matches the rmdir flag that also removes parent
// directories.
var parentsFlag = regexp.MustCompile(`^(?:--p|-[a-z]*p)`)

// kind names the rule an operand trips.
type kind int

const (
	// kindNone marks an operand no rule flags.
	kindNone kind = iota
	// kindVariable is a target that starts with a possibly-empty
	// variable.
	kindVariable
	// kindSubstitution is a target made only of command substitutions.
	kindSubstitution
	// kindChangeDir is a relative glob target after a directory change.
	kindChangeDir
	// kindBase is a glob target whose directory is not a literal path.
	kindBase
	// kindDepth is a glob target that spans more than one directory
	// level.
	kindDepth
)

// piece is one flattened part of an operand word: literal text, a
// parameter expansion, a command substitution, or any other dynamic
// part when all three are unset.
type piece struct {
	param *syntax.ParamExp
	text  string
	lit   bool
	subst bool
}

// Check walks prog and returns a deny reason for the first `rm` or
// `rmdir` operand that Claude Code would force a permission prompt on.
// The package documentation lists the shapes. command is the original
// command text, used to quote the call and build the suggested
// rewrite.
func Check(prog *syntax.File, command string) (string, bool) {
	var deny string

	// Offset of the first directory change, or past the end when the
	// command has none.
	firstChange := uint(len(command)) + 1

	syntax.Walk(prog, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if ok && named(call, changeDir) && call.Pos().Offset() < firstChange {
			firstChange = call.Pos().Offset()
		}

		return true
	})

	syntax.Walk(prog, func(node syntax.Node) bool {
		// Returning false only prunes the current subtree, so this
		// guard keeps the first offending call's reason.
		if deny != "" {
			return false
		}

		call, ok := node.(*syntax.CallExpr)
		if !ok || !named(call, commands) {
			return true
		}

		if operand, k := offender(call, firstChange < call.Pos().Offset()); k != kindNone {
			deny = reason(command, call, operand, k)

			return false
		}

		return true
	})

	return deny, deny != ""
}

// named reports whether call's command word statically resolves to
// one of names, either bare or by path (/bin/rm).
func named(call *syntax.CallExpr, names map[string]bool) bool {
	if len(call.Args) == 0 {
		return false
	}

	name, ok := literalWord(call.Args[0])
	if !ok {
		return false
	}

	return names[path.Base(name)]
}

// offender returns the first operand of call that a rule flags, with
// the rule's [kind]. Before a literal `--`, a word whose first part is
// a literal starting with `-` is a flag; every other word is an
// operand. changed says whether the command changes directory before
// call.
func offender(call *syntax.CallExpr, changed bool) (*syntax.Word, kind) {
	var operands []*syntax.Word

	operandsOnly, parents := false, false

	for _, arg := range call.Args[1:] {
		if !operandsOnly {
			if lit, ok := literalWord(arg); ok && lit == "--" {
				operandsOnly = true

				continue
			}

			if isFlag(arg) {
				if lit, ok := literalWord(arg); ok && parentsFlag.MatchString(lit) {
					parents = true
				}

				continue
			}
		}

		operands = append(operands, arg)
	}

	name, _ := literalWord(call.Args[0])
	parents = parents && path.Base(name) == "rmdir"

	for _, operand := range operands {
		if k := classify(operand, changed, parents); k != kindNone {
			return operand, k
		}
	}

	return nil, kindNone
}

// classify returns the rule word trips, or [kindNone]. changed says
// whether the command changes directory before the removal, and
// parents whether the call is an rmdir that also removes parent
// directories.
func classify(word *syntax.Word, changed, parents bool) kind {
	if dangerous(word) {
		return kindVariable
	}

	if wholeSubstitution(word) {
		return kindSubstitution
	}

	s := shape(word)
	relative := !strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "~") && !strings.HasPrefix(s, "$")

	full := s
	if relative {
		full = "./" + s
	}

	tail := globTail.FindStringSubmatchIndex(full)
	if tail == nil {
		return kindNone
	}

	base := full[:tail[0]]

	switch {
	case changed && relative:
		return kindChangeDir
	case strings.HasPrefix(s, "~"), strings.Contains(s, "$"), parents:
		return kindBase
	case relative && (hasSegment(s, "..") || strings.HasSuffix(s, "*/")):
		return kindBase
	}

	levels := strings.Count(full[tail[2]:tail[3]], "/")

	for seg := range strings.SplitSeq(base, "/") {
		if strings.ContainsAny(seg, "*?[") {
			levels++
		}
	}

	if levels > 1 {
		return kindDepth
	}

	return kindNone
}

// hasSegment reports whether one of the slash-separated segments of s
// equals want.
func hasSegment(s, want string) bool {
	for seg := range strings.SplitSeq(s, "/") {
		if seg == want {
			return true
		}
	}

	return false
}

// wholeSubstitution reports whether word is made only of command
// substitutions, quoted or not.
func wholeSubstitution(word *syntax.Word) bool {
	pieces := flatten(word)
	if len(pieces) == 0 {
		return false
	}

	for _, p := range pieces {
		if !p.subst {
			return false
		}
	}

	return true
}

// shape renders word as the path pattern the shell sees. Unquoted
// text stays as written, `$` stands for each expansion, and `_`
// replaces a quoted or escaped character, which is never a glob or a
// tilde.
func shape(word *syntax.Word) string {
	var b strings.Builder

	quoted := func(s string) {
		for _, r := range s {
			if strings.ContainsRune("*?[~$", r) {
				r = '_'
			}

			b.WriteRune(r)
		}
	}

	for _, part := range word.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			for i := 0; i < len(p.Value); i++ {
				if p.Value[i] == '\\' && i+1 < len(p.Value) {
					b.WriteByte('_')

					i++

					continue
				}

				b.WriteByte(p.Value[i])
			}
		case *syntax.SglQuoted:
			quoted(p.Value)
		case *syntax.DblQuoted:
			for _, dp := range p.Parts {
				if lit, ok := dp.(*syntax.Lit); ok {
					quoted(lit.Value)
				} else {
					b.WriteByte('$')
				}
			}
		case *syntax.ExtGlob:
			b.WriteByte('*')
		default:
			b.WriteByte('$')
		}
	}

	return b.String()
}

// isFlag reports whether word's first part is a literal starting with
// `-`.
func isFlag(word *syntax.Word) bool {
	if len(word.Parts) == 0 {
		return false
	}

	lit, ok := word.Parts[0].(*syntax.Lit)

	return ok && strings.HasPrefix(lit.Value, "-")
}

// dangerous reports whether word starts with a possibly-empty
// parameter expansion followed by a slash, with nothing after the
// slash that keeps the path below a top-level directory.
func dangerous(word *syntax.Word) bool {
	pieces := flatten(word)
	if len(pieces) < 2 || pieces[0].param == nil || !bare(pieces[0].param) || !pieces[1].lit {
		return false
	}

	// The parser keeps the backslash of an unquoted `\/` in the
	// literal.
	rest, ok := strings.CutPrefix(strings.TrimPrefix(pieces[1].text, `\`), "/")
	if !ok {
		return false
	}

	// `$X/` at the end of the word, or `$X/` followed by another
	// expansion. flatten merges adjacent literals, so a third piece
	// is never text.
	if rest == "" {
		return true
	}

	if strings.ContainsRune("*?[{/", rune(rest[0])) {
		return true
	}

	return len(pieces) == 2 && rootChild.MatchString(rest)
}

// flatten lists word's parts in order, descending into double quotes
// and merging adjacent literal text. It drops empty literals, so an
// empty quote pair yields no piece.
func flatten(word *syntax.Word) []piece {
	var pieces []piece

	text := func(s string) {
		if s == "" {
			return
		}

		if n := len(pieces); n > 0 && pieces[n-1].lit {
			pieces[n-1].text += s

			return
		}

		pieces = append(pieces, piece{text: s, lit: true})
	}

	var add func(parts []syntax.WordPart)

	add = func(parts []syntax.WordPart) {
		for _, part := range parts {
			switch p := part.(type) {
			case *syntax.Lit:
				text(p.Value)
			case *syntax.SglQuoted:
				text(p.Value)
			case *syntax.DblQuoted:
				add(p.Parts)
			case *syntax.ParamExp:
				pieces = append(pieces, piece{param: p})
			case *syntax.CmdSubst:
				pieces = append(pieces, piece{subst: true})
			default:
				pieces = append(pieces, piece{})
			}
		}
	}

	add(word.Parts)

	return pieces
}

// bare reports whether pe expands to nothing when its parameter is
// unset or empty: a plain `$X` or `${X}`, or a default form whose
// default is empty or another simple variable. An error form (`:?`)
// and every other operator are not bare.
func bare(pe *syntax.ParamExp) bool {
	if !simple(pe) {
		return false
	}

	if pe.Exp == nil {
		return true
	}

	if pe.Exp.Op != syntax.DefaultUnset && pe.Exp.Op != syntax.DefaultUnsetOrNull {
		return false
	}

	if pe.Exp.Word == nil {
		return true
	}

	def := flatten(pe.Exp.Word)

	switch len(def) {
	case 0:
		return true
	case 1:
		return def[0].param != nil && def[0].param.Exp == nil && simple(def[0].param)
	default:
		return false
	}
}

// simple reports whether pe names a [guardable] parameter and uses no
// operator other than, possibly, an [syntax.Expansion].
func simple(pe *syntax.ParamExp) bool {
	if pe.Excl || pe.Length || pe.Width || pe.Index != nil || pe.Slice != nil || pe.Repl != nil || pe.Names != 0 {
		return false
	}

	return pe.Param != nil && guardable.MatchString(pe.Param.Value)
}

// rewrite returns operand's source text with every bare expansion
// changed to `${NAME:?}` and every other byte kept. It reports false
// when a bare expansion is not a plain `$NAME` or `${NAME}`, where no
// mechanical rewrite applies.
func rewrite(command string, operand *syntax.Word) (string, bool) {
	var b strings.Builder

	pos := operand.Pos().Offset()

	for _, p := range flatten(operand) {
		pe := p.param
		if pe == nil || !bare(pe) {
			continue
		}

		if pe.Exp != nil || !plainName.MatchString(pe.Param.Value) {
			return "", false
		}

		b.WriteString(command[pos:pe.Pos().Offset()])
		b.WriteString("${" + pe.Param.Value + ":?}")

		pos = pe.End().Offset()
	}

	b.WriteString(command[pos:operand.End().Offset()])

	return b.String(), true
}

// literalWord resolves word to its literal value when every part is
// statically known: plain literals, single quotes, and double quotes
// composed only of literal parts.
func literalWord(word *syntax.Word) (string, bool) {
	var b strings.Builder

	for _, p := range flatten(word) {
		if !p.lit {
			return "", false
		}

		b.WriteString(p.text)
	}

	return b.String(), true
}

// nodeSrc returns the exact source text of node from command,
// truncated to [srcMaxBytes] on a rune boundary so the deny reason
// stays readable.
func nodeSrc(command string, node syntax.Node) string {
	src := command[node.Pos().Offset():node.End().Offset()]
	if len(src) <= srcMaxBytes {
		return src
	}

	cut := srcMaxBytes
	for cut > 0 && !utf8.RuneStart(src[cut]) {
		cut--
	}

	return src[:cut] + "..."
}

// reason builds the deny message: a first line naming the offending
// call and operand, then why Claude Code prompts on the shape and how
// to rewrite it.
func reason(command string, call *syntax.CallExpr, operand *syntax.Word, k kind) string {
	const prompt = "Claude Code forces a permission prompt on this shape, and no permission mode or allow rule skips it."

	callSrc, target := nodeSrc(command, call), nodeSrc(command, operand)

	if k == kindVariable {
		fix := "add `:?` to the leading variable so the command fails when it is empty, e.g. `\"${VAR:?}/...\"`"
		if fixed, ok := rewrite(command, operand); ok {
			fix = fmt.Sprintf("write the target as `%s`, which fails instead of expanding when a variable is empty", fixed)
		}

		return fmt.Sprintf("%s `%s` targets `%s`, which starts with a variable expansion and resolves to `/` or a top-level directory when that variable is empty.", ReasonPrefix, callSrc, target) + "\n\n" +
			prompt + " Rerun the command with a target that cannot expand to the root:\n" +
			"\n" +
			"- Either " + fix + ".\n" +
			"- Or use a literal path."
	}

	var why, fix string

	switch k {
	case kindSubstitution:
		why = "is the output of a command substitution, so nothing can check it before the command runs"
		fix = "Run the substitution on its own first, then remove the literal paths it prints."
	case kindChangeDir:
		why = "is a relative glob after a directory change, so the directory it expands in is not known before the command runs"
		fix = "Drop the directory change and give the glob an absolute directory, e.g. `rm -rf /abs/dir/*`."
	case kindDepth:
		why = "is a glob that spans more than one directory level, so the directories it reaches are not known before the command runs"
		fix = "List the matching paths first and remove them by literal path, or use one target per directory with a single trailing `/*`."
	default:
		why = "is a glob in a directory that is not a literal path (a variable, a substitution, `~`, `..`, a glob, or rmdir with `-p`)"
		fix = "Write the directory as a literal absolute path. To empty a directory, remove the directory itself and recreate it."
	}

	return fmt.Sprintf("%s `%s` targets `%s`, which %s.", UnresolvablePrefix, callSrc, target, why) + "\n\n" +
		prompt + " " + fix
}
