//go:build linux

package main

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLinuxBuildArgs(t *testing.T) {
	t.Parallel()

	s := newTestLinuxSandbox()

	t.Run("clearenv first", func(t *testing.T) {
		t.Parallel()

		args := s.buildArgs("/work", "/run/tofu", []string{"tofu", "validate"}, Policy{})
		require.GreaterOrEqual(t, len(args), 2)
		assert.Equal(t, "bwrap", args[0])
		assert.Equal(t, "--clearenv", args[1])
	})

	t.Run("unshare-net when no domains", func(t *testing.T) {
		t.Parallel()

		args := s.buildArgs("/work", "/run/tofu", nil, Policy{})
		assert.Contains(t, args, "--unshare-net")
		assert.NotContains(t, args, "--share-net")
	})

	t.Run("share-net when domains present", func(t *testing.T) {
		t.Parallel()

		args := s.buildArgs("/work", "/run/tofu", nil, Policy{
			AllowedDomains: []string{"registry.opentofu.org"},
		})
		assert.NotContains(t, args, "--unshare-net")
		assert.Contains(t, args, "--share-net")
	})

	t.Run("plugin cache binds rw after ~/.terraform.d ro", func(t *testing.T) {
		t.Parallel()

		args := s.buildArgs("/work", "/run/tofu", nil, Policy{})
		joined := strings.Join(args, " ")
		idxRO := strings.Index(joined, "--ro-bind-try /home/test/.terraform.d /home/test/.terraform.d")
		idxRW := strings.Index(joined, "--bind-try /home/test/.terraform.d/plugin-cache /home/test/.terraform.d/plugin-cache")
		require.Greater(t, idxRO, -1, "ro bind missing")
		require.Greater(t, idxRW, -1, "rw plugin-cache bind missing")
		assert.Less(t, idxRO, idxRW, "rw plugin-cache must come after the ro outer bind")
	})

	t.Run("allow-read and allow-write paths", func(t *testing.T) {
		t.Parallel()

		args := s.buildArgs("/work", "/run/tofu", nil, Policy{
			AllowRead:  []string{"/shared"},
			AllowWrite: []string{"/output"},
		})
		joined := strings.Join(args, " ")
		assert.Contains(t, joined, "--ro-bind /shared /shared")
		assert.Contains(t, joined, "--bind /output /output")
	})

	t.Run("workdir is rw bind", func(t *testing.T) {
		t.Parallel()

		args := s.buildArgs("/work", "/run/tofu", nil, Policy{})
		joined := strings.Join(args, " ")
		assert.Contains(t, joined, "--bind /work /work")
		assert.Contains(t, joined, "--chdir /work")
	})

	t.Run("die-with-parent set", func(t *testing.T) {
		t.Parallel()

		args := s.buildArgs("/work", "/run/tofu", nil, Policy{})
		assert.Contains(t, args, "--die-with-parent")
	})

	t.Run("argv terminator splits bwrap from tofu", func(t *testing.T) {
		t.Parallel()

		args := s.buildArgs("/work", "/run/tofu", []string{"tofu", "validate", "-json"}, Policy{})

		var sep int
		for i, a := range args {
			if a == "--" {
				sep = i
				break
			}
		}
		require.Greater(t, sep, 0)

		assert.Equal(t, "/run/tofu", args[sep+1])
		assert.Equal(t, []string{"validate", "-json"}, args[sep+2:])
	})
}

func TestLinuxWrap(t *testing.T) {
	t.Parallel()

	s := newTestLinuxSandbox()
	s.bin = "/run/current-system/sw/bin/bwrap"

	cmd := exec.CommandContext(context.Background(), "/run/tofu", "validate", "-json")
	cmd.Dir = "/work"

	require.NoError(t, s.Wrap(cmd, Policy{}))
	assert.Equal(t, "/run/current-system/sw/bin/bwrap", cmd.Path)
	assert.Equal(t, "bwrap", cmd.Args[0])
}

func TestLinuxWrapRequiresAbsoluteDir(t *testing.T) {
	t.Parallel()

	s := &linuxSandbox{bin: "/x", home: "/h"}

	cmd := exec.CommandContext(context.Background(), "/x", "y")
	cmd.Dir = "rel"

	err := s.Wrap(cmd, Policy{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSandbox)
}

func newTestLinuxSandbox() *linuxSandbox {
	s := &linuxSandbox{
		bin:         "/run/current-system/sw/bin/bwrap",
		home:        "/home/test",
		terraformrc: "/home/test/.terraformrc",
		terraformd:  "/home/test/.terraform.d",
		pluginCache: "/home/test/.terraform.d/plugin-cache",
	}
	s.staticPrefix = s.renderStaticPrefix()

	return s
}
