{
  lib,
  stdenvNoCC,
  stdenv,
  fetchurl,
  _7zz,
  autoPatchelfHook,
  gzip,
  alsa-lib,
  fontconfig,
  freetype,
  libgcc,
  libGL,
  libxkbcommon,
  openssl,
  vulkan-loader,
  wayland,
  libx11,
  libxcb,
  libxcursor,
  libxi,
  libxrandr,
  zlib,
}:

let
  version = "0.232.2";

  srcs = {
    "aarch64-darwin" = {
      url = "https://github.com/zed-industries/zed/releases/download/v${version}/Zed-aarch64.dmg";
      hash = "sha256-gnF93qTAOtAzqcmdFEDRtx1vYUGwLu3Rqhy46z3Lk48=";
    };
    "aarch64-linux" = {
      url = "https://github.com/zed-industries/zed/releases/download/v${version}/zed-linux-aarch64.tar.gz";
      hash = "sha256-9G9boyDEFVVErN+dBNzjPrcf5G8Wp4bUgB9LJ8Ai7ow=";
    };
    "x86_64-linux" = {
      url = "https://github.com/zed-industries/zed/releases/download/v${version}/zed-linux-x86_64.tar.gz";
      hash = "sha256-wpu3nOOOnMvkDZVtmgJVnA42iRb/JyuJK9WF6IlBX5M=";
    };
  };

  remoteServerSrcs = {
    "aarch64-linux" = {
      url = "https://github.com/zed-industries/zed/releases/download/v${version}/zed-remote-server-linux-aarch64.gz";
      hash = "sha256-c8fPtU0U9wV785OKcJqqA011L3nDvsDYhfHGnAep0r0=";
    };
    "x86_64-linux" = {
      url = "https://github.com/zed-industries/zed/releases/download/v${version}/zed-remote-server-linux-x86_64.gz";
      hash = "sha256-UR4/NbVzCDr2/I+hvhid6OPMcuFVzjTC7w0gGzimH0U=";
    };
  };

  src =
    srcs.${stdenvNoCC.hostPlatform.system}
      or (throw "Unsupported system: ${stdenvNoCC.hostPlatform.system}");

  inherit (stdenvNoCC) isDarwin;
  inherit (stdenvNoCC) isLinux;

  linuxLibs = [
    alsa-lib
    fontconfig
    freetype
    libgcc.lib
    libGL
    libxkbcommon
    openssl
    stdenv.cc.cc.lib
    vulkan-loader
    wayland
    libx11
    libxcb
    libxcursor
    libxi
    libxrandr
    zlib
  ];
in
stdenvNoCC.mkDerivation {
  pname = "zed-bin";
  inherit version;

  src = fetchurl { inherit (src) url hash; };

  sourceRoot = ".";
  dontStrip = true;

  nativeBuildInputs = lib.optionals isDarwin [ _7zz ] ++ lib.optionals isLinux [ autoPatchelfHook ];

  unpackPhase = lib.optionalString isDarwin ''
    7zz x $src -snld
  '';

  buildInputs = lib.optionals isLinux linuxLibs;

  installPhase =
    if isDarwin then
      ''
        mkdir -p $out/Applications $out/bin
        cp -R "Zed.app" $out/Applications/
        ln -s $out/Applications/Zed.app/Contents/MacOS/cli $out/bin/zed
      ''
    else
      ''
        mkdir -p $out
        cp -R zed.app/bin zed.app/libexec zed.app/share $out/
      '';

  passthru.remote_server =
    let
      remoteSrc =
        remoteServerSrcs.${stdenvNoCC.hostPlatform.system}
          or (throw "No remote server for: ${stdenvNoCC.hostPlatform.system}");
    in
    stdenvNoCC.mkDerivation {
      pname = "zed-remote-server";
      inherit version;

      src = fetchurl { inherit (remoteSrc) url hash; };

      dontUnpack = true;

      nativeBuildInputs = [
        gzip
        autoPatchelfHook
      ];

      buildInputs = [ stdenv.cc.cc.lib ];

      installPhase = ''
        mkdir -p $out/bin
        gzip -d < $src > $out/bin/zed-remote-server
        chmod +x $out/bin/zed-remote-server
      '';
    };

  meta = {
    description = "High-performance multiplayer code editor";
    homepage = "https://zed.dev";
    license = lib.licenses.gpl3;
    platforms = builtins.attrNames srcs;
    mainProgram = "zed";
  };
}
