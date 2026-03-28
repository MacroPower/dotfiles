{
  pkgs,
  lib,
  config,
  ...
}:

let
  inherit (lib) mkOption types;
  cfg = config.dotfiles.firefox;
  addons = pkgs.firefox-addons;
in
{
  options.dotfiles.firefox = {
    extraExtensions = mkOption {
      type = types.listOf types.package;
      default = [ ];
      description = "Additional Firefox extensions to install.";
    };
  };

  config = {
    stylix.targets.firefox.profileNames = [ "default" ];

    programs.firefox = {
      enable = true;
      package = if pkgs.stdenv.hostPlatform.isDarwin then pkgs.firefox-bin else null;

      profiles.default = {
        isDefault = true;

        extensions.packages =
          with addons;
          [
            ublock-origin
            bitwarden
            darkreader
            frankerfacez
            kagi-search
            sponsorblock
            reddit-enhancement-suite
          ]
          ++ cfg.extraExtensions;

        search = {
          default = "Kagi";
          force = true;
        };

        settings = {
          # Allow extensions to run without manual enable
          "extensions.autoDisableScopes" = 0;

          # Disable telemetry
          "toolkit.telemetry.enabled" = false;
          "toolkit.telemetry.unified" = false;
          "toolkit.telemetry.archive.enabled" = false;
          "datareporting.healthreport.uploadEnabled" = false;
          "datareporting.policy.dataSubmissionEnabled" = false;
          "browser.ping-centre.telemetry" = false;

          # Disable Pocket
          "extensions.pocket.enabled" = false;
          "browser.newtabpage.activity-stream.feeds.section.topstories" = false;

          # Disable activity stream telemetry and sponsored content
          "browser.newtabpage.activity-stream.telemetry" = false;
          "browser.newtabpage.activity-stream.feeds.telemetry" = false;
          "browser.newtabpage.activity-stream.showSponsored" = false;
          "browser.newtabpage.activity-stream.showSponsoredTopSites" = false;

          # Disable crash reporter
          "browser.crashReports.unsubmittedCheck.autoSubmit2" = false;
          "browser.tabs.crashReporting.sendReport" = false;

          # Disable studies and experiments
          "app.shield.optoutstudies.enabled" = false;
          "app.normandy.enabled" = false;
        };
      };
    };
  };
}
