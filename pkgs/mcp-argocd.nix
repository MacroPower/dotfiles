{
  lib,
  buildNpmPackage,
  fetchFromGitHub,
}:

buildNpmPackage rec {
  pname = "mcp-argocd";
  version = "0.6.0";

  src = fetchFromGitHub {
    owner = "argoproj-labs";
    repo = "mcp-for-argocd";
    rev = "v${version}";
    hash = "sha256-EZE62Ed6AvMLMEwDH0mAd1ocJAg7MTxbIiP39GxMY64=";
  };

  npmDepsHash = "sha256-QAE4FA9Aqib4YjZ4Y4nNbxeAmfQRAWwaEzdmCbNUelU=";

  meta = {
    description = "MCP server for Argo CD";
    homepage = "https://github.com/argoproj-labs/mcp-for-argocd";
    license = lib.licenses.asl20;
    mainProgram = "argocd-mcp";
  };
}
