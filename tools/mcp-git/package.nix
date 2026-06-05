{
  lib,
  buildGoModule,
  git,
}:

buildGoModule {
  pname = "mcp-git";
  version = "0.1.0";

  src = ./.;
  vendorHash = "sha256-gmgdLG5cwtuEj0dW5SnRfKrauC0lxDQuOOzQSngz5jo=";

  nativeCheckInputs = [ git ];

  meta = {
    description = "MCP server exposing idempotent git operations";
    homepage = "https://github.com/MacroPower/dotfiles/tree/main/tools/mcp-git";
    license = lib.licenses.asl20;
    mainProgram = "mcp-git";
  };
}
