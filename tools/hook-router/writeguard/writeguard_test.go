package writeguard_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/writeguard"
)

// layout names the directories that [newLayout] creates.
type layout struct {
	dirs    writeguard.Dirs
	root    string
	parent  string
	outside string
	abs     string
}

// newLayout creates a home directory with a user settings file and a
// project two levels under it, plus directories outside the home
// directory.
func newLayout(t *testing.T) layout {
	t.Helper()

	root := t.TempDir()
	home := filepath.Join(root, "home")
	parent := filepath.Join(home, "repos")
	l := layout{
		dirs: writeguard.Dirs{
			Home:       home,
			ConfigDir:  filepath.Join(home, ".claude"),
			ProjectDir: filepath.Join(parent, "app"),
			TempDir:    filepath.Join(root, "tmp", "claude-1"),
		},
		root:    root,
		parent:  parent,
		outside: filepath.Join(root, "outside"),
		abs:     filepath.Join(root, "abs"),
	}

	for _, dir := range []string{
		l.dirs.ConfigDir,
		filepath.Join(l.dirs.ProjectDir, ".claude"),
		l.dirs.TempDir,
		l.outside,
		l.abs,
		filepath.Join(home, ".cache", "nix"),
		filepath.Join(root, "docs"),
	} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}

	writeFile(t, filepath.Join(l.dirs.ConfigDir, "settings.json"), `{
		"permissions": {"additionalDirectories": ["../../../docs"]},
		"sandbox": {"filesystem": {"allowWrite": [
			"~/.cache/nix",
			"~/repos/**",
			"scratch",
			"/`+l.abs+`"
		]}}
	}`)
	writeFile(t, filepath.Join(l.dirs.ProjectDir, ".claude", "settings.local.json"), `{
		"sandbox": {"filesystem": {"denyWrite": ["./build/cache"]}}
	}`)

	require.NoError(t, os.Symlink(l.outside, filepath.Join(l.dirs.ProjectDir, "link-out")))
	require.NoError(t, os.Symlink(l.dirs.ProjectDir, filepath.Join(home, "alias")))

	return l
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestPolicyCheck(t *testing.T) {
	t.Parallel()

	l := newLayout(t)
	home := l.dirs.Home
	project := l.dirs.ProjectDir
	config := l.dirs.ConfigDir

	policy, err := writeguard.Load(l.dirs)
	require.NoError(t, err)

	tcs := map[string]struct {
		path    string
		refused bool
		reason  string
	}{
		"project file": {
			path: filepath.Join(project, "main.go"),
		},
		"new file in new project directories": {
			path: filepath.Join(project, "a", "b", "c.go"),
		},
		"temp directory": {
			path: filepath.Join(l.dirs.TempDir, "scratch.txt"),
		},
		"tilde allowWrite entry": {
			path: filepath.Join(home, ".cache", "nix", "x"),
		},
		"allowWrite entry with trailing glob": {
			path: filepath.Join(l.parent, "other", "x"),
		},
		"relative user entry resolves under the config directory": {
			path: filepath.Join(config, "scratch", "x"),
		},
		"double-slash absolute entry": {
			path: filepath.Join(l.abs, "x"),
		},
		"additional directory resolves under the project": {
			path: filepath.Join(l.root, "docs", "x"),
		},
		"plan file": {
			path: filepath.Join(config, "plans", "plan.md"),
		},
		"unprotected file under project .claude": {
			path: filepath.Join(project, ".claude", "notes.md"),
		},
		"startup file above the project": {
			path: filepath.Join(l.parent, ".bashrc"),
		},
		"symlink into the project": {
			path: filepath.Join(home, "alias", "x"),
		},
		"outside every write path": {
			path:    filepath.Join(l.outside, "x"),
			refused: true,
			reason:  "outside every sandbox write path",
		},
		"home startup file": {
			path:    filepath.Join(home, ".zshrc"),
			refused: true,
			reason:  "outside every sandbox write path",
		},
		"symlink out of the project": {
			path:    filepath.Join(project, "link-out", "x"),
			refused: true,
			reason:  "outside every sandbox write path",
		},
		"relative denyWrite entry": {
			path:    filepath.Join(project, "build", "cache", "x"),
			refused: true,
			reason:  `denyWrite entry "./build/cache"`,
		},
		"project settings file": {
			path:    filepath.Join(project, ".claude", "settings.json"),
			refused: true,
			reason:  "protected path",
		},
		"project skill": {
			path:    filepath.Join(project, ".claude", "skills", "s", "SKILL.md"),
			refused: true,
			reason:  "protected path",
		},
		"project git hook": {
			path:    filepath.Join(project, ".git", "hooks", "pre-commit"),
			refused: true,
			reason:  "protected path",
		},
		"project startup file": {
			path:    filepath.Join(project, ".bashrc"),
			refused: true,
			reason:  "protected path",
		},
		"settings file above the project": {
			path:    filepath.Join(l.parent, ".claude", "settings.local.json"),
			refused: true,
			reason:  "protected path",
		},
		"mcp config above the project": {
			path:    filepath.Join(l.parent, ".mcp.json"),
			refused: true,
			reason:  "protected path",
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			reason, refused := policy.Check(tc.path)
			assert.Equal(t, tc.refused, refused, reason)

			if !tc.refused {
				assert.Empty(t, reason)
				return
			}

			assert.Contains(t, reason, tc.path)
			assert.Contains(t, reason, tc.reason)
		})
	}
}

func TestLoadMalformedSettings(t *testing.T) {
	t.Parallel()

	l := newLayout(t)
	writeFile(t, filepath.Join(l.dirs.ProjectDir, ".claude", "settings.json"), `{"sandbox":`)

	_, err := writeguard.Load(l.dirs)
	require.ErrorIs(t, err, writeguard.ErrSettings)
}

func TestDirsFromEnv(t *testing.T) {
	t.Parallel()

	uid := strconv.Itoa(os.Getuid())

	tcs := map[string]struct {
		env  map[string]string
		cwd  string
		want writeguard.Dirs
		err  error
	}{
		"defaults": {
			env: map[string]string{"HOME": "/h"},
			cwd: "/p",
			want: writeguard.Dirs{
				Home:       "/h",
				ConfigDir:  "/h/.claude",
				ProjectDir: "/p",
				TempDir:    "/tmp/claude-" + uid,
			},
		},
		"overrides": {
			env: map[string]string{
				"HOME":               "/h",
				"CLAUDE_PROJECT_DIR": "/proj",
				"CLAUDE_CONFIG_DIR":  "/cfg",
				"CLAUDE_CODE_TMPDIR": "/scratch",
			},
			cwd: "/p/sub",
			want: writeguard.Dirs{
				Home:       "/h",
				ConfigDir:  "/cfg",
				ProjectDir: "/proj",
				TempDir:    "/scratch/claude-" + uid,
			},
		},
		"no home": {
			env: map[string]string{},
			cwd: "/p",
			err: writeguard.ErrNoHome,
		},
		"no project": {
			env: map[string]string{"HOME": "/h"},
			err: writeguard.ErrNoProjectDir,
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := writeguard.DirsFromEnv(func(key string) string { return tc.env[key] }, tc.cwd)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
