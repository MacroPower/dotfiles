package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serve starts a fake server that answers every request with one status
// and body, and records the requests.
func serve(t *testing.T, status int, body string) (string, *recorder) {
	t.Helper()

	rec := &recorder{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(t, r)
		assert.Equal(t, defaultUserAgent, r.Header.Get("User-Agent"))
		writeJSON(t, w, status, body)
	}))
	t.Cleanup(srv.Close)

	return srv.URL, rec
}

func TestServiceConfig(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err    error
		body   string
		want   ServiceConfig
		status int
	}{
		"config": {
			status: http.StatusOK,
			body: `{"apiKey": "key", "clientId": "54520", "authOrigin": "https://example.test",
				"artifacts": {"manifestVersion": "v1", "files": {"core": ["core-0.json"]}}}`,
			want: ServiceConfig{
				APIKey:     "key",
				ClientID:   "54520",
				AuthOrigin: "https://example.test",
				Artifacts: ArtifactIndex{
					ManifestVersion: "v1",
					Files:           map[string][]string{"core": {"core-0.json"}},
				},
			},
		},
		"missing credentials": {
			status: http.StatusOK,
			body:   `{"apiKey": "", "clientId": ""}`,
			err:    ErrService,
		},
		"server error": {
			status: http.StatusInternalServerError,
			body:   `{"error": "boom"}`,
			err:    ErrService,
		},
		"not JSON": {
			status: http.StatusOK,
			body:   `<html>`,
			err:    ErrService,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			u, _ := serve(t, tc.status, tc.body)

			got, err := NewService(WithServiceURL(u)).Config(t.Context())
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestServiceTokens(t *testing.T) {
	t.Parallel()

	u, rec := serve(t, http.StatusOK, `{"access_token": "a", "expires_in": 3600, "refresh_token": "r",
		"refresh_expires_in": 7776000, "membership_id": "42", "session": "s"}`)
	s := NewService(WithServiceURL(u))

	want := TokenResponse{
		AccessToken:      "a",
		ExpiresIn:        3600,
		RefreshToken:     "r",
		RefreshExpiresIn: 7776000,
		MembershipID:     "42",
		Session:          "s",
	}

	got, err := s.ExchangeCode(t.Context(), "abc")
	require.NoError(t, err)
	assert.Equal(t, want, got)

	got, err = s.RefreshTokens(t.Context(), "r0")
	require.NoError(t, err)
	assert.Equal(t, want, got)

	exchange := rec.calls(http.MethodPost, "/api/auth/token")
	require.Len(t, exchange, 1)
	assert.Equal(t, map[string]any{"code": "abc"}, exchange[0].Body)
	assert.Empty(t, exchange[0].Auth)

	refresh := rec.calls(http.MethodPost, "/api/auth/refresh")
	require.Len(t, refresh, 1)
	assert.Equal(t, map[string]any{"refreshToken": "r0"}, refresh[0].Body)
}

func TestServiceWrites(t *testing.T) {
	t.Parallel()

	u, rec := serve(t, http.StatusOK, `{"now": 11}`)
	s := NewService(WithServiceURL(u))
	hash := uint32(100)

	require.NoError(t, s.WriteTags(t.Context(), "session", []ItemTag{
		{InstanceID: "1001", ItemHash: 100, TagID: "keep"},
		{InstanceID: "1001", ItemHash: 100, TagID: "junk", RuleID: "r1", Removed: true},
	}))
	require.NoError(t, s.WriteNote(t.Context(), "session", Note{
		Kind: noteInstance, TargetID: "1001", ItemHash: &hash, Body: "god roll",
	}))
	require.NoError(t, s.WriteNote(t.Context(), "session", Note{Kind: noteSet, TargetID: "set-key", Removed: true}))
	require.NoError(t, s.WritePerkPreferences(t.Context(), "session", []PerkPreference{
		{Mode: "pve", Perk: "Firefly", Stance: "up"},
	}))
	require.NoError(t, s.WriteAutotagRules(t.Context(), "session", []AutotagRule{
		{ID: "r1", Query: "is:dupe", Vault: true, Enabled: true, Position: 2},
	}))

	// The service rejects a row with a field it does not expect to be
	// empty, so the bodies must match its parsers field for field.
	tags := rec.calls(http.MethodPost, "/api/tags")
	require.Len(t, tags, 1)
	assert.Equal(t, "Bearer session", tags[0].Auth)
	assert.Equal(t, map[string]any{
		"defs": []any{},
		"items": []any{
			map[string]any{"instanceId": "1001", "itemHash": float64(100), "tagId": "keep", "removed": false},
			map[string]any{
				"instanceId": "1001", "itemHash": float64(100), "tagId": "junk", "ruleId": "r1", "removed": true,
			},
		},
	}, tags[0].Body)

	notes := rec.calls(http.MethodPost, "/api/notes")
	require.Len(t, notes, 2)
	assert.Equal(t, map[string]any{
		"kind": "instance", "targetId": "1001", "itemHash": float64(100), "body": "god roll", "removed": false,
	}, notes[0].Body)
	assert.Equal(t, map[string]any{"kind": "set", "targetId": "set-key", "body": "", "removed": true}, notes[1].Body)

	prefs := rec.calls(http.MethodPost, "/api/perk-preferences")
	require.Len(t, prefs, 1)
	assert.Equal(t, map[string]any{"preferences": []any{
		map[string]any{"mode": "pve", "perk": "Firefly", "stance": "up", "removed": false},
	}}, prefs[0].Body)

	rules := rec.calls(http.MethodPost, "/api/autotag")
	require.Len(t, rules, 1)
	assert.Equal(t, map[string]any{"rules": []any{
		map[string]any{
			"id": "r1", "query": "is:dupe", "vault": true, "enabled": true, "position": float64(2), "removed": false,
		},
	}}, rules[0].Body)
}

func TestServiceGrantRejected(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body     string
		rejected bool
	}{
		"Bungie refuses the grant": {
			body:     `{"error": "Token endpoint returned 400 {\"error\":\"invalid_grant\"}"}`,
			rejected: true,
		},
		"Bungie is unavailable": {
			body: `{"error": "Token endpoint returned 503 maintenance"}`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			u, _ := serve(t, http.StatusInternalServerError, tc.body)

			_, err := NewService(WithServiceURL(u)).RefreshTokens(t.Context(), "revoked")
			require.ErrorIs(t, err, ErrService)

			if tc.rejected {
				require.ErrorIs(t, err, ErrGrantRejected)
			} else {
				require.NotErrorIs(t, err, ErrGrantRejected)
			}
		})
	}
}

func TestServiceUnauthorized(t *testing.T) {
	t.Parallel()

	u, _ := serve(t, http.StatusUnauthorized, `{"error": "Sign in required"}`)

	_, err := NewService(WithServiceURL(u)).Tags(t.Context(), "expired")
	require.ErrorIs(t, err, ErrUnauthorized)
	assert.Contains(t, err.Error(), "Sign in required")
}

func TestServiceArtifact(t *testing.T) {
	t.Parallel()

	u, rec := serve(t, http.StatusOK, `[1, 2]`)

	body, err := NewService(WithServiceURL(u)).Artifact(t.Context(), "core-0.json")
	require.NoError(t, err)

	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())
	assert.JSONEq(t, `[1, 2]`, string(raw))
	assert.Len(t, rec.calls(http.MethodGet, "/artifacts/core-0.json"), 1)

	missing, _ := serve(t, http.StatusNotFound, `{}`)

	_, err = NewService(WithServiceURL(missing)).Artifact(t.Context(), "core-0.json")
	require.ErrorIs(t, err, ErrService)
}

func TestBungiePrimaryMembership(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err  error
		body string
		want Membership
	}{
		"cross save primary": {
			body: `{"ErrorCode": 1, "Response": {"primaryMembershipId": "2", "destinyMemberships": [
				{"membershipType": 1, "membershipId": "1"}, {"membershipType": 3, "membershipId": "2"}]}}`,
			want: Membership{ID: "2", Type: 3},
		},
		"no primary falls back to the first": {
			body: `{"ErrorCode": 1, "Response": {"destinyMemberships": [
				{"membershipType": 1, "membershipId": "1"}, {"membershipType": 3, "membershipId": "2"}]}}`,
			want: Membership{ID: "1", Type: 1},
		},
		"no memberships": {
			body: `{"ErrorCode": 1, "Response": {"destinyMemberships": []}}`,
			err:  ErrBungie,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			u, rec := serve(t, http.StatusOK, tc.body)

			got, err := NewBungie(WithBungieURL(u)).
				PrimaryMembership(t.Context(), Credentials{APIKey: "key", AccessToken: "access"})
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)

			calls := rec.calls(http.MethodGet, "/User/GetMembershipsForCurrentUser/")
			require.Len(t, calls, 1)
			assert.Equal(t, "Bearer access", calls[0].Auth)
		})
	}
}

func TestBungieProfile(t *testing.T) {
	t.Parallel()

	var components string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/Destiny2/3/Profile/4611/", r.URL.Path)

		components = r.URL.Query().Get("components")

		writeJSON(t, w, http.StatusOK, `{"ErrorCode": 1, "Response": `+fixtureProfile+`}`)
	}))
	t.Cleanup(srv.Close)

	got, err := NewBungie(WithBungieURL(srv.URL)).
		Profile(t.Context(), Credentials{APIKey: "key", AccessToken: "access"}, Membership{ID: "4611", Type: 3})
	require.NoError(t, err)

	assert.Equal(t, "100,102,103,200,201,205,206,300,304,305,310", components)
	assert.Equal(t, []ProfileItem{
		{Hash: itemGlimmer, Quantity: 250000, Location: 1, BucketHash: 2689798308},
		{Hash: itemBrightDust, Quantity: 1500, Location: 1, BucketHash: 2689798311},
		{Hash: 77, Quantity: 3, Location: 1, BucketHash: 2689798305},
	}, got.ProfileCurrencies.Data.Items)

	loadouts := got.CharacterLoadouts.Data[titanID].Loadouts
	require.Len(t, loadouts, 1)
	assert.Equal(t, []ProfileLoadoutItem{{InstanceID: "1006"}}, loadouts[0].Items)
}

func TestBungieActivityHistory(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body string
		want []HistoryPeriod
	}{
		"page of runs": {
			body: `{"ErrorCode": 1, "Response": {"activities": [
				{"period": "2026-08-19T21:14:00Z",
				 "activityDetails": {"referenceId": 42, "directorActivityHash": 77, "instanceId": "500", "mode": 4},
				 "values": {
					"activityDurationSeconds": {"statId": "activityDurationSeconds", "basic": {"value": 2221}},
					"completed": {"basic": {"value": 1}}
				 }}
			]}}`,
			want: []HistoryPeriod{{
				Period: time.Date(2026, 8, 19, 21, 14, 0, 0, time.UTC),
				Activity: HistoryActivity{
					InstanceID:           "500",
					ReferenceID:          42,
					DirectorActivityHash: 77,
					Mode:                 4,
				},
				Values: map[string]HistoryValue{
					"activityDurationSeconds": historyValue(2221),
					"completed":               historyValue(1),
				},
			}},
		},
		"page past the last run": {
			body: `{"ErrorCode": 1, "Response": {}}`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			u, rec := serve(t, http.StatusOK, tc.body)

			got, err := NewBungie(WithBungieURL(u)).ActivityHistory(
				t.Context(),
				Credentials{APIKey: "key", AccessToken: "access"},
				Membership{ID: "4611", Type: 3},
				hunterID,
				25,
				2,
			)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)

			calls := rec.calls(http.MethodGet, "/Destiny2/3/Account/4611/Character/c1/Stats/Activities/")
			require.Len(t, calls, 1)
			assert.Equal(t, "count=25&page=2", calls[0].Query)
			assert.Equal(t, "Bearer access", calls[0].Auth)
		})
	}
}

func TestHistoryPeriodValue(t *testing.T) {
	t.Parallel()

	period := HistoryPeriod{Values: map[string]HistoryValue{"kills": historyValue(84)}}

	assert.InDelta(t, 84, period.Value("kills"), 0)
	assert.Zero(t, period.Value("deaths"))
}

// historyValue returns a stat value of the activity history.
func historyValue(v float64) HistoryValue {
	var out HistoryValue

	out.Basic.Value = v

	return out
}

func TestBungieErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body         string
		wantMessage  string
		status       int
		unauthorized bool
	}{
		"platform error": {
			status:      http.StatusOK,
			body:        `{"ErrorCode": 1642, "ErrorStatus": "DestinyNoRoomInDestination", "Message": "No room."}`,
			wantMessage: "bungie: DestinyNoRoomInDestination: No room.",
		},
		"expired access token": {
			status:       http.StatusOK,
			body:         `{"ErrorCode": 2111, "ErrorStatus": "AccessTokenHasExpired", "Message": "Expired."}`,
			wantMessage:  "bungie: AccessTokenHasExpired: Expired.",
			unauthorized: true,
		},
		"web auth required": {
			status:       http.StatusOK,
			body:         `{"ErrorCode": 99, "ErrorStatus": "WebAuthRequired", "Message": "Sign in."}`,
			wantMessage:  "bungie: WebAuthRequired: Sign in.",
			unauthorized: true,
		},
		"bare 401": {
			status:       http.StatusUnauthorized,
			body:         ``,
			unauthorized: true,
		},
		"bare 503": {
			status: http.StatusServiceUnavailable,
			body:   `<html>maintenance</html>`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			u, _ := serve(t, tc.status, tc.body)

			err := NewBungie(WithBungieURL(u)).Equip(t.Context(), Credentials{}, ItemAction{})
			require.Error(t, err)

			if tc.unauthorized {
				require.ErrorIs(t, err, ErrUnauthorized)
			} else {
				require.NotErrorIs(t, err, ErrUnauthorized)
				require.ErrorIs(t, err, ErrBungie)
			}

			if tc.wantMessage != "" {
				var platform *BungieError

				require.ErrorAs(t, err, &platform)
				assert.Equal(t, tc.wantMessage, platform.Error())
			}
		})
	}
}

func TestBungieActions(t *testing.T) {
	t.Parallel()

	u, rec := serve(t, http.StatusOK, `{"ErrorCode": 1, "Response": 0}`)
	b := NewBungie(WithBungieURL(u))
	creds := Credentials{APIKey: "key", AccessToken: "access"}
	action := ItemAction{
		InstanceID:  "1001",
		ItemHash:    100,
		CharacterID: hunterID,
		Membership:  Membership{ID: "4611", Type: 3},
	}

	require.NoError(t, b.Transfer(t.Context(), creds, action, true))
	require.NoError(t, b.Equip(t.Context(), creds, action))
	require.NoError(t, b.PullFromPostmaster(t.Context(), creds, action))
	require.NoError(t, b.SetLockState(t.Context(), creds, action, true))
	require.NoError(t, b.SetLockState(t.Context(), creds, action, false))
	require.NoError(t, b.EquipLoadout(t.Context(), creds, LoadoutAction{
		CharacterID: hunterID,
		Membership:  action.Membership,
		Index:       2,
	}))

	transfer := rec.calls(http.MethodPost, "/Destiny2/Actions/Items/TransferItem/")
	require.Len(t, transfer, 1)
	assert.Equal(t, map[string]any{
		"itemReferenceHash": float64(100),
		"stackSize":         float64(1),
		"transferToVault":   true,
		"itemId":            "1001",
		"characterId":       hunterID,
		"membershipType":    float64(3),
	}, transfer[0].Body)

	equip := rec.calls(http.MethodPost, "/Destiny2/Actions/Items/EquipItem/")
	require.Len(t, equip, 1)
	assert.Equal(t, map[string]any{
		"itemId":         "1001",
		"characterId":    hunterID,
		"membershipType": float64(3),
	}, equip[0].Body)

	pull := rec.calls(http.MethodPost, "/Destiny2/Actions/Items/PullFromPostmaster/")
	require.Len(t, pull, 1)
	assert.Equal(t, map[string]any{
		"itemReferenceHash": float64(100),
		"stackSize":         float64(1),
		"itemId":            "1001",
		"characterId":       hunterID,
		"membershipType":    float64(3),
	}, pull[0].Body)

	locks := rec.calls(http.MethodPost, "/Destiny2/Actions/Items/SetLockState/")
	require.Len(t, locks, 2)
	assert.Equal(t, map[string]any{
		"state":          true,
		"itemId":         "1001",
		"characterId":    hunterID,
		"membershipType": float64(3),
	}, locks[0].Body)
	assert.Equal(t, false, locks[1].Body["state"])

	loadouts := rec.calls(http.MethodPost, "/Destiny2/Actions/Loadouts/EquipLoadout/")
	require.Len(t, loadouts, 1)
	assert.Equal(t, map[string]any{
		"loadoutIndex":   float64(2),
		"characterId":    hunterID,
		"membershipType": float64(3),
	}, loadouts[0].Body)
}
