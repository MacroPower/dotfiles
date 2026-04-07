package main

import (
	"fmt"
	"os"
	"strconv"
)

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

// execPluginParams describes the inputs needed to mint a token via
// the host token subcommand. The exec plugin pins these flags so
// that the running plugin process cannot be subverted by caller-side
// environment, and so the host-resolved kubeconfig path survives
// across $KUBECONFIG sanitization in workmux host-exec.
type execPluginParams struct {
	KubeconfigPath string
	Context        string
	SAName         string
	Namespace      string
	Expiration     int
	ForGuest       bool
}

// buildExecPlugin returns the `user.exec` config for the scoped
// kubeconfig. When forGuest is false the plugin invokes the
// mcp-kubectx binary at its current absolute store path; when
// forGuest is true it goes through workmux host-exec so the in-VM
// kubectl can reach the host.
func buildExecPlugin(p execPluginParams) (execPlugin, error) {
	tokenArgs := []string{
		"host", "token",
		"--kubeconfig", p.KubeconfigPath,
		"--context", p.Context,
		"--sa", p.SAName,
		"--namespace", p.Namespace,
		"--sa-expiration", strconv.Itoa(p.Expiration),
	}

	var (
		cmd  string
		args = tokenArgs
	)

	if p.ForGuest {
		cmd = "workmux"

		args = append([]string{"host-exec", "mcp-kubectx"}, tokenArgs...)
	} else {
		self, err := os.Executable()
		if err != nil {
			return execPlugin{}, fmt.Errorf("resolve executable path: %w", err)
		}

		cmd = self
	}

	return execPlugin{
		APIVersion:      execAuthAPIVersion,
		Command:         cmd,
		Args:            args,
		InteractiveMode: "Never",
	}, nil
}
