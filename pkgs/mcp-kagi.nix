{
  python312Packages,
  fetchPypi,
}:

let
  kagiapi = python312Packages.buildPythonPackage {
    pname = "kagiapi";
    version = "0.2.1";
    pyproject = true;

    src = fetchPypi {
      pname = "kagiapi";
      version = "0.2.1";
      hash = "sha256-NV/kB7TGg9bwhIJ+T4VP2VE03yhC8V0Inaz/Yg4/Sus=";
    };

    build-system = [ python312Packages.setuptools ];

    dependencies = with python312Packages; [
      requests
      typing-extensions
    ];

    pythonImportsCheck = [ "kagiapi" ];
  };
in

python312Packages.buildPythonApplication {
  pname = "kagimcp";
  version = "0.1.4";
  pyproject = true;

  src = fetchPypi {
    pname = "kagimcp";
    version = "0.1.4";
    hash = "sha256-fCFmd6BKyyeggekFsJtno394ZeswTYSRELryHQQAcyY=";
  };

  build-system = [ python312Packages.hatchling ];

  dependencies = [
    kagiapi
    python312Packages.mcp
    python312Packages.pydantic
  ];

  pythonImportsCheck = [ "kagimcp" ];
  preInstallCheck = ''
    export KAGI_API_KEY=fake
  '';
}
