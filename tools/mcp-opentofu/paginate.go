package main

import "fmt"

const (
	defaultMaxLength = 5000
	maxMaxLength     = 1_000_000
)

// Truncate slices content as a []rune, applies defaults and clamps to
// startIndex and maxLength, and appends a continuation marker when more
// content remains beyond the slice. The returned string is safe to send
// as a single mcp.TextContent body.
//
// A maxLength of zero or negative is treated as [defaultMaxLength];
// values above [maxMaxLength] are clamped down. A negative startIndex is
// treated as zero. When startIndex lands beyond the end of content, the
// caller receives a sentinel marker rather than an empty body. Slicing
// happens on rune boundaries, so the next call with start_index set to
// the marker's value joins seamlessly with no broken codepoints.
func Truncate(content string, startIndex, maxLength int) string {
	if maxLength <= 0 {
		maxLength = defaultMaxLength
	}

	maxLength = min(maxLength, maxMaxLength)
	if startIndex < 0 {
		startIndex = 0
	}

	// Fast path: when starting from the beginning and the body fits the
	// window, no rune-counting is needed. Skips an O(n) []rune allocation
	// that scales with the full content (e.g. multi-MB plan output). The
	// non-empty guard preserves the empty-marker behavior below.
	if startIndex == 0 && content != "" && len(content) <= maxLength {
		return content
	}

	runes := []rune(content)
	total := len(runes)

	if startIndex >= total {
		return fmt.Sprintf("<content empty: start_index %d exceeds content length %d>", startIndex, total)
	}

	end := min(startIndex+maxLength, total)
	result := string(runes[startIndex:end])

	if end < total {
		result += fmt.Sprintf(
			"\n\n<content truncated: %d/%d characters shown. Use start_index=%d to continue reading>",
			end-startIndex, total, end,
		)
	}

	return result
}
