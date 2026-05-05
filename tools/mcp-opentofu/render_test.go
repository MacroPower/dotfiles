package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderSearchEmpty(t *testing.T) {
	t.Parallel()

	got := renderSearch("kubernetes", "all", nil)
	assert.Contains(t, got, `No results found for "kubernetes"`)
}

func TestRenderSearchFiltering(t *testing.T) {
	t.Parallel()

	items := []SearchResultItem{
		{
			Title:         "hashicorp/aws",
			Type:          typeProvider,
			Version:       "v5.0.0",
			LinkVariables: SearchLinkVars{Namespace: "hashicorp", Name: "aws"},
		},
		{
			Title:         "terraform-aws-modules/vpc",
			Type:          typeModule,
			Version:       "v5.1.2",
			LinkVariables: SearchLinkVars{Namespace: "terraform-aws-modules", Name: "vpc", Target: "aws"},
		},
		{
			Title:         "aws_s3_bucket",
			Type:          typeResource,
			Version:       "v5.0.0",
			LinkVariables: SearchLinkVars{Namespace: "hashicorp", Name: "aws", ID: "s3_bucket"},
		},
		{
			Title:         "aws_ami",
			Type:          typeDatasource,
			Version:       "v5.0.0",
			LinkVariables: SearchLinkVars{Namespace: "hashicorp", Name: "aws", ID: "ami"},
		},
	}

	tests := map[string]struct {
		filter  string
		want    []string
		notWant []string
	}{
		"all": {
			filter: "all",
			want: []string{
				"Found 4 results",
				"Provider: hashicorp/aws",
				"Module: terraform-aws-modules/vpc (aws)",
				"Resource: hashicorp/aws (s3_bucket)",
				"Data source: hashicorp/aws (ami)",
				"Full identifier: aws_s3_bucket",
				"Full identifier: aws_ami",
				"get_resource_docs",
				"get_datasource_docs",
				"get_provider_details",
			},
		},
		"empty filter same as all": {
			filter: "",
			want:   []string{"Found 4 results"},
		},
		"provider only": {
			filter:  "provider",
			want:    []string{"Found 1 results", "Provider: hashicorp/aws"},
			notWant: []string{"Module:", "Resource:", "Data source:"},
		},
		"module only": {
			filter:  "module",
			want:    []string{"Found 1 results", "Module: terraform-aws-modules/vpc"},
			notWant: []string{"Provider: hashicorp", "Resource:", "Data source:"},
		},
		"resource only": {
			filter:  "resource",
			want:    []string{"Found 1 results", "Resource: hashicorp/aws (s3_bucket)"},
			notWant: []string{"Module:", "Data source:"},
		},
		"data-source only": {
			filter:  "data-source",
			want:    []string{"Found 1 results", "Data source: hashicorp/aws (ami)"},
			notWant: []string{"Resource: hashicorp/aws"},
		},
		"unknown filter yields empty": {
			filter:  "bogus",
			want:    []string{"No results found"},
			notWant: []string{"Found"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := renderSearch("aws", tt.filter, items)
			for _, w := range tt.want {
				assert.Contains(t, got, w, "want substring %q", w)
			}

			for _, w := range tt.notWant {
				assert.NotContains(t, got, w, "did not want substring %q", w)
			}
		})
	}
}

func TestRenderSearchEmptyDescriptionNoDoubleSpace(t *testing.T) {
	t.Parallel()

	items := []SearchResultItem{
		{
			Title:         "hashicorp/aws",
			Description:   "",
			Type:          typeProvider,
			Version:       "v5.0.0",
			LinkVariables: SearchLinkVars{Namespace: "hashicorp", Name: "aws"},
		},
	}
	got := renderSearch("aws", "all", items)
	assert.Contains(t, got, "- hashicorp/aws (provider) (latest version: v5.0.0)")
	assert.NotContains(t, got, "  (provider)")
}

func TestRenderProvider(t *testing.T) {
	t.Parallel()

	p := Provider{
		Addr:        Addr{Display: "hashicorp/terraform-provider-aws", Namespace: "hashicorp", Name: "aws"},
		Description: "AWS provider",
		Versions:    []Version{{ID: "v5.0.0"}, {ID: "v4.0.0"}},
		Popularity:  1234,
		Link:        "https://docs.example.com/aws",
	}
	pv := ProviderVersion{
		ID: "v5.0.0",
		Docs: ProviderVersionDocs{
			Resources: []DocItem{
				{Name: "s3_bucket", Description: "Manages an S3 bucket"},
				{Name: "instance", Description: ""},
			},
			Datasources: []DocItem{
				{Name: "ami", Description: "Look up AMIs"},
			},
		},
	}

	got := renderProvider(p, pv)
	// Heading uses {ns}/{name}, NOT addr.display (which has the prefix).
	assert.Contains(t, got, "## Provider: hashicorp/aws")
	assert.NotContains(t, got, "terraform-provider-aws")

	assert.Contains(t, got, "AWS provider")
	assert.Contains(t, got, "**Latest Version**: v5.0.0")
	assert.Contains(t, got, "**All Versions**: v5.0.0, v4.0.0")
	assert.Contains(t, got, "**Popularity Score**: 1234")
	assert.Contains(t, got, "**Documentation**: https://docs.example.com/aws")
	assert.Contains(t, got, "## Latest Version Details (v5.0.0)")
	assert.Contains(t, got, "### Resources (2)")
	assert.Contains(t, got, "### Data Sources (1)")
	assert.Contains(t, got, "aws_s3_bucket")
	assert.Contains(t, got, "- s3_bucket: Manages an S3 bucket")
	assert.Contains(t, got, "- instance: No description")
	assert.Contains(t, got, "- ami: Look up AMIs")
}

func TestRenderProviderEmpty(t *testing.T) {
	t.Parallel()

	p := Provider{
		Addr: Addr{Namespace: "ns", Name: "n"},
	}
	pv := ProviderVersion{}

	got := renderProvider(p, pv)
	assert.Contains(t, got, "## Provider: ns/n")
	assert.Contains(t, got, "**Popularity Score**: 0")
	assert.Contains(t, got, "### Resources (0)")
	assert.Contains(t, got, "### Data Sources (0)")
	assert.NotContains(t, got, "**Documentation**")
	assert.NotContains(t, got, "**Latest Version**")
}

func TestRenderProviderTruncatesDescription(t *testing.T) {
	t.Parallel()

	long := "a description that is well over fifty characters long, indeed quite long"
	p := Provider{Addr: Addr{Namespace: "ns", Name: "n"}, Versions: []Version{{ID: "v1.0.0"}}}
	pv := ProviderVersion{
		ID: "v1.0.0",
		Docs: ProviderVersionDocs{
			Resources: []DocItem{{Name: "thing", Description: long}},
		},
	}
	got := renderProvider(p, pv)
	assert.Contains(t, got, "...")
	assert.NotContains(t, got, long)
}

func TestRenderModule(t *testing.T) {
	t.Parallel()

	m := Module{
		Addr:        Addr{Namespace: "terraform-aws-modules", Name: "vpc", Target: "aws", Display: "x/y/z"},
		Description: "VPC module",
		Versions:    []Version{{ID: "v5.0.0"}, {ID: "v4.0.0"}},
		Popularity:  99,
		ForkOf:      Addr{Display: "//", Namespace: "", Name: "", Target: ""},
		ForkCount:   0,
	}

	got := renderModule(m)
	assert.Contains(t, got, "## Module: terraform-aws-modules/vpc/aws")
	assert.Contains(t, got, "VPC module")
	assert.Contains(t, got, "**Available Versions**: v5.0.0, v4.0.0")
	assert.Contains(t, got, "**Popularity Score**: 99")
	assert.NotContains(t, got, "**Forked from**")
	assert.NotContains(t, got, "**Fork count**")
}

func TestRenderModuleFork(t *testing.T) {
	t.Parallel()

	m := Module{
		Addr:      Addr{Namespace: "ns", Name: "n", Target: "aws"},
		Versions:  []Version{{ID: "v1.0.0"}},
		ForkOf:    Addr{Display: "upstream/repo/aws", Namespace: "upstream", Name: "repo", Target: "aws"},
		ForkCount: 7,
	}

	got := renderModule(m)
	assert.Contains(t, got, "**Forked from**: upstream/repo/aws")
	assert.Contains(t, got, "**Fork count**: 7")
}

func TestTruncateRunes(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		in   string
		n    int
		want string
	}{
		"short stays whole":        {in: "hi", n: 5, want: "hi"},
		"exact stays whole":        {in: "hello", n: 5, want: "hello"},
		"long truncated":           {in: "hello world", n: 5, want: "hello..."},
		"unicode counted as runes": {in: "abčdef", n: 4, want: "abčd..."},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, truncateRunes(tt.in, tt.n))
		})
	}
}
