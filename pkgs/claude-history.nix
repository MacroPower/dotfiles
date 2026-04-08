{
  lib,
  rustPlatform,
  fetchFromGitHub,
  pkg-config,
  xorg,
  stdenv,
}:

let
  version = "0.1.51";
in
rustPlatform.buildRustPackage {
  pname = "claude-history";
  inherit version;

  src = fetchFromGitHub {
    owner = "raine";
    repo = "claude-history";
    rev = "v${version}";
    hash = "sha256-WsWDsCX3Cmo6vVC7RnMfaJ1kAnhzBGLkBHGL9hvP5Rc=";
  };

  cargoHash = "sha256-dIaKrngvzQDIejKq61oqp5N8xJmnOHbgZFsm+aDlk2U=";

  nativeBuildInputs = [ pkg-config ];

  buildInputs = lib.optionals stdenv.hostPlatform.isLinux [
    xorg.libxcb
  ];

  checkFlags = [
    # Fails in Nix sandbox due to filesystem restrictions
    "--skip=history::cache::tests::cache_file_roundtrip"
    # Upstream does not recognise linux/aarch64 yet
    "--skip=update::tests::test_platform_suffix_current"
  ];

  meta = {
    description = "Fuzzy-search Claude Code conversation history from the terminal";
    homepage = "https://github.com/raine/claude-history";
    license = lib.licenses.mit;
    mainProgram = "claude-history";
  };
}
