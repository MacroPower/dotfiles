{
  lib,
  rustPlatform,
  fetchFromGitHub,
  pkg-config,
  libxcb,
  onnxruntime,
  stdenv,
}:

let
  version = "0.1.60";
in
rustPlatform.buildRustPackage {
  pname = "claude-history";
  inherit version;

  src = fetchFromGitHub {
    owner = "raine";
    repo = "claude-history";
    rev = "v${version}";
    hash = "sha256-qwI2mXspLaHQGKhtmZzD+XJGl/D9MMglCc2e5g33IXE=";
  };

  cargoHash = "sha256-EPNu05t9+x06vNiPgRuiwpXXG3aypRGNzbpiA9f5+Jo=";

  nativeBuildInputs = [ pkg-config ];

  buildInputs = [
    onnxruntime
  ]
  ++ lib.optionals stdenv.hostPlatform.isLinux [
    libxcb
  ];

  # fastembed activates ort-sys's `download-binaries` feature, whose build
  # script fetches prebuilt ONNX Runtime tarballs from cdn.pyke.io. The Nix
  # sandbox blocks network access, so link against the system onnxruntime
  # instead. ORT_PREFER_DYNAMIC_LINK avoids ort-sys's static-link fallback.
  # See https://github.com/pykeio/ort/issues/517 for the dynamic-link flag.
  env = {
    ORT_STRATEGY = "system";
    ORT_LIB_LOCATION = "${lib.getLib onnxruntime}/lib";
    ORT_PREFER_DYNAMIC_LINK = "true";
  };

  # Tests need $HOME/.cache writes and a dyld-loadable onnxruntime; neither
  # survives the Nix sandbox (Darwin aborts with SIGABRT). Upstream's own
  # flake disables the check phase for the same reason.
  doCheck = false;

  meta = {
    description = "Fuzzy-search Claude Code conversation history from the terminal";
    homepage = "https://github.com/raine/claude-history";
    license = lib.licenses.mit;
    mainProgram = "claude-history";
  };
}
