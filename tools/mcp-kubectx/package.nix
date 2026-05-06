{
  lib,
  buildGoModule,
}:

buildGoModule {
  pname = "mcp-kubectx";
  version = "0.1.0";

  src = ./.;
  vendorHash = "sha256-5Qjzp60MwfLPTEPVKRtoXgf0WCgqO/kA9Qm24MBQkeQ=";

  meta = {
    description = "MCP server for Kubernetes context selection with ServiceAccount-scoped credentials";
    homepage = "https://github.com/MacroPower/dotfiles/tree/main/tools/mcp-kubectx";
    license = lib.licenses.asl20;
    mainProgram = "mcp-kubectx";
  };
}
