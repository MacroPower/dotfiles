# Dev Toolchain

Dagger module providing containerized development environments powered by Nix
home-manager. The module builds two container types from the same base: **Shell**
(unrestricted network) and **Sandbox** (network-isolated with domain allowlisting).

Both containers activate the `dev@linux` home-manager configuration from the
dotfiles flake, giving you fish shell, git, go, node, kubectl, helm, vim, and
the rest of the toolset defined in `home/`.

## Shell

The Shell container is a full development environment with unrestricted network
access. It enables `ExperimentalPrivilegedNesting`, which allows running Dagger
pipelines and Docker containers from within the shell.

```bash
# Via task runner (mounts current directory, injects secrets via sops)
task dev

# Via dagger directly
dagger call dev shell --repo-source . --interactive
```

The shell mounts your source directory at `/src` and your git configuration
from `~/.config/git`. An atuin daemon is started in the background before
the shell launches (the container has no systemd to manage it as a service).

## Sandbox

The Sandbox container provides the same development environment as Shell but
enforces strict outbound network controls. Only traffic to explicitly
allowlisted domains is permitted. All other outbound connections are dropped.

```bash
# Via task runner
task sandbox

# Via dagger directly
dagger call dev sandbox --interactive

# With a custom config
dagger call dev sandbox --sandbox-config ./my-config.yaml --interactive
```

Unlike Shell, the Sandbox uses `InsecureRootCapabilities` instead of
`ExperimentalPrivilegedNesting`. The container needs root capabilities to load
iptables rules, but deliberately does not enable privileged nesting -- that
would allow processes inside the container to manipulate the network namespace
and bypass the firewall.

### Privilege Separation

The sandbox uses Linux privilege separation to prevent the sandboxed process
from modifying the firewall rules that constrain it. Three UIDs are involved:

| UID | Process | Capabilities |
|-----|---------|-------------|
| 0 (root) | init (transient) | Full -- loads iptables, starts services, then exits |
| 999 | Envoy | None -- runs with `--clear-groups --no-new-privs` |
| 1000 | User shell | None -- runs with `--inh-caps=-all --bounding-set=-all --no-new-privs` |

The init process sets up the firewall as root, then permanently drops
privileges before executing the user's shell. The privilege drop uses `setpriv`
with three flags that work together to make the restriction irrevocable:

- **`--inh-caps=-all`**: Clears all inheritable capabilities. Even if the user
  executes a setuid binary, it cannot inherit any capabilities.
- **`--bounding-set=-all`**: Removes every capability from the bounding set.
  This is a one-way operation -- once a capability is removed from the bounding
  set, no process in the tree can ever reacquire it.
- **`--no-new-privs`**: Sets the `PR_SET_NO_NEW_PRIVS` flag on the process.
  This prevents all child processes from gaining privileges through execve
  (blocks setuid/setgid transitions and capability acquisition).

Modifying iptables requires `CAP_NET_ADMIN`, which is permanently removed by
the bounding set restriction. There is no mechanism in the Linux kernel to
restore a capability once it has been removed from the bounding set with
`no_new_privs` active.

### Pre-drop Validation

Before dropping privileges, the init process validates that the security
infrastructure is operational:

1. **iptables validation**: Runs `iptables-save` and checks that REDIRECT rules
   are present. If not, init exits with `ErrIptablesNotLoaded` rather than
   starting an unprotected shell.
2. **IPv6 validation**: Checks that ip6tables REDIRECT rules loaded, OR that
   IPv6 has been disabled via sysctl. Exits with `ErrIPv6Unsecured` if neither
   condition is met.
3. **Envoy validation**: Sends signal 0 to the Envoy process to confirm it is
   running. Exits with `ErrEnvoyNotRunning` if the process has already exited.

## Publishing

Two publish functions bake the dev environment into standalone container images
that work without Dagger cache volumes:

```bash
# Publish the shell image (no sandbox)
dagger call dev publish-shell --password env://GITHUB_TOKEN

# Publish the sandbox image
dagger call dev publish-sandbox --password env://GITHUB_TOKEN
```

The **shell image** (`ghcr.io/macropower/shell`) bakes the entire nix store
into image layers with `fish` as the entrypoint.

The **sandbox image** (`ghcr.io/macropower/sandbox`) includes the sandbox
binary and a default `config.yaml`, with `sandbox init -- fish` as the
entrypoint. Firewall configs (iptables rules, Envoy YAML) are
generated at runtime rather than baked in, because the upstream DNS server
varies by host. Users customize the sandbox by mounting their own `config.yaml`
at `/etc/sandbox/config.yaml`.

Both images are published to GHCR on every push to master via the
`publish.yaml` GitHub Actions workflow.
