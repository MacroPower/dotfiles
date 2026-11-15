package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/hook"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/msglint"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/state"
)

// createdPullRequestPayload is the PostToolUse payload that Claude Code
// 2.1.319 sent for a mcp__github__create_pull_request call that
// succeeded, with the session paths shortened. The call ran against a
// stand-in for the GitHub MCP server. The stand-in answered with the
// text that the server returned for a call that opened a pull request.
const createdPullRequestPayload = `{
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

// failedPullRequestPayload is the PostToolUseFailure payload that
// Claude Code 2.1.319 sent for a mcp__github__create_pull_request call
// whose result was an error. The call ran against a stand-in for the
// GitHub MCP server. Claude Code fires no PostToolUse event for such a
// call, and the payload carries no tool_response.
const failedPullRequestPayload = `{
	"session_id": "b9d8689e-7878-4289-9c88-8caff2e7877c",
	"transcript_path": "/home/user/.claude/projects/-work/b9d8689e.jsonl",
	"cwd": "/work",
	"permission_mode": "default",
	"hook_event_name": "PostToolUseFailure",
	"tool_name": "mcp__github__create_pull_request",
	"tool_input": {
		"owner": "MacroPower",
		"repo": "terrarium",
		"title": "feat: persist the proxy CA",
		"head": "proxy-ca-trust",
		"base": "main",
		"draft": true
	},
	"tool_use_id": "toolu_01GFsKdtQBQFjTy7C3WTYE98",
	"error": "failed to create pull request: POST https://api.github.com/repos/MacroPower/terrarium/pulls: 422 Validation Failed [{Resource:PullRequest Field: Code:custom Message:A pull request already exists for MacroPower:proxy-ca-trust.}]",
	"is_interrupt": false,
	"duration_ms": 7,
	"mcp_server": {"name": "github", "source": "dynamic"}
}`

// createdPullRequest returns the PostToolUse payload of a
// mcp__github__create_pull_request call that named the repository
// owner/repo and opened its pull request number, in the session
// sessionID. It has the form of [createdPullRequestPayload].
func createdPullRequest(t *testing.T, sessionID, owner, repo string, number int) []byte {
	t.Helper()

	text, err := json.Marshal(map[string]string{
		"id":  "5205026092",
		"url": fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, repo, number),
	})
	require.NoError(t, err)

	b, err := json.Marshal(map[string]any{
		"session_id":      sessionID,
		"hook_event_name": "PostToolUse",
		"tool_name":       "mcp__github__create_pull_request",
		"tool_input": map[string]any{
			"owner": owner,
			"repo":  repo,
			"title": "feat: persist the proxy CA",
			"head":  "proxy-ca-trust",
			"base":  "main",
			"draft": true,
		},
		"tool_response": []map[string]string{{"type": "text", "text": string(text)}},
	})
	require.NoError(t, err)

	return b
}

// pullRequestRows returns every row of opened_pull_requests as
// "owner/repo#number session".
func pullRequestRows(t *testing.T, store *state.Store) []string {
	t.Helper()

	rows, err := store.DB().QueryContext(t.Context(),
		`SELECT owner, repo, number, session_id FROM opened_pull_requests ORDER BY owner, repo, number`)
	require.NoError(t, err)

	defer func() {
		require.NoError(t, rows.Close())
	}()

	var got []string

	for rows.Next() {
		var (
			owner, repo, session string
			number               int
		)

		require.NoError(t, rows.Scan(&owner, &repo, &number, &session))

		got = append(got, fmt.Sprintf("%s/%s#%d %s", owner, repo, number, session))
	}

	require.NoError(t, rows.Err())

	return got
}

// unreadableRecordStore returns an opener of a state store that opens
// but cannot read the record of pull requests. The database is at the
// current schema version, so [state.Open] runs no migration. It has no
// opened_pull_requests table, so every query of the record fails.
func unreadableRecordStore(t *testing.T) func(context.Context) (*state.Store, error) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "state.db")

	store, err := state.Open(t.Context(), dbPath)
	require.NoError(t, err)

	_, err = store.DB().ExecContext(t.Context(), `DROP TABLE opened_pull_requests`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	return func(ctx context.Context) (*state.Store, error) {
		return state.Open(ctx, dbPath)
	}
}

func TestOpenedPullRequest(t *testing.T) {
	t.Parallel()

	text := func(s string) hook.ContentBlock {
		return hook.ContentBlock{Type: "text", Text: s}
	}

	// widgets is the tool_input of a call that names octo-org/widgets.
	widgets := map[string]any{"owner": "octo-org", "repo": "widgets", "title": "Add a flag"}

	// opened is the result for pull request 12 of octo-org/widgets.
	opened := []hook.ContentBlock{text(`{"id":"1","url":"https://github.com/octo-org/widgets/pull/12"}`)}

	cases := map[string]struct {
		input   map[string]any
		content []hook.ContentBlock
		want    state.PullRequest
		ok      bool
	}{
		"result of the GitHub MCP server": {
			input: map[string]any{"owner": "MacroPower", "repo": "terrarium"},
			content: []hook.ContentBlock{
				text(`{"id":"5205026092","url":"https://github.com/MacroPower/terrarium/pull/44"}`),
			},
			want: state.PullRequest{Owner: "MacroPower", Repo: "terrarium", Number: 44},
			ok:   true,
		},
		"address on another GitHub host": {
			input:   widgets,
			content: []hook.ContentBlock{text(`{"url":"https://github.example.com/octo-org/widgets/pull/7"}`)},
			want:    state.PullRequest{Owner: "octo-org", Repo: "widgets", Number: 7},
			ok:      true,
		},
		"repository name with a dot": {
			input:   map[string]any{"owner": "MacroPower", "repo": "macropower.github.io"},
			content: []hook.ContentBlock{text(`{"url":"https://github.com/MacroPower/macropower.github.io/pull/3"}`)},
			want:    state.PullRequest{Owner: "MacroPower", Repo: "macropower.github.io", Number: 3},
			ok:      true,
		},
		"address in the second block": {
			input:   widgets,
			content: []hook.ContentBlock{text("Opened the pull request."), opened[0]},
			want:    state.PullRequest{Owner: "octo-org", Repo: "widgets", Number: 12},
			ok:      true,
		},
		"call names the repository in another case": {
			input:   map[string]any{"owner": "Octo-Org", "repo": "Widgets"},
			content: opened,
			want:    state.PullRequest{Owner: "Octo-Org", Repo: "Widgets", Number: 12},
			ok:      true,
		},
		"address names the repository by another name": {
			input:   map[string]any{"owner": "octo-org", "repo": "gadgets"},
			content: opened,
			want:    state.PullRequest{Owner: "octo-org", Repo: "gadgets", Number: 12},
			ok:      true,
		},
		"no input": {
			content: opened,
		},
		"no owner": {
			input:   map[string]any{"repo": "widgets"},
			content: opened,
		},
		"empty owner": {
			input:   map[string]any{"owner": "", "repo": "widgets"},
			content: opened,
		},
		"owner that is not a string": {
			input:   map[string]any{"owner": float64(1), "repo": "widgets"},
			content: opened,
		},
		"no repo": {
			input:   map[string]any{"owner": "octo-org"},
			content: opened,
		},
		"empty repo": {
			input:   map[string]any{"owner": "octo-org", "repo": ""},
			content: opened,
		},
		"no content": {
			input: widgets,
		},
		"text that is not JSON": {
			input:   widgets,
			content: []hook.ContentBlock{text("https://github.com/octo-org/widgets/pull/12")},
		},
		"JSON that is not an object": {
			input:   widgets,
			content: []hook.ContentBlock{text(`["https://github.com/octo-org/widgets/pull/12"]`)},
		},
		"object with no url": {
			input:   widgets,
			content: []hook.ContentBlock{text(`{"id":"5205026092"}`)},
		},
		"url that is not a string": {
			input:   widgets,
			content: []hook.ContentBlock{text(`{"url":44}`)},
		},
		"address of an issue": {
			input:   widgets,
			content: []hook.ContentBlock{text(`{"url":"https://github.com/octo-org/widgets/issues/12"}`)},
		},
		"address of a repository": {
			input:   widgets,
			content: []hook.ContentBlock{text(`{"url":"https://github.com/octo-org/widgets"}`)},
		},
		"address with a path after the number": {
			input:   widgets,
			content: []hook.ContentBlock{text(`{"url":"https://github.com/octo-org/widgets/pull/12/files"}`)},
		},
		"address with a query": {
			input:   widgets,
			content: []hook.ContentBlock{text(`{"url":"https://github.com/octo-org/widgets/pull/12?w=1"}`)},
		},
		"address with an extra path segment": {
			input:   widgets,
			content: []hook.ContentBlock{text(`{"url":"https://github.com/a/octo-org/widgets/pull/12"}`)},
		},
		"address of the API": {
			input:   widgets,
			content: []hook.ContentBlock{text(`{"url":"https://api.github.com/repos/octo-org/widgets/pulls/12"}`)},
		},
		"address without https": {
			input:   widgets,
			content: []hook.ContentBlock{text(`{"url":"http://github.com/octo-org/widgets/pull/12"}`)},
		},
		"number zero": {
			input:   widgets,
			content: []hook.ContentBlock{text(`{"url":"https://github.com/octo-org/widgets/pull/0"}`)},
		},
		"number that is not a number": {
			input:   widgets,
			content: []hook.ContentBlock{text(`{"url":"https://github.com/octo-org/widgets/pull/new"}`)},
		},
		"number too large for an int": {
			input: widgets,
			content: []hook.ContentBlock{
				text(`{"url":"https://github.com/octo-org/widgets/pull/99999999999999999999999"}`),
			},
		},
		"block that is not text": {
			input: widgets,
			content: []hook.ContentBlock{
				{Type: "resource", Text: `{"url":"https://github.com/octo-org/widgets/pull/12"}`},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, ok := openedPullRequest(tc.input, tc.content)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestHandlePostCreatePullRequest checks which PostToolUse payloads
// leave a record in the state database.
func TestHandlePostCreatePullRequest(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	cases := map[string]struct {
		input string
		want  []string
	}{
		"call that succeeded": {
			input: createdPullRequestPayload,
			want:  []string{"MacroPower/terrarium#44 b96237cb-e49b-4a7c-830f-ad91c5d141cb"},
		},
		"call that failed": {
			input: failedPullRequestPayload,
		},
		"result with no pull request": {
			input: `{"session_id": "s1", "tool_input": {"owner": "octo-org", "repo": "widgets"},` +
				` "tool_response": [{"type": "text", "text": "{\"id\":\"1\"}"}]}`,
		},
		"result that is an object": {
			input: `{"session_id": "s1", "tool_input": {"owner": "octo-org", "repo": "widgets"},` +
				` "tool_response": {"url": "https://github.com/octo-org/widgets/pull/12"}}`,
		},
		"result that is a string": {
			input: `{"session_id": "s1", "tool_input": {"owner": "octo-org", "repo": "widgets"},` +
				` "tool_response": "https://github.com/octo-org/widgets/pull/12"}`,
		},
		"call that names no repository": {
			input: `{"session_id": "s1", "tool_input": {"title": "Add a flag"}, "tool_response":` +
				` [{"type": "text", "text": "{\"url\":\"https://github.com/octo-org/widgets/pull/12\"}"}]}`,
		},
		"invalid JSON": {
			input: `not json`,
		},
		"empty payload": {
			input: `{}`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := newTestStore(t)

			require.NoError(t, handlePostCreatePullRequest(t.Context(), []byte(tc.input), store, logger))
			assert.Equal(t, tc.want, pullRequestRows(t, store))
		})
	}

	t.Run("second call for the same pull request keeps one row", func(t *testing.T) {
		t.Parallel()

		store := newTestStore(t)

		first := createdPullRequest(t, "s1", "octo-org", "widgets", 12)
		second := createdPullRequest(t, "s2", "Octo-Org", "Widgets", 12)

		require.NoError(t, handlePostCreatePullRequest(t.Context(), first, store, logger))
		require.NoError(t, handlePostCreatePullRequest(t.Context(), second, store, logger))

		assert.Equal(t, []string{"octo-org/widgets#12 s1"}, pullRequestRows(t, store))
	})

	t.Run("store error is swallowed", func(t *testing.T) {
		t.Parallel()

		store, err := state.Open(t.Context(), filepath.Join(t.TempDir(), "closed.db"))
		require.NoError(t, err)
		require.NoError(t, store.Close())

		require.NoError(t, handlePostCreatePullRequest(t.Context(), []byte(createdPullRequestPayload), store, logger))
	})
}

// TestRunPostCreatePullRequest checks that [run] routes the
// CreatePullRequest sentinel to the record, and that the tool name of
// the payload alone does not.
func TestRunPostCreatePullRequest(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	t.Run("sentinel records the pull request", func(t *testing.T) {
		t.Parallel()

		store := newTestStore(t)

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(createdPullRequestPayload), &stdout,
			"PostToolUse", "CreatePullRequest", store, config{}, logger)
		require.NoError(t, err)

		assert.Empty(t, stdout.Bytes())
		assert.Equal(t,
			[]string{"MacroPower/terrarium#44 b96237cb-e49b-4a7c-830f-ad91c5d141cb"},
			pullRequestRows(t, store),
		)
	})

	t.Run("sentinel without a store is a no-op", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(createdPullRequestPayload), &stdout,
			"PostToolUse", "CreatePullRequest", nil, config{}, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("payload without the sentinel records nothing", func(t *testing.T) {
		t.Parallel()

		store := newTestStore(t)

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(createdPullRequestPayload), &stdout,
			"PostToolUse", "", store, config{}, logger)
		require.NoError(t, err)

		assert.Empty(t, stdout.Bytes())
		assert.Empty(t, pullRequestRows(t, store))
	})
}

// TestHandleMCPUpdatePullRequest runs mcp__github__update_pull_request
// and mcp__github__update_pull_request_branch calls through
// [handleMCP] with the rules that home/claude.nix sets for the tools.
// [handlePostCreatePullRequest] writes the record that the rules read,
// from the payload of a create_pull_request call. Each call reads the
// record through its own connection to the database, the way a hook
// process of another session does.
func TestHandleMCPUpdatePullRequest(t *testing.T) {
	t.Parallel()

	const (
		update = "mcp__github__update_pull_request"
		branch = "mcp__github__update_pull_request_branch"
		merge  = "mcp__github__merge_pull_request"

		openSession = "session-that-opened"
		editSession = "session-that-edits"

		recorded = "`pullNumber` is a pull request of `owner`/`repo` that an agent opened from this host"

		updateConditions = "`draft` is absent or true and `reviewers` is absent, false, or empty" +
			" and `base` is absent and `maintainer_can_modify` is absent and " + recorded
	)

	logger := slog.New(slog.DiscardHandler)
	dbPath := filepath.Join(t.TempDir(), "state.db")

	openStore := func(ctx context.Context) (*state.Store, error) {
		return state.Open(ctx, dbPath)
	}

	// An agent opens pull request 44 of MacroPower/terrarium. Pull
	// request 45 of that repository has no record.
	store, err := openStore(t.Context())
	require.NoError(t, err)
	require.NoError(t, handlePostCreatePullRequest(
		t.Context(),
		createdPullRequest(t, openSession, "MacroPower", "terrarium", 44),
		store,
		logger,
	))
	require.NoError(t, store.Close())

	rules, err := parseMCPRules(pullRequestRules, openStore, logger)
	require.NoError(t, err)

	cfg := config{
		mcpRules:  rules,
		openStore: openStore,
		messageLint: msglint.Config{
			Command: []string{
				"sh",
				"-c",
				`grep -q deliberate && { echo '<stdin>:1:1: Style::ProseReassurance: reassurance'; exit 1; }; exit 0`,
			},
			ToolFields: []msglint.ToolField{{Tool: update, Fields: []string{"title", "body"}}},
		},
	}

	// args returns the arguments of a call on the recorded pull
	// request, and sets each entry of fields in them.
	args := func(fields map[string]any) map[string]any {
		input := map[string]any{"owner": "MacroPower", "repo": "terrarium", "pullNumber": 44}

		maps.Copy(input, fields)

		return input
	}

	cases := map[string]struct {
		input   map[string]any
		tool    string
		session string
		want    string
		reason  string
	}{
		"recorded, title: allow": {
			input:  args(map[string]any{"title": "Persist the proxy CA"}),
			want:   "allow",
			reason: "mcp conditional rule (" + updateConditions + ")",
		},
		"recorded, body: allow": {
			input: args(map[string]any{"body": "Reuses the CA in the CA directory."}),
			want:  "allow",
		},
		"recorded, state closed: allow": {
			input: args(map[string]any{"state": "closed"}),
			want:  "allow",
		},
		"recorded, state open: allow": {
			input: args(map[string]any{"state": "open"}),
			want:  "allow",
		},
		"recorded, draft true: allow": {
			input: args(map[string]any{"draft": true}),
			want:  "allow",
		},
		"recorded, every field that runs without a prompt: allow": {
			input: args(map[string]any{
				"title": "Persist the proxy CA",
				"body":  "Reuses the CA in the CA directory.",
				"state": "open",
				"draft": true,
			}),
			want: "allow",
		},
		"recorded, empty reviewers list: allow": {
			input: args(map[string]any{"title": "Persist the proxy CA", "reviewers": []any{}}),
			want:  "allow",
		},
		"recorded, null base: allow": {
			input: args(map[string]any{"title": "Persist the proxy CA", "base": nil}),
			want:  "allow",
		},
		"recorded, title from the session that opened it: allow": {
			input:   args(map[string]any{"title": "Persist the proxy CA"}),
			session: openSession,
			want:    "allow",
		},
		"recorded, owner and repo in another case: allow": {
			input: map[string]any{"owner": "macropower", "repo": "TERRARIUM", "pullNumber": 44, "state": "closed"},
			want:  "allow",
		},
		"recorded, draft false: ask": {
			input:  args(map[string]any{"draft": false}),
			want:   "ask",
			reason: "This call needs confirmation. " + update + " runs without a prompt when " + updateConditions + ".",
		},
		"recorded, draft false with a title: ask": {
			input: args(map[string]any{"title": "Persist the proxy CA", "draft": false}),
			want:  "ask",
		},
		"recorded, reviewers: ask": {
			input: args(map[string]any{"reviewers": []any{"octocat"}}),
			want:  "ask",
		},
		"recorded, team reviewer with a title: ask": {
			input: args(map[string]any{"title": "Persist the proxy CA", "reviewers": []any{"octo-org/platform"}}),
			want:  "ask",
		},
		"recorded, base: ask": {
			input: args(map[string]any{"base": "release/1.x"}),
			want:  "ask",
		},
		"recorded, empty base: ask": {
			input: args(map[string]any{"base": ""}),
			want:  "ask",
		},
		"recorded, maintainer_can_modify true: ask": {
			input: args(map[string]any{"maintainer_can_modify": true}),
			want:  "ask",
		},
		"recorded, maintainer_can_modify false: ask": {
			input: args(map[string]any{"maintainer_can_modify": false}),
			want:  "ask",
		},
		"no record, title: ask": {
			input: map[string]any{
				"owner":      "MacroPower",
				"repo":       "terrarium",
				"pullNumber": 45,
				"title":      "Persist the proxy CA",
			},
			want:   "ask",
			reason: "This call needs confirmation. " + update + " runs without a prompt when " + updateConditions + ".",
		},
		"no record, title from the session that opened another one: ask": {
			input: map[string]any{
				"owner":      "MacroPower",
				"repo":       "terrarium",
				"pullNumber": 45,
				"title":      "Persist the proxy CA",
			},
			session: openSession,
			want:    "ask",
		},
		"same number in another repository: ask": {
			input: map[string]any{"owner": "MacroPower", "repo": "x", "pullNumber": 44, "state": "closed"},
			want:  "ask",
		},
		"same repository name under another owner: ask": {
			input: map[string]any{"owner": "octo-org", "repo": "terrarium", "pullNumber": 44, "state": "closed"},
			want:  "ask",
		},
		"number as a string: ask": {
			input: map[string]any{"owner": "MacroPower", "repo": "terrarium", "pullNumber": "44", "state": "closed"},
			want:  "ask",
		},
		"no number: ask": {
			input: map[string]any{"owner": "MacroPower", "repo": "terrarium", "state": "closed"},
			want:  "ask",
		},
		"no owner: ask": {
			input: map[string]any{"repo": "terrarium", "pullNumber": 44, "state": "closed"},
			want:  "ask",
		},
		"missing tool_input: ask": {
			want: "ask",
		},
		"recorded, Required finding in the body: deny": {
			input:  args(map[string]any{"body": "This is deliberate."}),
			want:   "deny",
			reason: "prose-lint: 1 finding in the `body` field of " + update + ".",
		},
		"recorded, update the branch: allow": {
			tool:   branch,
			input:  args(nil),
			want:   "allow",
			reason: "mcp conditional rule (" + recorded + ")",
		},
		"recorded, update the branch at an expected head: allow": {
			tool:  branch,
			input: args(map[string]any{"expectedHeadSha": "0f2c08f97b2d52ccaa723edc8d1a98b473889cb0"}),
			want:  "allow",
		},
		"recorded, update the branch from the session that opened it: allow": {
			tool:    branch,
			input:   args(nil),
			session: openSession,
			want:    "allow",
		},
		"no record, update the branch: ask": {
			tool:   branch,
			input:  map[string]any{"owner": "MacroPower", "repo": "terrarium", "pullNumber": 45},
			want:   "ask",
			reason: "This call needs confirmation. " + branch + " runs without a prompt when " + recorded + ".",
		},
		"recorded, merge: ask": {
			tool:   merge,
			input:  args(nil),
			want:   "ask",
			reason: "This MCP tool is on the ask list (" + merge + "). Confirm before running.",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tool := tc.tool
			if tool == "" {
				tool = update
			}

			session := tc.session
			if session == "" {
				session = editSession
			}

			payload := map[string]any{"session_id": session, "tool_name": tool}
			if tc.input != nil {
				payload["tool_input"] = tc.input
			}

			input, err := json.Marshal(payload)
			require.NoError(t, err)

			var stdout bytes.Buffer

			require.NoError(t, handleMCP(t.Context(), input, &stdout, cfg, logger))

			hso := preToolDecision(t, &stdout)
			assert.Equal(t, tc.want, hso["permissionDecision"])

			if tc.reason != "" {
				assert.Contains(t, hso["permissionDecisionReason"], tc.reason)
			}
		})
	}

	// decide returns the decision of one call under the ruleset that
	// reads the record through open.
	decide := func(t *testing.T, open func(context.Context) (*state.Store, error), tool string) string {
		t.Helper()

		rules, err := parseMCPRules(pullRequestRules, open, logger)
		require.NoError(t, err)

		var stdout bytes.Buffer

		input := mcpCall(t, tool, args(map[string]any{"state": "closed"}))

		require.NoError(t, handleMCP(t.Context(), input, &stdout, config{mcpRules: rules}, logger))

		decision, ok := preToolDecision(t, &stdout)["permissionDecision"].(string)
		require.True(t, ok)

		return decision
	}

	t.Run("recorded, no store configured: ask", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "ask", decide(t, nil, update))
		assert.Equal(t, "ask", decide(t, nil, branch))
	})

	t.Run("recorded, store that does not open: ask", func(t *testing.T) {
		t.Parallel()

		broken := func(context.Context) (*state.Store, error) {
			return nil, errors.New("disk on fire")
		}

		assert.Equal(t, "ask", decide(t, broken, update))
		assert.Equal(t, "ask", decide(t, broken, branch))
	})

	t.Run("recorded, store whose record does not read: ask", func(t *testing.T) {
		t.Parallel()

		unreadable := unreadableRecordStore(t)

		assert.Equal(t, "ask", decide(t, unreadable, update))
		assert.Equal(t, "ask", decide(t, unreadable, branch))
	})

	t.Run("recorded long ago, after a prune: allow", func(t *testing.T) {
		t.Parallel()

		oldPath := filepath.Join(t.TempDir(), "state.db")

		open := func(ctx context.Context) (*state.Store, error) {
			return state.Open(ctx, oldPath)
		}

		store, err := open(t.Context())
		require.NoError(t, err)

		require.NoError(t, handlePostCreatePullRequest(
			t.Context(),
			createdPullRequest(t, openSession, "MacroPower", "terrarium", 44),
			store,
			logger,
		))

		_, err = store.DB().ExecContext(t.Context(),
			`UPDATE opened_pull_requests SET created_at = datetime('now', '-400 days')`)
		require.NoError(t, err)
		require.NoError(t, store.PruneStale(t.Context()))
		require.NoError(t, store.Close())

		assert.Equal(t, "allow", decide(t, open, update))
		assert.Equal(t, "allow", decide(t, open, branch))
	})
}

// TestRecordedPullRequest checks the answers and the errors of the
// lookup that [recordedPullRequest] returns.
func TestRecordedPullRequest(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	dbPath := filepath.Join(t.TempDir(), "state.db")

	open := func(ctx context.Context) (*state.Store, error) {
		return state.Open(ctx, dbPath)
	}

	store, err := open(t.Context())
	require.NoError(t, err)
	require.NoError(t, store.RecordPullRequest(
		t.Context(),
		state.PullRequest{Owner: "octo-org", Repo: "widgets", Number: 12},
		"s1",
	))
	require.NoError(t, store.Close())

	lookup := recordedPullRequest(open, logger)

	found, err := lookup(t.Context(), "octo-org", "widgets", 12)
	require.NoError(t, err)
	assert.True(t, found)

	found, err = lookup(t.Context(), "Octo-Org", "Widgets", 12)
	require.NoError(t, err)
	assert.True(t, found, "the lookup ignores the case of the owner and the repository")

	found, err = lookup(t.Context(), "octo-org", "widgets", 13)
	require.NoError(t, err)
	assert.False(t, found)

	found, err = recordedPullRequest(nil, logger)(t.Context(), "octo-org", "widgets", 12)
	require.ErrorIs(t, err, errNoStore)
	assert.False(t, found)

	errOpen := errors.New("disk on fire")

	found, err = recordedPullRequest(func(context.Context) (*state.Store, error) {
		return nil, errOpen
	}, logger)(t.Context(), "octo-org", "widgets", 12)
	require.ErrorIs(t, err, errOpen)
	assert.False(t, found)

	found, err = recordedPullRequest(unreadableRecordStore(t), logger)(t.Context(), "octo-org", "widgets", 12)
	require.ErrorContains(t, err, "reading the pull request record")
	assert.False(t, found)
}
