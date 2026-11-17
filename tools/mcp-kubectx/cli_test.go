package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/execplugin"
)

// TestDispatchExecPlugin pins that `mcp-kubectx exec-plugin
// --socket <path>` reaches runExecPluginClient (asserted by the
// connect-time error sentinel), confirming the dispatcher routes
// the new subcommand correctly.
func TestDispatchExecPlugin(t *testing.T) { //nolint:paralleltest // mutates package-level hostStdout
	withHostStdout(t)

	bogus := filepath.Join(t.TempDir(), "no-such.sock")

	err := dispatch(t.Context(), []string{"mcp-kubectx", "exec-plugin", "--socket", bogus})
	require.ErrorIs(t, err, execplugin.ErrConnect,
		"exec-plugin must route to runExecPluginClient")
}

// TestServeSocketSlotsRejectsNonPositive pins runServe's post-parse
// validation: --socket-slots <= 0 surfaces [ErrInvalidSocketSlots]
// before any cluster or socket work happens. flag.Int accepts the
// raw integer; the explicit check is what enforces the contract
// matched by the Nix bundle's `types.ints.positive`.
func TestServeSocketSlotsRejectsNonPositive(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		val string
	}{
		"zero":     {val: "0"},
		"negative": {val: "-1"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := runServe(t.Context(), []string{"--socket-slots", tc.val})
			require.ErrorIs(t, err, ErrInvalidSocketSlots)
		})
	}
}

// TestDispatchHostCommand pins the argv that the binary takes when
// it runs as [hostCommand], the name that a Lima guest reaches
// through `workmux host-exec`. Under that name the binary runs the
// `host` subcommands with no `host` word in front. It refuses every
// other subcommand before it touches a file, so a guest cannot make
// the host remove the --output path of `serve`, append to its
// --log-file path, or read a token from the socket of a host-side
// serve through `exec-plugin`.
//
//nolint:paralleltest // mutates package-level state, uses t.Setenv
func TestDispatchHostCommand(t *testing.T) {
	tests := map[string]struct {
		err   error
		argv0 string
		want  string
		args  []string
	}{
		"list": {
			args: []string{subList},
			want: "Available contexts:",
		},
		"bare name of the link": {
			argv0: hostCommand,
			args:  []string{subList},
			want:  "Available contexts:",
		},
		"serve": {
			args: []string{subServe, "--output", "OUT", "--log-file", "LOG"},
			err:  ErrUnknownSubcommand,
		},
		"leading serve flag": {
			args: []string{"--output", "OUT"},
			err:  ErrUnknownSubcommand,
		},
		"exec-plugin": {
			args: []string{subExecPlugin, "--socket", "SOCK"},
			err:  ErrUnknownSubcommand,
		},
		"host word in front": {
			args: []string{subHost, subList},
			err:  ErrUnknownSubcommand,
		},
		"no subcommand": {
			err: ErrUnknownSubcommand,
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests mutate package-level state
		t.Run(name, func(t *testing.T) {
			stdout := withHostStdout(t)
			withHostLedger(t)
			withHostConfigFile(t, `{"role":"view"}`)

			dir := t.TempDir()
			isolateServeEnv(t, dir)
			t.Setenv("KUBECONFIG_HOST", writeTestKubeconfig(t, testKubeconfig()))

			out := filepath.Join(dir, "host.key")
			logFile := filepath.Join(dir, "leak.log")
			sock := filepath.Join(dir, "serve.0.host.sock")

			require.NoError(t, os.WriteFile(out, []byte(testHostID+"\n"), 0o600))

			argv0 := tc.argv0
			if argv0 == "" {
				argv0 = "/nix/store/x-mcp-kubectx/bin/" + hostCommand
			}

			args := []string{argv0}

			for _, arg := range tc.args {
				switch arg {
				case "OUT":
					arg = out
				case "LOG":
					arg = logFile
				case "SOCK":
					arg = sock
				}

				args = append(args, arg)
			}

			err := dispatch(t.Context(), args)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				require.NotErrorIs(t, err, execplugin.ErrConnect)
				assert.FileExists(t, out, "the call must not remove the --output path")
				assert.NoFileExists(t, logFile, "the call must not create the --log-file path")
				assert.Empty(t, stdout.String())

				return
			}

			require.NoError(t, err)
			assert.Contains(t, stdout.String(), tc.want)
		})
	}
}

// TestHostCommandProcess starts the test binary as a process whose
// argv[0] is [hostCommand], as `workmux host-exec` starts the link
// on the host, with the argv of the reproduction of a guest that
// removes host.key through `serve --output`. The process exits 1 and
// leaves the --output and --log-file paths alone.
func TestHostCommandProcess(t *testing.T) {
	t.Parallel()

	self, err := os.Executable()
	require.NoError(t, err)

	dir := t.TempDir()
	out := filepath.Join(dir, "host.key")
	logFile := filepath.Join(dir, "leak.log")

	require.NoError(t, os.WriteFile(out, []byte(testHostID+"\n"), 0o600))

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, self)
	cmd.Args = []string{hostCommand, subServe, "--output", out, "--log-file", logFile}
	// Only the variables that `workmux host-exec` passes, with every
	// path in dir. A serve that this process starts by mistake then
	// binds its socket and writes its id in dir, and it finds no
	// kubeconfig.
	cmd.Env = []string{
		runMainEnv + "=1",
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + dir,
		"XDG_CONFIG_HOME=" + filepath.Join(dir, "config"),
		"XDG_STATE_HOME=" + filepath.Join(dir, "state"),
	}

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	var exitErr *exec.ExitError

	require.ErrorAs(t, cmd.Run(), &exitErr)
	assert.Equal(t, 1, exitErr.ExitCode())
	assert.Contains(t, stderr.String(), ErrUnknownSubcommand.Error())
	assert.FileExists(t, out)
	assert.NoFileExists(t, logFile)
}

// isolateServeEnv points every path and variable that a `serve` reads
// at dir, or clears it. A test whose dispatch starts `serve` by
// mistake then binds its socket and writes its id in dir. It finds
// no kubeconfig, and it does not shell out through `workmux
// host-exec`. Each `host` subcommand that such a serve forks runs
// [run] in the test binary, and not the tests.
func isolateServeEnv(t *testing.T, dir string) {
	t.Helper()

	t.Setenv(runMainEnv, "1")
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("KUBECONFIG", "")
	t.Setenv("KUBECONFIG_HOST", "")
	t.Setenv(guestEnvVar, "")
	t.Setenv("WM_RPC_HOST", "")
	t.Setenv("WM_RPC_PORT", "")
	t.Setenv("WM_RPC_TOKEN", "")
}
