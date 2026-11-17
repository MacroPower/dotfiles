package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/serviceaccount"
)

// Sentinel errors for the host config file.
var (
	// ErrLoadHostConfig wraps a failure to find, read, parse, or
	// validate the host config file.
	ErrLoadHostConfig = errors.New("load host config")
	// ErrNoHome reports that the `host` subcommands cannot resolve
	// an absolute home directory. They then refuse to run, because
	// each default path of the host lies under the home directory.
	ErrNoHome = errors.New("resolve absolute home directory")
)

// hostConfigPath returns the path of the host config file. Tests
// replace it to keep the config in a temporary directory.
//
//nolint:grouper // test indirection, kept beside the loader that reads it
var hostConfigPath = defaultHostConfigPath

// hostConfig is the content of the host config file. It holds every
// limit that a `host` subcommand applies to a ServiceAccount, so
// that no caller sets a limit through argv. A Lima guest runs the
// `host` subcommands through `workmux host-exec` with an argv of its
// own choice, and no guest can write the file.
//
// The JSON keys of [serviceaccount.Config] sit at the top level of
// the file, beside allowedAPIServerHosts.
type hostConfig struct {
	// AllowedAPIServerHosts lists the hostnames that `host select`
	// accepts as the `server` of the cluster of a context. An empty
	// list accepts every apiserver of the host kubeconfig.
	AllowedAPIServerHosts []string `json:"allowedAPIServerHosts"`

	serviceaccount.Config
}

// defaultHostConfigPath returns mcp-kubectx/config.json in
// $XDG_CONFIG_HOME, or in ~/.config when $XDG_CONFIG_HOME does not
// hold an absolute path. home/claude.nix writes the file there. The
// path never depends on the working directory, because `workmux
// host-exec` runs each `host` subcommand in a worktree that the
// guest can write.
func defaultHostConfigPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(dir) {
		home, err := absHome()
		if err != nil {
			return "", err
		}

		dir = filepath.Join(home, ".config")
	}

	return filepath.Join(dir, "mcp-kubectx", "config.json"), nil
}

// absHome returns the home directory of the user. It returns
// [ErrNoHome] when the directory is unknown or not absolute.
func absHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrNoHome, err)
	}

	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("%w: %q", ErrNoHome, home)
	}

	return home, nil
}

// loadHostConfig reads the host config file at [hostConfigPath]. The
// file holds one JSON object. A key that [hostConfig] does not
// define fails the load, so that a misspelled allowedAPIServerHosts
// never becomes an empty allowlist that accepts every apiserver.
// loadHostConfig applies the defaults of
// [serviceaccount.Config.Validate] and returns its errors.
func loadHostConfig() (hostConfig, error) {
	path, err := hostConfigPath()
	if err != nil {
		return hostConfig{}, fmt.Errorf("%w: %w", ErrLoadHostConfig, err)
	}

	data, err := os.ReadFile(path) //nolint:gosec // the path comes from the environment of the host, not from argv
	if err != nil {
		return hostConfig{}, fmt.Errorf("%w: %w", ErrLoadHostConfig, err)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var cfg hostConfig

	err = dec.Decode(&cfg)
	if err != nil {
		return hostConfig{}, fmt.Errorf("%w: %s: %w", ErrLoadHostConfig, path, err)
	}

	_, err = dec.Token()
	if !errors.Is(err, io.EOF) {
		return hostConfig{}, fmt.Errorf("%w: %s: data after the JSON object", ErrLoadHostConfig, path)
	}

	err = cfg.Validate()
	if err != nil {
		return hostConfig{}, fmt.Errorf("%w: %s: %w", ErrLoadHostConfig, path, err)
	}

	return cfg, nil
}
