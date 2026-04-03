# Nerd Font glyph helpers and named icon constants.
# nf: BMP (4-digit) codepoints; nf2: supplementary (surrogate pair).
let
  nf = code: builtins.fromJSON ''"\u${code}"'';
  nf2 = hi: lo: builtins.fromJSON ''"\u${hi}\u${lo}"'';
in
{
  inherit nf nf2;

  icons = {
    # Powerline
    pl = nf "E0B0";
    plr = nf "E0B2";

    # Shell / prompt
    lock = nf2 "DB80" "DF3E";
    middleDot = nf "00b7";
    error = nf "f467";
    timer = nf2 "DB86" "DD9F";
    terminal = nf "f120";
    gear = nf "f013";
    container = nf "f4b7";

    # VCS
    branch = nf "f418";
    gitTag = nf "f412";

    # Infrastructure
    kubernetes = nf "2638";
    docker = nf "f308";
    aws = nf "e33d";
    azure = nf "ebd8";
    gcloud = nf "e7f1";

    # Languages & tools
    bun = nf "e76f";
    buf = nf "f49d";
    c = nf "e61e";
    cmake = nf "e794";
    conda = nf "f10c";
    cpp = nf "e61d";
    crystal = nf "e62f";
    dart = nf "e798";
    deno = nf "e7c0";
    direnv = nf "f07c";
    elixir = nf "e62d";
    elm = nf "e62c";
    fennel = nf "e6af";
    fortran = nf "e7de";
    golang = nf "e627";
    gradle = nf "e660";
    guixShell = nf "f325";
    haskell = nf "e777";
    haxe = nf "e666";
    java = nf "e256";
    julia = nf "e624";
    kotlin = nf "e634";
    lua = nf "e620";
    memory = nf2 "DB80" "DF5B";
    meson = nf2 "DB81" "DD37";
    nim = nf2 "DB80" "DDA5";
    nix = nf "f313";
    nodejs = nf "e718";
    ocaml = nf "e67a";
    package = nf2 "DB80" "DFD7";
    perl = nf "e67e";
    php = nf "e608";
    pulumi = nf "f1b2";
    python = nf "e235";
    rlang = nf2 "DB81" "DFD4";
    ruby = nf "e791";
    rust = nf2 "DB85" "DE17";
    scala = nf "e737";
    swift = nf "e755";
    terraform = nf2 "db84" "dc62";
    xmake = nf "e794";
    zig = nf "e6a9";

    # OS symbols
    os = {
      Alpaquita = nf "eaa2";
      Alpine = nf "f300";
      AlmaLinux = nf "f31d";
      Amazon = nf "f270";
      Android = nf "f17b";
      AOSC = nf "f301";
      Arch = nf "f303";
      Artix = nf "f31f";
      CachyOS = nf "f303";
      CentOS = nf "f304";
      Debian = nf "f306";
      DragonFly = nf "e28e";
      Elementary = nf "f309";
      Emscripten = nf "f205";
      EndeavourOS = nf "f197";
      Fedora = nf "f30a";
      FreeBSD = nf "f30c";
      Garuda = nf2 "DB81" "DED3";
      Gentoo = nf "f30d";
      HardenedBSD = nf2 "DB81" "DF8C";
      Illumos = nf2 "DB80" "DE38";
      Ios = nf2 "DB80" "DC37";
      Kali = nf "f327";
      Linux = nf "f31a";
      Mabox = nf "eb29";
      Macos = nf "f302";
      Manjaro = nf "f312";
      Mariner = nf "f1cd";
      MidnightBSD = nf "f186";
      Mint = nf "f30e";
      NetBSD = nf "f024";
      NixOS = nf "f313";
      Nobara = nf "f380";
      OpenBSD = nf2 "DB80" "DE3A";
      openSUSE = nf "f314";
      OracleLinux = nf2 "DB80" "DF37";
      Pop = nf "f32a";
      Raspbian = nf "f315";
      Redhat = nf "f316";
      RedHatEnterprise = nf "f316";
      RockyLinux = nf "f32b";
      Redox = nf2 "DB80" "DC18";
      Solus = nf2 "DB82" "DC33";
      SUSE = nf "f314";
      Ubuntu = nf "f31b";
      Unknown = nf "f22d";
      Void = nf "f32e";
      Windows = nf2 "DB80" "DF72";
      Zorin = nf "f32f";
    };
  };
}
