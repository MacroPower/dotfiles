package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// Sentinel errors for the orphan sweep.
var (
	// ErrSweepList wraps any failure to list a resource kind. The
	// sweep tolerates partial list failure: per-kind errors are
	// logged at warn level and the function continues with whatever
	// subset it could enumerate. [runHostSweep] surfaces this
	// sentinel only when every list call fails outright (rare:
	// usually only on RBAC misconfiguration that forbids cluster-wide
	// list on all three resource kinds).
	ErrSweepList = errors.New("sweep list")
	// ErrSweepDelete wraps a per-resource delete failure inside the
	// warn-log line. Never propagated from [runHostSweep]; the
	// sentinel exists only so the warn-log entries carry a
	// consistent error prefix.
	ErrSweepDelete = errors.New("sweep delete")
)

// sweepConcurrency bounds the number of in-flight K8s Delete*
// calls issued by [runHostSweep]. Small enough to be polite to a
// shared apiserver, large enough to keep startup latency tight
// when a backlog of orphans accumulates.
const sweepConcurrency = 8

// resourceKind labels the three resource families the sweep
// classifies. Used only inside log lines so operators can
// disambiguate counters per kind.
type resourceKind string

const (
	resourceServiceAccount     resourceKind = "ServiceAccount"
	resourceRoleBinding        resourceKind = "RoleBinding"
	resourceClusterRoleBinding resourceKind = "ClusterRoleBinding"
)

// runHostSweep is the body of the `host sweep` subcommand. It
// lists every SA, RoleBinding, and ClusterRoleBinding labeled
// `managed-by=mcp-kubectx,host-id=<own>`, classifies each against
// the set of live instance ids passed via --live-instance-id, and
// deletes the orphans best-effort.
//
// Error contract: returns [ErrParseHostSweepFlags] on flag parse
// failure, [ErrMissingHostID] when --host-id is empty, and
// [ErrSweepList] only when every list call fails outright (RBAC
// forbids cluster-wide list on all three resource kinds). Partial
// list failures and all delete failures are logged at warn level
// and swallowed because the next `serve` startup retries the
// sweep anyway.
//
// The --host-id flag is required and non-empty. An empty host id
// is a footgun because the resulting selector would either match
// nothing or match too much depending on whether historical
// resources carry the label.
//
// --context may be empty; in that case [runHostSweep] resolves
// `cfg.CurrentContext` from the loaded kubeconfig. If the
// resolved context is still empty (unconfigured kubeconfig) the
// sweep logs a warning and exits 0 — startup never blocks on the
// sweep.
//
// --live-instance-id is repeatable. Empty list means no live
// serves on this host, which is destructive: every labeled
// resource gets deleted. When the calling serve invokes
// [runHostSweep] from [*handler.runSweep], its own id is always
// in the live set (its sidecar was written before the sweep
// kicked off and is observed by [*handler.discoverLiveInstances]).
// Manual operator invocation accepts this destructive semantics
// explicitly by passing zero --live-instance-id flags.
func runHostSweep(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("host sweep", flag.ContinueOnError)

	kubeconfig := fs.String("kubeconfig", "", "path to host kubeconfig (default: $KUBECONFIG or ~/.kube/config)")
	contextName := fs.String(
		"context", "",
		"kubeconfig context to use (default: current-context from the loaded kubeconfig)",
	)
	hostID := fs.String(
		"host-id", "",
		"persistent host identifier used as the mcp-kubectx/host-id selector (required)",
	)

	var liveIDs stringSliceFlag

	fs.Var(
		&liveIDs,
		"live-instance-id",
		"instance id to preserve (repeatable; empty list = sweep every resource from this host)",
	)

	err := fs.Parse(args)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrParseHostSweepFlags, err)
	}

	if *hostID == "" {
		return ErrMissingHostID
	}

	resolvedKubeconfig := resolveHostKubeconfigPath(*kubeconfig)

	cfg, err := loadKubeconfig(resolvedKubeconfig)
	if err != nil {
		return err
	}

	resolvedContext := *contextName
	if resolvedContext == "" {
		resolvedContext = cfg.CurrentContext
	}

	if resolvedContext == "" {
		slog.WarnContext(ctx, "host sweep skipped: no context resolved from kubeconfig",
			slog.String("kubeconfig", resolvedKubeconfig),
		)

		return nil
	}

	client, err := hostKubeClient(resolvedKubeconfig, resolvedContext)
	if err != nil {
		slog.WarnContext(ctx, "host sweep build kube client",
			slog.String("context", resolvedContext),
			slog.Any("error", err),
		)

		return nil
	}

	live := make(map[string]struct{}, len(liveIDs))
	for _, id := range liveIDs {
		if id == "" {
			continue
		}

		live[id] = struct{}{}
	}

	selector := sweepSelector(*hostID)

	orphans, listErr := collectSweepOrphans(ctx, client, selector, live)
	if listErr != nil {
		return listErr
	}

	deleteSweepOrphans(ctx, client, orphans)

	return nil
}

// sweepSelector builds the LabelSelector used by every List call
// inside [runHostSweep]. The selector pins on both the
// managed-by value and the host id so cross-host resources are
// filtered out at the apiserver before the classifier ever runs.
func sweepSelector(hostID string) string {
	return fmt.Sprintf("%s=%s,%s=%s",
		managedByLabel, managedByValue,
		hostIDLabel, hostID,
	)
}

// sweepCandidate is one classified resource awaiting deletion.
type sweepCandidate struct {
	ref  ResourceRef
	kind resourceKind
}

// listResult carries one kind's list outcome through the
// fan-out/fan-in in [collectSweepOrphans]. err being non-nil
// means the apiserver call failed for that kind; refs is then
// empty.
type listResult struct {
	err  error
	kind resourceKind
	refs []ResourceRef
}

// collectSweepOrphans lists each managed resource kind in
// parallel and classifies every entry. Returns the candidates
// that should be deleted. Per-kind list failures are logged at
// warn level and produce an empty slice for that kind — the
// sweep still attempts the other kinds. The returned error is
// non-nil only when every kind failed outright, in which case it
// wraps [ErrSweepList] and the orphan slice is empty. This lets
// [runHostSweep] surface a clear total-failure mode while still
// tolerating partial RBAC gaps. Listing in parallel collapses
// three apiserver round-trips into one when the cluster is
// remote.
func collectSweepOrphans(
	ctx context.Context,
	client KubeClient,
	selector string,
	live map[string]struct{},
) ([]sweepCandidate, error) {
	listers := []struct {
		fn   func(context.Context, string) ([]ResourceRef, error)
		kind resourceKind
		name string
	}{
		{client.ListServiceAccounts, resourceServiceAccount, "service accounts"},
		{client.ListRoleBindings, resourceRoleBinding, "role bindings"},
		{client.ListClusterRoleBindings, resourceClusterRoleBinding, "cluster role bindings"},
	}

	results := make(chan listResult, len(listers))

	var wg sync.WaitGroup

	for _, l := range listers {
		wg.Go(func() {
			refs, err := l.fn(ctx, selector)
			if err != nil {
				slog.WarnContext(ctx, "sweep list "+l.name,
					slog.String("selector", selector),
					slog.Any("error", fmt.Errorf("%w: %w", ErrSweepList, err)),
				)
			}

			results <- listResult{kind: l.kind, refs: refs, err: err}
		})
	}

	wg.Wait()
	close(results)

	var (
		orphans  []sweepCandidate
		listErrs []error
	)

	for r := range results {
		if r.err != nil {
			listErrs = append(listErrs, r.err)
		}

		for _, ref := range r.refs {
			if shouldDelete(ref, live) {
				orphans = append(orphans, sweepCandidate{ref: ref, kind: r.kind})
			}
		}
	}

	if len(listErrs) == len(listers) {
		return nil, fmt.Errorf("%w: %w", ErrSweepList, errors.Join(listErrs...))
	}

	return orphans, nil
}

// shouldDelete reports whether a single resource is orphan. A
// resource is preserved when its instance id is in the live set,
// or when the instance-id label is missing entirely (conservative:
// pre-feature resources predate the sweep and have no way to
// attribute themselves; the manual recovery path documented in
// the README handles them). Otherwise it is an orphan.
func shouldDelete(ref ResourceRef, live map[string]struct{}) bool {
	id, ok := ref.Labels[instanceIDLabel]
	if !ok {
		return false
	}

	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}

	_, isLive := live[id]

	return !isLive
}

// deleteSweepOrphans deletes the classified orphan set with a
// bounded worker pattern. A buffered chan acts as a semaphore;
// each delete error is logged and swallowed so the caller (and
// [runHostSweep] above it) never propagates a per-resource
// failure to the parent process. [errgroup] is deliberately not
// used because its first-error-cancels-group semantics would
// interrupt other in-flight deletes.
func deleteSweepOrphans(ctx context.Context, client KubeClient, orphans []sweepCandidate) {
	if len(orphans) == 0 {
		return
	}

	slog.InfoContext(ctx, "sweep deleting orphans", slog.Int("count", len(orphans)))

	sem := make(chan struct{}, sweepConcurrency)

	var wg sync.WaitGroup

	for _, c := range orphans {
		wg.Add(1)

		sem <- struct{}{}

		go func(c sweepCandidate) {
			defer wg.Done()
			defer func() { <-sem }()

			deleteSweepCandidate(ctx, client, c)
		}(c)
	}

	wg.Wait()
}

// deleteSweepCandidate issues the matching Delete* call for the
// candidate's resource kind. Errors are wrapped with
// [ErrSweepDelete] for log-line consistency and never propagated
// up; the sweep is best-effort. Unknown kinds log a warn so a
// future addition to [resourceKind] without extending this switch
// fails loud instead of silent.
func deleteSweepCandidate(ctx context.Context, client KubeClient, c sweepCandidate) {
	var err error

	switch c.kind {
	case resourceServiceAccount:
		err = client.DeleteServiceAccount(ctx, c.ref.Namespace, c.ref.Name)
	case resourceRoleBinding:
		err = client.DeleteRoleBinding(ctx, c.ref.Namespace, c.ref.Name)
	case resourceClusterRoleBinding:
		err = client.DeleteClusterRoleBinding(ctx, c.ref.Name)
	default:
		slog.WarnContext(ctx, "sweep delete: unknown kind",
			slog.String("kind", string(c.kind)),
			slog.String("name", c.ref.Name),
		)

		return
	}

	if err != nil {
		slog.WarnContext(ctx, "sweep delete",
			slog.String("kind", string(c.kind)),
			slog.String("namespace", c.ref.Namespace),
			slog.String("name", c.ref.Name),
			slog.Any("error", fmt.Errorf("%w: %w", ErrSweepDelete, err)),
		)
	}
}

// runSweep shells out to `host sweep` with the calling serve's
// host id and the discovered live-instance set. Failure is logged
// at warn level and never propagated; startup is not blocked on a
// successful sweep. --context is intentionally omitted from the
// argv: no MCP `select` has happened yet, so the sweep resolves
// `cfg.CurrentContext` itself from the host kubeconfig.
//
// Refuses to invoke [runHostSweep] when liveSet is empty or does
// not contain h.instanceID. Either case indicates degraded
// discovery (sidecar read or dial probe failed for the own slot);
// shelling out anyway would let [runHostSweep]'s destructive
// "manual recovery" semantics delete every peer-serve's
// resources. The manual `host sweep` CLI keeps the
// empty-live-set semantics so operator recovery still works.
func (h *handler) runSweep(ctx context.Context, liveSet map[string]struct{}) {
	if len(liveSet) == 0 {
		slog.WarnContext(ctx, "host sweep skipped: empty live instance set",
			slog.String("host_id", h.hostID),
		)

		return
	}

	if _, ok := liveSet[h.instanceID]; !ok {
		slog.WarnContext(ctx, "host sweep skipped: own instance id missing from live set",
			slog.String("host_id", h.hostID),
			slog.String("instance_id", h.instanceID),
		)

		return
	}

	args := []string{}
	if h.kubeconfigPath != "" {
		args = append(args, "--kubeconfig", h.kubeconfigPath)
	}

	args = append(args, "--host-id", h.hostID)

	for id := range liveSet {
		args = append(args, "--live-instance-id", id)
	}

	_, err := h.runHost(ctx, subSweep, args)
	if err != nil {
		slog.WarnContext(ctx, "host sweep",
			slog.Any("error", err),
		)
	}
}
