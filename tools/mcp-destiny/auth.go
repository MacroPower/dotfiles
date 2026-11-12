package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	authorizeURL = "https://www.bungie.net/en/OAuth/Authorize"

	// tokenSkew is how long before its expiry [*Auth.Tokens] replaces an
	// access token.
	tokenSkew = time.Minute

	stateBytes = 16
)

var (
	// ErrNotSignedIn reports that the token file is missing or that its
	// refresh token has expired. The user signs in again to recover.
	ErrNotSignedIn = errors.New("not signed in")

	// ErrLogin reports a pasted redirect URL that [*Auth.CompleteLogin]
	// cannot use.
	ErrLogin = errors.New("login")
)

// Tokens is the content of the token file. AccessToken authenticates Bungie
// platform requests, and Session authenticates requests to Agg's Destiny
// Vault Manager.
type Tokens struct {
	AccessExpires  time.Time  `json:"accessExpires"`
	RefreshExpires time.Time  `json:"refreshExpires,omitzero"`
	AccessToken    string     `json:"accessToken"`
	RefreshToken   string     `json:"refreshToken,omitempty"`
	Session        string     `json:"session,omitempty"`
	BungieID       string     `json:"bungieMembershipId"`
	Membership     Membership `json:"membership,omitzero"`
}

// TokenStore reads and writes the token file. Every server process and the
// login subcommand share one file. [*Auth] therefore holds an advisory lock
// on a sibling lock file when it replaces the tokens.
type TokenStore struct {
	path string
}

// NewTokenStore returns a [*TokenStore] for the file at path.
func NewTokenStore(path string) *TokenStore {
	return &TokenStore{path: path}
}

// Path returns the location of the token file.
func (s *TokenStore) Path() string {
	return s.path
}

// Load reads the token file. It returns [ErrNotSignedIn] when the file does
// not exist.
func (s *TokenStore) Load() (Tokens, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Tokens{}, ErrNotSignedIn
	}

	if err != nil {
		return Tokens{}, fmt.Errorf("reading token file: %w", err)
	}

	var t Tokens

	err = json.Unmarshal(raw, &t)
	if err != nil {
		return Tokens{}, fmt.Errorf("decoding token file %s: %w", s.path, err)
	}

	return t, nil
}

// Save writes the token file with owner-only permissions. It writes a
// temporary file and renames it, so a concurrent [*TokenStore.Load] reads
// either the previous tokens or the saved tokens.
func (s *TokenStore) Save(t Tokens) error {
	//nolint:gosec // G117: The token file stores these secrets.
	raw, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding tokens: %w", err)
	}

	err = os.MkdirAll(filepath.Dir(s.path), 0o700)
	if err != nil {
		return fmt.Errorf("creating token directory: %w", err)
	}

	tmp := s.path + ".tmp"

	err = os.WriteFile(tmp, raw, 0o600)
	if err != nil {
		return fmt.Errorf("writing token file: %w", err)
	}

	err = os.Rename(tmp, s.path)
	if err != nil {
		return fmt.Errorf("replacing token file: %w", err)
	}

	return nil
}

// Remove deletes the token file. A missing file is not an error.
func (s *TokenStore) Remove() error {
	err := os.Remove(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing token file: %w", err)
	}

	return nil
}

// lock takes an exclusive advisory lock that serializes token replacement
// across processes. The caller runs the returned function to release it.
func (s *TokenStore) lock() (func(), error) {
	err := os.MkdirAll(filepath.Dir(s.path), 0o700)
	if err != nil {
		return nil, fmt.Errorf("creating token directory: %w", err)
	}

	f, err := os.OpenFile(s.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening token lock: %w", err)
	}

	//nolint:gosec // G115: a file descriptor fits in an int.
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	if err != nil {
		_ = f.Close() //nolint:errcheck // The lock error is the one to report.

		return nil, fmt.Errorf("locking token file: %w", err)
	}

	return func() {
		// Closing the descriptor releases the lock.
		_ = f.Close() //nolint:errcheck // Nothing can act on a close error here.
	}, nil
}

// TokenBroker exchanges Bungie OAuth codes and refresh tokens. The vault
// manager service holds the OAuth client secret, so [*Service] implements
// it.
type TokenBroker interface {
	Config(ctx context.Context) (ServiceConfig, error)
	ExchangeCode(ctx context.Context, code string) (TokenResponse, error)
	RefreshTokens(ctx context.Context, refreshToken string) (TokenResponse, error)
}

// AuthOption configures an [*Auth]. See [WithAuthClock].
type AuthOption func(*Auth)

// WithAuthClock overrides the clock that decides token expiry. It is an
// [AuthOption].
func WithAuthClock(now func() time.Time) AuthOption {
	return func(a *Auth) { a.now = now }
}

// Auth signs the user in and keeps the tokens usable.
//
// Bungie redirects a sign-in to the one URL registered for the deployment,
// which is the web client. A local process cannot receive that redirect.
// [*Auth.LoginURL] therefore issues an OAuth state that the web client does
// not recognize. The web client refuses the redirect and leaves the
// authorization code unused in the address bar. The user copies that URL to
// [*Auth.CompleteLogin], which exchanges the code through the service.
type Auth struct {
	broker TokenBroker
	store  *TokenStore
	now    func() time.Time
	state  string
	mu     sync.Mutex
}

// NewAuth returns an [*Auth] that persists tokens in store and exchanges
// them through broker.
func NewAuth(store *TokenStore, broker TokenBroker, opts ...AuthOption) *Auth {
	a := &Auth{
		broker: broker,
		store:  store,
		now:    time.Now,
	}
	for _, opt := range opts {
		opt(a)
	}

	return a
}

// LoginURL returns the Bungie authorization URL for the user to open.
func (a *Auth) LoginURL(ctx context.Context) (string, error) {
	cfg, err := a.broker.Config(ctx)
	if err != nil {
		return "", fmt.Errorf("reading service config: %w", err)
	}

	raw := make([]byte, stateBytes)

	_, err = rand.Read(raw)
	if err != nil {
		return "", fmt.Errorf("generating OAuth state: %w", err)
	}

	state := hex.EncodeToString(raw)

	a.mu.Lock()
	a.state = state
	a.mu.Unlock()

	query := url.Values{}
	query.Set("client_id", cfg.ClientID)
	query.Set("response_type", "code")
	query.Set("state", state)

	return authorizeURL + "?" + query.Encode(), nil
}

// CompleteLogin exchanges the authorization code in pasted and saves the
// tokens. Pasted is the URL that Bungie redirected to, or the bare code.
func (a *Auth) CompleteLogin(ctx context.Context, pasted string) (Tokens, error) {
	code, state, err := extractCode(pasted)
	if err != nil {
		return Tokens{}, err
	}

	a.mu.Lock()
	want := a.state
	a.mu.Unlock()

	if want != "" && state != "" && state != want {
		return Tokens{}, fmt.Errorf("%w: the URL carries a state from a different sign-in attempt", ErrLogin)
	}

	resp, err := a.broker.ExchangeCode(ctx, code)
	if err != nil {
		return Tokens{}, fmt.Errorf("exchanging authorization code: %w", err)
	}

	unlock, err := a.store.lock()
	if err != nil {
		return Tokens{}, err
	}
	defer unlock()

	t := a.fromResponse(resp)

	err = a.store.Save(t)
	if err != nil {
		return Tokens{}, err
	}

	a.mu.Lock()
	a.state = ""
	a.mu.Unlock()

	return t, nil
}

// Tokens returns tokens with an unexpired access token. It refreshes them
// first when the access token is within a minute of its expiry.
func (a *Auth) Tokens(ctx context.Context) (Tokens, error) {
	t, err := a.store.Load()
	if err != nil {
		return Tokens{}, err
	}

	if a.fresh(t) {
		return t, nil
	}

	return a.Refresh(ctx, t.AccessToken)
}

// Refresh replaces the tokens after Bungie or the service rejected the
// access token stale. When another process already replaced that token,
// Refresh returns the tokens from the file.
func (a *Auth) Refresh(ctx context.Context, stale string) (Tokens, error) {
	unlock, err := a.store.lock()
	if err != nil {
		return Tokens{}, err
	}
	defer unlock()

	t, err := a.store.Load()
	if err != nil {
		return Tokens{}, err
	}

	if t.AccessToken != stale && a.fresh(t) {
		return t, nil
	}

	if t.RefreshToken == "" || !a.now().Before(t.RefreshExpires) {
		return Tokens{}, fmt.Errorf("%w: the saved sign-in expired", ErrNotSignedIn)
	}

	resp, err := a.broker.RefreshTokens(ctx, t.RefreshToken)
	if err != nil {
		return Tokens{}, fmt.Errorf("refreshing tokens: %w", err)
	}

	next := a.fromResponse(resp)
	next.Membership = t.Membership

	err = a.store.Save(next)
	if err != nil {
		return Tokens{}, err
	}

	return next, nil
}

// SetMembership records the Destiny membership of the Bungie.net account
// bungieID in the token file. It leaves the file unchanged when the file
// holds the tokens of a different account. The accounts differ when another
// process signs in between the membership lookup and this call.
func (a *Auth) SetMembership(bungieID string, m Membership) error {
	unlock, err := a.store.lock()
	if err != nil {
		return err
	}
	defer unlock()

	t, err := a.store.Load()
	if err != nil {
		return err
	}

	if t.BungieID != bungieID {
		return nil
	}

	t.Membership = m

	return a.store.Save(t)
}

// Saved returns the tokens in the token file without refreshing them.
func (a *Auth) Saved() (Tokens, error) {
	return a.store.Load()
}

// Logout deletes the saved tokens.
func (a *Auth) Logout() error {
	unlock, err := a.store.lock()
	if err != nil {
		return err
	}
	defer unlock()

	return a.store.Remove()
}

func (a *Auth) fresh(t Tokens) bool {
	return t.AccessToken != "" && t.Session != "" && a.now().Add(tokenSkew).Before(t.AccessExpires)
}

func (a *Auth) fromResponse(resp TokenResponse) Tokens {
	now := a.now()

	t := Tokens{
		AccessToken:   resp.AccessToken,
		AccessExpires: now.Add(time.Duration(resp.ExpiresIn) * time.Second),
		RefreshToken:  resp.RefreshToken,
		Session:       resp.Session,
		BungieID:      resp.MembershipID,
	}

	if resp.RefreshExpiresIn > 0 {
		t.RefreshExpires = now.Add(time.Duration(resp.RefreshExpiresIn) * time.Second)
	}

	return t
}

// extractCode reads the authorization code and the OAuth state from a
// pasted redirect URL. A value without a query string is the bare code.
func extractCode(pasted string) (string, string, error) {
	trimmed := strings.TrimSpace(pasted)
	if trimmed == "" {
		return "", "", fmt.Errorf("%w: nothing pasted", ErrLogin)
	}

	if !strings.ContainsAny(trimmed, "?=") {
		return trimmed, "", nil
	}

	// A value without a "?" is the query string itself.
	rawQuery := trimmed[strings.Index(trimmed, "?")+1:]

	rawQuery, _, _ = strings.Cut(rawQuery, "#")

	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", "", fmt.Errorf("%w: parsing the pasted URL: %w", ErrLogin, err)
	}

	code := query.Get("code")
	if code == "" {
		return "", "", fmt.Errorf("%w: the pasted URL has no code parameter", ErrLogin)
	}

	return code, query.Get("state"), nil
}
