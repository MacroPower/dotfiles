package main

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/formatter"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/linter"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/wireinput"
)

// The texts in this file spell each escape with a doubled backslash in
// an interpreted string, and each decoded character with an eight-digit
// escape. Claude Code decodes neither spelling in a tool input, so an
// agent can edit this file with Write and Edit.

const (
	escapeCall  = "toolu_01EscapeCallAAAAAAAAAAAAAA"
	escapeAgent = "a0123456789abcdef"
)

// escapeTranscript appends the assistant entry of the tool call
// [escapeCall] to the transcript at path, with wire as the input from
// the API response.
func escapeTranscript(t *testing.T, path string, wire map[string]any) {
	t.Helper()

	escapeEntry(t, path, escapeCall, wire)
}

// escapeEntry appends the assistant entry of the tool call callID to
// the transcript at path, with wire as the input from the API response.
// A nil wire leaves wireToolInputs out of the entry, as in an entry
// whose input Claude Code did not record.
func escapeEntry(t *testing.T, path, callID string, wire map[string]any) {
	t.Helper()

	entry := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": []any{map[string]any{"type": "tool_use", "id": callID, "name": "Write"}},
		},
	}
	if wire != nil {
		entry["wireToolInputs"] = map[string]any{callID: wire}
	}

	line, err := json.Marshal(entry)
	require.NoError(t, err)

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	require.NoError(t, err)

	_, err = file.Write(append(line, '\n'))
	require.NoError(t, err)
	require.NoError(t, file.Close())
}

// escapePayload returns the PostToolUse payload of the tool call callID
// in the session whose transcript is at session. An empty callID
// leaves tool_use_id out of the payload.
func escapePayload(
	t *testing.T,
	session, agentID, callID, toolName string,
	toolInput, toolResponse map[string]any,
) []byte {
	t.Helper()

	fields := map[string]any{
		"transcript_path": session,
		"agent_id":        agentID,
		"tool_name":       toolName,
		"tool_input":      toolInput,
		"tool_response":   toolResponse,
	}
	if callID != "" {
		fields["tool_use_id"] = callID
	}

	input, err := json.Marshal(fields)
	require.NoError(t, err)

	return input
}

func TestHandlePostFileWriteRestoresEscapes(t *testing.T) {
	t.Parallel()

	const (
		quoteSent = "var quote = '\\u2018'"
		quoteRan  = "var quote = '\U00002018'"
		// literal is a line that holds the same character typed as
		// itself, which no restore touches.
		literal = "// The \U00002018 character opens a quotation.\n"
	)

	cases := map[string]struct {
		toolName string
		// file is the text on disk when the hook runs.
		file string
		// missing leaves the file out, as when a later call removed it
		// before the hook ran.
		missing bool
		// input and wire hold the tool input without its file_path, as
		// the tool ran it and as the model sent it. A nil wire leaves
		// the call out of the transcript.
		input    map[string]any
		wire     map[string]any
		response map[string]any
		// disable turns the restore off.
		disable bool
		// want is the text on disk after the hook. An empty want
		// stands for file.
		want string
		// wantNotice lists the texts that the block reason holds. A nil
		// slice means that the hook does not block.
		wantNotice []string
	}{
		"Write gets its escapes back": {
			toolName: "Write",
			file:     "package p\n\n" + quoteRan + "\n\nvar s = \"a\U000000e9b\U0000feff\"\n",
			input: map[string]any{
				"content": "package p\n\n" + quoteRan + "\n\nvar s = \"a\U000000e9b\U0000feff\"\n",
			},
			wire: map[string]any{"content": "package p\n\n" + quoteSent + "\n\nvar s = \"a\\u00e9b\\ufeff\"\n"},
			want: "package p\n\n" + quoteSent + "\n\nvar s = \"a\\u00e9b\\ufeff\"\n",
		},
		"Write keeps the whitespace strip": {
			toolName: "Write",
			file:     quoteRan + "\nend\n",
			input:    map[string]any{"content": quoteRan + "\nend\n"},
			wire:     map[string]any{"content": quoteSent + "  \nend\t\n"},
			want:     quoteSent + "\nend\n",
		},
		"Write keeps a character that the model typed as itself": {
			toolName: "Write",
			file:     literal + quoteRan + "\n",
			input:    map[string]any{"content": literal + quoteRan + "\n"},
			wire:     map[string]any{"content": literal + quoteSent + "\n"},
			want:     literal + quoteSent + "\n",
		},
		"Write that Claude Code left alone": {
			toolName: "Write",
			file:     literal,
			input:    map[string]any{"content": literal},
			wire:     map[string]any{"content": literal},
		},
		"Write of ASCII text that is not in the transcript": {
			toolName: "Write",
			file:     "plain\n",
			input:    map[string]any{"content": "plain\n"},
		},
		"Write with the restore off": {
			toolName: "Write",
			file:     quoteRan + "\n",
			input:    map[string]any{"content": quoteRan + "\n"},
			wire:     map[string]any{"content": quoteSent + "\n"},
			disable:  true,
		},
		"Write to a file that changed after the call": {
			toolName:   "Write",
			file:       quoteRan + "\nlater line\n",
			input:      map[string]any{"content": quoteRan + "\n"},
			wire:       map[string]any{"content": quoteSent + "\n"},
			wantNotice: []string{"could not match", "U+2018", `s/\x{2018}/\\u2018/g`},
		},
		"Write to a file that changed after the call lists each character once": {
			toolName:   "Write",
			file:       quoteRan + "\n" + quoteRan + "\nlater line\n",
			input:      map[string]any{"content": quoteRan + "\n" + quoteRan + "\n"},
			wire:       map[string]any{"content": quoteSent + "\n" + quoteSent + "\n"},
			wantNotice: []string{"could not match", "where you typed escapes: U+2018.\n"},
		},
		"Write to a file with a byte order mark": {
			toolName: "Write",
			file:     "\U0000feff" + quoteRan + "\n",
			input:    map[string]any{"content": quoteRan + "\n"},
			wire:     map[string]any{"content": quoteSent + "\n"},
			want:     "\U0000feff" + quoteSent + "\n",
		},
		"Write whose text starts with an escaped byte order mark": {
			toolName: "Write",
			file:     "\U0000feff" + quoteRan + "\n",
			input:    map[string]any{"content": "\U0000feff" + quoteRan + "\n"},
			wire:     map[string]any{"content": "\\ufeff" + quoteSent + "\n"},
			want:     "\\ufeff" + quoteSent + "\n",
		},
		"Write whose decoded characters a later write removed": {
			toolName: "Write",
			file:     "later\n",
			input:    map[string]any{"content": quoteRan + "\n"},
			wire:     map[string]any{"content": quoteSent + "\n"},
		},
		"Write whose file a later call removed": {
			toolName: "Write",
			missing:  true,
			input:    map[string]any{"content": quoteRan + "\n"},
			wire:     map[string]any{"content": quoteSent + "\n"},
		},
		"Edit gets its escapes back": {
			toolName: "Edit",
			file:     literal + quoteRan + " // new\nlast\n",
			input:    map[string]any{"old_string": "var quote = 'q'", "new_string": quoteRan + " // new"},
			wire:     map[string]any{"old_string": "var quote = 'q'", "new_string": quoteSent + " // new   "},
			response: map[string]any{
				"oldString":    "var quote = 'q'",
				"originalFile": literal + "var quote = 'q'\nlast\n",
			},
			want: literal + quoteSent + " // new\nlast\n",
		},
		"Edit under replace_all gets every copy back": {
			toolName: "Edit",
			file:     "a \U00002018 b \U00002018 c\n",
			input:    map[string]any{"old_string": "X", "new_string": "\U00002018", "replace_all": true},
			wire:     map[string]any{"old_string": "X", "new_string": "\\u2018", "replace_all": true},
			response: map[string]any{"oldString": "X", "originalFile": "a X b X c\n"},
			want:     "a \\u2018 b \\u2018 c\n",
		},
		"Edit that creates the file": {
			toolName: "Edit",
			file:     quoteRan + "\n",
			input:    map[string]any{"old_string": "", "new_string": quoteRan + "\n"},
			wire:     map[string]any{"old_string": "", "new_string": quoteSent + "\n"},
			response: map[string]any{"oldString": "", "originalFile": ""},
			want:     quoteSent + "\n",
		},
		"Edit that creates the file after a byte order mark": {
			toolName: "Edit",
			file:     "\U0000feff" + quoteRan + "\n",
			input:    map[string]any{"old_string": "", "new_string": quoteRan + "\n"},
			wire:     map[string]any{"old_string": "", "new_string": quoteSent + "\n"},
			response: map[string]any{"oldString": "", "originalFile": "\U0000feff"},
			want:     "\U0000feff" + quoteSent + "\n",
		},
		"Edit in a file with CRLF line ends": {
			toolName: "Edit",
			file:     "first\r\n" + quoteRan + "\r\n// next\r\nlast\r\n",
			input:    map[string]any{"old_string": "second", "new_string": quoteRan + "\n// next"},
			wire:     map[string]any{"old_string": "second", "new_string": quoteSent + "\n// next"},
			response: map[string]any{"oldString": "second", "originalFile": "first\nsecond\nlast\n"},
			want:     "first\r\n" + quoteSent + "\r\n// next\r\nlast\r\n",
		},
		"Edit in a file with mixed line ends": {
			toolName:   "Edit",
			file:       "first\r\n" + quoteRan + "\r\n// next\nlast\n",
			input:      map[string]any{"old_string": "second", "new_string": quoteRan + "\n// next"},
			wire:       map[string]any{"old_string": "second", "new_string": quoteSent + "\n// next"},
			response:   map[string]any{"oldString": "second", "originalFile": "first\nsecond\nlast\n"},
			wantNotice: []string{"could not match", "U+2018"},
		},
		"Edit that matched a curly-quoted spelling": {
			toolName: "Edit",
			file:     "a \U00002018q\U00002019 \U00002014\n",
			input:    map[string]any{"old_string": "'q'", "new_string": "'q' \U00002014"},
			wire:     map[string]any{"old_string": "'q'", "new_string": "'q' \\u2014"},
			response: map[string]any{
				"oldString":    "\U00002018q\U00002019",
				"originalFile": "a \U00002018q\U00002019\n",
			},
			want: "a \U00002018q\U00002019 \\u2014\n",
		},
		"Edit that matched an escaped spelling": {
			toolName: "Edit",
			file:     "var quote = '\\u2019' // was\n",
			input: map[string]any{
				"old_string": "var quote = '\U00002018'",
				"new_string": "var quote = '\U00002019' // was",
			},
			wire:     map[string]any{"old_string": quoteSent, "new_string": "var quote = '\\u2019' // was"},
			response: map[string]any{"oldString": quoteSent, "originalFile": quoteSent + "\n"},
		},
		"Edit after a later write to another line": {
			toolName: "Edit",
			file:     "changed since\n" + quoteRan + "\nlast\n",
			input:    map[string]any{"old_string": "var quote = 'q'", "new_string": quoteRan},
			wire:     map[string]any{"old_string": "var quote = 'q'", "new_string": quoteSent},
			response: map[string]any{"oldString": "var quote = 'q'", "originalFile": "first\nvar quote = 'q'\nlast\n"},
			want:     "changed since\n" + quoteSent + "\nlast\n",
		},
		"Edit whose text a later write changed": {
			toolName: "Edit",
			file:     "first\nvar other = '\U00002018'\nlast\n",
			input:    map[string]any{"old_string": "var quote = 'q'", "new_string": quoteRan},
			wire:     map[string]any{"old_string": "var quote = 'q'", "new_string": quoteSent},
			response: map[string]any{
				"oldString":    "var quote = 'q'",
				"originalFile": "first\nvar quote = 'q'\nlast\n",
			},
			wantNotice: []string{"could not match", "U+2018"},
		},
		"Edit whose decoded characters a later rewrite removed": {
			toolName: "Edit",
			file:     "// Package p quotes.\npackage p\n\nvar quote = \"x\"\n",
			input:    map[string]any{"old_string": "var quote = 'q'", "new_string": quoteRan},
			wire:     map[string]any{"old_string": "var quote = 'q'", "new_string": quoteSent},
			response: map[string]any{"oldString": "var quote = 'q'", "originalFile": "package p\n\nvar quote = 'q'\n"},
		},
		"Edit whose file a later write made shorter than the parts it left alone": {
			toolName: "Edit",
			file:     "x\n",
			input:    map[string]any{"old_string": "A", "new_string": quoteRan},
			wire:     map[string]any{"old_string": "A", "new_string": quoteSent},
			response: map[string]any{
				"oldString":    "A",
				"originalFile": "a long first line\nA\na long last line\n",
			},
		},
		"Edit without the file from before the edit": {
			toolName:   "Edit",
			file:       "first\n" + quoteRan + "\nlast\n",
			input:      map[string]any{"old_string": "var quote = 'q'", "new_string": quoteRan},
			wire:       map[string]any{"old_string": "var quote = 'q'", "new_string": quoteSent},
			wantNotice: []string{"could not match", "U+2018"},
		},
		"Write that the user changed in the permission dialog": {
			toolName: "Write",
			file:     "var quote = '\U00002018' // kept by the user\n",
			input:    map[string]any{"content": quoteRan + "\n"},
			wire:     map[string]any{"content": quoteSent + "\n"},
			response: map[string]any{"userModified": true},
		},
		"Write that Claude Code staged for review": {
			toolName: "Write",
			file:     "old text \U00002018\n",
			input:    map[string]any{"content": quoteRan + "\n"},
			wire:     map[string]any{"content": quoteSent + "\n"},
			response: map[string]any{"staged": true},
		},
		"MultiEdit is left alone": {
			toolName: "MultiEdit",
			file:     quoteRan + "\n",
			input: map[string]any{
				"edits": []any{map[string]any{"old_string": "q", "new_string": quoteRan}},
			},
			wire: map[string]any{
				"edits": []any{map[string]any{"old_string": "q", "new_string": quoteSent}},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tmp := t.TempDir()
			target := filepath.Join(tmp, "quote.go")
			session := filepath.Join(tmp, "session.jsonl")

			if !tc.missing {
				require.NoError(t, os.WriteFile(target, []byte(tc.file), 0o640))
			}

			require.NoError(t, os.WriteFile(session, nil, 0o600))

			if tc.wire != nil {
				escapeTranscript(t, session, tc.wire)
			}

			toolInput := map[string]any{"file_path": target}
			maps.Copy(toolInput, tc.input)

			cfg := config{
				formatterRules: formatter.New(nil),
				linterRules:    linter.New(nil),
				escapeRestore:  escapeRestore{Enable: !tc.disable, MaxWaitSeconds: 1},
			}

			input := escapePayload(t, session, "", escapeCall, tc.toolName, toolInput, tc.response)

			err := handlePostFileWrite(t.Context(), input, cfg, slog.New(slog.DiscardHandler))

			want := tc.want
			if want == "" {
				want = tc.file
			}

			if tc.missing {
				_, statErr := os.Stat(target)
				require.ErrorIs(t, statErr, fs.ErrNotExist, "the hook does not create the file again")
			} else {
				got, readErr := os.ReadFile(target)
				require.NoError(t, readErr)
				assert.Equal(t, want, string(got))

				info, statErr := os.Stat(target)
				require.NoError(t, statErr)
				assert.Equal(t, os.FileMode(0o640), info.Mode().Perm(), "the rewrite keeps the file mode")
			}

			if tc.wantNotice == nil {
				require.NoError(t, err)
				return
			}

			var blocked *blockError
			require.ErrorAs(t, err, &blocked)
			assert.Contains(t, blocked.Reason, target)

			for _, text := range tc.wantNotice {
				assert.Contains(t, blocked.Reason, text)
			}
		})
	}
}

func TestHandlePostFileWriteReadsTheSubagentTranscript(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	target := filepath.Join(tmp, "quote.go")
	session := filepath.Join(tmp, "session.jsonl")
	agent := filepath.Join(tmp, "session", "subagents", "workflows", "wf_1", "agent-"+escapeAgent+".jsonl")

	require.NoError(t, os.WriteFile(target, []byte("r := '\U00002018'\n"), 0o644))

	// The session transcript holds another call with the same id, which
	// a subagent's hook must not read.
	escapeTranscript(t, session, map[string]any{"content": "r := '\\u2019'\n"})
	escapeTranscript(t, agent, map[string]any{"content": "r := '\\u2018'\n"})

	cfg := config{escapeRestore: escapeRestore{Enable: true}}
	toolInput := map[string]any{"file_path": target, "content": "r := '\U00002018'\n"}
	input := escapePayload(t, session, escapeAgent, escapeCall, "Write", toolInput, nil)

	require.NoError(t, handlePostFileWrite(t.Context(), input, cfg, slog.New(slog.DiscardHandler)))

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "r := '\\u2018'\n", string(got))
}

func TestHandlePostFileWriteWaitsForTheTranscriptEntry(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	target := filepath.Join(tmp, "quote.go")
	session := filepath.Join(tmp, "session.jsonl")

	require.NoError(t, os.WriteFile(target, []byte("r := '\U00002018'\n"), 0o644))
	require.NoError(t, os.WriteFile(session, nil, 0o600))

	cfg := config{escapeRestore: escapeRestore{Enable: true, MaxWaitSeconds: 30}}
	toolInput := map[string]any{"file_path": target, "content": "r := '\U00002018'\n"}
	input := escapePayload(t, session, "", escapeCall, "Write", toolInput, nil)

	done := make(chan error, 1)

	go func() {
		done <- handlePostFileWrite(t.Context(), input, cfg, slog.New(slog.DiscardHandler))
	}()

	// Claude Code writes the entry after the model has sent the rest
	// of its message, which is after the hook started.
	time.Sleep(3 * wireinput.PollInterval)
	escapeTranscript(t, session, map[string]any{"content": "r := '\\u2018'\n"})

	require.NoError(t, <-done)

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "r := '\\u2018'\n", string(got))
}

func TestHandlePostFileWriteRestoresTwoEditsOfOneFileAtOnce(t *testing.T) {
	t.Parallel()

	const (
		before = "a := 'A'\nb := 'B'\n"
		afterA = "a := '\U00002018'\nb := 'B'\n"
		afterB = "a := '\U00002018'\nb := '\U00002019'\n"
		want   = "a := '\\u2018'\nb := '\\u2019'\n"
	)

	// Two Edits of one message changed the file one after the other. The
	// hooks of both wake together when the entries of the message land.
	edits := []struct {
		call, old, ran, sent, original string
	}{
		{
			call:     "toolu_01EscapeEditAAAAAAAAAAAAAA",
			old:      "'A'",
			ran:      "'\U00002018'",
			sent:     "'\\u2018'",
			original: before,
		},
		{
			call:     "toolu_01EscapeEditBBBBBBBBBBBBBB",
			old:      "'B'",
			ran:      "'\U00002019'",
			sent:     "'\\u2019'",
			original: afterA,
		},
	}

	cfg := config{escapeRestore: escapeRestore{Enable: true, MaxWaitSeconds: 1}}

	// A lost restore needs the two hooks to interleave, so the test
	// runs the pair many times.
	for run := range 100 {
		tmp := t.TempDir()
		target := filepath.Join(tmp, "quote.go")
		session := filepath.Join(tmp, "session.jsonl")

		require.NoError(t, os.WriteFile(target, []byte(afterB), 0o644))

		inputs := make([][]byte, 0, len(edits))

		for _, edit := range edits {
			escapeEntry(t, session, edit.call, map[string]any{"old_string": edit.old, "new_string": edit.sent})

			toolInput := map[string]any{"file_path": target, "old_string": edit.old, "new_string": edit.ran}
			response := map[string]any{"oldString": edit.old, "originalFile": edit.original}
			inputs = append(inputs, escapePayload(t, session, "", edit.call, "Edit", toolInput, response))
		}

		start := make(chan struct{})
		errs := make(chan error, len(inputs))

		for _, input := range inputs {
			go func() {
				<-start

				errs <- handlePostFileWrite(t.Context(), input, cfg, slog.New(slog.DiscardHandler))
			}()
		}

		close(start)

		for range inputs {
			require.NoError(t, <-errs, "run %d", run)
		}

		got, err := os.ReadFile(target)
		require.NoError(t, err)
		require.Equal(t, want, string(got), "run %d", run)
	}
}

func TestHandlePostFileWriteTellsTheAgentAboutAnUncheckedCall(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		content string
		// noCallID leaves tool_use_id out of the payload. The hook
		// cannot find the call in the transcript without it.
		noCallID bool
		// unrecorded puts the entry of the call in the transcript
		// without its wire input.
		unrecorded bool
		// maxWait is the wait limit in seconds. The cases that do not
		// wait set a long one, so a slow machine cannot reach it.
		maxWait int
		// wantWait is true when the hook waits for the entry and then
		// blocks.
		wantWait bool
	}{
		"text outside ASCII waits and then blocks": {
			content:  "r := '\U00002018' // \U0001f600\n",
			maxWait:  1,
			wantWait: true,
		},
		"ASCII text returns at once": {
			content: "r := 'q'\n",
			maxWait: 60,
		},
		"payload without a tool_use_id returns at once": {
			content:  "r := '\U00002018'\n",
			noCallID: true,
			maxWait:  60,
		},
		"entry without the wire input returns at once": {
			content:    "r := '\U00002018'\n",
			unrecorded: true,
			maxWait:    60,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tmp := t.TempDir()
			target := filepath.Join(tmp, "quote.go")
			session := filepath.Join(tmp, "session.jsonl")

			require.NoError(t, os.WriteFile(target, []byte(tc.content), 0o644))
			require.NoError(t, os.WriteFile(session, nil, 0o600))

			if tc.unrecorded {
				escapeTranscript(t, session, nil)
			}

			cfg := config{escapeRestore: escapeRestore{Enable: true, MaxWaitSeconds: tc.maxWait}}
			callID := escapeCall
			if tc.noCallID {
				callID = ""
			}

			toolInput := map[string]any{"file_path": target, "content": tc.content}
			input := escapePayload(t, session, "", callID, "Write", toolInput, nil)

			start := time.Now()
			err := handlePostFileWrite(t.Context(), input, cfg, slog.New(slog.DiscardHandler))
			waited := time.Since(start)

			got, readErr := os.ReadFile(target)
			require.NoError(t, readErr)
			assert.Equal(t, tc.content, string(got))

			if !tc.wantWait {
				require.NoError(t, err)
				assert.Less(t, waited, cfg.escapeRestore.maxWait())

				return
			}

			var blocked *blockError
			require.ErrorAs(t, err, &blocked)
			assert.GreaterOrEqual(t, waited, cfg.escapeRestore.maxWait())
			assert.Contains(t, blocked.Reason, "within 1s, so hook-router did not check "+target)
			assert.Contains(t, blocked.Reason, "U+2018, U+1F600")
			assert.Contains(t, blocked.Reason, `s/\x{2018}/\\u2018/g' '`+target+`'`)
		})
	}
}

func TestHandlePostFileWriteWithoutATranscriptDoesNotWait(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	target := filepath.Join(tmp, "quote.go")

	require.NoError(t, os.WriteFile(target, []byte("r := '\U00002018'\n"), 0o644))

	cfg := config{escapeRestore: escapeRestore{Enable: true, MaxWaitSeconds: 60}}
	toolInput := map[string]any{"file_path": target, "content": "r := '\U00002018'\n"}
	input := escapePayload(t, filepath.Join(tmp, "missing.jsonl"), "", escapeCall, "Write", toolInput, nil)

	start := time.Now()

	require.NoError(t, handlePostFileWrite(t.Context(), input, cfg, slog.New(slog.DiscardHandler)))
	assert.Less(t, time.Since(start), cfg.escapeRestore.maxWait())
}

func TestHandlePostFileWriteLintsTheRestoredEdit(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	target := filepath.Join(tmp, "doc.md")
	session := filepath.Join(tmp, "session.jsonl")

	require.NoError(t, os.WriteFile(target, []byte("one\ntwo \U00002018\nthree\n"), 0o644))
	escapeTranscript(t, session, map[string]any{"old_string": "2", "new_string": "two \\u2018"})

	rule := linter.Rule{PathGlob: filepath.Join(tmp, "*.md"), Command: lintLines}
	cfg := config{
		formatterRules: formatter.New(nil),
		linterRules:    linter.New([]linter.Rule{rule}),
		escapeRestore:  escapeRestore{Enable: true},
	}

	toolInput := map[string]any{"file_path": target, "old_string": "2", "new_string": "two \U00002018"}
	response := map[string]any{"oldString": "2", "originalFile": "one\n2\nthree\n"}
	input := escapePayload(t, session, "", escapeCall, "Edit", toolInput, response)

	err := handlePostFileWrite(t.Context(), input, cfg, slog.New(slog.DiscardHandler))

	var blocked *blockError
	require.ErrorAs(t, err, &blocked)

	// The linter reads the restored file, and the edit still reports
	// only the line it wrote.
	assert.Contains(t, blocked.Reason, target+":2:1: Style::Test: two \\u2018")
	assert.Equal(t, 1, strings.Count(blocked.Reason, target+":"))
}

func TestHandlePostFileWritePutsTheNoticeBeforeTheFindings(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	target := filepath.Join(tmp, "doc.md")
	session := filepath.Join(tmp, "session.jsonl")

	require.NoError(t, os.WriteFile(target, []byte("one \U00002018\nlater\n"), 0o644))
	escapeTranscript(t, session, map[string]any{"content": "one \\u2018\n"})

	rule := linter.Rule{PathGlob: filepath.Join(tmp, "*.md"), Command: lintLines}
	cfg := config{
		formatterRules: formatter.New(nil),
		linterRules:    linter.New([]linter.Rule{rule}),
		escapeRestore:  escapeRestore{Enable: true},
	}

	toolInput := map[string]any{"file_path": target, "content": "one \U00002018\n"}
	input := escapePayload(t, session, "", escapeCall, "Write", toolInput, nil)

	err := handlePostFileWrite(t.Context(), input, cfg, slog.New(slog.DiscardHandler))

	var blocked *blockError
	require.ErrorAs(t, err, &blocked)

	notice := strings.Index(blocked.Reason, "could not match")
	findings := strings.Index(blocked.Reason, "prose-lint: ")

	require.NotEqual(t, -1, notice)
	require.NotEqual(t, -1, findings)
	assert.Less(t, notice, findings)
}

func TestParseEscapeRestore(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in   string
		want escapeRestore
		err  bool
	}{
		"empty string is off": {
			in: "",
		},
		"fields round-trip": {
			in:   `{"enable":true,"maxWaitSeconds":60}`,
			want: escapeRestore{Enable: true, MaxWaitSeconds: 60},
		},
		"malformed JSON returns an error": {
			in:  `{"enable":`,
			err: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := parseEscapeRestore(tc.in)
			if tc.err {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestEscapeText(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "\\u2018", escapeText(0x2018))
	assert.Equal(t, "\\ud83d\\ude00", escapeText(0x1f600))
}

func TestCodePoints(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		runes []rune
		want  string
	}{
		"short list": {
			runes: []rune{0x2018, 0x1f600},
			want:  "U+2018, U+1F600",
		},
		"list past the cap": {
			runes: []rune{
				0x2010,
				0x2011,
				0x2012,
				0x2013,
				0x2014,
				0x2015,
				0x2016,
				0x2017,
				0x2018,
				0x2019,
				0x201a,
				0x201b,
				0x201c,
			},
			want: "U+2010, U+2011, U+2012, U+2013, U+2014, U+2015, U+2016, U+2017, U+2018, U+2019, U+201A, U+201B, " +
				"and 1 more",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, codePoints(tc.runes))
		})
	}
}

func TestRepairExample(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		escapes []wireinput.Escape
		want    string
	}{
		"first character outside ASCII": {
			escapes: []wireinput.Escape{{Text: "\\u0041", Rune: 'A'}, {Text: "\\u201C", Rune: 0x201c}},
			want:    `perl -CSD -i -pe 's/\x{201c}/\\u201C/g' '/tmp/it'\''s.go'`,
		},
		"surrogate pair": {
			escapes: []wireinput.Escape{{Text: "\\ud83d\\ude00", Rune: 0x1f600}},
			want:    `perl -CSD -i -pe 's/\x{1f600}/\\ud83d\\ude00/g' '/tmp/it'\''s.go'`,
		},
		"ASCII characters get no command": {
			escapes: []wireinput.Escape{{Text: "\\u0041", Rune: 'A'}},
			want:    "Rewrite only the lines that you wrote",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := repairExample("/tmp/it's.go", tc.escapes)
			assert.Contains(t, got, tc.want)
			assert.Equal(t, strings.Contains(tc.want, "perl"), strings.Contains(got, "perl"))
		})
	}
}

func TestEscapeRestoreWaitFor(t *testing.T) {
	t.Parallel()

	on := escapeRestore{Enable: true, MaxWaitSeconds: 60}

	cases := map[string]struct {
		cfg   escapeRestore
		event string
		tool  string
		input string
		want  time.Duration
	}{
		"Write from the payload": {
			cfg:   on,
			event: "PostToolUse",
			input: `{"tool_name":"Write"}`,
			want:  time.Minute,
		},
		"Edit from the flag": {
			cfg:   on,
			event: "PostToolUse",
			tool:  "Edit",
			want:  time.Minute,
		},
		"Bash does not wait": {
			cfg:   on,
			event: "PostToolUse",
			input: `{"tool_name":"Bash"}`,
		},
		"PreToolUse does not wait": {
			cfg:   on,
			event: "PreToolUse",
			input: `{"tool_name":"Write"}`,
		},
		"payload that does not parse": {
			cfg:   on,
			event: "PostToolUse",
			input: `{`,
		},
		"restore off": {
			cfg:   escapeRestore{MaxWaitSeconds: 60},
			event: "PostToolUse",
			input: `{"tool_name":"Write"}`,
		},
		"negative wait": {
			cfg:   escapeRestore{Enable: true, MaxWaitSeconds: -1},
			event: "PostToolUse",
			input: `{"tool_name":"Write"}`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, tc.cfg.waitFor(tc.event, tc.tool, []byte(tc.input)))
		})
	}
}
