{
  lib,
  buildGoModule,
}:

buildGoModule {
  pname = "mcp-destiny";
  version = "0.1.0";

  src = ./.;
  vendorHash = "sha256-gmgdLG5cwtuEj0dW5SnRfKrauC0lxDQuOOzQSngz5jo=";

  # Tests use httptest.NewServer, which binds to loopback. The Darwin sandbox
  # blocks all network access by default, and this flag allows loopback.
  __darwinAllowLocalNetworking = true;

  meta = {
    description = "MCP server (stdio) for Agg's Destiny Vault Manager";
    homepage = "https://github.com/MacroPower/dotfiles/tree/main/tools/mcp-destiny";
    license = lib.licenses.asl20;
    mainProgram = "mcp-destiny";
  };
}
