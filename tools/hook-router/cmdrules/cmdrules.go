package cmdrules

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// gitFlagsTakingValue lists top-level git flags that consume the
// following argument as their value (e.g. `git -C dir clone url`). The
// rule matcher skips both the flag and its value when locating the
// positional args, but only when [Rule.Command] is exactly
// "git". Other commands match strictly from position 1.
var gitFlagsTakingValue = map[string]bool{
	"-C":             true,
	"-c":             true,
	"--exec-path":    true,
	"--git-dir":      true,
	"--work-tree":    true,
	"--namespace":    true,
	"--super-prefix": true,
	"--config-env":   true,
}

// Command-rule actions. The zero value of Action is the empty string,
// which resolves to deny, so rules that omit the field are deny rules.
const (
	actionDeny = "deny"
	actionAsk  = "ask"
)

// Rule matches a Bash command whose AST contains a call
// matching Command + Args and resolves it to a PreToolUse decision
// per Action: "deny" (or "") blocks the call, "ask" forces a
// permission prompt even when settings or sandbox auto-allow would
// otherwise let it run. When Except is non-empty, the call is
// exempted only if it has at least one further positional arg whose
// literal value appears in Except. A bare `command + args` invocation
// (no further positional args) fires regardless of Except by default,
// because a bare `git stash` is still a save form; ExceptBare inverts
// that for groups whose bare form is a read, such as `git remote`.
//
// When Command == "git", leading top-level git flags are skipped
// before matching Args, along with the value after each flag listed in
// [gitFlagsTakingValue]. The value is skipped whether it is a literal,
// quoted, or expanded word, so `git -C "$dir" clone` matches a clone
// rule. Other commands match strictly from position 1.
//
// OnlyRemoteURL and ExceptLocalPath serve a rule that sends a command
// to a tool for remote repositories. With them, the rule leaves alone
// a call that reads only the local file system. Both fields read each
// word of the call after Args.
//
// With OnlyRemoteURL, the rule matches only when one of those words is
// a remote URL. A remote URL is a word with a `:` before its first
// `/`, such as https://host/repo or git@host:repo. A file:// URL is
// not a remote URL. Set OnlyRemoteURL for a command that always names
// its repository, such as `git clone`. That command reads a repository
// that is not a remote URL as a local path.
//
// ExceptLocalPath exempts a call when one of those words is a local
// path. A local path is a word that starts with `/`, `./`, `../`, `~`,
// or file://, or the word `.` or `..`. Set ExceptLocalPath for a
// command that reads a plain word as the name of a remote, such as
// `git fetch`. A call of that command that names no repository reaches
// a default remote, so only a path shows that the call reads a local
// repository.
//
// A word that starts with `-` is an option, and it is neither a remote
// URL nor a local path. In a word that holds an expansion, only the
// text before the first expansion counts. "$url" is therefore not a
// remote URL, and "$dir/repo" is not a local path.
type Rule struct {
	Command         string   `json:"command"`
	Args            []string `json:"args,omitempty"`
	Except          []string `json:"except,omitempty"`
	ExceptBare      bool     `json:"exceptBare,omitempty"`
	OnlyRemoteURL   bool     `json:"onlyRemoteURL,omitempty"`
	ExceptLocalPath bool     `json:"exceptLocalPath,omitempty"`
	Action          string   `json:"action,omitempty"`
	Reason          string   `json:"reason"`
}

// Ask reports whether the rule resolves to an "ask" decision rather
// than a deny.
func (r Rule) Ask() bool {
	return r.Action == actionAsk
}

// Engine is the deny/ask rule engine for PreToolUse:Bash.
// Construct with [New] or [Parse]; both return
// a non-nil zero value for empty input so callers can invoke
// [*Engine.Check] without nil guards.
//
// Rules are evaluated in slice order and the first match wins, so
// the caller building the list is responsible for ordering: deny
// rules before ask rules preserves deny precedence, and within a
// command's ask rules, subcommand-scoped rules must precede a
// catch-all fallback for the same command.
type Engine struct {
	rules []Rule
}

// New builds a [*Engine] from the given rules. A nil
// or empty slice yields an empty engine that matches nothing. Rules
// are evaluated in slice order; the first match wins.
func New(rules []Rule) *Engine {
	return &Engine{rules: rules}
}

// Empty reports whether the engine has no rules. A nil receiver
// reports true, so callers can treat nil and zero-rule engines
// identically (mirrors [*Engine.Check]).
func (r *Engine) Empty() bool {
	if r == nil {
		return true
	}

	return len(r.rules) == 0
}

// Rules returns a copy of the engine's rules in evaluation order. A
// nil or empty engine returns nil.
func (r *Engine) Rules() []Rule {
	if r == nil || len(r.rules) == 0 {
		return nil
	}

	return append([]Rule(nil), r.rules...)
}

// Check walks prog once and returns the first matching rule along with
// its reason. Every rule is evaluated against each [*syntax.CallExpr]
// in declaration order; when two rules could match the same call, the
// first declared wins.
func (r *Engine) Check(prog *syntax.File) (Rule, string, bool) {
	if r == nil || len(r.rules) == 0 {
		return Rule{}, "", false
	}

	var (
		matched Rule
		found   bool
	)

	syntax.Walk(prog, func(node syntax.Node) bool {
		if found {
			return false
		}

		call, ok := node.(*syntax.CallExpr)
		if !ok {
			return true
		}

		for _, rule := range r.rules {
			if matchRule(call, rule) {
				matched = rule
				found = true

				return false
			}
		}

		return true
	})

	if !found {
		return Rule{}, "", false
	}

	return matched, matched.Reason, true
}

// matchRule reports whether call satisfies rule. See [Rule]
// for the matching semantics.
func matchRule(call *syntax.CallExpr, rule Rule) bool {
	if len(call.Args) < 1 {
		return false
	}

	parts0 := call.Args[0].Parts
	if len(parts0) != 1 {
		return false
	}

	lit0, ok := parts0[0].(*syntax.Lit)
	if !ok || lit0.Value != rule.Command {
		return false
	}

	// Phase 1 collects rule.Args (with git-specific flag skipping).
	// Phase 2 takes the very next call.Arg as the Except candidate
	// without flag skipping, so e.g. `git stash --keep-index pop`
	// denies because `--keep-index` is the candidate. Flag skipping
	// accepts a non-literal flag value, so `git -C "$dir" clone`
	// still reaches `clone`. A non-literal positional arg ends
	// collection; positionals collected so far still count.
	matched := 0
	skipNext := false
	idx := 0

	for ; idx < len(call.Args)-1; idx++ {
		arg := call.Args[idx+1]

		if matched == len(rule.Args) {
			break
		}

		if skipNext {
			skipNext = false
			continue
		}

		if head := leadingLit(arg); rule.Command == "git" && strings.HasPrefix(head, "-") {
			// `--flag=value` carries the value inline, so only
			// consume the next arg for the bare form.
			if len(arg.Parts) == 1 && gitFlagsTakingValue[head] {
				skipNext = true
			}

			continue
		}

		if len(arg.Parts) != 1 {
			break
		}

		lit, ok := arg.Parts[0].(*syntax.Lit)
		if !ok {
			break
		}

		if lit.Value != rule.Args[matched] {
			return false
		}

		matched++
	}

	if matched < len(rule.Args) {
		return false
	}

	// idx points at the slot after the last args literal
	// (idx == len(call.Args)-1 means we exhausted the call), so rest
	// holds every word after Args.
	rest := call.Args[idx+1:]

	if excepted(rest, rule) {
		return false
	}

	if !rule.OnlyRemoteURL && !rule.ExceptLocalPath {
		return true
	}

	var remoteURL, localPath bool

	for _, word := range rest {
		switch classify(word) {
		case wordRemoteURL:
			remoteURL = true
		case wordLocalPath:
			localPath = true
		case wordOther:
		}
	}

	if rule.OnlyRemoteURL && !remoteURL {
		return false
	}

	return !rule.ExceptLocalPath || !localPath
}

// excepted reports whether [Rule.Except] exempts a call from rule.
// rest holds the words of the call after [Rule.Args], and its first
// word is the Except candidate. A bare `command + args` call has no
// candidate. Except does not exempt that call, because a bare
// `git stash` is still a save form. [Rule.ExceptBare] exempts it for
// groups whose bare form is a read.
func excepted(rest []*syntax.Word, rule Rule) bool {
	if len(rule.Except) == 0 {
		return false
	}

	if len(rest) == 0 {
		return rule.ExceptBare
	}

	cand := rest[0]
	if len(cand.Parts) != 1 {
		return false
	}

	candLit, ok := cand.Parts[0].(*syntax.Lit)
	if !ok {
		return false
	}

	return slices.Contains(rule.Except, candLit.Value)
}

// wordKind tells how [classify] reads a word of a call.
type wordKind int

const (
	// wordOther is a word that is neither a remote URL nor a local
	// path, such as an option, a plain name, or a word that starts
	// with an expansion.
	wordOther wordKind = iota
	// wordRemoteURL is a word that names a repository on another
	// host.
	wordRemoteURL
	// wordLocalPath is a word that names a path on the local file
	// system.
	wordLocalPath
)

// classify tells whether word is a remote URL, a local path, or
// neither, for [Rule.OnlyRemoteURL] and [Rule.ExceptLocalPath]. It
// reads the text of [literalStart], and it applies the first of these
// rules that fits:
//
//   - A word that starts with `-` is an option, and it is neither.
//   - A word that starts with file:// is a local path.
//   - A word with a `:` before its first `/` is a remote URL. git reads
//     such a word as a URL with a scheme, such as https://host/repo, or
//     as the SCP-style host:repo.
//   - A word that starts with `/`, `./`, `../`, or `~` is a local
//     path, and so are the words `.` and `..`.
//
// Every other word is neither. A plain name such as origin or repo.git
// is one of them. git reads such a name as a path in one command and
// as the name of a remote in another. A word that starts with an
// expansion, such as "$url" or "$dir/repo", is neither as well,
// because classify cannot read the value of the expansion.
//
// classify knows no option of a command. It therefore reads an option
// value that stands in a word of its own like every other word.
func classify(word *syntax.Word) wordKind {
	text, whole := literalStart(word)

	switch {
	case strings.HasPrefix(text, "-"):
		return wordOther
	case strings.HasPrefix(text, "file://"):
		return wordLocalPath
	}

	colon := strings.IndexByte(text, ':')
	slash := strings.IndexByte(text, '/')

	if colon >= 0 && (slash < 0 || colon < slash) {
		return wordRemoteURL
	}

	for _, prefix := range []string{"/", "./", "../", "~"} {
		if strings.HasPrefix(text, prefix) {
			return wordLocalPath
		}
	}

	if whole && (text == "." || text == "..") {
		return wordLocalPath
	}

	return wordOther
}

// literalStart returns the text of word before its first expansion,
// and it reports whether that text is the whole word. It reads through
// quotes. For "https://host/$repo", it returns https://host/ and
// false. An expansion is every part of a word that is not literal
// text, such as a parameter expansion or a command substitution.
func literalStart(word *syntax.Word) (string, bool) {
	var b strings.Builder

	whole := writeLiteral(&b, word.Parts)

	return b.String(), whole
}

// writeLiteral writes to b the literal text of parts before the first
// expansion, and it reports whether parts holds no expansion.
func writeLiteral(b *strings.Builder, parts []syntax.WordPart) bool {
	for _, part := range parts {
		switch p := part.(type) {
		case *syntax.Lit:
			b.WriteString(p.Value)
		case *syntax.SglQuoted:
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			if !writeLiteral(b, p.Parts) {
				return false
			}

		default:
			return false
		}
	}

	return true
}

// leadingLit returns the literal text that opens word, or "" when word
// starts with a quote or expansion. `--git-dir="$x"` yields "--git-dir=".
func leadingLit(word *syntax.Word) string {
	if len(word.Parts) == 0 {
		return ""
	}

	lit, ok := word.Parts[0].(*syntax.Lit)
	if !ok {
		return ""
	}

	return lit.Value
}

// Parse decodes the JSON payload passed via --command-rules
// into a [*Engine]. Empty input yields an empty engine; malformed
// JSON or an unknown rule action returns an error so wrapper
// misconfiguration is loud. Unknown fields are silently dropped
// (lenient json.Unmarshal), matching the other hook-router JSON flag
// parsers.
func Parse(s string) (*Engine, error) {
	if s == "" {
		return New(nil), nil
	}

	var rules []Rule

	err := json.Unmarshal([]byte(s), &rules)
	if err != nil {
		return nil, fmt.Errorf("decoding command rules JSON: %w", err)
	}

	for i, rule := range rules {
		switch rule.Action {
		case "", actionDeny, actionAsk:
		default:
			return nil, fmt.Errorf("command rule %d (%s): unknown action %q", i, rule.Command, rule.Action)
		}
	}

	return New(rules), nil
}
