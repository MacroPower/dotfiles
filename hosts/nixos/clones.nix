# This module removes a clone under cloneRoot as a whole, so an agent
# finds every file of a clone or no clone at all. Agents clone other
# repositories under cloneRoot and read them there. The module keeps the
# daily cleanup of /tmp out of that directory, and its service removes
# each old clone.
#
# hosts/nixos/default.nix imports the module, so every NixOS host gets
# it. The settings apply on each host whose Claude Code configuration
# turns on the git tool bundle. That bundle tells agents to clone into
# cloneRoot.
{
  config,
  lib,
  pkgs,
  utils,
  ...
}:
let
  inherit (config.dotfiles.system) username;
  user = config.home-manager.users.${username};
  # On a Linux host, the cloneRoot of home/claude.nix names this
  # directory in the instructions and the settings of the git MCP
  # server.
  cloneRoot = "/tmp/git";
  # expire-clones.service removes a clone that nothing changed for this
  # long. The span equals the age at which the daily cleanup deletes a
  # file in /tmp. The service appends " ago" to the value and passes
  # the result to `date -d`, so the value must be a relative span that
  # date reads, such as `10 days`. date rejects the tmpfiles form `10d`.
  # A check of this module fails the build of the system when date
  # cannot read the value.
  cloneMaxAge = "10 days";
  tmpfilesKeeps = import ./tmpfiles-check.nix { inherit config lib pkgs; };
  # home/claude.nix declares the dotfiles.claude options, and the git
  # tool bundle is one of them. A host whose homeModule leaves that
  # module out has no git tool bundle.
  hasGitBundle = user.dotfiles ? claude && user.dotfiles.claude.toolBundles.git.enable;
in
{
  config = lib.mkIf hasGitBundle {
    # This assertion fails the evaluation when the Claude Code
    # instructions send the clones to a directory other than cloneRoot.
    # The daily cleanup of /tmp then deletes single files of the
    # clones. An assertion of home/claude.nix fails the evaluation when
    # a NixOS host has the git tool bundle and lacks the tmpfiles rule
    # of this module.
    assertions = [
      {
        assertion = lib.hasInfix "`${cloneRoot}/<owner>/<repo>`" user.home.file.".claude/CLAUDE.md".text;
        message = ''
          hosts/nixos/clones.nix: ~/.claude/CLAUDE.md does not tell
          agents to clone into ${cloneRoot}/<owner>/<repo>. The module
          keeps the daily cleanup of /tmp out of ${cloneRoot} and
          removes each old clone there as a whole. Set cloneRoot in
          hosts/nixos/clones.nix to the directory that home/claude.nix
          names.
        '';
      }
    ];

    systemd.tmpfiles.rules = [
      # systemd-tmpfiles cleans /tmp once a day. It checks each file on
      # its own, and it deletes a file that nothing read or changed in
      # the last 10 days. Without this line, a clone loses each file
      # that no agent read and no fetch wrote in that time, and it
      # keeps the other files. A host that mounts its filesystem with
      # noatime, such as the terrarium VM, records no read. A clone
      # there also loses the files that agents read every day. An agent
      # that reads such a clone finds only a part of the upstream tree
      # and gets no error. The line keeps the cleanup out of the clone
      # directory, and expire-clones.service removes the old clones.
      "x ${cloneRoot}"
    ];

    # The tmpfiles rule for the clone directory keeps the daily cleanup
    # of /tmp out of it, so nothing else bounds the directory. This
    # service removes each clone that nothing changed for the span of
    # cloneMaxAge, and it removes the clone as a whole. A fetch changes
    # its clone, and a read does not. pkgs/expire-clones.nix describes
    # what counts as a clone.
    #
    # The service runs as root, as systemd-tmpfiles does. So it reads a
    # directory whose mode keeps the user out. It also removes the
    # files that the user cannot remove, such as the files that a
    # rootless container wrote with a subordinate uid. A service that
    # runs as the user keeps each clone that holds such a directory or
    # such a file. With --user, the tool refuses a cloneRoot that is a
    # symbolic link or that belongs to another user. The tool never
    # walks or removes the target of a symbolic link. It keeps each
    # clone that holds a mount, so it does not delete the files of the
    # source of a bind mount. When the tool keeps a clone that it
    # cannot remove, the run fails and names the clone.
    #
    # ProtectSystem and ProtectHome also keep the service from writing
    # outside cloneRoot. The hyphen before cloneRoot lets the service
    # start before the first clone creates the directory. The service
    # must see the /tmp of the host, so it does not set PrivateTmp. It
    # must remove the files of every uid, so it does not set
    # PrivateUsers. In a user namespace of its own, root cannot remove
    # the files of a uid that the namespace does not map.
    systemd.services.expire-clones = {
      description = "Remove the clones under ${cloneRoot} that nothing changed in ${cloneMaxAge}";
      serviceConfig = {
        Type = "oneshot";
        ExecStart = utils.escapeSystemdExecArgs [
          (lib.getExe pkgs.expire-clones)
          "--user"
          username
          cloneRoot
          "${cloneMaxAge} ago"
        ];
        IOSchedulingClass = "idle";
        ProtectSystem = "strict";
        ProtectHome = "read-only";
        ReadWritePaths = [ "-${cloneRoot}" ];
      };
    };

    # The timer has the schedule of systemd-tmpfiles-clean.timer. A
    # switch that adds the timer more than 15 minutes after the boot
    # starts the service immediately.
    systemd.timers.expire-clones = {
      wantedBy = [ "timers.target" ];
      timerConfig = {
        OnBootSec = "15min";
        OnUnitActiveSec = "1d";
      };
    };

    # The first check fails the build of the system when the daily
    # cleanup of /tmp deletes a file of a clone.
    # hosts/nixos/tmpfiles-check.nix describes how the check runs the
    # cleanup.
    system.checks = [
      (tmpfilesKeeps {
        name = "tmpfiles-keeps-clones";
        files = [
          "${cloneRoot}/owner/repo/.git/config"
          "${cloneRoot}/owner/repo/README"
        ];
        tree = "a clone";
      })
      # This check fails the build of the system when `date -d` cannot
      # read the time that expire-clones.service passes. The service
      # then fails on every run and removes no clone.
      (pkgs.runCommand "clone-max-age-parses" { } ''
        ${pkgs.coreutils}/bin/date -d ${lib.escapeShellArg "${cloneMaxAge} ago"} >/dev/null || {
          echo "cloneMaxAge in hosts/nixos/clones.nix is not a span that date -d reads, such as '10 days'." >&2
          exit 1
        }
        touch "$out"
      '')
    ];
  };
}
