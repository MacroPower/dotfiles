package auth

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungietest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
)

// fakeBroker is a [TokenBroker] that counts its exchanges.
type fakeBroker struct {
	err       error
	codes     []string
	refreshes []string
}

func (b *fakeBroker) Config(context.Context) (dvm.Config, error) {
	return dvm.Config{APIKey: "key", ClientID: "54520"}, nil
}

func (b *fakeBroker) ExchangeCode(_ context.Context, code string) (dvm.TokenResponse, error) {
	b.codes = append(b.codes, code)

	return dvm.TokenResponse{
		AccessToken:      "access-new",
		ExpiresIn:        3600,
		RefreshToken:     "refresh-new",
		RefreshExpiresIn: 7776000,
		MembershipID:     "42",
		Session:          "session-new",
	}, b.err
}

func (b *fakeBroker) RefreshTokens(_ context.Context, refreshToken string) (dvm.TokenResponse, error) {
	b.refreshes = append(b.refreshes, refreshToken)

	return dvm.TokenResponse{
		AccessToken:      "access-refreshed",
		ExpiresIn:        3600,
		RefreshToken:     "refresh-rotated",
		RefreshExpiresIn: 7776000,
		MembershipID:     "42",
		Session:          "session-refreshed",
	}, b.err
}

func newTestAuth(t *testing.T, saved *Tokens) (*Auth, *fakeBroker, *TokenStore) {
	t.Helper()

	store := NewTokenStore(filepath.Join(t.TempDir(), "state", "tokens.json"))
	if saved != nil {
		require.NoError(t, store.Save(*saved))
	}

	broker := &fakeBroker{}
	auth := New(store, broker, WithClock(func() time.Time { return bungietest.Now }))

	return auth, broker, store
}

func TestExtractCode(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err       error
		pasted    string
		wantCode  string
		wantState string
	}{
		"redirect URL": {
			pasted:    " https://example.test/auth/callback?code=abc&state=s1\n",
			wantCode:  "abc",
			wantState: "s1",
		},
		"redirect URL with a fragment": {
			pasted:    "https://example.test/auth/callback?state=s1&code=abc#top",
			wantCode:  "abc",
			wantState: "s1",
		},
		"bare code": {
			pasted:   "abc123",
			wantCode: "abc123",
		},
		"query string only": {
			pasted:   "code=abc",
			wantCode: "abc",
		},
		"empty": {
			pasted: "  ",
			err:    ErrLogin,
		},
		"URL without a code": {
			pasted: "https://example.test/auth/callback?error=access_denied",
			err:    ErrLogin,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			code, state, err := extractCode(tc.pasted)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantCode, code)
			assert.Equal(t, tc.wantState, state)
		})
	}
}

func TestLogin(t *testing.T) {
	t.Parallel()

	auth, broker, store := newTestAuth(t, nil)
	assert.Same(t, store, auth.Store())

	_, err := auth.Tokens(t.Context())
	require.ErrorIs(t, err, ErrNotSignedIn)

	raw, err := auth.LoginURL(t.Context())
	require.NoError(t, err)

	u, err := url.Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, "www.bungie.net", u.Host)
	assert.Equal(t, "54520", u.Query().Get("client_id"))
	assert.Equal(t, "code", u.Query().Get("response_type"))

	state := u.Query().Get("state")
	require.NotEmpty(t, state)

	_, err = auth.CompleteLogin(t.Context(), "https://example.test/auth/callback?code=abc&state=other")
	require.ErrorIs(t, err, ErrLogin)
	assert.Empty(t, broker.codes, "a state from another attempt does not reach the exchange")

	got, err := auth.CompleteLogin(t.Context(), "https://example.test/auth/callback?code=abc&state="+state)
	require.NoError(t, err)
	assert.Equal(t, []string{"abc"}, broker.codes)

	want := Tokens{
		AccessToken:    "access-new",
		AccessExpires:  bungietest.Now.Add(time.Hour),
		RefreshToken:   "refresh-new",
		RefreshExpires: bungietest.Now.Add(90 * 24 * time.Hour),
		Session:        "session-new",
		BungieID:       "42",
	}
	assert.Equal(t, want, got)

	saved, err := store.Load()
	require.NoError(t, err)
	assert.Equal(t, want, saved)

	info, err := os.Stat(store.Path())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	require.NoError(t, auth.Logout())

	_, err = auth.Tokens(t.Context())
	require.ErrorIs(t, err, ErrNotSignedIn)
}

func TestTokens(t *testing.T) {
	t.Parallel()

	membership := bungie.Membership{ID: "4611", Type: 3}

	tests := map[string]struct {
		err         error
		saved       Tokens
		wantAccess  string
		wantRefresh []string
	}{
		"fresh tokens need no refresh": {
			saved: Tokens{
				AccessToken:    "access-1",
				AccessExpires:  bungietest.Now.Add(time.Hour),
				RefreshToken:   "refresh-1",
				RefreshExpires: bungietest.Now.Add(24 * time.Hour),
				Session:        "session-1",
			},
			wantAccess: "access-1",
		},
		"access token inside the skew refreshes": {
			saved: Tokens{
				AccessToken:    "access-1",
				AccessExpires:  bungietest.Now.Add(30 * time.Second),
				RefreshToken:   "refresh-1",
				RefreshExpires: bungietest.Now.Add(24 * time.Hour),
				Session:        "session-1",
				Membership:     membership,
			},
			wantAccess:  "access-refreshed",
			wantRefresh: []string{"refresh-1"},
		},
		"missing session refreshes": {
			saved: Tokens{
				AccessToken:    "access-1",
				AccessExpires:  bungietest.Now.Add(time.Hour),
				RefreshToken:   "refresh-1",
				RefreshExpires: bungietest.Now.Add(24 * time.Hour),
				Membership:     membership,
			},
			wantAccess:  "access-refreshed",
			wantRefresh: []string{"refresh-1"},
		},
		"expired refresh token": {
			saved: Tokens{
				AccessToken:    "access-1",
				AccessExpires:  bungietest.Now.Add(-time.Hour),
				RefreshToken:   "refresh-1",
				RefreshExpires: bungietest.Now.Add(-time.Minute),
				Session:        "session-1",
			},
			err: ErrNotSignedIn,
		},
		"no refresh token": {
			saved: Tokens{
				AccessToken:   "access-1",
				AccessExpires: bungietest.Now.Add(-time.Hour),
				Session:       "session-1",
			},
			err: ErrNotSignedIn,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			auth, broker, store := newTestAuth(t, &tc.saved)

			got, err := auth.Tokens(t.Context())
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantAccess, got.AccessToken)
			assert.Equal(t, tc.wantRefresh, broker.refreshes)
			assert.Equal(t, tc.saved.Membership, got.Membership, "a refresh keeps the membership")

			saved, err := store.Load()
			require.NoError(t, err)
			assert.Equal(t, got, saved)
		})
	}
}

func TestRefresh(t *testing.T) {
	t.Parallel()

	saved := Tokens{
		AccessToken:    "access-2",
		AccessExpires:  bungietest.Now.Add(time.Hour),
		RefreshToken:   "refresh-2",
		RefreshExpires: bungietest.Now.Add(24 * time.Hour),
		Session:        "session-2",
	}

	tests := map[string]struct {
		stale       string
		wantAccess  string
		wantRefresh []string
	}{
		"another process already replaced the stale token": {
			stale:      "access-1",
			wantAccess: "access-2",
		},
		"the saved token is the rejected one": {
			stale:       "access-2",
			wantAccess:  "access-refreshed",
			wantRefresh: []string{"refresh-2"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			auth, broker, _ := newTestAuth(t, &saved)

			got, err := auth.Refresh(t.Context(), tc.stale)
			require.NoError(t, err)
			assert.Equal(t, tc.wantAccess, got.AccessToken)
			assert.Equal(t, tc.wantRefresh, broker.refreshes)
		})
	}
}

func TestRefreshErrors(t *testing.T) {
	t.Parallel()

	saved := Tokens{
		AccessToken:    "access-1",
		AccessExpires:  bungietest.Now.Add(-time.Hour),
		RefreshToken:   "refresh-1",
		RefreshExpires: bungietest.Now.Add(24 * time.Hour),
		Session:        "session-1",
	}

	tests := map[string]struct {
		broker     error
		err        error
		wantSignIn bool
	}{
		"Bungie rejected the refresh token": {
			broker:     fmt.Errorf("%w: %w", dvm.ErrGrantRejected, dvm.ErrService),
			err:        dvm.ErrGrantRejected,
			wantSignIn: true,
		},
		"the service is unavailable": {
			broker: dvm.ErrService,
			err:    dvm.ErrService,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			auth, broker, store := newTestAuth(t, &saved)
			broker.err = tc.broker

			_, err := auth.Tokens(t.Context())
			require.ErrorIs(t, err, tc.err)

			if tc.wantSignIn {
				require.ErrorIs(t, err, ErrNotSignedIn)
			} else {
				require.NotErrorIs(t, err, ErrNotSignedIn)
			}

			kept, err := store.Load()
			require.NoError(t, err)
			assert.Equal(t, saved, kept, "a refresh that did not succeed keeps the token file")
		})
	}
}

func TestSetMembership(t *testing.T) {
	t.Parallel()

	auth, _, store := newTestAuth(t, &Tokens{AccessToken: "access-1", Session: "session-1", BungieID: "42"})

	require.NoError(t, auth.SetMembership("7", bungie.Membership{ID: "9000", Type: 1}))

	saved, err := store.Load()
	require.NoError(t, err)
	assert.True(t, saved.Membership.IsZero(), "the tokens belong to a different account")

	require.NoError(t, auth.SetMembership("42", bungie.Membership{ID: "4611", Type: 3}))

	saved, err = store.Load()
	require.NoError(t, err)
	assert.Equal(t, bungie.Membership{ID: "4611", Type: 3}, saved.Membership)
	assert.Equal(t, "access-1", saved.AccessToken)
}
