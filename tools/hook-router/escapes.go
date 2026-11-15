package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/hook"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/wireinput"
)

const (
	// editTool is the tool_name of the Edit tool.
	editTool = "Edit"

	// byteOrderMark is U+FEFF in UTF-8, which can open a text file.
	byteOrderMark = "\U0000feff"

	// escapeNoticeRunes caps the characters that an escape notice
	// lists.
	escapeNoticeRunes = 12

	// reasonNoMatch explains a file whose text [rewriteEscapes] cannot
	// match to the text of the call. Either the file does not hold one
	// text between the parts that the call left alone, or
	// [wireinput.Restore] cannot align that text with the sent text.
	reasonNoMatch = "it could not match the text in the file to the text that this call sent"

	// escapeNoticeIntro opens every escape notice.
	escapeNoticeIntro = `hook-router: Claude Code decodes each \uXXXX escape in a tool input before the tool runs, ` +
		`and hook-router puts the escapes back after a Write or an Edit.`

	// escapeNoticeFix names the repair in every escape notice. An
	// Edit cannot make it, because the decoded escape in its
	// new_string equals the character in its old_string.
	escapeNoticeFix = `run a command that spells the escape with two backslashes, which Claude Code leaves alone.`
)

// escapeFields names the tool_input field whose text each file tool
// writes into the file. [restoreDecodedEscapes] covers the tools
// listed here.
var escapeFields = map[string]string{
	"Write":  "content",
	editTool: "new_string",
}

// escapeRestore configures [restoreDecodedEscapes]. Its zero value
// turns the handler off.
//
// JSON tags are camelCase because [builtins.toJSON] in home/claude.nix
// emits attribute names verbatim.
type escapeRestore struct {
	// Enable turns the handler on.
	Enable bool `json:"enable"`
	// MaxWaitSeconds is the longest wait for the transcript entry of a
	// call whose text holds a character outside ASCII.
	MaxWaitSeconds int `json:"maxWaitSeconds"`
}

// parseEscapeRestore decodes the --escape-restore-config JSON. An
// empty string returns the zero value.
func parseEscapeRestore(s string) (escapeRestore, error) {
	var cfg escapeRestore

	if s == "" {
		return cfg, nil
	}

	err := json.Unmarshal([]byte(s), &cfg)
	if err != nil {
		return escapeRestore{}, fmt.Errorf("decoding escape restore config: %w", err)
	}

	return cfg, nil
}

// maxWait returns MaxWaitSeconds as a duration. A negative value
// counts as zero.
func (c escapeRestore) maxWait() time.Duration {
	return time.Duration(max(c.MaxWaitSeconds, 0)) * time.Second
}

// waitFor returns the longest time [restoreDecodedEscapes] can wait in
// the invocation for event and tool. [mainErr] adds it to its own time
// budget, because the wait ends before the formatter and the linter
// start. It is zero unless the handler is on and the invocation is the
// PostToolUse of a tool in [escapeFields]. An empty tool reads the tool
// name from input, the payload.
func (c escapeRestore) waitFor(event, tool string, input []byte) time.Duration {
	if !c.Enable || event != eventPostToolUse {
		return 0
	}

	if tool == "" {
		parsed, err := hook.ParseInput(input)
		if err == nil {
			tool = parsed.ToolName
		}
	}

	_, ok := escapeFields[tool]
	if !ok {
		return 0
	}

	return c.maxWait()
}

// restoreDecodedEscapes puts back the `\uXXXX` escapes that Claude Code
// decoded in the text of a Write or an Edit before the tool ran.
// Claude Code decodes every such escape in a tool input. Go source that
// spells a character as an escape then reaches the file with the
// character itself. The handler runs before the formatter and the
// linter, so both see the file as the model wrote it.
//
// The handler reads the text the model sent from the transcript (see
// [wireinput.Wait]). It compares that text with the text in the
// payload (see [wireinput.Restore]). Then it finds the text that the
// call wrote in the file and rewrites only that text.
//
// Claude Code writes the transcript entry of a call after the model
// has sent its whole message, which can be after this hook starts. A
// decoded escape nearly always leaves a character outside ASCII. The
// handler therefore waits for the entry only when the text holds such
// a character, and for cfg.maxWait at most. Any other text gets one
// read of the transcript. The model is still sending its message
// during the wait, so the wait delays only the later calls of that
// message.
//
// The handler returns the payload for the linter, with the restored
// text in place of the decoded text. An Edit then still reports the
// findings on its own lines. The handler also returns a notice for the
// agent, which is empty in the common case. The notice names an escape
// that the handler could not put back, or a text that it could not
// check because the entry did not arrive in time.
//
// The handler leaves a call alone when the user changed it in the
// permission dialog, and when Claude Code staged its write for review.
func restoreDecodedEscapes(
	ctx context.Context,
	h hook.Input,
	filePath string,
	cfg escapeRestore,
	logger *slog.Logger,
) (hook.Input, string) {
	field, ok := escapeFields[h.ToolName]
	if !ok || h.ToolUseID == "" {
		return h, ""
	}

	ran := stringField(h.ToolInput, field)
	if ran == "" {
		return h, ""
	}

	// The file does not hold the text of the payload when the user
	// changed the call in the permission dialog, or when Claude Code
	// staged the write for review.
	if boolField(h.ToolResponse, "userModified") || boolField(h.ToolResponse, "staged") {
		return h, ""
	}

	// A session without a transcript file never gets the entry.
	_, err := os.Stat(h.TranscriptPath)
	if err != nil {
		return h, ""
	}

	outside := runesOutsideASCII(ran)

	var wait time.Duration
	if len(outside) > 0 {
		wait = cfg.maxWait()
	}

	waitCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()

	wire, err := wireinput.Wait(waitCtx, h.TranscriptPath, h.AgentID, h.ToolUseID)
	if err != nil {
		logger.DebugContext(ctx, "no wire input for the tool call",
			slog.String("tool_use_id", h.ToolUseID),
			slog.Duration("waited", wait),
			slog.Any("error", err),
		)

		if wait > 0 && errors.Is(err, wireinput.ErrNotFound) {
			return h, uncheckedNotice(h.ToolName, filePath, outside, wait)
		}

		return h, ""
	}

	sent := stringField(wire, field)

	decoded, err := wireinput.Restore(sent, ran)
	if err != nil || len(decoded.Escapes) == 0 {
		return h, ""
	}

	restored, left, reason := rewriteEscapes(ctx, h, filePath, sent, ran, decoded)
	if reason != "" {
		logger.InfoContext(ctx, "left decoded escapes in the file",
			slog.String("file_path", filePath),
			slog.String("tool", h.ToolName),
			slog.Int("escapes", len(left)),
			slog.String("reason", reason),
		)

		return h, decodedNotice(h.ToolName, filePath, left, reason)
	}

	if len(restored.Escapes) == 0 {
		return h, ""
	}

	logger.InfoContext(ctx, "restored escapes",
		slog.String("file_path", filePath),
		slog.String("tool", h.ToolName),
		slog.Int("escapes", len(restored.Escapes)),
	)

	toolInput := maps.Clone(h.ToolInput)
	toolInput[field] = restored.Text
	h.ToolInput = toolInput

	return h, ""
}

// rewriteEscapes rewrites the file with the escapes back and returns
// the restoration that the file now holds. A restoration with an empty
// escape list means that the file did not need a change. A non-empty
// reason says why the file keeps characters that Claude Code decoded.
// The second result then lists the escapes of those characters. The
// reason stays empty when a later call removed the file, or rewrote it
// so that the file lacks every decoded character.
//
// decoded is the restoration of ran, the text in the payload. The text
// in the file can differ from ran, because the Edit tool rewrites its
// new text in two cases. It spells every character outside ASCII as an
// escape when the text it replaces used escapes. It turns straight
// quotes into curly quotes when the text it replaces used curly quotes.
// The function therefore aligns sent with the text in the file, see
// [wireinput.Restore]. The rewrite keeps each escape and each curly
// quote that the Edit tool wrote.
//
// The function holds the lock of the file from the read to the write,
// see [lockFile]. The hook of another call to the file then reads the
// file with these escapes back.
func rewriteEscapes(
	ctx context.Context,
	h hook.Input,
	filePath, sent, ran string,
	decoded wireinput.Restoration,
) (wireinput.Restoration, []wireinput.Escape, string) {
	file, err := lockFile(ctx, filePath, os.O_RDWR)
	if errors.Is(err, fs.ErrNotExist) {
		// A later call removed the file and the characters with it.
		return wireinput.Restoration{}, nil, ""
	}

	if err != nil {
		return wireinput.Restoration{}, decoded.Escapes, "it could not lock the file: " + err.Error()
	}

	restored, left, reason := rewriteLockedEscapes(file, h, sent, ran, decoded)

	// Closing the file releases the lock.
	err = file.Close()
	if err != nil && reason == "" && len(restored.Escapes) > 0 {
		return wireinput.Restoration{}, restored.Escapes, "it could not write the file: " + err.Error()
	}

	return restored, left, reason
}

// rewriteLockedEscapes is [rewriteEscapes] on the file that it opened
// and locked.
func rewriteLockedEscapes(
	file *os.File,
	h hook.Input,
	sent, ran string,
	decoded wireinput.Restoration,
) (wireinput.Restoration, []wireinput.Escape, string) {
	content, err := io.ReadAll(file)
	if err != nil {
		return wireinput.Restoration{}, decoded.Escapes, "it could not read the file: " + err.Error()
	}

	// Claude Code reads a file with CRLF line ends as LF text and
	// writes the CRLF back. The parts then hold LF where the file
	// holds CRLF.
	current := string(content)
	crlf := false

	parts, ok := writtenParts(h, current, ran)
	if !ok {
		current = strings.ReplaceAll(current, "\r\n", "\n")
		crlf = strings.ReplaceAll(current, "\n", "\r\n") == string(content)

		parts, ok = writtenParts(h, current, ran)
		if !ok || !crlf {
			// A later write can rewrite the file without the
			// characters, for example a Write after an Edit.
			left := heldEscapes(current, decoded.Escapes)
			if len(left) == 0 {
				return wireinput.Restoration{}, nil, ""
			}

			return wireinput.Restoration{}, left, reasonNoMatch
		}
	}

	restored := decoded

	wrote, _ := textBetween(current, parts)
	if wrote != ran {
		restored, err = wireinput.Restore(sent, wrote)
		if err != nil {
			// The escapes whose characters the file text lacks are
			// text again, or gone with a later write.
			left := heldEscapes(wrote, decoded.Escapes)
			if len(left) == 0 {
				return wireinput.Restoration{}, nil, ""
			}

			return wireinput.Restoration{}, left, reasonNoMatch
		}
	}

	if len(restored.Escapes) == 0 {
		return wireinput.Restoration{}, nil, ""
	}

	want := strings.Join(parts, restored.Text)
	if crlf {
		want = strings.ReplaceAll(want, "\n", "\r\n")
	}

	// The write goes to the open file, so the file keeps its mode. Each
	// escape is longer than its character, so the new text covers the
	// old text. The truncate sets the file to the length of the new
	// text.
	_, err = file.WriteAt([]byte(want), 0)
	if err == nil {
		err = file.Truncate(int64(len(want)))
	}

	if err != nil {
		return wireinput.Restoration{}, restored.Escapes, "it could not write the file: " + err.Error()
	}

	return restored, nil, ""
}

// writtenParts returns the parts of current, the text of the file,
// around the text that the call wrote. That text sits between each
// pair of neighbors, see [textBetween].
//
// The parts come from the payload when the file still is those parts
// joined by one text, see [unchangedParts]. A later write to another
// place in the file breaks that match. For an Edit the function then
// cuts current at ran, the text in the payload. It does so only when
// current holds ran one time and the file from before the edit does
// not hold it. The match is then the text that this call wrote.
func writtenParts(h hook.Input, current, ran string) ([]string, bool) {
	parts, ok := unchangedParts(h, current, ran)
	if ok {
		_, ok = textBetween(current, parts)
		if ok {
			return parts, true
		}
	}

	original, ok := h.ToolResponse["originalFile"].(string)
	if !ok || h.ToolName != editTool || strings.Contains(original, ran) || strings.Count(current, ran) != 1 {
		return nil, false
	}

	before, after, _ := strings.Cut(current, ran)

	return []string{before, after}, true
}

// unchangedParts returns the parts of the file that the call left
// alone, in file order. The text that the call wrote sits between each
// pair of neighbors. A Write replaces the whole file, so its parts come
// from [wholeFileParts]. An Edit splits the file from before the edit
// at the text it replaced. The split is at the first match, or at every
// match under replace_all.
//
// The function reads the replaced text from tool_response.oldString.
// That field names the text that the Edit tool matched in the file.
// The matched text differs from tool_input.old_string when the tool
// matched an escaped or a curly-quoted spelling. The function reports
// false for an Edit whose payload lacks the file from before the edit.
// It also reports false when that file does not hold the replaced text.
func unchangedParts(h hook.Input, current, ran string) ([]string, bool) {
	if h.ToolName != editTool {
		return wholeFileParts(current, ran), true
	}

	replaced, ok := h.ToolResponse["oldString"].(string)
	if !ok {
		replaced = stringField(h.ToolInput, "old_string")
	}

	// An Edit with an empty old_string writes its text as the whole
	// file.
	if replaced == "" {
		return wholeFileParts(current, ran), true
	}

	original, ok := h.ToolResponse["originalFile"].(string)
	if !ok {
		return nil, false
	}

	limit := 2
	if replaceAll, ok := h.ToolInput["replace_all"].(bool); ok && replaceAll {
		limit = -1
	}

	parts := strings.SplitN(original, replaced, limit)

	return parts, len(parts) > 1
}

// wholeFileParts returns the parts of current that a call left alone
// when it wrote ran as the whole file. Claude Code puts the byte order
// mark of the file back in front of a text that starts without one.
// The mark is then the first part. Any other file gives two empty
// parts.
func wholeFileParts(current, ran string) []string {
	if strings.HasPrefix(current, byteOrderMark) && !strings.HasPrefix(ran, byteOrderMark) {
		return []string{byteOrderMark, ""}
	}

	return []string{"", ""}
}

// textBetween returns the text that sits between each pair of
// neighboring parts in content. It reports false when content is not
// the parts joined by one text.
func textBetween(content string, parts []string) (string, bool) {
	gaps := len(parts) - 1
	size := len(content)

	for _, part := range parts {
		size -= len(part)
	}

	if gaps < 1 || size < 0 || size%gaps != 0 {
		return "", false
	}

	start := len(parts[0])
	text := content[start : start+size/gaps]

	return text, strings.Join(parts, text) == content
}

// boolField reports whether fields[key] is true.
func boolField(fields map[string]any, key string) bool {
	value, ok := fields[key].(bool)

	return ok && value
}

// heldEscapes returns the escapes whose decoded character text holds.
func heldEscapes(text string, escapes []wireinput.Escape) []wireinput.Escape {
	var held []wireinput.Escape

	for _, escape := range escapes {
		if strings.ContainsRune(text, escape.Rune) {
			held = append(held, escape)
		}
	}

	return held
}

// runesOutsideASCII returns each character outside ASCII in text, one
// time, in the order of its first use.
func runesOutsideASCII(text string) []rune {
	var runes []rune

	seen := map[rune]bool{}

	for _, r := range text {
		if r < utf8.RuneSelf || seen[r] {
			continue
		}

		seen[r] = true

		runes = append(runes, r)
	}

	return runes
}

// decodedNotice returns the feedback for an agent whose call left
// decoded characters in the file.
func decodedNotice(tool, filePath string, escapes []wireinput.Escape, reason string) string {
	var runes []rune

	seen := map[rune]bool{}

	for _, escape := range escapes {
		if seen[escape.Rune] {
			continue
		}

		seen[escape.Rune] = true

		runes = append(runes, escape.Rune)
	}

	return fmt.Sprintf(`%s
It left the escapes of this %s decoded, because %s.
%s holds these characters where you typed escapes: %s.
To put an escape back, %s
%s`,
		escapeNoticeIntro, tool, reason, filePath, codePoints(runes), escapeNoticeFix,
		repairExample(filePath, escapes))
}

// uncheckedNotice returns the feedback for an agent whose call holds
// characters outside ASCII and has no transcript entry after the wait.
func uncheckedNotice(tool, filePath string, outside []rune, wait time.Duration) string {
	return fmt.Sprintf(`%s
The transcript did not hold what this %s sent within %ds, so hook-router did not check %s.
The text of the call holds these characters outside ASCII: %s.
If you typed one of them as an escape, %s
%s`,
		escapeNoticeIntro, tool, int(wait.Seconds()), filePath, codePoints(outside), escapeNoticeFix,
		repairExample(filePath, []wireinput.Escape{{Text: escapeText(outside[0]), Rune: outside[0]}}))
}

// repairExample returns a command that rewrites one of the decoded
// characters as its escape across the file, with the sentence that
// introduces it. The command doubles each backslash of the escape.
// Claude Code leaves an escape after an odd number of backslashes
// alone, and Perl reads the pair as one backslash.
//
// The command names the first character outside ASCII. A file holds
// an ASCII character in other text too, so escapes that are all ASCII
// get advice for single lines instead.
func repairExample(filePath string, escapes []wireinput.Escape) string {
	for _, escape := range escapes {
		if escape.Rune < utf8.RuneSelf {
			continue
		}

		return fmt.Sprintf(`This command turns every U+%04X in the file into its escape:
perl -CSD -i -pe 's/\x{%x}/%s/g' '%s'`,
			escape.Rune, escape.Rune,
			strings.ReplaceAll(escape.Text, `\`, `\\`), strings.ReplaceAll(filePath, `'`, `'\''`))
	}

	return `The characters are ASCII, and the file holds them in other text too. ` +
		`Rewrite only the lines that you wrote, with the escape as \\uXXXX in a sed or Perl replacement.`
}

// escapeText returns the `\uXXXX` escape of r, or the escapes of its
// surrogate pair when r lies past U+FFFF.
func escapeText(r rune) string {
	high, low := utf16.EncodeRune(r)
	if high == utf8.RuneError && low == utf8.RuneError {
		return fmt.Sprintf("\\u%04x", r)
	}

	return fmt.Sprintf("\\u%04x\\u%04x", high, low)
}

// codePoints lists runes by code point, up to [escapeNoticeRunes] of
// them.
func codePoints(runes []rune) string {
	shown := runes[:min(len(runes), escapeNoticeRunes)]
	points := make([]string, 0, len(shown))

	for _, r := range shown {
		points = append(points, fmt.Sprintf("U+%04X", r))
	}

	list := strings.Join(points, ", ")
	if len(runes) > len(shown) {
		list = fmt.Sprintf("%s, and %d more", list, len(runes)-len(shown))
	}

	return list
}
