package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultClientID = "Iv1.b507a08c87ecfe98"
	defaultScope    = "read:user"

	defaultDeviceCodeURL   = "https://github.com/login/device/code"
	defaultAccessTokenURL  = "https://github.com/login/oauth/access_token"
	defaultCopilotTokenURL = "https://api.github.com/copilot_internal/v2/token"

	// fallbackAPIBase is used only when the token exchange omits endpoints.api
	// and no override is configured. The exchange response is authoritative.
	fallbackAPIBase = "https://api.githubcopilot.com"

	// freshnessWindow is the slack before a token's stated expiry within which
	// it is treated as stale, so a token is never handed out moments before it
	// would expire mid-request.
	freshnessWindow = 60 * time.Second

	// fallbackRefresh is used when the exchange omits refresh_in.
	fallbackRefresh = 25 * time.Minute

	// minRefreshInterval floors the proactive refresh cadence so a short or
	// past-due refresh_in cannot spin the refresh loop.
	minRefreshInterval = 30 * time.Second
)

// Sentinel errors.
var (
	// ErrNoGitHubToken indicates no GitHub OAuth token is available; the login
	// subcommand must be run first.
	ErrNoGitHubToken = errors.New("no github token")

	// ErrUnauthorized indicates the GitHub token was rejected by Copilot's
	// token endpoint (revoked, or the account lacks a Copilot seat).
	ErrUnauthorized = errors.New("github token rejected by copilot")

	// ErrTokenEndpointNotFound indicates the Copilot token-exchange endpoint
	// returned 404. The token host is typically wrong for the account: a
	// Copilot Business/Enterprise seat or a GitHub Enterprise host whose token
	// endpoint is not api.github.com, or an account with no active Copilot seat.
	// It is kept distinct from [ErrUnauthorized] so a misrouted exchange is not
	// mistaken for a rejected credential.
	ErrTokenEndpointNotFound = errors.New("copilot token endpoint not found")
)

// EditorHeaders are the static editor-identification headers Copilot requires
// on every request. The exact values are not load-bearing across a wide range
// of versions, but the set must be present or the edge rejects the request.
type EditorHeaders struct {
	EditorVersion string
	PluginVersion string
	UserAgent     string
	IntegrationID string
	APIVersion    string
}

// DefaultEditorHeaders returns a working set of editor headers.
func DefaultEditorHeaders() EditorHeaders {
	return EditorHeaders{
		EditorVersion: "vscode/1.104.3",
		PluginVersion: "copilot-chat/0.26.7",
		UserAgent:     "GitHubCopilotChat/0.26.7",
		IntegrationID: "vscode-chat",
		APIVersion:    "2025-04-01",
	}
}

// Apply sets the editor headers on h, leaving any field with an empty value
// to its default.
func (e EditorHeaders) Apply(h http.Header) {
	d := DefaultEditorHeaders()
	h.Set("Editor-Version", orDefault(e.EditorVersion, d.EditorVersion))
	h.Set("Editor-Plugin-Version", orDefault(e.PluginVersion, d.PluginVersion))
	h.Set("User-Agent", orDefault(e.UserAgent, d.UserAgent))
	h.Set("Copilot-Integration-Id", orDefault(e.IntegrationID, d.IntegrationID))
	h.Set("X-GitHub-Api-Version", orDefault(e.APIVersion, d.APIVersion))
}

// Endpoints are the GitHub and Copilot auth URLs. Override for GitHub
// Enterprise; the zero value uses the public defaults.
type Endpoints struct {
	DeviceCode   string
	AccessToken  string
	CopilotToken string
}

// DefaultEndpoints returns the public GitHub/Copilot auth URLs.
func DefaultEndpoints() Endpoints {
	return Endpoints{
		DeviceCode:   defaultDeviceCodeURL,
		AccessToken:  defaultAccessTokenURL,
		CopilotToken: defaultCopilotTokenURL,
	}
}

// CopilotToken is a resolved short-lived session token and the plan-specific
// API base URL it is valid against.
type CopilotToken struct {
	Bearer    string
	BaseURL   string
	ExpiresAt time.Time
	RefreshAt time.Time
}

func (t CopilotToken) fresh(now time.Time) bool {
	return t.Bearer != "" && now.Before(t.ExpiresAt.Add(-freshnessWindow))
}

// Option configures a [Manager] or [Login]. The available options are:
//   - [WithGitHubToken]
//   - [WithDataDir]
//   - [WithHTTPClient]
//   - [WithEndpoints]
//   - [WithEditorHeaders]
//   - [WithAPIBaseOverride]
type Option func(*options)

type options struct {
	githubToken     string
	dataDir         string
	client          *http.Client
	clientID        string
	scope           string
	endpoints       Endpoints
	editor          EditorHeaders
	apiBaseOverride string
}

// WithGitHubToken supplies the GitHub OAuth token directly, bypassing the
// token store. It is an [Option].
func WithGitHubToken(tok string) Option { return func(o *options) { o.githubToken = tok } }

// WithDataDir sets the directory used to load and persist the GitHub token. It
// is an [Option].
func WithDataDir(dir string) Option { return func(o *options) { o.dataDir = dir } }

// WithHTTPClient sets the client used for auth requests. It is an [Option].
func WithHTTPClient(c *http.Client) Option { return func(o *options) { o.client = c } }

// WithEndpoints overrides the GitHub/Copilot auth URLs. It is an [Option].
func WithEndpoints(e Endpoints) Option { return func(o *options) { o.endpoints = e } }

// WithEditorHeaders overrides the editor identification headers. It is an
// [Option].
func WithEditorHeaders(e EditorHeaders) Option { return func(o *options) { o.editor = e } }

// WithAPIBaseOverride forces the upstream base URL instead of reading it from
// the token exchange. It is an [Option].
func WithAPIBaseOverride(base string) Option { return func(o *options) { o.apiBaseOverride = base } }

func newOptions(opts ...Option) options {
	o := options{
		client:    &http.Client{Timeout: 30 * time.Second},
		clientID:  defaultClientID,
		scope:     defaultScope,
		endpoints: DefaultEndpoints(),
		editor:    DefaultEditorHeaders(),
	}
	for _, fn := range opts {
		fn(&o)
	}
	if o.client == nil {
		o.client = &http.Client{Timeout: 30 * time.Second}
	}
	return o
}

// Manager owns one GitHub account's credentials and keeps the Copilot session
// token fresh. It is safe for concurrent use.
type Manager struct {
	client          *http.Client
	clientID        string
	endpoints       Endpoints
	editor          EditorHeaders
	apiBaseOverride string
	ghToken         string

	// refreshMu serializes token exchanges so concurrent callers coalesce.
	refreshMu sync.Mutex

	mu      sync.RWMutex
	current CopilotToken
}

// NewManager constructs a Manager. If no token is supplied via
// [WithGitHubToken], it is loaded from the data directory.
func NewManager(opts ...Option) (*Manager, error) {
	o := newOptions(opts...)

	dir := o.dataDir
	if dir == "" {
		d, err := DefaultDataDir()
		if err != nil {
			return nil, err
		}
		dir = d
	}

	tok := o.githubToken
	if tok == "" {
		tok, _ = LoadGitHubToken(dir)
	}

	return &Manager{
		client:          o.client,
		clientID:        o.clientID,
		endpoints:       o.endpoints,
		editor:          o.editor,
		apiBaseOverride: o.apiBaseOverride,
		ghToken:         strings.TrimSpace(tok),
	}, nil
}

// Start performs the initial token exchange and launches the background
// refresh loop, which runs until ctx is cancelled. It returns
// [ErrNoGitHubToken] if no GitHub token is configured.
func (m *Manager) Start(ctx context.Context) error {
	if m.ghToken == "" {
		return ErrNoGitHubToken
	}
	if _, err := m.doRefresh(ctx, ""); err != nil {
		return err
	}
	go m.refreshLoop(ctx)
	return nil
}

// Current returns a valid session token and base URL, refreshing on demand if
// the cached token is within the freshness window.
func (m *Manager) Current(ctx context.Context) (CopilotToken, error) {
	m.mu.RLock()
	cur := m.current
	m.mu.RUnlock()
	if cur.fresh(time.Now()) {
		return cur, nil
	}
	return m.doRefresh(ctx, cur.Bearer)
}

// ForceRefresh mints a new session token after an upstream rejection. If
// another caller already replaced stale, its result is returned without a new
// exchange.
func (m *Manager) ForceRefresh(ctx context.Context, stale string) (CopilotToken, error) {
	return m.doRefresh(ctx, stale)
}

// doRefresh exchanges the GitHub token for a session token under refreshMu. If
// stale is non-empty and the current bearer no longer equals it, another
// caller already refreshed and that token is returned instead.
func (m *Manager) doRefresh(ctx context.Context, stale string) (CopilotToken, error) {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()

	if stale != "" {
		m.mu.RLock()
		cur := m.current
		m.mu.RUnlock()
		// Coalesce only if a peer already minted a different, still-fresh token;
		// otherwise fall through and exchange so a forced refresh always yields a
		// usable token.
		if cur.Bearer != stale && cur.fresh(time.Now()) {
			return cur, nil
		}
	}

	tok, err := m.exchange(ctx)
	if err != nil {
		return CopilotToken{}, err
	}

	m.mu.Lock()
	m.current = tok
	m.mu.Unlock()
	return tok, nil
}

func (m *Manager) refreshLoop(ctx context.Context) {
	for {
		m.mu.RLock()
		refreshAt := m.current.RefreshAt
		m.mu.RUnlock()

		d := time.Until(refreshAt)
		if d < minRefreshInterval {
			d = minRefreshInterval
		}

		timer := time.NewTimer(d)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		if _, err := m.doRefresh(ctx, ""); err != nil {
			log.Printf("copilot-api-proxy: token refresh: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
			}
		}
	}
}

// tokenResponse is the Copilot token-exchange response. expires_at and
// refresh_in may be encoded as numbers or strings, so they decode via
// [jsonInt].
type tokenResponse struct {
	Token     string  `json:"token"`
	ExpiresAt jsonInt `json:"expires_at"`
	RefreshIn jsonInt `json:"refresh_in"`
	Endpoints struct {
		API string `json:"api"`
	} `json:"endpoints"`
}

func (m *Manager) exchange(ctx context.Context) (CopilotToken, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.endpoints.CopilotToken, nil)
	if err != nil {
		return CopilotToken{}, fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Authorization", "token "+m.ghToken)
	req.Header.Set("Accept", "application/json")
	m.editor.Apply(req.Header)

	resp, err := m.client.Do(req)
	if err != nil {
		return CopilotToken{}, fmt.Errorf("exchange token: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return CopilotToken{}, fmt.Errorf("%w: status %d", ErrUnauthorized, resp.StatusCode)
	case resp.StatusCode == http.StatusNotFound:
		return CopilotToken{}, fmt.Errorf("%w: GET %s returned 404; for a Copilot Business/Enterprise or GitHub Enterprise account set COPILOT_TOKEN_URL (or COPILOT_GHE_HOST) to the correct token host, and verify an active Copilot seat", ErrTokenEndpointNotFound, m.endpoints.CopilotToken)
	case resp.StatusCode != http.StatusOK:
		return CopilotToken{}, fmt.Errorf("exchange token: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return CopilotToken{}, fmt.Errorf("decode token response: %w", err)
	}
	if tr.Token == "" {
		return CopilotToken{}, errors.New("token exchange returned empty token")
	}

	now := time.Now()

	refreshIn := time.Duration(tr.RefreshIn) * time.Second
	if refreshIn <= 0 {
		refreshIn = fallbackRefresh
	}
	refreshAt := now.Add(refreshIn - freshnessWindow)
	if !refreshAt.After(now) {
		refreshAt = now.Add(time.Second)
	}

	expiresAt := time.Unix(int64(tr.ExpiresAt), 0)
	if tr.ExpiresAt == 0 || expiresAt.Before(now) {
		expiresAt = now.Add(refreshIn)
	}

	base := m.apiBaseOverride
	if base == "" {
		base = tr.Endpoints.API
	}
	if base == "" {
		base = fallbackAPIBase
	}

	return CopilotToken{
		Bearer:    tr.Token,
		BaseURL:   strings.TrimRight(base, "/"),
		ExpiresAt: expiresAt,
		RefreshAt: refreshAt,
	}, nil
}

// jsonInt decodes a JSON value that may be a number or a numeric string.
type jsonInt int64

func (n *jsonInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*n = 0
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("parse int %q: %w", s, err)
	}
	*n = jsonInt(v)
	return nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
