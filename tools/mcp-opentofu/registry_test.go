package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearch(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/registry/docs/search", r.URL.Path)
		assert.Equal(t, "aws", r.URL.Query().Get("q"))
		w.Header().Set("Content-Type", "application/json")
		mustWriteString(
			w,
			`[{"title":"hashicorp/aws","description":"AWS provider","type":"provider","version":"v5.0.0","link_variables":{"namespace":"hashicorp","name":"aws","target_system":"","id":""}}]`,
		)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(WithBaseURL(srv.URL))
	items, err := c.Search(t.Context(), "aws")
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "hashicorp/aws", items[0].Title)
	assert.Equal(t, "provider", items[0].Type)
	assert.Equal(t, "hashicorp", items[0].LinkVariables.Namespace)
}

func TestProvider(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/registry/docs/providers/hashicorp/aws/index.json", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		mustWriteString(w, `{
			"addr": {"display":"hashicorp/terraform-provider-aws","namespace":"hashicorp","name":"aws"},
			"description": "AWS provider",
			"versions": [{"id":"v5.0.0"},{"id":"v4.0.0"}],
			"popularity": 1234,
			"link": "https://docs.example.com/aws"
		}`)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(WithBaseURL(srv.URL))
	p, err := c.Provider(t.Context(), "hashicorp", "aws")
	require.NoError(t, err)
	assert.Equal(t, "hashicorp", p.Addr.Namespace)
	assert.Equal(t, "aws", p.Addr.Name)
	assert.Equal(t, 1234, p.Popularity)
	assert.Len(t, p.Versions, 2)
}

func TestProviderVersion(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/registry/docs/providers/hashicorp/aws/v5.0.0/index.json", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		mustWriteString(
			w,
			`{"id":"v5.0.0","docs":{"resources":[{"name":"s3_bucket","description":"An S3 bucket"}],"datasources":[{"name":"ami","description":"An AMI"}]}}`,
		)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(WithBaseURL(srv.URL))
	pv, err := c.ProviderVersion(t.Context(), "hashicorp", "aws", "v5.0.0")
	require.NoError(t, err)
	assert.Equal(t, "v5.0.0", pv.ID)
	require.Len(t, pv.Docs.Resources, 1)
	assert.Equal(t, "s3_bucket", pv.Docs.Resources[0].Name)
	require.Len(t, pv.Docs.Datasources, 1)
	assert.Equal(t, "ami", pv.Docs.Datasources[0].Name)
}

func TestModule(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/registry/docs/modules/terraform-aws-modules/vpc/aws/index.json", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		mustWriteString(w, `{
			"addr": {"display":"terraform-aws-modules/terraform-aws-vpc/aws","namespace":"terraform-aws-modules","name":"vpc","target":"aws"},
			"description": "VPC module",
			"versions": [{"id":"v5.0.0"}],
			"popularity": 99,
			"fork_of": {"display":"//","namespace":"","name":"","target":""},
			"fork_count": 0
		}`)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(WithBaseURL(srv.URL))
	m, err := c.Module(t.Context(), "terraform-aws-modules", "vpc", "aws")
	require.NoError(t, err)
	assert.Equal(t, "vpc", m.Addr.Name)
	assert.Equal(t, "aws", m.Addr.Target)
	assert.Empty(t, m.ForkOf.Namespace)
}

func TestResourceDocs(t *testing.T) {
	t.Parallel()

	body := "# aws_s3_bucket\n\nResource for managing S3 buckets."
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/registry/docs/providers/hashicorp/aws/v5.0.0/resources/s3_bucket.md", r.URL.Path)
		w.Header().Set("Content-Type", "text/markdown")
		mustWriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(WithBaseURL(srv.URL))
	got, err := c.ResourceDocs(t.Context(), "hashicorp", "aws", "v5.0.0", "s3_bucket")
	require.NoError(t, err)
	assert.Equal(t, body, got)
}

func TestDatasourceDocs(t *testing.T) {
	t.Parallel()

	body := "# aws_ami\n\nData source for AMIs."
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/registry/docs/providers/hashicorp/aws/v5.0.0/datasources/ami.md", r.URL.Path)
		// Registry sometimes returns text/plain for .md endpoints; verify we still accept it.
		w.Header().Set("Content-Type", "text/plain")
		mustWriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(WithBaseURL(srv.URL))
	got, err := c.DatasourceDocs(t.Context(), "hashicorp", "aws", "v5.0.0", "ami")
	require.NoError(t, err)
	assert.Equal(t, body, got)
}

func TestRegistryErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		handler http.HandlerFunc
		want    string
	}{
		"404 surfaces as registry error": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			want: "status 404",
		},
		"500 surfaces as registry error": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			want: "status 500",
		},
		"non-JSON body fails to decode": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				mustWriteString(w, "not json")
			},
			want: "decoding",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(tt.handler)
			t.Cleanup(srv.Close)

			c := NewClient(WithBaseURL(srv.URL))
			_, err := c.Provider(t.Context(), "hashicorp", "aws")
			require.Error(t, err)
			require.ErrorIs(t, err, ErrRegistry)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestLatestVersion(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		in   []Version
		want string
	}{
		"empty": {
			in:   nil,
			want: "",
		},
		"single v-prefixed": {
			in:   []Version{{ID: "v0.0.1"}},
			want: "v0.0.1",
		},
		"single bare semver": {
			in:   []Version{{ID: "1.2.3"}},
			want: "1.2.3",
		},
		"highest semver wins": {
			in:   []Version{{ID: "v1.0.0"}, {ID: "v3.2.1"}, {ID: "v2.0.0"}},
			want: "v3.2.1",
		},
		"prerelease ranks below release": {
			in:   []Version{{ID: "v1.2.3-rc1"}, {ID: "v1.2.3"}},
			want: "v1.2.3",
		},
		"unparseable falls through": {
			in:   []Version{{ID: "garbage"}, {ID: "v1.0.0"}},
			want: "v1.0.0",
		},
		"all unparseable falls back to first": {
			in:   []Version{{ID: "garbage"}, {ID: "more garbage"}},
			want: "garbage",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, latestVersion(tt.in))
		})
	}
}

func mustWriteString(w http.ResponseWriter, s string) {
	_, err := w.Write([]byte(s))
	if err != nil {
		panic(err)
	}
}
