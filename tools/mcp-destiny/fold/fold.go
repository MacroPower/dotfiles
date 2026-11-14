package fold

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Name folds s for a name lookup. It decomposes s to Unicode Normalization
// Form D, removes each character with the Unicode property Diacritic, and
// lowercases the rest. The tables of Go and of the browser each follow
// their own Unicode version. A character that only one of the two versions
// lists can therefore fold differently in the web client.
func Name(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Diacritic, r) {
			return -1
		}

		return r
	}, norm.NFD.String(s))

	return strings.ToLower(s)
}

// Equal reports whether a and b are equal after [Name] folds both.
func Equal(a, b string) bool {
	return Name(a) == Name(b)
}

// Contains reports whether s contains substr after [Name] folds both. A
// substr that folds to the empty text, such as "" or "^", matches every s.
func Contains(s, substr string) bool {
	return strings.Contains(Name(s), Name(substr))
}
