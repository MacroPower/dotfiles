{
  config,
  lib,
  ...
}:

let
  cfg = config.dotfiles.system.darwin.bluetoothAac;
in

{
  options.dotfiles.system.darwin.bluetoothAac.enable = lib.mkOption {
    type = lib.types.bool;
    default = false;
    description = ''
      Force the maximum AAC bitpool (80) and 320 kbps bitrate for
      Bluetooth audio, preventing the codec from falling back to
      lower quality under contention.
    '';
  };

  config = lib.mkIf cfg.enable {
    system.defaults.CustomUserPreferences = {
      # Force the maximum AAC bitpool (80) for Bluetooth audio negotiation,
      # preventing the codec from dropping to lower quality under contention
      "com.apple.BluetoothAudioAgent" = {
        "Apple Bitpool Max (editable)" = 80;
        "Apple Bitpool Min (editable)" = 80;
        "Apple Initial Bitpool (editable)" = 80;
        "Apple Initial Bitpool Min (editable)" = 80;
        "Negotiated Bitpool" = 80;
        "Negotiated Bitpool Max" = 80;
        "Negotiated Bitpool Min" = 80;
      };

      # Lower-level Bluetooth daemon settings: pin AAC at 320 kbps,
      # raise the packet ceiling, and enable both AAC and AptX codecs
      bluetoothaudiod = {
        "AAC Bitrate" = 320;
        "AAC max packet size" = 644;
        "Apple Bitpool Max" = 80;
        "Apple Bitpool Min" = 80;
        "Apple Initial Bitpool Min" = 80;
        "Apple Initial Bitpool" = 80;
        "Enable AAC codec" = true;
        "Enable AptX codec" = true;
        "Negotiated Bitpool Max" = 80;
        "Negotiated Bitpool Min" = 80;
        "Negotiated Bitpool" = 80;
      };
    };
  };
}
