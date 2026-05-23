{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.dotfiles.system.darwin.epsonScanV19II;

  version = "6.7.84";

  dmg = pkgs.fetchurl {
    url = "https://ftp.epson.com/drivers/V19II_EScan2_6784_NA.dmg";
    hash = "sha256-RpRLRzhTSD2Kr7Z+4RRhiamranWkbPEhPBzJs2DFnPw=";
  };

  # The .pkg is a Distribution wrapping 11 sub-pkgs. Deselect the TWAIN data
  # source, the GUI Utility, the standalone Scan 2 app, and the help docs;
  # keep choice0 (framework + per-model bundle + OCR + plug-ins) and choice1
  # (ICA backend the Apple Driver in NAPS2 talks to). The _2 variants
  # (choices 5-9) are gated to macOS 10.6-11 by pm_choice_selected in the
  # embedded Distribution script and are inert on modern macOS.
  choices = pkgs.writeText "epson-scan-v19ii-choices.xml" ''
    <?xml version="1.0" encoding="utf-8"?>
    <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
    <plist version="1.0">
    <array>
      <dict>
        <key>choiceIdentifier</key><string>choice2</string>
        <key>choiceAttribute</key><string>selected</string>
        <key>attributeSetting</key><integer>0</integer>
      </dict>
      <dict>
        <key>choiceIdentifier</key><string>choice3</string>
        <key>choiceAttribute</key><string>selected</string>
        <key>attributeSetting</key><integer>0</integer>
      </dict>
      <dict>
        <key>choiceIdentifier</key><string>choice4</string>
        <key>choiceAttribute</key><string>selected</string>
        <key>attributeSetting</key><integer>0</integer>
      </dict>
      <dict>
        <key>choiceIdentifier</key><string>choice10</string>
        <key>choiceAttribute</key><string>selected</string>
        <key>attributeSetting</key><integer>0</integer>
      </dict>
    </array>
    </plist>
  '';

  # /var/db is Apple's convention for persistent system state
  # (dslocal, locationd, com.apple.xpc.launchd all live there).
  stamp = "/var/db/dotfiles/epson-scan-v19ii.version";
in

{
  options.dotfiles.system.darwin.epsonScanV19II.enable = lib.mkOption {
    type = lib.types.bool;
    default = false;
    description = ''
      Install Epson's V19 II scanner driver (ICA backend + framework only)
      and the NAPS2 GUI that drives it.
    '';
  };

  config = lib.mkIf cfg.enable {
    dotfiles.system.homebrew.casks = [ "naps2" ];

    # postActivation is one of the few user-extension hooks nix-darwin
    # actually runs (see nix-darwin/modules/system/activation-scripts.nix);
    # custom-keyed entries like system.activationScripts.epsonScanV19II.text
    # are silently dropped. mkAfter appends after the existing postActivation
    # block in hosts/darwin/default.nix so activateSettings still runs first.
    system.activationScripts.postActivation.text = lib.mkAfter ''
      installed_version=$(/bin/cat "${stamp}" 2>/dev/null || true)
      if [ "$installed_version" != "${version}" ]; then
        echo "[epson-scan-v19ii] installing v${version} (drivers only, ~68MB)..." >&2
        mnt=$(/usr/bin/mktemp -d /tmp/epsonscan2-XXXXXX)
        trap '/usr/bin/hdiutil detach "$mnt" -quiet >/dev/null 2>&1 || true' EXIT
        /usr/bin/hdiutil attach "${dmg}" -nobrowse -readonly -mountpoint "$mnt" -quiet
        /usr/sbin/installer \
          -pkg "$mnt/Epson Scan 2.pkg" \
          -applyChoiceChangesXML "${choices}" \
          -target /
        /usr/bin/hdiutil detach "$mnt" -quiet
        trap - EXIT
        /bin/mkdir -p "$(/usr/bin/dirname "${stamp}")"
        /bin/echo "${version}" > "${stamp}"
      fi
    '';
  };
}
