package sandbox_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go.jacobcolvin.com/dotfiles/toolchains/dev/sandbox"
)

func TestParseUpstreamDNS(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		resolvConf string
		want       string
	}{
		"standard": {
			resolvConf: "nameserver 8.8.8.8\nnameserver 8.8.4.4\n",
			want:       "8.8.8.8",
		},
		"multiple nameservers": {
			resolvConf: "search example.com\nnameserver 1.1.1.1\nnameserver 8.8.8.8\n",
			want:       "1.1.1.1",
		},
		"ipv6": {
			resolvConf: "nameserver ::1\nnameserver 8.8.8.8\n",
			want:       "::1",
		},
		"empty": {
			resolvConf: "search example.com\n",
		},
		"comments and whitespace": {
			resolvConf: "# comment\n  nameserver  10.0.0.1  \n",
			want:       "10.0.0.1",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, sandbox.ParseUpstreamDNS(tt.resolvConf))
		})
	}
}
