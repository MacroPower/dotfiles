// Mcp-kubectx is an MCP server for Kubernetes context selection
// over stdio. The sandboxed caller cannot read the host kubeconfig
// directly, so this binary brokers access to it.
//
// The binary has three surface areas: the long-lived `serve`
// subcommand that speaks the MCP stdio protocol, a set of stateless
// `host *` one-shot subcommands that always run on the macOS host,
// and an `exec-plugin` UDS shim invoked by kubectl as its credential
// plugin. The MCP layer always shells out to `host *`, invoking the
// binary directly when on the host and going through `workmux
// host-exec` when the caller is itself inside a Lima sandbox guest.
// The host/guest decision is structural: only the `serve`
// [*handler] knows how to wrap with `workmux host-exec`, and `host
// *` and `exec-plugin` entry points never construct a `*handler`.
// So a host-side `host token` invoked by kubectl from the guest
// cannot recurse back across the boundary even if guest env vars
// leak through. The scoped kubeconfig produced by `host select`
// carries no token material; its user.exec block invokes
// `exec-plugin` to ask serve for a short-lived ServiceAccount
// token over a per-`serve` Unix domain socket.
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
//     path, parses --sa-* flags, binds the per-`serve` UDS via
//     [*handler.acquireServeSocket] (which walks the slot pool
//     described under "Socket slot pool" below), and shells out
//     to `host *` for every cluster-touching operation.
//   - host list: prints the available kubeconfig contexts.
//   - host select <ctx>: creates a ServiceAccount + role binding
//     and writes a scoped kubeconfig whose user.exec block points
//     at `exec-plugin --socket <path>`. Resolves the output path
//     and socket path itself when --out-path / --socket-path are
//     omitted, keyed off the --pid + --for-guest discriminator the
//     serve forwards. Prints a JSON [HostSelectResult] on stdout.
//   - host token: mints a fresh ServiceAccount token via
//     TokenRequest and prints an [ExecCredential] JSON document.
//     Reached internally by serve through [*handler.runHost], not
//     directly by kubectl.
//   - host release: deletes a ServiceAccount and its binding.
//     Best-effort: always exits 0 so `serve` never retries the
//     call across the rest of its lifetime.
//   - exec-plugin: kubectl-facing UDS shim. Dials the per-`serve`
//     socket, copies the response bytes (an [ExecCredential] JSON
//     document) to stdout, and exits. Pure UDS client; never
//     constructs a [*handler] and never imports the K8s client.
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
//     (default: <host's $XDG_STATE_HOME>/mcp-kubectx/kubeconfig.<pid>.<env>.yaml,
//     falling back to ~/.local/state/mcp-kubectx/... when
//     $XDG_STATE_HOME is unset on the host). The default is
//     resolved by `host select` itself; `serve` only forwards its
//     own pid plus host/guest env as the discriminator. <env> is
//     the literal string `host` or `guest`. <pid> is the serve
//     process id, scoping the file to the running serve so a host
//     serve and a Lima-guest serve cannot collide.
//     An explicit --output must be host-resolvable and writable
//     from the guest, since shutdown cleanup is a local
//     [os.Remove]. Default operation already satisfies that via
//     the writable bind mount of <stateHomeDir> declared in
//     workmux's extra_mounts; an explicit --output bypasses that
//     mount, so the user is responsible for picking a path that
//     is reachable on both sides.
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
//   - --socket-slots: number of UDS slot paths probed at startup
//     (default 16, must be >= 1). Each slot maps to one literal
//     entry in the Claude Code sandbox allowUnixSockets allowlist.
//     See "Socket slot pool" below.
//   - --allow-apiserver-host: hostname permitted as cluster.server
//     when selecting a context. Repeatable; an empty list lets
//     `select` accept any apiserver in the kubeconfig. Forwarded
//     verbatim to each `host select` invocation.
//
// # Socket slot pool
//
// The per-`serve` UDS lives at
// <socketStateDir>/serve.<slot>.<env>.sock, where <slot> is a dense
// integer 0..N-1 picked at startup by [*handler.acquireServeSocket].
// Slot indices replace the previous PID-based naming because Claude
// Code's sandbox `allowUnixSockets` setting matches entries as
// literal paths, not globs; enumerating one literal per slot is the
// only way to allow a per-`serve` socket whose filename varies.
// `acquireServeSocket` walks 0..N-1, skipping slots that are held
// by a live peer (detected via [clearStaleSocket]'s dial probe) or
// that race-lose at bind time (wrapped [syscall.EADDRINUSE] from
// [*handler.listenSocket]). Crash-leftover inodes are unlinked by
// `clearStaleSocket` and the slot is reused. When every slot is
// held by a live peer, startup fails with [ErrAllSlotsBusy] naming
// the slot count and the state directory; bumping --socket-slots
// (and matching the literal allowlist on the consuming side) is
// the remedy.
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
// The first positional argument is the context name. Path resolution
// is split: serve owns the discriminator (pid + host/guest env);
// host select owns the base directory ([stateHomeDir], read from
// the host's $XDG_STATE_HOME). When --out-path is empty, host select
// requires --pid and resolves the path itself; an explicit
// --out-path bypasses defaulting and is forwarded verbatim.
//
//   - --kubeconfig: path to the host kubeconfig file.
//   - --out-path: destination for the scoped kubeconfig (default:
//     <stateHomeDir>/kubeconfig.<pid>.<env>.yaml).
//   - --pid: serve process pid, used as the filename
//     discriminator when --out-path is empty (required in that
//     branch, ignored when --out-path is set).
//   - --for-guest: when true, the kubeconfig user.exec block wraps
//     `mcp-kubectx host token` with `workmux host-exec` so an
//     in-guest kubectl can reach the host. When false, user.exec
//     points at the absolute path of the current binary directly.
//     Also drives the <env> token (`host` or `guest`) when
//     --out-path defaulting is in effect.
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
// 30-second [context.Background] timeout, then unlinks the
// kubeconfig file with a local [os.Remove] of h.lastOutputPath.
// On a Lima-guest serve the unlink lands on the host through the
// writable bind mount of <stateHomeDir> declared in workmux's
// extra_mounts; the guest's write reach extends only to that
// directory, which only ever holds mcp-kubectx kubeconfigs.
// SIGKILL leaks at most one SA, one kubeconfig file, and one stale
// socket inode at the killed serve's slot; the next serve that
// reclaims that slot's path detects the leftover via
// [clearStaleSocket] and unlinks it before binding. Provisioned SAs
// and bindings carry the `app.kubernetes.io/managed-by=mcp-kubectx`
// label so a future sweep tool can find orphans by selector.
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
