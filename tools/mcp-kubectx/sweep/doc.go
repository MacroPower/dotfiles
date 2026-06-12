// Package sweep classifies and deletes orphaned mcp-kubectx
// ServiceAccounts and bindings: resources labeled with this host's
// id whose provisioning `serve` instance is no longer alive.
//
// The classifier is conservative by construction. The apiserver-side
// LabelSelector built by [Selector] pins on both the managed-by
// value and the host id, so cross-host resources never reach the
// classifier; a resource without an instance-id label cannot be
// attributed to a serve and is preserved; and a resource whose
// instance id appears in the caller-supplied live set is preserved.
// Everything else is deleted best-effort with bounded concurrency --
// per-resource delete failures are logged and swallowed because the
// next serve startup retries the sweep anyway.
package sweep
