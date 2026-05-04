# mcp-kubectx

Cross-VM transport for Kubernetes context selection. A sandboxed
Claude (typically running inside a Lima guest) needs to use `kubectl`
without the host's admin kubeconfig leaking into the guest's
filesystem. The kubeconfig the guest consumes carries no token
material. It holds only a recipe (a kubectl exec auth plugin) that
calls back to the host on demand for a short-lived ServiceAccount
token.

## Architecture

The binary has three surface areas dispatched by a single flat
switch in `cli.go`:

- **`serve`** is the long-lived MCP stdio server. It owns per-process
  state in `*handler`: the host kubeconfig path, the SA configuration
  parsed from `--sa-*` flags, an atomic `currentSA` descriptor, the
  per-`serve` UDS listener, and a slice of cleanup closures registered
  on each `select`. It never touches the cluster directly.
- **`host {list, select, token, release}`** are stateless one-shots.
  Each parses argv, talks to the cluster via client-go, prints JSON
  or text on stdout, and exits. They share no state with each other
  or with `serve`. Cluster state is the source of truth for what
  exists; in-process state in `serve` only tracks ownership.
- **`exec-plugin`** is the kubectl exec credential shim. It
  connects to `serve`'s UDS at `--socket <path>`, copies the bytes
  the server writes to stdout, and exits. Like the `host *`
  subcommands, it never constructs a `*handler`, which is what
  keeps the structural recursion guard (see
  [Recursion guard](#recursion-guard-and-the-env-chain)) honest.

End-to-end credential flow:

```
kubectl exec plugin
  -> mcp-kubectx exec-plugin --socket <state>/mcp-kubectx-run/serve.<slot>.<env>.sock
     (UDS)
  -> mcp-kubectx serve  (currentSA loaded atomically)
     -> defaultRunHost("token", ...)
        -> direct fork on host
        -> workmux host-exec on guest
     -> ExecCredential JSON written back to UDS
  -> shim copies bytes to stdout
  -> kubectl reads ExecCredential, calls API server
```

The shell-out boundary is the only place the guest/host distinction
still matters. `*handler.hostExecArgs` decides whether to invoke
`mcp-kubectx host *` directly (when `serve` is on the host) or wrap
it with `workmux host-exec mcp-kubectx host *` (when `serve` is
inside a Lima guest, indicated by `WM_SANDBOX_GUEST=1`). The
distinction is invisible to kubectl: both host- and guest-side
serves write the same exec-plugin block, and routing happens
server-side.

```
mcp-kubectx serve              # MCP stdio mode, local to its Claude
mcp-kubectx host list          # one-shot: print contexts
mcp-kubectx host select <ctx>  # one-shot: create SA, write kubeconfig, print descriptor
mcp-kubectx host token         # one-shot: mint token, print ExecCredential JSON
mcp-kubectx host release       # one-shot: delete SA + binding
mcp-kubectx exec-plugin        # kubectl-facing UDS shim
```

## Scoped kubeconfigs

`host select` writes a single uniform shape regardless of
`--for-guest`. The cluster section is copied verbatim from the
selected context in the host kubeconfig. The `user.exec` block is
the same tiny UDS shim in both host- and guest-side kubeconfigs:

```yaml
apiVersion: v1
kind: Config
clusters:
  - name: prod-cluster
    cluster:
      server: https://prod.example.com
      certificate-authority-data: ...
contexts:
  - name: prod
    context:
      cluster: prod-cluster
      user: claude-sa-abc12345
      namespace: kube-system
users:
  - name: claude-sa-abc12345
    user:
      exec:
        apiVersion: client.authentication.k8s.io/v1
        command: mcp-kubectx
        args:
          - exec-plugin
          - --socket
          - /Users/me/.local/state/mcp-kubectx-run/serve.0.host.sock
        interactiveMode: Never
```

`command: mcp-kubectx` is a bare program name (PATH lookup) so
that nix-darwin rebuilds do not invalidate kubeconfigs; an
absolute store path would change on every rebuild and force
kubectl to start over. `mcp-kubectx` is on PATH for both the
host and the in-Lima profiles via `home.packages` in
`home/claude.nix`.

The `--for-guest` flag is a _path discriminator_ only: it flips
the `<env>` token between `host` and `guest` in the defaulted
kubeconfig and socket filenames so a host serve and a Lima-guest
serve cannot collide on the same path. Kubectl sees a uniform
plugin shape either way.

## ExecCredential output

`host token` prints a single kubectl `ExecCredential` JSON document
on stdout:

```json
{
  "apiVersion": "client.authentication.k8s.io/v1",
  "kind": "ExecCredential",
  "status": {
    "expirationTimestamp": "2026-05-01T13:00:00Z",
    "token": "eyJhbGciOi..."
  }
}
```

kubectl caches the credential by `expirationTimestamp` and only
re-invokes the plugin when it expires. The perf cost is one fork
per token lifetime (default 1h), not per kubectl call. For a typical
Claude session that's a handful of forks total, each ~50-200 ms wall
time when round-tripping through `workmux host-exec`.

## Why this design

The scoped kubeconfig holds the SA name, cluster URL, and CA. No
token material lives on disk inside the VM. The token exists in
kubectl's memory for the duration of the API call and is gone after.
Compromise of the bind-mounted directory reveals a recipe for asking
the host to mint a token, not the token itself, and the host can
revoke the SA out from under it at any time.

This is plain kubectl exec-plugin plumbing, so anything that uses
client-go (kubectl, helm, kustomize, argocd) works without
modification. The Unix domain socket is bound by the same
long-lived MCP `serve` process that already owns the per-`serve`
state; there is no new daemon and the UDS lifetime tracks the
Claude session exactly. The shim itself is a single
`mcp-kubectx exec-plugin` subcommand built into the same binary
kubectl already invokes, so the deployment surface is unchanged.

Token rotation falls out of the design. Each plugin invocation
asks `serve` to mint a fresh token via the host's admin
TokenRequest API. Deleting the SA on the host invalidates any
cached token at its next API call; there's no separate revocation
channel to keep in sync.

The UDS exists specifically to escape the bash sandbox at
credential-mint time: kubectl runs under a sandbox that denies
reads of `~/.kube/config`, so the credential plugin (a child of
sandboxed kubectl) inherits the deny when it tries to read the
admin kubeconfig itself. Routing the mint through `serve` -- which
runs outside both the bash sandbox (host case) and the Lima guest
(guest case via `workmux host-exec`) -- sidesteps the deny without
broadening any sandbox grant. Sandbox grants the connect on the
single socket path; the token mint happens entirely outside.

## Lifecycle and cleanup

State that matters:

1. Which SA each `serve` instance currently owns. Lives in `serve`
   process memory, in the cleanup-closure slice.
2. Which SAs exist on the cluster. Authoritative: the cluster.
   Tagged with `app.kubernetes.io/managed-by=mcp-kubectx`.

On a new MCP `select` call, `*handler.selectCtx` snapshots the
previous cleanup list, clears the live one, and shells out to `host
select`. On success it registers a fresh release closure for the new
SA and drains the prior closures **inline** with a 30-second
`context.Background` timeout before returning the MCP response. The
drain has to be synchronous, because deriving from the request
context would let the MCP SDK cancel the in-flight
DeleteServiceAccount calls as soon as `selectCtx` returned, re-leaking
the prior SA. On failure, or if the `host select` JSON cannot be
parsed, `restoreCleanups` swaps the previous list back so the prior
SA still gets released at shutdown.

On SIGINT or SIGTERM, the cleanup closure returned by `sessionDir`
runs in this order:

1. `socketShutdown` closes the UDS listener and waits on a
   `sync.WaitGroup` for in-flight per-connection handlers to
   return. This is required so no handler is mid-token-mint when
   the next steps unlink files.
2. Drain registered SA release closures with a 30-second
   `context.Background` timeout.
3. Unlink the socket file at `h.socketPath`.
4. Unlink the scoped kubeconfig at `h.lastOutputPath`.
5. Unlink the hook-router sidecar symlink. (Local TMPDIR; never
   crosses the Lima boundary.)

SIGKILL leaks at most one SA + one kubeconfig file + one socket
inode at the killed serve's slot. The leaked socket inode is
reclaimed the next time a `serve` picks that slot during
`acquireServeSocket`'s walk: `listenSocket`'s `clearStaleSocket`
step dial-tests the path; ECONNREFUSED means the file is leftover
state and is unlinked before the bind, while a successful dial
means a live peer holds the slot and the loop advances to the
next slot.

The behaviors above are pinned by the `TestSelect*`, `TestSessionDir*`,
and `TestServeSocket*` test families.

## Recursion guard and the env chain

The risk is that a host-side `mcp-kubectx host token` invoked by
kubectl from inside a Lima guest could observe `WM_SANDBOX_GUEST=1`
in its environment and try to wrap itself with `workmux host-exec`,
recursing back across the boundary. Two independent defenses prevent
this.

**Design rule (structural).** Only `*handler` decides whether to
wrap with `workmux host-exec`. `hostExecArgs` is a method on
`*handler` (`shellout.go`), and neither the `host *` subcommand
entry points nor `exec-plugin` construct a `*handler`, so they
have no path to `runHost` and no way to invoke the wrapper.
`host token` reads the cluster directly via
`KubeClient.CreateTokenRequest`; `exec-plugin` is a pure UDS
client and does not even import the K8s client. The mechanism is
syntactic. There is no path through the call graph; it is not a
runtime env-var check.

**workmux env sanitization.** `workmux host-exec` strips every env
var not in its allowlist (`PATH`, `HOME`, `USER`, `LOGNAME`,
`TMPDIR`, `TERM`, `COLORTERM`, `LANG`, `LC_ALL`, `XDG_*`; see
`sandbox/rpc.rs` in workmux). Neither `WM_SANDBOX_GUEST` nor
`WM_RPC_TOKEN` is on that list, so the host-side `mcp-kubectx host
token` literally cannot observe itself as guest, and could not
authenticate back to the RPC server even if it tried.

**End-to-end env chain (guest variant).** In-VM Claude is launched
under `workmux sandbox shell`, which sets `WM_RPC_HOST` /
`WM_RPC_PORT` / `WM_RPC_TOKEN` in the shell env. kubectl inherits
those vars. The exec plugin = `workmux host-exec mcp-kubectx host
token ...` inherits them too and uses them to reach the host's RPC
server. The RPC server forks `mcp-kubectx host token` on the host
with sanitized env (no `WM_*`); the token subcommand needs nothing
from the workmux env. It talks only to k8s.

**PATH chain.** The wrapped `workmux` binary lives in
`home.packages` (`home/claude.nix`), so it is on the in-guest PATH
from the home-manager profile and the kubectl exec plugin can
resolve `command: workmux` without an absolute path.

The structural defense is pinned by
`TestHostTokenSkipsWorkmuxWhenEnvSetToGuest`, which sets
`WM_SANDBOX_GUEST=1` and confirms `runHostToken` calls
`CreateTokenRequest` directly with no intermediate fork. The
`exec-plugin` shim has no analogous test because the entry point
literally does not link the K8s client; the structural property is
visible at compile time.

## Deployment requirement: workmux host_commands allowlist

`mcp-kubectx` cannot work from a guest unless the host's workmux
config allowlists it. Add the binary name to the `sandbox` attrset
in `home/claude.nix`:

```nix
sandbox = lib.optionalAttrs cfg.lima.enable {
  # ...existing keys (enabled, backend, image, ...)...
  host_commands = [ "mcp-kubectx" ];
  # ...
};
```

workmux merges the user list with its builtins via
`effective_host_commands` and gates `Exec` RPCs against the result.
A guest-side `workmux host-exec mcp-kubectx ...` invocation against
a host that has not been configured this way is rejected by the RPC
server.

## Concurrent-select hazard and per-`serve` paths

The scoped kubeconfig file lives on the host filesystem. A
host-side `serve` and a Lima-guest `serve` could otherwise overwrite
each other's kubeconfig if path resolution did not key on the
running serve's identity. Resolution is split: `serve` owns the
discriminator (its own pid plus `host` or `guest` env), `host
select` owns the base directory (the host's `$XDG_STATE_HOME`).
`serve` forwards `--pid <h.pid>` plus `--for-guest=BOOL` to `host
select`, which builds the path:

```
<host's $XDG_STATE_HOME>/mcp-kubectx/kubeconfig.<pid>.<env>.yaml
```

`<env>` is the literal string `host` or `guest`; `<pid>` is the
serve process id. Falls back to `~/.local/state/mcp-kubectx/...`
when `$XDG_STATE_HOME` is unset on the host.

A guest-side `serve` reads the host's `$XDG_STATE_HOME` because
`workmux host-exec` runs `host select` on the host, so the env
introspection happens host-side. The guest sees the file at the
same absolute path through a writable bind mount of
`<host's $XDG_STATE_HOME>/mcp-kubectx` declared in workmux's
`extra_mounts` (see `home/claude.nix`). Shutdown cleanup is a
local `os.Remove(h.lastOutputPath)` from the serve process; on a
guest serve the unlink lands on the host through the same bind
mount. The mount is intentionally scoped to the mcp-kubectx
state dir so the guest's write reach extends only to mcp-kubectx
kubeconfig files -- not to the rest of the host filesystem.

The hook-router sidecar symlink published by [*handler.publishSidecar]
points at the same host-absolute path; hook-router resolves it
through the bind mount to _read_ the kubeconfig.

The MCP `select` response carries the host-resolved path back to
the caller so an interactive shell can `export KUBECONFIG=...` to
it. Each `serve` removes its own kubeconfig file on shutdown
alongside the `host release` of its current SA.

The `--out-path` flag on `host select` is an escape hatch. When
`serve` is invoked with `--output PATH`, it forwards the path
verbatim as `--out-path`, bypassing the defaulting. In a Lima
guest, an explicit `--output` must be host-resolvable and writable
from the guest -- typically only true under a bind mount.

### Sockets live outside the bind-mounted state dir

The per-`serve` UDS lives at
`<state>/mcp-kubectx-run/serve.<slot>.<env>.sock`, _not_ alongside
the kubeconfig under `<state>/mcp-kubectx/`. The latter is the
existing Lima writable bind mount declared in workmux's
`extra_mounts`; UDS-over-Lima-bind-mount semantics on macOS-host
are unverified, and the safe design avoids the question by hosting
the socket on each profile's local filesystem. See [Socket slot
pool](#socket-slot-pool) below for how `<slot>` is picked.

Both kinds of `serve` (host and guest) bind their socket on their
own filesystem. A guest serve's socket is reachable only from
inside the same guest, which is exactly the topology kubectl needs
since the kubectl that uses it also runs inside the guest. The
_kubeconfig_ still lives under the bind mount because both
profiles need read access to the file; the socket only needs
guest-local create + connect, so it lives outside.

### Socket slot pool

`serve` binds the first free slot in the range
`serve.0.<env>.sock` … `serve.<N-1>.<env>.sock` at startup, where
`N` defaults to 16 and is configurable via `--socket-slots`. Slot
indices keep socket filenames stable across `serve` restarts
because Claude Code's sandbox `allowUnixSockets` setting matches
entries as literal paths, not glob patterns; enumerating one
literal entry per slot is the only way to allow a per-`serve`
socket whose filename varies between processes.

`acquireServeSocket` walks slot indices upward and re-uses
`listenSocket`'s existing stale-vs-live dial probe to skip slots
held by a live peer. Crash-leftover inodes are silently unlinked
and reused. Concurrent `serve` instances on the same host occupy
distinct slots; on exhaustion, startup fails with a clear error
naming the configured slot count and the state directory so an
operator can grow the pool by raising `kubectxSocketSlots` in
`home/claude.nix` and rerunning `task switch`.

The Nix bundle in `home/claude.nix` enumerates exactly the
literal paths the binary may bind, sized from the same option,
so the rendered `~/.claude/settings.json` allowlist is always
1:1 with the `serve` binary's slot range.

### Trust boundary

Socket file mode is `0600`; parent dir is `0700`. Single-user
machine, identical to the kubeconfig at the same dir level. Any
process running as the same UID can connect, but the threat model
already trusts same-UID processes (they could read the kubeconfig
file itself if the deny list did not block it). The bash sandbox
specifically denies `~/.kube/config` to prevent admin kubeconfig
exfiltration; the socket gives the in-sandbox kubectl exactly one
narrow capability -- "ask serve for a fresh SA-scoped token" --
without expanding the read deny.

## Out of scope

- **Cluster reachability from the guest.** Lima networking and the
  egress proxy gate this. If a target cluster is unreachable from
  the guest, the user adds it to `kubeApiDomains` or routes around
  it. Not this package's problem.
- **Orphan cleanup.** SAs and stale kubeconfig files left behind
  by SIGKILLed serves accumulate at the rate of one per ungraceful
  exit per `serve`.
