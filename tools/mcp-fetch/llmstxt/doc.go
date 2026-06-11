// Package llmstxt discovers a site's llms.txt, the LLM-optimized index
// some origins publish at /llms.txt.
//
// A [Finder] probes each origin once and caches the outcome per origin,
// distinguishing "never probed" from "probed and absent" so a missing
// file is recorded once and never re-fetched. The finder shares the
// caller's [*http.Client] so probes obey the same redirect, proxy, and
// timeout policy as content fetches.
package llmstxt
