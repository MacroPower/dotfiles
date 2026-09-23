package msglint_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mvdan.cc/sh/v3/syntax"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/msglint"
)

func parse(t *testing.T, command string) *syntax.File {
	t.Helper()

	prog, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	require.NoError(t, err)

	return prog
}

func TestMessages(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	msgFile := filepath.Join(tmp, "msg.txt")
	require.NoError(t, os.WriteFile(msgFile, []byte("fix: from a file\n"), 0o644))

	cases := map[string]struct {
		command string
		want    []msglint.Message
	}{
		"-m with a double-quoted message": {
			command: `git commit -m "fix: retry the job"`,
			want:    []msglint.Message{{Kind: "commit message", Text: "fix: retry the job"}},
		},
		"-am cluster": {
			command: `git commit -am 'fix: retry the job'`,
			want:    []msglint.Message{{Kind: "commit message", Text: "fix: retry the job"}},
		},
		"-m glued to its value": {
			command: `git commit -mfix`,
			want:    []msglint.Message{{Kind: "commit message", Text: "fix"}},
		},
		"repeated -m joins paragraphs": {
			command: `git commit -m "fix: a" -m "The body."`,
			want:    []msglint.Message{{Kind: "commit message", Text: "fix: a\n\nThe body."}},
		},
		"--message= and --message forms": {
			command: `git commit --message="fix: a" --message "The body."`,
			want:    []msglint.Message{{Kind: "commit message", Text: "fix: a\n\nThe body."}},
		},
		"cat heredoc substitution": {
			command: "git commit -m \"$(cat <<'EOF'\nfeat: add thing\n\nThe body.\nEOF\n)\"",
			want:    []msglint.Message{{Kind: "commit message", Text: "feat: add thing\n\nThe body.\n"}},
		},
		"-F - with a heredoc on the command": {
			command: "git commit -F - <<'EOF'\nfeat: add thing\nEOF",
			want:    []msglint.Message{{Kind: "commit message", Text: "feat: add thing\n"}},
		},
		"-F reads an existing file": {
			command: "git commit -F " + msgFile,
			want:    []msglint.Message{{Kind: "commit message", Text: "fix: from a file\n"}},
		},
		"-F on a missing file is skipped": {
			command: "git commit -F /nonexistent/hook-router-test/msg.txt",
			want:    nil,
		},
		"git global options before commit": {
			command: `git -C /repo -c user.name=x commit -m "fix: a"`,
			want:    []msglint.Message{{Kind: "commit message", Text: "fix: a"}},
		},
		"parameter expansion is not readable": {
			command: `git commit -m "$MSG"`,
			want:    nil,
		},
		"unquoted heredoc with an expansion is not readable": {
			command: "git commit -m \"$(cat <<EOF\nfix: $thing\nEOF\n)\"",
			want:    nil,
		},
		"editor commit has no message": {
			command: `git commit`,
			want:    nil,
		},
		"amend without edit has no message": {
			command: `git commit --amend --no-edit`,
			want:    nil,
		},
		"author value is not a message": {
			command: `git commit --author "A <a@b>" -m "fix: a"`,
			want:    []msglint.Message{{Kind: "commit message", Text: "fix: a"}},
		},
		"other git subcommands are ignored": {
			command: `git log -m -1 && git status`,
			want:    nil,
		},
		"gh pr create title and body": {
			command: `gh pr create --title "feat: a" --body "The body."`,
			want:    []msglint.Message{{Kind: "pull request", Text: "feat: a\n\nThe body."}},
		},
		"gh pr create short flags and body-file heredoc": {
			command: "gh pr create -t 'feat: a' -F - <<'EOF'\nThe body.\nEOF",
			want:    []msglint.Message{{Kind: "pull request", Text: "feat: a\n\nThe body.\n"}},
		},
		"gh pr create --fill has no message": {
			command: `gh pr create --fill`,
			want:    nil,
		},
		"gh pr create value flags are skipped": {
			command: `gh pr create -B main -r someone --title "feat: a"`,
			want:    []msglint.Message{{Kind: "pull request", Text: "feat: a"}},
		},
		"commit and pr in one command": {
			command: `git add . && git commit -m "fix: a" && gh pr create -t "fix: a" -b "The body."`,
			want: []msglint.Message{
				{Kind: "commit message", Text: "fix: a"},
				{Kind: "pull request", Text: "fix: a\n\nThe body."},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, msglint.Messages(parse(t, tc.command)))
		})
	}
}

func TestParse(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in   string
		err  bool
		want msglint.Config
	}{
		"empty is disabled":      {in: "", want: msglint.Config{}},
		"command and timeout":    {in: `{"command":["prose-lint","--commit"],"timeout":"3s"}`, want: msglint.Config{Command: []string{"prose-lint", "--commit"}, Timeout: "3s"}},
		"malformed JSON errors":  {in: `{"command":`, err: true},
		"empty command disables": {in: `{"command":[]}`, want: msglint.Config{Command: []string{}}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := msglint.Parse(tc.in)
			if tc.err {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, len(tc.want.Command) == 0, got.Empty())
		})
	}
}

func TestLint(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		command []string
		timeout string
		want    []string
		err     bool
	}{
		"exit 0 is clean": {
			command: []string{"sh", "-c", `cat >/dev/null; exit 0`},
			want:    nil,
		},
		"exit 1 returns stdout lines": {
			command: []string{"sh", "-c", `cat >/dev/null; printf '<stdin>:1:1: bad\n\n<stdin>:3:1: worse\n'; exit 1`},
			want:    []string{"<stdin>:1:1: bad", "<stdin>:3:1: worse"},
		},
		"exit 1 without output is clean": {
			command: []string{"sh", "-c", `cat >/dev/null; exit 1`},
			want:    nil,
		},
		"stdin carries the text": {
			command: []string{"sh", "-c", `grep -q deliberate && { echo '<stdin>:1:1: certificate'; exit 1; }; exit 0`},
			want:    []string{"<stdin>:1:1: certificate"},
		},
		"exit 2 fails": {
			command: []string{"sh", "-c", `echo boom >&2; exit 2`},
			err:     true,
		},
		"missing binary fails": {
			command: []string{"/nonexistent/hook-router-test/linter"},
			err:     true,
		},
		"timeout fails": {
			command: []string{"sh", "-c", `sleep 5`},
			timeout: "150ms",
			err:     true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := msglint.Config{Command: tc.command, Timeout: tc.timeout}

			start := time.Now()
			got, err := cfg.Lint(t.Context(), "This is deliberate.\n")
			assert.Less(t, time.Since(start), 2*time.Second)

			if tc.err {
				require.ErrorIs(t, err, msglint.ErrLintFailed)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestLintDisabled(t *testing.T) {
	t.Parallel()

	got, err := msglint.Config{}.Lint(t.Context(), "anything")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestCheck(t *testing.T) {
	t.Parallel()

	flagDeliberate := msglint.Config{Command: []string{"sh", "-c", `grep -q deliberate && { echo '<stdin>:3:1: Style::ProseCertificate: certificate'; exit 1; }; exit 0`}}

	cases := map[string]struct {
		command    string
		cfg        msglint.Config
		wantDeny   bool
		wantReason string
		err        bool
	}{
		"dirty commit is denied with the findings": {
			command:    "git commit -m \"$(cat <<'EOF'\nfix: a\n\nThis is deliberate.\nEOF\n)\"",
			cfg:        flagDeliberate,
			wantDeny:   true,
			wantReason: "prose-lint: 1 finding in the commit message.",
		},
		"clean commit passes": {
			command: `git commit -m "fix: retry the job"`,
			cfg:     flagDeliberate,
		},
		"command without a message passes": {
			command: `git status`,
			cfg:     flagDeliberate,
		},
		"disabled config passes everything": {
			command: `git commit -m "This is deliberate."`,
			cfg:     msglint.Config{},
		},
		"dirty pull request names its kind": {
			command:    `gh pr create -t "feat: a" -b "This is deliberate."`,
			cfg:        flagDeliberate,
			wantDeny:   true,
			wantReason: "in the pull request.",
		},
		"linter crash returns the error": {
			command: `git commit -m "fix: a"`,
			cfg:     msglint.Config{Command: []string{"sh", "-c", `exit 2`}},
			err:     true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			reason, deny, err := msglint.Check(t.Context(), parse(t, tc.command), tc.cfg)
			if tc.err {
				require.ErrorIs(t, err, msglint.ErrLintFailed)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantDeny, deny)

			if tc.wantDeny {
				assert.Contains(t, reason, tc.wantReason)
				assert.Contains(t, reason, "<stdin>:3:1:")
			} else {
				assert.Empty(t, reason)
			}
		})
	}
}
