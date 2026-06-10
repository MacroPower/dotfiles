// Package kubectx gates kubectl usage on an mcp-kubectx context
// selection and manages the per-session kubectx directories the Claude
// Code launcher wrapper creates.
//
// The gate reads the wrapper-provided kubeconfig environment
// ($CLAUDE_KUBECTX_LOCAL and friends) to decide whether a context is
// effectively selected, and inspects shell ASTs for kubectl calls that
// try to point themselves at another kubeconfig. The directory
// lifecycle side removes a session's directory at SessionEnd and
// sweeps orphans whose owning process has exited.
package kubectx
