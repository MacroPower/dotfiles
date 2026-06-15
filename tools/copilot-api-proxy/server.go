package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.jacobcolvin.com/dotfiles/tools/copilot-api-proxy/auth"
)

const maxBodyBytes = 10 << 20 // 10 MiB

// Server serves the Anthropic Messages API from a Copilot subscription.
type Server struct {
	mgr    *auth.Manager
	cfg    Config
	client *http.Client
	log    *slog.Logger
}

// NewServer constructs a Server. The upstream client has no timeout so it can
// hold streaming responses open for the lifetime of a request. A nil logger
// discards output.
func NewServer(mgr *auth.Manager, cfg Config, logger *slog.Logger) *Server {
	return &Server{mgr: mgr, cfg: cfg, client: &http.Client{}, log: logger}
}

// discardLogger backs [Server.logger] when no logger is set, so a directly
// constructed zero-value Server (as in tests) logs nothing rather than
// panicking on a nil *slog.Logger.
var discardLogger = slog.New(slog.DiscardHandler)

// logger returns the server's logger, or a discarding logger when unset.
func (s *Server) logger() *slog.Logger {
	if s.log == nil {
		return discardLogger
	}
	return s.log
}

// Handler returns the proxy's HTTP routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/messages", s.handleMessages)
	mux.HandleFunc("POST /v1/messages/count_tokens", s.handleCountTokens)
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("/", s.handleCatchAll)
	return mux
}

// message is the subset of an Anthropic message needed to choose routing
// headers.
type message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type contentBlock struct {
	Type string `json:"type"`
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		s.logger().Warn("rejected unauthorized request", "path", r.URL.Path, "remote", r.RemoteAddr)
		writeAnthropicError(w, http.StatusUnauthorized, "authentication_error", "missing or invalid api key")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeReadError(w, err)
		return
	}

	prepared, stream, initiator, vision, err := s.prepare(body)
	if err != nil {
		s.logger().Warn("rejected invalid request body", "error", err, "bytes", len(body))
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	s.forward(w, r, prepared, stream, initiator, vision)
}

// prepare rewrites the requested model to the configured Copilot model by tier
// and derives the routing flags (stream, initiator, vision) from the body.
func (s *Server) prepare(body []byte) (out []byte, stream bool, initiator string, vision bool, err error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, false, "", false, fmt.Errorf("parse request body: %w", err)
	}

	var requested string
	if m, ok := raw["model"]; ok {
		_ = json.Unmarshal(m, &requested)
	}
	if s2, ok := raw["stream"]; ok {
		_ = json.Unmarshal(s2, &stream)
	}

	var msgs []message
	if m, ok := raw["messages"]; ok {
		_ = json.Unmarshal(m, &msgs)
	}
	initiator, vision = scanMessages(msgs)

	mappedModel := s.cfg.ModelFor(requested)
	mapped, err := json.Marshal(mappedModel)
	if err != nil {
		return nil, false, "", false, fmt.Errorf("encode model: %w", err)
	}
	raw["model"] = mapped
	s.logger().Debug("mapped model", "requested", requested, "mapped", mappedModel)

	// Re-encode with HTML escaping disabled so prompt text containing <, >, or
	// & (XML tags like <system-reminder>, shell &&, comparison operators) is
	// preserved byte-for-byte, keeping prompt-cache prefixes intact.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(raw); err != nil {
		return nil, false, "", false, fmt.Errorf("encode request body: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), stream, initiator, vision, nil
}

// scanMessages derives the X-Initiator value and whether the request includes
// an image. The initiator is "agent" for any multi-turn or tool-bearing
// conversation (which does not consume a premium request) and "user"
// otherwise.
func scanMessages(msgs []message) (initiator string, vision bool) {
	initiator = "user"
	for _, m := range msgs {
		// Anthropic carries tool results as tool_result blocks inside user
		// messages; the "tool" role check is defensive for non-Anthropic clients.
		if m.Role == "assistant" || m.Role == "tool" {
			initiator = "agent"
		}
		for _, b := range contentBlocks(m.Content) {
			switch b.Type {
			case "tool_result":
				initiator = "agent"
			case "image":
				vision = true
			}
		}
	}
	return initiator, vision
}

func contentBlocks(raw json.RawMessage) []contentBlock {
	if len(raw) == 0 {
		return nil
	}
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil // string content carries no blocks
	}
	return blocks
}

func (s *Server) forward(w http.ResponseWriter, r *http.Request, body []byte, stream bool, initiator string, vision bool) {
	start := time.Now()

	// A single x-request-id is reused across the 401 retry so the upstream
	// sees one logical request; it also correlates this proxy's log lines.
	reqID := newRequestID()

	tok, err := s.mgr.Current(r.Context())
	if err != nil {
		s.logger().Warn("token acquisition before request", "request_id", reqID, "error", err)
		writeAuthError(w, err)
		return
	}

	resp, err := s.do(r.Context(), r, tok, body, stream, initiator, vision, reqID)
	if err != nil {
		s.logger().Warn("upstream request error", "request_id", reqID, "error", err)
		writeAnthropicError(w, http.StatusBadGateway, "api_error", err.Error())
		return
	}

	retried := false
	if resp.StatusCode == http.StatusUnauthorized {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		s.logger().Debug("refreshing session token after upstream 401", "request_id", reqID)
		retried = true
		tok, err = s.mgr.ForceRefresh(r.Context(), tok.Bearer)
		if err != nil {
			s.logger().Warn("token refresh after 401", "request_id", reqID, "error", err)
			writeAuthError(w, err)
			return
		}
		resp, err = s.do(r.Context(), r, tok, body, stream, initiator, vision, reqID)
		if err != nil {
			s.logger().Warn("upstream request error after refresh", "request_id", reqID, "error", err)
			writeAnthropicError(w, http.StatusBadGateway, "api_error", err.Error())
			return
		}
	}
	defer resp.Body.Close()

	status := resp.StatusCode
	relay(w, resp)

	// Logged after relay so duration_ms covers streaming to the client. A
	// non-2xx upstream status is the proxy's most actionable signal, so it is
	// logged at warn; successful requests stay at info for a readable trace.
	level := slog.LevelInfo
	if status >= http.StatusBadRequest {
		level = slog.LevelWarn
	}
	s.logger().Log(r.Context(), level, "request",
		"request_id", reqID,
		"status", status,
		"initiator", initiator,
		"vision", vision,
		"stream", stream,
		"retried", retried,
		"duration_ms", time.Since(start).Milliseconds())
}

func (s *Server) do(ctx context.Context, r *http.Request, tok auth.CopilotToken, body []byte, stream bool, initiator string, vision bool, reqID string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tok.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+tok.Bearer)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Openai-Intent", "conversation-panel")
	req.Header.Set("X-Initiator", initiator)
	req.Header.Set("X-Request-Id", reqID)
	if vision {
		req.Header.Set("Copilot-Vision-Request", "true")
	}
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	s.cfg.Editor.Apply(req.Header)

	// Forward the client's Anthropic protocol version unchanged, but filter the
	// beta opt-ins: Copilot's endpoint 400s on any beta it does not recognize,
	// and Claude Code routinely sends betas (advisor-tool-*, context-1m-*, ...)
	// that Copilot has not allowlisted. Only betas matching the configured allow
	// prefixes (and no deny prefix) survive; the rest are stripped.
	req.Header.Set("Anthropic-Version", headerOr(r, "Anthropic-Version", "2023-06-01"))
	betas := filterBetas(r.Header.Values("Anthropic-Beta"), s.cfg.BetaAllowPrefixes)
	if len(betas) > 0 {
		req.Header.Set("Anthropic-Beta", strings.Join(betas, ","))
	}

	s.logger().Debug("forwarding to upstream",
		"request_id", reqID,
		"url", req.URL.String(),
		"bytes", len(body),
		"betas_forwarded", betas)

	return s.client.Do(req)
}

func (s *Server) handleCountTokens(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		s.logger().Warn("rejected unauthorized request", "path", r.URL.Path, "remote", r.RemoteAddr)
		writeAnthropicError(w, http.StatusUnauthorized, "authentication_error", "missing or invalid api key")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeReadError(w, err)
		return
	}

	// A rough local estimate; Claude Code uses this only for context-window
	// math and tolerates approximation.
	estimate := len(body) / 4
	if estimate < 1 {
		estimate = 1
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"input_tokens": estimate})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleCatchAll answers Claude Code's assorted startup probes with a benign
// 200 so warmup never hard-fails.
func (s *Server) handleCatchAll(w http.ResponseWriter, r *http.Request) {
	s.logger().Debug("catch-all probe", "method", r.Method, "path", r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (s *Server) authorized(r *http.Request) bool {
	if s.cfg.MasterKey == "" {
		return true
	}
	if secretEqual(r.Header.Get("x-api-key"), s.cfg.MasterKey) {
		return true
	}
	const prefix = "Bearer "
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, prefix) && secretEqual(h[len(prefix):], s.cfg.MasterKey) {
		return true
	}
	return false
}

// secretEqual compares two secrets in constant time.
func secretEqual(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// writeReadError reports a body-read failure, distinguishing the size cap from
// other read errors such as a client disconnect.
func writeReadError(w http.ResponseWriter, err error) {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		writeAnthropicError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body too large")
		return
	}
	writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "could not read request body")
}

// hopByHop headers must not be forwarded when relaying the upstream response.
var hopByHop = map[string]bool{
	"connection":          true,
	"keep-alive":          true,
	"proxy-authenticate":  true,
	"proxy-authorization": true,
	"te":                  true,
	"trailer":             true,
	"transfer-encoding":   true,
	"upgrade":             true,
	"content-length":      true,
}

// relay copies the upstream response to the client, flushing as data arrives
// so SSE events reach the client immediately. Upstream errors are relayed with
// their status and body unchanged: Copilot's native endpoint already returns
// Anthropic-shaped error bodies.
func relay(w http.ResponseWriter, resp *http.Response) {
	skip := connectionTokens(resp.Header)
	for k, vv := range resp.Header {
		lk := strings.ToLower(k)
		if hopByHop[lk] || skip[lk] {
			continue
		}
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 16*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			return
		}
	}
}

// connectionTokens returns the lowercased header names listed in the response's
// Connection header, which RFC 7230 marks hop-by-hop.
func connectionTokens(h http.Header) map[string]bool {
	tokens := map[string]bool{}
	for _, v := range h.Values("Connection") {
		for _, t := range strings.Split(v, ",") {
			if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
				tokens[t] = true
			}
		}
	}
	return tokens
}

func headerOr(r *http.Request, key, def string) string {
	if v := r.Header.Get(key); v != "" {
		return v
	}
	return def
}
