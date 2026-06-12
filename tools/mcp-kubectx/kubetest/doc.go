// Package kubetest provides a recording, fault-injectable fake
// implementation of the kube.Client interface for tests that drive
// ServiceAccount provisioning, release, token minting, and the
// orphan sweep without touching a real cluster.
package kubetest
