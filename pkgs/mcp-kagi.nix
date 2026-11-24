{
  python3Packages,
  fetchPypi,
}:

let
  # nixpkgs marks fastmcp-slim broken when its mcp argument is 2.x or
  # later. nixpkgs' mcp-nixos builds against a scope that swaps in mcp_1,
  # and Hydra builds fastmcp in that scope. Building against the same
  # scope lets every dependency substitute from cache.nixos.org, so only
  # kagimcp itself builds locally. Keep this scope identical to the one
  # in nixpkgs' pkgs/by-name/mc/mcp-nixos/package.nix.
  ps = python3Packages.overrideScope (
    final: _prev: {
      mcp = final.mcp_1;
    }
  );
in

ps.buildPythonApplication {
  pname = "kagimcp";
  version = "1.0.0";
  pyproject = true;

  src = fetchPypi {
    pname = "kagimcp";
    version = "1.0.0";
    hash = "sha256-Yq7NM0OL17r9O0/Xv1JBCeqr/Vn9Zti1EFvdjmKbapw=";
  };

  build-system = [ ps.hatchling ];

  # The sdist pins pydantic~=2.12.5; nixpkgs moves faster than Kagi's
  # generated client, and the pin is a codegen artifact, not a real
  # ceiling. Relax it and let the imports check catch actual breakage.
  pythonRelaxDeps = [ "pydantic" ];

  dependencies = with ps; [
    fastmcp
    pydantic
    urllib3
    python-dateutil
    typing-extensions
  ];

  # The sdist bundles a generated openapi_client for the Kagi v1 API.
  pythonImportsCheck = [
    "kagimcp"
    "openapi_client"
  ];
}
