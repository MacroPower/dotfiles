//go:build darwin

package main

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestDarwinSandbox() *darwinSandbox {
	s := &darwinSandbox{
		bin:         "/usr/bin/sandbox-exec",
		tmp:         "/var/folders/xx/T",
		terraformrc: "/Users/test/.terraformrc",
		terraformd:  "/Users/test/.terraform.d",
		pluginCache: "/Users/test/.terraform.d/plugin-cache",
	}
	s.staticProfile = s.renderStaticProfile()

	return s
}

func TestDarwinBuildProfile(t *testing.T) {
	t.Parallel()

	s := newTestDarwinSandbox()

	t.Run("deny default with no domains", func(t *testing.T) {
		t.Parallel()

		profile := s.buildProfile("/work", Policy{})
		assert.Contains(t, profile, "(version 1)")
		assert.Contains(t, profile, "(deny default)")
		assert.Contains(t, profile, "(deny network*)")
		assert.NotContains(t, profile, "remote host")
	})

	t.Run("workdir is rw", func(t *testing.T) {
		t.Parallel()

		profile := s.buildProfile("/work", Policy{})
		assert.Contains(t, profile, `(allow file-read* file-write* (subpath "/work"))`)
	})

	t.Run("plugin cache is rw inside ~/.terraform.d", func(t *testing.T) {
		t.Parallel()

		profile := s.buildProfile("/work", Policy{})
		assert.Contains(t, profile, `(allow file-read* (subpath "/Users/test/.terraform.d"))`)
		assert.Contains(t, profile, `(allow file-read* file-write* (subpath "/Users/test/.terraform.d/plugin-cache"))`)
		idxRO := strings.Index(profile, "(subpath \"/Users/test/.terraform.d\"))")
		idxRW := strings.Index(profile, "(subpath \"/Users/test/.terraform.d/plugin-cache\"))")
		assert.Less(t, idxRO, idxRW, "ro outer mount must come before rw inner mount")
	})

	t.Run("allowed domains", func(t *testing.T) {
		t.Parallel()

		profile := s.buildProfile("/work", Policy{
			AllowedDomains: []string{"registry.opentofu.org", "github.com"},
		})
		assert.Contains(t, profile, `(allow network-outbound (remote host "registry.opentofu.org"))`)
		assert.Contains(t, profile, `(allow network-outbound (remote host "github.com"))`)
		assert.NotContains(t, profile, "(deny network*)")
	})

	t.Run("allow read and write", func(t *testing.T) {
		t.Parallel()

		profile := s.buildProfile("/work", Policy{
			AllowRead:  []string{"/abs/shared"},
			AllowWrite: []string{"/abs/output"},
		})
		assert.Contains(t, profile, `(allow file-read* (subpath "/abs/shared"))`)
		assert.Contains(t, profile, `(allow file-read* file-write* (subpath "/abs/output"))`)
	})
}

func TestDarwinWrap(t *testing.T) {
	t.Parallel()

	s := newTestDarwinSandbox()

	cmd := exec.CommandContext(context.Background(), "/run/tofu", "validate", "-json")
	cmd.Dir = "/work"

	require.NoError(t, s.Wrap(cmd, Policy{}))

	assert.Equal(t, "/usr/bin/sandbox-exec", cmd.Path)
	require.GreaterOrEqual(t, len(cmd.Args), 5)
	assert.Equal(t, "sandbox-exec", cmd.Args[0])
	assert.Equal(t, "-p", cmd.Args[1])
	assert.Contains(t, cmd.Args[2], "(deny default)")
	assert.Equal(t, "/run/tofu", cmd.Args[3])
	assert.Equal(t, []string{"validate", "-json"}, cmd.Args[4:])
}

func TestDarwinWrapRequiresAbsoluteDir(t *testing.T) {
	t.Parallel()

	s := &darwinSandbox{bin: "/x", tmp: "/t"}

	cmd := exec.CommandContext(context.Background(), "/x", "y")
	cmd.Dir = "rel"

	err := s.Wrap(cmd, Policy{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSandbox)
}
