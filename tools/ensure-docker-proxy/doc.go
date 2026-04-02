// Ensure-docker-proxy manages a wollomatic/socket-proxy container that
// restricts Docker API access for Claude Code sessions.
//
// It communicates with the Docker daemon using the Docker Engine SDK
// (github.com/docker/docker/client), which talks directly to the Unix
// socket. This avoids any dependency on the docker CLI, which is
// critical because the hook-router rewrites docker CLI invocations to
// route through the proxy -- using the CLI here would cause infinite
// recursion.
//
// The proxy container is named "claude-docker-proxy" and is scoped to
// a working directory via -allowbindmountfrom. If a proxy is already
// running for the requested directory it is reused; otherwise any stale
// proxy is replaced.
//
// # Usage
//
//	ensure-docker-proxy <workdir>
//
// # Environment
//
//   - DOCKER_SOCKET: path to the Docker Unix socket
//     (default /var/run/docker.sock).
//   - DOCKER_PROXY_IMAGE: proxy image reference
//     (default ghcr.io/wollomatic/socket-proxy:1.11.4).
//   - DOCKER_PROXY_PORT: host port the proxy listens on
//     (default 2375).
package main
