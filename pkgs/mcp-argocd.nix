{
  lib,
  buildNpmPackage,
  fetchFromGitHub,
}:

buildNpmPackage rec {
  pname = "mcp-argocd";
  version = "0.7.0";

  src = fetchFromGitHub {
    owner = "argoproj-labs";
    repo = "mcp-for-argocd";
    rev = "v${version}";
    hash = "sha256-o8hBmiOize9fpDQyS8NHG4jVXF8grfMgIHFTg6F10SQ=";
  };

  npmDepsHash = "sha256-WocJa1rz6Ax+L2S1tDAk5B6vVt8BguV49ivehr4eYPU=";
  npmDepsFetcherVersion = 2;

  # Upstream package.json drifted from package-lock.json: it requires
  # `@modelcontextprotocol/sdk@^1.29.0` while the lockfile still pins 1.27.1,
  # so `npm ci` fails with ETARGET. Align the constraint with the lockfile.
  postPatch = ''
    substituteInPlace package.json \
      --replace-fail \
        '"@modelcontextprotocol/sdk": "^1.29.0"' \
        '"@modelcontextprotocol/sdk": "^1.27.1"'
  '';

  meta = {
    description = "MCP server for Argo CD";
    homepage = "https://github.com/argoproj-labs/mcp-for-argocd";
    license = lib.licenses.asl20;
    mainProgram = "argocd-mcp";
  };
}
