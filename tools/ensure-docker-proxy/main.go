package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"

	cerrdefs "github.com/containerd/errdefs"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const (
	containerName   = "claude-docker-proxy"
	proxyAPIVersion = "v1.47"
	workdirLabel    = "claude.workdir"
)

// dockerAPI abstracts the [client.Client] methods used by this tool.
type dockerAPI interface { //nolint:dupl // mirrors mockDocker in tests
	ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error)
	ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
	ImageInspect(
		ctx context.Context,
		imageID string,
		options ...client.ImageInspectOption,
	) (image.InspectResponse, error)
	ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error)
	ContainerCreate(
		ctx context.Context,
		config *container.Config,
		hostConfig *container.HostConfig,
		networkingConfig *network.NetworkingConfig,
		platform *ocispec.Platform,
		containerName string,
	) (container.CreateResponse, error)
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
}

// config holds runtime settings resolved from the environment.
type config struct {
	socketPath string
	image      string
	port       string
}

func configFromEnv() config {
	cfg := config{
		socketPath: "/var/run/docker.sock",
		image:      "ghcr.io/wollomatic/socket-proxy:1.11.4",
		port:       "2375",
	}

	if v := os.Getenv("DOCKER_SOCKET"); v != "" {
		cfg.socketPath = v
	}

	if v := os.Getenv("DOCKER_PROXY_IMAGE"); v != "" {
		cfg.image = v
	}

	if v := os.Getenv("DOCKER_PROXY_PORT"); v != "" {
		cfg.port = v
	}

	return cfg
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: ensure-docker-proxy <workdir>")
		os.Exit(1)
	}

	err := run(context.Background(), os.Args[1], configFromEnv())
	if err != nil {
		fmt.Fprintf(os.Stderr, "ensure-docker-proxy: %v\n", err)
		os.Exit(1)
	}
}

// newDockerClient returns a Docker SDK client that talks to the daemon
// over the given Unix socket.
func newDockerClient(socketPath string) (*client.Client, error) {
	c, err := client.NewClientWithOpts(
		client.WithHost("unix://"+socketPath),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}

	return c, nil
}

func run(ctx context.Context, workdir string, cfg config) error {
	dc, err := newDockerClient(cfg.socketPath)
	if err != nil {
		return fmt.Errorf("creating docker client: %w", err)
	}

	return runWithClient(ctx, dc, workdir, cfg)
}

func runWithClient(ctx context.Context, dc dockerAPI, workdir string, cfg config) error {
	// Check if a proxy container already exists.
	ok, err := inspectAndReuse(ctx, dc, workdir)
	if err != nil {
		return err
	}

	if ok {
		return nil
	}

	// Remove any stale container (best-effort).
	err = removeContainer(ctx, dc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ensure-docker-proxy: removing stale container: %v\n", err)
	}

	err = ensureImage(ctx, dc, cfg.image)
	if err != nil {
		return err
	}

	err = createContainer(ctx, dc, workdir, cfg)
	if err != nil {
		return err
	}

	err = startContainer(ctx, dc)
	if err != nil {
		return err
	}

	return waitReady(ctx, cfg.port)
}

// inspectAndReuse reports whether a running proxy container already
// exists for the given workdir and can be reused.
func inspectAndReuse(ctx context.Context, dc dockerAPI, workdir string) (bool, error) {
	info, err := dc.ContainerInspect(ctx, containerName)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return false, nil
		}

		return false, fmt.Errorf("inspecting container: %w", err)
	}

	if info.State.Running && info.Config.Labels[workdirLabel] == workdir {
		return true, nil
	}

	return false, nil
}

func removeContainer(ctx context.Context, dc dockerAPI) error {
	err := dc.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true})
	if err != nil && !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("removing container: %w", err)
	}

	return nil
}

func ensureImage(ctx context.Context, dc dockerAPI, imageName string) error {
	_, err := dc.ImageInspect(ctx, imageName)
	if err == nil {
		return nil
	}

	if !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("inspecting image: %w", err)
	}

	rc, err := dc.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pulling image %s: %w", imageName, err)
	}

	defer func() {
		cErr := rc.Close()
		if cErr != nil {
			slog.ErrorContext(ctx, "closing pull response",
				slog.Any("error", cErr),
			)
		}
	}()

	// Drain the response body (Docker streams progress JSON).
	_, drainErr := io.Copy(io.Discard, rc)
	if drainErr != nil {
		slog.ErrorContext(ctx, "draining pull response",
			slog.Any("error", drainErr),
		)
	}

	return nil
}

func createContainer(ctx context.Context, dc dockerAPI, workdir string, cfg config) error {
	containerCfg := &container.Config{
		Image: cfg.image,
		Labels: map[string]string{
			workdirLabel: workdir,
		},
		Cmd: []string{
			"-listenip=0.0.0.0",
			`-allowGET=/v1\..{1,2}/.*`,
			`-allowHEAD=/v1\..{1,2}/.*`,
			`-allowPOST=/v1\..{1,2}/(build|containers|images|volumes).*`,
			`-allowDELETE=/v1\..{1,2}/(containers|images|volumes)/.*`,
			"-allowbindmountfrom=" + workdir,
		},
		ExposedPorts: nat.PortSet{
			"2375/tcp": struct{}{},
		},
	}

	hostCfg := &container.HostConfig{
		Binds: []string{
			cfg.socketPath + ":/var/run/docker.sock:ro",
		},
		PortBindings: nat.PortMap{
			"2375/tcp": []nat.PortBinding{
				{HostIP: "127.0.0.1", HostPort: cfg.port},
			},
		},
		ReadonlyRootfs: true,
		CapDrop:        []string{"ALL"},
		SecurityOpt:    []string{"no-new-privileges"},
	}

	_, err := dc.ContainerCreate(ctx, containerCfg, hostCfg, nil, nil, containerName)
	if err != nil {
		return fmt.Errorf("creating container: %w", err)
	}

	return nil
}

func startContainer(ctx context.Context, dc dockerAPI) error {
	err := dc.ContainerStart(ctx, containerName, container.StartOptions{})
	if err != nil {
		return fmt.Errorf("starting container: %w", err)
	}

	return nil
}

// waitReady polls the proxy's /_ping endpoint until it responds 200 OK
// or a 5-second timeout elapses.
func waitReady(ctx context.Context, port string) error {
	const timeout = 5 * time.Second

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pingURL := "http://127.0.0.1:" + port + "/" + proxyAPIVersion + "/_ping"
	httpClient := &http.Client{Timeout: 500 * time.Millisecond}

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pingURL, http.NoBody)
		if err != nil {
			return fmt.Errorf("proxy not ready on port %s: %w", port, err)
		}

		resp, err := httpClient.Do(req)
		if err == nil {
			cErr := resp.Body.Close()
			if cErr != nil {
				slog.ErrorContext(ctx, "closing response body",
					slog.Any("error", cErr),
				)
			}

			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("proxy not ready after %s on port %s", timeout, port)
		case <-time.After(100 * time.Millisecond):
		}
	}
}
