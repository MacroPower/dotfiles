// Package serviceaccount provisions the short-lived, role-bound
// Kubernetes ServiceAccounts that back mcp-kubectx's scoped
// kubeconfigs, and defines the label taxonomy that lets the orphan
// sweep attribute each provisioned resource to the host and `serve`
// instance that created it.
//
// Provisioning is create-SA-then-bind with a best-effort rollback:
// a binding failure deletes the just-created SA, because an SA
// carrying a live serve's own instance id would dodge every sweep
// for as long as that serve runs. The binding name is derived from
// the SA name by [BindingName] -- the single source of truth shared
// by create and release so the two cannot drift.
package serviceaccount
