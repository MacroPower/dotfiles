package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnthropicErrorType(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		status int
		want   string
	}{
		"401":    {http.StatusUnauthorized, "authentication_error"},
		"403":    {http.StatusForbidden, "permission_error"},
		"404":    {http.StatusNotFound, "not_found_error"},
		"413":    {http.StatusRequestEntityTooLarge, "request_too_large"},
		"429":    {http.StatusTooManyRequests, "rate_limit_error"},
		"400":    {http.StatusBadRequest, "invalid_request_error"},
		"422":    {http.StatusUnprocessableEntity, "invalid_request_error"},
		"500":    {http.StatusInternalServerError, "api_error"},
		"teapot": {http.StatusTeapot, "invalid_request_error"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, anthropicErrorType(tc.status))
		})
	}
}

func TestWriteAnthropicError(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	writeAnthropicError(rec, http.StatusTooManyRequests, "rate_limit_error", "slow down")

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var got anthropicError
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "error", got.Type)
	assert.Equal(t, "rate_limit_error", got.Error.Type)
	assert.Equal(t, "slow down", got.Error.Message)
}
