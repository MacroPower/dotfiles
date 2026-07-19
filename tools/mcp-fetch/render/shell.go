package render

import (
	"regexp"
	"strings"
)

const (
	// shellMaxWords is the rendered-text word count below which a
	// script-bearing page is considered an empty application shell.
	shellMaxWords = 30

	// shellScanBytes caps how much of the document the heuristic
	// examines. The markers (script tag, empty mount point, noscript
	// notice) sit in the first few KB of real documents, and the check
	// runs on every uncached HTML fetch, where an unbounded regex scan
	// of a response up to 10 MiB is measurable latency.
	shellScanBytes = 64 * 1024
)

var (
	// emptyRootPattern matches the conventional empty SPA mount point:
	// a div with id "root" or "app" (or a React root marker) and no
	// content between the tags.
	emptyRootPattern = regexp.MustCompile(
		`(?is)<div[^>]*\b(?:id=["'](?:root|app)["']|data-reactroot)[^>]*>\s*</div>`,
	)

	// noscriptPattern matches the "you need to enable JavaScript"
	// notice SPAs put in a noscript element.
	noscriptPattern = regexp.MustCompile(
		`(?is)<noscript>.{0,500}?(?:enable|requires?|need)[^<]{0,80}javascript`,
	)

	scriptTagPattern = regexp.MustCompile(`(?i)<script`)
)

// LooksLikeJSShell reports whether an HTML body appears to be a
// client-rendered shell whose content only exists after JavaScript
// runs. body is the fetched document and md its markdown conversion;
// the check is a hint heuristic (bounded to a [shellScanBytes] prefix),
// so false positives merely suggest a pointless render.
func LooksLikeJSShell(body []byte, md string) bool {
	if len(body) > shellScanBytes {
		body = body[:shellScanBytes]
	}

	if len(md) > shellScanBytes {
		md = md[:shellScanBytes]
	}

	if !scriptTagPattern.Match(body) {
		return false
	}

	if emptyRootPattern.Match(body) || noscriptPattern.Match(body) {
		return true
	}

	return len(strings.Fields(md)) < shellMaxWords
}
