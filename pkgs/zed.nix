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
  version = "0.231.2";

  srcs = {
    "aarch64-darwin" = {
      url = "https://github.com/zed-industries/zed/releases/download/v${version}/Zed-aarch64.dmg";
      hash = "sha256-NbLDOKJg3h6kpKEPV5wURwMh1C8xR73QhWn2FJvk/2M=";
    };
    "aarch64-linux" = {
      url = "https://github.com/zed-industries/zed/releases/download/v${version}/zed-linux-aarch64.tar.gz";
      hash = "sha256-IaVGyhLav6/AzXXO65KSAgMBB6XFUmY7twPHG5hPKxQ=";
    };
    "x86_64-linux" = {
      url = "https://github.com/zed-industries/zed/releases/download/v${version}/zed-linux-x86_64.tar.gz";
      hash = "sha256-kgZRPji4o3zyfodL/z38ZebdGrH0rg++X+Eq90Zdw9w=";
    };
  };

  remoteServerSrcs = {
    "aarch64-linux" = {
      url = "https://github.com/zed-industries/zed/releases/download/v${version}/zed-remote-server-linux-aarch64.gz";
      hash = "sha256-tuzkIy4jNSbWgdfHI4Q8kAZqVyG+OJilZOQvto+7jS8=";
    };
    "x86_64-linux" = {
      url = "https://github.com/zed-industries/zed/releases/download/v${version}/zed-remote-server-linux-x86_64.gz";
      hash = "sha256-HIDGo4Fa2PWqh9ezmGepFSBlM9U46H1bViJ7wkaQ2NM=";
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
