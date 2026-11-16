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
  # home/claude.nix names this directory in the instructions and the
  # settings of the git MCP server.
  cloneRoot = "/tmp/git";
  # expire-clones.service removes a clone that nothing changed for this
  # long. The span equals the age at which the daily cleanup deletes a
  # file in /tmp. The service appends " ago" to the value and passes
  # the result to `date -d`, so the value must be a relative span that
  # date reads, such as `10 days`. date rejects the tmpfiles form `10d`.
  # A check of this module fails the build of the system when date
  # cannot read the value.
  cloneMaxAge = "10 days";
in
{
  config = lib.mkIf user.dotfiles.claude.toolBundles.git.enable {
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
    # what counts as a clone. The service runs as the user who owns the
    # clones.
    systemd.services.expire-clones = {
      description = "Remove the clones under ${cloneRoot} that nothing changed in ${cloneMaxAge}";
      serviceConfig = {
        Type = "oneshot";
        User = username;
        ExecStart = utils.escapeSystemdExecArgs [
          (lib.getExe pkgs.expire-clones)
          cloneRoot
          "${cloneMaxAge} ago"
        ];
        IOSchedulingClass = "idle";
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

    # This check fails the build of the system when the daily cleanup
    # of /tmp deletes a file of a clone. It runs systemd-tmpfiles with
    # the stock rule for /tmp and the rules of this configuration, as
    # /etc/tmpfiles.d holds them. faketime sets the clock of the run 11
    # days ahead, and NO_FAKE_STAT keeps the real times on the files.
    # So every file is 11 days old to the cleanup, and the cleanup
    # deletes each file that no rule protects.
    #
    # Under --root, systemd-tmpfiles reads the user and the group of
    # each rule from the `/etc/passwd` and `/etc/group` files of the
    # root. It exits 65 when a rule names a user or a group that those
    # files lack. A module that adds a rule for its own user then fails
    # this check. So the check writes every user and every group of
    # this configuration to the fake root. The cleanup reads only the
    # names, so each entry carries the id 0.
    system.checks = [
      (pkgs.runCommand "tmpfiles-keeps-clones"
        {
          nativeBuildInputs = [ pkgs.libfaketime ];
          # NixOS sets the source to a glob over the directory.
          tmpfilesDir = lib.removeSuffix "/*" config.environment.etc."tmpfiles.d".source;
          passwd = lib.concatMapStrings (u: "${u.name}:x:0:0::/:/\n") (lib.attrValues config.users.users);
          group = lib.concatMapStrings (g: "${g.name}:x:0:\n") (lib.attrValues config.users.groups);
          # This rule names the user of the clones and its group, so
          # the run fails when the fake root lacks that user or that
          # group. It fails that way even when every other rule names
          # only root. The rule sets no age, so the cleanup leaves its
          # path alone.
          owners = "d /var/lib/owners 0755 ${username} ${config.users.users.${username}.group} -\n";
          passAsFile = [
            "passwd"
            "group"
            "owners"
          ];
        }
        ''
          mkdir -p root/etc
          cp "$passwdPath" root/etc/passwd
          cp "$groupPath" root/etc/group
          cp "$ownersPath" owners.conf
          clone=root${cloneRoot}/owner/repo
          mkdir -p "$clone/.git" root/tmp/other
          touch "$clone/.git/config" "$clone/README" root/tmp/other/file
          NO_FAKE_STAT=1 faketime -f +11d ${config.systemd.package}/bin/systemd-tmpfiles \
            --root="$PWD/root" --clean "$tmpfilesDir/tmp.conf" "$tmpfilesDir/00-nixos.conf" "$PWD/owners.conf"
          if [ -e root/tmp/other ]; then
            echo "The cleanup kept an old file in /tmp, so this check cannot tell what the cleanup deletes." >&2
            exit 1
          fi
          for file in "$clone/.git/config" "$clone/README"; do
            if [ ! -e "$file" ]; then
              echo "The cleanup of /tmp deleted ''${file#root} from a clone." >&2
              exit 1
            fi
          done
          touch "$out"
        ''
      )
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
