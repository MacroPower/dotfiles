{
  lib,
  stdenv,
  fetchFromGitHub,
  nodejs_25,
  pnpm_9,
  fetchPnpmDeps,
  pnpmConfigHook,
  makeWrapper,
}:

stdenv.mkDerivation (finalAttrs: {
  pname = "mcp-opentofu";
  version = "1.0.0";

  src = fetchFromGitHub {
    owner = "opentofu";
    repo = "opentofu-mcp-server";
    tag = "v${finalAttrs.version}";
    hash = "sha256-qgjAnoduzAjvxgbgG8QW53CMF3/bW0NQhDbVv3ebntw=";
  };

  pnpmDeps = fetchPnpmDeps {
    inherit (finalAttrs) pname version src;
    pnpm = pnpm_9;
    fetcherVersion = 2;
    hash = "sha256-XvP7yJXmfm7+3/4i2fhjooJQk+18aHiZzjfmt4l+HyM=";
  };

  nativeBuildInputs = [
    nodejs_25
    pnpm_9
    pnpmConfigHook
    makeWrapper
  ];

  buildPhase = ''
    runHook preBuild
    pnpm build
    runHook postBuild
  '';

  installPhase = ''
    runHook preInstall
    mkdir -p $out/lib/mcp-opentofu $out/bin
    cp -r dist node_modules package.json $out/lib/mcp-opentofu/
    makeWrapper ${nodejs_25}/bin/node $out/bin/mcp-opentofu \
      --add-flags "$out/lib/mcp-opentofu/dist/local.js"
    runHook postInstall
  '';

  meta = {
    description = "OpenTofu Registry MCP server (local stdio)";
    homepage = "https://github.com/opentofu/opentofu-mcp-server";
    license = lib.licenses.mpl20;
    mainProgram = "mcp-opentofu";
  };
})
