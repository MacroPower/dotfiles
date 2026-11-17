{
  lib,
  buildGoModule,
}:

buildGoModule {
  pname = "mcp-kubectx";
  version = "0.1.0";

  src = ./.;
  vendorHash = "sha256-SEZxWum0M7DMVJVsNE+9/fH3etEICRDCoZEybgQXI3Q=";

  # Started as mcp-kubectx-host, the binary runs only its `host`
  # subcommands. A Lima guest reaches the host through `workmux
  # host-exec mcp-kubectx-host`, so the guest cannot start `serve` or
  # `exec-plugin` on the host.
  postInstall = ''
    ln -s mcp-kubectx $out/bin/mcp-kubectx-host
  '';

  meta = {
    description = "MCP server for Kubernetes context selection with ServiceAccount-scoped credentials";
    homepage = "https://github.com/MacroPower/dotfiles/tree/main/tools/mcp-kubectx";
    license = lib.licenses.asl20;
    mainProgram = "mcp-kubectx";
  };
}
