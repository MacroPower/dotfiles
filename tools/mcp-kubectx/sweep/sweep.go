package sweep

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kube"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/serviceaccount"
)

// Sentinel errors for the orphan sweep.
var (
	// ErrList wraps any failure to list a resource kind. The sweep
	// tolerates partial list failure: per-kind errors are logged at
	// warn level and the classification continues with whatever
	// subset it could enumerate. [CollectOrphans] surfaces this
	// sentinel only when every list call fails outright (rare:
	// usually only on RBAC misconfiguration that forbids
	// cluster-wide list on all three resource kinds).
	ErrList = errors.New("sweep list")
	// ErrDelete wraps a per-resource delete failure inside the
	// warn-log line. Never propagated from [DeleteOrphans]; the
	// sentinel exists only so the warn-log entries carry a
	// consistent error prefix.
	ErrDelete = errors.New("sweep delete")
)

// concurrency bounds the number of in-flight K8s Delete* calls
// issued by [DeleteOrphans]. Small enough to be polite to a shared
// apiserver, large enough to keep startup latency tight when a
// backlog of orphans accumulates.
const concurrency = 8

// CreationSlack is the margin that [CollectOrphans] subtracts from
// the time of the live set before it compares that time with the
// creation time of each resource. The apiserver keeps only whole
// seconds of the creation time, and its clock can run behind the
// clock that took the live set. Either one makes a resource that the
// apiserver created after the live set look older than the live set.
// The slack keeps such a resource. A resource of a dead `serve` that
// the apiserver created less than CreationSlack before the live set
// stays until a later sweep.
const CreationSlack = time.Minute

// Kind labels the three resource families the sweep classifies.
// Used only inside log lines so operators can disambiguate counters
// per kind.
type Kind string

// The resource kinds the sweep enumerates.
const (
	KindServiceAccount     Kind = "ServiceAccount"
	KindRoleBinding        Kind = "RoleBinding"
	KindClusterRoleBinding Kind = "ClusterRoleBinding"
)

// Selector builds the LabelSelector used by every List call in
// [CollectOrphans]. The selector pins on both the managed-by value
// and the host id so cross-host resources are filtered out at the
// apiserver before the classifier ever runs.
func Selector(hostID string) string {
	return fmt.Sprintf("%s=%s,%s=%s",
		serviceaccount.ManagedByLabel, serviceaccount.ManagedByValue,
		serviceaccount.HostIDLabel, hostID,
	)
}

// Candidate is one classified resource awaiting deletion.
type Candidate struct {
	// Ref locates the resource.
	Ref kube.ResourceRef

	// Kind selects the Delete* call [DeleteOrphans] issues.
	Kind Kind
}

// listResult carries one kind's list outcome through the
// fan-out/fan-in in [CollectOrphans]. err being non-nil means the
// apiserver call failed for that kind; refs is then empty.
type listResult struct {
	err  error
	kind Kind
	refs []kube.ResourceRef
}

// CollectOrphans lists each managed resource kind in parallel and
// classifies every entry. It returns the candidates to delete.
//
// live holds the instance id of each `serve` that was alive at
// liveAsOf. A `serve` that started after liveAsOf is missing from
// live, but it can create resources before the list calls answer.
// So CollectOrphans keeps each resource that the apiserver created
// at liveAsOf minus [CreationSlack] or later, whatever its instance
// id.
//
// CollectOrphans logs the failed list call of a kind at warn level
// and classifies the kinds that it could list. It returns an error
// only when every list call fails. The error then wraps [ErrList],
// and the orphan slice is empty. So a caller can report a sweep that
// listed nothing and still tolerate RBAC that forbids some kinds.
// The parallel list calls cost one apiserver round-trip instead of
// three when the cluster is remote.
func CollectOrphans(
	ctx context.Context,
	client kube.Client,
	selector string,
	live map[string]struct{},
	liveAsOf time.Time,
) ([]Candidate, error) {
	cutoff := liveAsOf.Add(-CreationSlack)

	listers := []struct {
		fn   func(context.Context, string) ([]kube.ResourceRef, error)
		kind Kind
		name string
	}{
		{client.ListServiceAccounts, KindServiceAccount, "service accounts"},
		{client.ListRoleBindings, KindRoleBinding, "role bindings"},
		{client.ListClusterRoleBindings, KindClusterRoleBinding, "cluster role bindings"},
	}

	results := make(chan listResult, len(listers))

	var wg sync.WaitGroup

	for _, l := range listers {
		wg.Go(func() {
			refs, err := l.fn(ctx, selector)
			if err != nil {
				slog.WarnContext(ctx, "sweep list "+l.name,
					slog.String("selector", selector),
					slog.Any("error", fmt.Errorf("%w: %w", ErrList, err)),
				)
			}

			results <- listResult{kind: l.kind, refs: refs, err: err}
		})
	}

	wg.Wait()
	close(results)

	var (
		orphans  []Candidate
		listErrs []error
	)

	for r := range results {
		if r.err != nil {
			listErrs = append(listErrs, r.err)
		}

		for _, ref := range r.refs {
			if shouldDelete(ref, live, cutoff) {
				orphans = append(orphans, Candidate{Ref: ref, Kind: r.kind})
			}
		}
	}

	if len(listErrs) == len(listers) {
		return nil, fmt.Errorf("%w: %w", ErrList, errors.Join(listErrs...))
	}

	return orphans, nil
}

// shouldDelete reports whether a single resource is orphan. It
// keeps these resources:
//
//   - A resource without an instance-id label, or with an empty
//     one. Resources from builds before the label cannot name the
//     `serve` that owns them. The manual recovery that the README
//     documents handles them.
//   - A resource whose instance id is in the live set.
//   - A resource that the apiserver created at cutoff or later. Its
//     `serve` might have started after the caller took the live
//     set.
//   - A resource without a creation time, because shouldDelete
//     cannot tell when the apiserver created it.
//
// Every other resource is orphan.
func shouldDelete(ref kube.ResourceRef, live map[string]struct{}, cutoff time.Time) bool {
	id, ok := ref.Labels[serviceaccount.InstanceIDLabel]
	if !ok {
		return false
	}

	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}

	_, isLive := live[id]
	if isLive {
		return false
	}

	return !ref.Created.IsZero() && ref.Created.Before(cutoff)
}

// DeleteOrphans deletes the classified orphan set with a bounded
// worker pattern. A buffered chan acts as a semaphore; each delete
// error is logged and swallowed so the caller never propagates a
// per-resource failure to the parent process. [errgroup] is
// deliberately not used because its first-error-cancels-group
// semantics would interrupt other in-flight deletes.
func DeleteOrphans(ctx context.Context, client kube.Client, orphans []Candidate) {
	if len(orphans) == 0 {
		return
	}

	slog.InfoContext(ctx, "sweep deleting orphans", slog.Int("count", len(orphans)))

	sem := make(chan struct{}, concurrency)

	var wg sync.WaitGroup

	for _, c := range orphans {
		wg.Add(1)

		sem <- struct{}{}

		go func(c Candidate) {
			defer wg.Done()
			defer func() { <-sem }()

			deleteCandidate(ctx, client, c)
		}(c)
	}

	wg.Wait()
}

// deleteCandidate issues the matching Delete* call for the
// candidate's resource kind. Errors are wrapped with [ErrDelete]
// for log-line consistency and never propagated up; the sweep is
// best-effort. Unknown kinds log a warn so a future addition to
// [Kind] without extending this switch fails loud instead of silent.
func deleteCandidate(ctx context.Context, client kube.Client, c Candidate) {
	var err error

	switch c.Kind {
	case KindServiceAccount:
		err = client.DeleteServiceAccount(ctx, c.Ref.Namespace, c.Ref.Name)
	case KindRoleBinding:
		err = client.DeleteRoleBinding(ctx, c.Ref.Namespace, c.Ref.Name)
	case KindClusterRoleBinding:
		err = client.DeleteClusterRoleBinding(ctx, c.Ref.Name)
	default:
		slog.WarnContext(ctx, "sweep delete: unknown kind",
			slog.String("kind", string(c.Kind)),
			slog.String("name", c.Ref.Name),
		)

		return
	}

	if err != nil {
		slog.WarnContext(ctx, "sweep delete",
			slog.String("kind", string(c.Kind)),
			slog.String("namespace", c.Ref.Namespace),
			slog.String("name", c.Ref.Name),
			slog.Any("error", fmt.Errorf("%w: %w", ErrDelete, err)),
		)
	}
}
