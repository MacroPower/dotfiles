{
  lib,
  buildGoModule,
  git,
}:

buildGoModule {
  pname = "mcp-git";
  version = "0.1.0";

  src = ./.;
  vendorHash = "sha256-KHvorQVs1KIPPeC9HwKG6WmVrtZcv27ugOhMYXD5y5I=";

  nativeCheckInputs = [ git ];

  meta = {
    description = "MCP server exposing idempotent git operations";
    homepage = "https://github.com/MacroPower/dotfiles/tree/main/tools/mcp-git";
    license = lib.licenses.asl20;
    mainProgram = "mcp-git";
  };
}
