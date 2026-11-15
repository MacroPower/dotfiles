package wireinput

import (
	"errors"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// escapeLen is the length of one `\uXXXX` escape: a backslash, a `u`,
// and four hex digits.
const escapeLen = 6

var (
	// ErrMisaligned reports two texts that [Restore] cannot align. The
	// text the tool ran with differs from the text the model sent by
	// more than decoded escapes, stripped whitespace at line ends, and
	// curled quotes.
	ErrMisaligned = errors.New("ran text does not follow from the sent text")

	// curlyQuotes maps each straight quote to the opening and the
	// closing curly quote that the Edit tool writes in its place.
	curlyQuotes = map[rune][2]rune{
		'\'': {0x2018, 0x2019},
		'"':  {0x201c, 0x201d},
	}
)

// Escape is one `\uXXXX` escape that Claude Code decoded on the way to
// a tool.
type Escape struct {
	// Text is the escape as the model sent it. The two escapes of a
	// surrogate pair make one Escape.
	Text string
	// Rune is the character the tool got in place of Text.
	Rune rune
}

// Restoration is the result of [Restore].
type Restoration struct {
	// Text is the text the tool ran with, with each decoded escape
	// back in place.
	Text string
	// Escapes lists the escapes that Text restores, in the order of
	// the text.
	Escapes []Escape
}

// Restore returns ran, the text a tool ran with, with each `\uXXXX`
// escape back that sent, the text the model sent, spells and that
// Claude Code decoded.
//
// It walks both texts from the start. Where an escape in sent meets its
// character in ran, Claude Code decoded it, and the result takes the
// escape. Text that is the same in both goes to the result unchanged.
// Whitespace that sent holds at the end of a line and ran lacks stays
// out of the result, the way Claude Code stripped it. An escape among
// that whitespace decoded to a whitespace character before the strip,
// so the result takes the escape and the whitespace before it.
//
// A straight quote in sent that meets a curly quote of the same kind in
// ran gives the result the curly quote. The Edit tool writes curly
// quotes in place of the straight quotes of its new text when the text
// that it replaces uses curly quotes.
//
// Any other difference returns [ErrMisaligned]. A [Restoration]
// without escapes means that Claude Code decoded nothing.
func Restore(sent, ran string) (Restoration, error) {
	var (
		out     strings.Builder
		held    strings.Builder
		escapes []Escape
		// stripped is true inside a run of sent that ran lacks.
		stripped bool
	)

	i, j := 0, 0
	for i < len(sent) {
		text, decoded, isEscape := escapeAt(sent, i)
		rest := ran[j:]

		// A decoded backslash also starts the escape's own text, so
		// ran reads as a kept escape when it holds that text.
		if isEscape && strings.HasPrefix(rest, string(decoded)) && !strings.HasPrefix(rest, text) {
			if stripped {
				return Restoration{}, ErrMisaligned
			}

			out.WriteString(text)

			escapes = append(escapes, Escape{Text: text, Rune: decoded})
			i += len(text)
			j += utf8.RuneLen(decoded)

			continue
		}

		r, size := utf8.DecodeRuneInString(sent[i:])
		char := sent[i : i+size]

		matched := char
		if !strings.HasPrefix(rest, matched) {
			matched = curledQuote(r, rest)
		}

		if matched != "" {
			// Claude Code strips whitespace only at the end of a line.
			if stripped && r != '\n' && r != '\r' {
				return Restoration{}, ErrMisaligned
			}

			stripped = false

			held.Reset()
			out.WriteString(matched)

			i += size
			j += len(matched)

			continue
		}

		switch {
		case isEscape && lineSpace(decoded):
			// The escape is text, so the whitespace before it no
			// longer ends the line.
			out.WriteString(held.String())
			held.Reset()
			out.WriteString(text)

			escapes = append(escapes, Escape{Text: text, Rune: decoded})
			i += len(text)

		case lineSpace(r):
			held.WriteString(char)

			i += size

		default:
			return Restoration{}, ErrMisaligned
		}

		stripped = true
	}

	if j != len(ran) {
		return Restoration{}, ErrMisaligned
	}

	return Restoration{Text: out.String(), Escapes: escapes}, nil
}

// curledQuote returns the curly quote that starts rest when r is a
// straight quote of the same kind. It returns an empty string for any
// other r or rest.
func curledQuote(r rune, rest string) string {
	curly, ok := curlyQuotes[r]
	next, size := utf8.DecodeRuneInString(rest)

	if !ok || (next != curly[0] && next != curly[1]) {
		return ""
	}

	return rest[:size]
}

// escapeAt returns the escape that starts at s[i] and the character
// that Claude Code decodes it to. It reports false where Claude Code
// leaves the text alone: after an odd number of backslashes, and for
// half of a surrogate pair. A high and a low surrogate escape in a row
// make one escape.
func escapeAt(s string, i int) (string, rune, bool) {
	unit, ok := escapeUnit(s, i)
	if !ok || backslashesBefore(s, i)%2 == 1 {
		return "", 0, false
	}

	if !utf16.IsSurrogate(unit) {
		return s[i : i+escapeLen], unit, true
	}

	low, ok := escapeUnit(s, i+escapeLen)
	if !ok {
		return "", 0, false
	}

	r := utf16.DecodeRune(unit, low)
	if r == utf8.RuneError {
		return "", 0, false
	}

	return s[i : i+2*escapeLen], r, true
}

// escapeUnit parses one `\uXXXX` escape at s[i] and returns the UTF-16
// code unit that its four hex digits name.
func escapeUnit(s string, i int) (rune, bool) {
	if len(s)-i < escapeLen || s[i] != '\\' || s[i+1] != 'u' {
		return 0, false
	}

	var unit rune

	for _, c := range []byte(s[i+2 : i+escapeLen]) {
		digit, ok := hexDigit(c)
		if !ok {
			return 0, false
		}

		unit = unit<<4 | digit
	}

	return unit, true
}

// hexDigit returns the value of the hex digit c.
func hexDigit(c byte) (rune, bool) {
	switch {
	case c >= '0' && c <= '9':
		return rune(c - '0'), true
	case c >= 'a' && c <= 'f':
		return rune(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return rune(c-'A') + 10, true
	default:
		return 0, false
	}
}

// backslashesBefore counts the backslashes that end at s[i-1].
func backslashesBefore(s string, i int) int {
	n := 0
	for i-n > 0 && s[i-n-1] == '\\' {
		n++
	}

	return n
}

// lineSpace reports whether r is whitespace that Claude Code strips
// from the end of a line. The set is the `\s` class of a JavaScript
// regular expression without the line feed and the carriage return,
// which end the line.
func lineSpace(r rune) bool {
	switch r {
	case '\t', '\v', '\f', ' ', 0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	default:
		return r >= 0x2000 && r <= 0x200a
	}
}
