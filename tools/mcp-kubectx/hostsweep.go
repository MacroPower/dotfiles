package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/identity"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kubeconfig"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/sweep"
)

// runHostSweep is the body of the `host sweep` subcommand. It
// lists every SA, RoleBinding, and ClusterRoleBinding labeled
// `managed-by=mcp-kubectx,host-id=<own>`, classifies each against
// the set of live instance ids passed via --live-instance-id, and
// deletes the orphans best-effort.
//
// Error contract: returns [ErrParseHostSweepFlags] on flag parse
// failure, [ErrMissingHostID] when --host-id is empty,
// [ErrInvalidHostID] when --host-id is not exactly 16 lowercase
// hex characters, and [sweep.ErrList] only when every list call
// fails outright (RBAC forbids cluster-wide list on all three
// resource kinds). Partial list failures and all delete failures
// are logged at warn level and swallowed because the next `serve`
// startup retries the sweep anyway.
//
// The --host-id flag is required and format-checked; see
// [ErrMissingHostID] and [ErrInvalidHostID] for the threat model
// behind each check.
//
// --context may be empty; in that case [runHostSweep] resolves
// `cfg.CurrentContext` from the loaded kubeconfig. If the
// resolved context is still empty (unconfigured kubeconfig) the
// sweep logs a warning and exits 0 -- startup never blocks on the
// sweep.
//
// --live-instance-id is repeatable. Empty list means no live
// serves on this host, which is destructive: every labeled
// resource gets deleted. When the calling serve invokes
// [runHostSweep] from [*handler.runSweep], its own id is always
// in the live set (its sidecar was written before the sweep
// kicked off and is observed by [socket.DiscoverLive]). Manual
// operator invocation accepts this destructive semantics
// explicitly by passing zero --live-instance-id flags.
func runHostSweep(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("host sweep", flag.ContinueOnError)

	kubeconfigPath := fs.String("kubeconfig", "", "path to host kubeconfig (default: $KUBECONFIG or ~/.kube/config)")
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

	if !identity.Valid(*hostID) {
		return fmt.Errorf("%w: %q", ErrInvalidHostID, *hostID)
	}

	resolvedKubeconfig := resolveHostKubeconfigPath(*kubeconfigPath)

	cfg, err := kubeconfig.Load(resolvedKubeconfig)
	if err != nil {
		return err //nolint:wrapcheck // Load already wraps kubeconfig.ErrLoad
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

	selector := sweep.Selector(*hostID)

	orphans, listErr := sweep.CollectOrphans(ctx, client, selector, live)
	if listErr != nil {
		return listErr //nolint:wrapcheck // CollectOrphans already wraps sweep.ErrList
	}

	sweep.DeleteOrphans(ctx, client, orphans)

	return nil
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

	args := h.kubeconfigArgs()
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
