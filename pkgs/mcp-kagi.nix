{
  python312Packages,
  fetchPypi,
}:

let
  # py-key-value-aio's check suite is skipped globally by
  # pyKeyValueAioOverlay in lib/default.nix, so the package set can be
  # used as-is here.
  ps = python312Packages;
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
