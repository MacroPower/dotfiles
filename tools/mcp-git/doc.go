// Mcp-git is an MCP server (stdio transport) that exposes git
// operations that contact a remote.
//
// git_clone clones a repository if the destination does not already
// exist, or pulls (fast-forward only) if it does, holding a flock on
// <dest>.lock beside the destination. It makes the dest input absolute
// first, as filepath.Abs does. Every spelling of one path, such as a
// relative path or a path with a trailing slash, therefore names the
// same destination and the same lock file.
//
// git_clone names the remote of a new clone origin, and it pulls an
// existing destination from origin. clone.defaultRemoteName of the git
// config does not change that name.
//
// When the destination holds a corrupt .git directory, git_clone
// renames the destination to <dest>.corrupt-<timestamp> and clones
// again. A .git directory is corrupt when git reports that it is not a
// git repository, such as a directory without HEAD. It is also corrupt
// when it has no config file, or when git cannot parse a line or a
// value of a config file of the directory. The config files of the
// directory are config, and config.worktree for a sparse clone. A
// crash can cut such a file short, such as inside the boolean of
// core.filemode.
//
// A cut inside the name of an extension in the config file leaves the
// name of an extension that git does not know. git_clone therefore
// counts a .git directory whose config file names such an extension as
// corrupt. A repository that a later git made with a new extension
// moves aside too.
//
// The result of git_clone names the path that the old checkout moved
// to. When the clone after the move fails, the error ends with that
// path, because the destination then does not hold the old checkout.
//
// git can also fail for another reason when git_clone checks the .git
// directory. For example, the server cannot start git, a signal kills
// git, or git cannot parse another config file than those of the .git
// directory. Examples of such files are the global or system config
// and a file that an include directive names. The checkout can then
// be healthy, so git_clone leaves the destination in place. The error
// says that git_clone could not open the repository. It names the
// .git directory, and it ends with the cause.
//
// git_clone refuses a destination whose repository has no origin
// remote, and a destination whose origin remote has a URL other than
// the url input. It compares the two URLs after git applies the
// url.<base>.insteadOf rewrites of the git config to each URL. A new
// clone authenticates with the token of GH_TOKEN when git connects to
// github.com over https after those rewrites.
//
// git_clone also refuses a destination whose origin remote has no URL,
// and the error says that the remote has none. The git config defines
// such a remote when it sets another key of the remote, such as
// remote.origin.pushurl, and stores no URL in remote.origin.url.
//
// git_clone also refuses a destination whose origin remote has a URL
// that git_clone refuses as the url input, and the error says so. An
// http URL is such a URL when the server runs without
// --allow-insecure, and so are a local path and a file:// URL.
// git_clone checks the URL after the rewrites. It does not pull from
// such a URL, whatever the url input holds, so the error does not
// name the URL. The error tells the caller to choose another dest, or
// to delete the directory and retry.
//
// When the origin remote has another URL, the error names both URLs
// before the rewrites. For the origin remote, it names the URL that
// the git config of the destination stores, when git_clone accepts
// that URL as the url input. A call that passes that URL as the url
// input matches the destination. The git config does not always store
// such a URL. For example, a file under .git/remotes can define the
// origin remote. The git config can also store for it a URL that
// git_clone refuses as the url input, such as an alias gh:org/repo
// that a rewrite expands. The error then names another url input that
// matches the destination, such as the URL that the rewrite gives for
// the alias.
//
// git_clone also refuses a destination whose origin remote matches no
// url input that git_clone accepts, and the error says so. For
// example, the git config can hold a second rewrite that changes the
// URL that the first rewrite gives for the alias. git applies the
// first rewrite when it reads the origin remote. It applies the second
// rewrite when a call passes that URL as the url input. The error
// names no URL, and it tells the caller to choose another dest, or to
// delete the directory and retry.
//
// The git config can store a credential in a URL, such as in the base
// of a rewrite. The tool result must not hold that credential, so the
// error names each URL without its password. It also names each URL
// without its user. An ssh URL and an SCP-style URL such as
// git@host:path keep the user, which is a login name.
//
// git_fetch, git_pull, and git_push operate on an existing local
// repository. They resolve its git directory (worktree-aware), and
// they take a flock on <gitdir>/mcp-git.lock. They check each URL of
// the named remote that the call uses, and they run the corresponding
// git subcommand with captured output. Each call authenticates with
// the token of GH_TOKEN when git connects to github.com over https.
// Remote and ref inputs are plain names, never URLs or refspecs, so a
// caller cannot express a remote delete or a force push (git_push
// offers force_with_lease explicitly).
//
// git_fetch, git_pull, and git_push report that a path is not a git
// repository only when git finds no repository from the path. git can
// also find a repository and fail to open it, such as a repository
// whose git config holds a line that git cannot parse. The error then
// says that the tool could not open the repository. It names the path,
// and it ends with the message of git, which gives the cause. The
// tools report a path that names no directory in the same way.
//
// git_fetch, git_pull, and git_push do not trust the git config of
// the repository. The server runs with the privileges of the user,
// outside any sandbox that confines the caller. The caller can write
// the git config of each repository under a path that its sandbox
// lets it write. The tools therefore keep git from running the
// commands that this config names. They also keep the config from
// choosing the working tree, the refs that a push updates, and the
// server that receives a credential. No tool input changes what the
// Hooks, Config Commands, Remotes, Connections, Working Tree, Pushed
// Refs, and Submodules sections describe. The Limits section names
// what the tools do not cover.
//
// # Hooks
//
// The tools never run a hook, because the caller can edit the code
// that a hook runs. They point core.hooksPath at /dev/null, so git
// runs no hook file. Git 2.54 and later also run each hook that the
// git config defines with hook.<name>.command. The tools list those
// hooks in every config file, and they disable each hook by its name.
//
// git_clone runs no hook either. It points core.hooksPath at
// /dev/null, so git runs no hook file of the template directory. The
// git config of a new clone does not exist before the clone. git_clone
// therefore lists the hooks that the system config, the global
// config, and the config of the command line define. It disables each
// hook by its name. A git clone does not read the config of a
// repository that holds the working directory of the server, so the
// list leaves that config out.
//
// A pre-push hook is repository policy, so git_push reports the
// pre-push hooks that it skips. The result of a successful push names
// the executable pre-push hook file and each enabled pre-push hook
// that git config defines. The result also tells the caller to run
// the checks of each hook itself, inside its sandbox.
//
// # Config Commands
//
// Git options and environment variables turn off each command that
// the git config names under a fixed key:
//
//   - core.fsmonitor and core.alternateRefsCommand get a value on the
//     command line. A fetch therefore lists no ref of an alternate
//     object store, and it can download an object that the alternate
//     holds.
//   - core.sshCommand, core.askPass, core.gitProxy, core.editor, and
//     sequence.editor lose to the variables GIT_SSH_COMMAND,
//     GIT_ASKPASS, GIT_PROXY_COMMAND, GIT_EDITOR, and
//     GIT_SEQUENCE_EDITOR. The server sets both editor variables to
//     true, and it sets each other variable that its own environment
//     does not set.
//   - maintenance.auto is false, so no command starts git
//     maintenance. Maintenance runs the pre-auto-gc hook and the
//     commands of gc.recentObjectsHook.
//   - --upload-pack and --receive-pack name the programs that serve a
//     fetch and a push. They win over remote.<name>.uploadpack and
//     remote.<name>.receivepack for the remote of the call.
//   - git_push passes --no-signed, so push.gpgSign does not start the
//     program of gpg.program.
//
// Git has no option that turns off the commands of the other keys.
// The config chooses a part of the name of such a key, or the key
// holds a list. The tools read the git config, and they refuse a
// repository whose own config files set one of these keys:
//
//   - git_fetch, git_pull, and git_push refuse credential.helper,
//     credential.<url>.helper, remote.<name>.uploadpack, and
//     remote.<name>.receivepack.
//   - git_pull also refuses the clean, smudge, and process keys of
//     filter.<name>, merge.<name>.driver, gpg.program,
//     gpg.<format>.program, and gpg.ssh.defaultKeyCommand. A pull
//     updates the working tree and can write commits, so it reads
//     these keys.
//   - git_pull also refuses keys of git-lfs. The clean and smudge keys
//     of lfs.extension.<name> name a command, and so do the path and
//     args keys of lfs.customtransfer.<name>. lfs.storage names the
//     directory where git-lfs writes each object that it downloads,
//     and that directory can lie outside every --allow-dir. When the
//     global config defines git-lfs as a filter, git-lfs reads these
//     keys from the config of the repository during a pull.
//
// The error names each key. The caller cannot write the system
// config, the global config, or the environment of the server, so
// these three can set the same keys. For a repository that git_pull
// alone refuses, the caller runs git_fetch, and then it merges or
// rebases inside its sandbox.
//
// # Remotes
//
// The tools reach a remote over https and ssh. With --allow-insecure,
// they also reach it over http and the git protocol. git_fetch,
// git_pull, and git_push check each URL of the remote that the call
// uses, as git_clone checks its url input. The git config can rewrite
// a URL, and it can name the transport of a remote. The tools
// therefore set GIT_ALLOW_PROTOCOL, and git refuses every other
// transport for each URL that it resolves. The ext transport, which
// runs a command from the URL, is one of the transports that git
// refuses.
//
// The tools also refuse a remote on the local file system, such as a
// path or a file:// URL. Git reaches such a repository by starting
// upload-pack or receive-pack in it. That process runs the hooks and
// the config commands of the other repository, because it does not
// inherit the options of the server.
//
// git_fetch, git_pull, and git_push also refuse a remote that has no
// URL. The git config defines such a remote when it sets another key
// of the remote, such as remote.<name>.fetch, and stores no URL in
// remote.<name>.url. The error says that the remote has no URL, and it
// names that key. git pushes to the push URLs of a remote, so git_push
// refuses the remote only when remote.<name>.pushurl stores no URL
// either. Its error names both keys.
//
// git_fetch, git_pull, and git_push report an unknown remote only when
// git finds no remote of that name in the repository. git can also
// fail to read a remote that the git config defines, such as a remote
// whose remote.<name>.url key has no value. The error then says that
// the tool could not read the remote, and it names the remote and the
// repository.
//
// # Connections
//
// Git sends a credential over the https connection to the host of the
// remote. The credential is the token of GH_TOKEN for github.com, or
// the answer of a credential helper of the global config. The config
// of a repository can point that connection at another server. An
// http key can name a proxy, an address for the host, or the
// certificates that git trusts.
//
// Git has no option that overrides an http key for every URL, because
// a key of the form http.<url>.<name> wins over http.<name>.
// git_fetch, git_pull, and git_push therefore refuse a repository
// whose own config files set a key under http, whatever its value is.
// They also refuse remote.<name>.proxy and
// remote.<name>.proxyAuthMethod, which win over http.proxy. The error
// names each key. The system config, the global config, and the
// environment of the server can set the same keys.
//
// # Working Tree
//
// git_fetch, git_pull, and git_push check these paths against
// --allow-dir: the path of the call, the common git directory, and
// the working tree. The git config of the repository can name the
// working tree with core.worktree. The tools therefore ask git for
// the working tree, and they refuse a working tree outside every
// --allow-dir. They pass the path that they checked to git with
// --work-tree, which wins over core.worktree. git_pull refuses a
// repository that has no working tree, such as a bare repository.
//
// The tools take a repository for one without a working tree only
// when git says that it has none. git can also fail to name the
// working tree. For example, a signal kills git, or the server cannot
// start git. The tool then fails before it contacts the remote. The
// error names the repository, and it ends with the cause.
//
// An --allow-dir can hold paths that the sandbox of the caller cannot
// write, such as another checkout or the hooks directory of its git
// directory. Git runs a file in a hooks directory as a hook. The
// working tree must therefore hold the git directory of the
// repository. Git must find that git directory from the working tree,
// through a .git directory or a .git file.
//
// A sandbox can let the caller write a git directory but not its
// hooks, such as the git directory of the main checkout for a session
// in a linked worktree. The caller can then write a .git file there
// that points git at its own git directory. It can also move aside
// HEAD and the other entries that mark a git directory. The git
// directory of each checkout has the name .git, so the tools refuse a
// working tree when a component of its path is .git. The comparison
// ignores case, because the file system of macOS ignores case. The
// tools also refuse a working tree that is a git directory with
// another name, such as a bare repository, or that lies inside one.
//
// git_clone sets the paths of a sparse checkout with a second git
// command after the clone. The caller can write the git config of the
// new clone before that command runs. git_clone therefore passes the
// destination to git with --work-tree, and git writes the files of the
// sparse checkout in the destination.
//
// # Pushed Refs
//
// git_push updates the branch or the tag that the call names, or the
// current branch when the call names no ref. It updates the ref of
// the remote that has the same full name. A call that names no ref
// and sets tags pushes the tags alone, as git push --tags does.
//
// The git config of the repository cannot name another ref. git_push
// passes a refspec that names the full ref on both sides. Git maps a
// plain name through remote.<name>.push, push.default, and the
// upstream of the branch, and it takes a refspec with two sides as it
// is. git_push also sets remote.<name>.mirror to false, and it passes
// --no-follow-tags, which wins over push.followTags.
//
// git_push sends no push option. Git sends each push option to the
// remote, and the server acts on it. A GitLab server can create a
// merge request for merge_request.create, and it can merge that
// request into another branch. git_push sets push.pushOption to an
// empty value, which clears the push options that the config files
// set.
//
// Git applies push.autoSetupRemote only to a push without a refspec,
// so git_push applies the key itself. When the key is true, a call
// that names no ref records the upstream of a branch that has none.
// The upstream is a setting of the local repository.
//
// A call that names no ref fails when HEAD names no branch. A call
// fails when its ref names both a branch and a tag. The caller then
// names refs/heads/<name> or refs/tags/<name>.
//
// # Submodules
//
// git_fetch and git_pull never fetch a submodule, and git_pull never
// updates the working tree of a submodule. Git does both with a git
// command of its own, and that command reads the git config of the
// submodule. The caller fetches a submodule with a git_fetch call of
// its own.
//
// git_push never pushes a submodule, and it never checks one. It sets
// push.recurseSubmodules to no, which also wins over
// submodule.recurse. With on-demand or only, git starts a push in
// each submodule, and it does not pass --no-verify on to that push.
// With check, git looks for each submodule commit that the push
// references. It fetches a missing commit from the promisor remote of
// a submodule that is a partial clone. Each of these commands reads
// the git config of the submodule, which the tools do not read, and
// that config can name a credential helper. The caller pushes each
// new submodule commit with a git_push call of its own, before it
// pushes the commit that references it.
//
// # Limits
//
// The tools read the git config before git reads it. A caller that
// changes the config between the two reads can define a hook, or set
// a key that the tools refuse. Git then runs the command, or it
// applies the key. The options for the fixed keys, the transport
// limit, the working tree, and the refspec of a push still hold,
// because git applies them itself.
//
// The tools also check each path before git opens it, and git follows
// a symbolic link in a path. A caller can replace a directory on the
// path of the working tree with a symbolic link between the check and
// the git command. Git then writes the working tree where the link
// points, which can lie outside every --allow-dir.
//
// The tools recognize a git directory with a name other than .git
// only by its HEAD, objects, refs, and commondir entries. The command
// git init --separate-git-dir creates such a git directory. A caller
// that can write that git directory can move these entries aside, and
// the tools then allow a working tree inside it.
//
// git_clone lists the hooks of a new clone outside every repository.
// A conditional include of the global config can therefore define a
// hook for the new clone alone, and git runs that hook.
//
// A caller that writes the git config of the destination during the
// clone can also define a hook, or set a key that git_pull refuses.
// Git then runs the command, or it applies the key. A key under http
// can point the connection that carries the token of GH_TOKEN at
// another server. The working tree of a sparse checkout still holds,
// because git_clone names it on the command line. When the
// destination exists, git_clone pulls with the limits of git_pull.
//
// A filter program of the global config reads keys of its own from
// the config of the repository. The server cannot know the keys of
// every filter program, so git_pull refuses only the keys of git-lfs
// that run a command or name where git-lfs writes. git-lfs still
// reads its other keys. Another filter program still reads every key
// of its own.
//
// # Flags
//
//   - --allow-dir: restrict repository and destination paths to
//     subdirectories of this path (repeatable; if omitted, all paths
//     are allowed)
//   - --allow-insecure: permit unencrypted URL schemes (http, git)
//   - --timeout: max duration for a single git operation (0 disables)
//
// The server resolves the symbolic links of each --allow-dir when it
// starts, and it resolves the links of each path that it checks. An
// --allow-dir with a symbolic link on its path, such as /tmp/git on
// macOS, therefore allows the paths under the directory that the
// link leads to. The --allow-dir can name a directory that does not
// exist when the server starts. It can also pass through a symbolic
// link whose target does not exist. It then names the same directory
// before and after someone creates that target.
//
// The server reads an absolute --allow-dir one element at a time, as
// the OS reads a path. A .. after a symbolic link therefore leads to
// the parent of the directory that the link leads to, whether or not
// that directory or the elements after the .. exist. A .. after an
// element that does not exist drops that element. The tools drop the
// element before each .. of a path as text, so the same spelling might
// name another directory in a tool call.
//
// The server reads a relative --allow-dir, such as repos or a single
// dot, against its working directory when it starts. It reads the
// value as the tools read a relative path, with the rules of
// filepath.Abs in the Go standard library. The value therefore starts
// from $PWD when $PWD names the working directory. Each .. in the
// value drops the element before it as text, also when that element
// is a symbolic link.
//
// The server exits with an error when it starts with an empty
// --allow-dir. It also exits when it cannot find its working
// directory for a relative --allow-dir, as happens after someone
// deletes that directory.
//
// The flock acquired before the main git invocation is not
// context-aware, so a repository contended by another caller can
// extend the wall-clock duration past --timeout. The deadline then
// expires during the flock wait, and the next git invocation aborts
// immediately after flock returns.
//
// # Environment
//
//   - GH_TOKEN: when set, configures a git credential helper that
//     authenticates HTTPS requests to github.com using this token.
//     This enables cloning, fetching, pulling, and pushing private
//     repositories. Every tool gives the helper to each https
//     connection to github.com after git applies the URL rewrites of
//     the git config. For a push, those connections include the
//     connection to each push URL of the remote that names github.com
//     over https. For a sparse clone without a depth, they include the
//     fetch of the files of the sparse paths. The credential helpers
//     of the config files still answer for every other host.
//   - GIT_DIR, GIT_WORK_TREE, and the other variables that point git
//     at one repository: the server removes them from the environment
//     of its git commands. With GIT_DIR set, git acts on the
//     repository that the variable names, whatever path the command
//     gives. Without the variables, every tool acts on the repository
//     at the path of its call. git sets some of the variables for
//     each command that it starts, such as a hook, and a server that
//     the command starts inherits them. git rev-parse
//     --local-env-vars prints the variables. The server keeps two
//     variables of that list, GIT_CONFIG_PARAMETERS and
//     GIT_CONFIG_COUNT. They pass config to every git command, and
//     they name no repository.
//   - GIT_REFERENCE_BACKEND: the server removes this variable as well,
//     although git 2.56 leaves it out of the output of git rev-parse
//     --local-env-vars. The variable names a ref store, and git reads
//     HEAD and every ref from that store, whatever path the command
//     gives.
//   - GIT_CONFIG: the server removes this variable as well. With the
//     variable set, a git config command without a file option reads
//     only the file that the variable names. Every other git command
//     ignores the variable. The tools read the git config of a
//     repository with git config. Without the variable, they read the
//     config files that a fetch, a pull, and a push read.
package main
