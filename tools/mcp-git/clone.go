package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	// ErrMissingURL is returned when the URL field is empty.
	ErrMissingURL = errors.New("url is required")

	// ErrMissingDest is returned when the dest field is empty.
	ErrMissingDest = errors.New("dest is required")

	// ErrDeniedURL is returned when the URL scheme is not
	// allowed.
	ErrDeniedURL = errors.New("url scheme not allowed")

	// ErrDeniedBranch is returned when the branch name starts
	// with a dash.
	ErrDeniedBranch = errors.New("branch must not start with '-'")

	// ErrDeniedRef is returned when the ref name starts
	// with a dash.
	ErrDeniedRef = errors.New("ref must not start with '-'")

	// ErrRefConflict is returned when both branch and ref
	// are set.
	ErrRefConflict = errors.New("branch and ref are mutually exclusive")

	// ErrDeniedSparsePath is returned when a sparse checkout
	// path is invalid.
	ErrDeniedSparsePath = errors.New("sparse path is invalid")

	// ErrDeniedDestPrefix is returned when the dest path starts
	// with a dash.
	ErrDeniedDestPrefix = errors.New("dest must not start with '-'")

	// ErrOriginMismatch is returned when the existing repo's
	// origin URL does not match the requested URL.
	ErrOriginMismatch = errors.New("origin URL mismatch")

	// ErrNoOrigin is returned when the existing repo at dest has no
	// origin remote.
	ErrNoOrigin = errors.New("dest holds a repository that has no origin remote")

	// ErrNoOriginURL is returned when the origin remote of the existing
	// repo at dest has no URL.
	ErrNoOriginURL = errors.New("dest holds a repository whose origin remote has no URL")

	// ErrDeniedOriginURL is returned when the origin remote of the
	// existing repo at dest has a URL that [handler.checkURL] refuses.
	ErrDeniedOriginURL = errors.New("dest holds a repository whose origin remote has a URL that git_clone refuses")

	// ErrUnmatchedOrigin is returned when no url input that
	// [handler.checkURL] accepts matches the origin remote of the
	// existing repo at dest.
	ErrUnmatchedOrigin = errors.New(
		"dest holds a repository whose origin remote matches no url input that git_clone accepts",
	)

	// ErrCorruptDest is returned when dest holds a .git directory that
	// [checkGitDir] reports as corrupt and the handler cannot move dest
	// aside.
	ErrCorruptDest = errors.New("dest holds a corrupt git directory")

	// ErrClone wraps the underlying git clone failure.
	ErrClone = errors.New("git clone failed")

	// scpPattern matches SCP-style git URLs (e.g., git@github.com:org/repo).
	scpPattern = regexp.MustCompile(`^\w+@[\w.-]+:`)

	// safeSchemes lists the URL prefixes accepted by [handler.checkURL].
	safeSchemes = []string{
		"https://",
		"ssh://",
	}

	// insecureSchemes lists unencrypted URL prefixes that are only
	// accepted when allowInsecure is set.
	insecureSchemes = []string{
		"http://",
		"git://",
	}
)

// CloneInput is the JSON input schema for the git_clone tool.
type CloneInput struct {
	URL            string   `json:"url"                      jsonschema:"Repository URL to clone"`
	Dest           string   `json:"dest"                     jsonschema:"Destination directory path"`
	Branch         string   `json:"branch,omitzero"          jsonschema:"Branch to clone"`
	Ref            string   `json:"ref,omitzero"             jsonschema:"Branch or tag to check out (alias for branch)"`
	SparsePaths    []string `json:"sparse_paths,omitzero"    jsonschema:"Paths for sparse checkout (implies sparse)"`
	Depth          int      `json:"depth,omitzero"           jsonschema:"Shallow clone depth"`
	TimeoutSeconds int      `json:"timeout_seconds,omitzero" jsonschema:"Override the default per-operation timeout in seconds; 0 means use the server default"`
	SingleBranch   bool     `json:"single_branch,omitzero"   jsonschema:"Clone only the specified branch"`
	Sparse         bool     `json:"sparse,omitzero"          jsonschema:"Enable sparse checkout (only root files unless sparse_paths is set)"`
}

func (h *handler) handleClone(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input CloneInput,
) (*mcp.CallToolResult, any, error) {
	if input.URL == "" {
		return toolError(ErrMissingURL), nil, nil
	}

	if input.Dest == "" {
		return toolError(ErrMissingDest), nil, nil
	}

	if strings.HasPrefix(input.Dest, "-") {
		return toolError(ErrDeniedDestPrefix), nil, nil
	}

	// Each later step reads the absolute path. Every spelling of one
	// destination, such as a path with a trailing slash, therefore
	// names the same parent directory and the same lock file.
	//
	// [handler.checkDest] checks the absolute path, and git must write
	// that path. The OS resolves a symbolic link before the ".."
	// segment that follows it, so a path with such a segment can name
	// another directory for git. The OS also resolves a relative path
	// against the physical working directory. [os.Getwd] returns $PWD
	// when $PWD names that directory, and $PWD can pass through a
	// symbolic link.
	dest, absErr := filepath.Abs(input.Dest)
	if absErr != nil {
		return nil, nil, fmt.Errorf("resolving dest path: %w", absErr)
	}

	input.Dest = dest

	urlErr := h.checkURL(input.URL)
	if urlErr != nil {
		return toolError(urlErr), nil, nil
	}

	if input.Branch != "" && strings.HasPrefix(input.Branch, "-") {
		return toolError(ErrDeniedBranch), nil, nil
	}

	if input.Ref != "" && strings.HasPrefix(input.Ref, "-") {
		return toolError(ErrDeniedRef), nil, nil
	}

	if input.Ref != "" && input.Branch != "" {
		return toolError(ErrRefConflict), nil, nil
	}

	if input.Ref != "" {
		input.Branch = input.Ref
	}

	sparseErr := checkSparsePaths(input.SparsePaths)
	if sparseErr != nil {
		return toolError(sparseErr), nil, nil
	}

	destErr := h.checkDest(input.Dest)
	if destErr != nil {
		return toolError(destErr), nil, nil
	}

	if d := h.effectiveTimeout(input.TimeoutSeconds); d > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}

	err := os.MkdirAll(filepath.Dir(input.Dest), 0o755) //nolint:gosec // G301: dest is user-provided input.
	if err != nil {
		return nil, nil, fmt.Errorf("creating parent directory: %w", err)
	}

	cleanup, err := acquireLock(input.Dest)
	if err != nil {
		return nil, nil, err
	}
	defer cleanup()

	gitDir := filepath.Join(input.Dest, ".git")

	info, statErr := os.Stat(gitDir)
	if statErr != nil || !info.IsDir() {
		return h.clone(ctx, input, "")
	}

	repoErr := checkGitDir(ctx, gitDir)
	if repoErr == nil {
		return h.pullExisting(ctx, input)
	}

	// Only a corrupt .git directory moves the checkout aside. The
	// check can also fail when the checkout is healthy, such as when
	// the server cannot start git.
	if !errors.Is(repoErr, ErrNotRepo) {
		return toolError(repoErr), nil, nil
	}

	aside, asideErr := h.moveAside(input.Dest)
	if asideErr != nil {
		return toolError(asideErr), nil, nil
	}

	return h.clone(ctx, input, aside)
}

// checkGitDir checks gitDir, the .git directory of a destination. It
// fails with [ErrNotRepo] when the directory is corrupt, and the
// caller then moves the destination aside. A directory is corrupt in
// these cases:
//
//   - git reports that it is not a git repository.
//   - git cannot parse a config file of the directory, or a value of
//     that file.
//   - The config file names an extension that git does not know.
//   - The directory holds no config file.
//
// The config files of the directory are config, and config.worktree
// when extensions.worktreeConfig is set. A sparse clone sets that
// extension.
//
// A cleanup job such as systemd-tmpfiles on /tmp deletes old files one
// at a time. It can remove HEAD, config, or both, and keep the rest of
// the directory. git reports that a directory without HEAD is not a
// git repository. git opens a git directory that has HEAD and no
// config file. git clone writes the origin remote to the config file,
// so a checkout that lost the file has no origin to pull from.
//
// A crash can leave a config file cut short or full of NUL bytes. git
// then cannot parse a line of the file, or a value such as the number
// of core.repositoryformatversion or the boolean of core.filemode. The
// clone that replaces the destination does not read the config files
// of gitDir. git reads the lines of the system and global config files
// before those files, and it stops at the first line that it cannot
// parse. git therefore names a config file of gitDir only when the
// clone can parse the lines of the other two. The clone still fails
// when git cannot parse a value of those two files.
//
// A cut inside the name of an extension, such as worktreeConfig,
// leaves the name of an extension that git does not know. git reads
// the extensions only from the config file of gitDir, so the clone
// does not meet that name. checkGitDir therefore also reports a
// repository that a later git made with a new extension as corrupt.
//
// The --git-dir flag stops git from walking up to a repository that
// encloses the destination. checkGitDir runs git with messageLocale,
// because it reads the message of git.
//
// git can also fail for a reason that does not show a corrupt
// directory. For example, the server cannot start git, a signal kills
// git, or git cannot parse another config file than those of gitDir.
// Examples of such files are the global config and a file that an
// include directive names. The checkout can then be healthy, so the
// error is not [ErrNotRepo]. [repoOpenError] describes that error.
func checkGitDir(ctx context.Context, gitDir string) error {
	args := []string{"--git-dir", gitDir, "rev-parse", "--git-dir"}

	_, message, err := gitOutput(ctx, append(gitEnv(), messageLocale), args...)
	if err != nil {
		corrupt := badConfigFile(message, gitDir) ||
			messageLine(message, unknownExtensionMessage) != "" ||
			badRepoBoolean(ctx, message, gitDir)
		if corrupt {
			return fmt.Errorf("%w: %s: %s", ErrNotRepo, gitDir, truncateOutput(message))
		}

		return repoOpenError(gitDir, message, err)
	}

	_, err = os.Stat(filepath.Join(gitDir, "config"))
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrNotRepo, gitDir, err)
	}

	return nil
}

// badConfigFile reports whether message says that git cannot parse a
// line of a config file of the git directory at gitDir, or a value of
// that file other than a boolean. git printed message in the C locale.
// The config files are config and config.worktree. git names each file
// in the form that its command line gives for gitDir.
//
// git names the file when it cannot parse a line or a number. It also
// names the file when it cannot use a value, such as a key without a
// value. The message for such a value puts the path in quotes.
func badConfigFile(message, gitDir string) bool {
	for line := range strings.Lines(message) {
		line = strings.TrimSuffix(line, "\n")

		for _, name := range []string{"config", "config.worktree"} {
			path := filepath.Join(gitDir, name)

			badLine := strings.HasPrefix(line, badConfigMessage) && strings.HasSuffix(line, " in file "+path)
			badNumber := strings.HasPrefix(line, badNumberMessage) && strings.Contains(line, " in file "+path+": ")
			badVariable := strings.HasPrefix(line, badVariableMessage) &&
				strings.Contains(line, " in file '"+path+"' at line ")

			if badLine || badNumber || badVariable {
				return true
			}
		}
	}

	return false
}

// badRepoBoolean reports whether message says that git cannot parse a
// boolean of a config file of the git directory at gitDir. git printed
// message in the C locale. The message names the key and the value,
// but not the file that holds the value. The value can also come from
// the system or global config, from the environment, or from a file
// that an include directive names.
//
// badRepoBoolean therefore asks git for the booleans of that key in
// each config file of gitDir alone. The --local option names config.
// The --worktree option names config.worktree when
// extensions.worktreeConfig is set, and config otherwise. The
// --no-includes option keeps out the files that the config file
// includes. The environment of git keeps out the system config, the
// global config, and the config from the environment. The value is in
// a config file of gitDir when git prints the same line again.
func badRepoBoolean(ctx context.Context, message, gitDir string) bool {
	line := messageLine(message, badBooleanMessage)

	// The line ends with the value and the key, each in quotes.
	rest, ok := strings.CutSuffix(line, "'")
	at := strings.LastIndex(rest, "' for '")

	if !ok || at < 0 {
		return false
	}

	key := rest[at+len("' for '"):]

	env := append(gitEnv(),
		messageLocale,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_COUNT=0",
		"GIT_CONFIG_PARAMETERS=",
	)

	for _, file := range []string{"--local", "--worktree"} {
		_, again, err := gitOutput(ctx, env,
			"--git-dir", gitDir, "config", file, "--no-includes", "--type=bool", "--get-all", key,
		)
		if err != nil && messageLine(again, badBooleanMessage) == line {
			return true
		}
	}

	return false
}

// messageLine returns the first line of message that starts with
// prefix, or "" when no line does.
func messageLine(message, prefix string) string {
	for line := range strings.Lines(message) {
		line = strings.TrimSuffix(line, "\n")

		if strings.HasPrefix(line, prefix) {
			return line
		}
	}

	return ""
}

// moveAside renames a corrupt checkout at dest to a timestamped
// sibling path and returns that path, so a clone can take dest. It
// renames the checkout and deletes nothing, because the working tree
// can hold files that exist nowhere else.
func (h *handler) moveAside(dest string) (string, error) {
	aside := fmt.Sprintf("%s.corrupt-%s", dest, time.Now().UTC().Format("20060102T150405.000000000Z"))

	destErr := h.checkDest(aside)
	if destErr != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrCorruptDest, dest, destErr)
	}

	err := os.Rename(dest, aside)
	if err != nil {
		return "", fmt.Errorf("%w: %s: delete it and retry: %w", ErrCorruptDest, dest, err)
	}

	return aside, nil
}

// checkURL verifies that url uses a permitted scheme. Accepted
// forms are https, ssh, and SCP-style (user@host:path).
// Unencrypted schemes (http, git) require allowInsecure. File
// URLs and local paths are rejected unless allowFileURLs is set.
func (h *handler) checkURL(url string) error {
	if h.allowFileURLs {
		return nil
	}

	for _, scheme := range safeSchemes {
		if strings.HasPrefix(url, scheme) {
			return nil
		}
	}

	if h.allowInsecure {
		for _, scheme := range insecureSchemes {
			if strings.HasPrefix(url, scheme) {
				return nil
			}
		}
	}

	if scpPattern.MatchString(url) {
		return nil
	}

	return fmt.Errorf("%w: %s", ErrDeniedURL, url)
}

//nolint:unparam // signature matches mcp.AddTool handler contract.
func (h *handler) pullExisting(ctx context.Context, input CloneInput) (*mcp.CallToolResult, any, error) {
	originErr := h.checkOrigin(ctx, input.URL, input.Dest)
	if originErr != nil {
		return toolError(originErr), nil, nil
	}

	// runRemote takes the git-directory lock that the
	// remote-operation tools use, nested under the dest lock
	// handleClone already holds; the ordering is fixed, so it
	// cannot deadlock.
	out, err := h.runRemote(ctx, remoteOp{
		repo:     input.Dest,
		remote:   defaultRemote,
		worktree: true,
		timeout:  input.TimeoutSeconds,
		args:     buildPullArgs(defaultRemote, PullInput{Repo: input.Dest}),
	})
	if err != nil {
		return toolError(fmt.Errorf("pulling latest changes in %s: %w", input.Dest, err)), nil, nil
	}

	return remoteResult(
		fmt.Sprintf("Pulled latest changes in %s", input.Dest),
		out,
	), nil, nil
}

// checkOrigin verifies that url is the URL of the origin remote of the
// existing repo at dest. A repo that has no origin remote fails with
// [ErrNoOrigin], as [remoteMissing] decides. When git cannot read the
// origin remote for another reason, such as a remote.origin.url key
// without a value, the error says that checkOrigin could not read the
// remote. A repo whose origin remote has no URL fails with
// [ErrNoOriginURL], as [remoteLacksURL] decides. A repo whose origin
// remote has a URL that [handler.checkURL] refuses fails with
// [ErrDeniedOriginURL]. A repo whose origin remote matches no url that
// checkURL accepts fails with [ErrUnmatchedOrigin]. None of these
// errors depends on url, and [unusableDest] gives each of them the
// same advice.
//
// checkOrigin passes to checkURL the URL that git reads for origin,
// after the url.<base>.insteadOf rewrites of the git config. When
// checkURL refuses that URL, no url makes git_clone pull the repo.
// [handler.handleClone] refuses the URL itself as url. A rewrite can
// change another url to that URL, and [handler.runRemote] then refuses
// to pull from origin. The [ErrDeniedOriginURL] error therefore does
// not name the URL.
//
// git clone stores url as the URL of origin. git applies the
// url.<base>.insteadOf rewrites of the git config each time it reads
// the URL of a remote. The URL that it reads for origin can therefore
// differ from url, and checkOrigin applies the rewrites to url as
// well, with [rewriteURL]. It reads both URLs in dest, so the same
// config files rewrite each URL. It strips a trailing ".git" suffix
// from each URL after the rewrites, and then it compares the two URLs.
//
// An [ErrOriginMismatch] error names the URL of [handler.storedOrigin]
// for origin, and it names url. It names both URLs before the
// rewrites. git_clone accepts the first URL as url, and a caller that
// passes it matches the checkout. The URL that git reads for origin
// does not always match the checkout. git applies the rewrites to that
// URL again when a caller passes it as url. A rewrite whose base
// starts with the prefix that the rewrite replaces changes its own
// output.
//
// The error names each of the two without a trailing ".git" suffix. It
// keeps the suffix of the URL of storedOrigin when the URL without it
// does not match the checkout, as [matchingForm] decides. It keeps the
// suffix of url when the URL without it equals the first URL that the
// error names. The two URLs therefore differ before [stripCredential]
// removes a credential from each.
//
// When storedOrigin returns no URL, the error names for origin the url
// input that [handler.matchingInput] returns, as matchingInput returns
// it. That input matches the checkout as well, so it differs from url.
// For example, storedOrigin returns no URL when [handler.checkURL]
// refuses the URL that the git config stores for origin, such as the
// alias gh:org/repo of a rewrite whose insteadOf value is gh:. That
// rewrite gives a URL that checkURL accepts, and matchingInput returns
// that URL when the URL matches the checkout.
//
// When matchingInput returns no input either, no url makes git_clone
// pull the repo, and checkOrigin fails with [ErrUnmatchedOrigin]. For
// example, the git config can hold a second rewrite that changes the
// URL that the first rewrite gives for the alias. git applies the
// first rewrite when it reads origin. It applies the second rewrite
// when a caller passes that URL as url, so that url names another URL
// than the URL of origin.
//
// The tool result holds the error, so the error names each URL as
// [stripCredential] returns it. The git config of dest can store a
// user and a password in the URL of origin. The base of a rewrite can
// add both to a URL. When the two URLs differ only in a part that
// stripCredential removes, the error names two equal URLs. It then
// says that a user or a password differs.
func (h *handler) checkOrigin(ctx context.Context, url, dest string) error {
	origin, err := gitValue(ctx, "-C", dest, "remote", "get-url", "origin")
	if err != nil {
		if errors.Is(err, ErrTimeout) {
			return err
		}

		if remoteMissing(err) {
			return unusableDest(ErrNoOrigin, dest)
		}

		return fmt.Errorf("reading the origin remote of %s: %w", dest, err)
	}

	lacksURL, err := remoteLacksURL(ctx, dest, defaultRemote, origin, false)
	if err != nil {
		return err
	}

	if lacksURL {
		return unusableDest(ErrNoOriginURL, dest)
	}

	// The check comes before the comparison. A rewrite can change a
	// url to the URL of origin. That url passes the comparison, and
	// runRemote then refuses to pull from origin.
	if h.checkURL(origin) != nil {
		return unusableDest(ErrDeniedOriginURL, dest)
	}

	rewritten, err := rewriteURL(ctx, dest, url)
	if err != nil {
		if errors.Is(err, ErrTimeout) {
			return err
		}

		return fmt.Errorf("applying the URL rewrites of the git config of %s: %w", dest, err)
	}

	got := strings.TrimSuffix(origin, ".git")
	want := strings.TrimSuffix(rewritten, ".git")

	if got == want {
		return nil
	}

	stored, err := h.storedOrigin(ctx, dest, origin)
	if err != nil {
		return err
	}

	if stored == "" {
		got, err = h.matchingInput(ctx, dest, got)
		if err != nil {
			return err
		}

		if got == "" {
			return unusableDest(ErrUnmatchedOrigin, dest)
		}
	} else {
		got, err = matchingForm(ctx, dest, stored, got)
		if err != nil {
			return err
		}
	}

	// got passes the check and url does not, so the two differ.
	want = strings.TrimSuffix(url, ".git")
	if want == got {
		want = url
	}

	shownGot := stripCredential(got)
	shownWant := stripCredential(want)

	note := ""

	// got and want differ. When stripCredential returns the same URL
	// for both, they differ only in a part that it removes.
	if shownGot == shownWant {
		note = ": the URLs differ only in a user or a password that this message leaves out"
	}

	return fmt.Errorf("%w: got %s, want %s%s", ErrOriginMismatch, shownGot, shownWant, note)
}

// unusableDest returns reason as an error that names dest and tells
// the caller how to continue. reason is a fault of the repository at
// dest that keeps git_clone from pulling it, whatever the url input
// holds.
func unusableDest(reason error, dest string) error {
	return fmt.Errorf("%w: %s: choose another dest, or delete the directory and retry", reason, dest)
}

// matchingForm returns stored without a trailing ".git" suffix when a
// url input of git_clone that holds that form still passes
// [handler.checkOrigin]. Otherwise it returns stored. stored is the
// URL of [handler.storedOrigin], and got is the URL that git reads for
// origin, without a trailing ".git" suffix.
//
// A url.<base>.insteadOf rewrite can match stored only together with
// its ".git" suffix, such as a rewrite whose insteadOf value is
// https://host/org/repo.git. git then rewrites stored but not the form
// without the suffix.
func matchingForm(ctx context.Context, dest, stored, got string) (string, error) {
	trimmed := strings.TrimSuffix(stored, ".git")
	if trimmed == stored {
		return stored, nil
	}

	rewritten, err := rewriteURL(ctx, dest, trimmed)
	if errors.Is(err, ErrTimeout) {
		return "", err
	}

	if err == nil && strings.TrimSuffix(rewritten, ".git") == got {
		return trimmed, nil
	}

	// When rewriteURL fails, git rejects trimmed as a url input, so
	// trimmed does not pass the check.
	return stored, nil
}

// matchingInput returns a url input of git_clone that matches the
// origin remote of the repository at dest. It returns "" when no url
// input matches. got is the URL that git reads for origin, without a
// trailing ".git" suffix.
//
// A url input matches when it is not empty, [handler.checkURL] accepts
// it, and [rewriteURL] returns got for it, with or without a trailing
// ".git" suffix. [handler.handleClone] then accepts that url input,
// and [handler.checkOrigin] passes for it.
//
// For a url input that holds the name of a remote with a URL,
// rewriteURL returns the URL of that remote. It applies one
// url.<base>.insteadOf rewrite to another url input that starts with
// the insteadOf value of that rewrite. It returns every other url
// input unchanged. Every url input for which rewriteURL returns got,
// with or without the suffix, is therefore one of these candidates:
//
//   - got, with or without the suffix
//   - a URL that [rewriteSources] returns for got, with or without the
//     suffix
//   - a name that [remoteNames] returns
//
// matchingInput returns the first candidate that matches. It reads the
// candidates in the order of the list, and it reads got without the
// suffix before got with the suffix.
func (h *handler) matchingInput(ctx context.Context, dest, got string) (string, error) {
	targets := []string{got, got + ".git"}

	sources, err := rewriteSources(ctx, dest, targets)
	if err != nil {
		return "", err
	}

	names, err := remoteNames(ctx, dest)
	if err != nil {
		return "", err
	}

	for _, candidate := range slices.Concat(targets, sources, names) {
		// handleClone refuses an empty url input, and checkURL
		// accepts one when allowFileURLs is set.
		if candidate == "" || h.checkURL(candidate) != nil {
			continue
		}

		rewritten, err := rewriteURL(ctx, dest, candidate)
		if errors.Is(err, ErrTimeout) {
			return "", err
		}

		// When rewriteURL fails, git rejects the candidate as a url
		// input, so the candidate does not pass the check.
		if err == nil && strings.TrimSuffix(rewritten, ".git") == got {
			return candidate, nil
		}
	}

	return "", nil
}

// rewriteSources returns each URL that one url.<base>.insteadOf
// rewrite of the git config of the repository at repo can change to
// one of targets. For each rewrite and each target that starts with
// the base of the rewrite, that URL is the insteadOf value of the
// rewrite and then the rest of the target.
//
// git applies to a URL only the rewrite with the longest insteadOf
// value that the URL starts with. Another rewrite can therefore change
// a returned URL to another URL than the target, and the caller must
// read each returned URL with [rewriteURL].
func rewriteSources(ctx context.Context, repo string, targets []string) ([]string, error) {
	rewrites, err := configEntries(ctx, repo, `^url\..*\.insteadof$`)
	if err != nil {
		return nil, err
	}

	var sources []string

	for _, rewrite := range rewrites {
		base := strings.TrimSuffix(strings.TrimPrefix(rewrite.key, "url."), ".insteadof")

		for _, target := range targets {
			rest, found := strings.CutPrefix(target, base)
			if found {
				sources = append(sources, rewrite.value+rest)
			}
		}
	}

	return sources, nil
}

// remoteNames returns the name of each remote that the repository at
// repo can define with a URL. git reads a url input that holds the
// name of a remote with a URL as the URL of that remote.
//
// The git config can define such a remote with the key
// remote.<name>.url, and remoteNames returns the name of each remote
// that has that key. A file under the remotes directory or the
// branches directory of the common git directory can also define one.
// The name of the file is the name of the remote. remoteNames returns
// the name of each entry of those two directories, and it does not
// read the entries.
//
// A returned name need not define a URL. A file can hold no URL. From
// git 2.46, an empty value of remote.<name>.url clears the URLs that
// the config stores for the remote before that value. The caller
// therefore reads each name with [rewriteURL].
func remoteNames(ctx context.Context, repo string) ([]string, error) {
	urls, err := configEntries(ctx, repo, `^remote\..*\.url$`)
	if err != nil {
		return nil, err
	}

	var names []string

	// The config stores an entry for each URL of a remote.
	for _, url := range urls {
		names = appendUnique(names, strings.TrimSuffix(strings.TrimPrefix(url.key, "remote."), ".url"))
	}

	commonDir, err := readRepoPath(ctx, repo, "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}

	for _, dir := range []string{"remotes", "branches"} {
		files, err := os.ReadDir(filepath.Join(commonDir, dir))

		// git reads no remote from a path that names no directory.
		if err != nil && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
			return nil, fmt.Errorf("listing the remotes of %s: %w", repo, err)
		}

		for _, file := range files {
			names = append(names, file.Name())
		}
	}

	return names, nil
}

// rewriteURL returns url after git applies the url.<base>.insteadOf
// rewrites of the git config of the repository at dest. It runs git
// ls-remote --get-url, which does not contact the remote.
//
// When the git config defines a remote whose name is url, such as with
// git config remote.<url>.url, git ls-remote --get-url prints the URL
// of that remote instead. When url holds no '/', such as the SCP-style
// url git@host:repo, a file named url under .git/remotes or
// .git/branches of dest also defines such a remote. git ls-remote
// --get-url then prints the URL of that file, or it fails when git
// rejects the file.
//
// git remote add, git remote rename, and git clone --origin refuse a
// name that holds ':', and every url that [handler.checkURL] accepts
// outside tests holds one. git writes a file under .git/remotes or
// .git/branches only when it copies one from the template directory.
// Only a config or a file that someone writes by hand therefore
// defines such a remote.
func rewriteURL(ctx context.Context, dest, url string) (string, error) {
	return gitValue(ctx, "-C", dest, "ls-remote", "--get-url", "--", url)
}

// storedOrigin returns the URL that the git config of the repository
// at dest stores for the origin remote, before git applies the
// url.<base>.insteadOf rewrites to it. origin is the URL that git
// reads for that remote, after the rewrites. git_clone accepts the
// returned URL as a url input, and that input passes
// [handler.checkOrigin].
//
// The config can hold more than one value of remote.origin.url, and
// git reads origin from one of them. storedOrigin returns the first
// value that [handler.checkURL] accepts and for which [rewriteURL]
// returns origin. It returns "" when the config holds no such value.
// For example, the config does not set the key when a file under
// .git/remotes defines origin. checkURL refuses some values whose
// rewrites it accepts, such as the alias gh:org/repo, an http URL
// without allowInsecure, or a local path without allowFileURLs.
// rewriteURL returns another URL for a value when the config defines
// a remote whose name is the value.
func (h *handler) storedOrigin(ctx context.Context, dest, origin string) (string, error) {
	values, err := gitValue(ctx, "-C", dest, "config", "--get-all", "remote.origin.url")
	if err != nil {
		if errors.Is(err, ErrTimeout) {
			return "", err
		}

		// git config exits 1 when the config does not set the key.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", nil
		}

		return "", fmt.Errorf("reading git config remote.origin.url of %s: %w", dest, err)
	}

	for stored := range strings.SplitSeq(values, "\n") {
		// An empty value clears the values before it, and git rejects
		// an empty url input. handleClone refuses a url input that
		// checkURL refuses before it calls checkOrigin, so the caller
		// cannot pass such a value.
		if stored == "" || h.checkURL(stored) != nil {
			continue
		}

		rewritten, err := rewriteURL(ctx, dest, stored)
		if errors.Is(err, ErrTimeout) {
			return "", err
		}

		// When rewriteURL fails, git rejects the value as a url input,
		// so the value cannot pass the check.
		if err == nil && rewritten == origin {
			return stored, nil
		}
	}

	return "", nil
}

// stripCredential returns url without the credential that it embeds,
// so that a message can name the URL. It removes the password of every
// URL. It removes the user as well, because the user of an https URL
// can be a token. A URL of the ssh scheme and an SCP-style URL keep
// the user. That user is a login name, and a caller must pass it to
// name the same remote.
//
// In a URL that has a scheme, the user and the password come before
// the last '@' of the host part, and a ':' separates them. The host
// part ends at the first '/', '?', or '#' after "://".
//
// git reads a URL that has no scheme as the SCP-style user@host:path
// when the URL holds a ':' and no '/' comes before that ':'. git reads
// no password in that URL, but a person can write one after the user.
// stripCredential therefore reads the text before the first '@' that
// a ':' follows as the user and the password. Every other URL that has
// no scheme is a local path, and stripCredential returns it unchanged.
func stripCredential(url string) string {
	scheme, rest, found := strings.Cut(url, "://")
	if found {
		host := rest

		end := strings.IndexAny(rest, "/?#")
		if end >= 0 {
			host = rest[:end]
		}

		at := strings.LastIndex(host, "@")
		if at < 0 {
			return url
		}

		if scheme != "ssh" {
			return scheme + "://" + rest[at+1:]
		}

		user, _, _ := strings.Cut(rest[:at], ":")

		return scheme + "://" + user + rest[at:]
	}

	colon := strings.Index(url, ":")
	slash := strings.Index(url, "/")

	if colon < 0 || (slash >= 0 && slash < colon) {
		return url
	}

	userinfo, hostPath, found := strings.Cut(url, "@")
	if !found || !strings.Contains(hostPath, ":") {
		return url
	}

	user, _, _ := strings.Cut(userinfo, ":")

	return user + "@" + hostPath
}

// clone clones input.URL into input.Dest and returns the result of the
// git_clone tool. input.Dest holds the absolute path that
// [handler.handleClone] makes from the dest input.
//
// A non-empty aside names the path that a corrupt checkout at the
// destination moved to. The result ends with the sentence of
// [asideNote], which names that path, both when the clone succeeds and
// when it fails. After a failure, the destination does not hold the
// old checkout, and only the error tells the caller where its files
// are.
//
//nolint:unparam // signature matches mcp.AddTool handler contract.
func (h *handler) clone(ctx context.Context, input CloneInput, aside string) (*mcp.CallToolResult, any, error) {
	err := h.runClone(ctx, input)
	if err != nil {
		if aside != "" {
			err = fmt.Errorf("%w\n\n%s", err, asideNote(aside))
		}

		return toolError(err), nil, nil
	}

	msg := fmt.Sprintf("Cloned %s into %s", input.URL, input.Dest)
	if len(input.SparsePaths) > 0 {
		msg += fmt.Sprintf(" (sparse: %s)", strings.Join(input.SparsePaths, ", "))
	}

	if aside != "" {
		msg += ". " + asideNote(aside)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}, nil, nil
}

// asideNote returns the sentence that tells the caller of git_clone
// where the corrupt checkout of the destination moved to. aside is the
// path of [handler.moveAside]. The sentence ends with the path and has
// no final period, so that the caller can read where the path ends.
func asideNote(aside string) string {
	return "The previous checkout had a corrupt .git directory and moved to " + aside
}

// runClone runs git clone for input. When input names sparse paths, it
// then sets them as the sparse checkout of the clone.
//
// Each git command takes the options of [guardOptions] and
// [hookOptions]. A new clone holds the hook files of the template
// directory, and git runs them when it checks out the clone. git also
// runs each hook of the global config in the clone, where the remote
// chose every file. [handler.runRemote] reads the git config of an
// existing repository before it runs git. The config of a new clone
// does not exist before the clone, so runClone disables the hooks of
// [readUserHooks]. The command of [sparseCheckoutArgs] runs after the
// clone, and it names the working tree itself.
//
// Each git command also takes the options of
// [handler.credentialArgs], which authenticate a clone from GitHub
// over https. A sparse clone without a depth leaves out each blob
// that its checkout does not hold. The command of [sparseCheckoutArgs]
// then fetches the blobs of the sparse paths from origin.
func (h *handler) runClone(ctx context.Context, input CloneInput) error {
	hooks, err := readUserHooks(ctx)
	if err != nil {
		return err
	}

	opts := slices.Concat(h.credentialArgs(), guardOptions(), hookOptions(hooks))

	_, err = h.runGit(ctx, slices.Concat(opts, buildCloneArgs(input))...)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrClone, err)
	}

	if len(input.SparsePaths) > 0 {
		_, err = h.runGit(ctx, sparseCheckoutArgs(opts, input.Dest, input.SparsePaths)...)
		if err != nil {
			return fmt.Errorf("setting sparse checkout: %w", err)
		}
	}

	return nil
}

// sparseCheckoutArgs returns the arguments of the git command that
// sets paths as the sparse checkout of the new clone at dest. opts
// holds the options that come before the subcommand, and dest is an
// absolute path.
//
// The command runs after the clone, and the caller can write the git
// config of the clone between the two commands. core.worktree in that
// config names the working tree, and git writes the files of paths
// there. The remote chooses the content of those files. --work-tree
// wins over core.worktree, so git writes the files in dest. git
// resolves a relative --work-tree against the directory of -C, so
// dest must be absolute.
func sparseCheckoutArgs(opts []string, dest string, paths []string) []string {
	return slices.Concat(opts, []string{"--work-tree=" + dest, "-C", dest, "sparse-checkout", "set"}, paths)
}

// checkSparsePaths validates each path in paths for use with
// git sparse-checkout. Empty paths, dash prefixes, absolute
// paths, ".." segments, and control characters are rejected.
func checkSparsePaths(paths []string) error {
	for _, p := range paths {
		if p == "" {
			return fmt.Errorf("%w: empty path", ErrDeniedSparsePath)
		}

		if strings.HasPrefix(p, "-") {
			return fmt.Errorf("%w: %q starts with '-'", ErrDeniedSparsePath, p)
		}

		if strings.HasPrefix(p, "/") {
			return fmt.Errorf("%w: %q is absolute", ErrDeniedSparsePath, p)
		}

		if strings.ContainsAny(p, "\x00\n\r") {
			return fmt.Errorf("%w: %q contains control characters", ErrDeniedSparsePath, p)
		}

		if slices.Contains(strings.Split(p, "/"), "..") {
			return fmt.Errorf("%w: %q contains '..'", ErrDeniedSparsePath, p)
		}
	}

	return nil
}

// buildCloneArgs converts a [CloneInput] into the argument list for git clone.
//
// The list names the remote of the new clone [defaultRemote], because
// [handler.pullExisting] pulls from the remote of that name. Without
// --origin, git names the remote after clone.defaultRemoteName of the
// git config.
func buildCloneArgs(input CloneInput) []string {
	args := []string{"clone", "-q", "--origin", defaultRemote}

	if input.Depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", input.Depth))
	}

	if input.Branch != "" {
		args = append(args, "--branch", input.Branch)
	}

	if input.SingleBranch {
		args = append(args, "--single-branch")
	}

	if input.Sparse || len(input.SparsePaths) > 0 {
		args = append(args, "--sparse")

		if input.Depth == 0 {
			args = append(args, "--filter=blob:none")
		}
	}

	args = append(args, "--", input.URL, input.Dest)

	return args
}
