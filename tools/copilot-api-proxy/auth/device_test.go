package auth_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/copilot-api-proxy/auth"
)

func TestLogin(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/device/code", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		assert.NotEmpty(t, r.PostForm.Get("client_id"))
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "dc",
			"user_code":        "ABCD-1234",
			"verification_uri": "https://github.com/login/device",
			"expires_in":       900,
			"interval":         0,
		})
	})

	var polls int32
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "dc", r.PostForm.Get("device_code"))
		if atomic.AddInt32(&polls, 1) < 2 {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "authorization_pending"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "ghu_token"})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	var out bytes.Buffer
	tok, err := auth.Login(t.Context(), &out,
		auth.WithEndpoints(auth.Endpoints{
			DeviceCode:  srv.URL + "/device/code",
			AccessToken: srv.URL + "/oauth/token",
		}),
	)
	require.NoError(t, err)
	assert.Equal(t, "ghu_token", tok)
	assert.Contains(t, out.String(), "ABCD-1234")
}
