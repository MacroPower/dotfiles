// Mcp-git is an MCP server (stdio transport) that exposes git
// operations that contact a remote.
//
// git_clone clones a repository if the destination does not already
// exist, or pulls (fast-forward only) if it does, holding a flock on
// <dest>.lock beside the destination. When the destination holds a
// .git directory that git cannot open or that has no config file,
// git_clone renames the destination to <dest>.corrupt-<timestamp> and
// clones again. It refuses a destination whose repository has no
// origin remote, and a destination whose origin remote has a URL
// other than the url input. It compares the two URLs after git applies
// the url.<base>.insteadOf rewrites of the git config to each URL. A
// new clone authenticates with the token of GH_TOKEN when git connects
// to github.com over https after those rewrites.
//
// git_fetch, git_pull, and git_push operate on an existing local
// repository. They resolve its git directory (worktree-aware), and
// they take a flock on <gitdir>/mcp-git.lock. They resolve the named
// remote's URL so the GH_TOKEN credential helper can authenticate it,
// and they run the corresponding git subcommand with captured output.
// Remote and ref inputs are plain names, never URLs or refspecs, so a
// caller cannot express a remote delete or a force push (git_push
// offers force_with_lease explicitly).
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
// The flock acquired before the main git invocation is not
// context-aware, so a repository contended by another caller can
// extend the wall-clock duration past --timeout: the deadline
// expires during the flock wait and the next git invocation aborts
// immediately once flock returns.
//
// # Environment
//
//   - GH_TOKEN: when set, configures a git credential helper that
//     authenticates HTTPS requests to github.com using this token.
//     This enables cloning, fetching, pulling, and pushing private
//     repositories. A new clone gives the helper to each https
//     connection to github.com after git applies the URL rewrites of
//     the git config. For a sparse clone without a depth, those
//     connections include the fetch of the files of the sparse
//     paths. The credential helpers of the config files still answer
//     for every other host. Every other call takes the helper when
//     the first URL of the remote starts with https://github.com/
//     after those rewrites.
package main
