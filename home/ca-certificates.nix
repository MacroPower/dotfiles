{
  pkgs,
  lib,
  config,
  ...
}:
let
  certs = config.dotfiles.caCertificateFiles;
  customCacert = pkgs.cacert.override { extraCertificateFiles = certs; };
  bundle = "${customCacert}/etc/ssl/certs/ca-bundle.crt";
in
{
  config = lib.mkIf (certs != [ ]) {
    dotfiles.caBundlePath = bundle;
    home.sessionVariables = {
      NIX_SSL_CERT_FILE = bundle;
      SSL_CERT_FILE = bundle;
      CURL_CA_BUNDLE = bundle;
      GIT_SSL_CAINFO = bundle;
      REQUESTS_CA_BUNDLE = bundle;
      NODE_EXTRA_CA_CERTS = bundle;
    };
  };
}
