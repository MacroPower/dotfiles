// Package wireinput reads the input of a tool call as the model sent
// it. It also puts back the `\uXXXX` escapes that Claude Code decoded
// before the tool ran.
//
// Claude Code rewrites the input of every tool call between the API
// response and the tool. One pass decodes each `\uXXXX` escape in a
// string into the character it names. A Write of Go source that spells
// a curly quote as an escape then stores the quote itself. A second
// pass strips the whitespace at the end of each line of a Write's
// content and of an Edit's new_string, except in a Markdown file. A
// hook receives the rewritten input.
//
// The session transcript keeps both forms. Each assistant entry holds
// the rewritten input in its tool_use block. It holds the input from
// the API response under wireToolInputs, keyed by the id of the tool
// call, and [Lookup] returns that form. The entries of a subagent go to
// a file of their own under the session's subagents directory. [Lookup]
// reads that file when the hook payload names an agent.
//
// Claude Code runs a tool as soon as the model has sent the call. It
// writes the transcript entries of a message after the model has sent
// the whole message. A PostToolUse hook for a call that other calls
// follow therefore starts before its entry exists, and [Wait] polls the
// transcript until the entry arrives.
//
// [Restore] aligns the text the model sent with the text the tool ran
// with. It returns the second text with each decoded escape back in
// place, and it keeps the whitespace strip. An escape that decoded to
// whitespace at the end of a line comes back too, because the strip
// removed its character. The second text can also come from the file
// that an Edit wrote. The Edit tool curls the straight quotes of its
// new text when the text that it replaces uses curly quotes. [Restore]
// therefore accepts a curly quote where the first text holds a
// straight quote of the same kind.
//
// [Restore] reads which escapes Claude Code decoded from the two
// texts, so it follows the pass wherever the pass leaves an escape
// alone. The pass skips an escape after an odd number of backslashes,
// an escape of a control character, and half of a surrogate pair. It
// also skips every escape in a string that holds a Windows path.
//
// Claude Code does not document wireToolInputs, the layout of the
// transcript directory, or the time it writes an entry. This package
// follows Claude Code 2.1.319. [Lookup] returns [ErrNotFound] for a
// call that it cannot find. [Restore] returns [ErrMisaligned] for two
// texts that differ in any other way. A caller leaves the file alone in
// both cases.
package wireinput
