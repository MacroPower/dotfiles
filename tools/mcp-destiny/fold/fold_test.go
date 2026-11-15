package fold_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/fold"
)

func TestName(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		text string
		want string
	}{
		"plain":                   {text: "Fatebringer", want: "fatebringer"},
		"precomposed diacritics":  {text: "D\u00e9j\u00e0 Vu", want: "deja vu"},
		"combining diacritics":    {text: "De\u0301ja\u0300 Vu", want: "deja vu"},
		"capital I with dot":      {text: "\u0130stanbul", want: "istanbul"},
		"letter without a decomp": {text: "\u00d8rsted", want: "\u00f8rsted"},
		"typographic quote stays": {text: "It\u2019s", want: "it\u2019s"},
		"spacing diacritics":      {text: "x^y`z", want: "xyz"},
		"digit for a letter":      {text: "St0mp-EE5", want: "st0mp-ee5"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, fold.Name(tc.text))
		})
	}
}

func TestLookalike(t *testing.T) {
	t.Parallel()

	// The first group holds names from the definitions of the game that
	// write a digit in place of a letter. The second group holds other
	// texts, such as names that write a number. Want lists texts that the
	// text matches under Lookalike.
	tests := map[string]struct {
		text string
		want []string
	}{
		"St0mp-EE5":               {text: "St0mp-EE5", want: []string{"stomp-ees"}},
		"Fr0st-EE5":               {text: "Fr0st-EE5", want: []string{"frost-ees"}},
		"Gr1p-EE5":                {text: "Gr1p-EE5", want: []string{"grip-ees"}},
		"BrayTech Sn0Boots":       {text: "BrayTech Sn0Boots", want: []string{"braytech snoboots"}},
		"BrayTech Sn0Helm":        {text: "BrayTech Sn0Helm", want: []string{"braytech snohelm"}},
		"BrayTech Sn0Mask":        {text: "BrayTech Sn0Mask", want: []string{"braytech snomask"}},
		"BrayTech Sn0Treads":      {text: "BrayTech Sn0Treads", want: []string{"braytech snotreads"}},
		"M1R Distribution Matrix": {text: "M1R Distribution Matrix", want: []string{"mir distribution matrix"}},
		"M3-D1 Shell":             {text: "M3-D1 Shell", want: []string{"me-di shell"}},
		"M3-D1 Accessory Pack":    {text: "M3-D1 Accessory Pack", want: []string{"me-di accessory pack"}},
		"N33-DL":                  {text: "N33-DL", want: []string{"nee-dl"}},
		"R3-Treads":               {text: "R3-Treads", want: []string{"re-treads"}},
		"5p 3AR":                  {text: "5p 3AR", want: []string{"sp ear"}},
		"E99 Shell":               {text: "E99 Shell", want: []string{"egg shell"}},
		"Se7enth":                 {text: "Se7enth", want: []string{"seventh", "selenth"}},
		"S733k Pack":              {text: "S733k Pack", want: []string{"sleek pack", "sveek pack"}},

		"number in a name":         {text: "1000 Yard Stare", want: []string{"iooo yard stare"}},
		"number after a name":      {text: "DEADHORSE 04", want: []string{"deadhorse o4"}},
		"digits without a letter":  {text: "Banshee-44", want: []string{"banshee-44"}},
		"every digit":              {text: "0123456789", want: []string{"oi2e4s6v8g", "oi2e4s6l8g"}},
		"name without a digit":     {text: "Fatebringer", want: []string{"fatebringer"}},
		"diacritics and a digit":   {text: "D\u00e9j\u00e0 Vu 3", want: []string{"deja vu e"}},
		"letters stay as they are": {text: "oiesvlg OIESVLG", want: []string{"oiesvlg oiesvlg"}},
		"look-alike letters stay":  {text: "Kill Clip", want: []string{"kill clip"}},
		"empty":                    {text: "", want: []string{""}},
		"diacritic marks only":     {text: "^", want: []string{""}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for _, want := range tc.want {
				assert.True(t, fold.Lookalike.Equal(tc.text, want), "%q matches %q", tc.text, want)
			}
		})
	}
}

func TestPasses(t *testing.T) {
	t.Parallel()

	passes := fold.Passes()
	require.Len(t, passes, 2)

	// The first fold keeps the digits, and the second reads them as
	// letters.
	tests := map[string]struct {
		a, b string
		want []bool
	}{
		"letter case and accents": {a: "St0mp-\u00c9E5", b: "st0mp-ee5", want: []bool{true, true}},
		"letters for digits":      {a: "St0mp-\u00c9E5", b: "stomp-ees", want: []bool{false, true}},
		"seven for l":             {a: "S733k Pack", b: "sleek pack", want: []bool{false, true}},
		"seven for v":             {a: "Se7enth", b: "seventh", want: []bool{false, true}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := make([]bool, 0, len(passes))
			for _, f := range passes {
				got = append(got, f.Equal(tc.a, tc.b))
			}

			assert.Equal(t, tc.want, got)
		})
	}
}

func TestText(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		text  fold.Text
		name  string
		equal bool
		in    bool
		empty bool
	}{
		"whole name in letters": {
			text:  fold.Lookalike.Text("sleek pack"),
			name:  "S733k Pack",
			equal: true,
			in:    true,
		},
		"part of a name in letters": {
			text: fold.Lookalike.Text("sleek"),
			name: "S733k Pack",
			in:   true,
		},
		"part of a name in the other reading": {
			text: fold.Lookalike.Text("sveek"),
			name: "S733k Pack",
			in:   true,
		},
		"l for v": {
			text: fold.Lookalike.Text("sveek"),
			name: "Sleek Pack",
		},
		"empty text": {
			text:  fold.Lookalike.Text("^"),
			name:  "S733k Pack",
			in:    true,
			empty: true,
		},
		"suffix with a digit for a letter": {
			text:  fold.Lookalike.Text("Rampage 3nhanced").TrimSuffix(" enhanced"),
			name:  "Rampag3",
			equal: true,
			in:    true,
		},
		"suffix in each reading": {
			text:  fold.Lookalike.Text("S733k Pack").TrimSuffix(" pack"),
			name:  "sleek",
			equal: true,
			in:    true,
		},
		"text without the suffix": {
			text:  fold.Lookalike.Text("Rampage").TrimSuffix(" enhanced"),
			name:  "rampage",
			equal: true,
			in:    true,
		},
		"suffix with a digit under the first fold": {
			text:  fold.Passes()[0].Text("Rampage 3nhanced").TrimSuffix(" enhanced"),
			name:  "Rampage 3nhanced",
			equal: true,
			in:    true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.equal, tc.text.Equal(tc.name), "equal")
			assert.Equal(t, tc.in, tc.text.In(tc.name), "in")
			assert.Equal(t, tc.empty, tc.text.Empty(), "empty")
		})
	}
}

func TestEqual(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		a, b string
		want bool
	}{
		"same text":               {a: "X\u00fbr", b: "X\u00fbr", want: true},
		"letter case":             {a: "XUR", b: "xur", want: true},
		"accent on one side":      {a: "X\u00fbr", b: "xur", want: true},
		"accent on the other":     {a: "xur", b: "X\u00fbr", want: true},
		"combining and precomp":   {a: "Xu\u0302r", b: "X\u00fbr", want: true},
		"different letters":       {a: "X\u00fbr", b: "xor", want: false},
		"part of the text":        {a: "X\u00fbr Prime", b: "xur", want: false},
		"letters for digits":      {a: "St0mp-EE5", b: "stomp-ees", want: true},
		"digits for letters":      {a: "stomp-ees", b: "ST0MP-EE5", want: true},
		"some digits as letters":  {a: "St0mp-EE5", b: "Stomp-EE5", want: true},
		"other digits":            {a: "St0mp-EE5", b: "5t0mp-33s", want: true},
		"another digit":           {a: "St0mp-EE5", b: "St9mp-EE5", want: false},
		"digit without a letter":  {a: "Banshee-44", b: "banshee-aa", want: false},
		"seven for v":             {a: "Se7enth", b: "seventh", want: true},
		"seven for l":             {a: "S733k Pack", b: "sleek pack", want: true},
		"l for seven":             {a: "sleek pack", b: "S733K PACK", want: true},
		"seven for l, read as v":  {a: "S733k Pack", b: "sveek pack", want: true},
		"l for v":                 {a: "sleek pack", b: "sveek pack", want: false},
		"look-alike letters":      {a: "Kill Clip", b: "Kiii Ciip", want: false},
		"number and its letters":  {a: "1000 Yard Stare", b: "iooo yard stare", want: true},
		"number in another digit": {a: "1000 Yard Stare", b: "1001 Yard Stare", want: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, fold.Equal(tc.a, tc.b))
		})
	}
}

func TestContains(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		text   string
		substr string
		want   bool
	}{
		"part without an accent":   {text: "Mataiodox\u00eda", substr: "doxia", want: true},
		"part with an accent":      {text: "Mataiodoxia", substr: "DOX\u00cdA", want: true},
		"letter case":              {text: "Gnawing Hunger", substr: "HUNGER", want: true},
		"empty part":               {text: "Gnawing Hunger", substr: "", want: true},
		"missing part":             {text: "Gnawing Hunger", substr: "thirst", want: false},
		"letters for digits":       {text: "St0mp-EE5", substr: "stomp", want: true},
		"digits as the name has":   {text: "St0mp-EE5", substr: "st0mp", want: true},
		"letters and digits":       {text: "St0mp-EE5", substr: "stomp-ee5", want: true},
		"digits for letters":       {text: "Stomp-EES", substr: "st0mp-ee5", want: true},
		"letters across a space":   {text: "5p 3AR", substr: "sp ear", want: true},
		"letters across a hyphen":  {text: "M3-D1 Shell", substr: "me-di", want: true},
		"letters without a hyphen": {text: "M3-D1 Shell", substr: "medi", want: false},
		"seven for v":              {text: "Se7enth", substr: "seven", want: true},
		"seven as the name has":    {text: "S733k Pack", substr: "s733k", want: true},
		"seven for l":              {text: "S733k Pack", substr: "sleek", want: true},
		"l for seven":              {text: "Sleek Pack", substr: "s733k", want: true},
		"l for v":                  {text: "Sleek Pack", substr: "sveek", want: false},
		"number as the name has":   {text: "1000 Yard Stare", substr: "1000 yard", want: true},
		"letters for a number":     {text: "1000 Yard Stare", substr: "ooo", want: true},
		"number for letters":       {text: "Dominion", substr: "10", want: true},
		"digit without a letter":   {text: "Banshee-44", substr: "aa", want: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, fold.Contains(tc.text, tc.substr))
		})
	}
}
