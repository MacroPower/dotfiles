package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/copilot-api-proxy/auth"
)

func TestScanMessages(t *testing.T) {
	t.Parallel()

	msg := func(role, content string) message {
		return message{Role: role, Content: json.RawMessage(content)}
	}

	tests := map[string]struct {
		msgs          []message
		wantInitiator string
		wantVision    bool
	}{
		"single user string":     {[]message{msg("user", `"hi"`)}, "user", false},
		"assistant present":      {[]message{msg("user", `"hi"`), msg("assistant", `"yo"`)}, "agent", false},
		"tool_result user block": {[]message{msg("user", `[{"type":"tool_result","tool_use_id":"t"}]`)}, "agent", false},
		"image block":            {[]message{msg("user", `[{"type":"image"}]`)}, "user", true},
		"text blocks only":       {[]message{msg("user", `[{"type":"text","text":"x"}]`)}, "user", false},
		"image and tool_result":  {[]message{msg("user", `[{"type":"tool_result"},{"type":"image"}]`)}, "agent", true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			gotInitiator, gotVision := scanMessages(tc.msgs)
			assert.Equal(t, tc.wantInitiator, gotInitiator)
			assert.Equal(t, tc.wantVision, gotVision)
		})
	}
}

func TestPreparePreservesBodyAndRewritesModel(t *testing.T) {
	t.Parallel()

	cfg := Config{Models: map[string]string{
		"opus": "claude-opus-4.8", "sonnet": "claude-sonnet-4.6",
		"haiku": "claude-haiku-4.5", "default": "claude-sonnet-4.6",
	}}
	s := &Server{cfg: cfg}

	in := `{"model":"claude-opus-4-8[1m]","max_tokens":1234567890123,"stream":true,` +
		`"messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":1024}}`

	out, stream, initiator, vision, err := s.prepare([]byte(in))
	require.NoError(t, err)
	assert.True(t, stream)
	assert.Equal(t, "user", initiator)
	assert.False(t, vision)

	var got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(out, &got))
	assert.JSONEq(t, `"claude-opus-4.8"`, string(got["model"]))
	// A large integer must survive re-marshaling without float rounding.
	assert.Equal(t, "1234567890123", string(got["max_tokens"]))
	// thinking is forwarded untouched.
	assert.JSONEq(t, `{"type":"enabled","budget_tokens":1024}`, string(got["thinking"]))
}

func TestPrepareRejectsInvalidBody(t *testing.T) {
	t.Parallel()
	s := &Server{cfg: Config{Models: map[string]string{"default": "x"}}}
	_, _, _, _, err := s.prepare([]byte("not json"))
	require.Error(t, err)
}

// TestPreparePreservesSpecialCharacters guards against JSON HTML-escaping
// silently rewriting <, >, and & in prompt text, which would corrupt prompts
// and break prompt-cache prefixes.
func TestPreparePreservesSpecialCharacters(t *testing.T) {
	t.Parallel()

	s := &Server{cfg: Config{Models: map[string]string{"sonnet": "claude-sonnet-4.6", "default": "claude-sonnet-4.6"}}}

	// Literal <, >, & as a real Anthropic client sends them (not pre-escaped).
	in := []byte(`{"model":"claude-sonnet-4-5","system":"emit <system-reminder> tags and run a && b > c","messages":[{"role":"user","content":"x"}]}`)

	out, _, _, _, err := s.prepare(in)
	require.NoError(t, err)

	// If the encoder HTML-escaped, these literals would instead appear as
	// <system-reminder> and a && b > c.
	assert.Contains(t, string(out), "<system-reminder>")
	assert.Contains(t, string(out), "a && b > c")
}

func TestAuthorized(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		master string
		apiKey string
		authz  string
		want   bool
	}{
		"no master key is open": {"", "", "", true},
		"x-api-key match":       {"sec", "sec", "", true},
		"bearer match":          {"sec", "", "Bearer sec", true},
		"wrong key":             {"sec", "nope", "", false},
		"missing credential":    {"sec", "", "", false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := &Server{cfg: Config{MasterKey: tc.master}}
			r := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			if tc.apiKey != "" {
				r.Header.Set("x-api-key", tc.apiKey)
			}
			if tc.authz != "" {
				r.Header.Set("Authorization", tc.authz)
			}
			assert.Equal(t, tc.want, s.authorized(r))
		})
	}
}

func TestRelayStripsHopByHopAndStreams(t *testing.T) {
	t.Parallel()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": {"text/event-stream"},
			"Connection":   {"keep-alive"},
			"X-Custom":     {"keep-me"},
		},
		Body: io.NopCloser(strings.NewReader("event: ping\ndata: {}\n\n")),
	}

	rec := httptest.NewRecorder()
	relay(rec, resp)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	assert.Equal(t, "keep-me", rec.Header().Get("X-Custom"))
	assert.Empty(t, rec.Header().Get("Connection"))
	assert.Contains(t, rec.Body.String(), "ping")
}

// TestHandleMessagesForwardsAndRetries exercises the full request path: model
// remap, header attachment, an upstream 401 forcing a token refresh, and the
// retry succeeding with the new token.
func TestHandleMessagesForwardsAndRetries(t *testing.T) {
	t.Parallel()

	var upstreamCalls int32
	var gotInitiator, gotAuth, gotModel, gotContentType, gotBeta string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&upstreamCalls, 1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		gotInitiator = r.Header.Get("X-Initiator")
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		gotBeta = r.Header.Get("Anthropic-Beta")
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		gotModel, _ = m["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"type":"message","content":[{"type":"text","text":"ok"}]}`)
	}))
	defer upstream.Close()

	var tokenCalls int32
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&tokenCalls, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      "tid=" + strconv.Itoa(int(n)),
			"expires_at": 9999999999,
			"refresh_in": 100000,
			"endpoints":  map[string]string{"api": upstream.URL},
		})
	}))
	defer tokenSrv.Close()

	mgr, err := auth.NewManager(
		auth.WithGitHubToken("gh"),
		auth.WithEndpoints(auth.Endpoints{CopilotToken: tokenSrv.URL}),
		auth.WithDataDir(t.TempDir()),
	)
	require.NoError(t, err)
	require.NoError(t, mgr.Start(t.Context()))

	cfg := Config{
		Models:            map[string]string{"opus": "claude-opus-4.8", "sonnet": "claude-sonnet-4.6", "haiku": "claude-haiku-4.5", "default": "claude-sonnet-4.6"},
		Editor:            auth.DefaultEditorHeaders(),
		BetaAllowPrefixes: defaultBetaAllowPrefixes,
	}
	srv := NewServer(mgr, cfg)

	body := `{"model":"claude-sonnet-4-5","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	// A denied beta (advisor-tool-) must be stripped; an allowed one kept.
	req.Header.Set("Anthropic-Beta", "advisor-tool-2026-03-01,interleaved-thinking-2025-05-14")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ok")
	assert.Equal(t, int32(2), atomic.LoadInt32(&upstreamCalls), "upstream called twice: 401 then retry")
	assert.Equal(t, int32(2), atomic.LoadInt32(&tokenCalls), "token exchanged at start and force-refreshed on 401")
	assert.Equal(t, "user", gotInitiator)
	assert.Equal(t, "Bearer tid=2", gotAuth, "retry uses the refreshed token")
	assert.Equal(t, "claude-sonnet-4.6", gotModel, "model remapped by tier")
	assert.Equal(t, "application/json", gotContentType)
	assert.Equal(t, "interleaved-thinking-2025-05-14", gotBeta, "denied beta stripped, allowed beta forwarded")
}

func TestCountTokensAndHealth(t *testing.T) {
	t.Parallel()

	srv := &Server{cfg: Config{}}
	mux := srv.Handler()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ok")

	rec = httptest.NewRecorder()
	body := `{"messages":[{"role":"user","content":"hello world"}]}`
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(body)))
	assert.Equal(t, http.StatusOK, rec.Code)
	var ct map[string]int
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &ct))
	assert.Positive(t, ct["input_tokens"])
}
