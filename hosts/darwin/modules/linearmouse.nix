{
  config,
  lib,
  ...
}:

let
  cfg = config.dotfiles.system.darwin.linearmouse;
in

{
  options.dotfiles.system.darwin.linearmouse.enable = lib.mkOption {
    type = lib.types.bool;
    default = false;
    description = ''
      Install LinearMouse and run it at login via a per-user launchd
      agent that sidesteps TCC restrictions on osascript-driven login items.
    '';
  };

  config = lib.mkIf cfg.enable {
    dotfiles.system.homebrew.casks = [ "linearmouse" ];

    # Disable Sparkle auto-updates (managed by nix-darwin via Homebrew).
    system.defaults.CustomUserPreferences."com.lujjjh.LinearMouse".SUAutomaticallyUpdate = false;

    # LinearMouse has no plist toggle for launch-at-login (uses SMAppService
    # internally, which only its own UI can flip). The shared loginItems
    # activation script can't add it either: osascript -> System Events is
    # denied by TCC and fails silently. A per-user launchd agent bypasses
    # TCC and lets launchd supervise the process directly.
    launchd.user.agents.linearmouse = {
      serviceConfig = {
        Label = "org.nixos.linearmouse";
        Program = "/Applications/LinearMouse.app/Contents/MacOS/LinearMouse";
        RunAtLoad = true;
        KeepAlive = false;
        # LinearMouse is LSUIElement=true; only load in the GUI (Aqua) session,
        # not in Background sessions used for ssh logins.
        LimitLoadToSessionType = "Aqua";
        StandardErrorPath = "/dev/null";
        StandardOutPath = "/dev/null";
      };
    };
  };
}
