package typography

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// Class buckets a disallowed rune by the ASCII remedy that applies to
// it in a [Reason] message.
type Class int

// Classes, in the order their groups appear in [Reason] output.
const (
	ClassDash Class = iota
	ClassQuote
	ClassEllipsis
)

// Change is one before/after text pair: an Edit's old and new
// strings, a Write's current on-disk content and new content, or a
// single MultiEdit edit. [Introduced] nets rune counts across all
// changes it is given, so a MultiEdit that moves a character between
// edits balances out.
type Change struct {
	Before string
	After  string
}

// Finding is one disallowed rune whose net occurrence count increased
// across a set of changes, with up to three sample lines from the
// after texts that contain it.
type Finding struct {
	Rune    rune
	Class   Class
	Samples []string
}

const (
	minusSign = '−'
	ellipsis  = '…'

	maxSamples   = 3
	maxSampleLen = 80
)

// Disallowed reports whether r is a non-ASCII typographic character
// this package blocks: dash punctuation other than the ASCII '-', the
// minus sign, a curly quotation mark, or the horizontal ellipsis.
func Disallowed(r rune) bool {
	_, ok := classify(r)

	return ok
}

// classify returns r's [Class] and true when [Disallowed] holds,
// otherwise the zero Class and false.
func classify(r rune) (Class, bool) {
	switch {
	case r == minusSign, r != '-' && unicode.Is(unicode.Pd, r):
		return ClassDash, true
	case r >= '‘' && r <= '‟':
		return ClassQuote, true
	case r == ellipsis:
		return ClassEllipsis, true
	}

	return 0, false
}

// Introduced returns one [Finding] per disallowed rune whose net
// occurrence count across changes is positive, i.e. the runes the
// change set newly adds. Counts are summed over every Before and
// every After before comparing, so moving a pre-existing character
// between changes nets to zero. Findings are ordered by rune for
// deterministic messages.
func Introduced(changes ...Change) []Finding {
	delta := map[rune]int{}

	for _, c := range changes {
		for _, r := range c.After {
			if Disallowed(r) {
				delta[r]++
			}
		}

		for _, r := range c.Before {
			if Disallowed(r) {
				delta[r]--
			}
		}
	}

	runes := make([]rune, 0, len(delta))

	for r, d := range delta {
		if d > 0 {
			runes = append(runes, r)
		}
	}

	slices.Sort(runes)

	findings := make([]Finding, 0, len(runes))

	for _, r := range runes {
		class, _ := classify(r)
		findings = append(findings, Finding{
			Rune:    r,
			Class:   class,
			Samples: sampleLines(changes, r),
		})
	}

	return findings
}

// sampleLines collects up to maxSamples distinct trimmed lines
// containing r from the changes' after texts, each capped at
// maxSampleLen runes.
func sampleLines(changes []Change, r rune) []string {
	var out []string

	seen := map[string]struct{}{}

	for _, c := range changes {
		for _, line := range strings.Split(c.After, "\n") {
			if !strings.ContainsRune(line, r) {
				continue
			}

			s := truncate(strings.TrimSpace(line), maxSampleLen)
			if _, ok := seen[s]; ok {
				continue
			}

			seen[s] = struct{}{}

			out = append(out, s)
			if len(out) == maxSamples {
				return out
			}
		}
	}

	return out
}

// truncate caps s at n runes, appending "..." when it was cut.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}

	return string(runes[:n]) + "..."
}

// classInfo carries each [Class]'s [Reason] group header and ASCII
// replacement guidance, in output order.
var classInfo = []struct {
	class    Class
	label    string
	guidance string
}{
	{ClassDash, "Dashes", `restructure the sentence, use (e.g. ...) or (i.e., ...), a colon, or a comma; if a dash is truly best, use a plain ASCII "--".`},
	{ClassQuote, "Curly quotes", `use ASCII straight quotes ' and ".`},
	{ClassEllipsis, "Ellipsis", `use three ASCII periods "...".`},
}

// runeNames covers the Unicode names of the commonly-introduced runes
// so [Reason] can label them without a Unicode data dependency; other
// runes fall back to a bare U+XXXX code.
var runeNames = map[rune]string{
	'‐':       "HYPHEN",
	'‑':       "NON-BREAKING HYPHEN",
	'‒':       "FIGURE DASH",
	'–':       "EN DASH",
	'—':       "EM DASH",
	'―':       "HORIZONTAL BAR",
	minusSign: "MINUS SIGN",
	'‘':       "LEFT SINGLE QUOTATION MARK",
	'’':       "RIGHT SINGLE QUOTATION MARK",
	'‚':       "SINGLE LOW-9 QUOTATION MARK",
	'‛':       "SINGLE HIGH-REVERSED-9 QUOTATION MARK",
	'“':       "LEFT DOUBLE QUOTATION MARK",
	'”':       "RIGHT DOUBLE QUOTATION MARK",
	'„':       "DOUBLE LOW-9 QUOTATION MARK",
	'‟':       "DOUBLE HIGH-REVERSED-9 QUOTATION MARK",
	ellipsis:  "HORIZONTAL ELLIPSIS",
}

// describe renders r as "U+XXXX NAME", or "U+XXXX" when the name is
// not in runeNames.
func describe(r rune) string {
	code := fmt.Sprintf("U+%04X", r)
	if name, ok := runeNames[r]; ok {
		return code + " " + name
	}

	return code
}

// Reason renders the deny message for findings: a header naming
// filePath (omitted when empty) and stating the introduced-only rule,
// then one group per [Class] listing the offending runes with their
// U+XXXX codes, the class's ASCII replacement guidance, and sample
// lines. Returns "" when findings is empty.
func Reason(filePath string, findings []Finding) string {
	if len(findings) == 0 {
		return ""
	}

	var b strings.Builder

	if filePath != "" {
		fmt.Fprintf(&b, "Non-ASCII typographic characters were introduced into %s. ", filePath)
	} else {
		b.WriteString("Non-ASCII typographic characters were introduced. ")
	}

	b.WriteString("Replace them with ASCII equivalents. " +
		"Only newly introduced characters are blocked; " +
		"characters already present in the file are fine to keep.")

	for _, info := range classInfo {
		var group []Finding

		for _, f := range findings {
			if f.Class == info.class {
				group = append(group, f)
			}
		}

		if len(group) == 0 {
			continue
		}

		names := make([]string, 0, len(group))
		for _, f := range group {
			names = append(names, describe(f.Rune))
		}

		fmt.Fprintf(&b, "\n\n%s (%s): %s",
			info.label, strings.Join(names, ", "), info.guidance)

		for _, f := range group {
			for _, s := range f.Samples {
				b.WriteString("\n  ")
				b.WriteString(s)
			}
		}
	}

	return b.String()
}
