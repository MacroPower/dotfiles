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

  # configs/harper/rules.toml lists every enabled rule under its tier,
  # so the manifest is the single source of truth for the `--only`
  # lists downstream and for the tier tag prose-lint prints. A name
  # with a rules/<Name>.weir file is a Weir rule, ProseHeading is the
  # awk check, and the rest are Harper built-ins. check.py fails the
  # build when a .weir file is missing from the manifest.
  manifestRules = builtins.fromTOML (builtins.readFile ../configs/harper/rules.toml);

  tierOrder = [
    "required"
    "recommended"
    "optional"
  ];

  # Rule name to tier label, capitalized the way prose-lint prints it.
  tiers = lib.listToAttrs (
    lib.concatMap (
      tier: map (name: lib.nameValuePair name (lib.toSentenceCase tier)) manifestRules.${tier}.rules
    ) tierOrder
  );

  allNames = builtins.attrNames tiers;

  ruleNames = lib.filter (name: builtins.pathExists (rulesDir + "/${name}.weir")) allNames;

  builtinRules = lib.filter (name: !(lib.elem name ruleNames) && name != "ProseHeading") allNames;

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
    python3 check.py prose.weirpack fixtures heading.awk rules.toml
    runHook postCheck
  '';

  installPhase = ''
    runHook preInstall
    install -D -m 0644 prose.weirpack "$out/share/harper/prose.weirpack"
    install -D -m 0644 heading.awk "$out/share/harper/heading.awk"
    runHook postInstall
  '';

  passthru = {
    inherit ruleNames builtinRules tiers;
  };

  meta = {
    description = "Weir rules that enforce the mechanical half of the prose skill";
    license = lib.licenses.asl20;
    platforms = lib.platforms.all;
  };
}
