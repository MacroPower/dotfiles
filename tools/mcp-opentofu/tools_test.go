package main

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureServer wires the registry endpoints exercised across tools_test.go.
// Every endpoint returns a small fixed JSON body or Markdown body that the
// renderers and handlers know how to consume.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("/registry/docs/search", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustWriteString(w, `[
			{"title":"hashicorp/aws","description":"AWS provider","type":"provider","version":"v5.0.0","link_variables":{"namespace":"hashicorp","name":"aws","target_system":"","id":""}},
			{"title":"terraform-aws-modules/vpc","description":"VPC module","type":"module","version":"v5.1.2","link_variables":{"namespace":"terraform-aws-modules","name":"vpc","target_system":"aws","id":""}}
		]`)
	})

	mux.HandleFunc("/registry/docs/providers/hashicorp/aws/index.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustWriteString(w, `{
			"addr": {"display":"hashicorp/terraform-provider-aws","namespace":"hashicorp","name":"aws"},
			"description": "AWS provider",
			"versions": [{"id":"v5.0.0"},{"id":"v4.0.0"}],
			"popularity": 1234,
			"link": ""
		}`)
	})

	mux.HandleFunc(
		"/registry/docs/providers/hashicorp/aws/v5.0.0/index.json",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			mustWriteString(
				w,
				`{"id":"v5.0.0","docs":{"resources":[{"name":"s3_bucket","description":"Manages an S3 bucket"}],"datasources":[{"name":"ami","description":"Look up AMIs"}]}}`,
			)
		},
	)

	mux.HandleFunc(
		"/registry/docs/providers/hashicorp/aws/v5.0.0/resources/s3_bucket.md",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/markdown")
			mustWriteString(w, "# aws_s3_bucket\nDocs body.")
		},
	)

	mux.HandleFunc(
		"/registry/docs/providers/hashicorp/aws/v5.0.0/datasources/ami.md",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/markdown")
			mustWriteString(w, "# aws_ami\nData source body.")
		},
	)

	mux.HandleFunc(
		"/registry/docs/modules/terraform-aws-modules/vpc/aws/index.json",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			mustWriteString(w, `{
			"addr": {"display":"terraform-aws-modules/terraform-aws-vpc/aws","namespace":"terraform-aws-modules","name":"vpc","target":"aws"},
			"description": "VPC module",
			"versions": [{"id":"v5.0.0"}],
			"popularity": 99,
			"fork_of": {"display":"//","namespace":"","name":"","target":""},
			"fork_count": 0
		}`)
		},
	)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

func newTestHandler(t *testing.T, srv *httptest.Server) *handler {
	t.Helper()

	return &handler{
		client: NewClient(WithBaseURL(srv.URL), WithHTTPClient(srv.Client())),
		log:    slog.New(slog.DiscardHandler),
	}
}

func TestHandleSearch(t *testing.T) {
	t.Parallel()

	srv := fixtureServer(t)
	h := newTestHandler(t, srv)

	r, _, err := h.handleSearch(t.Context(), nil, SearchInput{Query: "aws"})
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.False(t, r.IsError)

	text := resultText(t, r)
	assert.Contains(t, text, "Found 2 results")
	assert.Contains(t, text, "Provider: hashicorp/aws")
	assert.Contains(t, text, "Module: terraform-aws-modules/vpc (aws)")
}

func TestHandleSearchTypeFilter(t *testing.T) {
	t.Parallel()

	srv := fixtureServer(t)
	h := newTestHandler(t, srv)

	r, _, err := h.handleSearch(t.Context(), nil, SearchInput{Query: "aws", Type: "provider"})
	require.NoError(t, err)

	text := resultText(t, r)
	assert.Contains(t, text, "Found 1 results")
	assert.NotContains(t, text, "Module:")
}

func TestHandleProviderDetails(t *testing.T) {
	t.Parallel()

	srv := fixtureServer(t)
	h := newTestHandler(t, srv)

	r, _, err := h.handleProviderDetails(t.Context(), nil, ProviderDetailsInput{Namespace: "hashicorp", Name: "aws"})
	require.NoError(t, err)
	require.False(t, r.IsError)

	text := resultText(t, r)
	assert.Contains(t, text, "## Provider: hashicorp/aws")
	assert.Contains(t, text, "**Latest Version**: v5.0.0")
	assert.Contains(t, text, "### Resources (1)")
	assert.Contains(t, text, "- s3_bucket: Manages an S3 bucket")
}

func TestHandleProviderDetailsNotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	h := &handler{
		client: NewClient(WithBaseURL(srv.URL), WithHTTPClient(srv.Client())),
		log:    slog.New(slog.DiscardHandler),
	}

	r, _, err := h.handleProviderDetails(t.Context(), nil, ProviderDetailsInput{Namespace: "nope", Name: "missing"})
	require.NoError(t, err)
	require.True(t, r.IsError)
	assert.Contains(t, resultText(t, r), "getting details for provider nope/missing")
}

func TestHandleModuleDetails(t *testing.T) {
	t.Parallel()

	srv := fixtureServer(t)
	h := newTestHandler(t, srv)

	r, _, err := h.handleModuleDetails(t.Context(), nil, ModuleDetailsInput{
		Namespace: "terraform-aws-modules", Name: "vpc", Target: "aws",
	})
	require.NoError(t, err)
	require.False(t, r.IsError)

	text := resultText(t, r)
	assert.Contains(t, text, "## Module: terraform-aws-modules/vpc/aws")
	assert.Contains(t, text, "**Popularity Score**: 99")
}

func TestHandleResourceDocs(t *testing.T) {
	t.Parallel()

	srv := fixtureServer(t)
	h := newTestHandler(t, srv)

	tests := map[string]struct {
		in   ResourceDocsInput
		want string
	}{
		"explicit version": {
			in:   ResourceDocsInput{Namespace: "hashicorp", Name: "aws", Resource: "s3_bucket", Version: "v5.0.0"},
			want: "# aws_s3_bucket",
		},
		"latest resolved from index": {
			in:   ResourceDocsInput{Namespace: "hashicorp", Name: "aws", Resource: "s3_bucket"},
			want: "# aws_s3_bucket",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r, _, err := h.handleResourceDocs(t.Context(), nil, tt.in)
			require.NoError(t, err)
			require.False(t, r.IsError, resultText(t, r))
			assert.Contains(t, resultText(t, r), tt.want)
		})
	}
}

func TestHandleDatasourceDocs(t *testing.T) {
	t.Parallel()

	srv := fixtureServer(t)
	h := newTestHandler(t, srv)

	r, _, err := h.handleDatasourceDocs(t.Context(), nil, DataSourceDocsInput{
		Namespace: "hashicorp", Name: "aws", DataSource: "ami", Version: "v5.0.0",
	})
	require.NoError(t, err)
	require.False(t, r.IsError, resultText(t, r))
	assert.Contains(t, resultText(t, r), "# aws_ami")
}

func TestHandleResourceDocsNotFound(t *testing.T) {
	t.Parallel()

	srv := fixtureServer(t)
	h := newTestHandler(t, srv)

	r, _, err := h.handleResourceDocs(t.Context(), nil, ResourceDocsInput{
		Namespace: "hashicorp", Name: "aws", Resource: "missing", Version: "v5.0.0",
	})
	require.NoError(t, err)
	require.True(t, r.IsError)
	assert.Contains(t, resultText(t, r), "getting documentation for resource aws_missing")
}

func TestAddTool(t *testing.T) {
	t.Parallel()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.0.0"}, nil)
	h := &handler{client: NewClient(), log: slog.New(slog.DiscardHandler)}

	require.NotPanics(t, func() {
		mcp.AddTool(srv, &mcp.Tool{Name: "search_registry", Description: "x"}, h.handleSearch)
		mcp.AddTool(srv, &mcp.Tool{Name: "get_provider_details", Description: "x"}, h.handleProviderDetails)
		mcp.AddTool(srv, &mcp.Tool{Name: "get_module_details", Description: "x"}, h.handleModuleDetails)
		mcp.AddTool(srv, &mcp.Tool{Name: "get_resource_docs", Description: "x"}, h.handleResourceDocs)
		mcp.AddTool(srv, &mcp.Tool{Name: "get_datasource_docs", Description: "x"}, h.handleDatasourceDocs)
		mcp.AddTool(srv, &mcp.Tool{Name: "validate", Description: "x"}, h.handleValidate)
		mcp.AddTool(srv, &mcp.Tool{Name: "init", Description: "x"}, h.handleInit)
		mcp.AddTool(srv, &mcp.Tool{Name: "plan", Description: "x"}, h.handlePlan)
	})
}

// TestPaginationDefaultTruncates exercises the default behavior: when a
// caller omits MaxLength and StartIndex, a body longer than
// defaultMaxLength returns the first defaultMaxLength runes plus the
// continuation marker.
func TestPaginationDefaultTruncates(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("a", defaultMaxLength*2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		mustWriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	h := &handler{
		client: NewClient(WithBaseURL(srv.URL), WithHTTPClient(srv.Client())),
		log:    slog.New(slog.DiscardHandler),
	}

	r, _, err := h.handleResourceDocs(t.Context(), nil, ResourceDocsInput{
		Namespace: "hashicorp", Name: "aws", Resource: "s3_bucket", Version: "v5.0.0",
	})
	require.NoError(t, err)
	require.False(t, r.IsError)

	text := resultText(t, r)
	prefix := strings.Repeat("a", defaultMaxLength)
	assert.True(t, strings.HasPrefix(text, prefix),
		"default slice must contain the first %d runes verbatim", defaultMaxLength)
	assert.Contains(t, text, "Use start_index=5000 to continue reading",
		"continuation marker must point at the end of the first slice")
}

// TestPaginationCacheReuse asserts pagination operates on the cached
// upstream body: two handler calls with different StartIndex values for
// the same upstream path issue exactly one HTTP request, with the second
// slice served entirely from the response cache.
func TestPaginationCacheReuse(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("z", defaultMaxLength+200) // forces a second slice

	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/markdown")
		mustWriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	h := &handler{
		client: NewClient(WithBaseURL(srv.URL), WithHTTPClient(srv.Client())),
		log:    slog.New(slog.DiscardHandler),
	}

	r1, _, err := h.handleResourceDocs(t.Context(), nil, ResourceDocsInput{
		Namespace: "hashicorp", Name: "aws", Resource: "s3_bucket", Version: "v5.0.0",
	})
	require.NoError(t, err)
	require.False(t, r1.IsError)

	first := resultText(t, r1)
	require.Contains(t, first, "Use start_index=5000 to continue reading")

	r2, _, err := h.handleResourceDocs(t.Context(), nil, ResourceDocsInput{
		Namespace: "hashicorp", Name: "aws", Resource: "s3_bucket", Version: "v5.0.0",
		StartIndex: defaultMaxLength,
	})
	require.NoError(t, err)
	require.False(t, r2.IsError)

	second := resultText(t, r2)
	assert.Equal(t, strings.Repeat("z", 200), second,
		"second slice must read the remaining 200 runes from the cached body")

	assert.Equal(t, int32(1), hits.Load(),
		"paginated re-reads of the same upstream path must share one cache entry")
}

func resultText(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, r.Content)

	tc, ok := r.Content[0].(*mcp.TextContent)
	require.True(t, ok, "expected TextContent, got %T", r.Content[0])

	return tc.Text
}
