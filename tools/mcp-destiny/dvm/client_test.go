package dvm_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvmtest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/httpfake"
)

func TestServiceConfig(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err    error
		body   string
		want   dvm.Config
		status int
	}{
		"config": {
			status: http.StatusOK,
			body: `{"apiKey": "key", "clientId": "54520", "authOrigin": "https://example.test",
				"artifacts": {"manifestVersion": "v1", "files": {"core": ["core-0.json"]}}}`,
			want: dvm.Config{
				APIKey:     "key",
				ClientID:   "54520",
				AuthOrigin: "https://example.test",
				Artifacts: dvm.ArtifactIndex{
					ManifestVersion: "v1",
					Files:           map[string][]string{"core": {"core-0.json"}},
				},
			},
		},
		"missing credentials": {
			status: http.StatusOK,
			body:   `{"apiKey": "", "clientId": ""}`,
			err:    dvm.ErrService,
		},
		"server error": {
			status: http.StatusInternalServerError,
			body:   `{"error": "boom"}`,
			err:    dvm.ErrService,
		},
		"not JSON": {
			status: http.StatusOK,
			body:   `<html>`,
			err:    dvm.ErrService,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			u, _ := httpfake.Serve(t, dvm.DefaultUserAgent, tc.status, tc.body)

			got, err := dvm.New(dvm.WithURL(u)).Config(t.Context())
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

	u, rec := httpfake.Serve(
		t,
		dvm.DefaultUserAgent,
		http.StatusOK,
		`{"access_token": "a", "expires_in": 3600, "refresh_token": "r",
		"refresh_expires_in": 7776000, "membership_id": "42", "session": "s"}`,
	)
	s := dvm.New(dvm.WithURL(u))

	want := dvm.TokenResponse{
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

	exchange := rec.Calls(http.MethodPost, "/api/auth/token")
	require.Len(t, exchange, 1)
	assert.Equal(t, map[string]any{"code": "abc"}, exchange[0].Body)
	assert.Empty(t, exchange[0].Auth)

	refresh := rec.Calls(http.MethodPost, "/api/auth/refresh")
	require.Len(t, refresh, 1)
	assert.Equal(t, map[string]any{"refreshToken": "r0"}, refresh[0].Body)
}

func TestServiceWrites(t *testing.T) {
	t.Parallel()

	u, rec := httpfake.Serve(t, dvm.DefaultUserAgent, http.StatusOK, `{"now": 11}`)
	s := dvm.New(dvm.WithURL(u))
	hash := uint32(100)

	require.NoError(t, s.WriteTags(t.Context(), "session", []dvm.ItemTag{
		{InstanceID: "1001", ItemHash: 100, TagID: "keep"},
		{InstanceID: "1001", ItemHash: 100, TagID: "junk", RuleID: "r1", Removed: true},
	}))
	require.NoError(t, s.WriteNote(t.Context(), "session", dvm.Note{
		Kind: dvm.NoteInstance, TargetID: "1001", ItemHash: &hash, Body: "god roll",
	}))
	require.NoError(
		t,
		s.WriteNote(t.Context(), "session", dvm.Note{Kind: dvm.NoteSet, TargetID: "set-key", Removed: true}),
	)
	require.NoError(t, s.WritePerkPreferences(t.Context(), "session", []dvm.PerkPreference{
		{Mode: "pve", Perk: "Firefly", Stance: "up"},
	}))
	require.NoError(t, s.WriteAutotagRules(t.Context(), "session", []dvm.AutotagRule{
		{ID: "r1", Query: "is:dupe", Vault: true, Enabled: true, Position: 2},
	}))

	// The service rejects a row with a field it does not expect to be
	// empty, so the bodies must match its parsers field for field.
	tags := rec.Calls(http.MethodPost, "/api/tags")
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

	notes := rec.Calls(http.MethodPost, "/api/notes")
	require.Len(t, notes, 2)
	assert.Equal(t, map[string]any{
		"kind": "instance", "targetId": "1001", "itemHash": float64(100), "body": "god roll", "removed": false,
	}, notes[0].Body)
	assert.Equal(t, map[string]any{"kind": "set", "targetId": "set-key", "body": "", "removed": true}, notes[1].Body)

	prefs := rec.Calls(http.MethodPost, "/api/perk-preferences")
	require.Len(t, prefs, 1)
	assert.Equal(t, map[string]any{"preferences": []any{
		map[string]any{"mode": "pve", "perk": "Firefly", "stance": "up", "removed": false},
	}}, prefs[0].Body)

	rules := rec.Calls(http.MethodPost, "/api/autotag")
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
			err:    dvm.ErrService,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			u, rec := httpfake.Serve(t, dvm.DefaultUserAgent, tc.status, tc.body)

			got, err := dvm.New(dvm.WithURL(u)).ClaimAutotag(
				t.Context(), "session", "00000000000000001006", "00000000000000001009",
			)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				assert.Contains(t, err.Error(), "Bad autotag state")
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tc.want, got)

			claims := rec.Calls(http.MethodPost, "/api/autotag/state")
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

			u, _ := httpfake.Serve(t, dvm.DefaultUserAgent, http.StatusInternalServerError, tc.body)

			_, err := dvm.New(dvm.WithURL(u)).RefreshTokens(t.Context(), "revoked")
			require.ErrorIs(t, err, dvm.ErrService)

			if tc.rejected {
				require.ErrorIs(t, err, dvm.ErrGrantRejected)
			} else {
				require.NotErrorIs(t, err, dvm.ErrGrantRejected)
			}
		})
	}
}

func TestServiceUnauthorized(t *testing.T) {
	t.Parallel()

	u, _ := httpfake.Serve(t, dvm.DefaultUserAgent, http.StatusUnauthorized, `{"error": "Sign in required"}`)

	_, err := dvm.New(dvm.WithURL(u)).Tags(t.Context(), "expired")
	require.ErrorIs(t, err, dvm.ErrUnauthorized)
	require.NotErrorIs(t, err, dvm.ErrService)
	assert.Contains(t, err.Error(), "Sign in required")
}

func TestServiceArtifact(t *testing.T) {
	t.Parallel()

	u, rec := httpfake.Serve(t, dvm.DefaultUserAgent, http.StatusOK, `[1, 2]`)

	body, err := dvm.New(dvm.WithURL(u)).Artifact(t.Context(), "core-0.json")
	require.NoError(t, err)

	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())
	assert.JSONEq(t, `[1, 2]`, string(raw))
	assert.Len(t, rec.Calls(http.MethodGet, "/artifacts/core-0.json"), 1)

	missing, _ := httpfake.Serve(t, dvm.DefaultUserAgent, http.StatusNotFound, `{}`)

	_, err = dvm.New(dvm.WithURL(missing)).Artifact(t.Context(), "core-0.json")
	require.ErrorIs(t, err, dvm.ErrService)

	rejected, _ := httpfake.Serve(t, dvm.DefaultUserAgent, http.StatusUnauthorized, `{}`)

	_, err = dvm.New(dvm.WithURL(rejected)).Artifact(t.Context(), "core-0.json")
	require.ErrorIs(t, err, dvm.ErrService)
	require.NotErrorIs(t, err, dvm.ErrUnauthorized)
}

func TestServiceStats(t *testing.T) {
	t.Parallel()

	// The values match bungietest.HunterID, bungietest.ItemGlimmer, and
	// bungie.BucketKinetic. The tests of package dvm import neither package.
	const (
		hunterID             = "c1"
		itemGlimmer   uint32 = 3159615086
		bucketKinetic uint32 = 1498876634
	)

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
		"kind":        "item",
		"instanceId":  "1001",
		"itemHash":    100,
		"characterId": hunterID,
		"bucketHash":  bucketKinetic,
		"added":       true,
		"from":        285_000,
		"at":          300_000,
		"snapshot":    map[string]any{"item": map[string]any{"itemHash": 100}, "components": map[string]any{}},
	}

	first := dvmtest.Chunk{
		StartsAt:  300_000,
		UpdatedAt: 900_000,
		Changes:   []map[string]any{glimmer(310_000, 1_000, 2_000)},
	}
	second := dvmtest.Chunk{
		StartsAt:  600_000,
		UpdatedAt: 950_000,
		Changes:   []map[string]any{glimmer(610_000, 2_000, 1_500)},
	}
	// A second recorder replaced the first bucket with a longer copy.
	replaced := dvmtest.Chunk{
		StartsAt:  300_000,
		UpdatedAt: 1_200_000,
		Changes:   []map[string]any{glimmer(310_000, 1_000, 2_000), drop},
	}

	// reordered encodes a page whose more key comes before its chunks and
	// whose other keys the client skips.
	reordered := func(chunks ...dvmtest.Chunk) string {
		var page struct {
			Chunks json.RawMessage `json:"chunks"`
		}

		require.NoError(t, json.Unmarshal([]byte(dvmtest.StatPage(t, false, chunks...)), &page))

		return `{"now": 5, "more": false, "extra": {"chunks": [], "more": true}, "chunks": ` +
			string(page.Chunks) + `, "later": [1, 2]}`
	}

	tests := map[string]struct {
		pages       map[string]string
		wantQueries []string
		want        []dvm.StatChunk
	}{
		"one page": {
			pages: map[string]string{
				"since=0&after=0": dvmtest.StatPage(t, false, first, second),
			},
			wantQueries: []string{"since=0&after=0"},
			want: []dvm.StatChunk{
				{StartsAt: 300_000, EndsAt: 600_000, UpdatedAt: 900_000, Changes: []dvm.StatChange{
					{
						Kind:     "currency",
						ItemHash: itemGlimmer,
						From:     295_000,
						At:       310_000,
						Before:   1_000,
						After:    2_000,
					},
				}},
				{StartsAt: 600_000, EndsAt: 900_000, UpdatedAt: 950_000, Changes: []dvm.StatChange{
					{
						Kind:     "currency",
						ItemHash: itemGlimmer,
						From:     595_000,
						At:       610_000,
						Before:   2_000,
						After:    1_500,
					},
				}},
			},
		},
		"keys in another order and unknown keys": {
			pages:       map[string]string{"since=0&after=0": reordered(first)},
			wantQueries: []string{"since=0&after=0"},
			want: []dvm.StatChunk{
				{StartsAt: 300_000, EndsAt: 600_000, UpdatedAt: 900_000, Changes: []dvm.StatChange{
					{
						Kind:     "currency",
						ItemHash: itemGlimmer,
						From:     295_000,
						At:       310_000,
						Before:   1_000,
						After:    2_000,
					},
				}},
			},
		},
		"the later copy of a bucket replaces the earlier one": {
			pages: map[string]string{
				"since=0&after=0":           dvmtest.StatPage(t, true, first, second),
				"since=950000&after=600000": dvmtest.StatPage(t, false, replaced),
			},
			wantQueries: []string{"since=0&after=0", "since=950000&after=600000"},
			want: []dvm.StatChunk{
				{StartsAt: 300_000, EndsAt: 600_000, UpdatedAt: 1_200_000, Changes: []dvm.StatChange{
					{
						Kind:     "currency",
						ItemHash: itemGlimmer,
						From:     295_000,
						At:       310_000,
						Before:   1_000,
						After:    2_000,
					},
					{Kind: "item", InstanceID: "1001", ItemHash: 100, From: 285_000, At: 300_000},
				}},
				{StartsAt: 600_000, EndsAt: 900_000, UpdatedAt: 950_000, Changes: []dvm.StatChange{
					{
						Kind:     "currency",
						ItemHash: itemGlimmer,
						From:     595_000,
						At:       610_000,
						Before:   2_000,
						After:    1_500,
					},
				}},
			},
		},
		"an empty page ends the read": {
			pages: map[string]string{
				"since=0&after=0":           dvmtest.StatPage(t, true, first),
				"since=900000&after=300000": dvmtest.StatPage(t, true),
			},
			wantQueries: []string{"since=0&after=0", "since=900000&after=300000"},
			want: []dvm.StatChunk{
				{StartsAt: 300_000, EndsAt: 600_000, UpdatedAt: 900_000, Changes: []dvm.StatChange{
					{
						Kind:     "currency",
						ItemHash: itemGlimmer,
						From:     295_000,
						At:       310_000,
						Before:   1_000,
						After:    2_000,
					},
				}},
			},
		},
		"no chunks": {
			pages:       map[string]string{"since=0&after=0": dvmtest.StatPage(t, false)},
			wantQueries: []string{"since=0&after=0"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := &httpfake.Recorder{}

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				req := rec.Record(t, r)
				assert.Equal(t, http.MethodGet, req.Method)
				assert.Equal(t, "/api/stats", req.Path)
				assert.Equal(t, "Bearer session", req.Auth)

				query := r.URL.Query()

				page, ok := tc.pages["since="+query.Get("since")+"&after="+query.Get("after")]
				if !ok {
					httpfake.WriteJSON(t, w, http.StatusNotFound, `{"error": "unexpected cursor"}`)

					return
				}

				httpfake.WriteJSON(t, w, http.StatusOK, page)
			}))
			t.Cleanup(srv.Close)

			got, err := dvm.New(dvm.WithURL(srv.URL)).Stats(t.Context(), "session", dvm.StatCursor{})
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)

			var queries []string
			for _, req := range rec.All() {
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
	body := strconv.Quote("[" + strings.Repeat(" ", dvm.MaxStatChunkLength-2) + "]")
	count := dvm.MaxResponseBytes/dvm.MaxStatChunkLength + 1

	chunks := make([]string, 0, count)

	for i := range int64(count) {
		startsAt := i * 300_000
		chunks = append(chunks, fmt.Sprintf(
			`{"startsAt": %d, "endsAt": %d, "updatedAt": %d, "body": %s}`,
			startsAt, startsAt+300_000, i+1, body,
		))
	}

	page := `{"chunks": [` + strings.Join(chunks, ", ") + `], "more": false}`
	require.Greater(t, len(page), dvm.MaxResponseBytes)

	u, _ := httpfake.Serve(t, dvm.DefaultUserAgent, http.StatusOK, page)

	got, err := dvm.New(dvm.WithURL(u)).Stats(t.Context(), "session", dvm.StatCursor{})
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

			u, _ := httpfake.Serve(t, dvm.DefaultUserAgent, tc.status, tc.body)

			_, err := dvm.New(dvm.WithURL(u)).Stats(t.Context(), "session", dvm.StatCursor{})
			require.ErrorIs(t, err, dvm.ErrService)
		})
	}
}

func TestUserAgent(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		want string
		opts []dvm.Option
	}{
		"default": {
			want: dvm.DefaultUserAgent,
		},
		"option": {
			opts: []dvm.Option{dvm.WithUserAgent("MCP-Destiny/9.9.9")},
			want: "MCP-Destiny/9.9.9",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			u, rec := httpfake.Serve(t, tc.want, http.StatusOK, `{"items": []}`)
			s := dvm.New(append([]dvm.Option{dvm.WithURL(u)}, tc.opts...)...)

			_, err := s.Tags(t.Context(), "session")
			require.NoError(t, err)

			body, err := s.Artifact(t.Context(), "core-0.json")
			require.NoError(t, err)
			require.NoError(t, body.Close())

			assert.Len(t, rec.All(), 2)
		})
	}
}
