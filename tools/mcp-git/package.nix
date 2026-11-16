{
  lib,
  buildGoModule,
  git,
}:

buildGoModule {
  pname = "mcp-git";
  version = "0.2.0";

  src = ./.;
  vendorHash = "sha256-gmgdLG5cwtuEj0dW5SnRfKrauC0lxDQuOOzQSngz5jo=";

  nativeCheckInputs = [ git ];

  # Tests use httptest.NewServer, which binds to loopback. The Darwin sandbox
  # blocks all network access by default, and this flag allows loopback.
  __darwinAllowLocalNetworking = true;

  meta = {
    description = "MCP server exposing idempotent git operations";
    homepage = "https://github.com/MacroPower/dotfiles/tree/main/tools/mcp-git";
    license = lib.licenses.asl20;
    mainProgram = "mcp-git";
  };
}
