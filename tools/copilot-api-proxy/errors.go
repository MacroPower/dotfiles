package main

import (
	"encoding/json"
	"errors"
	"net/http"

	"go.jacobcolvin.com/dotfiles/tools/copilot-api-proxy/auth"
)

// anthropicError is the Anthropic error envelope. Claude Code's SDK reads
// error.type and error.message.
type anthropicError struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// writeAnthropicError synthesizes an Anthropic-shaped error response. It is
// used for proxy-internal failures; upstream Copilot errors are already
// Anthropic-shaped and are relayed unchanged.
func writeAnthropicError(w http.ResponseWriter, status int, errType, message string) {
	var e anthropicError
	e.Type = "error"
	e.Error.Type = errType
	e.Error.Message = message

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(e)
}

// writeAuthError maps a token-acquisition failure to an Anthropic error.
func writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrNoGitHubToken), errors.Is(err, auth.ErrUnauthorized):
		writeAnthropicError(w, http.StatusUnauthorized, "authentication_error",
			"copilot authentication failed; run `copilot-api-proxy login`")
	default:
		writeAnthropicError(w, http.StatusBadGateway, "api_error", err.Error())
	}
}

// anthropicErrorType maps an upstream HTTP status to an Anthropic error type.
func anthropicErrorType(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusRequestEntityTooLarge:
		return "request_too_large"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return "invalid_request_error"
	default:
		if status >= 500 {
			return "api_error"
		}
		return "invalid_request_error"
	}
}
