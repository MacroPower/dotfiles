{
  lib,
  buildGoModule,
}:

buildGoModule {
  pname = "copilot-api-proxy";
  version = "0.1.0";

  src = ./.;
  vendorHash = "sha256-yqtj+zCo7u2UwaQ12bHCHPNucgQNkvkN7nfkLynB67Y=";

  # The test suite binds loopback listeners (httptest). The macOS build
  # sandbox blocks all networking by default, including 127.0.0.1, so allow
  # local networking for the check phase. No effect on Linux.
  __darwinAllowLocalNetworking = true;

  meta = {
    description = "Anthropic Messages API proxy backed by a GitHub Copilot subscription";
    homepage = "https://github.com/MacroPower/dotfiles/tree/main/tools/copilot-api-proxy";
    license = lib.licenses.asl20;
    mainProgram = "copilot-api-proxy";
  };
}
