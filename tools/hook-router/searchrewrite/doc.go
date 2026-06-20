// Package searchrewrite transparently rewrites `grep` and `find`
// invocations in a Bash command into `rg` and `bfs` before the command
// runs, so weaker agents that reach for the wrong tool still get
// gitignore-aware, worktree-skipping search without blowing up the
// context window.
//
// Rewriting is byte-offset splicing over the original command text: only
// the spans of the nodes being changed are edited, every other byte is
// copied verbatim, so quoting and surrounding pipeline stages survive.
// `find` becomes `bfs` with a global `-exclude` prune injected right
// after the command word (bfs is a drop-in find replacement and hoists
// `-exclude` to a global prune regardless of position). `grep` has its
// argv remapped to rg equivalents with exclude globs appended, but only
// when every flag maps cleanly and the pattern carries no BRE construct
// that would mis-translate -- otherwise the call is left untouched.
//
// A rewrite is only safe to auto-approve when the whole command is
// read-only, so [Rewrite] also reports a structural read-only verdict
// (single statement, no redirection or backgrounding, every pipeline
// stage on a small filters-and-search allowlist, no find/bfs action
// word). The caller emits the rewrite only when both changed and
// read-only hold.
package searchrewrite
