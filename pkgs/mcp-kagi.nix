{
  python312Packages,
  fetchPypi,
}:

let
  # fastmcp's py-key-value-aio dependency pulls aioboto3/moto/cfn-lint into
  # its test closure, and cfn-lint's integration tests fail at the current
  # nixpkgs pin. The store backends are test-only there; skip the suite.
  ps = python312Packages.overrideScope (
    _: prev: {
      py-key-value-aio = prev.py-key-value-aio.overridePythonAttrs (_: {
        doCheck = false;
      });
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
