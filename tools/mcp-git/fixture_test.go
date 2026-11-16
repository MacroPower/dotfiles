package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// Most tests need a repository, and many need a bare repository with a
// clone of it. Each git command that builds one starts a process, and
// git clone, git commit, and git push each start more processes of
// their own. A process start is slow on some hosts. On a Mac that
// scans each new process, a suite that builds the repositories of
// every case with git runs for more than ten minutes.
//
// The test binary therefore builds each repository one time, as a
// template, and a test copies the template with file operations. A
// helper that returns a repository must start no git process after its
// template exists. [TestFixturesStartNoGit] checks each helper.
//
// Change the config of a copy with [setConfig], [appendGitConfig],
// [setOriginURL], and [unsetOriginURL], which write the config file.
// Give the remote of a copy a later commit with [pushRemoteCommit],
// which replaces the files of the remote.
//
// A template holds no path of its own directory, so that a copy never
// reads or writes the template. The git config of a template clone
// names its remote with a placeholder. The helper that copies the
// clone puts the path of the copied remote in its place.

const (
	// identityConfig is git config text that names the author of the
	// commits of a test.
	identityConfig = "[user]\n\temail = test@test.com\n\tname = Test\n"

	// originPlaceholder stands for the URL of the origin remote in the
	// git config of a template clone.
	originPlaceholder = "@origin@"

	// submodulePlaceholder stands for the URL of the submodule in the
	// git config of the template clones of [initSubmoduleRepos].
	submodulePlaceholder = "@submodule@"

	// remoteFile and localFile name the file that the commit of each
	// side adds, in the repositories of [initRepos].
	remoteFile = "remote.txt"
	localFile  = "local.txt"

	// refFormat is the git for-each-ref option whose output
	// [parseRefs] reads.
	refFormat = "--format=%(refname) %(objectname)"
)

var (
	// templateDir is the directory that holds every template.
	// [TestMain] creates it before the tests run, and removes it after
	// them.
	templateDir string

	// templateGit is the path of git. The test that builds a template
	// can run with another PATH.
	templateGit string

	// templateEnv is the environment of each git command that builds a
	// template. It is the environment that [TestMain] leaves, so the
	// variables that one test sets never reach a template. Its global
	// git config turns off the maintenance that git starts in the
	// background after a commit and after a push. Maintenance creates
	// and removes a lock file under the git directory, and a copy that
	// runs at that moment fails.
	templateEnv []string

	loadBaseTemplates = sync.OnceValues(buildBaseTemplates)

	loadRepoTemplates = sync.OnceValues(buildRepoTemplates)

	loadDocsTemplates = sync.OnceValues(buildDocsTemplates)

	loadEmptyTemplates = sync.OnceValues(buildEmptyTemplates)

	loadConflictTemplates = sync.OnceValues(buildConflictTemplates)

	loadFeatureTemplates = sync.OnceValues(buildFeatureTemplates)

	loadSubmoduleTemplates = sync.OnceValues(buildSubmoduleTemplates)

	loadAdvancedSubmoduleTemplates = sync.OnceValues(buildAdvancedSubmoduleTemplates)
)

// setUpTemplates creates the directory of the templates and the
// environment of the commands that build them. It returns a function
// that removes the directory.
func setUpTemplates() (func(), error) {
	dir, err := os.MkdirTemp("", "mcp-git-templates-")
	if err != nil {
		return nil, fmt.Errorf("creating the template directory: %w", err)
	}

	remove := func() {
		// The directory holds only what the tests can build again, so
		// a failure to remove it loses nothing.
		_ = os.RemoveAll(dir) //nolint:errcheck // best effort
	}

	git, err := exec.LookPath("git")
	if err != nil {
		remove()

		return nil, fmt.Errorf("finding git: %w", err)
	}

	global := filepath.Join(dir, "gitconfig")

	err = os.WriteFile(global, []byte("[maintenance]\n\tauto = false\n"), 0o644)
	if err != nil {
		remove()

		return nil, fmt.Errorf("writing the git config of the templates: %w", err)
	}

	templateDir = dir
	templateGit = git

	templateEnv = append(os.Environ(), "GIT_CONFIG_GLOBAL="+global)

	return remove, nil
}

// templateBuilder runs the steps that build a template. It keeps the
// first error, and it skips every step after that error.
type templateBuilder struct {
	err error
}

// fail keeps err as the error of the builder when err is the first
// error of a step.
func (b *templateBuilder) fail(err error) {
	if err != nil && b.err == nil {
		b.err = fmt.Errorf("building a template: %w", err)
	}
}

// git runs git with args, and it returns the trimmed stdout of git.
func (b *templateBuilder) git(args ...string) string {
	if b.err != nil {
		return ""
	}

	cmd := exec.CommandContext(context.Background(), templateGit, args...)
	cmd.Env = templateEnv

	out, err := cmd.Output()
	if err != nil {
		var (
			exitErr *exec.ExitError
			stderr  string
		)

		if errors.As(err, &exitErr) {
			stderr = strings.TrimSpace(string(exitErr.Stderr))
		}

		b.fail(fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, stderr))
	}

	return strings.TrimSpace(string(out))
}

// write creates the file at path with content, and each directory
// that the path needs.
func (b *templateBuilder) write(path, content string) {
	if b.err != nil {
		return
	}

	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		b.fail(err)

		return
	}

	b.fail(os.WriteFile(path, []byte(content), 0o644))
}

// identify gives the repository with the git directory at gitDir the
// identity of a test author.
func (b *templateBuilder) identify(gitDir string) {
	if b.err == nil {
		b.fail(appendFile(filepath.Join(gitDir, "config"), identityConfig))
	}
}

// copy copies the directory tree at src to dst.
func (b *templateBuilder) copy(src, dst string) {
	if b.err == nil {
		b.fail(copyTree(src, dst))
	}
}

// remove removes the directory tree at path.
func (b *templateBuilder) remove(path string) {
	if b.err == nil {
		b.fail(os.RemoveAll(path))
	}
}

// replace replaces old with text in the file at path.
func (b *templateBuilder) replace(path, old, text string) {
	if b.err != nil {
		return
	}

	content, err := os.ReadFile(path)
	if err != nil {
		b.fail(err)

		return
	}

	b.write(path, strings.ReplaceAll(string(content), old, text))
}

// clone copies the template clone at src to dst, and it gives the
// origin remote of the copy the URL of the repository at bare. The
// commands of a builder can then push to that repository. The builder
// puts the placeholder back with [templateBuilder.placeholder].
func (b *templateBuilder) clone(src, dst, bare string) {
	b.copy(src, dst)
	b.replace(filepath.Join(dst, ".git", "config"), originPlaceholder, configQuote(bare))
}

// placeholder gives the origin remote of the clone at repo the
// placeholder for its URL.
func (b *templateBuilder) placeholder(repo string) {
	b.git("-C", repo, "config", "remote.origin.url", originPlaceholder)
}

// commit writes content to the file name in the clone at repo, and
// commits the file with the message msg.
func (b *templateBuilder) commit(repo, name, content, msg string) {
	b.write(filepath.Join(repo, name), content)
	b.git("-C", repo, "add", ".")
	b.git("-C", repo, "commit", "-q", "-m", msg)
}

// head returns the commit that the repository at repo has checked
// out.
func (b *templateBuilder) head(repo string) string {
	return b.git("-C", repo, "rev-parse", "HEAD")
}

// refs returns the object of each ref of the repository at repo, by
// the name of the ref.
func (b *templateBuilder) refs(repo string) map[string]string {
	return parseRefs(b.git("-C", repo, "for-each-ref", refFormat))
}

// parseRefs reads the output of git for-each-ref with refFormat. It
// returns the object of each ref, by the name of the ref.
func parseRefs(out string) map[string]string {
	refs := map[string]string{}

	for line := range strings.SplitSeq(out, "\n") {
		name, object, ok := strings.Cut(line, " ")
		if ok {
			refs[name] = object
		}
	}

	return refs
}

// appendFile appends text to the file at path.
func appendFile(path, text string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}

	_, err = f.WriteString(text)
	if err != nil {
		_ = f.Close() //nolint:errcheck // the write already failed

		return fmt.Errorf("writing %s: %w", path, err)
	}

	err = f.Close()
	if err != nil {
		return fmt.Errorf("closing %s: %w", path, err)
	}

	return nil
}

// copyTree copies the directory tree at src to dst. dst must hold no
// file of the tree.
func copyTree(src, dst string) error {
	err := os.CopyFS(dst, os.DirFS(src))
	if err != nil {
		return fmt.Errorf("copying %s to %s: %w", src, dst, err)
	}

	return nil
}

// baseTemplates holds the templates of [initBareRepo] and
// [initWorkClone].
type baseTemplates struct {
	bare string // bare repository with the first commit
	work string // clone of bare

	// branch is the branch that both repositories have checked out.
	branch string

	// firstCommit is the commit of both repositories.
	firstCommit string
}

// buildBaseTemplates builds the templates of [initBareRepo] and
// [initWorkClone].
func buildBaseTemplates() (*baseTemplates, error) {
	root := filepath.Join(templateDir, "base")
	seed := filepath.Join(root, "seed")
	tm := &baseTemplates{
		bare: filepath.Join(root, "bare.git"),
		work: filepath.Join(root, "work"),
	}

	var b templateBuilder

	// The first commit reaches the bare repository through a push, as
	// the commit of a developer does.
	b.git("init", "-q", "--bare", tm.bare)
	b.git("clone", "-q", tm.bare, seed)
	b.identify(filepath.Join(seed, ".git"))
	b.commit(seed, "README", "hello", "init")
	b.git("-C", seed, "push", "-q")
	b.remove(seed)

	b.git("clone", "-q", tm.bare, tm.work)
	b.identify(filepath.Join(tm.work, ".git"))

	tm.branch = b.git("-C", tm.work, "symbolic-ref", "--short", "HEAD")
	tm.firstCommit = b.head(tm.work)

	b.placeholder(tm.work)

	return tm, b.err
}

// repoTemplates holds the templates of [initRepos]. upstream is a copy
// of bare with one more commit, and local is a copy of work with one
// more commit.
type repoTemplates struct {
	*baseTemplates

	upstream string // bare repository with the first commit and the remote commit
	local    string // clone of bare with the local commit

	remoteCommit string // commit that only upstream holds
	localCommit  string // commit that only local holds
}

// buildRepoTemplates builds the templates of [initRepos].
func buildRepoTemplates() (*repoTemplates, error) {
	base, err := loadBaseTemplates()
	if err != nil {
		return nil, err
	}

	root := filepath.Join(templateDir, "repos")
	author := filepath.Join(root, "author")
	tm := &repoTemplates{
		baseTemplates: base,
		upstream:      filepath.Join(root, "upstream.git"),
		local:         filepath.Join(root, "local"),
	}

	var b templateBuilder

	b.copy(base.bare, tm.upstream)
	b.clone(base.work, author, tm.upstream)
	b.commit(author, remoteFile, "remote", "remote commit")
	b.git("-C", author, "push", "-q")

	tm.remoteCommit = b.head(author)

	b.remove(author)

	b.copy(base.work, tm.local)
	b.commit(tm.local, localFile, "local", "local commit")

	tm.localCommit = b.head(tm.local)

	return tm, b.err
}

// docsTemplates holds the templates of [initDocsRepo].
type docsTemplates struct {
	bare    string // bare repository with the first commit and the commit that adds the files
	updated string // bare, and a commit that changes docs/guide

	// otherBlob is the blob of the file other/x.
	otherBlob string
}

// buildDocsTemplates builds the templates of [initDocsRepo].
func buildDocsTemplates() (*docsTemplates, error) {
	base, err := loadBaseTemplates()
	if err != nil {
		return nil, err
	}

	root := filepath.Join(templateDir, "docs")
	author := filepath.Join(root, "author")
	tm := &docsTemplates{
		bare:    filepath.Join(root, "bare.git"),
		updated: filepath.Join(root, "updated.git"),
	}

	var b templateBuilder

	b.copy(base.bare, tm.bare)
	b.clone(base.work, author, tm.bare)
	b.write(filepath.Join(author, "other", "x"), "other")
	b.commit(author, filepath.Join("docs", "guide"), "guide", "add docs")
	b.git("-C", author, "push", "-q")

	tm.otherBlob = b.git("-C", author, "rev-parse", "HEAD:other/x")

	b.copy(tm.bare, tm.updated)
	b.commit(author, filepath.Join("docs", "guide"), "guide v2", "update docs")
	b.git("-C", author, "push", "-q", tm.updated, "HEAD:refs/heads/"+base.branch)
	b.remove(author)

	return tm, b.err
}

// emptyTemplates holds a repository as git init creates it, and a
// bare repository as git init --bare creates it.
type emptyTemplates struct {
	repo string
	bare string
}

// buildEmptyTemplates builds the templates of [initRepoAt] and
// [initEmptyBare].
func buildEmptyTemplates() (*emptyTemplates, error) {
	root := filepath.Join(templateDir, "empty")
	tm := &emptyTemplates{
		repo: filepath.Join(root, "repo"),
		bare: filepath.Join(root, "bare.git"),
	}

	var b templateBuilder

	b.git("init", "-q", tm.repo)
	b.git("init", "-q", "--bare", tm.bare)

	return tm, b.err
}

// conflictTemplates holds the templates of [initRebaseConflict].
type conflictTemplates struct {
	bare string // bare repository with a commit that edits README
	work string // clone with a commit of its own that edits README
}

// buildConflictTemplates builds the templates of
// [initRebaseConflict].
func buildConflictTemplates() (*conflictTemplates, error) {
	base, err := loadBaseTemplates()
	if err != nil {
		return nil, err
	}

	root := filepath.Join(templateDir, "conflict")
	author := filepath.Join(root, "author")
	tm := &conflictTemplates{
		bare: filepath.Join(root, "bare.git"),
		work: filepath.Join(root, "work"),
	}

	var b templateBuilder

	b.copy(base.bare, tm.bare)
	b.clone(base.work, author, tm.bare)
	b.commit(author, "README", "remote edit", "remote edit")
	b.git("-C", author, "push", "-q")
	b.remove(author)

	b.copy(base.work, tm.work)
	b.commit(tm.work, "README", "local edit", "local edit")

	return tm, b.err
}

// featureTemplates holds the templates of [initFeatureRepos].
type featureTemplates struct {
	bare string
	work string

	// bareRefs and workRefs hold the object of each ref of the
	// repository, by the name of the ref.
	bareRefs map[string]string
	workRefs map[string]string
}

// buildFeatureTemplates builds the templates of [initFeatureRepos].
func buildFeatureTemplates() (*featureTemplates, error) {
	base, err := loadBaseTemplates()
	if err != nil {
		return nil, err
	}

	root := filepath.Join(templateDir, "feature")
	tm := &featureTemplates{
		bare: filepath.Join(root, "bare.git"),
		work: filepath.Join(root, "work"),
	}

	var b templateBuilder

	b.copy(base.bare, tm.bare)
	b.clone(base.work, tm.work, tm.bare)
	b.git("-C", tm.work, "switch", "-q", "-c", "base")
	b.git("-C", tm.work, "push", "-q", "origin", "base")
	b.commit(tm.work, "base.txt", "x", "local commit on base")
	b.git("-C", tm.work, "switch", "-q", "-c", "feat/x")
	b.commit(tm.work, "feature.txt", "x", "feature commit")
	b.git("-C", tm.work, "tag", "-a", "-m", "release", "v1")
	b.placeholder(tm.work)

	tm.bareRefs = b.refs(tm.bare)
	tm.workRefs = b.refs(tm.work)

	return tm, b.err
}

// submoduleTemplates holds the template of [initSubmoduleRepos], or
// of [initAdvancedSubmoduleRepos].
type submoduleTemplates struct {
	// root holds the bare repositories bare.git and sub.git, and the
	// clone work with its submodule sub.
	root string

	// bareHead is the commit of the checked-out branch of bare.git.
	bareHead string

	// subHead is the commit that the submodule has checked out, and
	// the commit of its remote-tracking branch.
	subHead string

	// subBareRefs is the output of git for-each-ref in sub.git.
	subBareRefs string
}

// bare returns the path of the upstream of the parent.
func (tm *submoduleTemplates) bare() string { return filepath.Join(tm.root, "bare.git") }

// subBare returns the path of the upstream of the submodule.
func (tm *submoduleTemplates) subBare() string { return filepath.Join(tm.root, "sub.git") }

// work returns the path of the parent.
func (tm *submoduleTemplates) work() string { return filepath.Join(tm.root, "work") }

// subGitDir returns the path of the git directory of the submodule.
// git keeps it inside the git directory of the parent.
func (tm *submoduleTemplates) subGitDir() string {
	return filepath.Join(tm.work(), ".git", "modules", "sub")
}

// record reads the commits and the refs that the template holds, and
// it puts each placeholder in the git config of the parent and of the
// submodule.
func (tm *submoduleTemplates) record(b *templateBuilder) {
	sub := filepath.Join(tm.work(), "sub")

	tm.bareHead = b.head(tm.bare())
	tm.subHead = b.head(sub)
	tm.subBareRefs = b.git("-C", tm.subBare(), "for-each-ref")

	b.placeholder(tm.work())
	b.git("-C", tm.work(), "config", "submodule.sub.url", submodulePlaceholder)
	b.git("-C", sub, "config", "remote.origin.url", submodulePlaceholder)
}

// buildSubmoduleTemplates builds the template of
// [initSubmoduleRepos].
//
// The parent names the submodule by a path relative to its own remote.
// The commit that adds the submodule therefore holds no path of the
// template.
func buildSubmoduleTemplates() (*submoduleTemplates, error) {
	base, err := loadBaseTemplates()
	if err != nil {
		return nil, err
	}

	tm := &submoduleTemplates{root: filepath.Join(templateDir, "submodule")}
	release := filepath.Join(tm.root, "release")

	var b templateBuilder

	// The upstream of the submodule holds a commit that only the tag
	// v1 reaches.
	b.copy(base.bare, tm.subBare())
	b.clone(base.work, release, tm.subBare())
	b.git("-C", release, "switch", "-q", "--detach")
	b.commit(release, "release.txt", "x", "release v1")
	b.git("-C", release, "tag", "v1")
	b.git("-C", release, "push", "-q", "origin", "v1")
	b.remove(release)

	b.copy(base.bare, tm.bare())
	b.clone(base.work, tm.work(), tm.bare())
	b.git("-C", tm.work(), "-c", "protocol.file.allow=always", "submodule", "--quiet", "add", "../sub.git", "sub")
	b.git("-C", tm.work(), "commit", "-q", "-m", "add submodule")
	b.git("-C", tm.work(), "push", "-q")
	b.identify(tm.subGitDir())

	tm.record(&b)

	return tm, b.err
}

// buildAdvancedSubmoduleTemplates builds the template of
// [initAdvancedSubmoduleRepos] from a copy of the template of
// [initSubmoduleRepos].
func buildAdvancedSubmoduleTemplates() (*submoduleTemplates, error) {
	base, err := loadBaseTemplates()
	if err != nil {
		return nil, err
	}

	plain, err := loadSubmoduleTemplates()
	if err != nil {
		return nil, err
	}

	tm := &submoduleTemplates{root: filepath.Join(templateDir, "advanced-submodule")}
	other := filepath.Join(tm.root, "other")
	otherSub := filepath.Join(other, "sub")

	var b templateBuilder

	// The copy holds the placeholders of the template. The commands of
	// this builder read the config of the parent, so the config names
	// the repositories of the copy until the builder records the
	// template.
	b.copy(plain.root, tm.root)
	b.replace(filepath.Join(tm.work(), ".git", "config"), originPlaceholder, configQuote(tm.bare()))
	b.replace(filepath.Join(tm.work(), ".git", "config"), submodulePlaceholder, configQuote(tm.subBare()))
	b.replace(filepath.Join(tm.subGitDir(), "config"), submodulePlaceholder, configQuote(tm.subBare()))

	b.git("clone", "-q", tm.bare(), other)
	b.identify(filepath.Join(other, ".git"))
	b.git("-C", other, "-c", "protocol.file.allow=always", "submodule", "--quiet", "update", "--init")
	b.identify(filepath.Join(other, ".git", "modules", "sub"))
	b.commit(otherSub, "next.txt", "x", "next submodule commit")
	b.git("-C", otherSub, "push", "-q", "origin", "HEAD:refs/heads/"+base.branch)
	b.commit(other, "bump.txt", "x", "bump submodule")
	b.git("-C", other, "push", "-q")
	b.remove(other)

	tm.record(&b)

	return tm, b.err
}

// copyTemplate copies the template at src to dst.
func copyTemplate(t *testing.T, src, dst string) {
	t.Helper()

	require.NoError(t, copyTree(src, dst))
}

// baseRepos returns the templates of [initBareRepo] and
// [initWorkClone], and it builds them when no test built them before.
func baseRepos(t *testing.T) *baseTemplates {
	t.Helper()

	tm, err := loadBaseTemplates()
	require.NoError(t, err)

	return tm
}

// repos returns the templates of [initRepos], and it builds them when
// no test built them before.
func repos(t *testing.T) *repoTemplates {
	t.Helper()

	tm, err := loadRepoTemplates()
	require.NoError(t, err)

	return tm
}

// defaultBranch returns the branch that each repository of
// [initBareRepo], [initWorkClone], and [initRepos] has checked out.
func defaultBranch(t *testing.T) string {
	t.Helper()

	return baseRepos(t).branch
}

// initBareRepo returns a new bare repository that holds one commit,
// which adds the file README.
func initBareRepo(t *testing.T) string {
	t.Helper()

	bare := filepath.Join(t.TempDir(), "bare.git")
	copyTemplate(t, baseRepos(t).bare, bare)

	return bare
}

// initWorkClone returns a new clone of the bare repository at bare,
// with the identity of a test author. bare must be a repository of
// [initBareRepo] that no command changed, because initWorkClone copies
// a clone of the template.
func initWorkClone(t *testing.T, bare string) string {
	t.Helper()

	tm := baseRepos(t)

	// A comparison of the two listings alone keeps the hashes out of
	// the message of a failure.
	require.True(t, maps.Equal(readFiles(t, tm.bare), readFiles(t, bare)),
		"initWorkClone copies a clone of the repository of initBareRepo, and %s differs from that repository", bare)

	return copyClone(t, tm.work, bare)
}

// initRepos returns a new bare repository and a new clone of it. Both
// hold the commit of [initBareRepo]. With upstream set, the bare
// repository also holds a commit that the clone lacks, which adds
// remoteFile. With local set, the clone also holds a commit that the
// bare repository lacks, which adds localFile.
func initRepos(t *testing.T, upstream, local bool) (string, string) {
	t.Helper()

	tm := repos(t)

	bareTemplate := tm.bare
	if upstream {
		bareTemplate = tm.upstream
	}

	workTemplate := tm.work
	if local {
		workTemplate = tm.local
	}

	bare := filepath.Join(t.TempDir(), "bare.git")
	copyTemplate(t, bareTemplate, bare)

	return bare, copyClone(t, workTemplate, bare)
}

// copyClone copies the template clone at src into a new directory,
// and it returns the copy. The origin remote of the copy has the URL
// origin, which is the path of a bare repository in most tests.
func copyClone(t *testing.T, src, origin string) string {
	t.Helper()

	work := filepath.Join(t.TempDir(), "work")
	copyCloneTo(t, src, work, origin)

	return work
}

// copyCloneTo copies the template clone at src to dst. The origin
// remote of the copy has the URL origin.
func copyCloneTo(t *testing.T, src, dst, origin string) {
	t.Helper()

	copyTemplate(t, src, dst)
	replaceInConfig(t, dst, originPlaceholder, configQuote(origin))
}

// initCloneAt creates a clone of the bare repository at bare at dest,
// as a git_clone call creates it. bare is a repository of
// [initBareRepo] that no command changed.
func initCloneAt(t *testing.T, bare, dest string) {
	t.Helper()

	copyCloneTo(t, baseRepos(t).work, dest, bare)
}

// pushRemoteCommit gives the bare repository at bare the remote commit
// of [initRepos], as a push from another clone does. bare is a
// repository of [initBareRepo] or of initRepos. pushRemoteCommit
// replaces the files of the repository, and it keeps the config file.
func pushRemoteCommit(t *testing.T, bare string) {
	t.Helper()

	replaceRepo(t, bare, repos(t).upstream)
}

// replaceRepo replaces the files of the bare repository at bare with
// a copy of the template at src. It keeps the config file of bare.
func replaceRepo(t *testing.T, bare, src string) {
	t.Helper()

	path := filepath.Join(bare, "config")

	config, err := os.ReadFile(path)
	require.NoError(t, err)

	require.NoError(t, os.RemoveAll(bare))
	copyTemplate(t, src, bare)
	require.NoError(t, os.WriteFile(path, config, 0o644))
}

// docsRepo holds the repository of [initDocsRepo].
type docsRepo struct {
	// bare is the path of the bare repository.
	bare string

	// otherBlob is the blob of the file other/x.
	otherBlob string
}

// initDocsRepo creates a bare repository at bare. Its checked-out
// branch holds the files README, docs/guide, and other/x, so a sparse
// clone of docs leaves out other/x.
func initDocsRepo(t *testing.T, bare string) docsRepo {
	t.Helper()

	tm, err := loadDocsTemplates()
	require.NoError(t, err)

	copyTemplate(t, tm.bare, bare)

	return docsRepo{bare: bare, otherBlob: tm.otherBlob}
}

// pushDocsUpdate gives the bare repository of [initDocsRepo] at bare
// a commit that changes the content of docs/guide to "guide v2", as a
// push from another clone does. It replaces the files of the
// repository, and it keeps the config file.
func pushDocsUpdate(t *testing.T, bare string) {
	t.Helper()

	tm, err := loadDocsTemplates()
	require.NoError(t, err)

	replaceRepo(t, bare, tm.updated)
}

// initRepo returns a new repository in a new directory under parent,
// as git init creates it.
func initRepo(t *testing.T, parent string) string {
	t.Helper()

	repo := filepath.Join(parent, "repo")
	initRepoAt(t, repo)

	return repo
}

// initRepoAt creates a repository at path, as git init creates it.
func initRepoAt(t *testing.T, path string) {
	t.Helper()

	tm, err := loadEmptyTemplates()
	require.NoError(t, err)

	copyTemplate(t, tm.repo, path)
}

// initEmptyBare creates a bare repository at path, as git init --bare
// creates it.
func initEmptyBare(t *testing.T, path string) {
	t.Helper()

	tm, err := loadEmptyTemplates()
	require.NoError(t, err)

	copyTemplate(t, tm.bare, path)
}

// initRebaseConflict returns a clone whose rebase pull conflicts. The
// clone and its remote each hold a commit of their own that edits
// README, the file that the first commit adds. The message of the
// commit of the clone is "local edit".
func initRebaseConflict(t *testing.T) string {
	t.Helper()

	tm, err := loadConflictTemplates()
	require.NoError(t, err)

	bare := filepath.Join(t.TempDir(), "bare.git")
	copyTemplate(t, tm.bare, bare)

	return copyClone(t, tm.work, bare)
}

// featureRepos holds the repositories of [initFeatureRepos].
type featureRepos struct {
	bare string
	work string

	// bareRefs and workRefs hold the object of each ref that the
	// repository holds when initFeatureRepos returns, by the name of
	// the ref.
	bareRefs map[string]string
	workRefs map[string]string
}

// initFeatureRepos returns a bare repository and a clone of it with a
// feature branch. The bare repository holds the default branch and the
// branch base. The clone has the new branch feat/x checked out, which
// has no upstream. The clone is one commit ahead of the bare
// repository on base, and feat/x holds one more commit. The annotated
// tag v1 names the commit of feat/x.
func initFeatureRepos(t *testing.T) featureRepos {
	t.Helper()

	tm, err := loadFeatureTemplates()
	require.NoError(t, err)

	bare := filepath.Join(t.TempDir(), "bare.git")
	copyTemplate(t, tm.bare, bare)

	return featureRepos{
		bare:     bare,
		work:     copyClone(t, tm.work, bare),
		bareRefs: maps.Clone(tm.bareRefs),
		workRefs: maps.Clone(tm.workRefs),
	}
}

// submoduleRepos holds a parent repository with a submodule, and the
// bare repository that each of them pushes to.
type submoduleRepos struct {
	work    string // the parent repository
	sub     string // the submodule, inside work
	bare    string // the upstream of work
	subBare string // the upstream of sub

	// marker is the file that the hook of the submodule creates when
	// it runs.
	marker string

	// bareHead is the commit of the checked-out branch of bare when
	// the helper returns.
	bareHead string

	// subHead is the commit that sub has checked out when the helper
	// returns. The remote-tracking branch of sub names the same
	// commit.
	subHead string

	// subBareRefs is the output of git for-each-ref in subBare when
	// the helper returns.
	subBareRefs string
}

// initSubmoduleRepos returns a parent repository with a submodule,
// and the upstream of each. The upstream of the parent holds the
// commit that adds the submodule. The upstream of the submodule holds
// a commit that only the tag v1 reaches, as a release tag of a
// third-party project does.
//
// The git config of the submodule defines the hook check for each
// event of events, such as pre-push. Git 2.54 and later run such a
// hook, and a git that does not know the keys ignores them.
func initSubmoduleRepos(t *testing.T, events ...string) submoduleRepos {
	t.Helper()

	tm, err := loadSubmoduleTemplates()
	require.NoError(t, err)

	return copySubmoduleRepos(t, tm, events)
}

// initAdvancedSubmoduleRepos returns the repositories of
// [initSubmoduleRepos] after a push to each upstream. The upstream of
// the submodule holds a new commit on its checked-out branch, which
// adds next.txt. The upstream of the parent holds a commit that moves
// the submodule to that commit. A fetch in the parent that recurses
// into submodules then fetches the new commit into the submodule.
func initAdvancedSubmoduleRepos(t *testing.T, events ...string) submoduleRepos {
	t.Helper()

	tm, err := loadAdvancedSubmoduleTemplates()
	require.NoError(t, err)

	return copySubmoduleRepos(t, tm, events)
}

// copySubmoduleRepos copies the template tm into a new directory. It
// defines the hook check of the submodule for each event of events.
func copySubmoduleRepos(t *testing.T, tm *submoduleTemplates, events []string) submoduleRepos {
	t.Helper()

	// The copy has the layout of the template.
	copied := &submoduleTemplates{root: filepath.Join(t.TempDir(), "repos")}
	copyTemplate(t, tm.root, copied.root)

	s := submoduleRepos{
		work:        copied.work(),
		sub:         filepath.Join(copied.work(), "sub"),
		bare:        copied.bare(),
		subBare:     copied.subBare(),
		marker:      filepath.Join(t.TempDir(), "hook-ran"),
		bareHead:    tm.bareHead,
		subHead:     tm.subHead,
		subBareRefs: tm.subBareRefs,
	}
	subGitDir := copied.subGitDir()

	replaceInConfig(t, s.work, originPlaceholder, configQuote(s.bare))
	replaceInConfig(t, s.work, submodulePlaceholder, configQuote(s.subBare))
	replaceInConfig(t, subGitDir, submodulePlaceholder, configQuote(s.subBare))

	hook := writeHookIn(t, t.TempDir(), "check", "#!/bin/sh\ntouch '"+s.marker+"'\n")

	var config strings.Builder

	config.WriteString("[hook \"check\"]\n\tcommand = " + configQuote(hook) + "\n")

	for _, event := range events {
		config.WriteString("\tevent = " + event + "\n")
	}

	appendGitConfig(t, subGitDir, config.String())

	return s
}

// readFiles returns the hash of each regular file under root, by its
// path below root.
func readFiles(t *testing.T, root string) map[string]string {
	t.Helper()

	files := map[string]string{}

	for path, entry := range readTree(t, root) {
		rel, err := filepath.Rel(root, path)
		require.NoError(t, err)

		// The entry of a regular file ends with the hash of its
		// content, after the mode.
		_, hash, found := strings.Cut(entry, " ")
		if found {
			files[rel] = hash
		}
	}

	return files
}

// configPath returns the path of the config file of the repository at
// repo. repo is a working tree that holds its git directory, or it is
// a git directory, such as a bare repository.
func configPath(t *testing.T, repo string) string {
	t.Helper()

	gitDir := filepath.Join(repo, ".git")

	info, err := os.Stat(gitDir)
	if err == nil && info.IsDir() {
		return filepath.Join(gitDir, "config")
	}

	require.FileExists(t, filepath.Join(repo, "HEAD"),
		"%s must hold a .git directory, or it must be a git directory", repo)

	return filepath.Join(repo, "config")
}

// appendGitConfig appends text to the config file of the repository
// at repo. Text reaches the keys that git config cannot write, such
// as a hook with an empty name.
func appendGitConfig(t *testing.T, repo, text string) {
	t.Helper()

	require.NoError(t, appendFile(configPath(t, repo), text))
}

// setConfig sets each key of the git config of the repository at repo
// to the value that follows it in pairs. It appends to the config
// file. For a key that the file already sets, the file therefore holds
// both values, as it does after git config --add.
func setConfig(t *testing.T, repo string, pairs ...string) {
	t.Helper()

	require.Zero(t, len(pairs)%2, "setConfig takes each key with its value")

	var text strings.Builder

	for i := 0; i < len(pairs); i += 2 {
		text.WriteString(configText(pairs[i], pairs[i+1]))
	}

	appendGitConfig(t, repo, text.String())
}

// configText returns git config text that sets key to value.
func configText(key, value string) string {
	section, rest, _ := strings.Cut(key, ".")
	header := "[" + section + "]"
	name := rest

	// The subsection of a key lies between its first dot and its last
	// dot, and it can hold dots of its own.
	if i := strings.LastIndex(rest, "."); i >= 0 {
		header = "[" + section + " " + configQuote(rest[:i]) + "]"
		name = rest[i+1:]
	}

	return header + "\n\t" + name + " = " + configQuote(value) + "\n"
}

// configQuote returns value as git reads it between double quotes in
// a config file. The quotes keep each character of a path that starts
// a comment outside them, such as '#'.
func configQuote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}

// replaceInConfig replaces old with text in the config file of the
// repository at repo. The file must hold old exactly one time.
func replaceInConfig(t *testing.T, repo, old, text string) {
	t.Helper()

	path := configPath(t, repo)

	config, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(config), old), "%s must hold %q one time", path, old)

	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(string(config), old, text, 1)), 0o644))
}

// remoteConfig returns git config text that defines the remote name
// with url as its URL, as git remote add writes it.
func remoteConfig(name, url string) string {
	return "[remote " + configQuote(name) + "]\n" +
		originURLLine(url) +
		"\tfetch = +refs/heads/*:refs/remotes/" + name + "/*\n"
}

// originURLLine returns the line that stores url as the URL of a
// remote in a config file. The config file of a clone of
// [initWorkClone] or [initRepos] holds that line for its origin.
func originURLLine(url string) string {
	return "\turl = " + configQuote(url) + "\n"
}

// setOriginURL changes the URL of the origin remote of the clone at
// work from old to url. work is a clone of [initWorkClone] or
// [initRepos], and old is the URL that its origin still has. That URL
// is the path of the bare repository until a test changes it.
func setOriginURL(t *testing.T, work, old, url string) {
	t.Helper()

	replaceInConfig(t, work, originURLLine(old), originURLLine(url))
}

// unsetOriginURL removes old, the URL of the origin remote of the
// clone at work, as [setOriginURL] changes it. The config keeps the
// other keys of the remote, so it defines a remote without a URL.
func unsetOriginURL(t *testing.T, work, old string) {
	t.Helper()

	replaceInConfig(t, work, originURLLine(old), "")
}

// A helper that returns a repository copies a template, so it must
// start no git process after the test binary built the template. The
// repositories of a test then cost no process start, however many
// cases the test has.
//
// The test changes PATH, so it starts the test binary again, and the
// new process runs only this test. probeEnv makes the test call each
// helper two times instead of starting a process. The first call
// builds the template of the helper. The second call runs with a PATH
// that holds no git, so a helper that starts git fails. A
// [templateBuilder] runs git from the path in templateGit, which PATH
// does not change. The second call therefore also runs with a
// templateGit that names no file.
//
// [initAdvancedSubmoduleRepos] copies its template with the code of
// [initSubmoduleRepos], so the test calls only that helper.
func TestFixturesStartNoGit(t *testing.T) { //nolint:paralleltest // The new process calls t.Setenv.
	// probeEnv marks the process that this test started.
	const probeEnv = "MCP_GIT_TEST_FIXTURE_PROBE"

	if os.Getenv(probeEnv) == "" {
		t.Parallel()

		binary, err := os.Executable()
		require.NoError(t, err)

		cmd := exec.CommandContext(t.Context(), binary, "-test.run=^"+t.Name()+"$")

		cmd.Env = append(os.Environ(), probeEnv+"=1")

		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)

		return
	}

	helpers := map[string]func(t *testing.T){
		"initBareRepo": func(t *testing.T) {
			t.Helper()

			initBareRepo(t)
		},
		"initWorkClone": func(t *testing.T) {
			t.Helper()

			initWorkClone(t, initBareRepo(t))
		},
		"initRepos": func(t *testing.T) {
			t.Helper()

			for _, upstream := range []bool{false, true} {
				for _, local := range []bool{false, true} {
					initRepos(t, upstream, local)
				}
			}
		},
		"pushRemoteCommit": func(t *testing.T) {
			t.Helper()

			pushRemoteCommit(t, initBareRepo(t))
		},
		"initDocsRepo and pushDocsUpdate": func(t *testing.T) {
			t.Helper()

			pushDocsUpdate(t, initDocsRepo(t, filepath.Join(t.TempDir(), "bare.git")).bare)
		},
		"initCloneAt": func(t *testing.T) {
			t.Helper()

			initCloneAt(t, initBareRepo(t), filepath.Join(t.TempDir(), "cloned"))
		},
		"initRepo": func(t *testing.T) {
			t.Helper()

			initRepo(t, t.TempDir())
		},
		"initEmptyBare": func(t *testing.T) {
			t.Helper()

			initEmptyBare(t, filepath.Join(t.TempDir(), "bare.git"))
		},
		"initConfigRepo": func(t *testing.T) {
			t.Helper()

			initConfigRepo(t, identityConfig)
		},
		"initOriginRepo": func(t *testing.T) {
			t.Helper()

			initOriginRepo(t, aliasPrefix+"a.git", identityConfig)
		},
		"initRebaseConflict": func(t *testing.T) {
			t.Helper()

			initRebaseConflict(t)
		},
		"initFeatureRepos": func(t *testing.T) {
			t.Helper()

			initFeatureRepos(t)
		},
		"initSubmoduleRepos": func(t *testing.T) {
			t.Helper()

			initSubmoduleRepos(t, "pre-push")
		},
		"config helpers": func(t *testing.T) {
			t.Helper()

			bare, work := initRepos(t, false, false)

			setConfig(t, work, "core.hooksPath", ".husky")
			appendGitConfig(t, work, identityConfig)
			setOriginURL(t, work, bare, aliasPrefix+"a.git")
			unsetOriginURL(t, work, aliasPrefix+"a.git")
		},
	}

	for _, helper := range helpers {
		helper(t)
	}

	t.Setenv("PATH", t.TempDir())

	_, err := exec.LookPath("git")
	require.ErrorIs(t, err, exec.ErrNotFound, "PATH must hold no git")

	// The new process runs only this test, and its subtests run in
	// sequence, so no other goroutine reads templateGit.
	git := templateGit
	templateGit = filepath.Join(t.TempDir(), "git")

	t.Cleanup(func() { templateGit = git })

	_, err = os.Stat(templateGit)
	require.ErrorIs(t, err, os.ErrNotExist, "templateGit must name no file")

	for name, helper := range helpers { //nolint:paralleltest // The parent calls t.Setenv.
		t.Run(name, helper)
	}
}

// The helpers write the config file of a repository without git. git
// must read each key that [setConfig] writes with its value, also when
// the value holds a character that starts a comment or that git
// escapes.
func TestSetConfig(t *testing.T) {
	t.Parallel()

	repo := initRepo(t, t.TempDir())

	// want holds each key in the spelling that git lists, with its
	// value.
	want := map[string]string{
		"mcp-git.plain":                       "value",
		"mcp-git.comment":                     "a # b ; c",
		"mcp-git.spaces":                      "  padded  ",
		"mcp-git.quote":                       `a "quoted" \ value`,
		"mcp-git.empty":                       "",
		"remote.origin.pushurl":               "/tmp/dir#1/bare.git",
		"branch.feat/x.merge":                 "refs/heads/feat/x",
		"http.https://example.invalid/.proxy": "http://proxy.invalid",
		"hook.spaced name.command":            "true",
		`hook.a "quoted" \ name.command`:      "true",
	}

	for key, value := range want {
		setConfig(t, repo, key, value)
	}

	got := map[string]string{}

	// Each entry holds a key, a newline, a value, and a NUL.
	for entry := range strings.SplitSeq(gitOut(t, "-C", repo, "config", "--local", "--list", "-z"), "\x00") {
		key, value, found := strings.Cut(entry, "\n")
		if found && !strings.HasPrefix(key, "core.") {
			got[key] = value
		}
	}

	require.Equal(t, want, got)
}

// [initWorkClone] copies a clone of a template and writes the URL of
// its remote. The copy must be the clone that git clone makes of the
// same bare repository, so that a test of a copy holds for a clone.
func TestWorkCloneMatchesGitClone(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	work := initWorkClone(t, bare)
	cloned := filepath.Join(t.TempDir(), "work")

	runGitCmds(t, [][]string{{"git", "clone", "-q", bare, cloned}})
	appendGitConfig(t, cloned, identityConfig)

	// describe returns the config, the refs, and the status of the
	// clone at repo.
	describe := func(repo string) []string {
		return []string{
			gitOut(t, "-C", repo, "config", "--local", "--list"),
			gitOut(t, "-C", repo, "for-each-ref"),
			gitOut(t, "-C", repo, "status", "--porcelain=v2", "--branch"),
		}
	}

	require.Equal(t, describe(cloned), describe(work))
}
