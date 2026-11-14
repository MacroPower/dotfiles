package fold_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

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
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, fold.Name(tc.text))
		})
	}
}

func TestEqual(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		a, b string
		want bool
	}{
		"same text":             {a: "X\u00fbr", b: "X\u00fbr", want: true},
		"letter case":           {a: "XUR", b: "xur", want: true},
		"accent on one side":    {a: "X\u00fbr", b: "xur", want: true},
		"accent on the other":   {a: "xur", b: "X\u00fbr", want: true},
		"combining and precomp": {a: "Xu\u0302r", b: "X\u00fbr", want: true},
		"different letters":     {a: "X\u00fbr", b: "xor", want: false},
		"part of the text":      {a: "X\u00fbr Prime", b: "xur", want: false},
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
		"part without an accent": {text: "Mataiodox\u00eda", substr: "doxia", want: true},
		"part with an accent":    {text: "Mataiodoxia", substr: "DOX\u00cdA", want: true},
		"letter case":            {text: "Gnawing Hunger", substr: "HUNGER", want: true},
		"empty part":             {text: "Gnawing Hunger", substr: "", want: true},
		"missing part":           {text: "Gnawing Hunger", substr: "thirst", want: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, fold.Contains(tc.text, tc.substr))
		})
	}
}
