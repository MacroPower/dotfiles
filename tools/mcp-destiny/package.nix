{
  lib,
  buildGoModule,
}:

buildGoModule {
  pname = "mcp-destiny";
  version = "0.1.0";

  src = ./.;
  vendorHash = "sha256-Wzn+EhaiACTX6ZJzeThaMGpc1kv13UaNa4YalUpuWq0=";

  # Tests use httptest.NewServer, which binds to loopback. The Darwin sandbox
  # blocks all network access by default, and this flag allows loopback.
  __darwinAllowLocalNetworking = true;

  # The upstream commit that the server was last checked against. The server
  # mirrors the web client and the service of Agg's Destiny Vault Manager by
  # hand, and an upgrade diffs upstream from this commit.
  passthru.upstream = {
    url = "https://github.com/agg23/destiny-inventory";
    rev = "305e8884cc33402f00477ebd578d49f36a3276df"; # 2026-11-11, Add dupe check to miss example rule
  };

  meta = {
    description = "MCP server (stdio) for Agg's Destiny Vault Manager";
    homepage = "https://github.com/MacroPower/dotfiles/tree/main/tools/mcp-destiny";
    license = lib.licenses.asl20;
    mainProgram = "mcp-destiny";
  };
}
