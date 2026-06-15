{
  lib,
  buildGoModule,
}:

buildGoModule {
  pname = "mcp-http-proxy";
  version = "0.1.0";

  src = ./.;
  vendorHash = "sha256-gmgdLG5cwtuEj0dW5SnRfKrauC0lxDQuOOzQSngz5jo=";

  # Several tests use httptest.NewServer, which binds to loopback. The Darwin
  # sandbox blocks all network access by default; this flag whitelists loopback.
  __darwinAllowLocalNetworking = true;

  meta = {
    description = "Stdio MCP server proxying to an upstream Streamable HTTP endpoint";
    homepage = "https://github.com/MacroPower/dotfiles/tree/main/tools/mcp-http-proxy";
    license = lib.licenses.asl20;
    mainProgram = "mcp-http-proxy";
  };
}
