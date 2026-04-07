// Mcp-kubectx is an MCP server that exposes Kubernetes context
// selection over stdio transport, bridging a sandboxed caller and
// the host kubeconfig it cannot read directly.
//
// The binary has two surface areas: the long-lived `serve`
// subcommand that speaks the MCP stdio protocol, and a set of
// stateless `host *` one-shot subcommands that always run on the
// macOS host. The MCP layer always shells out to `host *`, invoking
// the binary directly when on the host and going through `workmux
// host-exec` when the caller is itself inside a Lima sandbox guest.
// The host/guest decision is made structurally: only the `serve`
// [*handler] knows how to wrap with `workmux host-exec`, and `host
// *` entry points never construct a `*handler`, so a host-side
// `host token` invoked by kubectl from the guest cannot recurse
// back across the boundary even if guest env vars leak through.
// The scoped kubeconfig produced by `host select` carries no token
// material; its user.exec block invokes `host token` on demand to
// mint short-lived ServiceAccount tokens.
//
// # Binary layout
//
// One binary, two subcommand groups, one flat dispatch in [dispatch]
// / [dispatchHost]. `serve` is the long-lived MCP process and owns
// per-process state in [*handler]. The `host *` subcommands are
// stateless: argv in, JSON or text on stdout, exit. Their contract
// matches `workmux host-exec` exactly so the `serve` shell-out
// helper [*handler.defaultRunHost] is the single place that knows
// about the guest/host distinction.
//
// # Subcommands
//
//   - serve: MCP stdio server. Owns the per-process kubeconfig
//     path, parses --sa-* flags, and shells out to `host *` for
//     every cluster-touching operation.
//   - host list: prints the available kubeconfig contexts.
//   - host select <ctx>: creates a ServiceAccount + role binding
//     and writes a scoped kubeconfig whose user.exec block points
//     at `host token`. Prints a JSON [HostSelectResult] on stdout.
//   - host token: mints a fresh ServiceAccount token via
//     TokenRequest and prints an [ExecCredential] JSON document.
//   - host release: deletes a ServiceAccount and its binding.
//     Best-effort: always exits 0 so `serve` never retries the
//     call across the rest of its lifetime.
//
// # serve flags
//
// The --sa-* and --kubeconfig values configured here are forwarded
// to every `host select` invocation via [*handler.selectArgs];
// `serve` itself does not touch the cluster with them.
//
//   - --kubeconfig: path to the host kubeconfig file
//     (default: $KUBECONFIG, then ~/.kube/config).
//   - --output: path where the scoped kubeconfig is written
//     (default: $XDG_STATE_HOME/mcp-kubectx/kubeconfig.<pid>.<env>.yaml,
//     falling back to ~/.local/state/mcp-kubectx/... when
//     $XDG_STATE_HOME is unset). <env> is the literal string `host`
//     or `guest`. The <pid> component prevents two `serve` instances
//     (including a host serve and a Lima-guest serve sharing the
//     same $HOME) from overwriting each other's kubeconfig.
//   - --sa-role-name: name of the Role or ClusterRole to bind (required).
//   - --sa-role-kind: kind of role to bind: Role or ClusterRole
//     (default: ClusterRole).
//   - --sa-cluster-scoped: create a ClusterRoleBinding instead of a
//     RoleBinding (requires ClusterRole kind).
//   - --sa-namespace: namespace for the ServiceAccount
//     (default: context namespace or "default").
//   - --sa-expiration: token lifetime in seconds
//     (default: 3600, max: 86400).
//   - --log-file: path to JSON log file (append). Logs warnings and
//     cleanup events; defaults to discard.
//   - --allow-apiserver-host: hostname permitted as cluster.server
//     when selecting a context. Repeatable; an empty list lets
//     `select` accept any apiserver in the kubeconfig. Forwarded
//     verbatim to each `host select` invocation.
//
// # host list flags
//
// Prints contexts from the host kubeconfig.
//
//   - --kubeconfig: path to the host kubeconfig file
//     (default: $KUBECONFIG, then ~/.kube/config).
//
// # host select flags
//
// Creates a ServiceAccount + binding and writes a scoped kubeconfig.
// The first positional argument is the context name. --out-path is
// required. Path policy lives in `serve`, never on the host side, so
// two concurrent `serve` instances cannot collide on the same file.
//
//   - --kubeconfig: path to the host kubeconfig file.
//   - --out-path: destination for the scoped kubeconfig (required;
//     `serve` always supplies it from [*handler.resolveOutputPath]).
//   - --for-guest: when true, the kubeconfig user.exec block wraps
//     `mcp-kubectx host token` with `workmux host-exec` so an
//     in-guest kubectl can reach the host. When false, user.exec
//     points at the absolute path of the current binary directly.
//   - --sa-role-name, --sa-role-kind, --sa-cluster-scoped,
//     --sa-namespace, --sa-expiration: same semantics as the `serve`
//     flags above. `serve` forwards its own values verbatim.
//   - --allow-apiserver-host: hostname permitted as cluster.server.
//     Repeatable; empty list allows any apiserver. When non-empty,
//     the resolved cluster's `server` URL must have a matching
//     hostname or the call fails with [ErrAPIServerNotAllowed]
//     before any K8s mutation.
//
// # host token flags
//
// Mints a short-lived ServiceAccount token via TokenRequest and
// prints a single [ExecCredential] JSON document on stdout. Invoked
// by kubectl (and any other client-go consumer) through the exec
// auth plugin in the scoped kubeconfig.
//
//   - --kubeconfig: path to the host kubeconfig file.
//   - --context: kubeconfig context to use.
//   - --sa: ServiceAccount name (required).
//   - --namespace: ServiceAccount namespace (required).
//   - --sa-expiration: token lifetime in seconds (default: 3600).
//
// # host release flags
//
// Best-effort delete of a ServiceAccount and its binding. Always
// exits 0 (transient API hiccups, NotFound, exec failures alike) so
// `serve` never retries across its lifetime.
//
//   - --kubeconfig: path to the host kubeconfig file.
//   - --context: kubeconfig context to use.
//   - --sa: ServiceAccount name.
//   - --namespace: ServiceAccount namespace.
//   - --sa-cluster-scoped: the binding is a ClusterRoleBinding
//     rather than a RoleBinding.
//
// # Lifecycle
//
// On a new MCP `select`, [*handler.selectCtx] snapshots the previous
// cleanup list, shells out to `host select`, and on success registers
// a fresh release closure and drains the prior closures inline with
// a 30-second [context.Background] timeout before returning the MCP
// response. The drain is synchronous because deriving from the
// request context would let the MCP SDK cancel the in-flight
// DeleteServiceAccount calls as soon as `selectCtx` returned, which
// would re-leak the prior SA. On `host select` failure or JSON parse
// error, [*handler.restoreCleanups] swaps the previous list back so
// the prior SA still gets released at shutdown.
//
// On SIGINT or SIGTERM, the cleanup closure returned by
// [*handler.sessionDir] runs every registered release with the same
// 30-second [context.Background] timeout and then removes the
// kubeconfig file. SIGKILL leaks at most one SA and one kubeconfig
// file. Provisioned SAs and bindings carry the
// `app.kubernetes.io/managed-by=mcp-kubectx` label so a future
// sweep tool can find orphans by selector.
//
// # Recursion guard
//
// The guard against `host token` re-entering the guest path is
// structural rather than runtime. [*handler.hostExecArgs] is bound
// to `*handler`; the `host *` subcommand entry points
// ([runHostList], [runHostSelect], [runHostToken], [runHostRelease])
// never construct a `*handler`, so they have no path to
// [*handler.defaultRunHost] and cannot decide to wrap with `workmux
// host-exec`. Even when `WM_SANDBOX_GUEST=1` is set in their env,
// they have no shell-out path. `host token` calls the cluster
// directly via [KubeClient.CreateTokenRequest]. Pinned by
// `TestHostTokenSkipsWorkmuxWhenEnvSetToGuest`.
//
// # See also
//
// README.md in this package carries the YAML kubeconfig samples (host
// and guest variants), the [ExecCredential] JSON sample, the
// `home/claude.nix` `host_commands` deployment requirement, and the
// end-to-end env-chain explanation that closes the recursion-guard
// story.
package main
