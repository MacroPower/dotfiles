package wireinput_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/wireinput"
)

const (
	testCall  = "toolu_01TestCallAAAAAAAAAAAAAAAA"
	testAgent = "a0123456789abcdef"
)

// entry returns the transcript line of an assistant entry for the tool
// call id, with wire as the input from the API response. A nil wire
// leaves wireToolInputs out of the entry.
func entry(t *testing.T, id string, wire map[string]any) string {
	t.Helper()

	fields := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": []any{
				map[string]any{"type": "tool_use", "id": id, "name": "Write", "input": map[string]any{}},
			},
		},
	}
	if wire != nil {
		fields["wireToolInputs"] = map[string]any{id: wire}
	}

	line, err := json.Marshal(fields)
	require.NoError(t, err)

	return string(line)
}

// appendLines appends one line per entry to the transcript at path,
// and creates the file and its directory when they are missing.
func appendLines(t *testing.T, path string, lines ...string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	require.NoError(t, err)

	_, err = file.WriteString(strings.Join(lines, "\n") + "\n")
	require.NoError(t, err)
	require.NoError(t, file.Close())
}

// agentPath returns the transcript of the test agent for the session
// transcript at session, in dir below the subagents directory.
func agentPath(session, dir string) string {
	return filepath.Join(strings.TrimSuffix(session, ".jsonl"), "subagents", dir, "agent-"+testAgent+".jsonl")
}

func TestLookup(t *testing.T) {
	t.Parallel()

	wire := map[string]any{"file_path": "/tmp/x.go", "content": "var r = '\\u2018'\n"}
	other := map[string]any{"file_path": "/tmp/other.go", "content": "other"}

	// mention is a user entry that names the call and the key without
	// holding the input of the call.
	mention, err := json.Marshal(map[string]any{
		"type":           "user",
		"message":        map[string]any{"content": testCall + ` has "wireToolInputs"`},
		"wireToolInputs": map[string]any{testCall: other},
	})
	require.NoError(t, err)

	// quote is a later assistant entry whose text names the call, with
	// neither the tool_use block nor the input of the call.
	quote, err := json.Marshal(map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": []any{map[string]any{"type": "text", "text": "The hook of " + testCall + " ran."}},
		},
		"wireToolInputs": map[string]any{"toolu_other": other},
	})
	require.NoError(t, err)

	cases := map[string]struct {
		// session and agent hold the lines of each transcript. A nil
		// slice leaves the file out.
		session []string
		agent   []string
		// agentDir is the directory of the agent transcript below the
		// subagents directory.
		agentDir string
		agentID  string
		call     string
		want     map[string]any
		err      error
	}{
		"session transcript": {
			session: []string{entry(t, "toolu_other", other), entry(t, testCall, wire)},
			call:    testCall,
			want:    wire,
		},
		"entry before later lines": {
			session: []string{entry(t, testCall, wire), entry(t, "toolu_other", other), string(mention)},
			call:    testCall,
			want:    wire,
		},
		"entry before an assistant entry that quotes the call": {
			session: []string{entry(t, testCall, wire), string(quote)},
			call:    testCall,
			want:    wire,
		},
		"latest entry of a call wins": {
			session: []string{entry(t, testCall, other), entry(t, testCall, wire)},
			call:    testCall,
			want:    wire,
		},
		"subagent transcript": {
			session: []string{entry(t, testCall, other)},
			agent:   []string{entry(t, testCall, wire)},
			agentID: testAgent,
			call:    testCall,
			want:    wire,
		},
		"subagent transcript in a nested directory": {
			session:  []string{entry(t, "toolu_other", other)},
			agent:    []string{entry(t, testCall, wire)},
			agentDir: filepath.Join("workflows", "wf_1"),
			agentID:  testAgent,
			call:     testCall,
			want:     wire,
		},
		"subagent call that only the session transcript holds": {
			session: []string{entry(t, testCall, wire)},
			agent:   []string{entry(t, "toolu_other", other)},
			agentID: testAgent,
			call:    testCall,
			err:     wireinput.ErrNotFound,
		},
		"subagent without a transcript": {
			session: []string{entry(t, testCall, wire)},
			agentID: testAgent,
			call:    testCall,
			err:     wireinput.ErrNotFound,
		},
		"agent id that is a path": {
			session: []string{entry(t, testCall, wire)},
			agent:   []string{entry(t, testCall, wire)},
			agentID: filepath.Join("..", "subagents", testAgent),
			call:    testCall,
			err:     wireinput.ErrNotFound,
		},
		"entry without the wire input": {
			session: []string{entry(t, testCall, nil)},
			call:    testCall,
			err:     wireinput.ErrNotRecorded,
		},
		"entry with the wire input of another call": {
			session: []string{
				strings.Replace(entry(t, "toolu_other", other), `"id":"toolu_other"`, `"id":"`+testCall+`"`, 1),
			},
			call: testCall,
			err:  wireinput.ErrNotRecorded,
		},
		"call in no assistant entry": {
			session: []string{entry(t, "toolu_other", other), string(mention)},
			call:    testCall,
			err:     wireinput.ErrNotFound,
		},
		"no transcript": {
			call: testCall,
			err:  wireinput.ErrNotFound,
		},
		"no call id": {
			session: []string{entry(t, testCall, wire)},
			err:     wireinput.ErrNotFound,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			session := filepath.Join(t.TempDir(), "session.jsonl")

			if tc.session != nil {
				appendLines(t, session, tc.session...)
			}

			if tc.agent != nil {
				appendLines(t, agentPath(session, tc.agentDir), tc.agent...)
			}

			got, err := wireinput.Lookup(session, tc.agentID, tc.call)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestLookupReadsATranscriptPathThatNamesTheAgentFile(t *testing.T) {
	t.Parallel()

	wire := map[string]any{"content": "from the agent file"}
	agent := agentPath(filepath.Join(t.TempDir(), "session.jsonl"), "")
	appendLines(t, agent, entry(t, testCall, wire))

	got, err := wireinput.Lookup(agent, testAgent, testCall)
	require.NoError(t, err)
	assert.Equal(t, wire, got)
}

func TestLookupSkipsAnUnfinishedLastLine(t *testing.T) {
	t.Parallel()

	wire := map[string]any{"content": "whole"}
	session := filepath.Join(t.TempDir(), "session.jsonl")

	appendLines(t, session, entry(t, testCall, wire))

	// Claude Code is still writing a later entry of the same call.
	file, err := os.OpenFile(session, os.O_WRONLY|os.O_APPEND, 0o600)
	require.NoError(t, err)

	_, err = file.WriteString(strings.TrimSuffix(entry(t, testCall, map[string]any{"content": "cut"}), "}"))
	require.NoError(t, err)
	require.NoError(t, file.Close())

	got, err := wireinput.Lookup(session, "", testCall)
	require.NoError(t, err)
	assert.Equal(t, wire, got)
}

func TestLookupReadsPastTheFirstTail(t *testing.T) {
	t.Parallel()

	wire := map[string]any{"content": "early"}

	// Two mebibytes of later entries push the call out of the first
	// tail that Lookup reads.
	filler := entry(t, "toolu_other", map[string]any{"content": strings.Repeat("x", 4096)})
	lines := []string{entry(t, "toolu_first", wire), entry(t, testCall, wire)}

	for range 512 {
		lines = append(lines, filler)
	}

	session := filepath.Join(t.TempDir(), "session.jsonl")
	appendLines(t, session, lines...)

	got, err := wireinput.Lookup(session, "", testCall)
	require.NoError(t, err)
	assert.Equal(t, wire, got)

	_, err = wireinput.Lookup(session, "", "toolu_missing")
	require.ErrorIs(t, err, wireinput.ErrNotFound)
}

func TestLookupWithoutATranscriptPath(t *testing.T) {
	t.Parallel()

	_, err := wireinput.Lookup("", "", testCall)
	require.ErrorIs(t, err, wireinput.ErrNotFound)
}

func TestWait(t *testing.T) {
	t.Parallel()

	wire := map[string]any{"content": "var r = '\\u2018'\n"}
	other := map[string]any{"content": "other"}
	long := entry(t, "toolu_other", map[string]any{"content": strings.Repeat("x", 4096)})

	cases := map[string]struct {
		// before holds the session transcript lines from before the
		// wait starts.
		before []string
		// arrive writes the entries that land during the wait.
		arrive  func(t *testing.T, session string)
		agentID string
		want    map[string]any
		err     error
	}{
		"entry that is there at the start": {
			before: []string{entry(t, testCall, wire)},
			want:   wire,
		},
		"entry that lands during the wait": {
			before: []string{entry(t, "toolu_other", other)},
			arrive: func(t *testing.T, session string) {
				t.Helper()
				appendLines(t, session, entry(t, "toolu_later", other), entry(t, testCall, wire))
			},
			want: wire,
		},
		"entry in a subagent transcript that appears during the wait": {
			before: []string{entry(t, "toolu_other", other)},
			arrive: func(t *testing.T, session string) {
				t.Helper()
				appendLines(t, agentPath(session, filepath.Join("workflows", "wf_1")), entry(t, testCall, wire))
			},
			agentID: testAgent,
			want:    wire,
		},
		"entry in a transcript that Claude Code rewrote shorter during the wait": {
			before: []string{long, long, long},
			arrive: func(t *testing.T, session string) {
				t.Helper()
				require.NoError(t, os.WriteFile(session, []byte(entry(t, testCall, wire)+"\n"), 0o600))
			},
			want: wire,
		},
		"entry without the wire input ends the wait": {
			before: []string{entry(t, "toolu_other", other)},
			arrive: func(t *testing.T, session string) {
				t.Helper()
				appendLines(t, session, entry(t, testCall, nil))
			},
			err: wireinput.ErrNotRecorded,
		},
		"entry of another call only": {
			before: []string{entry(t, "toolu_other", other)},
			arrive: func(t *testing.T, session string) {
				t.Helper()
				appendLines(t, session, entry(t, "toolu_later", other))
			},
			err: wireinput.ErrNotFound,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			session := filepath.Join(t.TempDir(), "session.jsonl")
			appendLines(t, session, tc.before...)

			// The wait reads the transcripts once before the entries
			// arrive, and has time for several reads after.
			ctx, cancel := context.WithTimeout(t.Context(), 10*wireinput.PollInterval)
			defer cancel()

			type result struct {
				input map[string]any
				err   error
			}

			done := make(chan result, 1)

			go func() {
				input, err := wireinput.Wait(ctx, session, tc.agentID, testCall)
				done <- result{input: input, err: err}
			}()

			if tc.arrive != nil {
				time.Sleep(2 * wireinput.PollInterval)
				tc.arrive(t, session)
			}

			got := <-done
			if tc.err != nil {
				require.ErrorIs(t, got.err, tc.err)
				return
			}

			require.NoError(t, got.err)
			assert.Equal(t, tc.want, got.input)
		})
	}
}

func TestWaitReadsOnceWhenTheContextHasEnded(t *testing.T) {
	t.Parallel()

	wire := map[string]any{"content": "there"}
	session := filepath.Join(t.TempDir(), "session.jsonl")
	appendLines(t, session, entry(t, testCall, wire))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	got, err := wireinput.Wait(ctx, session, "", testCall)
	require.NoError(t, err)
	assert.Equal(t, wire, got)

	_, err = wireinput.Wait(ctx, session, "", "toolu_missing")
	require.ErrorIs(t, err, wireinput.ErrNotFound)
}
