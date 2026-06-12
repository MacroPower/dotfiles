// Package kubeconfig models the minimal kubeconfig subset
// mcp-kubectx needs: enumerating contexts, extracting individual
// context entries, and composing scoped single-context files.
// Cluster and user data are kept opaque ([any]) so unmodeled fields
// round-trip through load/marshal without the package tracking the
// full upstream schema.
//
// The deliberate narrowness is load-bearing for callers that write
// files back: a round-trip through [Config] normalizes the file to
// the modeled subset, so only files this tool owns outright (the
// scoped kubeconfig, the wrapper's local.yaml selection stub) should
// ever be written through it.
package kubeconfig
