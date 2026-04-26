{
  lib,
  buildGoModule,
}:

buildGoModule {
  pname = "mcp-fetch";
  version = "0.1.0";

  src = ./.;
  vendorHash = "sha256-VHiEFD6UVWmU6+A+grzhFU0AqfWv6+MeaqUXgmON//o=";

  # Tests use httptest.NewServer, which binds to loopback. The Darwin sandbox
  # blocks all network access by default; this flag whitelists loopback.
  __darwinAllowLocalNetworking = true;

  meta = {
    description = "MCP fetch server (stdio) for Claude Code";
    homepage = "https://github.com/MacroPower/dotfiles/tree/main/tools/mcp-fetch";
    license = lib.licenses.asl20;
    mainProgram = "mcp-fetch";
  };
}
