package execplugin

// APIVersion is the kubectl exec credential plugin protocol version
// used by the scoped kubeconfig.
const APIVersion = "client.authentication.k8s.io/v1"

// Plugin is the structure of the `user.exec` block written into the
// scoped kubeconfig. It mirrors the upstream
// [k8s.io/client-go/tools/clientcmd/api/v1.ExecConfig] schema closely
// enough for kubectl/client-go to consume.
type Plugin struct {
	APIVersion      string   `yaml:"apiVersion"`
	Command         string   `yaml:"command"`
	InteractiveMode string   `yaml:"interactiveMode"`
	Args            []string `yaml:"args"`
}

// New returns the `user.exec` config for the scoped kubeconfig. The
// plugin is a tiny in-binary UDS client (`mcp-kubectx exec-plugin
// --socket <path>`); only the per-`serve` socket path varies. Both
// host- and guest-side serves write the same shape, so kubectl never
// sees the workmux host-exec wrapper. The wrapper still happens
// server side: a guest serve decides between a direct fork and
// `workmux host-exec` for the in-process token call, transparent to
// the kubeconfig.
//
// `command` is the bare program name "mcp-kubectx" to keep the
// scoped kubeconfig portable across rebuilds (the absolute store
// path would be invalidated by the next nix-darwin switch). The
// binary is on PATH for both the host and the in-Lima profiles via
// `home.packages` in `home/claude.nix`.
func New(socketPath string) Plugin {
	return Plugin{
		APIVersion:      APIVersion,
		Command:         "mcp-kubectx",
		Args:            []string{"exec-plugin", "--socket", socketPath},
		InteractiveMode: "Never",
	}
}

// Credential is the kubectl exec credential plugin output schema.
// Only the fields kubectl reads on success are populated.
type Credential struct {
	APIVersion string           `json:"apiVersion"`
	Kind       string           `json:"kind"`
	Status     CredentialStatus `json:"status"`
}

// CredentialStatus carries the bearer token kubectl uses for the
// next API call. kubectl caches the credential by
// expirationTimestamp and only re-invokes the plugin once it expires.
type CredentialStatus struct {
	ExpirationTimestamp string `json:"expirationTimestamp"`
	Token               string `json:"token"`
}
