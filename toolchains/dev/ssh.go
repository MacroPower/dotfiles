// SSH readiness for the published shell image.
//
// The shell image is deployed as a long-lived utility container reachable
// over SSH (e.g. the TrueNAS `shell` compose service). OpenSSH has several
// hard requirements that a nix/home-manager rootfs does not meet out of
// the box: a privilege-separation user, an unlocked root shadow entry, a
// login shell that bootstraps the home-manager environment, and host key
// management. Everything static is baked into the image at build time by
// [withSSHReadiness]; runtime-only concerns (host keys, authorized_keys,
// idmapped mounts, per-mount trash dirs, persistence volumes) ship as
// env-driven scripts so deployments shrink to environment variables plus
// a one-line command:
//
//	command: ["container-init", "sshd-entrypoint"]
//
// The scripts live in scripts/ and are embedded at compile time. They
// hardcode the image layout (/home/dev, the /usr/local/bin install
// paths, /etc/ssh/keys), so changes there must stay in sync with the
// constants below. Scripts are installed under /usr/local/bin, which
// buildBase puts on the image PATH. Host keys and the authorized_keys
// cache live in /etc/ssh/keys; deployments mount a persistent volume
// there so the server fingerprint survives container recreates.

package main

import (
	_ "embed"

	"dagger/dev/internal/dagger"
)

const (
	// loginWrapperPath is root's login shell in the published shell
	// image. sshd execs it for interactive sessions and remote commands.
	loginWrapperPath = "/usr/local/bin/fish-login"

	// containerInitPath prepares env-driven runtime state (persistence
	// volumes, idmapped mounts, gomi trash dirs) and execs its arguments.
	containerInitPath = "/usr/local/bin/container-init"

	// sshdEntrypointPath provisions host keys and authorized_keys, then
	// execs sshd in the foreground.
	sshdEntrypointPath = "/usr/local/bin/sshd-entrypoint"
)

var (
	// loginWrapperScript recreates the environment the image's default
	// command would have provided before handing control to fish.
	//go:embed scripts/fish-login.sh
	loginWrapperScript string

	// containerInitScript handles env-driven runtime setup that must
	// happen before the main process starts, then execs its arguments.
	//go:embed scripts/container-init.sh
	containerInitScript string

	// sshdEntrypointScript provisions sshd's runtime state and execs it
	// in the foreground.
	//go:embed scripts/sshd-entrypoint.sh
	sshdEntrypointScript string

	// sshSetupScript applies the static, build-time half of SSH
	// readiness: the sshd privilege-separation user, the privsep chroot
	// dir, lastlog, an unlocked root shadow entry, and root's passwd
	// entry pointing at homeDir and the fish login wrapper.
	//go:embed scripts/setup-ssh.sh
	sshSetupScript string
)

// withSSHReadiness bakes SSH support into the shell image: the login
// wrapper, the runtime helper scripts, and the static passwd/shadow
// fixups OpenSSH requires. Runtime-only state (host keys,
// authorized_keys) is left to sshd-entrypoint, driven by environment
// variables, so nothing secret or machine-specific lands in image
// layers.
func withSSHReadiness(ctr *dagger.Container) *dagger.Container {
	exec := dagger.ContainerWithNewFileOpts{Permissions: 0o755}

	return ctr.
		WithNewFile(loginWrapperPath, loginWrapperScript, exec).
		WithNewFile(containerInitPath, containerInitScript, exec).
		WithNewFile(sshdEntrypointPath, sshdEntrypointScript, exec).
		WithExec([]string{"sh", "-c", sshSetupScript})
}
