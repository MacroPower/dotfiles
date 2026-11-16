// Package sweep classifies and deletes orphaned mcp-kubectx
// ServiceAccounts and bindings. An orphan carries the id of this
// host, and the `serve` instance that provisioned it is no longer
// alive.
//
// The classifier keeps every resource whose owner it cannot prove
// dead:
//
//   - The LabelSelector of [Selector] pins on both the managed-by
//     value and the host id, so the apiserver filters out the
//     resources of other hosts before the classifier runs.
//   - The classifier keeps each resource without an instance-id
//     label, because only that label names the `serve` that owns
//     the resource.
//   - The classifier keeps each resource whose instance id is in the
//     live set of the caller.
//   - The classifier keeps each resource that the apiserver created
//     after the caller took the live set, with [CreationSlack] for
//     the whole seconds of the creation time and for clock skew. A
//     `serve` that starts after the caller takes the live set is
//     missing from that set, but it can create resources before the
//     list calls answer.
//
// [DeleteOrphans] deletes every other resource with bounded
// concurrency. It logs and swallows each failed delete, because the
// startup of the next `serve` runs the sweep again.
package sweep
