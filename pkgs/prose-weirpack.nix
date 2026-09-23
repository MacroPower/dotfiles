{
  lib,
  stdenvNoCC,
  harper,
  zip,
  python3,
  gawk,
}:

let
  rulesDir = ../configs/harper/rules;

  # The rule name is the file stem, so the directory listing is the
  # single source of truth for every `--only` list downstream.
  ruleNames = map (lib.removeSuffix ".weir") (
    lib.filter (lib.hasSuffix ".weir") (builtins.attrNames (builtins.readDir rulesDir))
  );

  # Harper built-in rules enabled next to the Weir rules, one per line
  # with # comments, each covered by a fixture pair in the check phase.
  builtinRules = lib.filter (l: l != "" && !lib.hasPrefix "#" l) (
    map (l: lib.trim l) (lib.splitString "\n" (builtins.readFile ../configs/harper/builtin-rules.txt))
  );

  manifest = builtins.toJSON {
    author = "Jacob Colvin";
    version = "0.1.0";
    description = "Mechanical half of the prose skill";
    license = "Apache-2.0";
  };
in
stdenvNoCC.mkDerivation {
  pname = "prose-weirpack";
  version = "0.1.0";

  src = ../configs/harper;

  nativeBuildInputs = [ zip ];
  nativeCheckInputs = [
    harper
    python3
    gawk
  ];

  # Harper reads a rule's name from the archive entry's stem, so the
  # archive stays flat (`-j`). Pinned mtimes keep the zip reproducible.
  buildPhase = ''
    runHook preBuild
    printf '%s' ${lib.escapeShellArg manifest} > manifest.json
    find rules manifest.json -exec touch -d "@$SOURCE_DATE_EPOCH" {} +
    zip -X -j -q prose.weirpack rules/*.weir manifest.json
    runHook postBuild
  '';

  doCheck = true;

  # harper-cli looks for a user dictionary under $HOME and prints a
  # note when none exists; pointing HOME at the build dir keeps that
  # lookup inside the sandbox.
  checkPhase = ''
    runHook preCheck
    export HOME="$TMPDIR"
    for rule in rules/*.weir; do
      harper-cli test --no-color "$rule"
    done
    python3 check.py prose.weirpack fixtures heading.awk builtin-rules.txt
    runHook postCheck
  '';

  installPhase = ''
    runHook preInstall
    install -D -m 0644 prose.weirpack "$out/share/harper/prose.weirpack"
    install -D -m 0644 heading.awk "$out/share/harper/heading.awk"
    runHook postInstall
  '';

  passthru = {
    inherit ruleNames builtinRules;
  };

  meta = {
    description = "Weir rules that enforce the mechanical half of the prose skill";
    license = lib.licenses.asl20;
    platforms = lib.platforms.all;
  };
}
