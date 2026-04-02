package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cerrdefs "github.com/containerd/errdefs"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// mockDocker implements [dockerAPI] for testing.
type mockDocker struct {
	inspectFn   func(ctx context.Context, id string) (container.InspectResponse, error)
	removeFn    func(ctx context.Context, id string, opts container.RemoveOptions) error
	imageInspFn func(ctx context.Context, id string, opts ...client.ImageInspectOption) (image.InspectResponse, error)
	imagePullFn func(ctx context.Context, ref string, opts image.PullOptions) (io.ReadCloser, error)
	createFn    func(ctx context.Context, cfg *container.Config, host *container.HostConfig, net *network.NetworkingConfig, platform *ocispec.Platform, name string) (container.CreateResponse, error)
	startFn     func(ctx context.Context, id string, opts container.StartOptions) error
}

func (m *mockDocker) ContainerInspect(ctx context.Context, id string) (container.InspectResponse, error) {
	return m.inspectFn(ctx, id)
}

func (m *mockDocker) ContainerRemove(ctx context.Context, id string, opts container.RemoveOptions) error {
	return m.removeFn(ctx, id, opts)
}

func (m *mockDocker) ImageInspect(
	ctx context.Context,
	id string,
	opts ...client.ImageInspectOption,
) (image.InspectResponse, error) {
	return m.imageInspFn(ctx, id, opts...)
}

func (m *mockDocker) ImagePull(ctx context.Context, ref string, opts image.PullOptions) (io.ReadCloser, error) {
	return m.imagePullFn(ctx, ref, opts)
}

func (m *mockDocker) ContainerCreate(
	ctx context.Context,
	cfg *container.Config,
	host *container.HostConfig,
	netCfg *network.NetworkingConfig,
	platform *ocispec.Platform,
	name string,
) (container.CreateResponse, error) {
	return m.createFn(ctx, cfg, host, netCfg, platform, name)
}

func (m *mockDocker) ContainerStart(ctx context.Context, id string, opts container.StartOptions) error {
	return m.startFn(ctx, id, opts)
}

// startPingServer starts a TCP server that responds to the proxy readiness ping.
func startPingServer(t *testing.T, port string) {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:"+port)
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc("/"+proxyAPIVersion+"/_ping", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "OK") //nolint:errcheck // best-effort response write
	})

	srv := &http.Server{Handler: mux}

	go func() {
		sErr := srv.Serve(listener)
		if sErr != nil && !errors.Is(sErr, http.ErrServerClosed) {
			t.Logf("ping server: %v", sErr)
		}
	}()

	t.Cleanup(func() {
		require.NoError(t, srv.Close())
	})
}

// allocatePort finds a free TCP port and returns it as a string.
func allocatePort(t *testing.T) string {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok)

	port := fmt.Sprintf("%d", tcpAddr.Port)

	require.NoError(t, listener.Close())

	return port
}

func TestRunWithClient(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		workdir string
		mock    *mockDocker
		port    func(t *testing.T) string
		check   func(t *testing.T, mock *mockDocker)
	}{
		"reuse running container with matching workdir": {
			workdir: "/home/user/project",
			mock: &mockDocker{
				inspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
					return container.InspectResponse{
						ContainerJSONBase: &container.ContainerJSONBase{
							State: &container.State{Running: true},
						},
						Config: &container.Config{
							Labels: map[string]string{workdirLabel: "/home/user/project"},
						},
					}, nil
				},
			},
			port: func(_ *testing.T) string { return "0" },
		},
		"replace container with wrong workdir": {
			workdir: "/home/user/project",
			mock: &mockDocker{
				inspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
					return container.InspectResponse{
						ContainerJSONBase: &container.ContainerJSONBase{
							State: &container.State{Running: true},
						},
						Config: &container.Config{
							Labels: map[string]string{workdirLabel: "/other/dir"},
						},
					}, nil
				},
				removeFn: func(_ context.Context, _ string, _ container.RemoveOptions) error {
					return nil
				},
				imageInspFn: func(_ context.Context, _ string, _ ...client.ImageInspectOption) (image.InspectResponse, error) {
					return image.InspectResponse{}, nil
				},
				createFn: func(_ context.Context, _ *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
					return container.CreateResponse{ID: "abc123"}, nil
				},
				startFn: func(_ context.Context, _ string, _ container.StartOptions) error {
					return nil
				},
			},
			port: func(t *testing.T) string {
				t.Helper()

				port := allocatePort(t)
				startPingServer(t, port)

				return port
			},
		},
		"create container when none exists": {
			workdir: "/home/user/project",
			mock: &mockDocker{
				inspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
					return container.InspectResponse{}, cerrdefs.ErrNotFound
				},
				removeFn: func(_ context.Context, _ string, _ container.RemoveOptions) error {
					return cerrdefs.ErrNotFound
				},
				imageInspFn: func(_ context.Context, _ string, _ ...client.ImageInspectOption) (image.InspectResponse, error) {
					return image.InspectResponse{}, cerrdefs.ErrNotFound
				},
				imagePullFn: func(_ context.Context, _ string, _ image.PullOptions) (io.ReadCloser, error) {
					return io.NopCloser(io.LimitReader(nil, 0)), nil
				},
				createFn: func(_ context.Context, _ *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
					return container.CreateResponse{ID: "def456"}, nil
				},
				startFn: func(_ context.Context, _ string, _ container.StartOptions) error {
					return nil
				},
			},
			port: func(t *testing.T) string {
				t.Helper()

				port := allocatePort(t)
				startPingServer(t, port)

				return port
			},
		},
		"stopped container is replaced": {
			workdir: "/home/user/project",
			mock: &mockDocker{
				inspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
					return container.InspectResponse{
						ContainerJSONBase: &container.ContainerJSONBase{
							State: &container.State{Running: false},
						},
						Config: &container.Config{
							Labels: map[string]string{workdirLabel: "/home/user/project"},
						},
					}, nil
				},
				removeFn: func(_ context.Context, _ string, _ container.RemoveOptions) error {
					return nil
				},
				imageInspFn: func(_ context.Context, _ string, _ ...client.ImageInspectOption) (image.InspectResponse, error) {
					return image.InspectResponse{}, nil
				},
				createFn: func(_ context.Context, _ *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
					return container.CreateResponse{ID: "ghi789"}, nil
				},
				startFn: func(_ context.Context, _ string, _ container.StartOptions) error {
					return nil
				},
			},
			port: func(t *testing.T) string {
				t.Helper()

				port := allocatePort(t)
				startPingServer(t, port)

				return port
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			port := tc.port(t)
			cfg := config{socketPath: "/var/run/docker.sock", image: "test:latest", port: port}

			err := runWithClient(t.Context(), tc.mock, tc.workdir, cfg)
			require.NoError(t, err)

			if tc.check != nil {
				tc.check(t, tc.mock)
			}
		})
	}
}

func TestRunWithClientAssertions(t *testing.T) {
	t.Parallel()

	t.Run("replace container verifies removal and labels", func(t *testing.T) {
		t.Parallel()

		var (
			removed     bool
			capturedCfg *container.Config
		)

		mock := &mockDocker{
			inspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
				return container.InspectResponse{
					ContainerJSONBase: &container.ContainerJSONBase{
						State: &container.State{Running: true},
					},
					Config: &container.Config{
						Labels: map[string]string{workdirLabel: "/other/dir"},
					},
				}, nil
			},
			removeFn: func(_ context.Context, _ string, _ container.RemoveOptions) error {
				removed = true
				return nil
			},
			imageInspFn: func(_ context.Context, _ string, _ ...client.ImageInspectOption) (image.InspectResponse, error) {
				return image.InspectResponse{}, nil
			},
			createFn: func(_ context.Context, cfg *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
				capturedCfg = cfg
				return container.CreateResponse{ID: "abc123"}, nil
			},
			startFn: func(_ context.Context, _ string, _ container.StartOptions) error {
				return nil
			},
		}

		port := allocatePort(t)
		startPingServer(t, port)

		cfg := config{image: "test:latest", port: port}
		err := runWithClient(t.Context(), mock, "/home/user/project", cfg)
		require.NoError(t, err)

		assert.True(t, removed, "stale container should be removed")
		require.NotNil(t, capturedCfg)
		assert.Equal(t, "/home/user/project", capturedCfg.Labels[workdirLabel])
	})

	t.Run("create container verifies security config", func(t *testing.T) {
		t.Parallel()

		var (
			pulled       bool
			capturedCfg  *container.Config
			capturedHost *container.HostConfig
		)

		mock := &mockDocker{
			inspectFn: func(_ context.Context, _ string) (container.InspectResponse, error) {
				return container.InspectResponse{}, cerrdefs.ErrNotFound
			},
			removeFn: func(_ context.Context, _ string, _ container.RemoveOptions) error {
				return cerrdefs.ErrNotFound
			},
			imageInspFn: func(_ context.Context, _ string, _ ...client.ImageInspectOption) (image.InspectResponse, error) {
				return image.InspectResponse{}, cerrdefs.ErrNotFound
			},
			imagePullFn: func(_ context.Context, _ string, _ image.PullOptions) (io.ReadCloser, error) {
				pulled = true
				return io.NopCloser(io.LimitReader(nil, 0)), nil
			},
			createFn: func(_ context.Context, cfg *container.Config, host *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
				capturedCfg = cfg
				capturedHost = host

				return container.CreateResponse{ID: "def456"}, nil
			},
			startFn: func(_ context.Context, _ string, _ container.StartOptions) error {
				return nil
			},
		}

		port := allocatePort(t)
		startPingServer(t, port)

		cfg := config{socketPath: "/var/run/docker.sock", image: "test:latest", port: port}
		err := runWithClient(t.Context(), mock, "/home/user/project", cfg)
		require.NoError(t, err)

		assert.True(t, pulled, "image should be pulled")
		require.NotNil(t, capturedCfg)
		require.NotNil(t, capturedHost)

		// Verify bind mount restriction.
		assert.Contains(t, capturedCfg.Cmd, "-allowbindmountfrom=/home/user/project")

		// Verify security hardening.
		assert.True(t, capturedHost.ReadonlyRootfs)
		assert.Contains(t, capturedHost.CapDrop, "ALL")

		// Verify socket bind mount.
		assert.Contains(t, capturedHost.Binds, "/var/run/docker.sock:/var/run/docker.sock:ro")

		// Verify networks API is not allowed (prevents sandbox network bypass).
		for _, arg := range capturedCfg.Cmd {
			assert.NotContains(t, arg, "networks", "networks API should not be allowed")
		}
	})
}

func TestConfigFromEnv(t *testing.T) {
	// Cannot use t.Parallel: t.Setenv mutates process environment.
	t.Run("defaults", func(t *testing.T) {
		t.Setenv("DOCKER_SOCKET", "")
		t.Setenv("DOCKER_PROXY_IMAGE", "")
		t.Setenv("DOCKER_PROXY_PORT", "")

		cfg := configFromEnv()
		assert.Equal(t, "/var/run/docker.sock", cfg.socketPath)
		assert.Equal(t, "ghcr.io/wollomatic/socket-proxy:1.11.4", cfg.image)
		assert.Equal(t, "2375", cfg.port)
	})

	t.Run("env overrides", func(t *testing.T) {
		t.Setenv("DOCKER_SOCKET", "/tmp/custom.sock")
		t.Setenv("DOCKER_PROXY_IMAGE", "custom:v1")
		t.Setenv("DOCKER_PROXY_PORT", "9999")

		cfg := configFromEnv()
		assert.Equal(t, "/tmp/custom.sock", cfg.socketPath)
		assert.Equal(t, "custom:v1", cfg.image)
		assert.Equal(t, "9999", cfg.port)
	})
}

func TestWaitReadyTimeout(t *testing.T) {
	t.Parallel()

	port := allocatePort(t)

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	err := waitReady(ctx, port)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not ready")
}
