{ config, ... }:

{
  sops = {
    defaultSopsFile = ../secrets/dotfiles.yaml;
    age.keyFile = "${config.home.homeDirectory}/.config/sops/age/keys.txt";

    secrets = {
      kagi_api_key.key = "KAGI_API_KEY";
      gh_token.key = "GH_TOKEN";
    };
  };
}
