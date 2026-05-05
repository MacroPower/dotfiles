package main

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheHit(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		mustWriteString(
			w,
			`[{"title":"hashicorp/aws","description":"AWS","type":"provider","version":"v5.0.0","link_variables":{"namespace":"hashicorp","name":"aws","target_system":"","id":""}}]`,
		)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(WithBaseURL(srv.URL))
	for range 3 {
		items, err := c.Search(t.Context(), "aws")
		require.NoError(t, err)
		require.Len(t, items, 1)
	}

	assert.Equal(t, int32(1), hits.Load(), "second and third calls should hit the cache")
}

func TestCacheTTLExpiry(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		mustWriteString(w, `[]`)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(
		WithBaseURL(srv.URL),
		WithCache(expirable.NewLRU[string, []byte](16, nil, time.Millisecond)),
	)

	_, err := c.Search(t.Context(), "aws")
	require.NoError(t, err)

	// Sleep past the 1ms TTL so the entry is evicted before the next lookup.
	time.Sleep(20 * time.Millisecond)

	_, err = c.Search(t.Context(), "aws")
	require.NoError(t, err)
	assert.Equal(t, int32(2), hits.Load(), "expired entry should not satisfy the second call")
}

func TestCacheErrorsNotCached(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		handler http.HandlerFunc
	}{
		"500 status not cached": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
		},
		"404 with body not cached": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				mustWriteString(w, `{"error":"not found"}`)
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var hits atomic.Int32

			handler := tt.handler
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				handler(w, r)
			}))
			t.Cleanup(srv.Close)

			c := NewClient(WithBaseURL(srv.URL))
			_, err1 := c.Provider(t.Context(), "hashicorp", "aws")
			require.Error(t, err1)

			_, err2 := c.Provider(t.Context(), "hashicorp", "aws")
			require.Error(t, err2)

			assert.Equal(t, int32(2), hits.Load(), "non-2xx responses must reach upstream every time")
			assert.Equal(t, 0, c.responseCache.Len(), "no entry should be left in the cache")
		})
	}
}

func TestCacheEmptyBody(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/markdown")
		// Empty 200 body.
	}))
	t.Cleanup(srv.Close)

	c := NewClient(WithBaseURL(srv.URL))

	for range 2 {
		body, err := c.ResourceDocs(t.Context(), "hashicorp", "aws", "v5.0.0", "s3_bucket")
		require.NoError(t, err)
		assert.Empty(t, body)
	}

	assert.Equal(t, int32(1), hits.Load(), "empty 200 body should still populate the cache")
}

func TestCacheConcurrent(t *testing.T) {
	t.Parallel()

	const goroutines = 32

	want := Provider{
		Addr:        Addr{Display: "hashicorp/terraform-provider-aws", Namespace: "hashicorp", Name: "aws"},
		Description: "AWS",
		Versions:    []Version{{ID: "v5.0.0"}, {ID: "v4.0.0"}},
		Popularity:  1,
		Link:        "",
	}
	body := `{"addr":{"display":"hashicorp/terraform-provider-aws","namespace":"hashicorp","name":"aws"},"description":"AWS","versions":[{"id":"v5.0.0"},{"id":"v4.0.0"}],"popularity":1,"link":""}`

	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		mustWriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(WithBaseURL(srv.URL))

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()

			p, err := c.Provider(t.Context(), "hashicorp", "aws")
			assert.NoError(t, err)
			assert.Equal(t, want, p, "every caller sees the full, intact decoded body")
		}()
	}

	wg.Wait()

	got := hits.Load()
	assert.GreaterOrEqual(t, got, int32(1), "at least one upstream fetch is required")
	assert.LessOrEqual(t, got, int32(goroutines), "races may double-fetch but never exceed the goroutine count")
	assert.Equal(t, 1, c.responseCache.Len(), "concurrent writes must converge on a single entry")
}

func TestCacheTypeFilterShare(t *testing.T) {
	t.Parallel()

	// Type filtering happens client-side in renderSearch, so the upstream
	// search URL is identical for every Type value. Two handleSearch calls
	// with the same Query but different Type therefore share one cache
	// entry and one upstream fetch.
	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		assert.Equal(t, "/registry/docs/search", r.URL.Path)
		assert.Equal(t, "aws", r.URL.Query().Get("q"))
		w.Header().Set("Content-Type", "application/json")
		mustWriteString(w, `[
			{"title":"hashicorp/aws","description":"AWS","type":"provider","version":"v5.0.0","link_variables":{"namespace":"hashicorp","name":"aws","target_system":"","id":""}},
			{"title":"terraform-aws-modules/vpc","description":"VPC","type":"module","version":"v5.0.0","link_variables":{"namespace":"terraform-aws-modules","name":"vpc","target_system":"aws","id":""}}
		]`)
	}))
	t.Cleanup(srv.Close)

	h := &handler{
		client: NewClient(WithBaseURL(srv.URL), WithHTTPClient(srv.Client())),
		log:    slog.New(slog.DiscardHandler),
	}

	rProv, _, err := h.handleSearch(t.Context(), nil, SearchInput{Query: "aws", Type: "provider"})
	require.NoError(t, err)

	rMod, _, err := h.handleSearch(t.Context(), nil, SearchInput{Query: "aws", Type: "module"})
	require.NoError(t, err)

	provText := resultText(t, rProv)
	modText := resultText(t, rMod)
	assert.Contains(t, provText, "Provider: hashicorp/aws")
	assert.NotContains(t, provText, "Module:")
	assert.Contains(t, modText, "Module: terraform-aws-modules/vpc")
	assert.NotContains(t, modText, "Provider:")
	assert.Equal(t, int32(1), hits.Load(), "different Type filters must share one upstream fetch")
}

func TestWithCacheNilIgnored(t *testing.T) {
	t.Parallel()

	c := NewClient(WithCache(nil))
	require.NotNil(t, c.responseCache, "WithCache(nil) must not clobber the default cache")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustWriteString(w, `[]`)
	}))
	t.Cleanup(srv.Close)

	c = NewClient(WithBaseURL(srv.URL), WithCache(nil))
	_, err := c.Search(t.Context(), "aws")
	require.NoError(t, err, "fetch must not panic when WithCache(nil) was passed")
}

func TestCache3xxNotCached(t *testing.T) {
	t.Parallel()

	// Some 3xx responses (304 Not Modified, redirects with no Location)
	// surface to user code without being followed by net/http. They must
	// not be treated as cacheable successes.
	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNotModified)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(WithBaseURL(srv.URL))
	_, err1 := c.Provider(t.Context(), "hashicorp", "aws")
	require.Error(t, err1)
	require.ErrorIs(t, err1, ErrRegistry)

	_, err2 := c.Provider(t.Context(), "hashicorp", "aws")
	require.Error(t, err2)

	assert.Equal(t, int32(2), hits.Load(), "3xx responses must reach upstream every time")
	assert.Equal(t, 0, c.responseCache.Len(), "no entry should be left in the cache")
}

func TestCacheKeyEncodingNormalization(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"path-only":         "/registry/docs/providers/hashicorp/aws/index.json",
		"plus-space":        "/registry/docs/search?q=aws+iam",
		"percent-20-space":  "/registry/docs/search?q=aws%20iam",
		"reordered-params":  "/registry/docs/search?b=2&a=1",
		"reordered-reverse": "/registry/docs/search?a=1&b=2",
	}

	keys := map[string]string{}
	for name, raw := range tests {
		got, err := cacheKey("https://api.opentofu.org" + raw)
		require.NoError(t, err)

		keys[name] = got
	}

	assert.Equal(t, keys["plus-space"], keys["percent-20-space"],
		"`+` and `%%20` encodings of a space must collapse to one key")
	assert.Equal(t, keys["reordered-params"], keys["reordered-reverse"],
		"parameter order must not affect the key")
	assert.NotEqual(t, keys["plus-space"], keys["path-only"],
		"distinct queries must yield distinct keys")
}

func TestCacheCaseSensitivity(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		mustWriteString(w, `[]`)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(WithBaseURL(srv.URL))
	_, err := c.Search(t.Context(), "AWS")
	require.NoError(t, err)

	_, err = c.Search(t.Context(), "aws")
	require.NoError(t, err)

	assert.Equal(t, int32(2), hits.Load(), "case variants are intentionally not collapsed")
}
