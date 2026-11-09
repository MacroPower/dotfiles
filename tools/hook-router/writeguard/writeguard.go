package writeguard

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var (
	// ErrNoProjectDir reports that neither CLAUDE_PROJECT_DIR nor the
	// hook payload names a project directory.
	ErrNoProjectDir = errors.New("no project directory")

	// ErrNoHome reports that HOME is unset.
	ErrNoHome = errors.New("HOME is unset")

	// ErrSettings reports a settings file that exists but does not
	// decode.
	ErrSettings = errors.New("unreadable settings file")

	// protectedTree lists the protected paths that the sandbox denies
	// in the project directory and in every directory above it,
	// relative to each directory.
	protectedTree = []string{
		".claude/settings.json",
		".claude/settings.local.json",
		".claude/skills",
		".claude/agents",
		".claude/commands",
		".claude/hooks",
		".claude/workflows",
		".claude/scheduled_tasks.json",
		".mcp.json",
	}

	// protectedProject lists the protected paths that the sandbox
	// denies in the project directory alone, relative to it.
	protectedProject = []string{
		".bashrc",
		".bash_profile",
		".bash_login",
		".bash_logout",
		".profile",
		".zshrc",
		".zprofile",
		".zshenv",
		".zlogin",
		".zlogout",
		".gitconfig",
		".vscode",
		".idea",
		".git/hooks",
		".git/config",
	}
)

// Dirs names the directories that a [Policy] resolves settings paths
// against.
type Dirs struct {
	// Home is the user's home directory. Entries that start with `~`
	// resolve under it.
	Home string

	// ConfigDir is Claude Code's configuration directory. It holds the
	// user settings file and the plans directory, and relative entries
	// in the user settings file resolve under it.
	ConfigDir string

	// ProjectDir is the session's project directory. Commands can write
	// under it, and relative entries in the project and local settings
	// files resolve under it.
	ProjectDir string

	// TempDir is the per-user temp directory that commands can write.
	TempDir string
}

// DirsFromEnv returns the [Dirs] for a hook process that Claude Code
// started, reading variables through getenv. CLAUDE_PROJECT_DIR names
// the project directory, and cwd stands in when it is unset.
// CLAUDE_CONFIG_DIR replaces the ~/.claude default, and
// CLAUDE_CODE_TMPDIR replaces /tmp as the parent of the claude-<uid>
// temp directory.
func DirsFromEnv(getenv func(string) string, cwd string) (Dirs, error) {
	home := getenv("HOME")
	if home == "" {
		return Dirs{}, ErrNoHome
	}

	project := getenv("CLAUDE_PROJECT_DIR")
	if project == "" {
		project = cwd
	}

	if project == "" {
		return Dirs{}, ErrNoProjectDir
	}

	config := getenv("CLAUDE_CONFIG_DIR")
	if config == "" {
		config = filepath.Join(home, ".claude")
	}

	tmp := getenv("CLAUDE_CODE_TMPDIR")
	if tmp == "" {
		tmp = "/tmp"
	}

	return Dirs{
		Home:       home,
		ConfigDir:  config,
		ProjectDir: project,
		TempDir:    filepath.Join(tmp, "claude-"+strconv.Itoa(os.Getuid())),
	}, nil
}

// Policy holds the sandbox write boundary that [Load] rebuilds from the
// settings files. Every path it holds has its symlinks resolved.
type Policy struct {
	allow  []string
	deny   []rule
	exempt []string
}

// rule is a path that [Policy.Check] refuses, with the sentence that
// names why.
type rule struct {
	path   string
	reason string
}

// settings holds the fields of a Claude Code settings file that change
// the sandbox write boundary.
type settings struct {
	Permissions struct {
		AdditionalDirectories []string `json:"additionalDirectories"`
	} `json:"permissions"`
	Sandbox struct {
		Filesystem struct {
			AllowWrite []string `json:"allowWrite"`
			DenyWrite  []string `json:"denyWrite"`
		} `json:"filesystem"`
	} `json:"sandbox"`
}

// Load builds the [Policy] for dirs from the user, project, and local
// settings files. A missing settings file adds nothing. A settings file
// that exists but does not decode returns an error that wraps
// [ErrSettings].
func Load(dirs Dirs) (*Policy, error) {
	p := &Policy{
		allow:  []string{resolve(dirs.ProjectDir), resolve(dirs.TempDir)},
		exempt: []string{resolve(filepath.Join(dirs.ConfigDir, "plans"))},
	}

	layers := []struct {
		file string
		base string
	}{
		{file: filepath.Join(dirs.ConfigDir, "settings.json"), base: dirs.ConfigDir},
		{file: filepath.Join(dirs.ProjectDir, ".claude", "settings.json"), base: dirs.ProjectDir},
		{file: filepath.Join(dirs.ProjectDir, ".claude", "settings.local.json"), base: dirs.ProjectDir},
	}

	for _, layer := range layers {
		s, err := readSettings(layer.file)
		if err != nil {
			return nil, err
		}

		for _, entry := range s.Permissions.AdditionalDirectories {
			if entry == "" {
				continue
			}

			p.allow = append(p.allow, resolve(entryPath(dirs.Home, dirs.ProjectDir, entry)))
		}

		for _, entry := range s.Sandbox.Filesystem.AllowWrite {
			if entry == "" {
				continue
			}

			p.allow = append(p.allow, resolve(entryPath(dirs.Home, layer.base, entry)))
		}

		for _, entry := range s.Sandbox.Filesystem.DenyWrite {
			if entry == "" {
				continue
			}

			p.deny = append(p.deny, rule{
				path:   resolve(entryPath(dirs.Home, layer.base, entry)),
				reason: fmt.Sprintf("the sandbox denyWrite entry %q covers it", entry),
			})
		}
	}

	for dir := filepath.Clean(dirs.ProjectDir); ; dir = filepath.Dir(dir) {
		p.deny = append(p.deny, protected(dir, protectedTree)...)

		if filepath.Dir(dir) == dir {
			break
		}
	}

	p.deny = append(p.deny, protected(dirs.ProjectDir, protectedProject)...)

	return p, nil
}

// Check reports whether the sandbox refuses a command's write to the
// absolute path, and returns a sentence that names why. A path under
// the plans directory always passes.
func (p *Policy) Check(path string) (string, bool) {
	target := resolve(path)

	for _, dir := range p.exempt {
		if within(target, dir) {
			return "", false
		}
	}

	for _, r := range p.deny {
		if within(target, r.path) {
			return fmt.Sprintf("The sandbox does not let commands write %s, because %s.", path, r.reason), true
		}
	}

	for _, dir := range p.allow {
		if within(target, dir) {
			return "", false
		}
	}

	return fmt.Sprintf("The sandbox does not let commands write %s, because it is outside every sandbox write path.", path), true
}

// protected returns a [rule] for each of names under dir.
func protected(dir string, names []string) []rule {
	rules := make([]rule, 0, len(names))

	for _, name := range names {
		rules = append(rules, rule{
			path:   resolve(filepath.Join(dir, name)),
			reason: "it is a protected path",
		})
	}

	return rules
}

// readSettings decodes the settings file at path. A missing file
// decodes to an empty [settings].
func readSettings(path string) (settings, error) {
	var s settings

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}

	if err != nil {
		return s, fmt.Errorf("%w: %w", ErrSettings, err)
	}

	err = json.Unmarshal(data, &s)
	if err != nil {
		return s, fmt.Errorf("%w: %s: %w", ErrSettings, path, err)
	}

	return s, nil
}

// entryPath resolves a settings path entry the way Claude Code resolves
// sandbox paths. A `~` entry resolves under home, an absolute entry
// (including the `//` form) stands as written, and a relative entry
// resolves under base. A trailing `/**` covers the same directory as
// the bare path.
func entryPath(home, base, entry string) string {
	entry = strings.TrimSuffix(entry, "/**")

	switch {
	case entry == "~":
		return home
	case strings.HasPrefix(entry, "~/"):
		return filepath.Join(home, entry[2:])
	case filepath.IsAbs(entry):
		return filepath.Clean(entry)
	default:
		return filepath.Join(base, entry)
	}
}

// resolve returns path with every symlink in its longest existing
// prefix resolved. The components that do not exist yet stay as
// written, so a file that a write is about to create resolves through
// its parent directories.
func resolve(path string) string {
	path = filepath.Clean(path)

	var missing []string

	for dir := path; ; {
		resolved, err := filepath.EvalSymlinks(dir)
		if err == nil {
			return filepath.Join(append([]string{resolved}, missing...)...)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return path
		}

		missing = append([]string{filepath.Base(dir)}, missing...)
		dir = parent
	}
}

// within reports whether path is root or sits under it.
func within(path, root string) bool {
	if path == root {
		return true
	}

	sep := string(filepath.Separator)

	return strings.HasPrefix(path, strings.TrimSuffix(root, sep)+sep)
}
