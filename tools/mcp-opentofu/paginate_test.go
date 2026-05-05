package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// repeatRune builds a string of n copies of r. Useful for crafting
// boundary-of-default and exact-fit fixtures with no ambiguity about
// rune-vs-byte length.
func repeatRune(r rune, n int) string {
	return strings.Repeat(string(r), n)
}

// continuationMarker is the format string used by [Truncate] when more
// content remains. Tests reproduce the marker rather than re-importing
// the format, so a regression in the format string surfaces here.
func continuationMarker(shown, total, next int) string {
	return fmt.Sprintf(
		"\n\n<content truncated: %d/%d characters shown. Use start_index=%d to continue reading>",
		shown, total, next,
	)
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		content    string
		startIndex int
		maxLength  int
		want       string
	}{
		"negative startIndex coerced to zero": {
			content:    "hello world",
			startIndex: -5,
			maxLength:  5,
			want:       "hello" + continuationMarker(5, 11, 5),
		},
		"zero maxLength defaults to defaultMaxLength": {
			content:    repeatRune('a', defaultMaxLength),
			startIndex: 0,
			maxLength:  0,
			want:       repeatRune('a', defaultMaxLength),
		},
		"negative maxLength defaults to defaultMaxLength": {
			content:    repeatRune('a', defaultMaxLength),
			startIndex: 0,
			maxLength:  -1,
			want:       repeatRune('a', defaultMaxLength),
		},
		"maxLength above maxMaxLength is clamped": {
			content:    repeatRune('a', maxMaxLength+10),
			startIndex: 0,
			maxLength:  maxMaxLength * 5,
			want: repeatRune('a', maxMaxLength) +
				continuationMarker(maxMaxLength, maxMaxLength+10, maxMaxLength),
		},
		"zero-length body emits empty marker": {
			content:    "",
			startIndex: 0,
			maxLength:  100,
			want:       "<content empty: start_index 0 exceeds content length 0>",
		},
		"startIndex past end emits empty marker": {
			content:    "hello",
			startIndex: 100,
			maxLength:  10,
			want:       "<content empty: start_index 100 exceeds content length 5>",
		},
		"body exactly defaultMaxLength runes long has no marker": {
			content:    repeatRune('a', defaultMaxLength),
			startIndex: 0,
			maxLength:  0,
			want:       repeatRune('a', defaultMaxLength),
		},
		"exact-fit at len(runes) == startIndex+maxLength has no marker": {
			content:    repeatRune('b', 100),
			startIndex: 50,
			maxLength:  50,
			want:       repeatRune('b', 50),
		},
		"partial-fit reports chars-shown and continuation offset": {
			// startIndex > 0 makes (end - startIndex) and end diverge,
			// so the marker MUST report 30 chars shown / next start at 80.
			content:    repeatRune('c', 200),
			startIndex: 50,
			maxLength:  30,
			want:       repeatRune('c', 30) + continuationMarker(30, 200, 80),
		},
		"multibyte rune at slice boundary stays whole": {
			// Each "é" is two UTF-8 bytes but one rune. Slicing at rune
			// boundaries returns intact codepoints.
			content:    "é" + "é" + "é" + "é" + "é",
			startIndex: 0,
			maxLength:  2,
			want:       "éé" + continuationMarker(2, 5, 2),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := Truncate(tt.content, tt.startIndex, tt.maxLength)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestTruncateMultibyteAcrossBoundary asserts that a body containing a
// mix of single-byte and multibyte runes can be paginated by chaining
// calls with the marker's reported start_index, with no broken codepoints
// at the join.
func TestTruncateMultibyteAcrossBoundary(t *testing.T) {
	t.Parallel()

	// Mix ASCII with 4-byte runes (an emoji) so that byte-based slicing
	// would fragment a codepoint mid-cluster.
	body := strings.Repeat("ab🐈", 10) // 30 runes, mostly multibyte

	first := Truncate(body, 0, 7)
	require.Contains(t, first, "Use start_index=7 to continue reading")

	second := Truncate(body, 7, 7)
	require.Contains(t, second, "Use start_index=14 to continue reading")

	prefix := strings.Split(first, "\n\n<content truncated")[0]
	suffix := strings.Split(second, "\n\n<content truncated")[0]

	joined := prefix + suffix
	wantRunes := []rune(body)[:14]
	assert.Equal(t, string(wantRunes), joined,
		"joining successive slices must reproduce the original body verbatim")
}

// TestTruncateMultiCallTraversal drives a long body to exhaustion via
// repeated start_index calls and asserts the concatenation of the slices
// (with markers stripped) matches the original.
func TestTruncateMultiCallTraversal(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("the quick brown fox 🦊 jumps over the lazy dog\n", 200)

	const sliceLen = 137 // small, prime-ish chunk to exercise many boundaries

	var got strings.Builder

	startIndex := 0

	for {
		out := Truncate(body, startIndex, sliceLen)
		if strings.HasPrefix(out, "<content empty:") {
			t.Fatalf("traversal walked off the end at start_index=%d (body len %d runes)",
				startIndex, len([]rune(body)))
		}

		idx := strings.Index(out, "\n\n<content truncated:")
		if idx < 0 {
			got.WriteString(out)
			break
		}

		got.WriteString(out[:idx])

		marker := out[idx:]

		var shown, total, next int

		_, err := fmt.Sscanf(
			marker,
			"\n\n<content truncated: %d/%d characters shown. Use start_index=%d to continue reading>",
			&shown, &total, &next,
		)
		require.NoError(t, err, "marker did not parse: %q", marker)
		require.Greater(t, next, startIndex,
			"continuation start_index must advance: got %d, was %d", next, startIndex)

		startIndex = next
	}

	assert.Equal(t, body, got.String())
}
