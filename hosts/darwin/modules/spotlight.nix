{
  config,
  lib,
  ...
}:

let
  cfg = config.dotfiles.system.darwin.spotlight;
in

{
  options.dotfiles.system.darwin.spotlight.enable = lib.mkOption {
    type = lib.types.bool;
    default = false;
    description = ''
      Filter Spotlight categories (disabling web suggestions, music,
      photos, documents, etc.) and disable Spotlight indexing on
      external and network volumes via a launchd daemon that watches
      /Volumes.
    '';
  };

  config = lib.mkIf cfg.enable {
    # Spotlight: disable web suggestions and all other noisy/expensive categories
    system.defaults.CustomUserPreferences."com.apple.spotlight".orderedItems =
      let
        spotlightCategories = {
          APPLICATIONS = true; # .app bundles
          BOOKMARKS = false; # Safari and browser bookmarks
          CONTACT = true; # Contacts / address book entries
          DIRECTORIES = false; # Folder names
          DOCUMENTS = false; # Pages, Word, plain text, etc.
          EVENT_TODO = true; # Calendar events and Reminders
          FONTS = false; # Installed font families
          IMAGES = false; # Photos, screenshots, graphics
          MENU_CONVERSION = true; # Unit and currency conversions
          MENU_DEFINITION = true; # Dictionary definitions
          MENU_EXPRESSION = true; # Calculator / math expressions
          MENU_OTHER = false; # Miscellaneous results
          MENU_SPOTLIGHT_SUGGESTIONS = false; # Siri / Apple suggestions
          MENU_WEBSEARCH = false; # Web search suggestions
          MESSAGES = false; # iMessage / SMS history
          MOVIES = false; # Video files
          MUSIC = false; # Audio files and Apple Music
          PDF = false; # PDF documents
          PRESENTATIONS = false; # Keynote, PowerPoint slides
          SOURCE = false; # Source code files
          SPREADSHEETS = false; # Numbers, Excel sheets
          SYSTEM_PREFS = true; # System Settings panes
        };
      in
      map (name: {
        inherit name;
        enabled = spotlightCategories.${name};
      }) (builtins.attrNames spotlightCategories);

    # Disable Spotlight indexing on network and external volumes.
    # Watches /Volumes for mount events; also runs at boot (RunAtLoad).
    launchd.daemons.spotlight-volume-blocker = {
      serviceConfig = {
        ProgramArguments = [
          "/bin/sh"
          "-c"
          ''for vol in /Volumes/*/; do [ -d "$vol" ] && /usr/bin/mdutil -i off "$vol" 2>/dev/null; done''
        ];
        WatchPaths = [ "/Volumes" ];
        RunAtLoad = true;
        StandardErrorPath = "/dev/null";
        StandardOutPath = "/dev/null";
      };
    };
  };
}
