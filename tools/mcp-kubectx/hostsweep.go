package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/identity"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kubeconfig"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/ledger"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/serviceaccount"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/sweep"
)

// recordPassTimeout caps the API calls of the record pass of
// [runHostSweep] on one cluster. Without the cap, a cluster that
// accepts a connection and never answers holds the sweep until
// `serve` ends. The cap does not reach an exec credential plugin,
// because client-go starts the plugin without the context of the
// call. It is a package-level variable so that tests can shorten
// it. Production code never changes it.
//
//nolint:grouper // tunable timeout, kept separate from the sentinel-error var block
var recordPassTimeout = 30 * time.Second

// recordSlack is the margin that the record pass of [runHostSweep]
// subtracts from the time of the live set before it compares that
// time with the time of each provision record. `host select` takes
// the time of a record from the clock of the host. The live set
// takes its time from the clock of the calling `serve`. The clock
// of a Lima guest can run ahead of the clock of the host. Then a
// record that `host select` wrote after the live set looks older
// than the live set, and the slack keeps such a record. A record of
// a dead `serve` that `host select` wrote less than recordSlack
// before the live set stays until a later sweep.
const recordSlack = time.Minute

// runHostSweep is the body of the `host sweep` subcommand. It
// deletes the SAs and bindings that dead `serve` instances of one
// host id left behind. It runs two passes.
//
// The label pass covers one cluster. It lists every SA,
// RoleBinding, and ClusterRoleBinding labeled
// `managed-by=mcp-kubectx,host-id=<own>`, classifies each against
// the set of live instance ids passed via --live-instance-id, and
// deletes the orphans best-effort. The cluster is the one of
// --context. When --context is empty, it is the one of
// `cfg.CurrentContext` in the loaded kubeconfig.
//
// The record pass covers every cluster of the swept kubeconfig. It
// reads the provision records of the host id from the ledger. It
// takes each record whose instance id is not in the live set and
// whose kubeconfig is the swept kubeconfig. Of these, it takes only
// the records that `host select` wrote more than [recordSlack]
// before --live-as-of. For each record that it takes, it deletes
// the recorded SA and binding through the context of the record. It
// removes the record after the cluster holds neither resource. A
// record whose deletes fail stays, so the next sweep tries again. A
// record of another kubeconfig waits for a sweep with --kubeconfig
// set to that path. The record pass runs only when --context is
// empty, so that an explicit --context limits the whole sweep to
// that cluster.
//
// The record pass never loads the kubeconfig file that a record
// names. A Lima guest can run `host select` with a --kubeconfig of
// its choice through `workmux host-exec`. client-go runs the exec
// credential plugin of each kubeconfig file that it loads. The
// record pass of a host-side `serve` runs outside the sandbox of
// `workmux host-exec`, so it loads only the kubeconfig that the
// label pass loads too.
//
// The two passes run at the same time, and neither waits for the
// other. The record pass works on the records that the ledger held
// when the sweep started.
//
// Error contract: it returns these errors.
//
//   - [ErrParseHostSweepFlags] when the flags do not parse.
//   - [ErrMissingHostID] when --host-id is empty.
//   - [ErrInvalidHostID] when --host-id is not exactly 16
//     lowercase hex characters.
//   - An error when it cannot make the kubeconfig path absolute.
//   - An error that wraps [kubeconfig.ErrLoad] when the label pass
//     cannot load the kubeconfig.
//   - [sweep.ErrList] only when every list call of the label pass
//     fails outright, such as when RBAC forbids the cluster-wide
//     list on all three resource kinds.
//
// It logs every other failure at warn level and swallows it,
// because the next `serve` startup runs the sweep again. These
// failures include a failed list call of some resource kinds, each
// failed delete, and every failure of the record pass. An error of
// the label pass does not stop the record pass, and the sweep
// returns that error after both passes have ended.
//
// The --host-id flag is required and format-checked.
// [ErrMissingHostID] and [ErrInvalidHostID] name the threat model
// behind each check.
//
// When neither --context nor `cfg.CurrentContext` names a context
// (unconfigured kubeconfig), the label pass logs a warning and
// does nothing. Startup never blocks on the sweep.
//
// --live-instance-id is repeatable. An empty list says that no
// serve of this host is alive, so both passes delete every
// resource of the host id that they find. The label pass still
// keeps the resources that the apiserver created after
// --live-as-of. The record pass still keeps the records that
// `host select` wrote after --live-as-of. When the calling serve
// invokes [runHostSweep] from [*handler.runSweep], its own id is
// always in the live set. The serve writes its sidecar before it
// starts the sweep, and [socket.DiscoverLive] reads that sidecar.
// An operator who passes zero --live-instance-id flags by hand
// accepts the destructive semantics.
//
// --live-as-of is the time at which the caller took the live set,
// in RFC 3339 format. It defaults to the start of the sweep. A
// serve that starts after that time is missing from the live set.
// It can write a provision record before the record pass reads the
// ledger, and it can create resources before the list calls of the
// label pass answer. So the label pass keeps each resource that the
// apiserver created at that time minus [sweep.CreationSlack] or
// later. The record pass keeps each record that `host select` wrote
// at that time minus [recordSlack] or later. A time after the start
// of the sweep comes from a clock that runs ahead of the clock of
// this host, such as the clock of a Lima guest. The sweep then uses
// its own start instead, because the caller took the live set
// before the sweep started.
func runHostSweep(ctx context.Context, args []string) error {
	started := time.Now()

	fs := flag.NewFlagSet("host sweep", flag.ContinueOnError)

	kubeconfigPath := fs.String("kubeconfig", "", "path to host kubeconfig (default: $KUBECONFIG or ~/.kube/config)")
	contextName := fs.String(
		"context", "",
		"kubeconfig context of the one cluster to sweep by label "+
			"(default: current-context from the loaded kubeconfig, "+
			"plus the cluster of each provision record of the loaded kubeconfig)",
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

	liveAsOf := started

	fs.Func(
		"live-as-of",
		"RFC 3339 time at which the caller took the --live-instance-id set; "+
			"the sweep keeps each resource and each provision record created after it, "+
			"with a minute of slack for clock skew (default: the start of the sweep)",
		func(v string) error {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return err //nolint:wrapcheck // the flag package wraps it with the flag name
			}

			liveAsOf = t

			return nil
		},
	)

	err := fs.Parse(args)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrParseHostSweepFlags, err)
	}

	// The caller took the live set before the sweep started. A
	// later time comes from a clock that runs ahead of the clock of
	// this host.
	if liveAsOf.After(started) {
		liveAsOf = started
	}

	if *hostID == "" {
		return ErrMissingHostID
	}

	if !identity.Valid(*hostID) {
		return fmt.Errorf("%w: %q", ErrInvalidHostID, *hostID)
	}

	live := make(map[string]struct{}, len(liveIDs))
	for _, id := range liveIDs {
		if id == "" {
			continue
		}

		live[id] = struct{}{}
	}

	// `host select` records the absolute path, so the record pass
	// compares each record with the absolute path.
	sweptKubeconfig, err := filepath.Abs(resolveHostKubeconfigPath(*kubeconfigPath))
	if err != nil {
		return fmt.Errorf("resolve kubeconfig path: %w", err)
	}

	// The live set is a snapshot from the start of the calling
	// serve, and a serve that starts later is not in it. Such a
	// serve can write records before the sweep reads the ledger, and
	// deadRecords keeps them by the time of each record. Such a
	// serve can also write records while a pass waits on a slow
	// cluster. The record pass reads the ledger before the first
	// cluster call, so it never sees those records.
	records := hostLedger()

	var dead []ledger.Record
	if *contextName == "" {
		dead = deadRecords(ctx, records, sweptKubeconfig, *hostID, live, liveAsOf)
	}

	// The passes run at the same time. A label pass that waits on
	// its cluster, for example for a credential plugin, then does
	// not hold the record pass back.
	var wg sync.WaitGroup

	wg.Go(func() {
		releaseRecorded(ctx, records, sweptKubeconfig, dead)
	})

	sweepErr := sweepLabeled(ctx, sweptKubeconfig, *contextName, *hostID, live, liveAsOf)

	wg.Wait()

	return sweepErr
}

// sweepLabeled is the label pass of [runHostSweep]. It lists the
// labeled resources of hostID on the cluster of contextName and
// deletes each one whose instance id is not in live.
// [sweep.CollectOrphans] keeps the resources that the apiserver
// created after liveAsOf, the time of live. An empty contextName
// selects the current-context of the kubeconfig at kubeconfigPath.
//
// It returns the error of [kubeconfig.Load], and [sweep.ErrList]
// when every list call fails. Every other failure is logged and
// swallowed.
func sweepLabeled(
	ctx context.Context,
	kubeconfigPath, contextName, hostID string,
	live map[string]struct{},
	liveAsOf time.Time,
) error {
	cfg, err := kubeconfig.Load(kubeconfigPath)
	if err != nil {
		return err //nolint:wrapcheck // Load already wraps kubeconfig.ErrLoad
	}

	if contextName == "" {
		contextName = cfg.CurrentContext
	}

	if contextName == "" {
		slog.WarnContext(ctx, "host sweep skipped the label pass: no context resolved from kubeconfig",
			slog.String("kubeconfig", kubeconfigPath),
		)

		return nil
	}

	client, err := hostKubeClient(kubeconfigPath, contextName)
	if err != nil {
		slog.WarnContext(ctx, "host sweep build kube client",
			slog.String("context", contextName),
			slog.Any("error", err),
		)

		return nil
	}

	orphans, listErr := sweep.CollectOrphans(ctx, client, sweep.Selector(hostID), live, liveAsOf)
	if listErr != nil {
		return listErr //nolint:wrapcheck // CollectOrphans already wraps sweep.ErrList
	}

	sweep.DeleteOrphans(ctx, client, orphans)

	return nil
}

// deadRecords returns the provision records of the host hostID
// whose instance id is not in live, that `host select` wrote before
// liveAsOf minus [recordSlack], and whose kubeconfig is
// kubeconfigPath. Each of them names an SA that a dead `serve` left
// behind, or that a dead `serve` released without removing the
// record. liveAsOf is the time of live. A record written after it
// might belong to a `serve` that started after the caller took
// live. It logs each other record outside live and leaves that
// record in the ledger. When it cannot read the ledger, it logs the
// failure and returns no records, so the record pass does nothing.
func deadRecords(
	ctx context.Context,
	records *ledger.Ledger,
	kubeconfigPath, hostID string,
	live map[string]struct{},
	liveAsOf time.Time,
) []ledger.Record {
	all, err := records.List()
	if err != nil {
		slog.WarnContext(ctx, "host sweep read provision records", slog.Any("error", err))

		return nil
	}

	cutoff := liveAsOf.Add(-recordSlack)

	var dead []ledger.Record

	for _, r := range all {
		if r.HostID != hostID {
			continue
		}

		if _, isLive := live[r.InstanceID]; isLive {
			continue
		}

		if !r.Written.Before(cutoff) {
			slog.InfoContext(ctx, "host sweep skipped provision record written after the live set",
				slog.String("context", r.Context),
				slog.String("namespace", r.Namespace),
				slog.String("name", r.ServiceAccount),
				slog.String("instance_id", r.InstanceID),
				slog.Time("written", r.Written),
				slog.Time("live_as_of", liveAsOf),
			)

			continue
		}

		// A guest can choose the kubeconfig of a record, so the
		// record stays. [runHostSweep] says why.
		if r.Kubeconfig != kubeconfigPath {
			slog.InfoContext(ctx, "host sweep skipped provision record of another kubeconfig",
				slog.String("kubeconfig", r.Kubeconfig),
				slog.String("context", r.Context),
				slog.String("namespace", r.Namespace),
				slog.String("name", r.ServiceAccount),
				slog.String("instance_id", r.InstanceID),
			)

			continue
		}

		dead = append(dead, r)
	}

	return dead
}

// releaseRecorded is the record pass of [runHostSweep]. For each
// record in dead it deletes the recorded SA and binding, and then
// it removes the record. It reaches the cluster through the context
// of the record in the kubeconfig at kubeconfigPath. The records of
// one context share one client, and their API calls share at most
// [recordPassTimeout]. The contexts run in parallel, so a cluster
// that does not answer delays no other cluster. It logs every
// failure and leaves the record of the failure in place.
func releaseRecorded(
	ctx context.Context,
	records *ledger.Ledger,
	kubeconfigPath string,
	dead []ledger.Record,
) {
	byContext := make(map[string][]ledger.Record)

	for _, r := range dead {
		byContext[r.Context] = append(byContext[r.Context], r)
	}

	var wg sync.WaitGroup

	for contextName, group := range byContext {
		wg.Go(func() {
			releaseRecordedOn(ctx, records, kubeconfigPath, contextName, group)
		})
	}

	wg.Wait()
}

// releaseRecordedOn handles the records of one context for
// [releaseRecorded].
func releaseRecordedOn(
	ctx context.Context,
	records *ledger.Ledger,
	kubeconfigPath, contextName string,
	group []ledger.Record,
) {
	ctx, cancel := context.WithTimeout(ctx, recordPassTimeout)
	defer cancel()

	client, err := hostKubeClient(kubeconfigPath, contextName)
	if err != nil {
		slog.WarnContext(ctx, "host sweep build kube client for provision records",
			slog.String("kubeconfig", kubeconfigPath),
			slog.String("context", contextName),
			slog.Int("records", len(group)),
			slog.Any("error", err),
		)

		return
	}

	for i, r := range group {
		if ctx.Err() != nil {
			slog.WarnContext(ctx, "host sweep ran out of time for provision records",
				slog.String("kubeconfig", kubeconfigPath),
				slog.String("context", contextName),
				slog.Int("skipped", len(group)-i),
			)

			return
		}

		err := serviceaccount.Release(ctx, client, r.Namespace, r.ServiceAccount, r.ClusterScoped)
		if err != nil {
			slog.WarnContext(ctx, "host sweep release recorded service account",
				slog.String("context", r.Context),
				slog.String("namespace", r.Namespace),
				slog.String("name", r.ServiceAccount),
				slog.String("instance_id", r.InstanceID),
				slog.Any("error", err),
			)

			continue
		}

		slog.InfoContext(ctx, "host sweep released recorded service account",
			slog.String("context", r.Context),
			slog.String("namespace", r.Namespace),
			slog.String("name", r.ServiceAccount),
			slog.String("instance_id", r.InstanceID),
		)

		removeRecord(ctx, records, r)
	}
}

// removeRecord removes r from the ledger. A failure is logged and
// swallowed. A record that stays costs a later sweep one release
// that finds nothing on the cluster.
func removeRecord(ctx context.Context, records *ledger.Ledger, r ledger.Record) {
	err := records.Remove(r)
	if err != nil {
		slog.WarnContext(ctx, "remove provision record",
			slog.String("context", r.Context),
			slog.String("namespace", r.Namespace),
			slog.String("name", r.ServiceAccount),
			slog.Any("error", err),
		)
	}
}

// forgetServiceAccount removes every provision record of one SA
// from the ledger. The arguments locate the SA the way a record
// does. `host release` calls it after the cluster holds neither
// the SA nor its binding. Every record of that SA is then stale,
// whichever `serve` owns the record, so the caller does not name
// an instance. Failures are logged and swallowed.
func forgetServiceAccount(
	ctx context.Context,
	records *ledger.Ledger,
	kubeconfigPath, contextName, namespace, saName string,
) {
	// `host select` records the absolute path.
	absKubeconfig, err := filepath.Abs(kubeconfigPath)
	if err != nil {
		slog.WarnContext(ctx, "resolve kubeconfig path for provision records", slog.Any("error", err))

		return
	}

	released := ledger.Record{
		Kubeconfig:     absKubeconfig,
		Context:        contextName,
		Namespace:      namespace,
		ServiceAccount: saName,
	}

	all, err := records.List()
	if err != nil {
		slog.WarnContext(ctx, "read provision records", slog.Any("error", err))

		return
	}

	for _, r := range all {
		if r.SameServiceAccount(released) {
			removeRecord(ctx, records, r)
		}
	}
}

// runSweep shells out to `host sweep` with the calling serve's
// host id and the discovered live-instance set. Failure is logged
// at warn level and never propagated; startup is not blocked on a
// successful sweep. --context is intentionally omitted from the
// argv. Without it, the sweep covers the cluster of
// `cfg.CurrentContext` in the host kubeconfig by label, and it
// covers the cluster of every provision record of a dead serve
// that names the host kubeconfig.
//
// liveAsOf is the time at which the serve took liveSet, and
// runSweep passes it as --live-as-of. A serve that starts after
// that time is missing from liveSet. So the label pass keeps each
// resource that the apiserver created after liveAsOf, and the
// record pass keeps each provision record that `host select` wrote
// after liveAsOf.
//
// Refuses to invoke [runHostSweep] when liveSet is empty or does
// not contain h.instanceID. Either case indicates degraded
// discovery (sidecar read or dial probe failed for the own slot);
// shelling out anyway would let [runHostSweep]'s destructive
// "manual recovery" semantics delete every peer-serve's
// resources. The manual `host sweep` CLI keeps the
// empty-live-set semantics so operator recovery still works.
func (h *handler) runSweep(ctx context.Context, liveSet map[string]struct{}, liveAsOf time.Time) {
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
	args = append(args,
		"--host-id", h.hostID,
		"--live-as-of", liveAsOf.UTC().Format(time.RFC3339Nano),
	)

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
