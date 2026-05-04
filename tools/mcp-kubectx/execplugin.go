package main

// execAuthAPIVersion is the kubectl exec credential plugin protocol
// version used by the scoped kubeconfig.
const execAuthAPIVersion = "client.authentication.k8s.io/v1"

// execPlugin is the structure of the `user.exec` block written into
// the scoped kubeconfig. It mirrors the upstream
// [k8s.io/client-go/tools/clientcmd/api/v1.ExecConfig] schema closely
// enough for kubectl/client-go to consume.
type execPlugin struct {
	APIVersion      string   `yaml:"apiVersion"`
	Command         string   `yaml:"command"`
	InteractiveMode string   `yaml:"interactiveMode"`
	Args            []string `yaml:"args"`
}

// execPluginParams describes the inputs needed to build the kubectl
// exec credential plugin block. Only the per-`serve` socket path
// varies; the host- and guest-side kubeconfigs both use the same
// shim that connects back to its own serve over UDS, hiding the
// host/guest distinction from kubectl.
type execPluginParams struct {
	SocketPath string
}

// buildExecPlugin returns the `user.exec` config for the scoped
// kubeconfig. The plugin is a tiny in-binary UDS client
// (`mcp-kubectx exec-plugin --socket <path>`); both host- and
// guest-side serves write the same shape, so kubectl never sees
// the workmux host-exec wrapper. The wrapper still happens server
// side: guest serve's [*handler.defaultRunHost] decides between a
// direct fork and `workmux host-exec` for the in-process token
// call, transparent to the kubeconfig.
//
// `command` is the bare program name "mcp-kubectx" to keep the
// scoped kubeconfig portable across rebuilds (the absolute store
// path would be invalidated by the next nix-darwin switch). The
// binary is on PATH for both the host and the in-Lima profiles via
// `home.packages` in `home/claude.nix`.
func buildExecPlugin(p execPluginParams) execPlugin {
	return execPlugin{
		APIVersion:      execAuthAPIVersion,
		Command:         "mcp-kubectx",
		Args:            []string{"exec-plugin", "--socket", p.SocketPath},
		InteractiveMode: "Never",
	}
}
