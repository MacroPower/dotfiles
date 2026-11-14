package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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

func TestServiceClaimAutotag(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err    error
		body   string
		status int
		want   bool
	}{
		"the service applies the claim": {
			status: http.StatusOK,
			body:   `{"applied": true}`,
			want:   true,
		},
		"another runner claimed first": {
			status: http.StatusOK,
			body:   `{"applied": false}`,
		},
		"the service refuses the change": {
			status: http.StatusBadRequest,
			body:   `{"error": "Bad autotag state"}`,
			err:    ErrService,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			u, rec := serve(t, tc.status, tc.body)

			got, err := NewService(WithServiceURL(u)).ClaimAutotag(
				t.Context(), "session", "00000000000000001006", "00000000000000001009",
			)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				assert.Contains(t, err.Error(), "Bad autotag state")
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tc.want, got)

			claims := rec.calls(http.MethodPost, "/api/autotag/state")
			require.Len(t, claims, 1)
			assert.Equal(t, "Bearer session", claims[0].Auth)
			assert.Equal(t, map[string]any{
				"kind": "claim", "from": "00000000000000001006", "to": "00000000000000001009",
			}, claims[0].Body)
		})
	}
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

func TestServiceStats(t *testing.T) {
	t.Parallel()

	glimmer := func(at, before, after int64) map[string]any {
		return map[string]any{
			"kind":     "currency",
			"itemHash": itemGlimmer,
			"from":     at - 15_000,
			"at":       at,
			"before":   before,
			"after":    after,
		}
	}

	// The item record carries a snapshot that the client does not decode.
	drop := map[string]any{
		"kind": "item", "instanceId": "1001", "itemHash": 100, "characterId": hunterID, "bucketHash": bucketKinetic,
		"added": true, "from": 285_000, "at": 300_000,
		"snapshot": map[string]any{"item": map[string]any{"itemHash": 100}, "components": map[string]any{}},
	}

	first := testChunk{startsAt: 300_000, updatedAt: 900_000, changes: []map[string]any{glimmer(310_000, 1_000, 2_000)}}
	second := testChunk{
		startsAt:  600_000,
		updatedAt: 950_000,
		changes:   []map[string]any{glimmer(610_000, 2_000, 1_500)},
	}
	// A second recorder replaced the first bucket with a longer copy.
	replaced := testChunk{
		startsAt:  300_000,
		updatedAt: 1_200_000,
		changes:   []map[string]any{glimmer(310_000, 1_000, 2_000), drop},
	}

	// reordered encodes a page whose more key comes before its chunks and
	// whose other keys the client skips.
	reordered := func(chunks ...testChunk) string {
		var page struct {
			Chunks json.RawMessage `json:"chunks"`
		}

		require.NoError(t, json.Unmarshal([]byte(statPageJSON(t, false, chunks...)), &page))

		return `{"now": 5, "more": false, "extra": {"chunks": [], "more": true}, "chunks": ` +
			string(page.Chunks) + `, "later": [1, 2]}`
	}

	tests := map[string]struct {
		pages       map[string]string
		wantQueries []string
		want        []StatChunk
	}{
		"one page": {
			pages: map[string]string{
				"since=0&after=0": statPageJSON(t, false, first, second),
			},
			wantQueries: []string{"since=0&after=0"},
			want: []StatChunk{
				{StartsAt: 300_000, EndsAt: 600_000, UpdatedAt: 900_000, Changes: []StatChange{
					{Kind: "currency", ItemHash: itemGlimmer, From: 295_000, At: 310_000, Before: 1_000, After: 2_000},
				}},
				{StartsAt: 600_000, EndsAt: 900_000, UpdatedAt: 950_000, Changes: []StatChange{
					{Kind: "currency", ItemHash: itemGlimmer, From: 595_000, At: 610_000, Before: 2_000, After: 1_500},
				}},
			},
		},
		"keys in another order and unknown keys": {
			pages:       map[string]string{"since=0&after=0": reordered(first)},
			wantQueries: []string{"since=0&after=0"},
			want: []StatChunk{
				{StartsAt: 300_000, EndsAt: 600_000, UpdatedAt: 900_000, Changes: []StatChange{
					{Kind: "currency", ItemHash: itemGlimmer, From: 295_000, At: 310_000, Before: 1_000, After: 2_000},
				}},
			},
		},
		"the later copy of a bucket replaces the earlier one": {
			pages: map[string]string{
				"since=0&after=0":           statPageJSON(t, true, first, second),
				"since=950000&after=600000": statPageJSON(t, false, replaced),
			},
			wantQueries: []string{"since=0&after=0", "since=950000&after=600000"},
			want: []StatChunk{
				{StartsAt: 300_000, EndsAt: 600_000, UpdatedAt: 1_200_000, Changes: []StatChange{
					{Kind: "currency", ItemHash: itemGlimmer, From: 295_000, At: 310_000, Before: 1_000, After: 2_000},
					{Kind: "item", InstanceID: "1001", ItemHash: 100, From: 285_000, At: 300_000},
				}},
				{StartsAt: 600_000, EndsAt: 900_000, UpdatedAt: 950_000, Changes: []StatChange{
					{Kind: "currency", ItemHash: itemGlimmer, From: 595_000, At: 610_000, Before: 2_000, After: 1_500},
				}},
			},
		},
		"an empty page ends the read": {
			pages: map[string]string{
				"since=0&after=0":           statPageJSON(t, true, first),
				"since=900000&after=300000": statPageJSON(t, true),
			},
			wantQueries: []string{"since=0&after=0", "since=900000&after=300000"},
			want: []StatChunk{
				{StartsAt: 300_000, EndsAt: 600_000, UpdatedAt: 900_000, Changes: []StatChange{
					{Kind: "currency", ItemHash: itemGlimmer, From: 295_000, At: 310_000, Before: 1_000, After: 2_000},
				}},
			},
		},
		"no chunks": {
			pages:       map[string]string{"since=0&after=0": statPageJSON(t, false)},
			wantQueries: []string{"since=0&after=0"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := &recorder{}

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				req := rec.record(t, r)
				assert.Equal(t, http.MethodGet, req.Method)
				assert.Equal(t, "/api/stats", req.Path)
				assert.Equal(t, "Bearer session", req.Auth)

				query := r.URL.Query()

				page, ok := tc.pages["since="+query.Get("since")+"&after="+query.Get("after")]
				if !ok {
					writeJSON(t, w, http.StatusNotFound, `{"error": "unexpected cursor"}`)

					return
				}

				writeJSON(t, w, http.StatusOK, page)
			}))
			t.Cleanup(srv.Close)

			got, err := NewService(WithServiceURL(srv.URL)).Stats(t.Context(), "session", StatCursor{})
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)

			var queries []string
			for _, req := range rec.all() {
				queries = append(queries, req.Query)
			}

			assert.Equal(t, tc.wantQueries, queries)
		})
	}
}

func TestServiceStatsLargePage(t *testing.T) {
	t.Parallel()

	// Each body is the longest that the service accepts. The page holds one
	// chunk more than fits in maxResponseBytes, the cap of the other service
	// routes.
	body := strconv.Quote("[" + strings.Repeat(" ", maxStatChunkLength-2) + "]")
	count := maxResponseBytes/maxStatChunkLength + 1

	chunks := make([]string, 0, count)

	for i := range int64(count) {
		startsAt := i * 300_000
		chunks = append(chunks, fmt.Sprintf(
			`{"startsAt": %d, "endsAt": %d, "updatedAt": %d, "body": %s}`,
			startsAt, startsAt+300_000, i+1, body,
		))
	}

	page := `{"chunks": [` + strings.Join(chunks, ", ") + `], "more": false}`
	require.Greater(t, len(page), maxResponseBytes)

	u, _ := serve(t, http.StatusOK, page)

	got, err := NewService(WithServiceURL(u)).Stats(t.Context(), "session", StatCursor{})
	require.NoError(t, err)
	require.Len(t, got, count)
	assert.Equal(t, int64(count-1)*300_000, got[count-1].StartsAt)
}

func TestServiceStatsErrors(t *testing.T) {
	t.Parallel()

	chunk := func(body string) string {
		return `{"chunks": [{"startsAt": 300000, "endsAt": 600000, "updatedAt": 5, "body": ` +
			strconv.Quote(body) + `}], "more": false}`
	}

	tests := map[string]struct {
		body   string
		status int
	}{
		"body is an object": {
			status: http.StatusOK,
			body:   chunk(`{"kind": "currency"}`),
		},
		"body is null": {
			status: http.StatusOK,
			body:   chunk(`null`),
		},
		"body holds a number": {
			status: http.StatusOK,
			body:   chunk(`[1]`),
		},
		"body holds a record without a kind": {
			status: http.StatusOK,
			body:   chunk(`[{"itemHash": 1}]`),
		},
		"body is not JSON": {
			status: http.StatusOK,
			body:   chunk(`[{"kind": `),
		},
		"page is not JSON": {
			status: http.StatusOK,
			body:   `<html>`,
		},
		"page ends early": {
			status: http.StatusOK,
			body:   `{"chunks": [`,
		},
		"page is an array": {
			status: http.StatusOK,
			body:   `[]`,
		},
		"chunks is an object": {
			status: http.StatusOK,
			body:   `{"chunks": {}, "more": false}`,
		},
		"page does not advance the cursor": {
			status: http.StatusOK,
			body:   `{"chunks": [{"startsAt": 300000, "endsAt": 600000, "updatedAt": 5, "body": "[]"}], "more": true}`,
		},
		"server error": {
			status: http.StatusInternalServerError,
			body:   `{"error": "boom"}`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			u, _ := serve(t, tc.status, tc.body)

			_, err := NewService(WithServiceURL(u)).Stats(t.Context(), "session", StatCursor{})
			require.ErrorIs(t, err, ErrService)
		})
	}
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
	assert.Equal(t, ProfileLoadout{
		Items:     []ProfileLoadoutItem{{InstanceID: "1006"}},
		NameHash:  7,
		IconHash:  8,
		ColorHash: 9,
	}, loadouts[0])
}

func TestBungieManifest(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body string
		want map[string]string
	}{
		"English tables": {
			body: fixtureManifest,
			want: map[string]string{
				"DestinyInventoryItemDefinition": "/common/destiny2_content/json/en/DestinyInventoryItemDefinition-a1.json",
				string(loadoutNameTable):         pathLoadoutNames,
				string(loadoutIconTable):         pathLoadoutIcons,
				string(loadoutColorTable):        pathLoadoutColors,
			},
		},
		"no English tables": {
			body: `{"ErrorCode": 1, "Response": {"jsonWorldComponentContentPaths": {"fr": {"A": "/a.json"}}}}`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			u, rec := serve(t, http.StatusOK, tc.body)

			got, err := NewBungie(WithBungieURL(u)).
				Manifest(t.Context(), Credentials{APIKey: "key", AccessToken: "access"})
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)

			calls := rec.calls(http.MethodGet, pathManifest)
			require.Len(t, calls, 1)
			assert.Equal(t, "Bearer access", calls[0].Auth)
		})
	}
}

func TestBungieContent(t *testing.T) {
	t.Parallel()

	platform, platformRec := serve(t, http.StatusOK, `{"ErrorCode": 1, "Response": {}}`)
	content, contentRec := serve(t, http.StatusOK, fixtureLoadoutNames)

	b := NewBungie(WithBungieURL(platform), WithBungieContentURL(content+"/"))

	body, err := b.Content(t.Context(), pathLoadoutNames)
	require.NoError(t, err)

	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())
	assert.JSONEq(t, fixtureLoadoutNames, string(raw))

	calls := contentRec.calls(http.MethodGet, pathLoadoutNames)
	require.Len(t, calls, 1)
	assert.Empty(t, calls[0].Auth, "a content request carries no access token")
	assert.Empty(t, platformRec.all(), "a content request does not reach the platform base URL")

	missing, _ := serve(t, http.StatusNotFound, `{}`)

	_, err = NewBungie(WithBungieContentURL(missing)).Content(t.Context(), pathLoadoutNames)
	require.ErrorIs(t, err, ErrBungie)
	require.EqualError(t, err, "bungie: content file returned status 404")
}

// endlessBody is a response body that never ends and reads as zero-valued
// bytes. Its Close records the call and returns errBodyClose.
type endlessBody struct {
	closed bool
}

var errBodyClose = errors.New("body close refused")

func (b *endlessBody) Read(p []byte) (int, error) {
	clear(p)

	return len(p), nil
}

func (b *endlessBody) Close() error {
	b.closed = true

	return errBodyClose
}

// bodyTransport answers every request with status 200 and one body.
type bodyTransport struct {
	body io.ReadCloser
}

func (rt bodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       rt.body,
		Request:    req,
	}, nil
}

func TestCappedBody(t *testing.T) {
	t.Parallel()

	tcs := map[string]struct {
		open func(t *testing.T, client *http.Client) (io.ReadCloser, error)
	}{
		"service artifact": {
			open: func(t *testing.T, client *http.Client) (io.ReadCloser, error) {
				t.Helper()

				s := NewService(WithServiceURL("http://service.invalid"))
				s.http = client

				return s.Artifact(t.Context(), "core-0.json")
			},
		},
		"bungie content": {
			open: func(t *testing.T, client *http.Client) (io.ReadCloser, error) {
				t.Helper()

				b := NewBungie(WithBungieContentURL("http://content.invalid"))
				b.http = client

				return b.Content(t.Context(), pathLoadoutNames)
			},
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			raw := &endlessBody{}

			body, err := tc.open(t, &http.Client{Transport: bodyTransport{body: raw}})
			require.NoError(t, err)

			n, err := io.CopyN(io.Discard, body, maxArtifactBytes+1)
			assert.Equal(t, int64(maxArtifactBytes), n, "the reader stops at the cap")
			require.ErrorIs(t, err, io.EOF, "the reader ends at the cap")

			require.EqualError(t, body.Close(), errBodyClose.Error(),
				"the reader returns the close error of the body unchanged")
			assert.True(t, raw.closed)
		})
	}
}

func TestBungieVendors(t *testing.T) {
	t.Parallel()

	rec := &recorder{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(t, r)
		assert.Equal(t, "key", r.Header.Get("X-Api-Key"))

		writeJSON(t, w, http.StatusOK, `{"ErrorCode": 1, "Response": {
			"vendors": {"data": {
				"672118013": {"vendorHash": 672118013, "enabled": true, "canPurchase": true,
				 "nextRefreshDate": "2026-11-13T17:00:00Z", "vendorLocationIndex": 0}
			}},
			"categories": {"data": {
				"672118013": {"categories": [{"displayCategoryIndex": 2, "itemIndexes": [7, 4]}]}
			}},
			"sales": {"data": {
				"672118013": {"saleItems": {
					"7": {"vendorItemIndex": 7, "itemHash": 100, "quantity": 1, "saleStatus": 8,
					 "failureIndexes": [1], "augments": 0, "apiPurchasable": false,
					 "costs": [{"itemHash": 3159615086, "quantity": 50000, "hasConditionalVisibility": false}]}
				}}
			}}
		}}`)
	}))
	t.Cleanup(srv.Close)

	b := NewBungie(WithBungieURL(srv.URL))
	b.now = func() time.Time { return fixtureNow }

	vendors, err := b.Vendors(
		t.Context(),
		Credentials{APIKey: "key", AccessToken: "access"},
		Membership{ID: "4611", Type: 3},
		hunterID,
	)
	require.NoError(t, err)

	calls := rec.calls(http.MethodGet, "/Destiny2/3/Profile/4611/Character/c1/Vendors/")
	require.Len(t, calls, 1)
	assert.Equal(t, "components=400,401,402&_="+strconv.FormatInt(fixtureNow.UnixMilli(), 10), calls[0].Query)
	assert.Equal(t, "Bearer access", calls[0].Auth)

	assert.Equal(t, map[uint32]VendorComponent{
		672118013: {Hash: 672118013, Enabled: true, NextRefreshDate: time.Date(2026, 11, 13, 17, 0, 0, 0, time.UTC)},
	}, vendors.Vendors.Data)
	assert.Equal(t, map[uint32]VendorCategories{
		672118013: {Categories: []VendorCategory{{DisplayCategoryIndex: 2, ItemIndexes: []int{7, 4}}}},
	}, vendors.Categories.Data)
	assert.Equal(t, map[uint32]VendorSales{
		672118013: {SaleItems: map[int]VendorSale{
			7: {
				VendorItemIndex: 7,
				ItemHash:        100,
				Quantity:        1,
				SaleStatus:      8,
				FailureIndexes:  []int{1},
				Costs:           []VendorCost{{ItemHash: itemGlimmer, Quantity: 50000}},
			},
		}},
	}, vendors.Sales.Data)
}

func TestBungieActivities(t *testing.T) {
	t.Parallel()

	rec := &recorder{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(t, r)
		assert.Equal(t, "key", r.Header.Get("X-Api-Key"))

		writeJSON(t, w, http.StatusOK, `{"ErrorCode": 1, "Response": {"characterActivities": {"data": {
			"c1": {"dateActivityStarted": "2026-11-10T11:00:00Z", "currentActivityHash": 0, "availableActivities": [
				{"activityHash": 10, "isVisible": false, "isNew": true, "recommendedLight": 2010,
				 "isFocusedActivity": true, "challenges": [], "visibleRewards": [
					{"displayBehavior": 1, "rewardItems": [
						{"uiStyle": "extra_engram", "itemQuantity": {"itemHash": 3956025454, "quantity": 2,
						 "hasConditionalVisibility": false}},
						{"itemQuantity": {"itemHash": 600, "quantity": 1}}
					]},
					{"rewardItems": [{"uiStyle": "daily_grind_guaranteed", "itemQuantity": {"itemHash": 601, "quantity": 1}}]}
				]},
				{"activityHash": 11, "recommendedLight": null},
				{"activityHash": 12, "isVisible": true}
			]}
		}}}}`)
	}))
	t.Cleanup(srv.Close)

	b := NewBungie(WithBungieURL(srv.URL))
	b.now = func() time.Time { return fixtureNow }

	got, err := b.Activities(
		t.Context(),
		Credentials{APIKey: "key", AccessToken: "access"},
		Membership{ID: "4611", Type: 3},
	)
	require.NoError(t, err)

	calls := rec.calls(http.MethodGet, "/Destiny2/3/Profile/4611/")
	require.Len(t, calls, 1)
	assert.Equal(t, "components=204&_="+strconv.FormatInt(fixtureNow.UnixMilli(), 10), calls[0].Query)
	assert.Equal(t, "Bearer access", calls[0].Auth)

	reward := func(style string, hash uint32, quantity int) ActivityRewardItem {
		item := ActivityRewardItem{UIStyle: style}
		item.ItemQuantity.ItemHash = hash
		item.ItemQuantity.Quantity = quantity

		return item
	}

	require.Len(t, got, 1)

	// A missing isVisible decodes as visible.
	activities := got[hunterID].Available
	assert.Equal(t, []AvailableActivity{
		{
			Hash:             10,
			RecommendedLight: 2010,
			Focused:          true,
			VisibleRewards: []ActivityRewardMapping{
				{RewardItems: []ActivityRewardItem{reward(rewardBonusDrop, 3956025454, 2), reward("", 600, 1)}},
				{RewardItems: []ActivityRewardItem{reward(rewardFocus, 601, 1)}},
			},
		},
		{Hash: 11, Visible: true},
		{Hash: 12, Visible: true},
	}, activities)

	assert.Equal(t, []ActivityRewardItem{
		reward(rewardBonusDrop, 3956025454, 2),
		reward("", 600, 1),
		reward(rewardFocus, 601, 1),
	}, activities[0].RewardItems())
	assert.Empty(t, activities[1].RewardItems())
}

func TestBungieActivitiesErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body string
		err  error
	}{
		"Bungie refuses": {
			body: `{"ErrorCode": 1665, "ErrorStatus": "DestinyPrivacyRestriction", "Message": "Private."}`,
			err:  ErrBungie,
		},
		"activity is not an object": {
			body: `{"ErrorCode": 1, "Response": {"characterActivities": {"data": {"c1": {"availableActivities": [1]}}}}}`,
			err:  ErrBungie,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			u, _ := serve(t, http.StatusOK, tc.body)

			_, err := NewBungie(WithBungieURL(u)).
				Activities(t.Context(), Credentials{APIKey: "key", AccessToken: "access"}, Membership{ID: "4611", Type: 3})
			require.ErrorIs(t, err, tc.err)
		})
	}
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
	require.NoError(t, b.InsertSocketPlugFree(t.Context(), creds, action, 3, 903))
	require.NoError(t, b.EquipLoadout(t.Context(), creds, LoadoutAction{
		CharacterID: hunterID,
		Membership:  action.Membership,
		Index:       2,
	}))
	require.NoError(t, b.SnapshotLoadout(t.Context(), creds, LoadoutAction{
		CharacterID: hunterID,
		Membership:  action.Membership,
		Index:       1,
	}, LoadoutIdentifiers{NameHash: 4, IconHash: 20, ColorHash: 31}))
	require.NoError(t, b.ClearLoadout(t.Context(), creds, LoadoutAction{
		CharacterID: titanID,
		Membership:  action.Membership,
		Index:       0,
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

	inserts := rec.calls(http.MethodPost, "/Destiny2/Actions/Items/InsertSocketPlugFree/")
	require.Len(t, inserts, 1)
	assert.Equal(t, map[string]any{
		"plug": map[string]any{
			"socketIndex":     float64(3),
			"socketArrayType": float64(0),
			"plugItemHash":    float64(903),
		},
		"itemId":         "1001",
		"characterId":    hunterID,
		"membershipType": float64(3),
	}, inserts[0].Body)

	loadouts := rec.calls(http.MethodPost, "/Destiny2/Actions/Loadouts/EquipLoadout/")
	require.Len(t, loadouts, 1)
	assert.Equal(t, map[string]any{
		"loadoutIndex":   float64(2),
		"characterId":    hunterID,
		"membershipType": float64(3),
	}, loadouts[0].Body)

	snapshots := rec.calls(http.MethodPost, "/Destiny2/Actions/Loadouts/SnapshotLoadout/")
	require.Len(t, snapshots, 1)
	assert.Equal(t, map[string]any{
		"loadoutIndex":   float64(1),
		"characterId":    hunterID,
		"membershipType": float64(3),
		"nameHash":       float64(4),
		"iconHash":       float64(20),
		"colorHash":      float64(31),
	}, snapshots[0].Body)

	clears := rec.calls(http.MethodPost, "/Destiny2/Actions/Loadouts/ClearLoadout/")
	require.Len(t, clears, 1)
	assert.Equal(t, map[string]any{
		"loadoutIndex":   float64(0),
		"characterId":    titanID,
		"membershipType": float64(3),
	}, clears[0].Body)
}
