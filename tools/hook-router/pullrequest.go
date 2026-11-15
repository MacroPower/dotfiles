package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"strconv"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/hook"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/state"
)

// pullRequestURL matches the web address of a pull request on a GitHub
// host. One such address is
// https://github.com/MacroPower/terrarium/pull/44. The expression
// captures the number.
var pullRequestURL = regexp.MustCompile(`^https://[^/?#]+/[^/?#]+/[^/?#]+/pull/([1-9]\d*)$`)

// handlePostCreatePullRequest records the pull request that a
// create_pull_request call of the GitHub MCP server opened. The record
// lets a conditional rule of [handleMCP] tell a pull request that an
// agent opened from every other one. Agents call GitHub with the token
// of the user, so the author of a pull request does not show whether
// an agent opened it.
//
// "CreatePullRequest" is a routing sentinel. The hook matcher in
// home/claude.nix names the tool whose calls reach this handler. The
// record covers every pull request that the tool opens, including one
// whose call the user approved at a prompt.
//
// Claude Code fires PostToolUse only for a call that succeeded. A call
// whose result is an error fires PostToolUseFailure instead, and that
// event reaches no handler. A call that opened no pull request
// therefore leaves no record, and neither does a result that names no
// pull request.
//
// The handler is observational and takes no [io.Writer], the way
// [handleSubagentStart] does. Every store operation is fail-open. A
// pull request that goes unrecorded costs a prompt on its next edit.
// A returned error puts a hook-error notice in front of Claude for a
// call that already ran.
func handlePostCreatePullRequest(
	ctx context.Context,
	input []byte,
	store *state.Store,
	logger *slog.Logger,
) error {
	h, err := hook.ParseInput(input)
	if err != nil {
		logger.WarnContext(ctx, "unparsable hook input", slog.Any("error", err))
		return nil
	}

	pr, ok := openedPullRequest(h.ToolInput, h.ToolContent)
	if !ok {
		logger.InfoContext(ctx, "no pull request in the tool result", slog.String("tool", h.ToolName))
		return nil
	}

	attrs := []any{
		slog.String("session", h.SessionID),
		slog.String("owner", pr.Owner),
		slog.String("repo", pr.Repo),
		slog.Int("number", pr.Number),
	}

	err = store.RecordPullRequest(ctx, pr, h.SessionID)
	if err != nil {
		logger.ErrorContext(ctx, "recording pull request", append(attrs, slog.Any("error", err))...)
		return nil
	}

	logger.InfoContext(ctx, "recorded pull request", attrs...)

	return nil
}

// openedPullRequest reads the pull request that a create_pull_request
// call opened. The `owner` and `repo` arguments in input name the
// repository, the way they do in a later call that edits the pull
// request. The content blocks of the result supply the number. The
// GitHub MCP server answers with one text block that holds a JSON
// object. The `url` member of that object is the web address of the
// pull request, and the address ends in the number. The function
// reports false when input names no repository and when no block
// holds such an address.
func openedPullRequest(input map[string]any, content []hook.ContentBlock) (state.PullRequest, bool) {
	owner, ok := input["owner"].(string)
	if !ok || owner == "" {
		return state.PullRequest{}, false
	}

	repo, ok := input["repo"].(string)
	if !ok || repo == "" {
		return state.PullRequest{}, false
	}

	for _, block := range content {
		if block.Type != "text" {
			continue
		}

		var result struct {
			URL string `json:"url"`
		}

		err := json.Unmarshal([]byte(block.Text), &result)
		if err != nil {
			continue
		}

		m := pullRequestURL.FindStringSubmatch(result.URL)
		if m == nil {
			continue
		}

		number, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}

		return state.PullRequest{Owner: owner, Repo: repo, Number: number}, true
	}

	return state.PullRequest{}, false
}
