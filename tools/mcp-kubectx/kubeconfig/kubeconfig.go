package kubeconfig

import (
	"errors"
	"fmt"
	"net/url"
	"os"

	"gopkg.in/yaml.v3"
)

// Sentinel errors for kubeconfig operations.
var (
	// ErrLoad wraps any failure to read or parse a kubeconfig file.
	ErrLoad = errors.New("load kubeconfig")
	// ErrWrite wraps any failure to marshal or write a kubeconfig
	// file.
	ErrWrite = errors.New("write kubeconfig")
)

// Config represents a minimal kubeconfig structure sufficient for
// listing contexts and extracting individual context entries.
// Cluster and user data use [any] to round-trip opaque fields
// without modeling the full schema.
type Config struct {
	APIVersion     string         `yaml:"apiVersion"`
	Kind           string         `yaml:"kind"`
	CurrentContext string         `yaml:"current-context"`
	Clusters       []NamedCluster `yaml:"clusters"`
	Contexts       []NamedContext `yaml:"contexts"`
	Users          []NamedUser    `yaml:"users"`
}

// NamedCluster is one entry in the kubeconfig clusters list. The
// cluster body is opaque; use [ServerHost] to extract the apiserver
// hostname.
type NamedCluster struct {
	Cluster any    `yaml:"cluster"`
	Name    string `yaml:"name"`
}

// NamedContext is one entry in the kubeconfig contexts list.
type NamedContext struct {
	Name    string  `yaml:"name"`
	Context Context `yaml:"context"`
}

// Context carries the cluster/user/namespace triple of one context
// entry.
type Context struct {
	Cluster   string `yaml:"cluster"`
	User      string `yaml:"user"`
	Namespace string `yaml:"namespace,omitempty"`
}

// NamedUser is one entry in the kubeconfig users list. The user body
// is opaque so exec blocks and inline credentials round-trip without
// modeling.
type NamedUser struct {
	User any    `yaml:"user"`
	Name string `yaml:"name"`
}

// Load reads and parses a kubeconfig file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // kubeconfig paths come from operator flags and wrapper env by design
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrLoad, err)
	}

	var cfg Config

	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrLoad, err)
	}

	return &cfg, nil
}

// Marshal renders the config as YAML, wrapping failures with
// [ErrWrite].
func (c *Config) Marshal() ([]byte, error) {
	data, err := yaml.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrWrite, err)
	}

	return data, nil
}

// ServerHost extracts the hostname from a kubeconfig cluster's
// `server` URL. yaml.v3 always decodes string-keyed maps into
// map[string]any, so a single type assertion covers every
// well-formed kubeconfig.
func ServerHost(cluster any) (string, error) {
	m, ok := cluster.(map[string]any)
	if !ok {
		return "", fmt.Errorf("cluster is not an object: %T", cluster)
	}

	server, ok := m["server"].(string)
	if !ok || server == "" {
		return "", fmt.Errorf("cluster.server missing or not a string")
	}

	u, err := url.Parse(server)
	if err != nil {
		return "", fmt.Errorf("parse cluster.server %q: %w", server, err)
	}

	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("cluster.server %q has empty host", server)
	}

	return host, nil
}
