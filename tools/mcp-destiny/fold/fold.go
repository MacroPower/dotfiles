package fold

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var (
	// Lookalike folds each text as [Name] does. It then reads each digit that
	// the names of the game write in place of a letter as that letter. Each
	// pair comes from a name in the definitions:
	//
	//   - 0 reads as o, as in St0mp-EE5 and BrayTech Sn0Boots.
	//   - 1 reads as i, as in Gr1p-EE5 and M1R Distribution Matrix.
	//   - 3 reads as e, as in M3-D1 Shell and N33-DL.
	//   - 5 reads as s, as in 5p 3AR.
	//   - 7 reads as v, as in Se7enth, and as l, as in S733k Pack.
	//   - 9 reads as g, as in E99 Shell.
	//
	// Lookalike reads a text in two ways, because 7 stands for two letters.
	// One reading reads every 7 as v, and the other reads every 7 as l. Both
	// read the other digits in the same way. The texts seventh and sleek pack
	// therefore match Se7enth and S733k Pack. Each comparison applies one
	// reading to both texts, so the text sveek never matches the text sleek.
	//
	// Lookalike reads a digit as a letter wherever the digit stands, also in
	// a number. The name 1000 Yard Stare therefore matches the text iooo yard
	// stare, and the text 14 matches the text i4.
	Lookalike = Fold{readings: []func(string) string{
		lookalike(strings.NewReplacer("0", "o", "1", "i", "3", "e", "5", "s", "7", "v", "9", "g")),
		lookalike(strings.NewReplacer("0", "o", "1", "i", "3", "e", "5", "s", "7", "l", "9", "g")),
	}}

	// plain folds each text as [Name] does.
	plain = Fold{readings: []func(string) string{Name}}
)

// A Fold folds the texts of a lookup before the lookup compares them. It
// reads each text in one or more ways, and each reading folds the text to
// one text. Two texts match under a Fold when one of its readings folds
// both of them to texts that match. Each comparison applies one reading to
// both texts, so a match stays symmetric.
type Fold struct {
	readings []func(string) string
}

// Text folds s with each reading of f. A lookup that compares one text
// with many names folds that text one time.
func (f Fold) Text(s string) Text {
	texts := make([]string, 0, len(f.readings))
	for _, read := range f.readings {
		texts = append(texts, read(s))
	}

	return Text{readings: f.readings, texts: texts}
}

// Equal reports whether a and b are equal after f folds both.
func (f Fold) Equal(a, b string) bool {
	return f.Text(b).Equal(a)
}

// Contains reports whether s contains substr after f folds both. A substr
// that folds to the empty text, such as "" or "^", matches every s.
func (f Fold) Contains(s, substr string) bool {
	return f.Text(substr).In(s)
}

// Text holds a text as each reading of a [Fold] folded it. [Fold.Text]
// returns it.
type Text struct {
	readings []func(string) string
	texts    []string
}

// Equal reports whether s equals the text after the fold of t folds s.
func (t Text) Equal(s string) bool {
	for i, read := range t.readings {
		if read(s) == t.texts[i] {
			return true
		}
	}

	return false
}

// In reports whether s contains the text after the fold of t folds s. A
// text that folds to the empty text matches every s.
func (t Text) In(s string) bool {
	for i, read := range t.readings {
		if strings.Contains(read(s), t.texts[i]) {
			return true
		}
	}

	return false
}

// Empty reports whether the text folds to the empty text, as "" and "^"
// do.
func (t Text) Empty() bool {
	for _, text := range t.texts {
		if text != "" {
			return false
		}
	}

	return true
}

// TrimSuffix returns t without a trailing suffix. Each reading of the fold
// of t folds suffix, and TrimSuffix removes that text from the end of the
// text that the same reading gave.
func (t Text) TrimSuffix(suffix string) Text {
	texts := make([]string, 0, len(t.texts))
	for i, read := range t.readings {
		texts = append(texts, strings.TrimSuffix(t.texts[i], read(suffix)))
	}

	return Text{readings: t.readings, texts: texts}
}

// Name folds s as the web client folds a name. It decomposes s to Unicode
// Normalization Form D, removes each character with the Unicode property
// Diacritic, and lowercases the rest. The tables of Go and of the browser
// each follow their own Unicode version. A character that only one of the
// two versions lists can therefore fold differently in the web client.
func Name(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Diacritic, r) {
			return -1
		}

		return r
	}, norm.NFD.String(s))

	return strings.ToLower(s)
}

// Passes returns the folds of a lookup that picks one name, in the order
// in which the lookup applies them. The first fold folds as [Name] does,
// and the second is [Lookalike]. The lookup searches the names with the
// second fold only when the first fold finds no name.
func Passes() []Fold {
	return []Fold{plain, Lookalike}
}

// Equal reports whether a and b are equal after [Lookalike] folds both.
func Equal(a, b string) bool {
	return Lookalike.Equal(a, b)
}

// Contains reports whether s contains substr after [Lookalike] folds
// both. A substr that folds to the empty text, such as "" or "^", matches
// every s.
func Contains(s, substr string) bool {
	return Lookalike.Contains(s, substr)
}

// lookalike returns a reading that folds a text as [Name] does and then
// replaces each digit as letters replaces it.
func lookalike(letters *strings.Replacer) func(string) string {
	return func(s string) string {
		return letters.Replace(Name(s))
	}
}
