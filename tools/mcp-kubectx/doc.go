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
// # Package layout
//
// The binary's wiring lives in this main package: the [dispatch] /
// [dispatchHost] switch, the `serve` MCP handler with its
// per-process state, the `host *` one-shot subcommands, and the
// wrapper-env plumbing (merged-kubeconfig local view, hook-router
// sidecar symlink). The domain logic underneath is published as
// independent packages so each piece is importable and testable on
// its own:
//
//   - kubeconfig: minimal kubeconfig model, load/marshal, apiserver
//     host extraction.
//   - kube: the narrow [kube.Client] interface plus its client-go
//     implementation; kubetest carries the recording fake.
//   - serviceaccount: SA + binding provisioning and release, the
//     label taxonomy, binding-name convention, and flag-level config
//     validation.
//   - ledger: the provision records on disk, one for each SA that a
//     `serve` owns.
//   - sweep: orphan classification and bounded best-effort deletion.
//   - execplugin: both halves of the kubectl exec credential
//     protocol (the user.exec block and the UDS fetch client).
//   - socket: the per-`serve` UDS slot pool, sidecar attribution,
//     accept loop, and liveness discovery.
//   - identity: random instance ids and the persistent per-env host
//     id.
//   - statedir, statefile: XDG state-path resolution and
//     atomic/secure state-file writes.
//
// # Binary layout
//
// One binary, two subcommand groups, one flat dispatch in [dispatch]
// / [dispatchHost]. `serve` is the long-lived MCP process and owns
// per-process state in [*handler]. The `host *` subcommands are
// stateless: argv in, JSON or text on stdout, exit. Their contract
// matches `workmux host-exec` exactly so the `serve` shell-out
// helper [*handler.defaultRunHost] is the single place that knows
// about the guest/host distinction. The provision records of the
// ledger package are the only state that one `host *` call leaves
// for a later one. The section "Lifecycle" describes them.
//
// The `host *` subcommands write their logs to stderr, one JSON
// record per line. [runHostCommand] adds each record to the log of
// the `serve` that ran the subcommand, whatever its exit status. So
// the --log-file of `serve` holds the warnings of a `host release`
// or a `host sweep` that logs a failure and exits 0.
//
// # Subcommands
//
//   - serve: MCP stdio server. Owns the per-process kubeconfig
//     path, parses --sa-* flags, binds the per-`serve` UDS via
//     [socket.Acquire] (which walks the slot pool described under
//     "Socket slot pool" below), and shells out
//     to `host *` for every cluster-touching operation.
//   - host list: prints the available kubeconfig contexts.
//   - host select <ctx>: creates a ServiceAccount + role binding
//     and writes a scoped kubeconfig whose user.exec block points
//     at `exec-plugin --socket <path>`. Resolves the output path
//     and socket path itself when --out-path / --socket-path are
//     omitted, keyed off the --pid + --for-guest discriminator the
//     serve forwards. Prints a JSON [HostSelectResult] on stdout.
//     Before it creates the ServiceAccount for a `serve`, it adds
//     the provision record of the ServiceAccount to the ledger.
//   - host token: mints a fresh ServiceAccount token via
//     TokenRequest and prints an [execplugin.Credential] JSON document.
//     Reached internally by serve through [*handler.runHost], not
//     directly by kubectl.
//   - host release: deletes a ServiceAccount and its binding.
//     Best-effort: always exits 0 so `serve` never retries the
//     call across the rest of its lifetime. After the cluster holds
//     neither resource, it removes the provision records of the
//     ServiceAccount from the ledger.
//   - host sweep: deletes the SAs and bindings that dead `serve`
//     instances of one host id left behind. It runs two passes at
//     the same time. The label pass covers one cluster, which is the
//     cluster of --context or, without that flag, of the
//     current-context in the host kubeconfig. It lists SAs,
//     RoleBindings, and ClusterRoleBindings labeled
//     `managed-by=mcp-kubectx,host-id=<own>`, then deletes the
//     orphans whose `instance-id` is absent from the
//     --live-instance-id set. It keeps each resource that the
//     apiserver created after --live-as-of, the time of that set,
//     whatever its `instance-id`. The record pass runs only without
//     --context, and it covers every cluster of the host
//     kubeconfig. It deletes the SA and the binding of each
//     provision record of that kubeconfig whose instance id is
//     absent from that set, on the cluster of the record. It keeps
//     each record that `host select` wrote after --live-as-of.
//     Best-effort: surfaces [kubeconfig.ErrLoad] when the label
//     pass cannot load the host kubeconfig. It surfaces
//     [sweep.ErrList] on total list-call failure of the label pass,
//     and returns nil otherwise. `serve` launches it as a background
//     goroutine at startup, and an operator can also invoke it
//     directly. The label pass requires cluster-wide list verb on
//     the host kubeconfig. See the orphan-sweep section in
//     README.md.
//   - exec-plugin: kubectl-facing UDS shim. Dials the per-`serve`
//     socket, copies the response bytes (an [execplugin.Credential] JSON
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
//   - --output: path where the scoped kubeconfig is written. The
//     default is kubeconfig.<pid>.<env>.yaml, and `host select`
//     resolves it with [statedir.KubeconfigPath] on the host.
//     `serve` forwards only its own pid and env as the
//     discriminator. <env> is the literal string `host` or
//     `guest`. <pid> is the serve process id, which scopes the file
//     to the running serve, so a host serve and a Lima-guest serve
//     cannot collide. The file of a host-side serve lies in
//     <host's $XDG_STATE_HOME>/mcp-kubectx-run, which workmux does
//     not mount into a guest. The file of a guest-side serve lies in
//     <host's $XDG_STATE_HOME>/mcp-kubectx, which the guest reads
//     through the writable bind mount that workmux's extra_mounts
//     declares. Both fall back to ~/.local/state when
//     $XDG_STATE_HOME is unset on the host.
//     A guest-side serve removes its file at shutdown with a local
//     [os.Remove]. So in a guest, an explicit --output must be
//     host-resolvable and writable from the guest. The default
//     path of the guest env lies under the mount and meets both
//     conditions. An explicit --output bypasses the mount, so the
//     user picks a path that both sides reach.
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
//     cleanup events, and the log records of each `host *`
//     subcommand that `serve` runs. Defaults to discard. serve
//     fails to start when the last element of the path is a
//     symlink.
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
// The per-`serve` UDS lives at serve.<slot>.<env>.sock in
// [statedir.RunDir], where <slot> is a dense integer 0..N-1 picked
// at startup by [socket.Acquire].
// Slot indices replace the previous PID-based naming because Claude
// Code's sandbox `allowUnixSockets` setting matches entries as
// literal paths, not globs; enumerating one literal per slot is the
// only way to allow a per-`serve` socket whose filename varies.
// [socket.Acquire] walks 0..N-1, skipping slots that are held by a
// live peer (detected via a short-deadline dial probe) or that
// race-lose at bind time (wrapped [syscall.EADDRINUSE] from
// [socket.Listen]). Crash-leftover inodes are unlinked and the slot
// is reused. When every slot is held by a live peer, startup fails
// with [socket.ErrAllSlotsBusy] naming
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
// The first positional argument is the context name. Serve owns the
// discriminator of the default path, which is its pid and its env.
// host select owns the directory, which it resolves from the host's
// $XDG_STATE_HOME with [statedir.KubeconfigPath]. When --out-path is
// empty, host select requires --pid and resolves the path itself.
// An explicit --out-path bypasses the default, and host select
// writes to it as given. In this doc, <stateHomeDir> stands for
// [statedir.Dir] and <runDir> for [statedir.RunDir].
//
//   - --kubeconfig: path to the host kubeconfig file.
//   - --out-path: destination for the scoped kubeconfig (default:
//     <runDir>/kubeconfig.<pid>.host.yaml, or
//     <stateHomeDir>/kubeconfig.<pid>.guest.yaml with --for-guest).
//   - --pid: serve process pid, used as the filename
//     discriminator when --out-path is empty (required in that
//     branch, ignored when --out-path is set).
//   - --for-guest: picks the env of the default paths. With
//     --for-guest, the default kubeconfig lies in <stateHomeDir>,
//     which workmux mounts into each Lima guest. Its name and the
//     name of the default socket carry the `guest` env tag. Without
//     the flag, the default kubeconfig lies in <runDir>, which
//     workmux does not mount, and both names carry the `host` env
//     tag. The user.exec block is the same in both envs.
//   - --sa-role-name, --sa-role-kind, --sa-cluster-scoped,
//     --sa-namespace, --sa-expiration: same semantics as the `serve`
//     flags above. `serve` forwards its own values verbatim.
//   - --sa-instance-id: per-`serve` random identifier tagged on the
//     created SA and binding via [serviceaccount.InstanceIDLabel].
//     `serve` forwards
//     its own random id; empty omits the label.
//   - --sa-host-id: persistent per-user-per-host identifier tagged
//     via [serviceaccount.HostIDLabel]. `serve` forwards the id
//     loaded from [identity.HostPath]; empty omits the label.
//   - --allow-apiserver-host: hostname permitted as cluster.server.
//     Repeatable; empty list allows any apiserver. When non-empty,
//     the resolved cluster's `server` URL must have a matching
//     hostname or the call fails with [ErrAPIServerNotAllowed]
//     before any K8s mutation.
//
// When the caller sets both --sa-instance-id and --sa-host-id,
// `host select` writes the provision record of the SA before it
// creates the SA. An id that is not 16 lowercase hex characters
// then fails the call with [ledger.ErrInvalidID] before any K8s
// mutation. When either id is empty, `host select` writes no
// record.
//
// When the create fails, `host select` keeps the record in two
// cases. In the first, a create call failed without a refusal from
// the apiserver, for example after a lost connection or a timeout.
// So the apiserver might have stored the SA or the binding. An
// AlreadyExists answer counts as such a failure. After a storage
// timeout, client-go sends the create again, and the retry of a
// stored create gets AlreadyExists. In the second case, the binding
// create failed and the rollback delete of the SA failed too. After
// every other failure the cluster holds neither resource, and
// `host select` removes the record.
//
// # host token flags
//
// Mints a short-lived ServiceAccount token via TokenRequest and
// prints a single [execplugin.Credential] JSON document on stdout. Invoked
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
// exits 0 (transient API hiccups and exec failures alike) so
// `serve` never retries across its lifetime. A resource that the
// cluster does not hold counts as deleted. After the cluster holds
// neither resource, `host release` removes every provision record
// that locates the ServiceAccount by the same kubeconfig path,
// context, namespace, and name.
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
// [runServe] defers the cleanup closure that [*handler.sessionDir]
// returns right after the socket bind, so the closure runs at each
// later return of [runServe]. [runServe] returns when the MCP client
// closes the stdin of serve, when SIGINT or SIGTERM cancels the
// context of serve, or when the connection to the client fails.
//
// To stop a stdio server, the MCP specification asks the client to
// close the stdin of the server first. It asks for SIGTERM only
// when the server does not exit after that. Claude Code 2.1.319
// does not follow that order on Linux or macOS. It sends SIGINT to
// serve first. When serve still runs 100 ms later, Claude Code
// sends SIGTERM, and when serve still runs 400 ms after that, it
// sends SIGKILL. So with Claude Code as the client, the closure
// usually starts on SIGINT.
//
// The stdin of serve closes with no signal when a client follows
// the order of the specification, or when Claude Code ends without
// its signal sequence, such as when it crashes. Only a crash of
// serve or a signal that serve does not catch, such as SIGKILL or
// SIGHUP, ends serve before the closure runs or while it runs.
//
// The closure runs these steps in order:
//
//  1. [*handler.socketShutdown] closes the UDS listener and waits
//     for each connection handler to return, so each token mint
//     ends before a later step unlinks a file. The listener close
//     unlinks the per-slot sidecar file at [socket.SidecarPath] and
//     then the socket inode at h.socketPath.
//  2. The closure unlinks the scoped kubeconfig file with a local
//     [os.Remove] of h.lastOutputPath.
//  3. The closure unlinks the hook-router sidecar symlink at
//     [sidecarSymlinkPath].
//  4. [*handler.drainSweep] waits for the background `host sweep`
//     goroutine to return, for at most 30 seconds. The limit keeps
//     a hung `host sweep` subprocess from blocking the shutdown.
//     A SIGINT or a SIGTERM, before the closure or during it,
//     cancels the sweep. When neither signal arrives, the wait
//     lasts until the sweep ends or until the limit passes.
//  5. The closure runs every registered release closure with a
//     30-second [context.Background] timeout.
//
// The listener close unlinks the sidecar file while the listener
// still holds the slot. The unlink of the socket inode then frees
// the slot. A new serve might bind the slot and write its own
// sidecar file while step 1 waits for a token mint. The closure
// never unlinks either file after the listener close, so it leaves
// the files of that new serve in place.
//
// The unlinks are local and take microseconds. Step 4 and step 5
// can take seconds, and neither reads the files that the unlinks
// remove. So the unlinks run first.
//
// On a Lima-guest serve the unlink of the kubeconfig file lands on
// the host through the writable bind mount of <stateHomeDir> declared
// in workmux's extra_mounts. Through that mount, the guest can
// create, change, and delete the files of <stateHomeDir> of the host.
// It can also replace each of them with a symlink. The directory
// holds the scoped kubeconfig files of guest-side serves. `host
// select` writes each of them on the host, and each guest-side serve
// reads its file through the mount. So every guest can rewrite the
// kubeconfig of each guest-side serve.
//
// `host select` writes each kubeconfig with [statefile.WriteSecure],
// which writes a new file under a random name and renames it to the
// path. So a symlink that a guest plants in <stateHomeDir> does not
// receive the kubeconfig. serve opens its --log-file with
// O_NOFOLLOW, and it fails to start when the last element of that
// path is a symlink.
//
// The files that host-side serves write or trust lie in <runDir> of
// the host, which workmux does not mount into a guest. These files
// include the scoped kubeconfig of each host-side serve and the
// `host.id` file of host-side serves. They also include the log file
// of host-side serves, at the path that home/claude.nix passes as
// --log-file. Host-side kubectl reads such a kubeconfig through the
// hook-router sidecar symlink and runs the exec plugin that the file
// names. Each host-side serve reads `host.id` at startup for the
// `host-id` label of its resources and for the selector of its
// orphan sweep.
//
// A guest-side serve resolves <runDir> in the guest. So it keeps
// `guest.id` on the disk of the guest, outside the mount.
// serve writes its log to the --log-file path as given, and
// home/claude.nix renders that path from the xdg.stateHome of the
// machine that it configures. So on a guest that home/claude.nix
// configures, the log of guest-side serves lies on the disk of the
// guest too.
//
// Claude Code sends its SIGKILL about 500 ms after the SIGINT that
// starts the closure. A closure that runs longer, for example while
// a `host release` of step 5 runs, then ends partway, and its later
// steps never run. So under the signal sequence of Claude Code, the
// closure never reaches the 30-second limit of step 4 or step 5.
// A SIGKILL during step 4 or step 5 leaves behind only the SAs that
// serve had not released, with their bindings and provision
// records. The SIGKILL reaches only serve, so a `host release`
// subprocess that has started keeps running after serve ends.
//
// When serve ends before the closure starts, it leaves the items in
// this list behind. When serve ends while the closure runs, it
// leaves only the items of the steps that have not run. The socket
// inode and the per-slot sidecar file are the exception, because
// closing the listener unlinks both. [socket.Serve] closes the
// listener as soon as SIGINT or SIGTERM cancels the context of
// serve, and step 1 closes it when it is still open. So a serve
// that ends during the closure leaves neither file.
//
//   - Each SA that the serve had not released, and the binding of
//     that SA. The startup sweep of a later serve with the same
//     host-id deletes them on whichever cluster they lie, when that
//     serve uses the same host kubeconfig. The host-side serves of
//     one host share a host-id, and so do the guest-side serves of
//     one Lima guest VM. The sweep of the other env, or of another
//     guest VM, keeps them, because it selects another host-id. A
//     guest can still make a sweep select the host-id of these SAs,
//     because it can run `host sweep` through `workmux host-exec`
//     with that id.
//   - The provision record of each such SA, in the ledger directory
//     on the host. The same sweep removes the record after it has
//     deleted the SA and the binding.
//   - The socket inode at the slot of the killed serve. The next
//     serve that takes the slot detects the inode with the stale
//     probe in [socket.Listen] and unlinks it before the bind.
//   - The per-slot sidecar file at [socket.SidecarPath]. The next
//     serve that takes the slot unlinks the file in [socket.Listen]
//     before the bind, whether or not the socket inode remains.
//   - The scoped kubeconfig file. No step removes it. A later
//     `host select` that writes the same path overwrites it.
//   - The hook-router sidecar symlink at [sidecarSymlinkPath].
//     hook-router removes $CLAUDE_KUBECTX_DIR at SessionEnd, and
//     that directory holds the link of each session that the
//     launcher wrapper started. Outside such a session, the link
//     lies at <TMPDIR>/claude-kubectx/<PPID>/kubeconfig on the
//     machine that runs the serve. No hook removes that link.
//
// Provisioned SAs and bindings carry three labels:
// `app.kubernetes.io/managed-by=mcp-kubectx`,
// `mcp-kubectx/host-id=<persistent>`, and
// `mcp-kubectx/instance-id=<per-serve random>`. `host select` also
// writes one provision record for each SA that it creates for a
// `serve`. The record lies in the ledger directory on the host,
// <host's $XDG_STATE_HOME>/mcp-kubectx-run/ledger, and it holds the
// kubeconfig path, the context, the namespace, the SA name, the ids
// of the `serve`, and the time of the write. `host release` removes
// the record after it has deleted the SA and the binding.
//
// Every `serve` runs a background `host sweep` at startup, with
// the set of live serves that it discovered through the per-slot
// UDS sidecar in [socket.DiscoverLive]. The sweep runs two passes
// at the same time.
//
// The label pass covers only the cluster of the current-context in
// the host kubeconfig, because the labels do not say which other
// clusters hold resources. It lists the labeled resources for the
// `host-id` of the serve, classifies each by `instance-id` against
// the set of live serves, and best-effort-deletes the orphans.
// A serve that starts after the calling serve takes the set is
// missing from it, but it can provision before the list calls
// answer. So the label pass keeps each resource that the apiserver
// created after the calling serve took the set, with
// [sweep.CreationSlack] for clock skew.
// A resource tagged only with `managed-by` and no `host-id` names
// no host, so the label pass skips it. Recovery is the manual
// `kubectl delete` that the README documents.
//
// The record pass covers every cluster of the host kubeconfig. It
// takes the provision records of the `host-id` whose `instance-id`
// is not in the set of live serves. For each record it deletes the
// SA and the binding through the context of the record, and then it
// removes the record. A record whose deletes fail stays, so the
// sweep of the next serve tries again. A serve that starts after
// the calling serve takes the set can run `host select` before the
// record pass reads the ledger. So the record pass keeps each
// record that `host select` wrote after the calling serve took the
// set, with [recordSlack] for a guest clock that runs ahead of the
// host clock.
//
// The record pass takes only the records that name the host
// kubeconfig of the sweep. It never loads the kubeconfig file of a
// record. A Lima guest can run `host select` through `workmux
// host-exec` with a kubeconfig of its own. client-go runs the exec
// credential plugin of each kubeconfig that it loads. The sweep of
// a host-side serve runs outside the sandbox of `workmux
// host-exec`, and so does each plugin that it runs. A record of
// another kubeconfig stays until a sweep runs with `--kubeconfig`
// set to that path.
//
// The sweep deletes an SA of a dead serve on a cluster other than
// the current-context only through the record of the SA. For such
// an SA without a record, recovery is the manual `mcp-kubectx host
// sweep --context <ctx>` that the README documents.
//
// Both passes pin on `host-id`, persisted per env as `host.id` or
// `guest.id` (see [identity.LoadOrCreateHost]). So two operators on
// different machines cannot delete each other's resources. A
// host-side serve and a guest-side serve on the same machine
// cannot either, and neither side observes the live sockets of the
// other. Every Lima guest can break both bounds, because it can run
// `host sweep` through `workmux host-exec` with any `--host-id`.
//
// # Recursion guard
//
// The guard against `host token` re-entering the guest path is
// structural rather than runtime. [*handler.hostExecArgs] is bound
// to `*handler`; the `host *` subcommand entry points
// ([runHostList], [runHostSelect], [runHostToken], [runHostRelease],
// [runHostSweep]) never construct a `*handler`, so they have no
// path to [*handler.defaultRunHost] and cannot decide to wrap with
// `workmux host-exec`. Even when `WM_SANDBOX_GUEST=1` is set in
// their env, they have no shell-out path. `host token` calls the
// cluster directly via [kube.Client.CreateTokenRequest]. Pinned by
// `TestHostTokenSkipsWorkmuxWhenEnvSetToGuest`.
//
// # In-sandbox merged kubeconfig
//
// The launcher wrapper composes the in-sandbox $KUBECONFIG as a
// colon-list with a single writer each. Off the Lima guest it is two
// entries:
//
//	$CLAUDE_KUBECTX_LOCAL : $CLAUDE_KUBECTX_SIDECAR
//	   (selection overlay)    (external SA creds)
//
// On the Lima guest image the guest's own ~/.kube/config is spliced
// into the middle, making it a first-class local source:
//
//	$CLAUDE_KUBECTX_LOCAL : $HOME/.kube/config : $CLAUDE_KUBECTX_SIDECAR
//	   (selection overlay)    (guest cluster defs)   (external SA creds)
//
// local.yaml ($CLAUDE_KUBECTX_LOCAL) holds only current-context, the
// MCP-owned merged-view selection (first-file-wins). The guest config
// ($CLAUDE_KUBECTX_GUEST_CONFIG, set only on the guest image) holds
// the cluster/user definitions for guest-local clusters (kind / k3d /
// minikube / Talos-in-Docker) and is read, never written, by the MCP:
// [localView] enumerates its contexts alongside local.yaml's as the
// union driving [*handler.list] output and [*handler.selectCtx]
// routing. The sidecar carries the exec-plugin recipe for external
// contexts.
//
// [*handler.selectCtx] routes by union membership first: a name in
// local.yaml or the guest config takes the local (no-SA) path; only a
// name absent from both takes the external SA-mint path. The union is
// load-bearing -- a guest cluster's apiserver is unreachable from the
// macOS host, so an SA mint against it would fail. client-go is
// first-file-wins, so the guest config shadows the sidecar on a name
// collision; the routing invariant keeps each selected name defined in
// exactly one effective source. See [guestConfigPath] for why the
// guest-config source keys on the env var alone, independent of
// host/guest shell-out routing.
//
// # See also
//
// README.md in this package carries the YAML kubeconfig samples (host
// and guest variants), the [execplugin.Credential] JSON sample, the
// `home/claude.nix` `host_commands` deployment requirement, the
// three-way merge and its collision-precedence invariant, and the
// end-to-end env-chain explanation that closes the recursion-guard
// story.
package main
