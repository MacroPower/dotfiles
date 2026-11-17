package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kube"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kubetest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/statefile"
)

func TestHostExecArgsHost(t *testing.T) {
	t.Parallel()

	h := &handler{envLookup: constLookup("")}

	cmd, argv, err := h.hostExecArgs("token", []string{"--sa", "claude-sa-1"})
	require.NoError(t, err)

	assert.NotEqual(t, "workmux", cmd, "host variant must not invoke workmux")
	assert.Equal(t, []string{"host", "token", "--sa", "claude-sa-1"}, argv)
}

func TestHostExecArgsGuest(t *testing.T) {
	t.Parallel()

	h := &handler{envLookup: constLookup("1")}

	cmd, argv, err := h.hostExecArgs("select", []string{"prod", "--sa-host-id", testHostID})
	require.NoError(t, err)

	assert.Equal(t, "workmux", cmd)
	assert.Equal(t, []string{
		"host-exec", "mcp-kubectx-host",
		"select", "prod", "--sa-host-id", testHostID,
	}, argv)
}

func TestIsGuest(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		val  string
		want bool
	}{
		"unset":  {val: "", want: false},
		"one":    {val: "1", want: true},
		"zero":   {val: "0", want: false},
		"truthy": {val: "true", want: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := &handler{envLookup: constLookup(tc.val)}
			assert.Equal(t, tc.want, h.isGuest())
		})
	}
}

// fakeExecutable writes a small shell script at $TMPDIR/<name> with
// the given body and returns its absolute path. Tests of
// [runHostCommand] run this script in place of the mcp-kubectx
// binary.
func fakeExecutable(t *testing.T, body string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "fake-mcp-kubectx")

	err := statefile.WriteSecure(path, []byte("#!/bin/sh\n"+body+"\n"))
	require.NoError(t, err)

	require.NoError(t, exec.CommandContext(t.Context(), "chmod", "+x", path).Run())

	return path
}

// recordLog returns a logger that writes JSON records at info level
// and above, and a function that decodes the records written so far.
// The decoded records drop the time key, because each record that
// serve writes itself carries the time of the call.
func recordLog(t *testing.T) (*slog.Logger, func() []map[string]any) {
	t.Helper()

	var buf bytes.Buffer

	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	return logger, func() []map[string]any {
		t.Helper()

		var records []map[string]any

		for line := range bytes.Lines(buf.Bytes()) {
			var rec map[string]any

			require.NoError(t, json.Unmarshal(line, &rec))
			require.Contains(t, rec, slog.TimeKey)

			delete(rec, slog.TimeKey)

			records = append(records, rec)
		}

		return records
	}
}

func TestRunHostCommand(t *testing.T) {
	t.Parallel()

	const record = `{"time":"2026-11-16T22:00:00.123Z","level":"WARN","msg":"release service account",` +
		`"name":"claude-sa-x","error":"forbidden"}`

	relayed := map[string]any{
		"level":      "WARN",
		"msg":        "release service account",
		"subcommand": "release",
		"name":       "claude-sa-x",
		"error":      "forbidden",
	}

	tests := map[string]struct {
		script  string
		stdout  string
		err     error
		errText string
		want    []map[string]any
	}{
		"exit 0 with a record": {
			script: "echo out; echo '" + record + "' 1>&2",
			stdout: "out\n",
			want:   []map[string]any{relayed},
		},
		"exit 0 with a record and text": {
			script: "echo out; echo 'usage line' 1>&2; echo '" + record + "' 1>&2",
			stdout: "out\n",
			want: []map[string]any{
				relayed,
				{
					"level":      "WARN",
					"msg":        "host subcommand wrote to stderr",
					"subcommand": "release",
					"stderr":     "usage line",
				},
			},
		},
		"exit 0 with nothing on stderr": {
			script: "echo out",
			stdout: "out\n",
		},
		"non-zero exit keeps the record out of the error": {
			script:  "echo out; echo '" + record + "' 1>&2; echo 'release: boom' 1>&2; exit 9",
			stdout:  "out\n",
			err:     ErrHostExec,
			errText: `host subcommand "release": exit 9: release: boom`,
			want:    []map[string]any{relayed},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			exePath := fakeExecutable(t, tc.script)
			logger, records := recordLog(t)

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			stdout, err := runHostCommand(ctx, logger, subRelease, exePath, []string{"host", subRelease})
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				require.EqualError(t, err, tc.errText)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tc.stdout, string(stdout))
			assert.Equal(t, tc.want, records())
		})
	}
}

func TestRunHostCommandBinaryNotFound(t *testing.T) {
	t.Parallel()

	bogus := filepath.Join(t.TempDir(), "no-such-binary")
	logger, records := recordLog(t)

	_, err := runHostCommand(t.Context(), logger, subList, bogus, []string{"host", subList})
	require.ErrorIs(t, err, ErrHostExecRun)
	assert.Empty(t, records())
}

// TestRunHostCommandRelaysHostRelease runs `host release` of this
// test binary as a child process, with a flag that `host release`
// does not define. The child logs a warning and exits 0, and the
// warning reaches the logger of the parent.
func TestRunHostCommandRelaysHostRelease(t *testing.T) {
	t.Parallel()

	self, err := os.Executable()
	require.NoError(t, err)

	exePath := fakeExecutable(t, runMainEnv+"=1 exec '"+self+"' \"$@\"")
	logger, records := recordLog(t)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	stdout, err := runHostCommand(ctx, logger, subRelease, exePath, []string{"host", subRelease, "--no-such-flag"})
	require.NoError(t, err, "host release exits 0 after a failure")
	assert.Empty(t, stdout)

	got := records()
	require.Len(t, got, 2)

	assert.Equal(t, map[string]any{
		"level":      "WARN",
		"msg":        "parse host release flags",
		"subcommand": "release",
		"error":      "flag provided but not defined: -no-such-flag",
	}, got[0])

	assert.Equal(t, "host subcommand wrote to stderr", got[1]["msg"])
	assert.Equal(t, "release", got[1]["subcommand"])
	assert.Contains(t, got[1]["stderr"], "Usage of host release")
}

func TestRelayHostLog(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		stderr string
		level  slog.Level
		want   []string
		text   string
	}{
		"record keeps its time, level, message, and attributes": {
			stderr: `{"time":"2026-11-16T14:00:00.123-08:00","level":"WARN","msg":"sweep delete",` +
				`"kind":"ServiceAccount","records":2,"ratio":0.5,"clusterScoped":false,"group":{"a":"b"},"none":null}` + "\n",
			want: []string{
				`{"time":"2026-11-16T14:00:00.123-08:00","level":"WARN","msg":"sweep delete","subcommand":"sweep",` +
					`"kind":"ServiceAccount","records":2,"ratio":0.5,"clusterScoped":false,"group":{"a":"b"},"none":null}`,
			},
		},
		"records below the level of the logger drop": {
			stderr: `{"time":"2026-11-16T22:00:00Z","level":"INFO","msg":"host sweep released recorded service account"}` + "\n" +
				`{"time":"2026-11-16T22:00:01Z","level":"WARN","msg":"sweep delete"}` + "\n",
			level: slog.LevelWarn,
			want: []string{
				`{"time":"2026-11-16T22:00:01Z","level":"WARN","msg":"sweep delete","subcommand":"sweep"}`,
			},
		},
		"level with an offset": {
			stderr: `{"time":"2026-11-16T22:00:00Z","level":"WARN+2","msg":"m"}`,
			want:   []string{`{"time":"2026-11-16T22:00:00Z","level":"WARN+2","msg":"m","subcommand":"sweep"}`},
		},
		"lines without a record are text": {
			stderr: "not json\n" +
				`{"msg":"no level"}` + "\n" +
				`{"level":"WARN"}` + "\n" +
				`{"level":"LOUD","msg":"unknown level"}` + "\n" +
				`{"level":"WARN","msg":"trailing"} more` + "\n" +
				`["level","WARN"]` + "\n" +
				`{"level":"WARN","msg":` + "\n",
			text: "not json\n" +
				`{"msg":"no level"}` + "\n" +
				`{"level":"WARN"}` + "\n" +
				`{"level":"LOUD","msg":"unknown level"}` + "\n" +
				`{"level":"WARN","msg":"trailing"} more` + "\n" +
				`["level","WARN"]` + "\n" +
				`{"level":"WARN","msg":`,
		},
		"text around a record": {
			stderr: "\nfirst\n" + `{"time":"2026-11-16T22:00:00Z","level":"ERROR","msg":"m"}` + "\nlast\n",
			want:   []string{`{"time":"2026-11-16T22:00:00Z","level":"ERROR","msg":"m","subcommand":"sweep"}`},
			text:   "first\nlast",
		},
		"empty": {},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: tc.level}))

			text := relayHostLog(t.Context(), logger, subSweep, []byte(tc.stderr))
			assert.Equal(t, tc.text, text)

			var got []string
			for line := range strings.Lines(buf.String()) {
				got = append(got, strings.TrimSuffix(line, "\n"))
			}

			assert.Equal(t, tc.want, got)
		})
	}
}

// TestRelayHostLogWithoutTime pins that a record without a time key
// takes the time of the relay.
func TestRelayHostLogWithoutTime(t *testing.T) {
	t.Parallel()

	logger, records := recordLog(t)

	text := relayHostLog(t.Context(), logger, subSweep, []byte(`{"level":"INFO","msg":"m","n":"v"}`))
	assert.Empty(t, text)

	assert.Equal(t, []map[string]any{{
		"level":      "INFO",
		"msg":        "m",
		"subcommand": "sweep",
		"n":          "v",
	}}, records())
}

// TestHostTokenSkipsWorkmuxWhenEnvSetToGuest pins the recursion
// guard. Even with WM_SANDBOX_GUEST=1 in process env, runHostToken
// reaches k8s directly because it does not own a *handler and
// therefore has no path to runHost. The fake kube.Client records
// exactly one CreateTokenRequest call.
func TestHostTokenSkipsWorkmuxWhenEnvSetToGuest(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv(guestEnvVar, "1")

	fake := &kubetest.Fake{
		Token:       "tok-abc",
		TokenExpiry: time.Date(2026, 5, 1, 13, 0, 0, 0, time.UTC),
	}

	prevClient := hostKubeClient
	hostKubeClient = func(string, string) (kube.Client, error) { return fake, nil }
	t.Cleanup(func() { hostKubeClient = prevClient })

	var buf strings.Builder

	prevStdout := hostStdout
	hostStdout = &buf
	t.Cleanup(func() { hostStdout = prevStdout })

	records := withHostLedger(t)
	addOwnedRecord(t, records, "kube-system", "claude-sa-x", false)

	err := runHostToken(t.Context(), testOwnedKubeconfig, testHostConfig(), append([]string{
		"--context", "prod",
		"--sa", "claude-sa-x",
		"--namespace", "kube-system",
	}, ownerFlags()...))
	require.NoError(t, err)

	fake.Lock()
	defer fake.Unlock()

	require.Len(t, fake.TokenRequests, 1, "host token must call k8s exactly once")
	assert.Equal(t, "kube-system/claude-sa-x", fake.TokenRequests[0])
	assert.Contains(t, buf.String(), `"token":"tok-abc"`)
}

// TestSelectArgsSameForEveryEnv pins that handler.selectCtx passes
// `host select` the same argv in the host env and in the guest env,
// whatever the --output path and the socket path of serve. The argv
// names only the context and the owner of the SA. `host select`
// writes no kubeconfig, and serve writes its own on its own machine,
// so no path crosses to the host.
func TestSelectArgsSameForEveryEnv(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		guest  string
		output string
		sock   string
	}{
		"host": {
			sock: "/tmp/mcp-kubectx-run/serve.0.host.sock",
		},
		"guest": {
			guest:  "1",
			output: "/home/user/k.yaml",
			sock:   "/tmp/mcp-kubectx-run/serve.2.guest.sock",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := &handler{
				pid:        4242,
				outputPath: tc.output,
				socketPath: tc.sock,
				envLookup:  constLookup(tc.guest),
				hostID:     testHostID,
				instanceID: testInstanceID,
			}

			assert.Equal(t, []string{
				"prod",
				"--sa-instance-id", testInstanceID,
				"--sa-host-id", testHostID,
			}, h.selectArgs("prod"))
		})
	}
}
