package content

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrBadPattern is returned when a grep pattern fails to compile.
var ErrBadPattern = errors.New("invalid grep pattern")

// GrepOptions configures [Grep].
type GrepOptions struct {
	IgnoreCase bool
	Invert     bool
	Context    int
}

// CompilePattern compiles a grep pattern, prepending the (?i) flag for
// case-insensitive matching. A user-supplied inline (?i) still composes
// fine. A compile error is wrapped in [ErrBadPattern], so callers can
// validate a pattern up front and reuse this error path.
func CompilePattern(pattern string, ignoreCase bool) (*regexp.Regexp, error) {
	src := pattern
	if ignoreCase {
		src = "(?i)" + pattern
	}

	re, err := regexp.Compile(src)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadPattern, err)
	}

	return re, nil
}

// Grep filters text to lines matching pattern, GNU grep style: match
// lines render "N:text", context lines "N-text", and non-adjacent groups
// are separated by a "--" line. Case-insensitivity comes from
// opts.IgnoreCase, not the pattern. Line numbers are 1-based over the
// original text, so they stay meaningful regardless of any downstream
// pagination. A compile error is wrapped in [ErrBadPattern]; when nothing
// matches, the result is empty and matched is false.
func Grep(text, pattern string, opts GrepOptions) (string, bool, error) {
	re, err := CompilePattern(pattern, opts.IgnoreCase)
	if err != nil {
		return "", false, err
	}

	lines := strings.Split(text, "\n")

	ctx := max(opts.Context, 0)

	// isMatch[i] marks a line that hits the pattern (rendered with a ":"
	// separator); selected[i] additionally covers the context lines
	// around each match (rendered with "-").
	isMatch := make([]bool, len(lines))
	selected := make([]bool, len(lines))

	anyMatch := false

	for i, line := range lines {
		hit := re.MatchString(line)
		if opts.Invert {
			hit = !hit
		}

		if !hit {
			continue
		}

		anyMatch = true
		isMatch[i] = true

		lo := max(i-ctx, 0)
		hi := min(i+ctx, len(lines)-1)

		for j := lo; j <= hi; j++ {
			selected[j] = true
		}
	}

	if !anyMatch {
		return "", false, nil
	}

	var (
		b        strings.Builder
		prevLine = -1
	)

	for i, line := range lines {
		if !selected[i] {
			continue
		}

		// A gap between the previous emitted line and this one means the
		// two belong to non-adjacent groups; separate them with "--".
		if prevLine >= 0 && i > prevLine+1 {
			b.WriteString("--\n")
		}

		sep := "-"
		if isMatch[i] {
			sep = ":"
		}

		fmt.Fprintf(&b, "%d%s%s\n", i+1, sep, line)

		prevLine = i
	}

	return strings.TrimSuffix(b.String(), "\n"), true, nil
}

// Paginate slices text to the [startIndex, startIndex+maxLength) rune
// window and returns the result plus a flag indicating whether content
// was elided beyond the window. When content is elided, a continuation
// hint naming the next start_index is appended.
func Paginate(text string, startIndex, maxLength int) (string, bool) {
	runes := []rune(text)
	total := len(runes)

	if startIndex >= total {
		return fmt.Sprintf("<content empty: start_index %d exceeds content length %d>", startIndex, total), false
	}

	end := min(startIndex+maxLength, total)
	result := string(runes[startIndex:end])

	if end < total {
		result += fmt.Sprintf(
			"\n\n<content truncated: %d/%d characters shown. Use start_index=%d to continue reading>",
			end-startIndex, total, end,
		)

		return result, true
	}

	return result, false
}
