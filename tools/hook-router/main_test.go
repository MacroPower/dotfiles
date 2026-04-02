package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mvdan.cc/sh/v3/syntax"
)

func mustParse(t *testing.T, command string) *syntax.File {
	t.Helper()

	prog, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	require.NoError(t, err)

	return prog
}

func TestCheckGitStashDenied(t *testing.T) {
	t.Parallel()

	const stashDenied = "Do not use git stash to shelve changes. All issues in the working tree are your responsibility to fix, regardless of origin."

	tests := map[string]struct {
		input string
		want  string
	}{
		"bare git stash": {
			input: "git stash",
			want:  stashDenied,
		},
		"git stash push": {
			input: "git stash push",
			want:  stashDenied,
		},
		"git stash push with path": {
			input: "git stash push -- file.go",
			want:  stashDenied,
		},
		"git stash save": {
			input: `git stash save "wip"`,
			want:  stashDenied,
		},
		"git stash -k": {
			input: "git stash -k",
			want:  stashDenied,
		},
		"git stash --keep-index": {
			input: "git stash --keep-index",
			want:  stashDenied,
		},
		"git stash in pipeline": {
			input: "git stash || echo fail",
			want:  stashDenied,
		},
		"git stash in subshell": {
			input: "(git stash)",
			want:  stashDenied,
		},
		"no match: git stash pop": {
			input: "git stash pop",
		},
		"no match: git stash apply": {
			input: "git stash apply stash@{0}",
		},
		"no match: git stash list": {
			input: "git stash list",
		},
		"no match: git stash show": {
			input: "git stash show -p",
		},
		"no match: git stash drop": {
			input: "git stash drop stash@{1}",
		},
		"no match: git stash branch": {
			input: "git stash branch newbranch",
		},
		"no match: git stash clear": {
			input: "git stash clear",
		},
		"no match: git status": {
			input: "git status",
		},
		"no match: echo git stash": {
			input: "echo git stash",
		},
		"no match: sh -c git stash": {
			input: `sh -c "git stash"`,
		},
		"no match: git help stash": {
			input: "git help stash",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prog := mustParse(t, tt.input)
			got, denied := checkGitStashDenied(prog)
			assert.Equal(t, tt.want != "", denied)

			if denied {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestCheckK8sCliDenied(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  string
	}{
		"kubectl get pods": {
			input: "kubectl get pods",
			want:  "Direct kubectl usage is blocked. Use mcp__kubernetes__call_kubectl instead.",
		},
		"kubectl with namespace": {
			input: "kubectl -n kube-system get pods",
			want:  "Direct kubectl usage is blocked. Use mcp__kubernetes__call_kubectl instead.",
		},
		"kubectl in pipeline": {
			input: "kubectl get pods | grep foo",
			want:  "Direct kubectl usage is blocked. Use mcp__kubernetes__call_kubectl instead.",
		},
		"kubectl in compound": {
			input: "cd /tmp && kubectl apply -f manifest.yaml",
			want:  "Direct kubectl usage is blocked. Use mcp__kubernetes__call_kubectl instead.",
		},
		"no match: helm install": {
			input: "helm install my-release chart",
		},
		"no match: cilium status": {
			input: "cilium status",
		},
		"no match: hubble observe": {
			input: "hubble observe",
		},
		"no match: echo kubectl": {
			input: "echo kubectl get pods",
		},
		"no match: git status": {
			input: "git status",
		},
		"no match: kubecolor": {
			input: "kubecolor get pods",
		},
		"no match: helmfile": {
			input: "helmfile sync",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prog := mustParse(t, tt.input)
			got, denied := checkK8sCliDenied(prog)
			assert.Equal(t, tt.want != "", denied)

			if denied {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestCheckDockerCommand(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input          string
		wantFound      bool
		wantProxied    bool
		wantSubcommand string
		wantNetwork    string
	}{
		"docker run": {
			input:          "docker run -it ubuntu",
			wantFound:      true,
			wantSubcommand: "run",
		},
		"docker run with network host": {
			input:          "docker run --network host ubuntu",
			wantFound:      true,
			wantSubcommand: "run",
			wantNetwork:    "host",
		},
		"docker run with network=bridge": {
			input:          "docker run --network=bridge ubuntu",
			wantFound:      true,
			wantSubcommand: "run",
			wantNetwork:    "bridge",
		},
		"docker run with net none": {
			input:          "docker run --net none ubuntu",
			wantFound:      true,
			wantSubcommand: "run",
			wantNetwork:    "none",
		},
		"docker run with net=none": {
			input:          "docker run --net=none ubuntu",
			wantFound:      true,
			wantSubcommand: "run",
			wantNetwork:    "none",
		},
		"docker create": {
			input:          "docker create --name foo ubuntu",
			wantFound:      true,
			wantSubcommand: "create",
		},
		"docker compose up": {
			input:          "docker compose up -d",
			wantFound:      true,
			wantSubcommand: "compose",
		},
		"docker compose run": {
			input:          "docker compose run svc",
			wantFound:      true,
			wantSubcommand: "compose",
		},
		"docker build": {
			input:          "docker build .",
			wantFound:      true,
			wantSubcommand: "build",
		},
		"docker ps": {
			input:          "docker ps",
			wantFound:      true,
			wantSubcommand: "ps",
		},
		"docker in pipeline": {
			input:          "docker ps | grep foo",
			wantFound:      true,
			wantSubcommand: "ps",
		},
		"docker in compound": {
			input:          "cd /app && docker build .",
			wantFound:      true,
			wantSubcommand: "build",
		},
		"already proxied": {
			input:          "DOCKER_HOST=tcp://127.0.0.1:2375 docker ps",
			wantFound:      true,
			wantProxied:    true,
			wantSubcommand: "ps",
		},
		"no match: echo docker": {
			input: "echo docker run",
		},
		"no match: dockerize": {
			input: "dockerize -wait tcp://db:5432",
		},
		"no match: docker-compose v1": {
			input: "docker-compose up -d",
		},
		"no match: git status": {
			input: "git status",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prog := mustParse(t, tt.input)
			info := checkDockerCommand(prog)
			assert.Equal(t, tt.wantFound, info.found, "found")
			assert.Equal(t, tt.wantProxied, info.alreadyProxied, "proxied")
			assert.Equal(t, tt.wantSubcommand, info.subcommand, "subcommand")
			assert.Equal(t, tt.wantNetwork, info.networkFlag, "networkFlag")
		})
	}
}

func TestRun(t *testing.T) {
	t.Parallel()

	cfg := config{}

	makeInput := func(toolInput map[string]any) string {
		hook := map[string]any{"tool_input": toolInput}
		b, err := json.Marshal(hook)
		require.NoError(t, err)

		return string(b)
	}

	t.Run("non-matching command", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "git pull origin main",
		})

		var stdout bytes.Buffer

		err := run(strings.NewReader(input), &stdout, cfg, slog.New(slog.DiscardHandler))
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("invalid JSON", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		err := run(strings.NewReader("not json"), &stdout, cfg, slog.New(slog.DiscardHandler))
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("missing tool_input", func(t *testing.T) {
		t.Parallel()

		input, err := json.Marshal(map[string]any{"other": "field"})
		require.NoError(t, err)

		var stdout bytes.Buffer

		err = run(strings.NewReader(string(input)), &stdout, cfg, slog.New(slog.DiscardHandler))
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("missing command key", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"description": "no command here",
		})

		var stdout bytes.Buffer

		err := run(strings.NewReader(input), &stdout, cfg, slog.New(slog.DiscardHandler))
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("empty command string", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "",
		})

		var stdout bytes.Buffer

		err := run(strings.NewReader(input), &stdout, cfg, slog.New(slog.DiscardHandler))
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("denied git stash", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "git stash",
		})

		var stdout bytes.Buffer

		err := run(strings.NewReader(input), &stdout, cfg, slog.New(slog.DiscardHandler))
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "PreToolUse", hso["hookEventName"])
		assert.Equal(t, "deny", hso["permissionDecision"])
		assert.Contains(t, hso["permissionDecisionReason"], "git stash")
	})

	t.Run("denied kubectl", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "kubectl get pods -A",
		})

		var stdout bytes.Buffer

		err := run(strings.NewReader(input), &stdout, cfg, slog.New(slog.DiscardHandler))
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "PreToolUse", hso["hookEventName"])
		assert.Equal(t, "deny", hso["permissionDecision"])
		assert.Contains(t, hso["permissionDecisionReason"], "kubectl")
	})

	t.Run("docker command rewrite without ensure", func(t *testing.T) {
		t.Parallel()

		// No dockerProxyEnsure configured: docker commands pass through to delegate.
		input := makeInput(map[string]any{
			"command":     "docker ps",
			"description": "list containers",
		})

		var stdout bytes.Buffer

		err := run(strings.NewReader(input), &stdout, cfg)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes(), "should delegate, not rewrite")
	})

	t.Run("docker command rewrite with ensure", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command":     "docker ps -a",
			"description": "list all containers",
		})

		var stdout bytes.Buffer

		// Use "true" as a no-op ensure script.
		dockerCfg := config{dockerProxyEnsure: "true", dockerProxyPort: "2375"}

		err := run(strings.NewReader(input), &stdout, dockerCfg)
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "PreToolUse", hso["hookEventName"])

		updatedInput, ok := hso["updatedInput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "DOCKER_HOST=tcp://127.0.0.1:2375 docker ps -a", updatedInput["command"])
		assert.Equal(t, "list all containers", updatedInput["description"], "non-command fields should be preserved")
	})

	t.Run("docker run injects network none", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "docker run ubuntu echo hello",
		})

		var stdout bytes.Buffer

		dockerCfg := config{dockerProxyEnsure: "true", dockerProxyPort: "2375"}

		err := run(strings.NewReader(input), &stdout, dockerCfg)
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)

		updatedInput, ok := hso["updatedInput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "DOCKER_HOST=tcp://127.0.0.1:2375 docker run --network none ubuntu echo hello", updatedInput["command"])
	})

	t.Run("docker create injects network none", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "docker create ubuntu",
		})

		var stdout bytes.Buffer

		dockerCfg := config{dockerProxyEnsure: "true", dockerProxyPort: "2375"}

		err := run(strings.NewReader(input), &stdout, dockerCfg)
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)

		updatedInput, ok := hso["updatedInput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "DOCKER_HOST=tcp://127.0.0.1:2375 docker create --network none ubuntu", updatedInput["command"])
	})

	t.Run("docker run --network host denied", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "docker run --network host ubuntu",
		})

		var stdout bytes.Buffer

		dockerCfg := config{dockerProxyEnsure: "true", dockerProxyPort: "2375"}

		err := run(strings.NewReader(input), &stdout, dockerCfg)
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "deny", hso["permissionDecision"])
		assert.Contains(t, hso["permissionDecisionReason"], "--network none")
	})

	t.Run("docker run --network=host denied", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "docker run --network=host ubuntu",
		})

		var stdout bytes.Buffer

		dockerCfg := config{dockerProxyEnsure: "true", dockerProxyPort: "2375"}

		err := run(strings.NewReader(input), &stdout, dockerCfg)
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "deny", hso["permissionDecision"])
	})

	t.Run("docker run --net bridge denied", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "docker run --net bridge ubuntu",
		})

		var stdout bytes.Buffer

		dockerCfg := config{dockerProxyEnsure: "true", dockerProxyPort: "2375"}

		err := run(strings.NewReader(input), &stdout, dockerCfg)
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "deny", hso["permissionDecision"])
	})

	t.Run("docker run --network none passes through", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "docker run --network none ubuntu",
		})

		var stdout bytes.Buffer

		dockerCfg := config{dockerProxyEnsure: "true", dockerProxyPort: "2375"}

		err := run(strings.NewReader(input), &stdout, dockerCfg)
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)

		updatedInput, ok := hso["updatedInput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "DOCKER_HOST=tcp://127.0.0.1:2375 docker run --network none ubuntu", updatedInput["command"],
			"should not double-inject --network none")
	})

	t.Run("docker run --net=none passes through", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "docker run --net=none ubuntu",
		})

		var stdout bytes.Buffer

		dockerCfg := config{dockerProxyEnsure: "true", dockerProxyPort: "2375"}

		err := run(strings.NewReader(input), &stdout, dockerCfg)
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)

		updatedInput, ok := hso["updatedInput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "DOCKER_HOST=tcp://127.0.0.1:2375 docker run --net=none ubuntu", updatedInput["command"])
	})

	t.Run("docker build not network restricted", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "docker build .",
		})

		var stdout bytes.Buffer

		dockerCfg := config{dockerProxyEnsure: "true", dockerProxyPort: "2375"}

		err := run(strings.NewReader(input), &stdout, dockerCfg)
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)

		updatedInput, ok := hso["updatedInput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "DOCKER_HOST=tcp://127.0.0.1:2375 docker build .", updatedInput["command"],
			"build should not have --network none injected")
	})

	t.Run("docker run --network host denied without proxy", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "docker run --network host ubuntu",
		})

		var stdout bytes.Buffer

		// No dockerProxyEnsure configured.
		err := run(strings.NewReader(input), &stdout, cfg)
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "deny", hso["permissionDecision"],
			"network deny should fire even without proxy configured")
	})

	t.Run("already proxied docker command passes through", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "DOCKER_HOST=tcp://127.0.0.1:2375 docker ps",
		})

		var stdout bytes.Buffer

		dockerCfg := config{dockerProxyEnsure: "true", dockerProxyPort: "2375"}

		err := run(strings.NewReader(input), &stdout, dockerCfg)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes(), "already proxied should delegate, not rewrite")
	})

	t.Run("denied git stash with git clone", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "git stash && git clone URL dest",
		})

		var stdout bytes.Buffer

		err := run(strings.NewReader(input), &stdout, cfg, slog.New(slog.DiscardHandler))
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "deny", hso["permissionDecision"])
	})
}
