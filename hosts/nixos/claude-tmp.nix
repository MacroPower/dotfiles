# This module removes a session directory of Claude Code as a whole. So
# a session finds every file of its scratchpad or no file at all.
# Claude Code keeps its temporary files in tmpDir/claude-<uid>. That
# directory holds the files of each session as <project>/<session>,
# which are the scratchpad of the session and the output of its
# background commands. The module keeps the daily cleanup of /tmp out
# of that directory, and its service removes each old session
# directory.
#
# hosts/nixos/default.nix imports the module, so every NixOS host gets
# it. The settings apply on each host whose user has Claude Code.
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
  # On a Linux host, the claude wrapper of home/claude.nix passes this
  # directory to Claude Code as CLAUDE_CODE_TMPDIR. An assertion of
  # home/claude.nix fails the evaluation when the tmpfiles rule of this
  # module names another directory.
  tmpDir = "/tmp";
  # expire-claude-tmp.service removes a session directory that nothing
  # changed for this long. The span equals the age at which the daily
  # cleanup deletes a file in /tmp. The service appends " ago" to the
  # value and passes the result to `date -d`, so the value must be a
  # relative span that date reads, such as `10 days`. date rejects the
  # tmpfiles form `10d`. A check of this module fails the build of the
  # system when date cannot read the value.
  maxAge = "10 days";
  tmpfilesKeeps = import ./tmpfiles-check.nix { inherit config lib pkgs; };
in
{
  config = lib.mkIf user.programs.claude-code.enable {
    systemd.tmpfiles.rules = [
      # systemd-tmpfiles cleans /tmp once a day. It checks each file on
      # its own, and it deletes a file that nothing read or changed in
      # the last 10 days. Without this line, a session that lives for
      # more than 10 days loses each file of its scratchpad that it did
      # not read or write in that time. It keeps the other files.
      #
      # A host that mounts its filesystem with noatime, such as the
      # terrarium VM, records no read. A session there also loses the
      # files that it reads every day. The line keeps the cleanup out
      # of the directory, and expire-claude-tmp.service removes the old
      # session directories.
      #
      # The configuration does not hold the uid of the user, so the
      # line names the directory with a glob. The glob also matches
      # /tmp/claude-<id>-cwd when <id> starts with a digit. Claude Code
      # records the working directory of a command in that file, and it
      # removes the file when the command ends.
      "x ${tmpDir}/claude-[0-9]*"
    ];

    # The tmpfiles rule for the directory of Claude Code keeps the daily
    # cleanup of /tmp out of it, so nothing else bounds the directory.
    # This service removes each session directory that nothing changed
    # for the span of maxAge, and it removes the directory as a whole.
    # It keeps the directory named by the current id of each session
    # that runs. The sessions directory of Claude Code names those
    # sessions. It also keeps each directory that a process holds open
    # or works in. A session that changed its id, such as after /clear,
    # keeps the output of its background commands under its first id.
    # On Linux, Claude Code 2.1.319 holds that directory open while its
    # process runs, so the directory stays. pkgs/expire-claude-tmp.nix
    # describes what counts as a session directory, as a change, and as
    # a use.
    #
    # The service runs as root, as systemd-tmpfiles does. So it removes
    # the files that the user cannot remove, such as the files that a
    # rootless container wrote with a subordinate uid. It also reads a
    # directory whose mode keeps the user out, and it reads the open
    # files of the processes of every user. The tool never follows a
    # symbolic link out of the directory. It keeps each session
    # directory that holds a mount, so it does not delete the files of
    # the source of a bind mount. When the tool keeps a session
    # directory that it cannot remove, the run fails and names the
    # directory by its path.
    #
    # ProtectSystem and ProtectHome also keep the service from writing
    # outside tmpDir. The service must see the /tmp, the network
    # namespace, and the processes of the host. So it sets none of
    # PrivateTmp, PrivateNetwork, PrivatePIDs, PrivateUsers, and
    # ProtectProc. In a user namespace of its own, the service cannot
    # read the open files of the processes of the host.
    systemd.services.expire-claude-tmp = {
      description = "Remove the session directories of Claude Code under ${tmpDir} that nothing changed in ${maxAge}";
      serviceConfig = {
        Type = "oneshot";
        ExecStart = utils.escapeSystemdExecArgs [
          (lib.getExe pkgs.expire-claude-tmp)
          "--user"
          username
          tmpDir
          "${maxAge} ago"
          "${user.programs.claude-code.configDir}/sessions"
        ];
        IOSchedulingClass = "idle";
        ProtectSystem = "strict";
        ProtectHome = "read-only";
        ReadWritePaths = [ tmpDir ];
      };
    };

    # The timer has the schedule of systemd-tmpfiles-clean.timer. A
    # switch that adds the timer more than 15 minutes after the boot
    # starts the service immediately.
    systemd.timers.expire-claude-tmp = {
      wantedBy = [ "timers.target" ];
      timerConfig = {
        OnBootSec = "15min";
        OnUnitActiveSec = "1d";
      };
    };

    system.checks = [
      # This check fails the build of the system when the daily cleanup
      # of /tmp deletes a file of the directory of Claude Code.
      # hosts/nixos/tmpfiles-check.nix describes how the check runs the
      # cleanup.
      (tmpfilesKeeps {
        name = "tmpfiles-keeps-claude-tmp";
        files = [
          "${tmpDir}/claude-1000/-work-app/session/scratchpad/notes.md"
          "${tmpDir}/claude-1000/-work-app/session/tasks/a1.output"
          "${tmpDir}/claude-1000/bash-edit-diff/store/HEAD"
        ];
        tree = "the temporary directory of Claude Code";
      })
      # This check fails the build of the system when `date -d` cannot
      # read the time that expire-claude-tmp.service passes. The service
      # then fails on every run and removes no session directory.
      (pkgs.runCommand "claude-tmp-max-age-parses" { } ''
        ${pkgs.coreutils}/bin/date -d ${lib.escapeShellArg "${maxAge} ago"} >/dev/null || {
          echo "maxAge in hosts/nixos/claude-tmp.nix is not a span that date -d reads, such as '10 days'." >&2
          exit 1
        }
        touch "$out"
      '')
    ];
  };
}
