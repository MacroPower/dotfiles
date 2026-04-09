{
  pkgs,
  lib,
  config,
  ...
}:

let
  inherit (lib) mkOption types;
  inherit (config.lib.stylix) colors;
  inherit (config.stylix) fonts;
  cfg = config.dotfiles.firefox;
  addons = pkgs.firefox-addons;

  mkColor = name: {
    r = colors."${name}-rgb-r";
    g = colors."${name}-rgb-g";
    b = colors."${name}-rgb-b";
  };
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

        extensions.force = true;
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
            firefox-color
          ]
          ++ cfg.extraExtensions;

        extensions.settings."uBlock0@raymondhill.net".settings = {
          selectedFilterLists = [
            "user-filters"
            "ublock-filters"
            "ublock-badware"
            "ublock-privacy"
            "ublock-abuse"
            "ublock-unbreak"
            "easylist"
            "easyprivacy"
            "urlhaus-1"
            "plowe-0"
          ];
        };

        extensions.settings."frankerfacez@frankerfacez.com".settings = {
          "cfg-seen" = [ ];
          "cfg-collapsed" = [ ];
          "addons.enabled" = [
            "ffzap-core"
            "ffzap-bttv"
            "7tv-emotes"
            "chatterino-badges"
            "new-account-highlighter"
            "unread-mentions-counter"
          ];
          "p:0:addon.unread-mentions-counter.browser-notifications.enabled" = true;
          "p:0:metadata.player-stats" = true;
          "p:0:chat.points.auto-rewards" = true;
          "p:0:chat.emote-menu.icon" = true;
          "p:0:channel.auto-skip-trailer" = true;
          "p:0:channel.auto-click-off-featured" = true;
          "p:0:channel.auto-click-chat" = true;
          "p:0:chat.drops.auto-rewards" = true;
          "p:0:player.disable-content-warnings" = true;
          "p:0:chat.filtering.highlight-mentions" = true;
          "p:0:chat.filtering.highlight-basic-terms" = [
            {
              v = {
                v = "*macro*";
                t = "glob";
                c = "";
                s = false;
                h = false;
                w = true;
                p = 0;
                remove = false;
              };
            }
          ];
          "p:0:chat.filtering.highlight-basic-users" = [
            {
              v = {
                v = "agg23";
                t = "text";
                c = "";
                s = false;
                h = false;
                w = true;
                p = 0;
                remove = false;
              };
              id = "1676593422239-0.948802539741409-1";
            }
            {
              v = {
                remove = false;
                v = "Magnum_Phoenix";
                t = "text";
                c = "";
                s = false;
                h = false;
                w = true;
                p = 0;
              };
              id = "1676593432400-0.17189374847105177-2";
            }
          ];
        };

        extensions.settings."FirefoxColor@mozilla.com".settings = {
          firstRunDone = true;
          theme = {
            title = "Stylix OneDark";
            images.additional_backgrounds = [ "./bg-000.svg" ];
            colors = {
              # Window frame / tab bar background
              frame = mkColor "base00";
              frame_inactive = mkColor "base00";
              tab_background_text = mkColor "base05";
              tab_background_separator = mkColor "base02";
              tab_line = mkColor "base0E"; # purple accent on active tab
              tab_loading = mkColor "base0E";
              tab_selected = mkColor "base01"; # slightly lighter than frame
              tab_text = mkColor "base05";

              # Toolbar (navigation bar)
              toolbar = mkColor "base01";
              toolbar_text = mkColor "base05";
              toolbar_bottom_separator = mkColor "base02";
              toolbar_vertical_separator = mkColor "base02";

              # URL bar
              toolbar_field = mkColor "base00";
              toolbar_field_text = mkColor "base05";
              toolbar_field_border = mkColor "base02";
              toolbar_field_focus = mkColor "base00";
              toolbar_field_border_focus = mkColor "base0E";
              toolbar_field_highlight = mkColor "base0E";
              toolbar_field_highlight_text = mkColor "base00";
              toolbar_field_separator = mkColor "base02";

              # Buttons / icons
              button_background_active = mkColor "base02";
              icons = mkColor "base05";
              icons_attention = mkColor "base0D";

              # Popups (URL bar suggestions, menus)
              popup = mkColor "base01";
              popup_text = mkColor "base05";
              popup_border = mkColor "base02";
              popup_highlight = mkColor "base0E";
              popup_highlight_text = mkColor "base00";

              # Sidebar
              sidebar = mkColor "base00";
              sidebar_text = mkColor "base05";
              sidebar_border = mkColor "base02";
              sidebar_highlight = mkColor "base0E";
              sidebar_highlight_text = mkColor "base00";

              # New tab page
              ntp_background = mkColor "base00";
              ntp_text = mkColor "base05";
            };
          };
        };

        search = {
          default = "kagi";
          force = true;
          engines = {
            kagi = {
              name = "Kagi";
              urls = [ { template = "https://kagi.com/search?q={searchTerms}"; } ];
              icon = "https://kagi.com/favicon.ico";
              definedAliases = [ "@k" ];
            };
            "amazondotcom-us".metaData.hidden = true;
            "bing".metaData.hidden = true;
            "ebay".metaData.hidden = true;
            "wikipedia".metaData.hidden = true;
            "google".metaData.hidden = true;
            "ddg".metaData.hidden = true;
            "perplexity".metaData.hidden = true;
          };
          order = [ "kagi" ];
        };

        settings = {
          # Allow extensions to run without manual enable
          "extensions.autoDisableScopes" = 0;

          # Sidebar & Vertical Tabs
          "sidebar.revamp" = true;
          "sidebar.verticalTabs" = true;
          "sidebar.position_start" = false;

          # Fonts
          "font.name.monospace.x-western" = fonts.monospace.name;
          "font.name.sans-serif.x-western" = fonts.sansSerif.name;
          "font.name.serif.x-western" = fonts.serif.name;
          "font.size.monospace.x-western" = 19;

          # Password Manager & Autofill (using Bitwarden)
          "signon.autofillForms" = false;
          "signon.generation.enabled" = false;
          "signon.rememberSignons" = false;
          "signon.management.page.breach-alerts.enabled" = false;
          "signon.firefoxRelay.feature" = "disabled";
          "extensions.formautofill.addresses.enabled" = false;
          "extensions.formautofill.creditCards.enabled" = false;

          # AI/ML Features
          "browser.ml.chat.enabled" = false;
          "browser.ml.chat.page" = false;
          "browser.ai.control.sidebarChatbot" = "blocked";

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
