package main

import (
	"fmt"
	"strings"
)

const (
	typeProvider   = "provider"
	typeModule     = "module"
	typeResource   = "provider/resource"
	typeDatasource = "provider/datasource"
)

// renderSearch renders a list of [SearchResultItem] into a human-readable
// block. typeFilter is one of "provider", "module", "resource",
// "data-source", or "all"/"" (no filter).
func renderSearch(query, typeFilter string, items []SearchResultItem) string {
	filtered := filterByType(items, typeFilter)

	if len(filtered) == 0 {
		return fmt.Sprintf("No results found for %q in the OpenTofu Registry.", query)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d results for %q in the OpenTofu Registry:\n\n", len(filtered), query)

	for i := range filtered {
		renderSearchItem(&b, &filtered[i])
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

func filterByType(items []SearchResultItem, typeFilter string) []SearchResultItem {
	if typeFilter == "" || typeFilter == "all" {
		return items
	}

	want := map[string]string{
		typeProvider:  typeProvider,
		typeModule:    typeModule,
		"resource":    typeResource,
		"data-source": typeDatasource,
	}[typeFilter]

	if want == "" {
		return nil
	}

	out := make([]SearchResultItem, 0, len(items))
	for i := range items {
		if items[i].Type == want {
			out = append(out, items[i])
		}
	}

	return out
}

func renderSearchItem(b *strings.Builder, it *SearchResultItem) {
	if it.Description == "" {
		fmt.Fprintf(b, "- %s (%s) (latest version: %s)\n", it.Title, it.Type, it.Version)
	} else {
		fmt.Fprintf(
			b, "- %s %s (%s) (latest version: %s)\n",
			it.Title, it.Description, it.Type, it.Version,
		)
	}

	lv := it.LinkVariables
	switch it.Type {
	case typeProvider:
		fmt.Fprintf(b, "  Provider: %s/%s\n", lv.Namespace, lv.Name)
		fmt.Fprintf(
			b, "  Use `get_provider_details` with namespace=%q, name=%q for details.\n",
			lv.Namespace, lv.Name,
		)

	case typeModule:
		fmt.Fprintf(b, "  Module: %s/%s (%s)\n", lv.Namespace, lv.Name, lv.Target)
		fmt.Fprintf(
			b, "  Use `get_module_details` with namespace=%q, name=%q, target=%q for details.\n",
			lv.Namespace, lv.Name, lv.Target,
		)

	case typeResource:
		fmt.Fprintf(b, "  Resource: %s/%s (%s)\n", lv.Namespace, lv.Name, lv.ID)
		fmt.Fprintf(b, "  Full identifier: %s_%s\n", lv.Name, lv.ID)
		fmt.Fprintf(
			b, "  Use `get_resource_docs` with namespace=%q, name=%q, resource=%q for documentation.\n",
			lv.Namespace, lv.Name, lv.ID,
		)
		fmt.Fprintf(
			b, "  Use `get_provider_details` with namespace=%q, name=%q for details about this provider.\n",
			lv.Namespace, lv.Name,
		)

	case typeDatasource:
		fmt.Fprintf(b, "  Data source: %s/%s (%s)\n", lv.Namespace, lv.Name, lv.ID)
		fmt.Fprintf(b, "  Full identifier: %s_%s\n", lv.Name, lv.ID)
		fmt.Fprintf(
			b, "  Use `get_datasource_docs` with namespace=%q, name=%q, data_source=%q for documentation.\n",
			lv.Namespace, lv.Name, lv.ID,
		)
		fmt.Fprintf(
			b, "  Use `get_provider_details` with namespace=%q, name=%q for details about this provider.\n",
			lv.Namespace, lv.Name,
		)
	}
}

// renderProvider builds the Markdown block for get_provider_details.
// p is the response from /providers/{ns}/{name}/index.json; pv is the
// docs index for the version chosen by [latestVersion].
func renderProvider(p Provider, pv ProviderVersion) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## Provider: %s/%s\n\n", p.Addr.Namespace, p.Addr.Name)

	if p.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", p.Description)
	}

	if len(p.Versions) > 0 {
		fmt.Fprintf(&b, "**Latest Version**: %s\n", p.Versions[0].ID)
		fmt.Fprintf(&b, "**All Versions**: %s\n\n", joinVersions(p.Versions))
	}

	fmt.Fprintf(&b, "**Popularity Score**: %d\n", p.Popularity)

	if p.Link != "" {
		fmt.Fprintf(&b, "\n**Documentation**: %s\n", p.Link)
	}

	fmt.Fprintf(&b, "\n## Latest Version Details (%s)\n\n", pv.ID)

	renderDocSection(&b, docSection{
		heading:  "Resources",
		singular: "resource",
		tool:     "get_resource_docs",
		param:    "resource",
		ns:       p.Addr.Namespace,
		name:     p.Addr.Name,
		items:    pv.Docs.Resources,
	})
	renderDocSection(&b, docSection{
		heading:  "Data Sources",
		singular: "data source",
		tool:     "get_datasource_docs",
		param:    "data_source",
		ns:       p.Addr.Namespace,
		name:     p.Addr.Name,
		items:    pv.Docs.Datasources,
	})

	return strings.TrimRight(b.String(), "\n")
}

// docSection bundles the per-section parameters for [renderDocSection].
// singular carries the explicit lower-case noun used in prose ("resource",
// "data source"); deriving it from heading would silently mis-pluralize
// irregular plurals.
type docSection struct {
	heading  string
	singular string
	tool     string
	param    string
	ns       string
	name     string
	items    []DocItem
}

func renderDocSection(b *strings.Builder, s docSection) {
	fmt.Fprintf(b, "### %s (%d)\n", s.heading, len(s.items))

	if len(s.items) == 0 {
		b.WriteString("\n")
		return
	}

	first := s.items[0].Name
	fmt.Fprintf(
		b,
		"**Note**: When used in opentofu/terraform, the %s names are prefixed with the provider name (e.g., `%s_%s`).\n\n",
		s.singular,
		s.name,
		first,
	)
	fmt.Fprintf(
		b,
		"**Example**: Use `%s` with namespace=%q, name=%q, %s=%q to get documentation for the first %s.\n\n",
		s.tool, s.ns, s.name, s.param, first, s.singular,
	)

	for _, it := range s.items {
		desc := it.Description
		if desc == "" {
			desc = "No description"
		} else {
			desc = truncateRunes(desc, 50)
		}

		fmt.Fprintf(b, "- %s: %s\n", it.Name, desc)
	}

	b.WriteString("\n")
}

// renderModule builds the Markdown block for get_module_details.
func renderModule(m Module) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## Module: %s/%s/%s\n\n", m.Addr.Namespace, m.Addr.Name, m.Addr.Target)

	if m.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", m.Description)
	}

	if len(m.Versions) > 0 {
		fmt.Fprintf(&b, "**Available Versions**: %s\n\n", joinVersions(m.Versions))
	}

	fmt.Fprintf(&b, "**Popularity Score**: %d\n", m.Popularity)

	if m.ForkOf.Namespace != "" {
		fmt.Fprintf(&b, "\n**Forked from**: %s\n", m.ForkOf.Display)
	}

	if m.ForkCount > 0 {
		fmt.Fprintf(&b, "**Fork count**: %d\n", m.ForkCount)
	}

	return strings.TrimRight(b.String(), "\n")
}

func joinVersions(vs []Version) string {
	ids := make([]string, len(vs))
	for i, v := range vs {
		ids[i] = v.ID
	}

	return strings.Join(ids, ", ")
}

// truncateRunes truncates s to at most n runes, appending "..." if the
// original was longer. n is treated as a hard cap; the suffix is included
// only when truncation actually happened.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}

	return string(r[:n]) + "..."
}
