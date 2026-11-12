package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultServiceURL = "https://destiny-vault-manager.penumbraos.workers.dev"
	defaultUserAgent  = "MCP-Destiny/" + version
	defaultTimeout    = 60 * time.Second

	// maxResponseBytes caps a JSON API response body.
	maxResponseBytes = 32 << 20

	// maxArtifactBytes caps one definition artifact.
	maxArtifactBytes = 128 << 20

	// grantRefusal opens the message that the service returns when the
	// Bungie token endpoint answers with a 4xx status.
	grantRefusal = "Token endpoint returned 4"
)

var (
	// ErrService marks failures that originate from Agg's Destiny Vault Manager,
	// which are non-2xx responses and bodies that do not decode.
	ErrService = errors.New("service")

	// ErrUnauthorized reports that Agg's Destiny Vault Manager or Bungie
	// rejected the presented token. Callers refresh the tokens and retry.
	ErrUnauthorized = errors.New("unauthorized")

	// ErrGrantRejected reports that Bungie refused an authorization code or
	// a refresh token. The user signs in again to recover.
	ErrGrantRejected = errors.New("grant rejected")
)

// ArtifactIndex lists the definition artifacts the service publishes.
// Files maps an artifact name to its chunk file names.
type ArtifactIndex struct {
	Files           map[string][]string `json:"files"`
	ManifestVersion string              `json:"manifestVersion"`
}

// ServiceConfig is the response body of /api/config. The Bungie API key and
// OAuth client id are public values that the web client also reads.
type ServiceConfig struct {
	APIKey     string        `json:"apiKey"`
	ClientID   string        `json:"clientId"`
	AuthOrigin string        `json:"authOrigin"`
	Artifacts  ArtifactIndex `json:"artifacts"`
}

// TokenResponse is the response body of /api/auth/token and
// /api/auth/refresh. Session authenticates calls to the service itself, and
// AccessToken authenticates calls to Bungie.
type TokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	MembershipID     string `json:"membership_id"`
	Session          string `json:"session"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshExpiresIn int64  `json:"refresh_expires_in"`
}

// ItemTag ties one tag to one item instance. The service keeps removed rows,
// so Removed marks a tag that an item used to carry.
type ItemTag struct {
	InstanceID string `json:"instanceId"`
	TagID      string `json:"tagId"`
	RuleID     string `json:"ruleId,omitempty"`
	UpdatedAt  int64  `json:"updatedAt,omitzero"`
	ItemHash   uint32 `json:"itemHash"`
	Removed    bool   `json:"removed"`
}

// Note is free text attached to an item instance, a Collections entry, or a
// Collections armor set. ItemHash is set only for the instance kind.
type Note struct {
	ItemHash  *uint32 `json:"itemHash,omitempty"`
	Kind      string  `json:"kind"`
	TargetID  string  `json:"targetId"`
	Body      string  `json:"body"`
	UpdatedAt int64   `json:"updatedAt,omitzero"`
	Removed   bool    `json:"removed"`
}

// PerkPreference records a liked or disliked perk for one game mode.
type PerkPreference struct {
	Mode      string `json:"mode"`
	Perk      string `json:"perk"`
	Stance    string `json:"stance"`
	UpdatedAt int64  `json:"updatedAt,omitzero"`
	Removed   bool   `json:"removed"`
}

// AutotagRule tags, vaults, or tags and vaults newly acquired items that
// match Query.
type AutotagRule struct {
	ID        string `json:"id"`
	Query     string `json:"query"`
	TagID     string `json:"tagId,omitempty"`
	UpdatedAt int64  `json:"updatedAt,omitzero"`
	Position  int    `json:"position"`
	Vault     bool   `json:"vault"`
	Enabled   bool   `json:"enabled"`
	Removed   bool   `json:"removed"`
}

// AutotagState reports whether the web client runs autotag rules. Mark is
// the highest instance id that the rules have already handled.
type AutotagState struct {
	Mark      string `json:"mark"`
	UpdatedAt int64  `json:"updatedAt"`
	Enabled   bool   `json:"enabled"`
}

// AutotagSnapshot is the response body of /api/autotag.
type AutotagSnapshot struct {
	State *AutotagState `json:"state"`
	Rules []AutotagRule `json:"rules"`
}

// ServiceOption configures a [*Service]. See [WithServiceURL],
// [WithServiceHTTPClient], and [WithServiceUserAgent].
type ServiceOption func(*Service)

// WithServiceURL overrides the base URL of Agg's Destiny Vault Manager. It
// is a [ServiceOption].
func WithServiceURL(u string) ServiceOption {
	return func(s *Service) { s.baseURL = strings.TrimRight(u, "/") }
}

// WithServiceHTTPClient overrides the underlying [*http.Client]. It is a
// [ServiceOption].
func WithServiceHTTPClient(hc *http.Client) ServiceOption {
	return func(s *Service) { s.http = hc }
}

// WithServiceUserAgent overrides the User-Agent header. It is a
// [ServiceOption].
func WithServiceUserAgent(ua string) ServiceOption {
	return func(s *Service) { s.userAgent = ua }
}

// Service is the HTTP client for Agg's Destiny Vault Manager. The deployment
// stores tags, notes, perk preferences, and autotag rules per Bungie account,
// brokers the Bungie OAuth exchange, and serves the definition artifacts.
type Service struct {
	http      *http.Client
	baseURL   string
	userAgent string
}

// NewService returns a [*Service] configured with the given options.
func NewService(opts ...ServiceOption) *Service {
	s := &Service{
		http:      &http.Client{Timeout: defaultTimeout},
		baseURL:   defaultServiceURL,
		userAgent: defaultUserAgent,
	}
	for _, opt := range opts {
		opt(s)
	}

	return s
}

// BaseURL returns the base URL of the deployment.
func (s *Service) BaseURL() string {
	return s.baseURL
}

// Config calls /api/config.
func (s *Service) Config(ctx context.Context) (ServiceConfig, error) {
	var out ServiceConfig

	err := s.call(ctx, http.MethodGet, "/api/config", "", nil, &out)
	if err != nil {
		return ServiceConfig{}, err
	}

	if out.APIKey == "" || out.ClientID == "" {
		return ServiceConfig{}, fmt.Errorf("%w: /api/config carries no Bungie credentials", ErrService)
	}

	return out, nil
}

// ExchangeCode trades a Bungie OAuth authorization code for tokens.
func (s *Service) ExchangeCode(ctx context.Context, code string) (TokenResponse, error) {
	return s.tokens(ctx, "/api/auth/token", map[string]string{"code": code})
}

// RefreshTokens trades a Bungie refresh token for a fresh set of tokens.
func (s *Service) RefreshTokens(ctx context.Context, refreshToken string) (TokenResponse, error) {
	return s.tokens(ctx, "/api/auth/refresh", map[string]string{"refreshToken": refreshToken})
}

func (s *Service) tokens(ctx context.Context, path string, body any) (TokenResponse, error) {
	var out TokenResponse

	err := s.call(ctx, http.MethodPost, path, "", body, &out)
	if errors.Is(err, ErrService) && strings.Contains(err.Error(), grantRefusal) {
		return TokenResponse{}, fmt.Errorf("%w: %w", ErrGrantRejected, err)
	}

	if err != nil {
		return TokenResponse{}, err
	}

	if out.AccessToken == "" {
		return TokenResponse{}, fmt.Errorf("%w: %s returned no access token", ErrService, path)
	}

	return out, nil
}

// Artifact opens one definition artifact file. The caller closes the
// returned reader.
func (s *Service) Artifact(ctx context.Context, file string) (io.ReadCloser, error) {
	u := s.baseURL + "/artifacts/" + url.PathEscape(file)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	req.Header.Set("User-Agent", s.userAgent)

	//nolint:bodyclose // The caller closes the returned reader.
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("performing request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		closeBody(ctx, resp.Body, u)

		return nil, fmt.Errorf("%w: %s returned status %d", ErrService, u, resp.StatusCode)
	}

	return &limitedBody{Reader: io.LimitReader(resp.Body, maxArtifactBytes), body: resp.Body}, nil
}

// Tags returns every item tag row of the signed-in account, removed rows
// included.
func (s *Service) Tags(ctx context.Context, session string) ([]ItemTag, error) {
	var out struct {
		Items []ItemTag `json:"items"`
	}

	err := s.call(ctx, http.MethodGet, "/api/tags?since=0", session, nil, &out)
	if err != nil {
		return nil, err
	}

	return out.Items, nil
}

// WriteTags upserts item tag rows.
func (s *Service) WriteTags(ctx context.Context, session string, changes []ItemTag) error {
	body := map[string]any{"defs": []any{}, "items": changes}

	return s.call(ctx, http.MethodPost, "/api/tags", session, body, nil)
}

// Notes returns every note of the signed-in account, removed notes included.
func (s *Service) Notes(ctx context.Context, session string) ([]Note, error) {
	var out struct {
		Notes []Note `json:"notes"`
	}

	err := s.call(ctx, http.MethodGet, "/api/notes?since=0", session, nil, &out)
	if err != nil {
		return nil, err
	}

	return out.Notes, nil
}

// WriteNote upserts one note. An empty Body removes the note.
func (s *Service) WriteNote(ctx context.Context, session string, change Note) error {
	return s.call(ctx, http.MethodPost, "/api/notes", session, change, nil)
}

// PerkPreferences returns every perk preference of the signed-in account,
// removed preferences included.
func (s *Service) PerkPreferences(ctx context.Context, session string) ([]PerkPreference, error) {
	var out struct {
		Preferences []PerkPreference `json:"preferences"`
	}

	err := s.call(ctx, http.MethodGet, "/api/perk-preferences?since=0", session, nil, &out)
	if err != nil {
		return nil, err
	}

	return out.Preferences, nil
}

// WritePerkPreferences upserts perk preferences.
func (s *Service) WritePerkPreferences(ctx context.Context, session string, changes []PerkPreference) error {
	body := map[string]any{"preferences": changes}

	return s.call(ctx, http.MethodPost, "/api/perk-preferences", session, body, nil)
}

// Autotag returns every autotag rule of the signed-in account, removed rules
// included, and the autotag state.
func (s *Service) Autotag(ctx context.Context, session string) (AutotagSnapshot, error) {
	var out AutotagSnapshot

	err := s.call(ctx, http.MethodGet, "/api/autotag?since=0", session, nil, &out)
	if err != nil {
		return AutotagSnapshot{}, err
	}

	return out, nil
}

// WriteAutotagRules upserts autotag rules.
func (s *Service) WriteAutotagRules(ctx context.Context, session string, changes []AutotagRule) error {
	body := map[string]any{"rules": changes}

	return s.call(ctx, http.MethodPost, "/api/autotag", session, body, nil)
}

// SetAutotagEnabled turns autotagging on or off and reports whether the
// service applied the change. Turning it on stores mark as the highest
// instance id that the rules skip, unless autotagging is already on.
func (s *Service) SetAutotagEnabled(ctx context.Context, session string, enabled bool, mark string) (bool, error) {
	body := map[string]any{"kind": "disable"}
	if enabled {
		body = map[string]any{"kind": "enable", "mark": mark}
	}

	var out struct {
		Applied bool `json:"applied"`
	}

	err := s.call(ctx, http.MethodPost, "/api/autotag/state", session, body, &out)
	if err != nil {
		return false, err
	}

	return out.Applied, nil
}

// call performs one JSON request. It sends a non-empty session as a bearer
// token. A nil dst discards the response body.
func (s *Service) call(ctx context.Context, method, path, session string, body, dst any) error {
	u := s.baseURL + path

	var reader io.Reader = http.NoBody

	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}

		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}

	req.Header.Set("User-Agent", s.userAgent)
	req.Header.Set("Accept", "application/json")

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if session != "" {
		req.Header.Set("Authorization", "Bearer "+session)
	}

	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("performing request: %w", err)
	}

	defer closeBody(ctx, resp.Body, u)

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("reading %s: %w", u, err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%w: %s %s: %s", ErrUnauthorized, method, path, serviceMessage(raw))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf(
			"%w: %s %s returned status %d: %s",
			ErrService, method, path, resp.StatusCode, serviceMessage(raw),
		)
	}

	if dst == nil {
		return nil
	}

	err = json.Unmarshal(raw, dst)
	if err != nil {
		return fmt.Errorf("%w: decoding %s: %w", ErrService, path, err)
	}

	return nil
}

// serviceMessage extracts the "error" field of a service error body and
// falls back to the start of the raw body.
func serviceMessage(raw []byte) string {
	var body struct {
		Error string `json:"error"`
	}

	err := json.Unmarshal(raw, &body)
	if err == nil && body.Error != "" {
		return body.Error
	}

	const limit = 200

	text := strings.TrimSpace(string(raw))
	if len(text) > limit {
		text = text[:limit]
	}

	return text
}

// limitedBody reads through a size cap and closes the underlying body.
type limitedBody struct {
	io.Reader

	body io.Closer
}

// Close closes the underlying response body.
func (b *limitedBody) Close() error {
	err := b.body.Close()
	if err != nil {
		return fmt.Errorf("closing response body: %w", err)
	}

	return nil
}

func closeBody(ctx context.Context, body io.Closer, u string) {
	err := body.Close()
	if err != nil {
		slog.DebugContext(ctx, "closing response body",
			slog.String("url", u),
			slog.Any("error", err),
		)
	}
}
