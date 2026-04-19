{
  lib,
  buildGoModule,
}:

buildGoModule {
  pname = "mcp-http-proxy";
  version = "0.1.0";

  src = ./.;
  vendorHash = "sha256-KHvorQVs1KIPPeC9HwKG6WmVrtZcv27ugOhMYXD5y5I=";

  # httptest.NewServer binds a TCP listener, which the Nix build sandbox
  # does not permit. These two tests intentionally exercise the HTTP
  # transport and are run outside Nix via `go test ./...` / `task test`.
  checkFlags = [
    "-skip=^TestProxyRoundTrip$|^TestLogFile$"
  ];

  meta = {
    description = "Stdio MCP server proxying to an upstream Streamable HTTP endpoint";
    license = lib.licenses.asl20;
    mainProgram = "mcp-http-proxy";
  };
}
