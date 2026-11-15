package hook_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/hook"
)

// mcpPostToolUse is the PostToolUse payload that Claude Code 2.1.319
// sent for a mcp__github__create_pull_request call, with the session
// paths shortened. The call ran against a stand-in for the GitHub MCP
// server. The text of its content block is the result that the server
// returned for a call that opened a pull request.
const mcpPostToolUse = `{
	"session_id": "b96237cb-e49b-4a7c-830f-ad91c5d141cb",
	"transcript_path": "/home/user/.claude/projects/-work/b96237cb.jsonl",
	"cwd": "/work",
	"prompt_id": "8d0fa2f2-5868-4985-b109-891c91f2ac11",
	"permission_mode": "default",
	"effort": {"level": "medium"},
	"hook_event_name": "PostToolUse",
	"tool_name": "mcp__github__create_pull_request",
	"tool_input": {
		"owner": "MacroPower",
		"repo": "terrarium",
		"title": "feat: persist the proxy CA",
		"head": "proxy-ca-trust",
		"base": "main",
		"draft": true
	},
	"tool_response": [
		{
			"type": "text",
			"text": "{\"id\":\"5205026092\",\"url\":\"https://github.com/MacroPower/terrarium/pull/44\"}"
		}
	],
	"tool_use_id": "toolu_01F18cNuFkaeyTCHJeh7UBCf",
	"duration_ms": 8,
	"mcp_server": {"name": "github", "source": "dynamic"}
}`

func TestParseInput(t *testing.T) {
	t.Parallel()

	t.Run("payload of an MCP tool", func(t *testing.T) {
		t.Parallel()

		got, err := hook.ParseInput([]byte(mcpPostToolUse))
		require.NoError(t, err)

		assert.Equal(t, "b96237cb-e49b-4a7c-830f-ad91c5d141cb", got.SessionID)
		assert.Equal(t, "PostToolUse", got.HookEventName)
		assert.Equal(t, "mcp__github__create_pull_request", got.ToolName)
		assert.Equal(t, "MacroPower", got.ToolInput["owner"])
		assert.Nil(t, got.ToolResponse)
		assert.Equal(t, []hook.ContentBlock{{
			Type: "text",
			Text: `{"id":"5205026092","url":"https://github.com/MacroPower/terrarium/pull/44"}`,
		}}, got.ToolContent)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		t.Parallel()

		_, err := hook.ParseInput([]byte(`{"tool_name": "Bash"`))
		require.Error(t, err)
	})
}

// TestParseInputToolResponse checks the forms of tool_response that
// [hook.ParseInput] accepts. No form fails the decode of the fields
// around it.
func TestParseInputToolResponse(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		response     string
		wantResponse map[string]any
		wantContent  []hook.ContentBlock
	}{
		"object": {
			response:     `{"stdout": "ok", "interrupted": false}`,
			wantResponse: map[string]any{"stdout": "ok", "interrupted": false},
		},
		"empty object": {
			response:     `{}`,
			wantResponse: map[string]any{},
		},
		"list of content blocks": {
			response: `[{"type": "text", "text": "one"}, {"type": "image", "data": "aGk=", "mimeType": "image/png"}]`,
			wantContent: []hook.ContentBlock{
				{Type: "text", Text: "one"},
				{Type: "image"},
			},
		},
		"empty list": {
			response:    `[]`,
			wantContent: []hook.ContentBlock{},
		},
		"list of strings": {
			response: `["one", "two"]`,
		},
		"list with a block whose text is a number": {
			response: `[{"type": "text", "text": 7}]`,
		},
		"string": {
			response: `"plain text"`,
		},
		"number": {
			response: `7`,
		},
		"null": {
			response: `null`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			payload := `{"session_id": "s1", "tool_name": "mcp__probe__echo", "tool_response": ` +
				tc.response + `, "cwd": "/work"}`

			got, err := hook.ParseInput([]byte(payload))
			require.NoError(t, err)

			assert.Equal(t, "s1", got.SessionID)
			assert.Equal(t, "mcp__probe__echo", got.ToolName)
			assert.Equal(t, "/work", got.Cwd)
			assert.Equal(t, tc.wantResponse, got.ToolResponse)
			assert.Equal(t, tc.wantContent, got.ToolContent)
		})
	}

	t.Run("absent", func(t *testing.T) {
		t.Parallel()

		got, err := hook.ParseInput([]byte(`{"session_id": "s1"}`))
		require.NoError(t, err)

		assert.Equal(t, "s1", got.SessionID)
		assert.Nil(t, got.ToolResponse)
		assert.Nil(t, got.ToolContent)
	})
}
