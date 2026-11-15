// Mcp-git is an MCP server (stdio transport) that exposes git
// operations that contact a remote.
//
// git_clone clones a repository if the destination does not already
// exist, or pulls (fast-forward only) if it does, holding a flock on
// <dest>.lock beside the destination. When the destination holds a
// .git directory that git cannot open, git_clone renames the
// destination to <dest>.corrupt-<timestamp> and clones again.
// git_fetch, git_pull, and git_push operate on an existing local
// repository: they resolve its git directory (worktree-aware), take
// a flock on <gitdir>/mcp-git.lock, resolve the named remote's URL
// so the GH_TOKEN credential helper can authenticate it, and run the
// corresponding git subcommand with captured output. Remote and ref
// inputs are plain names, never URLs or refspecs, so remote deletes
// and force pushes cannot be expressed (git_push offers
// force_with_lease explicitly).
//
// git_fetch, git_pull, and git_push keep git from running the
// commands that the git config of the repository names. The server
// runs with the privileges of the user, outside any sandbox that
// confines the caller. The caller can write the git config of each
// repository under a path that its sandbox lets it write. No tool
// input changes what the Hooks, Config Commands, Remotes, and
// Submodules sections describe. The Limits section names what the
// tools do not cover.
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
// hook by its name.
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
//
// The error names each key. The caller cannot write the system
// config, the global config, or the environment of the server, so
// these three can set the same keys. For a repository that git_pull
// refuses, the caller runs git_fetch, and then it merges or rebases
// inside its sandbox.
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
// # Submodules
//
// git_fetch and git_pull never fetch a submodule, and git_pull never
// updates the working tree of a submodule. Git does both with a git
// command of its own, and that command reads the git config of the
// submodule. The caller fetches a submodule with a git_fetch call of
// its own.
//
// git_push never pushes a submodule. It passes --no-verify, which
// skips the pre-push event. Git does not pass --no-verify on to the
// push that it starts in a submodule. The git config can ask git push
// to recurse into submodules, with push.recurseSubmodules or
// submodule.recurse. In that case git_push makes git check the
// submodules instead. The check refuses a push that references a
// submodule commit that no remote-tracking ref of the submodule
// reaches. The caller then pushes the submodule with a git_push call
// of its own. Otherwise git_push checks no submodule, like a git push
// with the default config.
//
// # Limits
//
// The tools read the git config before git reads it. A caller that
// changes the config between the two reads can define a hook, or set
// a key that the tools refuse. Git then runs the command. The options
// for the fixed keys and the transport limit still hold, because git
// applies them itself.
//
// git_clone lists the hooks of a new clone outside every repository.
// A conditional include of the global config can therefore define a
// hook for the new clone alone, and git runs that hook. A caller that
// writes the git config of the destination during the clone can also
// define a hook, or set a key that git_pull refuses. Git then runs the
// command. When the destination exists, git_clone pulls with the
// limits of git_pull.
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
//     repositories.
package main
