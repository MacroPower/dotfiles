package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/copilot-api-proxy/auth"
)

func TestManagerStartAndCurrent(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		status     int
		body       map[string]any
		wantErr    error
		wantBearer string
		wantBase   string
	}{
		"success with numeric expiry": {
			status:     http.StatusOK,
			body:       map[string]any{"token": "tid=a", "expires_at": 9999999999, "refresh_in": 1500, "endpoints": map[string]string{"api": "https://api.example.test"}},
			wantBearer: "tid=a",
			wantBase:   "https://api.example.test",
		},
		"success with string expiry and trailing slash": {
			status:     http.StatusOK,
			body:       map[string]any{"token": "tid=b", "expires_at": "9999999999", "refresh_in": "1500", "endpoints": map[string]string{"api": "https://api.example.test/"}},
			wantBearer: "tid=b",
			wantBase:   "https://api.example.test",
		},
		"unauthorized": {
			status:  http.StatusUnauthorized,
			body:    map[string]any{},
			wantErr: auth.ErrUnauthorized,
		},
		"not found maps to unauthorized": {
			status:  http.StatusNotFound,
			body:    map[string]any{},
			wantErr: auth.ErrUnauthorized,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "token gh", r.Header.Get("Authorization"))
				assert.NotEmpty(t, r.Header.Get("Copilot-Integration-Id"))
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(tc.body)
			}))
			defer srv.Close()

			m, err := auth.NewManager(
				auth.WithGitHubToken("gh"),
				auth.WithEndpoints(auth.Endpoints{CopilotToken: srv.URL}),
				auth.WithDataDir(t.TempDir()),
			)
			require.NoError(t, err)

			err = m.Start(t.Context())
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)

			tok, err := m.Current(t.Context())
			require.NoError(t, err)
			assert.Equal(t, tc.wantBearer, tok.Bearer)
			assert.Equal(t, tc.wantBase, tok.BaseURL)
		})
	}
}

func TestManagerStartWithoutToken(t *testing.T) {
	t.Parallel()

	m, err := auth.NewManager(auth.WithDataDir(t.TempDir()))
	require.NoError(t, err)
	require.ErrorIs(t, m.Start(t.Context()), auth.ErrNoGitHubToken)
}

func TestForceRefreshCoalesces(t *testing.T) {
	t.Parallel()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      "tid=" + strconv.Itoa(int(n)),
			"expires_at": 9999999999,
			"refresh_in": 100000,
			"endpoints":  map[string]string{"api": "https://api.example.test"},
		})
	}))
	defer srv.Close()

	m, err := auth.NewManager(
		auth.WithGitHubToken("gh"),
		auth.WithEndpoints(auth.Endpoints{CopilotToken: srv.URL}),
		auth.WithDataDir(t.TempDir()),
	)
	require.NoError(t, err)
	require.NoError(t, m.Start(t.Context())) // first exchange

	first, err := m.Current(t.Context())
	require.NoError(t, err)

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.ForceRefresh(t.Context(), first.Bearer)
		}()
	}
	wg.Wait()

	// One exchange at Start; the 20-way concurrent herd produces exactly one
	// more because the stale-bearer compare short-circuits the rest.
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
}
