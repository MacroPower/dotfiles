{ config, lib, ... }:

lib.mkIf config.dotfiles.sops.enable {
  sops = {
    defaultSopsFile = ../secrets/dotfiles.yaml;
    age.keyFile = "${config.home.homeDirectory}/.config/sops/age/keys.txt";

    secrets = {
      kagi_api_key.key = "KAGI_API_KEY";
      gh_token.key = "GH_TOKEN";
      argocd_api_token.key = "ARGOCD_API_TOKEN";
      argocd_base_url.key = "ARGOCD_BASE_URL";
      dagger_cloud_token.key = "DAGGER_CLOUD_TOKEN";
      spacelift_api_key_endpoint.key = "SPACELIFT_API_KEY_ENDPOINT";
      spacelift_api_key_id.key = "SPACELIFT_API_KEY_ID";
      spacelift_api_key_secret.key = "SPACELIFT_API_KEY_SECRET";
    };
  };
}
