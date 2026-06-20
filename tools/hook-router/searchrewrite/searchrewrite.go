package searchrewrite

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Config declares the search-rewrite knobs. JSON tags are camelCase
// because [builtins.toJSON] in home/claude.nix emits attribute names
// verbatim and the Go struct tags must match.
//
// Grep and Find gate the grep->rg and find->bfs rewrites independently.
// FindExcludes is the single source of truth for the directories pruned
// from both rewrites: each entry becomes a bfs `-exclude` clause and an
// rg `-g` exclude glob. An entry containing a slash is treated as a path
// pattern (bfs `-path '*<entry>'`, rg `-g '!<entry>'` plus a `-g
// '!**/<entry>'` variant since rg anchors globs containing a slash);
// otherwise it is a basename (bfs `-name <entry>`, rg `-g '!<entry>'`).
type Config struct {
	Grep         bool     `json:"grep"`
	Find         bool     `json:"find"`
	FindExcludes []string `json:"findExcludes"`
}

// readOnlyCommands is the allowlist of command words that keep a
// statement read-only: the search tools (both the original `grep`/`find`
// and their `rg`/`bfs` rewrites) plus common filters and pagers. A
// pipeline is read-only only when every stage's command word is in this
// set.
var readOnlyCommands = map[string]bool{
	"grep":   true,
	"rg":     true,
	"find":   true,
	"bfs":    true,
	"head":   true,
	"tail":   true,
	"wc":     true,
	"sort":   true,
	"uniq":   true,
	"cut":    true,
	"cat":    true,
	"bat":    true,
	"less":   true,
	"column": true,
}

// findActionWords are find/bfs primaries that run a command or mutate the
// filesystem. Their presence disqualifies a find/bfs stage from being
// read-only, so a `find ... -delete` or `-exec` is never auto-approved.
var findActionWords = map[string]bool{
	"-exec":    true,
	"-execdir": true,
	"-ok":      true,
	"-okdir":   true,
	"-delete":  true,
	"-fprint":  true,
	"-fprintf": true,
	"-fprint0": true,
	"-fls":     true,
}

// grepShortFlags maps a bundled grep short-flag character to its rg
// token. An empty string drops the flag (rg is recursive by default, so
// -r/-R vanish). A character absent from this map makes the whole grep
// rewrite fall through, so unmapped or value-taking short flags (-A, -e,
// -m, ...) are left untranslated rather than guessed.
var grepShortFlags = map[rune]string{
	'r': "",
	'R': "",
	'i': "-i",
	'n': "-n",
	'v': "-v",
	'w': "-w",
	'x': "-x",
	'o': "-o",
	'c': "-c",
	'l': "-l",
	'H': "-H",
	'F': "-F",
	'E': "-E",
}

// grepLongFlags maps a grep long flag to its rg token, with the same
// drop-on-empty and fall-through-on-absent semantics as
// [grepShortFlags].
var grepLongFlags = map[string]string{
	"--recursive":          "",
	"--ignore-case":        "-i",
	"--line-number":        "-n",
	"--invert-match":       "-v",
	"--word-regexp":        "-w",
	"--line-regexp":        "-x",
	"--only-matching":      "-o",
	"--count":              "-c",
	"--files-with-matches": "-l",
	"--with-filename":      "-H",
	"--fixed-strings":      "-F",
	"--extended-regexp":    "-E",
}

// breConstructs are BRE backslash sequences that mean something different
// in rg's default (ERE-like) dialect: in BRE `\(` groups, in rg it is a
// literal paren. A pattern carrying any of these with no -E/-F suppresses
// the grep rewrite.
var breConstructs = []string{`\(`, `\)`, `\{`, `\}`, `\+`, `\?`, `\|`}

// edit is a byte-offset splice: replace command[start:end] with replace.
type edit struct {
	start, end int
	replace    string
}

// Rewrite takes the already-parsed prog of command and returns the
// rewritten command, whether the whole command is structurally read-only,
// and whether any grep/find rewrite actually happened. When changed is
// false newCmd equals command. The caller emits newCmd only when both
// changed and readOnly hold. readOnly reflects the original program and
// is meaningful only alongside changed; it is reported false when both
// rewrites are disabled.
func Rewrite(prog *syntax.File, command string, cfg Config) (newCmd string, readOnly bool, changed bool) {
	if !cfg.Grep && !cfg.Find {
		return command, false, false
	}

	readOnly = isReadOnly(prog)

	var edits []edit

	syntax.Walk(prog, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok {
			return true
		}

		name, ok := callName(call)
		if !ok {
			return true
		}

		switch {
		case cfg.Find && name == "find":
			word := call.Args[0]
			edits = append(edits, edit{
				start:   int(word.Pos().Offset()),
				end:     int(word.End().Offset()),
				replace: findPrefix(cfg.FindExcludes),
			})

		case cfg.Grep && name == "grep":
			span, ok := rewriteGrep(call, command, cfg.FindExcludes)
			if !ok {
				return true
			}

			edits = append(edits, edit{
				start:   int(call.Args[0].Pos().Offset()),
				end:     int(call.End().Offset()),
				replace: span,
			})
		}

		return true
	})

	if len(edits) == 0 {
		return command, readOnly, false
	}

	return applyEdits(command, edits), readOnly, true
}

// findPrefix builds the replacement for a `find` command word: `bfs` with
// a global `-exclude` prune of excludes. bfs hoists `-exclude` to a
// global prune regardless of position, so prepending it before the user's
// own paths and expressions is correct for any find invocation. With no
// excludes the prefix is a bare `bfs`.
func findPrefix(excludes []string) string {
	expr := excludeExpr(excludes)
	if expr == "" {
		return "bfs"
	}

	return `bfs -exclude \( ` + expr + ` \)`
}

// excludeExpr renders excludes as a bfs prune expression: basename
// entries as `-name <entry>`, slash-bearing entries as `-path '*<entry>'`
// (the leading `*` also matches when the dir is the search root itself),
// joined with `-o`. The interactive fish `find` wrapper builds the same
// expression in home/fish.nix (bfsExcludeExpr); keep the two in sync.
func excludeExpr(excludes []string) string {
	parts := make([]string, 0, len(excludes))

	for _, e := range excludes {
		if strings.Contains(e, "/") {
			parts = append(parts, "-path '*"+e+"'")
		} else {
			parts = append(parts, "-name "+e)
		}
	}

	return strings.Join(parts, " -o ")
}

// excludeGlobs renders excludes as rg `-g` exclude globs. Each entry gets
// a `-g '!<entry>'`; a slash-bearing entry also gets a `-g '!**/<entry>'`
// variant because rg anchors a glob containing a slash to the search
// root, so the `**/` form is needed to match it at any depth.
func excludeGlobs(excludes []string) []string {
	out := make([]string, 0, len(excludes)*2)

	for _, e := range excludes {
		out = append(out, "-g", "'!"+e+"'")

		if strings.Contains(e, "/") {
			out = append(out, "-g", "'!**/"+e+"'")
		}
	}

	return out
}

// rewriteGrep maps a grep CallExpr's argv to an rg argv string, returning
// false (no rewrite) when any flag is unmapped, the pattern looks like
// BRE that would mis-translate, or the call has no path argument (a
// pure-stdin grep gains nothing from rg). Pattern and path words are
// spliced verbatim from command to preserve their exact quoting; only the
// command word, flags, and exclude globs are generated.
func rewriteGrep(call *syntax.CallExpr, command string, excludes []string) (string, bool) {
	var (
		flags       []string
		positionals []*syntax.Word
		// explicitDialect records -E/-F (or their long forms): with an
		// explicit dialect the BRE-misread guard does not apply.
		explicitDialect bool
	)

	for _, arg := range call.Args[1:] {
		src := wordSrc(command, arg)

		// A leading '-' (other than the bare '-' stdin marker) marks a
		// flag. A flag we cannot prove maps cleanly -- an unmapped flag,
		// or a quoted/multi-part word like --include='*.go' that
		// singleLit rejects -- forces fall-through, never a reread as the
		// pattern. A word that does not start with '-' is a positional.
		if !strings.HasPrefix(src, "-") || src == "-" {
			positionals = append(positionals, arg)
			continue
		}

		lit, ok := singleLit(arg)
		if !ok {
			return "", false
		}

		if strings.HasPrefix(lit, "--") {
			tok, ok := grepLongFlags[lit]
			if !ok {
				return "", false
			}

			if lit == "--extended-regexp" || lit == "--fixed-strings" {
				explicitDialect = true
			}

			if tok != "" {
				flags = append(flags, tok)
			}

			continue
		}

		mapped, ok := mapShortCluster(lit[1:])
		if !ok {
			return "", false
		}

		if strings.ContainsAny(lit, "EF") {
			explicitDialect = true
		}

		flags = append(flags, mapped...)
	}

	// First positional is the pattern, the rest are paths. With no path,
	// rg reads stdin like grep would but adds no gitignore/context
	// benefit, so leave the call alone.
	if len(positionals) < 2 {
		return "", false
	}

	if !explicitDialect && looksBRE(wordSrc(command, positionals[0])) {
		return "", false
	}

	var b strings.Builder

	b.WriteString("rg")

	for _, f := range flags {
		b.WriteByte(' ')
		b.WriteString(f)
	}

	for _, p := range positionals {
		b.WriteByte(' ')
		b.WriteString(wordSrc(command, p))
	}

	for _, g := range excludeGlobs(excludes) {
		b.WriteByte(' ')
		b.WriteString(g)
	}

	return b.String(), true
}

// mapShortCluster maps a bundled short-flag cluster (the chars after the
// leading '-') to rg tokens, returning false if any character is
// unmapped. Dropped flags (-r/-R) contribute no token.
func mapShortCluster(chars string) ([]string, bool) {
	var out []string

	for _, c := range chars {
		tok, ok := grepShortFlags[c]
		if !ok {
			return nil, false
		}

		if tok != "" {
			out = append(out, tok)
		}
	}

	return out, true
}

// looksBRE reports whether pattern carries a BRE backslash construct that
// rg's default dialect would read differently.
func looksBRE(pattern string) bool {
	for _, c := range breConstructs {
		if strings.Contains(pattern, c) {
			return true
		}
	}

	return false
}

// applyEdits splices edits into command, copying unedited spans verbatim.
// Edits are sorted by start offset; they never overlap because grep and
// find rewrites touch disjoint spans.
func applyEdits(command string, edits []edit) string {
	sort.Slice(edits, func(i, j int) bool {
		return edits[i].start < edits[j].start
	})

	var b strings.Builder

	last := 0

	for _, e := range edits {
		b.WriteString(command[last:e.start])
		b.WriteString(e.replace)
		last = e.end
	}

	b.WriteString(command[last:])

	return b.String()
}

// isReadOnly reports whether prog is a single statement with no
// redirection or backgrounding whose command is one read-only stage or a
// pipeline of read-only stages. This is a whole-program structural check,
// not a "a matching call exists somewhere" walk, so `$(rg x) | dangerous`
// is correctly rejected.
func isReadOnly(prog *syntax.File) bool {
	if len(prog.Stmts) != 1 {
		return false
	}

	return readOnlyStmt(prog.Stmts[0])
}

// readOnlyStmt reports whether stmt and (for a pipeline) its stages are
// read-only.
func readOnlyStmt(stmt *syntax.Stmt) bool {
	if stmt == nil {
		return false
	}

	if len(stmt.Redirs) > 0 || stmt.Background || stmt.Coprocess {
		return false
	}

	switch cmd := stmt.Cmd.(type) {
	case *syntax.CallExpr:
		return readOnlyCall(cmd)
	case *syntax.BinaryCmd:
		if cmd.Op == syntax.Pipe || cmd.Op == syntax.PipeAll {
			return readOnlyStmt(cmd.X) && readOnlyStmt(cmd.Y)
		}

		// && and || sequence commands; not read-only.
		return false
	default:
		return false
	}
}

// readOnlyCall reports whether a single command call is read-only: its
// command word is on [readOnlyCommands] and, for find/bfs, it carries no
// [findActionWords] primary.
func readOnlyCall(call *syntax.CallExpr) bool {
	name, ok := callName(call)
	if !ok {
		return false
	}

	if !readOnlyCommands[name] {
		return false
	}

	if name == "find" || name == "bfs" {
		for _, arg := range call.Args[1:] {
			if lit, ok := singleLit(arg); ok && findActionWords[lit] {
				return false
			}
		}
	}

	return true
}

// callName returns the command word of call when it is a single
// unquoted literal (the form rules and rewrites match against).
func callName(call *syntax.CallExpr) (string, bool) {
	if len(call.Args) == 0 {
		return "", false
	}

	return singleLit(call.Args[0])
}

// singleLit returns the value of word when it is exactly one unquoted
// literal part, else ("", false).
func singleLit(word *syntax.Word) (string, bool) {
	if len(word.Parts) != 1 {
		return "", false
	}

	lit, ok := word.Parts[0].(*syntax.Lit)
	if !ok {
		return "", false
	}

	return lit.Value, true
}

// wordSrc returns the exact source text of word from command, preserving
// its original quoting.
func wordSrc(command string, word *syntax.Word) string {
	return command[word.Pos().Offset():word.End().Offset()]
}

// Parse decodes the JSON object passed via --search-rewrite-config into a
// [Config]. Empty input yields a zero config (both rewrites disabled);
// malformed JSON returns an error so wrapper misconfiguration is loud.
func Parse(s string) (Config, error) {
	if s == "" {
		return Config{}, nil
	}

	var cfg Config

	err := json.Unmarshal([]byte(s), &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("decoding search rewrite config JSON: %w", err)
	}

	return cfg, nil
}
