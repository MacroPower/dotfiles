{
  lib,
  buildGoModule,
}:

buildGoModule {
  pname = "mcp-opentofu";
  version = "0.1.0";

  src = ./.;
  vendorHash = "sha256-kQ0b62J+KLjgwQdvCQUPsFsSZT6kUl+cOA5tjGlybl0=";

  # Tests use httptest.NewServer, which binds to loopback. The Darwin sandbox
  # blocks all network access by default; this flag whitelists loopback.
  __darwinAllowLocalNetworking = true;

  meta = {
    description = "MCP OpenTofu Registry server (stdio) for Claude Code";
    homepage = "https://github.com/MacroPower/dotfiles/tree/main/tools/mcp-opentofu";
    license = lib.licenses.asl20;
    mainProgram = "mcp-opentofu";
  };
}
