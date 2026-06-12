// Package kube narrows the Kubernetes API down to the calls
// mcp-kubectx makes: ServiceAccount and binding provisioning,
// cleanup, label-filtered listing for the orphan sweep, and
// short-lived token minting via TokenRequest.
//
// [Client] is the seam between domain logic and the cluster:
// consumers accept the interface, production code constructs the
// client-go-backed [Clientset], and tests substitute a recording
// fake from the kubetest package. List results are projected
// into [ResourceRef] rather than typed K8s objects so the interface
// stays narrow and fakes stay trivially small.
package kube
