{
  pkgs,
  lib,
  config,
  osConfig ? { },
  ...
}:

let
  inherit (lib) mkOption types;
  inherit (config.lib.stylix) colors;
  inherit (import ../lib/colors.nix { inherit lib; }) lighten;
  cfg = config.dotfiles.claude;

  # Resolved from the host config (nix-darwin / NixOS) when home-manager
  # runs as a host module. Standalone home-manager (linux containers,
  # Dagger) has no `osConfig.users`, so the lookup returns null and the
  # uid-scoped paths drop out of the lists below.
  hostUid = lib.attrByPath [ "users" "users" config.home.username "uid" ] null osConfig;

  # The claude wrapper passes this directory to Claude Code as
  # CLAUDE_CODE_TMPDIR. Claude Code keeps its temporary files in the
  # directory claude-<uid> there. That directory holds the files of
  # each session as <project>/<session>, which are the scratchpad of
  # the session and the output of its background commands.
  #
  # A Linux host keeps the directory in /tmp. Without the variable,
  # Claude Code on Linux follows TMPDIR, so a session that starts in a
  # shell with another TMPDIR keeps its files elsewhere. On a NixOS
  # host, hosts/nixos/claude-tmp.nix keeps the daily cleanup of /tmp out
  # of /tmp/claude-<uid> and removes each old session directory as a
  # whole.
  #
  # A Darwin host cannot keep its cleanup of /tmp out of a directory,
  # as the comment on cloneRoot describes. That cleanup deletes each
  # file that nothing read or changed in the last three days. In /tmp,
  # a session that lives for more than three days loses single files of
  # its scratchpad and keeps the others. So a Darwin host keeps the
  # directory in ~/Library/Caches, which macOS does not clean. The
  # expire-claude-tmp agent of this module removes each old session
  # directory there as a whole. Time Machine leaves ~/Library/Caches
  # out of its backups.
  claudeTmpDir =
    if pkgs.stdenv.hostPlatform.isDarwin then
      "${config.home.homeDirectory}/Library/Caches/claude-tmp"
    else
      "/tmp";

  # Claude Code gives a sandboxed command the directory claude-<uid> as
  # TMPDIR. A Unix socket cannot have a long path. So when the path of
  # that directory is long, Claude Code gives the command
  # /tmp/claude-<uid> instead. Claude Code 2.1.319 does so for a path
  # of more than 44 bytes, and the directory of a Darwin host has such
  # a path. So the sandbox write paths hold /tmp/claude-<uid> on every
  # host. /tmp is a link to /private/tmp on macOS.
  #
  # Claude Code creates /tmp/claude-<uid> once for each process, so each
  # sandboxed command of a session fails after the directory is gone.
  # On a Darwin host, that directory holds only the short-lived files of
  # commands, so it often stays empty. The cleanup of /tmp deletes an
  # empty directory that nothing modified in three days. So the
  # expire-claude-tmp agent of this module creates the directory and
  # sets its modification time on each run.
  claudeTmpPaths = lib.optionals (hostUid != null) (
    lib.unique [
      "${claudeTmpDir}/claude-${toString hostUid}"
      "/tmp/claude-${toString hostUid}"
      "/private/tmp/claude-${toString hostUid}"
    ]
  );

  sopsEnabled = config.dotfiles.sops.enable;
  skipPerms = cfg.dangerouslySkipPermissions;

  # Claude Code's built-in sandbox. Darwin-only; the Linux backend is
  # unverified, and `failIfUnavailable = true` would break startup.
  sandboxEnabled = pkgs.stdenv.hostPlatform.isDarwin;

  # True when something contains the process: Claude Code's sandbox on
  # Darwin, or the Lima VM on terrarium. Drives `--auto-allow` on
  # hook-router, which lets the bash hook emit PreToolUse "allow" and
  # skip the static analyzer prompt on shell expansions. See
  # tools/hook-router/main.go config.autoAllow.
  #
  # Claude Code 2.1.299 and later ignore that "allow" in plan mode for a
  # command that is not read-only. The wrapper therefore also passes
  # --allow-dangerously-skip-permissions here, because plan mode stops
  # prompting for commands when bypassPermissions is in the mode cycle.
  # Plan mode then approves file-tool writes too, so hook-router's
  # --write-guard asks before one lands outside the sandbox write paths.
  autoAllowEnabled = sandboxEnabled || config.dotfiles.hostname == "terrarium";

  # Returns the sops secret path when sops is enabled, or a nonexistent
  # path when disabled.  Nix is lazy so the `then` branch (which accesses
  # config.sops.secrets) is never evaluated when sopsEnabled is false.
  secretPath =
    name: if sopsEnabled then config.sops.secrets.${name}.path else "/run/secrets/disabled";

  # Generate shell snippet that exports an env var from a sops secret file.
  exportSecret = envVar: secretName: ''
    if [ -f "${secretPath secretName}" ]; then
      export ${envVar}="$(cat "${secretPath secretName}" 2>/dev/null || true)"
    fi
  '';

  # Generate shell snippets for multiple env-var-to-secret mappings.
  exportSecrets = mappings: lib.concatStrings (lib.mapAttrsToList exportSecret mappings);

  claudePowerlineConfig = builtins.toJSON {
    inherit (cfg.powerline) theme;
    colors.${cfg.powerline.theme} = cfg.powerline.colors;
    display = {
      inherit (cfg.powerline.display)
        style
        charset
        colorCompatibility
        autoWrap
        padding
        lines
        ;
    };
    inherit (cfg.powerline) budget;
  };

  urlMatchOptions = {
    scheme = mkOption {
      type = types.str;
      default = "";
      description = "Regex pattern matching the URL scheme. Empty string means no constraint.";
    };
    host = mkOption {
      type = types.str;
      default = "";
      description = "Regex pattern matching the URL host. Empty string means no constraint.";
    };
    path = mkOption {
      type = types.str;
      default = "";
      description = "Regex pattern matching the URL path. Empty string means no constraint.";
    };
    query = mkOption {
      type = types.str;
      default = "";
      description = "Regex pattern matching the URL query string. Empty string means no constraint.";
    };
    fragment = mkOption {
      type = types.str;
      default = "";
      description = "Regex pattern matching the URL fragment. Empty string means no constraint.";
    };
  };

  urlMatchType = types.submodule { options = urlMatchOptions; };

  powerlineColorType = types.submodule {
    options = {
      bg = mkOption {
        type = types.str;
        description = "Background color for the segment. Any color string accepted by claude-powerline (e.g. `#rrggbb`).";
      };
      fg = mkOption {
        type = types.str;
        description = "Foreground color for the segment. Any color string accepted by claude-powerline (e.g. `#rrggbb`).";
      };
    };
  };

  powerlineSegmentType = types.submodule {
    freeformType = types.attrsOf types.anything;
    options.enabled = mkOption {
      type = types.bool;
      default = true;
      description = "Whether this segment renders.";
    };
  };

  mkBudgetType =
    defaults:
    types.submodule {
      options = {
        amount = mkOption {
          type = types.int;
          default = defaults.amount;
          description = "Budget limit. Units are interpreted per `type`.";
        };
        type = mkOption {
          type = types.enum [
            "tokens"
            "cost"
          ];
          default = defaults.type;
          description = "Budget unit.";
        };
        warningThreshold = mkOption {
          type = types.ints.between 0 100;
          default = 80;
          description = "Percent of budget at which to switch to the warning color.";
        };
      };
    };

  denyRuleType = types.submodule {
    options = urlMatchOptions // {
      reason = mkOption {
        type = types.nonEmptyStr;
        description = "Human-readable reason shown when a matching URL is denied.";
      };
      except = mkOption {
        type = types.listOf urlMatchType;
        default = [ ];
        description = "URL patterns exempted from this deny rule.";
      };
    };
  };

  # Bash command rule, evaluated by hook-router on PreToolUse:Bash.
  # Used for both deny rules (block the command) and ask rules (force
  # a permission prompt); the action is set by list membership when
  # the rules are serialized for the hook-router wrapper. See
  # tools/hook-router/cmdrules/cmdrules.go Rule for matching
  # semantics; the JSON tags there must stay aligned with these option
  # names.
  commandRuleType = types.submodule {
    options = {
      command = mkOption {
        type = types.nonEmptyStr;
        description = ''
          Executable name matched literally against the first word of
          the call.
        '';
      };
      args = mkOption {
        type = types.listOf types.nonEmptyStr;
        default = [ ];
        description = ''
          Positional arguments that must follow `command`, matched
          literally in order. With `command = "git"`, leading
          top-level git flags are skipped before matching; other
          commands match strictly from position 1. An empty list
          matches every invocation of `command`, so set a non-empty
          list to scope the rule to a specific subcommand.
        '';
      };
      except = mkOption {
        type = types.listOf types.nonEmptyStr;
        default = [ ];
        description = ''
          Literal arguments that exempt the call when they appear as
          the next argument after `args`. Lets `git stash pop` through
          while still denying `git stash push`. No flag-skipping is
          applied to the candidate slot, so intervening flags break
          the exemption. Bare `command + args` calls (no further
          arguments) fire regardless of `except` unless `exceptBare`
          is set.
        '';
      };
      exceptBare = mkOption {
        type = types.bool;
        default = false;
        description = ''
          Whether a bare `command + args` call (no further arguments)
          is exempt. Defaults to false because a bare `git stash` is
          still a save form; set it for groups whose bare form is a
          read, such as `git remote`.
        '';
      };
      onlyRemoteURL = mkOption {
        type = types.bool;
        default = false;
        description = ''
          Whether the rule matches only a call that names a remote URL
          after `args`. A remote URL is a word with a `:` before its
          first `/`, such as `https://host/repo` or `git@host:repo`. A
          `file://` URL is not a remote URL, and neither is an option.
          Only the text before the first expansion of a word counts, so
          `"$url"` is not a remote URL. Set the option for a command
          that always names its repository, such as `git clone`. A
          call between local paths then runs.
        '';
      };
      exceptLocalPath = mkOption {
        type = types.bool;
        default = false;
        description = ''
          Whether a call that names a local path after `args` is
          exempt. A local path is a word that starts with `/`, `./`,
          `../`, `~`, or `file://`, or the word `.` or `..`. Only the
          text before the first expansion of a word counts, so
          `"$dir/repo"` is not a local path. Set the option for a
          command that reads a plain word as the name of a remote, such
          as `git fetch`. A call that names a local repository by its
          path then runs.
        '';
      };
      reason = mkOption {
        type = types.nonEmptyStr;
        description = "Message shown when the rule denies a command or asks for confirmation.";
      };
    };
  };

  # A path-to-command routing rule shared by the formatter and linter
  # engines in hook-router. The matched file path is appended as the
  # final argv element when the rule's command runs. Option names are
  # camelCase because `builtins.toJSON` emits attribute names verbatim
  # and the Go struct tags expect camelCase. `commandDescription`
  # explains the command's contract and `defaultTimeout` is the budget
  # the engine falls back to.
  mkPathCommandRuleType =
    {
      commandDescription,
      defaultTimeout,
    }:
    types.submodule {
      options = {
        pathGlob = mkOption {
          type = types.nonEmptyStr;
          description = ''
            Absolute file-path glob evaluated with doublestar v4
            (`doublestar.PathMatch`), OS-separator aware like the older
            `filepath.Match`. `**` crosses path separators only when it
            occupies a full segment, so `/a/**/*.md` is recursive while
            `/a/**foo` is not (the `**` degrades to a single `*`). Brace
            alternation (`*.{md,go}`) is supported. Tilde expansion does
            not happen at runtime, so the Nix evaluator must produce the
            resolved absolute path (e.g.
            `''${config.home.homeDirectory}/.claude/plans/*.md`).
          '';
        };
        command = mkOption {
          type = types.listOf types.nonEmptyStr;
          description = commandDescription;
        };
        timeout = mkOption {
          type = types.str;
          default = defaultTimeout;
          description = ''
            Per-invocation wall-clock budget (Go time.ParseDuration
            syntax). Defaults to ${defaultTimeout}; malformed values
            silently fall back to the default.
          '';
        };
      };
    };

  # File-formatter routing rule, evaluated by hook-router on
  # PostToolUse:Write/Edit/MultiEdit for the written file and on
  # PostToolUse:Bash for each file the command changed. See
  # tools/hook-router/formatter/formatter.go Rule for matching
  # semantics.
  formatterRuleType = mkPathCommandRuleType {
    defaultTimeout = "5s";
    commandDescription = ''
      Formatter argv. The matched file path is appended as the final
      argument, so the binary must accept a path positional. Exit codes
      are logged and otherwise ignored.
    '';
  };

  # File-linter routing rule, evaluated by hook-router on
  # PostToolUse:Write/Edit/MultiEdit after the formatter has run. See
  # tools/hook-router/linter/linter.go Rule for matching semantics.
  linterRuleType = mkPathCommandRuleType {
    defaultTimeout = "10s";
    commandDescription = ''
      Linter argv. The matched file path is appended as the final
      argument, so the binary must accept a path positional. The
      command follows one exit-code contract. 0 means the file is
      clean. 1 means findings, one `path:line:col: message` line each
      on stdout, which hook-router returns to Claude through exit code
      2 (a Write reports every finding, an Edit those on the lines it
      wrote and those it created elsewhere in the file). Any other
      code is a crash, logged and swallowed.
    '';
  };

  # MCP tool pattern to the top-level tool_input keys whose string
  # values hook-router lints on PreToolUse. See
  # tools/hook-router/msglint/msglint.go ToolField for matching semantics.
  proseToolFieldsType = types.attrsOf (types.listOf types.nonEmptyStr);

  # Conditional allow rule for one MCP tool, evaluated by hook-router on
  # PreToolUse. See the Test constants and Condition in
  # tools/hook-router/mcprules/rule.go for the matching semantics. The
  # JSON tags of Condition there, and the tool and conditions tags in
  # Parse in tools/hook-router/mcprules/mcprules.go, must stay aligned
  # with these option names.
  conditionalRuleType = types.submodule {
    options = {
      tool = mkOption {
        type = types.strMatching "mcp__[^*]+__[^*]+";
        description = ''
          Exact name of the MCP tool that the rule covers, such as
          `mcp__git__git_push`. The type rejects a bare server name and
          a glob.
        '';
      };
      conditions = mkOption {
        type = types.nonEmptyListOf (
          types.submodule {
            options = {
              field = mkOption {
                type = types.nonEmptyStr;
                description = "Top-level `tool_input` key that the condition reads.";
              };
              test = mkOption {
                type = types.enum [
                  "matches"
                  "branchMatches"
                  "true"
                  "absentOrTrue"
                  "absentOrZero"
                  "absent"
                  "pullRequestRecorded"
                ];
                description = ''
                  Test that the field must pass. A field set to null
                  counts as absent.

                  - `matches`: the field is a string that `pattern`
                    matches from start to end.
                  - `branchMatches`: the field names a branch that
                    `pattern` matches from start to end. When the field
                    is absent or an empty string, hook-router asks git
                    for the current branch of the repository at the
                    path in the `repoField` field, and tests that
                    branch. The test fails when git names no current
                    branch there, as on a detached HEAD, and when the
                    path is relative. hook-router reads the branch
                    without `GIT_DIR`, `GIT_COMMON_DIR`,
                    `GIT_REFERENCE_BACKEND`, and the other variables of
                    its environment that name a repository. The MCP
                    server of the rule must ignore these variables too.
                    Otherwise the server can act on a branch that the
                    test did not read, or on the remotes of another
                    repository.
                  - `true`: the field is the boolean true.
                  - `absentOrTrue`: the field is absent or the boolean
                    true.
                  - `absentOrZero`: the field is absent, false, an empty
                    string, or an empty list.
                  - `absent`: the field is absent. Use it for a field
                    that changes something at every value, false and
                    the empty string included.
                  - `pullRequestRecorded`: the field is the number of a
                    pull request that an agent opened from this host.
                    The CreatePullRequest entry of the PostToolUse
                    hooks records each pull request that the
                    `create_pull_request` tool of the GitHub MCP server
                    opens. hook-router keeps the record in its state
                    database, and every session on the host reads it.
                    The test reads the owner and the name of the
                    repository from the `owner` and `repo` fields of
                    the call, and compares both without regard to case.
                    It fails when hook-router cannot read the database.
                '';
              };
              pattern = mkOption {
                type = types.str;
                default = "";
                description = ''
                  Regular expression of the `matches` and
                  `branchMatches` tests, in Go syntax. Each test holds
                  only when the expression matches the whole value. The
                  build of the hook-router wrapper fails when the
                  expression does not compile. Every other test takes
                  no pattern.
                '';
              };
              repoField = mkOption {
                type = types.str;
                default = "";
                description = ''
                  Top-level `tool_input` key that holds the path of a
                  git repository, for the `branchMatches` test. Every
                  other test takes no `repoField`.
                '';
              };
            };
          }
        );
        description = ''
          Conditions on the arguments of a call. The call runs without a
          prompt when every condition holds.
        '';
      };
    };
  };

  # Conventional Commits types that can open a branch name. The
  # conditional rules let a push to a branch named `<type>/<name>`, and
  # a draft pull request from one, run without a prompt. By convention
  # such a name marks a working branch, so `main` and every other
  # branch keep the prompt. `BRANCH_TYPES` in
  # configs/claude/skills/aven-ultraclose/scripts/ultraclose.py copies
  # these types, so update both together.
  conventionalBranchTypes = [
    "build"
    "chore"
    "ci"
    "docs"
    "feat"
    "fix"
    "perf"
    "refactor"
    "revert"
    "style"
    "test"
  ];

  # Regular expression, in Go syntax, for the name of such a branch.
  # Every conditional rule that tests a branch name reads it from here.
  # Mirrored by conventionalBranchPattern in
  # tools/hook-router/mcp_test.go, so update both together.
  conventionalBranchPattern = "^(${lib.concatStringsSep "|" conventionalBranchTypes})/.+$";

  # The types as a list of inline code, for the bundle instructions
  # that tell Claude how to name a branch.
  conventionalBranchTypeList = lib.concatMapStringsSep ", " (
    type: "`${type}`"
  ) conventionalBranchTypes;

  toPermGlob =
    path:
    let
      expanded =
        if lib.hasPrefix "~" path then config.home.homeDirectory + lib.removePrefix "~" path else path;
    in
    "/${expanded}/**";

  cleanAttrs = lib.filterAttrs (_: v: v != "" && v != [ ]);
  cleanRule =
    rule:
    let
      cleaned = cleanAttrs rule;
    in
    if cleaned ? except then cleaned // { except = map cleanAttrs cleaned.except; } else cleaned;

  # Runs `workmux set-window-status <status>` for the Claude Code hooks.
  # Each call loads the workmux config, which spawns two or three git
  # processes, and then spawns about six tmux processes. PostToolUse fires
  # after every tool call and passes --dedupe, which skips the call when
  # this pane last sent the same status less than 30 seconds ago. The
  # status stays "working" through a turn, so the skip changes nothing
  # the sidebar shows. workmux keeps status_ts while the status is
  # unchanged, and UserPromptSubmit, which never dedupes, still clears an
  # interrupted mark at the start of each turn.
  workmuxStatus = pkgs.writeShellApplication {
    name = "workmux-status";
    runtimeInputs = [ pkgs.coreutils ];
    text = ''
      status=$1
      dedupe=''${2:-}
      # Host panes carry TMUX_PANE. A sandboxed agent in the Lima guest
      # does not, but each one gets its own WM_RPC_PORT.
      key=''${TMUX_PANE:-''${WM_RPC_PORT:-}}
      if [ -n "$key" ]; then
        dir="''${XDG_RUNTIME_DIR:-''${TMPDIR:-/tmp}}/workmux-status"
        file="$dir/''${key//[^A-Za-z0-9]/_}"
        printf -v now '%(%s)T' -1
        last="" ts=""
        [ -r "$file" ] && { read -r last ts <"$file" || true; }
        if [ "$dedupe" = --dedupe ] && [ "$last" = "$status" ] &&
          [[ $ts =~ ^[0-9]+$ ]] && [ $((now - ts)) -lt 30 ]; then
          exit 0
        fi
        [ -d "$dir" ] || mkdir -p "$dir"
        printf '%s %s\n' "$status" "$now" >"$file"
      fi
      exec ${lib.getExe' pkgs.workmux-bin "workmux"} set-window-status "$status"
    '';
  };

  # Grants a workmux-created linked worktree's Claude session write access to
  # the shared parent .git so git add/commit/rebase/push work under the macOS
  # Seatbelt sandbox (the sandbox allowlists the worktree cwd but not the
  # parent repo's .git; anthropics/claude-code#64792). hooks/ and config stay
  # write-denied. Merges into (never overwrites) the
  # .claude/settings.local.json that files.copy just seeded from the parent
  # checkout.
  worktreeAllowGitWrites = pkgs.writeShellScript "workmux-worktree-allow-git-writes" ''
    set -euo pipefail
    cd "''${WM_WORKTREE_PATH:-$PWD}"
    common_dir=$(git rev-parse --path-format=absolute --git-common-dir)
    settings=".claude/settings.local.json"
    mkdir -p .claude
    [ -f "$settings" ] || printf '{}' > "$settings"
    tmp=$(mktemp)
    ${pkgs.jq}/bin/jq \
      --arg gitdir "$common_dir" \
      '.sandbox.filesystem.allowWrite =
          ((.sandbox.filesystem.allowWrite // []) + [$gitdir] | unique)
        | .sandbox.filesystem.denyWrite =
          ((.sandbox.filesystem.denyWrite // [])
            + [$gitdir + "/hooks", $gitdir + "/config"] | unique)' \
      "$settings" > "$tmp"
    mv "$tmp" "$settings"
  '';

  workmuxConfig = (pkgs.formats.yaml { }).generate "config.yaml" {
    nerdfont = true;
    merge_strategy = "rebase";
    # Start independent worktrees from the repo's effective main branch
    # (configured main_branch or detected default) instead of whatever
    # branch happens to be checked out, so spawning a task while sitting
    # on a feature branch doesn't silently stack unrelated work on it.
    # Explicit --base and --pr checkouts still take precedence.
    base_branch = "auto";
    inherit (cfg.workmux) agent;
    # Named agent entry consumed by the focused pane's <agent> placeholder.
    # The structured agent path re-derives the command on every render, so
    # workmux's double render_command() stays idempotent and agent mutations
    # (sandbox skip-permissions flag, --fork/--continue resume args) apply.
    # A raw pane command string is rendered twice, which double-wraps it in
    # sh -c and breaks quoting under the devbox toolchain wrapper.
    agents.${cfg.workmux.agent} = cfg.workmux.command;
    window_prefix = "wm-";
    worktree_dir = ".worktrees";
    status_format = false;
    status_icons = {
      working = "󱚣";
      waiting = "󰍻";
      sleeping = "󰤄";
      done = "󰄴";
    };
    sidebar = {
      # Order rows by attention: waiting, done, working, then sleeping.
      # The default "recency" ordering moves rows on every status change.
      sort = "priority";
      # Section the list per repo. Stale agents fold behind each group's
      # toggle (collapse_stale defaults to true). The `t` key switches
      # grouping at runtime and that choice persists over this value until
      # `workmux sidebar group --clear`.
      group_by = "project";
    };
    theme = {
      custom = {
        current_row_bg = "#${colors.base00}";
        text = "#${colors.base05}";
        accent = "#${colors.base0D}";
        success = "#${colors.base0B}";
        warning = "#${colors.base0A}";
        danger = "#${colors.base08}";
        dimmed = "#${colors.base03}";
        border = "#${colors.base02}";
        header = "#${colors.base0E}";
        keycap = "#${colors.base0C}";
        info = "#${colors.base0C}";
      };
    };
    post_create =
      lib.optionals cfg.lima.enable [
        "direnv allow >/dev/null 2>&1 || true"
        "lefthook install >/dev/null 2>&1 || true"
      ]
      # Darwin-only because that's where the Seatbelt sandbox runs. Kept on
      # even with lima: the agent pane runs in the VM (where the settings key
      # is inert), but host-side Claude sessions opened in the worktree still
      # need the write grant.
      ++ lib.optionals sandboxEnabled [
        "${worktreeAllowGitWrites}"
      ];
    panes = [
      {
        command = "<agent>";
        focus = true;
      }
      {
        split = "horizontal";
        command = "nvim";
      }
      (
        {
          split = "vertical";
        }
        // lib.optionalAttrs cfg.lima.enable {
          command = "workmux sandbox shell -- fish";
        }
      )
    ];
    files = {
      copy = [
        ".claude"
        # Seed new worktrees with the main checkout's tofu/terraform cache
        # (.terraform is gitignored, so fresh worktrees would otherwise
        # cold-init and re-download every provider and module). Bounded depths
        # on purpose: worktree_dir is ".worktrees" *inside* the repo, so a
        # recursive "**/.terraform" glob would descend into sibling worktrees
        # and copy their provider caches. ".terraform" covers a root module at
        # the repo root; "*/.terraform" covers a module one level down. Copies
        # stay lightweight because .terraform/providers entries are symlinks
        # into the shared plugin cache (see home/tools.nix) and workmux's
        # copy_dir_recursive preserves symlinks rather than following them.
        ".terraform"
        "*/.terraform"
      ];
    };
    sandbox = lib.optionalAttrs cfg.lima.enable {
      enabled = true;
      backend = "lima";
      image = "file://${config.home.homeDirectory}/.lima/_images/terrarium.qcow2";
      host_commands = [ "mcp-kubectx" ];
      toolchain = "auto";
      env_passthrough = [
        "GITHUB_TOKEN"
        "GITHUB_PERSONAL_ACCESS_TOKEN"
        "GH_TOKEN"
        "ARGOCD_API_TOKEN"
        "ARGOCD_BASE_URL"
        "DAGGER_CLOUD_TOKEN"
        "KAGI_API_KEY"
        "SPACELIFT_API_KEY_ENDPOINT"
        "SPACELIFT_API_KEY_ID"
        "SPACELIFT_API_KEY_SECRET"
        "HASS_TOKEN"
        "TF_CLI_CONFIG_FILE"
        "TERM_PROGRAM"
        "TERM_PROGRAM_VERSION"
        "ATUIN_USERNAME"
        "ATUIN_PASSWORD"
        # Per-launch effort for the sandboxed agent, set as
        # `CLAUDE_CODE_EFFORT_LEVEL=high workmux sandbox agent` or through
        # the `wma high` fish function in home/fish.nix. /effort
        # cannot save a level because home-manager links settings.json
        # from the read-only nix store. Since Claude Code 2.1.317 a
        # subagent on a different model no longer inherits the session
        # level. The generated CLAUDE.md asks for such downgrades on
        # Explore agents, so `wma` sets the subagent variable too.
        "CLAUDE_CODE_EFFORT_LEVEL"
        "CLAUDE_CODE_SUBAGENT_EFFORT"
      ];
      extra_mounts = [
        {
          host_path = researchDir;
          guest_path = researchDir;
          writable = true;
        }
        {
          # mcp-kubectx serve writes the scoped kubeconfig files of
          # guest-side serves at this path via `host select` running
          # on the macOS host. The guest's kubectl reads them through
          # this bind mount, and shutdown cleanup uses a local
          # os.Remove from the guest, so the mount must be writable.
          # Through this mount, the guest can create, change, and
          # delete the files of this directory of the host, and
          # replace each with a symlink. The directory holds the
          # scoped kubeconfig files of guest-side serves. `host select`
          # writes each of them to a new file under a random name and
          # renames that file into place. So it does not write through
          # a symlink that a guest plants.
          #
          # The files that host-side serves write or trust lie in
          # mcp-kubectx-run, which no entry of this list mounts. They
          # include host.id, the kubeconfig of each host-side serve,
          # and kubectx.log, the --log-file of servers.kubectx.
          host_path = "${config.xdg.stateHome}/mcp-kubectx";
          guest_path = "${config.xdg.stateHome}/mcp-kubectx";
          writable = true;
        }
        {
          # Output dir for the web-archive skill (btrix, yt-dlp).
          # Mount it like ~/Documents/repos so captures from inside
          # the sandbox persist to the host filesystem.
          host_path = cfg.archivesDir;
          guest_path = cfg.archivesDir;
          writable = true;
        }
        {
          # Read-only: the guest reads only the static key. Its own history
          # DB lives at the Linux /home/<user>/.local/share/atuin -- a
          # different absolute path -- so there is no write conflict.
          host_path = atuinDataDir;
          guest_path = atuinDataDir;
          writable = false;
        }
      ];
      lima = {
        isolation = "shared";
        projects_dir = "${config.home.homeDirectory}/Documents/repos";
        skip_default_provision = true;
        # Shared isolation trades Git metadata isolation for writable repo
        # mounts; workmux requires acknowledging that for unattended starts.
        accept_reduced_git_metadata_isolation = true;
        inherit (cfg.lima) cpus;
        inherit (cfg.lima) memory;
        inherit (cfg.lima) disk;
      };
    };
  };

  claudeStylixBase = if config.stylix.polarity == "light" then "light" else "dark";

  # Shimmer tokens in the upstream Claude Code dark theme are hardcoded
  # constants ~+12 lightness points above their base. base16 has no
  # lightened slots, so we derive them by bumping HSL lightness. This keeps
  # the warm pair-with-base relationship intact under any stylix scheme.
  shimmerOf = name: lighten colors name 12;

  claudeStylixTheme = (pkgs.formats.json { }).generate "stylix.json" {
    name = "Stylix";
    base = claudeStylixBase;
    overrides = {
      claude = "#${colors.base09}";
      claudeShimmer = shimmerOf "base09";
      text = "#${colors.base05}";
      inverseText = "#${colors.base00}";
      inactive = "#${colors.base04}";
      inactiveShimmer = shimmerOf "base04";
      subtle = "#${colors.base03}";
      permission = "#${colors.base0D}";
      permissionShimmer = shimmerOf "base0D";
      remember = "#${colors.base0D}";

      success = "#${colors.base0B}";
      error = "#${colors.base08}";
      warning = "#${colors.base0A}";
      warningShimmer = shimmerOf "base0A";
      merged = "#${colors.base0E}";

      promptBorder = "#${colors.base04}";
      promptBorderShimmer = shimmerOf "base04";
      planMode = "#${colors.base0D}";
      autoAccept = "#${colors.base0E}";
      bashBorder = "#${colors.base0F}";
      ide = "#${colors.base0D}";
      fastMode = "#${colors.base09}";
      fastModeShimmer = shimmerOf "base09";

      userMessageBackground = "#${colors.base01}";
      userMessageBackgroundHover = "#${colors.base02}";
      selectionBg = "#${colors.base02}";

      red_FOR_SUBAGENTS_ONLY = "#${colors.base08}";
      orange_FOR_SUBAGENTS_ONLY = "#${colors.base09}";
      yellow_FOR_SUBAGENTS_ONLY = "#${colors.base0A}";
      green_FOR_SUBAGENTS_ONLY = "#${colors.base0B}";
      cyan_FOR_SUBAGENTS_ONLY = "#${colors.base0C}";
      blue_FOR_SUBAGENTS_ONLY = "#${colors.base0D}";
      purple_FOR_SUBAGENTS_ONLY = "#${colors.base0E}";
      pink_FOR_SUBAGENTS_ONLY = "#${colors.base0F}";

      # Diff dimmed tokens (diffAddedDimmed, diffRemovedDimmed) are left to
      # the preset. Their upstream relationship to diffAdded/diffRemoved is
      # a desaturate-and-tint, not a simple lighten, so HSL lightening would
      # not reproduce the muted background look.
    };
  };

  fetchRules = (pkgs.formats.json { }).generate "mcp-fetch-rules.json" (
    lib.optionalAttrs cfg.fetchAllowlist {
      reason = "URL not in allowlist. If you need to fetch this content, ask the user to add an entry to the allowlist. Present the user with both the URL and your justification.";
    }
    // {
      deny = map cleanRule (bundledFetchDeny ++ cfg.extraFetchRules.deny);
      robotsExempt = map cleanAttrs (bundledFetchRobotsExempt ++ cfg.extraFetchRules.robotsExempt);
    }
    // lib.optionalAttrs cfg.fetchAllowlist {
      allow = map cleanAttrs (
        [
          { host = "(.*\\.)?adguard\\.com"; }
          { host = "(.*\\.)?anthropic\\.com"; }
          { host = "(.*\\.)?archlinux\\.org"; }
          { host = "argoproj\\.github\\.io"; }
          { host = "(.*\\.)?argoproj\\.io"; }
          { host = "(.*\\.)?artifacthub\\.io"; }
          { host = "(.*\\.)?astral\\.sh"; }
          { host = "(.*\\.)?atuin\\.sh"; }
          { host = "(.*\\.)?docs\\.auth0\\.com"; }
          { host = "bjw-s-labs\\.github\\.io"; }
          { host = "(.*\\.)?brew\\.sh"; }
          { host = "(.*\\.)?cert-manager\\.io"; }
          { host = "(.*\\.)?cilium\\.io"; }
          { host = "(.*\\.)?cncf\\.io"; }
          { host = "(.*\\.)?cloudflare\\.com"; }
          { host = "(.*\\.)?cloudnative-pg\\.io"; }
          { host = "(.*\\.)?containerd\\.io"; }
          { host = "(.*\\.)?coredns\\.io"; }
          { host = "(.*\\.)?crates\\.io"; }
          { host = "(.*\\.)?crossplane\\.io"; }
          { host = "(.*\\.)?crds\\.dev"; }
          { host = "(.*\\.)?dagger\\.io"; }
          { host = "(.*\\.)?daggerverse\\.dev"; }
          { host = "(.*\\.)?direnv\\.net"; }
          { host = "(.*\\.)?docker\\.com"; }
          { host = "(.*\\.)?docs\\.rs"; }
          { host = "(.*\\.)?editorconfig\\.org"; }
          { host = "(.*\\.)?docs\\.doppler\\.com"; }
          { host = "(.*\\.)?dragonflydb\\.io"; }
          { host = "(.*\\.)?envoyproxy\\.io"; }
          { host = "(.*\\.)?etcd\\.io"; }
          { host = "(.*\\.)?external-secrets\\.io"; }
          { host = "(.*\\.)?fishshell\\.com"; }
          { host = "(.*\\.)?fluxcd\\.io"; }
          { host = "(.*\\.)?freedesktop\\.org"; }
          { host = "(.*\\.)?gethomepage\\.dev"; }
          { host = "(.*\\.)?getsops\\.io"; }
          { host = "(.*\\.)?ghostty\\.org"; }
          { host = "(.*\\.)?git-scm\\.com"; }
          { host = "(.*\\.)?github\\.com"; }
          { host = "(.*\\.)?githubusercontent\\.com"; }
          { host = "(.*\\.)?gnu\\.org"; }
          { host = "(.*\\.)?gnupg\\.org"; }
          { host = "(.*\\.)?go\\.dev"; }
          { host = "(.*\\.)?golang\\.org"; }
          { host = "(.*\\.)?grafana\\.com"; }
          { host = "(.*\\.)?grpc\\.io"; }
          { host = "(.*\\.)?hashicorp\\.com"; }
          { host = "(.*\\.)?helm\\.sh"; }
          { host = "(.*\\.)?hetzner\\.com"; }
          { host = "(.*\\.)?ietf\\.org"; }
          { host = "(.*\\.)?inspektor-gadget\\.io"; }
          { host = "(.*\\.)?jacobcolvin\\.com"; }
          { host = "(.*\\.)?jetify\\.com"; }
          { host = "(.*\\.)?jsonnet\\.org"; }
          { host = "(.*\\.)?k9scli\\.io"; }
          { host = "(.*\\.)?kagi\\.com"; }
          { host = "(.*\\.)?kcl-lang\\.io"; }
          { host = "(.*\\.)?kernel\\.org"; }
          { host = "(.*\\.)?kubernetes\\.io"; }
          { host = "(.*\\.)?kyverno\\.io"; }
          { host = "(.*\\.)?letsencrypt\\.org"; }
          { host = "(.*\\.)?lix\\.systems"; }
          { host = "(.*\\.)?man7\\.org"; }
          { host = "(.*\\.)?microsoft\\.com"; }
          { host = "(.*\\.)?mozilla\\.org"; }
          { host = "(.*\\.)?nats\\.io"; }
          { host = "(.*\\.)?nix\\.dev"; }
          { host = "(.*\\.)?nixos\\.org"; }
          { host = "(.*\\.)?nmap\\.org"; }
          { host = "(.*\\.)?nodejs\\.org"; }
          { host = "(.*\\.)?npmjs\\.com"; }
          { host = "(.*\\.)?npmjs\\.org"; }
          { host = "(.*\\.)?obsidian\\.md"; }
          { host = "(.*\\.)?opencontainers\\.org"; }
          { host = "(.*\\.)?openebs\\.io"; }
          { host = "(.*\\.)?openssl\\.org"; }
          { host = "(.*\\.)?opentelemetry\\.io"; }
          { host = "(.*\\.)?opentofu\\.org"; }
          { host = "(.*\\.)?postgresql\\.org"; }
          { host = "(.*\\.)?prettier\\.io"; }
          { host = "(.*\\.)?prometheus\\.io"; }
          { host = "(.*\\.)?pypi\\.org"; }
          { host = "(.*\\.)?python\\.org"; }
          { host = "old\\.reddit\\.com"; }
          { host = "(.*\\.)?redis\\.io"; }
          { host = "(.*\\.)?rfc-editor\\.org"; }
          { host = "(.*\\.)?robusta\\.dev"; }
          { host = "(.*\\.)?rook\\.io"; }
          { host = "(.*\\.)?rust-lang\\.org"; }
          { host = "(.*\\.)?securecodebox\\.io"; }
          { host = "(.*\\.)?sigstore\\.dev"; }
          { host = "(.*\\.)?sigs\\.k8s\\.io"; }
          { host = "(.*\\.)?spacelift\\.io"; }
          { host = "(.*\\.)?sqlite\\.org"; }
          { host = "(.*\\.)?stakater\\.com"; }
          { host = "(.*\\.)?stackoverflow\\.com"; }
          { host = "(.*\\.)?starship\\.rs"; }
          { host = "(.*\\.)?talos\\.dev"; }
          { host = "(.*\\.)?taskfile\\.dev"; }
          { host = "(.*\\.)?terraform\\.io"; }
          { host = "(.*\\.)?tetragon\\.io"; }
          { host = "(.*\\.)?truenas\\.com"; }
          { host = "(.*\\.)?typescriptlang\\.org"; }
          { host = "(.*\\.)?ui\\.com"; }
          { host = "(.*\\.)?w3\\.org"; }
          { host = "(.*\\.)?whatwg\\.org"; }
          { host = "(.*\\.)?wikipedia\\.org"; }
          { host = "(.*\\.)?wireguard\\.com"; }
          { host = "(.*\\.)?wireshark\\.org"; }
          { host = "(.*\\.)?zed\\.dev"; }
        ]
        ++ bundledFetchAllow
        ++ cfg.extraFetchRules.allow
      );
    }
  );

  # JSON payload passed to hook-router via --post-impl-skills.
  # Derived from cfg.postImplSkills, filtered to enabled entries.
  # Field names must match the lowercase JSON tags on PostImplSkill
  # in tools/hook-router/plan.go.
  postImplCatalog = lib.mapAttrsToList (_: s: { inherit (s) label description; }) (
    lib.filterAttrs (_: s: s.enable) cfg.postImplSkills
  );

  # Wrap-up skills whose UserPromptSubmit invocation clears the
  # plan-guard session state, releasing Stop. `merge` is the public
  # name of the wm-merge skill (see configs/claude/skills/wm-merge/
  # SKILL.md `name: merge`). Other wm-* skills (rebase, coordinator,
  # workmux) are not wrap-up actions and are intentionally excluded.
  commitSkills = [
    "commit"
    "commit-push-pr"
    "merge"
  ];

  hookRouter = pkgs.writeShellApplication {
    name = "hook-router-wrapper";
    runtimeInputs = [
      pkgs.hook-router
      config.programs.git.package
    ];
    # The --command-rules JSON is shell-escaped via lib.escapeShellArg
    # (single-quoted). Backticks inside the JSON are literal data, but
    # shellcheck warns SC2016 about them as if they were unexpanded
    # command substitutions.
    excludeShellChecks = [ "SC2016" ];
    # hook-router exits with an error on every hook event when it
    # cannot parse one of the flags in `text`, such as a conditional
    # rule whose pattern does not compile. Claude Code runs the tool
    # call after that error, so every hook-router guard stops until a
    # later switch fixes the flag. The check runs the wrapper with
    # --check-flags, which parses the same flags and handles no
    # event. This build therefore fails on a flag of that kind.
    derivationArgs.postCheck = ''
      "$target" --check-flags
    '';
    text = ''
      exec hook-router \
        --db "${config.xdg.stateHome}/hook-router/state.db" \
        --log-file "${config.xdg.stateHome}/hook-router/hook-router.log" \
        --log-max-bytes ${
          # hook-router renames the log to hook-router.log.1 when the
          # log reaches this size, and starts an empty log. It keeps
          # the --log-backups most recent renamed files. The log and
          # those files together hold little more than this size times
          # one more than that count.
          toString (16 * 1024 * 1024)
        } \
        --log-backups 3 \
        --post-impl-skills ${lib.escapeShellArg (builtins.toJSON postImplCatalog)} \
        --commit-skills ${lib.escapeShellArg (builtins.toJSON commitSkills)} \
        --command-rules ${
          # Deny rules are serialized before ask rules: hook-router
          # evaluates the list in order with first-match-wins, so this
          # ordering preserves deny precedence.
          lib.escapeShellArg (
            builtins.toJSON (
              map (r: r // { action = "deny"; }) (bundledCommandDeny ++ cfg.extraCommandRules.deny)
              ++ map (r: r // { action = "ask"; }) (bundledCommandAsk ++ cfg.extraCommandRules.ask)
            )
          )
        } \
        --mcp-rules ${
          # The MCP entries of the same aggregated lists that feed
          # settings.permissions, re-enforced at hook level because
          # plan mode ignores settings allow rules for
          # subagent-originated MCP calls
          # (anthropics/claude-code#73633). Filtering by the mcp__
          # prefix keeps hook and settings in lockstep as bundles
          # change. The conditional rules have no settings counterpart,
          # because a settings rule cannot read the arguments of an MCP
          # call. hook-router alone decides the tools that they name
          # (see the conditionalRules bundle option).
          lib.escapeShellArg (
            builtins.toJSON {
              allow = lib.filter (lib.hasPrefix "mcp__") (enabledRules cfg.permissions.allow);
              ask = lib.filter (lib.hasPrefix "mcp__") (enabledRules cfg.permissions.ask);
              deny = lib.filter (lib.hasPrefix "mcp__") (enabledRules cfg.permissions.deny);
              conditional = bundledConditionalRules;
            }
          )
        } \
        --formatter-rules ${
          lib.escapeShellArg (builtins.toJSON (defaultFormatterRules ++ cfg.formatterRules))
        } \
        --linter-rules ${lib.escapeShellArg (builtins.toJSON (defaultLinterRules ++ cfg.linterRules))} \
        --compaction-config ${
          lib.escapeShellArg (builtins.toJSON (removeAttrs cfg.outputCompaction [ "saveFullOutput" ]))
        } \
        ${lib.optionalString cfg.outputCompaction.saveFullOutput ''--compaction-output-dir "${config.xdg.stateHome}/hook-router/outputs"''} \
        ${
          # The fish `find` function of home/fish.nix reads
          # dotfiles.findExcludes too. So the hook and the shell leave
          # the same directories out of a search.
          lib.optionalString cfg.searchRewrite.enable
            "--search-rewrite-config ${
              lib.escapeShellArg (
                builtins.toJSON {
                  inherit (cfg.searchRewrite) grep find;
                  inherit (config.dotfiles) findExcludes;
                }
              )
            }"
        } \
        --sleep-guard-config ${
          # Passed unconditionally: enable travels inside the JSON (the
          # --compaction-config pattern) because maxSeconds cannot double
          # as a disabled sentinel.
          lib.escapeShellArg (builtins.toJSON cfg.sleepGuard)
        } \
        ${
          # The same linter the Write/Edit hook runs, pointed at the
          # message a `git commit` or `gh pr create` carries and at the
          # MCP tool arguments that toolFields names. Each text arrives
          # on stdin. A Required finding denies the call every time, a
          # Recommended finding denies it once and lets the identical
          # rerun through, and an Optional finding never denies on its
          # own.
          lib.optionalString cfg.proseLint.enable
            "--message-lint-config ${
              lib.escapeShellArg (
                builtins.toJSON {
                  command = [
                    (lib.getExe pkgs.prose-lint)
                    "--commit"
                  ];
                  timeout = "15s";
                  toolFields = lib.mapAttrsToList (tool: fields: { inherit tool fields; }) proseToolFields;
                }
              )
            }"
        } \
        ${lib.optionalString autoAllowEnabled "--auto-allow"} \
        ${lib.optionalString cfg.skipPlanReview "--skip-plan-review"} \
        ${lib.optionalString cfg.rmGuard "--rm-guard"} \
        ${lib.optionalString cfg.promptGuard "--prompt-guard"} \
        --escape-restore-config ${
          # Passed unconditionally, like --sleep-guard-config: enable
          # travels inside the JSON.
          lib.escapeShellArg (builtins.toJSON cfg.escapeRestore)
        } \
        ${lib.optionalString sandboxEnabled "--write-guard"} \
        "$@"
    '';
  };

  # Appends every settings and skills edit Claude Code reports, including
  # the settings.local.json the workmux worktree hook writes. hook-router
  # creates this directory in its own logger, so a host that has never run
  # it needs the mkdir.
  configChangeLog = pkgs.writeShellApplication {
    name = "claude-config-change-log";
    runtimeInputs = [ pkgs.jq ];
    text = ''
      log_dir="${config.xdg.stateHome}/hook-router"
      # Exit code 2 blocks the config change and every other non-zero code
      # only prints stderr. jq exits 2 when the append fails, so remap both
      # failures to 1: a full disk must not stop a skill reload.
      mkdir -p "$log_dir" || exit 1
      jq -c '{ time: (now | todateiso8601), source, file_path }' \
        >>"$log_dir/config-changes.log" || exit 1
    '';
  };

  # Loads open aven tasks into a session started inside a tracked project.
  # `aven prime` prints the full aven skill ahead of the task list. The
  # skills block below installs that skill for Claude to load on demand, so
  # the hook keeps only the task sections. Inside a tracked project it
  # always prints the rule that sends out-of-scope issues to the
  # track-issue skill. A project with no open work still collects them.
  # It prints nothing outside a tracked project, and a failing `aven` never
  # blocks startup.
  avenPrime = pkgs.writeShellApplication {
    name = "claude-aven-prime";
    runtimeInputs = [
      pkgs.aven
      pkgs.gnused
    ];
    text = ''
      out=$(aven prime 2>/dev/null) || exit 0
      case $out in
      *"## Open Issues"*) ;;
      *) exit 0 ;;
      esac
      echo "aven tracks tasks for this project. Load the \`aven\` skill before running \`aven\` commands."
      echo "When you notice a bug outside the current task, or the user defers one, file it with the \`track-issue\` skill instead of fixing it or dropping it."
      case $out in
      *"Summary: total=0 "*) exit 0 ;;
      esac
      echo
      sed -n '/^## Local Conventions/,$p' <<<"$out"
    '';
  };

  # Logs the workmux Lima guest in to the self-hosted atuin server, so its
  # shell history syncs with the macOS host. workmux runs post_create on the
  # host, so only a hook inside the guest reaches the guest's atuin. The
  # guest agent receives ATUIN_USERNAME and ATUIN_PASSWORD through
  # env_passthrough, and its hooks inherit them. Without the password, atuin
  # prompts for it on the terminal, so the script exits when either variable
  # is empty.
  #
  # atuin reads the answer to its key prompt from stdin, which carries the
  # hook's JSON input. The printf answers with a blank line instead, so atuin
  # loads the mounted host key (key_path). atuin reads the password from
  # ATUIN_PASSWORD, so it never appears in argv. The shared VM keeps atuin's
  # session token on its own disk, so atuin login returns early in every
  # later Claude session.
  atuinSandboxLogin = pkgs.writeShellApplication {
    name = "claude-atuin-sandbox-login";
    runtimeInputs = [ config.programs.atuin.package ];
    text = ''
      if [ -z "''${ATUIN_USERNAME:-}" ] || [ -z "''${ATUIN_PASSWORD:-}" ]; then
        exit 0
      fi
      if printf '\n' | atuin login -u "$ATUIN_USERNAME" >/dev/null 2>&1; then
        atuin sync >/dev/null 2>&1 || true
      fi
    '';
  };

  # CA env vars injected into all stdio MCP servers
  caEnvVars = lib.optionalAttrs (config.dotfiles.caBundlePath != null) {
    NIX_SSL_CERT_FILE = config.dotfiles.caBundlePath;
    SSL_CERT_FILE = config.dotfiles.caBundlePath;
    CURL_CA_BUNDLE = config.dotfiles.caBundlePath;
    GIT_SSL_CAINFO = config.dotfiles.caBundlePath;
    REQUESTS_CA_BUNDLE = config.dotfiles.caBundlePath;
    NODE_EXTRA_CA_CERTS = config.dotfiles.caBundlePath;
  };

  # Post-process all servers to inject CA env into stdio servers
  injectCaEnv =
    servers:
    lib.mapAttrs (
      _: server:
      if (server.type or "") == "stdio" && caEnvVars != { } then
        server // { env = caEnvVars // (server.env or { }); }
      else
        server
    ) servers;

  gitWrapper = pkgs.writeShellScript "git-mcp-wrapper" ''
    ${exportSecret "GH_TOKEN" "gh_token"}
    exec ${pkgs.mcp-git}/bin/mcp-git "$@"
  '';

  # Agents clone other repositories under this directory and read them
  # there. The git tool bundle takes the directory from this value for
  # its instructions, for the git MCP server, and for the sandbox.
  #
  # A Linux host clones into /tmp/git. On a NixOS host,
  # hosts/nixos/clones.nix keeps the daily cleanup of /tmp out of that
  # directory and removes each old clone as a whole.
  #
  # A Darwin host cannot keep its cleanup of /tmp out of a directory.
  # macOS 14 and later clean /tmp every day with
  # /usr/libexec/tmp_cleaner. The script checks each file on its own,
  # and it deletes a file that nothing read or changed in the last
  # three days. It names /tmp and the age in its own text, and it
  # reads no setting. A clone in /tmp then loses each file that no
  # agent read and no fetch wrote in that time, and it keeps the other
  # files. An agent that reads such a clone finds only a part of the
  # upstream tree and gets no error.
  #
  # So a Darwin host clones into a directory that the script does not
  # clean. The expire-clones agent of this module removes each old
  # clone there as a whole. Time Machine leaves ~/Library/Caches out
  # of its backups.
  cloneRoot =
    if pkgs.stdenv.hostPlatform.isDarwin then
      "${config.home.homeDirectory}/Library/Caches/git-clones"
    else
      "/tmp/git";

  # inTmp tells whether a path lies in /tmp. /tmp is a link to
  # /private/tmp on macOS.
  inTmp =
    path:
    lib.any (tmp: lib.hasPrefix "${tmp}/" "${path}/") [
      "/tmp"
      "/private/tmp"
    ];

  # A Darwin host runs the expire-clones agent when the git tool bundle
  # is on, because that bundle tells agents to clone into cloneRoot.
  expireClonesEnabled = pkgs.stdenv.hostPlatform.isDarwin && cfg.toolBundles.git.enable;

  # The expire-clones agent writes one line here for each clone that it
  # removes. It also names each clone that it cannot remove here.
  expireClonesLog = "${config.home.homeDirectory}/Library/Logs/expire-clones.log";

  # The expire-claude-tmp agent writes one line here for each session
  # directory that it removes. It also names each session directory
  # that it cannot remove here.
  expireClaudeTmpLog = "${config.home.homeDirectory}/Library/Logs/expire-claude-tmp.log";

  kagiWrapper = pkgs.writeShellScript "kagi-mcp-wrapper" ''
    ${exportSecret "KAGI_API_KEY" "kagi_api_key"}
    exec ${pkgs.mcp-kagi}/bin/kagimcp "$@"
  '';

  spaceliftWrapper = pkgs.writeShellScript "spacelift-mcp-wrapper" ''
    ${exportSecrets {
      SPACELIFT_API_KEY_ENDPOINT = "spacelift_api_key_endpoint";
      SPACELIFT_API_KEY_ID = "spacelift_api_key_id";
      SPACELIFT_API_KEY_SECRET = "spacelift_api_key_secret";
    }}
    exec ${pkgs.spacectl}/bin/spacectl "$@"
  '';

  # --deny-tool flags for the github proxy, derived from the github tool
  # bundle's own permissions.deny (the single source of truth). Denying a
  # github MCP tool there also strips its schema from the proxy's tools/list,
  # so it stops costing context tokens even though the upstream
  # endpoint still serves it. The mcp__github__ prefix is stripped back to the
  # upstream tool name the proxy matches; non-MCP deny entries are ignored.
  ghProxyDenyFlags = lib.concatMapStringsSep " " (t: "--deny-tool ${t}") (
    map (lib.removePrefix "mcp__github__") (
      lib.filter (lib.hasPrefix "mcp__github__") cfg.toolBundles.github.permissions.deny
    )
  );

  # The x/all endpoint serves every tool across all upstream toolsets.
  # The narrower /mcp endpoint would serve only the `default` toolset
  # (context, repos, issues, pull_requests, users), which carries no
  # Actions, security-alert, discussion, or project tools. Claude
  # reaches these through ToolSearch, so an unused tool costs one name
  # in the deferred listing rather than a schema. permissions.deny of
  # the github bundle stays the place that spells out tool policy. It
  # strips every write tool except the pull request set that the
  # bundle gates with permissions.ask and conditionalRules. A new
  # upstream tool appears here on its own and prompts on first use
  # until it is allowed or denied.
  #
  # The single quotes around the header keep the token out of the proxy's
  # arguments, where ps shows it to every user. The proxy expands the
  # variable from its own environment.
  githubWrapper = pkgs.writeShellScript "github-mcp-wrapper" ''
    ${exportSecret "GH_TOKEN" "gh_token"}
    export GITHUB_PERSONAL_ACCESS_TOKEN="''${GH_TOKEN:-}"
    exec ${pkgs.mcp-http-proxy}/bin/mcp-http-proxy \
      --url https://api.githubcopilot.com/mcp/x/all \
      --header 'Authorization=Bearer $GITHUB_PERSONAL_ACCESS_TOKEN' \
      --log-file "${config.xdg.stateHome}/mcp-http-proxy/github.log" \
      ${ghProxyDenyFlags} \
      "$@"
  '';

  # The launcher wrapper's in-sandbox merged $KUBECONFIG, emitted as
  # exactly ONE `export KUBECONFIG` --run line so a later --run cannot
  # shadow an earlier one. local.yaml (MCP-owned selection overlay,
  # first-file-wins) and the sidecar (external SA creds) always bracket
  # the colon-list. On the Lima guest image (guestKubeconfigLocal) the
  # guest's own ~/.kube/config is spliced between them, with a preceding
  # --run that exports $CLAUDE_KUBECTX_GUEST_CONFIG, so guest-local
  # clusters (kind / k3d / minikube / Talos-in-Docker) resolve like any
  # normal cluster. The interpolation carries no trailing line
  # continuation; the wrapProgram call site supplies the `\`.
  kubectxKubeconfigRunArgs =
    if cfg.guestKubeconfigLocal then
      "--run 'export CLAUDE_KUBECTX_GUEST_CONFIG=\"$HOME/.kube/config\"' \\\n        "
      + "--run 'export KUBECONFIG=\"$CLAUDE_KUBECTX_LOCAL:$CLAUDE_KUBECTX_GUEST_CONFIG:$CLAUDE_KUBECTX_SIDECAR\"'"
    else
      "--run 'export KUBECONFIG=\"$CLAUDE_KUBECTX_LOCAL:$CLAUDE_KUBECTX_SIDECAR\"'";

  # The claude wrapper creates the directory claude-kubectx.<pid> for
  # each session and exports its path as CLAUDE_KUBECTX_DIR. The
  # directory holds local.yaml, which records the Kubernetes context
  # that the session selected. It also holds the kubeconfig link, which
  # mcp-kubectx points at the scoped kubeconfig of that context.
  # hook-router removes the directory when the session ends. At each
  # session start, hook-router also removes each directory beside it
  # whose process does not run.
  #
  # A session that ends without its SessionEnd hook, such as after a
  # crash or a restart, leaves its directory behind. A later session
  # can get the same PID, and the sweep at session start skips the
  # directory of a PID that a process holds. So the wrapper removes the
  # directory of its own PID before it creates the directory, and it
  # writes a new local.yaml. Each session starts with no selected
  # context and no kubeconfig link.
  #
  # This value names the parent of those directories on a Darwin host.
  # On a Linux host, the value is null, and the wrapper takes the parent
  # from the environment of each session. That parent is the runtime
  # directory of the login, which XDG_RUNTIME_DIR names, or /tmp
  # without that variable.
  #
  # macOS does not set XDG_RUNTIME_DIR, and a Darwin host cannot keep
  # its cleanup of /tmp out of a directory, as the comment on cloneRoot
  # describes. In /tmp, a session loses local.yaml when nothing reads or
  # changes the file for more than three days. The Bash gate of
  # hook-router then denies kubectl until the agent selects a context
  # again. So a Darwin host keeps the directories in the state home,
  # which macOS does not clean. A restart of the Mac empties /tmp but
  # not the state home. So a leftover directory in the state home stays
  # until the wrapper or the sweep removes it.
  #
  # ~/.local/state is a read path of the sandbox, and no write path of
  # the sandbox holds this directory. So sandboxed commands read the
  # files of a session and cannot change them, as in /tmp.
  # ~/Library/Caches, where cloneRoot and claudeTmpDir lie, is a write
  # path of the sandbox.
  kubectxSessionParent =
    if pkgs.stdenv.hostPlatform.isDarwin then "${config.xdg.stateHome}/claude-kubectx" else null;

  # The command of the wrapper that exports CLAUDE_KUBECTX_DIR. The
  # shell expands $$ to the process id of the wrapper. Claude Code runs
  # as that process, because the wrapper replaces itself with it.
  kubectxDirExport =
    if kubectxSessionParent == null then
      "export CLAUDE_KUBECTX_DIR=\"\${XDG_RUNTIME_DIR:-/tmp}/claude-kubectx.$$\""
    else
      "export CLAUDE_KUBECTX_DIR=${lib.escapeShellArg kubectxSessionParent}/claude-kubectx.$$";

  # Wrap claude with its invocation-time env so vars survive boundaries that
  # do not propagate the shell env (lima VMs, ssh without SendEnv, etc.).
  #
  # CLAUDE_CODE_DISABLE_INLINE_SHELL_RM_PROMPT turns off Claude Code's
  # removal check for `sh -c` scripts. That check prompts in every
  # permission mode on any script that runs `rm` and that it cannot
  # analyze. hook-router's rmguard still checks those scripts for unsafe
  # `rm` targets. Claude Code reads this variable only from its launch
  # environment and ignores it in settings.json `env`, so the wrapper
  # sets it.
  #
  # CLAUDE_CODE_TMPDIR names the parent of the directory claude-<uid>,
  # where Claude Code keeps the temporary files of each session. The
  # comment on claudeTmpDir describes the directory of each platform.
  # The wrapper sets the variable, so each hook of a session inherits
  # it. The write guard of hook-router reads it to find the directory
  # claude-<uid>.
  claudeWrapped = pkgs.symlinkJoin {
    name = "claude-code-wrapped";
    paths = [ pkgs.llm-agents.claude-code ];
    nativeBuildInputs = [ pkgs.makeWrapper ];
    inherit (pkgs.llm-agents.claude-code) meta version;
    postBuild = ''
      wrapProgram $out/bin/claude \
        --run 'if [ -n "''${KUBECONFIG-}" ]; then export KUBECONFIG_HOST="$KUBECONFIG"; fi' \
        --run ${lib.escapeShellArg kubectxDirExport} \
        --run 'rm -rf "$CLAUDE_KUBECTX_DIR"' \
        --run 'mkdir -p "$CLAUDE_KUBECTX_DIR"' \
        --run 'export CLAUDE_KUBECTX_SIDECAR="$CLAUDE_KUBECTX_DIR/kubeconfig"' \
        --run 'export CLAUDE_KUBECTX_LOCAL="$CLAUDE_KUBECTX_DIR/local.yaml"' \
        --run 'printf "apiVersion: v1\nkind: Config\n" > "$CLAUDE_KUBECTX_LOCAL"' \
        ${kubectxKubeconfigRunArgs} \
        --set CLAUDE_CODE_TMUX_TRUECOLOR 1 \
        --set CLAUDE_CODE_NO_FLICKER 1 \
        --set CLAUDE_CODE_DISABLE_INLINE_SHELL_RM_PROMPT 1 \
        --set CLAUDE_CODE_TMPDIR ${lib.escapeShellArg claudeTmpDir} \
        --set DISABLE_AUTOUPDATER 1 \
        --set CLAUDE_RESEARCH_DIR ${lib.escapeShellArg researchDir} \
        --set TF_CLI_CONFIG_FILE "${config.xdg.configHome}/opentofu/tofurc" \
        --set PLUGIN_UNIX_SOCKET_DIR "${config.home.homeDirectory}/.terraform.versions" \
        ${lib.optionalString skipPerms "--set IS_SANDBOX 1"} \
        ${lib.optionalString (
          skipPerms || autoAllowEnabled
        ) "--add-flags --allow-dangerously-skip-permissions"}
    '';
  };

  # Wrapper that injects sops secrets as env vars for sandbox env_passthrough.
  # Uses symlinkJoin so share/fish/vendor_completions.d/ from workmux-bin is preserved.
  workmuxWrapped = pkgs.symlinkJoin {
    name = "workmux-wrapped";
    paths = [ pkgs.workmux-bin ];
    nativeBuildInputs = [ pkgs.makeWrapper ];
    postBuild = ''
      wrapProgram $out/bin/workmux \
        --run '
          ${exportSecret "GH_TOKEN" "gh_token"}
          if [ -n "''${GH_TOKEN:-}" ]; then
            export GITHUB_TOKEN="$GH_TOKEN"
            export GITHUB_PERSONAL_ACCESS_TOKEN="$GH_TOKEN"
          fi
          ${exportSecrets {
            ARGOCD_API_TOKEN = "argocd_api_token";
            ARGOCD_BASE_URL = "argocd_base_url";
            DAGGER_CLOUD_TOKEN = "dagger_cloud_token";
            KAGI_API_KEY = "kagi_api_key";
            SPACELIFT_API_KEY_ENDPOINT = "spacelift_api_key_endpoint";
            SPACELIFT_API_KEY_ID = "spacelift_api_key_id";
            SPACELIFT_API_KEY_SECRET = "spacelift_api_key_secret";
            HASS_TOKEN = "hass_token";
            ATUIN_USERNAME = "atuin_username";
            ATUIN_PASSWORD = "atuin_password";
          }}
          export TF_CLI_CONFIG_FILE="${config.xdg.configHome}/opentofu/tofurc"
        '
    '';
  };

  # Aggregate enabled tool bundles
  enabledBundles = lib.filterAttrs (_: b: b.enable) cfg.toolBundles;
  bundleValues = lib.attrValues enabledBundles;

  applyAlwaysLoad =
    alwaysLoad: server: if alwaysLoad then server // { alwaysLoad = true; } else server;

  bundledServers = lib.foldl' lib.recursiveUpdate { } (
    map (b: lib.mapAttrs (_: server: applyAlwaysLoad b.alwaysLoad server) b.servers) bundleValues
  );
  bundledAllow = lib.concatMap (b: b.permissions.allow) bundleValues;
  bundledDeny = lib.concatMap (b: b.permissions.deny) bundleValues;
  bundledAsk = lib.concatMap (b: b.permissions.ask) bundleValues;
  bundledConditionalRules = lib.concatMap (b: b.conditionalRules) bundleValues;
  bundledDomains = lib.concatMap (b: b.sandbox.allowedDomains) bundleValues;
  bundledSockets = lib.concatMap (b: b.sandbox.allowUnixSockets) bundleValues;
  bundledReadPaths = lib.concatMap (b: b.sandbox.allowRead) bundleValues;
  bundledWritePaths = lib.concatMap (b: b.sandbox.allowWrite) bundleValues;
  bundledFetchDeny = lib.concatMap (b: b.fetchRules.deny) bundleValues;
  bundledFetchAllow = lib.concatMap (b: b.fetchRules.allow) bundleValues;
  bundledFetchRobotsExempt = lib.concatMap (b: b.fetchRules.robotsExempt) bundleValues;
  bundledCommandDeny = lib.concatMap (b: b.commandRules.deny) bundleValues;
  bundledCommandAsk = lib.concatMap (b: b.commandRules.ask) bundleValues;
  # Bundle entries and the user's own merge per tool pattern, and a field
  # named twice for one pattern lints once.
  proseToolFields = lib.zipAttrsWith (_: lists: lib.unique (lib.concatLists lists)) (
    map (b: b.proseLint.toolFields) bundleValues ++ [ cfg.proseLint.toolFields ]
  );

  researchDir =
    if cfg.research.useVault then
      "${config.dotfiles.obsidian.vaultsDir}/${cfg.research.vault}/research"
    else
      "${config.home.homeDirectory}/.local/share/claude/research";

  # Host atuin data dir, shared read-only into the sandbox so the guest's
  # atuin reads the host encryption key (key_path).
  atuinDataDir = "${config.home.homeDirectory}/.local/share/atuin";

  # mdformat's CommonMark core has no table parser, and stock pkgs.mdformat
  # ships no plugins. Without mdformat-gfm every pipe table parses as a
  # paragraph and `--wrap no` joins it onto one line, destroying it
  # irrecoverably. `--no-extensions` disables the plugin and reintroduces
  # that, so it must not come back; the explicit `--extensions` flags pin
  # the entry points the plugin registers. mdformat-gfm also preserves task
  # lists, strikethrough, and bare autolinks. The wrapped python binary is
  # invoked directly through its store-path `/bin/mdformat` so
  # hook-router-wrapper doesn't need it on PATH and the formatter's own
  # python wrapper script stays intact.
  mdformatBin = pkgs.mdformat.withPlugins (ps: [ ps.mdformat-gfm ]);

  # `--compact-tables` leaves table cells unpadded, so a table doesn't get
  # rewritten end-to-end whenever one cell's width changes.
  mdformatCommand = [
    "${mdformatBin}/bin/mdformat"
    "--wrap"
    "no"
    "--number"
    "--no-validate"
    "--extensions"
    "gfm"
    "--extensions"
    "tables"
    "--compact-tables"
    "--no-codeformatters"
  ];

  # Default formatter routes auto-installed by hook-router on
  # PostToolUse:Write/Edit. Plans and research notes
  # accumulate token-wasteful patterns (multi-blank-line runs, trailing
  # whitespace, inter-word double spaces) across many tool calls;
  # mdformat collapses them in place.
  defaultFormatterRules = [
    {
      pathGlob = "${config.home.homeDirectory}/.claude/plans/**/*.md";
      command = mdformatCommand;
      timeout = "5s";
    }
    {
      pathGlob = "${researchDir}/**/*.md";
      command = mdformatCommand;
      timeout = "5s";
    }
  ];

  # Default linter routes installed by hook-router on
  # PostToolUse:Write/Edit/MultiEdit. prose-lint runs the rules in
  # configs/harper against every markdown and source file Claude
  # writes, and its findings come back as feedback before the next
  # turn, grouped by the tier each rule's directory under
  # configs/harper/rules gives it. Each exclude glob becomes a leading
  # rule whose command exits
  # 0 without output, so first-match-wins skips the file. The whole
  # list is gated on proseLint.enable so disabling it drops the rule
  # along with the binary.
  #
  # Claude Code keeps each session's scratchpad and task output in the
  # directory claude-<uid> under claudeTmpDir. Most files there are
  # throwaway scripts and notes no reader sees, so those paths always
  # lead the exclude list. The Linter section of the prose skill and
  # the Plain output style tell Claude to run prose-lint by hand on a
  # draft there that a person reads. The globs match any uid, because
  # standalone home-manager has none to read. A sandboxed command on a
  # Darwin host gets /tmp/claude-<uid> as TMPDIR, so the globs for /tmp
  # apply on every host.
  proseLintScratchGlobs = lib.unique [
    "${claudeTmpDir}/claude-*/**"
    "/tmp/claude-*/**"
    "/private/tmp/claude-*/**"
  ];
  defaultLinterRules = lib.optionals cfg.proseLint.enable (
    map (glob: {
      pathGlob = glob;
      command = [ "${pkgs.coreutils}/bin/true" ];
      timeout = "5s";
    }) (proseLintScratchGlobs ++ cfg.proseLint.excludeGlobs)
    ++ [
      {
        pathGlob = "/**/*.{${lib.concatStringsSep "," pkgs.prose-lint.passthru.extensions}}";
        command = [ (lib.getExe pkgs.prose-lint) ];
        timeout = "15s";
      }
    ]
  );

  # Sops secrets that gitWrapper, kagiWrapper, spaceliftWrapper,
  # githubWrapper, and workmuxWrapped export into the session
  # environment. The sandbox credentials block denies them, so no
  # sandboxed command reads a token out of its own environment.
  #
  # The list spells the names out instead of deriving them from those
  # wrappers. The wrappers mix positional exportSecret calls, attrset
  # exportSecrets calls, and literal export lines. The union of their
  # variables also includes ARGOCD_BASE_URL, which is not a credential.
  #
  # DAGGER_CLOUD_TOKEN is missing here because the Dagger CLI needs it
  # inside the sandbox to reach Dagger Cloud. Mask mode, the middle
  # ground, breaks on macOS. HASS_TOKEN is missing because sandboxed
  # commands call the Home Assistant API with it.
  #
  # GITHUB_TOKEN and GITHUB_PERSONAL_ACCESS_TOKEN alias gh_token and
  # have no sops secret of their own. A literal export line in
  # githubWrapper sets GITHUB_PERSONAL_ACCESS_TOKEN, and literal export
  # lines in workmuxWrapped set both variables.
  deniedCredentialVars = [
    "ARGOCD_API_TOKEN"
    "ATUIN_PASSWORD"
    "ATUIN_USERNAME"
    "GH_TOKEN"
    "GITHUB_PERSONAL_ACCESS_TOKEN"
    "GITHUB_TOKEN"
    "KAGI_API_KEY"
    "SPACELIFT_API_KEY_ENDPOINT"
    "SPACELIFT_API_KEY_ID"
    "SPACELIFT_API_KEY_SECRET"
  ];

  # Default sandbox rules. Each list seeds an entry set under
  # dotfiles.claude.sandbox at mkDefault priority, so a host can drop an
  # entry by setting it to false. The settings.json lists and the
  # Read and Edit permission entries read the merged sets back through
  # sandboxFilesystem and sandboxNetwork.
  defaultDenyReadPaths = [ "/" ];

  defaultReadPaths = [
    "/nix/store"
    "/nix/var/nix/profiles"

    "/etc"
    "/private/etc"
    "/usr"
    "/bin"
    "/sbin"

    # macOS frameworks and developer tools (CLT, Xcode toolchain).
    "/Library/Frameworks"
    "/Library/Apple"
    "/Library/Developer"
    "/System"

    # Narrow /dev set — blanket /dev would expose /dev/disk*, /dev/kmem,
    # /dev/mem. Mirrors the nodes Claude Code auto-allows for writes plus
    # common stdin/entropy/fd nodes.
    "/dev/null"
    "/dev/zero"
    "/dev/random"
    "/dev/urandom"
    "/dev/stdin"
    "/dev/stdout"
    "/dev/stderr"
    "/dev/tty"
    "/dev/fd"
    "/dev/dtracehelper"
    "/dev/autofs_nowait"

    "/tmp"
    "/private/tmp"

    # macOS dyld shared cache — every binary launch reads this.
    "/var/db/dyld"
    "/private/var/db/dyld"

    # macOS per-user temp. Claude Code overrides $TMPDIR, but Apple
    # frameworks (codesign, security, simctl, parts of Xcode CLT)
    # resolve via confstr(_CS_DARWIN_USER_TEMP_DIR) and still hit
    # /var/folders.
    "/var/folders"
    "/private/var/folders"

    # Runtime dirs. /run is the Linux standard; on macOS the same
    # role lives at /private/var/run (with /var/run symlinked to it).
    "/run"
    "/private/var/run"

    # macOS default shell selector. /var/select/sh is the symlink some
    # Apple tooling consults to resolve the system shell.
    "/var/select/sh"
    "/private/var/select/sh"

    "/opt/homebrew"

    "~/.config"
    "~/.cache"
    "~/.local/state"
    "~/.local/share"
    "~/.local/bin"
    "~/.claude"
    "~/.tflint.d"

    # ~/Library narrowed — blanket access leaks Keychains, Cookies,
    # Messages, Mail, Containers, Application Support subdirs, etc.
    "~/Library/Caches"
    "~/Library/Preferences"
    "~/Library/Fonts"
    "~/Library/Frameworks"

    "~/Documents/repos"
    "~/Documents/screenshots"
    "~/go"

    # home-manager `home.file` places these directly under $HOME, so
    # `~/.config/**` and friends don't cover them.
    "~/Taskfile.yaml"
    "~/task"

    # Global gitconfig and gitignore. Resolved via XDG, but listed
    # explicitly so git can read them even when the sandbox does not
    # follow the home-manager-files symlink chain into /nix/store.
    "~/.config/git/config"
    "~/.config/git/ignore"

    # Listed explicitly so the read list doesn't depend on the vaultsDir
    # default (which resolves under ~/Library/Mobile Documents/...).
    researchDir
  ]
  ++ bundledReadPaths;

  defaultWritePaths = [
    "~/go/pkg"
    "~/Library/Caches"
    "~/.cache/nix"
    "~/.cache/helm"
    "~/.cache/gh"
    "~/.cache/uv"
    "~/.local/share/uv"
    # Recent gh releases also touch the config dir at runtime and fail
    # when it is read-only. Safe to expose: auth is env-var only (sops
    # GH_TOKEN), so gh never stores a token in hosts.yml, and the
    # hosts.yml Read deny below applies regardless.
    "~/.config/gh"
    "~/.local/state/workmux"
    "~/.local/state/hook-router"
    "~/.local/state/gh"
    "~/.local/share/claude"
    "~/.local/share/gh"
    "~/.tflint.d"

    # macOS per-user temp. Apple tooling (codesign, security, simctl,
    # Xcode CLT) resolves $TMPDIR via confstr(_CS_DARWIN_USER_TEMP_DIR)
    # and writes scratch files under /var/folders regardless of the
    # $TMPDIR Claude Code exports.
    "/var/folders"
    "/private/var/folders"

    researchDir
    cfg.archivesDir
  ]
  ++ claudeTmpPaths
  ++ bundledWritePaths;

  # Without XDG_RUNTIME_DIR, as on macOS, Claude Code binds the
  # messaging socket of each session in the directory cc-socks under
  # claudeTmpDir. On a Darwin host, claudeTmpDir lies in
  # ~/Library/Caches, which is a sandbox write path. This entry keeps
  # sandboxed commands from removing or replacing those sockets.
  defaultDenyWritePaths = lib.optionals pkgs.stdenv.hostPlatform.isDarwin [
    "${claudeTmpDir}/cc-socks"
  ];

  defaultUnixSockets = [
    "/nix/var/nix/daemon-socket/socket"
    (
      if config.dotfiles.tmux.socketPath != null then
        config.dotfiles.tmux.socketPath
      else
        "/private/tmp/tmux-501/default"
    )
  ]
  ++ bundledSockets;

  defaultAllowedDomains = [
    "jacobcolvin.com"
    "registry.dagger.io"
    "api.dagger.cloud"
    "auth.dagger.cloud"
    "proxy.golang.org"
    "sum.golang.org"
    # GitHub serves release downloads from here after redirecting
    # github.com/<owner>/<repo>/releases/download URLs.
    "release-assets.githubusercontent.com"
  ]
  ++ bundledDomains;

  # The sandbox blocks these even when an allowedDomains entry matches.
  defaultDeniedDomains = [
    # Azure Application Insights telemetry sink. az, dotnet, and VS Code
    # tooling report here.
    "dc.services.visualstudio.com"
  ];

  # Default permission rules. Each list seeds an entry set under
  # dotfiles.claude.permissions at mkDefault priority, so a host can drop
  # an entry by setting it to false. Tool bundle entries join the same
  # sets, so a host can drop those the same way.
  defaultDenyPermissions = [
    # Key material & certificates
    "Read(//**/*.key)"
    "Read(//**/*.p12)"
    "Read(//**/*.jks)"
    "Read(//**/*.asc)"
    "Read(//**/*.keystore)"
    "Read(//**/*.kdbx)"
    "Read(//**/wallet.dat)"
    "Read(//**/keystore/**)"
    "Read(//**/.ssh/**)"
    "Read(//**/.gnupg/**)"

    # Generic secrets
    "Read(//**/.env)"
    "Read(//**/.env.*)"
    "Read(//**/.secrets/**)"
    "Read(//**/.git-credentials)"
    "Read(//**/git/credentials)"
    "Read(//**/.netrc)"
    "Read(//**/.curlrc)"
    "Read(//**/.wgetrc)"
    "Read(//**/.password-store/**)"

    # Cloud credentials
    "Read(//**/.aws/credentials)"
    "Read(//**/.aws/config)"
    "Read(//**/.aws/sso/**)"
    "Read(//**/.azure/**)"
    "Read(//**/.config/gcloud/**)"
    "Read(//**/.config/hcloud/config.json)"
    "Read(//**/.config/argocd/**)"
    "Read(//**/.snyk)"
    "Read(//**/.wrangler/**)"

    # Container & Kubernetes
    "Read(//**/.docker/config.json)"
    "Read(//**/.docker/certs.d/**)"
    "Read(//**/.config/containers/auth.json)"
    "Read(//**/.talos/**)"
    "Read(//**/.cosign/**)"
    "Read(//**/.helm/repository/repositories.yaml)"

    # Secret managers & encryption
    "Read(//**/.doppler/**)"
    "Read(//**/age/keys.txt)"
    # sops-nix decrypts to defaultSymlinkPath,
    # ~/.config/sops-nix/secrets, a symlink into secrets.d
    # under the darwin per-user temp dir, so both spellings
    # appear here.
    "Read(//**/sops-nix/**)"
    "Read(//**/secrets.d/**)"
    "Read(//**/rclone.conf)"
    "Read(//**/.op/**)"
    "Read(//**/.config/op/**)"

    # IaC state & credentials
    "Read(//**/credentials.tfrc.json)"
    "Read(//**/.terraformrc)"
    "Read(//**/.terraform.d/credentials.tfrc.json)"
    "Read(//**/*.tfstate)"
    "Read(//**/*.tfstate.*)"
    "Read(//**/.pulumi/credentials.json)"

    # CI/CD & deployment tokens
    "Read(//**/.config/gh/hosts.yml)"
    "Read(//**/.jira.d/config.yml)"
    "Read(//**/.config/dagger/**)"
    "Read(//**/.config/spacelift/**)"
    "Read(~/.spacelift/**)"

    # Package manager credentials
    "Read(//**/.npmrc)"
    "Read(//**/.pypirc)"
    "Read(//**/.cargo/credentials.toml)"
    "Read(//**/.gem/credentials)"
    "Read(//**/.m2/settings.xml)"
    "Read(//**/.m2/settings-security.xml)"
    "Read(//**/.gradle/gradle.properties)"
    "Read(//**/.composer/auth.json)"
    "Read(//**/.config/poetry/auth.toml)"
    "Read(//**/.bunfig.toml)"

    # Claude Code credentials
    "Read(//**/.claude/.credentials.json)"

    # Developer tool sessions & generated keys
    "Read(//**/atuin/key)"
    "Read(//**/.lima/_config/user)"
  ]
  # On the Lima guest image ~/.kube/config is a readable
  # local kubectx source (guestKubeconfigLocal); everywhere
  # else it holds host admin creds and stays denied. The
  # sidecar and scoped kubeconfigs live under
  # $CLAUDE_KUBECTX_DIR / $XDG_STATE_HOME, not any .kube/
  # path, so they are unaffected either way.
  ++ lib.optionals (!cfg.guestKubeconfigLocal) [
    "Read(//**/.kube/config)"
    "Read(//**/.kube/config*)"
  ]
  ++ bundledDeny;

  defaultRules = entries: lib.genAttrs entries (_: lib.mkDefault true);
  enabledRules = rules: lib.attrNames (lib.filterAttrs (_: enabled: enabled) rules);

  # Whether a settings permission entry covers an MCP tool: the exact
  # tool name, a bare server name, or a name with `*` globs.
  permissionEntryCovers =
    entry: tool:
    builtins.match (lib.replaceStrings [ "\\*" ] [ ".*" ] (lib.escapeRegex entry)) tool != null
    || (!(lib.hasInfix "__" (lib.removePrefix "mcp__" entry)) && lib.hasPrefix "${entry}__" tool);

  # Settings ask and allow entries that cover a tool with a conditional
  # rule. Claude Code prompts on a settings ask rule even when a hook
  # allows the call, so an ask entry cancels the rule. An allow entry
  # runs every call that reaches the settings without a hook decision.
  # The assertion in `config` fails evaluation on each entry here.
  conditionalRuleTools = lib.unique (map (rule: rule.tool) bundledConditionalRules);
  conditionalRuleConflictsIn =
    list:
    lib.concatMap (
      tool:
      map (entry: "${tool} is covered by the permissions.${list} entry ${entry}") (
        lib.filter (entry: permissionEntryCovers entry tool) (enabledRules cfg.permissions.${list})
      )
    ) conditionalRuleTools;
  conditionalRuleConflicts = conditionalRuleConflictsIn "ask" ++ conditionalRuleConflictsIn "allow";

  # Conditions that pair a pattern or a repo field with the wrong test.
  # hook-router rejects them, so the build of its wrapper fails on
  # them. The assertion in `config` reports them first, at evaluation,
  # and names the option to change.
  conditionalRuleOperandFaults = lib.concatMap (
    rule:
    map (c: "${rule.tool} field ${c.field} with test ${c.test}") (
      lib.filter (
        c:
        (c.test == "matches" || c.test == "branchMatches") != (c.pattern != "")
        || (c.test == "branchMatches") != (c.repoField != "")
      ) rule.conditions
    )
  ) bundledConditionalRules;

  sandboxFilesystem = lib.mapAttrs (_: enabledRules) cfg.sandbox.filesystem;
  sandboxNetwork = lib.mapAttrs (_: enabledRules) cfg.sandbox.network;

  readPermEntries = map (p: "Read(${toPermGlob p})") sandboxFilesystem.allowRead;

  # Edit(path) rules cover all file-editing tools (Write, Edit,
  # NotebookEdit); a separate Write(path) rule is not matched by file
  # permission checks and triggers a startup warning since Claude Code
  # 2.1.210.
  writePermEntries = lib.concatMap (p: [
    "Read(${toPermGlob p})"
    "Edit(${toPermGlob p})"
  ]) sandboxFilesystem.allowWrite;

  bundledInstructions =
    let
      items = lib.concatMap (b: b.instructions.items) bundleValues;
    in
    lib.optionalString (items != [ ]) (
      "## Tools\n\n" + lib.concatMapStringsSep "\n" (i: "- ${i}") items
    );
in
{
  imports = [ ./playwright.nix ];

  options.dotfiles.claude = {
    kubeApiDomains = mkOption {
      type = types.listOf types.str;
      default = [ ];
      description = ''
        Kubernetes API server hostnames that `mcp__kubectx__select`
        is allowed to switch to. The same set is added to the sandbox
        network allowlist (effective on darwin, where Seatbelt
        sandboxing is active) so kubectl can reach them. Empty (the
        default) lets the tool select any apiserver in the kubeconfig;
        sandbox network access still has to be granted elsewhere when
        needed.
      '';
    };

    kubeClusterRole = mkOption {
      type = types.str;
      default = "view";
      description = "ClusterRole to bind ServiceAccounts to when selecting a Kubernetes context.";
    };

    kubectxSocketSlots = mkOption {
      type = types.ints.positive;
      default = 16;
      description = ''
        Number of UDS slots `mcp-kubectx serve` may bind. Each slot is
        enumerated as a literal entry in the sandbox's allowUnixSockets
        list, since Claude Code matches that allowlist as exact paths
        rather than as globs. Bumping this allows more concurrent Claude
        sessions on this host before slot exhaustion fails new serve
        starts. Drives both the `--socket-slots` flag passed to the
        binary and the size of the rendered allowlist; the two cannot
        drift because they share this option.
      '';
    };

    guestKubeconfigLocal = mkOption {
      type = types.bool;
      default = false;
      description = ''
        Treat the guest's own `~/.kube/config` as an in-sandbox
        "local" kubectx source. When enabled, the Claude launcher
        wrapper exports `$CLAUDE_KUBECTX_GUEST_CONFIG` and splices the
        guest config into the middle of the merged `$KUBECONFIG`
        (`$CLAUDE_KUBECTX_LOCAL : ~/.kube/config :
        $CLAUDE_KUBECTX_SIDECAR`), mcp-kubectx enumerates and routes
        its contexts as `(local)`, and the `~/.kube/config*` Read deny
        is dropped so the Read tool can see guest cluster definitions.
        A guest-local cluster (kind / k3d / minikube / Talos-in-Docker)
        then behaves like any normal cluster from any guest shell, with
        no per-consumer kubeconfig merge glue.

        Enable this ONLY on the Lima guest image that runs Claude
        inside the sandbox (`hosts/nixos/terrarium.nix`). On the macOS
        host and every other profile it must stay false: there
        `~/.kube/config` holds real admin credentials that must never
        enter the sandbox, the Read deny stays in force, and the merge
        keeps its byte-identical two-entry form.
      '';
    };

    dangerouslySkipPermissions = mkOption {
      type = types.bool;
      default = false;
      description = ''
        Start Claude Code in bypass-permissions mode instead of
        accept-edits mode. Sets IS_SANDBOX=1, passes
        --allow-dangerously-skip-permissions, sets
        permissions.defaultMode to bypassPermissions, pre-trusts the
        home directory, and runs gh auth login.
      '';
    };

    skipPlanReview = mkOption {
      type = types.bool;
      default = true;
      description = ''
        Skip the plan-reviewer deny gate on the first ExitPlanMode call.
        When disabled, hook-router denies the first ExitPlanMode of a
        session and instructs Claude to run the plan-reviewer agent
        first. When enabled, ExitPlanMode proceeds without the review
        round-trip. All plan-guard bookkeeping (plan path, baseline SHA,
        clearing in_plan_mode, the pending-plan handoff) still happens,
        so the Stop hook's post-implementation review gate is
        unaffected.
      '';
    };

    extraSettings = mkOption {
      type = types.attrsOf types.anything;
      default = { };
      description = "Additional settings merged into Claude Code settings.json.";
    };

    stylixTheme = mkOption {
      type = types.submodule {
        options = {
          enable = mkOption {
            type = types.bool;
            default = config.stylix.enable or false;
            description = "Generate ~/.claude/themes/stylix.json from the active stylix base16 scheme and select it as the Claude Code theme.";
          };
        };
      };
      default = { };
      description = "Wire Claude Code's custom-theme JSON to the active stylix base16 palette.";
    };

    hostContext = mkOption {
      type = types.lines;
      default = "";
      description = ''
        Host-specific prose appended to the generated ~/.claude/CLAUDE.md
        under a "## Host Environment" section. Use this to tell Claude
        Code about the environment it's running in (e.g. "You're in docker").
        Empty (the default) emits no section at all.
      '';
    };

    powerline = mkOption {
      type = types.submodule {
        options = {
          theme = mkOption {
            type = types.str;
            default = "custom";
            description = ''
              Top-level `theme` key in the emitted claude-powerline config,
              also used as the dynamic attr key under which `colors` is
              emitted (`colors.<theme>`). If you set `theme` to a value
              other than `custom`, also override `colors` — otherwise the
              stylix-derived default palette will be emitted under that
              theme name, shadowing any built-in palette claude-powerline
              ships.
            '';
          };

          colors = mkOption {
            type = types.attrsOf powerlineColorType;
            default = {
              directory = {
                bg = "#${colors.base09}";
                fg = "#${colors.base00}";
              };
              git = {
                bg = "#${colors.base02}";
                fg = "#${colors.base0E}";
              };
              model = {
                bg = "#${colors.base0B}";
                fg = "#${colors.base00}";
              };
              session = {
                bg = "#${colors.base01}";
                fg = "#${colors.base0C}";
              };
              today = {
                bg = "#${colors.base00}";
                fg = "#${colors.base0B}";
              };
              tmux = {
                bg = "#${colors.base02}";
                fg = "#${colors.base0B}";
              };
              context = {
                bg = "#${colors.base0B}";
                fg = "#${colors.base00}";
              };
              contextWarning = {
                bg = "#${colors.base09}";
                fg = "#${colors.base00}";
              };
              contextCritical = {
                bg = "#${colors.base08}";
                fg = "#${colors.base00}";
              };
              metrics = {
                bg = "#${colors.base02}";
                fg = "#${colors.base05}";
              };
              version = {
                bg = "#${colors.base02}";
                fg = "#${colors.base04}";
              };
              env = {
                bg = "#${colors.base01}";
                fg = "#${colors.base0E}";
              };
              weekly = {
                bg = "#${colors.base01}";
                fg = "#${colors.base0D}";
              };
            };
            description = ''
              Per-segment colors keyed by claude-powerline segment name
              (directory, git, model, session, today, tmux, context,
              contextWarning, contextCritical, metrics, version, env,
              weekly), not theme name — the outer theme wrapping is
              applied at emit time. Overriding a segment requires setting
              both `bg` and `fg`; partial overrides fail module
              evaluation. Unknown segment names pass type-checking and
              are silently ignored by claude-powerline. The default
              palette is derived from `config.lib.stylix.colors`;
              replacing this option with a literal attrset decouples the
              status line from the host's base16 scheme.
            '';
          };

          display = mkOption {
            type = types.submodule {
              options = {
                style = mkOption {
                  type = types.str;
                  default = "powerline";
                  description = "Display style passed through to claude-powerline.";
                };
                charset = mkOption {
                  type = types.str;
                  default = "unicode";
                  description = "Character set passed through to claude-powerline.";
                };
                colorCompatibility = mkOption {
                  type = types.str;
                  default = "auto";
                  description = "Color compatibility mode passed through to claude-powerline.";
                };
                autoWrap = mkOption {
                  type = types.bool;
                  default = true;
                  description = "Whether claude-powerline auto-wraps lines that exceed the terminal width.";
                };
                padding = mkOption {
                  type = types.int;
                  default = 1;
                  description = "Padding applied between segments.";
                };
                lines = mkOption {
                  type = types.listOf (
                    types.submodule {
                      options.segments = mkOption {
                        type = types.attrsOf powerlineSegmentType;
                        default = { };
                        description = "Segments rendered on this line, keyed by segment name.";
                      };
                    }
                  );
                  default = [
                    {
                      segments = {
                        git = {
                          enabled = true;
                          showRepoName = true;
                        };
                        context = {
                          enabled = true;
                          showPercentageOnly = false;
                          displayStyle = "text";
                          autocompactBuffer = 100000;
                        };
                      };
                    }
                    # The `block` segment, which shows the 5-hour rate
                    # limit window, is absent because Anthropic removed
                    # that limit.
                    {
                      segments = {
                        # `showPace` prints a second percentage after
                        # the usage, as in `62%/57%`. The second
                        # percentage is the usage that an even spend
                        # over the 7-day window reaches by the time of
                        # the render.
                        weekly = {
                          enabled = true;
                          showPace = true;
                        };
                      };
                    }
                  ];
                  description = ''
                    Lines rendered by claude-powerline, top-to-bottom.
                    Lists replace, not merge: a host that overrides this
                    option must supply the full layout. The same
                    replacement applies one level deeper — redefining a
                    single line's `segments` drops the other segments on
                    that line rather than merging. Segment names inside
                    `segments` must match the set claude-powerline
                    recognizes; unknown names pass type-checking and are
                    silently ignored.
                  '';
                };
              };
            };
            default = { };
            description = "claude-powerline `display` config block.";
          };

          budget = mkOption {
            type = types.submodule {
              options = {
                session = mkOption {
                  type = mkBudgetType {
                    amount = 220000;
                    type = "tokens";
                  };
                  default = { };
                  description = "Per-session budget.";
                };
              };
            };
            default = { };
            description = ''
              claude-powerline `budget` config block. The `weekly`
              segment does not read a budget. It shows the percentage of
              the 7-day rate limit that Claude Code reports.
            '';
          };
        };
      };
      default = { };
      description = "claude-powerline status line configuration emitted to ~/.config/claude-powerline/config.json.";
    };

    extraFetchRules = mkOption {
      type = types.submodule {
        options = {
          deny = mkOption {
            type = types.listOf denyRuleType;
            default = [ ];
            description = "Additional deny rules appended to the base mcp-fetch rules.";
          };
          allow = mkOption {
            type = types.listOf urlMatchType;
            default = [ ];
            description = "Additional allow rules appended to the base mcp-fetch allowlist.";
          };
          robotsExempt = mkOption {
            type = types.listOf urlMatchType;
            default = [ ];
            description = ''
              Additional URL patterns exempted from mcp-fetch's robots.txt
              enforcement. Enforcement stays on for every host not named
              here, so exempting one site never disables it globally.
            '';
          };
        };
      };
      default = { };
      description = "Extra mcp-fetch URL filtering rules merged with the base deny, allow, and robots-exemption lists.";
    };

    extraCommandRules = mkOption {
      type = types.submodule {
        options = {
          deny = mkOption {
            type = types.listOf commandRuleType;
            default = [ ];
            description = ''
              Extra bash command deny rules, appended after the
              bundle-contributed deny rules in hook-router's
              evaluation order. No `allow` counterpart exists:
              hook-router has only deny and ask rules with optional
              per-rule `except` exemptions; non-matching commands
              fall through to the normal permission flow.
            '';
          };
          ask = mkOption {
            type = types.listOf commandRuleType;
            default = [ ];
            description = ''
              Extra bash command ask rules, appended after the
              bundle-contributed ask rules in hook-router's
              evaluation order. All deny rules are evaluated before
              any ask rule.
            '';
          };
        };
      };
      default = { };
      description = "Extra hook-router command deny/ask rules appended after bundle-contributed rules.";
    };

    formatterRules = mkOption {
      type = types.listOf formatterRuleType;
      default = [ ];
      description = ''
        Extra hook-router formatter routing rules, appended after the
        built-in plans and research mdformat rules. Each rule maps an
        absolute file-path glob to a formatter argv; the matched path
        is appended as the final argument. Rules are evaluated in
        order on PostToolUse:Write/Edit for the written file and on
        PostToolUse:Bash for each file the command changed
        (bashEditDiffEnabled); the first matching glob wins.
      '';
    };

    clauth.enable = mkOption {
      type = types.bool;
      default = false;
      description = "Install clauth, the Claude Code account switcher and usage monitor.";
    };

    proseLint = mkOption {
      type = types.submodule {
        options = {
          enable = mkOption {
            type = types.bool;
            default = true;
            description = ''
              Run prose-lint (the Weir rules in configs/harper) on every
              markdown and source file Claude writes, and return its
              findings as feedback before the next turn. Also lint, on
              PreToolUse, the message a `git commit` or `gh pr create`
              carries and the MCP tool arguments that toolFields names,
              and deny the call with the findings. Installs prose-lint
              and harper so files can be linted by hand and rules
              iterated with `harper-cli test`. When false, no linter
              rule or message lint is registered and the packages are
              not installed.
            '';
          };
          toolFields = mkOption {
            type = proseToolFieldsType;
            default = { };
            example = {
              mcp__slack__post_message = [ "text" ];
            };
            description = ''
              MCP tool arguments that prose-lint checks before the call
              runs. Each key is a tool pattern in the MCP permission
              rule syntax: an exact tool name, a trailing `*` prefix
              glob, or a bare `mcp__<server>` for every tool on a
              server. Each value lists top-level tool_input keys whose
              string values go to the linter one at a time. Tool
              bundles add their own entries through
              `toolBundles.<name>.proseLint.toolFields`. Findings deny
              the call with the commit-message tier policy. Only
              `mcp__` tool names reach the hook, so a key for any other
              tool has no effect.
            '';
          };
          excludeGlobs = mkOption {
            type = types.listOf types.nonEmptyStr;
            default = [ ];
            description = ''
              Absolute file-path globs (doublestar syntax, see
              formatterRules) that prose-lint skips. Each becomes a
              leading linter rule that exits 0, so first-match-wins
              stops the prose-lint rule from seeing the file. The hook
              always skips every file under the temporary directory
              `claude-<uid>` of Claude Code, which holds the session
              scratchpad, so the list does not need to name that
              directory.
            '';
          };
        };
      };
      default = { };
      description = "prose-lint hook settings.";
    };

    linterRules = mkOption {
      type = types.listOf linterRuleType;
      default = [ ];
      description = ''
        Extra hook-router linter routing rules, appended after the
        built-in prose-lint rule. Each rule maps an absolute file-path
        glob to a linter argv; the matched path is appended as the
        final argument. Rules are evaluated in order on
        PostToolUse:Write/Edit/MultiEdit after the formatter has run;
        the first matching glob wins, so a rule here only fires on
        files prose-lint does not cover.
      '';
    };

    outputCompaction = mkOption {
      type = types.submodule {
        options = {
          enable = mkOption {
            type = types.bool;
            default = true;
            description = ''
              Whether hook-router compacts redundant successful Bash
              output on PostToolUse:Bash. Failing commands route to
              PostToolUseFailure, which carries no output, so this is
              a success-path-only optimization.
            '';
          };
          stripAnsi = mkOption {
            type = types.bool;
            default = true;
            description = "Whether to strip ANSI/VT escape sequences (color, cursor, OSC) from the surfaced output.";
          };
          minRunLength = mkOption {
            type = types.ints.positive;
            default = 3;
            description = ''
              Minimum number of consecutive byte-identical lines before a
              run is collapsed to one line plus a marker. Shorter runs
              pass through verbatim.
            '';
          };
          minBytes = mkOption {
            type = types.ints.unsigned;
            default = 2048;
            description = ''
              Skip compaction for outputs smaller than this many bytes,
              checked against the raw pre-strip length. Keeps the
              transforms off small outputs that carry no redundancy
              worth collapsing.
            '';
          };
          streams = mkOption {
            type = types.listOf (
              types.enum [
                "stdout"
                "stderr"
              ]
            );
            default = [
              "stdout"
              "stderr"
            ];
            description = ''
              Which tool_response output streams to compact. An empty
              list compacts nothing (equivalent to disabling the
              feature).
            '';
          };
          saveFullOutput = mkOption {
            type = types.bool;
            default = true;
            description = ''
              Whether hook-router archives the uncompacted stream to a
              file under ''${XDG_STATE_HOME}/hook-router/outputs before
              compacting it, appending a one-line pointer to the
              surfaced output that names the file so the model can read
              back the exact ANSI and repeated lines compaction drops.
              When false, compaction stays lossy: no file is written and
              no pointer is appended. This toggle gates the
              --compaction-output-dir wrapper flag and is not serialized
              into --compaction-config.
            '';
          };
        };
      };
      default = { };
      description = ''
        hook-router PostToolUse:Bash output compaction. camelCase keys so
        `builtins.toJSON` matches the Go struct tags in
        tools/hook-router/compactor.go CompactConfig.
      '';
    };

    searchRewrite = mkOption {
      type = types.submodule {
        options = {
          enable = mkOption {
            type = types.bool;
            default = true;
            description = ''
              Whether hook-router transparently rewrites `grep` and
              `find` Bash commands into `rg` and `bfs` on
              PreToolUse:Bash. When false, no --search-rewrite-config
              flag is passed and the wrapper leaves search commands
              untouched.
            '';
          };
          grep = mkOption {
            type = types.bool;
            default = true;
            description = ''
              Whether to rewrite `grep` into `rg`, mapping the known
              flag set and injecting exclude globs. Conservative:
              commands using an unmapped flag or a BRE pattern that
              would mis-translate are left untouched.
            '';
          };
          find = mkOption {
            type = types.bool;
            default = true;
            description = ''
              Whether to rewrite `find` into `bfs` with a global
              `-exclude` prune of `dotfiles.findExcludes` injected after
              the command word.
            '';
          };
        };
      };
      default = { };
      description = ''
        hook-router PreToolUse:Bash search rewriting. camelCase keys so
        `builtins.toJSON` matches the Go struct tags in
        tools/hook-router/searchrewrite/searchrewrite.go Config. The
        wrapper adds the `dotfiles.findExcludes` list of home/options.nix
        to the JSON as the `findExcludes` field. Each entry of the list
        becomes a bfs `-exclude` clause and an rg `-g` exclude glob.
      '';
    };

    sleepGuard = mkOption {
      type = types.submodule {
        options = {
          enable = mkOption {
            type = types.bool;
            default = true;
            description = ''
              Whether hook-router denies a foreground `sleep` on
              PreToolUse:Bash. A `sleep` whose duration is not a
              literal (`sleep $VAR`, `sleep $((5*60))`) is always
              denied, since its length cannot be read. A command whose
              only content is `sleep` plus no-op filler (`echo`,
              `true`, `jobs`) is denied at any duration as a filler
              wait, so the ceiling only governs sleeps embedded in real
              work. A Bash call with `run_in_background` set is never
              checked: a poll loop inside a background call is the
              documented way to wait on a condition, so `sleep` there
              is correct rather than a smell.
            '';
          };
          maxSeconds = mkOption {
            type = types.ints.positive;
            default = 10;
            description = ''
              Longest foreground `sleep` embedded in real work that
              stays allowed, in seconds. Durations sum across operands
              (`sleep 5 10` is 15s) and GNU suffixes count (`5m` is
              300s).
            '';
          };
        };
      };
      default = { };
      description = ''
        hook-router PreToolUse:Bash foreground-`sleep` guard. camelCase
        keys so `builtins.toJSON` matches the Go struct tags in
        tools/hook-router/sleepguard/sleepguard.go Config.
      '';
    };

    rmGuard = mkOption {
      type = types.bool;
      default = true;
      description = ''
        Whether hook-router denies an `rm` or `rmdir` on PreToolUse:Bash
        when Claude Code cannot prove the target safe. That covers a
        target that starts with a variable that can expand to nothing
        (`rm -rf $DIR/$name`), a command substitution target, a
        relative glob after a `cd`, a glob in a directory that is not
        a literal path, and a glob that spans more than one directory
        level. The guard also reads a script that a call passes to a
        shell with `-c`. Claude Code forces a permission prompt on
        those patterns in every permission mode. The call waits on the
        prompt until a person answers it or someone stops the session.
        Until then the prompt holds the agent that made the call. The
        deny reaches the agent first and carries the rewrite. See
        tools/hook-router/rmguard for the matching rules.
      '';
    };

    promptGuard = mkOption {
      type = types.bool;
      default = true;
      description = ''
        Whether hook-router denies each call of a Workflow agent that
        needs a permission prompt. Claude Code shows the prompt in the
        session that launched the Workflow, and the call waits until a
        person answers. No one watches that session during a long run,
        so one prompt holds the agent and its Workflow until someone
        stops the run. With the guard on, hook-router denies the call
        on PreToolUse where one of its rules asks for every other
        caller, and the deny carries the reason of the ask. On
        PermissionRequest, it also answers with a deny the prompts
        that Claude Code raises itself and lets a hook answer, such
        as the prompt of a `permissions.ask` rule. Claude Code skips
        that event for a prompt that only a person can answer, so
        such a prompt still holds the agent. The main conversation
        and the agents of the Agent tool keep their prompts. See
        tools/hook-router/promptguard for the marks of a Workflow
        agent.
      '';
    };

    escapeRestore = mkOption {
      type = types.submodule {
        options = {
          enable = mkOption {
            type = types.bool;
            default = true;
            description = ''
              Whether hook-router puts back the `\uXXXX` escapes that
              Claude Code decoded in the text of a Write or an Edit.
              Claude Code decodes each such escape in a tool input
              before the tool runs, so Go source that spells a
              character as an escape reaches the file with the
              character itself. hook-router reads the text that the
              model sent from the session transcript and rewrites the
              text that the call wrote. It leaves every other tool
              alone. A Bash command and an MCP tool argument keep the
              decoded characters.
            '';
          };
          maxWaitSeconds = mkOption {
            type = types.ints.positive;
            default = 120;
            description = ''
              Longest wait for the transcript entry of a Write or an
              Edit, in seconds. Claude Code runs a tool as soon as the
              model has sent the call. It writes the transcript entry
              after the model has sent its whole message, so the entry
              of a call that other calls follow arrives late.
              hook-router waits only when the text of the call holds a
              character outside ASCII. The model is still sending its
              message during the wait, so the wait delays only the
              later calls of that message. When the entry does not
              arrive, the hook tells the agent which characters it did
              not check. The value extends the timeout of the Write and
              Edit hook entries. A call whose escapes all name ASCII
              characters gets one read of the transcript, so the
              restore misses that call when its entry arrives late.
            '';
          };
        };
      };
      default = { };
      description = ''
        hook-router PostToolUse restore of decoded `\uXXXX` escapes
        after a Write or an Edit. camelCase keys so `builtins.toJSON`
        matches the Go struct tags in tools/hook-router/escapes.go
        escapeRestore. The restore reads `wireToolInputs` from the
        transcript, which Claude Code does not document. Set `enable`
        to false when a Claude Code release moves that data.
      '';
    };

    fetchAllowlist = mkOption {
      type = types.bool;
      default = true;
      description = "Whether to enforce the mcp-fetch URL allowlist. When false, all URLs are allowed unless explicitly denied.";
    };

    skills = mkOption {
      type = types.attrsOf (
        types.submodule {
          options = {
            enable = mkOption {
              type = types.bool;
              default = true;
              description = "Whether this skill is installed into ~/.claude/skills/<name>.";
            };
            source = mkOption {
              type = types.either types.path types.lines;
              description = "Skill directory path, or raw SKILL.md contents.";
            };
          };
        }
      );
      default = { };
      description = "Claude Code skills keyed by skill name. Defaults populated by this module; override `<name>.enable = false` to drop a bundled skill, or set `<name>.source` to add a custom one.";
    };

    agents = mkOption {
      type = types.attrsOf (
        types.submodule {
          options = {
            enable = mkOption {
              type = types.bool;
              default = true;
              description = "Whether this agent is installed into ~/.claude/agents/<name>.md.";
            };
            source = mkOption {
              type = types.either types.path types.lines;
              description = "Agent markdown file path, or raw agent body contents.";
            };
          };
        }
      );
      default = { };
      description = "Claude Code agents keyed by agent name (no .md suffix). Defaults populated by this module; override `<name>.enable = false` to drop a bundled agent, or set `<name>.source` to add a custom one.";
    };

    outputStyles = mkOption {
      type = types.attrsOf (
        types.submodule {
          options = {
            enable = mkOption {
              type = types.bool;
              default = true;
              description = "Whether this output style is installed into ~/.claude/output-styles/<name>.md.";
            };
            source = mkOption {
              type = types.either types.path types.lines;
              description = "Output style markdown file path, or raw output style contents.";
            };
          };
        }
      );
      default = { };
      description = "Claude Code output styles keyed by file name (no .md suffix). Defaults populated by this module; override `<name>.enable = false` to drop a bundled style, or set `<name>.source` to add a custom one.";
    };

    outputStyle = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = "Output style Claude Code starts sessions with, matched against the `name` frontmatter of a custom style or a built-in name. Null leaves the built-in default active.";
    };

    postImplSkills = mkOption {
      type = types.attrsOf (
        types.submodule (
          { name, ... }:
          {
            options = {
              enable = mkOption {
                type = types.bool;
                default = true;
                description = "Whether this option is offered in the Stop-gate post-implementation AskUserQuestion catalog.";
              };
              label = mkOption {
                type = types.str;
                default = "/${name}";
                description = "Slash-command label Claude dispatches when the user picks this option. Includes the leading slash.";
              };
              description = mkOption {
                type = types.str;
                description = "One-line description shown alongside the label in the post-implementation picker.";
              };
            };
          }
        )
      );
      default = { };
      description = "Post-implementation slash commands offered when a plan's Stop gate fires. Defaults populated by this module; override `<name>.enable = false` to drop a bundled option, or add an entry with `<name>.description` to extend the catalog. The label defaults to `/<name>`; set `<name>.label` to override.";
    };

    extraMcpServers = mkOption {
      type = types.attrsOf types.anything;
      default = { };
      description = "Additional MCP servers deep-merged into programs.mcp.servers.";
    };

    remoteControl = lib.mkEnableOption "Claude Code remote control for all sessions";

    research = {
      vault = mkOption {
        type = types.nonEmptyStr;
        default = "docs";
        description = "Obsidian vault name (under dotfiles.obsidian.vaultsDir) where the research skill writes reports.";
      };
      useVault = mkOption {
        type = types.bool;
        default = pkgs.stdenv.hostPlatform.isDarwin;
        description = "Whether CLAUDE_RESEARCH_DIR resolves to the Obsidian vault. Defaults to true on Darwin. Set true on Linux hosts that have the vault mounted at the same absolute path as the Darwin host (e.g. terrarium inside a workmux sandbox).";
      };
    };

    archivesDir = mkOption {
      type = types.nonEmptyStr;
      default = "${config.home.homeDirectory}/Documents/archives";
      description = ''
        Output directory for the web-archive skill (btrix, yt-dlp).
        Added to the sandbox write allowlist and bind-mounted into
        the Lima sandbox at the same host and guest path.
      '';
    };

    attribution = mkOption {
      type = types.submodule {
        options = {
          commit = mkOption {
            type = types.str;
            default = "";
            description = "Attribution footer appended to commit messages. Empty string disables attribution.";
          };
          pr = mkOption {
            type = types.str;
            default = "";
            description = "Attribution footer appended to pull request descriptions. Empty string disables attribution.";
          };
          sessionUrl = mkOption {
            type = types.bool;
            default = false;
            description = ''
              Append the claude.ai session link to commits (as a
              Claude-Session trailer) and pull request descriptions made
              from web and Remote Control sessions. Independent of the
              commit and pr footers; Claude Code defaults it to true.
            '';
          };
        };
      };
      default = { };
      description = "Per-host attribution settings for commits and PRs authored via Claude Code.";
    };

    # Workmux writes cpus, memory, and disk into the Lima config only when
    # it creates the sandbox VM. The "Sandbox VM" section of README.md
    # describes how to resize a VM that exists.
    lima = {
      enable = lib.mkEnableOption "Lima sandbox backend";
      cpus = mkOption {
        type = types.int;
        default = 8;
        description = "Number of CPUs allocated to the Lima VM.";
      };
      memory = mkOption {
        type = types.str;
        default = "8GiB";
        description = "Memory allocated to the Lima VM.";
      };
      disk = mkOption {
        type = types.str;
        default = "80GiB";
        description = "Disk size allocated to the Lima VM.";
      };
    };

    workmux = {
      agent = mkOption {
        type = types.str;
        default = "claude";
        description = "Value emitted for workmux's top-level agent key in ~/.config/workmux/config.yaml. Workmux uses this label to pick the output-pattern profile for status routing (working / waiting / done) and to select which agents.<name> entry defines the launch command for the focused pane's <agent> placeholder. Override on hosts that drive workmux with a different coding agent.";
      };
      command = mkOption {
        type = types.str;
        default = "claude";
        description = "Launch command registered as the agents.<agent> entry in workmux's config and consumed via the <agent> placeholder in the focused pane. Decoupled from agent because flag conventions vary between coding agents.";
      };
    };

    permissions =
      let
        ruleSet =
          description:
          mkOption {
            type = types.attrsOf types.bool;
            default = { };
            inherit description;
          };
      in
      {
        allow = ruleSet "Tool patterns for the permissions allow list. Set an entry to false to drop it. The sandbox allowRead and allowWrite paths add their own Read and Edit rules on top of this set.";
        deny = ruleSet "Tool patterns for the permissions deny list. Set an entry to false to drop it.";
        ask = ruleSet "Tool patterns for the permissions ask list. Set an entry to false to drop it.";
      };

    sandbox =
      let
        ruleSet =
          description:
          mkOption {
            type = types.attrsOf types.bool;
            default = { };
            inherit description;
          };
      in
      mkOption {
        type = types.submodule {
          options = {
            filesystem = {
              allowRead = ruleSet "Paths that sandboxed commands can read. Each enabled path also gets a Read permission allow rule.";
              allowWrite = ruleSet "Paths that sandboxed commands can read and write. Each enabled path also gets Read and Edit permission allow rules.";
              denyRead = ruleSet "Paths that sandboxed commands cannot read. An allowRead or allowWrite entry overrides this list for the paths it matches.";
              denyWrite = ruleSet "Paths that sandboxed commands cannot write, including paths under an allowWrite entry.";
            };
            network = {
              allowUnixSockets = ruleSet "Unix socket paths that sandboxed commands can connect to. Claude Code matches each entry as an exact path, not a glob.";
              allowedDomains = ruleSet "Network domains that sandboxed commands can reach.";
              deniedDomains = ruleSet "Network domains that the sandbox blocks, even when an allowedDomains entry matches.";
            };
          };
        };
        default = { };
        example = lib.literalExpression ''
          {
            filesystem.allowWrite."~/Documents/notes" = true;
            filesystem.allowRead."/opt/homebrew" = false;
            network.allowedDomains."example.com" = true;
          }
        '';
        description = ''
          Sandbox rules written to the `sandbox` block of settings.json,
          keyed by path, socket, or domain. Set an entry to `true` to add
          it, or to `false` to remove it. This module and the enabled
          tool bundles set their entries with `lib.mkDefault`, so a host
          can remove any of them with a plain `false`.
        '';
      };

    toolBundles = mkOption {
      type = types.attrsOf (
        types.submodule {
          options = {
            enable = mkOption {
              type = types.bool;
              default = true;
              description = "Whether this tool bundle is enabled.";
            };
            servers = mkOption {
              type = types.attrsOf types.anything;
              default = { };
              description = "MCP server definitions merged into programs.mcp.servers.";
            };
            permissions = {
              allow = mkOption {
                type = types.listOf types.str;
                default = [ ];
                description = "Tool patterns appended to the permissions allow list.";
              };
              deny = mkOption {
                type = types.listOf types.str;
                default = [ ];
                description = "Tool patterns appended to the permissions deny list.";
              };
              ask = mkOption {
                type = types.listOf types.str;
                default = [ ];
                description = "Tool patterns appended to the permissions ask list.";
              };
            };
            conditionalRules = mkOption {
              type = types.listOf conditionalRuleType;
              default = [ ];
              example = lib.literalExpression ''
                [
                  {
                    tool = "mcp__git__git_push";
                    conditions = [
                      {
                        field = "ref";
                        test = "branchMatches";
                        pattern = "(feat|fix)/.+";
                        repoField = "repo";
                      }
                      {
                        field = "tags";
                        test = "absentOrZero";
                      }
                    ];
                  }
                ]
              '';
              description = ''
                Conditional allow rules for this bundle's MCP tools.
                hook-router reads the arguments of each call on
                PreToolUse. A call that meets every condition of one
                rule runs without a prompt, and every other call of the
                tool prompts. Under `promptGuard`, a Workflow agent's
                call gets a deny in place of that prompt. Two rules that
                name one tool are alternatives. A `permissions.deny`
                entry that covers the tool still denies every call, and
                prose-lint still checks the `proseLint.toolFields` of a
                call before the rule allows it.

                A tool with a rule must have no `permissions.ask` or
                `permissions.allow` entry, and evaluation fails when it
                has one. Claude Code prompts on a settings ask rule even
                when a hook allows the call, so hook-router has to be
                the only source of the tool's decision. When hook-router
                emits no decision, for example because it exits with an
                error, the permission mode decides the call. The call
                then prompts in `default` and `acceptEdits` mode, and
                runs in `bypassPermissions` mode.

                Aggregated at `enable` granularity, so the bundle's
                rules turn off with the bundle.
              '';
            };
            sandbox = {
              allowedDomains = mkOption {
                type = types.listOf types.str;
                default = [ ];
                description = "Network domains to add to the sandbox allowlist.";
              };
              allowUnixSockets = mkOption {
                type = types.listOf types.str;
                default = [ ];
                description = "Unix socket paths to add to the sandbox allowlist.";
              };
              allowRead = mkOption {
                type = types.listOf types.str;
                default = [ ];
                description = "Filesystem paths to add to the sandbox read allowlist.";
              };
              allowWrite = mkOption {
                type = types.listOf types.str;
                default = [ ];
                description = "Filesystem paths to add to the sandbox write allowlist.";
              };
            };
            instructions = {
              items = mkOption {
                type = types.listOf types.str;
                default = [ ];
                description = "Instruction lines rendered as bullets under the `## Tools` heading in ~/.claude/CLAUDE.md.";
              };
            };
            alwaysLoad = mkOption {
              type = types.bool;
              default = false;
              description = ''
                Set `alwaysLoad = true` on every server in this bundle so its
                tool schemas load at session start rather than being deferred
                behind Claude Code's ToolSearch. Use sparingly — each upfront
                tool consumes context that would otherwise be available for
                the conversation. See
                https://code.claude.com/docs/en/mcp#configure-tool-search.
              '';
            };
            fetchRules = {
              deny = mkOption {
                type = types.listOf denyRuleType;
                default = [ ];
                description = ''
                  mcp-fetch deny rules contributed by this bundle. Aggregated
                  at `enable` granularity, so redirect messages that point at
                  this bundle's MCP tools automatically turn off when the
                  bundle is disabled.
                '';
              };
              allow = mkOption {
                type = types.listOf urlMatchType;
                default = [ ];
                description = "mcp-fetch allow rules contributed by this bundle.";
              };
              robotsExempt = mkOption {
                type = types.listOf urlMatchType;
                default = [ ];
                description = ''
                  URL patterns this bundle exempts from mcp-fetch's
                  robots.txt enforcement. Enforcement stays on for every
                  host not named here.
                '';
              };
            };
            proseLint.toolFields = mkOption {
              type = proseToolFieldsType;
              default = { };
              description = ''
                MCP tool arguments that prose-lint checks, contributed by
                this bundle. The format matches `proseLint.toolFields`,
                and the entries merge with it. Aggregated at `enable`
                granularity, so the bundle's rules turn off with the
                bundle.
              '';
            };
            commandRules = {
              deny = mkOption {
                type = types.listOf commandRuleType;
                default = [ ];
                description = ''
                  hook-router PreToolUse:Bash deny rules contributed by
                  this bundle. Aggregated at `enable` granularity, so
                  redirect messages that point at this bundle's MCP
                  tools turn off automatically when the bundle is
                  disabled.
                '';
              };
              ask = mkOption {
                type = types.listOf commandRuleType;
                default = [ ];
                description = ''
                  hook-router PreToolUse:Bash ask rules contributed by
                  this bundle. A matching command gets a forced
                  permission prompt, even when settings allow rules or
                  sandbox auto-allow would otherwise let it run. Under
                  `promptGuard`, a Workflow agent's command gets a deny
                  in place of that prompt. List order matters (first
                  match wins): scope rules to subcommands before a
                  catch-all fallback for the same command. All deny
                  rules are evaluated before any ask rule.
                '';
              };
            };
          };
        }
      );
      default = { };
      description = "Tool bundles grouping MCP servers, permissions, sandbox rules, mcp-fetch rules, and CLAUDE.md instructions.";
    };
  };

  config = {
    assertions = [
      {
        assertion = conditionalRuleConflicts == [ ];
        message = ''
          dotfiles.claude: a tool with a toolBundles.<name>.conditionalRules
          rule must have no permissions.ask or permissions.allow entry.
          Claude Code prompts on a settings ask rule even when hook-router
          allows the call, and a settings allow rule runs a call that
          hook-router did not decide. Remove the entry, or the rule:
          ${lib.concatMapStrings (conflict: "  - ${conflict}\n") conditionalRuleConflicts}'';
      }
      {
        assertion = conditionalRuleOperandFaults == [ ];
        message = ''
          dotfiles.claude: in toolBundles.<name>.conditionalRules, a
          condition must set `pattern` when its test is `matches` or
          `branchMatches`, and `repoField` when its test is
          `branchMatches`. It must leave each one empty for every other
          test:
          ${lib.concatMapStrings (fault: "  - ${fault}\n") conditionalRuleOperandFaults}'';
      }
      {
        # On a Linux host, the git bundle tells agents to clone into
        # /tmp/git. A NixOS host cleans /tmp every day, and the cleanup
        # deletes single files of those clones. The tmpfiles rule of
        # hosts/nixos/clones.nix keeps the cleanup out of the
        # directory. This assertion fails the evaluation of a NixOS
        # host that has the bundle and lacks the rule. osConfig holds
        # the tmpfiles rules only on a NixOS host.
        assertion =
          !(cfg.toolBundles.git.enable && osConfig ? systemd.tmpfiles.rules)
          || lib.elem "x ${cloneRoot}" osConfig.systemd.tmpfiles.rules;
        message = ''
          dotfiles.claude: toolBundles.git tells agents to clone into
          ${cloneRoot}, and this NixOS host has no systemd-tmpfiles rule
          `x ${cloneRoot}`. The daily cleanup of /tmp then deletes single
          files of the clones. hosts/nixos/clones.nix sets the rule, and
          hosts/nixos/default.nix imports that module. Import one of the
          two files in the host module.
        '';
      }
      {
        # macOS deletes single files of a clone under /tmp, and no
        # setting keeps that cleanup out of a directory. The comment on
        # cloneRoot describes the cleanup. This assertion fails the
        # evaluation of a Darwin host that has the git bundle and
        # clones into /tmp.
        assertion =
          !(pkgs.stdenv.hostPlatform.isDarwin && cfg.toolBundles.git.enable) || !(inTmp cloneRoot);
        message = ''
          dotfiles.claude: toolBundles.git tells agents to clone into
          ${cloneRoot} on this Darwin host. macOS deletes each file
          under /tmp that nothing read or changed in the last three
          days, so a clone there loses single files. Set cloneRoot in
          home/claude.nix to a directory outside /tmp for a Darwin
          host.
        '';
      }
      {
        # Claude Code keeps the temporary files of each session in the
        # directory claude-<uid> under claudeTmpDir. A NixOS host cleans
        # /tmp every day, and the cleanup deletes single files of that
        # directory. The tmpfiles rule of hosts/nixos/claude-tmp.nix
        # keeps the cleanup out of the directory. This assertion fails
        # the evaluation of a NixOS host that lacks the rule. osConfig
        # holds the tmpfiles rules only on a NixOS host.
        assertion =
          !(osConfig ? systemd.tmpfiles.rules)
          || lib.elem "x ${claudeTmpDir}/claude-[0-9]*" osConfig.systemd.tmpfiles.rules;
        message = ''
          dotfiles.claude: Claude Code keeps the temporary files of each
          session in ${claudeTmpDir}/claude-<uid>, and this NixOS host
          has no systemd-tmpfiles rule `x ${claudeTmpDir}/claude-[0-9]*`.
          The daily cleanup of /tmp then deletes single files of a
          session scratchpad. hosts/nixos/claude-tmp.nix sets the rule,
          and hosts/nixos/default.nix imports that module. Import one of
          the two files in the host module.
        '';
      }
      {
        # macOS deletes single files of a session scratchpad under /tmp,
        # and no setting keeps that cleanup out of a directory. The
        # comment on claudeTmpDir describes the cleanup. This assertion
        # fails the evaluation of a Darwin host that keeps the
        # temporary files of Claude Code in /tmp.
        assertion = !(pkgs.stdenv.hostPlatform.isDarwin && inTmp claudeTmpDir);
        message = ''
          dotfiles.claude: Claude Code keeps the temporary files of each
          session in ${claudeTmpDir}/claude-<uid> on this Darwin host.
          macOS deletes each file under /tmp that nothing read or
          changed in the last three days, so a session scratchpad there
          loses single files. Set claudeTmpDir in home/claude.nix to a
          directory outside /tmp for a Darwin host.
        '';
      }
      {
        # macOS deletes single files of the kubectx directory of a
        # session under /tmp, and no setting keeps that cleanup out of a
        # directory. The comment on kubectxSessionParent describes the
        # files. Without a value, the wrapper keeps the directory in
        # /tmp, because macOS does not set XDG_RUNTIME_DIR. This
        # assertion fails the evaluation of a Darwin host that names no
        # parent or a parent in /tmp.
        assertion =
          !pkgs.stdenv.hostPlatform.isDarwin
          || (kubectxSessionParent != null && !(inTmp kubectxSessionParent));
        message = ''
          dotfiles.claude: the claude wrapper keeps the kubectx files of
          each session in /tmp on this Darwin host. macOS deletes each
          file under /tmp that nothing read or changed in the last three
          days, so a session there loses the record of the context that
          it selected. Set kubectxSessionParent in home/claude.nix to a
          directory outside /tmp for a Darwin host.
        '';
      }
    ];

    dotfiles.claude.skills = {
      aven.source = "${pkgs.aven}/share/aven/skills/claude";
      aven-ultraclose.source = ../configs/claude/skills/aven-ultraclose;
      commit.source = ../configs/claude/skills/commit;
      commit-push-pr.source = ../configs/claude/skills/commit-push-pr;
      create-skill.source = ../configs/claude/skills/create-skill;
      dagger-modules.source = ../configs/claude/skills/dagger-modules;
      file-manager.source = ../configs/claude/skills/file-manager;
      frontend-design.source = ../configs/claude/skills/frontend-design;
      worktree.source = ../configs/claude/skills/worktree;
      wm-merge.source = ../configs/claude/skills/wm-merge;
      wm-rebase.source = ../configs/claude/skills/wm-rebase;
      wm-coordinator.source = ../configs/claude/skills/wm-coordinator;
      wm-spawn.source = ../configs/claude/skills/wm-spawn;
      wm-workmux.source = ../configs/claude/skills/wm-workmux;
      git-surgeon.source = ../configs/claude/skills/git-surgeon;
      presenterm.source = ../configs/claude/skills/presenterm;
      prose.source = ../configs/claude/skills/prose;
      research.source = ../configs/claude/skills/research;
      review-implementation.source = ../configs/claude/skills/review-implementation;
      self-improve.source = ../configs/claude/skills/self-improve;
      skill-creator.source = ../configs/claude/skills/skill-creator;
      taskfile.source = ../configs/claude/skills/taskfile;
      technical-writing.source = ../configs/claude/skills/technical-writing;
      track-issue.source = ../configs/claude/skills/track-issue;
      upgrade.source = ../configs/claude/skills/upgrade;
      web-archive.source = ../configs/claude/skills/web-archive;
    };

    dotfiles.claude.agents = {
      implementation-reviewer-code.source = ../configs/claude/agents/implementation-reviewer-code.md;
      implementation-reviewer-docs.source = ../configs/claude/agents/implementation-reviewer-docs.md;
      plan-reviewer.source = ../configs/claude/agents/plan-reviewer.md;
    };

    dotfiles.claude.outputStyles = {
      plain.source = ../configs/claude/output-styles/plain.md;
    };

    dotfiles.claude.outputStyle = "Plain";

    dotfiles.claude.sandbox = {
      filesystem = {
        allowRead = defaultRules defaultReadPaths;
        allowWrite = defaultRules defaultWritePaths;
        denyRead = defaultRules defaultDenyReadPaths;
        denyWrite = defaultRules defaultDenyWritePaths;
      };
      network = {
        allowUnixSockets = defaultRules defaultUnixSockets;
        allowedDomains = defaultRules defaultAllowedDomains;
        deniedDomains = defaultRules defaultDeniedDomains;
      };
    };

    dotfiles.claude.permissions = {
      allow = defaultRules bundledAllow;
      deny = defaultRules defaultDenyPermissions;
      ask = defaultRules bundledAsk;
    };

    dotfiles.claude.postImplSkills = {
      review-implementation.description = "Review code changes against the plan.";
      simplify.description = "Review and simplify the implemented code.";
      commit.description = "Wrap up the cycle by creating a git commit.";
    };

    dotfiles.claude.toolBundles = {
      fetch = {
        alwaysLoad = true;
        servers.fetch = {
          type = "stdio";
          command = "${pkgs.mcp-fetch}/bin/mcp-fetch";
          args = [
            "--db"
            "${config.xdg.stateHome}/mcp-fetch/fetches.db"
            "--rules-file"
            "${fetchRules}"
            "--log-file"
            "${config.xdg.stateHome}/mcp-fetch/fetch.log"
          ];
        };
        permissions.allow = [ "mcp__fetch__fetch" ];
        permissions.deny = [
          "WebSearch"
          "WebFetch"
        ];
        # Reddit's robots.txt is a blanket "Disallow: /" on every host it
        # serves, aimed at bulk scrapers. These fetches are human-directed
        # and one page at a time, so the exemption is scoped to reddit.com
        # rather than turning enforcement off globally.
        fetchRules.robotsExempt = [ { host = "(.*\\.)?reddit\\.com"; } ];
        # www.reddit.com answers non-browser clients with a bot-verification
        # interstitial; old.reddit.com serves the real thread markup.
        fetchRules.deny = [
          {
            host = "(www\\.)?reddit\\.com";
            reason = "www.reddit.com serves a bot-verification page instead of content. Fetch the same path on old.reddit.com instead.";
          }
        ];
        instructions = {
          items = [
            "Use `mcp__fetch__fetch` for fetching known URLs and web page content."
            "Fetch Reddit threads from `old.reddit.com`; `www.reddit.com` returns a verification page, and the `.json` and `.rss` endpoints are rate-limited."
          ];
        };
      };

      git = {
        alwaysLoad = true;
        servers.git = {
          type = "stdio";
          command = "${gitWrapper}";
          args = [
            "--allow-dir"
            cloneRoot
            # git_fetch, git_pull, and git_push work on the checkouts
            # that live here. git_clone checks its dest against the
            # same list, so it also accepts a dest in this tree. The
            # instruction of this bundle still sends each clone to
            # cloneRoot.
            "--allow-dir"
            "${config.home.homeDirectory}/Documents/repos"
            # Linux VMs (lima/terrarium) mount the macOS repos tree
            # at its original /Users path; allow that spelling too.
            # On darwin this duplicates the entry above, which is
            # harmless.
            "--allow-dir"
            "/Users/${config.home.username}/Documents/repos"
          ];
        };
        # Every git command runs without a prompt except the ones that
        # write to a remote. permissions.ask lists the Bash forms of
        # those commands other than git push. The push rule of
        # commandRules.deny decides a Bash git push, and
        # conditionalRules decides mcp__git__git_push. The allow
        # entries matter on hosts where the sandbox auto-allow is off.
        # On auto-allow hosts hook-router already allows git.
        # Permission rules evaluate deny, then ask, then allow,
        # whatever their specificity, so each ask entry wins over
        # Bash(git *).
        permissions.allow = [
          "mcp__git__git_clone"
          "mcp__git__git_fetch"
          "mcp__git__git_pull"
          "Bash(git)"
          "Bash(git *)"
        ];
        # Bash forms of the git commands that write to a remote.
        #
        # git push has no entry. The push rule of commandRules.deny
        # denies each Bash push except a push that names a local path,
        # and that push writes to no other host. Claude Code prompts on
        # a settings ask rule even when a hook allows the call. An
        # entry for git push therefore makes the push to a local path
        # prompt. Under promptGuard, a Workflow agent gets a deny in
        # place of that prompt.
        #
        # hook-router does not match a push that another command
        # starts, such as `timeout 30 git push`. Claude Code reads that
        # command as a git command, so Bash(git *) allows it without a
        # prompt. Bash(git *) also allows each push when hook-router
        # emits no decision.
        permissions.ask = [
          "Bash(git send-email)"
          "Bash(git send-email *)"
          "Bash(git lfs push *)"
          "Bash(git svn dcommit)"
          "Bash(git svn dcommit *)"
        ];
        # mcp__git__git_push has no permissions.ask entry, because
        # hook-router decides each call on its arguments. A push runs
        # without a prompt when the name of the pushed branch matches
        # conventionalBranchPattern and the call does not push tags.
        # Every other push prompts. force_with_lease, set_upstream, and
        # remote do not change the decision.
        #
        # A call with no `ref` pushes the current branch of `repo`, so
        # hook-router asks git for that branch. The call prompts when
        # git names no branch, as on a detached HEAD. hook-router and
        # mcp-git both ignore GIT_DIR, GIT_COMMON_DIR,
        # GIT_REFERENCE_BACKEND, and the other variables that name a
        # repository, so both read the branch of `repo`.
        #
        # mcp-git skips every pre-push hook and never pushes a
        # submodule. It also refuses a remote on the local file
        # system, whose hooks git runs as children of mcp-git. A push
        # that this rule allows therefore runs no pre-push hook outside
        # the Bash sandbox.
        #
        # Mirrored by pushRules in tools/hook-router/mcp_test.go, so
        # update both together.
        conditionalRules = [
          {
            tool = "mcp__git__git_push";
            conditions = [
              {
                field = "ref";
                test = "branchMatches";
                pattern = conventionalBranchPattern;
                repoField = "repo";
              }
              {
                field = "tags";
                test = "absentOrZero";
              }
            ];
          }
        ];
        sandbox.allowWrite = [ cloneRoot ];
        # mcp-git refuses a repository on the local file system as the
        # source of a clone and as the remote of a fetch, a pull, or a
        # push. The rules for those commands therefore leave a call
        # that names such a repository to Bash. A clone always names
        # its source, so the clone rule matches only a call that names
        # a remote URL. A fetch, a pull, or a push that names no
        # repository reaches the default remote, and a plain word names
        # a remote. The fetch rule, the pull rule, and the push rule
        # therefore exempt only a call that names a local path. The
        # reason of each of these rules tells the agent to name that
        # path.
        #
        # A push to a local path runs without a prompt, because it
        # writes to no other host. git runs the hooks of the other
        # repository as children of the Bash command, so no hook runs
        # outside the Bash sandbox.
        #
        # canonicalRules mirrors these rules in
        # `tools/hook-router/helpers_test.go` and in
        # `tools/hook-router/cmdrules/cmdrules_test.go`, so update the
        # three lists together.
        commandRules.deny = [
          {
            command = "git";
            args = [ "clone" ];
            onlyRemoteURL = true;
            reason = "Direct git clone of a remote URL is blocked. Use mcp__git__git_clone instead. A clone from a local path runs in Bash.";
          }
          {
            command = "git";
            args = [ "fetch" ];
            exceptLocalPath = true;
            reason = "Direct git fetch usage is blocked. Use mcp__git__git_fetch instead. That tool refuses a remote on the local file system. Fetch from one in Bash by a literal path that starts with `/`, `./`, `../`, or `file://`, as in `git fetch ../other main`.";
          }
          {
            command = "git";
            args = [ "pull" ];
            exceptLocalPath = true;
            reason = "Direct git pull usage is blocked. Use mcp__git__git_pull instead. That tool refuses a remote on the local file system. Pull from one in Bash by a literal path that starts with `/`, `./`, `../`, or `file://`, as in `git pull ../other main`.";
          }
          {
            command = "git";
            args = [ "push" ];
            exceptLocalPath = true;
            reason = "Direct git push usage is blocked. Use mcp__git__git_push instead. That tool refuses a remote on the local file system. Push to one in Bash by a literal path that starts with `/`, `./`, `../`, or `file://`, as in `git push ../other main`.";
          }
          {
            # `git remote update` fetches every remote, so it reaches the
            # network the same way `git fetch` does. The other `git
            # remote` leaves stay local and fall through.
            #
            # The command takes the names of remotes and never a path,
            # so the rule exempts no call. Its reason names the fetch
            # by path that updates the remote-tracking branches of a
            # remote on the local file system.
            command = "git";
            args = [
              "remote"
              "update"
            ];
            reason = "Direct git remote update usage is blocked. Use mcp__git__git_fetch instead. That tool refuses a remote on the local file system. Fetch from one in Bash by a literal path that starts with `/`, `./`, `../`, or `file://`, and name its remote-tracking branches in the refspec, as in `git fetch ../other '+refs/heads/*:refs/remotes/origin/*'`.";
          }
        ];
        fetchRules.deny = [
          {
            host = "raw\\.githubusercontent\\.com";
            except = [ { path = ".*\\.md"; } ];
            reason = "Use mcp__git__git_clone to clone the repo to ${cloneRoot}/<owner>/<repo> and read files locally instead of fetching raw GitHub files.";
          }
          {
            host = "github\\.com";
            path = "/[^/]+/[^/]+/(blob|tree)(/.*)?";
            reason = "Use mcp__git__git_clone to clone the repo to ${cloneRoot}/<owner>/<repo> and read files locally instead of fetching GitHub file pages.";
          }
          {
            host = "gitlab\\.com";
            path = "/.+/-/(blob|tree)(/.*)?";
            reason = "Use mcp__git__git_clone to clone the repo to ${cloneRoot}/<owner>/<repo> and read files locally instead of fetching GitLab file pages.";
          }
          {
            host = "codeberg\\.org";
            path = "/[^/]+/[^/]+/src/(branch|commit|tag)/.*";
            reason = "Use mcp__git__git_clone to clone the repo to ${cloneRoot}/<owner>/<repo> and read files locally instead of fetching Codeberg file pages.";
          }
        ];
        instructions = {
          items = [
            "Use `mcp__git__git_clone` to clone repositories into `${cloneRoot}/<owner>/<repo>` and read from there."
            "Use `mcp__git__git_fetch`, `mcp__git__git_pull`, and `mcp__git__git_push` for git operations that contact a remote; plain `git` in Bash handles local work (add, commit, branch, rebase)."
            "`mcp__git__git_push` runs without a prompt when the pushed branch is named `<type>/<name>` and the call does not set `tags`. `<type>` is one of ${conventionalBranchTypeList}. Every other push prompts, so give each branch that you create for a push a name of that form, such as `fix/retry-timeout`."
          ];
        };
      };

      kagi = {
        alwaysLoad = true;
        servers.kagi = {
          type = "stdio";
          command = "${kagiWrapper}";
        };
        permissions.allow = [ "mcp__kagi__kagi_search_fetch" ];
        # kagi_extract overlaps with mcp__fetch__fetch, which stays the
        # preferred page fetcher.
        permissions.deny = [ "mcp__kagi__kagi_extract" ];
        fetchRules.deny = [
          {
            host = "(google|bing|duckduckgo|brave)\\.com";
            reason = "Fetching from general-purpose search engines is blocked. Use mcp__kagi__kagi_search_fetch instead.";
          }
          # bungie.net serves every news article as an empty application
          # shell and fills it in the browser. The mcp-fetch renderer
          # cannot run that application, so a fetch returns no content
          # with or without render_js. Kagi returns the article text.
          # The path accepts either case of `News` and `Article` because
          # the site links to `/news/article/<slug>` and redirects
          # `/en/News/Article/<id>` to `/7/en/News/article/<id>`.
          {
            host = "(www\\.)?bungie\\.net";
            path = "/7/[^/]+/[Nn]ews/[Aa]rticle/.*";
            reason = "bungie.net news articles load in the browser, and mcp-fetch cannot render them. Call mcp__kagi__kagi_search_fetch with the article URL as the query and extract_count set to 1 to read the full text.";
          }
        ];
        instructions = {
          items = [
            "Use `mcp__kagi__kagi_search_fetch` for web searches."
          ];
        };
      };

      nixos = {
        servers.nixos = {
          type = "stdio";
          command = "${pkgs.mcp-nixos}/bin/mcp-nixos";
        };
        permissions.allow = [
          "mcp__nixos__nix"
          "mcp__nixos__nix_versions"
        ];
        fetchRules.deny = [
          {
            host = "search\\.nixos\\.org";
            path = "/(packages|options)(\\?.*)?";
            reason = "Use mcp__nixos__nix for Nix package searches and NixOS/home-manager/nix-darwin option lookups instead of scraping search.nixos.org.";
          }
          {
            host = "home-manager-options\\.extranix\\.com";
            reason = "Use mcp__nixos__nix to look up home-manager options instead of scraping home-manager-options.extranix.com.";
          }
        ];
      };

      github =
        let
          # gh read subcommands that have a mcp__github__* equivalent.
          # These are denied at the hook level and redirected to the MCP
          # tool named in the value, so reads flow through the github MCP
          # rather than the gh CLI. Write commands live in
          # ghWriteRedirectGroups below. Mirrored by ghRedirectRules in
          # tools/hook-router/{helpers,cmdrules/cmdrules}_test.go; update
          # all together.
          ghRedirectGroups = {
            discussion = {
              view = "mcp__github__get_discussion (+ mcp__github__get_discussion_comments for the thread)";
              list = "mcp__github__list_discussions";
            };
            gist = {
              view = "mcp__github__get_gist";
              list = "mcp__github__list_gists";
            };
            issue = {
              view = "mcp__github__issue_read";
              list = "mcp__github__list_issues";
            };
            pr = {
              view = "mcp__github__pull_request_read";
              list = "mcp__github__list_pull_requests";
              diff = "mcp__github__pull_request_read (diff method)";
            };
            release = {
              view = "mcp__github__get_release_by_tag / mcp__github__get_latest_release";
              list = "mcp__github__list_releases";
            };
            label = {
              list = "mcp__github__list_label";
            };
            project = {
              view = "mcp__github__projects_get (get_project)";
              list = "mcp__github__projects_list (list_projects)";
              "field-list" = "mcp__github__projects_list (list_project_fields)";
              "item-list" = "mcp__github__projects_list (list_project_items)";
            };
            ruleset = {
              view = "mcp__github__repository_ruleset_read (get)";
              list = "mcp__github__repository_ruleset_read (list)";
              check = "mcp__github__repository_ruleset_read (get_rules_for_branch)";
            };
            run = {
              view = "mcp__github__actions_get (get_workflow_run) / mcp__github__get_job_logs for logs";
              list = "mcp__github__actions_list (list_workflow_runs)";
            };
            workflow = {
              view = "mcp__github__actions_get (get_workflow)";
              list = "mcp__github__actions_list (list_workflows)";
            };
          };
          ghRedirectCommands = {
            search = "mcp__github__search_code / search_issues / search_pull_requests / search_repositories";
          };

          # gh write subcommands whose mutation a github MCP write tool
          # covers. A deny rule at the hook level redirects each one to
          # the MCP tool named in the value. Each call of that tool
          # prompts, so the confirmation moves from the gh CLI ask rule
          # to the MCP permission prompt. create_pull_request,
          # update_pull_request, and update_pull_request_branch are the
          # exceptions. hook-router decides them with the rules in
          # conditionalRules of this bundle, and a call that meets a
          # rule runs without a prompt. Writes with no equivalent stay
          # with the ask rules below, such as pr comment, run delete,
          # workflow enable, workflow disable, and the issue writes.
          # These leaves stay out of the except lists of the ask rules
          # on purpose. The deny rules win first, and dropping one falls
          # back to a prompt rather than an allow. Mirrored by
          # ghWriteRedirectRules in
          # `tools/hook-router/{helpers,cmdrules/cmdrules}_test.go`, so
          # update all together.
          ghWriteRedirectGroups = {
            pr = {
              create = "mcp__github__create_pull_request";
              edit = "mcp__github__update_pull_request";
              close = "mcp__github__update_pull_request (state: closed)";
              reopen = "mcp__github__update_pull_request (state: open)";
              ready = "mcp__github__update_pull_request (draft: false)";
              merge = "mcp__github__merge_pull_request";
              review = "mcp__github__pull_request_review_write (+ add_comment_to_pending_review for inline comments)";
              "update-branch" = "mcp__github__update_pull_request_branch";
            };
            run = {
              rerun = "mcp__github__actions_run_trigger (rerun_workflow_run / rerun_failed_jobs)";
              cancel = "mcp__github__actions_run_trigger (cancel_workflow_run)";
            };
            workflow = {
              run = "mcp__github__actions_run_trigger (run_workflow)";
            };
          };

          # gh subcommands that clone a repository or read files out of
          # one over the API. They reach the same content the git
          # bundle's `git clone` deny and the raw.githubusercontent.com
          # fetch rule redirect, so they land on mcp__git__git_clone too.
          # `repo view` prints the README by default, which makes it a
          # file read; the endpoint serves no repository-metadata tool,
          # so the reason names search_repositories for the --json form.
          # Like ghWriteRedirectGroups these leaves stay out of the
          # ask-rule except lists, so dropping a rule falls back to a
          # prompt. Mirrored by ghCloneRedirectRules in
          # tools/hook-router/{helpers,cmdrules/cmdrules}_test.go; update
          # all together.
          ghCloneRedirectGroups = {
            gist = {
              clone = "mcp__git__git_clone";
            };
            repo = {
              clone = "mcp__git__git_clone";
              "read-dir" = "mcp__git__git_clone";
              "read-file" = "mcp__git__git_clone";
              view = "mcp__git__git_clone (the README) or mcp__github__search_repositories (repository metadata)";
            };
          };

          # gh subcommands with no mcp__github__* equivalent that read
          # GitHub or only touch local state (pr checkout, downloads,
          # gh's own config). The MCP does not serve these, so they stay
          # allowed on the gh CLI. `gh run watch` stays here because it
          # streams a run to completion, which no MCP tool does. Some
          # non-mutating forms stay out on purpose and prompt. auth
          # stays out because `gh auth token` and `status --show-token`
          # print credentials, and `config set` because it can swap the
          # browser, editor, or pager gh runs. `pr checks` stays here
          # for the same reason as `run watch`: its --watch form streams
          # to completion. Single source of truth for the
          # permission allow entries below. Mirrored by ghAskRules in
          # tools/hook-router/{helpers,cmdrules/cmdrules}_test.go.
          ghAllowGroups = {
            alias = [ "list" ];
            attestation = [
              "download"
              "trusted-root"
              "verify"
            ];
            cache = [ "list" ];
            codespace = [
              "list"
              "logs"
              "view"
            ];
            config = [
              "clear-cache"
              "get"
              "list"
            ];
            extension = [
              "list"
              "search"
            ];
            "gpg-key" = [ "list" ];
            issue = [ "status" ];
            org = [ "list" ];
            pr = [
              "checkout"
              "checks"
              "status"
            ];
            release = [
              "download"
              "verify"
              "verify-asset"
            ];
            repo = [
              "gitignore"
              "license"
              "list"
              "set-default"
            ];
            run = [
              "download"
              "watch"
            ];
            secret = [ "list" ];
            "ssh-key" = [ "list" ];
            variable = [
              "get"
              "list"
            ];
          };
          ghAllowCommands = [
            "browse"
            "completion"
            "licenses"
            "status"
          ];

          # Every group carrying a read-only leaf (allowed or redirected),
          # and its full read-only leaf set. The ask rules exempt these
          # leaves so only mutating subcommands prompt; redirected leaves
          # (read and write) are already caught by the deny rules, which
          # hook-router evaluates before any ask rule.
          ghGroups = lib.attrNames (ghAllowGroups // ghRedirectGroups);
          ghReadLeaves =
            group: (ghAllowGroups.${group} or [ ]) ++ lib.attrNames (ghRedirectGroups.${group} or { });

          ghAllowPair = prefix: [
            "Bash(gh ${prefix})"
            "Bash(gh ${prefix} *)"
          ];
        in
        {
          servers.github = {
            type = "stdio";
            command = "${githubWrapper}";
          };
          # Every read-only tool the x/all endpoint serves that
          # permissions.deny below does not strip. Spelled out so a tool
          # added upstream lands outside this list and prompts on first
          # use rather than running unannounced.
          permissions.allow = [
            "mcp__github__actions_get"
            "mcp__github__actions_list"
            "mcp__github__check_dependency_vulnerabilities"
            "mcp__github__custom_properties_read"
            "mcp__github__get_code_quality_finding"
            "mcp__github__get_code_scanning_alert"
            "mcp__github__get_copilot_space"
            "mcp__github__get_dependabot_alert"
            "mcp__github__get_discussion"
            "mcp__github__get_discussion_comments"
            "mcp__github__get_gist"
            "mcp__github__get_global_security_advisory"
            "mcp__github__get_job_logs"
            "mcp__github__get_label"
            "mcp__github__get_latest_release"
            "mcp__github__get_notification_details"
            "mcp__github__get_release_by_tag"
            "mcp__github__get_secret_scanning_alert"
            "mcp__github__get_tag"
            "mcp__github__github_support_docs_search"
            "mcp__github__issue_read"
            "mcp__github__list_code_scanning_alerts"
            "mcp__github__list_copilot_spaces"
            "mcp__github__list_dependabot_alerts"
            "mcp__github__list_discussion_categories"
            "mcp__github__list_discussions"
            "mcp__github__list_gists"
            "mcp__github__list_global_security_advisories"
            "mcp__github__list_issue_fields"
            "mcp__github__list_issue_types"
            "mcp__github__list_issues"
            "mcp__github__list_label"
            "mcp__github__list_notifications"
            "mcp__github__list_org_repository_security_advisories"
            "mcp__github__list_pull_requests"
            "mcp__github__list_releases"
            "mcp__github__list_repository_security_advisories"
            "mcp__github__list_secret_scanning_alerts"
            "mcp__github__list_starred_repositories"
            "mcp__github__list_tags"
            "mcp__github__projects_get"
            "mcp__github__projects_list"
            "mcp__github__pull_request_read"
            "mcp__github__repository_ruleset_read"
            "mcp__github__search_code"
            "mcp__github__search_commits"
            "mcp__github__search_issues"
            "mcp__github__search_orgs"
            "mcp__github__search_pull_requests"
            "mcp__github__search_repositories"
            "mcp__github__semantic_issue_similarity_search"
          ]
          # Read-only and local-only gh CLI commands with no MCP
          # equivalent, derived from the ghAllowGroups / ghAllowCommands
          # tables above. These cover hosts where the sandbox auto-allow
          # is off; mutating gh
          # commands are caught by the commandRules.ask rules below, and
          # reads with an MCP equivalent by the commandRules.deny rules,
          # which prompt / redirect on every host. An ask entry like
          # Bash(gh *) would shadow all of these (permission rules
          # evaluate deny -> ask -> allow, regardless of specificity), so
          # gh must not appear in the ask list.
          ++ lib.concatLists (
            lib.mapAttrsToList (
              group: leaves: lib.concatMap (leaf: ghAllowPair "${group} ${leaf}") leaves
            ) ghAllowGroups
          )
          ++ lib.concatMap ghAllowPair ghAllowCommands;
          # This list is the single source of truth for the github MCP tool
          # filter: the proxy wrapper derives its --deny-tool flags from it
          # (see ghProxyDenyFlags), so denying a tool here both blocks the
          # call and strips the tool's schema from tools/list, reclaiming the
          # context tokens it would otherwise cost.
          permissions.deny = [
            # GitHub MCP: deny redundant / low-value tools.
            "mcp__github__get_commit"
            "mcp__github__get_copilot_job_status"
            "mcp__github__get_file_contents"
            "mcp__github__get_me"
            # Browsing a repository's paths over the API is the same
            # detour the fetch rules and the gh api deny close off; clone
            # with mcp__git__git_clone and read the tree locally.
            "mcp__github__get_repository_tree"
            "mcp__github__get_team_members"
            "mcp__github__get_teams"
            "mcp__github__list_branches"
            "mcp__github__list_commits"
            "mcp__github__list_repository_collaborators"
            "mcp__github__search_users"
            # Overlaps mcp__kagi__kagi_search_fetch, which stays the one
            # web search tool.
            "mcp__github__web_search"
            # GitHub MCP: deny every write tool except the pull request
            # set that permissions.ask and conditionalRules of this
            # bundle gate. The proxy filter strips the schemas of the
            # denied tools from `tools/list`.
            "mcp__github__add_issue_comment"
            "mcp__github__assign_copilot_to_issue_with_intent"
            "mcp__github__create_branch"
            "mcp__github__create_gist"
            "mcp__github__create_or_update_file"
            "mcp__github__create_pull_request_with_copilot"
            "mcp__github__create_repository"
            "mcp__github__create_repository_ruleset"
            "mcp__github__custom_properties_write"
            "mcp__github__delete_file"
            "mcp__github__discussion_comment_write"
            "mcp__github__dismiss_notification"
            "mcp__github__fork_repository"
            "mcp__github__issue_write"
            "mcp__github__label_write"
            "mcp__github__manage_notification_subscription"
            "mcp__github__manage_repository_notification_subscription"
            "mcp__github__mark_all_notifications_read"
            "mcp__github__projects_write"
            "mcp__github__push_files"
            "mcp__github__request_copilot_review"
            "mcp__github__star_repository"
            "mcp__github__sub_issue_write"
            "mcp__github__unstar_repository"
            "mcp__github__update_gist"
            "mcp__github__update_issue_comment"
            "mcp__github__run_secret_scanning"
          ];
          # Pull request and Actions write tools. With the tools that
          # conditionalRules decides, they are the only write surface
          # the MCP serves here. Each call of a tool in this list
          # prompts, and so does each mutating gh CLI command.
          # add_comment_to_pending_review rides along with
          # pull_request_review_write, because a pending review needs
          # both. actions_run_trigger triggers, re-runs, and cancels
          # workflow runs.
          permissions.ask = [
            "mcp__github__actions_run_trigger"
            "mcp__github__add_comment_to_pending_review"
            "mcp__github__add_reply_to_pull_request_comment"
            "mcp__github__merge_pull_request"
            "mcp__github__pull_request_review_write"
          ];
          # mcp__github__create_pull_request has no permissions.ask
          # entry, because hook-router decides each call on its
          # arguments. A call runs without a prompt when it opens a
          # draft, the name of its head branch matches
          # conventionalBranchPattern, and it requests no reviewers.
          # Every other call prompts. `base`, `owner`, and `repo` do
          # not change the decision.
          #
          # GitHub does not merge a draft, and it requests no review
          # from code owners for one. `reviewers` requests a review
          # from each account that it names, so a call that sets it
          # prompts.
          #
          # A `head` of the form `owner:branch` names a branch of a
          # fork. An owner name holds no slash, so the pattern does not
          # match that form, and a pull request from a fork prompts.
          #
          # mcp__github__update_pull_request and
          # mcp__github__update_pull_request_branch have no
          # permissions.ask entry either. Agents call GitHub with the
          # token of the user, so the author of a pull request does not
          # show whether an agent opened it. hook-router therefore
          # records each pull request that create_pull_request opens
          # in its state database, through the CreatePullRequest hook
          # entry of PostToolUse. The record covers a call that the
          # user approved at a prompt. The `pullRequestRecorded` test
          # reads the record. Every session on the host reads it, so a
          # later session edits a pull request that an earlier session
          # opened. Each host keeps its own record.
          #
          # On a recorded pull request, update_pull_request runs
          # without a prompt when it sets only `title`, `body`,
          # `state`, or `draft: true`. `state` closes or reopens the
          # pull request. The call prompts when it marks the pull
          # request ready with `draft: false`, names a reviewer, or
          # sets `base` or `maintainer_can_modify`. The server applies
          # `base` and `maintainer_can_modify` at every value, so
          # their test is `absent`. update_pull_request_branch runs
          # without a prompt on a recorded pull request. Both tools
          # prompt on every other pull request.
          #
          # Each rule tests the record last, so hook-router opens the
          # database only for a call that meets the other conditions.
          #
          # prose-lint checks `title` and `body` before a rule allows
          # a call, through proseLint.toolFields of this bundle.
          #
          # Mirrored by pullRequestRules in
          # `tools/hook-router/mcp_test.go`, so update both together.
          conditionalRules = [
            {
              tool = "mcp__github__create_pull_request";
              conditions = [
                {
                  field = "draft";
                  test = "true";
                }
                {
                  field = "head";
                  test = "matches";
                  pattern = conventionalBranchPattern;
                }
                {
                  field = "reviewers";
                  test = "absentOrZero";
                }
              ];
            }
            {
              tool = "mcp__github__update_pull_request";
              conditions = [
                {
                  field = "draft";
                  test = "absentOrTrue";
                }
                {
                  field = "reviewers";
                  test = "absentOrZero";
                }
                {
                  field = "base";
                  test = "absent";
                }
                {
                  field = "maintainer_can_modify";
                  test = "absent";
                }
                {
                  field = "pullNumber";
                  test = "pullRequestRecorded";
                }
              ];
            }
            {
              tool = "mcp__github__update_pull_request_branch";
              conditions = [
                {
                  field = "pullNumber";
                  test = "pullRequestRecorded";
                }
              ];
            }
          ];
          # The prose that the write tools above publish. A cmdrules deny
          # redirects `gh pr create` to create_pull_request, so these
          # entries are what lints pull request text.
          proseLint.toolFields = {
            mcp__github__create_pull_request = [
              "title"
              "body"
            ];
            mcp__github__update_pull_request = [
              "title"
              "body"
            ];
            mcp__github__merge_pull_request = [
              "commit_title"
              "commit_message"
            ];
            mcp__github__pull_request_review_write = [ "body" ];
            mcp__github__add_comment_to_pending_review = [ "body" ];
            mcp__github__add_reply_to_pull_request_comment = [ "body" ];
          };
          # Redirect read-only gh subcommands that have a github MCP
          # equivalent to the MCP, derived from the ghRedirectGroups /
          # ghRedirectCommands / ghWriteRedirectGroups tables above.
          # hook-router evaluates deny rules before ask rules, so these
          # win over the mutating-command ask fallback below and over the
          # sandbox auto-allow. Subcommands with no MCP equivalent stay on
          # gh: reads run, writes prompt via the ask rules.
          commandRules.deny =
            # `gh api` is the CLI form of fetching api.github.com, which
            # the fetchRules below deny outright, and its
            # repos/*/contents/* endpoints are the same repository-file
            # browsing the git bundle denies on raw.githubusercontent.com
            # and github.com blob/tree pages. Denied as a whole because
            # the rule matcher compares positional args literally and
            # cannot scope on an endpoint path. Mirrored by
            # ghRedirectRules in
            # tools/hook-router/{helpers,cmdrules/cmdrules}_test.go.
            [
              {
                command = "gh";
                args = [ "api" ];
                reason = "`gh api` reaches api.github.com, which is denied. Clone with mcp__git__git_clone to read repository files locally, or use the mcp__github__* tools for issues, PRs, releases, and search.";
              }
            ]
            ++ lib.concatLists (
              lib.mapAttrsToList (
                group: leaves:
                lib.mapAttrsToList (leaf: tool: {
                  command = "gh";
                  args = [
                    group
                    leaf
                  ];
                  reason = "Read via ${tool} instead of the gh CLI.";
                }) leaves
              ) ghRedirectGroups
            )
            ++ lib.mapAttrsToList (cmd: tool: {
              command = "gh";
              args = [ cmd ];
              reason = "Read via ${tool} instead of the gh CLI.";
            }) ghRedirectCommands
            ++ lib.concatLists (
              lib.mapAttrsToList (
                group: leaves:
                lib.mapAttrsToList (leaf: tool: {
                  command = "gh";
                  args = [
                    group
                    leaf
                  ];
                  reason = "Write via ${tool} instead of the gh CLI.";
                }) leaves
              ) ghWriteRedirectGroups
            )
            ++ lib.concatLists (
              lib.mapAttrsToList (
                group: leaves:
                lib.mapAttrsToList (leaf: tool: {
                  command = "gh";
                  args = [
                    group
                    leaf
                  ];
                  reason = "Read via ${tool} instead of the gh CLI.";
                }) leaves
              ) ghCloneRedirectGroups
            );
          # Fail-closed gating for the gh CLI, derived from the
          # ghGroups / ghAllowCommands tables above: read-only subcommands
          # fall through (redirected by the deny rules above, allowed by
          # the permission entries above, or by the sandbox auto-allow),
          # everything else gets a forced prompt -- including gh
          # subcommands that do not exist yet. Order matters:
          # subcommand-scoped rules run before the top-level fallback.
          commandRules.ask =
            map (group: {
              command = "gh";
              args = [ group ];
              except = ghReadLeaves group;
              reason = "This gh subcommand can mutate GitHub state. Confirm before running.";
            }) ghGroups
            ++ [
              {
                command = "gh";
                except =
                  ghGroups
                  ++ ghAllowCommands
                  ++ [
                    "help"
                    "version"
                    "--version"
                  ];
                reason = "This gh subcommand is not on the read-only allowlist. Confirm before running; prefer mcp__github__* tools for reads.";
              }
            ];
          fetchRules.deny = [
            {
              host = "api\\.github\\.com";
              reason = "-> mcp__github__* / mcp__git__*";
            }
            {
              host = "github\\.com";
              path = "/[^/]+/[^/]+/issues(/.*)?";
              reason = "-> mcp__github__list_issues / mcp__github__issue_read";
            }
            {
              host = "github\\.com";
              path = "/[^/]+/[^/]+/pulls?(/.*)?";
              reason = "-> mcp__github__list_pull_requests / mcp__github__pull_request_read";
            }
            {
              host = "github\\.com";
              path = "/[^/]+/[^/]+/(commit|compare)(/.*)?";
              reason = "-> mcp__git__git_clone -> git show / git log";
            }
            {
              host = "github\\.com";
              path = "/[^/]+/[^/]+/releases(/.*)?";
              reason = "-> mcp__github__list_releases / mcp__github__get_latest_release";
            }
            {
              host = "github\\.com";
              path = "/[^/]+/[^/]+/tags(/.*)?";
              reason = "-> mcp__github__list_tags / mcp__github__get_tag";
            }
            {
              host = "github\\.com";
              path = "/search(/.*)?";
              reason = "-> mcp__github__search_*";
            }
          ];
          instructions = {
            items = [
              "Use `mcp__github__*` and `mcp__git__*` tools for reading GitHub data (issues, PRs, releases, Actions runs and job logs, security alerts, discussions, projects, code search, etc.)"
              "The `gh` CLI is available for functions the `mcp__github__*` tools do not serve. A hook redirects the `gh` subcommands that have an MCP equivalent. Note that `gh api` is always denied."
              "`mcp__github__create_pull_request` runs without a prompt when `draft` is true, `head` is a branch named `<type>/<name>`, and the call sets no `reviewers`. `<type>` is one of ${conventionalBranchTypeList}. Every other call prompts, so open each pull request as a draft from a branch of that form, such as `fix/retry-timeout`. On a pull request that an agent opened with this tool from this host, `mcp__github__update_pull_request` runs without a prompt when it sets only `title`, `body`, `state`, or `draft: true`, and `mcp__github__update_pull_request_branch` runs without a prompt. An edit that sets `draft: false`, `reviewers`, `base`, or `maintainer_can_modify` prompts, and so does every edit to another pull request."
            ];
          };
        };

      opentofu = {
        servers.opentofu = {
          type = "stdio";
          command = "${pkgs.mcp-opentofu}/bin/mcp-opentofu";
        };
        permissions.allow = [
          "mcp__opentofu__search_registry"
          "mcp__opentofu__get_provider_details"
          "mcp__opentofu__get_module_details"
          "mcp__opentofu__get_resource_docs"
          "mcp__opentofu__get_datasource_docs"
        ];
        sandbox.allowedDomains = [
          "api.opentofu.org"
          "get.opentofu.org"
          "registry.opentofu.org"
        ];
        sandbox.allowWrite = [
          "~/.terraform.versions"
          # tofu init populates ~/.terraform.d/plugins (filesystem
          # mirror) and ~/.terraform.d/plugin-cache (shared download
          # cache when TF_PLUGIN_CACHE_DIR is set). Both need
          # read+write so providers can be downloaded and loaded.
          # Sibling ~/.terraform.d/credentials.tfrc.json stays
          # unreachable -- it is not under either subdirectory and
          # is additionally denied at the permission layer.
          "~/.terraform.d/plugins"
          "~/.terraform.d/plugin-cache"
        ];
        sandbox.allowRead = [ "~/.tfswitch.toml" ];
        # hashicorp/go-plugin (used by tofu providers and tflint plugins)
        # binds a Unix domain socket for IPC with the parent process. The
        # claude wrapper sets PLUGIN_UNIX_SOCKET_DIR=~/.terraform.versions
        # so tofu's socket lands in a directory the sandbox can both write
        # to and bind within. tflint ignores PLUGIN_UNIX_SOCKET_DIR and
        # always uses os.TempDir(), so a separate tflint wrapper points
        # TMPDIR at ~/.tflint.d/tmp; ~/.tflint.d is added here so sockets
        # inside it can be bound.
        sandbox.allowUnixSockets = [
          "~/.terraform.versions"
          "~/.tflint.d"
        ];
        fetchRules.deny = [
          {
            host = "registry\\.opentofu\\.org";
            path = "/(providers|modules)(/.*)?";
            reason = "Use mcp__opentofu__* tools instead of fetching OpenTofu Registry pages.";
          }
          {
            host = "registry\\.terraform\\.io";
            path = "/(providers|modules)(/.*)?";
            reason = "Use mcp__opentofu__* tools instead of fetching Terraform Registry pages.";
          }
        ];
      };

      destiny = {
        # A personal tool, so each host that wants it sets
        # `toolBundles.destiny.enable = true`.
        enable = lib.mkDefault false;
        servers.destiny = {
          type = "stdio";
          command = "${pkgs.mcp-destiny}/bin/mcp-destiny";
          args = [
            "serve"
            "--state-dir"
            "${config.xdg.stateHome}/mcp-destiny"
            "--cache-dir"
            "${config.xdg.cacheHome}/mcp-destiny"
            "--log-file"
            "${config.xdg.stateHome}/mcp-destiny/destiny.log"
          ];
        };
        # The sign-in tools write only the local token file, so they sit
        # with the reads.
        permissions.allow = [
          "mcp__destiny__status"
          "mcp__destiny__login"
          "mcp__destiny__complete_login"
          "mcp__destiny__list_characters"
          "mcp__destiny__list_items"
          "mcp__destiny__get_item"
          "mcp__destiny__compare_duplicates"
          "mcp__destiny__list_loadouts"
          "mcp__destiny__list_tags"
          "mcp__destiny__list_notes"
          "mcp__destiny__list_perk_preferences"
          "mcp__destiny__list_autotag_rules"
          "mcp__destiny__preview_autotag_rule"
          "mcp__destiny__list_activity_history"
          "mcp__destiny__list_activities"
          "mcp__destiny__list_vendors"
          "mcp__destiny__get_stats"
        ];
        # Each of these tools changes the live game inventory or the data
        # that the web client syncs.
        permissions.ask = [
          "mcp__destiny__move_items"
          "mcp__destiny__move_stack"
          "mcp__destiny__stage_junk"
          "mcp__destiny__set_lock"
          "mcp__destiny__set_perk"
          "mcp__destiny__equip_loadout"
          "mcp__destiny__save_loadout"
          "mcp__destiny__clear_loadout"
          "mcp__destiny__set_tag"
          "mcp__destiny__set_favorite"
          "mcp__destiny__set_note"
          "mcp__destiny__set_perk_preferences"
          "mcp__destiny__save_autotag_rule"
          "mcp__destiny__remove_autotag_rule"
          "mcp__destiny__set_autotag_enabled"
          "mcp__destiny__run_autotag_rules"
        ];
        instructions.items = [
          "Use `mcp__destiny__*` tools for Destiny 2 account data in Agg's Destiny Vault Manager, such as the inventory, tags, notes, and activity history."
        ];
      };

      spacelift = {
        servers.spacelift = {
          type = "stdio";
          command = "${spaceliftWrapper}";
          args = [
            "mcp"
            "server"
          ];
        };
        permissions.allow = [
          "mcp__spacelift__get_authentication_guide"
          "mcp__spacelift__get_blueprint"
          "mcp__spacelift__get_context"
          "mcp__spacelift__get_graphql_type_details"
          "mcp__spacelift__get_module"
          "mcp__spacelift__get_module_guide"
          "mcp__spacelift__get_module_run_logs"
          "mcp__spacelift__get_module_version"
          "mcp__spacelift__get_policy"
          "mcp__spacelift__get_policy_sample"
          "mcp__spacelift__get_space"
          "mcp__spacelift__get_stack_run"
          "mcp__spacelift__get_stack_run_changes"
          "mcp__spacelift__get_stack_run_logs"
          "mcp__spacelift__get_worker_pool"
          "mcp__spacelift__introspect_graphql_schema"
          "mcp__spacelift__list_blueprints"
          "mcp__spacelift__list_contexts"
          "mcp__spacelift__list_module_versions"
          "mcp__spacelift__list_modules"
          "mcp__spacelift__list_policies"
          "mcp__spacelift__list_policy_samples"
          "mcp__spacelift__list_policy_samples_indexed"
          "mcp__spacelift__list_resources"
          "mcp__spacelift__list_spaces"
          "mcp__spacelift__list_stack_proposed_runs"
          "mcp__spacelift__list_stack_runs"
          "mcp__spacelift__list_stacks"
          "mcp__spacelift__list_worker_pools"
          "mcp__spacelift__local_preview"
          "mcp__spacelift__local_preview_module"
          "mcp__spacelift__search_contexts"
          "mcp__spacelift__search_graphql_schema_fields"
          "mcp__spacelift__search_modules"
        ];
        permissions.ask = [
          "mcp__spacelift__trigger_stack_run"
          "mcp__spacelift__discard_stack_run"
          "mcp__spacelift__confirm_stack_run"
          "mcp__spacelift__run_stack_task"
        ];
        permissions.deny = [
          "mcp__spacelift__list_api_keys"
          "mcp__spacelift__get_api_key"
        ];
        commandRules.deny = [
          {
            command = "spacectl";
            args = [
              "stack"
              "list"
            ];
            reason = "Use `mcp__spacelift__list_stacks` instead of `spacectl stack list`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "show"
            ];
            reason = "Use `mcp__spacelift__list_stacks` instead of `spacectl stack show`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "run"
              "list"
            ];
            reason = "Use `mcp__spacelift__list_stack_runs` instead of `spacectl stack run list`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "logs"
            ];
            reason = "Use `mcp__spacelift__get_stack_run_logs` instead of `spacectl stack logs`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "changes"
            ];
            reason = "Use `mcp__spacelift__get_stack_run_changes` instead of `spacectl stack changes`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "resources"
              "list"
            ];
            reason = "Use `mcp__spacelift__list_resources` instead of `spacectl stack resources list`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "confirm"
            ];
            reason = "Use `mcp__spacelift__confirm_stack_run` instead of `spacectl stack confirm`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "discard"
            ];
            reason = "Use `mcp__spacelift__discard_stack_run` instead of `spacectl stack discard`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "local-preview"
            ];
            reason = "Use `mcp__spacelift__local_preview` instead of `spacectl stack local-preview`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "preview"
            ];
            reason = "Use `mcp__spacelift__trigger_stack_run` instead of `spacectl stack preview`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "deploy"
            ];
            reason = "Use `mcp__spacelift__trigger_stack_run` instead of `spacectl stack deploy`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "retry"
            ];
            reason = "Use `mcp__spacelift__trigger_stack_run` instead of `spacectl stack retry`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "replan"
            ];
            reason = "Use `mcp__spacelift__trigger_stack_run` instead of `spacectl stack replan`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "task"
            ];
            except = [ "list" ];
            reason = "Use `mcp__spacelift__run_stack_task` instead of `spacectl stack task`.";
          }
          {
            command = "spacectl";
            args = [
              "module"
              "list"
            ];
            reason = "Use `mcp__spacelift__list_modules` instead of `spacectl module list`.";
          }
          {
            command = "spacectl";
            args = [
              "module"
              "list-versions"
            ];
            reason = "Use `mcp__spacelift__list_module_versions` instead of `spacectl module list-versions`.";
          }
          {
            command = "spacectl";
            args = [
              "module"
              "local-preview"
            ];
            reason = "Use `mcp__spacelift__local_preview_module` instead of `spacectl module local-preview`.";
          }
          {
            command = "spacectl";
            args = [
              "module"
              "logs"
            ];
            reason = "Use `mcp__spacelift__get_module_run_logs` instead of `spacectl module logs`.";
          }
          {
            command = "spacectl";
            args = [
              "blueprint"
              "list"
            ];
            reason = "Use `mcp__spacelift__list_blueprints` instead of `spacectl blueprint list`.";
          }
          {
            command = "spacectl";
            args = [
              "blueprint"
              "show"
            ];
            reason = "Use `mcp__spacelift__get_blueprint` instead of `spacectl blueprint show`.";
          }
          {
            command = "spacectl";
            args = [
              "policy"
              "list"
            ];
            reason = "Use `mcp__spacelift__list_policies` instead of `spacectl policy list`.";
          }
          {
            command = "spacectl";
            args = [
              "policy"
              "show"
            ];
            reason = "Use `mcp__spacelift__get_policy` instead of `spacectl policy show`.";
          }
          {
            command = "spacectl";
            args = [
              "policy"
              "samples"
            ];
            reason = "Use `mcp__spacelift__list_policy_samples` instead of `spacectl policy samples`.";
          }
          {
            command = "spacectl";
            args = [
              "policy"
              "sample"
            ];
            reason = "Use `mcp__spacelift__get_policy_sample` instead of `spacectl policy sample`.";
          }
          {
            command = "spacectl";
            args = [
              "policy"
              "samples-indexed"
            ];
            reason = "Use `mcp__spacelift__list_policy_samples_indexed` instead of `spacectl policy samples-indexed`.";
          }
          {
            command = "spacectl";
            args = [
              "workerpool"
              "list"
            ];
            reason = "Use `mcp__spacelift__list_worker_pools` instead of `spacectl workerpool list`.";
          }
          {
            # Catch-all deny
            command = "spacectl";
            reason = "Do not invoke `spacectl` directly. Use the `mcp__spacelift__*` tools instead.";
          }
        ];
        instructions = {
          items = [
            "Use `mcp__spacelift__*` tools for Spacelift operations. Do not invoke `spacectl` directly."
          ];
        };
      };

      kubectx = {
        servers.kubectx = {
          type = "stdio";
          command = "${pkgs.mcp-kubectx}/bin/mcp-kubectx";
          args = [
            "serve"
            "--sa-role-name"
            cfg.kubeClusterRole
            "--sa-cluster-scoped"
            # The log lies in mcp-kubectx-run, outside the state dir
            # that the extra_mounts of the Lima sandbox mount into
            # each guest. So no guest can replace it with a symlink.
            "--log-file"
            "${config.xdg.stateHome}/mcp-kubectx-run/kubectx.log"
            "--socket-slots"
            (toString cfg.kubectxSocketSlots)
          ]
          ++ lib.concatMap (host: [
            "--allow-apiserver-host"
            host
          ]) cfg.kubeApiDomains;
        };
        permissions.allow = [
          "mcp__kubectx__list"
          "mcp__kubectx__select"
        ];
        commandRules.deny = [
          {
            command = "kubectx";
            reason = "Do not use kubectx or kubens directly. Use mcp__kubectx__list to list contexts and mcp__kubectx__select to switch contexts.";
          }
          {
            command = "kubens";
            reason = "Do not use kubectx or kubens directly. Use mcp__kubectx__list to list contexts and mcp__kubectx__select to switch contexts.";
          }
        ];
        sandbox.allowedDomains = cfg.kubeApiDomains;
        # Per-`serve` UDS: kubectl's exec credential plugin
        # (`mcp-kubectx exec-plugin --socket <path>`) connects here
        # instead of forking out to `host token` itself, so the
        # plugin can run inside Claude's bash sandbox without
        # tripping the `~/.kube/config` read deny.
        #
        # Claude Code's allowUnixSockets matcher is literal-only:
        # entries must be exact paths, not globs. We enumerate one
        # entry per slot (host + guest variant of each) so the
        # rendered allowlist is 1:1 with the slot range
        # `mcp-kubectx serve` walks at startup. Both env tags are
        # listed because the same bundle entry flows into both the
        # host (Darwin) and guest (Lima) profiles.
        sandbox.allowUnixSockets = lib.concatMap (slot: [
          "${config.xdg.stateHome}/mcp-kubectx-run/serve.${toString slot}.host.sock"
          "${config.xdg.stateHome}/mcp-kubectx-run/serve.${toString slot}.guest.sock"
        ]) (lib.genList lib.id cfg.kubectxSocketSlots);
        instructions = {
          items = [
            "Use `mcp__kubectx__list` to see available Kubernetes contexts."
            "Use `mcp__kubectx__select` to activate a context before running kubectl commands."
          ];
        };
      };

      aven = {
        permissions.allow = [
          "Bash(aven)"
          "Bash(aven *)"
        ];
        # The task database, its attachments, and the log all live here.
        sandbox.allowWrite = [ "~/.local/state/aven" ];
        # After each change, aven wakes its daemon through this socket
        # beside the database. The daemon then syncs the change without
        # waiting for its sync interval.
        sandbox.allowUnixSockets = [ "${config.xdg.stateHome}/aven/db.sqlite.host.sock" ];
        instructions.items = [
          "Refs like `APP-7KQ9` name aven tasks. Load the `aven` skill before running `aven` commands."
        ];
      };

      self-improve = {
        # The track-issue skill replaces Claude Code's built-in feedback
        # drafts. It files each notable issue with Claude Code or its
        # config as an aven task in the dotfiles project. That project holds
        # the config that caused the issue. A blanket deny removes the tool
        # from the model's context.
        permissions.deny = [ "SendFeedback" ];
        instructions.items = [
          "When you hit a notable issue with Claude Code or its config, start `Agent({subagent_type: \"fork\", prompt: \"Invoke the track-issue skill for this Claude Code issue: <one-line summary>\"})` and continue the task. Invoke the skill directly when forks are unavailable. Notable issues include a failed or wrongly denied tool call, misleading guidance, a missing capability, and a user complaint about how you worked."
        ];
      };

      claude-ai-integrations = {
        # Claude.ai web-app integration auth tools with no configured server here.
        # Denied defensively in case such a server gets wired up by accident.
        permissions.deny = [
          "mcp__claude_ai_Asana__authenticate"
          "mcp__claude_ai_Asana__complete_authentication"
          "mcp__claude_ai_Gmail__authenticate"
          "mcp__claude_ai_Gmail__complete_authentication"
          "mcp__claude_ai_Google_Calendar__authenticate"
          "mcp__claude_ai_Google_Calendar__complete_authentication"
          "mcp__claude_ai_Google_Drive__authenticate"
          "mcp__claude_ai_Google_Drive__complete_authentication"
        ];
      };
    };

    programs = {
      mcp = {
        enable = true;
        servers = injectCaEnv (lib.recursiveUpdate bundledServers cfg.extraMcpServers);
      };

      claude-code = {
        enable = true;
        package = claudeWrapped;
        # Pin the legacy location. This module writes .claude/CLAUDE.md,
        # themes, and sandbox rules against ~/.claude directly, and
        # preferXdgDirectories moves the default to ~/.config/claude at
        # stateVersion 26.11.
        configDir = "${config.home.homeDirectory}/.claude";

        settings = lib.recursiveUpdate (
          {
            env = {
              CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS = "1";
              # Keep /code-review in the session it was typed in. As of
              # Claude Code 2.1.229 it is the only command running in a
              # "fork" context, which spawns its orchestrator into a
              # backgrounded sub-conversation whose results often never
              # land. This flag routes it inline and has it report
              # through the ReportFindings tool instead. The read site
              # is a bare Boolean(), so reverting means deleting this
              # line -- "0" still reads as truthy.
              CLAUDE_CODE_REPORT_FINDINGS = "1";
            };
            # Prompt cache TTL. The main conversation holds a 1-hour
            # cache so long thinking pauses and slow tool runs still hit
            # it; everything spawned around it (subagents, workflows,
            # background helpers) keeps the cheaper 5-minute writes,
            # since those turns are short-lived anyway. Unset would mean
            # "automatic", which already picks 1h for a subscription --
            # setting both pins the split explicitly.
            promptCacheTtl = "1h";
            subagentPromptCacheTtl = "5m";
            disableAutoMode = "disable";
            includeGitInstructions = false;
            respondToBashCommands = true;
            awaySummaryEnabled = false;
            inherit (cfg) attribution;
            permissions = {
              defaultMode = if skipPerms then "bypassPermissions" else "acceptEdits";
              allow = readPermEntries ++ writePermEntries ++ enabledRules cfg.permissions.allow;
              deny = enabledRules cfg.permissions.deny;
              # git entries live in the git tool bundle, next to the
              # allow table they would otherwise shadow (permission
              # rules evaluate deny -> ask -> allow, regardless of
              # specificity).
              ask = enabledRules cfg.permissions.ask;
            };
            worktree.bgIsolation = "none";
            # "off" hides a bundled skill from both the model and the
            # slash menu. update-config edits settings.json by hand,
            # and this module regenerates that file on every
            # activation.
            skillOverrides = {
              update-config = "off";
            };
            statusLine = {
              type = "command";
              command = "${pkgs.claude-powerline}/bin/claude-powerline";
              padding = 0;
            };
            sandbox = {
              enabled = sandboxEnabled;
              failIfUnavailable = true;
              allowUnsandboxedCommands = false;
              # Allow access to the system TLS trust service.
              enableWeakerNetworkIsolation = true;
              network = {
                allowLocalBinding = true;
                inherit (sandboxNetwork) allowUnixSockets allowedDomains deniedDomains;
              };
              filesystem = {
                inherit (sandboxFilesystem) denyRead allowWrite denyWrite;
                allowRead = lib.unique (sandboxFilesystem.allowRead ++ sandboxFilesystem.allowWrite);
              };
              credentials = {
                # deny unsets the variable inside the sandbox. Only
                # allow and deny are used here: mask mode needs
                # network.tlsTerminate, which breaks on macOS.
                envVars = map (name: {
                  inherit name;
                  mode = "deny";
                }) deniedCredentialVars;
                # The entries above cover the variables; these cover the
                # files behind them, which sit inside the ~/.config
                # grant in filesystem.allowRead. A filesystem.denyRead
                # entry there would be inert, because allowRead takes
                # precedence over denyRead for matching paths. Only an
                # exact allowRead entry displaces a credentials.files
                # deny, so these hold against the ancestor grant.
                # sops-nix points ~/.config/sops-nix/secrets at
                # secrets.d under the darwin per-user temp dir
                # (defaultSecretsMountPoint is "%r/secrets.d", and %r
                # resolves through `getconf DARWIN_USER_TEMP_DIR`),
                # which the /var/folders entries in allowRead re-open in
                # turn, so both spellings appear here. Without them a
                # sandboxed `cat` still reads every token, and a
                # sandboxed workmux call re-exports the lot into its own
                # environment.
                files = [
                  {
                    path = "${config.xdg.configHome}/sops-nix";
                    mode = "deny";
                  }
                  {
                    path = "/var/folders/*/*/T/secrets.d";
                    mode = "deny";
                  }
                  {
                    path = "/private/var/folders/*/*/T/secrets.d";
                    mode = "deny";
                  }
                ];
              };
            };
            hooks =
              let
                # hook-router enforces its own 45s context timeout
                # (tools/hook-router/main.go); 60s is a backstop for a
                # hung process, not a working budget.
                routerTimeout = 60;
                # The Write and Edit entries can first wait
                # escapeRestore.maxWaitSeconds for the transcript entry
                # of the call. hook-router adds that wait to its own
                # timeout, so the backstop grows by the same amount.
                fileWriteTimeout =
                  routerTimeout + (if cfg.escapeRestore.enable then cfg.escapeRestore.maxWaitSeconds else 0);
                # Exec form: with `args` present Claude Code resolves
                # `command` as an executable and spawns it directly, so no
                # shell parses the store path or the flags.
                router = event: tool: {
                  type = "command";
                  command = lib.getExe hookRouter;
                  args = [
                    "--event"
                    event
                  ]
                  ++ lib.optionals (tool != null) [
                    "--tool"
                    tool
                  ];
                  timeout = routerTimeout;
                };
                # Limits a router hook to the main conversation's
                # payloads or to a subagent's (see agentScope in
                # tools/hook-router/main.go).
                scoped =
                  scope: hook:
                  hook
                  // {
                    args = hook.args ++ [
                      "--agent-scope"
                      scope
                    ];
                  };
                # workmux hooks emit nothing Claude Code reads, so they
                # run in the background and never block a turn.
                status = s: {
                  type = "command";
                  command = "${lib.getExe workmuxStatus} ${s}";
                  async = true;
                };
              in
              {
                # NOTE: All matching hooks run concurrently with the original input.
                # Only one hook per tool should return updatedInput to avoid
                # non-deterministic last-writer-wins races.
                PreToolUse = [
                  {
                    # Claude Code 2.1.314 and later also fire a "Bash"
                    # matcher on Monitor commands. They rename the tool to
                    # Bash and drop every input field except command and
                    # description. Naming Monitor here matches it as its
                    # own tool, so hook-router gets tool_name "Monitor".
                    # It then tells a Monitor script from a foreground
                    # Bash call.
                    matcher = "Bash|Monitor";
                    hooks = [ (router "PreToolUse" "Bash") ];
                  }
                  {
                    matcher = "ExitPlanMode";
                    hooks = [ ((router "PreToolUse" "ExitPlanMode") // { statusMessage = "plan gate"; }) ];
                  }
                  {
                    matcher = "EnterPlanMode";
                    hooks = [ (router "PreToolUse" "EnterPlanMode") ];
                  }
                  {
                    # All MCP tools route to one hook entry; "MCP" is a
                    # routing sentinel and hook-router reads the real
                    # tool name from the payload. Only this matcher
                    # matches mcp__* names, so there is exactly one
                    # decision emitter per MCP call.
                    matcher = "mcp__.*";
                    hooks = [ (router "PreToolUse" "MCP") ];
                  }
                  {
                    # "FileWrite" is a routing sentinel like "MCP". On a
                    # host with --write-guard, hook-router asks before a
                    # file tool in plan or bypassPermissions mode writes
                    # a path that the sandbox denies to commands
                    # (tools/hook-router/filewriteguard.go). Elsewhere it
                    # exits without output.
                    matcher = "^(Write|Edit|MultiEdit|NotebookEdit)$";
                    hooks = [ (router "PreToolUse" "FileWrite") ];
                  }
                ];
                # Claude Code fires this event when it raises a
                # permission prompt for a tool call. hook-router answers
                # the prompt of a Workflow agent's call with a deny
                # (tools/hook-router/promptguard.go). No one answers
                # that prompt, so the agent waits on it until someone
                # stops the run. The entry covers the prompts that
                # Claude Code raises itself, such as the prompt of a
                # `permissions.ask` rule. Claude Code skips the event
                # for a prompt that only a person can answer, so such a
                # prompt still holds the agent. The PreToolUse entries
                # already deny a Workflow agent's call where a
                # hook-router rule asks.
                #
                # Matcher-less on purpose, so the entry covers every
                # tool. hook-router emits nothing for any other caller,
                # and a person answers that prompt.
                PermissionRequest = lib.optional cfg.promptGuard {
                  hooks = [ (router "PermissionRequest" null) ];
                };
                UserPromptSubmit = [
                  { hooks = [ (status "working") ]; }
                  {
                    # Claude Code caps UserPromptSubmit command hooks at
                    # 30s by default; keep that rather than the 60s
                    # backstop.
                    hooks = [ ((router "UserPromptSubmit" null) // { timeout = 30; }) ];
                  }
                ];
                Notification = [
                  {
                    matcher = "permission_prompt|elicitation_dialog|elicitation_url_dialog";
                    hooks = [ (status "waiting") ];
                  }
                ];
                PostToolUse = [
                  # Matcher-less on purpose, because any tool result must flip
                  # the window back from `waiting` to `working`. --dedupe skips
                  # the repeats within a turn (see workmuxStatus).
                  { hooks = [ (status "working --dedupe") ]; }
                  {
                    # Mirrors hook-router's PostToolUse switch
                    # (tools/hook-router/main.go). Every other tool is a
                    # no-op there, so the matcher saves a process spawn
                    # per call. Bash output compaction returns
                    # updatedToolOutput, which Claude reads, so this entry
                    # stays synchronous; the same invocation formats the
                    # files a Bash command changed before compacting.
                    # Extend this list when a handler
                    # is added, unless the handler returns nothing Claude
                    # reads, which belongs on the entry below.
                    matcher = "^(AskUserQuestion|Bash)$";
                    hooks = [ (router "PostToolUse" null) ];
                  }
                  {
                    # Write, Edit, and MultiEdit run the formatter and
                    # then the linter. Linter findings arrive through
                    # exit code 2 (handlePostFileWrite returns a
                    # blockError, tools/hook-router/filewrite.go). A
                    # clean file exits 0 and Claude never hears from the
                    # hook. hook-router logs formatter and linter crashes
                    # and swallows them, so they stay in its own log.
                    #
                    # With escapeRestore on, the same invocation first
                    # puts back the `\uXXXX` escapes that Claude Code
                    # decoded in a Write or an Edit
                    # (tools/hook-router/escapes.go). The formatter and
                    # the linter then read the file as the model wrote
                    # it. A notice about an escape that hook-router
                    # could not put back also arrives through exit
                    # code 2. The restore waits for the transcript entry
                    # of the call, so fileWriteTimeout covers that wait.
                    # In the main conversation the restore runs in the
                    # background with the lint. A later call of the same
                    # message can then read the file before the escapes
                    # are back.
                    #
                    # Claude Code delivers an asyncRewake hook's exit-2
                    # stderr to the main conversation, even for a
                    # subagent's edit, so --agent-scope splits the work
                    # between two entries. The main conversation's edits
                    # run off the critical path with asyncRewake, which
                    # wakes Claude only on exit code 2. A subagent's
                    # edits run synchronously, so Claude Code shows the
                    # findings to the subagent that made the edit. Each
                    # entry exits 0 on the other caller's payloads.
                    #
                    # Passes no --tool: the PostToolUse switch reads the
                    # real tool name from the payload.
                    matcher = "^(Write|Edit|MultiEdit)$";
                    hooks = [
                      (
                        (scoped "main" (router "PostToolUse" null))
                        // {
                          asyncRewake = true;
                          timeout = fileWriteTimeout;
                        }
                      )
                      ((scoped "subagent" (router "PostToolUse" null)) // { timeout = fileWriteTimeout; })
                    ];
                  }
                ]
                # Records each pull request that create_pull_request
                # opens in the state database of hook-router
                # (tools/hook-router/pullrequest.go). The
                # `pullRequestRecorded` test in conditionalRules of the
                # github bundle reads the record. "CreatePullRequest"
                # is a routing sentinel, so this matcher is what names
                # the tool. The entry stays synchronous, because the
                # record must exist before the next call edits the
                # pull request. Claude Code fires PostToolUse only for
                # a call that succeeded, so a call that fails leaves no
                # record.
                ++ lib.optional cfg.toolBundles.github.enable {
                  matcher = "^mcp__github__create_pull_request$";
                  hooks = [ (router "PostToolUse" "CreatePullRequest") ];
                };
                # Fires when a tool call throws, not when one exits
                # non-zero: the Bash tool returns normally on a non-zero
                # exit and folds the code into stderr. The realistic
                # trigger here is a sandbox violation, which is exactly
                # when the window should ask for attention. Permission
                # denials and pre-execution rejections do not fire it, so
                # hook-router's own denies never reach it.
                PostToolUseFailure = [
                  { hooks = [ (status "waiting") ]; }
                ];
                Stop = [
                  { hooks = [ (status "done") ]; }
                  { hooks = [ ((router "Stop" null) // { statusMessage = "post-impl gate"; }) ]; }
                ];
                # Turn ended on an API error (rate limit, auth, overloaded,
                # ...). workmux has no error status; `waiting` is the one
                # that asks for attention and auto-clears on focus.
                StopFailure = [
                  { hooks = [ (status "waiting") ]; }
                ];
                # The Stop gate above covers the lead only, and
                # teammateMode = "in-process" lets a teammate go idle
                # around it. hook-router blocks each teammate's first idle
                # attempt per plan cycle through exit code 2, the only
                # channel this event reads a block from; a second attempt
                # allows through, since the event carries no
                # stop_hook_active to escape an unbounded gate with.
                TeammateIdle = [
                  { hooks = [ ((router "TeammateIdle" null) // { statusMessage = "post-impl gate"; }) ]; }
                ];
                # Matcher-less on purpose: every spawn is recorded, so no
                # agent type needs naming here. hook-router reads
                # agent_type and agent_id from the payload.
                SubagentStart = [
                  { hooks = [ (router "SubagentStart" null) ]; }
                ];
                # Matcher-less: both triggers are recorded. hook-router
                # reads the trigger from the payload and emits nothing,
                # which this event would otherwise parse as a decision
                # document and could act on by cancelling the compaction.
                PreCompact = [
                  { hooks = [ (router "PreCompact" null) ]; }
                ];
                SessionStart = [
                  {
                    # register-agent binds the session id to the tmux pane so
                    # the sidebar lists the agent at startup and the async
                    # status hooks keep routing when the hook process falls
                    # outside the pane's process tree (workmux refuses to
                    # route status by working directory alone).
                    matcher = "startup|resume|clear|fork";
                    hooks = [
                      {
                        type = "command";
                        command = "${lib.getExe' pkgs.workmux-bin "workmux"} register-agent";
                        async = true;
                      }
                    ];
                  }
                  { hooks = [ (router "SessionStart" null) ]; }
                  {
                    matcher = "startup|clear";
                    hooks = [
                      {
                        type = "command";
                        command = lib.getExe avenPrime;
                        timeout = 10;
                      }
                    ];
                  }
                ]
                ++ lib.optionals (config.dotfiles.hostname == "terrarium") [
                  {
                    # Every recreated VM starts logged out, and workmux can
                    # start its first agent with --continue or --resume, so
                    # the matcher includes resume.
                    matcher = "startup|resume";
                    hooks = [
                      {
                        type = "command";
                        command = lib.getExe atuinSandboxLogin;
                        async = true;
                      }
                    ];
                  }
                ];
                SessionEnd = [
                  {
                    # SessionEnd hooks share a 1.5s budget by default; a
                    # per-hook timeout raises it (up to 60s). 10s gives the
                    # kubectx session-dir cleanup room without delaying
                    # exit noticeably.
                    hooks = [ ((router "SessionEnd" null) // { timeout = 10; }) ];
                  }
                ];
                # Unanchored on purpose: a matcher of bare names and pipes
                # takes Claude Code's literal path, splitting on `|` and
                # testing exact membership, and `^(...)$` would force it
                # onto the regex path instead.
                #
                # Synchronous rather than async: the script remaps its
                # failures off exit code 2, the only code that blocks a
                # config change, so it cannot block. A synchronous failure
                # still prints stderr where an async one would vanish into
                # a background log.
                ConfigChange = [
                  {
                    matcher = "user_settings|local_settings|skills";
                    hooks = [
                      {
                        type = "command";
                        command = lib.getExe configChangeLog;
                      }
                    ];
                  }
                ];
              };
            autoMemoryEnabled = false;
            alwaysThinkingEnabled = true;
            skipDangerousModePermissionPrompt = true;
            teammateMode = "in-process";
            showThinkingSummaries = true;
            showClearContextOnPlanAccept = true;
            fileCheckpointingEnabled = true;
            askUserQuestionTimeout = "never";
            # Remote-forwarded dialogs and held cross-session messages
            # wait indefinitely instead of resolving to cancelled or
            # denied after the 5m default, matching
            # askUserQuestionTimeout above. Local-only prompts carry no
            # deadline either way, and CLAUDE_CODE_USER_DIALOG_TIMEOUT_MS
            # still overrides this per session.
            dialogExpiry = "never";
            showTurnDuration = true;
            terminalProgressBarEnabled = true;
            # Lets Claude send proactive push notifications to the mobile
            # app. Toggling it in /config replaces the settings.json link
            # with a plain file. The next activation moves that file to
            # a timestamped backup (home.backupCommand in home/default.nix).
            agentPushNotifEnabled = true;
            autoCompactEnabled = true;
            autoCompactWindow = 666666;
            # Inline Bash output up to the 128K character ceiling before
            # Claude Code saves it to a file. hook-router's PostToolUse
            # compaction collapses repeated lines in what arrives inline,
            # so the model reads a nix or Dagger build's output directly
            # instead of chasing the file pointer. Background task output
            # always lands in a file that the model reads with Read.
            bashOutputMaxChars = 128000;
            # Record which files a Bash command changed in its Git
            # repository, in every permission mode. The Bash result
            # carries a diff of up to five of them, and PostToolUse:Bash
            # hooks receive the whole list as
            # tool_response.bashEditDiff.changedFiles, which hook-router
            # routes through formatterRules so a sed -i or tee edit is
            # formatted like a Write or Edit.
            bashEditDiffEnabled = true;
            # Chat transcript retention in days. Claude Code rejects 0
            # and has no keep-forever value, so 36500 (about 100 years)
            # keeps transcripts indefinitely. The same period governs
            # how long ~/.claude/skills/.trash holds what
            # syncClaudeAiSkills below moves there. Claude Code 2.1.314
            # and later also cap that folder at about 1024 entries.
            cleanupPeriodDays = 36500;
            # Keep ~/.claude/skills and ~/.claude/plugins Nix-owned.
            # Skills and plugins enabled on the claude.ai account
            # otherwise download into skills/synced and plugins/synced
            # at session start and re-sync every 10 minutes. False stops
            # the downloads and moves already-synced skills to
            # skills/.trash at the next launch. Only false is honored.
            syncClaudeAiSkills = false;
            syncClaudeAiPlugins = false;
            # Straight-ASCII prompt input: no :shortcode: emoji expansion,
            # matching the plain-ASCII policy.
            emojiCompletionEnabled = false;
          }
          // lib.optionalAttrs cfg.stylixTheme.enable {
            theme = "custom:stylix";
          }
          // lib.optionalAttrs (cfg.outputStyle != null) {
            inherit (cfg) outputStyle;
          }
        ) cfg.extraSettings;

        agents = lib.mapAttrs (_: a: a.source) (lib.filterAttrs (_: a: a.enable) cfg.agents);

        outputStyles = lib.mapAttrs (_: o: o.source) (lib.filterAttrs (_: o: o.enable) cfg.outputStyles);

        skills = lib.mapAttrs (_: s: s.source) (lib.filterAttrs (_: s: s.enable) cfg.skills);
      };

    };

    xdg.configFile = {
      "claude-powerline/config.json".text = claudePowerlineConfig;
      "workmux/config.yaml".source = workmuxConfig;
    };

    # The daily cleanup of /tmp does not reach the clone directory of a
    # Darwin host, so this agent bounds the directory. The agent removes
    # each clone that nothing changed in the last 10 days, and it
    # removes the clone as a whole. A fetch changes its clone, and a
    # read does not.
    # pkgs/expire-clones.nix describes what counts as a clone.
    # expire-clones.service of hosts/nixos/clones.nix removes the clones
    # of a NixOS host at the same age.
    #
    # The agent runs as the user. So it keeps a clone that holds a
    # directory that the user cannot read or a file that the user
    # cannot delete. The run then fails, and expireClonesLog names the
    # clone. The service of the NixOS hosts runs as root and removes
    # such a clone.
    #
    # The agent runs every day at 04:00. When the Mac sleeps at that
    # time, launchd runs the agent after the Mac wakes. launchd skips
    # the run when the Mac is off at that time. So the agent also runs
    # each time that launchd loads it, which launchd does at each login.
    #
    # The tool passes its last argument to `date -d`, so the argument
    # must be a time that date reads.
    launchd.agents.expire-clones = lib.mkIf expireClonesEnabled {
      enable = true;
      config = {
        ProgramArguments = [
          (lib.getExe pkgs.expire-clones)
          cloneRoot
          "10 days ago"
        ];
        StartCalendarInterval = [
          {
            Hour = 4;
            Minute = 0;
          }
        ];
        RunAtLoad = true;
        LowPriorityIO = true;
        StandardOutPath = expireClonesLog;
        StandardErrorPath = expireClonesLog;
      };
    };

    # The daily cleanup of /tmp does not reach claudeTmpDir on a Darwin
    # host, so this agent bounds the temporary directory of Claude Code
    # there. The agent removes each session directory that nothing
    # changed in the last 10 days, and it removes the directory as a
    # whole. It keeps the directory named by the current id of each
    # session that runs. The sessions directory of Claude Code names
    # those sessions. It also keeps each directory that a process of the
    # user holds open or works in, which lsof lists. A session that
    # changed its id, such as after /clear, keeps the output of its
    # background commands under its first id. On macOS, Claude Code
    # 2.1.319 holds that directory open only while it writes there. So
    # the agent removes that directory after 10 days without a change,
    # unless a background command of the directory still runs.
    # pkgs/expire-claude-tmp.nix describes what counts as a session
    # directory, as a change, and as a use. expire-claude-tmp.service of
    # hosts/nixos/claude-tmp.nix removes the session directories of a
    # NixOS host at the same age.
    #
    # With --fallback, the agent also keeps /tmp/claude-<uid>, which
    # sandboxed commands get as TMPDIR. The comment on claudeTmpPaths
    # gives the reason. The cleanup of /tmp runs every day at midnight,
    # and it deletes the directory when the directory stays empty and
    # unmodified for three days. Each run of the agent modifies the
    # directory, so the cleanup keeps it.
    #
    # The agent runs every day at 04:30, and each time that launchd
    # loads it. The comment on the expire-clones agent gives the reason
    # for the second run.
    #
    # The tool passes the time that follows claudeTmpDir to `date -d`,
    # so the value must be a time that date reads.
    launchd.agents.expire-claude-tmp = lib.mkIf pkgs.stdenv.hostPlatform.isDarwin {
      enable = true;
      config = {
        ProgramArguments = [
          (lib.getExe pkgs.expire-claude-tmp)
          "--fallback"
          "/tmp"
          claudeTmpDir
          "10 days ago"
          "${config.programs.claude-code.configDir}/sessions"
        ];
        StartCalendarInterval = [
          {
            Hour = 4;
            Minute = 30;
          }
        ];
        RunAtLoad = true;
        LowPriorityIO = true;
        StandardOutPath = expireClaudeTmpLog;
        StandardErrorPath = expireClaudeTmpLog;
      };
    };

    dotfiles.extraInventoryPackages = [
      pkgs.hook-router
      pkgs.mcp-git
      pkgs.mcp-http-proxy
    ];

    home = {
      packages = [
        pkgs.chief
        pkgs.llm-agents.ccusage
        pkgs.mcp-fetch
        pkgs.mcp-kubectx
        workmuxWrapped
        pkgs.git-surgeon
        pkgs.slugify
        # container-init calls it from PATH in the published shell image
        # and in the Dagger dev container
        # (toolchains/dev/scripts/container-init.sh).
        pkgs.claude-json-sync
      ]
      ++ lib.optionals cfg.clauth.enable [ pkgs.clauth ]
      # `mcp-destiny login` signs in from a terminal.
      ++ lib.optionals cfg.toolBundles.destiny.enable [ pkgs.mcp-destiny ]
      ++ lib.optionals cfg.proseLint.enable [
        pkgs.prose-lint
        pkgs.harper
      ];

      file.".claude/themes/stylix.json" = lib.mkIf cfg.stylixTheme.enable {
        source = claudeStylixTheme;
      };

      file.".claude/CLAUDE.md".text = ''
        ## Agents
        - Use `Agent({..., model: "<model>"})` to downgrade models for tasks where reasoning is not required.
        - Explore agents (`subagent_type: "Explore"`) inherit the session model by default; downgrade to `model: "sonnet"` or `model: "haiku"`.
        - Plan agents (`subagent_type: "Plan"`) MUST never be downgraded; omit `model` so they inherit the session model.
        - For generic agents, use your best judgement based on the task being assigned.
        - Use `Agent({..., effort: "low"})` for mechanical work such as file searches and single-fact lookups. Omit `effort` otherwise, and never set it on Plan agents.
      ''
      # As of Claude Code 2.1.319, the Workflow tool relays a user message
      # to every agent of a Workflow as "the user request that triggered
      # this workflow run". It tells the agent that the request wins
      # over the task text. The tool picks the latest message that the
      # user typed in the turn of the launch. So an agent of a
      # long-running orchestrator can receive a message about other
      # work, and one such agent abandoned its task. Every Workflow
      # agent reads this file as instructions from the user, except
      # agents of a type that omits CLAUDE.md, such as the built-in
      # Explore and Plan agents. So the rule lives here.
      + ''

        ## Workflows
        - When you run as a Workflow agent, your task is the task text that the workflow gives you. The harness might also relay the latest message that the user typed to the session that launched the workflow. It presents that message as the user request behind the run.
        - The user wrote the relayed message to the launching session, and that session handles it. Follow the message where it is about your task. A message about other work does not cancel, replace, or narrow your task, so finish the task and do not act on the message.
      ''
      # The tool instructions in this file name calls that prompt, such
      # as a push of a branch that is not named `<type>/<name>`. With
      # promptGuard on, hook-router denies those calls for a Workflow
      # agent, so this line tells the agent what to do after the deny.
      + lib.optionalString cfg.promptGuard ''
        - When you run as a Workflow agent, no one answers your permission prompts, so a hook answers most of them with a deny. After such a deny, do the work another way, or continue without the call and name the call in your result.
      ''
      + ''

        ## Shell
        - `cd` persists between Bash calls only while it stays inside the project directory; a `cd` outside it resets the cwd to the project root on the next call.
        - Never wait by sleeping; a foreground poll loop breaks this rule even when each sleep is short.
      ''
      + lib.optionalString cfg.sleepGuard.enable ''
        - A hook denies foreground waiting: a filler command that only passes time (`sleep 6`, `sleep 1; echo waiting`) at any duration, a `sleep` over ${toString cfg.sleepGuard.maxSeconds}s even inside real work, and a `sleep` whose duration it cannot read (`sleep $VAR`). Short sleeps belong only in a settle step inside a command doing real work (`kill "$pid"; sleep 1; pgrep -f server`).
        - Do not bide time with no-op filler (`true`, `jobs`, bare `echo`) while a background task runs; end the turn instead -- the completion notification arrives on its own, and idle turns only delay it.
      ''
      + ''
        - Long-running work belongs in a Bash call with `run_in_background: true`. The session stays free, you get a notification when the command exits, and `Read` fetches its captured output. `sleep` inside a background call or a Monitor script is fine, so an `until <check>; do sleep 1; done` poll loop there is the right way to wait on a condition.
        - For one notification per event rather than one on completion, use the `Monitor` tool. It is deferred, so load it with `ToolSearch("select:Monitor")` before calling it.
        - Monitor watches end after 30 minutes at most and send a notification asking to be re-armed. Re-arm the watch when its condition is still pending.
        - Fire off independent work in parallel, then act on completion notifications as they arrive. A spawn-one, wait, spawn-the-next loop serializes work that could have run at once.
      ''
      + lib.optionalString cfg.rmGuard ''
        - A hook denies `rm` and `rmdir` on a target Claude Code cannot check before the command runs, because Claude Code forces a permission prompt on it in every permission mode. Give `rm` literal paths. Guard a leading variable with `:?` (`rm -rf "''${DIR:?}/''${name:?}"`). Do not remove the output of a command substitution (`rm -rf $(...)`); run it first and remove the paths it prints. Do not `cd` before a relative glob (`cd build && rm -rf ./*`). Do not put a trailing `/*` under `~`, `..`, a variable, or another glob (`rm -rf ~/x/*`, `rm -rf build/*/*`).
      ''
      + (
        if cfg.escapeRestore.enable then
          ''
            - Claude Code turns every `\uXXXX` escape in a tool input into its character before the tool runs. After a Write or an Edit, a hook puts back each escape of a character outside ASCII. The escape of an ASCII character, such as `<` in a JSON string, usually stays decoded, so build its backslash at run time (`chr(92) + "u003c"` in Python) or check the file after the call. No other tool gets the escapes back, so a heredoc, an inline script, or a `-m` message in a Bash command carries the character. Put text that needs the escape of a character outside ASCII in a file with Write, then run or pass the file in a later message (`python3 fix.py`, `git commit -F msg.txt`), because in the main conversation the escapes come back only after the message that wrote the file ends.
          ''
        else
          ''
            - Claude Code turns every `\uXXXX` escape in a tool input into its character before the tool runs, in Write and Edit too. To keep an escape, build its backslash at run time (`chr(92) + "u2028"` in Python), or type two backslashes where the reader takes the pair as one (a sed or Perl replacement).
          ''
      )
      + ''

        ## Python
        - Python is always available: `uv` manages a default interpreter on every host, so `python3` and `uv run` work with no setup.
        - Reach for a Python script instead of bash once logic outgrows a simple pipeline: parsing structured data beyond a `jq`/`yq` one-liner, arithmetic or date math, multi-step text transforms, anything wanting data structures or real error handling. A short script in the scratchpad beats a fragile chain of shell escapes.
        - Scripts that need third-party packages declare them inline with PEP 723 metadata (`# /// script` block with `dependencies = [...]`) and run via `uv run script.py`; uv resolves and caches the packages automatically. Never `pip install` into a shared environment.
        - One-liners that need a package: `uv run --with <pkg> python -c '...'`.

        ## Installed CLI Tools
        - `coreutils` are ALWAYS the GNU versions: sed, awk, grep, find, diff, patch, tar, and make on PATH all come from nix's GNU releases, never BSD system tools. Use GNU syntax freely (`sed -i`, `date -d`, `readlink -f`, `sort -V`, `grep -P`), you do not need to write BSD fallbacks.
        - `rust-parallel` runs commands in parallel, one job per `:::` argument (`rust-parallel -j4 cmd ::: a b c`) or per stdin line. `-s` runs each job as a `bash -c` script, so the command can hold a pipeline. Per-job output is buffered, so results never interleave. Prefer it over `xargs -P` and over bare `parallel` (from moreutils).
        - `rust-parallel` expands `{}` in the command for `:::` arguments but not for stdin lines. It appends each stdin line to the end of the command instead, so `ls | rust-parallel -s 'cmd {} | head'` runs `cmd {} | head <line>`. Add `-r '.*'` to make `{}` expand to the line: `ls | rust-parallel -r '.*' -s 'cmd "{}" | head'`.
        - `yq` (Go yq) queries and edits YAML/TOML/XML with `jq`-style syntax (`jq` is also available).
        - `agrind` (angle-grinder) slices and aggregates large log files with a query language; prefer it over long grep/awk pipelines.
        - `sponge` (moreutils) soaks up a pipeline's output before writing, allowing safe in-place file rewrites; `ts` prepends timestamps to lines.
        - Any nixpkgs package can be used without installing it: `nix run nixpkgs#<pkg> -- <args>` for a one-shot program, `nix shell nixpkgs#<pkg> --command <cmd>` when the binary name differs from the attribute or you need several packages at once. The bare `nixpkgs#` ref resolves to this flake's pinned nixpkgs, so cache hits are near-certain and nothing persists in a profile.

        ## Plan Mode
        - Writing untracked files is allowed in plan mode: scratch notes, files under /tmp, and repo clones via `mcp__git__git_clone` are all fine. Only files tracked by git are off-limits until the plan is approved.
        - Every plan ends with a `## Skills` section listing the skills the implementer invokes before starting work, one bullet per skill with the reason it applies (for example `prose` for any documentation or commit messages, `taskfile` for Taskfile edits). Write `None` when no skill applies. The implementer invokes each listed skill via the `Skill` tool before the first edit.

        ## Versioning
        - Projects on a v0 release (latest tag starts with `0.`) or labeled UNSTABLE, ALPHA, or pre-release make no compatibility promise, so make breaking changes freely there. Make the clean change instead of adding shims, deprecation paths, or fallbacks for the old behavior.
        - Projects on v1 or later, or labeled STABLE or PRODUCTION, promise compatibility, so keep the old behavior working there. Code that has not shipped in a release yet carries no promise, so you MAY break it.
      ''
      + lib.optionalString (bundledInstructions != "") "\n${bundledInstructions}\n"
      + lib.optionalString (cfg.hostContext != "") "\n## Host Environment\n\n${cfg.hostContext}\n";

      activation.ensureClaudeResearchDir =
        lib.mkIf (pkgs.stdenv.hostPlatform.isDarwin || !cfg.research.useVault)
          (
            lib.hm.dag.entryAfter [ "writeBoundary" ] ''
              run mkdir -p "${researchDir}"
            ''
          );

      # Lima refuses to start when an extra_mounts host_path is
      # missing, and `host select` only creates this dir lazily on
      # the first kubectx select. Pre-create it at activation so
      # `task lima:rebuild` succeeds on a fresh host.
      activation.ensureMcpKubectxStateDir = lib.mkIf cfg.lima.enable (
        lib.hm.dag.entryAfter [ "writeBoundary" ] ''
          run mkdir -p "${config.xdg.stateHome}/mcp-kubectx"
        ''
      );

      # Same Lima-needs-host-path-to-exist constraint as above.
      activation.ensureArchivesDir = lib.mkIf cfg.lima.enable (
        lib.hm.dag.entryAfter [ "writeBoundary" ] ''
          run mkdir -p ${lib.escapeShellArg cfg.archivesDir}
        ''
      );

      # Lima refuses to start when this extra_mounts host_path is missing,
      # and atuin creates the dir lazily on first run -- pre-create it.
      activation.ensureAtuinDataDir = lib.mkIf cfg.lima.enable (
        lib.hm.dag.entryAfter [ "writeBoundary" ] ''
          run mkdir -p "${atuinDataDir}"
        ''
      );

      # Per-`serve` UDS lives outside the Lima bind-mounted state
      # dir (UDS-over-bind-mount semantics on macOS-host are
      # unverified; the safe design avoids the question). Both host
      # and guest profiles need this dir at mode 0700 to match
      # listenSocket's parent-dir invariant; create it
      # unconditionally so non-Lima Darwin hosts also get it. The
      # dir also holds the files that host-side mcp-kubectx writes or
      # trusts, such as host.id, the kubeconfig of each host-side
      # serve, and kubectx.log. So the extra_mounts of the Lima
      # sandbox must not mount it.
      activation.ensureMcpKubectxRunDir = lib.hm.dag.entryAfter [ "writeBoundary" ] ''
        run install -d -m 700 "${config.xdg.stateHome}/mcp-kubectx-run"
      '';

      # launchd does not create the parent directory of a log path, so
      # the directory must exist before setupLaunchAgents loads the
      # expire-clones agent.
      activation.ensureExpireClonesLogDir = lib.mkIf expireClonesEnabled (
        lib.hm.dag.entryBetween [ "setupLaunchAgents" ] [ "writeBoundary" ] ''
          run mkdir -p ${lib.escapeShellArg (dirOf expireClonesLog)}
        ''
      );

      # The expire-claude-tmp agent needs the directory of its log in
      # the same way.
      activation.ensureExpireClaudeTmpLogDir = lib.mkIf pkgs.stdenv.hostPlatform.isDarwin (
        lib.hm.dag.entryBetween [ "setupLaunchAgents" ] [ "writeBoundary" ] ''
          run mkdir -p ${lib.escapeShellArg (dirOf expireClaudeTmpLog)}
        ''
      );

      # Sync the declared parts of the mutable ~/.claude.json: the MCP
      # servers of ~/.config/mcp/mcp.json and the settings in --merge.
      # claude-json-sync records the servers that it writes. A server
      # that the configuration stops declaring leaves the file on the
      # next activation, and a server from `claude mcp add` stays.
      #
      # The entry runs after linkGeneration, which links mcp.json. It
      # runs ahead of sops-nix and caffeineTccReset, so a failure in
      # either entry cannot skip the sync. An entry that sorts ahead of
      # this one and aborts still skips the sync.
      activation.syncClaudeJson =
        let
          settings =
            # Enable remote control for all interactive sessions.
            lib.optionalAttrs cfg.remoteControl { remoteControlAtStartup = true; }
            # Trust the home directory before the first session (sandbox
            # only). Workspace trust does not require a token, so the
            # setting does not depend on secrets.
            // lib.optionalAttrs skipPerms {
              projects.${config.dotfiles.homeDirectory}.hasTrustDialogAccepted = true;
            };
        in
        lib.hm.dag.entryBetween [ "sops-nix" "caffeineTccReset" ] [ "writeBoundary" "linkGeneration" ] ''
          CLAUDE_JSON="$HOME/.claude.json"

          # claude-json-sync prints the count of the servers that it
          # synced. An activation that prints no such line shows that a
          # sibling entry sorted ahead of this one and aborted.
          run ${lib.getExe pkgs.claude-json-sync} \
            --servers "${config.xdg.configHome}/mcp/mcp.json" \
            --record "${config.xdg.stateHome}/claude-json-sync/mcp-servers.json" \
            --merge ${lib.escapeShellArg (builtins.toJSON settings)} \
            "$CLAUDE_JSON"

          # Prune stale worktree entries from ~/.claude.json
          if [ -z "$DRY_RUN_CMD" ] && command -v workmux >/dev/null 2>&1 && [ -f "$CLAUDE_JSON" ]; then
            ${lib.getExe' pkgs.workmux-bin "workmux"} claude prune 2>/dev/null || true
          fi
        '';

      # Best-effort GitHub PAT upkeep for fish and gh. Kept separate from
      # syncClaudeJson so a sops-nix failure cannot prevent the MCP server
      # sync from running.
      activation.syncClaudeSecrets = lib.mkIf sopsEnabled (
        lib.hm.dag.entryAfter [ "sops-nix" "syncClaudeJson" ] ''
          # Earlier generations stored the PAT as a universal fish
          # variable. Fish writes universal variables in plaintext to
          # fish_variables, and a sandboxed fish reloads them past the
          # credentials scrub. githubWrapper exports the PAT itself, so
          # erase that copy. Drop this once every host has switched.
          if [ -z "$DRY_RUN_CMD" ]; then
            ${pkgs.fish}/bin/fish -c 'set -eU GITHUB_PERSONAL_ACCESS_TOKEN' 2>/dev/null || true
          fi

          ${lib.optionalString skipPerms ''
            # Authenticate gh with the scoped PAT (sandbox only)
            if [ -f "${secretPath "gh_token"}" ]; then
              GH_TOKEN=$(cat "${secretPath "gh_token"}" 2>/dev/null || true)
            fi
            if [ -z "$DRY_RUN_CMD" ] && [ -n "''${GH_TOKEN:-}" ]; then
              echo "''${GH_TOKEN}" | ${pkgs.gh}/bin/gh auth login --with-token
            fi
          ''}
        ''
      );
    };
  };
}
