{ config, ... }:

{
  sops = {
    defaultSopsFile = ../secrets/dotfiles.yaml;
    age.keyFile = "${config.home.homeDirectory}/.config/sops/age/keys.txt";

    secrets = {
      kagi_api_key.key = "KAGI_API_KEY";
      gh_token.key = "GH_TOKEN";
      argocd_api_token.key = "ARGOCD_API_TOKEN";
      argocd_base_url.key = "ARGOCD_BASE_URL";
      dagger_cloud_token.key = "DAGGER_CLOUD_TOKEN";
    };
  };
}
