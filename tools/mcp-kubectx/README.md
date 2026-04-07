# mcp-kubectx

Cross-VM transport for Kubernetes context selection. A sandboxed
Claude (typically running inside a Lima guest) needs to use `kubectl`
without the host's admin kubeconfig leaking into the guest's
filesystem. The kubeconfig the guest consumes carries no token
material. It holds only a recipe (a kubectl exec auth plugin) that
calls back to the host on demand for a short-lived ServiceAccount
token.

## Architecture

The binary has two surface areas dispatched by a single flat switch
in `cli.go`:

- **`serve`** is the long-lived MCP stdio server. It owns per-process
  state in `*handler`: the host kubeconfig path, the per-`serve`
  scoped-kubeconfig path, the SA configuration parsed from `--sa-*`
  flags, and a slice of cleanup closures registered on each `select`.
  It never touches the cluster directly.
- **`host {list, select, token, release}`** are stateless one-shots.
  Each parses argv, talks to the cluster via client-go, prints JSON
  or text on stdout, and exits. They share no state with each other
  or with `serve`. Cluster state is the source of truth for what
  exists; in-process state in `serve` only tracks ownership.

The shell-out boundary is the only place the guest/host distinction
matters. `*handler.hostExecArgs` decides whether to invoke
`mcp-kubectx host *` directly (when `serve` is on the host) or wrap
it with `workmux host-exec mcp-kubectx host *` (when `serve` is
inside a Lima guest, indicated by `WM_SANDBOX_GUEST=1`). The wrapped
form forwards argv to the host-side mcp-kubectx binary via the
workmux RPC server, gated by the host_commands allowlist (see
[Deployment requirement](#deployment-requirement-workmux-host_commands-allowlist)).

```
mcp-kubectx serve              # MCP stdio mode, local to its Claude
mcp-kubectx host list          # one-shot: print contexts
mcp-kubectx host select <ctx>  # one-shot: create SA, write kubeconfig, print descriptor
mcp-kubectx host token         # one-shot: mint token, print ExecCredential JSON
mcp-kubectx host release       # one-shot: delete SA + binding
```

## Scoped kubeconfigs

`host select` writes one of two variants depending on `--for-guest`.
The cluster section is identical in both, copied verbatim from the
selected context in the host kubeconfig. Only `user.exec.command`
and its leading args differ.

**Host consumer** (`--for-guest=false`):

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
        command: /nix/store/.../bin/mcp-kubectx
        args:
          - host
          - token
          - --kubeconfig
          - /Users/me/.kube/config
          - --context
          - prod
          - --sa
          - claude-sa-abc12345
          - --namespace
          - kube-system
          - --sa-expiration
          - "3600"
        interactiveMode: Never
```

**Guest consumer** (`--for-guest=true`):

```yaml
users:
  - name: claude-sa-abc12345
    user:
      exec:
        apiVersion: client.authentication.k8s.io/v1
        command: workmux
        args:
          - host-exec
          - mcp-kubectx
          - host
          - token
          - --kubeconfig
          - /Users/me/.kube/config
          - --context
          - prod
          - --sa
          - claude-sa-abc12345
          - --namespace
          - kube-system
          - --sa-expiration
          - "3600"
        interactiveMode: Never
```

`serve` always supplies `--for-guest=$(isGuest)` when it shells out
to `host select` (`kubeconfig.go`'s `selectArgs`), so the kubeconfig
written for an in-guest Claude is the guest variant and the one
written for a host Claude is the host variant.

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
modification. There's no custom proxy listening on a Unix socket, no
bind-mounted docker.sock equivalent, no SDK shim.

Token rotation falls out of the design. Each plugin invocation mints
a fresh token via the host's admin TokenRequest API. Deleting the SA
on the host invalidates any cached token at its next API call;
there's no separate revocation channel to keep in sync.

The transport is one-shot argv-in / stdout-out. That's exactly the
contract `workmux host-exec` already implements, so there's no
long-lived host process to babysit, no socket auth, no multiplexing
question, no signal protocol. kubectl inside the VM talks straight
to the cluster API server over the network it already has; the host
is contacted only at token-mint time.

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
runs every registered release with the same 30-second
`context.Background` timeout and then removes the kubeconfig file.
SIGKILL leaks at most one SA + one kubeconfig file, the same blast
radius as today's "kill mcp-kubectx without warning". No regression.

A future `mcp-kubectx host sweep --age=24h` tool can mop up orphans
by label and TTL. Not yet implemented.

The behaviors above are pinned by `TestSelectDrainsPriorCleanupOnSuccess`,
`TestSelectRestoresPrevCleanupOnFailure`,
`TestSessionDirCleanupRunsResourceCleanupWhenOutputSet`, and
`TestSessionDirCleanupRunsResourceCleanupWhenOutputUnset`.

## Recursion guard and the env chain

The risk is that a host-side `mcp-kubectx host token` invoked by
kubectl from inside a Lima guest could observe `WM_SANDBOX_GUEST=1`
in its environment and try to wrap itself with `workmux host-exec`,
recursing back across the boundary. Two independent defenses prevent
this.

**Design rule (structural).** Only `*handler` decides whether to
wrap with `workmux host-exec`. `hostExecArgs` is a method on
`*handler` (`shellout.go`), and the `host *` subcommand entry points
never construct a `*handler`, so they have no path to `runHost` and
no way to invoke the wrapper. `host token` reads the cluster
directly via `KubeClient.CreateTokenRequest`. The mechanism is
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
`CreateTokenRequest` directly with no intermediate fork.

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

A host `serve` and a Lima-guest `serve` share `$HOME`. If `host
select` defaulted the output kubeconfig path on the host side, two
`serve` instances could overwrite each other's kubeconfig. To avoid
that, `--out-path` is **required** on `host select`. Path policy
lives only in `serve`. Each `serve` resolves a path keyed by pid
plus `host` or `guest` env (`*handler.resolveOutputPath`):

```
$XDG_STATE_HOME/mcp-kubectx/kubeconfig.<pid>.<env>.yaml
```

`<env>` is the literal string `host` or `guest`; `<pid>` is the
serve process id. Falls back to `~/.local/state/mcp-kubectx/...`
when `$XDG_STATE_HOME` is unset.

The MCP `select` response carries the resolved path back to the
caller so an interactive shell can `export KUBECONFIG=...` to it.
Each `serve` removes its own kubeconfig file on shutdown alongside
the `host release` of its current SA.

## Out of scope

- **Cluster reachability from the guest.** Lima networking and the
  egress proxy gate this. If a target cluster is unreachable from
  the guest, the user adds it to `kubeApiDomains` or routes around
  it. Not this package's problem.
- **Orphan cleanup.** `mcp-kubectx host sweep --age=24h` is the
  intended tool for SAs and stale kubeconfig files left behind by
  SIGKILLed serves. Not yet implemented; until then, orphans
  accumulate at the rate of one per ungraceful exit per `serve`.
