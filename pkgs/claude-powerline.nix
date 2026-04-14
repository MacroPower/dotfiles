{
  lib,
  stdenv,
  fetchurl,
  nodejs,
  makeWrapper,
}:

let
  version = "1.24.4";
in
stdenv.mkDerivation {
  pname = "claude-powerline";
  inherit version;

  src = fetchurl {
    url = "https://registry.npmjs.org/@owloops/claude-powerline/-/claude-powerline-${version}.tgz";
    hash = "sha256-mR/OsXEEwHwqZM5onWeM/0stoLIbVJhsyFJBVleaNjE=";
  };

  nativeBuildInputs = [ makeWrapper ];

  dontUnpack = true;

  installPhase = ''
    mkdir -p $out/lib/claude-powerline $out/bin
    tar xzf $src --strip-components=1 -C $out/lib/claude-powerline
    makeWrapper ${nodejs}/bin/node $out/bin/claude-powerline \
      --add-flags "$out/lib/claude-powerline/bin/claude-powerline"
  '';

  meta = {
    description = "Powerline statusline for Claude Code with real-time usage tracking and custom themes";
    homepage = "https://github.com/Owloops/claude-powerline";
    license = lib.licenses.mit;
    mainProgram = "claude-powerline";
  };
}
