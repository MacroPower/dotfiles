{
  lib,
  stdenv,
  rustPlatform,
  fetchFromGitHub,
  installShellFiles,
  makeWrapper,
}:

rustPlatform.buildRustPackage {
  pname = "clauth";
  version = "0.16.0";

  src = fetchFromGitHub {
    owner = "uwuclxdy";
    repo = "clauth";
    rev = "v0.16.0";
    hash = "sha256-823ImaVDcXsaQ13EitckW4vxiUWDwNDLB9InT/gn4f0=";
  };

  cargoHash = "sha256-A9B6eA2Ws2PPBo/6jIAHfFkpAKwNGGACTYQzktnZDcY=";

  nativeBuildInputs = [
    installShellFiles
    makeWrapper
  ];

  doCheck = false;

  # Nix owns the binary and the completions, so turn off the self-update and
  # the first-launch prompt that edits the shell rc.
  postInstall = ''
    wrapProgram $out/bin/clauth \
      --set-default CLAUTH_NO_UPDATE 1 \
      --set-default CLAUTH_NO_COMPLETIONS 1
  ''
  + lib.optionalString (stdenv.buildPlatform.canExecute stdenv.hostPlatform) ''
    installShellCompletion --cmd clauth \
      --bash <($out/bin/clauth completions bash) \
      --fish <($out/bin/clauth completions fish) \
      --zsh <($out/bin/clauth completions zsh)
  '';

  meta = {
    description = "Claude Code account switcher and usage monitor";
    homepage = "https://github.com/uwuclxdy/clauth";
    license = lib.licenses.mit;
    mainProgram = "clauth";
  };
}
